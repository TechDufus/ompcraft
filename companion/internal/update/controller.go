package update

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
)

// Controller is the daemon's update policy: never replace the binary while the
// game runs. It checks as scheduled; with the game running it only downloads,
// verifies and stages the binary, and installs it when the game quits (or at
// a later check that finds no game).
type Controller struct {
	U           *Updater
	Env         config.Env
	Status      *Status
	Now         func() time.Time
	Log         func(format string, args ...any)
	GameRunning func() bool
	// OnStaged is called once per staged version (the one-shot Tern notice).
	OnStaged func(version string)

	mu sync.Mutex
}

// NewController wires the updater's swap hooks to state.json.
func NewController(u *Updater, env config.Env, st *Status, now func() time.Time, log func(string, ...any), game func() bool) *Controller {
	c := &Controller{U: u, Env: env, Status: st, Now: now, Log: log, GameRunning: game}
	c.U.BeforeSwap, c.U.SwapFailed = StateHooks(env, u.Version)
	return c
}

// StateHooks returns Updater.BeforeSwap and Updater.SwapFailed: before the
// swap, state.json records {pendingVersion, startAttempts: 0} and the version
// being replaced (the rollback target); if the swap fails the record goes away.
func StateHooks(env config.Env, running string) (before func(string) error, failed func(string)) {
	before = func(v string) error {
		return env.UpdateState(func(s *config.State) {
			s.PendingVersion, s.StartAttempts, s.PreviousVersion = v, 0, running
			s.StagedVersion = ""
		})
	}
	failed = func(v string) {
		_ = env.UpdateState(func(s *config.State) {
			if s.PendingVersion == v {
				s.PendingVersion, s.StartAttempts, s.PreviousVersion = "", 0, ""
			}
		})
	}
	return before, failed
}

// Check runs one scheduled check. applied reports that the binary was
// replaced and the daemon should re-exec.
func (c *Controller) Check(ctx context.Context) (applied bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rel, err := c.U.Latest(ctx)
	if err != nil {
		c.Status.Record(c.Now(), "", err)
		return false, err
	}
	newer, err := c.U.Newer(rel)
	if err != nil {
		c.Status.Record(c.Now(), rel.Version.String(), err)
		return false, err
	}
	v := rel.Version.String()
	if !newer {
		c.Status.Record(c.Now(), v, nil)
		c.Log("update: up to date (running %s, latest %s)", c.U.Version, v)
		return false, nil
	}
	st := c.Env.LoadState()
	if st.BadVersion == v {
		c.Status.Record(c.Now(), v, nil)
		c.Log("update: %s is available but failed to start before; not applying it", v)
		return false, nil
	}
	if c.GameRunning() {
		if st.StagedVersion == v && c.U.HasStaged() {
			c.Status.Record(c.Now(), v, nil)
			c.Log("update: %s is ready; it installs after you quit WoW", v)
			return false, nil
		}
		c.Log("update: %s is available (running %s); downloading and verifying it while WoW runs", v, c.U.Version)
		if err := c.U.Stage(ctx, rel); err != nil {
			err = fmt.Errorf("staging %s: %w", v, err)
			c.Status.Record(c.Now(), v, err)
			return false, err
		}
		_ = c.Env.UpdateState(func(s *config.State) { s.StagedVersion = v })
		c.Status.Record(c.Now(), v, nil)
		c.Status.SetStaged(v)
		c.Log("update: %s is ready; it installs after you quit WoW", v)
		if c.OnStaged != nil {
			c.OnStaged(v)
		}
		return false, nil
	}
	c.Log("update: %s is available (running %s); downloading", v, c.U.Version)
	if st.StagedVersion == v && c.U.HasStaged() {
		c.Log("update: using the staged %s", v)
	} else if err := c.U.Stage(ctx, rel); err != nil {
		err = fmt.Errorf("staging %s: %w", v, err)
		c.Status.Record(c.Now(), v, err)
		return false, err
	}
	if err := c.U.Install(ctx, v); err != nil {
		err = fmt.Errorf("installing %s: %w", v, err)
		c.Status.Record(c.Now(), v, err)
		return false, err
	}
	c.Status.Record(c.Now(), v, nil)
	c.Status.SetStaged("")
	c.Log("update: replaced %s with %s (previous binary kept as %s); restarting into it", c.U.Exe, v, c.U.OldPath())
	return true, nil
}

// ApplyStaged installs a staged binary, if there is one. The daemon calls it
// when the game exits. applied reports that the binary was replaced.
func (c *Controller) ApplyStaged(ctx context.Context) (applied bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.Env.LoadState()
	if st.StagedVersion == "" || !c.U.HasStaged() {
		return false, nil
	}
	v := st.StagedVersion
	if st.BadVersion == v {
		c.U.DiscardStaged()
		_ = c.Env.UpdateState(func(s *config.State) { s.StagedVersion = "" })
		return false, nil
	}
	c.Log("update: WoW has exited; installing the staged %s", v)
	if err := c.U.Install(ctx, v); err != nil {
		_ = c.Env.UpdateState(func(s *config.State) { s.StagedVersion = "" })
		c.Status.SetStaged("")
		c.Status.Record(c.Now(), v, fmt.Errorf("installing staged %s: %w", v, err))
		return false, fmt.Errorf("installing the staged %s: %w", v, err)
	}
	c.Status.SetStaged("")
	c.Log("update: replaced %s with %s (previous binary kept as %s); restarting into it", c.U.Exe, v, c.U.OldPath())
	return true, nil
}
