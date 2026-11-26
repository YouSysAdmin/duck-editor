package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// explorerTree creates files below a temporary directory, a name ending
// in a slash is a directory.
func explorerTree(t *testing.T, names ...string) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}

			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// feedJobs feeds keys and runs the jobs they queued.
func (e *Editor) feedJobs(t *testing.T, keys string) {
	t.Helper()

	e.feed(t, keys)
	runJobs(e)
}

func pickerLabels(e *Editor) []string {
	var out []string
	for _, m := range e.Picker.Matches() {
		out = append(out, e.Picker.Items()[m.Index].Label)
	}

	return out
}

func selectedLabel(e *Editor) string {
	item, _ := e.Picker.Selected()

	return item.Label
}

// TestExplorerSelectsBufferFile opens space . on the file of the buffer.
func TestExplorerSelectsBufferFile(t *testing.T) {
	t.Parallel()

	dir := explorerTree(t, "a.txt", "b.txt", "c.txt")
	e := newTestEditor(t, "#[x|]#")
	if _, err := e.Open(filepath.Join(dir, "b.txt"), ActionReplace); err != nil {
		t.Fatal(err)
	}

	e.execute(&Context{Editor: e}, "file_explorer_in_current_buffer_directory")
	runJobs(e)
	if got := selectedLabel(e); got != "b.txt" {
		t.Fatalf("selected %q", got)
	}
}

// TestExplorerPathTyping moves through directories with a typed slash
// and remembers the entry selected in a directory.
func TestExplorerPathTyping(t *testing.T) {
	t.Parallel()

	dir := explorerTree(t, "src/main/app.go", "src/lib/x.go", "src/lib/y.go", "docs/")
	e := newTestEditor(t, "#[x|]#")
	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)

	// A prefix of a directory and a full name both step in, the filter
	// starts empty there.
	e.feedJobs(t, "sr/lib/")
	if e.Picker.explorer.dir != filepath.Join(dir, "src", "lib") || e.Picker.Query() != "" {
		t.Fatalf("in %q with %q", e.Picker.explorer.dir, e.Picker.Query())
	}

	e.feed(t, "<down><down>")
	if got := selectedLabel(e); got != "y.go" {
		t.Fatalf("selected %q", got)
	}

	// .. goes up with the directory left selected.
	e.feedJobs(t, "../")
	if got := selectedLabel(e); got != "lib/" {
		t.Fatalf("after going up %q in %q", got, pickerLabels(e))
	}

	// Coming back selects the entry chosen there before.
	e.feedJobs(t, "<A-l>")
	if got := selectedLabel(e); got != "y.go" {
		t.Fatalf("remembered %q", got)
	}

	// A slash on its own goes to the root, a pasted path walks down.
	e.feedJobs(t, "/")
	if e.Picker.explorer.dir != "/" {
		t.Fatalf("root %q", e.Picker.explorer.dir)
	}

	e.HandlePaste(dir + "/src/main/")
	runJobs(e)
	if e.Picker.explorer.dir != filepath.Join(dir, "src", "main") || !slices.Equal(pickerLabels(e), []string{"../", "app.go"}) {
		t.Fatalf("pasted into %q listing %q", e.Picker.explorer.dir, pickerLabels(e))
	}

	// A slash after a name that matches no directory stays in the filter.
	e.feed(t, "zz/")
	if e.Picker.Query() != "zz/" {
		t.Fatalf("query %q", e.Picker.Query())
	}
}

// TestExplorerTabEntersDirectory enters the selected directory with tab
// while filtering, without a filter tab moves down.
func TestExplorerTabEntersDirectory(t *testing.T) {
	t.Parallel()

	dir := explorerTree(t, "alpha/a.txt", "beta/b.txt")
	e := newTestEditor(t, "#[x|]#")
	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)
	e.feed(t, "<tab>")
	if got := selectedLabel(e); got != "alpha/" {
		t.Fatalf("tab without filter selected %q", got)
	}

	e.feedJobs(t, "bet<tab>")
	if e.Picker.explorer.dir != filepath.Join(dir, "beta") {
		t.Fatalf("tab went to %q", e.Picker.explorer.dir)
	}
}

// TestExplorerCreateFromFilter opens a new file named by a filter that
// matches nothing and creates a directory for a name ending in a slash.
func TestExplorerCreateFromFilter(t *testing.T) {
	t.Parallel()

	dir := explorerTree(t, "a.txt")
	e := newTestEditor(t, "#[x|]#")
	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)
	e.feedJobs(t, "newdir/<ret>")
	if info, err := os.Stat(filepath.Join(dir, "newdir")); err != nil || !info.IsDir() {
		t.Fatalf("newdir not created: %v", err)
	}

	if e.Picker == nil || e.Picker.explorer.dir != filepath.Join(dir, "newdir") {
		t.Fatalf("picker %+v", e.Picker)
	}

	e.feedJobs(t, "note.md<ret>")
	if e.Picker != nil || e.Doc().Path() != filepath.Join(dir, "newdir", "note.md") {
		t.Fatalf("opened %q", e.Doc().Path())
	}
}

