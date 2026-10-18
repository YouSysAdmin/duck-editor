// Package ui renders editor state into styled terminal text.
package ui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// Glyph is one grapheme cluster placed on the visual line.
type Glyph struct {
	// Text is what gets drawn. Tabs become spaces and control characters
	// become caret notation.
	Text string

	// Col is the visual column where the glyph starts.
	Col int

	// Width is the number of terminal cells the glyph occupies.
	Width int

	// Char is the char offset of the cluster within the line.
	Char int

	// Chars is the number of chars in the cluster. Virtual text such as
	// inlay hints has zero chars.
	Chars int

	// Tab marks a tab expansion whose width depends on its column.
	Tab bool

	// Space marks source whitespace, also when it is drawn as a visible
	// symbol or an indent guide.
	Space bool

	// Style overrides the document styling for virtual text.
	Style *theme.Style

	// Tint is layered under the document styling, used for rendered
	// whitespace and indent guides.
	Tint *theme.Style
}

// WhitespaceGlyphs says how whitespace is drawn when rendering is on.
type WhitespaceGlyphs struct {
	Space, Nbsp, Nnbsp, Tab, Tabpad, Newline                       string
	RenderSpace, RenderNbsp, RenderNnbsp, RenderTab, RenderNewline bool
	Style                                                          theme.Style
}

// LayoutOptions tune Layout.
type LayoutOptions struct {
	TabWidth   int
	Whitespace *WhitespaceGlyphs

	// MaxCol and MaxChar stop the layout at the first glyph reaching
	// this column or char, zero lays out the whole line. ExtraCols lays
	// out this many more columns past MaxChar, so the rows around a char
	// are complete whatever the chars per cell.
	MaxCol    int
	MaxChar   int
	ExtraCols int
}

// LayoutWith splits a line into glyphs with their visual positions. It
// is the single source of truth for mapping grapheme indexes to columns.
func LayoutWith(line string, opts LayoutOptions) []Glyph {
	glyphs, _ := layoutLine(line, opts)

	return glyphs
}

// layoutLine is LayoutWith that also reports whether the whole line was
// laid out or MaxCol cut it short.
func layoutLine(line string, opts LayoutOptions) ([]Glyph, bool) {
	size := len(line)
	if opts.MaxCol > 0 {
		size = min(size, opts.MaxCol+1)
	}

	if opts.MaxChar > 0 {
		size = min(size, opts.MaxChar+1)
	}

	glyphs := make([]Glyph, 0, size)
	col := 0
	char := 0
	state := -1
	tabWidth := max(opts.TabWidth, 1)
	ws := opts.Whitespace

	reachedAt := -1
	for len(line) > 0 {
		if opts.MaxCol > 0 && col >= opts.MaxCol {
			return glyphs, false
		}

		if opts.MaxChar > 0 && char >= opts.MaxChar {
			if reachedAt < 0 {
				reachedAt = col
			}

			if col-reachedAt >= opts.ExtraCols {
				return glyphs, false
			}
		}

		var cluster string
		var width int
		chars := 1
		if c := line[0]; c >= 0x20 && c < 0x7f && (len(line) == 1 || line[1] < utf8.RuneSelf) {
			// Printable ASCII followed by ASCII is a cluster of its own,
			// most text takes this path past the segmenter.
			cluster, line, width, state = line[:1], line[1:], 1, -1
		} else {
			cluster, line, width, state = uniseg.FirstGraphemeClusterInString(line, state)
			chars = utf8.RuneCountInString(cluster)
		}

		text := cluster
		isTab := cluster == "\t"
		var tint *theme.Style
		switch {
		case isTab:
			width = tabWidth - col%tabWidth
			text = strings.Repeat(" ", width)
			if ws != nil && ws.RenderTab {
				text = ws.Tab + strings.Repeat(ws.Tabpad, max(width-lipgloss.Width(ws.Tab), 0))
				tint = &ws.Style
			}
		case cluster == " " && ws != nil && ws.RenderSpace:
			text = ws.Space
			tint = &ws.Style
		case cluster == "\u00a0" && ws != nil && ws.RenderNbsp:
			text = ws.Nbsp
			tint = &ws.Style
		case cluster == "\u202f" && ws != nil && ws.RenderNnbsp:
			text = ws.Nnbsp
			tint = &ws.Style
		case isControl(cluster):
			text = caretNotation(cluster)
			width = len(text)
		case isInvalid(cluster):
			// A stray byte drawn raw becomes whatever the terminal makes
			// of it, its hex value has a known width.
			text = fmt.Sprintf("<%02X>", cluster[0])
			width = len(text)
		case width == 0:
			// A zero width cluster on its own draws nothing or joins the
			// cell before it, its code point keeps columns right.
			r, _ := utf8.DecodeRuneInString(cluster)
			text = fmt.Sprintf("<%04X>", r)
			width = len(text)
		}

		space := strings.TrimSpace(cluster) == ""
		glyphs = append(glyphs, Glyph{Text: text, Col: col, Width: width, Char: char, Chars: chars, Tab: isTab, Space: space, Tint: tint})
		col += width
		char += chars
	}

	return glyphs, true
}

