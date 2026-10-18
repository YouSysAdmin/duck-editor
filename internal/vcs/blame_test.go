package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

func TestBlame(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	git("init", "-q")
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	git("add", "a.txt")
	git("-c", "user.name=Ann", "-c", "user.email=ann@example.com", "commit", "-q", "-m", "first lines")

	b, err := Blame(context.Background(), host.Local{}, path, "one\nchanged\nnew\n", 0)
	if err != nil {
		t.Fatal(err)
	}

	if !b.Committed() || b.Author != "Ann" || b.AuthorMail != "ann@example.com" || b.Summary != "first lines" || b.Time.IsZero() {
		t.Fatalf("committed line = %+v", b)
	}

	b, err = Blame(context.Background(), host.Local{}, path, "one\nchanged\nnew\n", 2)
	if err != nil {
		t.Fatal(err)
	}

	if b.Committed() {
		t.Fatalf("an unsaved line is not committed: %+v", b)
	}
}

func TestRelativeTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		10 * time.Second:     "just now",
		time.Minute:          "1 minute ago",
		3 * time.Hour:        "3 hours ago",
		50 * time.Hour:       "2 days ago",
		400 * 24 * time.Hour: "1 year ago",
	}
	for ago, want := range cases {
		if got := RelativeTime(now.Add(-ago), now); got != want {
			t.Errorf("%v ago = %q, want %q", ago, got, want)
		}
	}
}
