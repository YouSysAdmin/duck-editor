package editor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

func TestSplitsAndWindows(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc\ndef")
	e.Resize(80, 24)
	first := e.Focus

	e.feed(t, "<C-w>v")
	if e.Tree.Len() != 2 || e.Focus == first {
		t.Fatalf("vsplit: views %d focus %d", e.Tree.Len(), e.Focus)
	}

	second := e.Focus
	if e.View().Area.X == 0 || e.View().Area.H != 24 {
		t.Fatalf("second view area %+v", e.View().Area)
	}

	// Both views show the same document and the cursor carried over.
	if e.View().DocID != e.Views[first].DocID {
		t.Fatal("split should show the same document")
	}

	e.feed(t, "j")
	if e.Doc().SelectionFor(first).Primary().Head != 1 {
		t.Fatal("other view selection must not move")
	}

	e.feed(t, "<C-w>h")
	if e.Focus != first {
		t.Fatalf("jump left focus %d want %d", e.Focus, first)
	}

	e.feed(t, "<C-w>w")
	if e.Focus != second {
		t.Fatalf("rotate focus %d want %d", e.Focus, second)
	}

	e.feed(t, "<C-w>s")
	if e.Tree.Len() != 3 {
		t.Fatal("hsplit")
	}

	third := e.Focus
	if a, b := e.Views[second].Area, e.Views[third].Area; b.Y <= a.Y || a.X != b.X {
		t.Fatalf("hsplit areas %+v %+v", a, b)
	}

	e.feed(t, "<C-w>q")
	if e.Tree.Len() != 2 || e.ShouldQuit() {
		t.Fatal("wclose")
	}

	e.feed(t, "<C-w>o")
	if e.Tree.Len() != 1 || e.ShouldQuit() {
		t.Fatal("wonly")
	}

	e.feed(t, ":q<ret>")
	if !e.ShouldQuit() {
		t.Fatal("last view quits")
	}
}

func TestBuffers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("aaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(b, []byte("bbb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[s|]#cratch")
	e.feed(t, ":o "+a+"<ret>")
	if e.Doc().Path() != a || len(e.Docs) != 2 {
		t.Fatalf("open: path %q docs %d", e.Doc().Path(), len(e.Docs))
	}

	e.feed(t, ":o "+b+"<ret>")
	if len(e.Docs) != 3 || e.Doc().Path() != b {
		t.Fatal("second open")
	}

	e.feed(t, ":o "+a+"<ret>")
	if len(e.Docs) != 3 || e.Doc().Path() != a {
		t.Fatal("reopen must reuse the buffer")
	}

	e.feed(t, "ga")
	if e.Doc().Path() != b {
		t.Fatalf("ga = %q", e.Doc().Path())
	}

	e.feed(t, "gn")
	if e.Doc().Path() != "test.txt" {
		t.Fatalf("gn wraps to the first buffer, got %q", e.Doc().Path())
	}

	e.feed(t, "gp")
	if e.Doc().Path() != b {
		t.Fatalf("gp = %q", e.Doc().Path())
	}

	// Modified buffers refuse to close.
	e.feed(t, "ix<esc>:bc<ret>")
	if !e.Status().IsError || len(e.Docs) != 3 {
		t.Fatalf("bc on modified: %+v", e.Status())
	}

	e.feed(t, ":bc!<ret>")
	// The view falls back to the buffer accessed before b.
	if len(e.Docs) != 2 || e.Doc().Path() != "test.txt" {
		t.Fatalf("bc! docs %d path %q", len(e.Docs), e.Doc().Path())
	}

	e.feed(t, ":bco<ret>")
	if len(e.Docs) != 1 {
		t.Fatal("bco")
	}

	e.feed(t, ":bc<ret>")
	if len(e.Docs) != 1 || e.Doc().Path() != "" || e.ShouldQuit() {
		t.Fatal("closing the last buffer leaves a scratch buffer")
	}

	// :q with an unsaved buffer elsewhere refuses and switches to it.
	e.feed(t, ":o "+a+"<ret>ix<esc>:o "+b+"<ret>:q<ret>")
	if e.ShouldQuit() || e.Doc().Path() != a || !e.Status().IsError {
		t.Fatalf("q with unsaved: quit=%v path=%q status=%+v", e.ShouldQuit(), e.Doc().Path(), e.Status())
	}

	e.feed(t, ":qa!<ret>")
	if !e.ShouldQuit() {
		t.Fatal("qa!")
	}
}

