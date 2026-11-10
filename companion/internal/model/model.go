// Package model is the companion's view of the Tern agents: the sync request
// types, the agent table, toasts and the snapshot (PROTOCOL §3.2, §4, §5).
package model

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/luaser"
	"github.com/TechDufus/ompcraft/companion/internal/markdown"
)

const (
	ToastMax        = 20
	ToastMaxAge     = 10 * time.Minute
	failedWindow    = 5 * time.Second
	ToastInput      = "input"
	ToastDone       = "done"
	ToastFailed     = "failed"
	ToastExited     = "exited"
	ToastUpdate     = "update"
	StateWaiting    = "waiting_input"
	StateWorking    = "working"
	StateIdle       = "idle"
	StateExited     = "exited"
	SnapshotVersion = 0
)

// FlexID is an identifier that may arrive as a JSON string or a JSON number
// (pane ids can exceed 2^52, so it is kept as its decimal text, never a float).
type FlexID string

// UnmarshalJSON accepts a string, a number or null.
func (f *FlexID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*f = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = FlexID(n.String())
	return nil
}

// SyncRequest is the body of POST /v1/sync.
type SyncRequest struct {
	V        int           `json:"v"`
	Window   string        `json:"window"`
	VM       string        `json:"vm"`
	Hosts    []SyncHost    `json:"hosts"`
	Sessions []SyncSession `json:"sessions"`
	Agents   []SyncAgent   `json:"agents"`
	Events   []SyncEvent   `json:"events"`
}

