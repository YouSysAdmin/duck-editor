package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

func TestAutoPairsInsert(t *testing.T) {
	t.Parallel()

	for _, p := range edit.DefaultPairs {
		o, c := string(p.Open), string(p.Close)
		differing := p.Open != p.Close
		checkNL(t, "#[\n|]#", "i"+o, o+"#[|"+c+"]#")
		checkNL(t, o+"#[|"+c+"]#", "i ", o+" #[| ]#"+c)
		checkNL(t, o+" #[|"+c+"]#", "i ", o+"  #[|"+c+"]#")
		checkNL(t, "#[f|]#oo\n", "i"+o, o+"#[|f]#oo\n")
		checkNL(t, "#[foo|]#\n", "i"+o, o+"#[|foo]#\n")
		checkNL(t, "\n#[\n|]#", "i"+o, "\n"+o+"#[|"+c+"]#\n")
		checkNL(t, "#[\n|]##(\n|)##(\n|)#", "i"+o, o+"#[|"+c+"]#\n"+o+"#(|"+c+")#\n"+o+"#(|"+c+")#\n")
		// A pair typed in full ends after the closing char. A quote
		// that was already in the text is not typed over.
		checkNL(t, "#[\n|]#", "i"+o+c, o+c+"#[|\n]#")
		checkNL(t, "#[\n|]##(\n|)#", "i"+o+c, o+c+"#[|\n]#"+o+c+"#(|\n)#")
		if differing {
			checkNL(t, o+"#["+c+"|]#\n", "i"+c, o+c+"#[|\n]#")
		} else {
			checkNL(t, "a#["+c+"|]#\n", "i"+c, "a"+c+"#[|"+c+"]#\n")
		}

		checkNL(t, o+"#[|"+c+"]#\n", "i<backspace>", "#[|\n]#")
		checkNL(t, o+" #[| ]#"+c, "i<backspace>", o+"#[|"+c+"]#")
		checkNL(t, "foo"+o+" #[| ]#"+c, "i<backspace>", "foo"+o+"#[|"+c+"]#")
		checkNL(t, o+"  #[|"+c+"]#", "i<backspace>", o+" #[|"+c+"]#")
		if differing {
			checkNL(t, "foo#[\n|]#", "i"+o, "foo"+o+"#[|"+c+"]#\n")
			checkNL(t, "foo#[ wor|]#\n", "i"+o, "foo"+o+"#[|"+c+" wor]#\n")
			checkNL(t, "foo"+o+"#[|"+c+" wor]#\n", "i"+c, "foo"+o+c+"#[| wor]#\n")
			checkNL(t, o+"#["+c+"|]#\n", "i"+o, o+o+"#[|"+c+"]#"+c+"\n")
			checkNL(t, o+"#[|"+o+"]#", "i ", o+" #[|"+o+"]#")
			checkNL(t, o+" #[|"+o+"]#", "i<backspace>", o+"#[|"+o+"]#")
			checkNL(t, fmt.Sprintf("%s#[|%s]#\n%s#(|%s)#%s%s\n%s%s#(|%s%s)#\nfoo#(|\n)#\n", o, c, o, o, c, c, o, o, c, c), "i ",
				fmt.Sprintf("%s #[| ]#%s\n%s #(|%s)#%s%s\n%s%s #(| %s%s)#\nfoo #(|\n)#\n", o, c, o, o, c, c, o, o, c, c))
		} else {
			checkNL(t, "foo#[\n|]#", "i"+o, "foo"+o+"#[|\n]#")
		}
	}
}

func TestAutoPairsDisabled(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[\n|]#")
	e.Config.Editor.AutoPairs = false
	e.feed(t, "i(")
	if got := e.annotated(); got != "(#[|\n]#" {
		t.Fatalf("disabled = %q", got)
	}

	e = newTestEditor(t, "#[\n|]#")
	e.Config.Editor.AutoPairs = map[string]any{"<": ">"}
	e.feed(t, "i<lt>")
	if got := e.annotated(); got != "<#[|>]#\n" {
		t.Fatalf("configured = %q", got)
	}

	e.feed(t, "<esc>o(")
	if !strings.Contains(e.Doc().Text().String(), "(\n") || strings.Contains(e.Doc().Text().String(), "()") {
		t.Fatalf("default pair must be replaced: %q", e.Doc().Text().String())
	}
}

func TestNewlineBetweenPair(t *testing.T) {
	t.Parallel()

	check(t, "#[{|]#}", "a<ret>", "#[{\n\t\n|]#}")
	check(t, "{#[}|]#", "i<ret>", "{\n\t#[|\n]#}")
}

func TestToggleCommentsCommand(t *testing.T) {
	t.Parallel()

	withLang := func(in string) *Editor {
		e := newTestEditor(t, in)
		e.Doc().language = &syntax.Language{CommentTokens: []string{"//", "///"}, BlockCommentTokens: [][2]string{{"/*", "*/"}}}

		return e
	}
	e := withLang("#[a|]#bc\ndef\n")
	e.feed(t, "<C-c>")
	if got := e.Doc().Text().String(); got != "// abc\ndef\n" {
		t.Fatalf("comment = %q", got)
	}

	e.feed(t, "<C-c>")
	if got := e.Doc().Text().String(); got != "abc\ndef\n" {
		t.Fatalf("uncomment = %q", got)
	}

	e.feed(t, "xx<C-c>")
	if got := e.Doc().Text().String(); got != "// abc\n// def\n" {
		t.Fatalf("multi line = %q", got)
	}

	// Block comment of a selection inside a line.
	e = withLang("foo #[bar|]# baz\n")
	e.feed(t, " C")
	if got := e.Doc().Text().String(); got != "foo /* bar */ baz\n" {
		t.Fatalf("block = %q", got)
	}

	// No language falls back to #.
	check(t, "#[a|]#bc", "<C-c>", "# #[a|]#bc")
	// Doc comments continue on new lines.
	e = withLang("/// doc#[\n|]#x\n")
	e.feed(t, "khA<ret>more<esc>")
	if got := e.Doc().Text().String(); got != "/// doc\n/// more\nx\n" {
		t.Fatalf("continue = %q", got)
	}

	e = withLang("// one#[\n|]#x\n")
	e.feed(t, "otwo<esc>")
	if got := e.Doc().Text().String(); got != "// one\n// two\nx\n" {
		t.Fatalf("open continues = %q", got)
	}
}

