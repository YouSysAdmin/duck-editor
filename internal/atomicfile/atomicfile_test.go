package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"
)

func TestWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	for _, text := range []string{"one", "two"} {
		if err := Write(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}

		got, err := os.ReadFile(path)
		if err != nil || string(got) != text {
			t.Fatalf("read %q %v want %q", got, err, text)
		}
	}

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
}

func TestWriteFromFailureKeepsOldFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "lib.so")
	if err := Write(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	broken := errors.New("broken")
	if err := WriteFrom(path, iotest.ErrReader(broken), 0o755); !errors.Is(err, broken) {
		t.Fatalf("got %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Fatalf("the old file is %q", got)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files stay behind: %v", entries)
	}
}

func TestWriteMissingDir(t *testing.T) {
	t.Parallel()

	if err := Write(filepath.Join(t.TempDir(), "no", "file"), nil, 0o600); err == nil {
		t.Fatal("a missing directory is an error")
	}
}
