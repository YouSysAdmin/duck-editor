package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/syntax"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// TestBufferInfo names the language, the configured servers and why a
// server does not run.
func TestBufferInfo(t *testing.T) {
	t.Parallel()

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	e.Syntax = syntax.NewLoader(nil, langs)
	e.lspDisabled = true
	if _, err := e.Open(path, ActionReplace); err != nil {
		t.Fatal(err)
	}

	e.runTyped(&Context{Editor: e}, "info")
	if e.Popup == nil {
		t.Fatal("no popup")
	}

	text := strings.Join(ui.Strings(e.Popup.Lines), "\n")
	for _, want := range []string{"language    go", "gopls  off", "formatter", "indent      tabs"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}
