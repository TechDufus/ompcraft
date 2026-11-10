//go:build linux

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/logx"
	"github.com/TechDufus/ompcraft/companion/internal/testenv"
)

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func openClose(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// TestRunWithRealInotify drives the daemon with a real watcher: the test plays
// the game client by opening files, with a fake pid source for launches.
func TestRunWithRealInotify(t *testing.T) {
	l := testenv.Install(t, 6, 30)
	g := &game{pid: 4100}
	logs := &syncBuf{}
	ready := make(chan struct{})
	d := New(Config{
		Layout:     l,
		Version:    "0.1.0-test",
		Log:        logx.New(logs, true),
		FindGame:   g.Find,
		SaveState:  func(config.State) error { return nil },
		WatchReady: func() { close(ready) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop")
		}
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher not ready")
	}
	core := filepath.Join(l.AddonDir(), "Core", "Init.lua")
	ui := filepath.Join(l.AddonDir(), "UI", "Panel.lua")

	// Client starts the UI: first addon .lua open = UI session start + new launch.
	openClose(t, core)
	openClose(t, ui)
	waitFor(t, "first UI session start", func() bool { return d.Report().Counters.SessionStarts == 1 })
	rep := d.Report()
	if rep.Session == 0 || rep.Counters.NewLaunches != 1 {
		t.Fatalf("report: %+v", rep.Counters)
	}
	if got := mustDoorbells(t, d); !reflect.DeepEqual(got, []string{"0000.wav"}) {
		t.Fatalf("doorbells after new launch = %v", got)
	}

	// Client loads slot 4: it and every slot below are marked loaded (the client
	// loads in order), and later publishes skip them.
	openClose(t, l.SlotData(4))
	waitFor(t, "slot 4 loaded", func() bool { return reflect.DeepEqual(d.Report().LoadedSlots, []int{0, 1, 2, 3, 4}) })
	before, _ := os.ReadFile(l.SlotData(4))
	d.Sync(syncReq("working"))
	time.Sleep(1100 * time.Millisecond)
	d.Step()
	after, _ := os.ReadFile(l.SlotData(4))
	if string(before) != string(after) {
		t.Fatal("a slot the client already loaded was rewritten")
	}
	if cur, _ := os.ReadFile(l.SlotData(5)); !strings.Contains(string(cur), `name = "Demo task"`) {
		t.Fatalf("unloaded slot 5 did not get the agents:\n%s", cur)
	}

	// /reload more than 2 s later: second session, same launch (same pid).
	time.Sleep(2100 * time.Millisecond)
	openClose(t, core)
	waitFor(t, "second UI session start", func() bool { return d.Report().Counters.SessionStarts == 2 })
	rep = d.Report()
	if rep.Counters.NewLaunches != 1 || len(rep.LoadedSlots) != 0 {
		t.Fatalf("reload misdetected: %+v loaded=%v", rep.Counters, rep.LoadedSlots)
	}
	if got := mustDoorbells(t, d); len(got) < 2 {
		t.Fatalf("a reload must keep the doorbells, got %v", got)
	}

	// The game restarts under another pid: new launch, doorbells cleared again.
	time.Sleep(2100 * time.Millisecond)
	g.Set(4200)
	openClose(t, core)
	waitFor(t, "third UI session start", func() bool { return d.Report().Counters.SessionStarts == 3 })
	rep = d.Report()
	if rep.Counters.NewLaunches != 2 || rep.GamePid != 4200 {
		t.Fatalf("relaunch misdetected: %+v pid=%d", rep.Counters, rep.GamePid)
	}
	if got := mustDoorbells(t, d); !reflect.DeepEqual(got, []string{"0000.wav"}) {
		t.Fatalf("doorbells after relaunch = %v", got)
	}

	// Every open was logged with a timestamp at -v.
	out := logs.String()
	for _, want := range []string{"IN_OPEN Ompcraft/Core/Init.lua at ", "IN_OPEN Ompcraft/UI/Panel.lua at ", "IN_OPEN OmpcraftS004/data.lua at ",
		"UI session start at ", "new launch (pid 4100): "} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q", want)
		}
	}
	// slot_open: the explicit open of slot 4, plus the test's own reads of slot 4 (twice) and slot 0,
	// which are opens like any other.
	if c := d.Report().Counters.Inotify; c["lua_open"] != 4 || c["slot_open"] != 4 {
		t.Errorf("inotify counters = %v", c)
	}
}

func mustDoorbells(t *testing.T, d *Daemon) []string {
	t.Helper()
	entries, err := os.ReadDir(d.cfg.Layout.SignalsDir())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if n := e.Name(); len(n) == 8 && strings.HasSuffix(n, ".wav") && n[0] >= '0' && n[0] <= '9' {
			out = append(out, n)
		}
	}
	return out
}
