package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sync/atomic"

	"github.com/TechDufus/ompcraft/companion/internal/config"
	"github.com/TechDufus/ompcraft/companion/internal/setup"
	"github.com/TechDufus/ompcraft/companion/internal/update"
)

// fakeRelease serves a signed release of ompcraft_<GOOS>_<GOARCH>.tar.gz plus
// checksums.txt and checksums.txt.sig.
type fakeRelease struct {
	srv   *httptest.Server
	tag   string
	asset []byte
	sums  string
	pub   string
	priv  ed25519.PrivateKey
	hits  atomic.Int64 // release queries served
}

func newFakeRelease(t *testing.T, tag string, binary []byte) *fakeRelease {
	t.Helper()
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRelease{tag: tag, pub: base64.StdEncoding.EncodeToString(pubKey), priv: privKey}
	name := fmt.Sprintf("ompcraft_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "ompcraft", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(binary)
	_ = tw.Close()
	_ = gz.Close()
	r.asset = buf.Bytes()
	h := sha256.Sum256(r.asset)
	r.sums = hex.EncodeToString(h[:]) + "  " + name + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/TechDufus/ompcraft/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		r.hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": r.tag, "assets": []map[string]string{
			{"name": name, "browser_download_url": r.srv.URL + "/dl/" + name},
			{"name": "checksums.txt", "browser_download_url": r.srv.URL + "/dl/checksums.txt"},
			{"name": "checksums.txt.sig", "browser_download_url": r.srv.URL + "/dl/checksums.txt.sig"},
		}})
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, r.sums) })
	mux.HandleFunc("/dl/checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, base64.StdEncoding.EncodeToString(ed25519.Sign(r.priv, []byte(r.sums))))
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(r.asset) })
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// updaterFor returns an Updater trusting r's key whose staged binary "reports"
// the release's version.
func updaterFor(t *testing.T, r *fakeRelease, version string) (*update.Updater, string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "ompcraft")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	tag := strings.TrimPrefix(r.tag, "v")
	return &update.Updater{BaseURL: r.srv.URL, Version: version, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Exe: exe,
		PublicKey:  r.pub,
		RunVersion: func(context.Context, string) (string, error) { return "ompcraft " + tag + " (release)\n", nil },
	}, exe
}

func runUpd(t *testing.T, u *update.Updater, ch string, check, force bool, restart func(context.Context) (string, error)) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runUpdate(context.Background(), updateRun{Out: &out, Updater: u, Channel: ch, Check: check, Force: force, Restart: restart})
	return out.String(), err
}

func restarted(calls *int) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		*calls++
		return "✓ Restarted systemd user unit ompcraft.service", nil
	}
}

func TestUpdateCheck(t *testing.T) {
	r := newFakeRelease(t, "v0.2.0", []byte("NEW"))
	u, exe := updaterFor(t, r, "0.1.0")
	var calls int
	out, err := runUpd(t, u, "release", true, false, restarted(&calls))
	if err != nil || out != "0.2.0 is available (you have 0.1.0)\n" {
		t.Fatalf("%q %v", out, err)
	}
	u.Version = "0.2.0"
	out, err = runUpd(t, u, "release", true, false, restarted(&calls))
	if err != nil || out != "up to date (0.2.0)\n" {
		t.Fatalf("%q %v", out, err)
	}
	// --check works for source builds too, and never changes anything.
	u.Version = "0.1.0"
	out, err = runUpd(t, u, "source", true, false, restarted(&calls))
	if err != nil || !strings.HasPrefix(out, "0.2.0 is available") {
		t.Fatalf("%q %v", out, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" || calls != 0 {
		t.Error("--check changed something")
	}
}

func TestUpdateApplies(t *testing.T) {
	r := newFakeRelease(t, "v0.2.0", []byte("NEW BINARY"))
	u, exe := updaterFor(t, r, "0.1.0")
	var calls int
	out, err := runUpd(t, u, "release", false, false, restarted(&calls))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := fmt.Sprintf("✓ Downloaded ompcraft 0.2.0 for %s/%s, signature and checksum OK\n✓ Replaced %s (the previous binary is kept as %s.old)\n✓ Restarted systemd user unit ompcraft.service\n  The new version refreshes the addon and the Tern plugin when it starts.\n",
		runtime.GOOS, runtime.GOARCH, exe, exe)
	if out != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out, want)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW BINARY" || calls != 1 {
		t.Errorf("exe=%q restarts=%d", b, calls)
	}
}

