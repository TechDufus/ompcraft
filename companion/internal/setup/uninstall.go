package setup

import (
	"context"
	"os"
	"path/filepath"

	"github.com/TechDufus/ompcraft/companion/internal/install"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// UninstallOptions are the flags of `ompcraft uninstall`.
type UninstallOptions struct {
	Game  string
	Force bool // uninstall even while the game runs
	Purge bool // also remove config, state and link files
}

// Uninstall reverses setup: the service, the Tern plugin, the addon and its
// helpers. Config, state and link files only go with Purge. It prints what it
// removed.
func Uninstall(ctx context.Context, d Deps, o UninstallOptions) error {
	p := printer{d.Out}
	failed := false

	// Refuse before touching anything if the game runs (the addon folders are in use).
	if !o.Force {
		pid, err := d.GameRunning()
		switch {
		case err != nil:
			p.fail("I can't tell whether World of Warcraft is running (%v). Close it and pass --force to uninstall anyway.", err)
			return ErrFailed
		case pid != 0:
			p.fail("World of Warcraft is running. Close it first, or pass --force.")
			return ErrFailed
		}
	}

	// Service: stop it first, so it wipes the slots on its way out.
	svc := NewService(d)
	if removed, err := svc.Uninstall(ctx); err != nil {
		p.fail("Couldn't remove the %s: %v", svc.Describe(), err)
		failed = true
	} else {
		for _, r := range removed {
			p.ok("Removed %s", r)
		}
		if len(removed) == 0 && svc.Managed() {
			p.note("No ompcraft service was installed.")
		}
	}

	// Tern plugin.
	t := tern{d.Runner}
	if t.available() {
		switch what, err := t.unlink(ctx); {
		case err != nil:
			p.fail("Couldn't remove the Tern plugin: %v (try `tern plugin unlink ompcraft`)", err)
			failed = true
		case what != "":
			p.ok("Removed the %s", what)
		default:
			p.note("The Tern plugin was not linked.")
		}
	} else {
		p.note("Tern is not installed; nothing to unlink.")
	}
	if _, err := os.Stat(d.Env.PluginDir()); err == nil {
		if err := os.RemoveAll(d.Env.PluginDir()); err != nil {
			p.fail("Couldn't remove %s: %v", d.Env.PluginDir(), err)
			failed = true
		} else {
			p.ok("Removed the plugin copy in %s", d.Env.PluginDir())
		}
	}

	// Addon and helpers.
	if game := uninstallGame(d, o.Game); game == "" {
		p.note("No game folder found; the addon was not removed. Use --game DIR if it is installed somewhere unusual.")
	} else {
		removed, err := install.Uninstall(game)
		for _, n := range removed {
			p.ok("Removed %s", filepath.Join(layout.AddonsDir(game), n))
		}
		switch {
		case err != nil:
			p.fail("Couldn't remove everything from %s: %v", layout.AddonsDir(game), err)
			failed = true
		case len(removed) == 0:
			p.note("No ompcraft folders in %s.", layout.AddonsDir(game))
		}
	}

	// Config, state and link files.
	if o.Purge {
		removed, err := d.Env.Purge()
		for _, r := range removed {
			p.ok("Removed %s", r)
		}
		if err != nil {
			p.fail("Couldn't remove all config and state files: %v", err)
			failed = true
		}
	} else {
		p.note("Kept your config and state in %s and %s (add --purge to remove them).", d.Env.ConfigDir(), d.Env.StateDir())
	}

	p.blank()
	if failed {
		p.fail("Uninstall finished with problems (see above).")
		return ErrFailed
	}
	p.ok("ompcraft is uninstalled.")
	return nil
}

func uninstallGame(d Deps, explicit string) string {
	if explicit != "" {
		if abs, err := filepath.Abs(explicit); err == nil {
			return abs
		}
		return explicit
	}
	if g := savedGame(d.Env); g != "" {
		if st, err := os.Stat(g); err == nil && st.IsDir() {
			return g
		}
	}
	if found, err := d.Detect(); err == nil && len(found) == 1 {
		return found[0]
	}
	return ""
}
