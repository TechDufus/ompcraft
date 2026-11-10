package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/TechDufus/ompcraft"
	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/daemon"
	"github.com/TechDufus/ompcraft/companion/internal/demo"
	"github.com/TechDufus/ompcraft/companion/internal/gamedir"
	"github.com/TechDufus/ompcraft/companion/internal/layout"
	"github.com/TechDufus/ompcraft/companion/internal/logx"
	"github.com/TechDufus/ompcraft/companion/internal/model"
	"github.com/TechDufus/ompcraft/companion/internal/server"
	"github.com/TechDufus/ompcraft/companion/internal/setup"
	"github.com/TechDufus/ompcraft/companion/internal/update"
)

// runOptions is everything the daemon needs from the command line and the
// environment, so tests can drive runCompanion without real signals or /proc.
type runOptions struct {
	Env      config.Env
	Game     string // resolved G
	Port     int
	Verbose  bool
	FindGame func() (int, error)
	// Ready, if set, is called with the listen URL once link.json is written.
	Ready func(url string)

	// Version and Channel describe this build ("0.1.0", "release"/"source").
	Version string
	Channel string
	// Addon and Plugin are the embedded files used to self-heal on start; Runner
	// drives `tern`. A nil Addon skips self-healing.
	Addon, Plugin fs.FS
	Runner        setup.Runner
	// Updater, when set, makes the daemon check for releases (first after 2
	// min, then every 6 h) and update itself in place. The caller sets it only
	// for release builds with autoupdate on.
	Updater     *update.Updater
	UpdateAfter func(ctx context.Context, d time.Duration) error // tests: replaces the timer
	UpdateFirst time.Duration                                    // tests: replaces the 2 min delay
	// Exe is this binary's resolved path, read before any update replaces it;
	// Args and Environ are what to re-exec with; Exec performs the re-exec.
	Exe     string
	Args    []string
	Environ []string
	Exec    func(path string, argv, env []string) error
	// StableAfter waits d or returns ctx's error (nil: a real timer); a new
	// version that has served this long (StableDelay, default 30 s) is good.
	StableAfter func(ctx context.Context, d time.Duration) error
	StableDelay time.Duration
	// Demo feeds the daemon a scripted set of invented agents instead of the
	// plugin's HTTP feed: no server, no link.json, no self-heal, no updates.
	Demo bool
}

