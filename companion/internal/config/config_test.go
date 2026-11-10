package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestEnvDefaults(t *testing.T) {
	e := EnvFrom("/home/user", "", "")
	if e.ConfigPath() != "/home/user/.config/ompcraft/config.json" ||
		e.LinkPath() != "/home/user/.config/ompcraft/link.json" ||
		e.StatePath() != "/home/user/.local/state/ompcraft/state.json" ||
		e.LogPath() != "/home/user/.local/state/ompcraft/companion.log" {
		t.Fatalf("defaults: %+v", e)
	}
	e = EnvFrom("/home/user", "/xdg/c", "/xdg/s")
	if e.ConfigDir() != "/xdg/c/ompcraft" || e.StateDir() != "/xdg/s/ompcraft" {
		t.Fatalf("XDG: %+v", e)
	}
	// Relative XDG values are invalid per the spec and ignored.
	if e = EnvFrom("/home/user", "rel", "rel"); e.ConfigHome != "/home/user/.config" {
		t.Fatalf("relative XDG accepted: %+v", e)
	}
}

func TestConfigAndState(t *testing.T) {
	root := t.TempDir()
	e := EnvFrom(root, filepath.Join(root, "c"), filepath.Join(root, "s"))
	if c, err := e.LoadConfig(); err != nil || c.Game != "" {
		t.Fatalf("missing config: %+v %v", c, err)
	}
	if err := e.SaveConfig(Config{Game: "/x/_forever_"}); err != nil {
		t.Fatal(err)
	}
	if c, err := e.LoadConfig(); err != nil || c.Game != "/x/_forever_" {
		t.Fatalf("config: %+v %v", c, err)
	}
	if s := e.LoadState(); s != (State{}) {
		t.Fatalf("missing state: %+v", s)
	}
	if err := e.SaveState(State{GamePid: 42, SignalNext: 9}); err != nil {
		t.Fatal(err)
	}
	if s := e.LoadState(); s != (State{GamePid: 42, SignalNext: 9}) {
		t.Fatalf("state: %+v", s)
	}
	_ = os.WriteFile(e.StatePath(), []byte("{garbage"), 0o644)
	if s := e.LoadState(); s != (State{}) {
		t.Fatalf("corrupt state must read as zero: %+v", s)
	}
}

func TestLink(t *testing.T) {
	root := t.TempDir()
	e := EnvFrom(root, filepath.Join(root, "c"), filepath.Join(root, "s"))
	tok, err := NewToken()
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(tok) {
		t.Fatalf("token %q %v", tok, err)
	}
	tok2, _ := NewToken()
	if tok == tok2 {
		t.Fatal("tokens must differ")
	}
	l := Link{V: 0, URL: "http://127.0.0.1:47131", Token: tok, PID: 123}
	if err := e.WriteLink(l); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(e.LinkPath())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("link.json mode = %v, %v", st.Mode().Perm(), err)
	}
	// Rewriting over an existing, wider-mode file must still end up 0600.
	_ = os.Chmod(e.LinkPath(), 0o644)
	_ = os.WriteFile(e.LinkPath()+".tmp", nil, 0o644)
	if err := e.WriteLink(l); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(e.LinkPath()); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode after rewrite = %v", st.Mode().Perm())
	}
	if got, err := e.ReadLink(); err != nil || got != l {
		t.Fatalf("read: %+v %v", got, err)
	}

	// Another companion's link is not ours to delete.
	if err := e.RemoveLinkIfOurs("other"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.LinkPath()); err != nil {
		t.Fatal("link.json removed although the token differs")
	}
	if err := e.RemoveLinkIfOurs(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.LinkPath()); !os.IsNotExist(err) {
		t.Fatal("link.json still present")
	}
	if err := e.RemoveLinkIfOurs(tok); err != nil {
		t.Fatalf("removing a missing link must not fail: %v", err)
	}
}

func TestConfigDetailOption(t *testing.T) {
	root := t.TempDir()
	e := EnvFrom(root, filepath.Join(root, "c"), filepath.Join(root, "s"))

	// Default: on, and not written to the file.
	if !(Config{Game: "/g"}).DetailEnabled() {
		t.Fatal("detail must default to on")
	}
	if err := e.SaveConfig(Config{Game: "/g"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(e.ConfigPath())
	if strings.Contains(string(raw), "detail") {
		t.Fatalf("unset option must not be written: %s", raw)
	}

	// Off round-trips with the exact JSON shape.
	c, _ := e.LoadConfig()
	c.SetDetail(false)
	if err := e.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(e.ConfigPath())
	var shape map[string]any
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shape, map[string]any{"game": "/g", "detail": false}) {
		t.Fatalf("config.json = %s", raw)
	}
	if got, _ := e.LoadConfig(); got.DetailEnabled() || got.Game != "/g" {
		t.Fatalf("loaded %+v", got)
	}

	// An old config.json without the key still loads as on; explicit true is on.
	_ = os.WriteFile(e.ConfigPath(), []byte(`{"game":"/g"}`), 0o644)
	if got, _ := e.LoadConfig(); !got.DetailEnabled() {
		t.Fatal("missing key must mean on")
	}
	_ = os.WriteFile(e.ConfigPath(), []byte(`{"game":"/g","detail":true}`), 0o644)
	if got, _ := e.LoadConfig(); !got.DetailEnabled() {
		t.Fatal("explicit true must mean on")
	}
}

func TestSaveStateKeepsTheUpdateFields(t *testing.T) {
	root := t.TempDir()
	e := EnvFrom(root, filepath.Join(root, "c"), filepath.Join(root, "s"))
	if err := e.UpdateState(func(s *State) {
		s.PendingVersion, s.StartAttempts, s.PreviousVersion, s.StagedVersion, s.BadVersion = "0.2.0", 2, "0.1.0", "0.3.0", "0.0.9"
	}); err != nil {
		t.Fatal(err)
	}
	// The daemon saves its game fields: the updater's must survive.
	if err := e.SaveState(State{GamePid: 42, SignalNext: 9}); err != nil {
		t.Fatal(err)
	}
	want := State{GamePid: 42, SignalNext: 9, PendingVersion: "0.2.0", StartAttempts: 2, PreviousVersion: "0.1.0", StagedVersion: "0.3.0", BadVersion: "0.0.9"}
	if got := e.LoadState(); got != want {
		t.Fatalf("state = %+v, want %+v", got, want)
	}
	// And the other way round.
	if err := e.UpdateState(func(s *State) { s.PendingVersion = "" }); err != nil {
		t.Fatal(err)
	}
	if got := e.LoadState(); got.GamePid != 42 || got.SignalNext != 9 || got.PendingVersion != "" || got.BadVersion != "0.0.9" {
		t.Fatalf("state = %+v", got)
	}
	// Unused fields are not written at all.
	_ = e.SaveState(State{GamePid: 1, SignalNext: 2})
	_ = e.UpdateState(func(s *State) { *s = State{GamePid: 1, SignalNext: 2} })
	raw, _ := os.ReadFile(e.StatePath())
	if strings.Contains(string(raw), "Version") {
		t.Errorf("empty update fields leaked into state.json: %s", raw)
	}
}
