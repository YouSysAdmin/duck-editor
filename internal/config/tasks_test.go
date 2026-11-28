package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

func TestLoadTasks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")
	data := `site: hugo server -D
build:
  command: make
  dir: src
  description: build everything
  env:
    B: "2"
    A: "1"
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks, err := LoadTasks(host.File{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 2 || tasks[0].Name != "site" || tasks[0].Command != "hugo server -D" {
		t.Fatalf("tasks %+v, file order and the short form", tasks)
	}

	b := tasks[1]
	if b.Command != "make" || b.Dir != "src" || b.Description != "build everything" || !slices.Equal(b.Env, []string{"A=1", "B=2"}) {
		t.Fatalf("table form %+v", b)
	}

	none, err := LoadTasks(host.File{Path: filepath.Join(dir, "missing.yaml")})
	if err != nil || none != nil {
		t.Fatalf("a missing file has no tasks, got %v %v", none, err)
	}

	for _, bad := range []string{"x:\n  cmd: make\n", "x:\n  dir: a\n", "- a\n", "x: [a]\n"} {
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadTasks(host.File{Path: path})
		if err == nil || !strings.Contains(err.Error(), "line") {
			t.Fatalf("%q loaded without a line in the error: %v", bad, err)
		}
	}
}
