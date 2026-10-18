//go:build cgo

package syntax

import (
	"os"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// TestPerfLargeFile times parsing and editing a real file. Set
// DUCK_PERF_FILE to a source file to run it.
func TestPerfLargeFile(t *testing.T) {
	path := os.Getenv("DUCK_PERF_FILE")
	if path == "" {
		t.Skip("DUCK_PERF_FILE not set")
	}

	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	lang, ok := langs.ForPath(path)
	if !ok {
		t.Fatalf("no language for %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	text := buffer.NewRope(string(data))
	loader := NewLoader(dirs, langs)

	start := time.Now()
	s, err := New(loader, lang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()
	t.Logf("initial parse with injections: %v (%d layers)", time.Since(start), countLayers(s.root))

	start = time.Now()
	n := len(s.Highlights(0, min(text.LenBytes(), 4000)))
	t.Logf("highlights for first 4000 bytes: %v (%d spans)", time.Since(start), n)

	start = time.Now()
	for i := range 20 {
		pos := 100 + i
		cs := edit.ChangeSetFromChanges(text.Len(), []edit.Change{{From: pos, To: pos, Text: "x"}})
		newText := text
		cs.Apply(&newText)
		s.Update(text, newText, &cs)
		text = newText
	}

	t.Logf("20 single char edits: %v", time.Since(start))
}

func countLayers(ly *layer) int {
	n := 1
	for _, c := range ly.children {
		n += countLayers(c)
	}

	return n
}
