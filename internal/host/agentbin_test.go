package host

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// serveRelease serves release v1.0.0 with archives holding a dk that
// reads as the archive name.
func serveRelease(t *testing.T, names ...string) {
	t.Helper()

	files := map[string][]byte{}
	sums := ""
	for _, name := range names {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "dk", Mode: 0o755, Size: int64(len(name))})
		_, _ = tw.Write([]byte(name))
		_ = tw.Close()
		_ = gz.Close()
		files[name+".tar.gz"] = buf.Bytes()
		sum := sha256.Sum256(buf.Bytes())
		sums += fmt.Sprintf("%s  %s.tar.gz\n", hex.EncodeToString(sum[:]), name)
	}

	files["SHA256SUMS"] = []byte(sums)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	old := releaseBase
	releaseBase = srv.URL
	t.Cleanup(func() { releaseBase = old })
}

func readAgent(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// TestAgentBinaryPrefersStaticAgent installs the static Linux agent of a
// release, which runs on musl systems such as Alpine too.
func TestAgentBinaryPrefersStaticAgent(t *testing.T) {
	serveRelease(t, "dk-agent-linux-riscv", "dk-linux-riscv")
	cache := t.TempDir()
	if _, err := AgentBinary(cache, "1.0.0", "linux", "riscv", false); err == nil {
		t.Fatal("found an agent before downloading one")
	}

	path, err := AgentBinary(cache, "1.0.0", "linux", "riscv", true)
	if err != nil || readAgent(t, path) != "dk-agent-linux-riscv" {
		t.Fatalf("agent %s %v", path, err)
	}
}

// TestAgentBinaryOldRelease falls back to the full dk of a release
// without static agents and finds it again without downloading.
func TestAgentBinaryOldRelease(t *testing.T) {
	serveRelease(t, "dk-linux-riscv")
	cache := t.TempDir()
	path, err := AgentBinary(cache, "1.0.0", "linux", "riscv", true)
	if err != nil || readAgent(t, path) != "dk-linux-riscv" {
		t.Fatalf("agent %s %v", path, err)
	}

	again, err := AgentBinary(cache, "1.0.0", "linux", "riscv", false)
	if err != nil || readAgent(t, again) != "dk-linux-riscv" {
		t.Fatalf("second lookup %s %v", again, err)
	}
}
