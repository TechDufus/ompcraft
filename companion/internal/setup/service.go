package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	unitName     = "ompcraft.service"
	launchdLabel = "dev.ompcraft.companion"
)

// ServiceResult is what Install did, for the status lines.
type ServiceResult struct {
	// Describe names the service ("systemd user unit ompcraft.service").
	Describe string
	// ReplacedTransient: a temporary unit (systemd-run) was stopped first.
	ReplacedTransient bool
}

// ServiceStatus is what doctor reports.
type ServiceStatus struct {
	Describe string
	Enabled  bool
	Active   bool
	Detail   string
}

// Service keeps the companion running in the background.
type Service interface {
	// Managed is false when there is no service manager to drive (the user
	// must run `ompcraft run` themselves).
	Managed() bool
	Describe() string
	Install(ctx context.Context, exe string) (ServiceResult, error)
	Status(ctx context.Context) (ServiceStatus, error)
	// Uninstall stops and removes the service and returns what it removed.
	Uninstall(ctx context.Context) ([]string, error)
	// Restart restarts the running service (a new binary takes effect).
	Restart(ctx context.Context) error
}

// NewService picks the service manager for d.GOOS.
func NewService(d Deps) Service {
	switch d.GOOS {
	case "linux":
		return &systemd{
			r:    d.Runner,
			path: filepath.Join(d.Env.ConfigHome, "systemd", "user", unitName),
		}
	case "darwin":
		return &launchd{
			r:    d.Runner,
			uid:  d.UID,
			path: filepath.Join(d.Env.Home, "Library", "LaunchAgents", launchdLabel+".plist"),
			log:  d.Env.LogPath(),
		}
	}
	return manual{goos: d.GOOS}
}

// manual is the "no service manager" stand-in for other operating systems.
type manual struct{ goos string }

func (m manual) Managed() bool    { return false }
func (m manual) Describe() string { return "no service manager on " + m.goos }
func (m manual) Install(context.Context, string) (ServiceResult, error) {
	return ServiceResult{}, errors.New("no service manager support on " + m.goos + " yet")
}
func (m manual) Status(context.Context) (ServiceStatus, error) {
	return ServiceStatus{Describe: m.Describe()}, nil
}
func (m manual) Uninstall(context.Context) ([]string, error) { return nil, nil }
func (m manual) Restart(context.Context) error {
	return errors.New("no service manager support on " + m.goos + " yet")
}

// ---- systemd (Linux) -------------------------------------------------------

type systemd struct {
	r    Runner
	path string // unit file
}

func (s *systemd) Managed() bool    { return true }
func (s *systemd) Describe() string { return "systemd user unit " + unitName }

func (s *systemd) ctl(ctx context.Context, args ...string) (string, error) {
	return s.r.Run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// systemdQuote quotes one ExecStart word the way systemd.syntax expects.
func systemdQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "$", "$$")
	if strings.ContainsAny(s, " \t") || s != strings.TrimSpace(s) {
		return `"` + s + `"`
	}
	return s
}

func unitFile(exe string) string {
	return "[Unit]\n" +
		"Description=ompcraft companion (shows your omp agents inside WoW: Forever)\n" +
		"Documentation=https://github.com/TechDufus/ompcraft\n" +
		"\n[Service]\n" +
		"ExecStart=" + systemdQuote(exe) + " run\n" +
		"Restart=on-failure\n" +
		"RestartSec=3\n" +
		"\n[Install]\n" +
		"WantedBy=default.target\n"
}

func parseProps(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			m[k] = v
		}
	}
	return m
}

// transient reports whether a unit named ompcraft exists that systemd-run
// created: it is loaded but has no unit file of ours.
func (s *systemd) transient(ctx context.Context) (isTransient, active bool, err error) {
	out, err := s.ctl(ctx, "show", unitName, "-p", "LoadState", "-p", "ActiveState", "-p", "FragmentPath")
	if err != nil {
		return false, false, err
	}
	p := parseProps(out)
	loaded := p["LoadState"] == "loaded"
	fp := p["FragmentPath"]
	isTransient = loaded && (fp == "" || strings.Contains(fp, "/transient/"))
	return isTransient, p["ActiveState"] == "active", nil
}

