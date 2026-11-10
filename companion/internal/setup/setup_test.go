package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/install"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// norm makes output independent of the temp directory.
func (w *world) norm(s string) string {
	return strings.ReplaceAll(s, w.env.Home, "<home>")
}

func (w *world) output() string { return w.norm(w.out.String()) }

// preinstall puts an older install with small helper counts on disk.
func (w *world) preinstall(slots, signals int, version string) {
	w.t.Helper()
	err := install.Install(install.Options{Game: w.game, AddonFS: fakeAddonFS(version), Slots: slots, Signals: signals,
		GameRunning: func() (bool, error) { return false, nil }})
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) setup(o Options) error {
	w.t.Helper()
	return Setup(context.Background(), w.deps, o)
}

func (w *world) reset() {
	w.out.Reset()
	w.sys.calls = nil
	w.gamePolls, w.stateCalls, w.sleeps = 0, 0, 0
}

func TestSetupHappyPath(t *testing.T) {
	w := newWorld(t)
	w.closeAfter = 2    // WoW is running; it closes after 2 polls
	w.stateFailures = 1 // the companion needs a moment to come up
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("setup: %v\n%s", err, w.output())
	}
	want := `✓ Found WoW: Forever at <home>/Games/wow/_forever_
• Close World of Warcraft to continue (setup will pick up automatically)…
✓ World of Warcraft is closed
✓ Installed the addon 9.9.9, 1000 link slots and 2000 doorbells in <home>/Games/wow/_forever_/Interface/AddOns
✓ Tern plugin linked from <home>/data/ompcraft/tern-plugin and reloaded
✓ Companion service enabled and running (systemd user unit ompcraft.service)
✓ Companion is answering (version 0.1.0-test)
✓ Tern is syncing

✓ ompcraft is ready.
  Next: start World of Warcraft. Your agents appear under your quest objectives. /oc help for commands.
`
	if got := w.output(); got != want {
		t.Fatalf("output:\n%s\nwant:\n%s", got, want)
	}
	if w.sleeps != 3 { // 2 polls while the game runs + 1 while the companion starts
		t.Errorf("sleeps = %d, want 3", w.sleeps)
	}

	// Every step really happened.
	a := layout.AddonsDir(w.game)
	for _, f := range []string{"Ompcraft/Ompcraft.toc", "Ompcraft/Core/Init.lua", "OmpcraftS000/data.lua", "OmpcraftS999/data.lua",
		"OmpcraftSignals/0000.wav", "OmpcraftSignals/1999.wav", "OmpcraftSignals/ctl_present.wav"} {
		if !exists(filepath.Join(a, filepath.FromSlash(f))) {
			t.Errorf("%s was not installed", f)
		}
	}
	if cfg, _ := w.env.LoadConfig(); cfg.Game != w.game {
		t.Errorf("saved game = %q", cfg.Game)
	}
	for _, f := range []string{"plugin.toml", "window.luau"} {
		if !exists(filepath.Join(w.env.PluginDir(), f)) {
			t.Errorf("plugin file %s missing", f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(w.sys.plugDir, "ompcraft.path")); strings.TrimSpace(string(b)) != w.env.PluginDir() {
		t.Errorf("tern is linked to %q", b)
	}
	unit, err := os.ReadFile(w.sys.unitPath)
	if err != nil {
		t.Fatal(err)
	}
	wantUnit := "ExecStart=/home/user/go/bin/ompcraft run\nRestart=on-failure\n"
	if !strings.Contains(string(unit), wantUnit) || !strings.Contains(string(unit), "WantedBy=default.target") {
		t.Errorf("unit file:\n%s", unit)
	}
	wantCalls := []string{
		"tern plugin dir --json",
		"tern plugin link " + w.env.PluginDir(),
		"tern plugin reload ompcraft",
		"systemctl --user show ompcraft.service -p LoadState -p ActiveState -p FragmentPath",
		"systemctl --user daemon-reload",
		"systemctl --user enable --now ompcraft.service",
	}
	if !reflect.DeepEqual(w.sys.calls, wantCalls) {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(w.sys.calls, "\n"), strings.Join(wantCalls, "\n"))
	}
}

