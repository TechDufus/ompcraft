package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

func (w *world) doctor() error {
	w.t.Helper()
	return Doctor(context.Background(), w.deps)
}

func TestDoctorHealthy(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("setup: %v\n%s", err, w.output())
	}
	w.reset()
	if err := w.doctor(); err != nil {
		t.Fatalf("doctor: %v\n%s", err, w.output())
	}
	want := []string{
		"✓ Game folder: <home>/Games/wow/_forever_\n",
		"✓ Addon: installed 9.9.9 (matches this ompcraft)\n",
		"✓ Link helpers: 3/3 slots, 4/4 doorbells present\n",
		"✓ Service: systemd user unit ompcraft.service (enabled, active)\n",
		"✓ Companion: answering (version 0.1.0-test, pid 777)\n",
		"✓ Tern: found on PATH\n",
		"✓ Tern plugin: linked from <home>/data/ompcraft/tern-plugin, ready\n",
		"✓ Tern sync: syncing",
		"• Game: not running\n",
		"• UI session: not seen yet",
		"\n✓ Everything looks good.\n",
	}
	out := w.output()
	for _, s := range want {
		if !strings.Contains(out, s) {
			t.Errorf("doctor output lacks %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "✗") {
		t.Errorf("healthy doctor printed a ✗:\n%s", out)
	}
	// Read-only: no mutating commands.
	for _, c := range w.sys.calls {
		for _, bad := range []string{"link", "unlink", "reload", "enable", "restart", "stop", "daemon-reload", "disable"} {
			if strings.Contains(c, " "+bad) {
				t.Errorf("doctor ran a mutating command: %s", c)
			}
		}
	}
}

func TestDoctorGameRunningAndSessionAreInformational(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	_ = w.setup(Options{})
	w.reset()
	w.closeAfter = 1000 // the game runs
	if err := w.doctor(); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	if !strings.Contains(w.output(), "✓ Game: running (pid 4242)\n") {
		t.Errorf("output:\n%s", w.output())
	}
}

func TestDoctorBroken(t *testing.T) {
	w := newWorld(t)
	w.sys.haveTern = false
	w.stateFailures = 1000
	w.preinstall(3, 4, "9.9.9")
	// A broken install: one slot folder lost, the control doorbell gone, no service.
	a := layout.AddonsDir(w.game)
	if err := os.RemoveAll(filepath.Join(a, "OmpcraftS001")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(a, "OmpcraftSignals", "ctl_present.wav")); err != nil {
		t.Fatal(err)
	}
	err := w.doctor()
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed", err)
	}
	out := w.output()
	for _, s := range []string{
		"✓ Game folder: ",
		"✓ Addon: installed 9.9.9",
		"✗ Link helpers: 2/3 slots, 4/4 doorbells present; ctl_present.wav is missing\n",
		"✗ Service: systemd user unit ompcraft.service (no unit file)\n",
		"✗ Companion: not answering\n",
		"✗ Tern: not found on PATH\n",
		"✗ Tern plugin: unknown (Tern is not installed)\n",
		"✗ Tern sync: unknown (the companion is not answering)\n",
		"• Game: not running\n",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	// The suggested fix is for the FIRST ✗ (the helpers).
	if !strings.HasSuffix(out, "\n✗ Fix: run `ompcraft setup` (close the game first; setup waits for it)\n") {
		t.Errorf("output must end with the fix for the first ✗:\n%s", out)
	}
	if strings.Contains(out, "Everything looks good") {
		t.Error("claims everything is fine")
	}
}

func TestDoctorNothingInstalled(t *testing.T) {
	w := newWorld(t)
	w.sys.haveTern = false
	w.stateFailures = 1000
	w.games = nil
	if err := w.doctor(); !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v", err)
	}
	out := w.output()
	if !strings.Contains(out, "✗ Game folder: not found\n") || !strings.HasSuffix(out, "✗ Fix: run `ompcraft setup --game \"/path/to/World of Warcraft/_forever_\"`\n") {
		t.Errorf("output:\n%s", out)
	}
}

func TestDoctorOldAddonVersion(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "0.0.1")
	_ = w.doctor()
	if !strings.Contains(w.output(), "✗ Addon: installed 0.0.1, but this ompcraft carries 9.9.9\n") {
		t.Errorf("output:\n%s", w.output())
	}
}

func TestDoctorPluginProblems(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	_ = w.setup(Options{})
	w.reset()

	w.sys.pluginStatus = "failed"
	_ = w.doctor()
	if !strings.Contains(w.output(), "✗ Tern plugin: status is failed") {
		t.Errorf("output:\n%s", w.output())
	}

	w.reset()
	w.sys.pluginStatus = ""
	if err := os.Remove(filepath.Join(w.sys.plugDir, "ompcraft.path")); err != nil {
		t.Fatal(err)
	}
	_ = w.doctor()
	if !strings.Contains(w.output(), "✗ Tern plugin: not linked\n") || !strings.Contains(w.output(), "✗ Fix: run `ompcraft setup` to link and reload the plugin\n") {
		t.Errorf("output:\n%s", w.output())
	}

	w.reset()
	w.ternSyncing = false
	_ = w.doctor()
	if !strings.Contains(w.output(), "✗ Tern sync: no sync in the last 3 s\n") {
		t.Errorf("output:\n%s", w.output())
	}
}

// ---- uninstall -------------------------------------------------------------

func installedWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("setup: %v\n%s", err, w.output())
	}
	// Files a running companion leaves behind.
	for _, f := range []string{w.env.LinkPath(), w.env.StatePath(), w.env.LogPath()} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w.reset()
	return w
}

