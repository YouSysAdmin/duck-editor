package editor

import (
	"path/filepath"

	"strconv"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// activeSnippet is an inserted snippet whose tabstops can still be
// visited. Ranges are kept absolute and mapped through later edits.
type activeSnippet struct {
	// tabstops lists the ranges of every stop in navigation order, one
	// entry per cursor.
	tabstops [][]edit.Range
	current  int

	// placeholder is set while the current stop's default text is
	// selected so the next typed char replaces it.
	placeholder bool
}

func init() {
	register("goto_next_tabstop", "Goto next snippet tabstop", func(cx *Context) {
		cx.Editor.gotoTabstop(cx.Editor.Doc(), 1)
	})
	register("goto_prev_tabstop", "Goto previous snippet tabstop", func(cx *Context) {
		cx.Editor.gotoTabstop(cx.Editor.Doc(), -1)
	})
}

// snippetVars resolves the TM_ variables for the primary cursor.
func (e *Editor) snippetVars(doc *Document) func(name string) (string, bool) {
	text := doc.Text()
	primary := doc.Selection().Primary()
	cursor := primary.Cursor(text)
	line := text.CharToLine(cursor)
	path := doc.Path()

	return func(name string) (string, bool) {
		switch name {
		case "TM_SELECTED_TEXT":
			return "", true
		case "TM_CURRENT_LINE":
			return text.LineWithoutEnding(line), true
		case "TM_CURRENT_WORD":
			return edit.TextObjectWord(text, primary, edit.Inside, false).Fragment(text), true
		case "TM_LINE_INDEX":
			return strconv.Itoa(line), true
		case "TM_LINE_NUMBER":
			return strconv.Itoa(line + 1), true
		case "TM_FILENAME":
			return filepath.Base(path), true
		case "TM_FILENAME_BASE":
			base := filepath.Base(path)

			return strings.TrimSuffix(base, filepath.Ext(base)), true
		case "TM_DIRECTORY":
			return filepath.Dir(path), true
		case "TM_FILEPATH":
			return path, true
		}

		return "", false
	}
}

// insertSnippet replaces [cursor-before, cursor+after) at every cursor
// with the rendered snippet and selects its first tabstop.
func (e *Editor) insertSnippet(doc *Document, src string, before, after int) {
	text := doc.Text()
	snippet := edit.ParseSnippet(src)
	vars := e.snippetVars(doc)
	sel := doc.Selection()
	type inserted struct {
		from     int
		rendered edit.Rendered
	}
	var parts []inserted

	tx := edit.ChangeByAndWithSelection(text, sel, func(r edit.Range) (edit.Change, edit.Range, bool) {
		cursor := r.Cursor(text)
		from := max(cursor-before, 0)
		to := min(cursor+after, text.Len())
		line := text.CharToLine(from)
		lineText := text.Line(line)
		indent := lineText[:len(lineText)-len(strings.TrimLeft(lineText, " \t"))]
		rendered := snippet.Render(edit.RenderOptions{Indent: indent, IndentUnit: doc.Indent().String(), Var: vars})
		parts = append(parts, inserted{from: from, rendered: rendered})

		return edit.Change{From: from, To: to, Text: rendered.Text}, edit.Range{}, false
	})
	if len(parts) == 0 {
		return
	}

	doc.Apply(&tx)

	// Every cursor got the same snippet, so the tabstop lists line up.
	count := len(parts[0].rendered.Tabstops)
	stops := make([][]edit.Range, count)
	for _, p := range parts {
		base := tx.Changes().MapPos(p.from, edit.Before)
		for i, ts := range p.rendered.Tabstops {
			if i >= count {
				break
			}

			for _, r := range ts.Ranges {
				stops[i] = append(stops[i], edit.NewRange(base+r[0], base+r[1]))
			}
		}
	}

	doc.snippet = &activeSnippet{tabstops: stops, current: -1}
	e.gotoTabstop(doc, 1)
}

// gotoTabstop moves by dir stops. Leaving the last stop ends the
// snippet. It reports whether a snippet was active.
func (e *Editor) gotoTabstop(doc *Document, dir int) bool {
	s := doc.snippet
	if s == nil {
		return false
	}

	next := s.current + dir
	if next < 0 || next >= len(s.tabstops) {
		doc.snippet = nil

		return true
	}

	s.current = next
	ranges := s.tabstops[next]
	text := doc.Text()
	out := make([]edit.Range, 0, len(ranges))
	s.placeholder = false
	for _, r := range ranges {
		from, to := min(r.From(), text.Len()), min(r.To(), text.Len())
		if from < to {
			s.placeholder = true
			out = append(out, edit.NewRange(from, to))

			continue
		}

		out = append(out, insertCursor(text, from))
	}

	doc.SetSelection(edit.NewSelection(out, 0))
	if next == len(s.tabstops)-1 {
		// The final stop only places the cursor.
		doc.snippet = nil
	}

	return true
}

// insertCursor is the insert mode range for a cursor at pos: one
// grapheme wide with the head first, or a point at the end.
func insertCursor(text buffer.Rope, pos int) edit.Range {
	if pos >= text.Len() {
		return edit.NewRange(pos, pos)
	}

	return edit.NewRange(text.NextGraphemeBoundary(pos), pos)
}

// mapChanges keeps the tabstop ranges in place through an edit.
func (s *activeSnippet) mapChanges(changes *edit.ChangeSet) {
	for _, ranges := range s.tabstops {
		for i := range ranges {
			start := changes.MapPos(ranges[i].From(), edit.Before)
			end := max(changes.MapPos(ranges[i].To(), edit.After), start)
			ranges[i] = edit.NewRange(start, end)
		}
	}
}

// takePlaceholder reports whether typed text should replace the current
// selection and clears the flag.
func (d *Document) takePlaceholder() bool {
	if d.snippet == nil || !d.snippet.placeholder {
		return false
	}

	d.snippet.placeholder = false

	return true
}
