package gamedir

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestIsGameCmdline(t *testing.T) {
	cases := []struct {
		name          string
		args          []string
		match, strong bool
	}{
		{"wine process, windows path",
			[]string{`C:\Program Files (x86)\World of Warcraft\_forever_\Wow.exe`}, true, true},
		{"wine process, unix path",
			[]string{"/home/user/Games/wow/drive_c/Program Files (x86)/World of Warcraft/_forever_/Wow.exe", "-launcherlogin"}, true, true},
		{"case-insensitive",
			[]string{`c:\games\wow\_FOREVER_\WOW.EXE`}, true, true},
		{"launcher script mentioning both",
			[]string{"bash", "-c", "wine '/x/_forever_/Wow.exe' -foo"}, true, false},
		{"Wow.exe outside _forever_", []string{`C:\wow\_retail_\Wow.exe`}, false, false},
		{"_forever_ without Wow.exe", []string{"ompcraft", "install", "--game", "/x/_forever_"}, false, false},
		{"empty", nil, false, false},
	}
	for _, c := range cases {
		match, strong := IsGameCmdline(c.args)
		if match != c.match || strong != c.strong {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", c.name, match, strong, c.match, c.strong)
		}
	}
}

func fakeProc(t *testing.T, procs map[int][]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, args := range procs {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data := strings.Join(args, "\x00") + "\x00"
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Noise: non-numeric entry, a pid dir without cmdline.
	_ = os.MkdirAll(filepath.Join(root, "self"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "999"), 0o755)
	return root
}

func TestScanProc(t *testing.T) {
	game := `C:\Program Files (x86)\World of Warcraft\_forever_\Wow.exe`
	root := fakeProc(t, map[int][]string{
		10: {"/usr/bin/zsh"},
		20: {"bash", "-c", "wine /x/_forever_/Wow.exe -launcher"}, // weak launcher, lower pid
		30: {game},
		40: {game, "-extra"}, // second strong match, higher pid
	})
	pid, err := ScanProc(root)
	if err != nil || pid != 30 {
		t.Fatalf("got (%d, %v), want strong match 30", pid, err)
	}
	// Skipping the strong matches falls back to the weak launcher.
	if pid, _ := ScanProc(root, 30, 40); pid != 20 {
		t.Fatalf("weak fallback: got %d, want 20", pid)
	}
	if pid, _ := ScanProc(root, 20, 30, 40); pid != 0 {
		t.Fatalf("no game: got %d", pid)
	}
	if _, err := ScanProc(filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected an error for a missing proc root")
	}
}

func TestDetect(t *testing.T) {
	home := t.TempDir()
	pf := filepath.Join("drive_c", "Program Files (x86)", "World of Warcraft", "_forever_")
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{home}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	patterns := Patterns(home)
	if len(patterns) != 5 {
		t.Fatalf("want 5 patterns, got %d", len(patterns))
	}
	// Only the /Applications pattern is outside home; drop it for hermetic tests.
	patterns = patterns[:4]

	if _, err := DetectOne(patterns); err == nil || !strings.Contains(err.Error(), "--game") {
		t.Fatalf("0 candidates: %v", err)
	}
	a := mk(".wine", pf)
	got, err := DetectOne(patterns)
	if err != nil || got != a {
		t.Fatalf("1 candidate: (%q, %v)", got, err)
	}
	b := mk("Games", "battlenet", pf)
	c := mk("Games", "lutris", "pfx", pf)
	d := mk(".local", "share", "Steam", "steamapps", "compatdata", "12345", "pfx", pf)
	_, err = DetectOne(patterns)
	if err == nil {
		t.Fatal("expected an error for several candidates")
	}
	for _, p := range []string{a, b, c, d} {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error does not list %s:\n%v", p, err)
		}
	}
	// A plain file with a matching name is not a candidate.
	e := filepath.Join(home, "Games", "file", pf)
	_ = os.MkdirAll(filepath.Dir(e), 0o755)
	_ = os.WriteFile(e, nil, 0o644)
	found, _ := Detect(patterns)
	if len(found) != 4 {
		t.Fatalf("found %v", found)
	}
}