func TestSetupUpdatesInPlaceWhileTheGameRuns(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.0.0")
	a := layout.AddonsDir(w.game)
	// While the game runs the companion has removed some doorbells; setup must not recreate them.
	if err := os.Remove(filepath.Join(a, "OmpcraftSignals", "0001.wav")); err != nil {
		t.Fatal(err)
	}
	w.closeAfter = 1000 // WoW stays running
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	want := `✓ Found WoW: Forever at <home>/Games/wow/_forever_
✓ Updated the addon 9.9.9 (type /reload in game)
✓ Tern plugin linked from <home>/data/ompcraft/tern-plugin and reloaded
✓ Companion service enabled and running (systemd user unit ompcraft.service)
✓ Companion is answering (version 0.1.0-test)
✓ Tern is syncing

✓ ompcraft is ready. Type /reload in game to load the new addon.
`
	if got := w.output(); got != want {
		t.Fatalf("output:\n%s\nwant:\n%s", got, want)
	}
	if w.sleeps != 0 {
		t.Errorf("setup waited %d times for a game it did not need to wait for", w.sleeps)
	}
	if b, _ := os.ReadFile(filepath.Join(a, "Ompcraft", "Ompcraft.toc")); !strings.Contains(string(b), "9.9.9") {
		t.Error("the addon was not refreshed")
	}
	if exists(filepath.Join(a, "OmpcraftSignals", "0001.wav")) {
		t.Error("a doorbell was recreated while the game runs")
	}
	if !w.sys.called("systemctl --user enable --now ompcraft.service") {
		t.Error("the service step must still run")
	}
}

func TestSetupWaitsWhenHelpersAreMissingEvenIfTheGameRuns(t *testing.T) {
	broken := map[string]func(a string){
		"no helpers at all": func(a string) {
			_ = os.RemoveAll(filepath.Join(a, "OmpcraftSignals"))
			_ = os.RemoveAll(filepath.Join(a, "OmpcraftS000"))
		},
		"a slot is missing": func(a string) { _ = os.RemoveAll(filepath.Join(a, "OmpcraftS002")) },
		"slot data missing": func(a string) { _ = os.Remove(filepath.Join(a, "OmpcraftS001", "data.lua")) },
		"ctl_present gone":  func(a string) { _ = os.Remove(filepath.Join(a, "OmpcraftSignals", "ctl_present.wav")) },
		"signals toc gone":  func(a string) { _ = os.Remove(filepath.Join(a, "OmpcraftSignals", "OmpcraftSignals.toc")) },
	}
	for name, damage := range broken {
		w := newWorld(t)
		w.preinstall(3, 4, "9.9.9")
		damage(layout.AddonsDir(w.game))
		w.closeAfter = 2
		if err := w.setup(Options{}); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, w.output())
		}
		out := w.output()
		if w.sleeps < 2 || !strings.Contains(out, "• Close World of Warcraft to continue") || !strings.Contains(out, "✓ World of Warcraft is closed") ||
			strings.Contains(out, "Updated the addon") || !strings.Contains(out, "✓ ompcraft is ready.\n  Next: start World of Warcraft.") {
			t.Errorf("%s: sleeps=%d\n%s", name, w.sleeps, out)
		}
		if !exists(filepath.Join(layout.AddonsDir(w.game), "OmpcraftS002", "data.lua")) || !exists(filepath.Join(layout.AddonsDir(w.game), "OmpcraftSignals", "ctl_present.wav")) {
			t.Errorf("%s: the helpers were not repaired after the game closed", name)
		}
	}
}

func TestSetupGameClosedAndHelpersPresentDoesAFullInstall(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	_ = os.Remove(filepath.Join(layout.AddonsDir(w.game), "OmpcraftSignals", "0002.wav"))
	if err := w.setup(Options{}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(layout.AddonsDir(w.game), "OmpcraftSignals", "0002.wav")) {
		t.Error("with the game closed, setup repairs missing doorbells")
	}
	if !strings.Contains(w.output(), "✓ ompcraft is ready.\n  Next: start World of Warcraft.") {
		t.Errorf("output:\n%s", w.output())
	}
}

