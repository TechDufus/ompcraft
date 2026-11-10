// Package config holds the XDG paths and the small JSON files the companion
// persists: config.json, link.json and state.json.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Env abstracts the environment so tests do not touch the real home directory.
type Env struct {
	Home       string
	ConfigHome string // XDG_CONFIG_HOME or ~/.config
	StateHome  string // XDG_STATE_HOME or ~/.local/state
	DataHome   string // XDG_DATA_HOME or ~/.local/share
}

// SystemEnv reads the process environment.
func SystemEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return EnvFrom(home, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("XDG_STATE_HOME")).WithDataHome(os.Getenv("XDG_DATA_HOME")), nil
}

// EnvFrom builds an Env applying the XDG defaults (the data home is
// ~/.local/share; see WithDataHome).
func EnvFrom(home, xdgConfig, xdgState string) Env {
	e := Env{Home: home, ConfigHome: xdgConfig, StateHome: xdgState, DataHome: filepath.Join(home, ".local", "share")}
	if !filepath.IsAbs(e.ConfigHome) {
		e.ConfigHome = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(e.StateHome) {
		e.StateHome = filepath.Join(home, ".local", "state")
	}
	return e
}

func (e Env) ConfigDir() string  { return filepath.Join(e.ConfigHome, "ompcraft") }
func (e Env) StateDir() string   { return filepath.Join(e.StateHome, "ompcraft") }
func (e Env) DataDir() string    { return filepath.Join(e.DataHome, "ompcraft") }
func (e Env) ConfigPath() string { return filepath.Join(e.ConfigDir(), "config.json") }
func (e Env) LinkPath() string   { return filepath.Join(e.ConfigDir(), "link.json") }
func (e Env) StatePath() string  { return filepath.Join(e.StateDir(), "state.json") }
func (e Env) LogPath() string    { return filepath.Join(e.StateDir(), "companion.log") }

// PluginDir is where setup keeps its copy of the Tern plugin.
func (e Env) PluginDir() string { return filepath.Join(e.DataDir(), "tern-plugin") }

// WithDataHome returns e with XDG_DATA_HOME set to dir (ignored unless absolute).
func (e Env) WithDataHome(dir string) Env {
	if filepath.IsAbs(dir) {
		e.DataHome = dir
	}
	return e
}

// Config is config.json:
//
//	{ "game": "/path/to/_forever_", "detail": true, "autoupdate": true }
//
// detail and autoupdate are optional and default to true. detail=false keeps
// replies, steps, previews and last messages out of the files written into the
// game folder. autoupdate=false stops a release build from updating itself.
type Config struct {
	Game   string `json:"game"`
	Detail *bool  `json:"detail,omitempty"`
	// Autoupdate defaults to true; false stops release builds from updating themselves.
	Autoupdate *bool `json:"autoupdate,omitempty"`
}

// DetailEnabled reports the detail option (default true).
func (c Config) DetailEnabled() bool { return c.Detail == nil || *c.Detail }

// SetDetail sets the detail option.
func (c *Config) SetDetail(on bool) { c.Detail = &on }

// AutoupdateEnabled reports the autoupdate option (default true). It only has
// an effect on release builds.
func (c Config) AutoupdateEnabled() bool { return c.Autoupdate == nil || *c.Autoupdate }

// SetAutoupdate sets the autoupdate option.
func (c *Config) SetAutoupdate(on bool) { c.Autoupdate = &on }

// LoadConfig returns the saved config; a missing file yields the zero Config.
func (e Env) LoadConfig() (Config, error) {
	var c Config
	err := readJSON(e.ConfigPath(), &c)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	return c, err
}

// SaveConfig writes config.json.
func (e Env) SaveConfig(c Config) error {
	return writeJSONAtomic(e.ConfigPath(), c, 0o644)
}

// SaveGame stores the chosen game folder, keeping the other options.
func (e Env) SaveGame(game string) error {
	c, err := e.LoadConfig()
	if err != nil {
		return err
	}
	c.Game = game
	return e.SaveConfig(c)
}

// Purge removes config.json, link.json, state.json, the log and the plugin
// copy, then the ompcraft directories if they are empty. It returns what it
// removed.
func (e Env) Purge() ([]string, error) {
	var removed []string
	var firstErr error
	for _, p := range []string{e.ConfigPath(), e.LinkPath(), e.StatePath(), e.LogPath()} {
		switch err := os.Remove(p); {
		case err == nil:
			removed = append(removed, p)
		case !errors.Is(err, fs.ErrNotExist) && firstErr == nil:
			firstErr = err
		}
	}
	if _, err := os.Stat(e.PluginDir()); err == nil {
		if err := os.RemoveAll(e.PluginDir()); err != nil && firstErr == nil {
			firstErr = err
		} else {
			removed = append(removed, e.PluginDir())
		}
	}
	for _, d := range []string{e.ConfigDir(), e.StateDir(), e.DataDir()} {
		_ = os.Remove(d) // only succeeds when empty
	}
	return removed, firstErr
}

// Link is link.json: how the plugin finds the companion.
type Link struct {
	V     int    `json:"v"`
	URL   string `json:"url"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

// NewToken returns 64 random hex characters.
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// WriteLink writes link.json with mode 0600.
func (e Env) WriteLink(l Link) error {
	return writeJSONAtomic(e.LinkPath(), l, 0o600)
}

// ReadLink reads link.json.
func (e Env) ReadLink() (Link, error) {
	var l Link
	err := readJSON(e.LinkPath(), &l)
	return l, err
}

// RemoveLinkIfOurs deletes link.json only when it still carries token, so a
// newer companion's link is never removed.
func (e Env) RemoveLinkIfOurs(token string) error {
	l, err := e.ReadLink()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if l.Token != token {
		return nil
	}
	return os.Remove(e.LinkPath())
}

// State is state.json (PROTOCOL §6.5, §8.9).
type State struct {
	GamePid    int `json:"gamePid"`
	SignalNext int `json:"signalNext"`

	// StagedVersion is the version of the verified binary waiting in
	// <exe dir>/.ompcraft.staged for the game to quit.
	StagedVersion string `json:"stagedVersion,omitempty"`
	// PendingVersion is a just-installed version that has not yet served HTTP
	// for 30 s; StartAttempts counts the starts made with it.
	PendingVersion string `json:"pendingVersion,omitempty"`
	StartAttempts  int    `json:"startAttempts,omitempty"`
	// PreviousVersion is what ran before PendingVersion (the rollback target).
	PreviousVersion string `json:"previousVersion,omitempty"`
	// BadVersion failed to start 3 times and is never applied again.
	BadVersion string `json:"badVersion,omitempty"`
}

// LoadState returns the persisted state; a missing or corrupt file yields zero.
func (e Env) LoadState() State {
	var s State
	if err := readJSON(e.StatePath(), &s); err != nil {
		return State{}
	}
	return s
}

// stateMu serialises read-modify-write of state.json inside this process: the
// daemon saves the game fields while the updater saves the update fields.
var stateMu sync.Mutex

// SaveState writes the game fields (GamePid, SignalNext) of s into state.json
// and keeps every other field of the stored state.
func (e Env) SaveState(s State) error {
	return e.UpdateState(func(cur *State) {
		cur.GamePid, cur.SignalNext = s.GamePid, s.SignalNext
	})
}

// UpdateState applies fn to the stored state and writes it back.
func (e Env) UpdateState(fn func(*State)) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	cur := e.LoadState()
	fn(&cur)
	return writeJSONAtomic(e.StatePath(), cur, 0o644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSONAtomic(path string, v any, mode os.FileMode) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), mode); err != nil {
		return err
	}
	// WriteFile honours mode only on creation; enforce it for a pre-existing tmp.
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
