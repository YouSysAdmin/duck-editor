package editor

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// WrapOptions returns the soft wrap settings for a document in a view,
// nil when wrapping is off. The style is left for the UI to fill.
func (e *Editor) WrapOptions(v *View, doc *Document) *ui.WrapOptions {
	cfg := e.Config.Editor.SoftWrap
	enabled := cfg.Enable != nil && *cfg.Enable
	maxWrap, maxIndent, indicator, atTextWidth := cfg.MaxWrap, cfg.MaxIndentRetain, cfg.WrapIndicator, cfg.WrapAtTextWidth
	if doc.language != nil && doc.language.SoftWrap != nil {
		sw := doc.language.SoftWrap
		if b, ok := sw["enable"].(bool); ok {
			enabled = b
		}

		if n, ok := sw["max-wrap"].(int); ok {
			maxWrap = n
		}

		if n, ok := sw["max-indent-retain"].(int); ok {
			maxIndent = n
		}

		if s, ok := sw["wrap-indicator"].(string); ok {
			indicator = s
		}

		if b, ok := sw["wrap-at-text-width"].(bool); ok {
			atTextWidth = b
		}
	}

	if !enabled {
		return nil
	}

	width := e.viewTextWidth(v, doc)
	if atTextWidth {
		width = min(width, e.textWidthFor(doc))
	}

	// The wrap distance is capped at a quarter of the view.
	return &ui.WrapOptions{Width: width, MaxWrap: min(maxWrap, max(v.Area.W/4, 1)), MaxIndentRetain: maxIndent, Indicator: indicator}
}

// textWidthFor is the language or editor text-width of a document.
func (e *Editor) textWidthFor(doc *Document) int {
	if doc.ecTextWidth > 0 {
		return doc.ecTextWidth
	}

	if doc.language != nil && doc.language.TextWidth > 0 {
		return doc.language.TextWidth
	}

	return e.Config.Editor.TextWidth
}

// Rulers returns the rulers for a document.
func (e *Editor) Rulers(doc *Document) []int {
	rulers := e.Config.Editor.Rulers
	if doc.language != nil && len(doc.language.Rulers) > 0 {
		rulers = doc.language.Rulers
	}

	if w := e.textWidthFor(doc); e.Config.Editor.TextWidthRuler && w > 0 && !slices.Contains(rulers, w+1) {
		rulers = append(slices.Clone(rulers), w+1)
	}

	return rulers
}

// EOLDiagnostic is the message shown after a line end.
type EOLDiagnostic struct {
	Char     int
	Text     string
	Severity int
}

// InlineDiagnostic is a message shown on its own row under a line.
type InlineDiagnostic struct {
	Col      int
	Text     string
	Severity int
}

// severityFilter converts a filter name to the least severe level shown,
// 0 for disabled. Severity numbers grow towards hint.
func severityFilter(name string) int {
	switch name {
	case "hint":
		return lsp.SeverityHint
	case "info":
		return lsp.SeverityInfo
	case "warning":
		return lsp.SeverityWarning
	case "error":
		return lsp.SeverityError
	}

	return 0
}

// EOLDiagnostics returns the most severe diagnostic per line within the
// end-of-line-diagnostics filter, excluding lines that get inline rows.
func (e *Editor) EOLDiagnostics(doc *Document, firstLine, lastLine int) map[int]EOLDiagnostic {
	return e.eolDiagnostics(doc, firstLine, lastLine, e.InlineDiagnostics(doc))
}

// eolDiagnostics is EOLDiagnostics with the inline rows already known.
func (e *Editor) eolDiagnostics(doc *Document, firstLine, lastLine int, inline map[int][]InlineDiagnostic) map[int]EOLDiagnostic {
	minSev := severityFilter(e.Config.Editor.EndOfLineDiagnostics)
	if minSev == 0 || e.DiagnosticsHidden() {
		return nil
	}

	text := doc.Text()
	// The line being edited keeps its end free, its diagnostics show in
	// the box in the corner of the view.
	cursorLine := -1
	if e.cursorBoxShown() {
		cursorLine = doc.Selection().Primary().CursorLine(text)
	}

	out := map[int]EOLDiagnostic{}
	for _, d := range doc.diagnostics {
		sev := d.Severity
		if sev == 0 {
			sev = lsp.SeverityHint
		}

		if sev > minSev {
			continue
		}

		// Inline rows take the diagnostics they show, the rest stays at
		// the end of the line.
		line := text.CharToLine(min(d.Range.From(), text.Len()))
		_, hasInline := inline[line]
		shownInline := hasInline && sev <= e.inlineSeverity(doc, line)
		if line < firstLine || line >= lastLine || shownInline || line == cursorLine {
			continue
		}

		if cur, ok := out[line]; ok && cur.Severity <= sev {
			continue
		}

		msg, _, _ := strings.Cut(strings.TrimSpace(d.Message), "\n")
		out[line] = EOLDiagnostic{Char: utf8.RuneCountInString(doc.Line(line)), Text: "  " + msg, Severity: sev}
	}

	return out
}

