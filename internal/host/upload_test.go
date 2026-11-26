package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstallReportsProgress(t *testing.T) {
	for _, gz := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "gzip"}[gz], func(t *testing.T) {
			testInstall(t, gz)
		})
	}
}

func testInstall(t *testing.T, gz bool) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(t.TempDir(), "dk")
	data := strings.Repeat("x", 300<<10)
	if err := os.WriteFile(bin, []byte(data), 0o755); err != nil {
		t.Fatal(err)
	}

	var last, size atomic.Int64
	d := &commandDialer{
		name:     "test",
		argv:     func(_ bool, script string) []string { return []string{"sh", "-c", script} },
		agent:    func(string, string, bool) (string, error) { return bin, nil },
		progress: func(sent, total int64) { last.Store(sent); size.Store(total) },
		ids:      map[string]fileID{},
	}
	if err := d.install(context.Background(), false, needAgent{uname: "Linux x86_64", gzip: gz}); err != nil {
		t.Fatal(err)
	}

	if last.Load() != int64(len(data)) || size.Load() != int64(len(data)) {
		t.Fatalf("progress %d of %d, want %d", last.Load(), size.Load(), len(data))
	}

	id, _ := d.hash(bin)
	got, err := os.ReadFile(filepath.Join(home, ".cache/duck/agent", id, "dk"))
	if err != nil || string(got) != data {
		t.Fatalf("installed agent differs: %v", err)
	}
}

// blockingReader returns one chunk, then blocks until closed.
type blockingReader struct {
	first bool
	stop  chan struct{}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if !b.first {
		b.first = true

		return copy(p, "abc"), nil
	}

	<-b.stop

	return 0, io.EOF
}

func TestUploadStall(t *testing.T) {
	src := &blockingReader{stop: make(chan struct{})}
	defer close(src.stop)

	u := &uploadReader{r: src, size: 10}
	done := make(chan struct{})
	defer close(done)

	stalled := make(chan struct{})
	go u.watch(done, 10*time.Millisecond, func() { close(stalled) })

	if _, err := u.Read(make([]byte, 8)); err != nil {
		t.Fatal(err)
	}

	go func() { _, _ = u.Read(make([]byte, 8)) }()

	select {
	case <-stalled:
	case <-time.After(5 * time.Second):
		t.Fatal("stall not detected")
	}
}

func TestUploadWaitsForFirstRead(t *testing.T) {
	u := &uploadReader{r: strings.NewReader("abc"), size: 3}
	done := make(chan struct{})
	stalled := make(chan struct{})
	go u.watch(done, time.Millisecond, func() { close(stalled) })

	select {
	case <-stalled:
		t.Fatal("stalled before the first read")
	case <-time.After(1500 * time.Millisecond):
	}

	close(done)
}
