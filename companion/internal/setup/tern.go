package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const pluginID = "ompcraft"

// tern wraps the `tern plugin` commands.
type tern struct {
	r Runner
}

func (t tern) available() bool {
	_, err := t.r.LookPath("tern")
	return err == nil
}

func (t tern) run(ctx context.Context, args ...string) (string, error) {
	return t.r.Run(ctx, "tern", args...)
}

// pluginDir asks tern where plugins live (`tern plugin dir --json`).
func (t tern) pluginDir(ctx context.Context) (string, error) {
	out, err := t.run(ctx, "plugin", "dir", "--json")
	if err != nil {
		return "", err
	}
	var v struct {
		Dir string `json:"dir"`
	}
	if json.Unmarshal([]byte(out), &v) == nil && v.Dir != "" {
		return v.Dir, nil
	}
	if dir := strings.TrimSpace(out); dir != "" && !strings.HasPrefix(dir, "{") {
		return dir, nil
	}
	return "", errors.New("tern did not say where its plugins live")
}

// linkState describes how the ompcraft plugin is installed in tern.
type linkState struct {
	// LinkedTo is the folder the plugin is linked from ("" when not linked).
	LinkedTo string
	// Installed: a plain copy sits in the plugin directory (not a link).
	Installed bool
}

func (t tern) linkState(ctx context.Context) (linkState, error) {
	dir, err := t.pluginDir(ctx)
	if err != nil {
		return linkState{}, err
	}
	var s linkState
	if b, err := os.ReadFile(filepath.Join(dir, pluginID+".path")); err == nil {
		s.LinkedTo = filepath.Clean(strings.TrimSpace(string(b)))
	} else if st, err := os.Stat(filepath.Join(dir, pluginID)); err == nil && st.IsDir() {
		s.Installed = true
	}
	return s, nil
}

// pluginStatus returns the status tern reports for the ompcraft plugin
// ("ready", ...); found is false when tern does not list it.
func (t tern) pluginStatus(ctx context.Context) (status string, found bool, err error) {
	out, err := t.run(ctx, "plugin", "list", "--json")
	if err != nil {
		return "", false, err
	}
	var v struct {
		Plugins []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", false, fmt.Errorf("cannot read `tern plugin list --json`: %w", err)
	}
	for _, p := range v.Plugins {
		if p.ID == pluginID {
			return p.Status, true, nil
		}
	}
	return "", false, nil
}

// link makes tern use dir as the ompcraft plugin and reloads it. replaced says
// what it removed first, for the status line.
func (t tern) link(ctx context.Context, dir string) (replaced string, err error) {
	if st, serr := t.linkState(ctx); serr == nil {
		switch {
		case st.LinkedTo != "" && st.LinkedTo != filepath.Clean(dir):
			if _, err := t.run(ctx, "plugin", "unlink", pluginID); err != nil {
				return "", err
			}
			replaced = "the link to " + st.LinkedTo
		case st.Installed:
			if _, err := t.run(ctx, "plugin", "remove", pluginID); err != nil {
				return "", err
			}
			replaced = "an installed copy"
		}
		if st.LinkedTo == filepath.Clean(dir) {
			// Already linked to this very folder: only the reload is needed.
			_, err := t.run(ctx, "plugin", "reload", pluginID)
			return "", err
		}
	}
	if _, err := t.run(ctx, "plugin", "link", dir); err != nil {
		return replaced, err
	}
	_, err = t.run(ctx, "plugin", "reload", pluginID)
	return replaced, err
}

// unlink removes the ompcraft plugin link (or installed copy). It reports
// whether there was anything to remove.
func (t tern) unlink(ctx context.Context) (what string, err error) {
	st, err := t.linkState(ctx)
	if err != nil {
		return "", err
	}
	switch {
	case st.LinkedTo != "":
		_, err = t.run(ctx, "plugin", "unlink", pluginID)
		return "tern plugin link (" + st.LinkedTo + ")", err
	case st.Installed:
		_, err = t.run(ctx, "plugin", "remove", pluginID)
		return "tern plugin copy", err
	}
	return "", nil
}
