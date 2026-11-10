//go:build linux

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/server"
	"github.com/TechDufus/ompcraft/companion/internal/testenv"
)

// noTern is a Runner for a machine without tern.
type noTern struct{}

func (noTern) LookPath(string) (string, error) { return "", errors.New("not found") }
func (noTern) Run(context.Context, string, ...string) (string, error) {
	return "", errors.New("no commands expected")
}

type runWorld struct {
	l    layout.Layout
	env  config.Env
	game string
}

func newRunWorld(t *testing.T) runWorld {
	t.Helper()
	l := testenv.Install(t, 3, 6)
	game := filepath.Dir(filepath.Dir(l.Addons))
	home := t.TempDir()
	env := config.EnvFrom(home, filepath.Join(home, "cfg"), filepath.Join(home, "state")).WithDataHome(filepath.Join(home, "data"))
	if err := env.SaveConfig(config.Config{Game: game}); err != nil {
		t.Fatal(err)
	}
	return runWorld{l: l, env: env, game: game}
}

func startRun(t *testing.T, o runOptions) (url string, done chan error, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	o.Ready = func(u string) { ready <- u }
	done = make(chan error, 1)
	go func() { done <- runCompanion(ctx, o) }()
	select {
	case url = <-ready:
	case err := <-done:
		t.Fatalf("runCompanion returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not start")
	}
	return url, done, cancel
}

