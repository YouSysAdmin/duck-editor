package app

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

func newSized(t *testing.T, text string, width, height int) Model {
	t.Helper()

	cfg := config.Default()
	cfg.Editor.EditorConfig = false
	m := New(editor.New(editor.NewDocument("test.txt", text), cfg))
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return next.(Model)
}

// plainLines returns the rendered rows without styles. The canvas trims
// trailing blanks, so rows are padded back to the model width.
func plainLines(m Model) []string {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, l := range lines {
		if w := ansi.StringWidth(l); w < m.width {
			lines[i] = l + strings.Repeat(" ", m.width-w)
		}
	}

	for len(lines) < m.height {
		lines = append(lines, strings.Repeat(" ", m.width))
	}

	return lines
}

func press(t *testing.T, m Model, keys ...tea.Key) Model {
	t.Helper()

	for _, k := range keys {
		next, _ := m.Update(tea.KeyPressMsg(k))
		m = next.(Model)
	}

	return m
}

// typeText presses one key per char of s.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()

	for _, r := range s {
		m = press(t, m, tea.Key{Code: r, Text: string(r)})
	}

	return m
}

func TestViewLayout(t *testing.T) {
	t.Parallel()

	m := newSized(t, "first\n\tsecond\nthird\n", 30, 6)
	lines := plainLines(m)

	want := []string{
		"    1  first                  ",
		"    2      second             ",
		"    3  third                  ",
		"    4                         ",
	}
	if len(lines) != 6 {
		t.Fatalf("got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}

	if !strings.HasPrefix(lines[4], " NOR   test.txt") || !strings.HasSuffix(lines[4], " 1:1 ") || strings.Contains(lines[4], "sel") {
		t.Errorf("statusline = %q", lines[4])
	}

	if got := ansi.StringWidth(lines[4]); got != 30 {
		t.Errorf("statusline width = %d, want 30", got)
	}

	if strings.TrimSpace(lines[5]) != "" {
		t.Errorf("command line = %q", lines[5])
	}
}

func TestCursorAndModes(t *testing.T) {
	t.Parallel()

	m := newSized(t, "\tab", 20, 4)
	m = press(t, m, tea.Key{Code: 'l', Text: "l"})
	cur := m.View().Cursor
	if cur == nil || cur.X != 7+4 || cur.Y != 0 {
		t.Fatalf("cursor = %+v, want (11,0)", cur)
	}

	m = press(t, m, tea.Key{Code: 'i', Text: "i"})
	if !strings.Contains(plainLines(m)[2], " INS ") {
		t.Fatalf("statusline = %q", plainLines(m)[2])
	}

	if m.View().Cursor.Shape != tea.CursorBar {
		t.Fatal("insert mode should show a bar cursor")
	}

	m = press(t, m, tea.Key{Code: tea.KeyEscape}, tea.Key{Code: ':', Text: ":"}, tea.Key{Code: 'q', Text: "q"})
	if !strings.HasPrefix(plainLines(m)[3], ":q") {
		t.Fatalf("command line = %q", plainLines(m)[3])
	}

	if m.View().Cursor.Y != 3 {
		t.Fatalf("prompt cursor row = %d", m.View().Cursor.Y)
	}

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal(":q should quit")
	}
}

func TestToKey(t *testing.T) {
	t.Parallel()

	cases := map[string]tea.Key{
		"A":     {Code: 'a', ShiftedCode: 'A', Text: "A", Mod: tea.ModShift},
		"C-x":   {Code: 'x', Mod: tea.ModCtrl},
		"A-ret": {Code: tea.KeyEnter, Mod: tea.ModAlt},
		"space": {Code: tea.KeySpace, Text: " "},
		"esc":   {Code: tea.KeyEscape},
		"F2":    {Code: tea.KeyF2},
		"?":     {Code: '/', ShiftedCode: '?', Text: "?", Mod: tea.ModShift},
	}
	for want, in := range cases {
		if got := toKey(tea.KeyPressMsg(in)).String(); got != want {
			t.Errorf("toKey(%+v) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayOptions(t *testing.T) {
	t.Parallel()

	m := newSized(t, "a\n\tb c\nd\ne\n", 40, 8)
	cfg := &m.Editor().Config.Editor
	cfg.LineNumber = "relative"
	cfg.Cursorline = true
	cfg.Cursorcolumn = true
	cfg.Rulers = []int{20}
	cfg.Whitespace.Render = "all"
	cfg.IndentGuides.Render = true
	cfg.Bufferline = "always"
	cfg.Statusline.Right = []string{"position-percentage", "total-line-numbers", "file-type", "separator"}
	m = press(t, m, tea.Key{Code: 'j', Text: "j"})
	lines := plainLines(m)
	// Row 0 is the bufferline, the document starts below it.
	if !strings.Contains(lines[0], "test.txt") {
		t.Fatalf("bufferline = %q", lines[0])
	}

	// Indent guides draw over the rendered tab.
	if !strings.HasPrefix(lines[1], "    1  a") || !strings.HasPrefix(lines[2], "    2  │   b·c") || !strings.HasPrefix(lines[3], "    1  d") {
		t.Fatalf("relative numbers and whitespace:\n%s", strings.Join(lines, "\n"))
	}

	if !strings.HasSuffix(strings.TrimRight(lines[1], " "), "⏎") {
		t.Fatalf("newline glyph missing: %q", lines[1])
	}

	status := lines[len(lines)-2]
	if !strings.Contains(status, "40%") || !strings.Contains(status, "text") || !strings.Contains(status, "│") {
		t.Fatalf("statusline = %q", status)
	}

	cur := m.View().Cursor
	if cur == nil || cur.Y != 2 {
		t.Fatalf("cursor row with bufferline = %+v", cur)
	}

	// Cursor shapes follow the config.
	cfg.CursorShape = map[string]string{"normal": "underline"}
	if m.View().Cursor.Shape != tea.CursorUnderline {
		t.Fatalf("cursor shape = %v", m.View().Cursor.Shape)
	}

	// A custom gutter layout without line numbers.
	m = typeText(t, m, ":set gutters [diagnostics, diff]")
	m = press(t, m, tea.Key{Code: tea.KeyEnter})
	lines = plainLines(m)
	if !strings.HasPrefix(lines[1], "  a") {
		t.Fatalf("custom gutters = %q", lines[1])
	}
}

func TestPopupBorder(t *testing.T) {
	t.Parallel()

	m := newSized(t, "hello\n", 40, 10)
	m.Editor().Config.Editor.PopupBorder = "all"
	m.Editor().Popup = &editor.Popup{Lines: []string{"doc text"}}
	lines := plainLines(m)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "┌") || !strings.Contains(joined, "doc text") {
		t.Fatalf("bordered popup missing:\n%s", joined)
	}

	// The frame starts below the cursor line instead of covering it.
	if !strings.Contains(lines[0], "hello") || !strings.Contains(lines[1], "┌") {
		t.Fatalf("frame covers the cursor line:\n%s", joined)
	}

	m.Editor().Config.Editor.PopupBorder = "menu"
	if strings.Contains(strings.Join(plainLines(m), "\n"), "┌") {
		t.Fatal("popup should have no border when only menus are bordered")
	}
}

func TestSoftWrapCursor(t *testing.T) {
	t.Parallel()

	m := newSized(t, "aaaa bbbb cccc dddd\nz\n", 20, 10)
	on := true
	m.Editor().Config.Editor.SoftWrap.Enable = &on
	m.Editor().Config.Editor.SoftWrap.WrapIndicator = ">"
	// Gutter is 7 wide, leaving 13 cells: the line wraps after "aaaa bbbb ".
	lines := plainLines(m)
	if !strings.HasPrefix(lines[0], "    1  aaaa bbbb") || !strings.HasPrefix(lines[1], "       >cccc dddd") || !strings.HasPrefix(lines[2], "    2  z") {
		t.Fatalf("wrapped rows:\n%s", strings.Join(lines, "\n"))
	}

	m = press(t, m, tea.Key{Code: 'e', Text: "e"}, tea.Key{Code: 'e', Text: "e"}, tea.Key{Code: 'e', Text: "e"})
	cur := m.View().Cursor
	if cur == nil || cur.Y != 1 {
		t.Fatalf("cursor on wrapped row = %+v", cur)
	}

	// The second document line is shown after the wrapped rows.
	m = press(t, m, tea.Key{Code: 'j', Text: "j"})
	if cur := m.View().Cursor; cur == nil || cur.Y != 2 {
		t.Fatalf("cursor after wrapped line = %+v", cur)
	}
}

func TestSoftWrapRowScrollAndVisualMoves(t *testing.T) {
	t.Parallel()

	// 13 text cells, every "aaaa bbbb cccc dddd" line wraps into two rows.
	long := "aaaa bbbb cccc dddd"
	m := newSized(t, strings.Repeat(long+"\n", 6), 20, 6)
	on := true
	m.Editor().Config.Editor.SoftWrap.Enable = &on
	m.Editor().Config.Editor.Scrolloff = 0
	// j moves by visual rows: three presses land on the second row of line 2.
	m = press(t, m, tea.Key{Code: 'j', Text: "j"}, tea.Key{Code: 'j', Text: "j"}, tea.Key{Code: 'j', Text: "j"})
	ed := m.Editor()
	text := ed.Doc().Text()
	cursor := ed.Doc().Selection().Primary().Cursor(text)
	if line := text.CharToLine(cursor); line != 1 || cursor-text.LineToChar(1) < 10 {
		t.Fatalf("after 3 visual moves: line %d col %d", line, cursor-text.LineToChar(1))
	}

	if cur := m.View().Cursor; cur == nil || cur.Y != 3 {
		t.Fatalf("cursor row = %+v", cur)
	}

	// One more row no longer fits: the view scrolls by a single row and
	// the top line shows only its second half.
	m = press(t, m, tea.Key{Code: 'j', Text: "j"})
	v := ed.View()
	if v.FirstLine != 0 || v.FirstRow != 1 {
		t.Fatalf("scroll by rows: first line %d row %d", v.FirstLine, v.FirstRow)
	}

	lines := plainLines(m)
	if !strings.Contains(lines[0], "cccc dddd") || strings.Contains(lines[0], "aaaa") {
		t.Fatalf("top row should be the tail of line 1: %q", lines[0])
	}

	if cur := m.View().Cursor; cur == nil || cur.Y != 3 {
		t.Fatalf("cursor after row scroll = %+v", cur)
	}

	// k walks back up through the rows and scrolls the tail back in.
	press(t, m, tea.Key{Code: 'k', Text: "k"}, tea.Key{Code: 'k', Text: "k"}, tea.Key{Code: 'k', Text: "k"}, tea.Key{Code: 'k', Text: "k"})
	if v.FirstLine != 0 || v.FirstRow != 0 || ed.Doc().Selection().Primary().Cursor(ed.Doc().Text()) != 0 {
		t.Fatalf("back at top: line %d row %d cursor %d", v.FirstLine, v.FirstRow, ed.Doc().Selection().Primary().Cursor(ed.Doc().Text()))
	}
}

func TestUniqueNames(t *testing.T) {
	t.Parallel()

	got := uniqueNames([]string{"/p/a/main.go", "/p/b/main.go", "/p/util.go", "", "/q/x/c/lsp.go", "/q/y/c/lsp.go"})
	want := []string{"a/main.go", "b/main.go", "util.go", "[scratch]", "x/c/lsp.go", "y/c/lsp.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("names = %q", got)
	}
}

func TestDiagnosticFrame(t *testing.T) {
	t.Parallel()

	frame := func(text string, from, width int) (string, string) {
		t.Helper()

		doc := editor.NewDocument("", text)
		src, caret, ok := diagnosticFrame(doc, editor.Diagnostic{Range: edit.NewRange(from, from)}, width)
		if !ok {
			t.Fatalf("no frame for %q", text)
		}

		return src, caret
	}

	// The indent goes, the caret stays under the char.
	if src, caret := frame("    key: \"value\n", 9, 40); src != "key: \"value" || caret != "     ^" {
		t.Fatalf("indented = %q %q", src, caret)
	}

	// Tabs count as their width.
	if src, caret := frame("\tx\ty\n", 3, 40); src != "x   y" || caret != "    ^" {
		t.Fatalf("tabs = %q %q", src, caret)
	}

	// The end of the line points past the last char.
	if src, caret := frame("ab\n", 2, 40); src != "ab" || caret != "  ^" {
		t.Fatalf("end of line = %q %q", src, caret)
	}

	// A long line is cut around the caret.
	long := strings.Repeat("a", 50) + "X" + strings.Repeat("b", 50) + "\n"
	src, caret := frame(long, 50, 20)
	if !strings.HasPrefix(src, "…") || !strings.HasSuffix(src, "…") || []rune(src)[len([]rune(caret))-1] != 'X' {
		t.Fatalf("long = %q %q", src, caret)
	}
}

// TestVisibleRanges joins short lines and cuts long ones to the limit.
func TestVisibleRanges(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("ab\n" + strings.Repeat("x", 100) + "\ncd\nef\n")
	got := visibleRanges(text, 0, 4, 10)
	want := [][2]int{{0, 13}, {104, 110}}
	if !slices.Equal(got, want) {
		t.Fatalf("ranges %v want %v", got, want)
	}
}

// TestJobPanicBecomesError keeps the editor running when a background
// job panics.
func TestJobPanicBecomesError(t *testing.T) {
	t.Parallel()

	m := newSized(t, "x", 40, 10)
	ch := make(chan editor.JobResult, 1)
	runJob(editor.QueuedJob{Ctx: context.Background(), Run: func(context.Context, func(editor.JobResult)) {
		panic("boom")
	}}, ch)

	r, ok := <-ch
	if !ok {
		t.Fatal("no result from a panicking job")
	}

	m.editor.ApplyResult(r)
	if st := m.editor.Status(); !st.IsError || !strings.Contains(st.Text, "boom") {
		t.Fatalf("status %+v", st)
	}

	if _, open := <-ch; open {
		t.Fatal("the channel stays open")
	}
}

func TestWrapLines(t *testing.T) {
	t.Parallel()

	got := wrapLines([]string{"one two three four", "", "x"}, 10)
	want := []string{" one two", " three", " four", " ", " x"}
	if !slices.Equal(got, want) {
		t.Fatalf("wrapped %q want %q", got, want)
	}

	for _, l := range got {
		if ansi.StringWidth(l) > 10 {
			t.Fatalf("%q is wider than 10", l)
		}
	}
}

// TestDirectoryPreviewBackground draws a directory listing on the editor
// background, like file text, not on the picker background.
func TestDirectoryPreviewBackground(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newSized(t, "x", 80, 20)
	if m.theme.Base().Bg == nil {
		t.Skip("the theme has no background")
	}

	path := filepath.Join(dir, "sub")
	p := editor.NewPicker("t", []editor.PickerItem{{Label: "sub/", Path: path, Line: -1, Dir: true}}, nil)
	m.editor.PreparePreview(path, false)
	for _, job := range m.editor.TakeJobs() {
		ch := make(chan editor.JobResult, 4)
		runJob(job, ch)
		for r := range ch {
			m.editor.ApplyResult(r)
		}
	}

	lines, _ := m.renderPreview(p, 40, 10)
	want := m.theme.Render(m.previewStyle(m.theme.Text.Patch(m.theme.TextDirectory)), "inner/")
	if len(lines) == 0 || lines[0] != want || !strings.Contains(lines[0], "48;") {
		t.Fatalf("lines %q, want %q", lines, want)
	}
}

// TestSignaturePlacement keeps the signature popup off the cursor row,
// on screen and without blank rows, at the top, middle and bottom of
// the screen, framed or not.
func TestSignaturePlacement(t *testing.T) {
	t.Parallel()

	var text strings.Builder
	for i := range 30 {
		fmt.Fprintf(&text, "line%02d fmt.Println(x)\n", i)
	}

	docs := "Println formats using the default formats for its operands and writes to standard output. Spaces are always added between operands and a newline is appended."
	for _, border := range []string{"all", "none"} {
		for _, line := range []int{0, 1, 4, 10, 18} {
			m := newSized(t, text.String(), 60, 20)
			ed := m.Editor()
			ed.Config.Editor.PopupBorder = border
			ed.Doc().SetSelection(edit.PointSelection(ed.Doc().Text().LineToChar(line) + 19))
			ed.Mode = editor.Insert
			ed.EnsureCursorInView()
			ed.SignatureHelp = &editor.SignatureHelp{Label: "Println(a ...any) (n int, err error)", ParamStart: 8, ParamEnd: 16, Docs: []string{docs}}
			lines := plainLines(m)
			want := fmt.Sprintf("line%02d fmt.Println(x)", line)
			row := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, want) })
			if row < 0 {
				t.Fatalf("border %s line %d: the cursor row is covered:\n%s", border, line, strings.Join(lines, "\n"))
			}

			joined := strings.Join(lines, "\n")
			if !strings.Contains(joined, "Println(a ...any)") {
				t.Fatalf("border %s line %d: no signature:\n%s", border, line, joined)
			}

			if line > 2 && !strings.Contains(joined, "newline is appended.") {
				t.Fatalf("border %s line %d: docs are cut instead of wrapped:\n%s", border, line, joined)
			}

			if strings.Contains(joined, "…") {
				t.Fatalf("border %s line %d: truncated text:\n%s", border, line, joined)
			}

			if border == "all" {
				top := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "┌") })
				bottom := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "└") })
				if top < 0 || bottom < 0 {
					t.Fatalf("line %d: frame incomplete:\n%s", line, joined)
				}

				// The last row inside the frame holds text, no blank rows.
				if strings.TrimSpace(strings.Trim(strings.TrimSpace(lines[bottom-1][strings.Index(lines[bottom-1], "│"):]), "│")) == "" {
					t.Fatalf("line %d: blank row at the bottom of the frame:\n%s", line, joined)
				}
			}
		}
	}
}

