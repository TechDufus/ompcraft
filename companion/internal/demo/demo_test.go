package demo

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/luatest"
	"github.com/TechDufus/ompcraft/companion/internal/model"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

type seen struct {
	snaps    []model.Snapshot
	frames   []*model.SyncRequest
	toasts   map[string]bool // kind
	toastTxt map[string]string
}

// playLoop feeds one whole loop (plus the start of the next) into a model with
// a fake clock, taking a snapshot after every sync, exactly as the daemon does.
func playLoop(t *testing.T) seen {
	t.Helper()
	m := model.New()
	s := seen{toasts: map[string]bool{}, toastTxt: map[string]string{}}
	for el := time.Duration(0); el <= Loop+5*time.Second; el += 500 * time.Millisecond {
		req := Frame(el)
		now := t0.Add(el)
		m.Apply(req, now)
		agents, toasts := m.View(now)
		snap := model.Snapshot{V: 0, Gen: int64(len(s.snaps) + 1), Run: 1, At: now.Unix(), Session: now.Unix(), NextSignal: 1, Tern: "ok",
			Companion: "0.1.0", Agents: agents, Toasts: toasts}
		s.snaps = append(s.snaps, snap)
		s.frames = append(s.frames, req)
		for _, ts := range toasts {
			s.toasts[ts.Kind] = true
			s.toastTxt[ts.Kind] = ts.Text
		}
	}
	return s
}

func TestFramesAreDeterministicAndLoop(t *testing.T) {
	a, _ := json.Marshal(Frame(37 * time.Second))
	b, _ := json.Marshal(Frame(37 * time.Second))
	c, _ := json.Marshal(Frame(37*time.Second + Loop))
	d, _ := json.Marshal(Frame(37*time.Second + 5*Loop))
	if string(a) != string(b) || string(a) != string(c) || string(a) != string(d) {
		t.Error("a frame must depend only on the position in the loop")
	}
	// All agents vanish at the end of each loop, so the next loop starts clean.
	for _, el := range []time.Duration{Loop - VanishFor, Loop - time.Second, Loop - 1} {
		if n := len(Frame(el).Agents); n != 0 {
			t.Errorf("Frame(%v) has %d agents, want none", el, n)
		}
	}
	if len(Frame(0).Agents) != len(agents) || len(agents) != 8 {
		t.Errorf("%d agents at loop start, want 8", len(Frame(0).Agents))
	}
}

