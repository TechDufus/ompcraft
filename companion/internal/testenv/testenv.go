// Package testenv builds throwaway game folders with the link installed, for
// tests. Everything lives under t.TempDir() and uses invented content.
package testenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TechDufus/ompcraft/companion/internal/install"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

// Install installs a fake addon plus slots and doorbells into a temp game
// folder and returns the installed layout.
func Install(t *testing.T, slots, signals int) layout.Layout {
	t.Helper()
	root := t.TempDir()
	game := filepath.Join(root, "World of Warcraft", "_forever_")
	src := filepath.Join(root, "src", "Ompcraft")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(game, 0o755); err != nil {
		t.Fatal(err)
	}
	// The real addon keeps its .lua files in subfolders.
	for name, content := range map[string]string{
		"Ompcraft.toc":  "## Interface: 16001\nCore/Init.lua\nUI/Panel.lua\n",
		"Core/Init.lua": "-- demo\n",
		"Core/Util.lua": "-- demo\n",
		"UI/Panel.lua":  "-- demo\n",
		"UI/Deep/A.lua": "-- demo\n",
	} {
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := install.Install(install.Options{
		Game: game, AddonSrc: src, Slots: slots, Signals: signals,
		GameRunning: func() (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	l, err := layout.ReadInstalled(layout.AddonsDir(game))
	if err != nil {
		t.Fatal(err)
	}
	return l
}
