// Package demo is a scripted agent source for README screenshots: it produces
// the same sync requests the Tern plugin would, from INVENTED data only (no
// real names, hosts, paths or sessions), looping every Loop so every UI state
// shows up at some point.
package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/model"
)

// Loop is the length of the script. All agents disappear for the last
// VanishFor and come back in their first state, so every loop produces each
// toast exactly once.
const (
	Loop      = 90 * time.Second
	VanishFor = 2 * time.Second
)

type seg struct {
	from  float64 // seconds into the loop
	state string
}

type agent struct {
	pane     string
	title    string
	model    string
	session  string
	cwd      string
	timeline []seg
	// activities cycle every 8 s while working; todo picks the progress text.
	activities []string
	todo       func(sec float64) string
	steps      []model.SyncStep // finished steps shown on the detail page
	last       func(sec float64, state string) string
	reply      func(sec float64, state string) string
	// failedAt, if set, sends a `finished`/`failed` event while the agent
	// first shows its idle state after working.
	failedAt float64
}

func stateAt(tl []seg, sec float64) string {
	st := tl[0].state
	for _, s := range tl {
		if s.from <= sec {
			st = s.state
		}
	}
	return st
}

const (
	working = "working"
	waiting = "waiting_input"
	idle    = "idle"
	exited  = "exited"
)

var hosts = []model.SyncHost{
	{ID: "0", Name: "this-pc", Kind: "local", State: "connected"},
	{ID: "1", Name: "build-box", Kind: "remote", State: "connected"},
}

var sessions = []model.SyncSession{
	{ID: "1", Name: "api-gateway", Host: "0"},
	{ID: "2", Name: "docs-site", Host: "0"},
	{ID: "3", Name: "infra", Host: "1"},
}

var sessionHost = map[string]string{"1": "0", "2": "0", "3": "1"}

const replyQuestion = "## Which structure should the 2.0 migration guide use?\n\n" +
	"I can split it **by audience** or **by feature**. Both keep the `docs/migrate/` layout, but they differ in how much readers jump around.\n\n" +
	"- **By audience**\n  - operators: config and deploy changes\n  - library users: the renamed `Client.Do` API\n" +
	"- **By feature**\n  - one page per breaking change\n\n" +
	"Here is the redirect I would add either way:\n\n" +
	"```go\nfunc redirect(w http.ResponseWriter, r *http.Request) {\n\tname := path.Base(r.URL.Path)\n\tif name == \".\" || name == \"/\" {\n\t\tname = \"index\"\n\t}\n\thttp.Redirect(w, r, \"/docs/2.0/migrate/\"+name, http.StatusMovedPermanently)\n}\n```\n\n" +
	"| Option | Pages | Effort |\n|---|---|---|\n| By audience | 4 | small |\n| By feature | 11 | large |\n\n" +
	"Tell me which one and I will start writing."

const replyDone = "## CI now runs on the new runners\n\n" +
	"All 14 jobs pass on the new runners. The slowest job dropped from `11m` to **4m 12s**, mostly from cached dependencies.\n\n" +
	"- Changes\n  - `runs-on` labels updated in every workflow\n  - the old runner pool is drained\n" +
	"- Follow-ups\n  - delete the legacy `ci-old` pool next week\n\n" +
	"```go\njobs := []string{\"lint\", \"test\", \"build\"}\nfor _, j := range jobs {\n\tif err := run(j); err != nil {\n\t\tlog.Fatalf(\"%s failed: %v\", j, err)\n\t}\n}\n```\n\n" +
	"| Job | Before | After |\n|---|---|---|\n| test | 11m | 4m |\n| build | 6m | 3m |"

func st(name, text, status string) model.SyncStep {
	return model.SyncStep{Name: name, Text: text, Status: status}
}

