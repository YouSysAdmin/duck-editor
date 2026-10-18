package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/theme"

	"github.com/charmbracelet/x/ansi"
)

func splitPlain(s string) []string {
	return strings.Split(ansi.Strip(s), "\n")
}

func TestInsertInlays(t *testing.T) {
	t.Parallel()

	glyphs := Layout("a\tb", 4)
	glyphs = append(glyphs, Glyph{Text: " ", Col: LineWidth(glyphs), Width: 1, Char: 3, Chars: 1})
	out := InsertInlays(glyphs, []Inlay{{Char: 1, Text: ": int"}, {Char: 3, Text: "<"}}, 4)
	var cols []int
	var texts []string
	for _, g := range out {
		cols = append(cols, g.Col)
		texts = append(texts, g.Text)
	}

	wantTexts := []string{"a", ": int", "  ", "b", "<", " "}
	wantCols := []int{0, 1, 6, 8, 9, 10}
	for i := range wantTexts {
		if i >= len(out) || texts[i] != wantTexts[i] || cols[i] != wantCols[i] {
			t.Fatalf("glyphs = %q at %v, want %q at %v", texts, cols, wantTexts, wantCols)
		}
	}

	if got := InlayWidth([]Inlay{{Char: 1, Text: ": int"}, {Char: 3, Text: "<"}}, 2); got != 5 {
		t.Fatalf("InlayWidth = %d", got)
	}
}

func TestPromptCompletionLayout(t *testing.T) {
	t.Parallel()

	items := []string{"quit", "quit!", "open", "write", "buffer-close"}
	// Two columns of 30 fit in 70 cells, five items need three rows.
	if rows := PromptCompletionRows(items, 70, 10); rows != 3 {
		t.Fatalf("rows = %d", rows)
	}

	if rows := PromptCompletionRows(items, 70, 2); rows != 2 {
		t.Fatalf("capped rows = %d", rows)
	}

	th := DefaultTheme()
	out := RenderPromptCompletions(items, 3, 70, 3, &th)
	lines := splitPlain(out)
	if len(lines) != 3 || !hasPrefixAt(lines[0], 0, "quit") || !hasPrefixAt(lines[0], 35, "write") || !hasPrefixAt(lines[1], 35, "buffer-close") {
		t.Fatalf("layout:\n%s", out)
	}
}

func hasPrefixAt(line string, col int, s string) bool {
	runes := []rune(line)
	if col+len(s) > len(runes) {
		return false
	}

	return string(runes[col:col+len(s)]) == s
}

func TestRenderInfo(t *testing.T) {
	t.Parallel()

	entries := []InfoEntry{{Keys: "f", Desc: "Open file picker"}, {Keys: "g, G", Desc: "Changes"}}
	w, h := InfoSize("Space", entries, 10)
	th := DefaultTheme()
	lines := splitPlain(RenderInfo("Space", entries, 10, w, h, &th))
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "┌Space") || !strings.HasPrefix(lines[1], "│ f     Open file picker") || !strings.HasPrefix(lines[2], "│ g, G  Changes") {
		t.Fatalf("info box:\n%s", strings.Join(lines, "\n"))
	}

	for _, line := range lines {
		if ansi.StringWidth(line) != w || !strings.HasSuffix(line, "┐") && !strings.HasSuffix(line, "│") && !strings.HasSuffix(line, "┘") {
			t.Fatalf("row %q width %d, want %d", line, ansi.StringWidth(line), w)
		}
	}
}

func TestRenderInfoColumns(t *testing.T) {
	t.Parallel()

	entries := []InfoEntry{{Keys: "a", Desc: "one"}, {Keys: "b", Desc: "two"}, {Keys: "c", Desc: "three"}}
	w, h := InfoSize("T", entries, 2)
	if h != 4 {
		t.Fatalf("height = %d, want 2 rows plus border", h)
	}

	th := DefaultTheme()
	lines := splitPlain(RenderInfo("T", entries, 2, w, h, &th))
	if lines[1] != "│ a  one  c  three │" || !strings.HasPrefix(lines[2], "│ b  two ") {
		t.Fatalf("columns:\n%s", strings.Join(lines, "\n"))
	}
}

type fakeLines []string

func (f fakeLines) LineCount() int    { return len(f) }
func (f fakeLines) Line(i int) string { return f[i] }
func (f fakeLines) LineStart(i int) int {
	n := 0
	for _, l := range f[:i] {
		n += len([]rune(l)) + 1
	}

	return n
}

