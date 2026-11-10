//go:build linux

package gamedir

import "os"

// FindGame returns the game's pid, or 0 when it is not running.
func FindGame() (int, error) {
	return ScanProc("/proc", os.Getpid())
}
