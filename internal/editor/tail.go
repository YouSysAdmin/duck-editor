package editor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// tailBatch is how often new lines of a followed file are applied.
const tailBatch = 100 * time.Millisecond

// tailState is a followed stream: tail -F on a file, journalctl -f,
// docker or kubectl logs, or any command. It runs on the host of the
// buffer and its new lines are appended to a read only buffer.
type tailState struct {
	// path is the followed file, empty for other streams, title names
	// the stream then.
	path  string
	title string
	host  host.Host

	// command runs the stream, beginning with the last lines given.
	command func(lines int) host.Command

	// cancel stops the running stream, nil while it is stopped.
	cancel context.CancelFunc

	// gen tells the lines of the running stream from those of a stopped
	// one.
	gen int

	// all keeps the received lines, ending in their line break, up to
	// tail-lines. The buffer shows those matching filter, all without.
	all    []string
	filter *regexp.Regexp

	// notice runs on every batch of new lines, for advice such as a
	// journal that hides messages.
	notice func(e *Editor, text string)

	// loader parses the shown lines, nil without a known language.
	loader *syntax.Loader

	// window caches the highlights of the last drawn range.
	window windowCache
}

// name is what messages call the stream.
func (t *tailState) name() string {
	if t.path != "" {
		return t.path
	}

	return t.title
}

type windowCache struct {
	edits, start, end int
	spans             []HighlightSpan
}

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"tail"},
		doc:   "Follow a growing file such as a log in a read only buffer, new lines come in at the end. Without a path, the file of this buffer. In a followed buffer it stops or resumes following",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			doc := e.Doc()
			if len(args) == 0 && doc.tail != nil {
				e.toggleTail(doc)

				return nil
			}

			path := doc.Path()
			h := e.hostOf(doc)
			if len(args) > 0 {
				path, h = e.absPath(args[0]), e.Host
			}

			if path == "" {
				return errArgument("tail", "a path")
			}

			return e.openTail(h, path)
		},
	}, typedCommand{
		names: []string{"follow"},
		raw:   true,
		doc:   "Follow the output of a command in a read only buffer, such as :follow tail -F a.log b.log",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return errArgument("follow", "a command")
			}

			cmd, err := cx.Editor.expand(args[0])
			if err != nil {
				return err
			}

			shell := cx.Editor.shellCommand()
			argv := append(slices.Clone(shell.argv), cmd)
			cx.Editor.openFollow(&tailState{title: "follow:" + firstLines(cmd, 1), host: cx.Editor.Host, command: func(int) host.Command {
				return host.Command{Name: argv[0], Args: argv[1:], Dir: shell.dir}
			}}, "")

			return nil
		},
	}, typedCommand{
		names: []string{"filter"},
		doc:   "Show only the lines of a followed buffer that match a regex, new lines too. Without a regex, show all lines again",
		run: func(cx *Context, args []string) error {
			doc := cx.Editor.Doc()
			if doc.tail == nil {
				return cmdError("filter works in a buffer of :tail, :follow, :journal or the logs commands")
			}

			var re *regexp.Regexp
			query := strings.Join(args, " ")
			if query != "" {
				var err error
				re, err = searchRegexWith(query, cx.Editor.Config.Editor.Search.SmartCase)
				if err != nil {
					return fmt.Errorf("filter: %w", err)
				}
			}

			cx.Editor.setTailFilter(doc, re, query)

			return nil
		},
	})
}

// openTail shows the last lines of a file in a new read only buffer and
// follows it.
func (e *Editor) openTail(h host.Host, path string) error {
	if info, err := h.Stat(path); err != nil {
		return err
	} else if info.IsDir() {
		return cmdError(path + " is a directory")
	}

	e.openFollow(&tailState{path: path, host: h, command: func(lines int) host.Command {
		return host.Command{Name: "tail", Args: []string{"-n", strconv.Itoa(lines), "-F", path}}
	}}, path)

	return nil
}

// openFollow shows a stream in a new read only buffer. The language for
// highlighting comes from langPath, the log language without one.
func (e *Editor) openFollow(t *tailState, langPath string) {
	doc := NewDocument("", "")
	doc.host = t.host
	doc.readOnly = true
	doc.tail = t
	e.addDocument(doc)
	// The buffer keeps no syntax tree, the shown lines are parsed when
	// drawn, see windowHighlights.
	if e.Syntax != nil && e.Syntax.Languages() != nil {
		langs := e.Syntax.Languages()
		lang, ok := languageOn(langs, t.host, langPath)
		if langPath == "" {
			lang, ok = langs.Get("log")
		}

		if ok {
			doc.language = lang
			t.loader = e.Syntax
		}
	}

	e.Switch(doc.ID, ActionReplace)
	e.startTail(doc, e.tailLines())
}

// tailLines is the tail-lines option with a floor.
func (e *Editor) tailLines() int {
	return max(e.Config.Editor.TailLines, 100)
}

