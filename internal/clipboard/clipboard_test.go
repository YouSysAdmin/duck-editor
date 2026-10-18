package clipboard

import (
	"strings"
	"testing"
	"time"
)

func TestRunTimesOut(t *testing.T) {
	old := runTimeout
	runTimeout = 200 * time.Millisecond
	t.Cleanup(func() { runTimeout = old })

	start := time.Now()
	if _, err := run(Command{"sleep", []string{"5"}}, "", true); err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("hung provider error = %v", err)
	}

	if time.Since(start) > 3*time.Second {
		t.Fatal("the timeout did not stop the command")
	}
}

// TestDetectOverSSH prefers the terminal escape in an ssh session even
// when local clipboard tools exist.
func TestDetectOverSSH(t *testing.T) {
	t.Parallel()

	all := func(string) bool { return true }
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}

	if p, ok := detect("linux", env(map[string]string{"SSH_TTY": "/dev/pts/1", "DISPLAY": ":0"}), all).(*Memory); !ok || p.Title != "termcode" {
		t.Fatalf("ssh session picked %v", p)
	}

	if p := detect("darwin", env(map[string]string{}), all); p.Name() != "pasteboard" {
		t.Fatalf("local session picked %s", p.Name())
	}
}
