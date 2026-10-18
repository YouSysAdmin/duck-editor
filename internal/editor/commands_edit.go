package editor

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

type pasteAction int

const (
	pasteBefore pasteAction = iota
	pasteAfter
	pasteCursor
)

var lineEndingRegex = regexp.MustCompile(`\r\n|\r|\n`)

func init() {
	// Insert mode entry.
	register("insert_mode", "Insert before selection", func(cx *Context) {
		cx.Editor.Mode = Insert
		transformSelection(cx, func(_ buffer.Rope, r edit.Range) edit.Range {
			return edit.NewRange(r.To(), r.From())
		})
	})
	register("append_mode", "Append after selection", appendMode)
	register("insert_at_line_start", "Insert at start of line", func(cx *Context) {
		insertWithPosition(cx, func(text buffer.Rope, line int) int {
			off, ok := edit.FirstNonWhitespace(text.Line(line))
			if !ok {
				return text.LineEndCharIndex(line)
			}

			return text.LineToChar(line) + off
		})
	})
	register("insert_at_line_end", "Insert at end of line", func(cx *Context) {
		insertWithPosition(cx, func(text buffer.Rope, line int) int {
			return text.LineEndCharIndex(line)
		})
	})
	register("open_below", "Open new line below selection", func(cx *Context) { openLine(cx, edit.Forward) })
	register("open_above", "Open new line above selection", func(cx *Context) { openLine(cx, edit.Backward) })

	// Delete and change.
	register("delete_selection", "Delete selection", func(cx *Context) { deleteSelection(cx, false, true) })
	register("delete_selection_noyank", "Delete selection without yanking", func(cx *Context) {
		deleteSelection(cx, false, false)
	})
	register("change_selection", "Change selection", func(cx *Context) { deleteSelection(cx, true, true) })
	register("change_selection_noyank", "Change selection without yanking", func(cx *Context) {
		deleteSelection(cx, true, false)
	})

	// Yank and paste.
	register("yank", "Yank selection", func(cx *Context) {
		yank(cx.Editor, cx.Register(cx.Editor.yankRegister()))
		exitSelectMode(cx)
	})
	register("paste_after", "Paste after selection", func(cx *Context) {
		paste(cx, cx.Register(cx.Editor.yankRegister()), pasteAfter)
		exitSelectMode(cx)
	})
	register("paste_before", "Paste before selection", func(cx *Context) {
		paste(cx, cx.Register(cx.Editor.yankRegister()), pasteBefore)
		exitSelectMode(cx)
	})
	register("replace_with_yanked", "Replace with yanked text", func(cx *Context) {
		replaceWithRegister(cx, cx.Register(cx.Editor.yankRegister()))
		exitSelectMode(cx)
	})
	register("replace", "Replace with new char", replaceChar)
	register("yank_joined", "Join and yank selections", func(cx *Context) {
		e := cx.Editor
		yankJoined(e, string(e.Doc().LineEnding()), cx.Register(cx.Editor.yankRegister()))
		exitSelectMode(cx)
	})
	register("yank_main_selection_to_clipboard", "Yank main selection to clipboard", func(cx *Context) {
		yankPrimary(cx.Editor, clipboardRegister)
		exitSelectMode(cx)
	})
	register("yank_main_selection_to_primary_clipboard", "Yank main selection to primary clipboard", func(cx *Context) {
		yankPrimary(cx.Editor, primaryRegister)
		exitSelectMode(cx)
	})
	for _, c := range []struct {
		suffix string
		reg    rune
		label  string
	}{{"clipboard", clipboardRegister, "clipboard"}, {"primary_clipboard", primaryRegister, "primary clipboard"}} {
		register("yank_to_"+c.suffix, "Yank selections to "+c.label, func(cx *Context) {
			yank(cx.Editor, c.reg)
			exitSelectMode(cx)
		})
		register("yank_joined_to_"+c.suffix, "Join and yank selections to "+c.label, func(cx *Context) {
			e := cx.Editor
			yankJoined(e, string(e.Doc().LineEnding()), c.reg)
			exitSelectMode(cx)
		})
		register("paste_"+c.suffix+"_after", "Paste "+c.label+" after selections", func(cx *Context) {
			paste(cx, c.reg, pasteAfter)
			exitSelectMode(cx)
		})
		register("paste_"+c.suffix+"_before", "Paste "+c.label+" before selections", func(cx *Context) {
			paste(cx, c.reg, pasteBefore)
			exitSelectMode(cx)
		})
		register("replace_selections_with_"+c.suffix, "Replace selections by "+c.label+" content", func(cx *Context) {
			replaceWithRegister(cx, c.reg)
			exitSelectMode(cx)
		})
	}

	// Undo.
	register("undo", "Undo change", func(cx *Context) {
		for range cx.Count() {
			if !cx.Editor.Doc().Undo() {
				cx.Editor.SetStatus("Already at oldest change")

				break
			}
		}
	})
	register("redo", "Redo change", func(cx *Context) {
		for range cx.Count() {
			if !cx.Editor.Doc().Redo() {
				cx.Editor.SetStatus("Already at newest change")

				break
			}
		}
	})
	register("earlier", "Move backward in history", func(cx *Context) {
		for range cx.Count() {
			if !cx.Editor.Doc().Earlier(edit.UndoSteps(1)) {
				cx.Editor.SetStatus("Already at oldest change")

				break
			}
		}
	})
	register("later", "Move forward in history", func(cx *Context) {
		for range cx.Count() {
			if !cx.Editor.Doc().Later(edit.UndoSteps(1)) {
				cx.Editor.SetStatus("Already at newest change")

				break
			}
		}
	})
	register("commit_undo_checkpoint", "Commit changes to new checkpoint", func(cx *Context) {
		cx.Editor.Doc().AppendChangesToHistory()
	})

	// Case.
	register("switch_case", "Switch (toggle) case", func(cx *Context) {
		mapFragments(cx, func(s string) string {
			return strings.Map(func(r rune) rune {
				if unicode.IsUpper(r) {
					return unicode.ToLower(r)
				}

				return unicode.ToUpper(r)
			}, s)
		})
	})
	register("switch_to_lowercase", "Switch to lowercase", func(cx *Context) { mapFragments(cx, strings.ToLower) })
	register("switch_to_uppercase", "Switch to uppercase", func(cx *Context) { mapFragments(cx, strings.ToUpper) })

	// Lines.
	register("join_selections", "Join lines inside selection", func(cx *Context) { joinSelections(cx, false) })
	register("join_selections_space", "Join lines inside selection and select spaces", func(cx *Context) {
		joinSelections(cx, true)
	})
	register("indent", "Indent selection", indentLines)
	register("unindent", "Unindent selection", unindentLines)
	register("add_newline_above", "Add newline above", func(cx *Context) { addNewline(cx, edit.Backward) })
	register("add_newline_below", "Add newline below", func(cx *Context) { addNewline(cx, edit.Forward) })
}

