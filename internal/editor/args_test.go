package editor

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want []string
	}{
		{`a  b`, []string{"a", "b"}},
		{`"my file.txt" x`, []string{"my file.txt", "x"}},
		{`'it''s' 'a"b'`, []string{"its", `a"b`}},
		{`my\ file`, []string{"my file"}},
		{`"say \"hi\""`, []string{`say "hi"`}},
		{`''`, []string{""}},
		{`%sh{echo a b} c`, []string{"%sh{echo a b}", "c"}},
		{`'100%' 50%%`, []string{"100%%", "50%%"}},
		{`\%x`, []string{"%%x"}},
	}
	for _, tt := range tests {
		got, err := splitArgs(tt.in)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("splitArgs(%q) = %q, %v want %q", tt.in, got, err, tt.want)
		}
	}

	for _, bad := range []string{`"open`, `'open`, `%sh{x`} {
		if _, err := splitArgs(bad); err == nil {
			t.Errorf("splitArgs(%q) should fail", bad)
		}
	}

	// An escaped word reads back as itself.
	e := newTestEditor(t, "#[a|]#")
	for _, word := range []string{"my file", `a\b`, "100%", `it's`} {
		got, err := splitArgs(escapeArg(word))
		if err != nil || len(got) != 1 {
			t.Fatalf("escape of %q split into %q, %v", word, got, err)
		}

		if back, err := e.expand(got[0]); err != nil || back != word || unescapeArg(escapeArg(word)) != word {
			t.Errorf("escape round trip of %q gave %q, %v", word, back, err)
		}
	}
}

// TestOpenPathWithSpaces opens a quoted path and completes one with
// escaped spaces.
func TestOpenPathWithSpaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "my notes.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	e.runTyped(&Context{Editor: e}, `o "`+path+`"`)
	if e.Doc().Path() != path {
		t.Fatalf("opened %q, status %+v", e.Doc().Path(), e.Status())
	}

	comps := e.completeTyped("o " + escapeArg(dir) + "/my\\ no")
	if len(comps) != 1 || comps[0].Text != escapeArg(path) {
		t.Fatalf("completions %+v", comps)
	}
}

// TestCdCompletesDirectories proposes directories and links to them,
// never files.
func TestCdCompletesDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "releases"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "revisions.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("releases", filepath.Join(dir, "current")); err != nil {
		t.Skip("symlinks unsupported")
	}

	e := newTestEditor(t, "#[a|]#")
	var got []string
	for _, c := range e.completeTyped("cd " + escapeArg(dir) + "/") {
		got = append(got, filepath.Base(c.Text)+"/")
	}

	slices.Sort(got)
	if !slices.Equal(got, []string{"current/", "releases/"}) {
		t.Fatalf("completions %v", got)
	}
}

// TestOpenTildePath opens ~/ paths in the home directory, not in a
// directory named ~ under the working directory.
func TestOpenTildePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "duck", "workspaces.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("dev:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []string{"o", "vs"} {
		e := newTestEditor(t, "#[a|]#")
		e.runTyped(&Context{Editor: e}, cmd+" ~/.config/duck/workspaces.yaml")
		if e.Doc().Path() != path || e.Doc().Text().String() != "dev:\n" {
			t.Fatalf(":%s opened %q with %q, status %+v", cmd, e.Doc().Path(), e.Doc().Text().String(), e.Status())
		}
	}
}

// TestTabIntoDirectory lists the entries of a directory accepted with Tab
// when it was the only candidate, several candidates still cycle.
func TestTabIntoDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, f := range []string{".config/duck/config.yaml", ".config/duck/workspaces.yaml", ".config/dunst/dunstrc"} {
		path := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[a|]#")
	e.feed(t, ":o ~/.config/duc<tab>")
	p := e.Prompt
	if p.Input() != "o ~/.config/duck/" || !slices.Equal(p.Completions(), []string{"~/.config/duck/config.yaml", "~/.config/duck/workspaces.yaml"}) {
		t.Fatalf("after tab: %q %v", p.Input(), p.Completions())
	}

	e.feed(t, "<esc>:o ~/.config/du<tab>")
	p = e.Prompt
	first := p.Input()
	e.feed(t, "<tab>")
	if p.Input() == first || len(p.Completions()) != 2 {
		t.Fatalf("several candidates should cycle: %q then %q, %v", first, p.Input(), p.Completions())
	}
}

