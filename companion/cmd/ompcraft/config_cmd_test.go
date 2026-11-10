package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/TechDufus/ompcraft/companion/internal/config"
)

func isolatedEnv(t *testing.T) config.Env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	env, err := config.SystemEnv()
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func readShape(t *testing.T, env config.Env) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(env.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestConfigDetailCommand(t *testing.T) {
	env := isolatedEnv(t)
	if err := saveGame(env, "/x/_forever_"); err != nil {
		t.Fatal(err)
	}
	if err := dispatch([]string{"config", "detail", "off"}); err != nil {
		t.Fatal(err)
	}
	if got := readShape(t, env); !reflect.DeepEqual(got, map[string]any{"game": "/x/_forever_", "detail": false}) {
		t.Fatalf("config.json = %v", got)
	}
	cfg, _ := env.LoadConfig()
	if cfg.DetailEnabled() {
		t.Fatal("detail should be off")
	}

	// Re-running install keeps the option (saveGame edits only the game).
	if err := saveGame(env, "/y/_forever_"); err != nil {
		t.Fatal(err)
	}
	if got := readShape(t, env); !reflect.DeepEqual(got, map[string]any{"game": "/y/_forever_", "detail": false}) {
		t.Fatalf("config.json after saveGame = %v", got)
	}

	if err := dispatch([]string{"config", "detail", "on"}); err != nil {
		t.Fatal(err)
	}
	if got := readShape(t, env); !reflect.DeepEqual(got, map[string]any{"game": "/y/_forever_", "detail": true}) {
		t.Fatalf("config.json = %v", got)
	}
	if err := dispatch([]string{"config", "detail"}); err != nil { // prints the current value
		t.Fatal(err)
	}

	for _, bad := range [][]string{{"config"}, {"config", "color", "on"}, {"config", "detail", "maybe"}, {"config", "detail", "on", "x"}} {
		err := dispatch(bad)
		var ee *exitError
		if !errors.As(err, &ee) || ee.code != 2 {
			t.Errorf("dispatch(%v) = %v, want exit code 2", bad, err)
		}
	}
	if got := readShape(t, env); got["detail"] != true {
		t.Fatalf("a rejected command changed the config: %v", got)
	}
}

func TestConfigDetailCreatesConfigWhenMissing(t *testing.T) {
	env := isolatedEnv(t)
	if err := dispatch([]string{"config", "detail", "off"}); err != nil {
		t.Fatal(err)
	}
	if got := readShape(t, env); !reflect.DeepEqual(got, map[string]any{"game": "", "detail": false}) {
		t.Fatalf("config.json = %v", got)
	}
}
