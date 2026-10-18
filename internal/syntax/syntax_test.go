//go:build cgo

package syntax

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/config"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// runtimeDirs returns the runtime with compiled grammars for tests or skips.
func runtimeDirs(t *testing.T) []string {
	t.Helper()

	// The project runtime holds the compiled grammars during development.
	for _, c := range append([]string{filepath.Join("..", "..", "runtime")}, config.RuntimeDirs()...) {
		if _, err := os.Stat(filepath.Join(c, "grammars", "go.so")); err == nil {
			return []string{c}
		}
	}

	t.Skip("no runtime with a Go grammar available")

	return nil
}

func TestHighlightGo(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	goLang, _ := langs.Get("go")
	loader := NewLoader(dirs, langs)

	src := "package main\n\nfunc main() {\n\tx := \"hi\"\n}\n"
	text := buffer.NewRope(src)
	s, err := New(loader, goLang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	scopes := map[string]string{}
	for _, h := range s.Highlights(0, text.LenBytes()) {
		scopes[src[h.Start:h.End]] = h.Scope
	}

	if scopes["package"] != "keyword" && scopes["package"] != "keyword.control.import" {
		t.Errorf("package scope = %q", scopes["package"])
	}

	if scopes["func"] != "keyword.function" && scopes["func"] != "keyword" {
		t.Errorf("func scope = %q", scopes["func"])
	}

	if scopes["\"hi\""] != "string" {
		t.Errorf("string scope = %q", scopes["\"hi\""])
	}

	// Incremental update: turn x into a longer identifier.
	cs := edit.ChangeSetFromChanges(text.Len(), []edit.Change{{From: 29, To: 30, Text: "value"}})
	newText := text
	cs.Apply(&newText)
	s.Update(text, newText, &cs)
	found := false
	for _, h := range s.Highlights(0, newText.LenBytes()) {
		if newText.String()[h.Start:h.End] == "value" {
			found = true
		}
	}

	if !found {
		t.Error("identifier not highlighted after update")
	}
}

func TestInjectionsAndObjects(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dirs, langs)
	goLang, _ := langs.Get("go")

	src := "package main\n\n// TODO: fix this\nfunc main() {\n\tfmt.Println(a, (b))\n}\n"
	text := buffer.NewRope(src)
	s, err := New(loader, goLang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	// The comment grammar injects into Go comments and highlights TODO.
	found := false
	for _, h := range s.Highlights(0, text.LenBytes()) {
		if src[h.Start:h.End] == "TODO" {
			found = true
		}
	}

	if !found {
		t.Error("TODO in comment not highlighted through injection")
	}

	// Bracket matching through the tree.
	open := len("package main\n\n// TODO: fix this\nfunc main() {\n\tfmt.Println")
	if p, ok := s.MatchingBracket(text, open, false); !ok || text.Char(p) != ')' {
		t.Errorf("matching bracket of ( = %d %v", p, ok)
	}

	inside := open + 1
	if p, ok := s.MatchingBracket(text, inside, true); !ok || text.Char(p) != ')' {
		t.Errorf("fuzzy match from inside = %d %v", p, ok)
	}

	// Function text object around the cursor inside the body.
	r, ok := s.TextObject(text, edit.Point(inside), edit.Around, "function")
	if !ok || text.Slice(r.From(), r.To())[:4] != "func" {
		t.Errorf("function around = %v %v", r, ok)
	}

	r, ok = s.TextObject(text, edit.Point(inside), edit.Inside, "parameter")
	if !ok || r.Fragment(text) != "a" {
		t.Errorf("parameter inside = %q %v", r.Fragment(text), ok)
	}

	// Jump to the next function from the top.
	next := s.GotoTextObject(text, edit.Point(0), "function", edit.Forward, 1)
	if text.Slice(next.From(), next.To())[:4] != "func" {
		t.Errorf("goto function = %v", next)
	}

	// Expand from an identifier to the call and beyond.
	sel := s.ExpandSelection(text, edit.SingleSelection(inside, inside+1))
	if sel.Primary().Len() <= 1 {
		t.Errorf("expand did not grow: %v", sel.Primary())
	}
}

func TestIndentLevelGo(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	goLang, _ := langs.Get("go")
	loader := NewLoader(dirs, langs)
	src := "package main\n\nfunc main() {\n\tswitch x {\n\tcase 1:\n\t\tfoo(a,\n\t\t\tb)\n\t}\n}\n"
	text := buffer.NewRope(src)
	s, err := New(loader, goLang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()
	if !s.HasIndentQuery() {
		t.Fatal("go has an indent query")
	}

	cases := []struct {
		name    string
		pos     int
		newLine bool
		want    int
	}{
		{"after func brace", len("package main\n\nfunc main() {"), true, 1},
		{"after case colon", len("package main\n\nfunc main() {\n\tswitch x {\n\tcase 1:"), true, 2},
		{"inside args", len("package main\n\nfunc main() {\n\tswitch x {\n\tcase 1:\n\t\tfoo(a,"), true, 3},
		{"closing switch brace line", len("package main\n\nfunc main() {\n\tswitch x {\n\tcase 1:\n\t\tfoo(a,\n\t\t\tb)\n\t"), false, 1},
		{"closing func brace line", len(src) - 2, false, 0},
		{"top level", len("package main\n"), true, 0},
	}
	for _, c := range cases {
		got, ok := s.IndentFor(text, c.pos, c.newLine)
		if !ok || got.Level != c.want {
			t.Errorf("%s: level %d ok %v, want %d", c.name, got.Level, ok, c.want)
		}
	}
}

func TestRainbowAndOpaque(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	goLang, _ := langs.Get("go")
	loader := NewLoader(dirs, langs)
	src := "package main\n\nfunc f() {\n\tg(h(1), []int{2})\n\ts := `a\n  b`\n}\n"
	text := buffer.NewRope(src)
	s, err := New(loader, goLang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()
	// The function body braces are the outermost pair, the call parens one
	// level in, the nested call and the slice literal two levels in.
	byText := map[int]string{}
	for _, h := range s.RainbowBrackets(0, text.LenBytes()) {
		byText[h.Start] = h.Scope
	}

	bodyOpen := len("package main\n\nfunc f() ")
	callOpen := len("package main\n\nfunc f() {\n\tg")
	innerOpen := len("package main\n\nfunc f() {\n\tg(h")
	if byText[bodyOpen] != "rainbow.0" || byText[callOpen] != "rainbow.1" || byText[innerOpen] != "rainbow.2" {
		t.Fatalf("depths: body %q call %q inner %q (%v)", byText[bodyOpen], byText[callOpen], byText[innerOpen], byText)
	}

	// A range of just the inner call keeps its depth.
	inner := s.RainbowBrackets(innerOpen, innerOpen+1)
	if !slices.ContainsFunc(inner, func(h Highlight) bool { return h.Start == innerOpen && h.Scope == "rainbow.2" }) {
		t.Fatalf("inner range: %v", inner)
	}

	// A newline inside a raw string is opaque: the indent query keeps the
	// existing indentation instead of computing one.
	pos := len("package main\n\nfunc f() {\n\tg(h(1), []int{2})\n\ts := `a")
	in, ok := s.IndentFor(text, pos, true)
	if !ok || !in.Opaque {
		t.Fatalf("raw string indent = %+v ok %v", in, ok)
	}
}
