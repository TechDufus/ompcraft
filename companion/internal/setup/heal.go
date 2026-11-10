package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TechDufus/ompcraft/companion/internal/install"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// HealResult says what SelfHeal changed.
type HealResult struct {
	// AddonReplaced: A/Ompcraft differed from the embedded addon and was replaced.
	AddonReplaced bool
	// PluginRefreshed: the data-dir Tern plugin copy was rewritten (and reloaded).
	PluginRefreshed bool
	// Notes are log lines describing what happened.
	Notes []string
	// HelpersRewritten counts outdated helper TOC files rewritten in place.
	HelpersRewritten int
}

// Changed reports whether anything was updated.
func (r HealResult) Changed() bool { return r.AddonReplaced || r.PluginRefreshed }

// SelfHeal brings the installed addon and the Tern plugin copy in line with
// the files embedded in this binary, whichever way the binary got here
// (auto-update, manual update, go install, install.sh). It is safe while the
// game runs: the addon is replaced atomically, as `install --addon-only` does.
//
//   - Addon: if A/Ompcraft exists and its content hash differs from the
//     embedded addon, replace it.
//   - Plugin: if the data-dir copy exists, differs and tern uses it (or tern
//     does not say otherwise), rewrite it and `tern plugin reload ompcraft`. A
//     plugin linked from somewhere else (a dev checkout) is left alone.
func SelfHeal(ctx context.Context, d Deps, game string) (HealResult, error) {
	var res HealResult
	var errs []error

	if game != "" {
		addons := layout.AddonsDir(game)
		// Helper TOCs written by an older release: rewrite in place (harmless
		// while the game runs; the client reads TOC metadata at launch).
		if l, err := layout.ReadInstalled(addons); err == nil {
			switch n, err := install.RefreshHelperTOCs(l); {
			case err != nil:
				errs = append(errs, fmt.Errorf("updating helper TOCs: %w", err))
			case n > 0:
				res.HelpersRewritten = n
				res.Notes = append(res.Notes, fmt.Sprintf("self-heal: rewrote %d outdated helper TOC file(s); restart WoW once to tidy the AddOns list", n))
			}
		}
		installed := filepath.Join(addons, layout.AddonName)
		if st, err := os.Stat(installed); err == nil && st.IsDir() {
			have, err1 := install.HashTree(os.DirFS(installed))
			want, err2 := install.HashTree(d.Addon)
			switch {
			case err1 != nil || err2 != nil:
				errs = append(errs, fmt.Errorf("comparing the addon: %w", errors.Join(err1, err2)))
			case have != want:
				if err := install.ReplaceAddon(addons, d.Addon); err != nil {
					errs = append(errs, fmt.Errorf("replacing the addon: %w", err))
				} else {
					res.AddonReplaced = true
					res.Notes = append(res.Notes, "self-heal: the installed addon differed from this build's; replaced "+installed)
				}
			}
		}
	}

	if _, err := os.Stat(d.Env.PluginDir()); err == nil {
		t := tern{d.Runner}
		skip := false
		if t.available() {
			if st, err := t.linkState(ctx); err == nil && st.LinkedTo != "" && st.LinkedTo != filepath.Clean(d.Env.PluginDir()) {
				skip = true
				res.Notes = append(res.Notes, "self-heal: the Tern plugin is linked from "+st.LinkedTo+"; leaving it alone")
			}
		}
		if !skip {
			changed, err := install.WriteTree(d.Plugin, d.Env.PluginDir())
			switch {
			case err != nil:
				errs = append(errs, fmt.Errorf("refreshing the Tern plugin copy: %w", err))
			case changed:
				res.PluginRefreshed = true
				note := "self-heal: refreshed the Tern plugin copy in " + d.Env.PluginDir()
				if t.available() {
					if _, err := t.run(ctx, "plugin", "reload", pluginID); err != nil {
						errs = append(errs, fmt.Errorf("reloading the Tern plugin: %w", err))
					} else {
						note += " and reloaded it"
					}
				}
				res.Notes = append(res.Notes, note)
			}
		}
	}
	return res, errors.Join(errs...)
}
