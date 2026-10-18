package ui

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// PickerRow is one list entry to draw.
type PickerRow struct {
	Text      string
	Positions []int
	Selected  bool

	// Dir marks a directory entry, drawn in the directory style.
	Dir bool

	// Note follows Text in the dimmed style.
	Note string

	// TruncateStart cuts a too long Text at its start, so a path keeps
	// its file name.
	TruncateStart bool
}

// PickerView is everything the picker overlay renders.
type PickerView struct {
	Width   int
	Height  int
	Title   string
	Query   string
	Loading bool

	// Matched and Total feed the counter next to the prompt.
	Matched int
	Total   int
	Rows    []PickerRow

	// Preview holds prerendered lines for the right pane, nil hides it.
	// They are PickerLayout.PreviewWidth wide.
	Preview      []string
	PreviewTitle string
}

// MinPreviewWidth is the picker width below which the preview hides.
const MinPreviewWidth = 72

// PickerLayout splits the picker box into a bordered list pane, with the
// prompt above a separator, and an optional bordered preview pane. Every
// pane keeps one blank column inside its border on both sides.
type PickerLayout struct {
	// ListBox and PreviewBox are the outer widths of the panes,
	// PreviewBox is zero without a preview.
	ListBox    int
	PreviewBox int

	// ListWidth and ListHeight are the room for the entries.
	ListWidth  int
	ListHeight int

	// PreviewWidth and PreviewHeight are the room for preview lines.
	PreviewWidth  int
	PreviewHeight int

	// PromptX and PromptY place the start of the query in the box.
	PromptX int
	PromptY int
}

// pickerChrome is the rows the list pane spends on borders, the prompt and
// the separator.
const pickerChrome = 4

// NewPickerLayout lays out a width by height picker box.
func NewPickerLayout(width, height int, preview bool) PickerLayout {
	l := PickerLayout{ListBox: width, PromptX: 2, PromptY: 1}
	if preview {
		l.ListBox = width / 2
		l.PreviewBox = width - l.ListBox
		l.PreviewWidth = max(l.PreviewBox-4, 0)
		l.PreviewHeight = max(height-2, 0)
	}

	l.ListWidth = max(l.ListBox-4, 0)
	l.ListHeight = max(height-pickerChrome, 0)

	return l
}

// RenderPicker draws the overlay box.
func RenderPicker(v PickerView, th *Theme) string {
	bg := th.Picker
	base := bg.Patch(th.Text)
	border := bg.Patch(th.PickerBorder)
	l := NewPickerLayout(v.Width, v.Height, v.Preview != nil)

	counter := strconv.Itoa(v.Matched) + "/" + strconv.Itoa(v.Total)
	if v.Loading {
		counter = "~" + counter
	}

	list := make([]string, 0, v.Height)
	list = append(list, boxTop(v.Title, l.ListBox, th, border))
	list = append(list, boxRow(pickerPrompt(v.Query, counter, l.ListWidth, th, base), th, border, base))
	list = append(list, th.Render(border, "│")+th.Render(base.Patch(th.Separator), strings.Repeat("─", max(l.ListBox-2, 0)))+th.Render(border, "│"))
	for i := range l.ListHeight {
		line := th.Render(base, strings.Repeat(" ", l.ListWidth))
		if i < len(v.Rows) {
			line = renderPickerRow(v.Rows[i], l.ListWidth, th, base)
		}

		list = append(list, boxRow(line, th, border, base))
	}

	list = append(list, boxBottom(l.ListBox, th, border))

	if v.Preview == nil {
		return strings.Join(list, "\n")
	}

	// The preview shows document text, so its pane uses the editor
	// background.
	docBase := th.Base()
	preview := make([]string, 0, v.Height)
	preview = append(preview, boxTop(truncateStart(v.PreviewTitle, l.PreviewBox-6), l.PreviewBox, th, border))
	for i := range l.PreviewHeight {
		line := ""
		if i < len(v.Preview) {
			line = ansi.Truncate(v.Preview[i], l.PreviewWidth, "")
		}

		if pad := l.PreviewWidth - lipgloss.Width(line); pad > 0 {
			line += th.Render(docBase, strings.Repeat(" ", pad))
		}

		preview = append(preview, boxRow(line, th, border, docBase))
	}

	preview = append(preview, boxBottom(l.PreviewBox, th, border))

	var sb strings.Builder
	for i := range list {
		if i > 0 {
			sb.WriteByte('\n')
		}

		sb.WriteString(list[i])
		if i < len(preview) {
			sb.WriteString(preview[i])
		}
	}

	return sb.String()
}

