package keymap

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ParseMacro reads a key sequence in macro notation where plain
// characters are single keys and "<C-x>" groups name one key.
func ParseMacro(s string) ([]Key, error) {
	keys := []Key{}
	for len(s) > 0 {
		if s[0] == '>' {
			return nil, fmt.Errorf("unmatched '>' in %q", s)
		}

		if s[0] != '<' {
			r, size := utf8.DecodeRuneInString(s)
			name := string(r)
			if r == '-' {
				name = "minus"
			}

			key, err := Parse(name)
			if err != nil {
				return nil, err
			}

			keys = append(keys, key)
			s = s[size:]

			continue
		}

		end := strings.IndexByte(s, '>')
		if end < 0 {
			return nil, fmt.Errorf("'>' expected in %q", s)
		}

		key, err := Parse(s[1:end])
		if err != nil {
			return nil, err
		}

		keys = append(keys, key)
		s = s[end+1:]
	}

	return keys, nil
}
