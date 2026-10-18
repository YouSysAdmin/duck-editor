package editorconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".editorconfig", `
root = true

[*]
indent_style = space
indent_size = 4
end_of_line = lf
insert_final_newline = true

[*.go]
indent_style = tab
tab_width = 8

[{Makefile,*.mk}]
indent_style = tab

[lib/**.js]
indent_size = 2
max_line_length = 100
`)
	write("sub/.editorconfig", "[*.go]\ntrim_trailing_whitespace = true\nindent_size = 2\n")

	goProps := Resolve(nil, filepath.Join(dir, "sub", "main.go"))
	if goProps.IndentStyle != "tab" || goProps.TabWidth != 8 || goProps.IndentSize != 2 || goProps.TrimTrailingWhitespace == nil || !*goProps.TrimTrailingWhitespace {
		t.Fatalf("go props = %+v", goProps)
	}

	py := Resolve(nil, filepath.Join(dir, "a.py"))
	if py.IndentStyle != "space" || py.IndentSize != 4 || py.EndOfLine != "lf" || py.InsertFinalNewline == nil || !*py.InsertFinalNewline || py.TrimTrailingWhitespace != nil {
		t.Fatalf("py props = %+v", py)
	}

	js := Resolve(nil, filepath.Join(dir, "lib", "x", "y.js"))
	if js.IndentSize != 2 || js.MaxLineLength != 100 {
		t.Fatalf("js props = %+v", js)
	}

	if Resolve(nil, filepath.Join(dir, "Makefile")).IndentStyle != "tab" || Resolve(nil, filepath.Join(dir, "x.mk")).IndentStyle != "tab" {
		t.Fatal("brace alternatives")
	}

	if Resolve(nil, filepath.Join(dir, "other", "y.js")).IndentSize != 4 {
		t.Fatal("lib glob must not match other dirs")
	}
}
