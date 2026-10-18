package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// runJobs runs queued jobs to completion, including the ones their
// results queue.
func runJobs(e *Editor) {
	for jobs := e.TakeJobs(); len(jobs) > 0; jobs = e.TakeJobs() {
		for _, job := range jobs {
			job.Run(job.Ctx, func(r JobResult) { e.ApplyResult(r) })
		}
	}
}

func TestInlineBlame(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	git("init", "-q")
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	git("add", "a.txt")
	git("-c", "user.name=Ann", "-c", "user.email=ann@example.com", "commit", "-q", "-m", "first lines")

	e := newTestEditor(t, "#[x|]#")
	e.Config.Editor.InlineBlame = true
	e.vcsDisabled = false
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	runJobs(e)
	e.feed(t, "j")
	runJobs(e)
	line, text, ok := e.InlineBlame(doc)
	if !ok || line != 1 || !strings.HasPrefix(text, "Ann, ") || !strings.HasSuffix(text, "first lines") {
		t.Fatalf("blame = %d %q %v", line, text, ok)
	}

	e.feed(t, "oz<esc>")
	runJobs(e)
	if _, text, _ := e.InlineBlame(doc); text != "Not committed yet" {
		t.Fatalf("new line blame = %q", text)
	}

	e.Config.Editor.InlineBlame = false
	if _, _, ok := e.InlineBlame(doc); ok {
		t.Fatal("the option turns inline blame off")
	}
}

// TestBranchRefreshOnFocus picks up a branch switched in another program.
func TestBranchRefreshOnFocus(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	git("init", "-q", "-b", "main")
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	git("add", "a.txt")
	git("-c", "user.name=Ann", "-c", "user.email=ann@example.com", "commit", "-q", "-m", "first")

	e := newTestEditor(t, "#[x|]#")
	e.vcsDisabled = false
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	runJobs(e)
	if doc.Branch() != "main" {
		t.Fatalf("branch %q", doc.Branch())
	}

	git("checkout", "-q", "-b", "feature")
	e.FocusGained()
	runJobs(e)
	if doc.Branch() != "feature" {
		t.Fatalf("branch after focus %q", doc.Branch())
	}
}

// TestChangedFilePreviewDiff previews the changes of a file, unsaved
// edits of an open buffer included.
func TestChangedFilePreviewDiff(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	git("init", "-q")
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	git("add", "a.txt")
	git("-c", "user.name=Ann", "-c", "user.email=ann@example.com", "commit", "-q", "-m", "first")
	if err := os.WriteFile(path, []byte("one\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	e.vcsDisabled = false
	e.workDir = dir
	e.Run("changed_file_picker")
	runJobs(e)
	if e.Picker == nil || len(e.Picker.Items()) != 1 || !e.Picker.Items()[0].Diff {
		t.Fatalf("picker %+v status %+v", e.Picker, e.Status())
	}

	want := "@@ -1,2 +1,2 @@\n one\n-two\n+2\n"
	e.PreparePreview(path, true)
	if _, ok := e.Preview(path, true); ok {
		t.Fatal("the preview is ready before its job ran")
	}

	runJobs(e)
	p, ok := e.Preview(path, true)
	if !ok || p.Doc == nil || p.Doc.Text().String() != want {
		t.Fatalf("diff preview %v %+v", ok, p)
	}
}

// TestPreviewAfterLabel highlights a preview whose file was first read
// for a location label.
func TestPreviewAfterLabel(t *testing.T) {
	t.Parallel()

	runtime := filepath.Join("..", "..", "runtime")
	if _, err := os.Stat(filepath.Join(runtime, "grammars", "go.so")); err != nil || !syntax.Supported {
		t.Skip("no compiled go grammar")
	}

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package a\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	e.Syntax = syntax.NewLoader([]string{runtime}, langs)
	if doc := e.PreviewDocument(path); doc == nil || doc.Syntax() != nil {
		t.Fatal("a label read should not parse")
	}

	e.PreparePreview(path, false)
	p, ok := e.Preview(path, false)
	if !ok || p.Doc.Syntax() == nil || len(p.Doc.Highlights(0, p.Doc.Text().Len())) == 0 {
		t.Fatal("the preview of a file read for a label has no highlighting")
	}
}
