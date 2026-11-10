package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/daemon"
	"github.com/TechDufus/ompcraft/companion/internal/install"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// check is one line of `ompcraft doctor`.
type check struct {
	name   string
	ok     bool
	info   bool // informational: shown with • when false, never fails doctor
	detail string
	fix    string
}

func (c check) print(p printer) {
	switch {
	case c.ok:
		p.ok("%s: %s", c.name, c.detail)
	case c.info:
		p.note("%s: %s", c.name, c.detail)
	default:
		p.fail("%s: %s", c.name, c.detail)
	}
}

// Doctor runs the read-only checks and prints one line each, then the fix for
// the first failure. It returns ErrFailed if a required check failed.
func Doctor(ctx context.Context, d Deps) error {
	p := printer{d.Out}
	var checks []check
	add := func(c check) {
		c.print(p)
		checks = append(checks, c)
	}

	// Game folder.
	game, gameCheck := findGameQuietly(d)
	add(gameCheck)

	// Addon and helpers.
	if game != "" {
		add(addonCheck(d, game))
		add(helpersCheck(game))
	} else {
		add(check{name: "Addon", detail: "unknown (no game folder)", fix: "run `ompcraft setup`"})
		add(check{name: "Link helpers", detail: "unknown (no game folder)", fix: "run `ompcraft setup`"})
	}

	// Service.
	svc := NewService(d)
	if st, err := svc.Status(ctx); err != nil {
		add(check{name: "Service", detail: err.Error(), fix: "run `ompcraft setup`"})
	} else if !svc.Managed() {
		add(check{name: "Service", ok: false, info: true, detail: "no service manager support on " + d.GOOS + "; run `ompcraft run` yourself"})
	} else {
		add(check{name: "Service", ok: st.Enabled && st.Active,
			detail: fmt.Sprintf("%s (%s)", st.Describe, st.Detail), fix: "run `ompcraft setup` to install and start the service"})
	}

	// Companion.
	state, stateErr := d.FetchState()
	if stateErr != nil {
		add(check{name: "Companion", detail: "not answering", fix: "run `ompcraft setup` (or start it with `ompcraft run`); logs: " + logHint(d)})
	} else {
		add(check{name: "Companion", ok: true, detail: fmt.Sprintf("answering (version %s, pid %d)", state.Version, state.PID)})
	}

	// Updates (informational).
	if stateErr == nil {
		add(updateCheck(state.Update))
	}

	// Tern.
	t := tern{d.Runner}
	ternOK := t.available()
	if ternOK {
		add(check{name: "Tern", ok: true, detail: "found on PATH"})
	} else {
		add(check{name: "Tern", detail: "not found on PATH", fix: "install Stencil Tern, then run `ompcraft setup`"})
	}
	add(pluginCheck(ctx, t, ternOK, d))
	switch {
	case stateErr != nil:
		add(check{name: "Tern sync", detail: "unknown (the companion is not answering)", fix: "start the companion first"})
	case state.Snapshot.Tern == "ok":
		ago := ""
		if state.LastSyncMs > 0 {
			ago = fmt.Sprintf(", last sync %s ago", time.Since(time.UnixMilli(state.LastSyncMs)).Round(100*time.Millisecond))
		}
		add(check{name: "Tern sync", ok: true, detail: "syncing" + ago})
	default:
		add(check{name: "Tern sync", detail: "no sync in the last 3 s",
			fix: "open a Tern window (the plugin runs inside one); if it still fails run `ompcraft setup`"})
	}

	// Game (informational).
	if pid, err := d.GameRunning(); err != nil {
		add(check{name: "Game", info: true, detail: "cannot tell whether it is running (" + err.Error() + ")"})
	} else if pid != 0 {
		add(check{name: "Game", ok: true, info: true, detail: fmt.Sprintf("running (pid %d)", pid)})
	} else {
		add(check{name: "Game", info: true, detail: "not running"})
	}
	switch {
	case stateErr != nil:
		add(check{name: "UI session", info: true, detail: "unknown"})
	case state.Session != 0:
		add(check{name: "UI session", ok: true, info: true, detail: "seen at " + time.Unix(state.Session, 0).Format("15:04:05")})
	default:
		add(check{name: "UI session", info: true, detail: "not seen yet (it starts when you log in to a character)"})
	}

	for _, c := range checks {
		if !c.ok && !c.info {
			p.blank()
			p.fail("Fix: %s", c.fix)
			return ErrFailed
		}
	}
	p.blank()
	p.ok("Everything looks good.")
	return nil
}

