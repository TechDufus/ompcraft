package install

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func fixture(t *testing.T) (game, src string) {
	t.Helper()
	root := t.TempDir()
	game = filepath.Join(root, "World of Warcraft", "_forever_")
	if err := os.MkdirAll(game, 0o755); err != nil {
		t.Fatal(err)
	}
	src = filepath.Join(root, "repo", "addon", "Ompcraft")
	write(t, filepath.Join(src, "Ompcraft.toc"), "## Interface: 16001\nCore/Init.lua\n")
	write(t, filepath.Join(src, "Core", "Init.lua"), "-- demo\n")
	write(t, filepath.Join(src, "Core", "Deep", "More.lua"), "-- deeper\n")
	return game, src
}

func idle() (bool, error)    { return false, nil }
func running() (bool, error) { return true, nil }

func TestInstallFull(t *testing.T) {
	game, src := fixture(t)
	a := layout.AddonsDir(game)
	// Foreign content that must survive untouched.
	write(t, filepath.Join(a, "OtherAddon", "Other.toc"), "other\n")
	write(t, filepath.Join(a, "OmpcraftS1234", "keep.txt"), "keep\n")
	write(t, filepath.Join(a, "OmpcraftSabc", "keep.txt"), "keep\n")

	err := Install(Options{Game: game, AddonSrc: src, Slots: 5, Signals: 7, GameRunning: idle})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Ompcraft", "OmpcraftS000", "OmpcraftS001", "OmpcraftS002", "OmpcraftS003", "OmpcraftS004",
		"OmpcraftS1234", "OmpcraftSabc", "OmpcraftSignals", "OtherAddon"}
	if got := names(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("AddOns = %v\nwant %v", got, want)
	}
	for _, f := range []string{"OtherAddon/Other.toc", "OmpcraftS1234/keep.txt", "OmpcraftSabc/keep.txt"} {
		if _, err := os.Stat(filepath.Join(a, f)); err != nil {
			t.Errorf("foreign file %s gone: %v", f, err)
		}
	}

	// Main addon: byte-for-byte copy, nested dirs included.
	for _, f := range []string{"Ompcraft.toc", "Core/Init.lua", "Core/Deep/More.lua"} {
		a1, _ := os.ReadFile(filepath.Join(src, f))
		a2, err := os.ReadFile(filepath.Join(a, "Ompcraft", f))
		if err != nil || !bytes.Equal(a1, a2) {
			t.Errorf("addon file %s: %v", f, err)
		}
	}

	// Slots: exactly two files each, identical TOC bytes, empty inbox.
	for i := range 5 {
		dir := filepath.Join(a, layout.SlotName(i))
		if got := names(t, dir); !reflect.DeepEqual(got, []string{"OmpcraftS" + dir[len(dir)-3:] + ".toc", "data.lua"}) {
			t.Fatalf("slot %d files = %v", i, got)
		}
		toc, _ := os.ReadFile(filepath.Join(dir, layout.SlotName(i)+".toc"))
		if string(toc) != layout.SlotTOC(i) {
			t.Errorf("slot %d toc differs:\n%s", i, toc)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "data.lua"))
		if string(data) != "OmpcraftInbox = nil\n" {
			t.Errorf("slot %d data.lua = %q", i, data)
		}
	}

	// Signals: toc, ctl_present, 0000..0006 all the same WAV; no ctl_absent.
	sig := filepath.Join(a, "OmpcraftSignals")
	wantSig := []string{"0000.wav", "0001.wav", "0002.wav", "0003.wav", "0004.wav", "0005.wav", "0006.wav",
		"OmpcraftSignals.toc", "ctl_present.wav"}
	if got := names(t, sig); !reflect.DeepEqual(got, wantSig) {
		t.Fatalf("signals = %v", got)
	}
	toc, _ := os.ReadFile(filepath.Join(sig, "OmpcraftSignals.toc"))
	if string(toc) != layout.SignalsTOC(5, 7) {
		t.Errorf("signals toc:\n%s", toc)
	}
	for _, f := range wantSig {
		if filepath.Ext(f) != ".wav" {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(sig, f))
		if !bytes.Equal(b, layout.SilentWAV()) {
			t.Errorf("%s is not the silent WAV", f)
		}
	}
	if _, err := os.Stat(filepath.Join(sig, "ctl_absent.wav")); !os.IsNotExist(err) {
		t.Error("ctl_absent.wav must never exist")
	}
}

