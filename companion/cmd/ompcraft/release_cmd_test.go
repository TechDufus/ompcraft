package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs fn and returns what it printed.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	err := fn()
	_ = w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b), err
}

func TestReleaseKeygenAndSign(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "release.key")
	out, err := captureStdout(t, func() error { return dispatch([]string{"release-keygen", "--out", keyFile}) })
	if err != nil {
		t.Fatal(err)
	}
	// Only the public key is printed.
	pub := strings.TrimSpace(out)
	pubRaw, err := base64.StdEncoding.DecodeString(pub)
	if err != nil || len(pubRaw) != ed25519.PublicKeySize || strings.Count(out, "\n") != 1 {
		t.Fatalf("stdout = %q", out)
	}
	priv, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(keyFile); st.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %v", st.Mode().Perm())
	}
	privRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(priv)))
	if err != nil || len(privRaw) != ed25519.PrivateKeySize {
		t.Fatalf("private key file: %q %v", priv, err)
	}
	if strings.Contains(out, strings.TrimSpace(string(priv))) {
		t.Fatal("the private key was printed")
	}
	if !ed25519.PublicKey(pubRaw).Equal(ed25519.PrivateKey(privRaw).Public()) {
		t.Error("the printed public key does not match the private key file")
	}

	// Refuses to overwrite, and leaves the file alone.
	if _, err := captureStdout(t, func() error { return dispatch([]string{"release-keygen", "--out", keyFile}) }); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("overwrite: %v", err)
	}
	if again, _ := os.ReadFile(keyFile); string(again) != string(priv) {
		t.Error("the existing key was modified")
	}
	if err := dispatch([]string{"release-keygen"}); err == nil {
		t.Error("--out is required")
	}

	// Sign a checksums.txt.
	sums := filepath.Join(dir, "checksums.txt")
	data := []byte("abc123  ompcraft_linux_amd64.tar.gz\n")
	if err := os.WriteFile(sums, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMPCRAFT_SIGNING_KEY", strings.TrimSpace(string(priv)))
	out, err = captureStdout(t, func() error { return dispatch([]string{"release-sign", sums}) })
	if err != nil || out != "" {
		t.Fatalf("sign: stdout=%q err=%v (it must print nothing)", out, err)
	}
	sigB64, err := os.ReadFile(sums + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pubRaw), data, sig) {
		t.Fatalf("signature invalid: %v", err)
	}
}

func TestReleaseSignErrorsNeverLeakTheKey(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "checksums.txt")
	_ = os.WriteFile(file, []byte("x"), 0o644)

	t.Setenv("OMPCRAFT_SIGNING_KEY", "")
	if err := dispatch([]string{"release-sign", file}); err == nil || !strings.Contains(err.Error(), "OMPCRAFT_SIGNING_KEY is not set") {
		t.Errorf("no key: %v", err)
	}
	for _, bad := range []string{"c2VjcmV0LWtleS1tYXRlcmlhbA==", "not base64 at all!"} {
		t.Setenv("OMPCRAFT_SIGNING_KEY", bad)
		err := dispatch([]string{"release-sign", file})
		if err == nil || strings.Contains(err.Error(), bad) {
			t.Errorf("bad key %q: %v", bad, err)
		}
		if _, err := os.Stat(file + ".sig"); err == nil {
			t.Error("a signature was written with a bad key")
		}
	}
	t.Setenv("OMPCRAFT_SIGNING_KEY", "x")
	if err := dispatch([]string{"release-sign"}); err == nil {
		t.Error("FILE is required")
	}
	if err := dispatch([]string{"release-sign", filepath.Join(dir, "missing")}); err == nil {
		t.Error("a missing file must fail")
	}
}

func TestMaintainerCommandsAreNotInHelp(t *testing.T) {
	for _, hidden := range []string{"release-keygen", "release-sign", "OMPCRAFT_SIGNING_KEY"} {
		if strings.Contains(usage, hidden) {
			t.Errorf("the help text mentions %q", hidden)
		}
	}
}
