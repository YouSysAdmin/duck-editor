package editor

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/clipboard"
	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
	"github.com/YouSysAdmin/duck-editor/internal/vcs"
)

func TestSnippetTabstops(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[\n|]#")
	e.feed(t, "i")
	e.insertSnippet(e.Doc(), "for ${1:i} := range ${2:n} {\n\t$0\n}", 0, 0)
	if got := e.annotated(); got != "for #[i|]# := range n {\n\t\n}\n" {
		t.Fatalf("after insert = %q", got)
	}

	// Typing replaces the selected placeholder.
	e.feed(t, "k")
	if got := e.annotated(); got != "for k#[| ]#:= range n {\n\t\n}\n" {
		t.Fatalf("after replace = %q", got)
	}

	e.feed(t, "<tab>xs<tab>")
	if got := e.annotated(); got != "for k := range xs {\n\t#[|\n]#}\n" {
		t.Fatalf("after tabs = %q", got)
	}

	if e.Doc().snippet != nil {
		t.Fatal("snippet should end at the final stop")
	}

	// With no snippet active tab inserts indentation.
	e.feed(t, "<tab>")
	if got := e.Doc().Text().String(); got != "for k := range xs {\n\t\t\n}\n" {
		t.Fatalf("plain tab = %q", got)
	}
}

func TestSnippetMultiCursor(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#\n#(b|)#\n")
	e.feed(t, "a")
	e.insertSnippet(e.Doc(), "f($1)", 0, 0)
	if got := e.annotated(); got != "af(#[|)]#\nbf(#(|))#\n" {
		t.Fatalf("multi cursor = %q", got)
	}

	e.feed(t, "x<tab>")
	if got := e.annotated(); got != "af(x)#[|\n]#bf(x)#(|\n)#" {
		t.Fatalf("after x = %q", got)
	}

	e.feed(t, "<esc>")
	if e.Doc().snippet != nil {
		t.Fatal("snippet should be dropped")
	}
}

func TestSignatureHelpOffsets(t *testing.T) {
	t.Parallel()

	one := 1
	help := &lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{{
			Label:         "foo(a int, b string)",
			Documentation: jsontext.Value(`"Does foo."`),
			Parameters: []lsp.ParameterInformation{
				{Label: jsontext.Value(`"a int"`)},
				{Label: jsontext.Value(`"b string"`), Documentation: jsontext.Value(`{"kind":"markdown","value":"the b"}`)},
			},
		}, {
			Label:      "ünï(x)",
			Parameters: []lsp.ParameterInformation{{Label: jsontext.Value(`[4, 5]`)}},
		}},
		ActiveParameter: &one,
	}
	sh := newSignatureHelp(help, 1, true)
	if sh.Label != "foo(a int, b string)" || sh.ParamStart != 11 || sh.ParamEnd != 19 || sh.Count != 2 {
		t.Fatalf("got %+v", sh)
	}

	if len(sh.Docs) != 2 || sh.Docs[0] != "the b" || sh.Docs[1] != "Does foo." {
		t.Fatalf("docs = %q", sh.Docs)
	}

	help.ActiveSignature = &one
	sh = newSignatureHelp(help, 1, false)
	if sh.ParamStart != 4 || sh.ParamEnd != 5 || sh.Docs != nil {
		t.Fatalf("utf16 offsets = %+v", sh)
	}
}

func TestInlayHintMapping(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#b\ncd")
	doc := e.Doc()
	doc.hints.hints = []InlayHint{{Pos: 1, Text: ": int", Kind: lsp.InlayHintType}, {Pos: 3, Text: "x:", Kind: lsp.InlayHintParameter}}
	e.feed(t, "ax<esc>")
	hints := doc.InlayHints()
	if hints[0].Pos != 2 || hints[1].Pos != 4 {
		t.Fatalf("after insert = %+v", hints)
	}

	// Typing at a parameter hint keeps it before the new text.
	doc.SetSelection(edit.PointSelection(4))
	e.feed(t, "iq<esc>")
	if hints := doc.InlayHints(); hints[1].Pos != 4 {
		t.Fatalf("param hint = %+v", hints)
	}
}

func TestGotoChange(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#\nx\nc\nd\n")
	doc := e.Doc()
	doc.diff.base, doc.diff.raw = splitLines("a\nb\nc\n"), "a\nb\nc\n"
	e.scheduleDiff(doc)
	jobs := e.TakeJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d", len(jobs))
	}

	jobs[0].Run(jobs[0].Ctx, func(r JobResult) { e.ApplyResult(r) })
	want := []vcs.Hunk{{AStart: 1, AEnd: 2, BStart: 1, BEnd: 2}, {AStart: 3, AEnd: 3, BStart: 3, BEnd: 4}}
	if got := doc.DiffHunks(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("hunks = %v", got)
	}

	e.feed(t, "]g")
	if got := e.annotated(); got != "a\n#[x|]#\nc\nd\n" {
		t.Fatalf("next change = %q", got)
	}

	e.feed(t, "]g")
	if got := e.annotated(); got != "a\nx\nc\n#[d|]#\n" {
		t.Fatalf("second change = %q", got)
	}

	e.feed(t, "]g")
	if e.Status().Text != "No more changes" {
		t.Fatalf("status = %q", e.Status().Text)
	}

	e.feed(t, "[G")
	if got := e.annotated(); got != "a\n#[x|]#\nc\nd\n" {
		t.Fatalf("first change = %q", got)
	}
}

func TestTypedCommandCompletion(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.feed(t, ":")
	p := e.Prompt
	if p == nil || len(p.Completions()) != len(typedCommands) || p.Selected() != -1 {
		t.Fatalf("prompt = %+v", p)
	}

	e.feed(t, "q")
	if c := p.Completions(); len(c) == 0 || c[0] != "quit" {
		t.Fatalf("completions for q = %v", c)
	}

	e.feed(t, "<tab>")
	if p.Input() != "quit" || p.Selected() != 0 || p.Doc() == "" {
		t.Fatalf("after tab: input %q selected %d doc %q", p.Input(), p.Selected(), p.Doc())
	}

	e.feed(t, "<tab>")
	if p.Input() != "quit!" || p.Selected() != 1 {
		t.Fatalf("after second tab: %q %d", p.Input(), p.Selected())
	}

	e.feed(t, "<S-tab>")
	if p.Input() != "quit" {
		t.Fatalf("after shift tab: %q", p.Input())
	}

	// Typing again refilters from the new text.
	e.feed(t, "-")
	if c := p.Completions(); p.Selected() != -1 || len(c) == 0 || c[0] != "quit-all" {
		t.Fatalf("after typing: %v", c)
	}

	// Path arguments complete directory entries relative to the cwd.
	e.feed(t, "<C-u>o ../")
	found := false
	for _, c := range p.Completions() {
		if c == "../edit/" {
			found = true
		}
	}

	if !found {
		t.Fatalf("path completions = %v", p.Completions())
	}

	e.feed(t, "ed<tab>")
	if p.Input() != "o ../edit/" {
		t.Fatalf("path tab = %q", p.Input())
	}

	// Commands without path arguments offer nothing.
	e.feed(t, "<C-u>earlier x")
	if len(p.Completions()) != 0 {
		t.Fatalf("unexpected completions %v", p.Completions())
	}
}

func TestInfoBox(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.feed(t, " ")
	if e.Info == nil || e.Info.Title != "Space" {
		t.Fatalf("info = %+v", e.Info)
	}

	found := false
	for _, entry := range e.Info.Entries {
		if entry.Desc == "Open changed file picker" && len(entry.Keys) == 1 && entry.Keys[0] == "g" {
			found = true
		}
	}

	if !found {
		t.Fatalf("entries = %+v", e.Info.Entries)
	}

	e.feed(t, "<esc>")
	if e.Info != nil {
		t.Fatal("esc should close the info box")
	}

	// Keys sharing a target are listed together.
	e.feed(t, "g")
	for _, entry := range e.Info.Entries {
		if entry.Desc == "Goto line start" && len(entry.Keys) != 2 {
			t.Fatalf("goto entry = %+v", entry)
		}
	}

	e.feed(t, "g")
	if e.Info != nil {
		t.Fatal("completed sequence should close the info box")
	}

	// Sticky menus keep their box until escape.
	e.feed(t, "Z")
	if e.Info == nil || e.Info.Title != "View" {
		t.Fatalf("sticky info = %+v", e.Info)
	}

	e.feed(t, "j")
	if e.Info == nil {
		t.Fatal("sticky info should stay after a command")
	}

	e.feed(t, "<esc>")
	if e.Info != nil {
		t.Fatal("esc should leave the sticky menu")
	}

	e.Config.Editor.AutoInfo = false
	e.feed(t, " ")
	if e.Info != nil {
		t.Fatal("auto-info false must hide the box")
	}

	e.feed(t, "<esc>")
}

