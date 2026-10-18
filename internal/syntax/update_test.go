//go:build cgo

package syntax

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// describe renders the trees and ranges of every layer.
func describe(ly *layer) string {
	var b strings.Builder
	var walk func(ly *layer, indent string)
	walk = func(ly *layer, indent string) {
		fmt.Fprintf(&b, "%s%s %v\n", indent, ly.data.lang.Name, ly.ranges)
		if ly.tree != nil {
			fmt.Fprintf(&b, "%s%s\n", indent, ly.tree.RootNode().ToSexp())
		}

		for _, c := range ly.children {
			walk(c, indent+"  ")
		}
	}
	walk(ly, "")

	return b.String()
}

// TestUpdateMatchesFreshParse edits with several cursors and checks that
// the incremental trees and injection layers equal a parse from scratch.
func TestUpdateMatchesFreshParse(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	goLang, _ := langs.Get("go")
	loader := NewLoader(dirs, langs)
	src := "package main\n\n// one\n// two\nfunc a() {\n\tregexp.MustCompile(`a+`)\n}\n\n/* block */\nfunc b() {\n\tx := 1 // tail\n}\n"
	text := buffer.NewRope(src)
	s, err := New(loader, goLang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	steps := [][]edit.Change{
		// Several cursors in one change, the later ones after earlier
		// insertions and deletions.
		{{From: 15, To: 15, Text: "x"}, {From: 30, To: 32, Text: ""}, {From: 60, To: 60, Text: "\n// new\n"}},
		// Open a string that swallows the rest of the line.
		{{From: 5, To: 5, Text: "\"q"}},
		{{From: 5, To: 7, Text: ""}},
		// Remove a comment layer and add a regex layer.
		{{From: 0, To: 0, Text: "// head\n"}, {From: text.Len() - 2, To: text.Len() - 2, Text: "\n\tregexp.MustCompile(`b`)"}},
	}
	// A log that drops its first line while it grows at the end. The
	// ranges are computed from the text the earlier steps leave.
	steps = append(steps, nil)
	// A space right before the block comment and one right after it, the
	// comment layer must survive both.
	steps = append(steps, []edit.Change{{From: strings.Index(src, "/* block */") + 1, To: strings.Index(src, "/* block */") + 1, Text: " "}})
	steps = append(steps, []edit.Change{{From: strings.Index(src, "/* block */") + 1 + len("/* block */"), To: strings.Index(src, "/* block */") + 1 + len("/* block */"), Text: " "}})
	for i, changes := range steps {
		if changes == nil {
			first := text.LineToChar(1)
			changes = []edit.Change{{From: 0, To: first, Text: ""}, {From: text.Len(), To: text.Len(), Text: "// tail\n"}}
		}

		cs := edit.ChangeSetFromChanges(text.Len(), changes)
		next := text
		if !cs.Apply(&next) {
			t.Fatalf("step %d does not apply", i)
		}

		// The byte copy is patched, not rebuilt.
		s.bytes()
		s.Update(text, next, &cs)
		text = next
		if !s.bytesValid || string(s.bytes()) != text.String() {
			t.Fatalf("step %d: byte copy out of date", i)
		}

		fresh, err := New(loader, goLang, text)
		if err != nil {
			t.Fatal(err)
		}

		want := describe(fresh.root)
		fresh.Close()
		if got := describe(s.root); got != want {
			t.Fatalf("step %d differs from a fresh parse\ngot:\n%s\nwant:\n%s", i, got, want)
		}
	}
}

// TestUpdateKeepsLayersBetweenEdits reuses the injection layers between an
// edit at the start and one at the end.
func TestUpdateKeepsLayersBetweenEdits(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	goLang, _ := langs.Get("go")
	loader := NewLoader(dirs, langs)
	src := "package main\n\n// one\n\nfunc a() {}\n\n// two\n\nfunc b() {}\n"
	text := buffer.NewRope(src)
	s, err := New(loader, goLang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	middle := s.root.children[0]
	cs := edit.ChangeSetFromChanges(text.Len(), []edit.Change{{From: 0, To: 0, Text: "\n"}, {From: text.Len(), To: text.Len(), Text: "// end\n"}})
	next := text
	cs.Apply(&next)
	s.Update(text, next, &cs)
	if !slices.Contains(s.root.children, middle) {
		t.Fatal("the comment layer between the edits was rebuilt")
	}
}
