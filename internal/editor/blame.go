package editor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/vcs"
)

// blameDebounce waits for the cursor to settle before asking git.
const blameDebounce = 200 * time.Millisecond

// blameState is the blame of the line the cursor was on.
type blameState struct {
	line   int
	edits  int
	info   *vcs.BlameLine
	cancel context.CancelFunc
}

func init() {
	register("blame_line", "Show who last changed the cursor line", blameLine)
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"blame"},
		doc:   "Show who last changed the cursor line",
		run: func(cx *Context, _ []string) error {
			blameLine(cx)

			return nil
		},
	})
}

// refreshBlame asks git about the cursor line once it stays on a line
// whose blame is not known yet.
func (e *Editor) refreshBlame() {
	doc := e.Doc()
	if !e.Config.Editor.InlineBlame || e.vcsDisabled || doc.Path() == "" || doc.diff.base == nil {
		return
	}

	text := doc.Text()
	line := doc.Selection().Primary().CursorLine(text)
	st := &doc.blame
	if st.line == line && st.edits == doc.edits && (st.info != nil || st.cancel != nil) {
		return
	}

	if st.cancel != nil {
		st.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	*st = blameState{line: line, edits: doc.edits, cancel: cancel}
	docID, edits, path, content, h := doc.ID, doc.edits, doc.Path(), text.String(), e.hostOf(doc)
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(blameDebounce):
		case <-ctx.Done():
			return
		}

		info, err := vcs.Blame(ctx, h, path, content, line)
		emit(func(e *Editor) {
			cur, ok := e.Docs[docID]
			if !ok || cur.blame.line != line || cur.blame.edits != edits {
				return
			}

			cur.blame.cancel = nil
			if err != nil {
				e.logf("blame %s: %v", path, err)

				return
			}

			cur.blame.info = &info
		})
	})
}

// InlineBlame returns the blame text for the end of the cursor line of
// the focused document, false when there is none to show. It is hidden
// while inserting, the line is still changing.
func (e *Editor) InlineBlame(doc *Document) (int, string, bool) {
	st := doc.blame
	if !e.Config.Editor.InlineBlame || e.Mode == Insert || doc != e.Doc() || st.info == nil || st.edits != doc.edits {
		return 0, "", false
	}

	if doc.Selection().Primary().CursorLine(doc.Text()) != st.line {
		return 0, "", false
	}

	b := st.info
	if !b.Committed() {
		return st.line, "Not committed yet", true
	}

	return st.line, fmt.Sprintf("%s, %s \u2022 %s", b.Author, vcs.RelativeTime(b.Time, time.Now()), b.Summary), true
}

// blameLine shows the full blame of the cursor line in a popup.
func blameLine(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	if doc.Path() == "" {
		e.SetError("buffer has no file")

		return
	}

	text := doc.Text()
	line := doc.Selection().Primary().CursorLine(text)
	docID, path, content, h := doc.ID, doc.Path(), text.String(), e.hostOf(doc)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		info, err := vcs.Blame(ctx, h, path, content, line)
		emit(func(e *Editor) {
			if err != nil {
				e.SetError("%v", err)

				return
			}

			if e.Doc().ID != docID {
				return
			}

			e.Popup = &Popup{Lines: blameLines(info)}
		})
	})
}

// blameLines formats a blame for the popup.
func blameLines(b vcs.BlameLine) []string {
	if !b.Committed() {
		return []string{"Not committed yet"}
	}

	return []string{
		"commit " + b.Commit,
		"Author: " + strings.TrimSpace(b.Author+" <"+b.AuthorMail+">"),
		"Date:   " + b.Time.Format("2006-01-02 15:04") + " (" + vcs.RelativeTime(b.Time, time.Now()) + ")",
		"",
		b.Summary,
	}
}
