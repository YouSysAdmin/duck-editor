package edit

import "unicode"

// CharCategory classifies a char for word motions.
type CharCategory int

// Categories.
const (
	CatWhitespace CharCategory = iota
	CatEOL
	CatWord
	CatPunctuation
	CatUnknown
)

// CategorizeChar classifies ch.
func CategorizeChar(ch rune) CharCategory {
	switch {
	case IsLineEnding(ch):
		return CatEOL
	case unicode.IsSpace(ch):
		return CatWhitespace
	case IsWordChar(ch):
		return CatWord
	case IsPunctuation(ch):
		return CatPunctuation
	default:
		return CatUnknown
	}
}

// IsLineEnding reports whether ch terminates a line. Only LF counts, a
// CR is treated as part of CRLF by the line helpers.
func IsLineEnding(ch rune) bool {
	return ch == '\n'
}

// IsWhitespaceChar reports non line break whitespace.
func IsWhitespaceChar(ch rune) bool {
	switch ch {
	case '\t', ' ', 0x00A0, 0x180E, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}

	return ch >= 0x2000 && ch <= 0x200B
}

// IsPunctuation reports punctuation and symbol chars.
func IsPunctuation(ch rune) bool {
	return unicode.In(ch,
		unicode.Po, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf, unicode.Pc, unicode.Pd,
		unicode.Sm, unicode.Sc, unicode.Sk,
	)
}

// IsWordChar reports letters, digits and underscore.
func IsWordChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsNumber(ch) || ch == '_'
}

// FirstNonWhitespace returns the char offset of the first non whitespace
// char in s.
func FirstNonWhitespace(s string) (int, bool) {
	i := 0
	for _, ch := range s {
		if !unicode.IsSpace(ch) {
			return i, true
		}

		i++
	}

	return 0, false
}

// LastNonWhitespace returns the char offset of the last non whitespace
// char in s.
func LastNonWhitespace(s string) (int, bool) {
	runes := []rune(s)
	for i := len(runes) - 1; i >= 0; i-- {
		if !unicode.IsSpace(runes[i]) {
			return i, true
		}
	}

	return 0, false
}
