package edit

import "github.com/YouSysAdmin/duck-editor/internal/buffer"

// FindNthChar returns the gap index of the nth char matching pred from pos
// in the given direction.
func FindNthChar(n int, text buffer.Rope, pred func(rune) bool, pos int, dir Direction) (int, bool) {
	if n == 0 || pos < 0 || pos > text.Len() {
		return 0, false
	}

	if dir == Forward {
		for pos < text.Len() {
			if pred(text.Char(pos)) {
				n--
				if n == 0 {
					return pos, true
				}
			}

			pos++
		}

		return 0, false
	}

	for pos > 0 {
		pos--
		if pred(text.Char(pos)) {
			n--
			if n == 0 {
				return pos, true
			}
		}
	}

	return 0, false
}
