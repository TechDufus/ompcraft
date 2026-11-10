package update

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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSemver(t *testing.T) {
	cmp := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1}, {"v0.2.0", "0.1.0", 1}, {"1.0.0", "v1.0.0", 0},
		{"0.9.0", "0.10.0", -1}, {"1.2.3", "1.2.10", -1}, {"2.0.0", "1.99.99", 1},
		{"1.0.0-rc.1", "1.0.0", -1}, {"1.0.0", "1.0.0-rc.1", 1}, {"1.0.0-rc.1", "1.0.0-rc.2", -1},
		{"1.0.0+build5", "1.0.0", 0},
	}
	for _, c := range cmp {
		a, err1 := ParseVersion(c.a)
		b, err2 := ParseVersion(c.b)
		if err1 != nil || err2 != nil {
			t.Fatalf("%s / %s: %v %v", c.a, c.b, err1, err2)
		}
		if got := a.Compare(b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "1.2.x", "v1.2.3.4", "01.2.3", "1.2.3-", "latest", "-1.0.0"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) must fail", bad)
		}
	}
	v, _ := ParseVersion("v1.2.3-beta.1")
	if !v.Prerelease() || v.String() != "1.2.3-beta.1" {
		t.Errorf("%+v %s", v, v)
	}
}

// ---- signing ---------------------------------------------------------------

func newKey(t *testing.T) (pubB64 string, priv ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub), priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := base64.StdEncoding.DecodeString(pub); len(raw) != ed25519.PublicKeySize {
		t.Errorf("public key is %d bytes", len(raw))
	}
	if raw, _ := base64.StdEncoding.DecodeString(priv); len(raw) != ed25519.PrivateKeySize {
		t.Errorf("private key is %d bytes", len(raw))
	}
	msg := []byte("abc  ompcraft_linux_amd64.tar.gz\n")
	sig, err := Sign(priv, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(pub, msg, []byte(sig+"\n")); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifySignature(pub, append(msg, 'x'), []byte(sig)); err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("tampered message accepted: %v", err)
	}
	other, _, _ := GenerateKey()
	if err := VerifySignature(other, msg, []byte(sig)); err == nil {
		t.Error("a signature by another key was accepted")
	}
	for _, bad := range []string{"", "!!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if err := VerifySignature(pub, msg, []byte(bad)); err == nil {
			t.Errorf("malformed signature %q accepted", bad)
		}
	}
	if err := VerifySignature("", msg, []byte(sig)); !errors.Is(err, ErrNoReleaseKey) {
		t.Errorf("empty key: %v", err)
	}
	if err := VerifySignature("###", msg, []byte(sig)); err == nil {
		t.Error("an invalid public key was accepted")
	}
	// Errors never contain key material.
	for _, badKey := range []string{priv + "x", "AAAA", "not base64 !!"} {
		_, err := Sign(badKey, msg)
		if err == nil || strings.Contains(err.Error(), badKey) || strings.Contains(err.Error(), priv) {
			t.Errorf("Sign with a bad key: %v", err)
		}
	}
	if ReleasePublicKey != "" {
		if _, err := decodeKey(ReleasePublicKey, ed25519.PublicKeySize, "key"); err != nil {
			t.Errorf("the embedded release key is invalid: %v", err)
		}
	}
}

// ---- a fake GitHub ---------------------------------------------------------

type release struct {
	mu         sync.Mutex
	tag        string
	prerel     bool
	assets     map[string][]byte // served bodies; checksums.txt included
	omit       map[string]bool   // assets not listed in the API answer
	signer     ed25519.PrivateKey
	sigBody    string // overrides the served signature when set
	userAgent  string
	requests   []string
	srv        *httptest.Server
	archiveHit int
}

func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func sumLine(name string, data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) + "  " + name + "\n"
}

