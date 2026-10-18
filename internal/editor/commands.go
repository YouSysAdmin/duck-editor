package editor

import (
	"slices"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// Command is an editor action bound to a name in the keymap.
type Command func(cx *Context)

var commands = map[string]Command{}

// docs holds one line descriptions for info popups.
var docs = map[string]string{}

func register(name, doc string, fn Command) {
	commands[name] = fn
	docs[name] = doc
}

// describe returns the description of a command name.
func describe(name string) string {
	if d, ok := docs[name]; ok {
		return d
	}

	return name
}

func (e *Editor) movement() edit.Movement {
	if e.Mode == Select {
		return edit.Extend
	}

	return edit.Move
}

// transformSelection applies f to every range of the current selection.
func transformSelection(cx *Context, f func(text buffer.Rope, r edit.Range) edit.Range) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
		return f(text, r)
	}))
}

func init() {
	register("no_op", "Do nothing", func(*Context) {})

	// Horizontal and vertical motions.
	horizontal := func(dir edit.Direction, behaviour edit.Movement) Command {
		return func(cx *Context) {
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				return edit.MoveHorizontally(text, r, dir, cx.Count(), behaviour)
			})
		}
	}
	vertical := func(dir edit.Direction, behaviour edit.Movement) Command {
		return func(cx *Context) {
			tab := cx.Editor.Doc().TabWidth()
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				return edit.MoveVertically(text, r, dir, cx.Count(), behaviour, tab)
			})
		}
	}
	register("move_char_left", "Move left", horizontal(edit.Backward, edit.Move))
	register("move_char_right", "Move right", horizontal(edit.Forward, edit.Move))
	register("move_line_up", "Move up", vertical(edit.Backward, edit.Move))
	register("move_line_down", "Move down", vertical(edit.Forward, edit.Move))
	visual := func(dir edit.Direction, behaviour edit.Movement) Command {
		return func(cx *Context) {
			e := cx.Editor
			v, doc := e.View(), e.Doc()
			if e.WrapOptions(v, doc) == nil {
				vertical(dir, behaviour)(cx)

				return
			}

			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				return e.moveVisualLines(v, doc, r, dir, cx.Count(), behaviour == edit.Extend)
			})
		}
	}
	register("move_visual_line_up", "Move up", visual(edit.Backward, edit.Move))
	register("move_visual_line_down", "Move down", visual(edit.Forward, edit.Move))
	register("extend_char_left", "Extend left", horizontal(edit.Backward, edit.Extend))
	register("extend_char_right", "Extend right", horizontal(edit.Forward, edit.Extend))
	register("extend_line_up", "Extend up", vertical(edit.Backward, edit.Extend))
	register("extend_line_down", "Extend down", vertical(edit.Forward, edit.Extend))
	register("extend_visual_line_up", "Extend up", visual(edit.Backward, edit.Extend))
	register("extend_visual_line_down", "Extend down", visual(edit.Forward, edit.Extend))

	// Word motions. The extend variants keep the anchor.
	word := func(target edit.WordMotionTarget, extend bool) Command {
		return func(cx *Context) {
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				moved := edit.WordMove(text, r, cx.Count(), target)
				if extend {
					// The block cursor goes where the word motion put its
					// cursor, the anchor char stays in the selection.
					return r.PutCursor(text, moved.Cursor(text), true)
				}

				return moved
			})
		}
	}
	register("move_next_word_start", "Move to start of next word", word(edit.NextWordStart, false))
	register("move_prev_word_start", "Move to start of previous word", word(edit.PrevWordStart, false))
	register("move_next_word_end", "Move to end of next word", word(edit.NextWordEnd, false))
	register("move_prev_word_end", "Move to end of previous word", word(edit.PrevWordEnd, false))
	register("move_next_long_word_start", "Move to start of next long word", word(edit.NextLongWordStart, false))
	register("move_prev_long_word_start", "Move to start of previous long word", word(edit.PrevLongWordStart, false))
	register("move_next_long_word_end", "Move to end of next long word", word(edit.NextLongWordEnd, false))
	register("move_prev_long_word_end", "Move to end of previous long word", word(edit.PrevLongWordEnd, false))
	register("extend_next_sub_word_start", "Extend to start of next sub word", word(edit.NextSubWordStart, true))
	register("extend_prev_sub_word_start", "Extend to start of previous sub word", word(edit.PrevSubWordStart, true))
	register("extend_next_sub_word_end", "Extend to end of next sub word", word(edit.NextSubWordEnd, true))
	register("extend_prev_sub_word_end", "Extend to end of previous sub word", word(edit.PrevSubWordEnd, true))
	register("move_next_sub_word_start", "Move to start of next sub word", word(edit.NextSubWordStart, false))
	register("move_prev_sub_word_start", "Move to start of previous sub word", word(edit.PrevSubWordStart, false))
	register("move_next_sub_word_end", "Move to end of next sub word", word(edit.NextSubWordEnd, false))
	register("move_prev_sub_word_end", "Move to end of previous sub word", word(edit.PrevSubWordEnd, false))
	register("extend_next_word_start", "Extend to start of next word", word(edit.NextWordStart, true))
	register("extend_prev_word_start", "Extend to start of previous word", word(edit.PrevWordStart, true))
	register("extend_next_word_end", "Extend to end of next word", word(edit.NextWordEnd, true))
	register("extend_prev_word_end", "Extend to end of previous word", word(edit.PrevWordEnd, true))
	register("extend_next_long_word_start", "Extend to start of next long word", word(edit.NextLongWordStart, true))
	register("extend_prev_long_word_start", "Extend to start of previous long word", word(edit.PrevLongWordStart, true))
	register("extend_next_long_word_end", "Extend to end of next long word", word(edit.NextLongWordEnd, true))
	register("extend_prev_long_word_end", "Extend to end of previous long word", word(edit.PrevLongWordEnd, true))

	// Line motions.
	lineStart := func(behaviour func(*Editor) edit.Movement) Command {
		return func(cx *Context) {
			mv := behaviour(cx.Editor)
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				line := r.CursorLine(text)

				return r.PutCursor(text, text.LineToChar(line), mv == edit.Extend)
			})
		}
	}
	lineEnd := func(behaviour func(*Editor) edit.Movement) Command {
		return func(cx *Context) {
			mv := behaviour(cx.Editor)
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				line := r.CursorLine(text)
				start := text.LineToChar(line)
				pos := max(text.PrevGraphemeBoundary(text.LineEndCharIndex(line)), start)

				return r.PutCursor(text, pos, mv == edit.Extend)
			})
		}
	}
	lineEndNewline := func(behaviour func(*Editor) edit.Movement) Command {
		return func(cx *Context) {
			mv := behaviour(cx.Editor)
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				line := r.CursorLine(text)

				return r.PutCursor(text, text.LineEndCharIndex(line), mv == edit.Extend)
			})
		}
	}
	firstNonWS := func(behaviour func(*Editor) edit.Movement) Command {
		return func(cx *Context) {
			mv := behaviour(cx.Editor)
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				line := r.CursorLine(text)
				off, ok := edit.FirstNonWhitespace(text.Line(line))
				if !ok {
					return r
				}

				return r.PutCursor(text, text.LineToChar(line)+off, mv == edit.Extend)
			})
		}
	}
	byMode := (*Editor).movement
	always := func(*Editor) edit.Movement { return edit.Extend }
	register("goto_line_start", "Go to line start", lineStart(byMode))
	register("extend_to_line_start", "Extend to line start", lineStart(always))
	register("goto_line_end", "Go to line end", lineEnd(byMode))
	register("extend_to_line_end", "Extend to line end", lineEnd(always))
	register("goto_line_end_newline", "Go to newline at line end", lineEndNewline(byMode))
	register("extend_to_line_end_newline", "Extend to newline at line end", lineEndNewline(always))
	register("goto_first_nonwhitespace", "Go to first non-blank in line", firstNonWS(byMode))
	register("extend_to_first_nonwhitespace", "Extend to first non-blank in line", firstNonWS(always))

	// File and line jumps.
	gotoPos := func(pos func(text buffer.Rope, cx *Context) int, mv func(*Editor) edit.Movement) Command {
		return func(cx *Context) {
			m := mv(cx.Editor)
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				return r.PutCursor(text, pos(text, cx), m == edit.Extend)
			})
		}
	}
	lastLinePos := func(text buffer.Rope, _ *Context) int {
		return text.LineToChar(lastJumpLine(text))
	}
	register("goto_file_start", "Go to the start of the file", func(cx *Context) {
		if cx.HasCount() {
			gotoLine(cx, cx.Editor.movement())

			return
		}

		cx.Editor.pushJump()
		gotoPos(func(buffer.Rope, *Context) int { return 0 }, byMode)(cx)
	})
	register("extend_to_file_start", "Extend to the start of the file", func(cx *Context) {
		if cx.HasCount() {
			gotoLine(cx, edit.Extend)

			return
		}

		cx.Editor.pushJump()
		gotoPos(func(buffer.Rope, *Context) int { return 0 }, always)(cx)
	})
	register("goto_last_line", "Go to the last line of the file", func(cx *Context) {
		cx.Editor.pushJump()
		gotoPos(lastLinePos, byMode)(cx)
	})
	register("extend_to_last_line", "Extend to the last line of the file", func(cx *Context) {
		cx.Editor.pushJump()
		gotoPos(lastLinePos, always)(cx)
	})
	register("goto_line", "Go to line number <n>, the last line without a count", func(cx *Context) {
		if cx.HasCount() {
			gotoLine(cx, edit.Move)

			return
		}

		cx.Editor.pushJump()
		gotoPos(lastLinePos, byMode)(cx)
	})
	register("goto_column", "Go to column", func(cx *Context) { gotoColumn(cx, edit.Move) })
	register("extend_to_column", "Extend to column", func(cx *Context) { gotoColumn(cx, edit.Extend) })

	// Character search.
	findChar := func(dir edit.Direction, inclusive, extend bool) Command {
		return func(cx *Context) {
			count := cx.Count()
			cx.OnNextKey(func(cx *Context, key keymap.Key) {
				var motion func(e *Editor)
				switch key.Code {
				case keymap.KeyEnter:
					motion = func(e *Editor) {
						findCharLineEndingMotion(e, count, dir, inclusive, extend)
					}
				case keymap.KeyTab:
					motion = func(e *Editor) {
						findCharMotion(e, '\t', count, dir, inclusive, extend)
					}
				default:
					ch, ok := key.Char()
					if !ok {
						return
					}

					motion = func(e *Editor) {
						findCharMotion(e, ch, count, dir, inclusive, extend)
					}
				}

				cx.Editor.lastMotion = motion
				motion(cx.Editor)
			})
		}
	}
	register("find_till_char", "Move till next occurrence of char", findChar(edit.Forward, false, false))
	register("find_next_char", "Move to next occurrence of char", findChar(edit.Forward, true, false))
	register("extend_till_char", "Extend till next occurrence of char", findChar(edit.Forward, false, true))
	register("extend_next_char", "Extend to next occurrence of char", findChar(edit.Forward, true, true))
	register("till_prev_char", "Move till previous occurrence of char", findChar(edit.Backward, false, false))
	register("find_prev_char", "Move to previous occurrence of char", findChar(edit.Backward, true, false))
	register("extend_till_prev_char", "Extend till previous occurrence of char", findChar(edit.Backward, false, true))
	register("extend_prev_char", "Extend to previous occurrence of char", findChar(edit.Backward, true, true))
	register("repeat_last_motion", "Repeat last motion", func(cx *Context) {
		if cx.Editor.lastMotion != nil {
			cx.Editor.lastMotion(cx.Editor)
		}
	})

	// Paragraphs.
	paragraph := func(dir edit.Direction) Command {
		return func(cx *Context) {
			mv := cx.Editor.movement()
			transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
				if dir == edit.Forward {
					return edit.MoveNextParagraph(text, r, cx.Count(), mv)
				}

				return edit.MovePrevParagraph(text, r, cx.Count(), mv)
			})
		}
	}
	register("goto_next_paragraph", "Go to next paragraph", paragraph(edit.Forward))
	register("goto_prev_paragraph", "Go to previous paragraph", paragraph(edit.Backward))

	// Modes.
	register("normal_mode", "Enter normal mode", func(cx *Context) {
		// Escape in normal mode cancels a running shell command and puts
		// the search highlight away.
		if cx.Editor.Mode == Normal {
			cx.Editor.cancelShell()
			cx.Editor.searchHighlight = nil
		}

		cx.Editor.EnterNormalMode()
	})
	register("select_mode", "Enter selection extend mode", func(cx *Context) {
		transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
			if r.IsEmpty() && r.Head == text.Len() {
				return edit.NewRange(text.PrevGraphemeBoundary(r.Anchor), r.Head)
			}

			return r
		})
		cx.Editor.Mode = Select
	})
	register("exit_select_mode", "Exit selection mode", exitSelectMode)

	// Selection manipulation.
	register("collapse_selection", "Collapse selection into single cursor", func(cx *Context) {
		transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
			return edit.Point(r.Cursor(text))
		})
	})
	register("flip_selections", "Flip selection cursor and anchor", func(cx *Context) {
		transformSelection(cx, func(_ buffer.Rope, r edit.Range) edit.Range { return r.Flip() })
	})
	register("ensure_selections_forward", "Ensure all selections face forward", func(cx *Context) {
		transformSelection(cx, func(_ buffer.Rope, r edit.Range) edit.Range { return r.WithDirection(edit.Forward) })
	})
	register("select_all", "Select whole document", func(cx *Context) {
		doc := cx.Editor.Doc()
		doc.SetSelection(edit.SingleSelection(0, doc.Text().Len()))
	})
	register("extend_line_below", "Select current line, if already selected, extend to next line", func(cx *Context) {
		extendLine(cx, edit.Forward)
	})
	register("extend_line_above", "Select current line, if already selected, extend to previous line", func(cx *Context) {
		extendLine(cx, edit.Backward)
	})
	register("extend_to_line_bounds", "Extend selection to line bounds", func(cx *Context) {
		transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
			startLine, endLine := r.LineRange(text)

			return edit.NewRange(text.LineToChar(startLine), lineEndAfter(text, endLine)).WithDirection(r.Direction())
		})
	})
	register("shrink_to_line_bounds", "Shrink selection to line bounds", func(cx *Context) {
		transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
			startLine, endLine := r.LineRange(text)
			if startLine == endLine {
				return r
			}

			start := text.LineToChar(startLine)
			end := lineEndAfter(text, endLine)
			if start != r.From() {
				start = lineEndAfter(text, startLine)
			}

			if end != r.To() {
				end = text.LineToChar(endLine)
			}

			return edit.NewRange(start, end).WithDirection(r.Direction())
		})
	})
	register("copy_selection_on_next_line", "Copy selection on next line", func(cx *Context) {
		copySelectionOnLine(cx, edit.Forward)
	})
	register("copy_selection_on_prev_line", "Copy selection on previous line", func(cx *Context) {
		copySelectionOnLine(cx, edit.Backward)
	})
	register("keep_primary_selection", "Keep primary selection", func(cx *Context) {
		doc := cx.Editor.Doc()
		p := doc.Selection().Primary()
		doc.SetSelection(edit.SingleSelection(p.Anchor, p.Head))
	})
	register("remove_primary_selection", "Remove primary selection", func(cx *Context) {
		doc := cx.Editor.Doc()
		sel := doc.Selection()
		if sel.Len() == 1 {
			cx.Editor.SetError("no selections remaining")

			return
		}

		doc.SetSelection(sel.Remove(sel.PrimaryIndex()))
	})
	register("merge_selections", "Merge selections", func(cx *Context) {
		doc := cx.Editor.Doc()
		doc.SetSelection(doc.Selection().MergeRanges())
	})
	register("merge_consecutive_selections", "Merge consecutive selections", func(cx *Context) {
		doc := cx.Editor.Doc()
		doc.SetSelection(doc.Selection().MergeConsecutiveRanges())
	})
	register("split_selection_on_newline", "Split selection on newlines", func(cx *Context) {
		doc := cx.Editor.Doc()
		doc.SetSelection(edit.SplitOnNewline(doc.Text(), doc.Selection()))
	})
	register("rotate_selections_forward", "Rotate selections forward", func(cx *Context) {
		rotateSelections(cx, edit.Forward)
	})
	register("rotate_selections_backward", "Rotate selections backward", func(cx *Context) {
		rotateSelections(cx, edit.Backward)
	})
	register("trim_selections", "Trim whitespace from selections", trimSelections)
	register("select_register", "Select register", func(cx *Context) {
		cx.Editor.showTransientInfo(cx.Editor.registersInfo("Select register"))
		cx.OnNextKey(func(cx *Context, key keymap.Key) {
			if ch, ok := key.Char(); ok {
				cx.Editor.SelectedRegister = ch
			}
		})
	})
}