func TestSetupWhenGameIsAlreadyClosed(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	if err := w.setup(Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.output(), "✓ World of Warcraft is not running\n") || w.sleeps != 0 {
		t.Fatalf("sleeps=%d\n%s", w.sleeps, w.output())
	}
}

func TestSetupRerunIsIdempotentAndKeepsHelperCounts(t *testing.T) {
	w := newWorld(t)
	w.preinstall(5, 7, "9.0.0") // an older install with its own counts
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("first run: %v\n%s", err, w.output())
	}
	if !strings.Contains(w.output(), "Installed the addon 9.9.9, 5 link slots and 7 doorbells") {
		t.Fatalf("counts not kept:\n%s", w.output())
	}
	if b, _ := os.ReadFile(filepath.Join(layout.AddonsDir(w.game), "Ompcraft", "Ompcraft.toc")); !strings.Contains(string(b), "9.9.9") {
		t.Fatal("the addon was not updated to the embedded version")
	}

	// Age the helpers and the unit file, then run again.
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	var aged []string
	a := layout.AddonsDir(w.game)
	for _, root := range []string{"OmpcraftS000", "OmpcraftS004", "OmpcraftSignals"} {
		entries, _ := os.ReadDir(filepath.Join(a, root))
		for _, e := range entries {
			aged = append(aged, filepath.Join(a, root, e.Name()))
		}
	}
	aged = append(aged, w.sys.unitPath, filepath.Join(w.env.PluginDir(), "plugin.toml"))
	for _, f := range aged {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	w.reset()
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("second run: %v\n%s", err, w.output())
	}
	if !strings.Contains(w.output(), "5 link slots and 7 doorbells") {
		t.Fatalf("second run changed the counts:\n%s", w.output())
	}
	for _, f := range aged {
		if st, err := os.Stat(f); err != nil || !st.ModTime().Equal(old) {
			t.Errorf("%s was rewritten by the second run", f)
		}
	}
	if entries, _ := os.ReadDir(a); len(entries) != 1+5+1 { // addon, 5 slots, signals
		t.Errorf("AddOns has %d entries", len(entries))
	}
	// The service was already running: it is restarted so a new binary takes effect.
	if !w.sys.called("systemctl --user restart ompcraft.service") {
		t.Errorf("no restart:\n%s", strings.Join(w.sys.calls, "\n"))
	}
	// Tern was already linked to the same folder: only a reload.
	if w.sys.count("tern plugin link") != 0 || w.sys.count("tern plugin unlink") != 0 || !w.sys.called("tern plugin reload ompcraft") {
		t.Errorf("tern commands:\n%s", strings.Join(w.sys.calls, "\n"))
	}
}

func TestSetupTernMissingContinues(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.sys.haveTern = false
	err := w.setup(Options{})
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed", err)
	}
	out := w.output()
	for _, want := range []string{
		"✗ Stencil Tern isn't installed (no `tern` on your PATH). Install Stencil Tern, then re-run `ompcraft setup`.",
		"✓ Companion service enabled and running",
		"✓ Companion is answering",
		"✗ ompcraft is installed, but not everything is ready. Fix the ✗ above, then run `ompcraft setup` again.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ompcraft is ready") || strings.Contains(out, "Tern is syncing") {
		t.Errorf("claims success:\n%s", out)
	}
	if w.sys.count("tern ") != 0 {
		t.Errorf("tern was run although it is missing: %v", w.sys.calls)
	}
}

