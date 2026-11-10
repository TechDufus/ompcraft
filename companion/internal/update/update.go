package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the GitHub API root; OMPCRAFT_UPDATE_URL overrides it.
	DefaultBaseURL = "https://api.github.com"
	// Repo is the repository releases come from.
	Repo = "TechDufus/ompcraft"
	// MaxAssetBytes caps every download (and the extracted binary).
	MaxAssetBytes = 50 << 20
	// APITimeout bounds the release query.
	APITimeout = 15 * time.Second
	// VerifyTimeout bounds the staged binary's `version` run.
	VerifyTimeout = 5 * time.Second

	binaryName = "ompcraft"
	stagedName = ".ompcraft.staged"
	oldSuffix  = ".old"
)

// Release is the part of a GitHub release this package uses.
type Release struct {
	Tag        string
	Version    Version
	Prerelease bool
	Assets     map[string]string // asset name -> browser_download_url
}

// Updater talks to the release API and replaces the executable.
type Updater struct {
	BaseURL string
	HTTP    *http.Client
	// Version is the running version ("0.1.0").
	Version      string
	GOOS, GOARCH string
	// Exe is the path to replace.
	Exe string
	// PublicKey is the base64 ed25519 release key. Empty disables updating.
	PublicKey string
	// RunVersion runs `<path> version` and returns its output; nil runs the
	// real binary with a 5 s timeout.
	RunVersion func(ctx context.Context, path string) (string, error)
	// Timeout bounds the real `version` run; 0 means VerifyTimeout (5 s).
	Timeout time.Duration
	// BeforeSwap runs after the staged binary passed its version check and
	// before the executable is touched (record the pending version). An error
	// aborts the swap. SwapFailed runs if the swap then fails.
	BeforeSwap func(version string) error
	SwapFailed func(version string)
}

// BaseURLFromEnv returns OMPCRAFT_UPDATE_URL or the GitHub API.
func BaseURLFromEnv() string {
	if u := strings.TrimRight(os.Getenv("OMPCRAFT_UPDATE_URL"), "/"); u != "" {
		return u
	}
	return DefaultBaseURL
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (u *Updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ompcraft/"+u.Version)
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d MB; refusing it", url, limit>>20)
	}
	return data, nil
}

// Latest asks for the latest release.
func (u *Updater) Latest(ctx context.Context) (Release, error) {
	base := u.BaseURL
	if base == "" {
		base = BaseURLFromEnv()
	}
	ctx, cancel := context.WithTimeout(ctx, APITimeout)
	defer cancel()
	body, err := u.get(ctx, strings.TrimRight(base, "/")+"/repos/"+Repo+"/releases/latest", 4<<20)
	if err != nil {
		return Release{}, err
	}
	var raw struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Release{}, fmt.Errorf("cannot read the release answer: %w", err)
	}
	v, err := ParseVersion(raw.Tag)
	if err != nil {
		return Release{}, fmt.Errorf("latest release has an unusable tag: %w", err)
	}
	rel := Release{Tag: raw.Tag, Version: v, Prerelease: raw.Prerelease || raw.Draft || v.Prerelease(), Assets: map[string]string{}}
	for _, a := range raw.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// Newer reports whether rel is a stable release newer than the running
// version. Pre-release tags are ignored.
func (u *Updater) Newer(rel Release) (bool, error) {
	if rel.Prerelease {
		return false, nil
	}
	cur, err := ParseVersion(u.Version)
	if err != nil {
		return false, fmt.Errorf("the running version is unusable: %w", err)
	}
	return rel.Version.Compare(cur) > 0, nil
}

// AssetName is the archive for this platform.
func (u *Updater) AssetName() string {
	return fmt.Sprintf("ompcraft_%s_%s.tar.gz", u.GOOS, u.GOARCH)
}

// StagedPath is where a verified binary waits: next to the executable.
func (u *Updater) StagedPath() string { return filepath.Join(filepath.Dir(u.Exe), stagedName) }

// OldPath is where the previous binary is kept.
func (u *Updater) OldPath() string { return u.Exe + oldSuffix }

// Fetch downloads and verifies a release: checksums.txt and its signature
// first, the signature checked against the embedded release key before any
// checksum is trusted; then this platform's archive, its sha256, and the
// ompcraft binary inside. It touches no file.
func (u *Updater) Fetch(ctx context.Context, rel Release) ([]byte, error) {
	if rel.Prerelease {
		return nil, errors.New("the latest release is a pre-release; ignoring it")
	}
	if strings.TrimSpace(u.PublicKey) == "" {
		return nil, ErrNoReleaseKey
	}
	name := u.AssetName()
	archiveURL, ok := rel.Assets[name]
	if !ok {
		return nil, fmt.Errorf("release %s has no %s for %s/%s", rel.Tag, name, u.GOOS, u.GOARCH)
	}
	sumsURL, ok := rel.Assets["checksums.txt"]
	if !ok {
		return nil, fmt.Errorf("release %s has no checksums.txt; refusing to install unverified code", rel.Tag)
	}
	sigURL, ok := rel.Assets["checksums.txt.sig"]
	if !ok {
		return nil, fmt.Errorf("release %s has no checksums.txt.sig; refusing to install unsigned code", rel.Tag)
	}
	sums, err := u.get(ctx, sumsURL, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("downloading checksums.txt: %w", err)
	}
	sig, err := u.get(ctx, sigURL, 4<<10)
	if err != nil {
		return nil, fmt.Errorf("downloading checksums.txt.sig: %w", err)
	}
	if err := VerifySignature(u.PublicKey, sums, sig); err != nil {
		return nil, err
	}
	archive, err := u.get(ctx, archiveURL, MaxAssetBytes)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	if err := VerifyChecksum(sums, name, archive); err != nil {
		return nil, err
	}
	return extractBinary(archive)
}

