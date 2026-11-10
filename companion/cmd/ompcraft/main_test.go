package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft"
	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/daemon"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/model"
	"github.com/TechDufus/ompcraft/companion/internal/server"
	"github.com/TechDufus/ompcraft/companion/internal/setup"
)

func TestDispatch(t *testing.T) {
	err := dispatch(nil)
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("no args: %v", err)
	}
	if err := dispatch([]string{"bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("unknown command: %v", err)
	}
	if err := dispatch([]string{"version"}); err != nil {
		t.Fatal(err)
	}
}

func TestHelpListsSetupFirst(t *testing.T) {
	setupAt := strings.Index(usage, "ompcraft setup")
	for _, other := range []string{"ompcraft doctor", "ompcraft status", "ompcraft uninstall", "ompcraft install", "ompcraft run", "ompcraft config"} {
		if i := strings.Index(usage, other); i < 0 || i < setupAt {
			t.Errorf("%q must be listed after setup in the help text", other)
		}
	}
	if setupAt < 0 || !strings.Contains(usage, "--yes") || !strings.Contains(usage, "--no-service") || !strings.Contains(usage, "--no-tern") || !strings.Contains(usage, "--purge") {
		t.Error("help text lacks the setup and uninstall flags")
	}
}

func TestEmbeddedAssets(t *testing.T) {
	// The binary carries the addon and the Tern plugin.
	for _, f := range []string{"Ompcraft.toc", "Core/Init.lua", "UI/Tracker.lua"} {
		if _, err := fs.Stat(ompcraft.Addon, f); err != nil {
			t.Errorf("embedded addon lacks %s: %v", f, err)
		}
	}
	for _, f := range []string{"plugin.toml", "window.luau"} {
		if _, err := fs.Stat(ompcraft.TernPlugin, f); err != nil {
			t.Errorf("embedded plugin lacks %s: %v", f, err)
		}
	}
	toc, err := fs.ReadFile(ompcraft.Addon, "Ompcraft.toc")
	if err != nil || layout.TOCVersion(toc) == "" {
		t.Errorf("embedded toc has no version: %v", err)
	}
	// Tests and docs are not shipped inside the addon.
	if _, err := fs.Stat(ompcraft.Addon, "tests"); err == nil {
		t.Error("addon tests are embedded")
	}
}

func TestSetupRejectsBadFlags(t *testing.T) {
	for _, cmd := range []string{"setup", "doctor", "uninstall"} {
		var ee *exitError
		if err := dispatch([]string{cmd, "--no-such-flag"}); !errors.As(err, &ee) || ee.code != 2 {
			t.Errorf("%s with a bad flag: %v", cmd, err)
		}
	}
}

func TestSetupExitCodes(t *testing.T) {
	var ee *exitError
	if err := setupExit(setup.ErrFailed); !errors.As(err, &ee) || ee.code != 1 || ee.Error() != "" {
		t.Errorf("ErrFailed -> %v", err)
	}
	if err := setupExit(setup.ErrAborted); !errors.As(err, &ee) || ee.code != 130 {
		t.Errorf("ErrAborted -> %v", err)
	}
	if err := setupExit(nil); err != nil {
		t.Errorf("nil -> %v", err)
	}
	other := errors.New("boom")
	if err := setupExit(other); err != other {
		t.Errorf("other -> %v", err)
	}
}

func TestResolveGame(t *testing.T) {
	home := t.TempDir()
	env := config.EnvFrom(home, filepath.Join(home, "cfg"), filepath.Join(home, "state"))

	// Explicit flag wins and is made absolute.
	if got, err := resolveGame(env, "rel/dir"); err != nil || !filepath.IsAbs(got) {
		t.Fatalf("flag: %q %v", got, err)
	}
	// Saved config is used while the directory exists.
	saved := filepath.Join(home, "saved", "_forever_")
	_ = os.MkdirAll(saved, 0o755)
	if err := env.SaveConfig(config.Config{Game: saved}); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveGame(env, ""); err != nil || got != saved {
		t.Fatalf("saved: %q %v", got, err)
	}
	// A stale saved path falls through to detection, which finds nothing under a temp home.
	if err := env.SaveConfig(config.Config{Game: filepath.Join(home, "gone")}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveGame(env, ""); err == nil || !strings.Contains(err.Error(), "--game") {
		t.Fatalf("stale config: %v", err)
	}
}

func TestPrintStatus(t *testing.T) {
	st := server.StateResponse{
		PID:    1234,
		Leader: server.LeaderInfo{Window: "w", VM: "vm1", Active: true},
		Report: daemon.Report{
			Version: "0.1.0", Slots: 300, Signals: 2000, LoadedSlots: []int{1, 2}, SignalNext: 5,
			Session: 1762727900, GamePid: 4321, LastSyncMs: time.Now().Add(-time.Second).UnixMilli(),
			Snapshot: model.Snapshot{Gen: 17, Tern: "ok", Agents: []model.AgentView{
				{State: "working"}, {State: "working"}, {State: "idle"}}},
			Counters: daemon.Counters{Publishes: 17, SlotWrites: 5000, Doorbells: 17, SessionStarts: 2, NewLaunches: 1,
				Inotify: map[string]int64{"lua_open": 4}},
		},
	}
	var buf bytes.Buffer
	printStatus(&buf, "http://127.0.0.1:47131", st, time.Now())
	out := buf.String()
	for _, want := range []string{
		"ompcraft companion 0.1.0 (pid 1234) at http://127.0.0.1:47131",
		"tern:     ok (last sync 1s ago)",
		"game:     running (pid 4321)",
		"slots:    2/300 loaded; next doorbell 5/2000",
		"agents:   3 (1 idle, 2 working)",
		"gen 17; 17 publishes, 5000 slot writes, 17 doorbells rung",
		"lua_open:4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output lacks %q:\n%s", want, out)
		}
	}
}
