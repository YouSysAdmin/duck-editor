package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

// writeFiles creates files with their texts in a new directory.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// TestQuitLastWindowKeepsOtherChanges refuses to quit through the last
// window while another buffer has unsaved changes.
func TestQuitLastWindowKeepsOtherChanges(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	for _, keys := range []string{"<C-w>q", ":wq<ret>", ":q<ret>"} {
		e := newTestEditor(t, "#[x|]#")
		if _, err := e.Open(filepath.Join(dir, "b.txt"), ActionReplace); err != nil {
			t.Fatal(err)
		}

		e.feed(t, "iY<esc>")
		if _, err := e.Open(filepath.Join(dir, "a.txt"), ActionReplace); err != nil {
			t.Fatal(err)
		}

		e.feed(t, keys)
		if e.quit {
			t.Fatalf("%s quit with b.txt modified", keys)
		}
	}
}

// TestReloadDeletedFile keeps the text of a buffer whose file is gone
// and counts it as unsaved.
func TestReloadDeletedFile(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"d.txt": "keep me\n"})
	path := filepath.Join(dir, "d.txt")
	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	e.feed(t, ":reload<ret>")
	if doc.Text().String() != "keep me\n" || !e.Status().IsError {
		t.Fatalf("reload of a deleted file: %q status %+v", doc.Text().String(), e.Status())
	}

	e.CheckDiskChanges()
	if !doc.IsModified() || !strings.Contains(e.Status().Text, "deleted") {
		t.Fatalf("deleted file: modified %v status %+v", doc.IsModified(), e.Status())
	}

	e.feed(t, ":q<ret>")
	if e.quit {
		t.Fatal("quit dropped the text of a deleted file")
	}
}

// TestOlderMtimeCountsAsChange notices a file replaced by a copy that
// keeps an older modification time.
func TestOlderMtimeCountsAsChange(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"m.txt": "mine\n"})
	path := filepath.Join(dir, "m.txt")
	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	e.feed(t, "ix<esc>:w<ret>")
	if data, _ := os.ReadFile(path); string(data) != "theirs\n" {
		t.Fatalf("the external change was overwritten: %q", data)
	}

	if !doc.ChangedOnDisk() {
		t.Fatal("an older time is not seen as a change")
	}
}

// TestWritePathTarget makes :w paths absolute, refuses existing files
// without ! and keeps the old path when the write cannot happen.
func TestWritePathTarget(t *testing.T) {
	dir := writeFiles(t, map[string]string{"b.txt": "b\n", "precious.txt": "old\n", "sub/x": ""})
	t.Parallel()

	e := newTestEditor(t, "#[x|]#")
	e.workDir = dir
	doc, err := e.Open(filepath.Join(dir, "b.txt"), ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	e.feed(t, ":w precious.txt<ret>")
	if data, _ := os.ReadFile(filepath.Join(dir, "precious.txt")); string(data) != "old\n" || doc.Path() != filepath.Join(dir, "b.txt") {
		t.Fatalf("existing file: %q, path %q", data, doc.Path())
	}

	e.feed(t, ":w sub<ret>")
	if doc.Path() != filepath.Join(dir, "b.txt") || !e.Status().IsError {
		t.Fatalf("directory: path %q status %+v", doc.Path(), e.Status())
	}

	e.feed(t, ":w new.txt<ret>")
	if doc.Path() != filepath.Join(dir, "new.txt") {
		t.Fatalf("new path %q", doc.Path())
	}

	e.feed(t, ":w! precious.txt<ret>")
	if data, _ := os.ReadFile(filepath.Join(dir, "precious.txt")); string(data) != "b\n" {
		t.Fatalf(":w! wrote %q", data)
	}
}

// TestWriteWithoutNameKeepsText leaves the text alone when there is no
// file to write.
func TestWriteWithoutNameKeepsText(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[x|]#")
	e.NewScratch(ActionReplace)
	e.feed(t, "ihi<esc>:w<ret>")
	if got := e.Doc().Text().String(); got != "hi" || e.Status().Text != errNoFile.Error() {
		t.Fatalf("text became %q", got)
	}
}

// TestCloseBufferShowsAnother puts another open buffer in the window of
// a closed one instead of an empty scratch buffer.
func TestCloseBufferShowsAnother(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	e := newTestEditor(t, "#[x|]#")
	b, _ := e.Open(filepath.Join(dir, "b.txt"), ActionLoad)
	if _, err := e.Open(filepath.Join(dir, "a.txt"), ActionReplace); err != nil {
		t.Fatal(err)
	}

	e.CloseDocument(1, true)
	e.feed(t, ":bc<ret>")
	if e.Doc() != b {
		t.Fatalf("after :bc the window shows %q", e.Doc().displayName())
	}
}

// TestPipedDocumentIsUnsaved asks before quitting with piped text.
func TestPipedDocumentIsUnsaved(t *testing.T) {
	t.Parallel()

	if !NewPipedDocument("log line\n").IsModified() || NewPipedDocument("").IsModified() {
		t.Fatal("piped text should count as unsaved, an empty pipe not")
	}
}

// TestByteOrderMark keeps the mark out of the text and in the file.
func TestByteOrderMark(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"bom.txt": "\uFEFFhello\n"})
	path := filepath.Join(dir, "bom.txt")
	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil || doc.Text().String() != "hello\n" {
		t.Fatalf("text %q %v", doc.Text().String(), err)
	}

	e.feed(t, "ix<esc>:w<ret>")
	if data, _ := os.ReadFile(path); string(data) != "\uFEFFxhello\n" {
		t.Fatalf("saved %q", data)
	}
}

