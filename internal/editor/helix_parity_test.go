package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// TestSelectModeWordMotions keeps the anchor char selected when a word
// motion in select mode turns back, as Helix does.
func TestSelectModeWordMotions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ keys, want string }{
		{"glvbd", "one two \n"},
		{"wwvbbd", "wo three\n"},
		{"eevbd", "onewo three\n"},
	} {
		e := newTestEditor(t, "#[o|]#ne two three\n")
		e.feed(t, tc.keys)
		if got := e.Doc().Text().String(); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.keys, got, tc.want)
		}
	}
}

// TestEscapeTrimsIndentOnlyLine leaves no indentation behind on a line
// insert mode opened and left empty.
func TestEscapeTrimsIndentOnlyLine(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "\t#[x|]#\n")
	e.feed(t, "o<esc>")
	if got := e.Doc().Text().String(); got != "\tx\n\n" {
		t.Fatalf("text %q", got)
	}
}

// TestStarInsideWord adds a word boundary only where the selection
// starts or ends a word.
func TestStarInsideWord(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[f|]#oo bar\nfoo.baz foo\nfoobar\n")
	e.feed(t, "*")
	if got, _ := e.readRegister('/'); len(got) == 0 || got[0] != `\bf` {
		t.Fatalf("search register %q", got)
	}

	e = newTestEditor(t, "#[foo|]# bar\n")
	e.feed(t, "*")
	if got, _ := e.readRegister('/'); len(got) == 0 || got[0] != `\bfoo\b` {
		t.Fatalf("whole word register %q", got)
	}
}

// TestJoinCommentLines keeps one comment token when joining comments.
func TestJoinCommentLines(t *testing.T) {
	t.Parallel()

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "t.py")
	if err := os.WriteFile(path, []byte("# a\n# b\nx = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	e.Syntax = syntax.NewLoader(nil, langs)
	e.lspDisabled = true
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	e.feed(t, "xJ")
	if got := doc.Text().String(); got != "# a b\nx = 1\n" {
		t.Fatalf("joined %q", got)
	}
}

// TestSoftWrapVerticalFromColumnZero moves between wrapped rows of a
// line from its first column without skipping to a row end.
func TestSoftWrapVerticalFromColumnZero(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("abcdefghij", 40)
	e := newTestEditor(t, "#[s|]#hort\n"+long+"\nend\n")
	e.Resize(80, 20)
	e.runTyped(&Context{Editor: e}, "set soft-wrap.enable true")
	e.feed(t, "j")
	start := e.Doc().Selection().Primary().Cursor(e.Doc().Text())
	e.feed(t, "jk")
	if got := e.Doc().Selection().Primary().Cursor(e.Doc().Text()); got != start {
		t.Fatalf("j then k went from %d to %d", start, got)
	}

	e.feed(t, "j")
	col := e.Doc().Selection().Primary().Cursor(e.Doc().Text()) - start
	if col <= 0 || col >= 80 {
		t.Fatalf("j from the first column landed at column %d", col)
	}
}

// TestIncrementDateTime changes dates and times in their smallest unit,
// as Helix does.
func TestIncrementDateTime(t *testing.T) {
	t.Parallel()

	tests := []struct{ text, keys, want string }{
		{"#[2024-01-31|]#\n", "<C-a>", "2024-02-01\n"},
		{"#[2024-02-28 23:59|]#\n", "<C-a>", "2024-02-29 00:00\n"},
		{"#[1|]#2:30\n", "45<C-a>", "13:15\n"},
		{"#[2024-03-01|]#\n", "<C-x>", "2024-02-29\n"},
		{"#[41|]#\n", "<C-a>", "42\n"},
	}
	for _, tt := range tests {
		e := newTestEditor(t, tt.text)
		e.feed(t, tt.keys)
		if got := e.Doc().Text().String(); got != tt.want {
			t.Errorf("%q %s: got %q, want %q", tt.text, tt.keys, got, tt.want)
		}
	}
}
