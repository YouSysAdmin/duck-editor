package editor

import (
	"slices"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

func init() {
	register("insert_newline", "Insert newline", insertNewline)
	register("insert_tab", "Insert tab char", func(cx *Context) { insertTab(cx, 1) })
	register("smart_tab", "Insert tab if all cursors have all whitespace to their left", func(cx *Context) {
		e := cx.Editor
		if e.gotoTabstop(e.Doc(), 1) {
			return
		}

		if e.Config.Editor.SmartTabEnabled() && !cursorsAfterWhitespaceOnly(e.Doc()) && e.Doc().syntax != nil {
			e.execute(cx, "move_parent_node_end")

			return
		}

		insertTab(cx, 1)
	})
	register("delete_char_backward", "Delete previous char", deleteCharBackward)
	register("delete_char_forward", "Delete next char", func(cx *Context) {
		count := cx.Count()
		deleteBySelectionInsertMode(cx, func(text buffer.Rope, r edit.Range) (int, int) {
			pos := r.Cursor(text)

			return pos, text.NthNextGraphemeBoundary(pos, count)
		}, edit.Forward)
	})
	register("delete_word_backward", "Delete previous word", func(cx *Context) {
		count := cx.Count()
		deleteBySelectionInsertMode(cx, func(text buffer.Rope, r edit.Range) (int, int) {
			anchor := edit.WordMove(text, r, count, edit.PrevWordStart).From()
			next := edit.NewRange(anchor, r.Cursor(text))
			next = excludeCursor(text, next, r)

			return next.From(), next.To()
		}, edit.Backward)
	})
	register("delete_word_forward", "Delete next word", func(cx *Context) {
		count := cx.Count()
		deleteBySelectionInsertMode(cx, func(text buffer.Rope, r edit.Range) (int, int) {
			head := edit.WordMove(text, r, count, edit.NextWordEnd).To()

			return r.Cursor(text), head
		}, edit.Forward)
	})
	register("kill_to_line_start", "Delete till start of line", func(cx *Context) {
		deleteBySelectionInsertMode(cx, func(text buffer.Rope, r edit.Range) (int, int) {
			line := r.CursorLine(text)
			first := text.LineToChar(line)
			anchor := r.Cursor(text)
			head := first
			switch {
			case anchor == first && line != 0:
				head = text.LineEndCharIndex(line - 1)
			default:
				if off, ok := edit.FirstNonWhitespace(text.Line(line)); ok && first+off < anchor {
					head = first + off
				}
			}

			return head, anchor
		}, edit.Backward)
	})
	register("kill_to_line_end", "Delete till end of line", func(cx *Context) {
		deleteBySelectionInsertMode(cx, func(text buffer.Rope, r edit.Range) (int, int) {
			line := r.CursorLine(text)
			lineEnd := text.LineEndCharIndex(line)
			pos := r.Cursor(text)
			if pos == lineEnd {
				return pos, lineEndAfter(text, line)
			}

			return pos, lineEnd
		}, edit.Forward)
	})
}

// insertChar inserts ch at every cursor, closing pairs automatically.
func insertChar(cx *Context, ch rune) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	s := string(ch)
	pairs := cx.Editor.autoPairs(doc)
	replace := doc.takePlaceholder()
	isAutoClosed := func(pos int) bool { return slices.Contains(doc.autoClosed, pos) }
	var closed []int
	tx := edit.ChangeByAndWithSelection(text, doc.Selection(), func(r edit.Range) (edit.Change, edit.Range, bool) {
		if replace && !r.IsEmpty() {
			// The new cursor sits after the typed char, with a following
			// char only when the placeholder was not at the end.
			next := edit.NewRange(r.From()+1, r.From()+1)
			if r.To() < text.Len() {
				next = edit.NewRange(r.From()+2, r.From()+1)
			}

			return edit.Change{From: r.From(), To: r.To(), Text: s}, next, true
		}

		if pairs != nil {
			if change, next, ok := pairs.HookInsert(text, r, ch, isAutoClosed); ok {
				if p, _ := pairs.Get(ch); p.Open == p.Close && change.Text != "" {
					closed = append(closed, change.From)
				}

				return change, next, true
			}
		}

		cursor := r.Cursor(text)

		return edit.Change{From: cursor, To: cursor, Text: s}, edit.Range{}, false
	})
	doc.Apply(&tx)
	// The closing quote is the last char of each inserted pair.
	for _, pos := range closed {
		doc.autoClosed = append(doc.autoClosed, tx.Changes().MapPos(pos, edit.After)-1)
	}
}

