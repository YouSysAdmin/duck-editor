package grammar

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

const fakeParser = `
#include <stdint.h>
typedef struct { uint32_t version; } TSLanguage;
static const TSLanguage lang = { 14 };
const TSLanguage *tree_sitter_fake(void) { return &lang; }
`

const fakeScanner = `
#include <stdbool.h>
void *tree_sitter_fake_external_scanner_create(void) { return 0; }
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildLocalGrammar(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler")
	}

	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "src-fake")
	writeFile(t, filepath.Join(srcRoot, "src", "parser.c"), fakeParser)
	writeFile(t, filepath.Join(srcRoot, "src", "scanner.c"), fakeScanner)
	rt := filepath.Join(dir, "runtime")

	var statuses []Status
	opts := Options{RuntimeDir: rt, Jobs: 1, Report: func(s Status) { statuses = append(statuses, s) }}
	grammars := []syntax.Grammar{{Name: "fake", Path: srcRoot}}
	if failed := Build(context.Background(), grammars, opts); failed != 0 {
		t.Fatalf("build failed: %+v", statuses)
	}

	lib := LibraryPath(rt, "fake")
	if _, err := os.Stat(lib); err != nil {
		t.Fatalf("library missing: %v", err)
	}

	if statuses[0].Message != "built" {
		t.Fatalf("status = %+v", statuses[0])
	}

	// A second build is a no op until a source changes.
	statuses = nil
	Build(context.Background(), grammars, opts)
	if statuses[0].Message != "already built" {
		t.Fatalf("status = %+v", statuses[0])
	}

	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(srcRoot, "src", "scanner.c"), future, future); err != nil {
		t.Fatal(err)
	}

	statuses = nil
	Build(context.Background(), grammars, opts)
	if statuses[0].Message != "built" {
		t.Fatalf("status after touch = %+v", statuses[0])
	}

	// A broken source keeps the previous library.
	writeFile(t, filepath.Join(srcRoot, "src", "parser.c"), "this is not C")
	statuses = nil
	if failed := Build(context.Background(), grammars, opts); failed != 1 || statuses[0].Err == nil {
		t.Fatalf("expected failure, got %+v", statuses)
	}

	if _, err := os.Stat(lib); err != nil {
		t.Fatalf("library removed on failed build: %v", err)
	}
}

func TestFetchGitGrammar(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}

	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	writeFile(t, filepath.Join(origin, "src", "parser.c"), fakeParser)
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"config", "uploadpack.allowReachableSHA1InWant", "true"},
		{"add", "."},
		{"commit", "-qm", "init"},
	} {
		if _, err := git(ctx, origin, args...); err != nil {
			t.Fatal(err)
		}
	}

	rev, err := git(ctx, origin, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	rt := filepath.Join(dir, "runtime")
	var statuses []Status
	opts := Options{RuntimeDir: rt, Jobs: 1, Report: func(s Status) { statuses = append(statuses, s) }}
	grammars := []syntax.Grammar{{Name: "fake", Git: origin, Rev: rev}}
	if failed := Fetch(ctx, grammars, opts); failed != 0 {
		t.Fatalf("fetch failed: %+v", statuses)
	}

	if _, err := os.Stat(filepath.Join(SourceDir(rt, "fake"), "src", "parser.c")); err != nil {
		t.Fatalf("checkout missing: %v", err)
	}

	statuses = nil
	Fetch(ctx, grammars, opts)
	if statuses[0].Message != "up to date" {
		t.Fatalf("second fetch = %+v", statuses[0])
	}

	if _, err := exec.LookPath("cc"); err == nil {
		if failed := Build(ctx, grammars, opts); failed != 0 {
			t.Fatalf("build after fetch failed: %+v", statuses)
		}
	}
}
