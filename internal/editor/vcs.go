package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/vcs"
)

// diffState holds the committed base of a document and its hunks.
type diffState struct {
	// base is the committed text as lines for the hunks, raw the text
	// itself for putting lines back.
	base   []string
	raw    string
	hunks  []vcs.Hunk
	edits  int
	cancel context.CancelFunc
	branch string
}

// diffDebounce delays recomputing hunks after an edit.
const diffDebounce = 50 * time.Millisecond

func init() {
	register("goto_next_change", "Goto next change", func(cx *Context) { gotoChange(cx, edit.Forward) })
	register("goto_prev_change", "Goto previous change", func(cx *Context) { gotoChange(cx, edit.Backward) })
	register("goto_first_change", "Goto first change", func(cx *Context) { gotoChangeEdge(cx, true) })
	register("goto_last_change", "Goto last change", func(cx *Context) { gotoChangeEdge(cx, false) })
	register("changed_file_picker", "Open changed file picker", changedFilePicker)
}

// DiffHunks returns the differences from the committed file.
func (d *Document) DiffHunks() []vcs.Hunk {
	return d.diff.hunks
}

// Branch returns the git branch of the document's repository, loaded
// in the background when the document opens.
func (d *Document) Branch() string {
	return d.diff.branch
}

// attachVCS loads the committed base of a file in the background.
func (e *Editor) attachVCS(doc *Document) {
	path := doc.Path()
	if path == "" || e.vcsDisabled {
		return
	}

	docID, h := doc.ID, e.hostOf(doc)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		branch, _ := vcs.Branch(h, path)
		if branch != "" {
			emit(func(e *Editor) {
				if doc, ok := e.Docs[docID]; ok {
					doc.diff.branch = branch
				}
			})
		}

		data, err := vcs.Base(h, path)
		if err != nil {
			return
		}

		raw := string(data)
		base := splitLines(raw)
		emit(func(e *Editor) {
			doc, ok := e.Docs[docID]
			if !ok {
				return
			}

			doc.diff.base, doc.diff.raw = base, raw
			e.scheduleDiff(doc)
		})
	})
}

func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}

	return lines
}

func ropeLines(text buffer.Rope) []string {
	n := text.LenLines()
	lines := make([]string, n)
	for i := range n {
		lines[i] = text.LineWithoutEnding(i)
	}

	return lines
}

// scheduleDiff recomputes the hunks in the background after a short
// delay, replacing any pending computation.
func (e *Editor) scheduleDiff(doc *Document) {
	if doc.diff.base == nil {
		return
	}

	if doc.diff.cancel != nil {
		doc.diff.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	doc.diff.cancel = cancel
	docID := doc.ID
	edits := doc.edits
	text := doc.Text()
	base := doc.diff.base
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(diffDebounce):
		case <-ctx.Done():
			return
		}

		hunks := vcs.Diff(base, ropeLines(text))
		emit(func(e *Editor) {
			doc, ok := e.Docs[docID]
			if !ok || doc.diff.edits > edits {
				return
			}

			doc.diff.hunks = hunks
			doc.diff.edits = edits
			if doc.edits != edits {
				e.scheduleDiff(doc)
			}
		})
	})
}

// hunkRange returns the lines a hunk covers in the current text as a
// selection range. A pure deletion selects the line after it.
func hunkRange(text buffer.Rope, h vcs.Hunk) edit.Range {
	lineCount := text.LenLines()
	if h.BEnd == h.BStart {
		line := min(h.BStart, lineCount-1)
		start := text.LineToChar(line)

		return edit.NewRange(start, start)
	}

	// Hunks lag behind edits by the debounce, so clamp to the text.
	start := text.LineToChar(min(h.BStart, lineCount-1))
	last := max(min(h.BEnd-1, lineCount-1), 0)
	end := text.LineToChar(last) + charCount(text.LineWithoutEnding(last))
	if end == start {
		end = min(start+1, text.Len())
	}

	return edit.NewRange(start, end)
}

func gotoChange(cx *Context, dir edit.Direction) {
	e := cx.Editor
	doc := e.Doc()
	hunks := doc.diff.hunks
	if len(hunks) == 0 {
		e.SetStatus("No changes")

		return
	}

	text := doc.Text()
	count := cx.Count()
	line := doc.Selection().Primary().CursorLine(text)
	var target *vcs.Hunk
	if dir == edit.Forward {
		for i := range hunks {
			if hunks[i].BStart > line {
				if count--; count == 0 {
					target = &hunks[i]

					break
				}
			}
		}
	} else {
		for i := len(hunks) - 1; i >= 0; i-- {
			if hunks[i].BStart < line {
				if count--; count == 0 {
					target = &hunks[i]

					break
				}
			}
		}
	}

	if target == nil {
		e.SetStatus("No more changes")

		return
	}

	selectHunk(e, *target)
}

func gotoChangeEdge(cx *Context, first bool) {
	e := cx.Editor
	hunks := e.Doc().diff.hunks
	if len(hunks) == 0 {
		e.SetStatus("No changes")

		return
	}

	if first {
		selectHunk(e, hunks[0])
	} else {
		selectHunk(e, hunks[len(hunks)-1])
	}
}

// hunkAt returns the change covering a line, a deletion counts on the
// line that follows it.
func hunkAt(hunks []vcs.Hunk, line int) (vcs.Hunk, bool) {
	for _, h := range hunks {
		if line >= h.BStart && (line < h.BEnd || line == h.BStart) {
			return h, true
		}
	}

	return vcs.Hunk{}, false
}

func selectHunk(e *Editor, h vcs.Hunk) {
	doc := e.Doc()
	r := hunkRange(doc.Text(), h)
	if e.Mode == Select {
		primary := doc.Selection().Primary()
		r = edit.NewRange(primary.Anchor, r.Head)
	}

	doc.SetSelection(edit.SingleSelection(r.Anchor, r.Head))
	e.ensureCursorInViewCenter()
}

// changedFilePicker lists the files that differ from HEAD.
func changedFilePicker(cx *Context) {
	e := cx.Editor
	cwd, h := e.WorkingDir(), e.Host
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		root, files, err := vcs.Status(h, cwd)
		emit(func(e *Editor) {
			if err != nil {
				e.SetError("not in a git repository")

				return
			}

			if len(files) == 0 {
				e.SetStatus("No changed files")

				return
			}

			items := make([]PickerItem, 0, len(files))
			for _, f := range files {
				abs := filepath.Join(root, f.Path)
				rel, err := filepath.Rel(cwd, abs)
				if err != nil {
					rel = abs
				}

				items = append(items, PickerItem{Label: fmt.Sprintf("%c %s", f.Kind, rel), Path: abs, Line: -1, Diff: true})
			}

			e.openPicker(NewPicker("Changed files", items, openItem))
		})
	})
}
