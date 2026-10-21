// Package update finds the latest release of dk on GitHub and installs it
// over the running binary. The agent install of remote hosts downloads
// its binaries the same way.
package update

import (
	"archive/tar"
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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository the releases come from.
const Repo = "YouSysAdmin/duck-editor"

// The release URLs, variables so tests can serve them.
var (
	// LatestURL redirects to the page of the latest release, whose tag
	// is the version. No API call, so no rate limit applies.
	LatestURL = "https://github.com/" + Repo + "/releases/latest"

	// ReleaseBase is where the archives of a release are downloaded from.
	ReleaseBase = "https://github.com/" + Repo + "/releases/download"

	// ExecutablePath is the binary Install replaces, this executable with
	// its symlinks resolved.
	ExecutablePath = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}

		return filepath.EvalSymlinks(exe)
	}
)

// ErrNoArchive reports a release without the archive asked for.
var ErrNoArchive = errors.New("not in SHA256SUMS")

const (
	// checkEvery is how long a check of the latest version is trusted.
	checkEvery = 24 * time.Hour

	// pageTimeout bounds the small requests, downloadTimeout an archive.
	pageTimeout     = 30 * time.Second
	downloadTimeout = 5 * time.Minute

	// maxArchive bounds a download, far above any release.
	maxArchive = 256 << 20

	userAgent = "dk-update (+https://github.com/" + Repo + ")"
)

// stamp is the last check, kept in the cache directory so the editor
// asks GitHub once a day, not on every start.
type stamp struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

// Latest returns the version of the latest release, without the v.
func Latest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pageTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	location := resp.Header.Get("Location")
	if resp.StatusCode/100 != 3 || location == "" {
		return "", fmt.Errorf("%s: %s, no release found", LatestURL, resp.Status)
	}

	_, tag, ok := strings.Cut(location, "/releases/tag/")
	if !ok {
		return "", fmt.Errorf("%s redirects to %s, not a release", LatestURL, location)
	}

	version := strings.TrimPrefix(tag, "v")
	if _, ok := parse(version); !ok {
		return "", fmt.Errorf("release tag %s is not a version", tag)
	}

	return version, nil
}

// Newer reports whether latest is a later version than current. A
// development build, or anything that is not a version, is never
// updated.
func Newer(current, latest string) bool {
	cur, ok := parse(current)
	if !ok {
		return false
	}

	lat, ok := parse(latest)
	if !ok {
		return false
	}

	for i := range cur {
		if cur[i] != lat[i] {
			return cur[i] < lat[i]
		}
	}

	return false
}

// IsVersion reports whether version names a release, major.minor.patch,
// which a development build does not.
func IsVersion(version string) bool {
	_, ok := parse(version)

	return ok
}

// parse reads major.minor.patch, with a v and a pre-release suffix
// allowed.
func parse(version string) ([3]int, bool) {
	var out [3]int
	version = strings.TrimPrefix(version, "v")
	version, _, _ = strings.Cut(version, "-")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return out, false
	}

	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}

		out[i] = n
	}

	return out, true
}

// Check tells whether a release newer than current exists. The answer
// is kept in cacheDir for a day, so only the first call asks GitHub.
// A development build gets no answer and no request.
func Check(ctx context.Context, cacheDir, current string) (latest string, newer bool, err error) {
	if _, ok := parse(current); !ok {
		return "", false, nil
	}

	path := filepath.Join(cacheDir, "update.json")
	var s stamp
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &s) == nil && time.Since(s.Checked) < checkEvery && s.Latest != "" {
		return s.Latest, Newer(current, s.Latest), nil
	}

	latest, err = Latest(ctx)
	if err != nil {
		return "", false, err
	}

	if data, err := json.Marshal(stamp{Checked: time.Now(), Latest: latest}); err == nil && cacheDir != "" {
		_ = os.MkdirAll(cacheDir, 0o755)
		_ = os.WriteFile(path, data, 0o644)
	}

	return latest, Newer(current, latest), nil
}

// Download fetches the archive name.tar.gz of a release, checks it
// against the published SHA256SUMS and returns the dk binary inside it.
func Download(ctx context.Context, version, name string) ([]byte, error) {
	base := ReleaseBase + "/v" + strings.TrimPrefix(version, "v") + "/"
	archive := name + ".tar.gz"
	sums, err := fetch(ctx, base+"SHA256SUMS", pageTimeout)
	if err != nil {
		return nil, err
	}

	want := ""
	for line := range strings.Lines(string(sums)) {
		if fields := strings.Fields(line); len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == archive {
			want = fields[0]
		}
	}

	if want == "" {
		return nil, fmt.Errorf("%s: %w", archive, ErrNoArchive)
	}

	data, err := fetch(ctx, base+archive, downloadTimeout)
	if err != nil {
		return nil, err
	}

	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
		return nil, fmt.Errorf("%s does not match its checksum", archive)
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s holds no dk binary", archive)
		}

		if err != nil {
			return nil, err
		}

		if hdr.Typeflag == tar.TypeReg && (hdr.Name == "dk" || hdr.Name == "./dk") {
			return io.ReadAll(io.LimitReader(tr, maxArchive))
		}
	}
}

// Install downloads the release of version for this system and replaces
// the running binary with it. report, when set, gets a line per step.
// It returns the path that was replaced.
func Install(ctx context.Context, version string, report func(string)) (string, error) {
	say := func(format string, args ...any) {
		if report != nil {
			report(fmt.Sprintf(format, args...))
		}
	}

	exe, err := ExecutablePath()
	if err != nil {
		return "", err
	}

	info, err := os.Stat(exe)
	if err != nil {
		return "", err
	}

	name := "dk-" + runtime.GOOS + "-" + runtime.GOARCH
	say("downloading dk %s for %s/%s", version, runtime.GOOS, runtime.GOARCH)
	data, err := Download(ctx, version, name)
	if err != nil {
		return "", err
	}

	say("replacing %s", exe)
	mode := info.Mode().Perm()
	fresh, old := exe+".new", exe+".old"
	if err := os.WriteFile(fresh, data, mode); err != nil {
		return "", err
	}

	if err := os.Chmod(fresh, mode); err != nil {
		_ = os.Remove(fresh)

		return "", err
	}

	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(fresh)

		return "", err
	}

	if err := os.Rename(fresh, exe); err != nil {
		_ = os.Rename(old, exe)

		return "", err
	}

	_ = os.Remove(old)

	return exe, nil
}

// fetch reads a URL within timeout, up to maxArchive bytes.
func fetch(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxArchive))
}
