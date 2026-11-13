// Package cmdline splits and quotes the arguments of typed commands the
// way a shell does, and reads the %kind{content} expansions in them.
package cmdline

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Split splits a command line into arguments the way a shell does:
// spaces separate them, single quotes keep text as it is, double quotes
// and a backslash keep spaces. Expansions such as %{buffer_name} and
// %sh{...} stay whole. The arguments come back ready for expand, a
// percent sign that must stay literal is doubled.
func Split(s string) ([]string, error) {
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
			_, _, end, err := ParseExpansion(runes, i)
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

// Escape writes a completed word so Split reads it back as one
// argument.
func Escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || strings.ContainsRune(`\'"%`, r) {
			b.WriteRune('\\')
		}

		b.WriteRune(r)
	}

	return b.String()
}

// Unescape undoes Escape for a word being typed.
func Unescape(s string) string {
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

var expansionPairs = map[rune]rune{'{': '}', '[': ']', '(': ')', '<': '>', '\'': '\'', '"': '"', '|': '|'}

// ParseExpansion reads %kind{content} starting at the percent sign and
// returns the kind, the raw content and the index of the closing
// delimiter. Bracket delimiters nest.
func ParseExpansion(runes []rune, start int) (string, string, int, error) {
	i := start + 1
	for i < len(runes) && (runes[i] >= 'a' && runes[i] <= 'z') {
		i++
	}

	kind := string(runes[start+1 : i])
	if i >= len(runes) {
		return "", "", 0, fmt.Errorf("unterminated expansion at %q, %%%% is a literal %%", string(runes[start:]))
	}

	open := runes[i]
	closing, ok := expansionPairs[open]
	if !ok {
		return "", "", 0, fmt.Errorf("expansion needs a delimiter after %%%s, %%%% is a literal %%", kind)
	}

	depth := 0
	for j := i + 1; j < len(runes); j++ {
		switch {
		case runes[j] == open && open != closing:
			depth++
		case runes[j] == closing:
			if depth == 0 {
				return kind, string(runes[i+1 : j]), j, nil
			}

			depth--
		}
	}

	return "", "", 0, fmt.Errorf("unterminated expansion %%%s%c", kind, open)
}

// LastArgStart finds where the argument being typed starts: after the
// last space outside quotes that is not escaped. quote is the quote that
// opens it and is still open, zero for none.
func LastArgStart(runes []rune) (int, rune) {
	start, quote, escaped := 0, rune(0), false
	for i, r := range runes {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			start = i + 1
		}
	}

	if quote == 0 || start >= len(runes) || runes[start] != quote {
		quote = 0
	}

	return start, quote
}
