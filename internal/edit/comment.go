package edit

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// DefaultCommentToken is used when a language defines none.
const DefaultCommentToken = "#"

// BlockCommentToken is a pair of block comment delimiters.
type BlockCommentToken struct {
	Start string
	End   string
}

// DefaultBlockCommentToken is the C style block comment.
var DefaultBlockCommentToken = BlockCommentToken{Start: "/*", End: "*/"}

// CommentTokenOfLine returns the longest token that starts line, if any.
func CommentTokenOfLine(text buffer.Rope, tokens []string, line int) (string, bool) {
	content := text.Line(line)
	off, ok := FirstNonWhitespace(content)
	if !ok {
		return "", false
	}

	rest := string([]rune(content)[off:])
	best := ""
	for _, tok := range tokens {
		if strings.HasPrefix(rest, tok) && len(tok) > len(best) {
			best = tok
		}
	}

	return best, best != ""
}

// findLineComment reports whether all non blank lines are commented with
// token, the lines to change, the column for tokens and the margin after
// them.
func findLineComment(token string, text buffer.Rope, lines []int) (commented bool, toChange []int, col, margin int) {
	commented = true
	col = -1
	margin = 1
	tokenLen := utf8.RuneCountInString(token)

	for _, line := range lines {
		lineText := text.Line(line)
		content := []rune(lineText)
		pos, ok := FirstNonWhitespace(lineText)
		if !ok {
			continue
		}

		if col < 0 || pos < col {
			col = pos
		}

		end := min(pos+tokenLen, len(content))
		if string(content[pos:end]) != token {
			commented = false
		}

		if pos+tokenLen >= len(content) || content[pos+tokenLen] != ' ' {
			margin = 0
		}

		toChange = append(toChange, line)
	}

	if col < 0 {
		col = 0
	}

	return commented, toChange, col, margin
}

// ToggleLineComments comments or uncomments the lines of the selection.
func ToggleLineComments(text buffer.Rope, sel Selection, token string) Transaction {
	if token == "" {
		token = DefaultCommentToken
	}

	comment := token + " "

	var lines []int
	minNext := 0
	for _, r := range sel.Ranges() {
		start, end := r.LineRange(text)
		start = min(max(start, minNext), text.LenLines())
		end = min(end+1, text.LenLines())
		for l := start; l < end; l++ {
			lines = append(lines, l)
		}

		minNext = end
	}

	commented, toChange, col, margin := findLineComment(token, text, lines)
	changes := make([]Change, 0, len(toChange))
	for _, line := range toChange {
		pos := text.LineToChar(line) + col
		if commented {
			changes = append(changes, Change{From: pos, To: pos + utf8.RuneCountInString(token) + margin})
		} else {
			changes = append(changes, Change{From: pos, To: pos, Text: comment})
		}
	}

	return ChangeText(text, changes)
}

// commentChange describes one range for block commenting.
type commentChange struct {
	r           Range
	startPos    int
	endPos      int
	startMargin bool
	endMargin   bool
	startToken  string
	endToken    string
	kind        commentKind
}

// commentKind says what a range holds before a block comment toggle.
type commentKind int

const (
	rangeCommented commentKind = iota
	rangeUncommented
	rangeWhitespace
)

func findBlockComments(tokens []BlockCommentToken, text buffer.Rope, sel Selection) (bool, []commentChange) {
	commented := true
	onlyWhitespace := true
	var out []commentChange
	def := DefaultBlockCommentToken
	if len(tokens) > 0 {
		def = tokens[0]
	}

	// Longer tokens are tried first so "/**" wins over "/*".
	sorted := slices.Clone(tokens)
	slices.SortFunc(sorted, func(a, b BlockCommentToken) int {
		return cmp.Or(cmp.Compare(len(b.Start), len(a.Start)), cmp.Compare(len(b.End), len(a.End)))
	})

	for _, r := range sel.Ranges() {
		raw := r.Fragment(text)
		fragment := []rune(raw)
		startPos, ok1 := FirstNonWhitespace(raw)
		endPos, ok2 := LastNonWhitespace(raw)
		if !ok1 || !ok2 {
			out = append(out, commentChange{r: r, kind: rangeWhitespace})

			continue
		}

		onlyWhitespace = false
		found := false
		var afterStart, beforeEnd int
		var startTok, endTok string
		length := endPos + 1 - startPos
		for _, tok := range sorted {
			sl := utf8.RuneCountInString(tok.Start)
			el := utf8.RuneCountInString(tok.End)
			afterStart = startPos + sl
			beforeEnd = max(endPos-el, 0)
			if length >= sl+el &&
				string(fragment[startPos:afterStart]) == tok.Start &&
				string(fragment[beforeEnd+1:endPos+1]) == tok.End {
				startTok, endTok = tok.Start, tok.End
				found = true

				break
			}
		}

		if !found {
			out = append(out, commentChange{r: r, startPos: startPos, endPos: endPos, startToken: def.Start, endToken: def.End, kind: rangeUncommented})
			commented = false

			continue
		}

		out = append(out, commentChange{
			r: r, startPos: startPos, endPos: endPos,
			startMargin: afterStart < len(fragment) && fragment[afterStart] == ' ',
			endMargin:   afterStart != beforeEnd && beforeEnd < len(fragment) && fragment[beforeEnd] == ' ',
			startToken:  startTok, endToken: endTok, kind: rangeCommented,
		})
	}

	if onlyWhitespace {
		commented = false
	}

	return commented, out
}