// TestExplorerFileOperations creates, renames, copies and deletes
// entries, an open buffer follows its renamed file.
func TestExplorerFileOperations(t *testing.T) {
	t.Parallel()

	dir := explorerTree(t, "a.txt", "keep/")
	e := newTestEditor(t, "#[x|]#")
	if _, err := e.Open(filepath.Join(dir, "a.txt"), ActionReplace); err != nil {
		t.Fatal(err)
	}

	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)

	e.feedJobs(t, "<A-n>sub/new.txt<ret>")
	if _, err := os.Stat(filepath.Join(dir, "sub", "new.txt")); err != nil {
		t.Fatal(err)
	}

	if e.Picker == nil || selectedLabel(e) != "sub/" {
		t.Fatalf("after create %+v", e.Picker)
	}

	e.feedJobs(t, "a.txt<A-r><C-u>b.txt<ret>")
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}

	if e.Doc().Path() != filepath.Join(dir, "b.txt") {
		t.Fatalf("buffer still at %q", e.Doc().Path())
	}

	if selectedLabel(e) != "b.txt" {
		t.Fatalf("after rename %q", selectedLabel(e))
	}

	// Renaming onto a directory moves into it.
	e.feedJobs(t, "<A-c><C-u>c.txt<ret>")
	e.feedJobs(t, "c.txt<A-r><C-u>keep<ret>")
	if _, err := os.Stat(filepath.Join(dir, "keep", "c.txt")); err != nil {
		t.Fatal(err)
	}

	// An existing target is refused.
	e.feedJobs(t, "b.txt<A-c><C-u>b.txt<ret>")
	e.feedJobs(t, "b.txt<A-r><C-u>sub/new.txt<ret>")
	if st := e.Status(); !st.IsError || !strings.Contains(st.Text, "exists") {
		t.Fatalf("status %+v", st)
	}

	// Declining keeps the entry, y deletes it with what is below.
	e.feedJobs(t, "<esc>")
	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)
	e.feedJobs(t, "sub<C-x>n")
	if _, err := os.Stat(filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}

	e.feedJobs(t, "<C-w>sub<C-x>y")
	if _, err := os.Stat(filepath.Join(dir, "sub")); !os.IsNotExist(err) {
		t.Fatalf("sub still there: %v", err)
	}

	if e.Picker == nil || slices.Contains(pickerLabels(e), "sub/") {
		t.Fatalf("after delete %q", pickerLabels(e))
	}
}

// TestExplorerMarks opens and moves marked entries together.
func TestExplorerMarks(t *testing.T) {
	t.Parallel()

	dir := explorerTree(t, "a.txt", "b.txt", "c.txt", "dest/")
	e := newTestEditor(t, "#[x|]#")
	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)
	e.feed(t, "a.txt<A-space><C-w>c.txt<A-space>")
	if e.Picker.MarkCount() != 2 {
		t.Fatalf("marked %d", e.Picker.MarkCount())
	}

	e.feedJobs(t, "<A-r><C-u>dest<ret>")
	for _, name := range []string{"a.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, "dest", name)); err != nil {
			t.Fatal(err)
		}
	}

	if e.Picker.MarkCount() != 0 {
		t.Fatalf("marks left %d", e.Picker.MarkCount())
	}

	e.feedJobs(t, "dest/")
	e.feed(t, "a.txt<A-space><C-w>c.txt<A-space><ret>")
	var paths []string
	for _, doc := range e.Documents() {
		paths = append(paths, doc.Path())
	}

	if !slices.Contains(paths, filepath.Join(dir, "dest", "a.txt")) || e.Doc().Path() != filepath.Join(dir, "dest", "c.txt") {
		t.Fatalf("opened %q showing %q", paths, e.Doc().Path())
	}
}

// TestExplorerRecursiveToggle switches to the file picker of the
// directory and back, the filter stays.
func TestExplorerRecursiveToggle(t *testing.T) {
	t.Parallel()

	dir := explorerTree(t, "src/deep/target.go", "other.txt")
	e := newTestEditor(t, "#[x|]#")
	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)
	e.feedJobs(t, "targ<C-r>")
	if e.Picker == nil || e.Picker.explorer != nil || e.Picker.Query() != "targ" {
		t.Fatalf("picker %+v", e.Picker)
	}

	if got := pickerLabels(e); !slices.Equal(got, []string{filepath.Join("src", "deep", "target.go")}) {
		t.Fatalf("matches %q", got)
	}

	e.feedJobs(t, "<C-r>")
	if e.Picker == nil || e.Picker.explorer == nil || e.Picker.Query() != "targ" {
		t.Fatalf("back %+v", e.Picker)
	}
}

// TestExplorerGitStatus marks the changed entries of a repository.
func TestExplorerGitStatus(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	dir := explorerTree(t, "kept.txt", "changed.txt")
	git := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "changed.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "added.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	e.vcsDisabled = false
	fileExplorer(&Context{Editor: e}, dir)
	runJobs(e)
	got := map[string]rune{}
	for _, item := range e.Picker.Items() {
		got[item.Label] = item.Status
	}

	if got["changed.txt"] != '~' || got["added.txt"] != '+' || got["kept.txt"] != 0 {
		t.Fatalf("status %q", got)
	}
}
