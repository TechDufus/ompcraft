package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/TechDufus/ompcraft/companion/internal/config"
)

const configUsage = `usage: ompcraft config detail|autoupdate [on|off]

  detail on       snapshots carry replies, tool steps, previews and last messages (default)
  detail off      snapshots omit them; they never reach files in the game folder
  autoupdate on   a release build checks for new releases and updates itself (default)
  autoupdate off  never update by itself (` + "`ompcraft update`" + ` still works)

Settings live in config.json and are read when the companion starts
(re-run "ompcraft setup", or restart the companion, to apply a change).
`

// saveGame stores the chosen game folder in config.json, keeping other options.
func saveGame(env config.Env, game string) error { return env.SaveGame(game) }

func cmdConfig(args []string) error {
	if len(args) == 0 || len(args) > 2 || (args[0] != "detail" && args[0] != "autoupdate") {
		return &exitError{code: 2, err: errors.New(strings.TrimSpace(configUsage))}
	}
	key := args[0]
	env, err := config.SystemEnv()
	if err != nil {
		return err
	}
	cfg, err := env.LoadConfig()
	if err != nil {
		return err
	}
	current := cfg.DetailEnabled
	if key == "autoupdate" {
		current = cfg.AutoupdateEnabled
	}
	if len(args) == 1 {
		fmt.Printf("%s: %s\n", key, onOff(current()))
		return nil
	}
	var on bool
	switch strings.ToLower(args[1]) {
	case "on", "true", "yes", "1":
		on = true
	case "off", "false", "no", "0":
		on = false
	default:
		return &exitError{code: 2, err: fmt.Errorf("%s must be on or off, got %q", key, args[1])}
	}
	if key == "autoupdate" {
		cfg.SetAutoupdate(on)
	} else {
		cfg.SetDetail(on)
	}
	if err := env.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("%s: %s (written to %s; re-run `ompcraft setup` or restart the companion to apply)\n", key, onOff(on), env.ConfigPath())
	return nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
