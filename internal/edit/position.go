package edit

import (
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// Position is a zero based row and column in a text.
type Position struct {
	Row int
	Col int
}

// GraphemeWidth returns the terminal cells a cluster occupies, never less
// than one so every cluster stays editable.
func GraphemeWidth(g string) int {
	if len(g) > 0 && g[0] < 0x80 {
		return 1
	}

	return max(uniseg.StringWidth(g), 1)
}

// CoordsAtPos converts a char index to a line and grapheme column.
func CoordsAtPos(text buffer.Rope, pos int) Position {
	line := text.CharToLine(pos)
	lineStart := text.LineToChar(line)
	pos = text.EnsureGraphemeBoundaryPrev(pos)

	col := 0
	for range buffer.Graphemes(text.Slice(lineStart, pos)) {
		col++
	}

	return Position{Row: line, Col: col}
}

// VisualCoordsAtPos converts a char index to a line and visual column,
// expanding tabs and wide chars.
func VisualCoordsAtPos(text buffer.Rope, pos, tabWidth int) Position {
	line := text.CharToLine(pos)
	lineStart := text.LineToChar(line)
	pos = text.EnsureGraphemeBoundaryPrev(pos)

	col := 0
	for g := range buffer.Graphemes(text.Slice(lineStart, pos)) {
		if g == "\t" {
			col += tabWidth - col%tabWidth
		} else {
			col += GraphemeWidth(g)
		}
	}

	return Position{Row: line, Col: col}
}

// PosAtVisualCoords converts a line and visual column to a char index. A
// column inside a wide grapheme maps to the start of that grapheme.
func PosAtVisualCoords(text buffer.Rope, coords Position, tabWidth int) int {
	row := min(coords.Row, text.LenLines()-1)
	lineStart := text.LineToChar(row)
	lineEnd := text.LineEndCharIndex(row)

	offset := 0
	remaining := coords.Col
	for g := range buffer.Graphemes(text.Slice(lineStart, lineEnd)) {
		width := GraphemeWidth(g)
		if g == "\t" {
			width = tabWidth - (coords.Col-remaining)%tabWidth
		}

		if width > remaining {
			break
		}

		remaining -= width
		offset += utf8.RuneCountInString(g)
	}

	return lineStart + offset
}