// findGameQuietly resolves the game folder without prompting: the saved one,
// else a single auto-detected one.
func findGameQuietly(d Deps) (string, check) {
	c := check{name: "Game folder", fix: "run `ompcraft setup --game \"/path/to/World of Warcraft/_forever_\"`"}
	if g := savedGame(d.Env); g != "" {
		if st, err := os.Stat(g); err == nil && st.IsDir() {
			c.ok, c.detail = true, g
			return g, c
		}
	}
	found, err := d.Detect()
	switch {
	case err != nil:
		c.detail = err.Error()
	case len(found) == 1:
		c.ok, c.detail = true, found[0]
		return found[0], c
	case len(found) > 1:
		c.detail = fmt.Sprintf("%d installs found and none chosen yet", len(found))
	default:
		c.detail = "not found"
	}
	return "", c
}

func addonCheck(d Deps, game string) check {
	c := check{name: "Addon", fix: "run `ompcraft setup` (close the game first; setup waits for it)"}
	toc := filepath.Join(layout.AddonsDir(game), layout.AddonName, layout.AddonName+".toc")
	b, err := os.ReadFile(toc)
	if err != nil {
		c.detail = "not installed in " + layout.AddonsDir(game)
		return c
	}
	have, want := layout.TOCVersion(b), embeddedVersion(d.Addon)
	switch {
	case want != "" && have != want:
		c.detail = fmt.Sprintf("installed %s, but this ompcraft carries %s", orUnknown(have), want)
	default:
		c.ok = true
		c.detail = "installed " + orUnknown(have) + " (matches this ompcraft)"
		if want == "" {
			c.detail = "installed " + orUnknown(have)
		}
	}
	return c
}

func orUnknown(s string) string {
	if s == "" {
		return "(no version)"
	}
	return s
}

func helpersCheck(game string) check {
	c := check{name: "Link helpers", fix: "run `ompcraft setup` (close the game first; setup waits for it)"}
	addons := layout.AddonsDir(game)
	l, err := layout.ReadInstalled(addons)
	if err != nil {
		c.detail = "not installed"
		return c
	}
	slots := 0
	for i := range l.Slots {
		if st, err := os.Stat(l.SlotData(i)); err == nil && !st.IsDir() {
			slots++
		}
	}
	bells := 0
	for i := range l.Signals {
		if _, err := os.Stat(l.Doorbell(i)); err == nil {
			bells++
		}
	}
	_, ctlErr := os.Stat(filepath.Join(l.SignalsDir(), layout.CtlPresent))
	c.detail = fmt.Sprintf("%d/%d slots, %d/%d doorbells present", slots, l.Slots, bells, l.Signals)
	// Doorbells are removed while the game runs, so only slots and the control file are required.
	c.ok = slots == l.Slots && ctlErr == nil
	if ctlErr != nil {
		c.detail += "; ctl_present.wav is missing"
	}
	if n := install.OutdatedHelperTOCs(l); n > 0 {
		c.detail += fmt.Sprintf("; %d helper TOC file(s) outdated", n)
		if c.ok {
			c.fix = "run `ompcraft setup` (it updates them even while the game runs), then restart WoW once to tidy the AddOns list"
		}
		c.ok = false
	}
	return c
}

func pluginCheck(ctx context.Context, t tern, ternOK bool, d Deps) check {
	c := check{name: "Tern plugin", fix: "run `ompcraft setup` to link and reload the plugin"}
	if !ternOK {
		c.detail = "unknown (Tern is not installed)"
		return c
	}
	st, err := t.linkState(ctx)
	if err != nil {
		c.detail = err.Error()
		return c
	}
	status, found, err := t.pluginStatus(ctx)
	if err != nil {
		c.detail = err.Error()
		return c
	}
	switch {
	case st.LinkedTo == "" && !st.Installed:
		c.detail = "not linked"
	case !found:
		c.detail = "linked but Tern does not list it"
	case status != "ready":
		c.detail = "status is " + status + " (run `tern plugin list` for the problem)"
	default:
		c.ok = true
		where := "installed"
		if st.LinkedTo != "" {
			where = "linked from " + st.LinkedTo
		}
		c.detail = where + ", ready"
	}
	return c
}

// DescribeUpdate renders the update state for doctor and `status`.
func DescribeUpdate(u daemon.UpdateInfo, now time.Time) string {
	if u.Channel != "release" {
		return "source build; auto-update is off"
	}
	s := "release build, auto-update " + map[bool]string{true: "on", false: "off"}[u.Autoupdate]
	if u.LastCheck > 0 {
		s += ", last check " + now.Sub(time.UnixMilli(u.LastCheck)).Round(time.Second).String() + " ago"
	} else if u.Autoupdate {
		s += ", first check pending"
	}
	if u.Latest != "" {
		s += ", latest " + u.Latest
	}
	if u.Staged != "" {
		s += "; " + u.Staged + " is staged and installs after you quit WoW"
	}
	if u.LastError != "" {
		s += "; last check failed: " + u.LastError
	}
	return s
}

func updateCheck(u daemon.UpdateInfo) check {
	return check{name: "Updates", ok: u.LastError == "", info: true, detail: DescribeUpdate(u, time.Now())}
}