func TestIndentGuidesAndRulers(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	doc := fakeLines{"x", "        y", "    z"}
	vp := Viewport{
		Width: 30, Height: 3, LineCount: 3, TabWidth: 4, CursorLine: 0, Cursorcolumn: -1,
		IndentGuides: &IndentGuideOptions{Char: "│", Width: 4, Skip: 0},
		Rulers:       []int{25},
	}
	lines := splitPlain(RenderDocument(doc, vp, &th, nil))
	if !strings.HasPrefix(lines[1], "    2  │   │   y") || !strings.HasPrefix(lines[2], "    3  │   z") {
		t.Fatalf("indent guides:\n%s", strings.Join(lines, "\n"))
	}

	for _, l := range lines {
		if ansi.StringWidth(l) != 30 {
			t.Fatalf("row width %d: %q", ansi.StringWidth(l), l)
		}
	}

	if w := GutterWidthFor([]string{"line-numbers", "spacer"}, 12000, 3); w != 6 {
		t.Fatalf("gutter width = %d", w)
	}
}

func TestWrapGlyphs(t *testing.T) {
	t.Parallel()

	glyphs := Layout("  alpha beta gamma delta", 4)
	rows := WrapGlyphs(glyphs, &WrapOptions{Width: 12, MaxWrap: 4, MaxIndentRetain: 10, Indicator: "↪"})
	var texts []string
	for _, row := range rows {
		var sb strings.Builder
		for _, g := range row {
			sb.WriteString(g.Text)
		}

		texts = append(texts, sb.String())
	}

	// Whitespace at the wrap point stays on the row it ends.
	want := []string{"  alpha beta ", "  ↪gamma ", "  ↪delta"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", texts, want)
	}

	for _, row := range rows {
		if LineWidth(row) > 13 {
			t.Fatalf("row too wide: %v", row)
		}
	}

	// Position mapping works across rows and virtual prefixes.
	r, c := WrapPosition(rows, len("  alpha beta ")) // start of gamma
	if r != 1 || c != 3 {
		t.Fatalf("WrapPosition = %d,%d", r, c)
	}

	if ch := WrapCharAt(rows, 1, 4); ch != len("  alpha beta g") {
		t.Fatalf("WrapCharAt = %d", ch)
	}

	if got := WrapGlyphs(glyphs, nil); len(got) != 1 {
		t.Fatal("no options means one row")
	}
}

func TestRenderWrappedWithVirtualLines(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	doc := fakeLines{"one two three four", "x"}
	vp := Viewport{
		Width: 20, Height: 5, LineCount: 2, TabWidth: 4, CursorLine: 0, Cursorcolumn: -1,
		Gutters: []string{"line-numbers"}, Wrap: &WrapOptions{Width: 17, MaxWrap: 4, Indicator: ">"},
		VirtualLines: map[int][]VirtualLine{0: {{Text: "└─ problem"}}},
	}
	lines := splitPlain(RenderDocument(doc, vp, &th, nil))
	want := []string{"  1one two three ", "   >four ", "   └─ problem", "  2x ", ""}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Fatalf("row %d = %q, want prefix %q (all: %q)", i, lines[i], w, lines)
		}
	}

	if vp.RowCount(doc[0], 0, false) != 3 {
		t.Fatalf("row count = %d", vp.RowCount(doc[0], 0, false))
	}
}

func TestSpanLayers(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	doc := fakeLines{"abcd"}
	vp := Viewport{Width: 12, Height: 1, LineCount: 1, TabWidth: 4, Cursorcolumn: -1}
	sel := Span{Start: 0, End: 4, Style: theme.Style{Bg: color.RGBA{R: 200, A: 255}}, Layer: LayerSelection}
	syn := Span{Start: 1, End: 3, Style: theme.Style{Bg: color.RGBA{B: 200, A: 255}}, Layer: LayerSyntax}

	// The syntax span starts later but sits below the selection.
	got := RenderDocument(doc, vp, &th, []Span{sel, syn})
	want := RenderDocument(doc, vp, &th, []Span{sel})
	if got != want {
		t.Fatalf("syntax overrode the selection:\n%q\n%q", got, want)
	}
}

func TestWrapRenderedWhitespace(t *testing.T) {
	t.Parallel()

	ws := &WhitespaceGlyphs{Space: "·", RenderSpace: true}
	glyphs := LayoutWith("  alpha beta gamma delta", LayoutOptions{TabWidth: 4, Whitespace: ws})
	rows := WrapGlyphs(glyphs, &WrapOptions{Width: 12, MaxWrap: 4, MaxIndentRetain: 10, Indicator: "↪"})
	var texts []string
	for _, row := range rows {
		var sb strings.Builder
		for _, g := range row {
			sb.WriteString(g.Text)
		}

		texts = append(texts, sb.String())
	}

	// Visible spaces still keep the indent and break like spaces.
	want := []string{"··alpha·beta·", "  ↪gamma·", "  ↪delta"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", texts, want)
	}
}