func cmdRun(args []string) error {
	fset := flag.NewFlagSet("run", flag.ContinueOnError)
	game := fset.String("game", "", "game flavour folder (…/World of Warcraft/_forever_)")
	port := fset.Int("port", 47131, "preferred loopback port (any free port if taken)")
	verbose := fset.Bool("v", false, "verbose: log every IN_OPEN and each publish")
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	env, err := config.SystemEnv()
	if err != nil {
		return err
	}
	g, err := resolveGame(env, *game)
	if err != nil {
		return err
	}
	cfg, err := env.LoadConfig()
	if err != nil {
		return err
	}
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	o := runOptions{
		Env: env, Game: g, Port: *port, Verbose: *verbose, FindGame: gamedir.FindGame,
		Version: version, Channel: channel,
		Addon: ompcraft.Addon, Plugin: ompcraft.TernPlugin, Runner: setup.ExecRunner{},
		Exe: exe, Args: os.Args, Environ: os.Environ(), Exec: execSelf,
	}
	// Auto-update only for release builds, and only if the config allows it.
	if channel == "release" && cfg.AutoupdateEnabled() {
		o.Updater = newUpdater(exe)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runCompanion(ctx, o)
}

// runCompanion runs the daemon until ctx ends, a component fails or an update
// was applied. On the way out it wipes every slot and removes link.json; after
// an update it then re-execs the new binary (same pid).
func runCompanion(ctx context.Context, o runOptions) error {
	reexec, err := runCore(ctx, o)
	if reexec == "" {
		return err
	}
	exec := o.Exec
	if exec == nil {
		exec = execSelf
	}
	if xerr := exec(reexec, o.Args, o.Environ); xerr != nil {
		return fmt.Errorf("updated, but starting the new version failed: %w", xerr)
	}
	return nil
}

func runCore(ctx context.Context, o runOptions) (reexec string, err error) {
	env := o.Env
	cfg, err := env.LoadConfig()
	if err != nil {
		return "", err
	}
	l, err := layout.ReadInstalled(layout.AddonsDir(o.Game))
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(env.StateDir(), 0o755); err != nil {
		return "", err
	}
	var w io.Writer = os.Stderr
	if f, err := os.OpenFile(env.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		defer f.Close()
		w = io.MultiWriter(os.Stderr, f)
	} else {
		fmt.Fprintf(os.Stderr, "ompcraft: cannot open log file: %v\n", err)
	}
	log := logx.New(w, o.Verbose)

	// A freshly installed version that keeps failing to start is rolled back
	// before anything else happens.
	if o.Exe != "" && o.Version != "" && !o.Demo {
		if update.StartupCheck(env, o.Version, o.Exe, log.Printf) == update.StartRolledBack {
			return o.Exe, nil
		}
	}

	if old, err := env.ReadLink(); err == nil {
		if st, err := server.FetchState(old.URL, old.Token, time.Second); err == nil {
			if o.Demo {
				return "", fmt.Errorf("the real companion is running (pid %d); stop it first with `systemctl --user stop ompcraft`, then run the demo", st.PID)
			}
			return "", fmt.Errorf("a companion is already running (pid %d, %s)", st.PID, old.URL)
		}
	}

	var token, url string
	var ln net.Listener
	if !o.Demo {
		token, err = config.NewToken()
		if err != nil {
			return "", err
		}
		ln, err = server.Listen(o.Port)
		if err != nil {
			return "", err
		}
		url = server.URL(ln)
	}

	updStatus := update.NewStatus(o.Channel, cfg.AutoupdateEnabled())
	if sv := env.LoadState().StagedVersion; sv != "" {
		updStatus.SetStaged(sv)
	}
	var ctl *update.Controller
	updated := make(chan struct{})
	var updatedOnce sync.Once
	triggerUpdate := func() { updatedOnce.Do(func() { close(updated) }) }
	d := daemon.New(daemon.Config{
		Layout:    l,
		Version:   o.Version,
		Log:       log,
		FindGame:  o.FindGame,
		Resume:    env.LoadState(),
		SaveState: env.SaveState,
		NoDetail:  !cfg.DetailEnabled(),
		UpdateInfo: func() daemon.UpdateInfo {
			i := updStatus.Info()
			u := daemon.UpdateInfo{Channel: i.Channel, Autoupdate: i.Autoupdate, Latest: i.Latest, LastError: i.LastError, Staged: i.Staged}
			if !i.LastCheck.IsZero() {
				u.LastCheck = i.LastCheck.UnixMilli()
			}
			return u
		},
		// The game exited: install a staged update, if there is one.
		OnGameExit: func() {
			if ctl == nil {
				return
			}
			switch applied, err := ctl.ApplyStaged(ctx); {
			case err != nil:
				log.Printf("update: %v", err)
			case applied:
				triggerUpdate()
			}
		},
	})
	if o.Updater != nil {
		if strings.TrimSpace(o.Updater.PublicKey) == "" {
			log.Printf("update: %v", update.ErrNoReleaseKey)
		} else {
			ctl = update.NewController(o.Updater, env, updStatus, time.Now, log.Printf, func() bool {
				pid, _ := o.FindGame()
				return pid != 0
			})
			ctl.OnStaged = func(v string) {
				d.QueueNotice(model.Notice{ID: "update-ready-" + v, Level: "info", Text: "ompcraft " + v + " is ready", Sub: "It installs after you quit WoW."})
			}
		}
	}
	var httpSrv *http.Server
	if !o.Demo {
		srv := server.New(token, d, log, nil)
		httpSrv = server.HTTPServer(srv.Handler())
		if err := env.WriteLink(config.Link{V: 0, URL: url, Token: token, PID: os.Getpid()}); err != nil {
			_ = ln.Close()
			return "", fmt.Errorf("writing %s: %w", env.LinkPath(), err)
		}
		// Deferred first, so it runs after the slots are wiped below.
		defer func() {
			if err := env.RemoveLinkIfOurs(token); err != nil {
				log.Printf("removing link.json: %v", err)
			}
		}()
		log.Printf("ompcraft %s (%s) listening on %s; game %s (%d slots, %d doorbells)", o.Version, o.Channel, url, o.Game, l.Slots, l.Signals)
		if o.Port != 0 && url != fmt.Sprintf("http://127.0.0.1:%d", o.Port) {
			log.Printf("port %d was busy; using %s (link.json has the address)", o.Port, url)
		}
	} else {
		log.Printf("ompcraft %s demo; game %s (%d slots, %d doorbells)", o.Version, o.Game, l.Slots, l.Signals)
	}
	if !cfg.DetailEnabled() {
		log.Printf("config: detail is off; snapshots carry no replies, steps, previews or last messages")
	}
	selfHeal(ctx, o, d, log)
	if o.Ready != nil {
		o.Ready(url)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	serveErr := make(chan error, 1)
	if httpSrv != nil {
		go func() { serveErr <- httpSrv.Serve(ln) }()
	}
	if o.Demo {
		log.Printf("demo: feeding invented agents (loop every %s); no HTTP server, no link.json. Ctrl-C to stop.", demo.Loop)
		go demo.Drive(ctx, d.Sync, 500*time.Millisecond, time.Now)
	}
	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(ctx) }()

	// Auto-update: gated by the caller (release channel, config autoupdate).
	switch {
	case ctl != nil:
		sched := update.NewScheduler(ctl.Check, log.Printf)
		if o.UpdateFirst > 0 {
			sched.First = o.UpdateFirst
		}
		sched.After = o.UpdateAfter
		log.Printf("update: auto-update is on (%s build); checking in about %s, then every %s", o.Channel, sched.First, sched.Every)
		go func() {
			if sched.Loop(ctx) {
				triggerUpdate()
			}
		}()
	case o.Updater != nil:
		// the missing release key was logged above
	case o.Channel != "release":
		log.Printf("update: auto-update is off (%s build)", o.Channel)
	default:
		log.Printf("update: auto-update is off (config autoupdate=false)")
	}

	// A freshly installed version counts as good once it has served for 30 s.
	go func() {
		stable := o.StableAfter
		if stable == nil {
			stable = sleepCtx
		}
		delay := o.StableDelay
		if delay <= 0 {
			delay = 30 * time.Second
		}
		if stable(ctx, delay) == nil {
			update.MarkStable(env, o.Version)
		}
	}()

	var result error
	select {
	case err := <-runErr:
		result = err
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			result = fmt.Errorf("http server: %w", err)
		}
	case <-updated:
		reexec = o.Exe
	case <-ctx.Done():
	}
	cancel()
	shutdownCtx, stopTimer := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopTimer()
	if httpSrv != nil {
		_ = httpSrv.Shutdown(shutdownCtx)
	}
	if reexec != "" {
		log.Printf("shutting down to start the new version")
	} else {
		log.Printf("shutting down")
	}
	d.Shutdown() // wipe every slot before link.json goes away
	return reexec, result
}