// IndentGuideOptions draw a guide at every indent level of the leading
// whitespace.
type IndentGuideOptions struct {
	Char  string
	Skip  int
	Width int
	Style theme.Style
}

// applyIndentGuides replaces whitespace cells at indent columns.
func applyIndentGuides(glyphs []Glyph, opts *IndentGuideOptions) {
	if opts == nil || opts.Width <= 0 || opts.Char == "" {
		return
	}

	guide, _ := utf8.DecodeRuneInString(opts.Char)
	for i := range glyphs {
		g := &glyphs[i]
		if g.Chars == 0 {
			continue
		}

		// Only leading whitespace gets guides.
		if !g.Space {
			return
		}

		var runes []rune
		for col := g.Col; col < g.Col+g.Width; col++ {
			if col%opts.Width != 0 || col/opts.Width < opts.Skip {
				continue
			}

			if runes == nil {
				runes = []rune(g.Text)
			}

			if offset := col - g.Col; offset < len(runes) {
				runes[offset] = guide
				style := opts.Style
				if g.Tint != nil {
					style = g.Tint.Patch(opts.Style)
				}

				g.Tint = &style
			}
		}

		if runes != nil {
			g.Text = string(runes)
		}
	}
}

// Inlay is virtual text drawn before the char at Char of a line.
type Inlay struct {
	Char  int
	Text  string
	Style theme.Style
}

// InsertInlays places virtual glyphs into a laid out line and reflows the
// columns. Inlays must be sorted by Char.
func InsertInlays(glyphs []Glyph, inlays []Inlay, tabWidth int) []Glyph {
	if len(inlays) == 0 {
		return glyphs
	}

	out := make([]Glyph, 0, len(glyphs)+len(inlays))
	col := 0
	place := func(g Glyph) {
		g.Col = col
		if g.Tab {
			g.Width = tabWidth - col%tabWidth
			g.Text = strings.Repeat(" ", g.Width)
		}

		out = append(out, g)
		col += g.Width
	}
	next := 0
	for _, g := range glyphs {
		for next < len(inlays) && inlays[next].Char <= g.Char {
			style := inlays[next].Style
			place(Glyph{Text: inlays[next].Text, Width: lipgloss.Width(inlays[next].Text), Char: g.Char, Style: &style})
			next++
		}

		place(g)
	}

	for ; next < len(inlays); next++ {
		style := inlays[next].Style
		char := 0
		if len(out) > 0 {
			char = out[len(out)-1].Char + out[len(out)-1].Chars
		}

		place(Glyph{Text: inlays[next].Text, Width: lipgloss.Width(inlays[next].Text), Char: char, Style: &style})
	}

	return out
}

// InlayWidth returns the cells of virtual text drawn before char pos.
func InlayWidth(inlays []Inlay, pos int) int {
	width := 0
	for _, in := range inlays {
		if in.Char <= pos {
			width += lipgloss.Width(in.Text)
		}
	}

	return width
}

// LineWidth returns the visual width of a laid out line.
func LineWidth(glyphs []Glyph) int {
	if len(glyphs) == 0 {
		return 0
	}

	last := glyphs[len(glyphs)-1]

	return last.Col + last.Width
}

// isControl reports a C0, DEL or C1 control character. Drawn as is a
// terminal could act on it, C1 codes such as U+009B start escape
// sequences in some terminals.
func isControl(cluster string) bool {
	r, size := utf8.DecodeRuneInString(cluster)

	return size == len(cluster) && unicode.IsControl(r)
}

// caretNotation draws a control character visibly: ^X for C0, ^? for
// DEL and <XX> with the code in hex for C1.
func caretNotation(cluster string) string {
	r, _ := utf8.DecodeRuneInString(cluster)
	switch {
	case r == 0x7f:
		return "^?"
	case r < 0x20:
		return "^" + string(r+'@')
	default:
		return fmt.Sprintf("<%02X>", r)
	}
}

// TextWidth returns the number of terminal cells s takes.
func TextWidth(s string) int {
	return lipgloss.Width(s)
}

// isInvalid reports a byte that is not valid UTF-8.
func isInvalid(cluster string) bool {
	r, size := utf8.DecodeRuneInString(cluster)

	return r == utf8.RuneError && size == 1
}
