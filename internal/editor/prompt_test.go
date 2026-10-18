package editor

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

func TestPasteIntoPrompt(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	e.HandleKey(keymap.Key{Code: ':'})
	e.HandleKey(keymap.Key{Code: 'e'})
	e.HandlePaste("cho  one\ntwo\n")
	if got := e.Prompt.Input(); got != "echo  one two" {
		t.Fatalf("prompt input = %q", got)
	}

	if got := e.Doc().Text().String(); got != "abc" {
		t.Fatalf("the document changed: %q", got)
	}
}

func TestPasteIntoPicker(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	e.openPicker(NewPicker("test", []PickerItem{{Label: "alpha"}, {Label: "beta"}}, nil))
	e.HandlePaste("bet")
	if got := e.Picker.Query(); got != "bet" {
		t.Fatalf("picker query = %q", got)
	}

	if got := e.Doc().Text().String(); got != "abc" {
		t.Fatalf("the document changed: %q", got)
	}
}

// TestShellCommandExpandsOnce keeps %% a single % for the shell, the
// command line is not expanded a second time, and spacing inside quotes
// survives.
func TestShellCommandExpandsOnce(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	e.runTyped(&Context{Editor: e}, "sh printf '10%%%% of  %%s' x")
	runJobs(e)
	if e.Popup == nil || len(e.Popup.Lines) == 0 || e.Popup.Lines[0] != "10% of  x" {
		t.Fatalf("popup %+v status %+v", e.Popup, e.Status())
	}
}

// TestShellCommandFailingKeepsOutput shows what a failing command printed,
// diff and grep exit 1 with useful output.
func TestShellCommandFailingKeepsOutput(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	e.runTyped(&Context{Editor: e}, "sh echo changed; exit 1")
	runJobs(e)
	if e.Popup == nil || e.Popup.Lines[0] != "changed" || !e.Status().IsError {
		t.Fatalf("popup %+v status %+v", e.Popup, e.Status())
	}
}

// TestPipeRunsInBackground keeps the editor responsive: escape cancels a
// slow pipe, and output for text that changed meanwhile is dropped.
func TestPipeRunsInBackground(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	e.feed(t, "|sleep 5<ret>")
	jobs := e.TakeJobs()
	e.feed(t, "<esc>")
	start := time.Now()
	for _, job := range jobs {
		job.Run(job.Ctx, func(r JobResult) { e.ApplyResult(r) })
	}

	if time.Since(start) > 2*time.Second {
		t.Fatal("escape did not stop the command")
	}

	if got := e.Doc().Text().String(); got != "abc" {
		t.Fatalf("cancelled pipe changed the text to %q", got)
	}

	e.feed(t, "|echo x<ret>")
	jobs = e.TakeJobs()
	e.feed(t, "ly")
	e.feed(t, "P")
	before := e.Doc().Text().String()
	for _, job := range jobs {
		job.Run(job.Ctx, func(r JobResult) { e.ApplyResult(r) })
	}

	if got := e.Doc().Text().String(); got != before || !e.Status().IsError {
		t.Fatalf("stale output applied: %q status %+v", got, e.Status())
	}
}

// TestSearchSeesEdits finds text typed after an earlier search, and
// highlights every match of a line with many of them.
func TestSearchSeesEdits(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]# b a\n")
	e.feed(t, "/a<ret>")
	e.feed(t, "ggizz<esc>")
	e.feed(t, "gg/zz<ret>")
	if got := e.Doc().Selection().Primary().Fragment(e.Doc().Text()); got != "zz" {
		t.Fatalf("search after an edit selected %q", got)
	}

	e = newTestEditor(t, "#[x|]# éa éa éa\n")
	e.feed(t, "/a<ret>")
	got := e.SearchMatches(e.Doc(), 0, e.Doc().Text().Len())
	want := [][2]int{{3, 4}, {6, 7}, {9, 10}}
	if !slices.Equal(got, want) {
		t.Fatalf("matches %v want %v", got, want)
	}
}

// TestRunInteractive hands the terminal to a shell command in the
// working directory and refreshes when it is back.
func TestRunInteractive(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.Config.Editor.Shell = []string{"bash", "-c"}
	e.runTyped(&Context{Editor: e}, "ri lazygit %{buffer_name}")
	req := e.TakeExec()
	if req == nil || !slices.Equal(req.Argv, []string{"bash", "-c", "lazygit " + e.Doc().displayName()}) {
		t.Fatalf("request %+v", req)
	}

	if e.TakeExec() != nil {
		t.Fatal("the request is taken once")
	}

	e.ExecDone(errors.New("exit status 1"))
	if !e.Status().IsError {
		t.Fatalf("status %+v", e.Status())
	}
}

// TestShellCommandCancelled stops a slow :sh and :make with escape.
func TestShellCommandCancelled(t *testing.T) {
	t.Parallel()

	for _, cmd := range []string{"sh sleep 5", "make sleep 5"} {
		e := newTestEditor(t, "#[a|]#")
		e.runTyped(&Context{Editor: e}, cmd)
		jobs := e.TakeJobs()
		e.feed(t, "<esc>")
		start := time.Now()
		for _, job := range jobs {
			job.Run(job.Ctx, func(r JobResult) { e.ApplyResult(r) })
		}

		if time.Since(start) > 2*time.Second {
			t.Fatalf("%s: escape did not stop the command", cmd)
		}

		if e.Popup != nil || len(e.quickfix) != 0 {
			t.Fatalf("%s: a cancelled command left results", cmd)
		}
	}
}
