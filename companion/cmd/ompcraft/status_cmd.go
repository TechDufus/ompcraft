package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/server"
	"github.com/TechDufus/ompcraft/companion/internal/setup"
)

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	env, err := config.SystemEnv()
	if err != nil {
		return err
	}
	link, err := env.ReadLink()
	if err != nil {
		return &exitError{code: 1, err: errors.New("companion is not running (no link.json); start it with `ompcraft run`")}
	}
	st, err := server.FetchState(link.URL, link.Token, 3*time.Second)
	if err != nil {
		return &exitError{code: 1, err: fmt.Errorf("companion is not running (%v)", err)}
	}
	printStatus(os.Stdout, link.URL, st, time.Now())
	return nil
}

func printStatus(w io.Writer, url string, st server.StateResponse, now time.Time) {
	fmt.Fprintf(w, "ompcraft companion %s (pid %d) at %s\n", st.Version, st.PID, url)
	if st.LastSyncMs == 0 {
		fmt.Fprintf(w, "  tern:     %s (no sync received yet)\n", st.Snapshot.Tern)
	} else {
		ago := now.Sub(time.UnixMilli(st.LastSyncMs)).Round(100 * time.Millisecond)
		fmt.Fprintf(w, "  tern:     %s (last sync %s ago)\n", st.Snapshot.Tern, ago)
	}
	if st.Leader.VM != "" {
		fmt.Fprintf(w, "  leader:   window=%s vm=%s active=%t\n", st.Leader.Window, st.Leader.VM, st.Leader.Active)
	}
	if st.GamePid != 0 {
		fmt.Fprintf(w, "  game:     running (pid %d)\n", st.GamePid)
	} else {
		fmt.Fprintf(w, "  game:     not running\n")
	}
	fmt.Fprintf(w, "  detail:   %s\n", onOff(st.DetailEnabled))
	fmt.Fprintf(w, "  update:   %s\n", setup.DescribeUpdate(st.Update, now))
	if st.Session != 0 {
		fmt.Fprintf(w, "  session:  UI session started %s\n", time.Unix(st.Session, 0).Format(time.RFC3339))
	} else {
		fmt.Fprintf(w, "  session:  no UI session seen\n")
	}
	fmt.Fprintf(w, "  slots:    %d/%d loaded; next doorbell %d/%d\n", len(st.LoadedSlots), st.Slots, st.SignalNext, st.Signals)

	counts := map[string]int{}
	for _, a := range st.Snapshot.Agents {
		counts[a.State]++
	}
	var parts []string
	for s, n := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", n, s))
	}
	sort.Strings(parts)
	summary := ""
	if len(parts) > 0 {
		summary = " (" + strings.Join(parts, ", ") + ")"
	}
	fmt.Fprintf(w, "  agents:   %d%s\n", len(st.Snapshot.Agents), summary)
	c := st.Counters
	fmt.Fprintf(w, "  publish:  gen %d; %d publishes, %d slot writes, %d doorbells rung\n",
		st.Snapshot.Gen, c.Publishes, c.SlotWrites, c.Doorbells)
	fmt.Fprintf(w, "  inotify:  %v; %d UI session starts, %d new launches\n", c.Inotify, c.SessionStarts, c.NewLaunches)
}