// TestEnterOnDirectoryCompletion descends into a directory chosen with
// Tab instead of opening it, and :o on a directory opens the explorer.
func TestEnterOnDirectoryCompletion(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	// :cd below moves the process, the directory comes back afterwards.
	t.Chdir(home)
	for _, f := range []string{".config/duck/themes/my.yaml", ".config/duck/config.yaml"} {
		path := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[a|]#")
	e.feed(t, ":o ~/.config/duck/")
	p := e.Prompt
	for p.Input() != "o ~/.config/duck/themes/" {
		e.feed(t, "<tab>")
		if p.Selected() == len(p.Completions())-1 && p.Input() != "o ~/.config/duck/themes/" {
			t.Fatalf("themes/ not offered: %v", p.Completions())
		}
	}

	e.feed(t, "<ret>")
	if e.Prompt == nil || !slices.Equal(e.Prompt.Completions(), []string{"~/.config/duck/themes/my.yaml"}) {
		t.Fatalf("enter on a directory: prompt %v, status %+v", e.Prompt, e.Status())
	}

	e.feed(t, "<tab><ret>")
	if e.Prompt != nil || e.Doc().Path() != filepath.Join(home, ".config/duck/themes/my.yaml") {
		t.Fatalf("enter on a file opened %q", e.Doc().Path())
	}

	e.feed(t, ":o ~/.config/duck<ret>")
	if e.Picker == nil || e.Status().IsError {
		t.Fatalf(":o on a directory: picker %v status %+v", e.Picker, e.Status())
	}

	// For :cd the directory is the goal, Enter runs the command.
	e.Picker = nil
	e.feed(t, ":cd ~/.config/<tab><ret>")
	if e.Prompt != nil || e.WorkingDir() != filepath.Join(home, ".config/duck") {
		t.Fatalf(":cd went to %q", e.WorkingDir())
	}
}

// TestPromptOddInputs completes and runs odd command lines without a
// panic, as a lone space after the colon.
func TestPromptOddInputs(t *testing.T) {
	t.Parallel()

	inputs := []string{" ", "  ", "\t", " w", "o ", "o \"", "o '", "o \\", "set ", "set  x", "theme  ", "lang ", "%", "sh %", "goto", "goto 0", "goto -1", "0", "-1", "w --", "cd -", "echo %u{110000}", "o ~", "r", "sort --", "  q  "}
	for _, in := range inputs {
		e := newTestEditor(t, "#[a|]#")
		e.NewScratch(ActionReplace)
		e.feed(t, ":")
		for _, r := range in {
			e.Prompt.insert(&Context{Editor: e}, string(r))
		}

		_ = e.Prompt.Completions()
		e.feed(t, "<tab><ret>")
	}
}

// TestPromptEditingKeys covers C-w at slashes, word moves, C-r and
// Backspace on an empty prompt.
func TestPromptEditingKeys(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.feed(t, ":o sub/inner/<C-w>")
	if got := e.Prompt.Input(); got != "o sub/" {
		t.Fatalf("C-w left %q", got)
	}

	e.feed(t, "x<A-b><A-b>")
	if e.Prompt.Cursor() != 2 {
		t.Fatalf("A-b moved to %d", e.Prompt.Cursor())
	}

	e.feed(t, "<A-f>")
	if e.Prompt.Cursor() != 5 {
		t.Fatalf("A-f moved to %d", e.Prompt.Cursor())
	}

	e.feed(t, "<esc>")
	_ = e.writeRegister('a', []string{"value"})
	e.feed(t, ":echo <C-r>a")
	if got := e.Prompt.Input(); got != "echo value" {
		t.Fatalf("C-r a gave %q", got)
	}

	e.feed(t, "<esc>:<backspace>")
	if e.Prompt != nil {
		t.Fatal("backspace on an empty prompt keeps it open")
	}
}

// TestCompleteQuotedPath completes a path typed inside quotes.
func TestCompleteQuotedPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dir with space"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "dir with space", "f.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	e.workDir = dir
	comps := e.completeTyped(`o "dir with`)
	if len(comps) != 1 || comps[0].Text != `dir\ with\ space/` || comps[0].Start != 2 {
		t.Fatalf("completions %+v", comps)
	}

	comps = e.completeTyped(`o "dir with space/`)
	if len(comps) != 1 || comps[0].Text != `dir\ with\ space/f.txt` {
		t.Fatalf("inside the quoted dir %+v", comps)
	}
}

// TestSetRejectsBadValues keeps the old value when :set gets a value the
// option does not take.
func TestSetRejectsBadValues(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	for _, cmd := range []string{"set line-number garbage", "set scrolloff -5", "set default-yank-register ab", "set jump-label-alphabet a"} {
		before := e.Config.Editor
		e.runTyped(&Context{Editor: e}, cmd)
		if !e.Status().IsError || e.Config.Editor.LineNumber != before.LineNumber || e.Config.Editor.Scrolloff != before.Scrolloff {
			t.Fatalf(":%s: status %+v", cmd, e.Status())
		}
	}

	e.SetStatus("")
	e.runTyped(&Context{Editor: e}, "set line-number relative")
	if e.Config.Editor.LineNumber != "relative" || e.Status().IsError {
		t.Fatalf("valid value refused: %+v", e.Status())
	}
}
