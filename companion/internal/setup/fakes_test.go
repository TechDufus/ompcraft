package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/daemon"
	"github.com/TechDufus/ompcraft/companion/internal/model"
	"github.com/TechDufus/ompcraft/companion/internal/server"
)

// fakeSys is a tiny systemd and a tiny tern, enough for setup to talk to.
// It records every command it is asked to run.
type fakeSys struct {
	t     *testing.T
	calls []string

	haveTern, haveSystemctl bool

	// systemd
	unitPath      string
	transient     bool   // a unit started with systemd-run exists
	transientPath string // its FragmentPath ("" = empty, as the spec describes)
	active        bool
	enabled       bool
	busDown       bool // `systemctl --user` cannot reach the user session

	// tern
	plugDir      string
	pluginStatus string // "" = ready
	failTern     map[string]error
}

func newFakeSys(t *testing.T, env config.Env) *fakeSys {
	plug := filepath.Join(env.Home, "tern-plugins")
	if err := os.MkdirAll(plug, 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeSys{
		t: t, haveTern: true, haveSystemctl: true,
		unitPath: filepath.Join(env.ConfigHome, "systemd", "user", unitName),
		plugDir:  plug, failTern: map[string]error{},
	}
}

func (f *fakeSys) LookPath(name string) (string, error) {
	switch {
	case name == "tern" && f.haveTern:
		return "/usr/bin/tern", nil
	case name == "systemctl" && f.haveSystemctl:
		return "/usr/bin/systemctl", nil
	}
	return "", errors.New("not found: " + name)
}

func (f *fakeSys) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeSys) called(cmd string) bool {
	for _, c := range f.calls {
		if c == cmd {
			return true
		}
	}
	return false
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func (f *fakeSys) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, cmd)
	switch name {
	case "tern":
		return f.tern(cmd, args)
	case "systemctl":
		return f.systemctl(args)
	}
	f.t.Fatalf("setup ran an unexpected program: %s", cmd)
	return "", nil
}

