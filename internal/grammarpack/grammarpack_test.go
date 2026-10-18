package grammarpack

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()

	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return b.Bytes()
}

func TestPathExtractsOnce(t *testing.T) {
	t.Parallel()

	pack := New(fstest.MapFS{
		"go.so.gz":  {Data: gz(t, "go library")},
		"README.md": {Data: []byte("x")},
	})
	if got := pack.Names(); len(got) != 1 || got[0] != "go" {
		t.Fatalf("names = %v", got)
	}

	root := t.TempDir()
	stale := filepath.Join(root, "0123456789abcdef")
	other := filepath.Join(root, "keep-me")
	for _, d := range []string{stale, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	p, err := pack.Path(root, "go")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(p)
	if string(data) != "go library" || filepath.Base(filepath.Dir(p)) != pack.Version() {
		t.Fatalf("extracted %q to %s", data, p)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a stale cache directory should be removed")
	}

	if _, err := os.Stat(other); err != nil {
		t.Error("unrelated directories stay")
	}

	// A second call reuses the file.
	if err := os.WriteFile(p, []byte("cached"), 0o755); err != nil {
		t.Fatal(err)
	}

	if p2, _ := pack.Path(root, "go"); p2 != p {
		t.Fatalf("path changed to %s", p2)
	}

	if data, _ := os.ReadFile(p); string(data) != "cached" {
		t.Error("an extracted library is not written again")
	}

	for _, name := range []string{"rust", "../go"} {
		if _, err := pack.Path(root, name); !errors.Is(err, ErrNotEmbedded) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestVersionFollowsContent(t *testing.T) {
	t.Parallel()

	a := New(fstest.MapFS{"go.so.gz": {Data: gz(t, "a")}})
	b := New(fstest.MapFS{"go.so.gz": {Data: gz(t, "b")}})
	if a.Version() == "" || a.Version() == b.Version() {
		t.Fatalf("versions %q %q", a.Version(), b.Version())
	}

	if New(fstest.MapFS{}).Version() != "" {
		t.Error("an empty pack has no version")
	}
}
