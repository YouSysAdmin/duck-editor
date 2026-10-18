package editor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// newTailDoc shows an empty tail buffer of path without a running tail.
func newTailDoc(t *testing.T, e *Editor, path string) *Document {
	t.Helper()

	doc := NewDocument("", "")
	doc.readOnly = true
	doc.tail = &tailState{path: path, host: host.Local{}, gen: 1}
	e.addDocument(doc)
	e.Switch(doc.ID, ActionReplace)

	return doc
}

func numbered(from, to int) string {
	var b strings.Builder
	for i := from; i < to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}

	return b.String()
}

// TestAppendTail appends, follows the end, trims in steps and keeps the
// buffer out of the undo history.
func TestAppendTail(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.Config.Editor.TailLines = 100
	doc := newTailDoc(t, e, "/var/log/x.log")
	e.appendTail(doc.ID, 1, numbered(0, 105))
	if got := strings.Count(doc.Text().String(), "\n"); got != 105 {
		t.Fatalf("%d lines, the buffer is trimmed only past a tenth over the limit", got)
	}

	if line := doc.Selection().Primary().CursorLine(doc.Text()); line != 104 {
		t.Fatalf("cursor on line %d, it follows the last line", line)
	}

	e.appendTail(doc.ID, 1, numbered(105, 115))
	text := doc.Text().String()
	if !strings.HasPrefix(text, "line 15\n") || strings.Count(text, "\n") != 100 {
		t.Fatalf("trimmed to %d lines starting %q", strings.Count(text, "\n"), text[:10])
	}

	// A cursor away from the end stays on its text.
	doc.SetSelection(edit.PointSelection(doc.Text().LineToChar(50)))
	e.appendTail(doc.ID, 1, numbered(115, 130))
	if got := doc.Text().LineWithoutEnding(doc.Selection().Primary().CursorLine(doc.Text())); got != "line 65" {
		t.Fatalf("cursor moved to %q", got)
	}

	// A batch longer than the limit keeps its last lines only.
	e.appendTail(doc.ID, 1, numbered(130, 500))
	text = doc.Text().String()
	if !strings.HasPrefix(text, "line 400\n") || strings.Count(text, "\n") != 100 {
		t.Fatalf("big batch left %d lines starting %q", strings.Count(text, "\n"), text[:10])
	}

	// Lines of a stopped tail are dropped.
	e.appendTail(doc.ID, 0, "late\n")
	if strings.Contains(doc.Text().String(), "late") {
		t.Fatal("old generation applied")
	}

	if doc.IsModified() {
		t.Fatal("a tail buffer is never modified")
	}

	before := doc.Text().String()
	e.feed(t, "u")
	e.feed(t, "ix")
	if doc.Text().String() != before || !e.Status().IsError {
		t.Fatalf("edited a read only buffer, status %+v", e.Status())
	}

	e.feed(t, "<esc>")

	e.runTyped(&Context{Editor: e}, "w /tmp/never")
	if !e.Status().IsError || doc.Path() != "" {
		t.Fatalf("wrote a tail buffer: %+v", e.Status())
	}
}

// TestTailFollowsFile runs tail -F on a file, picks up appended lines and
// stops the process when the buffer closes.
func TestTailFollowsFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	if err := e.openTail(host.Local{}, path); err != nil {
		t.Fatal(err)
	}

	doc := e.Doc()
	if doc.tail == nil || doc.Title() != "tail:"+path {
		t.Fatalf("title %q", doc.Title())
	}

	results := make(chan JobResult, 64)
	done := make(chan struct{})
	jobs := e.TakeJobs()
	go func() {
		defer close(done)

		for _, job := range jobs {
			job.Run(job.Ctx, func(r JobResult) { results <- r })
		}
	}()
	waitFor := func(what string, cond func() bool) {
		t.Helper()

		deadline := time.After(5 * time.Second)
		for !cond() {
			select {
			case r := <-results:
				e.ApplyResult(r)
			case <-deadline:
				t.Fatalf("%s, buffer %q", what, doc.Text().String())
			}
		}
	}
	waitFor("initial lines", func() bool { return doc.Text().String() == "first\nsecond\n" })

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = f.WriteString("third\npart")
	waitFor("appended line", func() bool { return strings.HasSuffix(doc.Text().String(), "third\n") })
	_, _ = f.WriteString("ial\n")
	_ = f.Close()
	waitFor("joined partial line", func() bool { return strings.HasSuffix(doc.Text().String(), "third\npartial\n") })

	if err := e.CloseDocument(doc.ID, false); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tail kept running after the buffer closed")
	}
}

