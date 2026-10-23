package app

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/theme"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

const (
	maxPromptRows  = 10
	promptDocWidth = 90
	maxPopupWidth  = 80
	maxPopupHeight = 15
	maxMenuRows    = 10
	menuWidth      = 50
)

func (m Model) diagStyle(severity int) theme.Style {
	switch severity {
	case lsp.SeverityError:
		return m.theme.DiagError
	case lsp.SeverityWarning:
		return m.theme.DiagWarning
	case lsp.SeverityInfo:
		return m.theme.DiagInfo
	default:
		return m.theme.DiagHint
	}
}

// diagnosticMarks picks the worst diagnostic per line for the gutter.
func (m Model) diagnosticMarks(doc *editor.Document) map[int]ui.Mark {
	diags := doc.Diagnostics()
	if len(diags) == 0 {
		return nil
	}

	text := doc.Text()
	marks := map[int]ui.Mark{}
	worst := map[int]int{}
	for _, d := range diags {
		line := text.CharToLine(min(d.Range.From(), text.Len()))
		sev := d.Severity
		if sev == 0 {
			sev = lsp.SeverityHint
		}

		if cur, ok := worst[line]; ok && cur <= sev {
			continue
		}

		worst[line] = sev
		style := m.theme.Background.Patch(m.severityTextStyle(sev))
		marks[line] = ui.Mark{Symbol: "●", Style: style}
	}

	return marks
}

// severityTextStyle colors diagnostic text, unknown severities as hints.
func (m Model) severityTextStyle(sev int) theme.Style {
	// Protocol severities run from error 1 to hint 4, the table the
	// other way round.
	if sev < lsp.SeverityError || sev > lsp.SeverityHint {
		sev = lsp.SeverityHint
	}

	return m.theme.SeverityText[lsp.SeverityHint-sev]
}

// renderPopup places hover text below the cursor, or above when there
// is more room there.
func (m Model) renderPopup(p *editor.Popup, cursor *tea.Cursor) *lipgloss.Layer {
	width, height := ui.PopupSize(p.Lines, min(maxPopupWidth, m.width), min(maxPopupHeight, m.height-2))
	layer, _ := m.placeOverlay(overlayBox{
		width: width, height: height, x: cursor.X, cursorY: cursor.Y, z: 5, kind: "popup",
		sides:  []overlaySide{sideBelow, sideAbove},
		render: func(w, h int) string { return ui.RenderPopup(p.Lines, p.Scroll, w, h, m.theme) },
	})

	return layer
}

// menuOffset is the first row of a menu of height rows that shows the
// selected one, the height being what the screen left for it.
func menuOffset(selected, height int) int {
	return max(selected-height+1, 0)
}

// overlaySide is where a box sits relative to the cursor row.
type overlaySide int

const (
	sideBelow overlaySide = iota
	sideAbove
)

// overlayBox is content to place next to the cursor row.
type overlayBox struct {
	width, height int
	x, cursorY, z int
	kind          string

	// sides lists the sides to try in order, the first with room for the
	// whole box wins, else the one with more room.
	sides  []overlaySide
	render func(width, height int) string
}

// placeOverlay frames a box when popup-border asks for it and places it
// on a side of the cursor row, never over that row and never past the
// screen. The height shrinks to the room left, nil when none is.
func (m Model) placeOverlay(b overlayBox) (*lipgloss.Layer, overlaySide) {
	frame := 0
	if border := m.editor.Config.Editor.PopupBorder; border == "all" || border == b.kind {
		frame = 1
	}

	room := func(side overlaySide) int {
		if side == sideAbove {
			return b.cursorY - 2*frame
		}

		// The last row is the command line.
		return m.height - 1 - (b.cursorY + 1) - 2*frame
	}

	side := b.sides[0]
	for i, s := range b.sides {
		if room(s) >= b.height {
			side = s

			break
		}

		if i > 0 && room(s) > room(side) {
			side = s
		}
	}

	height := min(b.height, room(side))
	width := min(b.width, m.width-2*frame)
	if height < 1 || width < 1 {
		return nil, side
	}

	content := b.render(width, height)
	if frame == 1 {
		// Same frame as the pickers.
		content = ui.Bordered(content, m.theme, m.theme.Popup.Patch(m.theme.PickerBorder))
	}

	outerW, outerH := width+2*frame, height+2*frame
	x := min(max(b.x-frame, 0), max(m.width-outerW, 0))
	y := b.cursorY + 1
	if side == sideAbove {
		y = b.cursorY - outerH
	}

	return lipgloss.NewLayer(content).X(x).Y(y).Z(b.z), side
}

