package ui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// wrapPlain breaks plain text lines at spaces to fit width cells, words
// longer than a line are cut. Tabs become four spaces.
func wrapPlain(lines []string, width int) []string {
	width = max(width, 1)
	var out []string
	for _, line := range lines {
		line = strings.ReplaceAll(line, "\t", "    ")
		for lipgloss.Width(line) > width {
			fit := ansi.Truncate(line, width, "")
			idx := strings.LastIndex(fit, " ")
			if idx <= 0 {
				idx = len(fit)
			}

			out = append(out, strings.TrimRight(line[:idx], " "))
			line = strings.TrimLeft(line[idx:], " ")
		}

		out = append(out, line)
	}

	return out
}

// RenderPopup draws text lines in a box of the given size with the popup
// background, wrapping long lines.
func RenderPopup(lines []string, scroll, width, height int, th *Theme) string {
	style := th.Popup.Patch(th.Text)
	wrapped := wrapPlain(lines, width-2)
	wrapped = wrapped[max(min(scroll, len(wrapped)-height), 0):]
	var sb strings.Builder
	for row := range height {
		text := ""
		if row < len(wrapped) {
			text = wrapped[row]
		}

		text = " " + text
		sb.WriteString(th.Render(style, text+strings.Repeat(" ", max(width-lipgloss.Width(text), 0))))
		if row < height-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// PopupSize returns the box size for lines within the given limits, the
// height counting the wrapped rows.
func PopupSize(lines []string, maxWidth, maxHeight int) (int, int) {
	width := 0
	for _, l := range lines {
		width = max(width, lipgloss.Width(strings.ReplaceAll(l, "\t", "    "))+2)
	}

	width = min(max(width, 10), maxWidth)
	height := len(wrapPlain(lines, width-2))

	return width, min(max(height, 1), maxHeight)
}

// MenuItem is one completion row.
type MenuItem struct {
	Label  string
	Kind   string
	Detail string
}

// RenderMenu draws a completion menu with the selected row emphasized.
func RenderMenu(items []MenuItem, selected, offset, width, height int, th *Theme) string {
	base := th.Menu.Patch(th.Text)
	kindWidth := 0
	for _, it := range items {
		kindWidth = max(kindWidth, lipgloss.Width(it.Kind))
	}

	var sb strings.Builder
	for row := range height {
		idx := offset + row
		style := base
		text := ""
		if idx < len(items) {
			it := items[idx]
			if idx == selected {
				style = base.Patch(th.MenuSelected)
			}

			label := it.Label
			if it.Detail != "" {
				label += "  " + it.Detail
			}

			text = " " + label
			if kindWidth > 0 {
				kind := it.Kind + strings.Repeat(" ", kindWidth-lipgloss.Width(it.Kind))
				text = ansi.Truncate(text, max(width-kindWidth-2, 0), "…")
				text += strings.Repeat(" ", max(width-kindWidth-1-lipgloss.Width(text), 0)) + kind + " "
			}
		}

		text = ansi.Truncate(text, width, "")
		sb.WriteString(th.Render(style, text+strings.Repeat(" ", max(width-lipgloss.Width(text), 0))))
		if row < height-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// Signature is the content of the signature help popup.
type Signature struct {
	Label string

	// ActiveStart and ActiveEnd delimit the active parameter in chars.
	ActiveStart, ActiveEnd int
	Docs                   []string

	// Index and Count are shown as a counter when there are overloads.
	Index, Count int
}

// SignatureSize returns the box size for a signature popup.
func SignatureSize(sig Signature, maxWidth, maxHeight int) (int, int) {
	width := lipgloss.Width(sig.Label+signatureCounter(sig)) + 2
	for _, l := range sig.Docs {
		width = max(width, lipgloss.Width(strings.ReplaceAll(l, "\t", "    "))+2)
	}

	width = min(max(width, 10), maxWidth)

	return width, min(len(signatureRows(sig, width-2)), maxHeight)
}

func signatureCounter(sig Signature) string {
	if sig.Count > 1 {
		return " (" + strconv.Itoa(sig.Index+1) + "/" + strconv.Itoa(sig.Count) + ")"
	}

	return ""
}

// sigRow is one row of the signature popup: a piece of the label with
// the active parameter as rune offsets into it, the rule, or a doc line.
type sigRow struct {
	text   string
	active [2]int
	rule   bool
}

// signatureRows lays the signature out in inner cells: the label cut
// into rows, then a rule and the wrapped docs.
func signatureRows(sig Signature, inner int) []sigRow {
	inner = max(inner, 1)
	label := []rune(sig.Label + signatureCounter(sig))
	start := min(max(sig.ActiveStart, 0), len([]rune(sig.Label)))
	end := min(max(sig.ActiveEnd, start), len([]rune(sig.Label)))
	var rows []sigRow
	for from := 0; from < len(label) || from == 0; {
		to, cells := from, 0
		for to < len(label) && cells+lipgloss.Width(string(label[to])) <= inner {
			cells += lipgloss.Width(string(label[to]))
			to++
		}

		to = max(to, min(from+1, len(label)))
		rows = append(rows, sigRow{text: string(label[from:to]), active: [2]int{min(max(start-from, 0), to-from), min(max(end-from, 0), to-from)}})
		if to >= len(label) {
			break
		}

		from = to
	}

	if len(sig.Docs) > 0 {
		rows = append(rows, sigRow{rule: true})
		for _, l := range wrapPlain(sig.Docs, inner) {
			rows = append(rows, sigRow{text: l})
		}
	}

	return rows
}

// RenderSignatureHelp draws the signature with its active parameter
// emphasized, followed by the docs below a rule. Everything keeps the
// popup background, the active parameter only changes the text.
func RenderSignatureHelp(sig Signature, width, height int, th *Theme) string {
	style := th.Popup.Patch(th.Text)
	active := style.Patch(theme.Style{Fg: th.Special.Fg, Add: theme.ModBold | theme.ModUnderlined})
	rule := style.Patch(theme.Style{Fg: th.PickerBorder.Fg})
	inner := max(width-2, 1)
	rows := signatureRows(sig, inner)

	var sb strings.Builder
	for row := range height {
		var line string
		cells := 0
		if row < len(rows) {
			r := rows[row]
			switch {
			case r.rule:
				line, cells = th.Render(rule, strings.Repeat("─", inner)), inner
			default:
				runes := []rune(r.text)
				line = th.Render(style, string(runes[:r.active[0]])) + th.Render(active, string(runes[r.active[0]:r.active[1]])) +
					th.Render(style, string(runes[r.active[1]:]))
				cells = lipgloss.Width(r.text)
			}
		}

		sb.WriteString(th.Render(style, " "))
		sb.WriteString(line)
		if pad := width - 1 - cells; pad > 0 {
			sb.WriteString(th.Render(style, strings.Repeat(" ", pad)))
		}

		if row < height-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// promptBaseWidth is the minimum column width of the prompt completion
// table.
const promptBaseWidth = 30

// PromptCompletionRows returns how many rows the completion table needs
// for width columns, at most maxRows.
func PromptCompletionRows(items []string, width, maxRows int) int {
	if len(items) == 0 || width <= 0 {
		return 0
	}

	cols := promptColumns(items, width)

	return min((len(items)+cols-1)/cols, maxRows)
}

func promptColumns(items []string, width int) int {
	maxLen := promptBaseWidth
	for _, it := range items {
		maxLen = max(maxLen, lipgloss.Width(it))
	}

	return max(width/maxLen, 1)
}

// RenderPromptCompletions draws the candidates column by column in a
// table of the given size, paging so the selected item is visible.
func RenderPromptCompletions(items []string, selected, width, height int, th *Theme) string {
	base := th.Menu.Patch(th.Text)
	sel := base.Patch(th.MenuSelected)
	cols := promptColumns(items, width)
	colWidth := max((width-cols)/cols, 1)
	perPage := height * cols
	offset := 0
	if selected >= 0 && perPage > 0 {
		offset = selected / perPage * perPage
	}

	var sb strings.Builder
	for row := range height {
		used := 0
		for col := range cols {
			idx := offset + col*height + row
			text := ""
			style := base
			if idx < len(items) {
				text = ansi.Truncate(items[idx], colWidth-1, "")
				if idx == selected {
					style = sel
				}
			}

			cell := text + strings.Repeat(" ", max(colWidth-lipgloss.Width(text), 0))
			if col < cols-1 {
				cell += " "
			}

			sb.WriteString(th.Render(style, cell))
			used += lipgloss.Width(cell)
		}

		if used < width {
			sb.WriteString(th.Render(base, strings.Repeat(" ", width-used)))
		}

		if row < height-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// InfoEntry is one row of the key hint box.
type InfoEntry struct {
	Keys string
	Desc string
}

// maxInfoDesc bounds the description column so one long binding does
// not stretch the box across the screen.
const maxInfoDesc = 50

// infoColumn is one vertical run of entries with its column widths.
type infoColumn struct {
	entries   []InfoEntry
	keyWidth  int
	descWidth int
}

// infoColumns splits the entries into as few columns as fit in maxRows
// rows each when the box is taller than the screen.
func infoColumns(entries []InfoEntry, maxRows int) []infoColumn {
	if len(entries) == 0 {
		return nil
	}

	maxRows = max(maxRows, 1)
	count := (len(entries) + maxRows - 1) / maxRows
	rows := (len(entries) + count - 1) / count
	var cols []infoColumn
	for start := 0; start < len(entries); start += rows {
		col := infoColumn{entries: entries[start:min(start+rows, len(entries))]}
		for _, e := range col.entries {
			col.keyWidth = max(col.keyWidth, lipgloss.Width(e.Keys))
			col.descWidth = max(col.descWidth, min(lipgloss.Width(e.Desc), maxInfoDesc))
		}

		cols = append(cols, col)
	}

	return cols
}

func (c infoColumn) width() int {
	return c.keyWidth + 2 + c.descWidth
}

// InfoSize returns the outer size of the hint box: the columns of
// aligned keys and descriptions, a one cell margin on both sides of each
// column and the border. Entries flow into more columns when they need
// more than maxRows rows.
func InfoSize(title string, entries []InfoEntry, maxRows int) (int, int) {
	cols := infoColumns(entries, maxRows)
	inner := 0
	rows := 0
	for _, c := range cols {
		inner += c.width() + 2
		rows = max(rows, len(c.entries))
	}

	inner = max(inner, lipgloss.Width(title))

	return inner + 2, rows + 2
}

// RenderInfo draws the bordered hint box with the keys aligned per
// column.
func RenderInfo(title string, entries []InfoEntry, maxRows, width, height int, th *Theme) string {
	border := th.PopupInfo
	text := th.PopupInfo.Patch(th.TextInfo)
	inner := max(width-2, 0)
	cols := infoColumns(entries, maxRows)

	var sb strings.Builder
	top := "┌" + ansi.Truncate(title, inner, "") + strings.Repeat("─", max(inner-lipgloss.Width(title), 0)) + "┐"
	sb.WriteString(th.Render(border, top))
	for row := range max(height-2, 0) {
		var line strings.Builder
		for _, c := range cols {
			cell := ""
			if row < len(c.entries) {
				e := c.entries[row]
				cell = e.Keys + strings.Repeat(" ", c.keyWidth-lipgloss.Width(e.Keys)) + "  " + ansi.Truncate(e.Desc, maxInfoDesc, "…")
			}

			line.WriteString(" " + cell + strings.Repeat(" ", max(c.width()-lipgloss.Width(cell), 0)) + " ")
		}

		cells := ansi.Truncate(line.String(), inner, "…")
		sb.WriteByte('\n')
		sb.WriteString(th.Render(border, "│"))
		sb.WriteString(th.Render(text, cells+strings.Repeat(" ", max(inner-lipgloss.Width(cells), 0))))
		sb.WriteString(th.Render(border, "│"))
	}

	sb.WriteByte('\n')
	sb.WriteString(th.Render(border, "└"+strings.Repeat("─", inner)+"┘"))

	return sb.String()
}