// TestNewlineInPairWithSyntax keeps the closer at the indentation of the
// line that opened the pair when tree-sitter indents the inner line.
func TestNewlineInPairWithSyntax(t *testing.T) {
	runtime := filepath.Join("..", "..", "runtime")
	if _, err := os.Stat(filepath.Join(runtime, "grammars", "go.so")); err != nil {
		t.Skip("no Go grammar built")
	}

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct{ src, before, want string }{
		{"package main\n\nfunc main() {}\n", "func main() {", "package main\n\nfunc main() {\n\t\n}\n"},
		{"package main\n\nfunc main() {\n\tif ok {}\n}\n", "\tif ok {", "package main\n\nfunc main() {\n\tif ok {\n\t\t\n\t}\n}\n"},
		{"package main\n\nfunc main() {\n\tf()\n}\n", "\tf(", "package main\n\nfunc main() {\n\tf(\n\t\t\n\t)\n}\n"},
	}
	for _, tt := range tests {
		path := filepath.Join(t.TempDir(), "a.go")
		if err := os.WriteFile(path, []byte(tt.src), 0o644); err != nil {
			t.Fatal(err)
		}

		e := newTestEditor(t, "#[x|]#")
		e.Syntax = syntax.NewLoader([]string{runtime}, langs)
		e.lspDisabled = true
		doc, err := e.Open(path, ActionReplace)
		if err != nil || doc.Syntax() == nil {
			t.Fatalf("open: %v", err)
		}

		doc.SetSelection(edit.PointSelection(strings.Index(tt.src, tt.before) + len(tt.before)))
		e.feed(t, "i<ret>")
		if got := doc.Text().String(); got != tt.want {
			t.Errorf("enter after %q gives %q, want %q", tt.before, got, tt.want)
		}
	}
}

// TestIndentMatchesHelix replays cases compared byte for byte with
// Helix: nested pairs on one line indent once, and an aligned argument
// list takes its anchor column without an extra level.
func TestIndentMatchesHelix(t *testing.T) {
	runtime := filepath.Join("..", "..", "runtime")
	if _, err := os.Stat(filepath.Join(runtime, "grammars", "javascript.so")); err != nil {
		t.Skip("grammars not built")
	}

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct{ name, keys, want string }{
		{"t.js", "ifoo({<ret>x<esc>", "foo({\n  x\n})"},
		{"t.js", "ix = {a: {<ret>y<esc>", "x = {a: {\n  y\n}}"},
		{"t.json", "i[{<ret>1<esc>", "[{\n  1\n}]"},
		{"t.py", "ix = [[<ret>1<esc>", "x = [[\n    1\n]]"},
		{"t.py", "ix = g(a,<ret>b<esc>", "x = g(a,\n      b)"},
	}
	for _, tt := range tests {
		path := filepath.Join(t.TempDir(), tt.name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}

		e := newTestEditor(t, "#[x|]#")
		e.Syntax = syntax.NewLoader([]string{runtime}, langs)
		e.lspDisabled = true
		doc, err := e.Open(path, ActionReplace)
		if err != nil {
			t.Fatal(err)
		}

		e.feed(t, tt.keys)
		if got := strings.TrimRight(doc.Text().String(), "\n"); got != tt.want {
			t.Errorf("%s %s: got %q, want %q", tt.name, tt.keys, got, tt.want)
		}
	}
}

// TestTextObjectFromComment selects objects of the code around a
// comment while the cursor sits in the comment.
func TestTextObjectFromComment(t *testing.T) {
	runtime := filepath.Join("..", "..", "runtime")
	if _, err := os.Stat(filepath.Join(runtime, "grammars", "go.so")); err != nil {
		t.Skip("grammars not built")
	}

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct{ src, keys, want string }{
		{"package m\n\n// doc\nvar x = 1\n", "jjlmacd", "package m\n\n\nvar x = 1\n"},
		{"package m\n\nfunc f() {\n\t// note\n\tg()\n}\n", "jjjllmafd", "package m\n\n\n"},
	}
	for _, tt := range tests {
		path := filepath.Join(t.TempDir(), "a.go")
		if err := os.WriteFile(path, []byte(tt.src), 0o644); err != nil {
			t.Fatal(err)
		}

		e := newTestEditor(t, "#[x|]#")
		e.Syntax = syntax.NewLoader([]string{runtime}, langs)
		e.lspDisabled = true
		doc, err := e.Open(path, ActionReplace)
		if err != nil {
			t.Fatal(err)
		}

		e.feed(t, tt.keys)
		if got := doc.Text().String(); got != tt.want {
			t.Errorf("%q %s: got %q, want %q", tt.src, tt.keys, got, tt.want)
		}
	}
}
