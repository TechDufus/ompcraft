package update

import (
	"fmt"

	"github.com/TechDufus/ompcraft/companion/internal/config"
)

// MaxStartAttempts is how many starts a freshly installed version gets before
// the previous binary is restored.
const MaxStartAttempts = 3

// StartResult is what StartupCheck decided.
type StartResult int

const (
	// StartNormal: carry on starting.
	StartNormal StartResult = iota
	// StartRolledBack: the previous binary was restored; re-exec it.
	StartRolledBack
)

// StartupCheck runs first thing in `ompcraft run`. It guards a freshly
// installed version (state.json's pendingVersion):
//
//   - a pending version that is not the running one is stale: forget it;
//   - the running pending version counts this start (startAttempts++);
//   - once startAttempts has reached MaxStartAttempts, the next start restores
//     <exe>.old over the executable, records the version as bad so it is never
//     applied again, and asks the caller to re-exec the old binary.
func StartupCheck(env config.Env, running, exe string, log func(string, ...any)) StartResult {
	st := env.LoadState()
	if st.PendingVersion == "" {
		return StartNormal
	}
	if st.PendingVersion != running {
		_ = env.UpdateState(func(s *config.State) { s.PendingVersion, s.StartAttempts, s.PreviousVersion = "", 0, "" })
		return StartNormal
	}
	if st.StartAttempts < MaxStartAttempts {
		_ = env.UpdateState(func(s *config.State) { s.StartAttempts++ })
		log("update: starting %s for the %s time since the update (it must serve for 30 s to count as good)",
			running, ordinal(st.StartAttempts+1))
		return StartNormal
	}
	old := st.PreviousVersion
	if old == "" {
		old = "the previous version"
	}
	u := &Updater{Exe: exe}
	bad := st.PendingVersion
	markBad := func() {
		_ = env.UpdateState(func(s *config.State) {
			s.PendingVersion, s.StartAttempts, s.PreviousVersion, s.BadVersion = "", 0, "", bad
		})
	}
	if err := u.Rollback(); err != nil {
		log("update: %s failed to start %d times, but it cannot be rolled back: %v", bad, MaxStartAttempts, err)
		markBad()
		return StartNormal
	}
	markBad()
	log("update: %s failed to start %d times; rolled back to %s", bad, MaxStartAttempts, old)
	return StartRolledBack
}

// MarkStable clears the pending record once the new process has served for long
// enough.
func MarkStable(env config.Env, running string) {
	_ = env.UpdateState(func(s *config.State) {
		if s.PendingVersion == running {
			s.PendingVersion, s.StartAttempts, s.PreviousVersion = "", 0, ""
		}
	})
}

func ordinal(n int) string {
	switch n {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	}
	return fmt.Sprintf("%dth", n)
}