func postSync(t *testing.T, url, token string) string {
	t.Helper()
	body := `{"v":0,"window":"w","vm":"v","agents":[]}`
	req, _ := http.NewRequest(http.MethodPost, url+"/v1/sync", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("sync: %v %v", resp, err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func gameIs(pid *atomic.Int64) func() (int, error) {
	return func() (int, error) { return int(pid.Load()), nil }
}

// holdLongWaits makes the scheduler's second wait (6 h) block until shutdown.
func holdLongWaits(ctx context.Context, d time.Duration) error {
	if d > 10*time.Minute {
		<-ctx.Done()
	}
	return ctx.Err()
}

func TestAutoUpdateWithNoGameAppliesShutsDownThenReexecs(t *testing.T) {
	w := newRunWorld(t)
	rel := newFakeRelease(t, "v0.2.0", []byte("NEW BINARY"))
	u, exe := updaterFor(t, rel, "0.1.0")

	var waits []time.Duration
	var execPath string
	var execArgs []string
	var linkGoneAtExec, slotsEmptyAtExec, binaryNewAtExec bool
	var pendingAtExec config.State
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.1.0", Channel: "release",
		FindGame: func() (int, error) { return 0, nil },
		Updater:  u,
		UpdateAfter: func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			return ctx.Err()
		},
		Exe: exe, Args: []string{"ompcraft", "run", "-v"}, Environ: []string{"X=1"},
		Exec: func(path string, argv, env []string) error {
			execPath, execArgs = path, argv
			_, err := os.Stat(w.env.LinkPath())
			linkGoneAtExec = os.IsNotExist(err)
			slotsEmptyAtExec = true
			for i := range w.l.Slots {
				if b, _ := os.ReadFile(w.l.SlotData(i)); string(b) != layout.EmptyData {
					slotsEmptyAtExec = false
				}
			}
			b, _ := os.ReadFile(path)
			binaryNewAtExec = string(b) == "NEW BINARY"
			pendingAtExec = w.env.LoadState()
			return nil
		},
	}
	_, done, cancel := startRun(t, o)
	defer cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runCompanion: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the daemon never updated and re-executed")
	}
	if execPath != exe || strings.Join(execArgs, " ") != "ompcraft run -v" {
		t.Errorf("exec(%q, %v)", execPath, execArgs)
	}
	if !binaryNewAtExec {
		t.Error("the binary was not replaced before the re-exec")
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "OLD" {
		t.Errorf("the previous binary was not kept: %q", b)
	}
	if !linkGoneAtExec || !slotsEmptyAtExec {
		t.Errorf("the normal shutdown path must run before the re-exec: link removed=%v slots wiped=%v", linkGoneAtExec, slotsEmptyAtExec)
	}
	if pendingAtExec.PendingVersion != "0.2.0" || pendingAtExec.StartAttempts != 0 || pendingAtExec.PreviousVersion != "0.1.0" {
		t.Errorf("state at re-exec = %+v", pendingAtExec)
	}
	if len(waits) != 1 || waits[0] < 108*time.Second || waits[0] > 132*time.Second {
		t.Errorf("first check delay = %v, want 2 min ±10%%", waits)
	}
}

func TestAutoUpdateNeverReplacesTheBinaryWhileTheGameRuns(t *testing.T) {
	w := newRunWorld(t)
	rel := newFakeRelease(t, "v0.2.0", []byte("NEW BINARY"))
	u, exe := updaterFor(t, rel, "0.1.0")
	var pid atomic.Int64
	pid.Store(4242)

	execCalled := make(chan string, 1)
	var execState config.State
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.1.0", Channel: "release",
		FindGame: gameIs(&pid), Updater: u, UpdateAfter: holdLongWaits,
		Exe: exe, Args: []string{"ompcraft", "run"},
		Exec: func(path string, argv, env []string) error {
			b, _ := os.ReadFile(path)
			execState = w.env.LoadState()
			execCalled <- string(b)
			return nil
		},
	}
	url, done, cancel := startRun(t, o)
	defer cancel()
	link, _ := w.env.ReadLink()

	// The first check downloads and verifies, then waits.
	var st server.StateResponse
	deadline := time.Now().Add(8 * time.Second)
	for {
		var err error
		st, err = server.FetchState(url, link.Token, 2*time.Second)
		if err == nil && st.Update.Staged == "0.2.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing was staged: %+v %v", st.Update, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" || exists(exe+".old") {
		t.Fatal("the executable changed while the game runs")
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), ".ompcraft.staged")); string(b) != "NEW BINARY" {
		t.Fatalf("staged binary = %q", b)
	}
	if got := w.env.LoadState().StagedVersion; got != "0.2.0" {
		t.Errorf("state.json stagedVersion = %q", got)
	}
	select {
	case <-execCalled:
		t.Fatal("re-executed while the game runs")
	case <-time.After(300 * time.Millisecond):
	}

	// The one-shot Tern notice says it is ready and when it installs.
	resp := postSync(t, url, link.Token)
	want := `"notice":{"id":"update-ready-0.2.0","level":"info","text":"ompcraft 0.2.0 is ready","sub":"It installs after you quit WoW."}`
	if !strings.Contains(resp, want) {
		t.Fatalf("sync response: %s", resp)
	}
	if second := postSync(t, url, link.Token); strings.Contains(second, "notice") {
		t.Errorf("notice repeated: %s", second)
	}

	// The game exits: the daemon's exit handler installs the staged binary and re-execs.
	pid.Store(0)
	select {
	case content := <-execCalled:
		if content != "NEW BINARY" {
			t.Errorf("exec'd binary = %q", content)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the staged binary was not installed after the game exited")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "OLD" {
		t.Errorf(".old = %q", b)
	}
	if execState.PendingVersion != "0.2.0" || execState.StagedVersion != "" || execState.PreviousVersion != "0.1.0" {
		t.Errorf("state at re-exec = %+v", execState)
	}
}

