package model

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/luatest"
	"github.com/TechDufus/ompcraft/companion/internal/markdown"
)

func TestFormatDuration(t *testing.T) {
	cases := map[string]string{
		"43.2s": "43s", "43.5s": "44s", "0.4s": "0s", "59.6s": "1m 0s", "72s": "1m 12s", "1m 12s": "1m 12s",
		"1m12s": "1m 12s", "2m": "2m 0s", "1h 4m 3s": "1h 4m", "1h": "1h 0m", "3723s": "1h 2m", "850ms": "1s", "120ms": "0s",
	}
	for in, want := range cases {
		if got, ok := FormatDuration(in); !ok || got != want {
			t.Errorf("FormatDuration(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "tok", "22K tok", "$1.09", "1.09", "abc", "s", "this turn"} {
		if got, ok := FormatDuration(in); ok {
			t.Errorf("FormatDuration(%q) = %q, want not a duration", in, got)
		}
	}
}

func TestStats(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"omp footer", "All done.\n·\n43.2s · 22K tok · $1.09 this turn", "43s · 22K tok · $1.09"},
		{"minutes", "ok\n·\n1m 12s · 22K tok · $0.42 this turn", "1m 12s · 22K tok · $0.42"},
		{"hours", "ok\n·\n1h 4m 3s · 1.2M tok · $14.20 this turn", "1h 4m · 1.2M tok · $14.20"},
		{"CRLF and padding", "ok\r\n  ·  \r\n 43.2s · 22K tok · $1.09 this turn \r\n", "43s · 22K tok · $1.09"},
		{"duration only", "ok\n·\n5s", "5s"},
		{"cost only", "ok\n·\n$0.10 this turn", "$0.10"},
		{"tokens only", "ok\n·\n22K tok", "22K tok"},
		{"missing duration", "ok\n·\n22K tok · $0.42 this turn", "22K tok · $0.42"},
		{"short unknown part kept next to known ones", "ok\n·\n43.2s · 3 tools · $1.09", "43s · 3 tools · $1.09"},
		{"long unknown part dropped", "ok\n·\n43.2s · some very long explanation of things · $1.09", "43s · $1.09"},
		{"empty parts", "ok\n·\n43.2s ·  · · $1.09", "43s · $1.09"},
		{"no footer", "just an answer", ""},
		{"empty", "", ""},
		{"separator but empty footer", "answer\n·\n", ""},
		{"separator inside the answer, prose after it", "a\n·\nand then some prose follows here that is long", ""},
		{"only this turn", "ok\n·\nthis turn", ""},
		{"only the first separator splits", "x\n·\n43.2s · $1.09\n·\nmore", "43s · $1.09 · more"},
	}
	for _, c := range cases {
		if got := Stats(c.in); got != c.want {
			t.Errorf("%s: Stats(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func agD(pane, state, title string, d *SyncDetail) SyncAgent {
	a := ag(pane, state, title)
	a.Detail = d
	return a
}

func viewByID(t *testing.T, m *Model, now time.Time) map[string]AgentView {
	t.Helper()
	agents, _ := m.View(now)
	out := map[string]AgentView{}
	for _, a := range agents {
		out[a.ID] = a
	}
	return out
}

func TestRichFields(t *testing.T) {
	r := &SyncRequest{
		Hosts:    []SyncHost{{ID: "0", Name: "host-a", Kind: "local"}, {ID: "1", Name: "host-b", Kind: "remote"}},
		Sessions: []SyncSession{{ID: "10", Name: "demo-session"}, {ID: "11", Name: "second"}},
		Agents: []SyncAgent{
			{Pane: "1", State: StateWorking, Title: "A", Cwd: "/home/user/p", Host: "0", Session: "10",
				LastMessage: "streaming...", Detail: &SyncDetail{Activity: "Running tests", Todo: "3/7",
					Reply: "## Plan\n\nFirst **thing**.\n\n- a\n- b\n"}},
			{Pane: "2", State: StateIdle, Title: "B", Cwd: "/home/user/p", Host: "1", Session: "11",
				LastMessage: "Fixed the `parser` bug.\n·\n1m 12s · 22K tok · $0.42 this turn",
				Detail:      &SyncDetail{Activity: "stale activity", Todo: "7/7"}},
			{Pane: "3", State: StateIdle, Title: "C", Host: "1", Session: "", LastMessage: "plain **reply** with a [link](http://x.y)"},
		},
	}
	m := New()
	m.Apply(r, t0)
	v := viewByID(t, m, t0)

	a := v["1"]
	if a.Section != "demo-session" || a.Activity != "Running tests" || a.Todo != "3/7" || a.Stats != "" || a.DoneAt != 0 {
		t.Errorf("working agent: %+v", a)
	}
	if a.Preview != "Plan First thing. a b" {
		t.Errorf("preview from reply = %q", a.Preview)
	}
	b := v["2"]
	if b.Section != "second · host-b" {
		t.Errorf("remote section = %q", b.Section)
	}
	if b.Activity != "" {
		t.Errorf("activity must be empty unless working, got %q", b.Activity)
	}
	if b.Todo != "7/7" {
		t.Errorf("todo = %q", b.Todo)
	}
	if b.Stats != "1m 12s · 22K tok · $0.42" {
		t.Errorf("stats = %q", b.Stats)
	}
	if b.Preview != "Fixed the parser bug." {
		t.Errorf("preview from last = %q (the footer must not leak in)", b.Preview)
	}
	if c := v["3"]; c.Section != "host-b" || c.Preview != "plain reply with a link" || c.Stats != "" {
		t.Errorf("agent 3: %+v", c)
	}
	if v["3"].Detail != nil || v["2"].Detail != nil {
		t.Error("unexpected detail")
	}

	// Long reply: preview is one line of at most 140 bytes.
	long := &SyncDetail{Reply: strings.Repeat("日本語 words ", 60)}
	m.Apply(req(agD("1", StateWorking, "A", long)), t0.Add(time.Second))
	if p := viewByID(t, m, t0)["1"].Preview; len(p) > 140 || strings.Contains(p, "\n") || !strings.HasSuffix(p, "…") {
		t.Errorf("long preview: %d bytes %q", len(p), p)
	}
	// Activity is capped.
	m.Apply(req(agD("1", StateWorking, "A", &SyncDetail{Activity: strings.Repeat("x", 500)})), t0.Add(2*time.Second))
	if a := viewByID(t, m, t0)["1"]; len(a.Activity) > activityMax {
		t.Errorf("activity = %d bytes", len(a.Activity))
	}
}

func TestDoneAt(t *testing.T) {
	m := New()
	m.Apply(req(ag("a", StateWorking, "A"), ag("b", StateIdle, "B"), ag("c", StateWorking, "C"), ag("d", StateWorking, "D"),
		ag("e", StateIdle, "E")), t0)
	if d := viewByID(t, m, t0); d["a"].DoneAt != 0 || d["b"].DoneAt != 0 {
		t.Fatal("doneAt must be 0 on first sight")
	}
	t1 := t0.Add(10 * time.Second)
	m.Apply(req(ag("a", StateIdle, "A"), ag("b", StateIdle, "B"), ag("c", StateWaiting, "C"), ag("d", StateExited, "D"),
		ag("e", StateWorking, "E")), t1)
	d := viewByID(t, m, t1)
	for _, id := range []string{"a", "c", "d"} {
		if d[id].DoneAt != t1.Unix() {
			t.Errorf("%s: doneAt = %d, want %d (working -> %s)", id, d[id].DoneAt, t1.Unix(), d[id].State)
		}
	}
	if d["b"].DoneAt != 0 {
		t.Error("b never worked while observed: doneAt must stay 0")
	}
	if d["e"].DoneAt != 0 {
		t.Error("idle -> working is not a finish")
	}

	// doneAt survives unchanged states and non-finishing transitions; a new finish replaces it.
	t2 := t1.Add(10 * time.Second)
	m.Apply(req(ag("a", StateIdle, "A"), ag("c", StateWorking, "C"), ag("e", StateIdle, "E")), t2)
	d = viewByID(t, m, t2)
	if d["a"].DoneAt != t1.Unix() || d["c"].DoneAt != t1.Unix() {
		t.Errorf("doneAt not preserved: a=%d c=%d", d["a"].DoneAt, d["c"].DoneAt)
	}
	if d["e"].DoneAt != t2.Unix() {
		t.Errorf("e finished at t2: doneAt = %d", d["e"].DoneAt)
	}
	t3 := t2.Add(10 * time.Second)
	m.Apply(req(ag("c", StateIdle, "C")), t3)
	if got := viewByID(t, m, t3)["c"].DoneAt; got != t3.Unix() {
		t.Errorf("second finish: doneAt = %d, want %d", got, t3.Unix())
	}
}

func steps(n int) []SyncStep {
	var out []SyncStep
	for i := 1; i <= n; i++ {
		out = append(out, SyncStep{Name: "bash", Text: "step " + strconv.Itoa(i), Status: "done"})
	}
	return out
}

func detailIDs(t *testing.T, m *Model, now time.Time) []string {
	t.Helper()
	agents, _ := m.View(now)
	var ids []string
	for _, a := range agents {
		if a.Detail != nil {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

func TestDetailSelection(t *testing.T) {
	d := &SyncDetail{Reply: "text", Steps: steps(3)}

	// 3 waiting + 7 working: only 8 carry detail, waiting first.
	m := New()
	var agents []SyncAgent
	for i := range 3 {
		agents = append(agents, agD("w"+strconv.Itoa(i), StateWaiting, "W", d))
	}
	for i := range 7 {
		agents = append(agents, agD("k"+strconv.Itoa(i), StateWorking, "K", d))
	}
	m.Apply(req(agents...), t0)
	ids := detailIDs(t, m, t0)
	if len(ids) != 8 {
		t.Fatalf("%d agents carry detail, want 8: %v", len(ids), ids)
	}
	for _, want := range []string{"w0", "w1", "w2"} {
		if !contains(ids, want) {
			t.Errorf("waiting agent %s lacks detail", want)
		}
	}

	// 1 waiting + 1 working + 6 finished: the 4 most recent finished ones join.
	m = New()
	var first, fin []SyncAgent
	first = append(first, agD("w", StateWaiting, "W", d), agD("k", StateWorking, "K", d))
	for i := range 6 {
		id := "f" + strconv.Itoa(i)
		first = append(first, agD(id, StateWorking, id, d))
		fin = append(fin, agD(id, StateIdle, id, d))
	}
	m.Apply(req(first...), t0)
	// f0..f5 finish one second apart, f5 last.
	var cur []SyncAgent
	cur = append(cur, agD("w", StateWaiting, "W", d), agD("k", StateWorking, "K", d))
	for i := range 6 {
		id := "f" + strconv.Itoa(i)
		cur = append(cur, agD(id, StateWorking, id, d))
	}
	for i := range 6 {
		cur[2+i] = fin[i]
		m.Apply(req(cur...), t0.Add(time.Duration(i+1)*time.Second))
	}
	got := detailIDs(t, m, t0.Add(10*time.Second))
	want := []string{"w", "k", "f2", "f3", "f4", "f5"}
	if len(got) != len(want) {
		t.Fatalf("detail ids = %v, want %v (any order)", got, want)
	}
	for _, id := range want {
		if !contains(got, id) {
			t.Errorf("%s lacks detail; got %v", id, got)
		}
	}

	// Agents the plugin sent no detail for never carry one, and do not use up the budget.
	m = New()
	m.Apply(req(ag("n1", StateWaiting, "N"), agD("d1", StateWaiting, "D", d)), t0)
	if got := detailIDs(t, m, t0); !reflect.DeepEqual(got, []string{"d1"}) {
		t.Errorf("detail ids = %v", got)
	}
	// An idle agent that never finished while observed (doneAt 0) gets none.
	m.Apply(req(agD("i1", StateIdle, "I", d)), t0)
	if got := detailIDs(t, m, t0); len(got) != 0 {
		t.Errorf("detail ids = %v", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestDetailStepsAndLines(t *testing.T) {
	m := New()
	d := &SyncDetail{
		Reply: "## Done\n\n- `a` | b\n\n```\nx | \"y\"\n```\n",
		Steps: append(steps(13), SyncStep{Name: "todo", Text: "", Status: "running"}),
	}
	m.Apply(req(agD("1", StateWorking, "A", d)), t0)
	a := viewByID(t, m, t0)["1"]
	if a.Detail == nil {
		t.Fatal("no detail")
	}
	if len(a.Detail.Steps) != 10 {
		t.Fatalf("steps = %d, want the last 10", len(a.Detail.Steps))
	}
	if first := a.Detail.Steps[0]; first != (Step{N: "bash", T: "step 5", S: "done"}) {
		t.Errorf("first kept step = %+v", first)
	}
	if last := a.Detail.Steps[9]; last != (Step{N: "todo", T: "todo", S: "running"}) {
		t.Errorf("last step = %+v (text falls back to the name)", last)
	}
	want := []markdown.Line{{K: "h", T: "Done"}, {K: "gap"}, {K: "li", T: "\x01a\x02 | b", D: 1}, {K: "gap"},
		{K: "code", T: `x | "y"`}}
	if !reflect.DeepEqual(a.Detail.Lines, want) {
		t.Errorf("lines = %+v", a.Detail.Lines)
	}

	// No detail from the plugin: no detail page, and no steps.
	m.Apply(req(ag("1", StateWorking, "A")), t0.Add(time.Second))
	if a := viewByID(t, m, t0)["1"]; a.Detail != nil || a.Todo != "" || a.Activity != "" {
		t.Errorf("stale detail kept: %+v", a)
	}
}

func TestRichSnapshotRoundTripsThroughLua(t *testing.T) {
	luatest.Available(t)
	m := New()
	d := &SyncDetail{
		Activity: "Running |cffff0000tests|r",
		Todo:     "3/7",
		Reply: "# Result\n\nThe **fix** is in `a|b.go` — see [docs](http://x.y).\n\n- one \U0001F680\n  - nested \"two\"\n\n" +
			"```go\nfmt.Println(\"a | b\")\n\tx := 'é'\n```\n",
		Steps: []SyncStep{{Name: "bash", Text: "Running tests", Status: "running"}, {Name: "edit", Text: "Edit a|b", Status: "done"}},
	}
	m.Apply(req(SyncAgent{Pane: "7881264988162", State: StateWorking, Title: "Demo", Cwd: "/home/user/p",
		Host: "0", Session: "1", LastMessage: "x\n·\n43.2s · 22K tok · $1.09 this turn", Detail: d}), t0)
	agents, toasts := m.View(t0)
	snap := Snapshot{Run: 1, At: 2, Session: 3, NextSignal: 4, Gen: 5, Tern: "ok", Companion: "0.1.0", Agents: agents, Toasts: toasts}
	data, err := snap.Lua()
	if err != nil {
		t.Fatal(err)
	}
	a := agents[0]
	if a.Detail == nil {
		t.Fatal("no detail")
	}
	var lines, stps []any
	for _, l := range a.Detail.Lines {
		e := map[string]any{"k": l.K, "t": l.T}
		if l.D > 0 {
			e["d"] = l.D
		}
		lines = append(lines, e)
	}
	for _, s := range a.Detail.Steps {
		stps = append(stps, map[string]any{"n": s.N, "t": s.T, "s": s.S})
	}
	hasSentinel := false
	for _, l := range a.Detail.Lines {
		if strings.ContainsAny(l.T, "\x01\x02\x03\x04") {
			hasSentinel = true
		}
	}
	if !hasSentinel {
		t.Fatal("fixture has no sentinels")
	}
	wantAgent := map[string]any{"id": a.ID, "name": a.Name, "host": a.Host, "session": a.Session, "state": a.State,
		"since": a.Since, "model": a.Model, "cwd": a.Cwd, "last": a.Last, "section": a.Section,
		"activity": a.Activity, "todo": "3/7", "stats": a.Stats, "doneAt": a.DoneAt, "preview": a.Preview,
		"detail": map[string]any{"lines": lines, "steps": stps}}
	want := luatest.Expect(map[string]any{
		"v": 0, "gen": 5, "run": 1, "at": 2, "session": 3, "nextSignal": 4, "tern": "ok", "companion": "0.1.0",
		"agents": []any{wantAgent}, "toasts": []any{},
	})
	if got := luatest.Run(t, data); !reflect.DeepEqual(got, want) {
		for i := range max(len(got), len(want)) {
			var g, w string
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Fatalf("mismatch at %d:\n lua : %s\n want: %s\n%s", i, g, w, data)
			}
		}
	}
	if !strings.Contains(string(data), `\001`) || !strings.Contains(string(data), `\003`) {
		t.Errorf("sentinel bytes are not escaped as \\001/\\003 in:\n%s", data)
	}
}

func TestActivityKey(t *testing.T) {
	base := Snapshot{Tern: "ok", Agents: []AgentView{{ID: "1", State: StateWorking, Activity: "a", Todo: "1/2", Name: "n", Last: "x"}}}
	k0, _ := base.ActivityKey()
	for name, mut := range map[string]func(s *Snapshot){
		"activity": func(s *Snapshot) { s.Agents[0].Activity = "b" },
		"todo":     func(s *Snapshot) { s.Agents[0].Todo = "2/2" },
		"state":    func(s *Snapshot) { s.Agents[0].State = StateIdle },
	} {
		s := base
		s.Agents = append([]AgentView(nil), base.Agents...)
		mut(&s)
		if k, _ := s.ActivityKey(); k == k0 {
			t.Errorf("%s must change the activity key", name)
		}
	}
	for name, mut := range map[string]func(s *Snapshot){
		"name":    func(s *Snapshot) { s.Agents[0].Name = "m" },
		"last":    func(s *Snapshot) { s.Agents[0].Last = "y" },
		"stats":   func(s *Snapshot) { s.Agents[0].Stats = "5s" },
		"preview": func(s *Snapshot) { s.Agents[0].Preview = "p" },
		"detail":  func(s *Snapshot) { s.Agents[0].Detail = &Detail{Steps: []Step{{N: "a"}}} },
	} {
		s := base
		s.Agents = append([]AgentView(nil), base.Agents...)
		mut(&s)
		if k, _ := s.ActivityKey(); k != k0 {
			t.Errorf("%s must not change the activity key", name)
		}
		if k, _ := s.ImportantKey(); k != mustImp(base) {
			t.Errorf("%s must not change the important key", name)
		}
	}
	s := base
	s.Agents = append([]AgentView(nil), base.Agents...)
	s.Agents[0].Activity = "zzz"
	if k, _ := s.ImportantKey(); k != mustImp(base) {
		t.Error("activity must not change the important key")
	}
}

func mustImp(s Snapshot) string {
	k, err := s.ImportantKey()
	if err != nil {
		panic(err)
	}
	return k
}
