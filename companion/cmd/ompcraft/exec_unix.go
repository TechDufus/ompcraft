//go:build unix

package main

import "syscall"

// execSelf replaces the current process image. The pid stays the same, so
// systemd and launchd do not notice.
func execSelf(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}