func TestShellCommands(t *testing.T) {
	t.Parallel()

	// Pipe replaces each selection with the command output.
	check(t, "#[b\na\n|]#c\n", "|sort<ret>", "#[a\nb\n|]#c\n")
	// Insert and append run the command once and reuse its output.
	check(t, "#[a|]# #(b|)#", "!echo x<ret>", "#[x|]#a #(x|)#b")
	check(t, "#[a|]# #(b|)#", "<A-!>echo x<ret>", "a#[x|]# b#(x|)#")
	// Pipe-to leaves the text alone.
	check(t, "#[abc|]#", "<A-|>cat <gt> /dev/null<ret>", "#[abc|]#")
	// Keep-pipe drops selections whose command fails.
	check(t, "#[a|]# #(b|)# #(a|)#", "$grep a<ret>", "#[a|]# b #(a|)#")

	e := newTestEditor(t, "#[x|]#")
	e.feed(t, "|false<ret>")
	runJobs(e)
	if !e.Status().IsError {
		t.Fatalf("failed pipe should report an error, got %+v", e.Status())
	}

	if e.Registers['|'][0] != "false" {
		t.Fatalf("register history = %v", e.Registers['|'])
	}
}

func TestExpansion(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "one\nt#[w|]#o\nthree")
	e.Registers['a'] = []string{"ra"}
	cases := map[string]string{
		"%{cursor_line}:%{cursor_column}":       "2:2",
		"%{selection} %{selection_line_start}":  "w 2",
		"%{language} %%{x} %u{41}":              "text %{x} A",
		"%sh{echo %{cursor_line} | tr 2 9}":     "9",
		"%reg{a} %{buffer_name} %{line_ending}": "ra test.txt \n",
		"%sh(printf '[%%s]' \"%{selection}\")":  "[w]",
	}
	for in, want := range cases {
		got, err := e.expand(in)
		if err != nil || got != want {
			t.Errorf("expand(%q) = %q, %v, want %q", in, got, err, want)
		}
	}

	for _, bad := range []string{"%{nope}", "%{cursor_line", "%u{zz}", "%x{1}"} {
		if _, err := e.expand(bad); err == nil {
			t.Errorf("expand(%q) should fail", bad)
		}
	}

	// Typed command arguments expand too, and :sh shows output in a popup.
	e.feed(t, ":sh echo line %{cursor_line}<ret>")
	jobs := e.TakeJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d", len(jobs))
	}

	jobs[0].Run(jobs[0].Ctx, func(r JobResult) { e.ApplyResult(r) })
	if e.Popup == nil || len(e.Popup.Lines) != 1 || e.Popup.Lines[0] != "line 2" {
		t.Fatalf("popup = %+v", e.Popup)
	}
}

func TestClipboardRegisters(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]# #(b|)#\n")
	mem := &clipboard.Memory{}
	e.SetClipboard(mem)
	e.feed(t, " y")
	if mem.Contents[0] != "a\nb" {
		t.Fatalf("clipboard = %q", mem.Contents[0])
	}

	// Pasting back keeps the per selection split while the clipboard is
	// unchanged, then falls back to one value.
	e.feed(t, " p")
	if got := e.annotated(); got != "a#[a|]# b#(b|)#\n" {
		t.Fatalf("paste = %q", got)
	}

	mem.Contents[0] = "zz"
	e.feed(t, "\"+P")
	if got := e.Doc().Text().String(); got != "azza bzzb\n" {
		t.Fatalf("paste changed clipboard = %q", got)
	}

	e.feed(t, ":clipboard-yank-join ,<ret>")
	if mem.Contents[0] != "zz,zz" {
		t.Fatalf("yank join = %q", mem.Contents[0])
	}

	e.feed(t, " Y")
	if mem.Contents[0] != "zz" {
		t.Fatalf("yank main = %q", mem.Contents[0])
	}

	e.feed(t, "%\"+R")
	if got := e.Doc().Text().String(); got != "zz" {
		t.Fatalf("replace = %q", got)
	}

	// The terminal provider queues writes for the UI.
	e.Config.Editor.ClipboardProvider = "termcode"
	e.SetClipboard(nil)
	e.feed(t, " y")
	if writes := e.TakeClipboardWrites(); len(writes) != 1 || writes[0] != "zz" {
		t.Fatalf("osc52 writes = %v", writes)
	}

	e.feed(t, ":show-clipboard-provider<ret>")
	if e.Status().Text != "clipboard provider: termcode" {
		t.Fatalf("status = %q", e.Status().Text)
	}
}

func TestIncrementInteger(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in     string
		amount int64
		want   string
		ok     bool
	}{
		{"1", 1, "2", true}, {"9", 1, "10", true}, {"-1", 1, "0", true}, {"0", -1, "-1", true},
		{"007", 1, "008", true}, {"-007", 7, "000", true}, {"1_000", 1, "1_001", true},
		{"0x0f", 1, "0x10", true}, {"0xFF", 1, "0x100", true}, {"0b101", 1, "0b110", true},
		{"0o17", 1, "0o20", true}, {"0x1", -5, "0x0", true},
		{"999_999", 1, "1_000_000", true}, {"1_000_000", -1, "999_999", true},
		{"-999_999", -1, "-1_000_000", true}, {"0x0000_0000_0001", 0x1_ffff_0000, "0x0001_ffff_0001", true},
		{"0x0000_0000", -1, "0x0000_0000", true}, {"0b01111111_11111111", 1, "0b10000000_00000000", true},
		{"0b11111111_11111111", 1, "0b1_00000000_00000000", true}, {"9_", 1, "", false},
		{"-1_000", 1, "-999", true}, {"-1_000_000", 1, "-999_999", true}, {"-999", -1, "-1000", true},
		{"abc", 1, "", false}, {"_1", 1, "", false}, {"", 1, "", false},
	}
	for _, c := range cases {
		got, ok := incrementInteger(c.in, c.amount)
		if ok != c.ok || got != c.want {
			t.Errorf("incrementInteger(%q, %d) = %q %v, want %q %v", c.in, c.amount, got, ok, c.want, c.ok)
		}
	}

	check(t, "x = #[1|]#", "<C-a>", "x = #[2|]#")
	check(t, "x = #[9|]#", "3<C-a>", "x = #[12|]#")
	check(t, "#[1|]# #(1|)# #(1|)#", "\"#<C-a>", "#[2|]# #(3|)# #(4|)#")
	check(t, "#[10|]#", "<C-x>", "#[9|]#")
}

func TestMacros(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#\nb\nc\n")
	e.feed(t, "QA!<esc>Q")
	if got := e.Registers['@']; len(got) != 1 || got[0] != "A!<esc>" {
		t.Fatalf("macro register = %q", got)
	}

	if e.Status().Text != "Recorded to register [@]" {
		t.Fatalf("status = %q", e.Status().Text)
	}

	e.feed(t, "j2q")
	if got := e.Doc().Text().String(); got != "a!\nb!!\nc\n" {
		t.Fatalf("after replay = %q", got)
	}

	// The replay, twice over, is one undo step.
	e.feed(t, "u")
	if got := e.Doc().Text().String(); got != "a!\nb\nc\n" {
		t.Fatalf("undo of the replay = %q", got)
	}

	e.feed(t, "U")
	if got := e.Doc().Text().String(); got != "a!\nb!!\nc\n" {
		t.Fatalf("redo of the replay = %q", got)
	}

	e.feed(t, "\"zQx<esc>Q")
	if got := e.Registers['z']; len(got) != 1 || got[0] != "x<esc>" {
		t.Fatalf("named register = %q", got)
	}

	if got := macroString(mustKeys(t, "a<C-x><ret><lt>")); got != "a<C-x><ret><lt>" {
		t.Fatalf("macroString = %q", got)
	}
}

func mustKeys(t *testing.T, s string) []keymap.Key {
	t.Helper()
	keys, err := keymap.ParseMacro(s)
	if err != nil {
		t.Fatal(err)
	}

	return keys
}