var agents = []agent{
	{
		pane: "9100000000101", title: "Add rate limiting to the public API", model: "Opus 5.5", session: "1", cwd: "/home/user/api-gateway",
		timeline:   []seg{{0, idle}, {2, working}},
		activities: []string{"Reading the router middleware", "Editing rate_limit.go", "Running unit tests", "Checking the benchmark"},
		todo: func(s float64) string {
			switch {
			case s < 20:
				return "2/5"
			case s < 45:
				return "3/5"
			case s < 70:
				return "4/5"
			}
			return "5/5"
		},
		steps: []model.SyncStep{st("read", "Reading the router middleware", "done"), st("grep", "Searching for existing limiters", "done"),
			st("edit", "Editing rate_limit.go", "done"), st("bash", "Running unit tests", "error"), st("edit", "Fixing the burst calculation", "done")},
		last: func(float64, string) string { return "Wiring the limiter into the middleware chain." },
	},
	{
		pane: "9100000000102", title: "Fix flaky checkout test", model: "GPT-6 Astra", session: "1", cwd: "/home/user/api-gateway",
		timeline:   []seg{{0, idle}, {3, working}},
		activities: []string{"Reading checkout_test.go", "Re-running the test 20 times", "Bisecting the flaky assertion", "Editing checkout_test.go"},
		todo: func(s float64) string {
			switch {
			case s < 30:
				return "1/3"
			case s < 60:
				return "2/3"
			}
			return "3/3"
		},
		steps: []model.SyncStep{st("read", "Reading checkout_test.go", "done"), st("bash", "Re-running the test 20 times", "done"),
			st("grep", "Searching for shared fixtures", "done"), st("bash", "Bisecting the flaky assertion", "error"), st("edit", "Isolating the clock fixture", "done")},
		last: func(float64, string) string { return "The failure only appears when two subtests share a clock." },
	},
	{
		pane: "9100000000103", title: "Write the 2.0 migration guide", model: "Fable 5.2", session: "2", cwd: "/home/user/docs-site",
		timeline:   []seg{{0, idle}, {5, working}, {12, waiting}},
		activities: []string{"Drafting the upgrade steps"},
		todo:       func(float64) string { return "3/6" },
		steps: []model.SyncStep{st("read", "Reading the 1.x changelog", "done"), st("grep", "Listing the breaking changes", "done"),
			st("edit", "Drafting docs/migrate/index.md", "done"), st("bash", "Building the docs site", "error"),
			st("edit", "Fixing the broken anchors", "done"), st("ask", "Asking which layout to use", "running")},
		last: func(s float64, state string) string {
			if state == waiting {
				return "Which structure should the 2.0 migration guide use?"
			}
			return ""
		},
		reply: func(s float64, state string) string {
			if state == waiting || s >= 12 {
				return replyQuestion
			}
			return ""
		},
	},
	{
		pane: "9100000000104", title: "Migrate CI to the new runners", model: "Opus 5.5", session: "3", cwd: "/home/user/infra",
		timeline:   []seg{{0, idle}, {10, working}, {30, idle}},
		activities: []string{"Updating the runner labels", "Running the pipeline on the new runners"},
		todo:       func(float64) string { return "4/7" },
		steps: []model.SyncStep{st("read", "Reading the runner config", "done"), st("edit", "Updating the runner labels", "done"),
			st("bash", "Running the pipeline on the new runners", "done"), st("bash", "Comparing build times", "done"),
			st("todo", "Updating the checklist", "done"), st("bash", "Cleaning up the old runners", "error")},
		last: func(s float64, state string) string {
			if s >= 30 {
				return "All 14 jobs pass on the new runners.\n·\n4m 12s · 38K tok · $0.61 this turn"
			}
			return ""
		},
		reply: func(s float64, state string) string {
			if s >= 30 {
				return replyDone
			}
			return ""
		},
	},
	{
		pane: "9100000000105", title: "Upgrade the Postgres driver", model: "GPT-6 Astra", session: "3", cwd: "/home/user/infra",
		timeline:   []seg{{0, idle}, {35, working}, {50, idle}},
		activities: []string{"Reading the query layer", "Running the migration check"},
		todo:       func(float64) string { return "2/4" },
		last: func(s float64, state string) string {
			if s >= 50 {
				return "The migration check failed: two queries still use the removed `QueryRowContext` signature.\n·\n1m 5s · 12K tok · $0.18 this turn"
			}
			return ""
		},
		failedAt: 50,
	},
	{
		pane: "9100000000106", title: "Triage overnight alerts", model: "Fable 5.2", session: "3", cwd: "/home/user/infra",
		timeline:   []seg{{0, idle}, {55, working}, {66, exited}},
		activities: []string{"Grouping the alerts by service"},
		last:       func(float64, string) string { return "" },
	},
	{
		pane: "9100000000107", title: "Refactor the billing module", model: "Opus 5.5", session: "1", cwd: "/home/user/api-gateway",
		timeline: []seg{{0, idle}},
		last:     func(float64, string) string { return "Split the invoice builder from the tax rules." },
	},
	{
		pane: "9100000000108", title: "Review dependency licenses", model: "GPT-6 Astra", session: "2", cwd: "/home/user/docs-site",
		timeline: []seg{{0, idle}},
		last:     func(float64, string) string { return "All 31 dependencies use licenses we already allow." },
	},
}

