package editor

import (
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// newTestEditor builds an editor from annotated text and a view size.
func newTestEditor(t *testing.T, annotated string) *Editor {
	t.Helper()

	text, sel, err := edit.ParseAnnotated(annotated)
	if err != nil {
		t.Fatalf("bad annotation %q: %v", annotated, err)
	}

	doc := NewDocument("test.txt", text)
	doc.SetSelection(sel)
	cfg := config.Default()
	// Tests must not depend on .editorconfig files on this machine.
	cfg.Editor.EditorConfig = false
	// Completion fires immediately in tests instead of after idle-timeout.
	cfg.Editor.IdleTimeout = 0
	e := New(doc, cfg)
	e.vcsDisabled = true
	e.TakeJobs()
	e.Resize(80, 24)

	return e
}

func (e *Editor) feed(t *testing.T, keys string) {
	t.Helper()

	parsed, err := keymap.ParseMacro(keys)
	if err != nil {
		t.Fatalf("bad keys %q: %v", keys, err)
	}

	for _, k := range parsed {
		e.HandleKey(k)
	}
}

func (e *Editor) annotated() string {
	return edit.Annotate(e.Doc().Text(), e.Doc().Selection())
}

// check runs keys against the annotated input and compares the annotated
// output.
func check(t *testing.T, in, keys, want string) {
	t.Helper()

	wantText, wantSel, err := edit.ParseAnnotated(want)
	if err != nil {
		t.Fatalf("bad expectation %q: %v", want, err)
	}

	e := newTestEditor(t, in)
	e.feed(t, keys)
	// Shell commands run as background jobs.
	runJobs(e)
	if e.Doc().Text().String() != wantText || !sameRanges(e.Doc().Selection(), wantSel) {
		t.Errorf("%q after %q:\n got %q\nwant %q", in, keys, e.annotated(), want)
	}
}

// sameRanges compares selections ignoring the remembered visual column,
// which vertical motions set and annotations cannot express.
func sameRanges(a, b edit.Selection) bool {
	if a.PrimaryIndex() != b.PrimaryIndex() || a.Len() != b.Len() {
		return false
	}

	for i, r := range a.Ranges() {
		o := b.Ranges()[i]
		if r.Anchor != o.Anchor || r.Head != o.Head {
			return false
		}
	}

	return true
}

func TestMotions(t *testing.T) {
	t.Parallel()

	check(t, "#[h|]#ello world", "l", "h#[e|]#llo world")
	check(t, "#[h|]#ello world", "3l", "hel#[l|]#o world")
	check(t, "hell#[o|]# world", "h", "hel#[l|]#o world")
	check(t, "#[h|]#ello world", "w", "#[hello |]#world")
	check(t, "#[h|]#ello world", "e", "#[hello|]# world")
	check(t, "hello #[w|]#orld", "b", "#[|hello ]#world")
	check(t, "#[a|]#bc\ndef", "j", "abc\n#[d|]#ef")
	check(t, "abc\n#[d|]#ef", "k", "#[a|]#bc\ndef")
	check(t, "#[a|]#bc\ndef\nghi", "ge", "abc\ndef\n#[g|]#hi")
	check(t, "abc\ndef\n#[g|]#hi", "gg", "#[a|]#bc\ndef\nghi")
	check(t, "#[a|]#bc\ndef\nghi", "2G", "abc\n#[d|]#ef\nghi")
	check(t, "#[a|]#bc def", "$", "#[a|]#bc def")
	check(t, "#[a|]#bc def", "gl", "abc de#[f|]#")
	check(t, "abc d#[e|]#f", "gh", "#[a|]#bc def")
	check(t, "#[a|]#bc def", "fd", "#[abc d|]#ef")
	check(t, "#[a|]#bc def", "td", "#[abc |]#def")
	check(t, "abc de#[f|]#", "Fb", "a#[|bc def]#")
	check(t, "  #[a|]#bc\n\ndef", "]p", "  #[abc\n\n|]#def")
}

func TestSelectMode(t *testing.T) {
	t.Parallel()

	check(t, "#[h|]#ello world", "vl", "#[he|]#llo world")
	check(t, "#[h|]#ello world", "vw", "#[hello |]#world")
	check(t, "#[h|]#ello world", "ve<esc>", "#[hello|]# world")
	check(t, "#[h|]#ello world", "x", "#[hello world|]#")
	check(t, "#[a|]#bc\ndef\nghi", "x", "#[abc\n|]#def\nghi")
	check(t, "#[a|]#bc\ndef\nghi", "xx", "#[abc\ndef\n|]#ghi")
	check(t, "a#[b|]#c", ";", "a#[b|]#c")
	check(t, "#[abc|]#", ";", "ab#[c|]#")
	check(t, "#[abc|]#", "<A-;>", "#[|abc]#")
	check(t, "#[a|]#bc", "%", "#[abc|]#")
}

func TestInsertAndAppend(t *testing.T) {
	t.Parallel()

	check(t, "#[h|]#ello", "iX<esc>", "X#[|h]#ello")
	check(t, "#[h|]#ello", "aX<esc>", "#[hX|]#ello")
	check(t, "#[h|]#ello", "AX<esc>", "helloX#[|]#")
	check(t, "hel#[l|]#o", "IX<esc>", "X#[h|]#ello")
	check(t, "#[a|]#bc", "oX<esc>", "abc\nX#[|]#")
	check(t, "#[a|]#bc", "OX<esc>", "X#[\n|]#abc")
	check(t, "\t#[a|]#bc", "oX<esc>", "\tabc\n\tX#[|]#")
	check(t, "#[a|]#bc", "ihello<ret>world<esc>", "hello\nworld#[|a]#bc")
	check(t, "ab#[c|]#", "i<backspace><esc>", "a#[|c]#")
	check(t, "#[a|]#bc", "i<del><esc>", "#[b|]#c")
	check(t, "#[a|]#bc", "ix<C-h>y<esc>", "y#[|a]#bc")
	check(t, "hello #[w|]#orld", "i<C-w><esc>", "#[|w]#orld")
}

func TestDeleteChangeYankPaste(t *testing.T) {
	t.Parallel()

	check(t, "#[hello |]#world", "d", "#[w|]#orld")
	check(t, "#[hello|]# world", "cX<esc>", "X#[ |]#world")
	check(t, "#[hello|]# world", "yp", "hello#[hello|]# world")
	check(t, "#[hello|]# world", "yP", "#[hello|]#hello world")
	check(t, "#[abc\n|]#def\n", "yp", "abc\n#[abc\n|]#def\n")
	check(t, "#[abc\n|]#def\n", "d", "#[d|]#ef\n")
	check(t, "#[abc|]# def", "yeR", "abc#[abc|]#")
	check(t, "#[a|]#bc", "rx", "#[x|]#bc")
	check(t, "#[abc|]#", "~", "#[ABC|]#")
	check(t, "#[abc\ndef|]#", "J", "#[abc def|]#")
}

func TestUndoRedoGrouping(t *testing.T) {
	t.Parallel()

	check(t, "#[a|]#bc", "ihello<esc>u", "#[|a]#bc")
	check(t, "#[a|]#bc", "ihello<esc>uU", "hello#[|a]#bc")
	check(t, "#[a|]#bc", "ihello<esc>ix<esc>u", "hello#[|a]#bc")
	check(t, "#[a|]#bc", "ihello<esc>ix<esc>uu", "#[|a]#bc")
}

func TestMultiCursor(t *testing.T) {
	t.Parallel()

	check(t, "#[a|]#bc\ndef", "C", "#(a|)#bc\n#[d|]#ef")
	check(t, "#[a|]#bc\ndef", "CiX<esc>", "X#(|a)#bc\nX#[|d]#ef")
	check(t, "#[a|]#bc\ndef", "C,", "abc\n#[d|]#ef")
	check(t, "#[a|]#bc\ndef", "C<A-,>", "#[a|]#bc\ndef")
	check(t, "#[one two|]# three", "s[a-z]+<ret>", "#[one|]# #(two|)# three")
	check(t, "#[one two three|]#", "s[a-z]+<ret>)", "#(one|)# #[two|]# #(three|)#")
}

func TestSearch(t *testing.T) {
	t.Parallel()

	check(t, "#[a|]#bc def abc", "/def<ret>", "abc #[def|]# abc")
	check(t, "#[a|]#bc def abc", "/abc<ret>", "abc def #[abc|]#")
	check(t, "#[a|]#bc def abc", "/abc<ret>n", "#[abc|]# def abc")
	check(t, "#[a|]#bc def abc", "/abc<ret>nN", "abc def #[abc|]#")
	check(t, "#[a|]#bc def abc", "/zzz<ret>", "#[a|]#bc def abc")
	check(t, "#[a|]#bc def abc", "/d<esc>", "#[a|]#bc def abc")
	check(t, "#[abc|]# def abc def", "*n", "abc def #[abc|]# def")
}

func TestTypedCommands(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	e.feed(t, ":q<ret>")
	if !e.ShouldQuit() {
		t.Fatal("expected quit")
	}

	e = newTestEditor(t, "#[a|]#bc")
	e.feed(t, "ix<esc>:q<ret>")
	if e.ShouldQuit() || !e.Status().IsError {
		t.Fatalf("modified buffer must refuse quit: quit=%v status=%+v", e.ShouldQuit(), e.Status())
	}

	e.feed(t, ":q!<ret>")
	if !e.ShouldQuit() {
		t.Fatal("expected forced quit")
	}

	e = newTestEditor(t, "#[a|]#bc")
	e.feed(t, ":nope<ret>")
	if st := e.Status(); !st.IsError || !strings.Contains(st.Text, "no such command") {
		t.Fatalf("status = %+v", st)
	}
}

func TestCountAndRepeat(t *testing.T) {
	t.Parallel()

	check(t, "#[a|]#bcdef", "2d", "#[b|]#cdef")
	check(t, "#[a|]#bcdef", "ix<esc>l.", "xax#[|b]#cdef")
	check(t, "#[a|]#bcdef", "3ix<esc>", "x#[|a]#bcdef")
}

func TestScrolling(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := range 100 {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%5))
		sb.WriteByte('\n')
	}

	e := newTestEditor(t, "#[l|]#"+sb.String()[1:])
	// Ten text rows plus the statusline.
	e.Resize(40, 11)
	e.feed(t, "<C-d>")
	// The cursor landed on the first view line, scrolloff pulls the view
	// back so four lines of context stay above it.
	if e.View().FirstLine != 1 {
		t.Fatalf("first line after half page = %d", e.View().FirstLine)
	}

	if line := e.Doc().Selection().Primary().CursorLine(e.Doc().Text()); line != 5 {
		t.Fatalf("cursor line after half page = %d", line)
	}

	e.feed(t, "<pagedown>")
	if e.View().FirstLine != 11 {
		t.Fatalf("first line after page down = %d", e.View().FirstLine)
	}

	if line := e.Doc().Selection().Primary().CursorLine(e.Doc().Text()); line != 15 {
		t.Fatalf("cursor must be pulled into view at scrolloff, line %d", line)
	}

	e.feed(t, "50G")
	first := e.View().FirstLine
	if 49 < first || 49 >= first+10 {
		t.Fatalf("cursor line 49 not in view starting at %d", first)
	}

	e.feed(t, "zz")
	if e.View().FirstLine != 49-4 {
		t.Fatalf("center = %d", e.View().FirstLine)
	}
}

func TestRelativeTo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		base, path, want string
		ok               bool
	}{
		{"/a", "/a/b/c", "b/c", true},
		{"/a", "/a/..b/c", "..b/c", true},
		{"/a", "/b", "", false},
		{"/a/b", "/a", "", false},
	}
	for _, c := range cases {
		got, ok := RelativeTo(c.base, c.path)
		if got != c.want || ok != c.ok {
			t.Errorf("RelativeTo(%q, %q) = %q %v", c.base, c.path, got, ok)
		}
	}
}