// sgrRe matches a select graphic rendition sequence.
var sgrRe = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// cellsWithBg lists the row and column of every cell drawn with the RGB
// background bg, such as "1;2;3".
func cellsWithBg(view, bg string) [][2]int {
	var out [][2]int
	cur := ""
	for row, line := range strings.Split(view, "\n") {
		col := 0
		for len(line) > 0 {
			if loc := sgrRe.FindStringSubmatchIndex(line); loc != nil && loc[0] == 0 {
				codes := strings.Split(line[loc[2]:loc[3]], ";")
				for i := 0; i < len(codes); i++ {
					switch {
					case codes[i] == "" || codes[i] == "0" || codes[i] == "49":
						cur = ""
					case codes[i] == "48" && i+4 < len(codes) && codes[i+1] == "2":
						cur = strings.Join(codes[i+2:i+5], ";")
						i += 4
					case (codes[i] == "38" || codes[i] == "58") && i+1 < len(codes) && codes[i+1] == "2":
						i += 4
					case (codes[i] == "38" || codes[i] == "48" || codes[i] == "58") && i+1 < len(codes) && codes[i+1] == "5":
						if codes[i] == "48" {
							cur = "idx" + codes[i+2]
						}

						i += 2
					}
				}

				line = line[loc[1]:]

				continue
			}

			r, size := utf8.DecodeRuneInString(line)
			if cur == bg {
				out = append(out, [2]int{row, col})
			}

			col += max(ansi.StringWidth(string(r)), 1)
			line = line[size:]
		}
	}

	return out
}