// toggleTail stops a followed buffer, or resumes it with the lines that
// come from now on.
func (e *Editor) toggleTail(doc *Document) {
	if doc.tail.cancel != nil {
		e.stopTail(doc)
		e.SetStatus("stopped following %s", doc.tail.name())

		return
	}

	e.startTail(doc, 0)
	e.SetStatus("following %s again", doc.tail.name())
}

// stopTail ends the stream of a document, if one runs.
func (e *Editor) stopTail(doc *Document) {
	if doc.tail == nil || doc.tail.cancel == nil {
		return
	}

	doc.tail.cancel()
	doc.tail.cancel = nil
	doc.tail.gen++
}

// resumeTails restarts the streams of the workspace host after a
// reconnect. Lines written while the connection was down are not
// fetched.
func (e *Editor) resumeTails() {
	for _, doc := range e.documentsByID() {
		if doc.tail == nil || doc.tail.host != e.Host {
			continue
		}

		e.stopTail(doc)
		e.startTail(doc, 0)
		e.SetStatus("%s resumed, lines while offline are missing", doc.tail.name())
	}
}

// startTail runs the stream on the host of the buffer, beginning with
// the last lines, and applies complete lines in batches.
func (e *Editor) startTail(doc *Document, lines int) {
	ctx, cancel := context.WithCancel(context.Background())
	t := doc.tail
	t.cancel = cancel
	t.gen++
	gen, docID, h, cmd := t.gen, doc.ID, t.host, t.command(lines)
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		proc, err := h.Start(cmd)
		if err != nil {
			emit(func(e *Editor) { e.tailStopped(docID, gen, err) })

			return
		}

		// The process is killed and reaped however the job ends, one that
		// exited by itself is just reaped.
		var waitErr error
		waited := false
		wait := func() error {
			if !waited {
				waited = true
				waitErr = proc.Wait()
			}

			return waitErr
		}
		defer func() {
			_ = proc.Kill()
			_ = wait()
		}()

		chunks := make(chan string, 64)
		go func() {
			defer close(chunks)

			buf := make([]byte, 64<<10)
			for {
				n, err := proc.Stdout().Read(buf)
				if n > 0 {
					select {
					case chunks <- string(buf[:n]):
					case <-ctx.Done():
						return
					}
				}

				if err != nil {
					return
				}
			}
		}()

		// Lines are applied whole, a partial one waits for its end.
		var pending strings.Builder
		partial := ""
		flush := func() {
			if pending.Len() == 0 {
				return
			}

			text := pending.String()
			pending.Reset()
			emit(func(e *Editor) { e.appendTail(docID, gen, text) })
		}
		ticker := time.NewTicker(tailBatch)
		defer ticker.Stop()

		for {
			select {
			case chunk, ok := <-chunks:
				if !ok {
					// The last line may lack its break when the stream ends.
					if partial != "" {
						pending.WriteString(partial + "\n")
					}

					flush()
					err := wait()
					if errors.Is(err, io.EOF) {
						err = nil
					}

					emit(func(e *Editor) { e.tailStopped(docID, gen, err) })

					return
				}

				text := partial + chunk
				cut := strings.LastIndexByte(text, '\n') + 1
				pending.WriteString(text[:cut])
				partial = text[cut:]
			case <-ticker.C:
				flush()
			case <-ctx.Done():
				return
			}
		}
	})
}

// tailStopped reports a stream that ended by itself, such as on a lost
// connection, a missing program or a command that finished.
func (e *Editor) tailStopped(id DocID, gen int, err error) {
	doc, ok := e.Docs[id]
	if !ok || doc.tail == nil || doc.tail.gen != gen {
		return
	}

	if doc.tail.cancel != nil {
		doc.tail.cancel()
		doc.tail.cancel = nil
	}

	switch {
	case err != nil && !e.hostGone(err):
		e.SetError("%s stopped: %v", doc.tail.name(), err)
	case err == nil && doc.tail.path == "":
		e.SetStatus("%s ended", doc.tail.name())
	}
}

// appendTail adds lines of a followed stream. All lines are kept up to
// tail-lines for the filter, the buffer gets those the filter lets
// through.
func (e *Editor) appendTail(id DocID, gen int, text string) {
	doc, ok := e.Docs[id]
	if !ok || doc.tail == nil || doc.tail.gen != gen || text == "" {
		return
	}

	t := doc.tail
	if t.notice != nil {
		t.notice(e, text)
	}

	limit := e.tailLines()
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	t.all = append(t.all, lines...)
	if len(t.all) > limit+limit/10 {
		t.all = slices.Clone(t.all[len(t.all)-limit:])
	}

	if t.filter != nil {
		var shown strings.Builder
		for _, l := range lines {
			if t.filter.MatchString(strings.TrimRight(l, "\r\n")) {
				shown.WriteString(l)
			}
		}

		text = shown.String()
	}

	if text != "" {
		e.appendShown(doc, text, limit)
	}
}