func (f *fakeSys) tern(cmd string, args []string) (string, error) {
	if err, ok := f.failTern[cmd]; ok {
		return "", err
	}
	path := filepath.Join(f.plugDir, "ompcraft.path")
	switch strings.Join(args, " ") {
	case "plugin dir --json":
		b, _ := json.Marshal(map[string]string{"dir": f.plugDir})
		return string(b), nil
	case "plugin list --json":
		type plugin struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		out := struct {
			Plugins  []plugin `json:"plugins"`
			Problems []string `json:"problems"`
		}{Plugins: []plugin{}, Problems: []string{}}
		if exists(path) {
			st := f.pluginStatus
			if st == "" {
				st = "ready"
			}
			out.Plugins = append(out.Plugins, plugin{ID: "ompcraft", Status: st})
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	case "plugin unlink ompcraft":
		return "", os.Remove(path)
	case "plugin remove ompcraft":
		return "", os.RemoveAll(filepath.Join(f.plugDir, "ompcraft"))
	case "plugin reload ompcraft":
		return "", nil
	}
	if len(args) == 3 && args[0] == "plugin" && args[1] == "link" {
		return "", os.WriteFile(path, []byte(args[2]+"\n"), 0o644)
	}
	f.t.Fatalf("unexpected tern command: %s", cmd)
	return "", nil
}

func (f *fakeSys) systemctl(args []string) (string, error) {
	if f.busDown {
		return "", errors.New("Failed to connect to bus: No medium found")
	}
	if len(args) == 0 || args[0] != "--user" {
		f.t.Fatalf("systemctl without --user: %v", args)
	}
	rest := strings.Join(args[1:], " ")
	switch rest {
	case "show ompcraft.service -p LoadState -p ActiveState -p FragmentPath":
		load, fp := "not-found", ""
		switch {
		case f.transient:
			load, fp = "loaded", f.transientPath
		case exists(f.unitPath):
			load, fp = "loaded", f.unitPath
		}
		st := "inactive"
		if f.active {
			st = "active"
		}
		return fmt.Sprintf("LoadState=%s\nActiveState=%s\nFragmentPath=%s\n", load, st, fp), nil
	case "stop ompcraft.service":
		f.active, f.transient = false, false
	case "reset-failed ompcraft.service", "daemon-reload":
	case "enable --now ompcraft.service":
		f.enabled, f.active = true, true
	case "restart ompcraft.service":
		f.active = true
	case "disable --now ompcraft.service":
		f.enabled, f.active = false, false
	case "is-enabled ompcraft.service":
		if f.enabled {
			return "enabled\n", nil
		}
		return "disabled\n", errors.New("exit status 1")
	case "is-active ompcraft.service":
		if f.active {
			return "active\n", nil
		}
		return "inactive\n", errors.New("exit status 3")
	default:
		f.t.Fatalf("unexpected systemctl command: %s", rest)
	}
	return "", nil
}

// world is a complete fake machine for setup, doctor and uninstall.
type world struct {
	t     *testing.T
	env   config.Env
	game  string
	sys   *fakeSys
	out   bytes.Buffer
	deps  Deps
	games []string // what Detect returns

	// game process: running until `closeAfter` polls were made.
	gamePolls  int
	closeAfter int
	gameErr    error

	// companion: answers after `stateFailures` failed calls.
	stateFailures int
	stateCalls    int
	ternSyncing   bool
	sleeps        int
	input         string
}

const addonTOC = "## Interface: 16001\n## Version: 9.9.9\nCore/Init.lua\n"

func fakeAddonFS(version string) fs.FS {
	return fstest.MapFS{
		"Ompcraft.toc":  {Data: []byte("## Interface: 16001\n## Version: " + version + "\nCore/Init.lua\n")},
		"Core/Init.lua": {Data: []byte("-- addon\n")},
	}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	env := config.EnvFrom(home, filepath.Join(home, "cfg"), filepath.Join(home, "state")).WithDataHome(filepath.Join(home, "data"))
	w := &world{t: t, env: env, ternSyncing: true}
	w.game = filepath.Join(home, "Games", "wow", "_forever_")
	if err := os.MkdirAll(w.game, 0o755); err != nil {
		t.Fatal(err)
	}
	w.games = []string{w.game}
	w.sys = newFakeSys(t, env)
	w.deps = Deps{
		Out: &w.out, In: strings.NewReader(""), Env: env,
		GOOS: "linux", UID: 1000, Exe: "/home/user/go/bin/ompcraft",
		Addon: fakeAddonFS("9.9.9"),
		Plugin: fstest.MapFS{
			"plugin.toml": {Data: []byte("id = \"ompcraft\"\n")},
			"window.luau": {Data: []byte("-- plugin\n")},
		},
		Runner: w.sys,
		GameRunning: func() (int, error) {
			if w.gameErr != nil {
				return 0, w.gameErr
			}
			w.gamePolls++
			if w.gamePolls <= w.closeAfter {
				return 4242, nil
			}
			return 0, nil
		},
		Detect: func() ([]string, error) { return w.games, nil },
		Looked: []string{home + "/Games/*/drive_c/Program Files (x86)/World of Warcraft/_forever_", home + "/.wine/drive_c/Program Files (x86)/World of Warcraft/_forever_"},
		FetchState: func() (server.StateResponse, error) {
			w.stateCalls++
			if w.stateCalls <= w.stateFailures {
				return server.StateResponse{}, errors.New("connection refused")
			}
			tern := "down"
			if w.ternSyncing {
				tern = "ok"
			}
			return server.StateResponse{PID: 777, Report: daemon.Report{Version: "0.1.0-test",
				Snapshot: model.Snapshot{Tern: tern}, LastSyncMs: 1}}, nil
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			w.sleeps++
			return ctx.Err()
		},
	}
	return w
}

// emptyFS is an fs.FS with nothing in it.
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func pluginFS(window string) fs.FS {
	return fstest.MapFS{
		"plugin.toml": {Data: []byte("id = \"ompcraft\"\n")},
		"window.luau": {Data: []byte(window)},
	}
}
