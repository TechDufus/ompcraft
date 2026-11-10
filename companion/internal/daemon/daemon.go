// Package daemon ties the model, publisher and watcher together: it decides
// when to publish (PROTOCOL §6.1), reacts to client events (§6.3), tracks the
// game process (§6.4) and persists state (§6.5).
package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/logx"
	"github.com/TechDufus/ompcraft/companion/internal/model"
	"github.com/TechDufus/ompcraft/companion/internal/publish"
)

const (
	CoalesceInterval = 1000 * time.Millisecond
	HeartbeatEvery   = 120 * time.Second
	CosmeticInterval = 60 * time.Second
	ActivityInterval = 20 * time.Second
	TernFresh        = 3 * time.Second
	stepEvery        = 250 * time.Millisecond
	pollEvery        = 2 * time.Second
)

// Config wires a Daemon. Zero Now/FindGame/SaveState fall back to no-ops that
// are only suitable for tests.
type Config struct {
	Layout    layout.Layout
	Version   string
	Log       *logx.Logger
	Now       func() time.Time
	FindGame  func() (int, error) // returns 0 when the game is not running
	Resume    config.State        // persisted state to resume from
	SaveState func(config.State) error
	// WatchReady, if set, is called once the inotify watches are in place.
	WatchReady func()
	// NoDetail omits detail, preview and last from snapshots (config option
	// "detail": false).
	NoDetail bool
	// UpdateInfo, if set, reports the auto-update state in /v1/state.
	UpdateInfo func() UpdateInfo
	// OnGameExit, if set, runs in its own goroutine after the game exited (the
	// updater installs a staged binary there).
	OnGameExit func()
}

// Counters are exposed by /v1/state.
type Counters struct {
	Publishes      int64            `json:"publishes"`
	SlotWrites     int64            `json:"slotWrites"`
	WriteErrors    int64            `json:"writeErrors"`
	SkippedIdle    int64            `json:"skippedIdle"`
	SlotWipes      int64            `json:"slotWipes"`
	Doorbells      int64            `json:"doorbellsRung"`
	DoorbellRegens int64            `json:"doorbellRegens"`
	SessionStarts  int64            `json:"uiSessionStarts"`
	NewLaunches    int64            `json:"newLaunches"`
	Syncs          int64            `json:"syncs"`
	Inotify        map[string]int64 `json:"inotifyEvents"`
}

// UpdateInfo is the auto-update part of /v1/state.
type UpdateInfo struct {
	Channel    string `json:"channel"`
	Autoupdate bool   `json:"autoupdate"`
	LastCheck  int64  `json:"lastCheck"` // unix ms, 0 = never
	Latest     string `json:"latest"`
	LastError  string `json:"lastError"`
	Staged     string `json:"staged,omitempty"` // a verified update waiting for the game to quit
}

// Report is the daemon part of GET /v1/state.
type Report struct {
	Version       string         `json:"version"`
	Run           int64          `json:"run"`
	Slots         int            `json:"slots"`
	Signals       int            `json:"signals"`
	Snapshot      model.Snapshot `json:"snapshot"`
	LoadedSlots   []int          `json:"loadedSlots"`
	SignalNext    int            `json:"signalNext"`
	Session       int64          `json:"session"`
	GamePid       int            `json:"gamePid"`
	LastSyncMs    int64          `json:"lastSyncMs"`
	Counters      Counters       `json:"counters"`
	DetailEnabled bool           `json:"detailEnabled"`
	Update        UpdateInfo     `json:"update"`
}

type Daemon struct {
	cfg Config
	pub *publish.Publisher
	log *logx.Logger
	run int64

	mu          sync.Mutex
	model       *model.Model
	det         publish.SessionDetector
	gen         int64
	lastKey     string // full content key of the last publish
	lastActKey  string // important + activity/todo key of the last publish
	lastImpKey  string // important-fields key of the last publish
	lastPublish time.Time
	lastSync    time.Time
	session     int64 // unix seconds of the last UI session start; 0 = not seen
	gamePid     int
	sessionPid  int  // game pid at the previous UI session start
	haveSession bool // a UI session start (or a resumed launch) is known
	lastSnap    model.Snapshot
	counters    Counters
	pollErrLog  bool
	stopped     bool // Shutdown ran: no more file writes
	notices     []model.Notice
}

