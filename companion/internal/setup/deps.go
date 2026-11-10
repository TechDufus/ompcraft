// Package setup is the friendly front door: `ompcraft setup` installs
// everything, `doctor` checks it and `uninstall` reverses it. Every system
// interaction (running programs, the process list, the companion's HTTP API,
// sleeping, prompting) goes through Deps so tests can fake it.
package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/gamedir"
	"github.com/TechDufus/ompcraft/companion/internal/server"
)

// Runner runs external programs (systemctl, launchctl, tern).
type Runner interface {
	// LookPath finds name on PATH.
	LookPath(name string) (string, error)
	// Run runs name and returns its stdout. A non-zero exit returns the stdout
	// together with an error that includes stderr.
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner is the real Runner.
type ExecRunner struct{}

func (ExecRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return stdout.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
	}
	return stdout.String(), nil
}

// Deps is everything setup, doctor and uninstall touch outside their own
// files.
type Deps struct {
	Out io.Writer
	In  io.Reader
	Env config.Env
	// GOOS selects the service manager ("linux", "darwin", other).
	GOOS string
	UID  int
	// Exe is the absolute path of the running binary (what the service runs).
	Exe string
	// Addon and Plugin are the embedded files (roots hold Ompcraft.toc and
	// plugin.toml).
	Addon, Plugin fs.FS
	Runner        Runner
	// GameRunning returns the game's pid, 0 when it is not running.
	GameRunning func() (int, error)
	// Detect lists candidate game folders; Looked is where it looks, for the
	// "not found" message.
	Detect func() ([]string, error)
	Looked []string
	// FetchState asks the running companion for /v1/state.
	FetchState func() (server.StateResponse, error)
	// Sleep waits d or returns ctx's error.
	Sleep func(ctx context.Context, d time.Duration) error
}

// SystemDeps wires the real system.
func SystemDeps(out io.Writer, in io.Reader, addon, plugin fs.FS) (Deps, error) {
	env, err := config.SystemEnv()
	if err != nil {
		return Deps{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Deps{}, fmt.Errorf("cannot find my own path: %w", err)
	}
	if resolved, err := evalSymlinks(exe); err == nil {
		exe = resolved
	}
	patterns := gamedir.Patterns(env.Home)
	return Deps{
		Out: out, In: in, Env: env,
		GOOS: goos(), UID: os.Getuid(), Exe: exe,
		Addon: addon, Plugin: plugin,
		Runner:      ExecRunner{},
		GameRunning: gamedir.FindGame,
		Detect:      func() ([]string, error) { return gamedir.Detect(patterns) },
		Looked:      patterns,
		FetchState: func() (server.StateResponse, error) {
			link, err := env.ReadLink()
			if err != nil {
				return server.StateResponse{}, errors.New("no link.json: the companion is not running")
			}
			return server.FetchState(link.URL, link.Token, 2*time.Second)
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}, nil
}