func createBlockCommentTransaction(text buffer.Rope, commented bool, changes []commentChange) (Transaction, []Range) {
	var edits []Change
	var ranges []Range
	offs := 0
	for _, c := range changes {
		from := c.r.From()
		switch {
		case commented:
			if c.kind != rangeCommented {
				continue
			}

			sm, em := 0, 0
			if c.startMargin {
				sm = 1
			}

			if c.endMargin {
				em = 1
			}

			edits = append(edits,
				Change{From: from + c.startPos, To: from + c.startPos + utf8.RuneCountInString(c.startToken) + sm},
				Change{From: from + c.endPos - utf8.RuneCountInString(c.endToken) - em + 1, To: from + c.endPos + 1},
			)
		case c.kind == rangeUncommented:
			edits = append(edits,
				Change{From: from + c.startPos, To: from + c.startPos, Text: c.startToken + " "},
				Change{From: from + c.endPos + 1, To: from + c.endPos + 1, Text: " " + c.endToken},
			)
			offset := utf8.RuneCountInString(c.startToken) + utf8.RuneCountInString(c.endToken) + 2
			ranges = append(ranges, NewRange(from+offs, from+offs+c.endPos+1+offset).WithDirection(c.r.Direction()))
			offs += offset
		default:
			ranges = append(ranges, NewRange(c.r.From()+offs, c.r.To()+offs))
		}
	}

	// Adjacent ranges insert an end and a start token at the same point,
	// a stable sort keeps them in text order.
	slices.SortStableFunc(edits, func(a, b Change) int { return cmp.Compare(a.From, b.From) })

	return ChangeText(text, edits), ranges
}

// ToggleBlockComments wraps or unwraps every range in block comments.
func ToggleBlockComments(text buffer.Rope, sel Selection, tokens []BlockCommentToken) Transaction {
	commented, changes := findBlockComments(tokens, text, sel)
	tx, ranges := createBlockCommentTransaction(text, commented, changes)
	if !commented && len(ranges) > 0 {
		tx = tx.WithSelection(NewSelection(ranges, min(sel.PrimaryIndex(), len(ranges)-1)))
	}

	return tx
}

// SplitLinesOfSelection turns every range into one range per line.
func SplitLinesOfSelection(text buffer.Rope, sel Selection) Selection {
	var ranges []Range
	for _, r := range sel.Ranges() {
		start, end := r.LineRange(text)
		for l := start; l <= end; l++ {
			ranges = append(ranges, NewRange(text.LineToChar(l), text.LineToChar(l+1)))
		}
	}

	return NewSelection(ranges, 0)
}

// ToggleComments picks the style: line tokens only give line comments, block
// commented lines or selections get uncommented, otherwise block tokens
// comment line by line and plain line comments are the fallback.
func ToggleComments(text buffer.Rope, sel Selection, lineToken string, blockTokens []BlockCommentToken) Transaction {
	if lineToken != "" && blockTokens == nil {
		return ToggleLineComments(text, sel, lineToken)
	}

	splitLines := SplitLinesOfSelection(text, sel)
	tokens := blockTokens
	if tokens == nil {
		tokens = []BlockCommentToken{DefaultBlockCommentToken}
	}

	lineCommented, lineChanges := findBlockComments(tokens, text, splitLines)
	if lineCommented {
		tx, _ := createBlockCommentTransaction(text, true, lineChanges)

		return tx
	}

	blockCommented, blockChanges := findBlockComments(tokens, text, sel)
	if blockCommented {
		tx, _ := createBlockCommentTransaction(text, true, blockChanges)

		return tx
	}

	if lineToken == "" && blockTokens != nil {
		tx, _ := createBlockCommentTransaction(text, false, lineChanges)

		return tx
	}

	return ToggleLineComments(text, sel, lineToken)
}
