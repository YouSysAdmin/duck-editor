package edit

import "github.com/YouSysAdmin/duck-editor/internal/buffer"

// Brackets are the pairs that can be matched in plain text.
var Brackets = [][2]rune{
	{'(', ')'},
	{'{', '}'},
	{'[', ']'},
	{'<', '>'},
	{'‘', '’'},
	{'“', '”'},
	{'«', '»'},
	{'「', '」'},
	{'（', '）'},
}

// Pairs are all brackets plus the symmetric quote pairs and closure bars,
// which need a syntax tree to be matched.
var Pairs = append(append([][2]rune{}, Brackets...), [2]rune{'"', '"'}, [2]rune{'\'', '\''}, [2]rune{'`', '`'}, [2]rune{'|', '|'})

const maxPlaintextScan = 10000

// GetPair returns the open and close chars for ch, or (ch, ch) when it is
// not a known pair.
func GetPair(ch rune) (rune, rune) {
	for _, p := range Pairs {
		if p[0] == ch || p[1] == ch {
			return p[0], p[1]
		}
	}

	return ch, ch
}

// IsOpenBracket reports an opening bracket char.
func IsOpenBracket(ch rune) bool {
	for _, p := range Brackets {
		if p[0] == ch {
			return true
		}
	}

	return false
}

// IsCloseBracket reports a closing bracket char.
func IsCloseBracket(ch rune) bool {
	for _, p := range Brackets {
		if p[1] == ch {
			return true
		}
	}

	return false
}

// IsValidBracket reports either side of a bracket pair.
func IsValidBracket(ch rune) bool {
	return IsOpenBracket(ch) || IsCloseBracket(ch)
}

// IsValidPair reports either side of any pair including quotes.
func IsValidPair(ch rune) bool {
	for _, p := range Pairs {
		if p[0] == ch || p[1] == ch {
			return true
		}
	}

	return false
}

// IsPair reports whether open and close form a known pair.
func IsPair(open, closer rune) bool {
	for _, p := range Pairs {
		if p[0] == open && p[1] == closer {
			return true
		}
	}

	return false
}

// FindMatchingBracketPlaintext returns the position of the bracket matching
// the one under the cursor, scanning at most maxPlaintextScan chars. It
// reports false when the cursor is not on a bracket or no match exists.
func FindMatchingBracketPlaintext(text buffer.Rope, pos int) (int, bool) {
	if pos < 0 || pos >= text.Len() {
		return 0, false
	}

	bracket := text.Char(pos)
	if !IsValidBracket(bracket) {
		return 0, false
	}

	open, closer := GetPair(bracket)
	matching := closer
	if bracket == closer {
		matching = open
	}

	forward := IsOpenBracket(bracket)
	depth := 1
	if forward {
		for i, p := 0, pos+1; i < maxPlaintextScan && p < text.Len(); i, p = i+1, p+1 {
			ch := text.Char(p)
			switch ch {
			case bracket:
				depth++
			case matching:
				depth--
				if depth == 0 {
					return p, true
				}
			}
		}

		return 0, false
	}

	for i, p := 0, pos-1; i < maxPlaintextScan && p >= 0; i, p = i+1, p-1 {
		ch := text.Char(p)
		switch ch {
		case bracket:
			depth++
		case matching:
			depth--
			if depth == 0 {
				return p, true
			}
		}
	}

	return 0, false
}
