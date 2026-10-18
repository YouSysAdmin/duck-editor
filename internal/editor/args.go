package editor

import (
	"errors"
	"strings"
	"unicode"
)

// splitArgs splits a command line into arguments the way a shell does:
// spaces separate them, single quotes keep text as it is, double quotes
// and a backslash keep spaces. Expansions such as %{buffer_name} and
// %sh{...} stay whole. The arguments come back ready for expand, a
// percent sign that must stay literal is doubled.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false
	literal := func(r rune) {
		if r == '%' {
			cur.WriteString("%%")
		} else {
			cur.WriteRune(r)
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}

			continue
		case r == '\\' && i+1 < len(runes):
			i++
			literal(runes[i])
		case r == '\'':
			end := i + 1
			for end < len(runes) && runes[end] != '\'' {
				literal(runes[end])
				end++
			}

			if end >= len(runes) {
				return nil, errors.New("unterminated single quote")
			}

			i = end
		case r == '"':
			end := i + 1
			for end < len(runes) && runes[end] != '"' {
				if runes[end] == '\\' && end+1 < len(runes) && (runes[end+1] == '"' || runes[end+1] == '\\') {
					end++
				}

				cur.WriteRune(runes[end])
				end++
			}

			if end >= len(runes) {
				return nil, errors.New("unterminated double quote")
			}

			i = end
		case r == '%' && i+1 < len(runes) && runes[i+1] == '%':
			cur.WriteString("%%")
			i++
		case r == '%':
			_, _, end, err := parseExpansion(runes, i)
			if err != nil {
				return nil, err
			}

			cur.WriteString(string(runes[i : end+1]))
			i = end
		default:
			cur.WriteRune(r)
		}

		inArg = true
	}

	if inArg {
		args = append(args, cur.String())
	}

	return args, nil
}

// escapeArg writes a completed word so splitArgs reads it back as one
// argument.
func escapeArg(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || strings.ContainsRune(`\'"%`, r) {
			b.WriteRune('\\')
		}

		b.WriteRune(r)
	}

	return b.String()
}

// unescapeArg undoes escapeArg for a word being typed.
func unescapeArg(s string) string {
	var b strings.Builder
	escaped := false
	for _, r := range s {
		if r == '\\' && !escaped {
			escaped = true

			continue
		}

		b.WriteRune(r)
		escaped = false
	}

	return b.String()
}