func TestSetupReplacesTransientUnit(t *testing.T) {
	for _, fragment := range []string{"", "/run/user/1000/systemd/transient/ompcraft.service"} {
		w := newWorld(t)
		w.preinstall(3, 4, "9.9.9")
		w.sys.transient, w.sys.transientPath, w.sys.active = true, fragment, true
		if err := w.setup(Options{}); err != nil {
			t.Fatalf("setup: %v\n%s", err, w.output())
		}
		if !strings.Contains(w.output(), "Companion service enabled and running (systemd user unit ompcraft.service) (replaced the temporary unit started with systemd-run)") {
			t.Errorf("fragment %q: output:\n%s", fragment, w.output())
		}
		var seq []string
		for _, c := range w.sys.calls {
			if strings.HasPrefix(c, "systemctl --user ") && !strings.Contains(c, " show ") {
				seq = append(seq, strings.TrimPrefix(c, "systemctl --user "))
			}
		}
		want := []string{"stop ompcraft.service", "reset-failed ompcraft.service", "daemon-reload", "enable --now ompcraft.service"}
		if !reflect.DeepEqual(seq, want) {
			t.Errorf("fragment %q: sequence %v, want %v (stop the transient unit before enabling the real one; no extra restart)", fragment, seq, want)
		}
		if !exists(w.sys.unitPath) || w.sys.transient {
			t.Error("the real unit must exist and the transient one be gone")
		}
	}
}