// renderCompletionDoc places the documentation of the selected item
// beside the menu, on the side with more room.
func (m Model) renderCompletionDoc(menu *editor.CompletionMenu, menuLayer *lipgloss.Layer) *lipgloss.Layer {
	lines := menu.Doc()
	if len(lines) == 0 || !m.editor.Config.Editor.LSP.DisplayCompletionDocs {
		return nil
	}

	frame := 0
	if border := m.editor.Config.Editor.PopupBorder; border == "all" || border == "popup" {
		frame = 1
	}

	right := m.width - (menuLayer.GetX() + menuLayer.Width())
	left := menuLayer.GetX()
	room := max(right, left) - 2*frame
	y := menuLayer.GetY()
	maxHeight := m.height - 1 - y - 2*frame
	if room < 12 || maxHeight < 1 {
		return nil
	}

	width, height := ui.PopupSize(lines, min(maxPopupWidth, room), min(maxPopupHeight, maxHeight))
	content := ui.RenderPopup(lines, 0, width, height, m.theme)
	if frame == 1 {
		content = ui.Bordered(content, m.theme, m.theme.Popup.Patch(m.theme.PickerBorder))
	}

	x := menuLayer.GetX() + menuLayer.Width()
	if right < left && right < width+2*frame {
		x = menuLayer.GetX() - width - 2*frame
	}

	return lipgloss.NewLayer(content).X(x).Y(y).Z(6)
}

// renderCompletion places the menu under the word being completed.
func (m Model) renderCompletion(menu *editor.CompletionMenu, cursor *tea.Cursor) (*lipgloss.Layer, overlaySide) {
	items := menu.Items()
	if len(items) == 0 {
		return nil, sideBelow
	}

	rows := make([]ui.MenuItem, len(items))
	for i, it := range items {
		rows[i] = ui.MenuItem{Label: it.Label, Kind: completionKind(it.Kind), Detail: it.Detail}
	}

	height := min(len(rows), maxMenuRows)

	ed := m.editor
	doc := ed.Doc()
	text := doc.Text()
	primary := doc.Selection().Primary().Cursor(text)
	startCol := cursor.X - (primary - menu.Start())
	layer, side := m.placeOverlay(overlayBox{
		width: min(menuWidth, m.width), height: height, x: startCol, cursorY: cursor.Y, z: 6, kind: "menu",
		sides: []overlaySide{sideBelow, sideAbove},
		render: func(w, h int) string {
			return ui.RenderMenu(rows, menu.Selected(), menuOffset(menu.Selected(), h), w, h, m.theme)
		},
	})

	return layer, side
}

// renderMenu places a selection list under the cursor sized to its
// longest entry.
func (m Model) renderMenu(menu *editor.Menu, cursor *tea.Cursor) *lipgloss.Layer {
	// The title takes the first row, the selection moves down by one.
	var rows []ui.MenuItem
	width, kindWidth := 0, 0
	if menu.Title != "" {
		rows = append(rows, ui.MenuItem{Label: menu.Title})
		width = lipgloss.Width(menu.Title) + 2
	}

	for i, it := range menu.Items {
		row := ui.MenuItem{Label: it}
		if i < len(menu.Kinds) {
			row.Kind = menu.Kinds[i]
			kindWidth = max(kindWidth, lipgloss.Width(menu.Kinds[i]))
		}

		rows = append(rows, row)
		width = max(width, lipgloss.Width(it)+2)
	}

	if kindWidth > 0 {
		width += kindWidth + 2
	}

	width = min(max(width, 20), m.width)
	height := min(len(rows), maxMenuRows)
	selected := menu.Selected() + len(rows) - len(menu.Items)

	layer, _ := m.placeOverlay(overlayBox{
		width: width, height: height, x: cursor.X, cursorY: cursor.Y, z: 6, kind: "menu",
		sides: []overlaySide{sideBelow, sideAbove},
		render: func(w, h int) string {
			return ui.RenderMenu(rows, selected, menuOffset(selected, h), w, h, m.theme)
		},
	})

	return layer
}

