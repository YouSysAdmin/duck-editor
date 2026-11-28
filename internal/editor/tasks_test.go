package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// taskProject writes a project with a tasks file and points e at it.
func taskProject(t *testing.T, e *Editor, tasks string) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".duck"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, ".duck", "tasks.yaml")
	if err := os.WriteFile(path, []byte(tasks), 0o644); err != nil {
		t.Fatal(err)
	}

	e.TasksFile = host.File{Path: path}

	return root
}

// TestTaskRunsInBackground starts tasks without showing their buffer,
// with error output merged in and the state shown in the picker.
func TestTaskRunsInBackground(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	taskProject(t, e, `hello:
  command: echo out; echo err >&2; echo $GREETING; pwd
  dir: sub
  env:
    GREETING: hi
fail: exit 3
`)
	before := e.Doc().ID
	e.runTyped(&Context{Editor: e}, "task hello")
	if e.Doc().ID != before {
		t.Fatal("starting a task switched the buffer")
	}

	doc := e.taskDoc("hello")
	if doc == nil || doc.Title() != "task:hello" || e.RunningTasks() != 1 {
		t.Fatalf("no running task buffer, status %+v", e.Status())
	}

	runJobs(e)
	text := doc.Text().String()
	if !strings.HasPrefix(text, "out\nerr\nhi\n") || !strings.HasSuffix(text, "/sub\n") {
		t.Fatalf("task output %q", text)
	}

	if doc.tail.exit != "ended" || e.RunningTasks() != 0 || !strings.Contains(e.Status().Text, "ended") {
		t.Fatalf("exit %q, status %+v", doc.tail.exit, e.Status())
	}

	e.runTyped(&Context{Editor: e}, "task fail")
	runJobs(e)
	if !e.Status().IsError || !strings.Contains(e.Status().Text, "exited 3") {
		t.Fatalf("a failing task reported %+v", e.Status())
	}

	// A second run appends to the same buffer.
	e.runTyped(&Context{Editor: e}, "task hello")
	runJobs(e)
	if got := strings.Count(doc.Text().String(), "out\n"); got != 2 {
		t.Fatalf("second run left %q", doc.Text().String())
	}

	if err := e.taskPicker(); err != nil {
		t.Fatal(err)
	}

	items := e.Picker.items
	if len(items) != 2 || items[0].Label != "hello" || items[0].Tag != "ended   " || items[0].TagKind != ui.TagDim || items[1].Tag != "exited 3" || items[1].TagKind != ui.TagBad {
		t.Fatalf("picker items %+v", items)
	}

	// Enter starts a task that does not run and the picker stays open
	// with the new state.
	picker := e.Picker
	e.Picker.accept(&Context{Editor: e}, PickerOpen)
	if e.Picker != picker || e.Picker.items[0].Tag != "running " || e.Picker.items[0].TagKind != ui.TagGood {
		t.Fatalf("picker after enter %v, items %+v", e.Picker != nil, e.Picker)
	}

	// Enter on a running task stops it, the picker stays.
	e.Picker.accept(&Context{Editor: e}, PickerOpen)
	if e.Picker != picker || e.Picker.items[0].Tag != "stopped " || doc.tail.cancel != nil {
		t.Fatalf("enter on a running task should stop it, items %+v", e.Picker.items)
	}

	// The failed task shows its output.
	e.Picker.move(1)
	e.Picker.output(&Context{Editor: e}, e.Picker.items[1])
	if e.Picker != nil || e.taskDoc("fail") != e.Doc() {
		t.Fatal("the output of a failed task should open")
	}

	runJobs(e)

	e.runTyped(&Context{Editor: e}, "task nope")
	if !e.Status().IsError {
		t.Fatal("an unknown task should fail")
	}

	e.runTyped(&Context{Editor: e}, "task-output hello")
	if e.Doc() != doc {
		t.Fatal("task-output did not show the buffer")
	}

	if comps := e.completeTyped("task-stop he"); len(comps) != 1 || comps[0].Text != "hello" {
		t.Fatalf("completion %+v", comps)
	}
}

// TestTaskWithoutProject explains where tasks come from.
func TestTaskWithoutProject(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.runTyped(&Context{Editor: e}, "task")
	if !e.Status().IsError || !strings.Contains(e.Status().Text, ".duck/tasks.yaml") {
		t.Fatalf("status %+v", e.Status())
	}
}
