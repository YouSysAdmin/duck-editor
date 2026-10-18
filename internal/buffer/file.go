package buffer

import (
	"bytes"
	"unicode/utf8"
)

// sniffSize is how much of a file decides whether it is text.
const sniffSize = 8 << 10

// LooksBinary reports whether data, the start of a file, is not text: it
// holds a NUL byte or is not valid UTF-8. A rune cut at the end of data
// does not count.
func LooksBinary(data []byte) bool {
	data = data[:min(len(data), sniffSize)]
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}

	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			return len(data) >= utf8.UTFMax || utf8.FullRune(data)
		}

		data = data[size:]
	}

	return false
}
