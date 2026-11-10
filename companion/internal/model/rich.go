package model

import (
	"strings"
	"unicode/utf8"

	"github.com/TechDufus/ompcraft/companion/internal/markdown"
)

const (
	previewMax    = 140
	maxSteps      = 10
	maxDetail     = 8
	maxRecentDone = 4
	activityMax   = 160
	todoMax       = 32
	stepTextMax   = 120
	stepNameMax   = 40
	stepStatusMax = 16
)

// SyncDetail is agents[].detail (PROTOCOL §8.1); every field is optional.
type SyncDetail struct {
	Activity string     `json:"activity"`
	Todo     string     `json:"todo"`
	Steps    []SyncStep `json:"steps"`
	Reply    string     `json:"reply"`
}

// SyncStep is one tool record in detail.steps.
type SyncStep struct {
	Name   string `json:"name"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

// Step is one step in a snapshot's detail (PROTOCOL §8.4).
type Step struct {
	N string `lua:"n" json:"n"`
	T string `lua:"t" json:"t"`
	S string `lua:"s" json:"s"`
}

// Detail is the per-agent detail page content (PROTOCOL §8.3, §8.4).
type Detail struct {
	Lines []markdown.Line `lua:"lines" json:"lines"`
	Steps []Step          `lua:"steps" json:"steps"`
}

// clip cuts s to at most n bytes on a UTF-8 boundary; the "…" that marks a
// cut counts toward n.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	if cut < 0 {
		return ""
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ") + "…"
}

// section implements PROTOCOL §8.2 `section`: the session name, with
// " · <host>" appended for remote hosts. host is "" for the local host.
func section(session, host string) string {
	switch {
	case session != "" && host != "":
		return session + " · " + host
	case session != "":
		return session
	}
	return host
}

// derived holds what is computed from last_message and detail.reply, cached
// per agent while both are unchanged.
type derived struct {
	lastRaw string
	reply   string
	stats   string
	preview string
	lines   []markdown.Line
}

func derive(prev *derived, lastMessage, reply string) *derived {
	if prev != nil && prev.lastRaw == lastMessage && prev.reply == reply {
		return prev
	}
	d := &derived{lastRaw: lastMessage, reply: reply, stats: Stats(lastMessage)}
	if reply != "" {
		d.lines = markdown.Render(reply)
		d.preview = markdown.Preview(d.lines, previewMax)
	}
	if d.preview == "" {
		body, _ := splitFooter(Clean(lastMessage))
		d.preview = markdown.Preview(markdown.Render(body), previewMax)
	}
	return d
}

func buildSteps(in []SyncStep) []Step {
	if len(in) > maxSteps {
		in = in[len(in)-maxSteps:]
	}
	out := make([]Step, 0, len(in))
	for _, s := range in {
		name := clip(oneLine(s.Name), stepNameMax)
		text := clip(oneLine(s.Text), stepTextMax)
		if text == "" {
			text = name
		}
		out = append(out, Step{N: name, T: text, S: clip(oneLine(s.Status), stepStatusMax)})
	}
	return out
}
