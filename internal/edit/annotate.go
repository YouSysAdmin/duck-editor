package edit

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// ParseAnnotated turns a test string with inline selection markers into
// plain text plus a selection. The notation:
//
//	#[|text]#  primary range, head before anchor
//	#(|text)#  secondary range, head before anchor
//	#[text|]#  primary range, head after anchor
//	#(text|)#  secondary range, head after anchor
//
// A line ending directly inside a closing marker that is repeated right
// after it is dropped, so a cursor can sit on a line ending.
func ParseAnnotated(s string) (string, Selection, error) {
	graphemes := splitGraphemes(s)
	var left strings.Builder
	leftChars := 0
	ranges := []Range{}
	primary := -1

	write := func(g string) {
		left.WriteString(g)
		leftChars += utf8.RuneCountInString(g)
	}

	for i := 0; i < len(graphemes); i++ {
		g := graphemes[i]
		start := leftChars
		if g != "#" {
			write(g)

			continue
		}

		if i+1 >= len(graphemes) {
			break
		}

		var isPrimary bool
		var closePair string
		switch graphemes[i+1] {
		case "[":
			isPrimary, closePair = true, "]"
		case "(":
			isPrimary, closePair = false, ")"
		default:
			write("#")
			write(graphemes[i+1])
			i++

			continue
		}

		i++
		if isPrimary && primary >= 0 {
			return "", Selection{}, fmt.Errorf("primary #[ already appeared in %q", s)
		}

		headAtBeg := i+1 < len(graphemes) && graphemes[i+1] == "|"
		if headAtBeg {
			i++
		}

		closed := false
		for i+1 < len(graphemes) {
			i++
			c := graphemes[i]
			isClose := c == closePair && i+1 < len(graphemes) && graphemes[i+1] == "#"
			if !isClose {
				write(c)

				continue
			}

			if !headAtBeg {
				prev := lastGrapheme(left.String())
				if prev != "|" {
					write(c)

					continue
				}

				text := strings.TrimSuffix(left.String(), "|")
				left.Reset()
				left.WriteString(text)
				leftChars--
			}

			i++ // skip "#"
			prev := lastGrapheme(left.String())
			if (prev == "\r\n" || prev == "\n") && i+1 < len(graphemes) && graphemes[i+1] == prev {
				i++
			}

			if isPrimary {
				primary = len(ranges)
			}

			if headAtBeg {
				ranges = append(ranges, NewRange(leftChars, start))
			} else {
				ranges = append(ranges, NewRange(start, leftChars))
			}

			closed = true

			break
		}

		if !closed {
			return "", Selection{}, fmt.Errorf("missing end %s# in %q", closePair, s)
		}
	}

	if primary < 0 {
		return "", Selection{}, errors.New("missing primary #[|]#")
	}

	return left.String(), NewSelection(ranges, primary), nil
}

// MustParseAnnotated is ParseAnnotated for tests and panics on error.
func MustParseAnnotated(s string) (string, Selection) {
	text, sel, err := ParseAnnotated(s)
	if err != nil {
		panic(err)
	}

	return text, sel
}

// Annotate renders text with selection markers, the inverse of
// ParseAnnotated.
func Annotate(text buffer.Rope, sel Selection) string {
	type marker struct {
		byteIdx int
		text    string
	}

	markers := make([]marker, 0, 2*sel.Len())
	for i, r := range sel.Ranges() {
		isPrimary := i == sel.PrimaryIndex()
		var open, closer string
		switch {
		case r.Anchor < r.Head && isPrimary:
			open, closer = "#[", "|]#"
		case r.Anchor < r.Head:
			open, closer = "#(", "|)#"
		case isPrimary:
			open, closer = "]#", "#[|"
		default:
			open, closer = ")#", "#(|"
		}

		markers = append(markers,
			marker{text.CharToByte(r.Anchor), open},
			marker{text.CharToByte(r.Head), closer},
		)
	}

	// Insert from the back so earlier byte offsets stay valid.
	slices.SortStableFunc(markers, func(a, b marker) int { return cmp.Compare(b.byteIdx, a.byteIdx) })

	out := text.String()
	for _, m := range markers {
		out = out[:m.byteIdx] + m.text + out[m.byteIdx:]
	}

	return out
}

func splitGraphemes(s string) []string {
	out := make([]string, 0, len(s))
	state := -1
	for len(s) > 0 {
		var g string
		g, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		out = append(out, g)
	}

	return out
}

func lastGrapheme(s string) string {
	last := ""
	state := -1
	for len(s) > 0 {
		last, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
	}

	return last
}