func exitSelectMode(cx *Context) {
	if cx.Editor.Mode == Select {
		cx.Editor.Mode = Normal
	}
}

// lastJumpLine is the last line, skipping an empty trailing line.
func lastJumpLine(text buffer.Rope) int {
	last := text.LenLines() - 1
	if last > 0 && text.Line(last) == "" {
		last--
	}

	return last
}

// lineEndAfter returns the char index after line's ending, or the text
// length for the last line.
func lineEndAfter(text buffer.Rope, line int) int {
	if line+1 >= text.LenLines() {
		return text.Len()
	}

	return text.LineToChar(line + 1)
}

func gotoLine(cx *Context, mv edit.Movement) {
	cx.Editor.pushJump()
	doc := cx.Editor.Doc()
	text := doc.Text()
	line := min(cx.Count()-1, lastJumpLine(text))
	pos := text.LineToChar(line)
	doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
		return r.PutCursor(text, pos, mv == edit.Extend)
	}))
}

func gotoColumn(cx *Context, mv edit.Movement) {
	cx.Editor.pushJump()
	count := cx.Count()
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		line := r.CursorLine(text)
		start := text.LineToChar(line)
		pos := min(text.NthNextGraphemeBoundary(start, count-1), text.LineEndCharIndex(line))

		return r.PutCursor(text, pos, mv == edit.Extend)
	})
}