// TestWriteLocal writes a copy of the buffer to this machine and leaves
// the buffer bound to its file.
func TestWriteLocal(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#bc")
	e.Doc().SetPath("/remote/file.txt")
	copyPath := filepath.Join(t.TempDir(), "copy.txt")
	e.runTyped(&Context{Editor: e}, "wl "+copyPath)
	if data, err := os.ReadFile(copyPath); err != nil || string(data) != "abc" {
		t.Fatalf("copy %q, %v, status %+v", data, err, e.Status())
	}

	if e.Doc().Path() != "/remote/file.txt" {
		t.Fatalf("buffer path %q", e.Doc().Path())
	}
}

// TestTailWindowHighlights colors the shown lines of a tail buffer
// without a syntax tree of the whole buffer.
func TestTailWindowHighlights(t *testing.T) {
	t.Parallel()

	runtime := filepath.Join("..", "..", "runtime")
	if _, err := os.Stat(filepath.Join(runtime, "grammars", "log.so")); err != nil || !syntax.Supported {
		t.Skip("no compiled log grammar")
	}

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	e.Syntax = syntax.NewLoader([]string{runtime}, langs)
	doc := newTailDoc(t, e, "/var/log/app.log")
	doc.language, _ = langs.ForPath("app.log")
	doc.tail.loader = e.Syntax
	var b strings.Builder
	for i := range 20000 {
		fmt.Fprintf(&b, "2026-10-18 12:00:00 INFO request %d done\n", i)
	}

	e.appendTail(doc.ID, 1, b.String())
	start := doc.Text().LineToChar(doc.LineCount() - 40)
	began := time.Now()
	spans := doc.Highlights(start, doc.Text().Len())
	took := time.Since(began)
	if doc.syntax != nil || len(spans) == 0 || spans[0].Start < start {
		t.Fatalf("syntax tree %v, %d spans", doc.syntax != nil, len(spans))
	}

	if took > 50*time.Millisecond {
		t.Fatalf("highlighting 40 lines took %v", took)
	}
}

// fakeTailHost runs tail as a process whose output the test writes, so
// the test sees when it is killed and reaped.
type fakeTailHost struct {
	host.Local
	proc *fakeProc
}

func (h *fakeTailHost) Start(host.Command) (host.Process, error) {
	return h.proc, nil
}

type fakeProc struct {
	r              *io.PipeReader
	w              *io.PipeWriter
	killed, waited atomic.Int32
	stdin          io.WriteCloser
}

func newFakeProc() *fakeProc {
	r, w := io.Pipe()

	return &fakeProc{r: r, w: w}
}

func (p *fakeProc) Stdin() io.WriteCloser { return p.stdin }
func (p *fakeProc) Stdout() io.Reader     { return p.r }
func (p *fakeProc) Kill() error {
	p.killed.Add(1)
	_ = p.w.Close()

	return nil
}

func (p *fakeProc) Wait() error {
	p.waited.Add(1)

	return nil
}