func appendMode(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	e.Mode = Insert
	doc.RestoreCursor = true
	text := doc.Text()

	// Make room at the end of the document when the last range touches it.
	end := text.Len()
	ranges := doc.Selection().Ranges()
	last := ranges[len(ranges)-1]
	if !last.IsEmpty() && last.To() == end {
		tx := edit.ChangeText(text, []edit.Change{{From: end, To: end, Text: string(doc.LineEnding())}})
		doc.Apply(&tx)
	}

	// Each range grows one grapheme past its end. Adjacent ranges would
	// then overlap and merge, so a start never goes below the previous
	// range's new end.
	prevEnd := 0
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		to := text.NextGraphemeBoundary(r.To())
		from := max(r.From(), prevEnd)
		prevEnd = to

		return edit.NewRange(from, to)
	})
}

func insertWithPosition(cx *Context, pos func(text buffer.Rope, line int) int) {
	cx.Editor.Mode = Insert
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		return edit.Point(pos(text, r.CursorLine(text)))
	})
}

// openLine inserts count new lines below or above every selection.
func openLine(cx *Context, dir edit.Direction) {
	e := cx.Editor
	doc := e.Doc()
	count := cx.Count()
	e.Mode = Insert
	text := doc.Text()
	sel := doc.Selection()
	lineEnding := string(doc.LineEnding())
	ranges := make([]edit.Range, 0, sel.Len()*count)
	offs := 0

	tx := edit.ChangeBySelection(text, sel, func(r edit.Range) edit.Change {
		var currLine int
		if dir == edit.Forward {
			currLine = text.CharToLine(text.PrevGraphemeBoundary(r.To()))
		} else {
			currLine = text.CharToLine(r.From())
		}

		nextNewLine := currLine
		if dir == edit.Forward {
			nextNewLine++
		}

		aboveNext := max(nextNewLine-1, 0)

		var insertAt, endingWidth int
		if nextNewLine > 0 {
			insertAt = text.LineEndCharIndex(aboveNext)
			endingWidth = len(lineEnding)
		}

		indent := doc.indentForLine(currLine)
		if dir == edit.Forward {
			indent = e.newLineIndent(doc, currLine, text.LineToChar(currLine)+charCount(text.LineWithoutEnding(currLine)))
		}

		commentToken := ""
		if tok := e.continuedCommentToken(doc, currLine); tok != "" {
			commentToken = tok + " "
			if off, ok := edit.FirstNonWhitespace(text.Line(currLine)); ok {
				indent = string([]rune(text.Line(currLine))[:off])
			}
		}

		var piece string
		if dir == edit.Backward && nextNewLine == 0 {
			piece = indent + commentToken + lineEnding
		} else {
			piece = lineEnding + indent + commentToken
		}

		newText := strings.Repeat(piece, count)

		pos := offs + insertAt + endingWidth
		indentLen := charCount(indent) + charCount(commentToken)
		for i := range count {
			ranges = append(ranges, edit.Point(pos+i*(len(lineEnding)+indentLen)+indentLen))
		}

		offs += charCount(newText)

		return edit.Change{From: insertAt, To: insertAt, Text: newText}
	})
	tx = tx.WithSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
	doc.Apply(&tx)
}