// Frame returns the sync request for t seconds into the script (wrapped at
// Loop). The last VanishFor of each loop has no agents at all.
func Frame(t time.Duration) *model.SyncRequest {
	t %= Loop
	req := &model.SyncRequest{V: 0, Window: "demo-window", VM: "demo-vm", Hosts: hosts, Sessions: sessions}
	if t >= Loop-VanishFor {
		return req
	}
	sec := t.Seconds()
	for _, a := range agents {
		state := stateAt(a.timeline, sec)
		sa := model.SyncAgent{
			Pane: model.FlexID(a.pane), State: state, Title: a.title, Cwd: a.cwd, Model: a.model,
			Host: model.FlexID(sessionHost[a.session]), Session: model.FlexID(a.session),
		}
		if a.last != nil {
			sa.LastMessage = a.last(sec, state)
		}
		if a.activities != nil || a.reply != nil || a.steps != nil {
			d := &model.SyncDetail{Steps: stepsFor(a, state, sec)}
			if state == working && len(a.activities) > 0 {
				d.Activity = a.activities[int(sec/8)%len(a.activities)]
			}
			if a.todo != nil {
				d.Todo = a.todo(sec)
			}
			if a.reply != nil {
				d.Reply = a.reply(sec, state)
			}
			sa.Detail = d
		}
		req.Agents = append(req.Agents, sa)
		if a.failedAt > 0 && sec >= a.failedAt && sec < a.failedAt+2 {
			req.Events = append(req.Events, model.SyncEvent{T: "finished", Pane: model.FlexID(a.pane), Result: "failed"})
		}
	}
	return req
}

// stepsFor lists the agent's recent steps: the finished ones, plus the running
// activity while working.
func stepsFor(a agent, state string, sec float64) []model.SyncStep {
	if len(a.steps) == 0 {
		return nil
	}
	steps := append([]model.SyncStep(nil), a.steps...)
	if state == working && len(a.activities) > 0 {
		act := a.activities[int(sec/8)%len(a.activities)]
		steps = append(steps, model.SyncStep{Name: "bash", Text: act, Status: "running"})
	}
	return steps
}

// Drive feeds Frame to sink every `every`, starting at once, until ctx ends.
func Drive(ctx context.Context, sink func(*model.SyncRequest), every time.Duration, now func() time.Time) {
	start := now()
	sink(Frame(0))
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			sink(Frame(now().Sub(start)))
		}
	}
}

// String describes a frame for logs and test failures.
func Describe(req *model.SyncRequest) string {
	s := ""
	for _, a := range req.Agents {
		s += fmt.Sprintf("%s=%s ", a.Pane, a.State)
	}
	return s
}
