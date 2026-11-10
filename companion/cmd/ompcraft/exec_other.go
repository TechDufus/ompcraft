//go:build !unix

package main

import "errors"

func execSelf(path string, argv, env []string) error {
	return errors.New("re-executing is not supported on this OS yet")
}