// appendShown adds text at the end of a followed buffer and drops the
// oldest lines beyond limit. The buffer is trimmed in steps of a tenth of
// the limit, so most batches only append. A view whose cursor sits on
// the last line follows the new lines, others keep their place.
func (e *Editor) appendShown(doc *Document, text string, limit int) {
	old := doc.Text()
	// The text ends with a line break, the empty line after it does not
	// count.
	oldLines := old.LenLines()
	if old.Len() == 0 || old.Char(old.Len()-1) == '\n' {
		oldLines--
	}

	total := oldLines + strings.Count(text, "\n")
	cut := 0
	if total > limit+limit/10 {
		drop := total - limit
		if drop >= oldLines {
			// The batch alone fills the buffer, the old text goes and so
			// do the first lines of the batch.
			cut = old.Len()
			for range drop - oldLines {
				text = text[strings.IndexByte(text, '\n')+1:]
			}
		} else {
			cut = old.LineToChar(drop)
		}
	}

	// Views on the last line keep following.
	lastLine := max(old.LenLines()-2, 0)
	following := map[ViewID]bool{}
	for _, v := range e.Views {
		if v.DocID == doc.ID {
			following[v.ID] = old.Len() == 0 || doc.SelectionFor(v.ID).Primary().CursorLine(old) >= lastLine
		}
	}

	changes := []edit.Change{{From: old.Len(), To: old.Len(), Text: text}}
	if cut == old.Len() {
		changes = []edit.Change{{From: 0, To: old.Len(), Text: text}}
	} else if cut > 0 {
		changes = []edit.Change{{From: 0, To: cut}, {From: old.Len(), To: old.Len(), Text: text}}
	}

	tx := edit.ChangeText(old, changes)
	if !doc.applyQuiet(&tx) {
		return
	}

	e.followEnd(doc, following)
}

// followEnd moves the views marked as following to the last line.
func (e *Editor) followEnd(doc *Document, following map[ViewID]bool) {
	next := doc.Text()
	start := next.LineToChar(max(next.LenLines()-2, 0))
	for view, follow := range following {
		if follow {
			doc.SetSelectionFor(view, edit.PointSelection(start))
		}
	}

	if following[e.Focus] {
		e.EnsureCursorInView()
	}
}

// setTailFilter shows the kept lines of a followed buffer that match re,
// all of them for nil, and keeps filtering the lines that come.
func (e *Editor) setTailFilter(doc *Document, re *regexp.Regexp, query string) {
	t := doc.tail
	t.filter = re
	var shown strings.Builder
	count := 0
	for _, l := range t.all {
		if re == nil || re.MatchString(strings.TrimRight(l, "\r\n")) {
			shown.WriteString(l)
			count++
		}
	}

	old := doc.Text()
	tx := edit.ChangeText(old, []edit.Change{{From: 0, To: old.Len(), Text: shown.String()}})
	if !doc.applyQuiet(&tx) {
		return
	}

	following := map[ViewID]bool{}
	for _, v := range e.Views {
		if v.DocID == doc.ID {
			following[v.ID] = true
		}
	}

	e.followEnd(doc, following)
	if re == nil {
		e.SetStatus("showing all %d lines", count)
	} else {
		e.SetStatus("%d of %d lines match %s", count, len(t.all), query)
	}
}

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"write-local", "wl"},
		doc:   "Write a copy of the buffer to a file on this machine, also in a remote session. The buffer keeps its own file",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return errArgument("write-local", "a path")
			}

			path, err := localPath(args[0])
			if err != nil {
				return err
			}

			doc := cx.Editor.Doc()
			if err := (host.Local{}).WriteFile(path, []byte(doc.Text().String()), cx.Editor.Config.Editor.AtomicSave); err != nil {
				return err
			}

			cx.Editor.SetStatus("wrote a copy of %s to %s", doc.displayName(), path)

			return nil
		},
	})
}

// localDir is the directory dk was started in, where relative local
// paths begin.
func localDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}

	return dir
}

// localPath resolves a path typed for this machine, with ~ for home.
func localPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}

		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(localDir(), path)
	}

	return path, nil
}

// windowHighlights parses only the chars start to end of a tail buffer
// and highlights them. A followed file grows all the time and grammars
// such as the log one reparse a whole file on every change, the lines on
// screen are cheap to parse each time the text moved on.
func (d *Document) windowHighlights(start, end int) []HighlightSpan {
	t := d.tail
	if t.loader == nil || d.language == nil || end <= start {
		return nil
	}

	if c := t.window; c.spans != nil && c.edits == d.edits && c.start == start && c.end == end {
		return c.spans
	}

	window := buffer.NewRope(d.text.Slice(start, end))
	syn, err := syntax.New(t.loader, d.language, window)
	if err != nil {
		return nil
	}

	defer syn.Close()

	raw := syn.Highlights(0, window.LenBytes())
	spans := make([]HighlightSpan, 0, len(raw))
	for _, h := range raw {
		spans = append(spans, HighlightSpan{
			Start: start + window.ByteToChar(max(h.Start, 0)),
			End:   start + window.ByteToChar(min(h.End, window.LenBytes())),
			Scope: h.Scope,
		})
	}

	t.window = windowCache{edits: d.edits, start: start, end: end, spans: spans}

	return spans
}