func selectionIsLinewise(sel edit.Selection, text buffer.Rope) bool {
	for _, r := range sel.Ranges() {
		if strings.Count(r.Fragment(text), "\n") < 1 {
			return false
		}

		startLine, endLine := r.LineRange(text)
		if text.LineToChar(startLine) != r.From() || lineEndAfter(text, endLine) != r.To() {
			return false
		}
	}

	return true
}

func deleteSelection(cx *Context, change, doYank bool) {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	sel := doc.Selection()
	onlyWholeLines := selectionIsLinewise(sel, text)

	if doYank {
		if err := e.yankTo(cx.Register(cx.Editor.yankRegister()), sel.Fragments(text)); err != nil {
			e.SetError("%v", err)
		}
	}

	tx := edit.DeleteBySelection(text, sel, func(r edit.Range) (int, int) {
		return r.From(), r.To()
	})
	doc.Apply(&tx)

	switch {
	case !change:
		exitSelectMode(cx)
	case onlyWholeLines:
		openLine(cx, edit.Backward)
	default:
		e.Mode = Insert
	}
}

func yank(e *Editor, reg rune) {
	doc := e.Doc()
	values := doc.Selection().Fragments(doc.Text())
	if err := e.yankTo(reg, values); err != nil {
		e.SetError("%v", err)

		return
	}

	e.SetStatus("yanked %s to register %c", plural(len(values), "selection"), reg)
}

// yankJoined yanks the selections as one value joined by separator.
func yankJoined(e *Editor, separator string, reg rune) {
	doc := e.Doc()
	values := doc.Selection().Fragments(doc.Text())
	if err := e.yankTo(reg, []string{strings.Join(values, separator)}); err != nil {
		e.SetError("%v", err)

		return
	}

	e.SetStatus("joined and yanked %s to register %c", plural(len(values), "selection"), reg)
}

// yankPrimary yanks only the primary selection.
func yankPrimary(e *Editor, reg rune) {
	doc := e.Doc()
	value := doc.Selection().Primary().Fragment(doc.Text())
	if err := e.yankTo(reg, []string{value}); err != nil {
		e.SetError("%v", err)

		return
	}

	e.SetStatus("yanked primary selection to register %c", reg)
}

func paste(cx *Context, reg rune, action pasteAction) {
	values, ok := cx.Editor.readRegister(reg)
	if !ok {
		return
	}

	pasteImpl(cx, values, action, cx.Count())
}