func TestInstallRefusesWhileRunning(t *testing.T) {
	game, src := fixture(t)
	a := layout.AddonsDir(game)
	opts := Options{Game: game, AddonSrc: src, Slots: 2, Signals: 3}

	for name, fn := range map[string]func() (bool, error){
		"running":   running,
		"unknown":   func() (bool, error) { return false, errors.New("no /proc") },
		"nil probe": nil,
	} {
		o := opts
		o.GameRunning = fn
		if err := Install(o); err == nil {
			t.Errorf("%s: install must refuse", name)
		}
		if _, err := os.Stat(a); !os.IsNotExist(err) {
			t.Fatalf("%s: refused install still created %s", name, a)
		}
	}
	if err := Install(Options{Game: game, AddonSrc: src, Slots: 2, Signals: 3, GameRunning: running}); !errors.Is(err, ErrGameRunning) {
		t.Fatalf("want ErrGameRunning, got %v", err)
	}

	// --addon-only is allowed while running and touches only A/Ompcraft.
	o := opts
	o.GameRunning, o.AddonOnly = running, true
	if err := Install(o); err != nil {
		t.Fatal(err)
	}
	if got := names(t, a); !reflect.DeepEqual(got, []string{"Ompcraft"}) {
		t.Fatalf("addon-only created %v", got)
	}

	// --force overrides the check.
	o = opts
	o.GameRunning, o.Force = running, true
	if err := Install(o); err != nil {
		t.Fatal(err)
	}
	if got := names(t, a); len(got) != 4 { // Ompcraft, 2 slots, signals
		t.Fatalf("forced install created %v", got)
	}
}

func TestReinstallReplacesAddonAndShrinks(t *testing.T) {
	game, src := fixture(t)
	a := layout.AddonsDir(game)
	if err := Install(Options{Game: game, AddonSrc: src, Slots: 6, Signals: 9, GameRunning: idle}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(a, "OmpcraftS1234", "keep.txt"), "keep\n")
	write(t, filepath.Join(a, "Ompcraft", "Stale.lua"), "stale\n")
	write(t, filepath.Join(a, ".Ompcraft.tmp", "Leftover.lua"), "leftover\n")
	write(t, filepath.Join(a, "OmpcraftSignals", "notes.txt"), "keep\n")
	write(t, filepath.Join(src, "New.lua"), "-- new\n")

	if err := Install(Options{Game: game, AddonSrc: src, Slots: 3, Signals: 4, GameRunning: idle}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a, "Ompcraft", "Stale.lua")); !os.IsNotExist(err) {
		t.Error("stale addon file survived replacement")
	}
	if _, err := os.Stat(filepath.Join(a, "Ompcraft", "New.lua")); err != nil {
		t.Error("new addon file missing")
	}
	want := []string{"Ompcraft", "OmpcraftS000", "OmpcraftS001", "OmpcraftS002", "OmpcraftS1234", "OmpcraftSignals"}
	if got := names(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("AddOns = %v\nwant %v", got, want)
	}
	wantSig := []string{"0000.wav", "0001.wav", "0002.wav", "0003.wav", "OmpcraftSignals.toc", "ctl_present.wav", "notes.txt"}
	if got := names(t, filepath.Join(a, "OmpcraftSignals")); !reflect.DeepEqual(got, wantSig) {
		t.Fatalf("signals = %v", got)
	}
}

