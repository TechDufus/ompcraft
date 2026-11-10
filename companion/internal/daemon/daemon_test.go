package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/logx"
	"github.com/TechDufus/ompcraft/companion/internal/model"
	"github.com/TechDufus/ompcraft/companion/internal/publish"
	"github.com/TechDufus/ompcraft/companion/internal/testenv"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

type game struct {
	mu  sync.Mutex
	pid int
}

func (g *game) Find() (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pid, nil
}

func (g *game) Set(pid int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pid = pid
}

type rig struct {
	d     *Daemon
	l     layout.Layout
	clk   *clock
	game  *game
	saved []config.State
}

func newRig(t *testing.T, slots, signals, gamePid int, resume config.State) *rig {
	t.Helper()
	r := &rig{l: testenv.Install(t, slots, signals), clk: &clock{t: t0}, game: &game{pid: gamePid}}
	r.d = New(Config{
		Layout:   r.l,
		Version:  "0.1.0-test",
		Log:      logx.Discard(),
		Now:      r.clk.Now,
		FindGame: r.game.Find,
		Resume:   resume,
		SaveState: func(s config.State) error {
			r.saved = append(r.saved, s)
			return nil
		},
	})
	return r
}

func (r *rig) doorbells(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(r.l.SignalsDir())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if layout.IsDoorbellName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

func (r *rig) slot(t *testing.T, i int) string {
	t.Helper()
	b, err := os.ReadFile(r.l.SlotData(i))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func syncReq(state string) *model.SyncRequest {
	return &model.SyncRequest{
		Hosts:    []model.SyncHost{{ID: "0", Name: "host-a"}},
		Sessions: []model.SyncSession{{ID: "1", Name: "demo-session"}},
		Agents: []model.SyncAgent{{Pane: "100", State: state, Title: "Demo task", Cwd: "/home/user/project",
			Model: "demo-model", Host: "0", Session: "1", LastMessage: "working on it"}},
	}
}

func luaOpen(at time.Time) publish.Event {
	return publish.Event{Kind: publish.KindLuaOpen, Name: "Ompcraft/Core/Init.lua", At: at}
}

func TestInitialPublishAndCoalescing(t *testing.T) {
	r := newRig(t, 3, 10, 4242, config.State{})
	r.d.Step() // first publish happens at once
	rep := r.d.Report()
	if rep.Counters.Publishes != 1 || rep.Snapshot.Gen != 1 || rep.Snapshot.Tern != "down" || rep.Snapshot.Run != t0.UnixMilli() {
		t.Fatalf("after start: %+v", rep.Snapshot)
	}
	if !strings.Contains(r.slot(t, 0), `companion = "0.1.0-test"`) {
		t.Fatalf("slot:\n%s", r.slot(t, 0))
	}

	// Nothing changed (tern is still down): no publish, however often we step.
	for range 5 {
		r.clk.Advance(100 * time.Millisecond)
		r.d.Step()
	}
	if got := r.d.Report().Counters.Publishes; got != 1 {
		t.Fatalf("publishes = %d without a change", got)
	}

	// A leader sync changes tern and agents; 0.5 s after the last publish it is held back.
	r.d.Sync(syncReq(model.StateWorking))
	if got := r.d.Report().Counters.Publishes; got != 1 {
		t.Fatalf("publish not coalesced: %d", got)
	}
	r.clk.Advance(400 * time.Millisecond) // 0.9 s since the publish
	r.d.Step()
	if got := r.d.Report().Counters.Publishes; got != 1 {
		t.Fatalf("publish not coalesced at 0.9 s: %d", got)
	}
	r.clk.Advance(100 * time.Millisecond) // 1.0 s
	r.d.Step()
	rep = r.d.Report()
	if rep.Counters.Publishes != 2 || rep.Snapshot.Gen != 2 || len(rep.Snapshot.Agents) != 1 {
		t.Fatalf("coalesced publish missing: %+v / %+v", rep.Counters, rep.Snapshot)
	}
	if !strings.Contains(r.slot(t, 1), `name = "Demo task"`) || !strings.Contains(r.slot(t, 1), `host = "host-a"`) {
		t.Fatalf("slot:\n%s", r.slot(t, 1))
	}

	// Several changes inside the window collapse into one publish.
	r.d.Sync(syncReq(model.StateIdle))
	r.clk.Advance(300 * time.Millisecond)
	r.d.Sync(syncReq(model.StateWaiting))
	r.clk.Advance(300 * time.Millisecond)
	r.d.Step()
	if got := r.d.Report().Counters.Publishes; got != 2 {
		t.Fatalf("publishes = %d inside the coalescing window", got)
	}
	r.clk.Advance(500 * time.Millisecond)
	r.d.Step()
	rep = r.d.Report()
	if rep.Counters.Publishes != 3 || rep.Snapshot.Agents[0].State != model.StateWaiting {
		t.Fatalf("got %+v", rep.Counters)
	}
}

func TestTernGoesDownWithoutSyncs(t *testing.T) {
	r := newRig(t, 2, 6, 4242, config.State{})
	r.d.Sync(syncReq(model.StateWorking))
	if got := r.d.Report().Snapshot.Tern; got != "ok" {
		t.Fatalf("tern = %s", got)
	}
	r.clk.Advance(2900 * time.Millisecond)
	r.d.Step()
	if got := r.d.Report().Snapshot.Tern; got != "ok" {
		t.Fatalf("tern = %s at 2.9 s", got)
	}
	n := r.d.Report().Counters.Publishes
	r.clk.Advance(200 * time.Millisecond)
	r.d.Step() // 3.1 s since the sync: tern flips, which is a change
	rep := r.d.Report()
	if rep.Snapshot.Tern != "down" || rep.Counters.Publishes != n+1 {
		t.Fatalf("tern=%s publishes=%d (was %d)", rep.Snapshot.Tern, rep.Counters.Publishes, n)
	}
	if !strings.Contains(r.slot(t, 0), `tern = "down"`) {
		t.Fatal("slot does not say tern is down")
	}
}

func TestHeartbeat(t *testing.T) {
	r := newRig(t, 2, 6, 4242, config.State{})
	r.d.Step()
	r.clk.Advance(HeartbeatEvery - time.Second)
	r.d.Step()
	if got := r.d.Report().Counters.Publishes; got != 1 {
		t.Fatalf("early heartbeat: %d", got)
	}
	r.clk.Advance(time.Second)
	r.d.Step()
	rep := r.d.Report()
	if rep.Counters.Publishes != 2 || rep.Snapshot.Gen != 2 {
		t.Fatalf("no heartbeat publish: %+v", rep.Counters)
	}
	if !strings.Contains(r.slot(t, 0), "gen = 2,") {
		t.Fatal("heartbeat did not reach the slots")
	}
}

func TestUISessionStartAndQuietRule(t *testing.T) {
	r := newRig(t, 4, 20, 4242, config.State{})
	r.d.Step()
	pub := r.d.Publisher()
	pub.MarkLoaded(1) // pretend a previous session loaded slot 1

	// First open: a UI session starts, loaded set is cleared, publish is immediate.
	at := r.clk.Advance(10 * time.Second)
	before := r.d.Report().Counters.Publishes
	r.d.HandleEvent(luaOpen(at))
	rep := r.d.Report()
	if rep.Session != at.Unix() || rep.Snapshot.Session != at.Unix() {
		t.Fatalf("session = %d, want %d", rep.Session, at.Unix())
	}
	if len(rep.LoadedSlots) != 0 {
		t.Fatalf("loaded set not cleared: %v", rep.LoadedSlots)
	}
	if rep.Counters.Publishes != before+1 || rep.Counters.SessionStarts != 1 {
		t.Fatalf("publish on session start missing: %+v", rep.Counters)
	}
	if d := r.slot(t, 1); !strings.Contains(d, "session = "+strconv.FormatInt(at.Unix(), 10)+",") {
		t.Fatalf("slot 1 was not refreshed with the session:\n%s", d)
	}

	// More addon files opened within 2 s of each other: still the same session.
	for i := 1; i <= 3; i++ {
		r.d.HandleEvent(luaOpen(at.Add(time.Duration(i) * 500 * time.Millisecond)))
	}
	if got := r.d.Report().Counters.SessionStarts; got != 1 {
		t.Fatalf("burst counted as %d sessions", got)
	}

	// Opening a slot marks it and every slot below it loaded (the client loads
	// in order); the next publish skips them.
	r.d.HandleEvent(publish.Event{Kind: publish.KindSlotOpen, Slot: 2, At: at.Add(2 * time.Second)})
	if got := r.d.Report().LoadedSlots; !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("loaded = %v", got)
	}
	r.clk.Advance(5 * time.Second)
	r.d.Sync(syncReq(model.StateWorking))
	r.clk.Advance(CoalesceInterval)
	r.d.Step()
	if d := r.slot(t, 2); strings.Contains(d, "agents = {\n    {") {
		t.Fatalf("loaded slot 2 was rewritten:\n%s", d)
	}

	// 2 s of quiet, then another open: /reload, a second session start.
	later := at.Add(1500*time.Millisecond + 2*time.Second)
	r.clk.Advance(10 * time.Second)
	r.d.HandleEvent(luaOpen(later))
	rep = r.d.Report()
	if rep.Counters.SessionStarts != 2 || rep.Session != later.Unix() || len(rep.LoadedSlots) != 0 {
		t.Fatalf("second session not detected: %+v session=%d loaded=%v", rep.Counters, rep.Session, rep.LoadedSlots)
	}
	if rep.Counters.Inotify["lua_open"] != 5 || rep.Counters.Inotify["slot_open"] != 1 {
		t.Fatalf("event counters = %v", rep.Counters.Inotify)
	}
}

func TestNewLaunchClearsDoorbells(t *testing.T) {
	r := newRig(t, 3, 12, 1000, config.State{})
	pub := r.d.Publisher()
	r.d.Step() // rings doorbell 0 (state of a companion started mid-launch)
	r.d.Sync(syncReq(model.StateWorking))
	r.clk.Advance(2 * time.Second)
	r.d.Step()
	if pub.SignalNext() < 1 {
		t.Fatal("setup: nothing rung")
	}

	// First UI session seen: new launch, even though pid 1000 was running at start.
	at := r.clk.Advance(time.Second)
	r.d.HandleEvent(luaOpen(at))
	if got := r.doorbells(t); !reflect.DeepEqual(got, []string{"0000.wav"}) {
		t.Fatalf("after new launch only the doorbell for this publish may exist, got %v", got)
	}
	if pub.SignalNext() != 1 {
		t.Fatalf("signalNext = %d, want 1 (reset to 0, then one publish)", pub.SignalNext())
	}
	if _, err := os.Stat(filepath.Join(r.l.SignalsDir(), layout.CtlPresent)); err != nil {
		t.Fatal("ctl_present.wav must survive")
	}
	if !strings.Contains(r.slot(t, 0), "nextSignal = 1,") {
		t.Fatalf("slot:\n%s", r.slot(t, 0))
	}

	// A /reload of the same process: doorbells and signalNext carry on.
	r.clk.Advance(30 * time.Second)
	r.d.HandleEvent(luaOpen(r.clk.Now()))
	if got := r.doorbells(t); !reflect.DeepEqual(got, []string{"0000.wav", "0001.wav"}) {
		t.Fatalf("reload must keep doorbells, got %v", got)
	}
	if pub.SignalNext() != 2 {
		t.Fatalf("signalNext = %d", pub.SignalNext())
	}

	// The game restarts under a new pid: everything is cleared again.
	r.game.Set(2000)
	r.clk.Advance(30 * time.Second)
	r.d.HandleEvent(luaOpen(r.clk.Now()))
	if got := r.doorbells(t); !reflect.DeepEqual(got, []string{"0000.wav"}) {
		t.Fatalf("second launch: doorbells = %v", got)
	}
	rep := r.d.Report()
	if pub.SignalNext() != 1 || rep.Counters.NewLaunches != 2 || rep.Counters.SessionStarts != 3 {
		t.Fatalf("signalNext=%d counters=%+v", pub.SignalNext(), rep.Counters)
	}
	if rep.GamePid != 2000 {
		t.Fatalf("gamePid = %d", rep.GamePid)
	}
}

func TestGameExitRecreatesDoorbells(t *testing.T) {
	r := newRig(t, 3, 12, 777, config.State{})
	pub := r.d.Publisher()
	r.d.HandleEvent(luaOpen(r.clk.Now())) // new launch: all doorbells deleted, 0000 rung
	r.d.Sync(syncReq(model.StateWorking))
	r.clk.Advance(2 * time.Second)
	r.d.Step()
	if got := r.doorbells(t); len(got) >= 12 {
		t.Fatalf("setup: expected most doorbells deleted, got %d", len(got))
	}
	pub.MarkLoaded(1)

	r.d.PollGame() // still running: nothing happens
	if got := r.doorbells(t); len(got) >= 12 {
		t.Fatal("doorbells recreated while the game runs")
	}

	r.game.Set(0)
	r.clk.Advance(2 * time.Second)
	r.d.PollGame()
	if got := r.doorbells(t); len(got) != 12 {
		t.Fatalf("doorbells after exit = %d, want all 12", len(got))
	}
	if _, err := os.Stat(filepath.Join(r.l.SignalsDir(), layout.CtlPresent)); err != nil {
		t.Fatal(err)
	}
	rep := r.d.Report()
	// The exit resets signalNext to 0; the snapshot change (session cleared) may then ring doorbell 0.
	if rep.GamePid != 0 || rep.Session != 0 || rep.SignalNext > 1 || len(rep.LoadedSlots) != 0 {
		t.Fatalf("state after exit: %+v", rep)
	}
	if last := r.saved[len(r.saved)-1]; last.GamePid != 0 {
		t.Fatalf("saved state = %+v", last)
	}

	// The next launch is a new launch again, even with the same pid.
	r.game.Set(777)
	r.d.PollGame()
	r.clk.Advance(time.Second)
	r.d.HandleEvent(luaOpen(r.clk.Now()))
	if got := r.doorbells(t); !reflect.DeepEqual(got, []string{"0000.wav"}) {
		t.Fatalf("doorbells after relaunch = %v", got)
	}
}

func TestStartupWithoutGameRecreatesDoorbells(t *testing.T) {
	l := testenv.Install(t, 2, 5)
	_ = os.Remove(l.Doorbell(1))
	_ = os.Remove(l.Doorbell(4))
	d := New(Config{Layout: l, Log: logx.Discard(), FindGame: func() (int, error) { return 0, nil }})
	_ = d
	for i := range 5 {
		if _, err := os.Stat(l.Doorbell(i)); err != nil {
			t.Errorf("doorbell %d missing after start without a game", i)
		}
	}
}

func TestResumeState(t *testing.T) {
	// Same pid still running: signalNext is resumed, and a reload is not a new launch.
	r := newRig(t, 3, 20, 555, config.State{GamePid: 555, SignalNext: 7})
	if got := r.d.Publisher().SignalNext(); got != 7 {
		t.Fatalf("signalNext = %d, want 7", got)
	}
	_ = os.Remove(r.l.Doorbell(3)) // would be recreated only by a launch reset
	r.d.HandleEvent(luaOpen(r.clk.Now()))
	if got := r.d.Publisher().SignalNext(); got != 8 {
		t.Fatalf("signalNext = %d, want 8 (resumed 7 + one publish)", got)
	}
	if r.d.Report().Counters.NewLaunches != 0 {
		t.Fatal("a resumed launch must not count as a new launch")
	}
	if len(r.saved) == 0 || r.saved[len(r.saved)-1] != (config.State{GamePid: 555, SignalNext: 8}) {
		t.Fatalf("saved = %+v", r.saved)
	}

	// Different pid: start from 0.
	r = newRig(t, 3, 20, 556, config.State{GamePid: 555, SignalNext: 7})
	if got := r.d.Publisher().SignalNext(); got != 0 {
		t.Fatalf("signalNext = %d, want 0 for a different pid", got)
	}
	// Game not running: start from 0 as well.
	r = newRig(t, 3, 20, 0, config.State{GamePid: 555, SignalNext: 7})
	if got := r.d.Publisher().SignalNext(); got != 0 {
		t.Fatalf("signalNext = %d, want 0 without a game", got)
	}
}

func TestToastsReachSnapshot(t *testing.T) {
	r := newRig(t, 2, 6, 4242, config.State{})
	r.d.Sync(syncReq(model.StateWorking))
	r.clk.Advance(1500 * time.Millisecond)
	r.d.Sync(syncReq(model.StateIdle))
	r.clk.Advance(1500 * time.Millisecond)
	r.d.Step()
	rep := r.d.Report()
	if len(rep.Snapshot.Toasts) != 1 || rep.Snapshot.Toasts[0].Text != "Demo task finished" || rep.Snapshot.Toasts[0].Kind != "done" {
		t.Fatalf("toasts = %+v", rep.Snapshot.Toasts)
	}
	if d := r.slot(t, 0); !strings.Contains(d, `text = "Demo task finished"`) {
		t.Fatalf("slot:\n%s", d)
	}
}

func streamReq(state, title, last string) *model.SyncRequest {
	r := syncReq(state)
	r.Agents[0].Title, r.Agents[0].LastMessage = title, last
	return r
}

func TestCosmeticChangesArePublishedAtMostOncePerMinute(t *testing.T) {
	r := newRig(t, 3, 50, 4242, config.State{})
	pubs := func() int64 { return r.d.Report().Counters.Publishes }

	r.d.Sync(streamReq(model.StateWorking, "Demo task", "m0")) // first publish of the run
	if pubs() != 1 {
		t.Fatalf("first publish missing: %d", pubs())
	}
	// A working agent whose last message streams every second: nothing is published for 59 s.
	for i := 1; i <= 59; i++ {
		r.clk.Advance(time.Second)
		r.d.Sync(streamReq(model.StateWorking, "Demo task", "streaming "+strconv.Itoa(i)))
	}
	if pubs() != 1 || r.d.Publisher().SignalNext() != 1 {
		t.Fatalf("cosmetic changes published early: publishes=%d signalNext=%d", pubs(), r.d.Publisher().SignalNext())
	}
	if strings.Contains(r.slot(t, 0), "streaming") {
		t.Fatal("slot was rewritten for a cosmetic change")
	}
	// 60 s after the last publish the pending cosmetic change goes out.
	r.clk.Advance(time.Second)
	r.d.Sync(streamReq(model.StateWorking, "Demo task", "streaming 60"))
	if pubs() != 2 || !strings.Contains(r.slot(t, 0), "streaming 60") {
		t.Fatalf("cosmetic publish missing: publishes=%d", pubs())
	}

	// Renames are cosmetic too.
	r.clk.Advance(time.Second)
	r.d.Sync(streamReq(model.StateWorking, "Renamed task", "streaming 60"))
	if pubs() != 2 {
		t.Fatal("rename published early")
	}

	// An important change (state change + toast) carries pending cosmetic changes along,
	// after the usual 1 s coalescing.
	r.clk.Advance(time.Second)
	r.d.Sync(streamReq(model.StateIdle, "Renamed task", "final answer"))
	if pubs() != 3 {
		t.Fatalf("state change not published: %d", pubs())
	}
	slot := r.slot(t, 0)
	for _, want := range []string{`name = "Renamed task"`, `last = "final answer"`, `state = "idle"`, `text = "Renamed task finished"`} {
		if !strings.Contains(slot, want) {
			t.Errorf("slot lacks %s:\n%s", want, slot)
		}
	}
}

func TestImportantChangesAreCoalescedNotRateLimited(t *testing.T) {
	r := newRig(t, 3, 50, 4242, config.State{})
	pubs := func() int64 { return r.d.Report().Counters.Publishes }
	r.d.Sync(streamReq(model.StateWorking, "Demo task", "m"))
	r.clk.Advance(1500 * time.Millisecond)
	r.d.Sync(streamReq(model.StateWaiting, "Demo task", "m")) // state change: published after 1 s
	if pubs() != 2 {
		t.Fatalf("state change: publishes = %d", pubs())
	}
	r.clk.Advance(1500 * time.Millisecond)
	r.d.Sync(&model.SyncRequest{}) // agent removed
	if pubs() != 3 || len(r.d.Report().Snapshot.Agents) != 0 {
		t.Fatalf("agent removal: publishes = %d", pubs())
	}
	r.clk.Advance(1500 * time.Millisecond)
	r.d.Sync(streamReq(model.StateWorking, "Demo task", "m")) // agent added
	if pubs() != 4 {
		t.Fatalf("agent added: publishes = %d", pubs())
	}
}

func TestSlotWindowAfterSlotOpen(t *testing.T) {
	r := newRig(t, 20, 50, 0, config.State{})
	r.d.HandleEvent(luaOpen(r.clk.Now())) // UI session: all slots written
	for i := range 20 {
		if !strings.Contains(r.slot(t, i), "session = ") {
			t.Fatalf("slot %d not written at session start", i)
		}
	}
	r.d.HandleEvent(publish.Event{Kind: publish.KindSlotOpen, Slot: 0, At: r.clk.Now()})
	r.clk.Advance(2 * time.Second)
	r.d.Sync(streamReq(model.StateWorking, "Demo task", "m"))
	if !strings.Contains(r.slot(t, 8), `name = "Demo task"`) {
		t.Fatal("slot 8 (8th lowest non-loaded) must be written")
	}
	if strings.Contains(r.slot(t, 9), `name = "Demo task"`) {
		t.Fatal("slot 9 is outside the 8-slot window")
	}
}

func TestNoFileWritesWhileGameIsClosed(t *testing.T) {
	r := newRig(t, 5, 20, 0, config.State{})
	st := func() (int64, int64, int64) {
		rep := r.d.Report()
		return rep.Counters.SlotWrites, rep.Counters.Doorbells, rep.Counters.SkippedIdle
	}
	before := r.doorbells(t)

	// Syncs and heartbeats with the game closed: gen advances, nothing touches the slots.
	r.d.Step()
	r.d.Sync(syncReq(model.StateWorking))
	r.clk.Advance(2 * time.Second)
	r.d.Sync(syncReq(model.StateIdle))
	r.clk.Advance(HeartbeatEvery)
	r.d.Step()
	if w, b, skipped := st(); w != 0 || b != 0 || skipped < 3 {
		t.Fatalf("slot writes=%d doorbells=%d skipped=%d", w, b, skipped)
	}
	for i := range 5 {
		if r.slot(t, i) != layout.EmptyData {
			t.Fatalf("slot %d was written while the game is closed", i)
		}
	}
	if got := r.doorbells(t); !reflect.DeepEqual(got, before) || r.d.Publisher().SignalNext() != 0 {
		t.Fatalf("doorbells changed while the game is closed: %v signalNext=%d", got, r.d.Publisher().SignalNext())
	}
	rep := r.d.Report()
	if rep.Snapshot.Gen < 3 || len(rep.Snapshot.Agents) != 1 || rep.Snapshot.Agents[0].State != model.StateIdle {
		t.Fatalf("snapshot state must keep advancing: %+v", rep.Snapshot)
	}

	// The game starts and the client opens the UI: the session-start publish writes every
	// slot with the latest snapshot.
	r.game.Set(31337)
	r.clk.Advance(time.Second)
	r.d.HandleEvent(luaOpen(r.clk.Now()))
	for i := range 5 {
		d := r.slot(t, i)
		if !strings.Contains(d, `state = "idle"`) || !strings.Contains(d, "session = ") {
			t.Fatalf("slot %d lacks the latest snapshot:\n%s", i, d)
		}
	}
	if w, b, _ := st(); w != 5 || b != 1 {
		t.Fatalf("session start: slot writes=%d doorbells=%d", w, b)
	}

	// The game exits: doorbells are recreated and writes stop again until the next UI session.
	r.game.Set(0)
	r.clk.Advance(2 * time.Second)
	r.d.PollGame()
	w0, b0, _ := st()
	r.clk.Advance(2 * time.Second)
	r.d.Sync(syncReq(model.StateWorking))
	r.clk.Advance(2 * time.Second)
	r.d.Sync(syncReq(model.StateIdle))
	if w, b, _ := st(); w != w0 || b != b0 {
		t.Fatalf("writes after game exit: slot %d->%d doorbells %d->%d", w0, w, b0, b)
	}
	if got := r.doorbells(t); len(got) != 20 {
		t.Fatalf("doorbells after exit = %d, want all 20", len(got))
	}
}

func TestWritesContinueWhileGameRunsWithoutSessionSeen(t *testing.T) {
	// A companion started mid-session sees a game pid but no UI session start.
	r := newRig(t, 3, 10, 4242, config.State{})
	r.d.Sync(syncReq(model.StateWorking))
	if w := r.d.Report().Counters.SlotWrites; w != 3 {
		t.Fatalf("slot writes = %d, want 3 with the game running", w)
	}
}

func richReq(state, activity, todo, reply, last string) *model.SyncRequest {
	r := syncReq(state)
	r.Agents[0].LastMessage = last
	r.Agents[0].Detail = &model.SyncDetail{Activity: activity, Todo: todo, Reply: reply,
		Steps: []model.SyncStep{{Name: "bash", Text: activity, Status: "running"}}}
	return r
}

func TestActivityClassIsPublishedAtMostEvery20Seconds(t *testing.T) {
	r := newRig(t, 3, 50, 4242, config.State{})
	pubs := func() int64 { return r.d.Report().Counters.Publishes }

	r.d.Sync(richReq(model.StateWorking, "Reading", "1/5", "", "m")) // first publish
	if pubs() != 1 {
		t.Fatalf("first publish: %d", pubs())
	}
	r.clk.Advance(time.Second)
	r.d.Sync(richReq(model.StateWorking, "Editing", "1/5", "", "m"))
	r.clk.Advance(18 * time.Second) // 19 s since the publish
	r.d.Sync(richReq(model.StateWorking, "Editing", "1/5", "", "m"))
	if pubs() != 1 || strings.Contains(r.slot(t, 0), "Editing") {
		t.Fatalf("activity published early: publishes=%d", pubs())
	}
	r.clk.Advance(time.Second) // 20 s
	r.d.Sync(richReq(model.StateWorking, "Editing", "1/5", "", "m"))
	if pubs() != 2 || !strings.Contains(r.slot(t, 0), `activity = "Editing",`) {
		t.Fatalf("activity publish missing: publishes=%d\n%s", pubs(), r.slot(t, 0))
	}
	// A todo change is activity-class as well.
	r.clk.Advance(5 * time.Second)
	r.d.Sync(richReq(model.StateWorking, "Editing", "2/5", "", "m"))
	if pubs() != 2 {
		t.Fatalf("todo published early: %d", pubs())
	}
	r.clk.Advance(15 * time.Second)
	r.d.Step()
	if pubs() != 3 || !strings.Contains(r.slot(t, 0), `todo = "2/5",`) {
		t.Fatalf("todo publish missing: %d", pubs())
	}
	// Activity only exists while working: it disappears with the state change (important).
	r.clk.Advance(2 * time.Second)
	r.d.Sync(richReq(model.StateIdle, "Editing", "2/5", "", "m"))
	if d := r.slot(t, 0); strings.Contains(d, "activity =") || !strings.Contains(d, `state = "idle"`) {
		t.Fatalf("slot after finish:\n%s", d)
	}
}

func TestStatsPreviewAndDetailAreCosmetic(t *testing.T) {
	r := newRig(t, 3, 50, 4242, config.State{})
	pubs := func() int64 { return r.d.Report().Counters.Publishes }
	r.d.Sync(richReq(model.StateWorking, "Reading", "", "reply 0", "m")) // first publish
	for i := 1; i <= 59; i++ {
		r.clk.Advance(time.Second)
		r.d.Sync(richReq(model.StateWorking, "Reading", "", "reply "+strconv.Itoa(i), "m\n·\n"+strconv.Itoa(i)+"s · 1K tok"))
	}
	if pubs() != 1 {
		t.Fatalf("cosmetic changes published after %d publishes", pubs())
	}
	r.clk.Advance(time.Second)
	r.d.Sync(richReq(model.StateWorking, "Reading", "", "reply 60", "m\n·\n60s · 1K tok"))
	slot := r.slot(t, 0)
	if pubs() != 2 || !strings.Contains(slot, `preview = "reply 60",`) || !strings.Contains(slot, `stats = "1m 0s \194\183 1K tok",`) {
		t.Fatalf("cosmetic publish missing: %d\n%s", pubs(), slot)
	}

	// An important change carries pending activity and cosmetic changes along.
	r.clk.Advance(5 * time.Second)
	r.d.Sync(richReq(model.StateWorking, "Reading", "4/9", "reply 61", "m"))
	r.clk.Advance(5 * time.Second)
	r.d.Sync(richReq(model.StateWaiting, "Reading", "4/9", "needs input now", "m"))
	slot = r.slot(t, 0)
	if pubs() != 3 {
		t.Fatalf("important change not published: %d", pubs())
	}
	for _, want := range []string{`todo = "4/9",`, `preview = "needs input now",`, `state = "waiting_input",`, "detail = {", `doneAt = `} {
		if !strings.Contains(slot, want) {
			t.Errorf("slot lacks %s", want)
		}
	}
}

func (r *rig) allSlotsEmpty(t *testing.T) bool {
	t.Helper()
	for i := range r.l.Slots {
		if r.slot(t, i) != layout.EmptyData {
			return false
		}
	}
	return true
}

func TestSlotsAreWipedWhenTheGameExits(t *testing.T) {
	r := newRig(t, 4, 10, 777, config.State{})
	r.d.HandleEvent(luaOpen(r.clk.Now())) // UI session: all slots written
	r.d.Sync(richReq(model.StateWorking, "Reading", "", "secret reply text", "m"))
	r.clk.Advance(2 * time.Second)
	r.d.Step()
	for i := range 4 {
		if !strings.Contains(r.slot(t, i), "secret reply text") {
			t.Fatalf("setup: slot %d has no reply text", i)
		}
	}
	r.game.Set(0)
	r.clk.Advance(2 * time.Second)
	r.d.PollGame()
	if !r.allSlotsEmpty(t) {
		t.Fatal("slots still hold data after the game exited")
	}
	if got := r.d.Report().Counters.SlotWipes; got != 1 {
		t.Fatalf("slotWipes = %d, want 1", got)
	}
	for i := range 4 {
		if _, err := os.Stat(filepath.Join(r.l.SlotDir(i), layout.DataTmp)); !os.IsNotExist(err) {
			t.Errorf("slot %d left a tmp file", i)
		}
	}
	// Later syncs with the game closed do not put the text back.
	r.clk.Advance(2 * time.Second)
	r.d.Sync(richReq(model.StateWorking, "Reading", "", "more secret text", "m"))
	r.clk.Advance(2 * time.Second)
	r.d.Step()
	if !r.allSlotsEmpty(t) {
		t.Fatal("a sync rewrote slots while the game is closed")
	}
}

func TestShutdownWipesSlotsAndStopsWrites(t *testing.T) {
	r := newRig(t, 4, 10, 777, config.State{})
	r.d.HandleEvent(luaOpen(r.clk.Now()))
	r.d.Sync(richReq(model.StateWorking, "Reading", "", "secret reply text", "m"))
	r.clk.Advance(2 * time.Second)
	r.d.Step()
	if r.allSlotsEmpty(t) {
		t.Fatal("setup: nothing was written")
	}
	r.d.Shutdown()
	if !r.allSlotsEmpty(t) {
		t.Fatal("slots still hold data after Shutdown")
	}
	// The game is still running, but nothing may be published after shutdown.
	r.clk.Advance(2 * time.Second)
	r.d.Sync(richReq(model.StateIdle, "", "", "another secret", "m"))
	r.clk.Advance(2 * time.Second)
	r.d.HandleEvent(luaOpen(r.clk.Now()))
	r.d.Step()
	if !r.allSlotsEmpty(t) {
		t.Fatal("slots were rewritten after Shutdown")
	}
}

func TestStartupWithoutGameWipesLeftoverData(t *testing.T) {
	l := testenv.Install(t, 3, 5)
	if err := os.WriteFile(l.SlotData(1), []byte("OmpcraftInbox = { stale = \"left by a crash\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	New(Config{Layout: l, Log: logx.Discard(), FindGame: func() (int, error) { return 0, nil }})
	b, _ := os.ReadFile(l.SlotData(1))
	if string(b) != layout.EmptyData {
		t.Fatalf("slot 1 = %q", b)
	}
	// With the game running at startup nothing is wiped (the client may be reading).
	if err := os.WriteFile(l.SlotData(1), []byte("OmpcraftInbox = { keep = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	New(Config{Layout: l, Log: logx.Discard(), FindGame: func() (int, error) { return 99, nil }})
	if b, _ := os.ReadFile(l.SlotData(1)); !strings.Contains(string(b), "keep") {
		t.Fatalf("slot 1 wiped although the game runs: %q", b)
	}
}

func TestDetailOff(t *testing.T) {
	for _, noDetail := range []bool{false, true} {
		l := testenv.Install(t, 2, 5)
		clk := &clock{t: t0}
		d := New(Config{Layout: l, Version: "t", Log: logx.Discard(), Now: clk.Now, NoDetail: noDetail,
			FindGame: func() (int, error) { return 4242, nil }})
		r := richReq(model.StateWorking, "Reading", "1/5", "the reply text", "the last text\n·\n43.2s · 22K tok")
		d.Sync(r)
		b, err := os.ReadFile(l.SlotData(0))
		if err != nil {
			t.Fatal(err)
		}
		slot := string(b)
		rep := d.Report()
		if rep.DetailEnabled == noDetail {
			t.Fatalf("noDetail=%v: report says detailEnabled=%v", noDetail, rep.DetailEnabled)
		}
		// Everything else is kept either way.
		for _, want := range []string{`activity = "Reading",`, `todo = "1/5",`, `section = "demo-session`, `stats = "43s`, `name = "Demo task",`} {
			if !strings.Contains(slot, want) {
				t.Errorf("noDetail=%v: slot lacks %s", noDetail, want)
			}
		}
		hasText := strings.Contains(slot, "the reply text") || strings.Contains(slot, "the last text") ||
			strings.Contains(slot, "detail = {") || strings.Contains(slot, `preview = "the`) || strings.Contains(slot, `last = "the`)
		if noDetail {
			if hasText {
				t.Errorf("detail off, but the slot carries text:\n%s", slot)
			}
			if !strings.Contains(slot, `last = "",`) || !strings.Contains(slot, `preview = "",`) {
				t.Errorf("detail off: last and preview must be empty:\n%s", slot)
			}
			if a := rep.Snapshot.Agents[0]; a.Last != "" || a.Preview != "" || a.Detail != nil {
				t.Errorf("report snapshot leaks detail: %+v", a)
			}
		} else if !hasText {
			t.Errorf("detail on, but the slot carries no text:\n%s", slot)
		}
	}
}

func TestUpdateToastAndNotice(t *testing.T) {
	r := newRig(t, 2, 6, 4242, config.State{})
	r.d.AddToast(model.ToastUpdate, "ompcraft updated to 0.2.0. Type /reload to load it.")
	rep := r.d.Report()
	if len(rep.Snapshot.Toasts) != 1 {
		t.Fatalf("toasts = %+v", rep.Snapshot.Toasts)
	}
	ts := rep.Snapshot.Toasts[0]
	if ts.Kind != "update" || ts.Agent != "" || ts.ID < r.clk.Now().UnixMilli() {
		t.Errorf("toast = %+v", ts)
	}
	if d := r.slot(t, 0); !strings.Contains(d, `kind = "update",`) || !strings.Contains(d, `text = "ompcraft updated to 0.2.0. Type /reload to load it.",`) {
		t.Errorf("slot:\n%s", d)
	}
	// A second toast in the same millisecond still gets a larger id.
	r.d.AddToast(model.ToastUpdate, "again")
	if ts := r.d.Report().Snapshot.Toasts; len(ts) != 2 || ts[1].ID <= ts[0].ID {
		t.Errorf("toasts = %+v", ts)
	}

	if r.d.TakeNotice() != nil {
		t.Fatal("no notice queued yet")
	}
	r.d.QueueNotice(model.Notice{ID: "a", Text: "one"})
	r.d.QueueNotice(model.Notice{ID: "b", Text: "two"})
	if n := r.d.TakeNotice(); n == nil || n.ID != "a" {
		t.Fatalf("first = %+v", n)
	}
	if n := r.d.TakeNotice(); n == nil || n.ID != "b" {
		t.Fatalf("second = %+v", n)
	}
	if r.d.TakeNotice() != nil {
		t.Fatal("notices must be delivered once")
	}
	if got := r.d.Report().Update; got.Channel != "source" {
		t.Errorf("default update info = %+v", got)
	}
}
