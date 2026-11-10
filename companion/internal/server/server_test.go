package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/daemon"
	"github.com/TechDufus/ompcraft/companion/internal/logx"
	"github.com/TechDufus/ompcraft/companion/internal/model"
	"github.com/TechDufus/ompcraft/companion/internal/testenv"
)

const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeBackend struct {
	mu      sync.Mutex
	syncs   []*model.SyncRequest
	notices []model.Notice
}

func (f *fakeBackend) Sync(r *model.SyncRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs = append(f.syncs, r)
}

func (f *fakeBackend) Report() daemon.Report { return daemon.Report{Version: "fake"} }

func (f *fakeBackend) TakeNotice() *model.Notice {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.notices) == 0 {
		return nil
	}
	n := f.notices[0]
	f.notices = f.notices[1:]
	return &n
}

func (f *fakeBackend) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.syncs)
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestServer(t *testing.T) (*httptest.Server, *fakeBackend, *fakeClock) {
	t.Helper()
	b := &fakeBackend{}
	clk := &fakeClock{t: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	ts := httptest.NewServer(New(token, b, logx.Discard(), clk.Now).Handler())
	t.Cleanup(ts.Close)
	return ts, b, clk
}

func do(t *testing.T, method, url, auth, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func syncBody(window, vm string) string {
	return `{"v":0,"window":"` + window + `","vm":"` + vm + `","hosts":[],"sessions":[],"agents":[],"events":[]}`
}

func decodeSync(t *testing.T, body string) syncResponse {
	t.Helper()
	var r syncResponse
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("bad response %q: %v", body, err)
	}
	return r
}

func TestAuth(t *testing.T) {
	ts, b, _ := newTestServer(t)
	for _, auth := range []string{"", "Bearer wrong", "Bearer " + token + "x", token, "bearer " + token, "Basic " + token} {
		if code, _ := do(t, "POST", ts.URL+"/v1/sync", auth, syncBody("w", "v")); code != http.StatusUnauthorized {
			t.Errorf("sync with %q: status %d, want 401", auth, code)
		}
		if code, _ := do(t, "GET", ts.URL+"/v1/state", auth, ""); code != http.StatusUnauthorized {
			t.Errorf("state with %q: status %d, want 401", auth, code)
		}
	}
	// A bad token is rejected before the body is looked at, and nothing reaches the model.
	if code, _ := do(t, "POST", ts.URL+"/v1/sync", "Bearer wrong", "{not json"); code != http.StatusUnauthorized {
		t.Errorf("bad token + bad body: %d", code)
	}
	if b.count() != 0 {
		t.Fatal("unauthenticated request reached the backend")
	}
	if code, _ := do(t, "POST", ts.URL+"/v1/sync", "Bearer "+token, syncBody("w", "v")); code != http.StatusOK {
		t.Fatalf("valid token: %d", code)
	}
}

func TestSyncErrors(t *testing.T) {
	ts, b, _ := newTestServer(t)
	auth := "Bearer " + token
	for name, body := range map[string]string{
		"malformed json": "{not json",
		"empty body":     "",
		"wrong shape":    `{"agents": 5}`,
		"future version": `{"v":1,"window":"w","vm":"v"}`,
	} {
		if code, _ := do(t, "POST", ts.URL+"/v1/sync", auth, body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
	if code, _ := do(t, "GET", ts.URL+"/v1/sync", auth, ""); code != http.StatusMethodNotAllowed {
		t.Errorf("GET sync: %d", code)
	}
	if code, _ := do(t, "POST", ts.URL+"/v1/state", auth, "{}"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST state: %d", code)
	}
	if code, _ := do(t, "GET", ts.URL+"/nope", auth, ""); code != http.StatusNotFound {
		t.Errorf("unknown path: %d", code)
	}
	if b.count() != 0 {
		t.Fatal("a rejected request reached the backend")
	}
}

func TestSyncResponseAndOptionalFields(t *testing.T) {
	ts, b, _ := newTestServer(t)
	body := `{"v":0,"window":"w","vm":"v","unknown":{"x":1},
	  "agents":[{"pane":"7881264988162","state":"idle","title":null,"cwd":null,"model":null,"last_message":null,"delivery_error":null,"session":"12884901894","host":0}],
	  "sessions":[{"id":"12884901894","name":"demo-session","host":0}],
	  "hosts":[{"id":0,"name":"host-a","kind":"local","state":"connected"}]}`
	code, resp := do(t, "POST", ts.URL+"/v1/sync", "Bearer "+token, body)
	if code != 200 {
		t.Fatalf("status %d: %s", code, resp)
	}
	if r := decodeSync(t, resp); r.V != 0 || !r.Leader || r.IntervalMs != 500 {
		t.Fatalf("response = %+v", r)
	}
	if !strings.Contains(resp, `"interval_ms":500`) || !strings.Contains(resp, `"leader":true`) {
		t.Fatalf("raw response: %s", resp)
	}
	if b.count() != 1 || b.syncs[0].Agents[0].Pane != "7881264988162" || b.syncs[0].Sessions[0].ID != "12884901894" {
		t.Fatalf("backend got %+v", b.syncs)
	}
}

func TestLeaderAndFollower(t *testing.T) {
	ts, b, clk := newTestServer(t)
	auth := "Bearer " + token
	post := func(window, vm string) syncResponse {
		code, body := do(t, "POST", ts.URL+"/v1/sync", auth, syncBody(window, vm))
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		return decodeSync(t, body)
	}

	if r := post("w1", "vmA"); !r.Leader || r.IntervalMs != 500 {
		t.Fatalf("first caller must lead: %+v", r)
	}
	clk.Advance(time.Second)
	if r := post("w2", "vmA"); r.Leader || r.IntervalMs != 3000 {
		t.Fatalf("other window: %+v", r)
	}
	if r := post("w2", "vmB"); r.Leader || r.IntervalMs != 3000 {
		t.Fatalf("other window, other vm: %+v", r)
	}
	if b.count() != 1 {
		t.Fatalf("follower data must be ignored, backend saw %d syncs", b.count())
	}
	// The leader keeps leading and refreshes its lease.
	for range 3 {
		clk.Advance(2 * time.Second)
		if r := post("w1", "vmA"); !r.Leader {
			t.Fatal("leader lost its seat while syncing")
		}
		if r := post("w2", "vmA"); r.Leader {
			t.Fatal("follower took over a live leader")
		}
	}
	if b.count() != 4 {
		t.Fatalf("backend syncs = %d", b.count())
	}
}

func TestLeaderTakeoverOnPluginReload(t *testing.T) {
	ts, b, clk := newTestServer(t)
	auth := "Bearer " + token
	post := func(window, vm string) syncResponse {
		_, body := do(t, "POST", ts.URL+"/v1/sync", auth, syncBody(window, vm))
		return decodeSync(t, body)
	}
	if r := post("w1", "vmA"); !r.Leader {
		t.Fatal("first caller must lead")
	}
	// The plugin reloads: same window key, new vm id, 300 ms later. It leads at once.
	clk.Advance(300 * time.Millisecond)
	if r := post("w1", "vmB"); !r.Leader || r.IntervalMs != 500 {
		t.Fatalf("reloaded vm must take over immediately: %+v", r)
	}
	if b.count() != 2 {
		t.Fatalf("both syncs must reach the backend, got %d", b.count())
	}
	// A different window still waits for the 5 s timeout.
	clk.Advance(time.Second)
	if r := post("w2", "vmC"); r.Leader {
		t.Fatal("other window took over a live leader")
	}
	clk.Advance(LeaderTimeout + time.Millisecond)
	if r := post("w2", "vmC"); !r.Leader {
		t.Fatal("other window must take over after the timeout")
	}
	// And that window's reload takes over at once as well.
	if r := post("w2", "vmD"); !r.Leader {
		t.Fatal("reload in the new leader's window must take over")
	}
}

func TestLeaderTimeout(t *testing.T) {
	ts, b, clk := newTestServer(t)
	auth := "Bearer " + token
	post := func(window, vm string) syncResponse {
		_, body := do(t, "POST", ts.URL+"/v1/sync", auth, syncBody(window, vm))
		return decodeSync(t, body)
	}
	post("w1", "vmA")
	clk.Advance(LeaderTimeout) // silent for exactly 5 s: not yet lost
	if r := post("w2", "vmB"); r.Leader {
		t.Fatal("leadership lost at exactly 5 s")
	}
	clk.Advance(time.Millisecond) // more than 5 s
	if r := post("w2", "vmB"); !r.Leader || r.IntervalMs != 500 {
		t.Fatalf("next caller must take over after the leader was silent > 5 s: %+v", r)
	}
	// The old leader is now a follower.
	if r := post("w1", "vmA"); r.Leader || r.IntervalMs != 3000 {
		t.Fatalf("old leader: %+v", r)
	}
	if b.count() != 2 {
		t.Fatalf("backend syncs = %d, want one per leader", b.count())
	}

	// A follower's polling does not keep the old leader's lease alive, but the
	// leader's own call does.
	clk.Advance(4 * time.Second)
	post("w1", "vmA") // follower poll
	clk.Advance(2 * time.Second)
	if r := post("w1", "vmA"); !r.Leader {
		t.Fatal("w2/vmB has been silent for 6 s; w1/vmA should have taken over")
	}
}

func TestStateEndpoint(t *testing.T) {
	l := testenv.Install(t, 3, 6)
	d := daemon.New(daemon.Config{Layout: l, Version: "0.1.0-test", Log: logx.Discard(),
		FindGame:  func() (int, error) { return 4242, nil },
		SaveState: func(config.State) error { return nil }})
	ts := httptest.NewServer(New(token, d, logx.Discard(), nil).Handler())
	defer ts.Close()

	body := `{"v":0,"window":"w","vm":"v","hosts":[{"id":0,"name":"host-a","kind":"remote","state":"connected"}],
	  "sessions":[{"id":"9","name":"demo-session","host":0}],
	  "agents":[{"pane":"101","state":"working","title":"Demo","cwd":"/home/user/project","host":0,"session":"9","model":"demo-model","last_message":"hi"}]}`
	if code, resp := do(t, "POST", ts.URL+"/v1/sync", "Bearer "+token, body); code != 200 {
		t.Fatalf("sync: %d %s", code, resp)
	}

	st, err := FetchState(ts.URL, token, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "0.1.0-test" || st.Slots != 3 || st.Signals != 6 || !st.Leader.Active || st.Leader.VM != "v" || st.Leader.Window != "w" {
		t.Fatalf("state = %+v", st)
	}
	if st.Snapshot.Tern != "ok" || len(st.Snapshot.Agents) != 1 || st.Snapshot.Agents[0].Name != "Demo" ||
		st.Snapshot.Agents[0].Host != "host-a" || st.Snapshot.Agents[0].Session != "demo-session" {
		t.Fatalf("snapshot = %+v", st.Snapshot)
	}
	if st.LastSyncMs == 0 || st.Counters.Publishes < 1 || st.Counters.SlotWrites < 3 || st.Counters.Syncs != 1 || st.PID == 0 {
		t.Fatalf("counters = %+v last=%d", st.Counters, st.LastSyncMs)
	}
	if st.LoadedSlots == nil || st.Counters.Inotify == nil {
		t.Fatal("loadedSlots and inotifyEvents must be present (not null) in JSON")
	}
	if _, err := FetchState(ts.URL, "wrong", time.Second); err == nil {
		t.Fatal("FetchState with a bad token must fail")
	}
	if _, err := FetchState("http://127.0.0.1:1", token, time.Second); err == nil {
		t.Fatal("FetchState against a closed port must fail")
	}
}

func TestListenFallsBackToFreePort(t *testing.T) {
	first, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	port := first.Addr().(*net.TCPAddr).Port
	second, err := Listen(port) // busy: must fall back
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	addr := second.Addr().(*net.TCPAddr)
	if addr.Port == port || !addr.IP.IsLoopback() {
		t.Fatalf("fallback listener on %v", addr)
	}
	if got := URL(second); !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatalf("URL = %s", got)
	}
}

func TestNoticeIsDeliveredOnceToTheLeader(t *testing.T) {
	ts, b, clk := newTestServer(t)
	auth := "Bearer " + token
	post := func(window, vm string) string {
		_, body := do(t, "POST", ts.URL+"/v1/sync", auth, syncBody(window, vm))
		return body
	}
	post("w1", "vmA") // establishes the leader (nothing queued yet)
	b.notices = []model.Notice{{ID: "n1", Level: "info", Text: "ompcraft updated to 0.2.0", Sub: "Type /reload in WoW to load the new addon."}}
	// A follower never consumes the notice.
	if body := post("w2", "vmB"); strings.Contains(body, "notice") || len(b.notices) != 1 {
		t.Fatalf("a follower got or consumed the notice: %s", body)
	}
	clk.Advance(time.Second)
	body := post("w1", "vmA")
	var resp struct {
		Notice *model.Notice `json:"notice"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil || resp.Notice == nil || resp.Notice.Text != "ompcraft updated to 0.2.0" ||
		resp.Notice.Level != "info" || resp.Notice.Sub != "Type /reload in WoW to load the new addon." || resp.Notice.ID != "n1" {
		t.Fatalf("notice = %s (%v)", body, err)
	}
	if !strings.Contains(body, `"notice":{"id":"n1","level":"info","text":"ompcraft updated to 0.2.0","sub":"Type /reload in WoW to load the new addon."}`) {
		t.Errorf("wire shape: %s", body)
	}
	// Exactly once.
	if body := post("w1", "vmA"); strings.Contains(body, "notice") {
		t.Errorf("the notice was sent twice: %s", body)
	}
}
