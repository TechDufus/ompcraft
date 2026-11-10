package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/TechDufus/ompcraft"
	"github.com/TechDufus/ompcraft/companion/internal/setup"
)

func systemDeps() (setup.Deps, error) {
	return setup.SystemDeps(os.Stdout, os.Stdin, ompcraft.Addon, ompcraft.TernPlugin)
}

// setupExit maps the setup package's sentinel errors to exit codes. The steps
// have already printed their own ✓/✗ lines, so these exit without a message.
func setupExit(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, setup.ErrFailed):
		return &exitError{code: 1}
	case errors.Is(err, setup.ErrAborted):
		return &exitError{code: 130}
	}
	return err
}

func cmdSetup(args []string) error {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	game := fset.String("game", "", "game flavour folder (…/World of Warcraft/_forever_); found automatically if omitted")
	yes := fset.Bool("yes", false, "never ask: take the first game found")
	noService := fset.Bool("no-service", false, "do not install the background service")
	noTern := fset.Bool("no-tern", false, "do not set up the Tern plugin")
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	d, err := systemDeps()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return setupExit(setup.Setup(ctx, d, setup.Options{Game: *game, Yes: *yes, NoService: *noService, NoTern: *noTern}))
}

func cmdDoctor(args []string) error {
	fset := flag.NewFlagSet("doctor", flag.ContinueOnError)
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	d, err := systemDeps()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return setupExit(setup.Doctor(ctx, d))
}

func cmdUninstall(args []string) error {
	fset := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	game := fset.String("game", "", "game flavour folder (…/World of Warcraft/_forever_)")
	force := fset.Bool("force", false, "uninstall even if the game appears to be running")
	purge := fset.Bool("purge", false, "also remove ompcraft's config, state and log files")
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	d, err := systemDeps()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return setupExit(setup.Uninstall(ctx, d, setup.UninstallOptions{Game: *game, Force: *force, Purge: *purge}))
}
