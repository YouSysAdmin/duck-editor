package vcs

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// TestDirStatus reports the changed entries of a subdirectory, a
// directory with changes of several kinds as modified.
func TestDirStatus(t *testing.T) {
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
	write := func(name, text string) {
		t.Helper()

		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q")
	for _, name := range []string{"src/a.go", "src/b.go", "src/mixed/old.go", "top.txt"} {
		write(name, "one\n")
	}

	git("add", ".")
	git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	write("src/a.go", "two\n")
	write("src/new.go", "x\n")
	write("src/fresh/x.go", "x\n")
	write("src/mixed/old.go", "two\n")
	write("src/mixed/add.go", "x\n")
	write("top.txt", "two\n")

	got, err := DirStatus(t.Context(), host.Local{}, filepath.Join(dir, "src"))
	if err != nil {
		t.Fatal(err)
	}

	if !InRepository(host.Local{}, filepath.Join(dir, "src", "mixed")) || InRepository(host.Local{}, t.TempDir()) {
		t.Fatal("InRepository is wrong about the repository or a directory outside")
	}

	want := map[string]rune{"a.go": '~', "new.go": '+', "fresh": '+', "mixed": '~'}
	if !maps.Equal(got, want) {
		t.Fatalf("status %q, want %q", got, want)
	}
}
