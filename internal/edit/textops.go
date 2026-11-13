package edit

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// HardWrap refills each paragraph to width, keeping the indent of the
// first line of the paragraph and a trailing newline when present. Lines
// are joined with nl, the line ending of the document.
func HardWrap(s string, width int, nl string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	trailing := strings.HasSuffix(s, "\n")
	paragraphs := strings.Split(strings.TrimRight(s, "\n"), "\n\n")
	var out []string
	for _, p := range paragraphs {
		lines := strings.Split(p, "\n")
		indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], " \t"))]
		words := strings.Fields(p)
		var wrapped []string
		line := indent
		for _, w := range words {
			switch {
			case line == indent:
				line += w
			case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) > width:
				wrapped = append(wrapped, line)
				line = indent + w
			default:
				line += " " + w
			}
		}

		if line != indent || len(words) == 0 {
			wrapped = append(wrapped, line)
		}

		out = append(out, strings.Join(wrapped, nl))
	}

	result := strings.Join(out, nl+nl)
	if trailing {
		result += nl
	}

	return result
}

// CompareNumeric orders fragments by their leading number. Fragments
// without one go after the numbers, in text order.
func CompareNumeric(a, b string) int {
	na, okA := parseLeadingNumber(a)
	nb, okB := parseLeadingNumber(b)
	switch {
	case okA && okB:
		return cmp.Or(cmp.Compare(na, nb), strings.Compare(a, b))
	case okA:
		return -1
	case okB:
		return 1
	}

	return strings.Compare(a, b)
}

func parseLeadingNumber(s string) (float64, bool) {
	m := leadingNumber.FindString(s)
	if m == "" {
		return 0, false
	}

	n, err := strconv.ParseFloat(strings.TrimSpace(m), 64)

	return n, err == nil
}

// TrailingWhitespace returns deletions of the spaces and tabs ending the
// lines first to last.
func TrailingWhitespace(text buffer.Rope, first, last int) []Change {
	var changes []Change
	for i := first; i <= last; i++ {
		line := text.LineWithoutEnding(i)
		trimmed := strings.TrimRight(line, " \t")
		if len(trimmed) == len(line) {
			continue
		}

		start := text.LineToChar(i) + utf8.RuneCountInString(trimmed)
		changes = append(changes, Change{From: start, To: text.LineToChar(i) + utf8.RuneCountInString(line)})
	}

	return changes
}

// leadingNumber matches the number a fragment starts with for a numeric
// sort, after leading blanks.
var leadingNumber = regexp.MustCompile(`^\s*[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?`)
