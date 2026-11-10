//go:build !linux

package gamedir

import "fmt"

// FindGame is not implemented off Linux yet.
func FindGame() (int, error) {
	return 0, fmt.Errorf("game process detection: %w", ErrUnsupported)
}
