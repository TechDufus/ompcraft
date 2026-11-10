package update

import (
	"path/filepath"
	"testing"

	"github.com/TechDufus/ompcraft/companion/internal/config"
)

type (
	configEnv = config.Env
	State     = config.State
)

// testConfigEnv is an Env rooted in a temp dir, so tests never touch the real
// config or state.
func testConfigEnv(t *testing.T) config.Env {
	t.Helper()
	home := t.TempDir()
	return config.EnvFrom(home, filepath.Join(home, "cfg"), filepath.Join(home, "state")).WithDataHome(filepath.Join(home, "data"))
}
