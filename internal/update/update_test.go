package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.1.0", "0.1.1", true},
		{"1.9.9", "2.0.0", true},
		{"0.2.0", "0.2.0", false},
		{"0.3.0", "0.2.9", false},
		{"v0.1.0", "v0.1.1", true},
		{"0.1.0-rc1", "0.1.0", false},
		{"dev", "9.9.9", false},
		{"0.1.0", "latest", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// serve answers the latest release page with a redirect to tag and the
// release archives of version, counting the requests.
func serve(t *testing.T, tag string, files map[string][]byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.URL.Path == "/latest":
			http.Redirect(w, r, "https://github.com/"+Repo+"/releases/tag/"+tag, http.StatusFound)
		case files[filepath.Base(r.URL.Path)] != nil:
			_, _ = w.Write(files[filepath.Base(r.URL.Path)])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	oldLatest, oldBase := LatestURL, ReleaseBase
	LatestURL, ReleaseBase = srv.URL+"/latest", srv.URL
	t.Cleanup(func() { LatestURL, ReleaseBase = oldLatest, oldBase })

	return srv, &hits
}

// release packs content as the dk binary of the archive name.tar.gz and
// returns the files of the release with their SHA256SUMS.
func release(name, content string) map[string][]byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "dk", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())

	return map[string][]byte{
		name + ".tar.gz": buf.Bytes(),
		"SHA256SUMS":     fmt.Appendf(nil, "%s  %s.tar.gz\n", hex.EncodeToString(sum[:]), name),
	}
}

func TestLatest(t *testing.T) {
	serve(t, "v0.4.2", nil)
	got, err := Latest(context.Background())
	if err != nil || got != "0.4.2" {
		t.Fatalf("Latest() = %q, %v", got, err)
	}
}

func TestCheckAsksOnceADay(t *testing.T) {
	_, hits := serve(t, "v0.4.2", nil)
	cache := t.TempDir()
	latest, newer, err := Check(context.Background(), cache, "0.4.0")
	if err != nil || latest != "0.4.2" || !newer {
		t.Fatalf("Check() = %q, %v, %v", latest, newer, err)
	}

	if _, err := os.Stat(filepath.Join(cache, "update.json")); err != nil {
		t.Fatal("no stamp written:", err)
	}

	latest, newer, err = Check(context.Background(), cache, "0.4.2")
	if err != nil || latest != "0.4.2" || newer {
		t.Fatalf("second Check() = %q, %v, %v", latest, newer, err)
	}

	if hits.Load() != 1 {
		t.Fatalf("GitHub asked %d times, want once", hits.Load())
	}

	if _, _, err := Check(context.Background(), cache, "dev"); err != nil || hits.Load() != 1 {
		t.Fatal("a development build asked GitHub")
	}
}

func TestDownloadChecksTheSum(t *testing.T) {
	files := release("dk-linux-amd64", "binary")
	files["SHA256SUMS"] = []byte(strings.Repeat("0", 64) + "  dk-linux-amd64.tar.gz\n")
	serve(t, "v1.0.0", files)
	if _, err := Download(context.Background(), "1.0.0", "dk-linux-amd64"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Download() = %v, want a checksum error", err)
	}

	if _, err := Download(context.Background(), "1.0.0", "dk-plan9-mips"); err == nil || !strings.Contains(err.Error(), ErrNoArchive.Error()) {
		t.Fatalf("Download() = %v, want %v", err, ErrNoArchive)
	}
}

func TestInstallReplacesTheBinary(t *testing.T) {
	name := "dk-" + runtime.GOOS + "-" + runtime.GOARCH
	serve(t, "v1.0.0", release(name, "new binary"))
	dir := t.TempDir()
	exe := filepath.Join(dir, "dk")
	if err := os.WriteFile(exe, []byte("old binary"), 0o700); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}

	old := ExecutablePath
	ExecutablePath = func() (string, error) { return filepath.EvalSymlinks(link) }
	t.Cleanup(func() { ExecutablePath = old })

	var steps []string
	replaced, err := Install(context.Background(), "1.0.0", func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}

	if want, _ := filepath.EvalSymlinks(exe); replaced != want {
		t.Fatalf("replaced %s, want %s", replaced, want)
	}

	data, err := os.ReadFile(exe)
	if err != nil || string(data) != "new binary" {
		t.Fatalf("binary holds %q, %v", data, err)
	}

	info, _ := os.Stat(exe)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v, want 0700", info.Mode().Perm())
	}

	if len(steps) != 2 {
		t.Fatalf("steps %q", steps)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("leftovers in %s: %v", dir, entries)
	}
}