func TestSelectionCommands(t *testing.T) {
	t.Parallel()

	// Insert register pastes at the cursor in insert mode.
	e := newTestEditor(t, "#[a|]#b")
	e.Registers['r'] = []string{"XY"}
	e.feed(t, "a<C-r>r<esc>")
	if got := e.Doc().Text().String(); got != "aXYb" {
		t.Fatalf("insert register = %q", got)
	}

	// Align pads the second selection on each line to the same column.
	check(t, "a #[=|]# 1\nlonger #(=|)# 2\n", "&", "a      #[=|]# 1\nlonger #(=|)# 2\n")
	// Rotate and reverse contents.
	// The primary selection follows its content around.
	check(t, "#[1|]# #(2|)# #(3|)#", "<A-)>", "#(3|)# #[1|]# #(2|)#")
	check(t, "#[1|]# #(2|)# #(3|)#", "<A-(>", "#(2|)# #(3|)# #[1|]#")
	e = newTestEditor(t, "#[ab|]# #(c|)#")
	e.execute(&Context{Editor: e}, "reverse_selection_contents")
	if got := e.Doc().Text().String(); got != "c ab" {
		t.Fatalf("reverse = %q", got)
	}

	// select_line_below selects the line, then grows downwards.
	e = newTestEditor(t, "a\n#[b|]#\nc\nd\n")
	cx := &Context{Editor: e}
	e.execute(cx, "select_line_below")
	if got := e.annotated(); got != "a\n#[b\n|]#c\nd\n" {
		t.Fatalf("select line = %q", got)
	}

	e.execute(cx, "select_line_below")
	if got := e.annotated(); got != "a\n#[b\nc\n|]#d\n" {
		t.Fatalf("grow below = %q", got)
	}

	e.execute(cx, "select_line_above")
	if got := e.annotated(); got != "a\n#[|b\n]#c\nd\n" {
		t.Fatalf("shrink above = %q", got)
	}

	// goto_file_end and goto_last_modification.
	check(t, "#[a|]#bc\nd", "ge", "abc\n#[d|]#")
	e = newTestEditor(t, "#[a|]#bc\nxyz")
	e.feed(t, "jiQ<esc>ggg.")
	if got := e.annotated(); got != "abc\nQ#[x|]#yz" {
		t.Fatalf("last modification = %q", got)
	}
}

func TestGotoFileAndPalette(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(target, []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "see #[o|]#ther.txt here\n")
	e.Doc().SetPath(filepath.Join(dir, "main.txt"))
	e.feed(t, "gf")
	if e.Doc().Path() != target {
		t.Fatalf("goto_file opened %q, status %q", e.Doc().Path(), e.Status().Text)
	}

	e = newTestEditor(t, "#[a|]#")
	e.feed(t, " ?")
	if e.Picker == nil || len(e.Picker.Items()) < 200 {
		t.Fatalf("palette = %+v", e.Picker)
	}

	e.feed(t, "yank_joined<ret>")
	if !strings.Contains(e.Status().Text, "joined and yanked") {
		t.Fatalf("palette run status = %q", e.Status().Text)
	}

	e.feed(t, " ?:theme<ret>")
	if e.Prompt == nil || !strings.HasPrefix(e.Prompt.Input(), "lsp") && e.Prompt.Input() == "" {
		t.Fatalf("palette typed = %+v", e.Prompt)
	}

	e.feed(t, "<esc>")
}

func TestTypedExtras(t *testing.T) {
	// Not parallel: pushd and popd change the process working directory.
	dir := t.TempDir()
	e := newTestEditor(t, "#[a|]#b\n")
	e.feed(t, ":echo hello world<ret>")
	if e.Status().Text != "hello world" {
		t.Fatalf("echo = %q", e.Status().Text)
	}

	e.feed(t, ":goto 1<ret>:char<ret>")
	if !strings.Contains(e.Status().Text, "U+0061") || !strings.Contains(e.Status().Text, "Dec 97") {
		t.Fatalf("char info = %q", e.Status().Text)
	}

	e.feed(t, ":line-ending<ret>")
	if e.Status().Text != "lf" {
		t.Fatalf("line ending = %q", e.Status().Text)
	}

	e.feed(t, ":line-ending crlf<ret>")
	if got := e.Doc().Text().String(); got != "ab\r\n" {
		t.Fatalf("crlf = %q", got)
	}

	e.feed(t, ":indent-style 2<ret>:indent-style<ret>")
	if e.Status().Text != "2 spaces" || e.Doc().Indent().Width != 2 {
		t.Fatalf("indent = %q %+v", e.Status().Text, e.Doc().Indent())
	}

	e.feed(t, ":set-register q hello there<ret>")
	if got := e.Registers['q']; len(got) != 1 || got[0] != "hello there" {
		t.Fatalf("set-register = %v", got)
	}

	e.feed(t, ":clear-register q<ret>")
	if _, ok := e.Registers['q']; ok {
		t.Fatal("register not cleared")
	}

	// Options by reflection.
	e.feed(t, ":set scrolloff 9<ret>")
	if e.Config.Editor.Scrolloff != 9 {
		t.Fatalf("scrolloff = %d", e.Config.Editor.Scrolloff)
	}

	e.feed(t, ":toggle lsp.display-inlay-hints<ret>")
	if !e.Config.Editor.LSP.DisplayInlayHints {
		t.Fatal("toggle did not flip")
	}

	// An unset optional boolean turns on, then off.
	e.feed(t, ":toggle soft-wrap.enable<ret>")
	if w := e.Config.Editor.SoftWrap.Enable; w == nil || !*w {
		t.Fatalf("soft-wrap.enable = %v, status %+v", w, e.Status())
	}

	e.feed(t, ":toggle soft-wrap.enable<ret>")
	if w := e.Config.Editor.SoftWrap.Enable; w == nil || *w {
		t.Fatalf("soft-wrap.enable = %v", w)
	}

	e.feed(t, ":get lsp.display-inlay-hints<ret>")
	if e.Status().Text != "true" {
		t.Fatalf("get = %q", e.Status().Text)
	}

	e.feed(t, ":set nope 1<ret>")
	if !e.Status().IsError {
		t.Fatal("unknown option must error")
	}

	if keys := optionKeys(); !slices.Contains(keys, "lsp.display-inlay-hints") || !slices.Contains(keys, "scrolloff") {
		t.Fatalf("option keys = %v", keys)
	}

	// Theme request and completion.
	e.ThemeNames = func() []string { return []string{"catppuccin_mocha", "onedark"} }
	e.feed(t, ":theme one<tab>")
	if e.Prompt == nil || e.Prompt.Input() != "theme onedark" {
		t.Fatalf("theme completion = %+v", e.Prompt)
	}

	e.feed(t, "<ret>")
	if name, ok := e.TakeThemeRequest(); !ok || name != "onedark" {
		t.Fatalf("theme request = %q %v", name, ok)
	}

	// Files: write!, read, move, cd and directory stack.
	path := filepath.Join(dir, "sub", "new.txt")
	e.feed(t, ":w! "+path+"<ret>")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("write! = %v, status %q", err, e.Status().Text)
	}

	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}

	e.feed(t, ":read "+other+"<ret>")
	if got := e.Doc().Text().String(); !strings.Contains(got, "X") {
		t.Fatalf("read = %q", got)
	}

	moved := filepath.Join(dir, "moved.txt")
	e.feed(t, ":move "+moved+"<ret>")
	if e.Doc().Path() != moved {
		t.Fatalf("move path = %q status %q", e.Doc().Path(), e.Status().Text)
	}

	if _, err := os.Stat(path); err == nil {
		t.Fatal("old file still exists after move")
	}

	cwd := e.WorkingDir()
	e.feed(t, ":pushd "+dir+"<ret>")
	if got, _ := filepath.EvalSymlinks(e.WorkingDir()); got != mustEval(dir) || len(e.dirStack) != 1 {
		t.Fatalf("pushd cwd=%q stack=%v", e.WorkingDir(), e.dirStack)
	}

	e.feed(t, ":popd<ret>")
	if got, _ := filepath.EvalSymlinks(e.WorkingDir()); got != mustEval(cwd) {
		t.Fatalf("popd cwd=%q", e.WorkingDir())
	}

	// cquit sets the exit code.
	e.feed(t, ":cq! 3<ret>")
	if !e.ShouldQuit() || e.ExitCode != 3 {
		t.Fatalf("cquit quit=%v code=%d", e.ShouldQuit(), e.ExitCode)
	}
}

func TestSortReflowAndDiffReset(t *testing.T) {
	t.Parallel()

	check(t, "#[b|]# #(c|)# #(a|)#", ":sort<ret>", "#[a|]# #(b|)# #(c|)#")
	check(t, "#[b|]# #(c|)# #(a|)#", ":sort -r<ret>", "#[c|]# #(b|)# #(a|)#")
	check(t, "#[B|]# #(c|)# #(a|)#", ":sort -i<ret>", "#[a|]# #(B|)# #(c|)#")
	e := newTestEditor(t, "#[x|]#")
	e.feed(t, ":sort<ret>")
	if !e.Status().IsError {
		t.Fatal("sort with one selection must fail")
	}

	if got := hardWrap("  one two three four five\n", 12, "\n"); got != "  one two\n  three four\n  five\n" {
		t.Fatalf("hardWrap = %q", got)
	}

	if got := hardWrap("a b\n\nc d", 80, "\n"); got != "a b\n\nc d" {
		t.Fatalf("paragraphs = %q", got)
	}

	if got := hardWrap("a b\r\n\r\nc d e\r\n", 3, "\r\n"); got != "a b\r\n\r\nc d\r\ne\r\n" {
		t.Fatalf("crlf = %q", got)
	}

	check(t, "#[one two three four|]#", ":reflow 9<ret>", "#[one two\nthree\nfour|]#")

	e = newTestEditor(t, "a\n#[x|]#\nc\nd\n")
	e.Doc().diff.base, e.Doc().diff.raw = splitLines("a\nb\nc\n"), "a\nb\nc\n"
	e.scheduleDiff(e.Doc())
	jobs := e.TakeJobs()
	jobs[0].Run(jobs[0].Ctx, func(r JobResult) { e.ApplyResult(r) })
	e.feed(t, ":diffget<ret>")
	if got := e.Doc().Text().String(); got != "a\nb\nc\nd\n" {
		t.Fatalf("reset change = %q status %q", got, e.Status().Text)
	}
}

