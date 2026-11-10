package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/TechDufus/ompcraft/companion/internal/config"
)

// Sentinel errors the command maps to exit codes. Their messages are never
// printed: the steps already said what went wrong.
var (
	// ErrFailed: something went wrong (a ✗ line explains it).
	ErrFailed = errors.New("failed")
	// ErrAborted: the user pressed Ctrl-C.
	ErrAborted = errors.New("aborted")
)

func goos() string { return runtime.GOOS }

func evalSymlinks(p string) (string, error) { return filepath.EvalSymlinks(p) }

// printer prints the one-line-per-step output.
type printer struct{ w io.Writer }

func (p printer) ok(format string, a ...any)   { fmt.Fprintf(p.w, "✓ "+format+"\n", a...) }
func (p printer) fail(format string, a ...any) { fmt.Fprintf(p.w, "✗ "+format+"\n", a...) }
func (p printer) note(format string, a ...any) { fmt.Fprintf(p.w, "• "+format+"\n", a...) }
func (p printer) line(format string, a ...any) { fmt.Fprintf(p.w, "  "+format+"\n", a...) }
func (p printer) blank()                       { fmt.Fprintln(p.w) }

// resolveGame picks the game folder: the explicit one, else the saved one,
// else auto-detection. With several candidates it asks, or takes the first
// with yes. It prints the ✗ line itself when it cannot decide.
func resolveGame(d Deps, p printer, explicit string, yes bool, in *bufio.Reader) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			p.fail("%s is not a folder. Pass the WoW: Forever folder (the one named _forever_) with --game.", abs)
			return "", ErrFailed
		}
		return abs, nil
	}
	if cfg, err := d.Env.LoadConfig(); err == nil && cfg.Game != "" {
		if st, err := os.Stat(cfg.Game); err == nil && st.IsDir() {
			return cfg.Game, nil
		}
	}
	found, err := d.Detect()
	if err != nil {
		p.fail("Looking for WoW: Forever failed: %v", err)
		return "", ErrFailed
	}
	switch len(found) {
	case 0:
		p.fail("Couldn't find WoW: Forever. I looked in:")
		for _, l := range d.Looked {
			p.line("%s", l)
		}
		p.line("Tell me where it is: ompcraft setup --game \"/path/to/World of Warcraft/_forever_\"")
		return "", ErrFailed
	case 1:
		return found[0], nil
	}
	if yes {
		p.note("Found %d WoW: Forever installs; using the first (--yes).", len(found))
		return found[0], nil
	}
	fmt.Fprintf(d.Out, "Found %d WoW: Forever installs:\n", len(found))
	for i, f := range found {
		p.line("%d) %s", i+1, f)
	}
	fmt.Fprintf(d.Out, "Which one? [1] ")
	line, err := in.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Fprintln(d.Out)
		p.fail("I need an answer to choose between them. Run again with --game DIR, or --yes to take the first.")
		return "", ErrFailed
	}
	choice := 1
	if s := strings.TrimSpace(line); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > len(found) {
			p.fail("%q is not one of 1-%d. Run again, or pass --game DIR.", s, len(found))
			return "", ErrFailed
		}
		choice = n
	}
	return found[choice-1], nil
}

// savedConfig is a helper for doctor/uninstall: the saved game folder or "".
func savedGame(env config.Env) string {
	if cfg, err := env.LoadConfig(); err == nil {
		return cfg.Game
	}
	return ""
}

// isTemporaryPath reports whether an executable path looks like a throwaway
// build (go run cache, /tmp).
func isTemporaryPath(p string) bool {
	p = filepath.ToSlash(p)
	tmp := filepath.ToSlash(filepath.Clean(os.TempDir()))
	switch {
	case strings.Contains(p, "/go-build"):
		return true
	case strings.HasPrefix(p, "/tmp/"), strings.HasPrefix(p, tmp+"/"):
		return true
	}
	return false
}