// TestSignatureBackgroundInsideFrame paints the popup background inside
// the frame only, no band beside or below it.
func TestSignatureBackgroundInsideFrame(t *testing.T) {
	t.Parallel()

	m := newSized(t, "package main\n\nfunc main() {\n\tfmt.Println\n}\n", 100, 12)
	m.theme.Popup = theme.Style{Bg: color.RGBA{R: 1, G: 2, B: 3, A: 255}}
	ed := m.Editor()
	ed.Config.Editor.PopupBorder = "all"
	ed.Doc().SetSelection(edit.PointSelection(ed.Doc().Text().LineToChar(3) + 12))
	ed.Mode = editor.Insert
	ed.SignatureHelp = &editor.SignatureHelp{Label: "Println(a ...any) (n int, err error)", ParamStart: 8, ParamEnd: 16,
		Docs: []string{"Println formats using the default formats for its operands and writes to standard output."}}
	view := m.View().Content
	lines := strings.Split(ansi.Strip(view), "\n")
	top := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "┌") })
	bottom := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "└") })
	if top < 0 || bottom < 0 {
		t.Fatalf("no frame:\n%s", strings.Join(lines, "\n"))
	}

	left := ansi.StringWidth(lines[top][:strings.Index(lines[top], "┌")])
	right := ansi.StringWidth(lines[top][:strings.Index(lines[top], "┐")])
	cells := cellsWithBg(view, "1;2;3")
	if len(cells) == 0 {
		t.Fatal("the popup background is not drawn")
	}

	for _, c := range cells {
		if c[0] < top || c[0] > bottom || c[1] < left || c[1] > right {
			t.Fatalf("popup background at row %d col %d outside the frame rows %d-%d cols %d-%d:\n%s", c[0], c[1], top, bottom, left, right, strings.Join(lines, "\n"))
		}
	}

	// The active parameter keeps the popup background.
	for row := top; row <= bottom; row++ {
		if strings.Contains(lines[row], "a ...any") {
			col := ansi.StringWidth(lines[row][:strings.Index(lines[row], "a ...any")])
			if !slices.Contains(cells, [2]int{row, col}) {
				t.Fatalf("the active parameter has another background")
			}
		}
	}
}