// InlineDiagnostics returns the diagnostics drawn as rows under their
// line, following the inline-diagnostics cursor-line and other-lines
// filters of the focused view.
func (e *Editor) InlineDiagnostics(doc *Document) map[int][]InlineDiagnostic {
	if e.DiagnosticsHidden() {
		return nil
	}

	cfg := e.Config.Editor.InlineDiagnostics
	if severityFilter(cfg.CursorLine) == 0 && severityFilter(cfg.OtherLines) == 0 {
		return nil
	}

	text := doc.Text()
	out := map[int][]InlineDiagnostic{}
	for _, d := range doc.diagnostics {
		sev := d.Severity
		if sev == 0 {
			sev = lsp.SeverityHint
		}

		line := text.CharToLine(min(d.Range.From(), text.Len()))
		minSev := e.inlineSeverity(doc, line)
		if minSev == 0 || sev > minSev {
			continue
		}

		if cfg.MaxDiagnostics > 0 && len(out[line]) >= cfg.MaxDiagnostics {
			continue
		}

		col := edit.VisualCoordsAtPos(text, d.Range.From(), doc.TabWidth()).Col
		prefix := "└" + strings.Repeat("─", max(cfg.PrefixLen, 1)) + " "
		msg, _, _ := strings.Cut(strings.TrimSpace(d.Message), "\n")
		out[line] = append(out[line], InlineDiagnostic{Col: col, Text: strings.Repeat(" ", col) + prefix + msg, Severity: sev})
	}

	// Several diagnostics on one line stack with a branch character.
	for line, list := range out {
		for i := range list {
			if i < len(list)-1 {
				list[i].Text = strings.Replace(list[i].Text, "└", "├", 1)
			}
		}

		out[line] = list
	}

	return out
}

// inlineSeverity is the lowest severity drawn as an inline row on a line,
// zero when the line gets none. The cursor line of the focused document
// follows cursor-line, every other line other-lines.
func (e *Editor) inlineSeverity(doc *Document, line int) int {
	cfg := e.Config.Editor.InlineDiagnostics
	if e.Doc() == doc && line == doc.Selection().Primary().CursorLine(doc.Text()) {
		return severityFilter(cfg.CursorLine)
	}

	return severityFilter(cfg.OtherLines)
}

// lineViewport builds the layout only parts of a viewport for row
// counting: tab width, wrapping, inlay widths and virtual lines.
func (e *Editor) lineViewport(v *View, doc *Document) ui.Viewport {
	vp := ui.Viewport{TabWidth: doc.TabWidth(), Wrap: e.WrapOptions(v, doc), Width: v.Area.W, Height: v.TextHeight(), FirstLine: v.FirstLine, FirstRow: v.FirstRow}
	text := doc.Text()
	inlays := map[int][]ui.Inlay{}
	for _, h := range doc.hints.hints {
		pos := min(h.Pos, text.Len())
		line := text.CharToLine(pos)
		inlays[line] = append(inlays[line], ui.Inlay{Char: pos - text.LineToChar(line), Text: h.Text})
	}

	inline := e.InlineDiagnostics(doc)
	for line, d := range e.eolDiagnostics(doc, 0, doc.LineCount(), inline) {
		inlays[line] = append(inlays[line], ui.Inlay{Char: d.Char, Text: d.Text})
	}

	for _, list := range inlays {
		slices.SortStableFunc(list, func(a, b ui.Inlay) int { return cmp.Compare(a.Char, b.Char) })
	}

	vp.Inlays = inlays
	virtual := map[int][]ui.VirtualLine{}
	for line, list := range inline {
		for _, d := range list {
			virtual[line] = append(virtual[line], ui.VirtualLine{Text: d.Text})
		}
	}

	vp.VirtualLines = virtual

	return vp
}

// usesRows reports whether lines can take more than one screen row.
func (e *Editor) usesRows(v *View, doc *Document) bool {
	if e.WrapOptions(v, doc) != nil {
		return true
	}

	cfg := e.Config.Editor.InlineDiagnostics

	return severityFilter(cfg.CursorLine) != 0 || severityFilter(cfg.OtherLines) != 0
}