func TestJumpLabels(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[f|]#oo bar baz\nqux x quux\n")
	e.feed(t, "gw")
	if len(e.JumpLabels) == 0 || e.JumpLabels[0].Pos != 0 || e.JumpLabels[0].Label != "aa" {
		t.Fatalf("labels = %+v", e.JumpLabels)
	}

	// Nearest words come first and single chars get no label.
	var positions []int
	for _, l := range e.JumpLabels {
		positions = append(positions, l.Pos)
	}

	if !slices.Equal(positions, []int{0, 4, 8, 12, 18}) {
		t.Fatalf("positions = %v", positions)
	}

	label := e.JumpLabels[3].Label
	e.feed(t, label)
	if len(e.JumpLabels) != 0 {
		t.Fatal("labels should clear after a jump")
	}

	if got := e.annotated(); got != "foo bar baz\n#[q|]#ux x quux\n" {
		t.Fatalf("after jump = %q", got)
	}

	e.feed(t, "gw")
	e.feed(t, string([]rune(e.JumpLabels[1].Label)[0]))
	if e.JumpTyped() == "" || len(e.JumpLabels) == 0 {
		t.Fatal("first char should narrow labels")
	}

	e.feed(t, "<esc>")
	if len(e.JumpLabels) != 0 {
		t.Fatal("esc should cancel")
	}

	// Extend variant keeps the anchor.
	e = newTestEditor(t, "#[f|]#oo bar\n")
	e.feed(t, "gw")
	label = e.JumpLabels[1].Label
	e.execute(&Context{Editor: e}, "extend_to_word")
	e.feed(t, label)
	if got := e.annotated(); got != "#[foo b|]#ar\n" {
		t.Fatalf("extend = %q", got)
	}
}

func TestExplorerAndInteractive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "sub", "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	fileExplorer(&Context{Editor: e}, dir)
	if e.Picker == nil || len(e.Picker.Items()) != 2 || e.Picker.Items()[1].Label != "sub/" {
		t.Fatalf("explorer = %+v", e.Picker)
	}

	e.feed(t, "sub<ret>")
	if e.Picker == nil || len(e.Picker.Items()) != 2 || e.Picker.Items()[1].Label != "f.txt" {
		t.Fatalf("sub explorer = %+v", e.Picker)
	}

	e.feed(t, "f.txt<ret>")
	if e.Picker != nil || e.Doc().Path() != filepath.Join(dir, "sub", "f.txt") {
		t.Fatalf("opened %q", e.Doc().Path())
	}

	check(t, "#[a|]# #(b|)#", "", "#[a|]# #(b|)#")
	e = newTestEditor(t, "#[ab|]# #(cd|)#")
	cx := &Context{Editor: e}
	e.execute(cx, "insert_char_interactive")
	e.feed(t, "<lt>")
	e.execute(cx, "append_char_interactive")
	e.feed(t, "<gt>")
	if got := e.Doc().Text().String(); got != "<ab> <cd>" {
		t.Fatalf("interactive = %q", got)
	}

	e.Registers['a'] = []string{"v"}
	e.execute(cx, "copy_between_registers")
	e.feed(t, "ab")
	if got := e.Registers['b']; len(got) != 1 || got[0] != "v" {
		t.Fatalf("copy registers = %v", got)
	}
}

func mustEval(p string) string {
	out, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}

	return out
}

func TestSaveBehavior(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	e := newTestEditor(t, "#[a|]#  \nb\t\n\n\n")
	e.Doc().SetPath(path)
	e.Config.Editor.TrimTrailingWhitespace = true
	e.Config.Editor.TrimFinalNewlines = true
	e.feed(t, ":w<ret>")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "a\nb\n" {
		t.Fatalf("saved %q err %v", data, err)
	}

	// insert-final-newline adds the missing newline, atomic save keeps the mode.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	e = newTestEditor(t, "#[x|]#")
	e.Doc().SetPath(path)
	e.feed(t, ":w<ret>")
	data, _ = os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != "x\n" || info.Mode().Perm() != 0o600 {
		t.Fatalf("final newline %q mode %v", data, info.Mode().Perm())
	}

	e = newTestEditor(t, "#[x|]#")
	e.Doc().SetPath(path)
	e.Config.Editor.InsertFinalNewline = false
	e.feed(t, ":w<ret>")
	data, _ = os.ReadFile(path)
	if string(data) != "x" {
		t.Fatalf("no final newline %q", data)
	}

	// Auto save after a delay runs as a job and writes modified files.
	e = newTestEditor(t, "#[x|]#\n")
	e.Doc().SetPath(filepath.Join(dir, "auto.txt"))
	e.Config.Editor.AutoSave.AfterDelay = config.AutoSaveDelay{Enable: true, Timeout: 100}
	e.feed(t, "iy<esc>")
	jobs := e.TakeJobs()
	if len(jobs) == 0 {
		t.Fatal("no auto save job queued")
	}

	jobs[len(jobs)-1].Run(jobs[len(jobs)-1].Ctx, func(r JobResult) { e.ApplyResult(r) })
	if data, err := os.ReadFile(filepath.Join(dir, "auto.txt")); err != nil || string(data) != "yx\n" || e.Doc().IsModified() {
		t.Fatalf("auto save %q %v modified=%v", data, err, e.Doc().IsModified())
	}
}

func TestEditorConfigAndDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".editorconfig"), []byte("root = true\n[*.py]\nindent_style = space\nindent_size = 3\nend_of_line = crlf\ntrim_trailing_whitespace = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "x.py")
	if err := os.WriteFile(path, []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	e.Config.Editor.EditorConfig = true
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	if doc.Indent() != (IndentStyle{Width: 3}) || doc.LineEnding() != buffer.CRLF || doc.ecTrimTrailing == nil || !*doc.ecTrimTrailing {
		t.Fatalf("editorconfig applied: indent %+v le %q trim %v", doc.Indent(), doc.LineEnding(), doc.ecTrimTrailing)
	}

	e = New(NewDocument("", ""), config.Default())
	e.Config.Editor.DefaultLineEnding = "crlf"
	scratch := e.NewScratch(ActionReplace)
	if scratch.LineEnding() != buffer.CRLF {
		t.Fatalf("default line ending = %q", scratch.LineEnding())
	}

	e.Config.Editor.DefaultYankRegister = "z"
	e.Doc().SetSelection(edit.PointSelection(0))
	if e.yankRegister() != 'z' {
		t.Fatal("default yank register")
	}
}

func TestMouse(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc\ndef\nghi\n")
	e.Resize(40, 10)
	gutter := e.GutterWidth(e.Doc())
	e.HandleMouse(MouseEvent{X: gutter + 1, Y: 1, Button: MouseLeft, Kind: MousePress})
	if got := e.annotated(); got != "abc\nd#[e|]#f\nghi\n" {
		t.Fatalf("click = %q", got)
	}

	e.HandleMouse(MouseEvent{X: gutter + 2, Y: 2, Button: MouseLeft, Kind: MouseMotion})
	e.HandleMouse(MouseEvent{X: gutter + 2, Y: 2, Button: MouseLeft, Kind: MouseRelease})
	if got := e.annotated(); got != "abc\nd#[ef\nghi|]#\n" {
		t.Fatalf("drag = %q", got)
	}

	e.HandleMouse(MouseEvent{X: gutter, Y: 0, Button: MouseLeft, Kind: MousePress, Shift: true})
	if got := e.annotated(); got != "#[|abc\nd]#ef\nghi\n" {
		t.Fatalf("shift click = %q", got)
	}

	e.Config.Editor.MouseYankRegister = "m"
	e.Registers['m'] = []string{"ZZ"}
	e.HandleMouse(MouseEvent{X: gutter, Y: 2, Button: MouseMiddle, Kind: MousePress})
	if got := e.Doc().Text().String(); got != "abc\ndef\nZZghi\n" {
		t.Fatalf("middle paste = %q", got)
	}

	e.Config.Editor.Mouse = false
	e.HandleMouse(MouseEvent{X: gutter, Y: 0, Button: MouseLeft, Kind: MousePress})
	if got := e.Doc().Text().String(); got != "abc\ndef\nZZghi\n" || e.Doc().Selection().Primary().Cursor(e.Doc().Text()) == 0 {
		t.Fatal("mouse disabled must ignore clicks")
	}
}

func TestMouseWheelAndWordCompletion(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#\n"+strings.Repeat("x\n", 40))
	e.Resize(40, 10)
	e.Config.Editor.ScrollLines = 5
	e.HandleMouse(MouseEvent{X: 5, Y: 3, Button: MouseWheelDown, Kind: MouseWheel})
	if e.View().FirstLine != 5 {
		t.Fatalf("wheel scrolled to %d", e.View().FirstLine)
	}

	// Word completion starts at the configured trigger length of 7.
	e = newTestEditor(t, "alphabetical alphabravo\n#[\n|]#")
	e.feed(t, "ialphab")
	if e.Completion != nil {
		t.Fatal("menu before the trigger length")
	}

	e.feed(t, "e")
	if e.Completion == nil {
		t.Fatal("word completion menu expected")
	}

	items := e.Completion.Items()
	if len(items) != 1 || items[0].Label != "alphabetical" {
		t.Fatalf("word items = %+v", items)
	}

	e.feed(t, "<C-n><ret>")
	if got := e.Doc().Text().String(); got != "alphabetical alphabravo\nalphabetical\n" {
		t.Fatalf("after accept = %q", got)
	}

	e.Config.Editor.AutoCompletion = false
	e.feed(t, " alphabe")
	if e.Completion != nil {
		t.Fatal("auto-completion false must not open the menu")
	}

	e.feed(t, "<esc>")
}

func TestEmptyCompletionReleasesArrows(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "alphabetical alphabravo\none\n#[\n|]#")
	e.feed(t, "ialphabe")
	if e.Completion == nil {
		t.Fatal("word completion menu expected")
	}

	// Typing past every candidate closes the menu, arrows move again.
	e.feed(t, "z")
	if e.Completion != nil {
		t.Fatalf("empty menu stays open: %+v", e.Completion.Items())
	}

	e.feed(t, "<up>")
	if line := e.Doc().Text().CharToLine(e.Doc().Selection().Primary().Cursor(e.Doc().Text())); line != 1 {
		t.Fatalf("up moved to line %d", line)
	}

	e.feed(t, "<esc>")
}