func TestAutoUpdateStatusAndGate(t *testing.T) {
	w := newRunWorld(t)
	rel := newFakeRelease(t, "v0.1.0", []byte("SAME")) // already current
	u, exe := updaterFor(t, rel, "0.1.0")
	execCalled := false
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.1.0", Channel: "release",
		FindGame: func() (int, error) { return 4242, nil },
		Updater:  u, UpdateAfter: holdLongWaits,
		Exe: exe, Exec: func(string, []string, []string) error { execCalled = true; return nil },
	}
	url, done, cancel := startRun(t, o)
	link, _ := w.env.ReadLink()
	var st server.StateResponse
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		st, err = server.FetchState(url, link.Token, 2*time.Second)
		if err == nil && st.Update.LastCheck > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no update check recorded: %+v %v", st.Update, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Update.Channel != "release" || !st.Update.Autoupdate || st.Update.Latest != "0.1.0" || st.Update.LastError != "" || st.Update.Staged != "" {
		t.Errorf("update = %+v", st.Update)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if execCalled {
		t.Error("an up-to-date daemon must not re-exec")
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Error("the binary changed")
	}
}

func TestAutoUpdateIsDisabledWithoutAReleaseKey(t *testing.T) {
	w := newRunWorld(t)
	rel := newFakeRelease(t, "v0.2.0", []byte("NEW BINARY"))
	u, exe := updaterFor(t, rel, "0.1.0")
	u.PublicKey = "" // as in a build that embeds no key yet
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.1.0", Channel: "release",
		FindGame: func() (int, error) { return 0, nil },
		Updater:  u, UpdateAfter: func(ctx context.Context, d time.Duration) error { return nil },
		Exe: exe, Exec: func(string, []string, []string) error { t.Error("re-exec without a key"); return nil },
	}
	url, done, cancel := startRun(t, o)
	link, _ := w.env.ReadLink()
	time.Sleep(300 * time.Millisecond) // a scheduled check would have happened by now
	st, err := server.FetchState(url, link.Token, 2*time.Second)
	if err != nil || st.Update.LastCheck != 0 {
		t.Errorf("update = %+v %v", st.Update, err)
	}
	cancel()
	<-done
	if b, _ := os.ReadFile(exe); string(b) != "OLD" || rel.hits.Load() != 0 {
		t.Error("an update was attempted without a release key")
	}
	logb, _ := os.ReadFile(w.env.LogPath())
	if !strings.Contains(string(logb), "update: this build has no release key; updates are disabled") {
		t.Errorf("log:\n%s", logb)
	}
}

func TestStartRollsBackAfterThreeFailedStarts(t *testing.T) {
	w := newRunWorld(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "ompcraft")
	_ = os.WriteFile(exe, []byte("BAD 0.2.0"), 0o755)
	_ = os.WriteFile(exe+".old", []byte("GOOD 0.1.0"), 0o755)
	if err := w.env.UpdateState(func(s *config.State) { s.PendingVersion, s.StartAttempts, s.PreviousVersion = "0.2.0", 3, "0.1.0" }); err != nil {
		t.Fatal(err)
	}
	var execPath string
	var execContent string
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.2.0", Channel: "release",
		FindGame: func() (int, error) { return 0, nil },
		Exe:      exe, Args: []string{"ompcraft", "run"},
		Exec: func(path string, argv, env []string) error {
			execPath = path
			b, _ := os.ReadFile(path)
			execContent = string(b)
			return nil
		},
	}
	if err := runCompanion(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if execPath != exe || execContent != "GOOD 0.1.0" {
		t.Errorf("exec(%q) content %q: the old binary must be restored and re-executed", execPath, execContent)
	}
	st := w.env.LoadState()
	if st.BadVersion != "0.2.0" || st.PendingVersion != "" {
		t.Errorf("state = %+v", st)
	}
	if exists(w.env.LinkPath()) {
		t.Error("a rolled-back start must not serve")
	}
	logb, _ := os.ReadFile(w.env.LogPath())
	if !strings.Contains(string(logb), "update: 0.2.0 failed to start 3 times; rolled back to 0.1.0") {
		t.Errorf("log:\n%s", logb)
	}
}

