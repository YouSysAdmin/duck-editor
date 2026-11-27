package editor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

func TestParseLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want location
		ok   bool
	}{
		{"main.go:12:5: undefined: x", location{"/w/main.go", 12, 5, "undefined: x"}, true},
		{"./pkg/a.go:3: missing return", location{"/w/pkg/a.go", 3, 0, "missing return"}, true},
		{"/abs/f.py:7:1", location{"/abs/f.py", 7, 1, ""}, true},
		{"src/x.ts:40:2 - error TS2304", location{"/w/src/x.ts", 40, 2, "error TS2304"}, true},
		{"make: *** [all] Error 1", location{}, false},
		{"http://example.com:80/x", location{}, false},
	}
	for _, tt := range tests {
		got, ok := parseLocation(tt.in, "/w")
		if ok != tt.ok || ok && got != tt.want {
			t.Errorf("parseLocation(%q) = %+v %v want %+v %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// TestMakeAndPickFrom collects the places a command prints, steps
// through them and picks from the lines of another command.
func TestMakeAndPickFrom(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, text := range map[string]string{"a.txt": "one\ntwo\nthree\n", "b.txt": "x\ny\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEditor(t, "#[a|]#")
	e.workDir = dir
	cx := &Context{Editor: e}
	e.runTyped(cx, `make printf 'a.txt:2:2: first\nnoise\nb.txt:1: second\n' >&2; exit 2`)
	runJobs(e)
	if len(e.quickfix) != 2 || e.Doc().Path() != filepath.Join(dir, "a.txt") {
		t.Fatalf("quickfix %+v doc %q status %+v", e.quickfix, e.Doc().Path(), e.Status())
	}

	if got := e.annotated(); got != "one\nt#[w|]#o\nthree\n" || e.Status().Text != "[1/2] first" {
		t.Fatalf("first place %q status %q", got, e.Status().Text)
	}

	e.feed(t, "]q")
	if e.Doc().Path() != filepath.Join(dir, "b.txt") || e.Status().Text != "[2/2] second" {
		t.Fatalf("next place %q status %q", e.Doc().Path(), e.Status().Text)
	}

	e.feed(t, "]q")
	if e.Doc().Path() != filepath.Join(dir, "a.txt") {
		t.Fatalf("wrap to %q", e.Doc().Path())
	}

	e.runTyped(cx, "pick-from printf 'b.txt\\na.txt:3\\n'")
	runJobs(e)
	if e.Picker == nil || len(e.Picker.Items()) != 2 {
		t.Fatalf("picker %+v status %+v", e.Picker, e.Status())
	}

	e.feed(t, "a.txt<ret>")
	if got := e.annotated(); got != "one\ntwo\n#[t|]#hree\n" {
		t.Fatalf("picked %q", got)
	}
}

// TestYankHistory keeps yanks newest first without repeats and pastes
// the picked one.
func TestYankHistory(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[one|]# two")
	yankRange := func(from, to int) {
		e.Doc().SetSelection(edit.SingleSelection(from, to))
		e.feed(t, "y")
	}
	yankRange(0, 3)
	yankRange(4, 7)
	yankRange(0, 3)
	if len(e.yankRing) != 2 || e.yankRing[0][0] != "one" || e.yankRing[1][0] != "two" {
		t.Fatalf("ring %q", e.yankRing)
	}

	e.feed(t, " \"")
	if e.Picker == nil || len(e.Picker.Items()) != 2 {
		t.Fatalf("picker %+v", e.Picker)
	}

	e.feed(t, "<down><ret>")
	if got := e.Doc().Text().String(); got != "onetwo two" {
		t.Fatalf("pasted %q", got)
	}

	if got := e.Registers['"']; len(got) != 1 || got[0] != "two" {
		t.Fatalf("yank register %q", got)
	}
}

// TestBase64 encodes and decodes selections and leaves the text alone
// when one selection is not base64.
func TestBase64(t *testing.T) {
	t.Parallel()

	check(t, "password: #[s3cret|]#\n", ":b64e<ret>", "password: #[czNjcmV0|]#\n")
	check(t, "a: #[czNjcmV0|]#\nb: #(aGk=|)#\n", ":b64d<ret>", "a: #[s3cret|]#\nb: #(hi|)#\n")
	check(t, "#[aGk|]#", ":b64d<ret>", "#[hi|]#")

	e := newTestEditor(t, "#[czNjcmV0|]# #(not base64!|)#")
	e.runTyped(&Context{Editor: e}, "b64d")
	if got := e.Doc().Text().String(); got != "czNjcmV0 not base64!" || !e.Status().IsError {
		t.Fatalf("partly invalid decode changed %q, status %+v", got, e.Status())
	}
}

// TestWorkspaceSwitch picks a workspace or an ssh host and ends the
// session for it, unless a buffer is unsaved.
func TestWorkspaceSwitch(t *testing.T) {
	t.Parallel()

	source := func() ([]config.Workspace, error) {
		return []config.Workspace{
			{Name: "worker", Host: "10.0.0.1", Path: "/srv/app", Description: "build box"},
			{Name: "notes", Path: "/srv/notes"},
		}, nil
	}
	hosts := func() []string { return []string{"worker", "mac-mini"} }
	e := newTestEditor(t, "#[a|]#")
	e.Workspaces, e.SSHHosts = source, hosts
	e.Place = config.Workspace{Path: "/srv/notes/"}
	e.feed(t, " W")
	if e.Picker == nil || len(e.Picker.Items()) != 2 {
		t.Fatalf("picker %+v", e.Picker)
	}

	items := e.Picker.Items()
	if !strings.Contains(items[0].Label, "build box") || !strings.HasPrefix(items[1].Label, "* notes") {
		t.Fatalf("the current workspace is not marked last: %q %q", items[0].Label, items[1].Label)
	}

	e.feed(t, "build<ret>")
	w := e.TakeNextWorkspace()
	if w == nil || w.Name != "worker" || !e.ShouldQuit() {
		t.Fatalf("switch to %+v, quit %v", w, e.ShouldQuit())
	}

	e = newTestEditor(t, "#[a|]#")
	e.Workspaces, e.SSHHosts = source, hosts
	e.feed(t, " H")
	if e.Picker == nil || len(e.Picker.Items()) != 2 {
		t.Fatalf("host picker %+v", e.Picker)
	}

	e.feed(t, "mac<ret>")
	w = e.TakeNextWorkspace()
	if w == nil || w.Host != "mac-mini" || !e.ShouldQuit() {
		t.Fatalf("switch to host %+v, quit %v", w, e.ShouldQuit())
	}

	e = newTestEditor(t, "#[a|]#")
	e.Workspaces, e.SSHHosts = source, hosts
	e.runTyped(&Context{Editor: e}, "ws mac-mini")
	if e.TakeNextWorkspace() != nil || !e.Status().IsError {
		t.Fatalf(":ws took an ssh host: %+v", e.Status())
	}

	e = newTestEditor(t, "#[a|]#")
	e.Workspaces = source
	e.feed(t, "ix<esc>")
	e.runTyped(&Context{Editor: e}, "ws worker")
	if e.TakeNextWorkspace() != nil || e.ShouldQuit() || !e.Status().IsError {
		t.Fatalf("an unsaved buffer did not stop the switch: %+v", e.Status())
	}
}

// TestWorkspaceProjectList shows the project workspaces alone when the
// project has some, C-i adds those of the user.
func TestWorkspaceProjectList(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.Workspaces = func() ([]config.Workspace, error) {
		return []config.Workspace{{Name: "ansible", Path: "/p/ansible", Project: true}, {Name: "mine", Path: "/srv"}}, nil
	}
	e.feed(t, " W")
	if e.Picker == nil || len(e.Picker.Items()) != 1 || !strings.Contains(e.Picker.Items()[0].Label, "ansible") {
		t.Fatalf("project picker %+v", e.Picker)
	}

	e.feed(t, "<C-i>")
	if len(e.Picker.Items()) != 2 || !strings.HasSuffix(e.Picker.Title, "(all)") || !strings.Contains(e.Picker.Items()[0].Label, "(project)") {
		t.Fatalf("all workspaces %q %+v", e.Picker.Title, e.Picker.Items())
	}

	e.feed(t, "mine<ret>")
	if w := e.TakeNextWorkspace(); w == nil || w.Name != "mine" {
		t.Fatalf("switch to %+v", w)
	}

	e = newTestEditor(t, "#[a|]#")
	e.Workspaces = func() ([]config.Workspace, error) { return []config.Workspace{{Name: "mine", Path: "/srv"}}, nil }
	e.feed(t, " W<C-i>")
	if len(e.Picker.Items()) != 1 || e.Picker.Title != "Workspaces" {
		t.Fatalf("user picker %q %+v", e.Picker.Title, e.Picker.Items())
	}
}

// TestWorkspaceAdd saves the place of the session to the workspaces
// file and refuses a name already there.
func TestWorkspaceAdd(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "workspaces.yaml")
	e := newTestEditor(t, "#[a|]#")
	e.WorkspacesFile = file
	e.runTyped(&Context{Editor: e}, "ws-add box")
	if !e.Status().IsError {
		t.Fatalf("a session without a place was saved: %+v", e.Status())
	}

	e.Place = config.Workspace{Docker: "app", User: "dev", Path: "/srv"}
	e.runTyped(&Context{Editor: e}, "ws-add box")
	ws, err := config.LoadWorkspaces(file)
	if err != nil || len(ws) != 1 || ws[0].Name != "box" || ws[0].Docker != "app" || ws[0].User != "dev" || ws[0].Path != e.WorkingDir() {
		t.Fatalf("workspaces %+v %v, status %+v", ws, err, e.Status())
	}

	if e.Place.Name != "box" {
		t.Fatalf("the place did not take the name: %+v", e.Place)
	}

	e.runTyped(&Context{Editor: e}, "ws-add box")
	if !e.Status().IsError {
		t.Fatalf("a duplicate name was saved: %+v", e.Status())
	}

	root := t.TempDir()
	e.ProjectWorkspacesFile = filepath.Join(root, ".duck", "workspaces.yaml")
	e.Place = config.Workspace{Path: root}
	e.workDir = filepath.Join(root, "ansible")
	e.runTyped(&Context{Editor: e}, "ws-add --project ans")
	ws, err = config.LoadWorkspaces(e.ProjectWorkspacesFile)
	if err != nil || len(ws) != 1 || ws[0].Path != "ansible" {
		t.Fatalf("project workspaces %+v %v, status %+v", ws, err, e.Status())
	}

	e.ProjectWorkspacesFile = ""
	e.runTyped(&Context{Editor: e}, "ws-add -p other")
	if !e.Status().IsError {
		t.Fatalf("--project without a project wrote somewhere: %+v", e.Status())
	}
}

// TestDiffHost compares the buffer with a file on another host and says
// so when they are the same.
func TestDiffHost(t *testing.T) {
	t.Parallel()

	other := filepath.Join(t.TempDir(), "nginx.conf")
	if err := os.WriteFile(other, []byte("worker 2\nlisten 80\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[w|]#orker 4\nlisten 80\n")
	e.Workspaces = func() ([]config.Workspace, error) { return nil, nil }
	e.SSHHosts = func() []string { return []string{"web2"} }
	connects := 0
	e.ConnectHost = func(context.Context, config.Workspace) (host.Host, error) {
		connects++

		return host.Local{}, nil
	}
	e.runTyped(&Context{Editor: e}, "dh web2 "+other)
	runJobs(e)
	want := "--- web2:" + other + "\n+++ test.txt\n@@ -1,2 +1,2 @@\n-worker 2\n+worker 4\n listen 80\n"
	if got := e.Doc().Text().String(); got != want || e.Doc().Title() != "diff:web2:"+other || !e.Doc().ReadOnly() {
		t.Fatalf("diff %q title %q, status %+v", got, e.Doc().Title(), e.Status())
	}

	if len(e.Views) != 2 {
		t.Fatalf("the diff opens in a split, %d views", len(e.Views))
	}

	// The same text again, through the kept connection.
	e.Switch(1, ActionReplace)
	replaceByLines(e.Doc(), "worker 2\nlisten 80\n")
	e.runTyped(&Context{Editor: e}, "dh web2 "+other)
	runJobs(e)
	if !strings.Contains(e.Status().Text, "is the same as") || connects != 1 {
		t.Fatalf("status %+v, %d connects", e.Status(), connects)
	}
}

// TestIndentTextObject selects a YAML block by indent, with its key for
// around.
func TestIndentTextObject(t *testing.T) {
	t.Parallel()

	yaml := "app:\n  image: nginx\n  ports:\n    - 80\n\n    - 443\n  env: prod\nnext: 1\n"
	check(t, strings.Replace(yaml, "- 80", "- #[8|]#0", 1), "mii",
		"app:\n  image: nginx\n  ports:\n#[    - 80\n\n    - 443\n|]#  env: prod\nnext: 1\n")
	check(t, strings.Replace(yaml, "- 80", "- #[8|]#0", 1), "mai",
		"app:\n  image: nginx\n#[  ports:\n    - 80\n\n    - 443\n|]#  env: prod\nnext: 1\n")
	check(t, strings.Replace(yaml, "image", "#[i|]#mage", 1), "mii",
		"app:\n#[  image: nginx\n  ports:\n    - 80\n\n    - 443\n  env: prod\n|]#next: 1\n")
}

func TestUniqRetabDeleteFile(t *testing.T) {
	t.Parallel()

	check(t, "#[b|]#\na\nb\nc\na\n", ":uniq<ret>", "#[b|]#\na\nc\n")
	check(t, "x\n#[b\nb\na\n|]#b\n", ":uniq<ret>", "x\n#[b\na\n|]#b\n")

	e := newTestEditor(t, "#[a|]#\n\tb\n  \t c\n   \nd\n")
	e.Doc().indent = IndentStyle{Width: 4}
	e.Doc().tabWidth = 4
	e.runTyped(&Context{Editor: e}, "retab")
	if got := e.Doc().Text().String(); got != "a\n    b\n     c\n\nd\n" {
		t.Fatalf("retab to spaces %q", got)
	}

	path := filepath.Join(t.TempDir(), "gone.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e = newTestEditor(t, "#[a|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	e.runTyped(&Context{Editor: e}, "delete-file")
	e.feed(t, "n")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("deleted without a yes")
	}

	e.runTyped(&Context{Editor: e}, "delete-file")
	e.feed(t, "y")
	if _, err := os.Stat(path); !os.IsNotExist(err) || e.Docs[doc.ID] != nil {
		t.Fatalf("file still there %v, buffer open %v", err, e.Docs[doc.ID] != nil)
	}
}

// TestSmallCommands covers G without a count, the text-width ruler and
// closing a buffer from the buffer picker.
func TestSmallCommands(t *testing.T) {
	t.Parallel()

	check(t, "#[a|]#\nb\nc\n", "G", "a\nb\n#[c|]#\n")
	check(t, "#[a|]#\nb\nc\n", "2G", "a\n#[b|]#\nc\n")

	e := newTestEditor(t, "#[a|]#")
	e.Config.Editor.Rulers = []int{100}
	e.Config.Editor.TextWidth = 72
	if got := e.Rulers(e.Doc()); !slices.Equal(got, []int{100}) {
		t.Fatalf("rulers without the option %v", got)
	}

	e.Config.Editor.TextWidthRuler = true
	if got := e.Rulers(e.Doc()); !slices.Equal(got, []int{100, 73}) || !slices.Equal(e.Config.Editor.Rulers, []int{100}) {
		t.Fatalf("rulers %v, config %v", got, e.Config.Editor.Rulers)
	}

	e = newTestEditor(t, "#[a|]#")
	e.NewScratch(ActionReplace)
	e.feed(t, " b")
	if e.Picker == nil || len(e.Picker.Items()) != 2 {
		t.Fatalf("buffer picker %+v", e.Picker)
	}

	e.feed(t, "<C-x>")
	if len(e.Docs) != 1 || len(e.Picker.Items()) != 1 {
		t.Fatalf("after C-x %d buffers, %d items", len(e.Docs), len(e.Picker.Items()))
	}
}

// TestPickerPreviews shows a preview pane for pickers of files, buffers
// or text such as the command palette, none for plain lists.
func TestPickerPreviews(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.feed(t, " ?")
	if e.Picker == nil || !e.Picker.Previews() {
		t.Fatal("the command palette shows no descriptions")
	}

	e.feed(t, "file_picker")
	item, _ := e.Picker.Selected()
	if !strings.Contains(item.Label, "space f") || !slices.Contains(item.Text, "Keys: space f") {
		t.Fatalf("file_picker item %q text %q", item.Label, item.Text)
	}

	e.feed(t, "<esc>")
	e.Workspaces = func() ([]config.Workspace, error) {
		return []config.Workspace{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}}, nil
	}
	e.feed(t, " W")
	if e.Picker == nil || e.Picker.Previews() {
		t.Fatal("a plain list shows a preview pane")
	}

	e.feed(t, "<esc> b")
	if e.Picker == nil || !e.Picker.Previews() {
		t.Fatal("buffer picker without a preview")
	}

	e.feed(t, "<C-t>")
	if e.Picker.Previews() {
		t.Fatal("C-t did not hide the preview")
	}
}