// pickerPrompt is the query line with the match counter on the right,
// exactly width cells.
func pickerPrompt(query, counter string, width int, th *Theme, base theme.Style) string {
	prompt := query
	room := width - len(counter) - 1
	if room < 0 {
		return th.Render(base, ansi.Truncate(prompt, width, ""))
	}

	prompt = ansi.Truncate(prompt, room, "")
	gap := width - lipgloss.Width(prompt) - len(counter)

	return th.Render(base, prompt+strings.Repeat(" ", gap)) + th.Render(base.Patch(th.TextInactive), counter)
}

// boxTop is a top border of width cells with title set into it.
func boxTop(title string, width int, th *Theme, border theme.Style) string {
	inner := max(width-2, 0)
	if title != "" && inner > 2 {
		title = ansi.Truncate(" "+title+" ", inner-1, "… ")

		return th.Render(border, "┌─"+title+strings.Repeat("─", max(inner-1-lipgloss.Width(title), 0))+"┐")
	}

	return th.Render(border, "┌"+strings.Repeat("─", inner)+"┐")
}

// boxRow frames a line with the side borders and one blank column on
// each side.
func boxRow(line string, th *Theme, border, pad theme.Style) string {
	return th.Render(border, "│") + th.Render(pad, " ") + line + th.Render(pad, " ") + th.Render(border, "│")
}

// boxBottom is a bottom border of width cells.
func boxBottom(width int, th *Theme, border theme.Style) string {
	return th.Render(border, "└"+strings.Repeat("─", max(width-2, 0))+"┘")
}

// truncateStart cuts s to width cells by dropping its start.
func truncateStart(s string, width int) string {
	w := lipgloss.Width(s)
	if w <= width || width < 2 {
		return s
	}

	return ansi.TruncateLeft(s, w-width+1, "…")
}

// renderPickerRow draws one entry with matched chars emphasized.
func renderPickerRow(row PickerRow, width int, th *Theme, base theme.Style) string {
	style := base
	if row.Dir {
		style = style.Patch(th.TextDirectory)
	}

	if row.Selected {
		style = style.Patch(th.TextFocus)
	}

	matchStyle := style.Patch(th.Special)

	marker := "  "
	if row.Selected {
		marker = "> "
	}

	var sb strings.Builder
	sb.WriteString(th.Render(style, marker))

	// Group runs of matched and unmatched bytes, walking the match
	// offsets in order alongside the text.
	pos := row.Positions
	if !slices.IsSorted(pos) {
		pos = slices.Sorted(slices.Values(pos))
	}

	text := row.Text
	if avail := width - 2 - lipgloss.Width(row.Note); row.TruncateStart && lipgloss.Width(text) > avail && avail > 1 {
		cut := truncateStart(text, avail)
		// Match offsets move with the dropped bytes, the ellipsis takes
		// their place.
		dropped := len(text) - (len(cut) - len("…"))
		shifted := make([]int, 0, len(pos))
		for _, p := range pos {
			if p >= dropped {
				shifted = append(shifted, p-dropped+len("…"))
			}
		}

		text, pos = cut, shifted
	}

	var run strings.Builder
	runMatched := false
	flush := func() {
		if run.Len() == 0 {
			return
		}

		if runMatched {
			sb.WriteString(th.Render(matchStyle, run.String()))
		} else {
			sb.WriteString(th.Render(style, run.String()))
		}

		run.Reset()
	}
	for i, r := range text {
		for len(pos) > 0 && pos[0] < i {
			pos = pos[1:]
		}

		m := len(pos) > 0 && pos[0] == i
		if m != runMatched {
			flush()
			runMatched = m
		}

		run.WriteRune(r)
	}

	flush()
	if row.Note != "" {
		sb.WriteString(th.Render(style.Patch(th.Dimmed()), " "+row.Note))
	}

	out := ansi.Truncate(sb.String(), width, "…")
	pad := width - lipgloss.Width(out)
	if pad > 0 {
		out += th.Render(style, strings.Repeat(" ", pad))
	}

	return out
}