func (s *systemd) Install(ctx context.Context, exe string) (ServiceResult, error) {
	res := ServiceResult{Describe: s.Describe()}
	if _, err := s.r.LookPath("systemctl"); err != nil {
		return res, errors.New("systemctl not found")
	}
	trans, active, err := s.transient(ctx)
	if err != nil {
		return res, fmt.Errorf("the systemd user session is not available (%w)", err)
	}
	if trans {
		// Replace the unit started with systemd-run by a real one.
		_, _ = s.ctl(ctx, "stop", unitName)
		_, _ = s.ctl(ctx, "reset-failed", unitName)
		res.ReplacedTransient = true
		active = false
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return res, err
	}
	if _, err := writeFileIfDifferent(s.path, []byte(unitFile(exe))); err != nil {
		return res, err
	}
	if _, err := s.ctl(ctx, "daemon-reload"); err != nil {
		return res, err
	}
	if _, err := s.ctl(ctx, "enable", "--now", unitName); err != nil {
		return res, err
	}
	if active {
		// enable --now does not restart a running service; a new binary or a new
		// unit file must take effect.
		if _, err := s.ctl(ctx, "restart", unitName); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *systemd) Status(ctx context.Context) (ServiceStatus, error) {
	st := ServiceStatus{Describe: s.Describe()}
	if _, err := os.Stat(s.path); err != nil {
		st.Detail = "no unit file"
		return st, nil
	}
	en, _ := s.ctl(ctx, "is-enabled", unitName)
	ac, _ := s.ctl(ctx, "is-active", unitName)
	st.Enabled = strings.TrimSpace(en) == "enabled"
	st.Active = strings.TrimSpace(ac) == "active"
	st.Detail = fmt.Sprintf("%s, %s", strings.TrimSpace(en), strings.TrimSpace(ac))
	return st, nil
}

func (s *systemd) Restart(ctx context.Context) error {
	_, err := s.ctl(ctx, "restart", unitName)
	return err
}

func (s *systemd) Uninstall(ctx context.Context) ([]string, error) {
	var removed []string
	_, haveCtl := s.r.LookPath("systemctl")
	if haveCtl == nil {
		trans, _, err := s.transient(ctx)
		if err == nil && trans {
			_, _ = s.ctl(ctx, "stop", unitName)
			_, _ = s.ctl(ctx, "reset-failed", unitName)
			removed = append(removed, "temporary unit "+unitName)
		}
	}
	if _, err := os.Stat(s.path); err == nil {
		if haveCtl == nil {
			_, _ = s.ctl(ctx, "disable", "--now", unitName)
		}
		if err := os.Remove(s.path); err != nil {
			return removed, err
		}
		removed = append(removed, "service "+unitName+" (stopped, disabled) and "+s.path)
		if haveCtl == nil {
			_, _ = s.ctl(ctx, "daemon-reload")
		}
	}
	return removed, nil
}

// writeFileIfDifferent writes data unless name already holds it.
func writeFileIfDifferent(name string, data []byte) (bool, error) {
	if old, err := os.ReadFile(name); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	return true, os.WriteFile(name, data, 0o644)
}

// ---- launchd (macOS) -------------------------------------------------------

type launchd struct {
	r    Runner
	uid  int
	path string // plist
	log  string
}

func (l *launchd) Managed() bool    { return true }
func (l *launchd) Describe() string { return "launchd agent " + launchdLabel }

func (l *launchd) domain() string { return "gui/" + strconv.Itoa(l.uid) }
func (l *launchd) target() string { return l.domain() + "/" + launchdLabel }

func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func plistFile(exe, logPath string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + launchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(exe) + `</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(logPath) + `</string>
</dict>
</plist>
`
}

func (l *launchd) Install(ctx context.Context, exe string) (ServiceResult, error) {
	res := ServiceResult{Describe: l.Describe()}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return res, err
	}
	if _, err := writeFileIfDifferent(l.path, []byte(plistFile(exe, l.log))); err != nil {
		return res, err
	}
	// A loaded copy would make bootstrap fail: unload it first (it may not be loaded).
	_, _ = l.r.Run(ctx, "launchctl", "bootout", l.target())
	if _, err := l.r.Run(ctx, "launchctl", "bootstrap", l.domain(), l.path); err != nil {
		return res, err
	}
	_, _ = l.r.Run(ctx, "launchctl", "kickstart", "-k", l.target())
	return res, nil
}

func (l *launchd) Status(ctx context.Context) (ServiceStatus, error) {
	st := ServiceStatus{Describe: l.Describe()}
	if _, err := os.Stat(l.path); err != nil {
		st.Detail = "no plist"
		return st, nil
	}
	out, err := l.r.Run(ctx, "launchctl", "print", l.target())
	if err != nil {
		st.Detail = "not loaded"
		return st, nil
	}
	st.Enabled = true
	st.Active = strings.Contains(out, "state = running")
	st.Detail = "loaded"
	if st.Active {
		st.Detail = "loaded, running"
	}
	return st, nil
}

func (l *launchd) Restart(ctx context.Context) error {
	_, err := l.r.Run(ctx, "launchctl", "kickstart", "-k", l.target())
	return err
}

func (l *launchd) Uninstall(ctx context.Context) ([]string, error) {
	var removed []string
	if _, err := os.Stat(l.path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	_, _ = l.r.Run(ctx, "launchctl", "bootout", l.target())
	if err := os.Remove(l.path); err != nil {
		return removed, err
	}
	removed = append(removed, "launchd agent "+launchdLabel+" (unloaded) and "+l.path)
	return removed, nil
}
