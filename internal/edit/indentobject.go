package edit

import (
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// TextObjectIndent selects the block of lines around the cursor indented
// at least as deep as the cursor line, blank lines inside included.
// Around adds the less indented line above, the key of a YAML block or
// the header of a Python one.
func TextObjectIndent(text buffer.Rope, r Range, kind TextObjectKind, tabWidth int) Range {
	lines := text.LenLines()
	if text.Len() > 0 && text.Char(text.Len()-1) == '\n' {
		lines--
	}

	if lines <= 0 {
		return r
	}

	indent := func(l int) (int, bool) {
		s := text.LineWithoutEnding(l)
		if strings.TrimSpace(s) == "" {
			return 0, false
		}

		width := 0
		for _, c := range s {
			switch c {
			case ' ':
				width++
			case '\t':
				width += tabWidth - width%tabWidth
			default:
				return width, true
			}
		}

		return width, true
	}

	// A blank cursor line takes the indent of the next line with text.
	cur := min(r.CursorLine(text), lines-1)
	base, ok := indent(cur)
	for l := cur + 1; !ok && l < lines; l++ {
		base, ok = indent(l)
	}

	if !ok {
		return r
	}

	inBlock := func(l int) bool {
		w, text := indent(l)

		return !text || w >= base
	}
	start, end := cur, cur
	for start > 0 && inBlock(start-1) {
		start--
	}

	for end+1 < lines && inBlock(end+1) {
		end++
	}

	// Blank lines at the edges belong to the surroundings.
	for start < end {
		if _, text := indent(start); text {
			break
		}

		start++
	}

	for end > start {
		if _, text := indent(end); text {
			break
		}

		end--
	}

	if kind == Around && start > 0 {
		if w, text := indent(start - 1); text && w < base {
			start--
		}
	}

	from := text.LineToChar(start)
	to := text.Len()
	if end+1 < text.LenLines() {
		to = text.LineToChar(end + 1)
	}

	if r.Direction() == Backward {
		return NewRange(to, from)
	}

	return NewRange(from, to)
}
