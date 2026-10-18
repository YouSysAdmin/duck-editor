package ui

import (
	"cmp"
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// Theme holds the resolved styles the renderer needs plus a cache of
// their lipgloss equivalents. Styles are theme.Style so they can be
// layered per cell before conversion.
type Theme struct {
	Background       theme.Style
	Text             theme.Style
	Gutter           theme.Style
	GutterCurrent    theme.Style
	Selection        theme.Style
	SelectionPrimary theme.Style
	Cursor           theme.Style
	CursorPrimary    theme.Style
	Statusline       theme.Style
	StatuslineInact  theme.Style
	Window           theme.Style
	Popup            theme.Style
	TextFocus        theme.Style
	TextInactive     theme.Style
	Special          theme.Style
	Highlight        theme.Style
	Menu             theme.Style
	MenuSelected     theme.Style
	DiagError        theme.Style
	DiagWarning      theme.Style
	DiagInfo         theme.Style
	DiagHint         theme.Style
	ModeNormal       theme.Style
	ModeInsert       theme.Style
	ModeSelect       theme.Style
	Error            theme.Style
	Warning          theme.Style
	Info             theme.Style
	DiffAdd          theme.Style
	DiffDelete       theme.Style
	DiffChange       theme.Style
	InlayHint        theme.Style

	// SearchMatch marks matches of the last search, ui.highlight.search
	// or ui.highlight.
	SearchMatch theme.Style

	// Blame styles the inline blame at the end of the cursor line,
	// ui.virtual.blame or the inlay hint style.
	Blame            theme.Style
	InlayHintParam   theme.Style
	InlayHintType    theme.Style
	PopupInfo        theme.Style
	TextInfo         theme.Style
	JumpLabel        theme.Style
	Cursorline       theme.Style
	CursorlineOther  theme.Style
	Cursorcolumn     theme.Style
	Ruler            theme.Style
	Whitespace       theme.Style
	IndentGuide      theme.Style
	Bufferline       theme.Style
	BufferlineActive theme.Style
	BufferlineBg     theme.Style
	StatusSeparator  theme.Style
	Wrap             theme.Style

	// PickerBorder frames pickers, ui.picker.border or the inactive
	// text color.
	PickerBorder theme.Style

	// Picker is the body of a picker, ui.picker or the editor background.
	Picker theme.Style

	// Separator is the line under the picker prompt,
	// ui.background.separator or the picker border.
	Separator theme.Style

	// TextDirectory marks directories in pickers and previews,
	// ui.text.directory or the plain text.
	TextDirectory theme.Style

	// SeverityText colors diagnostic text and counts, ordered hint,
	// info, warning, error.
	SeverityText [4]theme.Style

	// Rainbow holds the rainbow bracket styles by nesting depth.
	Rainbow []theme.Style

	// undercurl allows curly underlines, otherwise they render plain.
	undercurl bool

	source *theme.Theme
	cache  map[theme.Style]lipgloss.Style
}

// SetUndercurl toggles curly underline support and drops cached styles.
func (th *Theme) SetUndercurl(on bool) {
	if th.undercurl == on {
		return
	}

	th.undercurl = on
	th.cache = map[theme.Style]lipgloss.Style{}
}

// FromTheme resolves the UI scopes of a theme.
func FromTheme(t *theme.Theme) Theme {
	th := Theme{
		Background:       t.Get("ui.background"),
		Text:             t.Get("ui.text"),
		Gutter:           t.Get("ui.linenr"),
		GutterCurrent:    t.Get("ui.linenr.selected"),
		Selection:        t.Get("ui.selection"),
		SelectionPrimary: t.Get("ui.selection.primary"),
		Cursor:           t.Get("ui.cursor"),
		CursorPrimary:    t.Get("ui.cursor.primary"),
		Statusline:       t.Get("ui.statusline"),
		StatuslineInact:  t.Get("ui.statusline.inactive"),
		Window:           t.Get("ui.window"),
		Popup:            t.Get("ui.popup"),
		TextFocus:        t.Get("ui.text.focus"),
		TextInactive:     t.Get("ui.text.inactive"),
		Special:          t.Get("special"),
		Highlight:        t.Get("ui.highlight"),
		Menu:             t.Get("ui.menu"),
		MenuSelected:     t.Get("ui.menu.selected"),
		DiagError:        t.Get("diagnostic.error"),
		DiagWarning:      t.Get("diagnostic.warning"),
		DiagInfo:         t.Get("diagnostic.info"),
		DiagHint:         t.Get("diagnostic.hint"),
		ModeNormal:       t.Get("ui.statusline.normal"),
		ModeInsert:       t.Get("ui.statusline.insert"),
		ModeSelect:       t.Get("ui.statusline.select"),
		Error:            t.Get("error"),
		Warning:          t.Get("warning"),
		Info:             t.Get("info"),
		DiffAdd:          t.Get("diff.plus"),
		DiffDelete:       t.Get("diff.minus"),
		DiffChange:       t.Get("diff.delta"),
		InlayHint:        t.Get("ui.virtual.inlay-hint"),
		Blame:            t.Get("ui.virtual.blame"),
		SearchMatch:      t.Get("ui.highlight.search"),
		InlayHintParam:   t.Get("ui.virtual.inlay-hint.parameter"),
		InlayHintType:    t.Get("ui.virtual.inlay-hint.type"),
		PopupInfo:        t.Get("ui.popup.info"),
		TextInfo:         t.Get("ui.text.info"),
		JumpLabel:        t.Get("ui.virtual.jump-label"),
		Cursorline:       t.Get("ui.cursorline.primary"),
		CursorlineOther:  t.Get("ui.cursorline.secondary"),
		Cursorcolumn:     t.Get("ui.cursorcolumn.primary"),
		Ruler:            t.Get("ui.virtual.ruler"),
		Whitespace:       t.Get("ui.virtual.whitespace"),
		IndentGuide:      t.Get("ui.virtual.indent-guide"),
		Bufferline:       t.Get("ui.bufferline"),
		BufferlineActive: t.Get("ui.bufferline.active"),
		BufferlineBg:     t.Get("ui.bufferline.background"),
		StatusSeparator:  t.Get("ui.statusline.separator"),
		Wrap:             t.Get("ui.virtual.wrap"),
		PickerBorder:     t.Get("ui.picker.border"),
		Picker:           t.Get("ui.picker"),
		Separator:        t.Get("ui.background.separator"),
		TextDirectory:    t.Get("ui.text.directory"),
		source:           t,
		cache:            map[theme.Style]lipgloss.Style{},
	}
	if th.Cursor.IsZero() {
		th.Cursor = theme.Style{Add: theme.ModReversed}
	}

	if th.CursorPrimary.IsZero() {
		th.CursorPrimary = th.Cursor
	}

	if th.Statusline.IsZero() {
		th.Statusline = theme.Style{Add: theme.ModReversed}
	}

	if th.Selection.IsZero() {
		th.Selection = theme.Style{Bg: theme.Indexed(8)}
	}

	if th.StatuslineInact.IsZero() {
		th.StatuslineInact = th.Statusline.Patch(theme.Style{Add: theme.ModDim})
	}

	if th.TextFocus.IsZero() {
		th.TextFocus = theme.Style{Add: theme.ModReversed}
	}

	if th.Popup.IsZero() {
		th.Popup = th.Background
	}

	if th.Menu.IsZero() {
		th.Menu = th.Popup
	}

	if th.MenuSelected.IsZero() {
		th.MenuSelected = theme.Style{Add: theme.ModReversed}
	}

	if th.DiagError.IsZero() {
		th.DiagError = theme.Style{Add: theme.ModUnderlined, UnderlineColor: th.Error.Fg}
	}

	if th.DiagWarning.IsZero() {
		th.DiagWarning = theme.Style{Add: theme.ModUnderlined, UnderlineColor: th.Warning.Fg}
	}

	if th.Cursorline.IsZero() {
		th.Cursorline = theme.Style{Bg: theme.Indexed(8)}
	}

	if th.CursorlineOther.IsZero() {
		th.CursorlineOther = th.Cursorline
	}

	if th.Cursorcolumn.IsZero() {
		th.Cursorcolumn = th.Cursorline
	}

	if th.Ruler.IsZero() {
		th.Ruler = theme.Style{Bg: theme.Indexed(8)}
	}

	if th.Whitespace.IsZero() {
		th.Whitespace = th.Gutter
	}

	if th.IndentGuide.IsZero() {
		th.IndentGuide = th.Gutter
	}

	if th.Bufferline.IsZero() {
		th.Bufferline = th.StatuslineInact
	}

	if th.BufferlineActive.IsZero() {
		th.BufferlineActive = th.Statusline
	}

	if th.BufferlineBg.IsZero() {
		th.BufferlineBg = th.Bufferline
	}

	if th.JumpLabel.IsZero() {
		th.JumpLabel = theme.Style{Fg: theme.Indexed(1), Add: theme.ModBold}
	}

	if th.PopupInfo.IsZero() {
		th.PopupInfo = th.Popup
	}

	if th.TextInfo.IsZero() {
		th.TextInfo = th.Text
	}

	if th.InlayHint.IsZero() {
		th.InlayHint = theme.Style{Add: theme.ModDim}
	}

	if th.InlayHintParam.IsZero() {
		th.InlayHintParam = th.InlayHint
	}

	if th.InlayHintType.IsZero() {
		th.InlayHintType = th.InlayHint
	}

	if th.DiffAdd.IsZero() {
		th.DiffAdd = theme.Style{Fg: theme.Indexed(2)}
	}

	if th.DiffDelete.IsZero() {
		th.DiffDelete = theme.Style{Fg: theme.Indexed(1)}
	}

	if th.DiffChange.IsZero() {
		th.DiffChange = theme.Style{Fg: theme.Indexed(3)}
	}

	th.SeverityText = [4]theme.Style{
		{Fg: t.Get("hint").Fg}, {Fg: th.Info.Fg}, {Fg: th.Warning.Fg}, {Fg: th.Error.Fg},
	}
	for depth := range t.RainbowCount() {
		th.Rainbow = append(th.Rainbow, t.Get("rainbow."+strconv.Itoa(depth)))
	}

	if th.PickerBorder.IsZero() {
		th.PickerBorder = theme.Style{Fg: cmp.Or[color.Color](th.TextInactive.Fg, th.Text.Fg)}
	}

	if th.Picker.IsZero() {
		th.Picker = th.Background
	}

	if th.Separator.IsZero() {
		th.Separator = th.PickerBorder
	}

	if th.TextDirectory.IsZero() {
		th.TextDirectory = th.Text
	}

	if th.Blame.IsZero() {
		th.Blame = th.InlayHint
	}

	if th.SearchMatch.IsZero() {
		th.SearchMatch = th.Highlight
	}

	if th.SearchMatch.IsZero() {
		th.SearchMatch = theme.Style{Add: theme.ModUnderlined}
	}

	return th
}

// DefaultTheme uses the built in theme.
func DefaultTheme() Theme {
	return FromTheme(theme.Default())
}

// Scope resolves a highlight scope to a style through the source theme.
func (th *Theme) Scope(scope string) theme.Style {
	if th.source == nil {
		return theme.Style{}
	}

	return th.source.Get(scope)
}

// BackgroundColor returns the terminal background to set, or nil.
func (th *Theme) BackgroundColor() color.Color {
	return toLipColor(th.Background.Bg)
}

// Dimmed is the foreground for secondary text in pickers, such as the
// target of a link, without a background so the row keeps its own.
func (th *Theme) Dimmed() theme.Style {
	switch {
	case th.TextInactive.Fg != nil:
		return theme.Style{Fg: th.TextInactive.Fg}
	case th.InlayHint.Fg != nil:
		return theme.Style{Fg: th.InlayHint.Fg}
	}

	return theme.Style{Add: theme.ModDim}
}

// Base is the style of an empty cell: the background with the text
// style layered on top.
func (th *Theme) Base() theme.Style {
	return th.Background.Patch(th.Text)
}

// Lip converts a style to lipgloss, caching the result.
func (th *Theme) Lip(s theme.Style) lipgloss.Style {
	if th.cache == nil {
		th.cache = map[theme.Style]lipgloss.Style{}
	}

	if ls, ok := th.cache[s]; ok {
		return ls
	}

	ls := lipgloss.NewStyle()
	if c := toLipColor(s.Fg); c != nil {
		ls = ls.Foreground(c)
	}

	if c := toLipColor(s.Bg); c != nil {
		ls = ls.Background(c)
	}

	if c := toLipColor(s.UnderlineColor); c != nil {
		ls = ls.UnderlineColor(c)
	}

	if s.Has(theme.ModBold) {
		ls = ls.Bold(true)
	}

	if s.Has(theme.ModDim) {
		ls = ls.Faint(true)
	}

	if s.Has(theme.ModItalic) {
		ls = ls.Italic(true)
	}

	if s.Has(theme.ModUnderlined) {
		ls = ls.Underline(true)
		switch s.UnderlineStyle {
		case "curl":
			if th.undercurl {
				ls = ls.UnderlineStyle(lipgloss.UnderlineCurly)
			}
		case "double":
			ls = ls.UnderlineStyle(lipgloss.UnderlineDouble)
		case "dotted":
			ls = ls.UnderlineStyle(lipgloss.UnderlineDotted)
		case "dashed":
			ls = ls.UnderlineStyle(lipgloss.UnderlineDashed)
		}
	}

	if s.Has(theme.ModSlowBlink) || s.Has(theme.ModRapidBlink) {
		ls = ls.Blink(true)
	}

	if s.Has(theme.ModReversed) {
		ls = ls.Reverse(true)
	}

	if s.Has(theme.ModCrossedOut) {
		ls = ls.Strikethrough(true)
	}

	th.cache[s] = ls

	return ls
}

// Render draws text in a composed style.
func (th *Theme) Render(s theme.Style, text string) string {
	return th.Lip(s).Render(text)
}

func toLipColor(c color.Color) color.Color {
	switch v := c.(type) {
	case nil:
		return nil
	case theme.Indexed:
		return lipgloss.ANSIColor(v)
	default:
		if theme.IsReset(c) {
			return nil
		}

		return c
	}
}
