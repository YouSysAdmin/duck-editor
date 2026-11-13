package edit

import (
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// dateTimeLayouts are the date and time formats C-a and C-x change, the
// smallest unit last in each.
var dateTimeLayouts = []string{
	"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02",
	"2006/01/02", "15:04:05", "15:04",
}

// dateTimeRe finds candidates for the layouts in a line.
var dateTimeRe = regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}(?:[ T]\d{2}:\d{2}(?::\d{2})?)?|\d{2}:\d{2}(?::\d{2})?`)

// DateTimeAt finds the date or time the selection is or the cursor sits
// in, as char offsets.
func DateTimeAt(text buffer.Rope, r Range) (int, int, bool) {
	if frag := r.Fragment(text); dateTimeRe.FindString(frag) == frag && frag != "" {
		return r.From(), r.To(), true
	}

	if !r.IsSingleGrapheme(text) {
		return 0, 0, false
	}

	line := r.CursorLine(text)
	lineStart := text.LineToChar(line)
	content := text.Line(line)
	cursor := r.Cursor(text) - lineStart
	for _, m := range dateTimeRe.FindAllStringIndex(content, -1) {
		// The layouts are ASCII, byte offsets in the match are chars.
		start := utf8.RuneCountInString(content[:m[0]])
		end := start + (m[1] - m[0])
		if start <= cursor && cursor < end {
			return lineStart + start, lineStart + end, true
		}
	}

	return 0, 0, false
}

// IncrementDateTime adds amount of the smallest unit of its layout to a
// date or time: days, minutes or seconds.
func IncrementDateTime(s string, amount int64) (string, bool) {
	for _, layout := range dateTimeLayouts {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}

		unit := 24 * time.Hour
		switch {
		case strings.HasSuffix(layout, ":05"):
			unit = time.Second
		case strings.HasSuffix(layout, ":04"):
			unit = time.Minute
		}

		return t.Add(time.Duration(amount) * unit).Format(layout), true
	}

	return "", false
}

// IncrementInteger adds amount to a decimal, 0x, 0o or 0b literal,
// keeping its width, case and digit separators.
func IncrementInteger(s string, amount int64) (string, bool) {
	const sep = '_'
	if s == "" || strings.HasPrefix(s, string(sep)) || strings.HasSuffix(s, string(sep)) {
		return "", false
	}

	radix := 10
	switch {
	case strings.HasPrefix(s, "0x"):
		radix = 16
	case strings.HasPrefix(s, "0o"):
		radix = 8
	case strings.HasPrefix(s, "0b"):
		radix = 2
	}

	// Literals are ASCII, so byte offsets are char offsets.
	var sepFromEnd []int
	for i := range len(s) {
		if s[len(s)-1-i] == sep {
			sepFromEnd = append(sepFromEnd, i)
		}
	}

	word := strings.ReplaceAll(s, string(sep), "")

	var out string
	if radix == 10 {
		value, ok := new(big.Int).SetString(word, 10)
		if !ok {
			return "", false
		}

		next := new(big.Int).Add(value, big.NewInt(amount))
		width := len(word) - len(sepFromEnd)
		switch {
		case value.Sign() < 0 && next.Sign() >= 0:
			width--
		case value.Sign() >= 0 && next.Sign() < 0:
			width++
		}

		if strings.HasPrefix(word, "0") || strings.HasPrefix(word, "-0") {
			out = padNumber(next.String(), width)
		} else {
			out = next.String()
		}
	} else {
		digits := word[2:]
		value, ok := new(big.Int).SetString(digits, radix)
		if !ok || digits == "" {
			return "", false
		}

		next := new(big.Int).Add(value, big.NewInt(amount))
		if next.Sign() < 0 {
			next.SetInt64(0)
		}

		width := len(s) - 2 - len(sepFromEnd)
		body := next.Text(radix)
		if radix == 16 {
			upper, lower := 0, 0
			for _, c := range digits {
				switch {
				case unicode.IsUpper(c):
					upper++
				case unicode.IsLower(c):
					lower++
				}
			}

			if upper > lower {
				body = strings.ToUpper(body)
			}
		}

		out = s[:2] + padNumber(body, width)
	}

	// Digits start after the sign or the radix prefix, a separator never
	// goes before the first digit.
	prefix := 0
	if radix != 10 {
		prefix = 2
	}

	if strings.HasPrefix(out, "-") {
		prefix++
	}

	// Put the separators back counting from the right, nearest first so
	// each offset already includes the separators after it.
	for _, fromEnd := range sepFromEnd {
		if pos := len(out) - fromEnd; pos > prefix {
			out = out[:pos] + string(sep) + out[pos:]
		}
	}

	// A number that grew gets more separators with the same spacing.
	if len(out) > len(s) && len(sepFromEnd) > 0 {
		spacing := sepFromEnd[0]
		if n := len(sepFromEnd); n > 1 {
			spacing = sepFromEnd[n-1] - sepFromEnd[n-2] - 1
		}

		if index := strings.IndexRune(out, sep); index >= 0 && spacing > 0 {
			for index-prefix > spacing {
				index -= spacing
				out = out[:index] + string(sep) + out[index:]
			}
		}
	}

	return out, true
}

// padNumber zero pads a possibly negative number to width digits.
func padNumber(s string, width int) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if neg {
		width--
	}

	if len(s) < width {
		s = strings.Repeat("0", width-len(s)) + s
	}

	if neg {
		return "-" + s
	}

	return s
}
