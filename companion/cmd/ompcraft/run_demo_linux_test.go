//go:build linux

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/layout"
)

func TestDemoFeedsTheSlotsWithoutHTTPOrLinkJSON(t *testing.T) {
	w := newRunWorld(t)
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.1.0", Channel: "source", Demo: true,
		FindGame: func() (int, error) { return 4242, nil },
	}
	_, done, cancel := startRun(t, o)

	// No link.json: the real plugin sees "companion not running".
	if exists(w.env.LinkPath()) {
		t.Fatal("demo mode wrote link.json")
	}
	// Wait for the first publish without opening the slot (an open would look like the client loading it).
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := os.Stat(w.l.SlotData(0))
		if err == nil && st.Size() > int64(len(layout.EmptyData)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the demo never published")
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, err := os.ReadFile(w.l.SlotData(0))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`name = "Add rate limiting to the public API"`, `name = "Write the 2.0 migration guide"`, `name = "Triage overnight alerts"`,
		`section = "api-gateway"`, `section = "docs-site"`, `tern = "ok"`, `model = "Opus 5.5"`, `model = "GPT-6 Astra"`, `model = "Fable 5.2"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("slot lacks %s", want)
		}
	}
	if strings.Count(s, "      id = \"91000000001") != 8 {
		t.Errorf("want 8 invented agents:\n%s", s)
	}
	if exists(w.env.LinkPath()) {
		t.Error("link.json appeared while the demo runs")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for i := range w.l.Slots {
		if b, _ := os.ReadFile(w.l.SlotData(i)); string(b) != layout.EmptyData {
			t.Errorf("slot %d keeps demo data after the demo stopped", i)
		}
	}
	if exists(w.env.LinkPath()) {
		t.Error("link.json after the demo")
	}
}

func TestDemoRefusesWhileTheRealCompanionAnswers(t *testing.T) {
	w := newRunWorld(t)
	real := runOptions{Env: w.env, Game: w.game, Version: "0.1.0", Channel: "source", FindGame: func() (int, error) { return 0, nil }}
	_, done, cancel := startRun(t, real)
	defer func() { cancel(); <-done }()
	link, err := w.env.ReadLink()
	if err != nil {
		t.Fatal(err)
	}

	err = runCompanion(context.Background(), runOptions{Env: w.env, Game: w.game, Version: "0.1.0", Channel: "source", Demo: true,
		FindGame: func() (int, error) { return 0, nil }})
	if err == nil || !strings.Contains(err.Error(), "the real companion is running") || !strings.Contains(err.Error(), "systemctl --user stop ompcraft") {
		t.Fatalf("err = %v", err)
	}
	// The real companion's link is untouched.
	if again, err := w.env.ReadLink(); err != nil || again != link {
		t.Errorf("link.json changed: %+v %v", again, err)
	}
}

func TestDemoCommandIsHidden(t *testing.T) {
	if strings.Contains(usage, "demo") {
		t.Error("the demo command must not be in --help")
	}
}