type SyncHost struct {
	ID    FlexID `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	State string `json:"state"`
}

type SyncSession struct {
	ID   FlexID `json:"id"`
	Name string `json:"name"`
	Host FlexID `json:"host"`
}

type SyncAgent struct {
	Pane        FlexID      `json:"pane"`
	State       string      `json:"state"`
	Title       string      `json:"title"`
	Cwd         string      `json:"cwd"`
	Model       string      `json:"model"`
	Host        FlexID      `json:"host"`
	Session     FlexID      `json:"session"`
	LastMessage string      `json:"last_message"`
	DeliveryErr *string     `json:"delivery_error"`
	Detail      *SyncDetail `json:"detail"`
}

type SyncEvent struct {
	T      string `json:"t"`
	Pane   FlexID `json:"pane"`
	Result string `json:"result"`
	Old    string `json:"old"`
	New    string `json:"new"`
}

// AgentView is one agent in the snapshot.
type AgentView struct {
	ID       string  `lua:"id" json:"id"`
	Name     string  `lua:"name" json:"name"`
	Host     string  `lua:"host" json:"host"`
	Session  string  `lua:"session" json:"session"`
	State    string  `lua:"state" json:"state"`
	Since    int64   `lua:"since" json:"since"`
	Model    string  `lua:"model" json:"model"`
	Cwd      string  `lua:"cwd" json:"cwd"`
	Last     string  `lua:"last" json:"last"`
	Section  string  `lua:"section" json:"section"`
	Activity string  `lua:"activity,omitempty" json:"activity,omitempty"`
	Todo     string  `lua:"todo,omitempty" json:"todo,omitempty"`
	Stats    string  `lua:"stats" json:"stats"`
	DoneAt   int64   `lua:"doneAt" json:"doneAt"`
	Preview  string  `lua:"preview" json:"preview"`
	Detail   *Detail `lua:"detail,omitempty" json:"detail,omitempty"`
}

// Toast is one notification. at (the transition time) is internal.
type Toast struct {
	ID    int64  `lua:"id" json:"id"`
	Kind  string `lua:"kind" json:"kind"`
	Agent string `lua:"agent" json:"agent"`
	Text  string `lua:"text" json:"text"`
	at    time.Time
	name  string
}

type agentState struct {
	view      AgentView
	derived   *derived
	hasDetail bool
	lines     []markdown.Line
	steps     []Step
}

// Model tracks agents and toasts across syncs. It is not safe for concurrent
// use; the daemon serialises access.
type Model struct {
	agents    map[string]*agentState
	toasts    []Toast
	lastToast int64
}

func New() *Model {
	return &Model{agents: map[string]*agentState{}}
}

// Apply folds one leader sync into the model. now is the companion's clock.
func (m *Model) Apply(req *SyncRequest, now time.Time) {
	hosts := make(map[FlexID]string, len(req.Hosts))
	for _, h := range req.Hosts {
		if h.Kind == "local" {
			hosts[h.ID] = "" // the local host is not worth a label
			continue
		}
		hosts[h.ID] = oneLine(h.Name)
	}
	sessions := make(map[FlexID]string, len(req.Sessions))
	for _, s := range req.Sessions {
		sessions[s.ID] = oneLine(s.Name)
	}

	seen := make(map[string]bool, len(req.Agents))
	for _, a := range req.Agents {
		if a.Pane == "" {
			continue
		}
		pane := string(a.Pane)
		seen[pane] = true
		prev, known := m.agents[pane]
		var prevDerived *derived
		var detail SyncDetail
		if known {
			prevDerived = prev.derived
		}
		if a.Detail != nil {
			detail = *a.Detail
		}
		dv := derive(prevDerived, a.LastMessage, detail.Reply)
		v := AgentView{
			ID:      pane,
			Name:    Name(a.Title, a.Cwd),
			Host:    hosts[a.Host],
			Session: sessions[a.Session],
			State:   a.State,
			Model:   oneLine(a.Model),
			Cwd:     Basename(a.Cwd),
			Last:    Last(a.LastMessage),
			Section: section(sessions[a.Session], hosts[a.Host]),
			Todo:    clip(oneLine(detail.Todo), todoMax),
			Stats:   dv.stats,
			Preview: dv.preview,
		}
		if a.State == StateWorking {
			v.Activity = clip(oneLine(detail.Activity), activityMax)
		}
		switch {
		case !known:
			v.Since = 0 // state start not observed; no toast on first sight
		case prev.view.State != a.State:
			v.Since = now.Unix()
			v.DoneAt = prev.view.DoneAt
			if prev.view.State == StateWorking && (a.State == StateIdle || a.State == StateWaiting || a.State == StateExited) {
				v.DoneAt = now.Unix()
			}
			m.transition(prev.view.State, v, now)
		default:
			v.Since = prev.view.Since
		}
		if known && prev.view.State == a.State {
			v.DoneAt = prev.view.DoneAt
		}
		st := &agentState{view: v, derived: dv, hasDetail: a.Detail != nil, lines: dv.lines}
		if a.Detail != nil {
			st.steps = buildSteps(detail.Steps)
		}
		m.agents[pane] = st
	}
	for pane := range m.agents {
		if !seen[pane] {
			delete(m.agents, pane)
		}
	}

	// A `finished` event with result "failed" within 5 s of the transition turns
	// the done toast into a failed one. It may arrive in the same sync as the
	// transition or in a later one, so amend whichever toast is still young.
	for _, e := range req.Events {
		if e.T != "finished" || e.Result != "failed" {
			continue
		}
		for i := range m.toasts {
			t := &m.toasts[i]
			if t.Kind == ToastDone && t.Agent == string(e.Pane) && now.Sub(t.at) <= failedWindow {
				t.Kind = ToastFailed
				t.Text = t.name + " failed"
			}
		}
	}
	m.prune(now)
}

func (m *Model) transition(old string, v AgentView, now time.Time) {
	switch {
	case v.State == StateWaiting:
		m.addToast(ToastInput, v, v.Name+" needs you", now)
	case v.State == StateExited:
		m.addToast(ToastExited, v, v.Name+" exited", now)
	case old == StateWorking && v.State == StateIdle:
		m.addToast(ToastDone, v, v.Name+" finished", now)
	}
}

// AddSystemToast adds a toast that belongs to no agent (kind "update"): a new
// toast id, strictly increasing like the others.
func (m *Model) AddSystemToast(kind, text string, now time.Time) {
	m.addToast(kind, AgentView{}, text, now)
}

// Notice is the one-shot message in a /v1/sync response (PROTOCOL §8.8).
type Notice struct {
	ID    string `json:"id"`
	Level string `json:"level"`
	Text  string `json:"text"`
	Sub   string `json:"sub,omitempty"`
}

func (m *Model) addToast(kind string, v AgentView, text string, now time.Time) {
	id := now.UnixMilli()
	if id <= m.lastToast {
		id = m.lastToast + 1
	}
	m.lastToast = id
	m.toasts = append(m.toasts, Toast{ID: id, Kind: kind, Agent: v.ID, Text: text, at: now, name: v.Name})
}

func (m *Model) prune(now time.Time) {
	kept := m.toasts[:0]
	for _, t := range m.toasts {
		if now.Sub(t.at) < ToastMaxAge {
			kept = append(kept, t)
		}
	}
	m.toasts = kept
	if len(m.toasts) > ToastMax {
		m.toasts = append([]Toast(nil), m.toasts[len(m.toasts)-ToastMax:]...)
	}
}

func stateRank(s string) int {
	switch s {
	case StateWaiting:
		return 0
	case StateWorking:
		return 1
	case StateIdle:
		return 2
	case StateExited:
		return 3
	}
	return 4
}

// View returns the ordered agents and the live toasts (PROTOCOL §4.1, §4.2, §5).
// At most maxDetail agents carry detail (PROTOCOL §8.4): every waiting_input
// agent, then every working agent, then up to maxRecentDone of the most
// recently finished (doneAt > 0), counting only agents the plugin sent detail
// for.
func (m *Model) View(now time.Time) ([]AgentView, []Toast) {
	m.prune(now)
	states := make([]*agentState, 0, len(m.agents))
	for _, a := range m.agents {
		states = append(states, a)
	}
	sort.Slice(states, func(i, j int) bool {
		a, b := states[i].view, states[j].view
		if ra, rb := stateRank(a.State), stateRank(b.State); ra != rb {
			return ra < rb
		}
		if a.Since != b.Since {
			return a.Since > b.Since
		}
		return a.ID < b.ID
	})
	withDetail := map[string]bool{}
	pick := func(st *agentState) {
		if len(withDetail) < maxDetail && st.hasDetail {
			withDetail[st.view.ID] = true
		}
	}
	for _, st := range states {
		if st.view.State == StateWaiting {
			pick(st)
		}
	}
	for _, st := range states {
		if st.view.State == StateWorking {
			pick(st)
		}
	}
	recent := make([]*agentState, 0, len(states))
	for _, st := range states {
		if st.view.DoneAt > 0 && !withDetail[st.view.ID] {
			recent = append(recent, st)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool {
		a, b := recent[i].view, recent[j].view
		if a.DoneAt != b.DoneAt {
			return a.DoneAt > b.DoneAt
		}
		return a.ID < b.ID
	})
	picked := 0
	for _, st := range recent {
		if picked >= maxRecentDone {
			break
		}
		if st.hasDetail {
			pick(st)
			picked++
		}
	}
	agents := make([]AgentView, len(states))
	for i, st := range states {
		agents[i] = st.view
		if withDetail[st.view.ID] {
			agents[i].Detail = &Detail{Lines: st.lines, Steps: st.steps}
		}
	}
	toasts := append([]Toast(nil), m.toasts...)
	return agents, toasts
}

// Snapshot is the data.lua payload (PROTOCOL §5) and the /v1/state JSON.
type Snapshot struct {
	V          int         `lua:"v" json:"v"`
	Gen        int64       `lua:"gen" json:"gen"`
	Run        int64       `lua:"run" json:"run"`
	At         int64       `lua:"at" json:"at"`
	Session    int64       `lua:"session" json:"session"`
	NextSignal int         `lua:"nextSignal" json:"nextSignal"`
	Tern       string      `lua:"tern" json:"tern"`
	Companion  string      `lua:"companion" json:"companion"`
	Agents     []AgentView `lua:"agents" json:"agents"`
	Toasts     []Toast     `lua:"toasts" json:"toasts"`
}

// Lua renders the snapshot as data.lua contents.
func (s Snapshot) Lua() ([]byte, error) {
	return luaser.Marshal("OmpcraftInbox", s)
}

// ContentKey is the snapshot's Lua with gen, at and nextSignal zeroed: two
// snapshots with equal keys do not differ (PROTOCOL §6.1).
func (s Snapshot) ContentKey() (string, error) {
	s.Gen, s.At, s.NextSignal = 0, 0, 0
	b, err := s.Lua()
	return string(b), err
}

// ImportantKey is the content key restricted to what is worth a doorbell
// (PROTOCOL §6.1): which agents exist and their states, toasts, tern, session
// and companion version. Names, models, hosts, cwd and last message are
// cosmetic and left out.
func (s Snapshot) ImportantKey() (string, error) {
	agents := make([]AgentView, len(s.Agents))
	for i, a := range s.Agents {
		agents[i] = AgentView{ID: a.ID, State: a.State}
	}
	s.Agents = agents
	return s.ContentKey()
}

// ActivityKey is ImportantKey plus each agent's activity and todo: the
// fields of the Activity publish class (PROTOCOL §8.5).
func (s Snapshot) ActivityKey() (string, error) {
	agents := make([]AgentView, len(s.Agents))
	for i, a := range s.Agents {
		agents[i] = AgentView{ID: a.ID, State: a.State, Activity: a.Activity, Todo: a.Todo}
	}
	s.Agents = agents
	return s.ContentKey()
}