func pasteImpl(cx *Context, values []string, action pasteAction, count int) {
	e := cx.Editor
	doc := e.Doc()
	if len(values) == 0 {
		return
	}

	if e.Mode == Insert {
		doc.AppendChangesToHistory()
	}

	linewise := false
	for _, v := range values {
		if buffer.LineEndingOf(v) != "" {
			linewise = true
		}
	}

	mapValue := func(v string) string {
		v = lineEndingRegex.ReplaceAllString(v, string(doc.LineEnding()))

		return strings.Repeat(v, count)
	}
	mapped := make([]string, len(values))
	for i, v := range values {
		mapped[i] = mapValue(v)
	}

	text := doc.Text()
	sel := doc.Selection()
	offset := 0
	ranges := make([]edit.Range, 0, sel.Len())
	i := 0
	tx := edit.ChangeBySelection(text, sel, func(r edit.Range) edit.Change {
		var pos int
		switch {
		case action == pasteBefore && linewise:
			pos = text.LineToChar(text.CharToLine(r.From()))
		case action == pasteAfter && linewise:
			_, line := r.LineRange(text)
			pos = lineEndAfter(text, line)
		case action == pasteBefore:
			pos = r.From()
		case action == pasteAfter:
			pos = r.To()
		default:
			pos = r.Cursor(text)
		}

		value := mapped[min(i, len(mapped)-1)]
		i++
		n := charCount(value)
		anchor := offset + pos
		ranges = append(ranges, edit.NewRange(anchor, anchor+n).WithDirection(r.Direction()))
		offset += n

		return edit.Change{From: pos, To: pos, Text: value}
	})
	if e.Mode == Normal {
		tx = tx.WithSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
	}

	doc.Apply(&tx)
	doc.AppendChangesToHistory()
}

func replaceWithRegister(cx *Context, reg rune) {
	e := cx.Editor
	doc := e.Doc()
	values, ok := e.readRegister(reg)
	if !ok {
		return
	}

	count := cx.Count()
	mapValue := func(v string) string {
		return strings.Repeat(lineEndingRegex.ReplaceAllString(v, string(doc.LineEnding())), count)
	}
	i := 0
	tx := edit.ChangeBySelection(doc.Text(), doc.Selection(), func(r edit.Range) edit.Change {
		if r.IsEmpty() {
			return edit.Change{From: r.From(), To: r.To()}
		}

		v := mapValue(values[min(i, len(values)-1)])
		i++

		return edit.Change{From: r.From(), To: r.To(), Text: v}
	})
	doc.Apply(&tx)
	doc.AppendChangesToHistory()
}

func replaceChar(cx *Context) {
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		doc := cx.Editor.Doc()
		var repl string
		switch key.Code {
		case keymap.KeyEnter:
			repl = string(doc.LineEnding())
		case keymap.KeyTab:
			repl = "\t"
		default:
			ch, ok := key.Char()
			if !ok {
				return
			}

			repl = string(ch)
		}

		text := doc.Text()
		tx := edit.ChangeBySelection(text, doc.Selection(), func(r edit.Range) edit.Change {
			if r.IsEmpty() {
				return edit.Change{From: r.From(), To: r.To()}
			}

			// Every grapheme becomes the replacement, line endings included.
			count := len(splitGraphemes(r.Fragment(text)))

			return edit.Change{From: r.From(), To: r.To(), Text: strings.Repeat(repl, count)}
		})
		doc.Apply(&tx)
		exitSelectMode(cx)
	})
}

// mapFragments replaces every selected fragment with f(fragment).
func mapFragments(cx *Context, f func(string) string) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	tx := edit.ChangeBySelection(text, doc.Selection(), func(r edit.Range) edit.Change {
		return edit.Change{From: r.From(), To: r.To(), Text: f(r.Fragment(text))}
	})
	doc.Apply(&tx)
	exitSelectMode(cx)
}

// selectionLines returns the sorted unique lines touched by the selection.
func selectionLines(doc *Document) []int {
	text := doc.Text()
	lines := []int{}
	for _, r := range doc.Selection().Ranges() {
		start, end := r.LineRange(text)
		for l := start; l <= end; l++ {
			lines = append(lines, l)
		}
	}

	slices.Sort(lines)

	return slices.Compact(lines)
}