// rowsTo counts the screen rows of the lines from the top of the
// view up to but excluding line, plus the row offset of pos inside line.
func (e *Editor) rowsTo(v *View, doc *Document, vp ui.Viewport, line, pos int) int {
	rows := -v.FirstRow
	last := doc.LineCount() - 1
	for l := v.FirstLine; l < line && l <= last; l++ {
		rows += vp.RowCount(doc.Line(l), l, l == last)
	}

	lineStart := doc.Text().LineToChar(min(line, last))
	r, _ := ui.WrapPosition(rowsAround(vp, doc, line, pos-lineStart, 1), pos-lineStart)

	return rows + r
}

// rowsAround lays out the rows of a line up to the row of char offset
// col plus extra rows. The rows before a char depend only on the text up
// to the end of its row, a long line is not laid out to its end for a
// cursor near its start. The limit is measured in columns past the char,
// so a text of many chars per cell gets its rows complete too.
func rowsAround(vp ui.Viewport, doc *Document, line, col, extra int) [][]ui.Glyph {
	width := 1
	if vp.Wrap != nil {
		width = max(vp.Wrap.Width, 1)
	}

	vp.MaxChar = col + 1
	vp.ExtraCols = width*(extra+2) + 1
	last := doc.LineCount() - 1

	return vp.LineRows(doc.Line(line), line, line == last)
}

// lineRowCount returns the screen rows of a line without virtual lines.
func (e *Editor) lineRowCount(doc *Document, vp ui.Viewport, line int) int {
	last := doc.LineCount() - 1

	return len(vp.LineRows(doc.Line(line), line, line == last))
}

// moveVisualLines moves the cursor by count wrapped rows, crossing into
// neighbouring document lines, keeping the visual column.
func (e *Editor) moveVisualLines(v *View, doc *Document, r edit.Range, dir edit.Direction, count int, extend bool) edit.Range {
	vp := e.lineViewport(v, doc)
	text := doc.Text()
	pos := r.Cursor(text)
	last := doc.LineCount() - 1
	line := text.CharToLine(pos)
	rows := vp.LineRows(doc.Line(line), line, line == last)
	row, col := ui.WrapPosition(rows, pos-text.LineToChar(line))
	if r.OldVisual.Valid {
		col = r.OldVisual.Col
	}

	for range count {
		if dir == edit.Forward {
			if row+1 < len(rows) {
				row++
			} else if line < last {
				line++
				rows = vp.LineRows(doc.Line(line), line, line == last)
				row = 0
			}
		} else {
			if row > 0 {
				row--
			} else if line > 0 {
				line--
				rows = vp.LineRows(doc.Line(line), line, line == last)
				row = len(rows) - 1
			}
		}
	}

	newPos := text.LineToChar(line) + ui.WrapCharAt(rows, row, col)
	if extend && doc.Line(line) == "" {
		return r
	}

	out := r.PutCursor(text, newPos, extend)
	out.OldVisual = edit.VisualPosition{Col: col, Valid: true}

	return out
}

// CursorRow returns the screen row and text column of a char when lines
// take several rows, ok is false when plain layout applies.
func (e *Editor) CursorRow(v *View, doc *Document, pos int) (int, int, bool) {
	if !e.usesRows(v, doc) {
		return 0, 0, false
	}

	vp := e.lineViewport(v, doc)
	text := doc.Text()
	line := text.CharToLine(min(pos, text.Len()))
	if line < v.FirstLine {
		return -1, 0, true
	}

	col := pos - text.LineToChar(line)
	_, c := ui.WrapPosition(rowsAround(vp, doc, line, col, 1), col)

	return e.rowsTo(v, doc, vp, line, pos), c, true
}

// DiagnosticsHidden reports whether diagnostics stay off the text right
// now, while inserting with hide-diagnostics-in-insert on.
func (e *Editor) DiagnosticsHidden() bool {
	return e.Config.Editor.HideDiagnosticsInInsert && e.Mode == Insert
}

// cursorBoxShown reports whether the diagnostics under the cursor go to
// a box in the top right corner of the view, which is the case unless
// inline diagnostics already show them under the cursor line.
func (e *Editor) cursorBoxShown() bool {
	return severityFilter(e.Config.Editor.InlineDiagnostics.CursorLine) == 0
}

// CursorDiagnostics returns the diagnostics whose range touches the
// primary cursor of a view, for the box in its top right corner.
func (e *Editor) CursorDiagnostics(v *View) []Diagnostic {
	if !e.cursorBoxShown() || e.DiagnosticsHidden() {
		return nil
	}

	doc, ok := e.Docs[v.DocID]
	if !ok {
		return nil
	}

	cursor := doc.SelectionFor(v.ID).Primary().Cursor(doc.Text())
	var out []Diagnostic
	for _, d := range doc.diagnostics {
		if d.Range.From() <= cursor && cursor <= d.Range.To() {
			out = append(out, d)
		}
	}

	return out
}
