// Package install creates and removes the folders the companion owns under
// G/Interface/AddOns (PROTOCOL §1).
package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// Options configures Install.
type Options struct {
	Game string // G
	// AddonFS holds the addon, with Ompcraft.toc at its root (the embedded
	// copy in a normal run).
	AddonFS fs.FS
	// AddonSrc is a development override: a directory with Ompcraft.toc at its
	// root. It is used only when AddonFS is nil.
	AddonSrc  string
	Slots     int
	Signals   int
	AddonOnly bool
	Force     bool
	// GameRunning reports whether the game process exists. A nil func or an
	// error counts as "cannot tell"; that refuses a full install without Force.
	GameRunning func() (bool, error)
	Logf        func(format string, args ...any)
}

func (o *Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// ErrGameRunning is returned when a full install is attempted while the game runs.
var ErrGameRunning = errors.New("the game is running; close it first (or pass --force)")

// Install implements PROTOCOL §1. It is idempotent: the addon folder is
// replaced, but slot TOCs, slot data and doorbells are only written when
// missing or different, so a re-run never rewrites what is already right.
func Install(o Options) error {
	if o.Slots == 0 {
		o.Slots = layout.DefaultSlots
	}
	if o.Signals == 0 {
		o.Signals = layout.DefaultSignals
	}
	if o.Slots < 1 || o.Slots > layout.MaxSlots {
		return fmt.Errorf("--slots must be 1..%d", layout.MaxSlots)
	}
	if o.Signals < 1 || o.Signals > layout.MaxSignals {
		return fmt.Errorf("--signals must be 1..%d", layout.MaxSignals)
	}
	if st, err := os.Stat(o.Game); err != nil || !st.IsDir() {
		return fmt.Errorf("game folder %q does not exist", o.Game)
	}
	src := o.AddonFS
	if src == nil {
		src = os.DirFS(o.AddonSrc)
	}
	if _, err := fs.Stat(src, layout.AddonName+".toc"); err != nil {
		return fmt.Errorf("the addon source has no %s.toc", layout.AddonName)
	}
	if !o.AddonOnly && !o.Force {
		if o.GameRunning == nil {
			return ErrGameRunning
		}
		running, err := o.GameRunning()
		if err != nil {
			return fmt.Errorf("cannot tell whether the game is running (%w); pass --force to install anyway", err)
		}
		if running {
			return ErrGameRunning
		}
	}

	addons := layout.AddonsDir(o.Game)
	if err := os.MkdirAll(addons, 0o755); err != nil {
		return err
	}
	if err := installAddon(src, addons); err != nil {
		return err
	}
	o.logf("installed %s", filepath.Join(addons, layout.AddonName))
	if o.AddonOnly {
		return nil
	}
	l := layout.Layout{Addons: addons, Slots: o.Slots, Signals: o.Signals}
	if err := installSlots(l); err != nil {
		return err
	}
	o.logf("installed %d slots", o.Slots)
	if err := installSignals(l); err != nil {
		return err
	}
	o.logf("installed %d doorbells", o.Signals)
	return nil
}

func installAddon(src fs.FS, addons string) error {
	tmp := filepath.Join(addons, layout.AddonTmp)
	dst := filepath.Join(addons, layout.AddonName)
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("copying addon: %w", err)
	}
	if err := os.RemoveAll(dst); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// writeIfDifferent writes data to name unless the file already holds exactly
// that. It reports whether it wrote.
func writeIfDifferent(name string, data []byte) (bool, error) {
	if old, err := os.ReadFile(name); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	return true, os.WriteFile(name, data, 0o644)
}

func installSlots(l layout.Layout) error {
	for i := range l.Slots {
		dir := l.SlotDir(i)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if _, err := writeIfDifferent(filepath.Join(dir, layout.SlotName(i)+".toc"), []byte(layout.SlotTOC(i))); err != nil {
			return err
		}
		// data.lua is only created, never reset: the companion wipes it itself,
		// and the client must not see a needless rewrite.
		data := filepath.Join(dir, layout.DataFile)
		if _, err := os.Stat(data); err != nil {
			if err := os.WriteFile(data, []byte(layout.EmptyData), 0o644); err != nil {
				return err
			}
		}
	}
	// Drop slots from an earlier, larger install.
	entries, err := os.ReadDir(l.Addons)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if n, ok := layout.SlotIndex(e.Name()); ok && n >= l.Slots {
			if err := os.RemoveAll(filepath.Join(l.Addons, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func installSignals(l layout.Layout) error {
	dir := l.SignalsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := writeIfDifferent(filepath.Join(dir, layout.SignalsName+".toc"), []byte(layout.SignalsTOC(l.Slots, l.Signals))); err != nil {
		return err
	}
	wav := layout.SilentWAV()
	ensure := func(name string) error {
		if st, err := os.Stat(name); err == nil && st.Size() == int64(len(wav)) {
			return nil
		}
		return os.WriteFile(name, wav, 0o644)
	}
	if err := ensure(filepath.Join(dir, layout.CtlPresent)); err != nil {
		return err
	}
	for i := range l.Signals {
		if err := ensure(l.Doorbell(i)); err != nil {
			return err
		}
	}
	// Drop doorbells beyond M from an earlier, larger install.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !layout.IsDoorbellName(e.Name()) {
			continue
		}
		if n, err := strconv.Atoi(e.Name()[:4]); err == nil && n >= l.Signals {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// Uninstall removes exactly A/Ompcraft, A/OmpcraftS### (3 digits) and
// A/OmpcraftSignals under game, and returns the names it removed.
func Uninstall(game string) ([]string, error) {
	addons := layout.AddonsDir(game)
	entries, err := os.ReadDir(addons)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		name := e.Name()
		owned := name == layout.AddonName || name == layout.SignalsName || layout.IsSlotName(name)
		if !owned || !e.IsDir() {
			continue // only folders with exactly the names in PROTOCOL §1.4
		}
		if err := os.RemoveAll(filepath.Join(addons, name)); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// copyTree copies the tree at the root of src to the directory dst.
// Symlinks are followed for files and skipped for directories.
func copyTree(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			st, err := fs.Stat(src, p)
			if err != nil || st.IsDir() {
				return nil
			}
		} else if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(src, p, target)
	})
}

func copyFile(src fs.FS, name, dst string) error {
	in, err := src.Open(path.Clean(name))
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// WriteTree makes dir hold exactly the files of src: files are written only
// when missing or different, and files not in src are removed. It reports
// whether anything changed. Used for the Tern plugin copy.
func WriteTree(src fs.FS, dir string) (bool, error) {
	changed := false
	want := map[string]bool{}
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		want[filepath.Clean(target)] = true
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		wrote, err := writeIfDifferent(target, data)
		changed = changed || wrote
		return err
	})
	if err != nil {
		return changed, err
	}
	// Remove stale files (and then-empty directories) that are not in src.
	var stale []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !want[filepath.Clean(p)] {
			stale = append(stale, p)
		}
		return nil
	})
	for _, p := range stale {
		if err := os.Remove(p); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// ResolveAddonSrc resolves the --addon-src development override: dir itself or
// dir/Ompcraft must hold Ompcraft.toc.
func ResolveAddonSrc(dir string) (string, error) {
	for _, c := range []string{dir, filepath.Join(dir, layout.AddonName)} {
		if _, err := os.Stat(filepath.Join(c, layout.AddonName+".toc")); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("--addon-src %q has no %s.toc", dir, layout.AddonName)
}

// HashTree returns a content hash of every regular file under the root of
// fsys (paths and contents; directories do not count). Two trees with equal
// hashes hold the same files.
func HashTree(fsys fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(data))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReplaceAddon replaces A/Ompcraft with src atomically (what --addon-only
// does). Allowed while the game runs.
func ReplaceAddon(addons string, src fs.FS) error {
	return installAddon(src, addons)
}

// helperTOCs lists every helper TOC that exists with its expected content.
func helperTOCs(l layout.Layout) map[string]string {
	want := map[string]string{}
	for i := range l.Slots {
		p := filepath.Join(l.SlotDir(i), layout.SlotName(i)+".toc")
		if _, err := os.Stat(p); err == nil {
			want[p] = layout.SlotTOC(i)
		}
	}
	p := filepath.Join(l.SignalsDir(), layout.SignalsName+".toc")
	if _, err := os.Stat(p); err == nil {
		want[p] = layout.SignalsTOC(l.Slots, l.Signals)
	}
	return want
}

// OutdatedHelperTOCs counts the existing helper TOCs whose content differs
// from what this build writes. It writes nothing.
func OutdatedHelperTOCs(l layout.Layout) int {
	n := 0
	for p, want := range helperTOCs(l) {
		if have, err := os.ReadFile(p); err != nil || string(have) != want {
			n++
		}
	}
	return n
}

// RefreshHelperTOCs rewrites the existing helper TOCs that differ, in place.
// It creates nothing, so it is safe while the game runs: the client reads TOC
// metadata at launch only. It returns how many it rewrote.
func RefreshHelperTOCs(l layout.Layout) (int, error) {
	n := 0
	for p, want := range helperTOCs(l) {
		wrote, err := writeIfDifferent(p, []byte(want))
		if err != nil {
			return n, err
		}
		if wrote {
			n++
		}
	}
	return n, nil
}
