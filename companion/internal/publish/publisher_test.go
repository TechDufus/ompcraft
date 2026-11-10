package publish

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/model"
	"github.com/TechDufus/ompcraft/companion/internal/testenv"
)

// setup installs a small link (slots, signals) into a temp game dir.
func setup(t *testing.T, slots, signals int) layout.Layout {
	t.Helper()
	return testenv.Install(t, slots, signals)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func doorbells(t *testing.T, l layout.Layout) []string {
	t.Helper()
	entries, err := os.ReadDir(l.SignalsDir())
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

func slotData(t *testing.T, l layout.Layout, i int) string {
	t.Helper()
	b, err := os.ReadFile(l.SlotData(i))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPublishWritesNonLoadedSlotsAndRingsOneDoorbell(t *testing.T) {
	l := setup(t, 5, 8)
	p := NewPublisher(l, nil)
	if _, err := p.ClearDoorbells(); err != nil { // as after a new launch
		t.Fatal(err)
	}
	if got := doorbells(t, l); len(got) != 0 {
		t.Fatalf("doorbells left after clear: %v", got)
	}
	if !exists(filepath.Join(l.SignalsDir(), layout.CtlPresent)) {
		t.Fatal("ctl_present.wav must survive ClearDoorbells")
	}

	var order []string
	p.onWrite = func(path string) { order = append(order, strings.TrimPrefix(path, l.Addons+string(os.PathSeparator))) }
	p.MarkLoaded(1) // the client loads in order, so slots 0 and 1 are both used

	snap := &model.Snapshot{Run: 5, Tern: "ok", Companion: "0.1.0", Gen: 1}
	if err := p.Publish(snap); err != nil {
		t.Fatal(err)
	}
	if snap.NextSignal != 1 {
		t.Fatalf("nextSignal = %d, want signalNext+1 = 1", snap.NextSignal)
	}
	wantOrder := []string{
		filepath.Join("OmpcraftS002", "data.lua"), filepath.Join("OmpcraftS003", "data.lua"),
		filepath.Join("OmpcraftS004", "data.lua"), filepath.Join("OmpcraftSignals", "0000.wav"),
	}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("write order = %v\nwant %v (slots first, doorbell last)", order, wantOrder)
	}
	for _, i := range []int{2, 3, 4} {
		if d := slotData(t, l, i); !strings.Contains(d, "nextSignal = 1,") || !strings.HasPrefix(d, "OmpcraftInbox = {") {
			t.Errorf("slot %d data.lua:\n%s", i, d)
		}
	}
	for _, i := range []int{0, 1} {
		if d := slotData(t, l, i); d != layout.EmptyData {
			t.Errorf("loaded slot %d was overwritten:\n%s", i, d)
		}
	}
	if got := doorbells(t, l); !reflect.DeepEqual(got, []string{"0000.wav"}) {
		t.Fatalf("doorbells = %v, want exactly 0000.wav", got)
	}
	if p.SignalNext() != 1 {
		t.Fatalf("signalNext = %d", p.SignalNext())
	}
	for i := range 5 {
		if exists(filepath.Join(l.SlotDir(i), layout.DataTmp)) {
			t.Errorf("slot %d left a data.lua.tmp", i)
		}
	}

	// Second publish: the doorbell rung is the one the previous snapshot announced.
	order = nil
	if err := p.Publish(&model.Snapshot{Gen: 2}); err != nil {
		t.Fatal(err)
	}
	if got := doorbells(t, l); !reflect.DeepEqual(got, []string{"0000.wav", "0001.wav"}) {
		t.Fatalf("doorbells = %v", got)
	}
	if d := slotData(t, l, 2); !strings.Contains(d, "nextSignal = 2,") {
		t.Errorf("second snapshot must announce doorbell 1:\n%s", d)
	}
	st := p.Stats()
	if st.Publishes != 2 || st.SlotWrites != 6 || st.Doorbells != 2 || st.WriteErrs != 0 {
		t.Fatalf("stats = %+v", st)
	}

	// New UI session: loaded set cleared, all slots are written again.
	p.ClearLoaded()
	if err := p.Publish(&model.Snapshot{Gen: 3}); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if d := slotData(t, l, i); !strings.Contains(d, "gen = 3,") {
			t.Errorf("slot %d not refreshed after ClearLoaded", i)
		}
	}
}

func TestDoorbellsExhausted(t *testing.T) {
	l := setup(t, 2, 2)
	var logs []string
	p := NewPublisher(l, func(f string, a ...any) { logs = append(logs, f) })
	_, _ = p.ClearDoorbells()
	for i := 1; i <= 4; i++ {
		if err := p.Publish(&model.Snapshot{Gen: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if got := doorbells(t, l); !reflect.DeepEqual(got, []string{"0000.wav", "0001.wav"}) {
		t.Fatalf("doorbells = %v", got)
	}
	if p.SignalNext() != 2 {
		t.Fatalf("signalNext = %d, must stop at M", p.SignalNext())
	}
	if n := len(logs); n != 1 || !strings.Contains(logs[0], "doorbells used") {
		t.Fatalf("exhaustion must be logged once, got %v", logs)
	}
	// Snapshots still reach the slots.
	if d := slotData(t, l, 0); !strings.Contains(d, "gen = 4,") {
		t.Fatalf("slot not updated after exhaustion:\n%s", d)
	}
}

func TestEnsureDoorbells(t *testing.T) {
	l := setup(t, 1, 4)
	p := NewPublisher(l, nil)
	_, _ = p.ClearDoorbells()
	_ = os.Remove(filepath.Join(l.SignalsDir(), layout.CtlPresent))
	n, err := p.EnsureDoorbells()
	if err != nil || n != 5 {
		t.Fatalf("created %d, %v; want 4 doorbells + ctl_present", n, err)
	}
	if got := doorbells(t, l); len(got) != 4 {
		t.Fatalf("doorbells = %v", got)
	}
	if n, _ := p.EnsureDoorbells(); n != 0 {
		t.Fatalf("second call created %d", n)
	}
	b, _ := os.ReadFile(l.Doorbell(2))
	if string(b) != string(layout.SilentWAV()) {
		t.Fatal("recreated doorbell is not the silent WAV")
	}
}

func TestSessionDetectorQuietRule(t *testing.T) {
	var d SessionDetector
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	steps := []struct {
		at   time.Duration
		want bool
	}{
		{0, true},                             // first ever open
		{100 * time.Millisecond, false},       // burst of addon files
		{900 * time.Millisecond, false},       // still within 2 s of the previous open
		{2800 * time.Millisecond, false},      // 1.9 s after the last open: not quiet enough
		{4800 * time.Millisecond, true},       // exactly 2 s of quiet
		{10 * time.Second, true},              // a /reload later
		{10*time.Second + time.Second, false}, // its burst
	}
	for i, s := range steps {
		if got := d.LuaOpen(t0.Add(s.at)); got != s.want {
			t.Errorf("step %d at %v: got %v want %v", i, s.at, got, s.want)
		}
	}
}

func TestSlotWriteWindow(t *testing.T) {
	l := setup(t, 20, 4)
	p := NewPublisher(l, nil)
	has := func(i int, gen int64) bool {
		return strings.Contains(slotData(t, l, i), "gen = "+strconv.FormatInt(gen, 10)+",")
	}
	publish := func(gen int64) {
		t.Helper()
		if err := p.Publish(&model.Snapshot{Gen: gen}); err != nil {
			t.Fatal(err)
		}
	}

	// No slot open seen yet: every non-loaded slot is written.
	publish(1)
	for i := range 20 {
		if !has(i, 1) {
			t.Fatalf("slot %d missing gen 1 before any slot open", i)
		}
	}

	// Slots 0 and 1 got loaded: only the 8 lowest non-loaded slots (2..9) are rewritten.
	p.MarkLoaded(0)
	p.MarkLoaded(1)
	publish(2)
	for i := 2; i <= 9; i++ {
		if !has(i, 2) {
			t.Errorf("slot %d should be in the write window", i)
		}
	}
	for i := 10; i < 20; i++ {
		if !has(i, 1) {
			t.Errorf("slot %d is outside the window and must keep gen 1", i)
		}
	}
	if st := p.Stats(); st.SlotWrites != 20+8 {
		t.Fatalf("slot writes = %d, want 28", st.SlotWrites)
	}

	// The window slides with the loaded set.
	p.MarkLoaded(2)
	publish(3)
	if !has(10, 3) || has(11, 3) {
		t.Fatal("window did not slide to slots 3..10")
	}

	// A new UI session: loaded set empty, every slot is written again.
	p.ClearLoaded()
	publish(4)
	for i := range 20 {
		if !has(i, 4) {
			t.Fatalf("slot %d not written after ClearLoaded", i)
		}
	}

	// An out-of-range slot open is ignored and does not start the window.
	if p.MarkLoaded(99) {
		t.Fatal("slot 99 accepted")
	}
	publish(5)
	if !has(19, 5) {
		t.Fatal("bogus slot open narrowed the window")
	}
}

// A companion restarted in the middle of a UI session starts with an empty
// loaded set while the client has already used the low slots. The first slot
// open it sees must move the write window above that slot, or the client keeps
// loading slots that never get fresh data.
func TestSlotWindowAfterMidSessionRestart(t *testing.T) {
	l := setup(t, 40, 4)
	p := NewPublisher(l, nil)
	if err := p.Publish(&model.Snapshot{Gen: 1}); err != nil {
		t.Fatal(err)
	}
	p.MarkLoaded(25) // the client had loaded 0..24 before the restart
	if err := p.Publish(&model.Snapshot{Gen: 2}); err != nil {
		t.Fatal(err)
	}
	if got := p.LoadedSlots(); len(got) != 26 || got[0] != 0 || got[25] != 25 {
		t.Fatalf("loaded = %v, want 0..25", got)
	}
	if !strings.Contains(slotData(t, l, 26), "gen = 2,") {
		t.Fatal("slot 26, the client's next load, did not get the new snapshot")
	}
	if strings.Contains(slotData(t, l, 0), "gen = 2,") {
		t.Fatal("slot 0 was rewritten although the client already used it")
	}
}

func TestWipeSlots(t *testing.T) {
	l := setup(t, 6, 4)
	p := NewPublisher(l, nil)
	p.MarkLoaded(2) // loaded slots are wiped too
	if err := p.Publish(&model.Snapshot{Gen: 1, Tern: "ok"}); err != nil {
		t.Fatal(err)
	}
	if slotData(t, l, 3) == layout.EmptyData {
		t.Fatal("setup: nothing written")
	}
	if err := os.WriteFile(l.SlotData(2), []byte("OmpcraftInbox = { stale = true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := p.WipeSlots()
	if err != nil || n != 6 {
		t.Fatalf("wiped %d, %v", n, err)
	}
	for i := range 6 {
		if d := slotData(t, l, i); d != layout.EmptyData {
			t.Errorf("slot %d = %q", i, d)
		}
		if exists(filepath.Join(l.SlotDir(i), layout.DataTmp)) {
			t.Errorf("slot %d left a tmp file", i)
		}
		toc, _ := os.ReadFile(filepath.Join(l.SlotDir(i), layout.SlotName(i)+".toc"))
		if string(toc) != layout.SlotTOC(i) {
			t.Errorf("slot %d toc changed", i)
		}
	}
	if st := p.Stats(); st.Wipes != 1 {
		t.Fatalf("stats = %+v", st)
	}
}