func TestPathCompletion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[\n|]#")
	e.feed(t, "i"+dir+"/")
	if e.Completion == nil || len(e.Completion.Items()) != 2 {
		t.Fatalf("path completion = %+v", e.Completion)
	}

	e.feed(t, "tw<C-n><ret>")
	if got := e.Doc().Text().String(); got != dir+"/two.txt\n" {
		t.Fatalf("after path accept = %q", got)
	}
}

func TestFormatterShebangPreview(t *testing.T) {
	t.Parallel()

	// An external formatter runs as a job and replaces the text by lines.
	e := newTestEditor(t, "#[k|]#eep\nlower case\nkeep too\n")
	e.Doc().language = &syntax.Language{Name: "x", Formatter: syntax.Formatter{Command: "sh", Args: []string{"-c", "tr a-z A-Z"}}}
	e.feed(t, ":format<ret>")
	jobs := e.TakeJobs()
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d", len(jobs))
	}

	jobs[0].Run(jobs[0].Ctx, func(r JobResult) { e.ApplyResult(r) })
	if got := e.Doc().Text().String(); got != "KEEP\nLOWER CASE\nKEEP TOO\n" {
		t.Fatalf("formatted = %q status %q", got, e.Status().Text)
	}

	e.feed(t, "u")
	if got := e.Doc().Text().String(); got != "keep\nlower case\nkeep too\n" {
		t.Fatalf("undo after format = %q", got)
	}

	// A formatter that prints nothing leaves the text alone.
	e.Doc().language = &syntax.Language{Name: "x", Formatter: syntax.Formatter{Command: "sh", Args: []string{"-c", "cat >/dev/null"}}}
	e.feed(t, ":format<ret>")
	runJobs(e)
	if got := e.Doc().Text().String(); got != "keep\nlower case\nkeep too\n" || !e.Status().IsError {
		t.Fatalf("empty formatter output gave %q status %+v", got, e.Status())
	}

	// A formatter that is not installed is skipped like a missing server.
	e.Doc().language = &syntax.Language{Name: "x", AutoFormat: true, Formatter: syntax.Formatter{Command: "duck-no-such-formatter"}}
	if e.hasFormatter(e.Doc()) || shouldFormat(e, e.Doc(), writeOptions{}) {
		t.Fatal("a missing formatter is used")
	}

	e.feed(t, ":format<ret>")
	if jobs := e.TakeJobs(); len(jobs) != 0 || !strings.Contains(e.Status().Text, "no formatter") {
		t.Fatalf("jobs %d status %+v", len(jobs), e.Status())
	}

	// Shebangs identify files without a known extension.
	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	if lang, ok := langs.ForShebang("#!/usr/bin/env python3"); !ok || lang.Name != "python" {
		t.Fatalf("python shebang = %+v %v", lang, ok)
	}

	if lang, ok := langs.ForShebang("#!/bin/bash -e"); !ok || lang.Name != "bash" {
		t.Fatalf("bash shebang = %+v %v", lang, ok)
	}

	if _, ok := langs.ForShebang("plain text"); ok {
		t.Fatal("no shebang")
	}

	// Preview insertion shows the selected item and reverts on escape.
	e = newTestEditor(t, "a#[b|]#")
	e.feed(t, "a")
	e.Completion = &CompletionMenu{items: []lsp.CompletionItem{{Label: "abc"}, {Label: "abd"}}, start: 0, docID: e.Doc().ID}
	e.Completion.filter("ab")
	e.feed(t, "<C-n><C-n>")
	// Appending at the end of the file adds a newline.
	if got := e.Doc().Text().String(); got != "abd\n" {
		t.Fatalf("preview = %q", got)
	}

	e.feed(t, "<C-p>")
	if got := e.Doc().Text().String(); got != "abc\n" {
		t.Fatalf("preview back = %q", got)
	}

	e.feed(t, "<esc>")
	if got := e.Doc().Text().String(); got != "ab\n" || e.Completion != nil {
		t.Fatalf("after escape = %q", got)
	}

	// Escape closed the menu and left insert mode, A goes back.
	e.feed(t, "A")
	e.Config.Editor.PreviewCompletionInsert = false
	e.Completion = &CompletionMenu{items: []lsp.CompletionItem{{Label: "abc"}}, start: 0, docID: e.Doc().ID}
	e.Completion.filter("ab")
	e.feed(t, "<C-n>")
	if got := e.Doc().Text().String(); got != "ab\n" {
		t.Fatalf("preview disabled = %q", got)
	}

	e.feed(t, "<ret>")
	if got := e.Doc().Text().String(); got != "abc\n" {
		t.Fatalf("accept = %q", got)
	}
}

func TestInlineDiagnosticsRows(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc\ndef\n")
	e.Resize(40, 10)
	doc := e.Doc()
	doc.diagnostics = []Diagnostic{
		{Range: edit.NewRange(1, 2), Severity: lsp.SeverityError, Message: "bad thing"},
		{Range: edit.NewRange(4, 5), Severity: lsp.SeverityHint, Message: "hint here"},
	}
	e.Config.Editor.InlineDiagnostics.CursorLine = "warning"
	e.Config.Editor.InlineDiagnostics.OtherLines = "disable"
	inline := e.InlineDiagnostics(doc)
	if len(inline) != 1 || len(inline[0]) != 1 || !strings.HasSuffix(inline[0][0].Text, "└─ bad thing") || inline[0][0].Col != 1 {
		t.Fatalf("inline = %+v", inline)
	}

	// End of line diagnostics skip lines that have inline rows but keep
	// the others within the filter.
	eol := e.EOLDiagnostics(doc, 0, doc.LineCount())
	if _, ok := eol[0]; ok {
		t.Fatal("line with inline rows should not get an eol diagnostic")
	}

	if d, ok := eol[1]; !ok || d.Text != "  hint here" {
		t.Fatalf("eol = %+v", eol)
	}

	// A diagnostic below the cursor line filter still shows at the end
	// of its line next to the inline rows.
	doc.diagnostics = append(doc.diagnostics, Diagnostic{Range: edit.NewRange(0, 1), Severity: lsp.SeverityInfo, Message: "minor"})
	if d, ok := e.EOLDiagnostics(doc, 0, doc.LineCount())[0]; !ok || d.Text != "  minor" {
		t.Fatalf("diagnostic below the inline filter lost: %+v", d)
	}

	doc.diagnostics = doc.diagnostics[:2]

	vp := e.lineViewport(e.View(), doc)
	if vp.RowCount(doc.Line(0), 0, false) != 2 {
		t.Fatalf("row count = %d", vp.RowCount(doc.Line(0), 0, false))
	}

	if row, _, ok := e.CursorRow(e.View(), doc, 4); !ok || row != 2 {
		t.Fatalf("cursor row below virtual line = %d %v", row, ok)
	}
}