// sharedCommentToken is the line comment token that starts both line and
// the line after it, empty when they are not both comments.
func sharedCommentToken(doc *Document, text buffer.Rope, line int) string {
	if doc.language == nil {
		return ""
	}

	this := strings.TrimLeft(text.Line(line), " \t")
	next := strings.TrimLeft(text.Line(line+1), " \t")
	for _, token := range doc.language.CommentTokens {
		if token != "" && strings.HasPrefix(this, token) && strings.HasPrefix(next, token) {
			return token
		}
	}

	return ""
}

func joinSelections(cx *Context, selectSpace bool) {
	doc := cx.Editor.Doc()
	text := doc.Text()

	lines := []int{}
	for _, r := range doc.Selection().Ranges() {
		start, end := r.LineRange(text)
		if start == end {
			end = min(end+1, text.LenLines()-1)
		}

		for l := start; l < end; l++ {
			lines = append(lines, l)
		}
	}

	slices.Sort(lines)
	lines = slices.Compact(lines)

	changes := make([]edit.Change, 0, len(lines))
	for _, line := range lines {
		if line+1 >= text.LenLines() {
			continue
		}

		start := text.LineEndCharIndex(line)
		end := text.LineToChar(line + 1)
		for end < text.Len() && (text.Char(end) == ' ' || text.Char(end) == '\t') {
			end++
		}

		// Joining two comment lines keeps one comment token.
		if token := sharedCommentToken(doc, text, line); token != "" {
			end += charCount(token)
			for end < text.Len() && (text.Char(end) == ' ' || text.Char(end) == '\t') {
				end++
			}
		}

		sep := " "
		if end == text.LineEndCharIndex(line+1) {
			// The joined line holds only whitespace, no separator.
			sep = ""
		}

		changes = append(changes, edit.Change{From: start, To: end, Text: sep})
	}

	if len(changes) == 0 {
		return
	}

	tx := edit.ChangeText(text, changes)
	if selectSpace {
		ranges := make([]edit.Range, 0, len(changes))
		offset := 0
		for _, c := range changes {
			if c.Text != "" {
				pos := c.From + offset
				ranges = append(ranges, edit.NewRange(pos, pos+1))
			}

			offset += charCount(c.Text) - (c.To - c.From)
		}

		if len(ranges) > 0 {
			tx = tx.WithSelection(edit.NewSelection(ranges, 0))
		}
	}

	doc.Apply(&tx)
}

func indentLines(cx *Context) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	unit := strings.Repeat(doc.Indent().String(), cx.Count())
	changes := []edit.Change{}
	for _, line := range selectionLines(doc) {
		if strings.TrimSpace(text.Line(line)) == "" {
			continue
		}

		pos := text.LineToChar(line)
		changes = append(changes, edit.Change{From: pos, To: pos, Text: unit})
	}

	if len(changes) == 0 {
		return
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
}

func unindentLines(cx *Context) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	tab := doc.TabWidth()
	indentWidth := doc.Indent().IndentWidth(tab) * cx.Count()
	changes := []edit.Change{}
	for _, line := range selectionLines(doc) {
		width := 0
		pos := 0
	scan:
		for _, ch := range text.Line(line) {
			switch ch {
			case ' ':
				width++
			case '\t':
				width = (width/tab + 1) * tab
			default:
				break scan
			}

			pos++
			if width >= indentWidth {
				break
			}
		}

		if pos > 0 {
			start := text.LineToChar(line)
			changes = append(changes, edit.Change{From: start, To: start + pos})
		}
	}

	if len(changes) == 0 {
		return
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
}

func addNewline(cx *Context, dir edit.Direction) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	ending := strings.Repeat(string(doc.LineEnding()), cx.Count())
	changes := []edit.Change{}
	for _, r := range doc.Selection().Ranges() {
		start, end := r.LineRange(text)
		pos := text.LineToChar(start)
		if dir == edit.Forward {
			pos = text.LineEndCharIndex(end)
		}

		changes = append(changes, edit.Change{From: pos, To: pos, Text: ending})
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
}