// selfHeal brings the addon and the Tern plugin copy in line with this binary
// and tells the player about it (PROTOCOL §8.7, §8.8).
func selfHeal(ctx context.Context, o runOptions, d *daemon.Daemon, log *logx.Logger) {
	if o.Addon == nil {
		return
	}
	runner := o.Runner
	if runner == nil {
		runner = setup.ExecRunner{}
	}
	res, err := setup.SelfHeal(ctx, setup.Deps{Env: o.Env, Addon: o.Addon, Plugin: o.Plugin, Runner: runner}, o.Game)
	for _, n := range res.Notes {
		log.Printf("%s", n)
	}
	if err != nil {
		log.Printf("self-heal: %v", err)
	}
	if !res.Changed() {
		return
	}
	pid, _ := o.FindGame()
	sub := "The new addon loads the next time you start WoW."
	if pid != 0 && res.AddonReplaced {
		// The client already loaded the old addon: tell the player in game too.
		d.AddToast(model.ToastUpdate, fmt.Sprintf("ompcraft updated to %s. Type /reload to load it.", o.Version))
		sub = "Type /reload in WoW to load the new addon."
	}
	d.QueueNotice(model.Notice{ID: "update-" + o.Version, Level: "info", Text: "ompcraft updated to " + o.Version, Sub: sub})
}

// sleepCtx waits d or returns ctx's error.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