func TestEveryUIStateAppearsInOneLoop(t *testing.T) {
	s := playLoop(t)

	var (
		workingWithActivity, waitingWithPreview, waitingWithDetail, finishedUnseen, failedIdle, exitedSeen, idleSeen bool
		maxAgents, maxDetail                                                                                         int
		activities                                                                                                   = map[string]bool{}
		todos                                                                                                        = map[string]bool{}
		sections                                                                                                     = map[string]bool{}
		maxWorking                                                                                                   int
	)
	for _, snap := range s.snaps {
		maxAgents = max(maxAgents, len(snap.Agents))
		details, working := 0, 0
		for _, a := range snap.Agents {
			sections[a.Section] = true
			if a.Detail != nil {
				details++
			}
			switch a.State {
			case "working":
				working++
				if a.Activity != "" && a.Todo != "" {
					workingWithActivity = true
					activities[a.Activity] = true
					todos[a.Todo] = true
				}
			case "waiting_input":
				if strings.HasPrefix(a.Preview, "Which structure should the 2.0 migration guide use?") && len(a.Preview) <= 140 {
					waitingWithPreview = true
				}
				if a.Detail != nil && len(a.Detail.Lines) > 0 && len(a.Detail.Steps) == 6 {
					waitingWithDetail = true
				}
			case "idle":
				idleSeen = true
				if a.DoneAt > 0 && a.Stats == "4m 12s · 38K tok · $0.61" && a.Detail != nil && len(a.Detail.Lines) > 0 {
					finishedUnseen = true
				}
				if a.DoneAt > 0 && strings.Contains(a.Last, "migration check failed") {
					failedIdle = true
				}
			case "exited":
				exitedSeen = true
			}
		}
		maxDetail = max(maxDetail, details)
		maxWorking = max(maxWorking, working)
		if details > 8 {
			t.Fatalf("%d agents carry detail; the cap is 8", details)
		}
	}
	for name, ok := range map[string]bool{
		"working agent with activity and todo":        workingWithActivity,
		"waiting_input with a question preview":       waitingWithPreview,
		"waiting_input with detail (lines + 6 steps)": waitingWithDetail,
		"finished agent: doneAt, stats, detail":       finishedUnseen,
		"idle agent after a failed turn":              failedIdle,
		"exited agent":                                exitedSeen,
		"idle agent":                                  idleSeen,
	} {
		if !ok {
			t.Errorf("the loop never shows: %s", name)
		}
	}
	if maxAgents != 8 {
		t.Errorf("at most %d agents at once, want 8", maxAgents)
	}
	if maxWorking < 2 {
		t.Errorf("at most %d working at once, want at least 2", maxWorking)
	}
	if len(activities) < 4 {
		t.Errorf("activities seen: %v (want them to cycle)", activities)
	}
	for _, want := range []string{"Reading the router middleware", "Running unit tests", "Editing rate_limit.go", "Checking the benchmark"} {
		if !activities[want] {
			t.Errorf("activity %q never shown", want)
		}
	}
	for _, want := range []string{"2/5", "3/5", "4/5"} {
		if !todos[want] {
			t.Errorf("todo %q never shown", want)
		}
	}
	wantSections := map[string]bool{"api-gateway": true, "docs-site": true, "infra · build-box": true}
	for sec := range sections {
		if !wantSections[sec] {
			t.Errorf("unexpected section %q", sec)
		}
	}
	for sec := range wantSections {
		if !sections[sec] {
			t.Errorf("missing section %q", sec)
		}
	}

	// Toasts: needs you, finished, failed, exited — each reached by a real transition.
	for _, kind := range []string{"input", "done", "failed", "exited"} {
		if !s.toasts[kind] {
			t.Errorf("no %q toast in a loop (got %v)", kind, s.toasts)
		}
	}
	if s.toastTxt["input"] != "Write the 2.0 migration guide needs you" || s.toastTxt["failed"] != "Upgrade the Postgres driver failed" ||
		s.toastTxt["done"] != "Migrate CI to the new runners finished" || s.toastTxt["exited"] != "Triage overnight alerts exited" {
		t.Errorf("toast texts = %v", s.toastTxt)
	}
}

func TestEachTransitionHappensOnceAndRepeatsEveryLoop(t *testing.T) {
	m := model.New()
	counts := map[string]int{}
	seenID := map[int64]bool{}
	for el := time.Duration(0); el < 3*Loop; el += 500 * time.Millisecond {
		now := t0.Add(el)
		m.Apply(Frame(el), now)
		_, toasts := m.View(now)
		for _, ts := range toasts {
			if !seenID[ts.ID] {
				seenID[ts.ID] = true
				counts[ts.Kind]++
			}
		}
	}
	// Failed amends the done toast of that agent in place, so it never counts as "done".
	want := map[string]int{"input": 3, "done": 3, "failed": 3, "exited": 3}
	if len(counts) != len(want) || counts["input"] != 3 || counts["done"] != 3 || counts["failed"] != 3 || counts["exited"] != 3 {
		t.Errorf("toasts over three loops = %v, want %v", counts, want)
	}
}

