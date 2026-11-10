//go:build linux

package publish

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

type watchKind int

const (
	wkAddons watchKind = iota // A itself, to notice A/Ompcraft being replaced
	wkAddon                   // A/Ompcraft or any directory below it
	wkSlot                    // A/OmpcraftSnnn
)

type target struct {
	kind watchKind
	slot int    // wkSlot
	dir  string // wkAddon: path relative to A/Ompcraft ("" for the root)
}

const addonMask = syscall.IN_OPEN | syscall.IN_CREATE | syscall.IN_MOVED_TO

// Watch watches IN_OPEN on every directory under A/Ompcraft (recursively; new
// subdirectories are picked up as they appear) and on every slot directory
// (PROTOCOL §6.3), and calls emit for each event until ctx ends. emit is
// called from one goroutine only. ready (optional) is called once the watches
// are in place.
func Watch(ctx context.Context, l layout.Layout, ready func(), emit func(Event), logf func(string, ...any)) error {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return fmt.Errorf("inotify_init1: %w", err)
	}
	// A non-blocking fd goes through the runtime poller, so Close unblocks Read.
	f := os.NewFile(uintptr(fd), "inotify")
	defer f.Close()

	wds := map[int32]target{}
	add := func(dir string, mask uint32, t target) error {
		wd, err := syscall.InotifyAddWatch(fd, dir, mask)
		if err != nil {
			return fmt.Errorf("watch %s: %w", dir, err)
		}
		wds[int32(wd)] = t
		return nil
	}
	// addTree watches rel (relative to A/Ompcraft) and every directory below it.
	addTree := func(rel string) error {
		root := filepath.Join(l.AddonDir(), rel)
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == root {
					return err
				}
				return nil // vanished while walking
			}
			if !d.IsDir() {
				return nil
			}
			r, err := filepath.Rel(l.AddonDir(), p)
			if err != nil {
				return err
			}
			if r == "." {
				r = ""
			}
			if err := add(p, addonMask, target{kind: wkAddon, dir: r}); err != nil {
				if errors.Is(err, syscall.ENOENT) {
					return nil
				}
				return err
			}
			return nil
		})
	}

	if err := add(l.Addons, syscall.IN_MOVED_TO|syscall.IN_CREATE, target{kind: wkAddons}); err != nil {
		return err
	}
	if err := addTree(""); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		logf("watch: %s does not exist yet; waiting for it", l.AddonDir())
	}
	for i := range l.Slots {
		if err := add(l.SlotDir(i), syscall.IN_OPEN, target{kind: wkSlot, slot: i}); err != nil {
			return err
		}
	}

	if ready != nil {
		ready()
	}
	go func() {
		<-ctx.Done()
		f.Close()
	}()

	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, os.ErrClosed) {
				return nil
			}
			return fmt.Errorf("inotify read: %w", err)
		}
		now := time.Now()
		for off := 0; off+syscall.SizeofInotifyEvent <= n; {
			wd := int32(binary.NativeEndian.Uint32(buf[off:]))
			mask := binary.NativeEndian.Uint32(buf[off+4:])
			nameLen := int(binary.NativeEndian.Uint32(buf[off+12:]))
			start := off + syscall.SizeofInotifyEvent
			end := min(start+nameLen, n)
			name := strings.TrimRight(string(buf[start:end]), "\x00")
			off = start + nameLen

			if mask&syscall.IN_Q_OVERFLOW != 0 {
				emit(Event{Kind: KindOverflow, At: now})
				continue
			}
			if mask&syscall.IN_IGNORED != 0 {
				delete(wds, wd)
				continue
			}
			t, ok := wds[wd]
			if !ok {
				continue
			}
			isDir := mask&syscall.IN_ISDIR != 0
			switch t.kind {
			case wkAddons:
				// The Ompcraft directory itself was moved in or created.
				if isDir && name == layout.AddonName {
					if err := addTree(""); err != nil {
						logf("watch: re-adding %s: %v", l.AddonDir(), err)
						continue
					}
					emit(Event{Kind: KindAddonDirReplaced, Name: name, At: now})
				}
			case wkAddon:
				switch {
				case isDir && mask&(syscall.IN_CREATE|syscall.IN_MOVED_TO) != 0:
					if err := addTree(filepath.Join(t.dir, name)); err != nil {
						logf("watch: new directory %s: %v", filepath.Join(layout.AddonName, t.dir, name), err)
					}
				case !isDir && mask&syscall.IN_OPEN != 0 && strings.HasSuffix(name, ".lua"):
					emit(Event{Kind: KindLuaOpen, Name: filepath.Join(layout.AddonName, t.dir, name), At: now})
				}
			case wkSlot:
				if !isDir && mask&syscall.IN_OPEN != 0 && name == layout.DataFile {
					emit(Event{Kind: KindSlotOpen, Slot: t.slot, Name: filepath.Join(layout.SlotName(t.slot), name), At: now})
				}
			}
		}
	}
}
