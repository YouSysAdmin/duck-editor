package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// WrapOptions describe soft wrapping of long lines.
type WrapOptions struct {
	// Width is the number of cells a row may use.
	Width int

	// MaxWrap is how far back a row may be cut at whitespace.
	MaxWrap int

	// MaxIndentRetain caps the indent copied to continuation rows.
	MaxIndentRetain int

	// Indicator starts every continuation row.
	Indicator string
	Style     theme.Style
}

// WrapGlyphs splits a laid out line into rows that fit the width.
// Continuation rows start with the retained indent and the indicator as
// virtual glyphs. Without options or when the line fits, one row comes
// back. Columns are relative to each row.
func WrapGlyphs(glyphs []Glyph, opts *WrapOptions) [][]Glyph {
	if opts == nil || opts.Width <= 0 || LineWidth(glyphs) <= opts.Width {
		return [][]Glyph{glyphs}
	}

	indent := 0
	for _, g := range glyphs {
		if !g.Space || g.Chars == 0 {
			break
		}

		indent += g.Width
	}

	indent = min(indent, opts.MaxIndentRetain)
	indicatorWidth := lipgloss.Width(opts.Indicator)
	prefixWidth := indent + indicatorWidth
	if prefixWidth >= opts.Width {
		indent = 0
		prefixWidth = indicatorWidth
	}

	style := opts.Style
	prefix := func() []Glyph {
		var out []Glyph
		if indent > 0 {
			out = append(out, Glyph{Text: strings.Repeat(" ", indent), Width: indent, Style: &style})
		}

		if indicatorWidth > 0 {
			out = append(out, Glyph{Text: opts.Indicator, Col: indent, Width: indicatorWidth, Style: &style})
		}

		return out
	}

	var rows [][]Glyph
	var row []Glyph
	col := 0
	limit := opts.Width
	flush := func() {
		rows = append(rows, row)
		row = prefix()
		col = prefixWidth
		limit = opts.Width
	}
	for i, g := range glyphs {
		if col+g.Width > limit && g.Chars > 0 && g.Space && len(row) > 0 {
			// Whitespace at the wrap point stays on the row it ends.
			g.Col = col
			row = append(row, g)
			col += g.Width
			if i+1 < len(glyphs) {
				flush()
			}

			continue
		}

		if col+g.Width > limit && len(row) > 0 && col > 0 {
			// Cut at the last whitespace within max-wrap cells when there
			// is one, otherwise right here.
			cut := -1
			for j := len(row) - 1; j >= 0 && col-row[j].Col <= opts.MaxWrap; j-- {
				if row[j].Chars > 0 && row[j].Space {
					cut = j + 1

					break
				}
			}

			if cut > 0 && cut < len(row) && row[cut-1].Chars > 0 {
				carried := append([]Glyph(nil), row[cut:]...)
				row = row[:cut]
				flush()
				for _, c := range carried {
					c.Col = col
					row = append(row, c)
					col += c.Width
				}
			} else {
				flush()
			}
		}

		g.Col = col
		row = append(row, g)
		col += g.Width
	}

	rows = append(rows, row)

	return rows
}

// WrapPosition returns the row and column of a char inside wrapped rows.
func WrapPosition(rows [][]Glyph, char int) (int, int) {
	for r, row := range rows {
		for _, g := range row {
			if g.Chars == 0 {
				continue
			}

			if char >= g.Char && char < g.Char+g.Chars {
				return r, g.Col
			}
		}
	}

	if len(rows) == 0 {
		return 0, 0
	}

	last := rows[len(rows)-1]
	if len(last) == 0 {
		return len(rows) - 1, 0
	}

	end := last[len(last)-1]

	return len(rows) - 1, end.Col + end.Width
}

// WrapCharAt returns the char at a visual column of a wrapped row, the
// first char for a column before the text and the last char for one
// past its end.
func WrapCharAt(rows [][]Glyph, row, col int) int {
	if len(rows) == 0 {
		return 0
	}

	row = min(max(row, 0), len(rows)-1)
	last := 0
	for _, g := range rows[row] {
		if g.Chars == 0 {
			continue
		}

		// A column before the text, on the wrap indicator or the kept
		// indent, lands on the first char of the row.
		if col < g.Col+g.Width {
			return g.Char
		}

		last = g.Char
	}

	return last
}

// VirtualLine is a row drawn under a document line, such as an inline
// diagnostic.
type VirtualLine struct {
	Text  string
	Style theme.Style
}