func TestJumplist(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[l|]#1\nl2\nl3\nl4\nl5")
	e.feed(t, "<C-s>3G<C-s>5G")
	line := func() int { return e.Doc().Selection().Primary().CursorLine(e.Doc().Text()) }
	if line() != 4 {
		t.Fatalf("at %d", line())
	}

	e.feed(t, "<C-o>")
	if line() != 2 {
		t.Fatalf("jump back = %d", line())
	}

	e.feed(t, "<C-o>")
	if line() != 0 {
		t.Fatalf("jump back twice = %d", line())
	}

	e.feed(t, "<C-i>")
	if line() != 2 {
		t.Fatalf("jump forward = %d", line())
	}

	e.feed(t, "<C-i>")
	if line() != 4 {
		t.Fatalf("jump forward to saved end = %d", line())
	}

	// Edits above a jump shift it.
	e.feed(t, "ggOnew<esc><C-o><C-o>")
	if got := e.Doc().Line(line()); got != "l3" {
		t.Fatalf("after insert jump landed on %q", got)
	}
}

func TestPicker(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	items := []PickerItem{{Label: "cmd/ce/main.go", Line: -1}, {Label: "internal/editor/model.go", Line: -1}, {Label: "README.md", Line: -1}}
	var picked []string
	p := NewPicker("Test", items, func(cx *Context, item PickerItem, action PickerAction) {
		picked = append(picked, item.Label)
	})
	e.openPicker(p)
	if e.Picker == nil || len(p.Matches()) != 3 {
		t.Fatal("picker open")
	}

	e.feed(t, "edmd")
	if len(p.Matches()) == 0 || p.Items()[p.Matches()[0].Index].Label != "internal/editor/model.go" {
		t.Fatalf("filter: %v", p.Matches())
	}

	e.feed(t, "<C-u>")
	e.HandleKey(keymap.Ctrl('w'))
	if p.Query() != "" || len(p.Matches()) != 3 {
		t.Fatalf("clear query: %q %d", p.Query(), len(p.Matches()))
	}

	// A changed query selects its best match, the first row.
	if sel, _ := p.Selected(); sel.Label != "cmd/ce/main.go" {
		t.Fatalf("selection after a new query = %q", sel.Label)
	}

	e.feed(t, "<C-n><C-n>")
	if sel, _ := p.Selected(); sel.Label != "README.md" {
		t.Fatalf("move down = %q", sel.Label)
	}

	e.feed(t, "<C-n>")
	if sel, _ := p.Selected(); sel.Label != "cmd/ce/main.go" {
		t.Fatalf("wrap = %q", sel.Label)
	}

	e.feed(t, "<ret>")
	if e.Picker != nil || len(picked) != 1 || picked[0] != "cmd/ce/main.go" {
		t.Fatalf("accept: picker=%v picked=%v", e.Picker, picked)
	}

	e.openPicker(p)
	e.feed(t, "<esc>")
	if e.Picker != nil {
		t.Fatal("esc closes")
	}

	e.feed(t, "<space>'")
	if e.Picker != p {
		t.Fatal("last picker reopens")
	}

	e.feed(t, "<esc>")
}

func TestFilePickerLoads(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"one.go", "two.go", "sub/three.txt"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[a|]#")
	cx := &Context{Editor: e}
	filePicker(cx, dir)
	jobs := e.TakeJobs()
	if len(jobs) != 1 || e.Picker == nil || !e.Picker.Loading {
		t.Fatalf("job queued %d picker %v", len(jobs), e.Picker)
	}

	jobs[0].Run(jobs[0].Ctx, func(r JobResult) { e.ApplyResult(r) })
	if e.Picker.Loading || len(e.Picker.Items()) != 3 {
		t.Fatalf("loaded %d items loading=%v", len(e.Picker.Items()), e.Picker.Loading)
	}

	e.feed(t, "three<ret>")
	if e.Doc().Path() != filepath.Join(dir, "sub/three.txt") {
		t.Fatalf("opened %q", e.Doc().Path())
	}
}