// TestTailReapsProcess reaps tail when it exits by itself, keeping its
// unfinished last line, and kills and reaps it when the buffer closes.
func TestTailReapsProcess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, end func(e *Editor, doc *Document, p *fakeProc)) *fakeProc {
		t.Helper()

		h := &fakeTailHost{proc: newFakeProc()}
		e := newTestEditor(t, "#[a|]#")
		if err := e.openTail(h, path); err != nil {
			t.Fatal(err)
		}

		doc := e.Doc()
		jobs := e.TakeJobs()
		results := make(chan JobResult, 64)
		done := make(chan struct{})
		go func() {
			defer close(done)

			for _, job := range jobs {
				job.Run(job.Ctx, func(r JobResult) { results <- r })
			}
		}()
		apply := func(cond func() bool) {
			deadline := time.After(5 * time.Second)
			for !cond() {
				select {
				case r := <-results:
					e.ApplyResult(r)
				case <-deadline:
					t.Fatalf("waiting, buffer %q", doc.Text().String())
				}
			}
		}
		_, _ = h.proc.w.Write([]byte("one\ntwo"))
		apply(func() bool { return strings.HasPrefix(doc.Text().String(), "one\n") })
		end(e, doc, h.proc)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the tail job did not end")
		}

		apply(func() bool { return doc.tail.cancel == nil })

		return h.proc
	}

	var last *Document
	p := run(t, func(_ *Editor, doc *Document, p *fakeProc) {
		last = doc
		_ = p.w.Close()
	})
	if p.waited.Load() != 1 || last.Text().String() != "one\ntwo\n" {
		t.Fatalf("a tail that exited was reaped %d times and left %q", p.waited.Load(), last.Text().String())
	}

	p = run(t, func(e *Editor, doc *Document, _ *fakeProc) {
		if err := e.CloseDocument(doc.ID, false); err != nil {
			t.Fatal(err)
		}
	})
	if p.killed.Load() == 0 || p.waited.Load() != 1 {
		t.Fatalf("closing the buffer killed %d and reaped %d", p.killed.Load(), p.waited.Load())
	}
}

// TestTailFilter shows the kept lines that match, filters new lines as
// they come and shows everything again without a regex.
func TestTailFilter(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	doc := newTailDoc(t, e, "/var/log/app.log")
	e.appendTail(doc.ID, 1, "INFO start\nERROR disk full\nINFO ok\n")
	e.runTyped(&Context{Editor: e}, "filter error")
	if got := doc.Text().String(); got != "ERROR disk full\n" {
		t.Fatalf("filtered %q, status %+v", got, e.Status())
	}

	e.appendTail(doc.ID, 1, "INFO more\nERROR again\n")
	if got := doc.Text().String(); got != "ERROR disk full\nERROR again\n" {
		t.Fatalf("new lines filtered to %q", got)
	}

	e.runTyped(&Context{Editor: e}, "filter")
	if got := doc.Text().String(); got != "INFO start\nERROR disk full\nINFO ok\nINFO more\nERROR again\n" {
		t.Fatalf("unfiltered %q", got)
	}

	if doc.IsModified() {
		t.Fatal("filtering marked the buffer modified")
	}

	plain := newTestEditor(t, "#[a|]#")
	plain.runTyped(&Context{Editor: plain}, "filter x")
	if !plain.Status().IsError {
		t.Fatal("filter outside a followed buffer should fail")
	}
}

// TestFollowCommand follows the output of a command until it ends.
func TestFollowCommand(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.runTyped(&Context{Editor: e}, "follow printf 'a\\nb\\n'; echo oops >&2")
	doc := e.Doc()
	if doc.tail == nil || doc.Title() != "follow:printf 'a\\nb\\n'; echo oops >&2" {
		t.Fatalf("title %q", doc.Title())
	}

	runJobs(e)
	if got := doc.Text().String(); got != "a\nb\n" {
		t.Fatalf("followed %q", got)
	}

	if doc.tail.cancel != nil || !strings.Contains(e.Status().Text, "ended") {
		t.Fatalf("status %+v", e.Status())
	}
}