func findCharMotion(e *Editor, target rune, count int, dir edit.Direction, inclusive, extend bool) {
	doc := e.Doc()
	text := doc.Text()
	pred := func(ch rune) bool { return ch == target }
	doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
		cursorAnchor := r.Cursor(text)
		cursorHead := text.NextGraphemeBoundary(cursorAnchor)

		var start int
		switch {
		case inclusive && dir == edit.Forward:
			start = cursorHead
		case inclusive:
			start = cursorAnchor
		case dir == edit.Forward:
			start = cursorHead + 1
		default:
			start = max(cursorAnchor-1, 0)
		}

		if start > text.Len() {
			return r
		}

		pos, ok := edit.FindNthChar(count, text, pred, start, dir)
		if !ok {
			return r
		}

		switch {
		case !inclusive && dir == edit.Forward:
			pos--
		case !inclusive:
			pos++
		}

		if extend {
			return r.PutCursor(text, pos, true)
		}

		return edit.Point(r.Cursor(text)).PutCursor(text, pos, true)
	}))
}

// findCharLineEndingMotion is the line ending aware variant of f and t
// used when the searched char is Enter.
func findCharLineEndingMotion(e *Editor, count int, dir edit.Direction, inclusive, extend bool) {
	doc := e.Doc()
	text := doc.Text()
	doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
		cursorAnchor := r.Cursor(text)
		cursorHead := text.NextGraphemeBoundary(cursorAnchor)
		cursorLine := r.CursorLine(text)

		var pos int
		if dir == edit.Forward {
			lineEnd := text.LineEndCharIndex(cursorLine)
			onEdge := lineEnd == cursorAnchor
			if !inclusive {
				onEdge = lineEnd == cursorHead || lineEnd == cursorAnchor
			}

			line := cursorLine + count - 1
			if onEdge {
				line++
			}

			if line >= text.LenLines()-1 {
				return r
			}

			pos = text.LineEndCharIndex(line)
			if !inclusive {
				pos--
			}
		} else if inclusive {
			line := cursorLine - count
			if line < 0 {
				return r
			}

			pos = text.LineEndCharIndex(line)
		} else {
			onEdge := text.LineToChar(cursorLine) == cursorAnchor
			line := cursorLine - count + 1
			if onEdge {
				line--
			}

			if line <= 0 {
				return r
			}

			pos = text.LineToChar(line)
		}

		if extend {
			return r.PutCursor(text, pos, true)
		}

		return edit.Point(r.Cursor(text)).PutCursor(text, pos, true)
	}))
}