func TestEachStartCountsAndServingForThirtySecondsClearsPending(t *testing.T) {
	w := newRunWorld(t)
	exe := filepath.Join(t.TempDir(), "ompcraft")
	_ = os.WriteFile(exe, []byte("NEW"), 0o755)
	if err := w.env.UpdateState(func(s *config.State) { s.PendingVersion, s.StartAttempts, s.PreviousVersion = "0.2.0", 0, "0.1.0" }); err != nil {
		t.Fatal(err)
	}
	var waited []time.Duration
	release := make(chan struct{})
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.2.0", Channel: "release",
		FindGame: func() (int, error) { return 0, nil }, Exe: exe,
		StableAfter: func(ctx context.Context, d time.Duration) error {
			waited = append(waited, d)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	_, done, cancel := startRun(t, o)
	// Serving, but not for 30 s yet: still pending, one start counted.
	if st := w.env.LoadState(); st.PendingVersion != "0.2.0" || st.StartAttempts != 1 {
		t.Fatalf("state = %+v", st)
	}
	close(release) // 30 s passed
	deadline := time.Now().Add(3 * time.Second)
	for w.env.LoadState().PendingVersion != "" {
		if time.Now().After(deadline) {
			t.Fatalf("pending not cleared: %+v", w.env.LoadState())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := w.env.LoadState(); st.StartAttempts != 0 || st.PreviousVersion != "" {
		t.Errorf("state = %+v", st)
	}
	if len(waited) != 1 || waited[0] != 30*time.Second {
		t.Errorf("waited %v, want one 30 s wait", waited)
	}
	cancel()
	<-done

	// A process killed before 30 s leaves the pending record for the next start to count.
	_ = w.env.UpdateState(func(s *config.State) { s.PendingVersion, s.StartAttempts, s.PreviousVersion = "0.2.0", 1, "0.1.0" })
	o.StableAfter = func(ctx context.Context, d time.Duration) error { <-ctx.Done(); return ctx.Err() }
	_, done, cancel = startRun(t, o)
	cancel()
	<-done
	if st := w.env.LoadState(); st.PendingVersion != "0.2.0" || st.StartAttempts != 2 {
		t.Errorf("state after a short run = %+v", st)
	}
}

func TestNoUpdaterMeansNoChecks(t *testing.T) {
	w := newRunWorld(t)
	for _, tc := range []struct{ channel, cfgAuto, wantChannel string }{{"source", "", "source"}, {"release", "off", "release"}} {
		cfg := config.Config{Game: w.game}
		if tc.cfgAuto == "off" {
			cfg.SetAutoupdate(false)
		}
		if err := w.env.SaveConfig(cfg); err != nil {
			t.Fatal(err)
		}
		// cmdRun leaves Updater nil for these; runCompanion then never schedules a check.
		o := runOptions{Env: w.env, Game: w.game, Version: "0.1.0", Channel: tc.channel,
			FindGame: func() (int, error) { return 4242, nil }}
		url, done, cancel := startRun(t, o)
		link, _ := w.env.ReadLink()
		st, err := server.FetchState(url, link.Token, 2*time.Second)
		if err != nil || st.Update.Channel != tc.wantChannel || st.Update.LastCheck != 0 || st.Update.Autoupdate == (tc.cfgAuto == "off") {
			t.Errorf("%+v: update = %+v %v", tc, st.Update, err)
		}
		cancel()
		<-done
	}
}

func healedAddon() fstest.MapFS {
	return fstest.MapFS{
		"Ompcraft.toc":  {Data: []byte("## Interface: 16001\n## Version: 0.2.0\nCore/Init.lua\n")},
		"Core/Init.lua": {Data: []byte("-- the new addon\n")},
	}
}

func TestSelfHealWhileTheGameRunsToastsAndNotifiesOnce(t *testing.T) {
	w := newRunWorld(t)
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.2.0", Channel: "source",
		FindGame: func() (int, error) { return 4242, nil },
		Addon:    healedAddon(), Plugin: fstest.MapFS{"plugin.toml": {Data: []byte("x")}}, Runner: noTern{},
	}
	url, done, cancel := startRun(t, o)
	defer func() { cancel(); <-done }()
	link, _ := w.env.ReadLink()

	// The installed addon was replaced by content, atomically.
	if b, _ := os.ReadFile(filepath.Join(w.l.AddonDir(), "Core", "Init.lua")); string(b) != "-- the new addon\n" {
		t.Fatalf("addon not healed: %q", b)
	}
	if exists(filepath.Join(w.l.Addons, layout.AddonTmp)) {
		t.Error("temp folder left behind")
	}

	// The snapshot carries an update toast.
	st, err := server.FetchState(url, link.Token, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ts := range st.Snapshot.Toasts {
		if ts.Kind == "update" && ts.Text == "ompcraft updated to 0.2.0. Type /reload to load it." && ts.Agent == "" && ts.ID > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("toasts = %+v", st.Snapshot.Toasts)
	}

	// The notice arrives with the first leader sync, and only once.
	first := postSync(t, url, link.Token)
	want := `"notice":{"id":"update-0.2.0","level":"info","text":"ompcraft updated to 0.2.0","sub":"Type /reload in WoW to load the new addon."}`
	if !strings.Contains(first, want) {
		t.Fatalf("first sync response: %s", first)
	}
	if second := postSync(t, url, link.Token); strings.Contains(second, "notice") {
		t.Fatalf("second sync response repeats the notice: %s", second)
	}
}

func TestSelfHealWithTheGameClosedHasNoToast(t *testing.T) {
	w := newRunWorld(t)
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.2.0", Channel: "source",
		FindGame: func() (int, error) { return 0, nil },
		Addon:    healedAddon(), Plugin: fstest.MapFS{"plugin.toml": {Data: []byte("x")}}, Runner: noTern{},
	}
	url, done, cancel := startRun(t, o)
	defer func() { cancel(); <-done }()
	link, _ := w.env.ReadLink()
	if b, _ := os.ReadFile(filepath.Join(w.l.AddonDir(), "Core", "Init.lua")); string(b) != "-- the new addon\n" {
		t.Fatalf("addon not healed: %q", b)
	}
	st, _ := server.FetchState(url, link.Token, 2*time.Second)
	for _, ts := range st.Snapshot.Toasts {
		if ts.Kind == "update" {
			t.Errorf("an update toast with the game closed: %+v", ts)
		}
	}
	if resp := postSync(t, url, link.Token); !strings.Contains(resp, "The new addon loads the next time you start WoW.") {
		t.Errorf("sync response: %s", resp)
	}
}

func TestSelfHealDoesNothingWhenTheAddonMatches(t *testing.T) {
	w := newRunWorld(t)
	same := fstest.MapFS{}
	for _, f := range []string{"Ompcraft.toc", "Core/Init.lua", "Core/Util.lua", "UI/Panel.lua", "UI/Deep/A.lua"} {
		b, err := os.ReadFile(filepath.Join(w.l.AddonDir(), filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		same[f] = &fstest.MapFile{Data: b}
	}
	o := runOptions{
		Env: w.env, Game: w.game, Version: "0.2.0", Channel: "source",
		FindGame: func() (int, error) { return 4242, nil },
		Addon:    same, Plugin: fstest.MapFS{}, Runner: noTern{},
	}
	url, done, cancel := startRun(t, o)
	defer func() { cancel(); <-done }()
	link, _ := w.env.ReadLink()
	st, _ := server.FetchState(url, link.Token, 2*time.Second)
	if len(st.Snapshot.Toasts) != 0 {
		t.Errorf("toasts = %+v", st.Snapshot.Toasts)
	}
	if resp := postSync(t, url, link.Token); strings.Contains(resp, "notice") {
		t.Errorf("notice without an update: %s", resp)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
