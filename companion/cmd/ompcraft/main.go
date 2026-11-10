// Command ompcraft is the companion for the ompcraft WoW addon: it installs
// the link addons, serves the Tern plugin over loopback HTTP and publishes
// agent status into the game (see docs/PROTOCOL.md).
package main

import (
	"errors"
	"fmt"
	"os"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.1.0"

// channel is "release" for binaries built by the release workflow (-ldflags
// "-X main.channel=release") and "source" for everything else. Only release
// builds ever replace themselves.
var channel = "source"

const usage = `ompcraft - see your omp coding agents inside World of Warcraft: Forever

Start here:
  ompcraft setup      Install everything and start it. Safe to re-run; it updates in place.
                      [--game DIR] [--yes] [--no-service] [--no-tern]

Day to day:
  ompcraft doctor     Check every piece and say what to fix.
  ompcraft status     Show what the running companion is doing.
  ompcraft uninstall  Remove everything setup added.
                      [--game DIR] [--force] [--purge: also delete config and state]
  ompcraft update     Update to the latest release now (release builds update themselves).
                      [--check] [--force]
  ompcraft config detail|autoupdate [on|off]
                      detail: keep replies, steps and previews out of the game's files.
                      autoupdate: let a release build update itself (default on).

Lower level (setup does these for you):
  ompcraft install    [--game DIR] [--slots 1000] [--signals 2000] [--addon-only] [--force]
                      [--addon-src DIR: development override for the embedded addon]
  ompcraft run        [--game DIR] [--port 47131] [-v]
  ompcraft version
`

// exitError carries a process exit code; its message may be empty.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		code := 1
		var ee *exitError
		if errors.As(err, &ee) {
			code = ee.code
		}
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, "ompcraft:", msg)
		}
		os.Exit(code)
	}
}

func dispatch(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return &exitError{code: 2}
	}
	rest := args[1:]
	switch args[0] {
	case "install":
		return cmdInstall(rest)
	case "uninstall":
		return cmdUninstall(rest)
	case "run":
		return cmdRun(rest)
	case "setup":
		return cmdSetup(rest)
	case "demo": // screenshots: not in --help
		return cmdDemo(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "config":
		return cmdConfig(rest)
	case "update":
		return cmdUpdate(rest)
	case "release-keygen": // maintainer commands: not in --help
		return cmdReleaseKeygen(rest)
	case "release-sign":
		return cmdReleaseSign(rest)
	case "status":
		return cmdStatus(rest)
	case "version", "--version", "-version":
		fmt.Printf("ompcraft %s (%s)\n", version, channel)
		return nil
	case "help", "-h", "--help", "-help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return &exitError{code: 2, err: fmt.Errorf("unknown command %q", args[0])}
}