// TestMenuScrollsInShortScreen keeps the selected row of a menu visible
// when the screen leaves fewer rows than the menu would take.
func TestMenuScrollsInShortScreen(t *testing.T) {
	t.Parallel()

	m := newSized(t, "x\n", 60, 9)
	items := make([]string, 10)
	for i := range items {
		items[i] = fmt.Sprintf("action%02d", i)
	}

	m.Editor().Menu = &editor.Menu{Items: items}
	for range 8 {
		m = press(t, m, tea.Key{Code: 'n', Mod: tea.ModCtrl})
	}

	if got := strings.Join(plainLines(m), "\n"); !strings.Contains(got, "action08") {
		t.Fatalf("the selected row is not shown:\n%s", got)
	}
}

// TestMultiLineStatus shows every line of a message, such as the error
// output of :sh, above the command line.
func TestMultiLineStatus(t *testing.T) {
	t.Parallel()

	m := newSized(t, "x\n", 40, 10)
	m.Editor().SetError("first line\nsecond line\nexit status 2")
	lines := plainLines(m)
	got := strings.Join(lines[len(lines)-3:], "\n")
	for _, want := range []string{"first line", "second line", "exit status 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

// TestLongPromptScrolls keeps the end of a long command line and the
// cursor on screen.
func TestLongPromptScrolls(t *testing.T) {
	t.Parallel()

	m := newSized(t, "x\n", 30, 8)
	m = typeText(t, m, ":echo "+strings.Repeat("a", 40)+"END")
	lines := plainLines(m)
	last := lines[len(lines)-1]
	if !strings.Contains(last, "END") || !strings.HasPrefix(last, "…") {
		t.Fatalf("command line %q", last)
	}

	if c := m.View().Cursor; c == nil || c.X != 29 {
		t.Fatalf("cursor %+v", c)
	}
}

// TestPopupScrolls moves a long popup with C-d and C-u instead of
// closing it.
func TestPopupScrolls(t *testing.T) {
	t.Parallel()

	m := newSized(t, "x\n", 40, 12)
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("doc line %02d", i)
	}

	m.Editor().Popup = &editor.Popup{Lines: lines}
	m = press(t, m, tea.Key{Code: 'd', Mod: tea.ModCtrl})
	got := strings.Join(plainLines(m), "\n")
	if m.Editor().Popup == nil || !strings.Contains(got, "doc line 05") || strings.Contains(got, "doc line 00") {
		t.Fatalf("after C-d:\n%s", got)
	}

	m = press(t, m, tea.Key{Code: 'u', Mod: tea.ModCtrl})
	if got := strings.Join(plainLines(m), "\n"); !strings.Contains(got, "doc line 00") {
		t.Fatalf("after C-u:\n%s", got)
	}
}