// New builds a Daemon and resumes persisted state (§6.5): signalNext is kept
// only if the persisted game pid is still the running game.
func New(cfg Config) *Daemon {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = logx.Discard()
	}
	if cfg.FindGame == nil {
		cfg.FindGame = func() (int, error) { return 0, nil }
	}
	if cfg.SaveState == nil {
		cfg.SaveState = func(config.State) error { return nil }
	}
	d := &Daemon{
		cfg:   cfg,
		log:   cfg.Log,
		run:   cfg.Now().UnixMilli(),
		model: model.New(),
	}
	d.counters.Inotify = map[string]int64{}
	d.pub = publish.NewPublisher(cfg.Layout, cfg.Log.Printf)

	pid, err := cfg.FindGame()
	if err != nil {
		d.logPollErr(err)
	}
	d.gamePid = pid
	switch {
	case pid != 0 && pid == cfg.Resume.GamePid:
		d.pub.SetSignalNext(cfg.Resume.SignalNext)
		d.sessionPid, d.haveSession = pid, true
		d.log.Printf("resumed: game pid %d, signalNext %d", pid, cfg.Resume.SignalNext)
	case pid == 0:
		// No game: make sure every doorbell exists for the next launch.
		d.regenDoorbells()
		d.wipeLocked("startup without a game") // data left by a crashed run
	}
	d.saveStateLocked()
	return d
}

// Publisher exposes the publisher (tests, state).
func (d *Daemon) Publisher() *publish.Publisher { return d.pub }

func (d *Daemon) logPollErr(err error) {
	if !d.pollErrLog {
		d.pollErrLog = true
		d.log.Printf("game detection: %v", err)
	}
}

func (d *Daemon) saveStateLocked() {
	st := config.State{GamePid: d.gamePid, SignalNext: d.pub.SignalNext()}
	if err := d.cfg.SaveState(st); err != nil {
		d.log.Printf("saving state: %v", err)
	}
}

func (d *Daemon) regenDoorbells() {
	n, err := d.pub.EnsureDoorbells()
	d.counters.DoorbellRegens++
	if err != nil {
		d.log.Printf("recreating doorbells: %v", err)
	}
	d.log.Printf("doorbells recreated (%d files created)", n)
}

// Sync applies a leader sync (PROTOCOL §3.2) and publishes if due.
func (d *Daemon) Sync(req *model.SyncRequest) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.cfg.Now()
	d.model.Apply(req, now)
	d.lastSync = now
	d.counters.Syncs++
	d.stepLocked(now)
}

// Step runs the publish decision at the current time.
func (d *Daemon) Step() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stepLocked(d.cfg.Now())
}

func (d *Daemon) snapshotLocked(now time.Time) model.Snapshot {
	agents, toasts := d.model.View(now)
	if d.cfg.NoDetail { // detail off: no reply text, steps, previews or last messages at rest
		for i := range agents {
			agents[i].Detail, agents[i].Preview, agents[i].Last = nil, "", ""
		}
	}
	tern := "down"
	if !d.lastSync.IsZero() && now.Sub(d.lastSync) < TernFresh {
		tern = "ok"
	}
	return model.Snapshot{
		V:         model.SnapshotVersion,
		Run:       d.run,
		Session:   d.session,
		Tern:      tern,
		Companion: d.cfg.Version,
		Agents:    agents,
		Toasts:    toasts,
	}
}

// snapKeys returns the full, important and activity content keys of snap.
func snapKeys(snap model.Snapshot) (full, imp, act string, err error) {
	if full, err = snap.ContentKey(); err != nil {
		return
	}
	if imp, err = snap.ImportantKey(); err != nil {
		return
	}
	act, err = snap.ActivityKey()
	return
}

