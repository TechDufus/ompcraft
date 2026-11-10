//go:build !linux

package publish

import (
	"context"
	"fmt"

	"github.com/TechDufus/ompcraft/companion/internal/gamedir"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// Watch is not implemented off Linux yet.
func Watch(ctx context.Context, l layout.Layout, ready func(), emit func(Event), logf func(string, ...any)) error {
	return fmt.Errorf("file watching: %w", gamedir.ErrUnsupported)
}