func insertTab(cx *Context, count int) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	indent := doc.Indent()
	changes := make([]edit.Change, 0, doc.Selection().Len())
	for _, r := range doc.Selection().Ranges() {
		cursor := r.Cursor(text)
		unit := strings.Repeat(indent.String(), count)
		if !indent.Tabs {
			line := r.CursorLine(text)
			offset := (cursor - text.LineToChar(line)) % max(indent.Width, 1)
			unit = unit[offset:]
		}

		changes = append(changes, edit.Change{From: cursor, To: cursor, Text: unit})
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
}

// insertNewline inserts a line break with indentation, trimming trailing
// whitespace before the cursor. Comments continue their token
// and an empty pair gets an indented inner line.
func insertNewline(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	lineEnding := string(doc.LineEnding())
	sel := doc.Selection()
	ranges := make([]edit.Range, 0, sel.Len())
	globalOffs := 0
	lastPos := 0
	pairs := e.autoPairs(doc)

	tx := edit.ChangeBySelection(text, sel, func(r edit.Range) edit.Change {
		pos := r.Cursor(text)
		line := text.CharToLine(pos)
		lineStart := text.LineToChar(line)

		var change edit.Change
		var localOffs int
		charsDeleted := 0
		if idx, ok := edit.LastNonWhitespace(text.Slice(lineStart, pos)); ok {
			firstTrailing := min(max(lineStart+idx+1, lastPos), pos)
			lastPos = pos
			commentToken := e.continuedCommentToken(doc, line)
			indent := e.newLineIndent(doc, line, pos)
			if commentToken != "" {
				if off, ok := edit.FirstNonWhitespace(text.Line(line)); ok {
					indent = string([]rune(text.Line(line))[:off])
				}
			}

			var newText string
			switch {
			case commentToken != "":
				newText = lineEnding + indent + commentToken + " "
				localOffs = charCount(newText)
			case pairs != nil && pos > 0 && pos < text.Len() && isAutoPairGap(pairs, text.Char(pos-1), text.Char(pos)):
				// The closer stays at the indentation of the line that
				// opened the pair, the cursor goes one level deeper.
				outer := leadingWhitespace(text.Line(line))
				inner := indent
				if indentWidth(inner, doc.TabWidth()) <= indentWidth(outer, doc.TabWidth()) {
					inner = outer + doc.Indent().String()
				}

				newText = lineEnding + inner
				localOffs = charCount(newText)
				newText += lineEnding + outer
			default:
				newText = lineEnding + indent
				localOffs = charCount(newText)
			}

			charsDeleted = pos - firstTrailing
			localOffs -= charsDeleted
			change = edit.Change{From: firstTrailing, To: pos, Text: newText}
		} else {
			// A whitespace only line keeps its indentation on the new line.
			localOffs = charCount(lineEnding)
			change = edit.Change{From: lineStart, To: lineStart, Text: lineEnding}
		}

		var newRange edit.Range
		if r.Cursor(text) > r.Anchor {
			newRange = edit.NewRange(r.Anchor+globalOffs, r.Head+localOffs+globalOffs)
		} else {
			newRange = edit.NewRange(r.Anchor+localOffs+globalOffs, r.Head+localOffs+globalOffs)
		}

		ranges = append(ranges, newRange)
		globalOffs += charCount(change.Text) - charsDeleted

		return change
	})
	tx = tx.WithSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
	doc.Apply(&tx)
}

// leadingWhitespace returns the indentation of a line.
func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// indentWidth is the width of indentation in cells.
func indentWidth(indent string, tabWidth int) int {
	width := 0
	for _, ch := range indent {
		if ch == '\t' {
			width += tabWidth
		} else {
			width++
		}
	}

	return width
}

