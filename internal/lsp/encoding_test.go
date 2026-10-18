package lsp

import (
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

func TestPositionConversion(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("a𝄞b\ncd\r\nef")
	// U+1D11E is one char, two UTF-16 units, four UTF-8 bytes.
	cases := []struct {
		pos  int
		enc  Encoding
		want Position
	}{
		{0, UTF16, Position{0, 0}},
		{2, UTF16, Position{0, 3}},
		{2, UTF8, Position{0, 5}},
		{2, UTF32, Position{0, 2}},
		{4, UTF16, Position{1, 0}},
		{8, UTF16, Position{2, 0}},
	}
	for _, c := range cases {
		if got := PosToLSP(text, c.pos, c.enc); got != c.want {
			t.Errorf("PosToLSP(%d, %v) = %v, want %v", c.pos, c.enc, got, c.want)
		}

		back, ok := LSPToPos(text, c.want, c.enc)
		if !ok || back != c.pos {
			t.Errorf("LSPToPos(%v, %v) = %d, %v want %d", c.want, c.enc, back, ok, c.pos)
		}
	}

	// Past the line end clamps before the line ending.
	if p, _ := LSPToPos(text, Position{1, 99}, UTF16); p != 6 {
		t.Errorf("clamp = %d", p)
	}

	if p, _ := LSPToPos(text, Position{9, 0}, UTF16); p != text.Len() {
		t.Errorf("past end = %d", p)
	}
}

func TestChangesetToChanges(t *testing.T) {
	t.Parallel()

	old := buffer.NewRope("hello world\nsecond")
	cs := edit.ChangeSetFromChanges(old.Len(), []edit.Change{{From: 6, To: 11, Text: "there"}, {From: 13, To: 14}})
	newText := old
	cs.Apply(&newText)
	if newText.String() != "hello there\nscond" {
		t.Fatalf("new text %q", newText.String())
	}

	changes := ChangesetToChanges(old, newText, &cs, UTF16)
	if len(changes) != 2 {
		t.Fatalf("changes %+v", changes)
	}

	if *changes[0].Range != (Range{Position{0, 6}, Position{0, 11}}) || changes[0].Text != "there" {
		t.Errorf("first %+v", changes[0])
	}

	if *changes[1].Range != (Range{Position{1, 1}, Position{1, 2}}) || changes[1].Text != "" {
		t.Errorf("second %+v", changes[1])
	}

	// Inserting a newline moves following positions to the next line.
	old = buffer.NewRope("ab")
	cs = edit.ChangeSetFromChanges(old.Len(), []edit.Change{{From: 1, To: 1, Text: "\n"}, {From: 2, To: 2, Text: "!"}})
	newText = old
	cs.Apply(&newText)
	changes = ChangesetToChanges(old, newText, &cs, UTF16)
	if *changes[1].Range != (Range{Position{1, 1}, Position{1, 1}}) {
		t.Errorf("after newline %+v", changes[1])
	}
}

func TestTransactionFromEdits(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("abc\ndef")
	edits := []TextEdit{
		{Range: Range{Position{1, 0}, Position{1, 3}}, NewText: "xyz"},
		{Range: Range{Position{0, 0}, Position{0, 1}}, NewText: "A"},
		{Range: Range{Position{0, 0}, Position{0, 2}}, NewText: "overlap"},
	}
	tx := TransactionFromEdits(text, edits, UTF16)
	tx.Apply(&text)
	if text.String() != "Abc\nxyz" {
		t.Fatalf("got %q", text.String())
	}
}

func TestURIs(t *testing.T) {
	t.Parallel()

	uri := PathToURI("/tmp/a b/c.go")
	if uri != "file:///tmp/a%20b/c.go" {
		t.Fatalf("uri %q", uri)
	}

	if URIToPath(uri) != "/tmp/a b/c.go" {
		t.Fatalf("path %q", URIToPath(uri))
	}

	if URIToPath("https://x") != "" {
		t.Fatal("non file uri")
	}
}
