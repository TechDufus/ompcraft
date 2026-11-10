package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/TechDufus/ompcraft/companion/internal/luatest"
)

func TestClean(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello world", "hello world"},
		{"private use BMP", "a\ue000b\uf8ffc", "abc"},
		{"just above private use kept", "a\uf900b", "a\uf900b"},
		{"private use plane 15/16", "x\U000F0000y\U0010FFFFz", "xyz"},
		{"astral emoji", "ok 🙂 done 𝒳", "ok done"},
		{"nerd font glyph", "\uf120 omp", "omp"},
		{"C0 controls dropped", "a\x00b\x01c\x1bd\x7fe", "abcde"},
		{"tab and CR are whitespace", "a\tb\r\nc", "a b\nc"},
		{"collapse spaces", "  a   b \t c  ", "a b c"},
		{"collapse blank lines", "a\n\n\n b \n\n", "a\nb"},
		{"nbsp and line separator", "a\u00a0\u00a0b\u2028c", "a b\nc"},
		{"invalid utf8", "a\xffb\xc3", "ab"},
		{"only junk", "\ue000\x01 \n", ""},
		{"BMP non-latin kept", "日本語 é ñ", "日本語 é ñ"},
	}
	for _, c := range cases {
		if got := Clean(c.in); got != c.want {
			t.Errorf("%s: Clean(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestLast(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"joins lines", "line one\nline two\n\nline three", "line one line two line three"},
		{"cuts at footer", "answer text\nmore\n·\nfooter 1.2s\nmore footer", "answer text more"},
		{"footer with blank lines around", "answer\n\n·\n\nfooter", "answer"},
		{"trailing footer marker", "answer\n·\n", "answer"},
		{"dot inside a line is not a footer", "a · b\nc", "a · b c"},
		{"strips glyphs", "done \uf00c \U0001F680 ok", "done ok"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := Last(c.in); got != c.want {
			t.Errorf("%s: Last(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestLastTruncation(t *testing.T) {
	// ASCII: exactly 280 bytes survive, then the ellipsis.
	got := Last(strings.Repeat("a", 500))
	if want := strings.Repeat("a", 280) + "…"; got != want {
		t.Errorf("ASCII truncation: len=%d", len(got))
	}
	if got := Last(strings.Repeat("a", 280)); got != strings.Repeat("a", 280) {
		t.Errorf("exactly 280 bytes must not be cut, got len %d", len(got))
	}
	// 3-byte runes: byte 280 falls inside a rune, so the cut backs up to 279.
	got = Last(strings.Repeat("日", 100))
	if want := strings.Repeat("日", 93) + "…"; got != want {
		t.Errorf("UTF-8 boundary truncation: got %d bytes %q", len(got), got)
	}
	if !utf8.ValidString(got) {
		t.Error("truncation produced invalid UTF-8")
	}
	// 2-byte runes land exactly on 280.
	if got, want := Last(strings.Repeat("é", 200)), strings.Repeat("é", 140)+"…"; got != want {
		t.Errorf("2-byte truncation: got %d bytes", len(got))
	}
	// A trailing space before the cut does not precede the ellipsis.
	if got := Last(strings.Repeat("a", 279) + " bcd"); got != strings.Repeat("a", 279)+"…" {
		t.Errorf("got %q", got)
	}
}

func TestName(t *testing.T) {
	cases := []struct{ title, cwd, want string }{
		{"Fix the parser", "/home/user/project", "Fix the parser"},
		{"", "/home/user/project", "project"},
		{"omp", "/home/user/project/", "project"},
		{"zsh", `C:\Users\demo\work`, "work"},
		{"  \uf120 refactor\nthing  ", "/x", "refactor thing"},
		{"", "/", "agent"},
		{"", "", "agent"},
		{"omp", "", "omp"},
		{"Omp", "/home/user/p", "Omp"}, // exact match only
	}
	for _, c := range cases {
		if got := Name(c.title, c.cwd); got != c.want {
			t.Errorf("Name(%q,%q) = %q, want %q", c.title, c.cwd, got, c.want)
		}
	}
}

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func req(agents ...SyncAgent) *SyncRequest {
	return &SyncRequest{Agents: agents}
}

func ag(pane, state, title string) SyncAgent {
	return SyncAgent{Pane: FlexID(pane), State: state, Title: title, Cwd: "/home/user/project"}
}

func kinds(ts []Toast) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Kind+":"+t.Text)
	}
	return out
}

func TestToastTransitions(t *testing.T) {
	m := New()
	m.Apply(req(ag("1", StateWorking, "Alpha"), ag("2", StateIdle, "Beta"), ag("3", StateIdle, "")), t0)
	if _, ts := m.View(t0); len(ts) != 0 {
		t.Fatalf("first sight must not toast, got %v", kinds(ts))
	}

	// working -> idle: done. idle -> waiting_input: input. idle -> exited: exited.
	t1 := t0.Add(2 * time.Second)
	m.Apply(req(ag("1", StateIdle, "Alpha"), ag("2", StateWaiting, "Beta"), ag("3", StateExited, "")), t1)
	_, ts := m.View(t1)
	want := []string{"done:Alpha finished", "input:Beta needs you", "exited:project exited"}
	if got := kinds(ts); !reflect.DeepEqual(got, want) {
		t.Fatalf("toasts = %v, want %v", got, want)
	}
	for i := 1; i < len(ts); i++ {
		if ts[i].ID <= ts[i-1].ID {
			t.Fatalf("toast ids not strictly increasing: %d then %d", ts[i-1].ID, ts[i].ID)
		}
	}
	if ts[0].ID < t1.UnixMilli() {
		t.Fatalf("toast id %d is before now %d", ts[0].ID, t1.UnixMilli())
	}
	if ts[0].Agent != "1" {
		t.Fatalf("toast agent = %q", ts[0].Agent)
	}

	// Unchanged state: no new toasts. working (idle->working) never toasts.
	t2 := t1.Add(time.Second)
	m.Apply(req(ag("1", StateWorking, "Alpha"), ag("2", StateWaiting, "Beta"), ag("3", StateExited, "")), t2)
	if _, ts := m.View(t2); len(ts) != 3 {
		t.Fatalf("expected no new toast, got %v", kinds(ts))
	}

	// waiting_input -> exited toasts as exited; waiting -> working does not toast.
	t3 := t2.Add(time.Second)
	m.Apply(req(ag("1", StateWorking, "Alpha"), ag("2", StateExited, "Beta")), t3)
	_, ts = m.View(t3)
	if got := kinds(ts); got[len(got)-1] != "exited:Beta exited" || len(got) != 4 {
		t.Fatalf("toasts = %v", got)
	}
}

func TestFailedWithinFiveSeconds(t *testing.T) {
	failedEvt := SyncEvent{T: "finished", Pane: "1", Result: "failed"}

	// Same sync as the transition.
	m := New()
	m.Apply(req(ag("1", StateWorking, "Alpha")), t0)
	r := req(ag("1", StateIdle, "Alpha"))
	r.Events = []SyncEvent{failedEvt}
	m.Apply(r, t0.Add(time.Second))
	if _, ts := m.View(t0.Add(time.Second)); !reflect.DeepEqual(kinds(ts), []string{"failed:Alpha failed"}) {
		t.Fatalf("same-sync failed: %v", kinds(ts))
	}

	// Event arrives in a later sync, 4 s after the transition: still failed.
	m = New()
	m.Apply(req(ag("1", StateWorking, "Alpha")), t0)
	m.Apply(req(ag("1", StateIdle, "Alpha")), t0.Add(time.Second))
	r = req(ag("1", StateIdle, "Alpha"))
	r.Events = []SyncEvent{failedEvt}
	m.Apply(r, t0.Add(5*time.Second)) // exactly 4 s later
	if _, ts := m.View(t0.Add(5 * time.Second)); !reflect.DeepEqual(kinds(ts), []string{"failed:Alpha failed"}) {
		t.Fatalf("late failed within 5s: %v", kinds(ts))
	}

	// More than 5 s after the transition: stays done.
	m = New()
	m.Apply(req(ag("1", StateWorking, "Alpha")), t0)
	m.Apply(req(ag("1", StateIdle, "Alpha")), t0.Add(time.Second))
	r = req(ag("1", StateIdle, "Alpha"))
	r.Events = []SyncEvent{failedEvt}
	m.Apply(r, t0.Add(7*time.Second))
	if _, ts := m.View(t0.Add(7 * time.Second)); !reflect.DeepEqual(kinds(ts), []string{"done:Alpha finished"}) {
		t.Fatalf("late failed after 5s: %v", kinds(ts))
	}

	// A successful finished event, or one for another pane, changes nothing.
	m = New()
	m.Apply(req(ag("1", StateWorking, "Alpha")), t0)
	r = req(ag("1", StateIdle, "Alpha"))
	r.Events = []SyncEvent{{T: "finished", Pane: "1", Result: "done"}, {T: "finished", Pane: "9", Result: "failed"}}
	m.Apply(r, t0.Add(time.Second))
	if _, ts := m.View(t0.Add(time.Second)); !reflect.DeepEqual(kinds(ts), []string{"done:Alpha finished"}) {
		t.Fatalf("got %v", kinds(ts))
	}
}

func TestToastRetention(t *testing.T) {
	m := New()
	m.Apply(req(ag("1", StateIdle, "A")), t0)
	now := t0
	for i := range 30 { // 30 toasts, alternating working/idle every second
		now = now.Add(time.Second)
		st := StateWorking
		if i%2 == 1 {
			st = StateIdle
		}
		m.Apply(req(ag("1", st, "A")), now)
	}
	_, ts := m.View(now)
	// 29 transitions, but only the newest 20 are kept; here only idle ones toast (15).
	if len(ts) > ToastMax {
		t.Fatalf("kept %d toasts", len(ts))
	}

	// Cap at 20 with many agents flipping at once.
	m = New()
	var work, idle []SyncAgent
	for i := range 25 {
		id := string(rune('a' + i))
		work = append(work, ag(id, StateWorking, "n"+id))
		idle = append(idle, ag(id, StateIdle, "n"+id))
	}
	m.Apply(req(work...), t0)
	m.Apply(req(idle...), t0.Add(time.Second))
	_, ts = m.View(t0.Add(time.Second))
	if len(ts) != 20 || ts[19].Agent != "y" {
		t.Fatalf("want newest 20 toasts, got %d (last agent %q)", len(ts), ts[len(ts)-1].Agent)
	}
	for i := 1; i < len(ts); i++ {
		if ts[i].ID <= ts[i-1].ID {
			t.Fatal("ids must stay strictly increasing within one millisecond")
		}
	}

	// Older than 10 minutes: dropped.
	_, ts = m.View(t0.Add(time.Second + ToastMaxAge - time.Millisecond))
	if len(ts) != 20 {
		t.Fatalf("toasts dropped too early: %d", len(ts))
	}
	_, ts = m.View(t0.Add(time.Second + ToastMaxAge))
	if len(ts) != 0 {
		t.Fatalf("toasts not dropped after 10 min: %d", len(ts))
	}
}

func TestSinceAndOrder(t *testing.T) {
	m := New()
	m.Apply(req(ag("a", StateIdle, "A"), ag("b", StateWorking, "B"), ag("c", StateWorking, "C")), t0)
	t1 := t0.Add(10 * time.Second)
	m.Apply(req(ag("a", StateIdle, "A"), ag("b", StateIdle, "B"), ag("c", StateWorking, "C"),
		ag("d", StateExited, "D"), ag("e", StateWaiting, "E")), t1)
	agents, _ := m.View(t1)
	var ids []string
	for _, a := range agents {
		ids = append(ids, a.ID)
	}
	// waiting_input, working, idle (b changed state at t1, so it is newer than a), exited.
	if want := []string{"e", "c", "b", "a", "d"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("order = %v, want %v", ids, want)
	}
	byID := map[string]AgentView{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	// since 0 = state start not observed: first sight, and unchanged state since then.
	for _, id := range []string{"a", "c", "d", "e"} {
		if byID[id].Since != 0 {
			t.Errorf("agent %s: since = %d, want 0 (no transition observed)", id, byID[id].Since)
		}
	}
	if byID["b"].Since != t1.Unix() {
		t.Errorf("b: since = %d, want the observed transition time %d", byID["b"].Since, t1.Unix())
	}

	// A later transition sets since; a vanished agent is forgotten.
	t2 := t1.Add(5 * time.Second)
	m.Apply(req(ag("a", StateWorking, "A")), t2)
	agents, _ = m.View(t2)
	if len(agents) != 1 || agents[0].Since != t2.Unix() {
		t.Fatalf("agents = %+v", agents)
	}
	// A reappearing agent is a first sight again.
	m.Apply(req(), t2)
	m.Apply(req(ag("a", StateWorking, "A")), t2.Add(time.Second))
	if agents, _ = m.View(t2); agents[0].Since != 0 {
		t.Fatalf("reappeared agent since = %d", agents[0].Since)
	}
}

func TestSyncDecodingAndResolution(t *testing.T) {
	body := `{
	  "v": 0, "window": "w", "vm": "x",
	  "hosts": [{"id": 0, "name": "host-a", "kind": "local", "state": "connected"},
	            {"id": 1, "name": "host-b\u001b", "kind": "remote", "state": null}],
	  "sessions": [{"id": "12884901894", "name": "demo-session", "host": 0},
	               {"id": 4538784184008707, "name": "second", "host": 1}],
	  "agents": [
	    {"pane": "7881264988162", "state": "working", "title": "T", "cwd": "/home/user/project",
	     "model": "m\ue000odel", "host": 0, "session": "12884901894", "last_message": "hi\n·\nfooter", "delivery_error": null},
	    {"pane": 4538784184008707, "state": "idle", "title": null, "cwd": null, "model": null,
	     "host": 1, "session": 4538784184008707, "last_message": null}
	  ],
	  "events": [{"t": "finished", "pane": 7881264988162, "result": "failed"}]
	}`
	var r SyncRequest
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if r.Agents[1].Pane != "4538784184008707" || r.Events[0].Pane != "7881264988162" {
		t.Fatalf("numeric ids must keep their exact decimal text: %+v", r)
	}
	m := New()
	m.Apply(&r, t0)
	agents, _ := m.View(t0)
	byID := map[string]AgentView{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	a := byID["7881264988162"]
	// Host "host-a" is kind "local": blank. Since is 0: first sight.
	want := AgentView{ID: "7881264988162", Name: "T", Host: "", Session: "demo-session", State: "working",
		Since: 0, Model: "model", Cwd: "project", Last: "hi", Section: "demo-session", Preview: "hi"}
	if a != want {
		t.Fatalf("agent = %+v\nwant    %+v", a, want)
	}
	b := byID["4538784184008707"]
	if b.Host != "host-b" || b.Session != "second" || b.Name != "agent" || b.Last != "" {
		t.Fatalf("agent b = %+v", b)
	}
}

func TestSnapshotLuaRoundTripAndContentKey(t *testing.T) {
	luatest.Available(t)
	m := New()
	m.Apply(req(SyncAgent{Pane: "7881264988162", State: StateWaiting,
		Title: "evil \"|cffff0000red|r\" \\ 日本 \x01", Cwd: "/home/user/proj|x",
		LastMessage: "line one\nline two"}), t0)
	m.Apply(req(SyncAgent{Pane: "7881264988162", State: StateExited, Title: "evil \"|cffff0000red|r\" \\ 日本 \x01",
		Cwd: "/home/user/proj|x", LastMessage: "line one\nline two"}), t0.Add(time.Second))
	agents, toasts := m.View(t0.Add(time.Second))
	snap := Snapshot{V: 0, Gen: 17, Run: 1762727000123, At: 1762728000, Session: 1762727900,
		NextSignal: 5, Tern: "ok", Companion: "0.1.0", Agents: agents, Toasts: toasts}
	data, err := snap.Lua()
	if err != nil {
		t.Fatal(err)
	}
	a := agents[0]
	tt := toasts[0]
	want := luatest.Expect(map[string]any{
		"v": 0, "gen": 17, "run": int64(1762727000123), "at": int64(1762728000), "session": int64(1762727900),
		"nextSignal": 5, "tern": "ok", "companion": "0.1.0",
		"agents": []any{map[string]any{"id": a.ID, "name": a.Name, "host": a.Host, "session": a.Session,
			"state": a.State, "since": a.Since, "model": a.Model, "cwd": a.Cwd, "last": a.Last,
			"section": a.Section, "stats": a.Stats, "doneAt": a.DoneAt, "preview": a.Preview}},
		"toasts": []any{map[string]any{"id": tt.ID, "kind": tt.Kind, "agent": tt.Agent, "text": tt.Text}},
	})
	if got := luatest.Run(t, data); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch\n got %v\nwant %v\nlua:\n%s", got, want, data)
	}

	k1, _ := snap.ContentKey()
	snap.Gen, snap.At, snap.NextSignal = 99, 1, 77
	k2, _ := snap.ContentKey()
	if k1 != k2 {
		t.Error("content key must ignore gen, at and nextSignal")
	}
	snap.Tern = "down"
	if k3, _ := snap.ContentKey(); k3 == k1 {
		t.Error("content key must change with other fields")
	}
}

func TestImportantKeyIgnoresCosmeticFields(t *testing.T) {
	base := Snapshot{Tern: "ok", Companion: "0.1.0",
		Agents: []AgentView{{ID: "1", Name: "A", Host: "h", Session: "s", State: StateWorking, Since: 5, Model: "m", Cwd: "c", Last: "x"}},
		Toasts: []Toast{{ID: 1, Kind: ToastDone, Agent: "1", Text: "A finished"}}}
	baseKey, _ := base.ImportantKey()

	same := map[string]func(s *Snapshot){
		"name":    func(s *Snapshot) { s.Agents[0].Name = "B" },
		"host":    func(s *Snapshot) { s.Agents[0].Host = "h2" },
		"session": func(s *Snapshot) { s.Agents[0].Session = "s2" },
		"model":   func(s *Snapshot) { s.Agents[0].Model = "m2" },
		"cwd":     func(s *Snapshot) { s.Agents[0].Cwd = "c2" },
		"last":    func(s *Snapshot) { s.Agents[0].Last = "streaming..." },
		"gen/at":  func(s *Snapshot) { s.Gen, s.At, s.NextSignal = 9, 9, 9 },
	}
	for name, mut := range same {
		s := base
		s.Agents = append([]AgentView(nil), base.Agents...)
		mut(&s)
		if k, _ := s.ImportantKey(); k != baseKey {
			t.Errorf("%s change must be cosmetic", name)
		}
		if k, _ := s.ContentKey(); name != "gen/at" && k == mustKey(base) {
			t.Errorf("%s change must still change the full content key", name)
		}
	}
	diff := map[string]func(s *Snapshot){
		"state":       func(s *Snapshot) { s.Agents[0].State = StateIdle },
		"agent added": func(s *Snapshot) { s.Agents = append(s.Agents, AgentView{ID: "2", State: StateIdle}) },
		"agent gone":  func(s *Snapshot) { s.Agents = nil },
		"toast added": func(s *Snapshot) {
			s.Toasts = append(s.Toasts, Toast{ID: 2, Kind: ToastInput, Agent: "1", Text: "A needs you"})
		},
		"toast kind": func(s *Snapshot) { s.Toasts = []Toast{{ID: 1, Kind: ToastFailed, Agent: "1", Text: "A failed"}} },
		"tern":       func(s *Snapshot) { s.Tern = "down" },
		"session":    func(s *Snapshot) { s.Session = 123 },
	}
	for name, mut := range diff {
		s := base
		s.Agents = append([]AgentView(nil), base.Agents...)
		s.Toasts = append([]Toast(nil), base.Toasts...)
		mut(&s)
		if k, _ := s.ImportantKey(); k == baseKey {
			t.Errorf("%s change must be important", name)
		}
	}
}

func mustKey(s Snapshot) string {
	k, err := s.ContentKey()
	if err != nil {
		panic(err)
	}
	return k
}
