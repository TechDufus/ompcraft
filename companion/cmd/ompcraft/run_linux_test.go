//go:build linux

package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/server"
	"github.com/TechDufus/ompcraft/companion/internal/testenv"
)

func TestRunCompanionWipesSlotsBeforeRemovingLinkOnShutdown(t *testing.T) {
	l := testenv.Install(t, 4, 6)
	game := filepath.Dir(filepath.Dir(l.Addons)) // G/Interface/AddOns -> G
	home := t.TempDir()
	env := config.EnvFrom(home, filepath.Join(home, "cfg"), filepath.Join(home, "state"))
	cfg := config.Config{Game: game}
	cfg.SetDetail(false) // also proves `run` reads the option
	if err := env.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- runCompanion(ctx, runOptions{
			Env: env, Game: game, Port: 0,
			FindGame: func() (int, error) { return 4242, nil },
			Ready:    func(url string) { ready <- url },
		})
	}()
	var url string
	select {
	case url = <-ready:
	case err := <-done:
		t.Fatalf("runCompanion returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not start")
	}

	link, err := env.ReadLink()
	if err != nil || link.URL != url || len(link.Token) != 64 {
		t.Fatalf("link.json = %+v, %v", link, err)
	}
	if st, err := os.Stat(env.LinkPath()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("link.json mode: %v %v", st, err)
	}

	body := `{"v":0,"window":"w","vm":"v","sessions":[{"id":"1","name":"demo-session"}],
	  "agents":[{"pane":"100","state":"working","title":"Demo","cwd":"/home/user/p","session":"1",
	  "last_message":"secret last text","detail":{"activity":"Reading","reply":"secret reply text"}}]}`
	req, _ := http.NewRequest(http.MethodPost, url+"/v1/sync", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+link.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("sync: %v %v", resp, err)
	}
	resp.Body.Close()

	// The agent change is published within a second (1 s coalescing after the
	// start-up publish). Poll the counter, not the slot: reading a slot would
	// look like the client loading it.
	waitUntil := time.Now().Add(5 * time.Second)
	for {
		st, err := server.FetchState(url, link.Token, 2*time.Second)
		if err == nil && st.Counters.Publishes >= 2 {
			break
		}
		if time.Now().After(waitUntil) {
			t.Fatalf("agent never published: %+v %v", st.Counters, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Detail is off in config.json: the slot has the agent but none of the text.
	b, err := os.ReadFile(l.SlotData(0))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, `name = "Demo"`) || !strings.Contains(s, `activity = "Reading"`) ||
		strings.Contains(s, "secret") || strings.Contains(s, "detail = {") {
		t.Fatalf("slot 0:\n%s", s)
	}
	st, err := server.FetchState(url, link.Token, 2*time.Second)
	if err != nil || st.DetailEnabled {
		t.Fatalf("state: detailEnabled=%v err=%v", st.DetailEnabled, err)
	}

	// Shutdown: slots are wiped before link.json is removed.
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(env.LinkPath()); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("link.json not removed after shutdown")
		}
		time.Sleep(time.Millisecond)
	}
	for i := range l.Slots {
		if b, _ := os.ReadFile(l.SlotData(i)); string(b) != layout.EmptyData {
			t.Fatalf("slot %d still holds data when link.json disappeared: %q", i, b)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runCompanion: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runCompanion did not return")
	}
}

func TestRunCompanionKeepsDetailByDefault(t *testing.T) {
	l := testenv.Install(t, 2, 4)
	game := filepath.Dir(filepath.Dir(l.Addons))
	home := t.TempDir()
	env := config.EnvFrom(home, filepath.Join(home, "cfg"), filepath.Join(home, "state"))
	if err := env.SaveConfig(config.Config{Game: game}); err != nil { // no "detail" key
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- runCompanion(ctx, runOptions{Env: env, Game: game, FindGame: func() (int, error) { return 4242, nil },
			Ready: func(url string) { ready <- url }})
	}()
	var url string
	select {
	case url = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not start")
	}
	link, _ := env.ReadLink()
	st, err := server.FetchState(url, link.Token, 2*time.Second)
	if err != nil || !st.DetailEnabled {
		t.Fatalf("detailEnabled=%v err=%v", st.DetailEnabled, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
