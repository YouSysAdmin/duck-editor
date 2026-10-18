package editor

import (
	"context"
	"math/big"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

func init() {
	register("increment", "Increment item under cursor", func(cx *Context) { incrementImpl(cx, 1) })
	register("decrement", "Decrement item under cursor", func(cx *Context) { incrementImpl(cx, -1) })
	register("record_macro", "Record macro", recordMacro)
	register("replay_macro", "Replay macro", replayMacro)
	register("insert_register", "Insert register", insertRegister)
	register("align_selections", "Align selections in column", alignSelections)
	register("rotate_selection_contents_forward", "Rotate selection contents forward", func(cx *Context) {
		reorderSelectionContents(cx, reorderForward)
	})
	register("rotate_selection_contents_backward", "Rotate selections contents backward", func(cx *Context) {
		reorderSelectionContents(cx, reorderBackward)
	})
	register("reverse_selection_contents", "Reverse selections contents", func(cx *Context) {
		reorderSelectionContents(cx, reorderReverse)
	})
	register("rotate_selections_first", "Make the first selection your primary one", func(cx *Context) {
		doc := cx.Editor.Doc()
		doc.SetSelection(doc.Selection().WithPrimaryIndex(0))
	})
	register("rotate_selections_last", "Make the last selection your primary one", func(cx *Context) {
		doc := cx.Editor.Doc()
		doc.SetSelection(doc.Selection().WithPrimaryIndex(doc.Selection().Len() - 1))
	})
	register("select_line_below", "Select current line, if already selected, extend or shrink line below based on the anchor", func(cx *Context) {
		selectLine(cx, edit.Forward)
	})
	register("select_line_above", "Select current line, if already selected, extend or shrink line above based on the anchor", func(cx *Context) {
		selectLine(cx, edit.Backward)
	})
	register("extend_line", "Select current line, if already selected, extend to another line based on the anchor", func(cx *Context) {
		dir := edit.Forward
		if cx.Editor.Doc().Selection().Primary().Head < cx.Editor.Doc().Selection().Primary().Anchor {
			dir = edit.Backward
		}

		extendLine(cx, dir)
	})
	register("goto_file_end", "Goto file end", func(cx *Context) { gotoFileEnd(cx, cx.Editor.Mode == Select) })
	register("extend_to_file_end", "Extend to file end", func(cx *Context) { gotoFileEnd(cx, true) })
	register("make_search_word_bounded", "Modify current search to make it word bounded", func(cx *Context) {
		searchSelection(cx, true)
	})
	register("goto_last_modification", "Goto last modification", gotoLastModification)
	register("goto_file", "Goto files/URLs in selections", func(cx *Context) { gotoFile(cx, ActionReplace) })
	register("goto_file_hsplit", "Goto files in selections (hsplit)", func(cx *Context) { gotoFile(cx, ActionHorizontalSplit) })
	register("goto_file_vsplit", "Goto files in selections (vsplit)", func(cx *Context) { gotoFile(cx, ActionVerticalSplit) })
	register("suspend", "Suspend and return to shell", func(cx *Context) { cx.Editor.suspend = true })
	register("command_palette", "Open command palette", commandPalette)
}

// incrementImpl adds amount times the count to the number in every
// selection. With the # register each selection gets one more than the
// previous.
func incrementImpl(cx *Context, sign int64) {
	e := cx.Editor
	amount := sign * int64(cx.Count())
	step := int64(0)
	if cx.Register(0) == '#' {
		step = sign
	}

	doc := e.Doc()
	text := doc.Text()
	sel := doc.Selection()
	changes := make([]edit.Change, 0, sel.Len())
	ranges := make([]edit.Range, 0, sel.Len())
	offset := 0
	for _, r := range sel.Ranges() {
		// A date or time around the cursor changes in its smallest unit,
		// otherwise the selected number does.
		span := r
		next, ok := "", false
		if start, end, found := dateTimeAt(text, r); found {
			span = edit.NewRange(start, end)
			next, ok = incrementDateTime(text.Slice(start, end), amount)
		}

		if !ok {
			span = r
			next, ok = incrementInteger(r.Fragment(text), amount)
		}

		amount += step
		from := span.From() + offset
		if !ok {
			ranges = append(ranges, edit.NewRange(r.From()+offset, r.To()+offset))

			continue
		}

		n := charCount(next)
		ranges = append(ranges, edit.NewRange(from, from+n))
		offset += n - span.Len()
		changes = append(changes, edit.Change{From: span.From(), To: span.To(), Text: next})
	}

	if len(changes) == 0 {
		return
	}

	tx := edit.ChangeText(text, changes).WithSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
	doc.Apply(&tx)
}

// dateTimeLayouts are the date and time formats C-a and C-x change, the
// smallest unit last in each.
var dateTimeLayouts = []string{
	"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02",
	"2006/01/02", "15:04:05", "15:04",
}

// dateTimeRe finds candidates for the layouts in a line.
var dateTimeRe = regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}(?:[ T]\d{2}:\d{2}(?::\d{2})?)?|\d{2}:\d{2}(?::\d{2})?`)

// dateTimeAt finds the date or time the selection is or the cursor sits
// in, as char offsets.
func dateTimeAt(text buffer.Rope, r edit.Range) (int, int, bool) {
	if frag := r.Fragment(text); dateTimeRe.FindString(frag) == frag && frag != "" {
		return r.From(), r.To(), true
	}

	if !r.IsSingleGrapheme(text) {
		return 0, 0, false
	}

	line := r.CursorLine(text)
	lineStart := text.LineToChar(line)
	content := text.Line(line)
	cursor := r.Cursor(text) - lineStart
	for _, m := range dateTimeRe.FindAllStringIndex(content, -1) {
		// The layouts are ASCII, byte offsets in the match are chars.
		start := utf8.RuneCountInString(content[:m[0]])
		end := start + (m[1] - m[0])
		if start <= cursor && cursor < end {
			return lineStart + start, lineStart + end, true
		}
	}

	return 0, 0, false
}

// incrementDateTime adds amount of the smallest unit of its layout to a
// date or time: days, minutes or seconds.
func incrementDateTime(s string, amount int64) (string, bool) {
	for _, layout := range dateTimeLayouts {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}

		unit := 24 * time.Hour
		switch {
		case strings.HasSuffix(layout, ":05"):
			unit = time.Second
		case strings.HasSuffix(layout, ":04"):
			unit = time.Minute
		}

		return t.Add(time.Duration(amount) * unit).Format(layout), true
	}

	return "", false
}

// incrementInteger adds amount to a decimal, 0x, 0o or 0b literal,
// keeping its width, case and digit separators.
func incrementInteger(s string, amount int64) (string, bool) {
	const sep = '_'
	if s == "" || strings.HasPrefix(s, string(sep)) || strings.HasSuffix(s, string(sep)) {
		return "", false
	}

	radix := 10
	switch {
	case strings.HasPrefix(s, "0x"):
		radix = 16
	case strings.HasPrefix(s, "0o"):
		radix = 8
	case strings.HasPrefix(s, "0b"):
		radix = 2
	}

	// Literals are ASCII, so byte offsets are char offsets.
	var sepFromEnd []int
	for i := range len(s) {
		if s[len(s)-1-i] == sep {
			sepFromEnd = append(sepFromEnd, i)
		}
	}

	word := strings.ReplaceAll(s, string(sep), "")

	var out string
	if radix == 10 {
		value, ok := new(big.Int).SetString(word, 10)
		if !ok {
			return "", false
		}

		next := new(big.Int).Add(value, big.NewInt(amount))
		width := len(word) - len(sepFromEnd)
		switch {
		case value.Sign() < 0 && next.Sign() >= 0:
			width--
		case value.Sign() >= 0 && next.Sign() < 0:
			width++
		}

		if strings.HasPrefix(word, "0") || strings.HasPrefix(word, "-0") {
			out = padNumber(next.String(), width)
		} else {
			out = next.String()
		}
	} else {
		digits := word[2:]
		value, ok := new(big.Int).SetString(digits, radix)
		if !ok || digits == "" {
			return "", false
		}

		next := new(big.Int).Add(value, big.NewInt(amount))
		if next.Sign() < 0 {
			next.SetInt64(0)
		}

		width := len(s) - 2 - len(sepFromEnd)
		body := next.Text(radix)
		if radix == 16 {
			upper, lower := 0, 0
			for _, c := range digits {
				switch {
				case unicode.IsUpper(c):
					upper++
				case unicode.IsLower(c):
					lower++
				}
			}

			if upper > lower {
				body = strings.ToUpper(body)
			}
		}

		out = s[:2] + padNumber(body, width)
	}

	// Digits start after the sign or the radix prefix, a separator never
	// goes before the first digit.
	prefix := 0
	if radix != 10 {
		prefix = 2
	}

	if strings.HasPrefix(out, "-") {
		prefix++
	}

	// Put the separators back counting from the right, nearest first so
	// each offset already includes the separators after it.
	for _, fromEnd := range sepFromEnd {
		if pos := len(out) - fromEnd; pos > prefix {
			out = out[:pos] + string(sep) + out[pos:]
		}
	}

	// A number that grew gets more separators with the same spacing.
	if len(out) > len(s) && len(sepFromEnd) > 0 {
		spacing := sepFromEnd[0]
		if n := len(sepFromEnd); n > 1 {
			spacing = sepFromEnd[n-1] - sepFromEnd[n-2] - 1
		}

		if index := strings.IndexRune(out, sep); index >= 0 && spacing > 0 {
			for index-prefix > spacing {
				index -= spacing
				out = out[:index] + string(sep) + out[index:]
			}
		}
	}

	return out, true
}

// padNumber zero pads a possibly negative number to width digits.
func padNumber(s string, width int) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if neg {
		width--
	}

	if len(s) < width {
		s = strings.Repeat("0", width-len(s)) + s
	}

	if neg {
		return "-" + s
	}

	return s
}

// macroString writes keys in the notation ParseMacro reads.
func macroString(keys []keymap.Key) string {
	var sb strings.Builder
	for _, k := range keys {
		s := k.String()
		if utf8.RuneCountInString(s) == 1 && s != "<" && s != ">" {
			sb.WriteString(s)
		} else {
			sb.WriteString("<" + s + ">")
		}
	}

	return sb.String()
}

// recordMacro starts recording keys into a register or stops and stores
// the recording.
func recordMacro(cx *Context) {
	e := cx.Editor
	if rec := e.macroRecording; rec != nil {
		e.macroRecording = nil
		// The key that ended the recording is not part of it.
		keys := rec.keys
		if len(keys) > 0 {
			keys = keys[:len(keys)-1]
		}

		if err := e.writeRegister(rec.register, []string{macroString(keys)}); err != nil {
			e.SetError("%v", err)

			return
		}

		e.SetStatus("Recorded to register [%c]", rec.register)

		return
	}

	reg := cx.Register('@')
	e.macroRecording = &macroRecording{register: reg}
	e.SetStatus("Recording to register [%c]", reg)
}

// replayMacro feeds the keys stored in a register count times.
func replayMacro(cx *Context) {
	e := cx.Editor
	reg := cx.Register('@')
	for _, r := range e.macroReplaying {
		if r == reg {
			e.SetError("Cannot replay from register [%c] because already replaying from same register", reg)

			return
		}
	}

	values, ok := e.readRegister(reg)
	if !ok || len(values) != 1 {
		e.SetError("Register [%c] empty", reg)

		return
	}

	keys, err := keymap.ParseMacro(values[0])
	if err != nil {
		e.SetError("Invalid macro: %v", err)

		return
	}

	count := cx.Count()
	e.macroReplaying = append(e.macroReplaying, reg)
	defer func() { e.macroReplaying = e.macroReplaying[:len(e.macroReplaying)-1] }()
	// The whole replay is one undo step in every document it changes.
	held := e.documentsByID()
	for _, doc := range held {
		doc.historyHold++
	}

	defer func() {
		for _, doc := range held {
			doc.historyHold--
			doc.AppendChangesToHistory()
		}
	}()

	for range count {
		for _, k := range keys {
			e.HandleKey(k)
		}
	}
}

// insertRegister pastes a register at the cursor in insert mode.
func insertRegister(cx *Context) {
	count := cx.Count()
	cx.Editor.showTransientInfo(cx.Editor.registersInfo("Insert register"))
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		ch, ok := key.Char()
		if !ok {
			return
		}

		values, ok := cx.Editor.readRegister(ch)
		if !ok {
			return
		}

		pasteImpl(cx, values, pasteCursor, count)
	})
}

// alignSelections pads selections so the nth selection of every line
// starts at the same column.
func alignSelections(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	sel := doc.Selection()
	tabWidth := doc.TabWidth()

	var columnWidths []int
	type coord struct{ row, col int }
	coords := make([]coord, 0, sel.Len())
	prevLine, colIdx, running := -1, 0, 0
	for _, r := range sel.Ranges() {
		head := edit.VisualCoordsAtPos(text, r.Head, tabWidth)
		anchor := edit.VisualCoordsAtPos(text, r.Anchor, tabWidth)
		if head.Row != anchor.Row {
			e.SetError("align cannot work with multi line selections")

			return
		}

		if head.Row != prevLine {
			colIdx, running, prevLine = 0, 0, head.Row
		}

		width := head.Col - running
		if colIdx < len(columnWidths) {
			columnWidths[colIdx] = max(columnWidths[colIdx], width)
		} else {
			columnWidths = append(columnWidths, width)
		}

		coords = append(coords, coord{head.Row, head.Col})
		running += width
		colIdx++
	}

	positions := make([]int, len(columnWidths))
	sum := 0
	for i, w := range columnWidths {
		sum += w
		positions[i] = sum
	}

	prevLine, colIdx, running = -1, 0, 0
	changes := make([]edit.Change, 0, sel.Len())
	for i, r := range sel.Ranges() {
		c := coords[i]
		if c.row != prevLine {
			colIdx, running, prevLine = 0, 0, c.row
		}

		inserts := positions[colIdx] - c.col - running
		colIdx++
		running += inserts
		changes = append(changes, edit.Change{From: r.From(), To: r.From(), Text: strings.Repeat(" ", inserts)})
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
	exitSelectMode(cx)
}

type reorderStrategy int

const (
	reorderForward reorderStrategy = iota
	reorderBackward
	reorderReverse
)

// reorderSelectionContents moves the text between selections.
func reorderSelectionContents(cx *Context, strategy reorderStrategy) {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	sel := doc.Selection()
	fragments := sel.Fragments(text)
	n := len(fragments)
	if n == 0 {
		return
	}

	by := 1
	if cx.HasCount() {
		by = min(cx.Count(), n)
	}

	primary := sel.PrimaryIndex()
	rotated := make([]string, n)
	switch strategy {
	case reorderForward:
		for i, f := range fragments {
			rotated[(i+by)%n] = f
		}

		primary = (primary + by) % n
	case reorderBackward:
		for i, f := range fragments {
			rotated[(i+n-by)%n] = f
		}

		primary = (primary + n - by) % n
	case reorderReverse:
		if by%2 == 0 {
			return
		}

		for i, f := range fragments {
			rotated[n-1-i] = f
		}

		primary = n - 1 - primary
	}

	changes := make([]edit.Change, n)
	for i, r := range sel.Ranges() {
		changes[i] = edit.Change{From: r.From(), To: r.To(), Text: rotated[i]}
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
	doc.SetSelection(doc.Selection().WithPrimaryIndex(primary))
}

// selectLine selects whole lines and then grows or shrinks by count
// lines on the side given by dir, keeping the anchor side fixed.
func selectLine(cx *Context, dir edit.Direction) {
	count := cx.Count()
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		lines := text.LenLines()
		lineTo := func(l int) int { return text.LineToChar(min(l, lines)) }
		startLine, endLine := r.LineRange(text)
		start, end := lineTo(startLine), lineTo(endLine+1)
		n := count
		if r.From() != start || r.To() != end {
			n--
		}

		forward := r.Head >= r.Anchor
		var anchorLine, headLine int
		switch {
		case dir == edit.Backward && forward:
			anchorLine, headLine = startLine, max(endLine-n, 0)
		case dir == edit.Backward:
			anchorLine, headLine = endLine, max(startLine-n, 0)
		case forward:
			anchorLine, headLine = startLine, min(endLine+n, lines)
		default:
			anchorLine, headLine = endLine, min(startLine+n, lines)
		}

		switch {
		case anchorLine < headLine:
			return edit.NewRange(lineTo(anchorLine), lineTo(headLine+1))
		case anchorLine > headLine:
			return edit.NewRange(lineTo(anchorLine+1), lineTo(headLine))
		case dir == edit.Backward:
			return edit.NewRange(lineTo(anchorLine+1), lineTo(headLine))
		default:
			return edit.NewRange(lineTo(headLine), lineTo(anchorLine+1))
		}
	})
}

func gotoFileEnd(cx *Context, extend bool) {
	cx.Editor.pushJump()
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		return r.PutCursor(text, text.Len(), extend)
	})
}

// gotoLastModification moves the cursor to where the last edit ended.
func gotoLastModification(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	pos, ok := doc.lastEditPos()
	if !ok {
		return
	}

	e.pushJump()
	extend := e.Mode == Select
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		return r.PutCursor(text, min(pos, text.Len()), extend)
	})
}

// gotoFile follows document links from the language server under the
// selections when the server offers them, else opens the paths found in
// the text.
func gotoFile(cx *Context, action Action) {
	e := cx.Editor
	doc := e.Doc()
	client := e.clientFor(doc, featDocumentLinks)
	if client == nil {
		gotoFilePaths(cx, action)

		return
	}

	docID := doc.ID
	client.DocumentLinks(doc.uri, func(links []lsp.DocumentLink, err error) {
		cur, ok := e.Docs[docID]
		if !ok {
			return
		}

		text := cur.Text()
		var targets []string
		if err == nil {
			for _, link := range links {
				r, ok := lsp.LSPToRange(text, link.Range, client.Encoding)
				if !ok || link.Target == "" {
					continue
				}

				for _, sel := range cur.Selection().Ranges() {
					if sel.Overlaps(r) || r.Contains(sel.Cursor(text)) {
						targets = append(targets, link.Target)

						break
					}
				}
			}
		}

		if len(targets) == 0 {
			gotoFilePaths(&Context{Editor: e}, action)

			return
		}

		for _, target := range targets {
			e.openTarget(target, action)
		}
	})
}

// systemOpen opens a URL with the opener of the desktop.
func systemOpen(ctx context.Context, target string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}

	return exec.CommandContext(ctx, opener, target).Run()
}

// openTarget opens a file URI in the editor and anything else with the
// system opener.
func (e *Editor) openTarget(target string, action Action) {
	if path := lsp.URIToPath(target); path != "" {
		e.pushJump()
		if _, err := e.Open(path, action); err != nil {
			e.SetError("%v", err)
		}

		return
	}

	e.openExternal(target)
}

// openExternal hands a URL to the system opener of this machine.
func (e *Editor) openExternal(target string) {
	open := e.openURL
	if open == nil {
		open = systemOpen
	}

	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		err := open(ctx, target)
		emit(func(e *Editor) {
			if err != nil {
				e.SetError("open %s: %v", target, err)
			}
		})
	})
}

// gotoFilePaths opens the path under every selection. A one char
// selection takes the surrounding path like characters.
func gotoFilePaths(cx *Context, action Action) {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	var paths []string
	for _, r := range doc.Selection().Ranges() {
		p := r.Fragment(text)
		if r.Len() <= 1 {
			p = pathAround(text, r.Cursor(text))
		}

		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}

	if len(paths) == 0 {
		e.SetError("no path under cursor")

		return
	}

	for _, p := range paths {
		resolved := e.resolvePath(doc, p)
		if !exists(e.hostOf(doc), resolved) {
			e.SetError("file not found: %s", p)

			continue
		}

		e.pushJump()
		if _, err := e.Open(resolved, action); err != nil {
			e.SetError("%v", err)
		}
	}
}

// pathAround extends from pos over characters that may appear in paths.
func pathAround(text buffer.Rope, pos int) string {
	isPath := func(ch rune) bool {
		return !unicode.IsSpace(ch) && !strings.ContainsRune("\"'`<>()[]{},;", ch)
	}
	if pos >= text.Len() || !isPath(text.Char(pos)) {
		return ""
	}

	start, end := pos, pos+1
	for start > 0 && isPath(text.Char(start-1)) {
		start--
	}

	for end < text.Len() && isPath(text.Char(end)) {
		end++
	}

	return text.Slice(start, end)
}

