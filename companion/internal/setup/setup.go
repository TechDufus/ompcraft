package setup

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/install"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/server"
)

// Options are the flags of `ompcraft setup`.
type Options struct {
	Game      string
	Yes       bool
	NoService bool
	NoTern    bool
}

const (
	pollEvery        = time.Second
	verifyEvery      = 500 * time.Millisecond
	verifyAttempts   = 20 // 10 s
	readyNextMessage = "Next: start World of Warcraft. Your agents appear under your quest objectives. /oc help for commands."
)

// Setup runs the one-command install. Each step prints one line: ✓ when it
// worked, ✗ with how to fix it when it did not. Steps that cannot work (no
// Tern) do not stop the rest. It returns ErrFailed if anything needs
// attention and ErrAborted on Ctrl-C.
func Setup(ctx context.Context, d Deps, o Options) error {
	p := printer{d.Out}
	in := bufio.NewReader(d.In)
	incomplete := false

	// 1. Find the game.
	game, err := resolveGame(d, p, o.Game, o.Yes, in)
	if err != nil {
		return err
	}
	p.ok("Found WoW: Forever at %s", game)

	// 2. Wait for the game to close, but only if there is something to create:
	// new helper folders or files cannot be added while the client runs. When
	// every helper already exists, the addon is refreshed in place instead.
	addons := layout.AddonsDir(game)
	forceInstall, inPlace := false, false
	pid, err := d.GameRunning()
	switch {
	case err != nil:
		p.note("I can't tell whether World of Warcraft is running here (%v). Make sure it is closed.", err)
		forceInstall = true
	case pid != 0 && helpersComplete(addons):
		inPlace = true
	case pid != 0:
		p.note("Close World of Warcraft to continue (setup will pick up automatically)…")
		for pid != 0 {
			if err := d.Sleep(ctx, pollEvery); err != nil {
				p.fail("Aborted.")
				return ErrAborted
			}
			if pid, err = d.GameRunning(); err != nil {
				p.note("I can't tell whether World of Warcraft is running any more (%v); carrying on.", err)
				forceInstall = true
				break
			}
		}
		p.ok("World of Warcraft is closed")
	default:
		p.ok("World of Warcraft is not running")
	}

	// 3. Install (keeping existing helper counts). In place: only the addon.
	slots, signals := layout.DefaultSlots, layout.DefaultSignals
	if l, err := layout.ReadInstalled(addons); err == nil {
		slots, signals = l.Slots, l.Signals
	}
	err = install.Install(install.Options{
		Game: game, AddonFS: d.Addon, Slots: slots, Signals: signals, AddonOnly: inPlace,
		Force: forceInstall, GameRunning: func() (bool, error) { return false, nil },
	})
	if err != nil {
		p.fail("Couldn't install into %s: %v", addons, err)
		return ErrFailed
	}
	version := embeddedVersion(d.Addon)
	if inPlace {
		p.ok("Updated the addon%s (type /reload in game)", versionSuffix(version))
		// Helper TOCs from an older release get the new category/group lines. The
		// client reads TOC metadata at launch, so rewriting them now is harmless.
		if l, err := layout.ReadInstalled(addons); err == nil {
			switch n, err := install.RefreshHelperTOCs(l); {
			case err != nil:
				p.fail("Couldn't update the link helpers' TOC files: %v", err)
				incomplete = true
			case n > 0:
				p.note("Restart WoW once to tidy the AddOns list (its helpers move into a collapsible \"%s\" section)", layout.HelperCategory)
			}
		}
	} else {
		p.ok("Installed the addon%s, %d link slots and %d doorbells in %s", versionSuffix(version), slots, signals, addons)
	}
	if err := d.Env.SaveGame(game); err != nil {
		p.fail("Couldn't remember the game folder in %s: %v", d.Env.ConfigPath(), err)
		incomplete = true
	}

	// 4. Tern plugin.
	ternReady := false
	if o.NoTern {
		p.note("Skipping the Tern plugin (--no-tern).")
	} else {
		ternReady = setupTern(ctx, d, p, &incomplete)
	}

	// 5. Service.
	svc := NewService(d)
	serviceReady := false
	switch {
	case o.NoService:
		p.note("Skipping the service (--no-service). Start the companion yourself with: ompcraft run")
	case !svc.Managed():
		p.note("There is no service manager support on %s yet. Start the companion yourself with: ompcraft run", d.GOOS)
	default:
		if isTemporaryPath(d.Exe) {
			p.note("ompcraft is running from a temporary location (%s). A service pointing there will break.", d.Exe)
			p.line("Install it properly first: go install github.com/TechDufus/ompcraft/companion/cmd/ompcraft@latest")
			p.line("then run `ompcraft setup` again.")
		}
		res, err := svc.Install(ctx, d.Exe)
		if err != nil {
			p.fail("Couldn't set up the %s: %v", svc.Describe(), err)
			p.line("Start the companion yourself with `ompcraft run`, or fix the above and re-run `ompcraft setup`.")
			incomplete = true
			break
		}
		serviceReady = true
		extra := ""
		if res.ReplacedTransient {
			extra = " (replaced the temporary unit started with systemd-run)"
		}
		p.ok("Companion service enabled and running (%s)%s", res.Describe, extra)
	}

	// 6. Verify.
	if serviceReady {
		st, ok := waitForCompanion(ctx, d, func(s stateView) bool { return true })
		if ctx.Err() != nil {
			p.fail("Aborted.")
			return ErrAborted
		}
		if !ok {
			p.fail("The companion is not answering yet. %s", logHint(d))
			incomplete = true
		} else {
			p.ok("Companion is answering (version %s)", st.Version)
			if ternReady {
				_, ok := waitForCompanion(ctx, d, func(s stateView) bool { return s.Snapshot.Tern == "ok" })
				if ctx.Err() != nil {
					p.fail("Aborted.")
					return ErrAborted
				}
				if ok {
					p.ok("Tern is syncing")
				} else {
					p.fail("Tern isn't syncing yet. Open a Tern window (the plugin runs inside one), then run `ompcraft doctor`.")
					incomplete = true
				}
			}
		}
	}

	p.blank()
	if incomplete {
		p.fail("ompcraft is installed, but not everything is ready. Fix the ✗ above, then run `ompcraft setup` again.")
		return ErrFailed
	}
	switch {
	case inPlace:
		p.ok("ompcraft is ready. Type /reload in game to load the new addon.")
		if !serviceReady {
			p.line("First start the companion: ompcraft run")
		}
	case serviceReady:
		p.ok("ompcraft is ready.")
		p.line("%s", readyNextMessage)
	default:
		p.ok("ompcraft is ready.")
		p.line("First start the companion: ompcraft run")
		p.line("%s", readyNextMessage)
	}
	return nil
}

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return " " + v
}

