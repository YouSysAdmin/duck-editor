package buffer

import "strings"

// LineEnding is the newline sequence used by a document.
type LineEnding string

// Supported line endings.
const (
	LF   LineEnding = "\n"
	CRLF LineEnding = "\r\n"
)

// Name returns the short label shown in the statusline.
func (le LineEnding) Name() string {
	if le == CRLF {
		return "CRLF"
	}

	return "LF"
}

// Len returns the number of chars in the line ending.
func (le LineEnding) Len() int {
	return len(le)
}

// DetectLineEnding returns the ending of the first line break in text,
// defaulting to LF.
func DetectLineEnding(text string) LineEnding {
	idx := strings.IndexByte(text, '\n')
	if idx > 0 && text[idx-1] == '\r' {
		return CRLF
	}

	return LF
}

// LineEndingOf returns the ending that terminates line, empty when the
// line has none.
func LineEndingOf(line string) LineEnding {
	switch {
	case strings.HasSuffix(line, string(CRLF)):
		return CRLF
	case strings.HasSuffix(line, string(LF)):
		return LF
	default:
		return ""
	}
}

// LineEndCharIndex returns the char index of the end of line l, not
// including its line ending.
func (r Rope) LineEndCharIndex(l int) int {
	end := r.LineToChar(l + 1)

	return end - len(LineEndingOf(r.Line(l)))
}

// LineWithoutEnding returns the text of line l without its ending.
func (r Rope) LineWithoutEnding(l int) string {
	line := r.Line(l)

	return strings.TrimSuffix(line, string(LineEndingOf(line)))
}

// IsLineBlank reports whether line l holds only its line ending.
func (r Rope) IsLineBlank(l int) bool {
	return r.LineWithoutEnding(l) == ""
}