// diffMarks builds the version control gutter column from the hunks.
func (m Model) diffMarks(doc *editor.Document) map[int]ui.Mark {
	hunks := doc.DiffHunks()
	if len(hunks) == 0 {
		return nil
	}

	lineCount := doc.LineCount()
	marks := map[int]ui.Mark{}
	for _, h := range hunks {
		switch {
		case h.BEnd == h.BStart:
			// Deleted lines are marked on the line that follows them.
			line, symbol := h.BStart, "▔"
			if line >= lineCount {
				line, symbol = lineCount-1, "▁"
			}

			marks[line] = ui.Mark{Symbol: symbol, Style: m.theme.DiffDelete}
		case h.AEnd == h.AStart:
			for line := h.BStart; line < h.BEnd && line < lineCount; line++ {
				marks[line] = ui.Mark{Symbol: "▍", Style: m.theme.DiffAdd}
			}
		default:
			for line := h.BStart; line < h.BEnd && line < lineCount; line++ {
				marks[line] = ui.Mark{Symbol: "▍", Style: m.theme.DiffChange}
			}
		}
	}

	return marks
}

// eolDiagnostics styles the editor's end of line diagnostics.
func (m Model) eolDiagnostics(doc *editor.Document, firstLine, lastLine int) map[int]ui.Inlay {
	out := map[int]ui.Inlay{}
	for line, d := range m.editor.EOLDiagnostics(doc, firstLine, lastLine) {
		out[line] = ui.Inlay{Char: d.Char, Text: d.Text, Style: m.severityTextStyle(d.Severity)}
	}

	return out
}

// virtualLines styles the inline diagnostics rows.
func (m Model) virtualLines(doc *editor.Document) map[int][]ui.VirtualLine {
	out := map[int][]ui.VirtualLine{}
	for line, list := range m.editor.InlineDiagnostics(doc) {
		for _, d := range list {
			out[line] = append(out[line], ui.VirtualLine{Text: d.Text, Style: m.severityTextStyle(d.Severity)})
		}
	}

	return out
}

// inlays groups the inlay hints and end of line diagnostics of the
// visible lines by line.
func (m Model) inlays(doc *editor.Document, firstLine, lastLine int) map[int][]ui.Inlay {
	hints := doc.InlayHints()
	eol := m.eolDiagnostics(doc, firstLine, lastLine)
	blameLine, blame, hasBlame := m.editor.InlineBlame(doc)
	if len(hints) == 0 && len(eol) == 0 && !hasBlame {
		return nil
	}

	text := doc.Text()
	out := map[int][]ui.Inlay{}
	for line, in := range eol {
		out[line] = append(out[line], in)
	}

	// The blame follows a diagnostic at the end of the same line.
	if hasBlame && blameLine >= firstLine && blameLine < lastLine {
		end := utf8.RuneCountInString(strings.TrimRight(doc.Line(blameLine), "\r\n"))
		out[blameLine] = append(out[blameLine], ui.Inlay{Char: end, Text: "    " + blame, Style: m.theme.Blame})
	}

	for _, h := range hints {
		pos := min(h.Pos, text.Len())
		line := text.CharToLine(pos)
		if line < firstLine || line >= lastLine {
			continue
		}

		style := m.theme.InlayHint
		switch h.Kind {
		case lsp.InlayHintParameter:
			style = m.theme.InlayHintParam
		case lsp.InlayHintType:
			style = m.theme.InlayHintType
		}

		out[line] = append(out[line], ui.Inlay{Char: pos - text.LineToChar(line), Text: h.Text, Style: style})
	}

	for _, list := range out {
		slices.SortStableFunc(list, func(a, b ui.Inlay) int { return cmp.Compare(a.Char, b.Char) })
	}

	return out
}