// TestWholeFileEditKeepsCursor diffs a server edit that replaces the
// whole file, the cursor stays on its line.
func TestWholeFileEditKeepsCursor(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"a.py": "def greet():\n    pass\n\n\ngreet()\n"})
	path := filepath.Join(dir, "a.py")
	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	doc.SetSelection(edit.PointSelection(doc.Text().LineToChar(4) + 2))
	we := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{lsp.PathToURI(path): {{
		Range:   lsp.Range{Start: lsp.Position{}, End: lsp.Position{Line: 5}},
		NewText: "def hello():\n    pass\n\n\nhello()\n",
	}}}}
	if err := e.applyWorkspaceEdit(&lsp.Client{}, we); err != nil {
		t.Fatal(err)
	}

	if line := doc.Selection().Primary().CursorLine(doc.Text()); line != 4 || doc.Text().String() != "def hello():\n    pass\n\n\nhello()\n" {
		t.Fatalf("cursor line %d text %q", line, doc.Text().String())
	}
}

// TestWorkspaceDiagnosticsOfClosedFiles lists what a server reported for
// files that are not open.
func TestWorkspaceDiagnosticsOfClosedFiles(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[x|]#")
	path := filepath.Join(t.TempDir(), "other.go")
	e.handleDiagnostics(&lsp.Client{}, lsp.PublishDiagnosticsParams{URI: lsp.PathToURI(path), Diagnostics: []lsp.Diagnostic{{
		Range: lsp.Range{Start: lsp.Position{Line: 4}}, Severity: lsp.SeverityError, Message: "undefined: x",
	}}})
	e.Run("workspace_diagnostics_picker")
	if e.Picker == nil || len(e.Picker.Items()) != 1 || e.Picker.Items()[0].Line != 4 || e.Picker.Title != "Workspace diagnostics" {
		t.Fatalf("picker %+v", e.Picker)
	}
}

// TestStderrReason picks the line of a crash output that explains it.
func TestStderrReason(t *testing.T) {
	t.Parallel()

	for out, want := range map[string]string{
		"b/bundler/worker.rb:54:in\n'Kernel#loop'\n\nAn error occurred while installing mysql2 (0.5.6), and Bundler cannot continue.\nIn Gemfile:": "An error occurred while installing mysql2 (0.5.6), and Bundler cannot continue.",
		"rbenv: version `4.0.6' is not installed (set by /x/.ruby-version)":                                                                        "rbenv: version `4.0.6' is not installed (set by /x/.ruby-version)",
		"starting\npanic in handler": "panic in handler",
	} {
		if got := stderrReason(out); got != want {
			t.Errorf("stderrReason(%q) = %q, want %q", out, got, want)
		}
	}
}