// Stage fetches a release and writes the verified binary to StagedPath
// (atomically). Nothing else changes.
func (u *Updater) Stage(ctx context.Context, rel Release) error {
	bin, err := u.Fetch(ctx, rel)
	if err != nil {
		return err
	}
	tmp := u.StagedPath() + ".tmp"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("cannot write next to %s: %w", u.Exe, err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, u.StagedPath()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// HasStaged reports whether a staged binary exists.
func (u *Updater) HasStaged() bool {
	st, err := os.Stat(u.StagedPath())
	return err == nil && st.Mode().IsRegular()
}

// DiscardStaged removes the staged binary.
func (u *Updater) DiscardStaged() { _ = os.Remove(u.StagedPath()) }

func (u *Updater) runVersion(ctx context.Context, p string) (string, error) {
	if u.RunVersion != nil {
		return u.RunVersion(ctx, p)
	}
	timeout := u.Timeout
	if timeout <= 0 {
		timeout = VerifyTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "version")
	cmd.WaitDelay = 500 * time.Millisecond // a child that holds the pipe must not outlive the timeout
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("the staged binary did not answer `version` within %s", timeout)
	}
	if err != nil {
		return "", fmt.Errorf("the staged binary failed to run: %w", err)
	}
	return string(out), nil
}

// VersionOutputMatches reports whether out is `ompcraft <version> (<channel>)`.
func VersionOutputMatches(out, version string) bool {
	f := strings.Fields(out)
	return len(f) >= 2 && f[0] == binaryName && f[1] == version
}

// Install swaps the staged binary in as the executable:
//
//  1. run it with `version` (5 s) and require the expected version string;
//  2. BeforeSwap (record the pending version);
//  3. keep the current executable as <exe>.old, overwriting an older one;
//  4. rename the staged binary over the executable (atomic).
//
// Any failure leaves the executable as it was.
func (u *Updater) Install(ctx context.Context, version string) error {
	staged := u.StagedPath()
	out, err := u.runVersion(ctx, staged)
	if err != nil {
		u.DiscardStaged()
		return err
	}
	if !VersionOutputMatches(out, version) {
		u.DiscardStaged()
		return fmt.Errorf("the staged binary says %q, expected ompcraft %s; refusing it", strings.TrimSpace(out), version)
	}
	if u.BeforeSwap != nil {
		if err := u.BeforeSwap(version); err != nil {
			return fmt.Errorf("cannot record the pending update: %w", err)
		}
	}
	fail := func(err error) error {
		if u.SwapFailed != nil {
			u.SwapFailed(version)
		}
		return err
	}
	if err := copyFile(u.Exe, u.OldPath()); err != nil {
		return fail(fmt.Errorf("cannot keep the previous binary as %s: %w", u.OldPath(), err))
	}
	if err := os.Rename(staged, u.Exe); err != nil {
		return fail(fmt.Errorf("cannot replace %s: %w", u.Exe, err))
	}
	return nil
}

// Apply fetches, stages and installs a release at once (a manual update).
func (u *Updater) Apply(ctx context.Context, rel Release) error {
	if err := u.Stage(ctx, rel); err != nil {
		return err
	}
	return u.Install(ctx, rel.Version.String())
}

// Rollback puts <exe>.old back over the executable (atomically).
func (u *Updater) Rollback() error {
	if _, err := os.Stat(u.OldPath()); err != nil {
		return fmt.Errorf("there is no %s to roll back to", u.OldPath())
	}
	return os.Rename(u.OldPath(), u.Exe)
}

// copyFile copies src to dst through a temp file and a rename, mode 0755.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// VerifyChecksum checks data against the line for name in a checksums.txt
// ("<sha256>  <name>" per line).
func VerifyChecksum(sums []byte, name string, data []byte) error {
	var want string
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = strings.ToLower(f[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s; refusing to install it", name)
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s); not installing", name, want, hex.EncodeToString(got[:]))
	}
	return nil
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive is not a gzip file: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the archive has no ompcraft binary")
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != binaryName {
			continue
		}
		if h.Size > MaxAssetBytes {
			return nil, errors.New("the ompcraft binary in the archive is too large")
		}
		data, err := io.ReadAll(io.LimitReader(tr, MaxAssetBytes+1))
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if int64(len(data)) > MaxAssetBytes {
			return nil, errors.New("the ompcraft binary in the archive is too large")
		}
		if len(data) == 0 {
			return nil, errors.New("the ompcraft binary in the archive is empty")
		}
		return data, nil
	}
}