func extendLine(cx *Context, dir edit.Direction) {
	count := cx.Count()
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		startLine, endLine := r.LineRange(text)
		start := text.LineToChar(startLine)
		end := lineEndAfter(text, endLine)
		lines := text.LenLines()

		lineTo := func(l int) int {
			if l >= lines {
				return text.Len()
			}

			return text.LineToChar(l)
		}

		selected := r.From() == start && r.To() == end
		switch {
		case dir == edit.Backward && selected:
			return edit.NewRange(end, lineTo(max(startLine-count, 0)))
		case dir == edit.Backward:
			return edit.NewRange(end, lineTo(max(startLine-(count-1), 0)))
		case selected:
			return edit.NewRange(start, lineTo(min(endLine+count+1, lines)))
		default:
			return edit.NewRange(start, lineTo(min(endLine+count, lines)))
		}
	})
}

func copySelectionOnLine(cx *Context, dir edit.Direction) {
	count := cx.Count()
	doc := cx.Editor.Doc()
	text := doc.Text()
	tab := doc.TabWidth()
	sel := doc.Selection()
	ranges := make([]edit.Range, 0, sel.Len()*(count+1))
	primary := 0

	for i, r := range sel.Ranges() {
		isPrimary := i == sel.PrimaryIndex()
		var head, anchor int
		if r.Anchor < r.Head {
			head, anchor = r.Head-1, r.Anchor
		} else {
			head, anchor = r.Head, max(r.Anchor-1, 0)
		}

		headPos := edit.VisualCoordsAtPos(text, head, tab)
		anchorPos := edit.VisualCoordsAtPos(text, anchor, tab)
		height := max(headPos.Row, anchorPos.Row) - min(headPos.Row, anchorPos.Row) + 1

		if isPrimary {
			primary = len(ranges)
		}

		ranges = append(ranges, r)

		sels := 0
		for i := 0; sels < count; i++ {
			offset := (i + 1) * height
			var anchorRow, headRow int
			if dir == edit.Forward {
				anchorRow, headRow = anchorPos.Row+offset, headPos.Row+offset
			} else {
				anchorRow, headRow = anchorPos.Row-offset, headPos.Row-offset
			}

			if anchorRow < 0 || headRow < 0 || anchorRow >= text.LenLines() || headRow >= text.LenLines() {
				break
			}

			newAnchor := edit.PosAtVisualCoords(text, edit.Position{Row: anchorRow, Col: anchorPos.Col}, tab)
			newHead := edit.PosAtVisualCoords(text, edit.Position{Row: headRow, Col: headPos.Col}, tab)

			// Skip lines that are too short to hold the selection.
			if edit.VisualCoordsAtPos(text, newAnchor, tab).Col == anchorPos.Col &&
				edit.VisualCoordsAtPos(text, newHead, tab).Col == headPos.Col {
				if isPrimary {
					primary = len(ranges)
				}

				ranges = append(ranges, edit.Point(newAnchor).PutCursor(text, newHead, true))
				sels++
			}

			if anchorRow == 0 && headRow == 0 {
				break
			}
		}
	}

	doc.SetSelection(edit.NewSelection(ranges, primary))
}