func TestRegisterInfoAndFeatures(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.Registers['q'] = []string{"first line\nsecond"}
	e.feed(t, "\"")
	if e.Info == nil || e.Info.Title != "Select register" {
		t.Fatalf("info = %+v", e.Info)
	}

	found := false
	for _, entry := range e.Info.Entries {
		if entry.Keys[0] == "q" && entry.Desc == "first line" {
			found = true
		}
	}

	if !found {
		t.Fatalf("entries = %+v", e.Info.Entries)
	}

	e.feed(t, "q")
	if e.Info != nil || e.SelectedRegister != 'q' {
		t.Fatalf("after register key: info %v register %c", e.Info, e.SelectedRegister)
	}

	// Feature filters pick the server per feature.
	dc := docClient{only: []string{"diagnostics"}}
	if dc.allows("format") || !dc.allows("diagnostics") {
		t.Fatal("only filter")
	}

	dc = docClient{except: []string{"format"}}
	if dc.allows("format") || !dc.allows("hover") {
		t.Fatal("except filter")
	}

	opts, rest, _ := splitWriteFlags([]string{"--no-format", "a.txt", "--no-code-actions"})
	if !opts.noFormat || !opts.noCodeActions || len(rest) != 1 || rest[0] != "a.txt" {
		t.Fatalf("write flags = %+v %v", opts, rest)
	}
}

func TestExternalChangeGuard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "guard.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	e.feed(t, "itwo <esc>")
	// Another program writes the file after it was read here.
	future := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	e.feed(t, ":w<ret>")
	if !e.Status().IsError || !strings.Contains(e.Status().Text, "external") {
		t.Fatalf("status = %+v", e.Status())
	}

	if data, _ := os.ReadFile(path); string(data) != "other\n" {
		t.Fatalf("file overwritten: %q", data)
	}

	e.feed(t, ":w!<ret>")
	if data, _ := os.ReadFile(path); string(data) != "two one\n" {
		t.Fatalf("force write = %q status %q", data, e.Status().Text)
	}

	if doc.ChangedOnDisk() {
		t.Fatal("disk time not refreshed after the forced write")
	}
}

func TestParameterOffsets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label, param string
		start, end   int
	}{
		{"x(x int)", `"x int"`, 2, 7},
		{"x(a int, x int)", `"x"`, 9, 10},
		{"fn(é, b)", `"b"`, 6, 7},
		{"f(a)", `[2, 3]`, 2, 3},
		{"f(a)", `"zz"`, 0, 0},
	}
	for _, c := range cases {
		start, end := parameterOffsets(c.label, jsontext.Value(c.param))
		if start != c.start || end != c.end {
			t.Errorf("parameterOffsets(%q, %s) = %d, %d want %d, %d", c.label, c.param, start, end, c.start, c.end)
		}
	}
}