// renderSignatureHelp places the signature above the cursor, or below
// when there is no room, away from the completion menu.
func (m Model) renderSignatureHelp(sh *editor.SignatureHelp, cursor *tea.Cursor, sides []overlaySide) *lipgloss.Layer {
	sig := ui.Signature{Label: sh.Label, ActiveStart: sh.ParamStart, ActiveEnd: sh.ParamEnd, Docs: sh.Docs, Index: sh.Index, Count: sh.Count}
	width, height := ui.SignatureSize(sig, min(maxPopupWidth, m.width), min(maxPopupHeight, max(m.height-2, 1)))
	layer, _ := m.placeOverlay(overlayBox{
		width: width, height: height, x: cursor.X, cursorY: cursor.Y, z: 5, kind: "popup",
		sides:  sides,
		render: func(w, h int) string { return ui.RenderSignatureHelp(sig, w, h, m.theme) },
	})

	return layer
}

// renderPromptCompletions draws the command completion table above the
// command line and the command doc above the table.
func (m Model) renderPromptCompletions(p *editor.Prompt) []*lipgloss.Layer {
	var layers []*lipgloss.Layer
	items := p.Completions()
	rows := ui.PromptCompletionRows(items, m.width, min(maxPromptRows, max(m.height-2, 0)))
	top := m.height - 1
	if rows > 0 {
		top -= rows
		layers = append(layers, lipgloss.NewLayer(ui.RenderPromptCompletions(items, p.Selected(), m.width, rows, m.theme)).Y(top).Z(7))
	}

	if doc := p.Doc(); doc != "" && top > 0 {
		lines := strings.Split(doc, "\n")
		width, height := ui.PopupSize(ui.PlainLines(lines), min(promptDocWidth, m.width), min(top, maxPopupHeight))
		layers = append(layers, lipgloss.NewLayer(ui.RenderPopup(ui.PlainLines(lines), 0, width, height, m.theme)).Y(top-height).Z(7))
	}

	return layers
}

// renderInfo places the key hint box at the bottom right above the
// statusline and command line.
func (m Model) renderInfo(info *editor.Info) *lipgloss.Layer {
	entries := make([]ui.InfoEntry, len(info.Entries))
	for i, e := range info.Entries {
		entries[i] = ui.InfoEntry{Keys: strings.Join(e.Keys, ", "), Desc: e.Desc}
	}

	// Rows left above the statusline and command line, minus the border.
	maxRows := max(m.height-4, 1)
	width, height := ui.InfoSize(info.Title, entries, maxRows)
	width = min(width, m.width)
	x := max(m.width-width, 0)
	y := max(m.height-2-height, 0)

	return lipgloss.NewLayer(ui.RenderInfo(info.Title, entries, maxRows, width, height, m.theme)).X(x).Y(y).Z(4)
}

// jumpOverlays draws the remaining chars of every jump label over the
// word they point at, in the focused view only.
func (m Model) jumpOverlays(doc *editor.Document, v *editor.View) map[int][]ui.Overlay {
	ed := m.editor
	if len(ed.JumpLabels) == 0 || v.ID != ed.Focus || doc.ID != ed.Doc().ID {
		return nil
	}

	text := doc.Text()
	typed := utf8.RuneCountInString(ed.JumpTyped())
	out := map[int][]ui.Overlay{}
	for _, l := range ed.JumpLabels {
		line := text.CharToLine(l.Pos)
		col := l.Pos - text.LineToChar(line)
		for i, ch := range []rune(l.Label)[typed:] {
			out[line] = append(out[line], ui.Overlay{Char: col + i, Text: string(ch), Style: m.theme.JumpLabel})
		}
	}

	return out
}

var completionKinds = []string{"", "text", "method", "function", "constructor", "field", "variable", "class", "interface", "module", "property", "unit", "value", "enum", "keyword", "snippet", "color", "file", "reference", "folder", "enum_member", "constant", "struct", "event", "operator", "type_param"}

func completionKind(k int) string {
	if k > 0 && k < len(completionKinds) {
		return completionKinds[k]
	}

	return ""
}

