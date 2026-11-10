package update

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	// FirstCheckDelay is how long after start the daemon first checks.
	FirstCheckDelay = 2 * time.Minute
	// CheckEvery is the interval between later checks.
	CheckEvery = 6 * time.Hour
	// JitterFraction is the ± spread applied to every delay.
	JitterFraction = 0.10
)

// Status is what /v1/state, `ompcraft status` and `doctor` show.
type Status struct {
	mu         sync.Mutex
	channel    string
	autoupdate bool
	lastCheck  time.Time
	latest     string
	lastError  string
	staged     string
}

// NewStatus starts a status for a build channel and the autoupdate setting.
func NewStatus(channel string, autoupdate bool) *Status {
	return &Status{channel: channel, autoupdate: autoupdate}
}

// Info is a copy of the status.
type Info struct {
	Channel    string
	Autoupdate bool
	LastCheck  time.Time
	Latest     string
	LastError  string
	Staged     string // version waiting for the game to quit
}

// Record notes one finished check.
func (s *Status) Record(now time.Time, latest string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCheck = now
	if latest != "" {
		s.latest = latest
	}
	s.lastError = ""
	if err != nil {
		s.lastError = err.Error()
	}
}

// SetStaged records the version waiting to be installed after the game quits
// ("" = none).
func (s *Status) SetStaged(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staged = v
}

// Info returns the current values.
func (s *Status) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{Channel: s.channel, Autoupdate: s.autoupdate, LastCheck: s.lastCheck, Latest: s.latest, LastError: s.lastError, Staged: s.staged}
}

// Scheduler runs update checks: once FirstCheckDelay after start, then every
// CheckEvery, each delay spread by ±JitterFraction. Every piece of time is
// injectable.
type Scheduler struct {
	First, Every time.Duration
	Jitter       float64
	// Rand returns a number in [0, 1); nil uses math/rand.
	Rand func() float64
	// After waits d or returns ctx's error; nil uses a real timer.
	After func(ctx context.Context, d time.Duration) error
	// Check does one check (and applies an update if there is one). applied
	// says the binary was replaced and the loop should stop.
	Check func(ctx context.Context) (applied bool, err error)
	// Log receives one line per check outcome.
	Log func(format string, args ...any)
}

// NewScheduler returns a Scheduler with the spec's timings.
func NewScheduler(check func(context.Context) (bool, error), log func(string, ...any)) *Scheduler {
	return &Scheduler{First: FirstCheckDelay, Every: CheckEvery, Jitter: JitterFraction, Check: check, Log: log}
}

// Delay applies the jitter to base: base × (1 + Jitter × (2r − 1)).
func (s *Scheduler) Delay(base time.Duration) time.Duration {
	r := rand.Float64
	if s.Rand != nil {
		r = s.Rand
	}
	f := 1 + s.Jitter*(2*r()-1)
	return time.Duration(float64(base) * f)
}

func (s *Scheduler) wait(ctx context.Context, d time.Duration) error {
	if s.After != nil {
		return s.After(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Loop runs the checks until ctx ends or an update was applied. It returns
// true if one was applied.
func (s *Scheduler) Loop(ctx context.Context) bool {
	d := s.Delay(s.First)
	for {
		if s.Log != nil {
			s.Log("update: next check in %s", d.Round(time.Second))
		}
		if err := s.wait(ctx, d); err != nil {
			return false
		}
		applied, err := s.Check(ctx)
		if err != nil && s.Log != nil {
			s.Log("update: check failed: %v", err)
		}
		if applied {
			return true
		}
		d = s.Delay(s.Every)
	}
}
