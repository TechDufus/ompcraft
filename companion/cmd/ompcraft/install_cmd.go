package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/TechDufus/ompcraft"
	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/gamedir"
	"github.com/TechDufus/ompcraft/companion/internal/install"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// resolveGame picks G: the flag, else the saved config, else auto-detection.
func resolveGame(env config.Env, flagVal string) (string, error) {
	if flagVal != "" {
		abs, err := filepath.Abs(flagVal)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	cfg, err := env.LoadConfig()
	if err != nil {
		return "", err
	}
	if cfg.Game != "" {
		if st, err := os.Stat(cfg.Game); err == nil && st.IsDir() {
			return cfg.Game, nil
		}
	}
	return gamedir.DetectOne(gamedir.Patterns(env.Home))
}

func gameRunning() (bool, error) {
	pid, err := gamedir.FindGame()
	return pid != 0, err
}

// cmdInstall is the low-level install; `setup` is the front door.
func cmdInstall(args []string) error {
	fset := flag.NewFlagSet("install", flag.ContinueOnError)
	game := fset.String("game", "", "game flavour folder (…/World of Warcraft/_forever_)")
	slots := fset.Int("slots", layout.DefaultSlots, "number of link slots")
	signals := fset.Int("signals", layout.DefaultSignals, "number of doorbell files")
	addonSrc := fset.String("addon-src", "", "development override: install the addon from this folder instead of the embedded copy")
	addonOnly := fset.Bool("addon-only", false, "copy only the Ompcraft addon (allowed while the game runs)")
	force := fset.Bool("force", false, "install even if the game appears to be running")
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
	var addon fs.FS = ompcraft.Addon
	if *addonSrc != "" {
		dir, err := install.ResolveAddonSrc(*addonSrc)
		if err != nil {
			return err
		}
		addon = os.DirFS(dir)
	}
	err = install.Install(install.Options{
		Game:        g,
		AddonFS:     addon,
		Slots:       *slots,
		Signals:     *signals,
		AddonOnly:   *addonOnly,
		Force:       *force,
		GameRunning: gameRunning,
		Logf:        func(f string, a ...any) { fmt.Printf(f+"\n", a...) },
	})
	if err != nil {
		return err
	}
	if err := saveGame(env, g); err != nil {
		return fmt.Errorf("installed, but saving %s failed: %w", env.ConfigPath(), err)
	}
	fmt.Printf("game folder: %s\n", g)
	if *addonOnly {
		fmt.Println("addon updated; /reload in game to pick it up")
	} else {
		fmt.Println("done; start (or restart) the game so it sees the new folders, then run `ompcraft run`")
	}
	return nil
}