func TestInstallValidation(t *testing.T) {
	game, src := fixture(t)
	for name, o := range map[string]Options{
		"slots too many":   {Game: game, AddonSrc: src, Slots: 1001, Signals: 10, GameRunning: idle},
		"slots negative":   {Game: game, AddonSrc: src, Slots: -1, Signals: 10, GameRunning: idle},
		"signals too many": {Game: game, AddonSrc: src, Slots: 1, Signals: 10001, GameRunning: idle},
		"no game dir":      {Game: filepath.Join(game, "missing"), AddonSrc: src, Slots: 1, Signals: 1, GameRunning: idle},
		"no addon toc":     {Game: game, AddonSrc: t.TempDir(), Slots: 1, Signals: 1, GameRunning: idle},
	} {
		if err := Install(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := os.Stat(layout.AddonsDir(game)); !os.IsNotExist(err) {
		t.Error("a rejected install created AddOns")
	}
}

func TestUninstallTouchesOnlyOwnedNames(t *testing.T) {
	game, src := fixture(t)
	a := layout.AddonsDir(game)
	if err := Install(Options{Game: game, AddonSrc: src, Slots: 4, Signals: 3, GameRunning: idle}); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "precious.txt"), "precious\n")
	decoys := []string{"OmpcraftS1234", "OmpcraftS12", "OmpcraftSabc", "OmpcraftSignals2", "Ompcraft2", "OtherAddon", ".Ompcraft.tmp", "ompcraft"}
	for _, d := range decoys {
		write(t, filepath.Join(a, d, "keep.txt"), "keep\n")
	}
	write(t, filepath.Join(a, "OmpcraftS777"), "a plain file, not a folder\n")
	if err := os.Symlink(outside, filepath.Join(a, "OmpcraftS888")); err != nil {
		t.Fatal(err)
	}

	removed, err := Uninstall(game)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Ompcraft", "OmpcraftS000", "OmpcraftS001", "OmpcraftS002", "OmpcraftS003", "OmpcraftSignals"}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed %v, want %v", removed, want)
	}
	left := append([]string{"OmpcraftS777", "OmpcraftS888"}, decoys...)
	sort.Strings(left)
	if got := names(t, a); !reflect.DeepEqual(got, left) {
		t.Fatalf("left = %v\nwant %v", got, left)
	}
	if _, err := os.Stat(filepath.Join(outside, "precious.txt")); err != nil {
		t.Fatal("uninstall reached through a symlink")
	}

	// Nothing installed, or no AddOns folder at all: not an error.
	if removed, err := Uninstall(game); err != nil || len(removed) != 0 {
		t.Fatalf("second uninstall: %v %v", removed, err)
	}
	if _, err := Uninstall(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAddonSrc(t *testing.T) {
	_, src := fixture(t) // .../repo/addon/Ompcraft
	for _, flag := range []string{src, filepath.Dir(src)} {
		if got, err := ResolveAddonSrc(flag); err != nil || got != src {
			t.Errorf("--addon-src %s: %q %v", flag, got, err)
		}
	}
	if _, err := ResolveAddonSrc(t.TempDir()); err == nil {
		t.Error("expected an error for a directory without the toc")
	}
}

func fakeAddon() fstest.MapFS {
	return fstest.MapFS{
		"Ompcraft.toc":     {Data: []byte("## Interface: 16001\n## Version: 9.9.9\nCore/Init.lua\n")},
		"Core/Init.lua":    {Data: []byte("-- embedded\n")},
		"UI/Deep/Pane.lua": {Data: []byte("-- deep\n")},
	}
}

func TestInstallFromEmbeddedFS(t *testing.T) {
	game, _ := fixture(t)
	a := layout.AddonsDir(game)
	if err := Install(Options{Game: game, AddonFS: fakeAddon(), Slots: 3, Signals: 4, GameRunning: idle}); err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]string{"Ompcraft.toc": "## Interface: 16001\n## Version: 9.9.9\nCore/Init.lua\n",
		"Core/Init.lua": "-- embedded\n", "UI/Deep/Pane.lua": "-- deep\n"} {
		b, err := os.ReadFile(filepath.Join(a, "Ompcraft", filepath.FromSlash(f)))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v", f, b, err)
		}
	}
	if err := Install(Options{Game: game, AddonFS: fstest.MapFS{"x": {}}, Slots: 1, Signals: 1, GameRunning: idle}); err == nil {
		t.Error("an FS without Ompcraft.toc must be rejected")
	}
}

