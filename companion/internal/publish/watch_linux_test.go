//go:build linux

package publish

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/model"
)

type collector struct {
	ch chan Event
}

func startWatch(t *testing.T, l layout.Layout) *collector {
	t.Helper()
	c := &collector{ch: make(chan Event, 1024)}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, l, func() { close(ready) }, func(e Event) { c.ch <- e }, t.Logf)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("watch failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("watch not ready")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("watch returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("watch did not stop after cancel")
		}
	})
	return c
}

func (c *collector) next(t *testing.T) Event {
	t.Helper()
	select {
	case e := <-c.ch:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

func (c *collector) expectNone(t *testing.T) {
	t.Helper()
	select {
	case e := <-c.ch:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(300 * time.Millisecond):
	}
}

func openClose(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestWatchSlotAndAddonOpens(t *testing.T) {
	l := setup(t, 6, 4)
	c := startWatch(t, l)

	before := time.Now()
	openClose(t, l.SlotData(3))
	e := c.next(t)
	if e.Kind != KindSlotOpen || e.Slot != 3 || e.Name != filepath.Join("OmpcraftS003", "data.lua") {
		t.Fatalf("event = %+v", e)
	}
	if e.At.Before(before) || e.At.After(time.Now()) {
		t.Fatalf("event time %v outside [%v, now]", e.At, before)
	}

	// The addon keeps its .lua files in subfolders, at any depth.
	for _, rel := range []string{"Core/Init.lua", "UI/Panel.lua", "UI/Deep/A.lua"} {
		openClose(t, filepath.Join(l.AddonDir(), filepath.FromSlash(rel)))
		if e := c.next(t); e.Kind != KindLuaOpen || e.Name != filepath.Join("Ompcraft", filepath.FromSlash(rel)) {
			t.Fatalf("%s: event = %+v", rel, e)
		}
	}

	// Opens the client does not make for data: toc files, directories, other names.
	openClose(t, filepath.Join(l.AddonDir(), "Ompcraft.toc"))
	openClose(t, filepath.Join(l.SlotDir(2), layout.SlotName(2)+".toc"))
	openClose(t, l.SlotDir(2))
	openClose(t, l.AddonDir())
	c.expectNone(t)
}

func TestWatchIgnoresCompanionWrites(t *testing.T) {
	l := setup(t, 4, 4)
	c := startWatch(t, l)
	p := NewPublisher(l, nil)
	p.MarkLoaded(0)
	for i := range 3 {
		if err := p.Publish(&model.Snapshot{Gen: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	// Atomic tmp+rename writes must not look like the client opening data.lua.
	c.expectNone(t)

	// A slot loaded afterwards is still seen.
	openClose(t, l.SlotData(0))
	if e := c.next(t); e.Kind != KindSlotOpen || e.Slot != 0 {
		t.Fatalf("event = %+v", e)
	}
}

func TestWatchAddonDirReplaced(t *testing.T) {
	l := setup(t, 2, 2)
	c := startWatch(t, l)

	// Addon-only install: build a new folder next to it, swap it in.
	tmp := filepath.Join(l.Addons, layout.AddonTmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "Ompcraft.toc"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "Sub", "Deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "Sub", "Deeper", "New.lua"), []byte("-- new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "New.lua"), []byte("-- new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(l.AddonDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, l.AddonDir()); err != nil {
		t.Fatal(err)
	}
	if e := c.next(t); e.Kind != KindAddonDirReplaced {
		t.Fatalf("event = %+v", e)
	}
	openClose(t, filepath.Join(l.AddonDir(), "Sub", "Deeper", "New.lua"))
	if e := c.next(t); e.Kind != KindLuaOpen || e.Name != filepath.Join("Ompcraft", "Sub", "Deeper", "New.lua") {
		t.Fatalf("watch not re-established on the new folder tree: %+v", e)
	}
}

func TestWatchSubdirectoryCreatedLater(t *testing.T) {
	l := setup(t, 2, 2)
	c := startWatch(t, l)

	dir := filepath.Join(l.AddonDir(), "Modules", "Late")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Give the watcher a moment to add watches to the new directories.
	time.Sleep(300 * time.Millisecond)
	file := filepath.Join(dir, "Mod.lua")
	if err := os.WriteFile(file, []byte("-- mod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e := c.next(t); e.Kind != KindLuaOpen || e.Name != filepath.Join("Ompcraft", "Modules", "Late", "Mod.lua") {
		t.Fatalf("event = %+v", e)
	}
	openClose(t, file)
	if e := c.next(t); e.Kind != KindLuaOpen {
		t.Fatalf("event = %+v", e)
	}
	// Non-.lua files in subdirectories stay quiet.
	openClose(t, filepath.Join(l.AddonDir(), "Core", "Init.lua")) // existing dir, still watched
	if e := c.next(t); e.Kind != KindLuaOpen {
		t.Fatalf("event = %+v", e)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.expectNone(t)
}