// resolvePath expands ~ and makes relative paths absolute against the
// document directory first, then the working directory.
func (e *Editor) resolvePath(doc *Document, p string) string {
	h := e.hostOf(doc)
	if strings.HasPrefix(p, "~/") {
		if home, err := h.Home(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}

	if filepath.IsAbs(p) {
		return p
	}

	if doc.Path() != "" {
		candidate := filepath.Join(filepath.Dir(doc.Path()), p)
		if exists(h, candidate) {
			return candidate
		}
	}

	return filepath.Join(e.WorkingDir(), p)
}

// commandPalette lists every command with its description. Typed
// commands open the prompt with the name filled in.
func commandPalette(cx *Context) {
	e := cx.Editor
	keys := e.Keymaps.Map(keymap.Normal).Bindings()
	var items []PickerItem
	for name, doc := range docs {
		label := name
		text := []string{name, "", doc}
		if bound := keys[name]; len(bound) > 0 {
			label += "  " + strings.Join(bound, ", ")
			text = append(text, "", "Keys: "+strings.Join(bound, ", "))
		}

		items = append(items, PickerItem{Label: label, Match: name, Line: -1, Text: text})
	}

	for _, tc := range typedCommands {
		name := ":" + tc.names[0]
		text := append([]string{name, ""}, strings.Split(tc.doc, "\n")...)
		if len(tc.names) > 1 {
			text = append(text, "", "Aliases: :"+strings.Join(tc.names[1:], ", :"))
		}

		items = append(items, PickerItem{Label: name, Match: name, Line: -1, Text: text})
	}

	sortPickerItems(items)
	e.openPicker(NewPicker("Commands", items, func(cx *Context, item PickerItem, _ PickerAction) {
		name, _, _ := strings.Cut(item.Label, "  ")
		if strings.HasPrefix(name, ":") {
			cx.Editor.execute(cx, "command_mode")
			if p := cx.Editor.Prompt; p != nil {
				p.input = []rune(strings.TrimPrefix(name, ":") + " ")
				p.cursor = len(p.input)
				p.recalculate()
			}

			return
		}

		cx.Editor.execute(cx, name)
	}))
}

func sortPickerItems(items []PickerItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Label < items[j-1].Label; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