func TestFilePickerRoots(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(sub, "a.go")
	if err := os.WriteFile(path, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	if _, err := e.Open(path, ActionReplace); err != nil {
		t.Fatal(err)
	}

	e.feed(t, "<space>F")
	if e.Picker == nil || e.Picker.previewBase != sub {
		t.Fatalf("space F should list the buffer directory, got %+v", e.Picker)
	}

	e.feed(t, "<esc><space>f")
	if e.Picker == nil || e.Picker.previewBase != e.workspaceRoot() {
		t.Fatalf("space f should list the workspace, got %+v", e.Picker)
	}
}

func TestModifiedFollowsText(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	e.feed(t, "<C-c>")
	if !doc.IsModified() {
		t.Fatal("a commented line is a change")
	}

	e.feed(t, "<C-c>")
	if doc.IsModified() {
		t.Fatalf("toggling twice restores the text %q", doc.Text().String())
	}

	e.feed(t, "ix<esc>")
	if !doc.IsModified() {
		t.Fatal("an insert is a change")
	}

	e.feed(t, "hd")
	if doc.IsModified() {
		t.Fatalf("deleting the insert restores the text %q", doc.Text().String())
	}

	e.feed(t, ":q<ret>")
	if e.Status().IsError {
		t.Fatalf(":q refused: %s", e.Status().Text)
	}
}

func TestFormatterRunsInProjectRoot(t *testing.T) {
	t.Parallel()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	for path, data := range map[string]string{filepath.Join(root, "go.mod"): "module x\n", filepath.Join(sub, "a.go"): "x\n"} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(filepath.Join(sub, "a.go"), ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	doc.language = &syntax.Language{Name: "x", Roots: []string{"go.mod"}, Formatter: syntax.Formatter{Command: "pwd"}}
	e.feed(t, ":format<ret>")
	for _, job := range e.TakeJobs() {
		job.Run(job.Ctx, func(r JobResult) { e.ApplyResult(r) })
	}

	if got := doc.Text().String(); got != root+"\n" {
		t.Fatalf("formatter ran in %q, want %q", got, root)
	}
}

func TestReloadLanguagesUpdatesDocuments(t *testing.T) {
	t.Parallel()

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	e.Syntax = syntax.NewLoader(nil, langs)
	e.lspDisabled = true
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := e.Open(path, ActionReplace)
	if err != nil || doc.language == nil || doc.language.Name != "go" {
		t.Fatalf("open: %v %+v", err, doc.language)
	}

	before := doc.language
	reloaded, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	e.reloadLanguages(reloaded)
	if doc.language == before || doc.language.Name != "go" {
		t.Fatal("a reload hands the document the new go entry")
	}
}

func TestTrimSortNumericBufferDirectory(t *testing.T) {
	t.Parallel()

	check(t, "#[a|]#  \nb\t\nc\n", ":trim<ret>", "#[a|]#\nb\nc\n")
	// With a wider selection only its lines are trimmed.
	check(t, "#[a  \nb |]#\nc  \n", ":trim<ret>", "#[a\nb|]#\nc  \n")

	e := newTestEditor(t, "#[10|]# #(9|)# #(x|)# #(-2.5|)# #(100|)#")
	e.feed(t, ":sort --numeric<ret>")
	if got := e.Doc().Text().String(); got != "-2.5 9 10 100 x" {
		t.Fatalf("numeric sort = %q", got)
	}

	if got, err := e.expand("%{buffer_directory}"); err != nil || got != "." {
		t.Fatalf("buffer_directory = %q %v", got, err)
	}
}

func TestSearchHighlightAndLiteral(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#.b a.b axb\n")
	doc := e.Doc()
	e.feed(t, "/a.b<ret>")
	if got := e.SearchMatches(doc, 0, doc.Text().Len()); len(got) != 3 {
		t.Fatalf("regex matches = %v", got)
	}

	e.feed(t, "<esc>")
	if e.SearchMatches(doc, 0, doc.Text().Len()) != nil {
		t.Fatal("escape in normal mode clears the highlight")
	}

	e.execute(&Context{Editor: e}, "search_literal")
	e.feed(t, "a.b<ret>")
	if got := e.SearchMatches(doc, 0, doc.Text().Len()); len(got) != 2 {
		t.Fatalf("literal matches = %v", got)
	}

	e.feed(t, "n")
	if got := doc.Selection().Primary(); got.From() != 4 || got.To() != 7 {
		t.Fatalf("n after a literal search selects %v", got)
	}

	e.feed(t, "<esc>/zzz<esc>")
	if e.SearchMatches(doc, 0, doc.Text().Len()) != nil {
		t.Fatal("a cancelled search restores the earlier, cleared highlight")
	}
}

func TestPickerPreviewScroll(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[x|]#")
	e.openPicker(NewPicker("t", []PickerItem{{Label: "a"}, {Label: "b"}}, func(*Context, PickerItem, PickerAction) {}))
	e.Resize(80, 24)
	e.feed(t, "<A-d><A-d>")
	if got := e.Picker.PreviewScroll(); got <= 0 {
		t.Fatalf("scroll after A-d = %d", got)
	}

	e.feed(t, "<down>")
	if got := e.Picker.PreviewScroll(); got != 0 {
		t.Fatalf("another item starts unscrolled, got %d", got)
	}

	e.feed(t, "<up>")
	if got := e.Picker.PreviewScroll(); got <= 0 {
		t.Fatalf("back on the first item the scroll is kept, got %d", got)
	}
}

func TestHideDiagnosticsInInsert(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc\nxy\n")
	doc := e.Doc()
	doc.diagnostics = []Diagnostic{{Range: edit.NewRange(4, 5), Severity: lsp.SeverityError, Message: "bad"}}
	e.Config.Editor.HideDiagnosticsInInsert = true
	if len(e.EOLDiagnostics(doc, 0, 2)) != 1 {
		t.Fatal("normal mode shows the diagnostic")
	}

	e.feed(t, "i")
	if len(e.EOLDiagnostics(doc, 0, 2)) != 0 || !e.DiagnosticsHidden() {
		t.Fatal("insert mode hides it")
	}

	e.feed(t, "<esc>")
	if len(e.EOLDiagnostics(doc, 0, 2)) != 1 {
		t.Fatal("back in normal mode it shows again")
	}
}

func TestPromptHistory(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[x|]#")
	e.feed(t, ":echo one<ret>:echo two<ret>:set-option a b<ret>")
	e.feed(t, ":<up>")
	if got := e.Prompt.Input(); got != "set-option a b" {
		t.Fatalf("newest = %q", got)
	}

	e.feed(t, "<up><up>")
	if got := e.Prompt.Input(); got != "echo one" {
		t.Fatalf("oldest = %q", got)
	}

	e.feed(t, "<down><down><down>")
	if got := e.Prompt.Input(); got != "" {
		t.Fatalf("past the newest the draft comes back, got %q", got)
	}

	// Typed text filters the walk by prefix and stays as the draft.
	e.feed(t, "<esc>:ec<up>")
	if got := e.Prompt.Input(); got != "echo two" {
		t.Fatalf("prefix walk = %q", got)
	}

	e.feed(t, "<up><up>")
	if got := e.Prompt.Input(); got != "echo one" {
		t.Fatalf("prefix walk stops at the last match, got %q", got)
	}

	e.feed(t, "<down><down>")
	if got := e.Prompt.Input(); got != "ec" {
		t.Fatalf("draft = %q", got)
	}
}

func TestCheckDiskChanges(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	later := time.Now().Add(time.Minute)
	writeAt := func(text string, at time.Time) {
		t.Helper()

		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}

	// An untouched buffer follows the file.
	writeAt("two\n", later)
	e.CheckDiskChanges()
	if got := doc.Text().String(); got != "two\n" || doc.IsModified() {
		t.Fatalf("reloaded text %q modified %v", got, doc.IsModified())
	}

	// A buffer with edits keeps them and warns once.
	e.feed(t, "ix<esc>")
	writeAt("three\n", later.Add(time.Minute))
	e.CheckDiskChanges()
	if got := doc.Text().String(); got != "xtwo\n" || !e.Status().IsError {
		t.Fatalf("edited buffer = %q status %q", got, e.Status().Text)
	}

	e.SetStatus("")
	e.CheckDiskChanges()
	if e.Status().IsError {
		t.Fatal("the same change is reported once")
	}
}

func TestReopenBuffer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	if _, err := e.Open(path, ActionReplace); err != nil {
		t.Fatal(err)
	}

	e.feed(t, "j:buffer-close<ret>")
	if _, open := e.DocumentByPath(path); open {
		t.Fatal("buffer still open")
	}

	e.feed(t, ":reopen<ret>")
	doc, open := e.DocumentByPath(path)
	if !open || doc.Selection().Primary().CursorLine(doc.Text()) != 1 {
		t.Fatalf("reopened %v, status %q", open, e.Status().Text)
	}

	e.feed(t, ":reopen<ret>")
	if !e.Status().IsError {
		t.Fatal("nothing left to reopen")
	}
}

func TestPersistSession(t *testing.T) {
	t.Parallel()

	state := t.TempDir()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	first := newTestEditor(t, "#[x|]#")
	first.SetStateDir(state)
	if _, err := first.Open(path, ActionReplace); err != nil {
		t.Fatal(err)
	}

	first.feed(t, "jllix<esc>:w<ret>")
	first.SaveSession()

	second := newTestEditor(t, "#[x|]#")
	second.SetStateDir(state)
	doc, err := second.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	text := doc.Text()
	if line := doc.Selection().Primary().CursorLine(text); line != 1 {
		t.Fatalf("cursor restored to line %d", line)
	}

	if doc.IsModified() {
		t.Fatal("a restored history starts saved")
	}

	second.feed(t, "u")
	if got := doc.Text().String(); got != "one\ntwo\nthree\n" {
		t.Fatalf("undo after reopening = %q", got)
	}

	// A file changed elsewhere drops the stored history.
	if err := os.WriteFile(path, []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	third := newTestEditor(t, "#[x|]#")
	third.SetStateDir(state)
	doc, err = third.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	third.feed(t, "u")
	if got := doc.Text().String(); got != "other\n" {
		t.Fatalf("stale history applied: %q", got)
	}
}

func TestCursorDiagnosticsBox(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "a\n#[b|]#c\nd\n")
	doc := e.Doc()
	doc.diagnostics = []Diagnostic{
		{Range: edit.NewRange(2, 3), Severity: lsp.SeverityError, Message: "under the cursor"},
		{Range: edit.NewRange(0, 1), Severity: lsp.SeverityWarning, Message: "elsewhere"},
	}

	if got := e.CursorDiagnostics(e.View()); len(got) != 1 || got[0].Message != "under the cursor" {
		t.Fatalf("box = %v", got)
	}

	eol := e.EOLDiagnostics(doc, 0, 3)
	if _, ok := eol[1]; ok {
		t.Fatal("the cursor line keeps its end free")
	}

	if _, ok := eol[0]; !ok {
		t.Fatal("other lines still show their diagnostic at the end")
	}

	// With inline diagnostics on the cursor line the box steps aside.
	e.Config.Editor.InlineDiagnostics.CursorLine = "hint"
	if got := e.CursorDiagnostics(e.View()); got != nil {
		t.Fatalf("box with inline diagnostics = %v", got)
	}
}

func TestPullBackFromEOF(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("a\nbc\n")
	if got := pullBackFromEOF(text, edit.NewRange(5, 5)); got != edit.NewRange(4, 4) {
		t.Fatalf("end of file = %v, want the line break of the last line", got)
	}

	for _, r := range []edit.Range{edit.NewRange(2, 3), edit.NewRange(4, 5)} {
		if got := pullBackFromEOF(text, r); got != r {
			t.Fatalf("%v moved to %v", r, got)
		}
	}

	noBreak := buffer.NewRope("a\nbc")
	if got := pullBackFromEOF(noBreak, edit.NewRange(4, 4)); got != edit.NewRange(4, 4) {
		t.Fatalf("without a final break the end stays, got %v", got)
	}
}

// TestChangeTextObject selects the change under the cursor with mig.
func TestChangeTextObject(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "a\n#[x|]#\ny\nc\n")
	e.Doc().diff.base = splitLines("a\nb\nc\n")
	e.scheduleDiff(e.Doc())
	runJobs(e)
	e.feed(t, "mig")
	if got := e.annotated(); got != "a\n#[x\ny|]#\nc\n" {
		t.Fatalf("mig = %q status %q", got, e.Status().Text)
	}
}

// TestWrappedScrollKeepsCursorInMargin scrolls a long wrapped line in one
// step and keeps the cursor between the scrolloff margins.
func TestWrappedScrollKeepsCursorInMargin(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#"+strings.Repeat("bcdefghij ", 300)+"\nshort\n")
	on := true
	e.Config.Editor.SoftWrap.Enable = &on
	e.Config.Editor.Scrolloff = 2
	e.Resize(40, 12)
	v := e.View()
	height := v.TextHeight()
	inMargin := func(what string) {
		t.Helper()

		row, _, ok := e.CursorRow(v, e.Doc(), e.Doc().Selection().Primary().Cursor(e.Doc().Text()))
		if !ok || row < 0 || row+2 >= height {
			t.Fatalf("%s: cursor row %d of %d, first line %d row %d", what, row, height, v.FirstLine, v.FirstRow)
		}
	}

	e.feed(t, "gl")
	if v.FirstLine != 0 || v.FirstRow == 0 {
		t.Fatalf("end of the line: first line %d row %d", v.FirstLine, v.FirstRow)
	}

	inMargin("end of the line")
	if row, _, _ := e.CursorRow(v, e.Doc(), e.Doc().Selection().Primary().Cursor(e.Doc().Text())); row != height-3 {
		t.Fatalf("scrolling down stops at the margin, row %d of %d", row, height)
	}

	e.feed(t, "j")
	inMargin("next line")
	e.feed(t, "gg")
	if v.FirstLine != 0 || v.FirstRow != 0 {
		t.Fatalf("back to the top: first line %d row %d", v.FirstLine, v.FirstRow)
	}
}