// dedent returns the deletion that removes one indent level before the
// cursor when only indentation precedes it.
func dedent(doc *Document, r edit.Range) (int, int, bool) {
	text := doc.Text()
	pos := r.Cursor(text)
	lineStart := text.LineToChar(r.CursorLine(text))
	fragment := text.Slice(lineStart, pos)
	if fragment == "" {
		return 0, 0, false
	}

	for _, ch := range fragment {
		if ch != ' ' && ch != '\t' {
			return 0, 0, false
		}
	}

	if pos > 0 && text.Char(pos-1) == '\t' {
		return text.PrevGraphemeBoundary(pos), pos, true
	}

	tab := doc.TabWidth()
	indentWidth := doc.Indent().IndentWidth(tab)
	width := 0
	for _, ch := range fragment {
		if ch == '\t' {
			width += tab
		} else {
			width++
		}
	}

	drop := width % indentWidth
	if drop == 0 {
		drop = indentWidth
	}

	start := pos
	runes := []rune(fragment)
	for i := len(runes) - 1; i >= 0 && drop > 0; i-- {
		if runes[i] != ' ' {
			break
		}

		start--
		drop--
	}

	return start, pos, true
}

func deleteCharBackward(cx *Context) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	count := cx.Count()
	pairs := cx.Editor.autoPairs(doc)
	replace := doc.takePlaceholder()
	tx := edit.DeleteByAndWithSelection(text, doc.Selection(), func(r edit.Range) (int, int, edit.Range, bool) {
		if replace && !r.IsEmpty() {
			next := edit.NewRange(r.From(), r.From())
			if r.To() < text.Len() {
				next = edit.NewRange(r.From()+1, r.From())
			}

			return r.From(), r.To(), next, true
		}

		pos := r.Cursor(text)
		if pos == 0 {
			return pos, pos, edit.Range{}, false
		}

		if from, to, ok := dedent(doc, r); ok {
			return from, to, edit.Range{}, false
		}

		if pairs != nil {
			if from, to, next, ok := pairs.HookDelete(text, r); ok {
				return from, to, next, true
			}
		}

		return text.NthPrevGraphemeBoundary(pos, count), pos, edit.Range{}, false
	})
	doc.Apply(&tx)
}

// deleteBySelectionInsertMode deletes per range in insert mode keeping
// the cursor in place when deleting forward.
func deleteBySelectionInsertMode(cx *Context, f func(text buffer.Rope, r edit.Range) (int, int), dir edit.Direction) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	sel := doc.Selection()
	ranges := make([]edit.Range, 0, sel.Len())
	insertNewlineAtEOF := false
	textLen := text.Len()

	tx := edit.DeleteBySelection(text, sel, func(r edit.Range) (int, int) {
		start, end := f(text, r)
		if dir == edit.Forward {
			if r.Head > r.Anchor {
				insertNewlineAtEOF = insertNewlineAtEOF || end == textLen
				r.Head++
			}

			ranges = append(ranges, r)
		}

		return start, end
	})
	if insertNewlineAtEOF {
		tx = tx.InsertAtEOF(string(doc.LineEnding()))
	}

	if dir == edit.Forward {
		doc.SetSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
	}

	doc.Apply(&tx)
}

func excludeCursor(text buffer.Rope, r, cursor edit.Range) edit.Range {
	if r.To() == cursor.To() && text.Len() != cursor.To() {
		return edit.NewRange(r.From(), text.PrevGraphemeBoundary(cursor.To()))
	}

	return r
}

func splitGraphemes(s string) []string {
	out := make([]string, 0, len(s))
	state := -1
	for len(s) > 0 {
		var g string
		g, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		out = append(out, g)
	}

	return out
}

// isAutoPairGap reports a cursor sitting between an open and close pair.
func isAutoPairGap(pairs *edit.AutoPairs, prev, next rune) bool {
	p, ok := pairs.Get(prev)

	return ok && p.Open == prev && p.Close == next
}

// cursorsAfterWhitespaceOnly reports whether every cursor has only
// whitespace before it on its line.
func cursorsAfterWhitespaceOnly(doc *Document) bool {
	text := doc.Text()
	for _, r := range doc.Selection().Ranges() {
		cursor := r.Cursor(text)
		start := text.LineToChar(text.CharToLine(cursor))
		if strings.TrimSpace(text.Slice(start, cursor)) != "" {
			return false
		}
	}

	return true
}