func (w *world) uninstall(o UninstallOptions) error {
	w.t.Helper()
	return Uninstall(context.Background(), w.deps, o)
}

func TestUninstallReversesSetup(t *testing.T) {
	w := installedWorld(t)
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	out := w.output()
	a := "<home>/Games/wow/_forever_/Interface/AddOns/"
	for _, s := range []string{
		"✓ Removed service ompcraft.service (stopped, disabled) and <home>/cfg/systemd/user/ompcraft.service\n",
		"✓ Removed the tern plugin link (<home>/data/ompcraft/tern-plugin)\n",
		"✓ Removed the plugin copy in <home>/data/ompcraft/tern-plugin\n",
		"✓ Removed " + a + "Ompcraft\n",
		"✓ Removed " + a + "OmpcraftS000\n",
		"✓ Removed " + a + "OmpcraftS002\n",
		"✓ Removed " + a + "OmpcraftSignals\n",
		"• Kept your config and state in <home>/cfg/ompcraft and <home>/state/ompcraft (add --purge to remove them).\n",
		"\n✓ ompcraft is uninstalled.\n",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	// Really gone.
	if exists(w.sys.unitPath) || w.sys.enabled || w.sys.active {
		t.Error("service still installed")
	}
	if exists(filepath.Join(w.sys.plugDir, "ompcraft.path")) || exists(w.env.PluginDir()) {
		t.Error("tern plugin still linked or copied")
	}
	if entries, _ := os.ReadDir(layout.AddonsDir(w.game)); len(entries) != 0 {
		t.Errorf("AddOns still has %d entries", len(entries))
	}
	for _, f := range []string{"systemctl --user disable --now ompcraft.service", "systemctl --user daemon-reload", "tern plugin unlink ompcraft"} {
		if !w.sys.called(f) {
			t.Errorf("did not run %q", f)
		}
	}
	// Kept without --purge.
	for _, f := range []string{w.env.ConfigPath(), w.env.LinkPath(), w.env.StatePath(), w.env.LogPath()} {
		if !exists(f) {
			t.Errorf("%s removed without --purge", f)
		}
	}
}

func TestUninstallPurge(t *testing.T) {
	w := installedWorld(t)
	if err := w.uninstall(UninstallOptions{Purge: true}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	for _, f := range []string{w.env.ConfigPath(), w.env.LinkPath(), w.env.StatePath(), w.env.LogPath()} {
		if exists(f) {
			t.Errorf("%s survived --purge", f)
		}
		if !strings.Contains(w.output(), "✓ Removed "+w.norm(f)+"\n") {
			t.Errorf("output does not list %s:\n%s", f, w.output())
		}
	}
	if exists(w.env.ConfigDir()) || exists(w.env.StateDir()) || exists(w.env.DataDir()) {
		t.Error("empty ompcraft directories left behind")
	}
	if strings.Contains(w.output(), "Kept your config") {
		t.Error("--purge must not say it kept the config")
	}
}

func TestUninstallRefusesWhileTheGameRuns(t *testing.T) {
	w := installedWorld(t)
	w.closeAfter = 1000
	err := w.uninstall(UninstallOptions{})
	if !errors.Is(err, ErrFailed) || !strings.Contains(w.output(), "✗ World of Warcraft is running. Close it first, or pass --force.") {
		t.Fatalf("err=%v\n%s", err, w.output())
	}
	if len(w.sys.calls) != 0 || !exists(w.sys.unitPath) || !exists(filepath.Join(layout.AddonsDir(w.game), "Ompcraft")) {
		t.Error("a refused uninstall must change nothing")
	}
	// --force goes ahead.
	w.reset()
	w.closeAfter = 1000
	if err := w.uninstall(UninstallOptions{Force: true}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	if exists(filepath.Join(layout.AddonsDir(w.game), "Ompcraft")) {
		t.Error("--force did not uninstall")
	}
}

func TestUninstallTwiceAndTransientUnit(t *testing.T) {
	w := installedWorld(t)
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	w.reset()
	w.sys.transient, w.sys.active = true, true // a systemd-run unit is still around
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	out := w.output()
	for _, s := range []string{"✓ Removed temporary unit ompcraft.service\n", "• The Tern plugin was not linked.\n", "• No ompcraft folders in <home>/Games/wow/_forever_/Interface/AddOns.\n"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	if w.sys.transient {
		t.Error("the temporary unit is still there")
	}
}

func TestUninstallLeavesForeignAddonsAndWorksWithoutTernOrGame(t *testing.T) {
	w := installedWorld(t)
	other := filepath.Join(layout.AddonsDir(w.game), "SomeOtherAddon")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	w.sys.haveTern = false
	w.games = nil
	if err := os.Remove(w.env.ConfigPath()); err != nil { // no saved game folder either
		t.Fatal(err)
	}
	if err := w.uninstall(UninstallOptions{}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	out := w.output()
	if !exists(other) {
		t.Error("another addon was removed")
	}
	if !strings.Contains(out, "• Tern is not installed; nothing to unlink.\n") ||
		!strings.Contains(out, "• No game folder found; the addon was not removed.") {
		t.Errorf("output:\n%s", out)
	}
	// With --game it still finds the addon.
	w.reset()
	if err := w.uninstall(UninstallOptions{Game: w.game}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(layout.AddonsDir(w.game), "Ompcraft")) || !exists(other) {
		t.Error("--game uninstall wrong")
	}
}
