package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// TestBuiltinThemeDefinesUIScopes keeps every scope the interface reads
// spelled out in the built in theme, so an exported copy shows them all.
func TestBuiltinThemeDefinesUIScopes(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("theme.go")
	if err != nil {
		t.Fatal(err)
	}

	th := theme.Default()
	scopes := regexp.MustCompile(`t\.Get\("([a-z0-9._-]+)"\)`).FindAllStringSubmatch(string(src), -1)
	if len(scopes) < 40 {
		t.Fatalf("found only %d scopes in theme.go", len(scopes))
	}

	for _, m := range scopes {
		if !th.Defines(m[1]) {
			t.Errorf("the built in theme does not define %s", m[1])
		}
	}

	if th.RainbowCount() == 0 {
		t.Error("the built in theme has no rainbow colors")
	}
}

// TestBuiltinThemeStylesCaptures gives every capture of the catalogue
// highlight queries a style, directly or through a parent scope.
func TestBuiltinThemeStylesCaptures(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("../../catalog/queries/*/highlights.scm")
	if err != nil || len(files) == 0 {
		t.Fatalf("no queries: %v", err)
	}

	strs := regexp.MustCompile(`"(\\.|[^"\\])*"`)
	comments := regexp.MustCompile(`;.*`)
	captures := regexp.MustCompile(`@([a-zA-Z][\w.-]*)`)
	th := theme.Default()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}

		text := comments.ReplaceAllString(strs.ReplaceAllString(string(data), `""`), "")
		for _, m := range captures.FindAllStringSubmatch(text, -1) {
			name := m[1]
			// none turns highlighting off, clean only feeds predicates.
			if name == "none" || name == "clean" || strings.HasPrefix(name, "injection") || strings.HasPrefix(name, "local") {
				continue
			}

			if _, ok := th.TryGet(name); !ok {
				t.Errorf("%s: no style for @%s", filepath.Base(filepath.Dir(f)), name)
			}
		}
	}
}
