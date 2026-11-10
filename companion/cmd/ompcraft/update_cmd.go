package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/gamedir"
	"github.com/TechDufus/ompcraft/companion/internal/setup"
	"github.com/TechDufus/ompcraft/companion/internal/update"
)

const sourceRefusal = "built from source; update with your build method or re-run install.sh"

// updateRun is everything `ompcraft update` touches, injectable for tests.
type updateRun struct {
	Out     io.Writer
	Updater *update.Updater
	Channel string
	Check   bool
	Force   bool
	// Restart restarts the background service so the new binary runs. It
	// returns a line describing what it did.
	Restart func(ctx context.Context) (string, error)
	// Env, if set, is where state.json lives (bad and staged versions).
	Env *config.Env
	// GameRunning tells whether WoW runs, for the /reload hint.
	GameRunning func() bool
}

// runUpdate implements `ompcraft update [--check] [--force]`.
func runUpdate(ctx context.Context, r updateRun) error {
	u := r.Updater
	rel, err := u.Latest(ctx)
	if err != nil {
		return fmt.Errorf("cannot check for updates: %w", err)
	}
	newer, err := u.Newer(rel)
	if err != nil {
		return err
	}
	if r.Check {
		if newer {
			fmt.Fprintf(r.Out, "%s is available (you have %s)\n", rel.Version, u.Version)
		} else {
			fmt.Fprintf(r.Out, "up to date (%s)\n", u.Version)
		}
		return nil
	}
	if r.Channel != "release" && !r.Force {
		return &exitError{code: 1, err: errors.New(sourceRefusal)}
	}
	if !newer && !r.Force {
		fmt.Fprintf(r.Out, "up to date (%s)\n", u.Version)
		return nil
	}
	var st config.State
	if r.Env != nil {
		st = r.Env.LoadState()
	}
	if st.BadVersion == rel.Version.String() && !r.Force {
		return &exitError{code: 1, err: fmt.Errorf("%s failed to start on this machine before and was rolled back; use --force to try it again", rel.Version)}
	}
	if err := u.Apply(ctx, rel); err != nil {
		return fmt.Errorf("update failed, nothing was changed: %w", err)
	}
	if r.Env != nil {
		_ = r.Env.UpdateState(func(s *config.State) {
			s.StagedVersion = ""
			if s.BadVersion == rel.Version.String() {
				s.BadVersion = ""
			}
		})
	}
	fmt.Fprintf(r.Out, "✓ Downloaded ompcraft %s for %s/%s, signature and checksum OK\n", rel.Version, u.GOOS, u.GOARCH)
	fmt.Fprintf(r.Out, "✓ Replaced %s (the previous binary is kept as %s)\n", u.Exe, u.OldPath())
	msg, err := r.Restart(ctx)
	switch {
	case err != nil:
		fmt.Fprintf(r.Out, "✗ Couldn't restart the service: %v\n", err)
		fmt.Fprintln(r.Out, "  Restart it yourself so the new version runs: ompcraft setup")
		return &exitError{code: 1}
	default:
		fmt.Fprintf(r.Out, "%s\n", msg)
	}
	fmt.Fprintln(r.Out, "  The new version refreshes the addon and the Tern plugin when it starts.")
	if r.GameRunning != nil && r.GameRunning() {
		fmt.Fprintln(r.Out, "• WoW is running: type /reload in game to load the new addon.")
	}
	return nil
}

// restartService restarts the background service if there is one running.
func restartService(d setup.Deps) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		svc := setup.NewService(d)
		if !svc.Managed() {
			return "• No service manager here: restart `ompcraft run` yourself to use the new version.", nil
		}
		st, err := svc.Status(ctx)
		if err != nil {
			return "", err
		}
		if !st.Enabled && !st.Active {
			return "• No ompcraft service is running; start the new version with `ompcraft run` (or `ompcraft setup` to install the service).", nil
		}
		if err := svc.Restart(ctx); err != nil {
			return "", err
		}
		return "✓ Restarted " + svc.Describe(), nil
	}
}

func cmdUpdate(args []string) error {
	fset := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fset.Bool("check", false, "only say whether a newer release exists")
	force := fset.Bool("force", false, "install the latest release even if it is not newer (also lets a source build update)")
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	// Refuse early for source builds, before any network.
	if channel != "release" && !*force && !*check {
		return &exitError{code: 1, err: errors.New(sourceRefusal)}
	}
	d, err := systemDeps()
	if err != nil {
		return err
	}
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	env, err := config.SystemEnv()
	if err != nil {
		return err
	}
	u := newUpdater(exe)
	// A manual update is recorded like an automatic one, so a version that
	// keeps failing to start is rolled back.
	u.BeforeSwap, u.SwapFailed = update.StateHooks(env, version)
	return runUpdate(ctx, updateRun{
		Out:     os.Stdout,
		Updater: u,
		Channel: channel,
		Check:   *check,
		Force:   *force,
		Restart: restartService(d),
		Env:     &env,
		GameRunning: func() bool {
			pid, _ := gamedir.FindGame()
			return pid != 0
		},
	})
}

func newUpdater(exe string) *update.Updater {
	return &update.Updater{
		BaseURL:   update.BaseURLFromEnv(),
		Version:   version,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		Exe:       exe,
		PublicKey: update.ReleasePublicKey,
	}
}

// resolvedExecutable is the absolute, symlink-free path of this binary. It must
// be read before an update replaces the file: afterwards Linux reports it as
// "... (deleted)".
func resolvedExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot find my own path: %w", err)
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return exe, nil
}