// Bounds of the cursor diagnostics box.
const (
	diagnosticBoxWidth  = 100
	diagnosticBoxHeight = 15
)

// cursorDiagnostics draws the diagnostics under the cursor of a view in
// its top right corner, right aligned, one layer per row so the text
// left of each row stays visible.
func (m Model) cursorDiagnostics(v *editor.View, top int) []*lipgloss.Layer {
	diags := m.editor.CursorDiagnostics(v)
	if len(diags) == 0 {
		return nil
	}

	width := min(diagnosticBoxWidth, v.Area.W)
	type row struct {
		text  string
		style theme.Style

		// block is the width of a group of rows aligned on their left
		// side, the source line and its caret, zero for a row aligned
		// right on its own.
		block int
	}

	doc := m.editor.Docs[v.DocID]
	var rows []row
	for _, d := range diags {
		style := m.theme.Background.Patch(m.severityTextStyle(d.Severity))
		for line := range strings.SplitSeq(strings.TrimRight(d.Message, "\n "), "\n") {
			line = strings.TrimRight(strings.ReplaceAll(line, "\t", "    "), " ")
			for wrapped := range strings.SplitSeq(ansi.Wrap(line, width, ""), "\n") {
				rows = append(rows, row{text: wrapped, style: style})
			}
		}

		// A zero code, as yaml-language-server sends for every error, says
		// nothing.
		if d.Code != "" && d.Code != "0" {
			rows = append(rows, row{text: "(" + d.Code + ")", style: style})
		}

		if src, caret, ok := diagnosticFrame(doc, d, width); ok {
			block := max(lipgloss.Width(src), lipgloss.Width(caret))
			rows = append(rows,
				row{},
				row{text: src, style: m.theme.Background.Patch(m.theme.TextInactive), block: block},
				row{text: caret, style: style, block: block},
			)
		}
	}

	rows = rows[:min(len(rows), diagnosticBoxHeight, max(v.Area.H-2, 0))]
	layers := make([]*lipgloss.Layer, 0, len(rows))
	for i, r := range rows {
		if r.text == "" {
			continue
		}

		w := cmp.Or(r.block, lipgloss.Width(r.text))
		layers = append(layers, lipgloss.NewLayer(m.theme.Render(r.style, r.text)).
			X(v.Area.X+v.Area.W-w).Y(v.Area.Y+top+1+i).Z(3))
	}

	return layers
}

// diagnosticFrame returns the source line of a diagnostic and a caret
// line pointing at its start. The indent is left out and a line wider
// than width is cut around the caret, with an ellipsis on a cut side.
func diagnosticFrame(doc *editor.Document, d editor.Diagnostic, width int) (string, string, bool) {
	if doc == nil || width < 8 || doc.Text().Len() == 0 {
		return "", "", false
	}

	text := doc.Text()
	pos := min(d.Range.From(), text.Len())
	line := text.CharToLine(pos)
	offset := pos - text.LineToChar(line)
	glyphs := ui.LayoutWith(strings.TrimRight(doc.Line(line), "\r\n"), ui.LayoutOptions{TabWidth: doc.TabWidth()})
	lineWidth := ui.LineWidth(glyphs)

	col := lineWidth
	for _, g := range glyphs {
		if offset < g.Char+g.Chars {
			col = g.Col

			break
		}
	}

	indent := lineWidth
	for _, g := range glyphs {
		if !g.Space {
			indent = g.Col

			break
		}
	}

	start, end := min(indent, col), lineWidth
	room := width - 2
	if end-start > room {
		start = max(start, col-room*2/3)
		end = min(end, start+room)
	}

	var prefix, suffix string
	if start > indent {
		prefix = "\u2026"
	}

	if end < lineWidth {
		suffix = "\u2026"
	}

	var body strings.Builder
	for _, g := range glyphs {
		if g.Col >= start && g.Col+g.Width <= end {
			body.WriteString(g.Text)
		}
	}

	caret := strings.Repeat(" ", lipgloss.Width(prefix)+col-start) + "^"

	return prefix + body.String() + suffix, caret, true
}
