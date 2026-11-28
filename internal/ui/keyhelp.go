package ui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// KeyHelpRow is one row of the key help, a section heading or a binding.
type KeyHelpRow struct {
	Heading bool
	Keys    string
	Desc    string
}

// KeyHelpView is what the key help box shows: the visible rows, the
// filter and the count of bindings.
type KeyHelpView struct {
	Width, Height int
	Title         string
	Query         string
	Matched       int
	Total         int
	Rows          []KeyHelpRow

	// KeyWidth is the width of the keys column, taken over all rows so
	// it stays the same while the list scrolls.
	KeyWidth int
}

// KeyHelpKeyWidth is the widest keys of rows.
func KeyHelpKeyWidth(rows []KeyHelpRow) int {
	width := 0
	for _, r := range rows {
		if !r.Heading {
			width = max(width, lipgloss.Width(r.Keys))
		}
	}

	return width
}

// KeyHelpPromptX is the column of the filter inside the box.
const KeyHelpPromptX = 2

// RenderKeyHelp draws the key help box in the picker style: the filter,
// a rule, then headings and bindings with the keys in one column.
func RenderKeyHelp(v KeyHelpView, th *Theme) string {
	bg := th.Picker
	base := bg.Patch(th.Text)
	border := bg.Patch(th.PickerBorder)
	inner := max(v.Width-4, 1)
	keyWidth := min(v.KeyWidth, inner/3)
	counter := strconv.Itoa(v.Matched) + "/" + strconv.Itoa(v.Total)
	lines := make([]string, 0, v.Height)
	lines = append(lines, boxTop(v.Title, v.Width, th, border))
	lines = append(lines, boxRow(pickerPrompt(v.Query, counter, inner, th, base), th, border, base))
	lines = append(lines, th.Render(border, "│")+th.Render(base.Patch(th.Separator), strings.Repeat("─", max(v.Width-2, 0)))+th.Render(border, "│"))
	for i := range max(v.Height-4, 0) {
		line := th.Render(base, strings.Repeat(" ", inner))
		if i < len(v.Rows) {
			line = keyHelpRow(v.Rows[i], inner, keyWidth, th, base)
		}

		lines = append(lines, boxRow(line, th, border, base))
	}

	lines = append(lines, boxBottom(v.Width, th, border))

	return strings.Join(lines, "\n")
}

func keyHelpRow(r KeyHelpRow, width, keyWidth int, th *Theme, base theme.Style) string {
	if r.Heading {
		text := ansi.Truncate(r.Desc, width, "…")

		return th.Render(base.Patch(theme.Style{Add: theme.ModBold}), text) + th.Render(base, strings.Repeat(" ", max(width-lipgloss.Width(text), 0)))
	}

	keys := ansi.Truncate(r.Keys, keyWidth, "…")
	keys = "  " + keys + strings.Repeat(" ", max(keyWidth-lipgloss.Width(keys), 0)) + "  "
	desc := ansi.Truncate(r.Desc, max(width-lipgloss.Width(keys), 0), "…")
	pad := max(width-lipgloss.Width(keys)-lipgloss.Width(desc), 0)

	return th.Render(base.Patch(th.Special), keys) + th.Render(base, desc+strings.Repeat(" ", pad))
}