func TestUpdateUpToDateAndForce(t *testing.T) {
	r := newFakeRelease(t, "v0.1.0", []byte("SAME"))
	u, exe := updaterFor(t, r, "0.1.0")
	var calls int
	out, err := runUpd(t, u, "release", false, false, restarted(&calls))
	if err != nil || out != "up to date (0.1.0)\n" || calls != 0 {
		t.Fatalf("%q %v %d", out, err, calls)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Error("an up-to-date install was touched")
	}
	out, err = runUpd(t, u, "release", false, true, restarted(&calls))
	if err != nil || !strings.Contains(out, "✓ Replaced") || calls != 1 {
		t.Fatalf("--force: %q %v", out, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "SAME" {
		t.Error("--force did not reinstall")
	}
}

func TestUpdateRefusesSourceBuilds(t *testing.T) {
	r := newFakeRelease(t, "v0.2.0", []byte("NEW"))
	u, exe := updaterFor(t, r, "0.1.0")
	var calls int
	_, err := runUpd(t, u, "source", false, false, restarted(&calls))
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 || err.Error() != "built from source; update with your build method or re-run install.sh" {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" || calls != 0 {
		t.Error("a source build updated itself")
	}
	// --force overrides.
	if _, err := runUpd(t, u, "source", false, true, restarted(&calls)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW" {
		t.Error("--force on a source build must update")
	}
	// The command itself refuses before touching the network.
	old := channel
	channel = "source"
	defer func() { channel = old }()
	if err := cmdUpdate(nil); err == nil || err.Error() != "built from source; update with your build method or re-run install.sh" {
		t.Errorf("cmdUpdate: %v", err)
	}
}

func TestUpdateFailuresChangeNothing(t *testing.T) {
	r := newFakeRelease(t, "v0.2.0", []byte("NEW"))
	r.sums = "0000000000000000000000000000000000000000000000000000000000000000  ompcraft_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz\n"
	u, exe := updaterFor(t, r, "0.1.0")
	var calls int
	_, err := runUpd(t, u, "release", false, false, restarted(&calls))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" || calls != 0 {
		t.Error("state changed after a failed update")
	}
	// Unreachable API.
	r.srv.Close()
	if _, err := runUpd(t, u, "release", true, false, restarted(&calls)); err == nil || !strings.Contains(err.Error(), "cannot check for updates") {
		t.Errorf("err = %v", err)
	}
}

func TestUpdateRestartProblems(t *testing.T) {
	r := newFakeRelease(t, "v0.2.0", []byte("NEW"))
	u, _ := updaterFor(t, r, "0.1.0")
	out, err := runUpd(t, u, "release", false, false, func(context.Context) (string, error) { return "", errors.New("Failed to connect to bus") })
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 || !strings.Contains(out, "✗ Couldn't restart the service: Failed to connect to bus") || !strings.Contains(out, "Restart it yourself") {
		t.Fatalf("%q %v", out, err)
	}
	// No service at all is a note, not a failure.
	u2, _ := updaterFor(t, r, "0.1.0")
	out, err = runUpd(t, u2, "release", false, false, func(context.Context) (string, error) {
		return "• No ompcraft service is running; start the new version with `ompcraft run`.", nil
	})
	if err != nil || !strings.Contains(out, "• No ompcraft service is running") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestRestartServiceOnUnsupportedOS(t *testing.T) {
	msg, err := restartService(setup.Deps{GOOS: "freebsd", Env: config.EnvFrom(t.TempDir(), "", "")})(context.Background())
	if err != nil || !strings.Contains(msg, "No service manager here") {
		t.Fatalf("%q %v", msg, err)
	}
}

func TestVersionShowsTheChannel(t *testing.T) {
	capture := func() string {
		r, w, _ := os.Pipe()
		old := os.Stdout
		os.Stdout = w
		err := dispatch([]string{"version"})
		_ = w.Close()
		os.Stdout = old
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		return string(b)
	}
	if got := capture(); got != "ompcraft "+version+" (source)\n" {
		t.Errorf("default build: %q", got)
	}
	oldCh, oldV := channel, version
	channel, version = "release", "0.1.0"
	defer func() { channel, version = oldCh, oldV }()
	if got := capture(); got != "ompcraft 0.1.0 (release)\n" {
		t.Errorf("release build: %q", got)
	}
}

func TestConfigAutoupdate(t *testing.T) {
	env := isolatedEnv(t)
	if cfg, _ := env.LoadConfig(); !cfg.AutoupdateEnabled() {
		t.Fatal("autoupdate must default to on")
	}
	if err := dispatch([]string{"config", "autoupdate", "off"}); err != nil {
		t.Fatal(err)
	}
	got := readShape(t, env)
	if got["autoupdate"] != false || len(got) != 2 { // game "" and autoupdate
		t.Fatalf("config.json = %v", got)
	}
	cfg, _ := env.LoadConfig()
	if cfg.AutoupdateEnabled() || !cfg.DetailEnabled() {
		t.Errorf("cfg = %+v", cfg)
	}
	// The other option is kept when this one changes.
	if err := dispatch([]string{"config", "detail", "off"}); err != nil {
		t.Fatal(err)
	}
	if got := readShape(t, env); got["autoupdate"] != false || got["detail"] != false {
		t.Errorf("config.json = %v", got)
	}
	if err := dispatch([]string{"config", "autoupdate", "on"}); err != nil {
		t.Fatal(err)
	}
	if got := readShape(t, env); got["autoupdate"] != true {
		t.Errorf("config.json = %v", got)
	}
	if err := dispatch([]string{"config", "autoupdate"}); err != nil {
		t.Fatal(err)
	}
	var ee *exitError
	if err := dispatch([]string{"config", "autoupdate", "maybe"}); !errors.As(err, &ee) || ee.code != 2 {
		t.Errorf("bad value: %v", err)
	}
}

func TestManualUpdateRecordsPendingAndHintsReloadWhileTheGameRuns(t *testing.T) {
	r := newFakeRelease(t, "v0.2.0", []byte("NEW BINARY"))
	u, exe := updaterFor(t, r, "0.1.0")
	env := isolatedEnv(t)
	u.BeforeSwap, u.SwapFailed = update.StateHooks(env, "0.1.0")
	_ = env.UpdateState(func(s *config.State) { s.StagedVersion = "0.2.0"; s.BadVersion = "0.2.0" })

	var out bytes.Buffer
	run := func(force, gameRunning bool) error {
		out.Reset()
		return runUpdate(context.Background(), updateRun{Out: &out, Updater: u, Channel: "release", Force: force, Env: &env,
			Restart:     func(context.Context) (string, error) { return "✓ Restarted systemd user unit ompcraft.service", nil },
			GameRunning: func() bool { return gameRunning }})
	}
	// A version that failed to start here before is not retried without --force.
	err := run(false, false)
	var ee *exitError
	if !errors.As(err, &ee) || !strings.Contains(err.Error(), "failed to start on this machine before") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Fatal("a known-bad version was installed")
	}
	// --force applies immediately even with the game running (the user's explicit choice).
	if err := run(true, true); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW BINARY" {
		t.Error("the manual update did not apply")
	}
	if !strings.Contains(out.String(), "• WoW is running: type /reload in game to load the new addon.") {
		t.Errorf("output:\n%s", out.String())
	}
	st := env.LoadState()
	if st.PendingVersion != "0.2.0" || st.StartAttempts != 0 || st.PreviousVersion != "0.1.0" || st.StagedVersion != "" || st.BadVersion != "" {
		t.Errorf("state = %+v", st)
	}
	// Game closed: no /reload hint.
	u2, _ := updaterFor(t, r, "0.1.0")
	u = u2
	if err := run(true, false); err != nil || strings.Contains(out.String(), "/reload") {
		t.Errorf("err=%v output:\n%s", err, out.String())
	}
}

func TestManualUpdateWithoutAReleaseKeyIsRefused(t *testing.T) {
	r := newFakeRelease(t, "v0.2.0", []byte("NEW"))
	u, exe := updaterFor(t, r, "0.1.0")
	u.PublicKey = ""
	var calls int
	_, err := runUpd(t, u, "release", false, false, restarted(&calls))
	if err == nil || !strings.Contains(err.Error(), "this build has no release key; updates are disabled") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" || calls != 0 {
		t.Error("state changed")
	}
	// --check only compares versions and needs no key.
	if out, err := runUpd(t, u, "release", true, false, restarted(&calls)); err != nil || !strings.HasPrefix(out, "0.2.0 is available") {
		t.Errorf("--check: %q %v", out, err)
	}
}
