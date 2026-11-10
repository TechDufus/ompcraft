// Package publish writes snapshots into the slot addons, rings doorbells,
// tracks which slots the client has loaded and watches the game with inotify
// (PROTOCOL §6).
package publish

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/model"
)

// Stats are the publisher's counters.
type Stats struct {
	Publishes  int64 `json:"publishes"`
	SlotWrites int64 `json:"slotWrites"`
	Doorbells  int64 `json:"doorbells"`
	WriteErrs  int64 `json:"writeErrors"`
	Skipped    int64 `json:"skippedIdle"` // publishes skipped while no game/UI session
	Wipes      int64 `json:"slotWipes"`   // times every slot was reset to the empty form
}

// WriteWindow is how many of the lowest-index non-loaded slots each publish
// writes once a slot open has been seen in the current UI session. Until then,
// every non-loaded slot is written.
const WriteWindow = 8

// Publisher owns the slot/doorbell files and the loaded-slot set.
type Publisher struct {
	L    layout.Layout
	Logf func(format string, args ...any)

	mu         sync.Mutex
	loaded     []bool
	signalNext int
	wav        []byte
	stats      Stats
	exhausted  bool // "signals exhausted" already logged
	// slotOpenSeen: a slot open was observed in the current UI session.
	slotOpenSeen bool
	idle         bool              // see SetIdle
	onWrite      func(path string) // test hook, called after each completed file write
}

func NewPublisher(l layout.Layout, logf func(string, ...any)) *Publisher {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Publisher{L: l, Logf: logf, loaded: make([]bool, l.Slots), wav: layout.SilentWAV()}
}

// SetIdle turns file writes off (true) or on (false). While idle, Publish
// stamps the snapshot but writes no slot and rings no doorbell: there is no
// client to read them.
func (p *Publisher) SetIdle(idle bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idle = idle
}

// Publish implements PROTOCOL §6.2: it stamps snap.NextSignal, writes data.lua
// atomically into every slot not marked loaded, then rings doorbell signalNext
// and increments it. Write failures are logged and counted, and reported in
// the returned error after the whole procedure ran.
func (p *Publisher) Publish(snap *model.Snapshot) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	snap.NextSignal = p.signalNext + 1
	data, err := snap.Lua()
	if err != nil {
		return err
	}
	if p.idle {
		p.stats.Skipped++
		return nil
	}
	var errs []error
	written := 0
	for i := range p.L.Slots {
		if p.loaded[i] {
			continue
		}
		// Once the client has loaded a slot this UI session, it always loads the
		// lowest unloaded index next, so only the first WriteWindow need fresh data.
		if p.slotOpenSeen && written >= WriteWindow {
			break
		}
		written++
		if err := p.writeAtomic(p.L.SlotDir(i), layout.DataTmp, layout.DataFile, data); err != nil {
			p.stats.WriteErrs++
			errs = append(errs, err)
			continue
		}
		p.stats.SlotWrites++
	}
	if p.signalNext < p.L.Signals {
		if err := p.ring(p.signalNext); err != nil {
			p.stats.WriteErrs++
			errs = append(errs, err)
		} else {
			p.stats.Doorbells++
		}
		p.signalNext++
	} else if !p.exhausted {
		p.exhausted = true
		p.Logf("all %d doorbells used; no longer ringing (addon falls back to its timer)", p.L.Signals)
	}
	p.stats.Publishes++
	if len(errs) > 0 {
		p.Logf("publish: %d write error(s), first: %v", len(errs), errs[0])
	}
	return errors.Join(errs...)
}

func (p *Publisher) writeAtomic(dir, tmpName, name string, data []byte) error {
	tmp := filepath.Join(dir, tmpName)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	dst := filepath.Join(dir, name)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if p.onWrite != nil {
		p.onWrite(dst)
	}
	return nil
}

func (p *Publisher) ring(i int) error {
	name := layout.DoorbellName(i)
	return p.writeAtomic(p.L.SignalsDir(), "."+name+".tmp", name, p.wav)
}

// SignalNext returns the next doorbell to ring.
func (p *Publisher) SignalNext() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.signalNext
}

// SetSignalNext sets the next doorbell to ring.
func (p *Publisher) SetSignalNext(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signalNext = n
	p.exhausted = false
}

// MarkLoaded records that the client opened slot i's data.lua. The client
// always loads the lowest unloaded slot, so every slot below i was loaded this
// UI session too. Marking them matters after a companion restart in the middle
// of a session: the loaded set starts empty, and without this the write window
// would keep refreshing slots the client already used while the client loads
// stale ones above them.
func (p *Publisher) MarkLoaded(i int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < 0 || i >= len(p.loaded) {
		return false
	}
	for j := 0; j <= i; j++ {
		p.loaded[j] = true
	}
	p.slotOpenSeen = true
	return true
}

// ClearLoaded forgets all loaded slots (a new UI session started).
func (p *Publisher) ClearLoaded() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.loaded)
	p.slotOpenSeen = false
}

// LoadedSlots returns the loaded slot indices in ascending order.
func (p *Publisher) LoadedSlots() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []int{}
	for i, l := range p.loaded {
		if l {
			out = append(out, i)
		}
	}
	return out
}

// ClearDoorbells deletes every doorbell file. ctl_present.wav is never touched.
func (p *Publisher) ClearDoorbells() (int, error) {
	entries, err := os.ReadDir(p.L.SignalsDir())
	if err != nil {
		return 0, err
	}
	var errs []error
	n := 0
	for _, e := range entries {
		if !layout.IsDoorbellName(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(p.L.SignalsDir(), e.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// WipeSlots rewrites every slot's data.lua to the empty form
// `OmpcraftInbox = nil` (atomically), so no reply text stays at rest in the
// game folder when the game is not running. It reads nothing (a read would
// raise IN_OPEN) and returns the number of slots rewritten.
func (p *Publisher) WipeSlots() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	n := 0
	for i := range p.L.Slots {
		if err := p.writeAtomic(p.L.SlotDir(i), layout.DataTmp, layout.DataFile, []byte(layout.EmptyData)); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	p.stats.Wipes++
	return n, errors.Join(errs...)
}

// EnsureDoorbells recreates every missing doorbell and ctl_present.wav, so they
// all exist when the game next starts (PROTOCOL §6.4).
func (p *Publisher) EnsureDoorbells() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.MkdirAll(p.L.SignalsDir(), 0o755); err != nil {
		return 0, err
	}
	var errs []error
	n := 0
	create := func(path string) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if err := os.WriteFile(path, p.wav, 0o644); err != nil {
			errs = append(errs, err)
			return
		}
		n++
	}
	create(filepath.Join(p.L.SignalsDir(), layout.CtlPresent))
	for i := range p.L.Signals {
		create(p.L.Doorbell(i))
	}
	return n, errors.Join(errs...)
}

// Stats returns a copy of the counters.
func (p *Publisher) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}