func rotateSelections(cx *Context, dir edit.Direction) {
	doc := cx.Editor.Doc()
	sel := doc.Selection()
	n := sel.Len()
	idx := sel.PrimaryIndex()
	count := cx.Count() % n
	if dir == edit.Forward {
		idx = (idx + count) % n
	} else {
		idx = (idx + n - count) % n
	}

	doc.SetSelection(sel.WithPrimaryIndex(idx))
}

func trimSelections(cx *Context) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	sel := doc.Selection()
	ranges := make([]edit.Range, 0, sel.Len())
	// The primary stays on the same range, or the one after a dropped one.
	primary := 0
	for i, r := range sel.Ranges() {
		if i == sel.PrimaryIndex() {
			primary = len(ranges)
		}

		fragment := r.Fragment(text)
		startOff, ok := edit.FirstNonWhitespace(fragment)
		if !ok {
			continue
		}

		endOff, _ := edit.LastNonWhitespace(fragment)
		start := r.From() + startOff
		end := r.From() + endOff + 1
		ranges = append(ranges, edit.NewRange(start, end).WithDirection(r.Direction()))
	}

	if len(ranges) == 0 {
		cx.Editor.SetError("no selections remaining")

		return
	}

	doc.SetSelection(edit.NewSelection(ranges, min(primary, len(ranges)-1)))
}

// DescribeCommand returns the one line description of a command name,
// the name itself when it has none.
func DescribeCommand(name string) string {
	return describe(name)
}

// TypedCommandDoc describes a command of the command line.
type TypedCommandDoc struct {
	// Names holds the name and its aliases.
	Names []string
	Doc   string
}

// TypedCommandDocs lists the commands of the command line in
// registration order.
func TypedCommandDocs() []TypedCommandDoc {
	out := make([]TypedCommandDoc, 0, len(typedCommands))
	for _, tc := range typedCommands {
		out = append(out, TypedCommandDoc{Names: slices.Clone(tc.names), Doc: tc.doc})
	}

	return out
}