// stepLocked decides whether to publish (PROTOCOL §6.1, §8.5). Changes fall in
// three classes. Important (agents added or removed, state changes, toasts,
// tern, session) is coalesced to one publish per CoalesceInterval. Activity
// (activity, todo) is limited to one publish per ActivityInterval. Cosmetic
// (everything else: names, stats, preview, detail, ...) is limited to one per
// CosmeticInterval. An important publish carries pending activity and
// cosmetic changes along. The first publish of a run and the heartbeat always
// go out.
func (d *Daemon) stepLocked(now time.Time) {
	snap := d.snapshotLocked(now)
	key, impKey, actKey, err := snapKeys(snap)
	if err != nil {
		d.log.Printf("snapshot: %v", err)
		return
	}
	since := now.Sub(d.lastPublish)
	first := d.lastPublish.IsZero()
	important := impKey != d.lastImpKey && since >= CoalesceInterval
	activity := actKey != d.lastActKey && since >= ActivityInterval
	cosmetic := key != d.lastKey && since >= CosmeticInterval
	beat := since >= HeartbeatEvery
	if first || important || activity || cosmetic || beat {
		d.publishLocked(now, snap)
	}
}

func (d *Daemon) publishLocked(now time.Time, snap model.Snapshot) {
	key, impKey, actKey, err := snapKeys(snap)
	if err != nil {
		d.log.Printf("snapshot: %v", err)
		return
	}
	d.gen++
	snap.Gen = d.gen
	snap.At = now.Unix()
	// Without a game process and without a UI session since the last game exit
	// there is no client to read slots or hear doorbells: only the snapshot
	// state advances. The UI-session-start publish then writes every slot.
	d.pub.SetIdle(d.stopped || (d.gamePid == 0 && d.session == 0))
	if err := d.pub.Publish(&snap); err != nil {
		d.log.Printf("publish gen %d: %v", snap.Gen, err)
	}
	d.lastSnap = snap
	d.lastKey, d.lastImpKey, d.lastActKey = key, impKey, actKey
	d.lastPublish = now
	d.saveStateLocked()
	d.log.Debugf("published gen %d (%d agents, nextSignal %d)", snap.Gen, len(snap.Agents), snap.NextSignal)
}

// HandleEvent reacts to a watcher event (PROTOCOL §6.3).
func (d *Daemon) HandleEvent(ev publish.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.counters.Inotify[string(ev.Kind)]++
	switch ev.Kind {
	case publish.KindLuaOpen:
		d.log.Debugf("IN_OPEN %s at %s", ev.Name, ev.At.Format(time.RFC3339Nano))
		if d.det.LuaOpen(ev.At) {
			d.sessionStartLocked(ev.At, ev.Name)
		}
	case publish.KindSlotOpen:
		d.log.Debugf("IN_OPEN %s at %s", ev.Name, ev.At.Format(time.RFC3339Nano))
		d.pub.MarkLoaded(ev.Slot)
	case publish.KindAddonDirReplaced:
		d.log.Printf("%s replaced; watch re-established", layout.AddonName)
	case publish.KindOverflow:
		d.log.Printf("inotify queue overflowed; some opens were missed")
	}
}

func (d *Daemon) sessionStartLocked(t time.Time, name string) {
	d.counters.SessionStarts++
	d.session = t.Unix()
	d.pub.ClearLoaded()
	pid, err := d.cfg.FindGame()
	if err != nil {
		d.logPollErr(err)
	}
	newLaunch := !d.haveSession || pid != d.sessionPid
	if newLaunch {
		n, err := d.pub.ClearDoorbells()
		if err != nil {
			d.log.Printf("clearing doorbells: %v", err)
		}
		d.pub.SetSignalNext(0)
		d.counters.NewLaunches++
		d.log.Printf("new launch (pid %d): %d doorbells cleared", pid, n)
	}
	d.sessionPid, d.haveSession = pid, true
	if pid != 0 {
		d.gamePid = pid
	}
	d.log.Printf("UI session start at %s (first open: %s)", t.Format(time.RFC3339Nano), name)
	now := d.cfg.Now()
	d.publishLocked(now, d.snapshotLocked(now)) // immediately, bypassing coalescing
}

// wipeLocked resets every slot's data.lua to the empty form so no reply text
// stays at rest in the game folder.
func (d *Daemon) wipeLocked(why string) {
	n, err := d.pub.WipeSlots()
	if err != nil {
		d.log.Printf("wiping slots (%s): %v", why, err)
	}
	d.log.Printf("slots wiped (%s): %d data.lua files reset", why, n)
}

