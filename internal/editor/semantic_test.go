package editor

import (
	"slices"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// The server's tokens color over the syntax, a builtin by its modifier,
// and stay on their words through an edit until the next answer.
func TestSemanticTokens(t *testing.T) {
	answer := []int{
		0, 0, 7, 0, 0, // "package" type
		0, 8, 4, 1, 2, // "main" function, defaultLibrary
	}
	e, _, _ := newLSPEditor(t, "package main\n", func(fs *fakeServer) { fs.semantic = func() []int { return answer } })
	doc := e.Doc()
	e.refreshSemanticTokens()
	runJobs(e)
	pump(t, e, func() bool { return doc.semantic.valid })

	scopes := func(from, to int) []string {
		var out []string
		for _, h := range doc.semanticHighlights(from, to) {
			out = append(out, doc.Text().Slice(h.Start, h.End)+"="+h.Scope)
		}

		return out
	}

	if got := scopes(0, 12); !slices.Equal(got, []string{"package=type", "main=function.builtin"}) {
		t.Fatalf("spans %v", got)
	}

	// In start order, which the renderer needs, each token after the syntax
	// spans of its word so it colors over them.
	all := doc.Highlights(0, 12)
	if !slices.IsSortedFunc(all, func(a, b HighlightSpan) int { return a.Start - b.Start }) {
		t.Fatalf("not in start order: %v", all)
	}

	for i, h := range all {
		if h.Scope == "type" || h.Scope == "function.builtin" {
			for _, later := range all[i+1:] {
				if later.Start == h.Start && later.Scope != h.Scope {
					t.Fatalf("a syntax span of %q comes after its token: %v", doc.Text().Slice(h.Start, h.End), all)
				}
			}
		}
	}

	// Two spaces typed before "main": the span moves with its word.
	tx := edit.ChangeText(doc.Text(), []edit.Change{{From: 7, To: 7, Text: "  "}})
	doc.Apply(&tx)
	if got := scopes(0, 14); !slices.Equal(got, []string{"package=type", "main=function.builtin"}) {
		t.Fatalf("after the edit %v", got)
	}
}

// Tokens on several lines all reach the renderer: it stops a line at the
// first span of the next, so the spans must stay in start order.
func TestMergeHighlights(t *testing.T) {
	syntax := []HighlightSpan{{0, 5, "variable"}, {6, 9, "function"}, {10, 15, "variable"}, {20, 25, "variable"}}
	tokens := []HighlightSpan{{0, 5, "variable.builtin"}, {20, 25, "type"}}
	got := mergeHighlights(syntax, tokens)
	want := []HighlightSpan{{0, 5, "variable"}, {0, 5, "variable.builtin"}, {6, 9, "function"}, {10, 15, "variable"}, {20, 25, "variable"}, {20, 25, "type"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestSemanticTokensOff(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n", func(fs *fakeServer) { fs.semantic = func() []int { return []int{0, 0, 7, 0, 0} } })
	e.Config.Editor.LSP.SemanticTokens = false
	e.refreshSemanticTokens()
	if e.Doc().semantic.cancel != nil {
		t.Fatal("asked with semantic-tokens off")
	}

	_ = fs
}
