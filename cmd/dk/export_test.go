package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

func TestExport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	base := filepath.Join(dir, "duck")

	if err := exportCommand([]string{"theme", "mine"}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(base, "themes", "mine.yaml"))
	if th, err := theme.Parse("mine", string(data)); err != nil || len(th.Warnings) > 0 {
		t.Fatalf("exported theme: %v %v", err, th.Warnings)
	}

	if err := exportCommand([]string{"theme", "mine"}); err == nil {
		t.Fatal("an existing theme is replaced only with -force")
	}

	if err := exportCommand([]string{"theme", "-force", "mine"}); err != nil {
		t.Fatal(err)
	}

	if err := exportCommand([]string{"config"}); err != nil {
		t.Fatal(err)
	}

	if _, err := config.LoadFiles(filepath.Join(base, "config.yaml")); err != nil {
		t.Fatalf("exported config: %v", err)
	}

	if err := exportCommand([]string{"queries", "go/highlights.scm"}); err != nil {
		t.Fatal(err)
	}

	files, _ := filepath.Glob(filepath.Join(base, "runtime", "queries", "go", "*"))
	if len(files) != 1 || filepath.Base(files[0]) != "highlights.scm" {
		t.Fatalf("one query file expected, got %v", files)
	}

	if err := exportCommand([]string{"queries", "no-such-language"}); err == nil {
		t.Fatal("unknown language accepted")
	}
}