// TestPickerQAFixes covers last_picker on a loading picker, C-x on the
// last row, pasted control chars and bounded preview scrolling.
func TestPickerQAFixes(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	p := NewPicker("Files", nil, nil)
	p.load = func(_ context.Context, gen int, emit func(JobResult)) {
		emit(p.emitItems(gen, []PickerItem{{Label: "a", Line: -1}, {Label: "b", Line: -1}}))
	}
	e.openPicker(p)
	runJobs(e)
	e.feed(t, "<C-n><esc>")
	// Reopening empties the list while the selection pointed into it.
	e.openPicker(e.LastPicker)
	runJobs(e)
	if len(p.Matches()) != 2 {
		t.Fatalf("rerun matches %d", len(p.Matches()))
	}

	e.feed(t, "<esc>")
	removed := 0
	b := NewPicker("Buffers", []PickerItem{{Label: "x", DocID: 1}, {Label: "y", DocID: 2}, {Label: "z", DocID: 3}}, nil)
	b.remove = func(*Context, PickerItem) bool { removed++; return true }

	e.openPicker(b)
	e.feed(t, "<end><C-x><C-x><C-x><C-x>")
	if removed != 3 || len(b.Matches()) != 0 {
		t.Fatalf("removed %d, left %d", removed, len(b.Matches()))
	}

	e.feed(t, "<esc>")
	q := NewPicker("Q", []PickerItem{{Label: "main go", Line: -1}}, nil)
	e.openPicker(q)
	q.insert("main\tgo\nx")
	if q.Query() != "main go x" {
		t.Fatalf("pasted query %q", q.Query())
	}

	e.feed(t, "<esc>")
	lines := make([]string, 30)
	s := NewPicker("S", []PickerItem{{Label: "t", Text: lines, Line: -1}}, nil)
	e.openPicker(s)
	for range 20 {
		s.handleKey(&Context{Editor: e}, keymap.Alt('d'), 10)
	}

	if got := s.PreviewScroll(); got != 29+5 {
		t.Fatalf("scrolled to %d, past the end", got)
	}

	for range 20 {
		s.handleKey(&Context{Editor: e}, keymap.Alt('u'), 10)
	}

	if got := s.PreviewScroll(); got != 5 {
		t.Fatalf("scrolled back to %d", got)
	}
}

// TestFilePickerShowAll lists hidden and ignored files after C-i and
// keeps the filter text.
func TestFilePickerShowAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, text := range map[string]string{".git/HEAD": "", ".gitignore": "*.log\n", "a.go": "", "a.log": "", ".env": ""} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[a|]#")
	filePicker(&Context{Editor: e}, dir)
	runJobs(e)
	title := e.Picker.Title
	if n := len(e.Picker.Items()); n != 1 {
		t.Fatalf("filtered list has %d items", n)
	}

	e.feed(t, "a<C-i>")
	runJobs(e)
	if n, m := len(e.Picker.Items()), len(e.Picker.Matches()); n != 4 || m != 2 || !strings.HasSuffix(e.Picker.Title, "(all)") {
		t.Fatalf("all lists %d items, %d match, title %q", n, m, e.Picker.Title)
	}

	e.feed(t, "<C-i>")
	runJobs(e)
	if n := len(e.Picker.Items()); n != 1 || e.Picker.Title != title {
		t.Fatalf("filtered again %d items, title %q", n, e.Picker.Title)
	}
}

// TestOverlayKeysFromConfig runs picker and prompt keys as the config
// binds them.
func TestOverlayKeysFromConfig(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	cfg, err := config.Parse("keys:\n  picker:\n    A-j: picker_next\n    C-n: no_op\n  prompt:\n    C-g: prompt_close\n")
	if err != nil {
		t.Fatal(err)
	}

	e.Keymaps = keymap.New(cfg.Keys)
	e.openPicker(NewPicker("Items", []PickerItem{{Label: "a"}, {Label: "b"}, {Label: "c"}}, nil))
	e.feed(t, "<C-n><A-j>")
	if item, _ := e.Picker.Selected(); item.Label != "b" {
		t.Fatalf("selected %q", item.Label)
	}

	e.feed(t, "<esc>:<C-g>")
	if e.Prompt != nil {
		t.Fatal("C-g left the command line open")
	}
}