func embeddedVersion(addon fs.FS) string {
	b, err := fs.ReadFile(addon, layout.AddonName+".toc")
	if err != nil {
		return ""
	}
	return layout.TOCVersion(b)
}

// helpersComplete reports whether every link helper that must exist before
// the client starts is already there: the signals TOC, ctl_present.wav and
// every slot's TOC and data.lua. Doorbells are not checked: the companion
// removes them while the game runs.
func helpersComplete(addons string) bool {
	l, err := layout.ReadInstalled(addons)
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(l.SignalsDir(), layout.CtlPresent)); err != nil {
		return false
	}
	for i := range l.Slots {
		for _, f := range []string{l.SlotData(i), filepath.Join(l.SlotDir(i), layout.SlotName(i)+".toc")} {
			if _, err := os.Stat(f); err != nil {
				return false
			}
		}
	}
	return true
}

func logHint(d Deps) string {
	switch d.GOOS {
	case "linux":
		return "Look at `journalctl --user -u ompcraft -e` or " + d.Env.LogPath()
	}
	return "Look at " + d.Env.LogPath()
}

// setupTern is step 4. It returns true when the plugin is linked and reloaded.
func setupTern(ctx context.Context, d Deps, p printer, incomplete *bool) bool {
	t := tern{d.Runner}
	if !t.available() {
		p.fail("Stencil Tern isn't installed (no `tern` on your PATH). Install Stencil Tern, then re-run `ompcraft setup`.")
		*incomplete = true
		return false
	}
	dir := d.Env.PluginDir()
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_, err = install.WriteTree(d.Plugin, dir)
		if err != nil {
			p.fail("Couldn't write the Tern plugin to %s: %v", dir, err)
			*incomplete = true
			return false
		}
	} else {
		p.fail("Couldn't create %s: %v", dir, err)
		*incomplete = true
		return false
	}
	replaced, err := t.link(ctx, dir)
	if err != nil {
		p.fail("Couldn't link the Tern plugin: %v", err)
		p.line("Try by hand: tern plugin link %s && tern plugin reload ompcraft", dir)
		*incomplete = true
		return false
	}
	extra := ""
	if replaced != "" {
		extra = " (replaced " + replaced + ")"
	}
	p.ok("Tern plugin linked from %s and reloaded%s", dir, extra)
	return true
}

// stateView is the part of /v1/state the polling loops look at.
type stateView = server.StateResponse

// waitForCompanion polls the companion for up to 10 s until ready(state) holds.
func waitForCompanion(ctx context.Context, d Deps, ready func(stateView) bool) (stateView, bool) {
	var last stateView
	for i := 0; i < verifyAttempts; i++ {
		if st, err := d.FetchState(); err == nil {
			last = st
			if ready(st) {
				return st, true
			}
		}
		if err := d.Sleep(ctx, verifyEvery); err != nil {
			return last, false
		}
	}
	return last, false
}