func newRelease(t *testing.T, tag, goos, goarch string, binary []byte, signer ed25519.PrivateKey) *release {
	t.Helper()
	r := &release{tag: tag, assets: map[string][]byte{}, omit: map[string]bool{}, signer: signer}
	name := fmt.Sprintf("ompcraft_%s_%s.tar.gz", goos, goarch)
	r.assets[name] = tarGz(t, map[string][]byte{"LICENSE": []byte("mit"), "ompcraft": binary})
	r.assets["checksums.txt"] = []byte(sumLine(name, r.assets[name]))
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/TechDufus/ompcraft/releases/latest", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.userAgent = req.Header.Get("User-Agent")
		r.requests = append(r.requests, req.URL.Path)
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		var assets []asset
		names := []string{"checksums.txt", "checksums.txt.sig"}
		for n := range r.assets {
			if n != "checksums.txt" {
				names = append(names, n)
			}
		}
		for _, n := range names {
			if !r.omit[n] {
				assets = append(assets, asset{n, r.srv.URL + "/download/" + n})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": r.tag, "prerelease": r.prerel, "assets": assets})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		name := strings.TrimPrefix(req.URL.Path, "/download/")
		r.requests = append(r.requests, req.URL.Path)
		if strings.HasSuffix(name, ".tar.gz") {
			r.archiveHit++
		}
		if name == "checksums.txt.sig" {
			body := r.sigBody
			if body == "" {
				body = base64.StdEncoding.EncodeToString(ed25519.Sign(r.signer, r.assets["checksums.txt"]))
			}
			_, _ = w.Write([]byte(body))
			return
		}
		data, ok := r.assets[name]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(data)
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

func (r *release) downloaded(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.requests {
		if q == "/download/"+name {
			return true
		}
	}
	return false
}

// env bundles a fake release with a matching updater.
type env struct {
	r     *release
	u     *Updater
	exe   string
	pub   string
	priv  ed25519.PrivateKey
	swaps []string
}

func fakeVersion(version string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) { return "ompcraft " + version + " (release)\n", nil }
}

func newEnv(t *testing.T, tag, running string, binary []byte) *env {
	t.Helper()
	pub, priv := newKey(t)
	e := &env{pub: pub, priv: priv}
	e.r = newRelease(t, tag, "linux", "amd64", binary, priv)
	dir := t.TempDir()
	e.exe = filepath.Join(dir, "ompcraft")
	if err := os.WriteFile(e.exe, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.u = &Updater{BaseURL: e.r.srv.URL, Version: running, GOOS: "linux", GOARCH: "amd64", Exe: e.exe, PublicKey: pub,
		RunVersion: fakeVersion(strings.TrimPrefix(tag, "v")),
		BeforeSwap: func(v string) error { e.swaps = append(e.swaps, "before:"+v); return nil },
		SwapFailed: func(v string) { e.swaps = append(e.swaps, "failed:"+v) },
	}
	return e
}

func (e *env) latest(t *testing.T) Release {
	t.Helper()
	rel, err := e.u.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(b)
}

func TestLatestAndNewer(t *testing.T) {
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	rel := e.latest(t)
	if e.r.userAgent != "ompcraft/0.1.0" {
		t.Errorf("User-Agent = %q", e.r.userAgent)
	}
	if rel.Tag != "v0.2.0" || rel.Version.String() != "0.2.0" || rel.Prerelease {
		t.Errorf("release = %+v", rel)
	}
	if newer, err := e.u.Newer(rel); err != nil || !newer {
		t.Errorf("0.2.0 must be newer than 0.1.0: %v %v", newer, err)
	}
	e.u.Version = "0.2.0"
	if newer, _ := e.u.Newer(rel); newer {
		t.Error("same version is not newer")
	}
	e.u.Version = "0.3.0"
	if newer, _ := e.u.Newer(rel); newer {
		t.Error("an older release is not newer")
	}
	e.u.Version = "nonsense"
	if _, err := e.u.Newer(rel); err == nil {
		t.Error("an unparsable running version must be an error")
	}
	for _, tc := range []struct {
		tag    string
		prerel bool
	}{{"v0.9.0-rc.1", false}, {"v0.9.0", true}} {
		e.r.tag, e.r.prerel = tc.tag, tc.prerel
		e.u.Version = "0.1.0"
		rel := e.latest(t)
		if newer, _ := e.u.Newer(rel); newer {
			t.Errorf("%+v must be ignored", tc)
		}
		if err := e.u.Apply(context.Background(), rel); err == nil || !strings.Contains(err.Error(), "pre-release") {
			t.Errorf("Apply of a pre-release: %v", err)
		}
	}
	e.r.tag = "latest"
	if _, err := e.u.Latest(context.Background()); err == nil {
		t.Error("an unusable tag must be an error")
	}
}

func TestLatestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "rate limited", 403) }))
	defer srv.Close()
	u := &Updater{BaseURL: srv.URL, Version: "0.1.0"}
	if _, err := u.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v", err)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) }))
	defer srv2.Close()
	u.BaseURL = srv2.URL
	if _, err := u.Latest(context.Background()); err == nil {
		t.Error("bad JSON must be an error")
	}
}

func TestBaseURLFromEnv(t *testing.T) {
	t.Setenv("OMPCRAFT_UPDATE_URL", "")
	if BaseURLFromEnv() != "https://api.github.com" {
		t.Error("default")
	}
	t.Setenv("OMPCRAFT_UPDATE_URL", "http://127.0.0.1:9999/")
	if BaseURLFromEnv() != "http://127.0.0.1:9999" {
		t.Errorf("override = %q", BaseURLFromEnv())
	}
}

// ---- signature gate --------------------------------------------------------