func TestReinstallDoesNotRewriteHelpers(t *testing.T) {
	game, _ := fixture(t)
	a := layout.AddonsDir(game)
	opts := Options{Game: game, AddonFS: fakeAddon(), Slots: 3, Signals: 4, GameRunning: idle}
	if err := Install(opts); err != nil {
		t.Fatal(err)
	}
	// Age every helper file and make slot 1's data look like a live snapshot.
	if err := os.WriteFile(filepath.Join(a, "OmpcraftS001", "data.lua"), []byte("OmpcraftInbox = { gen = 5 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	var files []string
	for _, root := range []string{"OmpcraftS000", "OmpcraftS001", "OmpcraftS002", "OmpcraftSignals"} {
		_ = filepath.WalkDir(filepath.Join(a, root), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files = append(files, p)
			}
			return nil
		})
	}
	for _, f := range files {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := Install(opts); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if !st.ModTime().Equal(old) {
			t.Errorf("%s was rewritten by a re-install", f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(a, "OmpcraftS001", "data.lua")); !strings.Contains(string(b), "gen = 5") {
		t.Error("data.lua was reset by a re-install")
	}
	// A missing doorbell and a corrupt slot toc are repaired.
	_ = os.Remove(filepath.Join(a, "OmpcraftSignals", "0002.wav"))
	_ = os.WriteFile(filepath.Join(a, "OmpcraftS002", "OmpcraftS002.toc"), []byte("junk"), 0o644)
	if err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(a, "OmpcraftSignals", "0002.wav")); string(b) != string(layout.SilentWAV()) {
		t.Error("missing doorbell not recreated")
	}
	if b, _ := os.ReadFile(filepath.Join(a, "OmpcraftS002", "OmpcraftS002.toc")); string(b) != layout.SlotTOC(2) {
		t.Error("slot toc not repaired")
	}
}

func TestWriteTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin")
	src := fstest.MapFS{"plugin.toml": {Data: []byte("id = \"x\"\n")}, "lib/a.luau": {Data: []byte("a\n")}}
	changed, err := WriteTree(src, dir)
	if err != nil || !changed {
		t.Fatalf("first write: %v %v", changed, err)
	}
	if changed, err := WriteTree(src, dir); err != nil || changed {
		t.Fatalf("second write must change nothing: %v %v", changed, err)
	}
	write(t, filepath.Join(dir, "stale.luau"), "old\n")
	write(t, filepath.Join(dir, "lib", "a.luau"), "edited\n")
	changed, err = WriteTree(src, dir)
	if err != nil || !changed {
		t.Fatalf("repair: %v %v", changed, err)
	}
	if got := names(t, dir); !reflect.DeepEqual(got, []string{"lib", "plugin.toml"}) {
		t.Fatalf("dir = %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "lib", "a.luau")); string(b) != "a\n" {
		t.Fatalf("a.luau = %q", b)
	}
}

func TestRefreshHelperTOCs(t *testing.T) {
	game, _ := fixture(t)
	a := layout.AddonsDir(game)
	if err := Install(Options{Game: game, AddonFS: fakeAddon(), Slots: 4, Signals: 3, GameRunning: idle}); err != nil {
		t.Fatal(err)
	}
	l := layout.Layout{Addons: a, Slots: 4, Signals: 3}
	if n := OutdatedHelperTOCs(l); n != 0 {
		t.Fatalf("fresh install: %d outdated", n)
	}
	// An older release wrote these.
	oldSlot := "## Interface: 16001\n## Title: |cff808080Ompcraft link slot|r\n## Group: Ompcraft\n## LoadOnDemand: 1\ndata.lua\n"
	for i := range 4 {
		write(t, filepath.Join(a, layout.SlotName(i), layout.SlotName(i)+".toc"), oldSlot)
	}
	write(t, filepath.Join(a, "OmpcraftSignals", "OmpcraftSignals.toc"), "## Interface: 16001\n## Group: Ompcraft\n## X-Ompcraft-Slots: 4\n## X-Ompcraft-Signals: 3\n")
	// A slot whose TOC is missing is not "outdated"; refresh must not create it.
	if err := os.Remove(filepath.Join(a, "OmpcraftS003", "OmpcraftS003.toc")); err != nil {
		t.Fatal(err)
	}
	if n := OutdatedHelperTOCs(l); n != 4 { // slots 0-2 and the signals toc
		t.Fatalf("outdated = %d, want 4", n)
	}
	n, err := RefreshHelperTOCs(l)
	if err != nil || n != 4 {
		t.Fatalf("refreshed %d, %v", n, err)
	}
	if exists(filepath.Join(a, "OmpcraftS003", "OmpcraftS003.toc")) {
		t.Error("refresh created a missing file")
	}
	if b, _ := os.ReadFile(filepath.Join(a, "OmpcraftS001", "OmpcraftS001.toc")); string(b) != layout.SlotTOC(1) {
		t.Errorf("slot 1 toc:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(a, "OmpcraftSignals", "OmpcraftSignals.toc")); string(b) != layout.SignalsTOC(4, 3) {
		t.Errorf("signals toc:\n%s", b)
	}
	if n, _ := RefreshHelperTOCs(l); n != 0 || OutdatedHelperTOCs(l) != 0 {
		t.Error("a second refresh must change nothing")
	}
	// A normal install rewrites outdated TOCs as well.
	write(t, filepath.Join(a, "OmpcraftS000", "OmpcraftS000.toc"), oldSlot)
	if err := Install(Options{Game: game, AddonFS: fakeAddon(), Slots: 4, Signals: 3, GameRunning: idle}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(a, "OmpcraftS000", "OmpcraftS000.toc")); string(b) != layout.SlotTOC(0) {
		t.Error("install did not rewrite an outdated slot toc")
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
