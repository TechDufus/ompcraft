package publish

import "time"

// EventKind classifies a watcher event.
type EventKind string

const (
	// KindLuaOpen: the client opened a .lua file under A/Ompcraft.
	KindLuaOpen EventKind = "lua_open"
	// KindSlotOpen: the client opened A/OmpcraftSnnn/data.lua.
	KindSlotOpen EventKind = "slot_open"
	// KindAddonDirReplaced: A/Ompcraft was replaced (addon-only install) and
	// the watch on it was re-established.
	KindAddonDirReplaced EventKind = "addon_dir_replaced"
	// KindOverflow: the inotify queue overflowed; events were lost.
	KindOverflow EventKind = "overflow"
)

// Event is one thing the watcher saw.
type Event struct {
	Kind EventKind
	Slot int    // KindSlotOpen
	Name string // file name, for logs
	At   time.Time
}

// QuietPeriod is how long A/Ompcraft must see no .lua opens before the next
// one counts as a UI session start (PROTOCOL §6.3).
const QuietPeriod = 2 * time.Second

// SessionDetector applies the 2 s quiet rule.
type SessionDetector struct {
	Quiet time.Duration
	last  time.Time
}

// LuaOpen records an open of an A/Ompcraft .lua file at t and reports whether
// it starts a new UI session: the first open after Quiet without any other.
func (d *SessionDetector) LuaOpen(t time.Time) bool {
	quiet := d.Quiet
	if quiet == 0 {
		quiet = QuietPeriod
	}
	start := d.last.IsZero() || t.Sub(d.last) >= quiet
	d.last = t
	return start
}