func TestEmptyReleaseKeyDisablesUpdates(t *testing.T) {
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	e.u.PublicKey = ""
	rel := e.latest(t)
	for name, fn := range map[string]func() error{
		"Fetch": func() error { _, err := e.u.Fetch(context.Background(), rel); return err },
		"Stage": func() error { return e.u.Stage(context.Background(), rel) },
		"Apply": func() error { return e.u.Apply(context.Background(), rel) },
	} {
		err := fn()
		if !errors.Is(err, ErrNoReleaseKey) || err.Error() != "this build has no release key; updates are disabled" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(e.r.requests) != 1 { // only the release query
		t.Errorf("nothing may be downloaded without a key: %v", e.r.requests)
	}
	if readFile(t, e.exe) != "OLD BINARY" {
		t.Error("the executable changed")
	}
}

func TestSignatureIsVerifiedBeforeAnyChecksumIsTrusted(t *testing.T) {
	cases := map[string]func(e *env){
		"signed by another key": func(e *env) {
			_, other := newKey(t)
			e.r.signer = other
		},
		"signature of other content": func(e *env) {
			e.r.sigBody = base64.StdEncoding.EncodeToString(ed25519.Sign(e.priv, []byte("something else")))
		},
		"malformed signature": func(e *env) { e.r.sigBody = "this is not a signature" },
		"truncated signature": func(e *env) { e.r.sigBody = base64.StdEncoding.EncodeToString([]byte("tooshort")) },
		"checksums replaced after signing": func(e *env) {
			// The signature is computed over the served checksums.txt, so pin it to the original.
			e.r.sigBody = base64.StdEncoding.EncodeToString(ed25519.Sign(e.priv, e.r.assets["checksums.txt"]))
			e.r.assets["checksums.txt"] = []byte(sumLine("ompcraft_linux_amd64.tar.gz", e.r.assets["ompcraft_linux_amd64.tar.gz"]) + "# tampered\n")
		},
	}
	for name, tamper := range cases {
		e := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
		tamper(e)
		err := e.u.Apply(context.Background(), e.latest(t))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if e.r.archiveHit != 0 {
			t.Errorf("%s: the archive was downloaded before the signature was verified", name)
		}
		if readFile(t, e.exe) != "OLD BINARY" || exists(e.u.StagedPath()) || len(e.swaps) != 0 {
			t.Errorf("%s: state changed (%v)", name, e.swaps)
		}
	}
	// A valid signature over a checksums.txt that disagrees with the archive is still refused.
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	e.r.assets["checksums.txt"] = []byte(sumLine("ompcraft_linux_amd64.tar.gz", []byte("something else")))
	if err := e.u.Apply(context.Background(), e.latest(t)); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("err = %v", err)
	}
	// Missing signature asset.
	e = newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	e.r.omit["checksums.txt.sig"] = true
	if err := e.u.Apply(context.Background(), e.latest(t)); err == nil || !strings.Contains(err.Error(), "checksums.txt.sig") || e.r.archiveHit != 0 {
		t.Errorf("err = %v", err)
	}
	// Missing checksums.txt or no entry for our archive.
	e = newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	e.r.omit["checksums.txt"] = true
	if err := e.u.Apply(context.Background(), e.latest(t)); err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("err = %v", err)
	}
	e = newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	e.r.assets["checksums.txt"] = []byte(sumLine("ompcraft_darwin_arm64.tar.gz", []byte("x")))
	if err := e.u.Apply(context.Background(), e.latest(t)); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("err = %v", err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// ---- install, backup, atomic swap -----------------------------------------

func TestApplySwapsAtomicallyAndKeepsTheOldBinary(t *testing.T) {
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("#!NEW BINARY"))
	if err := e.u.Apply(context.Background(), e.latest(t)); err != nil {
		t.Fatal(err)
	}
	if readFile(t, e.exe) != "#!NEW BINARY" {
		t.Fatalf("executable = %q", readFile(t, e.exe))
	}
	if readFile(t, e.exe+".old") != "OLD BINARY" {
		t.Errorf(".old = %q", readFile(t, e.exe+".old"))
	}
	if st, _ := os.Stat(e.exe); st.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(e.exe + ".old"); st.Mode().Perm() != 0o755 {
		t.Errorf(".old mode = %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(e.exe))
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	if !reflect.DeepEqual(names, []string{"ompcraft", "ompcraft.old"}) {
		t.Errorf("directory = %v (no staged or temp files may remain)", names)
	}
	if !reflect.DeepEqual(e.swaps, []string{"before:0.2.0"}) {
		t.Errorf("hooks = %v", e.swaps)
	}
	if !e.r.downloaded("checksums.txt.sig") || !e.r.downloaded("ompcraft_linux_amd64.tar.gz") {
		t.Errorf("requests: %v", e.r.requests)
	}

	// A later update overwrites the older .old with the binary it replaces.
	e.r.tag = "v0.3.0"
	e.u.Version = "0.2.0"
	e.u.RunVersion = fakeVersion("0.3.0")
	e.r.assets["ompcraft_linux_amd64.tar.gz"] = tarGz(t, map[string][]byte{"ompcraft": []byte("THIRD")})
	e.r.assets["checksums.txt"] = []byte(sumLine("ompcraft_linux_amd64.tar.gz", e.r.assets["ompcraft_linux_amd64.tar.gz"]))
	if err := e.u.Apply(context.Background(), e.latest(t)); err != nil {
		t.Fatal(err)
	}
	if readFile(t, e.exe) != "THIRD" || readFile(t, e.exe+".old") != "#!NEW BINARY" {
		t.Errorf("exe=%q old=%q", readFile(t, e.exe), readFile(t, e.exe+".old"))
	}
}

func TestStageOnlyWritesTheStagedFile(t *testing.T) {
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("STAGED BINARY"))
	if err := e.u.Stage(context.Background(), e.latest(t)); err != nil {
		t.Fatal(err)
	}
	if !e.u.HasStaged() || readFile(t, filepath.Join(filepath.Dir(e.exe), ".ompcraft.staged")) != "STAGED BINARY" {
		t.Fatal("no staged binary")
	}
	if st, _ := os.Stat(e.u.StagedPath()); st.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	if readFile(t, e.exe) != "OLD BINARY" || exists(e.exe+".old") || len(e.swaps) != 0 {
		t.Error("staging must not touch the executable, the backup or state")
	}
	// Installing the staged binary later does the swap.
	if err := e.u.Install(context.Background(), "0.2.0"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, e.exe) != "STAGED BINARY" || e.u.HasStaged() {
		t.Error("staged binary not installed")
	}
}

func TestInstallRefusesAStagedBinaryThatFailsItsVersionCheck(t *testing.T) {
	cases := map[string]func(context.Context, string) (string, error){
		"wrong version":    fakeVersion("9.9.9"),
		"garbage output":   func(context.Context, string) (string, error) { return "segmentation fault\n", nil },
		"wrong program":    func(context.Context, string) (string, error) { return "othertool 0.2.0\n", nil },
		"fails to run":     func(context.Context, string) (string, error) { return "", errors.New("exec format error") },
		"prefix of a name": fakeVersion("0.2.01"),
	}
	for name, rv := range cases {
		e := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
		e.u.RunVersion = rv
		err := e.u.Apply(context.Background(), e.latest(t))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if readFile(t, e.exe) != "OLD BINARY" || exists(e.exe+".old") {
			t.Errorf("%s: executable or backup changed", name)
		}
		if len(e.swaps) != 0 {
			t.Errorf("%s: pending was recorded for a refused binary: %v", name, e.swaps)
		}
		if e.u.HasStaged() {
			t.Errorf("%s: the refused binary stays staged", name)
		}
	}
	if !VersionOutputMatches("ompcraft 0.2.0 (release)\n", "0.2.0") || VersionOutputMatches("ompcraft 0.2.0-rc.1 (release)", "0.2.0") {
		t.Error("VersionOutputMatches")
	}
}

func TestRealVersionRunAndTimeout(t *testing.T) {
	dir := t.TempDir()
	script := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := script("good", `[ "$1" = version ] && echo "ompcraft 0.2.0 (release)"`)
	slow := script("slow", `sleep 5`)
	u := &Updater{Timeout: 300 * time.Millisecond}
	out, err := u.runVersion(context.Background(), good)
	if err != nil || !VersionOutputMatches(out, "0.2.0") {
		t.Fatalf("%q %v", out, err)
	}
	start := time.Now()
	if _, err := u.runVersion(context.Background(), slow); err == nil || !strings.Contains(err.Error(), "did not answer `version` within") {
		t.Errorf("slow binary: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("the timeout was not enforced")
	}
	if _, err := u.runVersion(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing binary must fail")
	}
	notExec := filepath.Join(dir, "plain")
	_ = os.WriteFile(notExec, []byte("not a program"), 0o644)
	if _, err := u.runVersion(context.Background(), notExec); err == nil {
		t.Error("a non-executable file must fail")
	}
	// Through Install with the real runner: a staged "binary" that reports the right version passes.
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("#!/bin/sh\necho \"ompcraft 0.2.0 (release)\"\n"))
	e.u.RunVersion = nil
	if err := e.u.Apply(context.Background(), e.latest(t)); err != nil {
		t.Fatalf("real run: %v", err)
	}
	e2 := newEnv(t, "v0.2.0", "0.1.0", []byte("#!/bin/sh\necho \"ompcraft 0.9.9 (release)\"\n"))
	e2.u.RunVersion = nil
	if err := e2.u.Apply(context.Background(), e2.latest(t)); err == nil || readFile(t, e2.exe) != "OLD BINARY" {
		t.Errorf("a binary with the wrong version string was installed: %v", err)
	}
}

func TestSwapFailureCallsTheHookAndKeepsTheExecutable(t *testing.T) {
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	rel := e.latest(t)
	if err := e.u.Stage(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(e.exe); err != nil { // nothing to back up: the swap cannot proceed
		t.Fatal(err)
	}
	if err := e.u.Install(context.Background(), "0.2.0"); err == nil {
		t.Fatal("expected an error")
	}
	if !reflect.DeepEqual(e.swaps, []string{"before:0.2.0", "failed:0.2.0"}) {
		t.Errorf("hooks = %v", e.swaps)
	}
	// A BeforeSwap error aborts before anything is touched.
	e2 := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	e2.u.BeforeSwap = func(string) error { return errors.New("disk full") }
	err := e2.u.Apply(context.Background(), e2.latest(t))
	if err == nil || !strings.Contains(err.Error(), "disk full") || readFile(t, e2.exe) != "OLD BINARY" || exists(e2.exe+".old") {
		t.Errorf("err=%v", err)
	}
}

func TestRollbackRestoresTheOldBinary(t *testing.T) {
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("NEW"))
	if err := e.u.Apply(context.Background(), e.latest(t)); err != nil {
		t.Fatal(err)
	}
	if err := e.u.Rollback(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, e.exe) != "OLD BINARY" || exists(e.exe+".old") {
		t.Errorf("exe=%q", readFile(t, e.exe))
	}
	if err := e.u.Rollback(); err == nil || !strings.Contains(err.Error(), "no") {
		t.Errorf("a second rollback must fail: %v", err)
	}
}

func TestPlatformAndArchiveProblems(t *testing.T) {
	// Another OS's archive only: refused before anything is downloaded.
	pub, priv := newKey(t)
	r := newRelease(t, "v0.2.0", "darwin", "arm64", []byte("MAC BINARY"), priv)
	exe := filepath.Join(t.TempDir(), "ompcraft")
	_ = os.WriteFile(exe, []byte("OLD BINARY"), 0o755)
	u := &Updater{BaseURL: r.srv.URL, Version: "0.1.0", GOOS: "linux", GOARCH: "amd64", Exe: exe, PublicKey: pub, RunVersion: fakeVersion("0.2.0")}
	rel, _ := u.Latest(context.Background())
	err := u.Apply(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), "no ompcraft_linux_amd64.tar.gz for linux/amd64") {
		t.Fatalf("err = %v", err)
	}
	if len(r.requests) != 1 || readFile(t, exe) != "OLD BINARY" {
		t.Errorf("requests=%v", r.requests)
	}

	cases := map[string][]byte{
		"not gzip":         []byte("plain text"),
		"no ompcraft file": tarGz(t, map[string][]byte{"README": []byte("hi")}),
		"empty binary":     tarGz(t, map[string][]byte{"ompcraft": nil}),
	}
	for name, archive := range cases {
		e := newEnv(t, "v0.2.0", "0.1.0", []byte("x"))
		e.r.assets["ompcraft_linux_amd64.tar.gz"] = archive
		e.r.assets["checksums.txt"] = []byte(sumLine("ompcraft_linux_amd64.tar.gz", archive))
		if err := e.u.Apply(context.Background(), e.latest(t)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if readFile(t, e.exe) != "OLD BINARY" {
			t.Errorf("%s: executable changed", name)
		}
	}
	// A nested path is accepted: the member's base name is ompcraft.
	e := newEnv(t, "v0.2.0", "0.1.0", nil)
	archive := tarGz(t, map[string][]byte{"ompcraft_0.2.0_linux_amd64/ompcraft": []byte("NESTED")})
	e.r.assets["ompcraft_linux_amd64.tar.gz"] = archive
	e.r.assets["checksums.txt"] = []byte(sumLine("ompcraft_linux_amd64.tar.gz", archive))
	if err := e.u.Apply(context.Background(), e.latest(t)); err != nil || readFile(t, e.exe) != "NESTED" {
		t.Errorf("err=%v exe=%q", err, readFile(t, e.exe))
	}
}

func TestOversizedAssetIsRefused(t *testing.T) {
	e := newEnv(t, "v0.2.0", "0.1.0", []byte("x"))
	e.r.assets["ompcraft_linux_amd64.tar.gz"] = bytes.Repeat([]byte{0}, MaxAssetBytes+1)
	err := e.u.Apply(context.Background(), e.latest(t))
	if err == nil || !strings.Contains(err.Error(), "larger than 50 MB") {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, e.exe) != "OLD BINARY" {
		t.Error("executable changed")
	}
}

func TestVerifyChecksumFormats(t *testing.T) {
	data := []byte("payload")
	h := sha256.Sum256(data)
	sum := hex.EncodeToString(h[:])
	for _, line := range []string{sum + "  a.tar.gz\n", sum + " *a.tar.gz\n", strings.ToUpper(sum) + "  a.tar.gz\n", "# comment\n" + sum + "  a.tar.gz\nother  b.tar.gz\n"} {
		if err := VerifyChecksum([]byte(line), "a.tar.gz", data); err != nil {
			t.Errorf("%q: %v", line, err)
		}
	}
	if err := VerifyChecksum([]byte(sum+"  b.tar.gz\n"), "a.tar.gz", data); err == nil {
		t.Error("a checksum for another file must not count")
	}
}

// ---- controller: never update mid-session ----------------------------------

type ctlEnv struct {
	*env
	c      *Controller
	cfgEnv configEnv
	game   bool
	staged []string
	logs   []string
	now    time.Time
	status *Status
}

func newCtl(t *testing.T, tag, running string, binary []byte) *ctlEnv {
	t.Helper()
	ce := &ctlEnv{env: newEnv(t, tag, running, binary), now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	ce.cfgEnv = testConfigEnv(t)
	ce.status = NewStatus("release", true)
	ce.c = NewController(ce.u, ce.cfgEnv, ce.status, func() time.Time { return ce.now },
		func(f string, a ...any) { ce.logs = append(ce.logs, fmt.Sprintf(f, a...)) }, func() bool { return ce.game })
	ce.c.OnStaged = func(v string) { ce.staged = append(ce.staged, v) }
	return ce
}

func (ce *ctlEnv) logged(sub string) bool {
	for _, l := range ce.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func TestControllerWithTheGameRunningOnlyStages(t *testing.T) {
	ce := newCtl(t, "v0.2.0", "0.1.0", []byte("NEW BINARY"))
	ce.game = true
	applied, err := ce.c.Check(context.Background())
	if applied || err != nil {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if readFile(t, ce.exe) != "OLD BINARY" || exists(ce.exe+".old") {
		t.Fatal("the executable changed while the game runs")
	}
	if !ce.u.HasStaged() || readFile(t, ce.u.StagedPath()) != "NEW BINARY" {
		t.Fatal("the verified binary was not staged")
	}
	st := ce.cfgEnv.LoadState()
	if st.StagedVersion != "0.2.0" || st.PendingVersion != "" {
		t.Errorf("state = %+v", st)
	}
	if !reflect.DeepEqual(ce.staged, []string{"0.2.0"}) {
		t.Errorf("notices = %v", ce.staged)
	}
	if !ce.logged("update: 0.2.0 is ready; it installs after you quit WoW") {
		t.Errorf("logs = %v", ce.logs)
	}
	if ce.status.Info().Staged != "0.2.0" {
		t.Errorf("status = %+v", ce.status.Info())
	}

	// The next check with the game still running: no re-download, no second notice.
	hits := ce.r.archiveHit
	ce.logs = nil
	if applied, err := ce.c.Check(context.Background()); applied || err != nil {
		t.Fatalf("%v %v", applied, err)
	}
	if ce.r.archiveHit != hits || len(ce.staged) != 1 || !ce.logged("is ready; it installs after you quit WoW") {
		t.Errorf("hits %d->%d notices=%v logs=%v", hits, ce.r.archiveHit, ce.staged, ce.logs)
	}
}

func TestControllerInstallsTheStagedBinaryWhenTheGameExits(t *testing.T) {
	ce := newCtl(t, "v0.2.0", "0.1.0", []byte("NEW BINARY"))
	ce.game = true
	_, _ = ce.c.Check(context.Background())
	hits := ce.r.archiveHit

	ce.game = false
	applied, err := ce.c.ApplyStaged(context.Background()) // the game-exit event
	if !applied || err != nil {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if readFile(t, ce.exe) != "NEW BINARY" || readFile(t, ce.exe+".old") != "OLD BINARY" || ce.u.HasStaged() {
		t.Error("the staged binary was not swapped in")
	}
	if ce.r.archiveHit != hits {
		t.Error("the staged binary must not be downloaded again")
	}
	st := ce.cfgEnv.LoadState()
	if st.PendingVersion != "0.2.0" || st.StartAttempts != 0 || st.PreviousVersion != "0.1.0" || st.StagedVersion != "" {
		t.Errorf("state = %+v", st)
	}
	if ce.status.Info().Staged != "" {
		t.Error("staged status not cleared")
	}
	// Nothing staged any more.
	if applied, err := ce.c.ApplyStaged(context.Background()); applied || err != nil {
		t.Errorf("second apply: %v %v", applied, err)
	}
}

func TestControllerAppliesAtTheNextCheckWhenNoGameRuns(t *testing.T) {
	ce := newCtl(t, "v0.2.0", "0.1.0", []byte("NEW BINARY"))
	ce.game = true
	_, _ = ce.c.Check(context.Background())
	ce.game = false
	hits := ce.r.archiveHit
	applied, err := ce.c.Check(context.Background())
	if !applied || err != nil {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if ce.r.archiveHit != hits || readFile(t, ce.exe) != "NEW BINARY" {
		t.Errorf("hits %d->%d exe=%q", hits, ce.r.archiveHit, readFile(t, ce.exe))
	}
	if !ce.logged("update: using the staged 0.2.0") || !ce.logged("restarting into it") {
		t.Errorf("logs = %v", ce.logs)
	}

	// With no game and nothing staged: download, verify and install in one go.
	ce = newCtl(t, "v0.2.0", "0.1.0", []byte("NEW BINARY"))
	if applied, err := ce.c.Check(context.Background()); !applied || err != nil {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if readFile(t, ce.exe) != "NEW BINARY" || len(ce.staged) != 0 {
		t.Errorf("exe=%q notices=%v", readFile(t, ce.exe), ce.staged)
	}
	if st := ce.cfgEnv.LoadState(); st.PendingVersion != "0.2.0" || st.PreviousVersion != "0.1.0" {
		t.Errorf("state = %+v", st)
	}
}

func TestControllerSkipsABadVersionAndUpToDate(t *testing.T) {
	ce := newCtl(t, "v0.2.0", "0.1.0", []byte("NEW"))
	_ = ce.cfgEnv.UpdateState(func(s *State) { s.BadVersion = "0.2.0" })
	if applied, err := ce.c.Check(context.Background()); applied || err != nil || ce.r.archiveHit != 0 {
		t.Fatalf("applied=%v err=%v hits=%d", applied, err, ce.r.archiveHit)
	}
	if !ce.logged("failed to start before; not applying it") {
		t.Errorf("logs = %v", ce.logs)
	}
	// A staged copy of a bad version is thrown away, not installed.
	ce.game = true
	_ = ce.u.Stage(context.Background(), ce.latest(t))
	_ = ce.cfgEnv.UpdateState(func(s *State) { s.StagedVersion = "0.2.0" })
	if applied, _ := ce.c.ApplyStaged(context.Background()); applied || ce.u.HasStaged() || readFile(t, ce.exe) != "OLD BINARY" {
		t.Error("a bad staged version was installed")
	}

	ce = newCtl(t, "v0.1.0", "0.1.0", []byte("SAME"))
	if applied, err := ce.c.Check(context.Background()); applied || err != nil {
		t.Fatal(applied, err)
	}
	if info := ce.status.Info(); info.Latest != "0.1.0" || !info.LastCheck.Equal(ce.now) || info.LastError != "" {
		t.Errorf("status = %+v", info)
	}
	if !ce.logged("update: up to date (running 0.1.0, latest 0.1.0)") {
		t.Errorf("logs = %v", ce.logs)
	}
}

func TestControllerRecordsFailures(t *testing.T) {
	ce := newCtl(t, "v0.2.0", "0.1.0", []byte("NEW"))
	_, other := newKey(t)
	ce.r.signer = other // a signature the embedded key does not accept
	ce.game = true
	applied, err := ce.c.Check(context.Background())
	if applied || err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if ce.u.HasStaged() || len(ce.staged) != 0 || ce.cfgEnv.LoadState().StagedVersion != "" {
		t.Error("an unverified binary was staged")
	}
	if !strings.Contains(ce.status.Info().LastError, "signature verification failed") {
		t.Errorf("status = %+v", ce.status.Info())
	}
	// A staged binary that fails its version check at install time is dropped.
	ce = newCtl(t, "v0.2.0", "0.1.0", []byte("NEW"))
	ce.game = true
	_, _ = ce.c.Check(context.Background())
	ce.u.RunVersion = fakeVersion("9.9.9")
	ce.game = false
	applied, err = ce.c.ApplyStaged(context.Background())
	if applied || err == nil || ce.u.HasStaged() || ce.cfgEnv.LoadState().StagedVersion != "" || readFile(t, ce.exe) != "OLD BINARY" {
		t.Errorf("applied=%v err=%v", applied, err)
	}
	// An API failure is recorded and a later success clears it.
	ce.r.srv.Close()
	if _, err := ce.c.Check(context.Background()); err == nil || ce.status.Info().LastError == "" {
		t.Errorf("err=%v", err)
	}
	ce.status.Record(ce.now, "0.2.0", nil)
	if ce.status.Info().LastError != "" {
		t.Error("lastError not cleared")
	}
}

// ---- start-up guard and rollback -------------------------------------------

func TestStartupCheckRollsBackAfterThreeFailedStarts(t *testing.T) {
	cfg := testConfigEnv(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "ompcraft")
	_ = os.WriteFile(exe, []byte("NEW 0.2.0"), 0o755)
	_ = os.WriteFile(exe+".old", []byte("OLD 0.1.0"), 0o755)
	_ = cfg.UpdateState(func(s *State) { s.PendingVersion, s.StartAttempts, s.PreviousVersion = "0.2.0", 0, "0.1.0" })
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	for i := 1; i <= 3; i++ { // starts 1..3 are allowed and counted
		if got := StartupCheck(cfg, "0.2.0", exe, logf); got != StartNormal {
			t.Fatalf("start %d: %v", i, got)
		}
		if st := cfg.LoadState(); st.StartAttempts != i || st.PendingVersion != "0.2.0" {
			t.Fatalf("start %d: state = %+v", i, st)
		}
	}
	if readFile(t, exe) != "NEW 0.2.0" {
		t.Fatal("rolled back too early")
	}
	// The fourth start finds startAttempts >= 3: roll back.
	logs = nil
	if got := StartupCheck(cfg, "0.2.0", exe, logf); got != StartRolledBack {
		t.Fatalf("result = %v", got)
	}
	if readFile(t, exe) != "OLD 0.1.0" || exists(exe+".old") {
		t.Errorf("exe = %q", readFile(t, exe))
	}
	st := cfg.LoadState()
	if st.BadVersion != "0.2.0" || st.PendingVersion != "" || st.StartAttempts != 0 || st.PreviousVersion != "" {
		t.Errorf("state = %+v", st)
	}
	if len(logs) != 1 || logs[0] != "update: 0.2.0 failed to start 3 times; rolled back to 0.1.0" {
		t.Errorf("logs = %q", logs)
	}
	// The old binary then starts normally.
	if got := StartupCheck(cfg, "0.1.0", exe, logf); got != StartNormal {
		t.Errorf("after rollback: %v", got)
	}
}

func TestStartupCheckOtherCases(t *testing.T) {
	cfg := testConfigEnv(t)
	exe := filepath.Join(t.TempDir(), "ompcraft")
	_ = os.WriteFile(exe, []byte("BIN"), 0o755)
	logf := func(string, ...any) {}

	if StartupCheck(cfg, "0.1.0", exe, logf) != StartNormal {
		t.Fatal("no pending state")
	}
	// A pending version that is not the running one (someone replaced the binary by hand) is forgotten.
	_ = cfg.UpdateState(func(s *State) { s.PendingVersion, s.StartAttempts, s.PreviousVersion = "0.2.0", 2, "0.1.0" })
	if StartupCheck(cfg, "0.3.0", exe, logf) != StartNormal {
		t.Fatal("stale pending")
	}
	if st := cfg.LoadState(); st.PendingVersion != "" || st.StartAttempts != 0 {
		t.Errorf("state = %+v", st)
	}
	// Three failed starts but no .old to go back to: keep running, mark the version bad.
	_ = cfg.UpdateState(func(s *State) { s.PendingVersion, s.StartAttempts = "0.2.0", 3 })
	var logs []string
	if StartupCheck(cfg, "0.2.0", exe, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }) != StartNormal {
		t.Fatal("cannot roll back without .old")
	}
	if st := cfg.LoadState(); st.BadVersion != "0.2.0" || st.PendingVersion != "" {
		t.Errorf("state = %+v", st)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "cannot be rolled back") {
		t.Errorf("logs = %q", logs)
	}
	if readFile(t, exe) != "BIN" {
		t.Error("the executable changed")
	}
}

func TestMarkStable(t *testing.T) {
	cfg := testConfigEnv(t)
	_ = cfg.UpdateState(func(s *State) {
		s.PendingVersion, s.StartAttempts, s.PreviousVersion, s.StagedVersion, s.GamePid = "0.2.0", 1, "0.1.0", "0.3.0", 77
	})
	MarkStable(cfg, "0.1.0") // not the pending version: nothing happens
	if cfg.LoadState().PendingVersion != "0.2.0" {
		t.Fatal("cleared for the wrong version")
	}
	MarkStable(cfg, "0.2.0")
	st := cfg.LoadState()
	if st.PendingVersion != "" || st.StartAttempts != 0 || st.PreviousVersion != "" {
		t.Errorf("state = %+v", st)
	}
	if st.StagedVersion != "0.3.0" || st.GamePid != 77 {
		t.Errorf("unrelated fields changed: %+v", st)
	}
}

// ---- scheduler -------------------------------------------------------------

func TestSchedulerDelayJitter(t *testing.T) {
	s := &Scheduler{Jitter: JitterFraction}
	for _, tc := range []struct {
		r    float64
		want time.Duration
	}{{0, 108 * time.Second}, {0.5, 120 * time.Second}, {0.999999, 132 * time.Second}} {
		s.Rand = func() float64 { return tc.r }
		got := s.Delay(FirstCheckDelay)
		if d := got - tc.want; d > time.Second || d < -time.Second {
			t.Errorf("r=%v: delay %v, want ~%v", tc.r, got, tc.want)
		}
	}
	s.Rand = nil
	for range 200 {
		if d := s.Delay(CheckEvery); d < 5*time.Hour+24*time.Minute || d > 6*time.Hour+36*time.Minute {
			t.Fatalf("delay %v outside ±10%% of 6h", d)
		}
	}
}

func TestSchedulerLoop(t *testing.T) {
	var waits []time.Duration
	var checks int
	var logs []string
	s := &Scheduler{
		First: FirstCheckDelay, Every: CheckEvery, Jitter: JitterFraction,
		Rand:  func() float64 { return 0.5 },
		After: func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return ctx.Err() },
		Log:   func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) },
		Check: func(ctx context.Context) (bool, error) {
			checks++
			switch checks {
			case 2:
				return false, errors.New("network down")
			case 4:
				return true, nil
			}
			return false, nil
		},
	}
	if !s.Loop(context.Background()) {
		t.Fatal("Loop must report the applied update")
	}
	want := []time.Duration{2 * time.Minute, 6 * time.Hour, 6 * time.Hour, 6 * time.Hour}
	if !reflect.DeepEqual(waits, want) || checks != 4 {
		t.Errorf("waits = %v checks = %d, want %v / 4", waits, checks, want)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "update: next check in 2m0s") || !strings.Contains(joined, "update: check failed: network down") {
		t.Errorf("logs:\n%s", joined)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checks = 0
	s.After = nil
	if s.Loop(ctx) || checks != 0 {
		t.Error("a cancelled loop must not check")
	}
}