// TestSaveRecovery keeps only buffers with unsaved changes.
func TestSaveRecovery(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	dir := t.TempDir()
	if saved, err := e.SaveRecovery(dir); err != nil || len(saved) != 0 {
		t.Fatalf("clean buffer saved %v, %v", saved, err)
	}

	e.feed(t, "ix<esc>")
	saved, err := e.SaveRecovery(dir)
	if err != nil || len(saved) != 1 {
		t.Fatalf("saved %v, %v", saved, err)
	}

	if data, _ := os.ReadFile(saved[0][1]); string(data) != e.Doc().Text().String() {
		t.Fatalf("recovery file holds %q", data)
	}
}

// TestExplorerFiltersAndNavigates hides dot files and ignored entries,
// goes into a directory with A-l and back up with backspace, selecting
// the directory it came from.
func TestExplorerFiltersAndNavigates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, d := range []string{".git", "build", "src"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for name, text := range map[string]string{".gitignore": "build/\n", ".env": "x", "src/a.go": "package a\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	labels := func(e *Editor) []string {
		var out []string
		for _, item := range e.Picker.Items() {
			out = append(out, item.Label)
		}

		return out
	}
	e := newTestEditor(t, "#[a|]#")
	fileExplorer(&Context{Editor: e}, dir)
	if got := labels(e); !slices.Equal(got, []string{"../", "src/"}) {
		t.Fatalf("listed %q", got)
	}

	e.feed(t, "<down><A-l>")
	if got := labels(e); !slices.Equal(got, []string{"../", "a.go"}) {
		t.Fatalf("inside src %q", got)
	}

	e.feed(t, "<backspace>")
	if sel, _ := e.Picker.Selected(); sel.Label != "src/" {
		t.Fatalf("after going up %q is selected in %q", sel.Label, labels(e))
	}

	e.Config.Editor.FilePicker.Hidden = false
	e.Config.Editor.FilePicker.GitIgnore = false
	fileExplorer(&Context{Editor: e}, dir)
	if got := labels(e); !slices.Equal(got, []string{"../", "build/", "src/", ".env", ".gitignore"}) {
		t.Fatalf("with everything shown %q", got)
	}
}

// TestDocumentsFromCommandLine loads documents behind the shown one at
// a position, once each, and sets their language.
func TestDocumentsFromCommandLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path, []byte("one\ntwo three\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	shown := e.Doc()
	doc, err := OpenDocument(host.Local{}, path)
	if err != nil {
		t.Fatal(err)
	}

	e.AddDocument(doc)
	e.GotoPositionIn(doc, 2, 5)
	again, _ := OpenDocument(host.Local{}, path)
	e.AddDocument(again)
	if e.Doc() != shown || len(e.Docs) != 2 {
		t.Fatalf("shown %v, %d documents", e.Doc() == shown, len(e.Docs))
	}

	e.Switch(doc.ID, ActionReplace)
	if got := e.annotated(); got != "one\ntwo #[t|]#hree\n" {
		t.Fatalf("position %q", got)
	}

	if err := e.SetLanguageOf(doc, "nope"); err == nil {
		t.Fatal("unknown language accepted")
	}
}

// TestRestoreSession reopens the files of the last session and drops the
// empty scratch buffer the editor started with.
func TestRestoreSession(t *testing.T) {
	t.Parallel()

	dir, state := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[a|]#")
	e.workDir = dir
	e.SetStateDir(state)
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := e.Open(filepath.Join(dir, name), ActionReplace); err != nil {
			t.Fatal(err)
		}
	}

	_, _ = e.Open(filepath.Join(dir, "a.txt"), ActionReplace)
	e.SaveSession()

	next := New(NewDocument("", ""), e.Config)
	next.workDir = dir
	next.SetStateDir(state)
	n, err := next.RestoreSession()
	if err != nil || n != 2 || len(next.Docs) != 2 || next.Doc().Path() != filepath.Join(dir, "a.txt") {
		t.Fatalf("restored %d, %v, %d documents, current %q", n, err, len(next.Docs), next.Doc().Path())
	}

	other := New(NewDocument("", ""), e.Config)
	other.workDir = t.TempDir()
	other.SetStateDir(state)
	if _, err := other.RestoreSession(); err == nil {
		t.Fatal("another directory has no session")
	}
}

// TestReplaceByLines keeps the final line break and CRLF endings right
// when text is replaced line by line.
func TestReplaceByLines(t *testing.T) {
	t.Parallel()

	cases := []struct{ text, next string }{
		{"a\nb", "a\nb\n"},
		{"a\nb\n", "a\nb"},
		{"a\r\nb\r\nc\r\n", "a\r\n  b\r\nc\r\n"},
		{"", "x\n"},
		{"one\ntwo\n", "two\n"},
	}
	for _, c := range cases {
		e := newTestEditor(t, "#[|]#"+c.text)
		replaceByLines(e.Doc(), c.next)
		if got := e.Doc().Text().String(); got != c.next {
			t.Errorf("%q -> %q gave %q", c.text, c.next, got)
		}
	}

	// A cursor on an untouched line stays there.
	e := newTestEditor(t, "one\n#[t|]#wo\nthree\n")
	replaceByLines(e.Doc(), "ONE\ntwo\nthree")
	if got := e.annotated(); got != "ONE\n#[t|]#wo\nthree" {
		t.Fatalf("cursor moved: %q", got)
	}
}

// TestWrappedCursorRowWithCombiningMarks keeps the cursor row right in
// text with many chars per cell, where the rows past the cursor need
// more chars than a plain line would.
func TestWrappedCursorRowWithCombiningMarks(t *testing.T) {
	t.Parallel()

	// Each cell is one letter with four combining marks.
	cell := "a\u0301\u0300\u0302\u0303"
	word := strings.Repeat(cell, 6)
	text := strings.Repeat(word+" ", 12)
	e := newTestEditor(t, "#[|]#"+text+"\n")
	on := true
	e.Config.Editor.SoftWrap.Enable = &on
	e.Resize(30, 10)
	v, doc := e.View(), e.Doc()
	vp := e.lineViewport(v, doc)
	full := vp.LineRows(doc.Line(0), 0, false)
	for _, pos := range []int{0, len([]rune(word)) - 1, len([]rune(word)) + 1, len([]rune(text)) / 2} {
		wantRow, _ := ui.WrapPosition(full, pos)
		gotRow, _ := ui.WrapPosition(rowsAround(vp, doc, 0, pos, 1), pos)
		if gotRow != wantRow {
			t.Fatalf("char %d: row %d from the partial layout, %d from the whole line", pos, gotRow, wantRow)
		}
	}
}

// TestWorkspaceRootFollowsFile starts pickers in the project of the file
// when the session works in the home directory: a git repository, a
// deployed release without git, or the file directory.
func TestWorkspaceRootFollowsFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mk := func(rel string) string {
		t.Helper()

		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		return p
	}
	repoFile := mk("work/app/internal/a.go")
	if err := os.MkdirAll(filepath.Join(home, "work/app/.git"), 0o755); err != nil {
		t.Fatal(err)
	}

	mk("work/app/go.mod")
	mk("srv/shop/current/Gemfile")
	mk("srv/shop/current/REVISION")
	releaseFile := mk("srv/shop/current/app/models/user.rb")
	looseFile := mk("notes/today.txt")

	e := newTestEditor(t, "#[a|]#")
	e.Host = homeHost{home: home}
	e.workDir = home
	cases := map[string]string{
		repoFile:    filepath.Join(home, "work/app"),
		releaseFile: filepath.Join(home, "srv/shop/current"),
		looseFile:   filepath.Join(home, "notes"),
	}
	for file, want := range cases {
		if _, err := e.Open(file, ActionReplace); err != nil {
			t.Fatal(err)
		}

		if got := e.workspaceRoot(); got != want {
			t.Errorf("%s: root %s want %s", file, got, want)
		}
	}

	// After :cd the working directory wins, also for a file outside it.
	if _, err := e.Open(looseFile, ActionReplace); err != nil {
		t.Fatal(err)
	}

	// What :cd leaves, without changing the directory of the test.
	e.workDir, e.cwdChosen = filepath.Join(home, "srv"), true

	if got := e.workspaceRoot(); got != filepath.Join(home, "srv") {
		t.Fatalf("root after :cd %s", got)
	}

	e.cwdChosen = false

	// A session in a project keeps it for files inside.
	e.workDir = filepath.Join(home, "work/app")
	if _, err := e.Open(repoFile, ActionReplace); err != nil {
		t.Fatal(err)
	}

	if got := e.workspaceRoot(); got != filepath.Join(home, "work/app") {
		t.Fatalf("project session root %s", got)
	}
}

// homeHost is the local machine with another home directory.
type homeHost struct {
	host.Local
	home string
}

func (h homeHost) Home() (string, error) { return h.home, nil }
