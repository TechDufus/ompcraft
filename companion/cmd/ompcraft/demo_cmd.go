package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/gamedir"
)

// cmdDemo runs the normal daemon (slots, doorbells, inotify, publisher) fed by
// a scripted source of invented agents, for README screenshots. It starts no
// HTTP server and writes no link.json, so the real Tern plugin sees "companion
// not running". Not listed in --help.
func cmdDemo(args []string) error {
	fset := flag.NewFlagSet("demo", flag.ContinueOnError)
	game := fset.String("game", "", "game flavour folder (…/World of Warcraft/_forever_)")
	verbose := fset.Bool("v", false, "verbose: log every IN_OPEN and each publish")
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	env, err := config.SystemEnv()
	if err != nil {
		return err
	}
	g, err := resolveGame(env, *game)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runCompanion(ctx, runOptions{
		Env: env, Game: g, Verbose: *verbose, FindGame: gamedir.FindGame,
		Version: version, Channel: channel, Demo: true, Args: os.Args,
	})
}