func TestSetupDoesNotTouchARealUnitOfOurs(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	if err := os.MkdirAll(filepath.Dir(w.sys.unitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.sys.unitPath, []byte("[Service]\nExecStart=/old/ompcraft run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.sys.active, w.sys.enabled = true, true
	if err := w.setup(Options{}); err != nil {
		t.Fatal(err)
	}
	if w.sys.called("systemctl --user stop ompcraft.service") {
		t.Error("a real unit file is not a transient unit")
	}
	if b, _ := os.ReadFile(w.sys.unitPath); !strings.Contains(string(b), "ExecStart=/home/user/go/bin/ompcraft run") {
		t.Errorf("unit not updated:\n%s", b)
	}
	if !w.sys.called("systemctl --user restart ompcraft.service") {
		t.Error("running service must be restarted")
	}
}

func TestSetupReplacesTernLinkedElsewhere(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	dev := "/home/user/dev/ompcraft/tern-plugin"
	if err := os.WriteFile(filepath.Join(w.sys.plugDir, "ompcraft.path"), []byte(dev+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.setup(Options{}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	if !strings.Contains(w.output(), "and reloaded (replaced the link to "+dev+")") {
		t.Errorf("output:\n%s", w.output())
	}
	want := []string{"tern plugin dir --json", "tern plugin unlink ompcraft", "tern plugin link " + w.env.PluginDir(), "tern plugin reload ompcraft"}
	var got []string
	for _, c := range w.sys.calls {
		if strings.HasPrefix(c, "tern ") {
			got = append(got, c)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tern commands %v, want %v", got, want)
	}
}

func TestSetupTernLinkFailureIsReportedAndOthersContinue(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.sys.failTern["tern plugin reload ompcraft"] = errors.New("tern plugin reload ompcraft: exit status 1: ompcraft: window.luau failed")
	err := w.setup(Options{})
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v", err)
	}
	out := w.output()
	if !strings.Contains(out, "✗ Couldn't link the Tern plugin: ") || !strings.Contains(out, "window.luau failed") ||
		!strings.Contains(out, "✓ Companion service enabled") {
		t.Errorf("output:\n%s", out)
	}
}

func TestSetupServiceFailure(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.sys.busDown = true
	err := w.setup(Options{})
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v", err)
	}
	out := w.output()
	if !strings.Contains(out, "✗ Couldn't set up the systemd user unit ompcraft.service: the systemd user session is not available") ||
		!strings.Contains(out, "Start the companion yourself with `ompcraft run`") || !strings.Contains(out, "✓ Tern plugin linked") {
		t.Errorf("output:\n%s", out)
	}
	if w.stateCalls != 0 {
		t.Error("verification must be skipped when no service was started")
	}
}

func TestSetupVerifyFailures(t *testing.T) {
	// Companion never answers.
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.stateFailures = 1000
	err := w.setup(Options{})
	if !errors.Is(err, ErrFailed) || !strings.Contains(w.output(), "✗ The companion is not answering yet. Look at `journalctl --user -u ompcraft -e` or") {
		t.Fatalf("err=%v\n%s", err, w.output())
	}
	if w.stateCalls != verifyAttempts {
		t.Errorf("polled %d times, want %d (10 s)", w.stateCalls, verifyAttempts)
	}

	// Companion answers, Tern does not sync.
	w = newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.ternSyncing = false
	err = w.setup(Options{})
	if !errors.Is(err, ErrFailed) || !strings.Contains(w.output(), "✓ Companion is answering") ||
		!strings.Contains(w.output(), "✗ Tern isn't syncing yet. Open a Tern window") {
		t.Fatalf("err=%v\n%s", err, w.output())
	}
}

func TestSetupFlags(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	if err := w.setup(Options{NoService: true, NoTern: true}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	out := w.output()
	for _, want := range []string{"• Skipping the Tern plugin (--no-tern).", "• Skipping the service (--no-service). Start the companion yourself with: ompcraft run",
		"✓ ompcraft is ready.\n  First start the companion: ompcraft run\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if len(w.sys.calls) != 0 {
		t.Errorf("commands ran: %v", w.sys.calls)
	}
}

func TestSetupOtherOSPrintsManualInstructions(t *testing.T) {
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	w.deps.GOOS = "freebsd"
	if err := w.setup(Options{NoTern: true}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	if !strings.Contains(w.output(), "• There is no service manager support on freebsd yet. Start the companion yourself with: ompcraft run") {
		t.Errorf("output:\n%s", w.output())
	}
}

func TestSetupTemporaryExecutableWarning(t *testing.T) {
	for _, exe := range []string{"/tmp/go-build123/b001/exe/ompcraft", "/tmp/ompcraft", "/home/user/.cache/go-build/ab/ompcraft"} {
		w := newWorld(t)
		w.preinstall(3, 4, "9.9.9")
		w.deps.Exe = exe
		if err := w.setup(Options{NoTern: true}); err != nil {
			t.Fatal(err)
		}
		out := w.output()
		if !strings.Contains(out, "• ompcraft is running from a temporary location ("+exe+")") ||
			!strings.Contains(out, "go install github.com/TechDufus/ompcraft/companion/cmd/ompcraft@latest") {
			t.Errorf("%s: no warning:\n%s", exe, out)
		}
	}
	w := newWorld(t)
	w.preinstall(3, 4, "9.9.9")
	_ = w.setup(Options{NoTern: true})
	if strings.Contains(w.output(), "temporary location") {
		t.Error("a normal path must not warn")
	}
}

func TestSetupCtrlCWhileWaitingForTheGame(t *testing.T) {
	w := newWorld(t)
	w.closeAfter = 1000
	ctx, cancel := context.WithCancel(context.Background())
	w.deps.Sleep = func(ctx context.Context, d time.Duration) error {
		w.sleeps++
		if w.sleeps == 3 {
			cancel() // the user presses Ctrl-C
		}
		return ctx.Err()
	}
	err := Setup(ctx, w.deps, Options{})
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if !strings.Contains(w.output(), "• Close World of Warcraft to continue (setup will pick up automatically)…\n✗ Aborted.\n") {
		t.Errorf("output:\n%s", w.output())
	}
	if exists(layout.AddonsDir(w.game)) {
		t.Error("nothing may be installed after an abort")
	}
	if len(w.sys.calls) != 0 {
		t.Errorf("commands ran: %v", w.sys.calls)
	}
}

func TestSetupCannotTellIfTheGameRuns(t *testing.T) {
	w := newWorld(t)
	w.gameErr = errors.New("game process detection: unsupported on this OS yet")
	if err := w.setup(Options{NoService: true, NoTern: true}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	if !strings.Contains(w.output(), "• I can't tell whether World of Warcraft is running here (game process detection: unsupported on this OS yet). Make sure it is closed.") {
		t.Errorf("output:\n%s", w.output())
	}
	if !exists(filepath.Join(layout.AddonsDir(w.game), "Ompcraft")) {
		t.Error("the install must go ahead")
	}
}

func TestSetupFindingTheGame(t *testing.T) {
	second := filepath.Join(t.TempDir(), "other", "_forever_")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}

	// Several installs: numbered list, the answer picks one.
	w := newWorld(t)
	w.games = []string{w.game, second}
	w.deps.In = strings.NewReader("2\n")
	if err := w.setup(Options{NoService: true, NoTern: true}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	out := w.output()
	if !strings.Contains(out, "Found 2 WoW: Forever installs:\n  1) <home>/Games/wow/_forever_\n  2) ") || !strings.Contains(out, "Which one? [1] ") ||
		!strings.Contains(out, "✓ Found WoW: Forever at "+w.norm(second)) {
		t.Errorf("output:\n%s", out)
	}

	// An empty answer takes the first.
	w = newWorld(t)
	w.games = []string{w.game, second}
	w.deps.In = strings.NewReader("\n")
	if err := w.setup(Options{NoService: true, NoTern: true}); err != nil || !strings.Contains(w.output(), "✓ Found WoW: Forever at <home>/Games/wow/_forever_") {
		t.Fatalf("%v\n%s", err, w.output())
	}

	// --yes never asks.
	w = newWorld(t)
	w.games = []string{w.game, second}
	if err := w.setup(Options{Yes: true, NoService: true, NoTern: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.output(), "Which one?") || !strings.Contains(w.output(), "using the first (--yes)") ||
		!strings.Contains(w.output(), "✓ Found WoW: Forever at <home>/Games/wow/_forever_") {
		t.Errorf("--yes output:\n%s", w.output())
	}

	// No answer available (stdin closed) and no --yes: say so instead of guessing.
	w = newWorld(t)
	w.games = []string{w.game, second}
	if err := w.setup(Options{}); !errors.Is(err, ErrFailed) || !strings.Contains(w.output(), "Run again with --game DIR, or --yes") {
		t.Fatalf("err=%v\n%s", err, w.output())
	}
	// A bad answer.
	w = newWorld(t)
	w.games = []string{w.game, second}
	w.deps.In = strings.NewReader("7\n")
	if err := w.setup(Options{}); !errors.Is(err, ErrFailed) || !strings.Contains(w.output(), `✗ "7" is not one of 1-2.`) {
		t.Fatalf("err=%v\n%s", err, w.output())
	}

	// Nothing found: say where we looked and how to point at it.
	w = newWorld(t)
	w.games = nil
	err := w.setup(Options{})
	out = w.output()
	if !errors.Is(err, ErrFailed) || !strings.Contains(out, "✗ Couldn't find WoW: Forever. I looked in:\n  <home>/Games/*/drive_c/Program Files (x86)/World of Warcraft/_forever_\n") ||
		!strings.Contains(out, `ompcraft setup --game "/path/to/World of Warcraft/_forever_"`) {
		t.Fatalf("err=%v\n%s", err, out)
	}

	// --game wins over everything and must be a folder.
	w = newWorld(t)
	w.games = nil
	if err := w.setup(Options{Game: w.game, NoService: true, NoTern: true}); err != nil {
		t.Fatalf("%v\n%s", err, w.output())
	}
	w = newWorld(t)
	if err := w.setup(Options{Game: filepath.Join(w.game, "missing")}); !errors.Is(err, ErrFailed) || !strings.Contains(w.output(), "is not a folder") {
		t.Fatalf("err=%v\n%s", err, w.output())
	}

	// A saved game folder is used without asking, even with several candidates.
	w = newWorld(t)
	w.games = []string{second, w.game}
	if err := w.env.SaveGame(w.game); err != nil {
		t.Fatal(err)
	}
	if err := w.setup(Options{NoService: true, NoTern: true}); err != nil || !strings.Contains(w.output(), "✓ Found WoW: Forever at <home>/Games/wow/_forever_") {
		t.Fatalf("%v\n%s", err, w.output())
	}
}

func TestInstallFailureStopsSetup(t *testing.T) {
	w := newWorld(t)
	w.deps.Addon = fakeAddonFS("1") // fine...
	w.deps.Addon = emptyFS{}        // ...but this has no Ompcraft.toc
	err := w.setup(Options{})
	if !errors.Is(err, ErrFailed) || !strings.Contains(w.output(), "✗ Couldn't install into ") {
		t.Fatalf("err=%v\n%s", err, w.output())
	}
	if len(w.sys.calls) != 0 {
		t.Errorf("later steps ran: %v", w.sys.calls)
	}
}
