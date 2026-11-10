// Package ompcraft carries the files the companion installs, embedded in the
// binary so `go install` gives a self-contained tool.
package ompcraft

import (
	"embed"
	"io/fs"
)

//go:embed addon/Ompcraft tern-plugin
var assets embed.FS

// Addon holds the WoW addon, rooted at addon/Ompcraft (Ompcraft.toc is at the
// top level).
var Addon = mustSub("addon/Ompcraft")

// TernPlugin holds the Tern window plugin, rooted at tern-plugin
// (plugin.toml is at the top level).
var TernPlugin = mustSub("tern-plugin")

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(assets, dir)
	if err != nil {
		panic("ompcraft: embedded " + dir + ": " + err.Error())
	}
	return sub
}
