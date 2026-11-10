// Package gamedir finds the WoW: Forever flavour folder (G) and the running
// game process.
package gamedir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrUnsupported is returned by OS-specific code that has no implementation
// for this platform yet.
var ErrUnsupported = errors.New("unsupported on this OS yet")

const forever = "_forever_"

// Patterns lists the glob patterns tried for G, relative to home (PROTOCOL §1).
func Patterns(home string) []string {
	pf := filepath.Join("drive_c", "Program Files (x86)", "World of Warcraft", forever)
	return []string{
		filepath.Join(home, "Games", "*", pf),
		filepath.Join(home, ".wine", pf),
		filepath.Join(home, "Games", "*", "pfx", pf),
		filepath.Join(home, ".local", "share", "Steam", "steamapps", "compatdata", "*", "pfx", pf),
		filepath.Join("/Applications", "World of Warcraft", forever),
	}
}

// Detect globs patterns and returns the existing directories, deduplicated and
// sorted.
func Detect(patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var found []string
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			m = filepath.Clean(m)
			if st, err := os.Stat(m); err != nil || !st.IsDir() || seen[m] {
				continue
			}
			seen[m] = true
			found = append(found, m)
		}
	}
	sort.Strings(found)
	return found, nil
}

// DetectOne returns the only candidate, or an error listing 0 or >1 of them.
func DetectOne(patterns []string) (string, error) {
	found, err := Detect(patterns)
	if err != nil {
		return "", err
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", errors.New("no WoW _forever_ folder found; pass --game DIR")
	}
	return "", fmt.Errorf("several WoW _forever_ folders found, pass --game DIR with one of:\n  %s",
		strings.Join(found, "\n  "))
}

// IsGameCmdline reports whether a process cmdline belongs to the game
// (PROTOCOL §1.4): an arg containing both `_forever_` and `Wow.exe`
// (case-insensitive). strong is true when an arg is itself the Wow.exe path
// under a `_forever_` folder (ends in `\Wow.exe` or `/Wow.exe`), as opposed to
// a launcher script that merely mentions both.
func IsGameCmdline(args []string) (match, strong bool) {
	for _, a := range args {
		l := strings.ToLower(a)
		hasForever := strings.Contains(l, strings.ToLower(forever))
		if hasForever && strings.Contains(l, "wow.exe") {
			match = true
		}
		if hasForever && (strings.HasSuffix(l, `\wow.exe`) || strings.HasSuffix(l, `/wow.exe`)) {
			return true, true
		}
	}
	return match, false
}

// ScanProc scans procRoot (normally /proc) for game processes and returns the
// pid to track: the lowest pid among strong matches, else the lowest among
// weak ones; 0 when none. skip pids are ignored.
func ScanProc(procRoot string, skip ...int) (int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0, err
	}
	skipSet := map[int]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	strongPID, weakPID := 0, 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 || skipSet[pid] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		match, strong := IsGameCmdline(args)
		switch {
		case strong && (strongPID == 0 || pid < strongPID):
			strongPID = pid
		case match && !strong && (weakPID == 0 || pid < weakPID):
			weakPID = pid
		}
	}
	if strongPID != 0 {
		return strongPID, nil
	}
	return weakPID, nil
}