func TestDetailMarkdownHasEveryElement(t *testing.T) {
	s := playLoop(t)
	var snap model.Snapshot
	for _, sn := range s.snaps {
		for _, a := range sn.Agents {
			if a.State == "waiting_input" && a.Detail != nil && len(a.Detail.Lines) > 0 {
				snap = sn
			}
		}
	}
	if len(snap.Agents) == 0 {
		t.Fatal("no waiting agent with detail")
	}
	for _, a := range snap.Agents {
		if a.Detail == nil {
			continue
		}
		kinds := map[string]bool{}
		depth2 := false
		code := 0
		bold, inline := false, false
		for _, l := range a.Detail.Lines {
			kinds[l.K] = true
			if l.K == "li" && l.D == 2 {
				depth2 = true
			}
			if l.K == "code" {
				code++
			}
			if strings.Contains(l.T, "\x03") {
				bold = true
			}
			if strings.Contains(l.T, "\x01") {
				inline = true
			}
		}
		if a.State != "waiting_input" {
			continue
		}
		for _, k := range []string{"h", "p", "li", "code", "tr"} {
			if !kinds[k] {
				t.Errorf("%s: no %q line", a.Name, k)
			}
		}
		if !depth2 || code < 6 || !bold || !inline {
			t.Errorf("%s: nested=%v code lines=%d bold=%v inline=%v", a.Name, depth2, code, bold, inline)
		}
		trs := 0
		for _, l := range a.Detail.Lines {
			if l.K == "tr" {
				trs++
			}
		}
		if trs != 3 {
			t.Errorf("%s: %d table rows, want 3", a.Name, trs)
		}
		statuses := map[string]bool{}
		for _, st := range a.Detail.Steps {
			statuses[st.S] = true
		}
		if len(a.Detail.Steps) < 6 || !statuses["done"] || !statuses["error"] || !statuses["running"] {
			t.Errorf("%s: steps = %+v", a.Name, a.Detail.Steps)
		}
	}
}

func TestSnapshotsAreValidLuaAndRoundTrip(t *testing.T) {
	luatest.Available(t)
	s := playLoop(t)
	// A spread of moments: start, mid-loop states, the finished agent, the vanish window, next loop.
	for _, idx := range []int{0, 10, 30, 66, 100, 122, 140, 170} {
		snap := s.snaps[idx]
		data, err := snap.Lua()
		if err != nil {
			t.Fatalf("frame %d: %v", idx, err)
		}
		lines := luatest.Run(t, data) // executes the file in lua5.1: it must be literals only
		if len(lines) == 0 {
			t.Fatalf("frame %d: empty dump", idx)
		}
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, " T") {
			t.Errorf("frame %d: no tables in the dump", idx)
		}
	}
}

// ---- no real-looking personal data -------------------------------------------------

var (
	reIPv4    = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	reEmail   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+`)
	reURL     = regexp.MustCompile(`https?://|www\.|\.com\b|\.net\b|\.org\b|\.io\b|\.local\b`)
	reHomeBad = regexp.MustCompile(`/home/(?:[^u/]|u[^s/]|us[^e/]|use[^r/]|user[^/\s"])`) // anything but /home/user
	reMacBad  = regexp.MustCompile(`/Users/|C:\\|~/`)
)

func TestNoRealLookingPersonalData(t *testing.T) {
	forbidden := []string{"techdufus", "ompcraft-session", "password", "secret", "token", "api_key", "apikey", "bearer", "ssh-rsa", "BEGIN "}
	for el := time.Duration(0); el < Loop; el += time.Second {
		raw, err := json.Marshal(Frame(el))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		lower := strings.ToLower(text)
		for _, f := range forbidden {
			if strings.Contains(lower, strings.ToLower(f)) {
				t.Fatalf("t=%v: frame contains %q", el, f)
			}
		}
		for name, re := range map[string]*regexp.Regexp{"IPv4 address": reIPv4, "email": reEmail, "URL or domain": reURL, "non-placeholder home dir": reHomeBad, "other OS path": reMacBad} {
			if m := re.FindString(text); m != "" {
				t.Fatalf("t=%v: frame contains a %s: %q", el, name, m)
			}
		}
	}
	// Every path is under the placeholder /home/user, and every name comes from the invented list.
	okHost := map[string]bool{"this-pc": true, "build-box": true}
	for _, h := range hosts {
		if !okHost[h.Name] {
			t.Errorf("host %q is not an invented placeholder", h.Name)
		}
	}
	for _, a := range agents {
		if !strings.HasPrefix(a.cwd, "/home/user/") {
			t.Errorf("cwd %q", a.cwd)
		}
		if !strings.HasPrefix(a.pane, "91000000001") {
			t.Errorf("pane %q does not look invented", a.pane)
		}
	}
	allowedModels := map[string]bool{"Opus 5.5": true, "GPT-6 Astra": true, "Fable 5.2": true}
	for _, a := range agents {
		if !allowedModels[a.model] {
			t.Errorf("model %q", a.model)
		}
	}
	for _, s := range sessions {
		if !map[string]bool{"api-gateway": true, "docs-site": true, "infra": true}[s.Name] {
			t.Errorf("session %q", s.Name)
		}
	}
}
