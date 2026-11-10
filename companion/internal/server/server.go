// Package server is the loopback HTTP API the Tern plugin talks to
// (PROTOCOL §3) plus the authenticated GET /v1/state debug endpoint.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/daemon"
	"github.com/TechDufus/ompcraft/companion/internal/logx"
	"github.com/TechDufus/ompcraft/companion/internal/model"
)

const (
	LeaderTimeout    = 5 * time.Second
	LeaderIntervalMs = 500
	FollowerInterval = 3000
	maxBody          = 8 << 20
)

// Backend is what the server needs from the daemon.
type Backend interface {
	Sync(req *model.SyncRequest)
	Report() daemon.Report
	// TakeNotice returns the next one-shot notice, once, or nil.
	TakeNotice() *model.Notice
}

// LeaderInfo describes the current leader in /v1/state.
type LeaderInfo struct {
	Window     string `json:"window"`
	VM         string `json:"vm"`
	LastSeenMs int64  `json:"lastSeenMs"`
	Active     bool   `json:"active"` // heard from within LeaderTimeout
}

// Leader implements PROTOCOL §3.3: one (window, vm) pair leads; a leader that
// is silent for LeaderTimeout loses leadership to the next caller.
type Leader struct {
	Now func() time.Time

	mu       sync.Mutex
	window   string
	vm       string
	has      bool
	lastSeen time.Time
}

func (l *Leader) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Check reports whether (window, vm) is the leader. The seat is taken when it
// is empty, when the old leader was silent for more than LeaderTimeout, or
// when the caller has the leader's window key but a different vm: a reloaded
// plugin VM replaces the old one in that window, which is gone. Only the
// leader's own calls refresh the lease.
func (l *Leader) Check(window, vm string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	sameWindow := l.has && l.window == window
	if !l.has || sameWindow || now.Sub(l.lastSeen) > LeaderTimeout {
		l.has, l.window, l.vm, l.lastSeen = true, window, vm, now
		return true
	}
	return false
}

// Info returns the current leader.
func (l *Leader) Info() LeaderInfo {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.has {
		return LeaderInfo{}
	}
	return LeaderInfo{
		Window:     l.window,
		VM:         l.vm,
		LastSeenMs: l.lastSeen.UnixMilli(),
		Active:     l.now().Sub(l.lastSeen) <= LeaderTimeout,
	}
}

// Server is the HTTP handler set.
type Server struct {
	Token   string
	Backend Backend
	Leader  *Leader
	Log     *logx.Logger
}

// New builds a Server with a fresh Leader on the given clock (nil = real time).
func New(token string, b Backend, log *logx.Logger, now func() time.Time) *Server {
	if log == nil {
		log = logx.Discard()
	}
	return &Server{Token: token, Backend: b, Leader: &Leader{Now: now}, Log: log}
}

// Handler returns the routed handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sync", s.auth(s.handleSync))
	mux.HandleFunc("/v1/state", s.auth(s.handleState))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	want := []byte("Bearer " + s.Token)
	return func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ompcraft"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

type syncResponse struct {
	V          int  `json:"v"`
	Leader     bool `json:"leader"`
	IntervalMs int  `json:"interval_ms"`
	// Notice is a one-shot message for the plugin to show (PROTOCOL §8.8).
	Notice *model.Notice `json:"notice,omitempty"`
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req model.SyncRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.V != 0 {
		http.Error(w, fmt.Sprintf("bad request: unsupported protocol version %d", req.V), http.StatusBadRequest)
		return
	}
	resp := syncResponse{V: 0, Leader: s.Leader.Check(req.Window, req.VM)}
	if resp.Leader {
		resp.IntervalMs = LeaderIntervalMs
		s.Backend.Sync(&req)
		// A notice is handed to the leader once; receiving it is the ack.
		resp.Notice = s.Backend.TakeNotice()
	} else {
		resp.IntervalMs = FollowerInterval
	}
	writeJSON(w, resp)
}

// StateResponse is the body of GET /v1/state.
type StateResponse struct {
	PID    int        `json:"pid"`
	Leader LeaderInfo `json:"leader"`
	daemon.Report
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, StateResponse{PID: os.Getpid(), Leader: s.Leader.Info(), Report: s.Backend.Report()})
}

// FetchState calls GET /v1/state on a running companion.
func FetchState(baseURL, token string, timeout time.Duration) (StateResponse, error) {
	var out StateResponse
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/state", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("companion answered %s", resp.Status)
	}
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// Listen binds 127.0.0.1:preferred, or any free loopback port if that is taken.
func Listen(preferred int) (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferred))
	if err == nil {
		return ln, nil
	}
	ln, err2 := net.Listen("tcp", "127.0.0.1:0")
	if err2 != nil {
		return nil, fmt.Errorf("listen: %w", err2)
	}
	return ln, nil
}

// URL returns the http base URL of ln.
func URL(ln net.Listener) string {
	return "http://" + strings.TrimPrefix(ln.Addr().String(), "tcp://")
}

// HTTPServer wraps h with conservative timeouts.
func HTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