func TestRenderPickerFrame(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	v := PickerView{
		Width: 40, Height: 7, Title: "Files", Query: "ma", Matched: 1, Total: 3,
		Rows:    []PickerRow{{Text: "main.go", Positions: []int{0, 1}, Selected: true}},
		Preview: []string{"package main"}, PreviewTitle: "main.go",
	}
	lines := splitPlain(RenderPicker(v, &th))
	want := []string{
		"┌─ Files ──────────┐┌─ main.go ────────┐",
		"│ ma           1/3 ││ package main     │",
		"│──────────────────││                  │",
		"│ > main.go        ││                  │",
		"│                  ││                  │",
		"│                  ││                  │",
		"└──────────────────┘└──────────────────┘",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("picker:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	l := NewPickerLayout(40, 7, true)
	if l.ListHeight != 3 || l.ListWidth != 16 || l.PreviewWidth != 16 || l.PreviewHeight != 5 {
		t.Fatalf("layout %+v", l)
	}
}

func TestPickerBorderVisible(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	if th.PickerBorder.Fg == nil || th.PickerBorder.Fg == th.Popup.Bg || th.PickerBorder.Fg == th.Window.Fg {
		t.Fatalf("picker border %+v blends into popup %+v", th.PickerBorder, th.Popup)
	}
}

// TestLongLineRendersVisiblePart lays out only what fits on screen and
// draws the same as a line cut at the screen edge.
func TestLongLineRendersVisiblePart(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	long := strings.Repeat("ab ", 400000)
	short := long[:200]
	for _, wrap := range []*WrapOptions{nil, {Width: 20, MaxWrap: 4}} {
		vp := Viewport{Width: 24, Height: 4, LineCount: 2, TabWidth: 4, Cursorcolumn: -1, Wrap: wrap, Gutters: []string{"line-numbers"}}
		got := RenderDocument(fakeLines{long, "x"}, vp, &th, nil)
		want := RenderDocument(fakeLines{short, "x"}, vp, &th, nil)
		if got != want {
			t.Fatalf("wrap %v:\n%s\nwant\n%s", wrap != nil, got, want)
		}
	}

	if n := len((Viewport{TabWidth: 4, MaxCol: 30}).LineGlyphs(long, 0, false)); n > 31 {
		t.Fatalf("laid out %d glyphs for 30 columns", n)
	}
}

// TestPickerNoteKeepsRowBackground draws the note of a selected row on
// the selection, not on the background of inlay hints.
func TestPickerNoteKeepsRowBackground(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	th.TextFocus = theme.Style{Bg: color.RGBA{R: 10, G: 20, B: 30, A: 255}}
	th.TextInactive = theme.Style{Fg: color.RGBA{R: 150, G: 150, B: 150, A: 255}}
	th.InlayHint = theme.Style{Fg: color.RGBA{R: 50, A: 255}, Bg: color.RGBA{R: 90, G: 90, B: 90, A: 255}}
	row := renderPickerRow(PickerRow{Text: "current/", Note: "-> releases/1", Selected: true, Dir: true}, 40, &th, theme.Style{})
	if strings.Contains(row, "48;2;90;90;90") {
		t.Fatalf("the note takes the inlay hint background: %q", row)
	}

	if !strings.Contains(row, "38;2;150;150;150") || strings.Count(row, "48;2;10;20;30") == 0 {
		t.Fatalf("the note is not dimmed on the selection: %q", row)
	}

	if ansi.Strip(row) != "> current/ -> releases/1"+strings.Repeat(" ", 16) {
		t.Fatalf("row %q", ansi.Strip(row))
	}
}

// TestPickerRowTruncatesPathStart keeps the file name of a long path and
// the match highlight on it.
func TestPickerRowTruncatesPathStart(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	text := "deeply/nested/directory/structure/target_file.go"
	name := strings.Index(text, "target")
	row := renderPickerRow(PickerRow{Text: text, Positions: []int{0, name}, TruncateStart: true}, 24, &th, theme.Style{})
	if got := ansi.Strip(row); got != "  …ucture/target_file.go" {
		t.Fatalf("row %q", got)
	}

	plain := renderPickerRow(PickerRow{Text: text}, 24, &th, theme.Style{})
	if got := ansi.Strip(plain); !strings.HasPrefix(got, "  deeply/") {
		t.Fatalf("other lists keep the start: %q", got)
	}
}