// Shutdown wipes every slot and stops all further file writes. Call it on
// clean shutdown, before link.json is removed.
func (d *Daemon) Shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	d.pub.SetIdle(true)
	d.wipeLocked("shutdown")
}

// PollGame checks the game process once (PROTOCOL §6.4).
func (d *Daemon) PollGame() {
	pid, err := d.cfg.FindGame()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.logPollErr(err)
		return
	}
	if pid == d.gamePid {
		return
	}
	prev := d.gamePid
	d.gamePid = pid
	if pid != 0 {
		d.log.Printf("game running, pid %d", pid)
		d.saveStateLocked()
		return
	}
	d.log.Printf("game exited (pid %d)", prev)
	d.regenDoorbells()
	d.wipeLocked("game exit")
	d.pub.SetSignalNext(0)
	d.pub.ClearLoaded()
	d.session, d.sessionPid, d.haveSession = 0, 0, false
	d.saveStateLocked()
	d.stepLocked(d.cfg.Now())
	if d.cfg.OnGameExit != nil {
		go d.cfg.OnGameExit() // outside the daemon's lock: it may take a while
	}
}

// Report returns the daemon's state for /v1/state.
func (d *Daemon) Report() Report {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.cfg.Now()
	snap := d.snapshotLocked(now)
	snap.Gen, snap.At, snap.NextSignal = d.lastSnap.Gen, d.lastSnap.At, d.lastSnap.NextSignal
	st := d.pub.Stats()
	c := d.counters
	c.Inotify = make(map[string]int64, len(d.counters.Inotify))
	for k, v := range d.counters.Inotify {
		c.Inotify[k] = v
	}
	c.Publishes, c.SlotWrites, c.Doorbells, c.WriteErrors = st.Publishes, st.SlotWrites, st.Doorbells, st.WriteErrs
	c.SkippedIdle, c.SlotWipes = st.Skipped, st.Wipes
	var lastSyncMs int64
	if !d.lastSync.IsZero() {
		lastSyncMs = d.lastSync.UnixMilli()
	}
	return Report{
		Version:       d.cfg.Version,
		Run:           d.run,
		Slots:         d.cfg.Layout.Slots,
		Signals:       d.cfg.Layout.Signals,
		Snapshot:      snap,
		LoadedSlots:   d.pub.LoadedSlots(),
		SignalNext:    d.pub.SignalNext(),
		Session:       d.session,
		GamePid:       d.gamePid,
		LastSyncMs:    lastSyncMs,
		Counters:      c,
		DetailEnabled: !d.cfg.NoDetail,
		Update:        d.updateInfo(),
	}
}

// AddToast adds a toast that belongs to no agent (kind "update") and publishes
// it with the next step (PROTOCOL §8.7).
func (d *Daemon) AddToast(kind, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.cfg.Now()
	d.model.AddSystemToast(kind, text, now)
	d.stepLocked(now)
}

// QueueNotice queues a one-shot notice for the next /v1/sync response.
func (d *Daemon) QueueNotice(n model.Notice) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.notices = append(d.notices, n)
}

// TakeNotice returns the next queued notice, once, or nil.
func (d *Daemon) TakeNotice() *model.Notice {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.notices) == 0 {
		return nil
	}
	n := d.notices[0]
	d.notices = d.notices[1:]
	return &n
}

func (d *Daemon) updateInfo() UpdateInfo {
	if d.cfg.UpdateInfo == nil {
		return UpdateInfo{Channel: "source"}
	}
	return d.cfg.UpdateInfo()
}

// Run drives the daemon until ctx ends: the inotify watcher, the publish
// ticker and the game-process poller. It returns a watcher failure, if any.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchErr := make(chan error, 1)
	go func() {
		watchErr <- publish.Watch(ctx, d.cfg.Layout, d.cfg.WatchReady, d.HandleEvent, d.log.Printf)
	}()

	d.Step()
	step := time.NewTicker(stepEvery)
	defer step.Stop()
	poll := time.NewTicker(pollEvery)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-watchErr:
			if err != nil {
				return fmt.Errorf("watcher: %w", err)
			}
			return nil
		case <-step.C:
			d.Step()
		case <-poll.C:
			d.PollGame()
		}
	}
}
