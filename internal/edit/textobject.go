package edit

import (
	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// TextObjectKind selects the inside or around variant of a text object.
type TextObjectKind int

// Text object kinds.
const (
	Around TextObjectKind = iota
	Inside

	// ObjectMovement is used when jumping between objects.
	ObjectMovement
)

// String returns the capture suffix used in textobjects.scm.
func (k TextObjectKind) String() string {
	switch k {
	case Inside:
		return "inside"
	case ObjectMovement:
		return "movement"
	default:
		return "around"
	}
}

func findWordBoundary(text buffer.Rope, pos int, dir Direction, long bool) int {
	var prev CharCategory
	switch {
	case dir == Forward && pos == 0, dir == Backward && pos == text.Len():
		prev = CatWhitespace
	case dir == Forward:
		prev = CategorizeChar(text.Char(pos - 1))
	default:
		prev = CategorizeChar(text.Char(pos))
	}

	c := &charCursor{text: text, pos: pos, rev: dir == Backward}
	for {
		ch, ok := c.next()
		if !ok {
			break
		}

		cat := CategorizeChar(ch)
		if cat == CatEOL || cat == CatWhitespace {
			return pos
		}

		if !long && cat != prev && pos != 0 && pos != text.Len() {
			return pos
		}

		if dir == Forward {
			pos++
		} else {
			pos = max(pos-1, 0)
		}

		prev = cat
	}

	return pos
}

// TextObjectWord selects the word under the cursor. Around also takes
// the whitespace after it, or before it when none follows.
func TextObjectWord(text buffer.Rope, r Range, kind TextObjectKind, long bool) Range {
	pos := r.Cursor(text)

	wordStart := findWordBoundary(text, pos, Backward, long)
	wordEnd := pos
	if pos < text.Len() {
		cat := CategorizeChar(text.Char(pos))
		if cat != CatWhitespace && cat != CatEOL {
			wordEnd = findWordBoundary(text, pos+1, Forward, long)
		}
	}

	if wordStart == wordEnd {
		return NewRange(wordStart, wordEnd)
	}

	if kind != Around {
		return NewRange(wordStart, wordEnd)
	}

	right := 0
	for p := wordEnd; p < text.Len() && IsWhitespaceChar(text.Char(p)); p++ {
		right++
	}

	if right > 0 {
		return NewRange(wordStart, wordEnd+right)
	}

	left := 0
	for p := wordStart - 1; p >= 0 && IsWhitespaceChar(text.Char(p)); p-- {
		left++
	}

	return NewRange(wordStart-left, wordEnd)
}

// TextObjectParagraph selects count paragraphs around the cursor.
func TextObjectParagraph(text buffer.Rope, r Range, kind TextObjectKind, count int) Range {
	line := r.CursorLine(text)
	lines := text.LenLines()
	blank := func(l int) bool {
		if l < 0 || l >= lines {
			return true
		}

		return text.IsLineBlank(l)
	}

	prevLineEmpty := blank(line - 1)
	currLineEmpty := blank(line)
	nextLineEmpty := line+1 >= lines || blank(line+1)
	lastChar := text.PrevGraphemeBoundary(text.LineToChar(line+1)) == r.Cursor(text)
	prevEmptyToLine := prevLineEmpty && !currLineEmpty
	currEmptyToLine := currLineEmpty && !nextLineEmpty

	lineBack := line
	if prevEmptyToLine || currEmptyToLine {
		lineBack++
	}

	if !(currEmptyToLine && lastChar) {
		for lineBack > 0 && blank(lineBack-1) {
			lineBack--
		}

		for lineBack > 0 && !blank(lineBack-1) {
			lineBack--
		}
	}

	if currEmptyToLine && lastChar {
		line++
	}

	countDone := 0
	for range count {
		done := false
		for line < lines && !blank(line) {
			line++
			done = true
		}

		for line < lines && blank(line) {
			line++
		}

		if done {
			countDone++
		}
	}

	// At the end of the text look one paragraph back so the object is
	// still useful after trailing newlines.
	if countDone != count && line >= lines {
		for lineBack > 0 && blank(lineBack-1) {
			lineBack--
		}

		for lineBack > 0 && !blank(lineBack-1) {
			lineBack--
		}
	}

	if kind == Inside {
		for line > 0 && blank(line-1) && line > lineBack {
			line--
		}
	}

	anchor := text.LineToChar(min(lineBack, lines))
	head := text.Len()
	if line < lines {
		head = text.LineToChar(line)
	}

	return NewRange(anchor, head)
}

// TextObjectPairSurround selects the contents of the nth pair of ch.
func TextObjectPairSurround(matcher BracketMatcher, text buffer.Rope, r Range, kind TextObjectKind, ch rune, count int) Range {
	anchor, head, err := FindNthPairsPos(matcher, text, ch, r, count)
	if err != nil {
		return r
	}

	return pairRange(text, anchor, head, kind)
}

// TextObjectPairSurroundClosest selects the contents of the nth closest
// pair of any kind.
func TextObjectPairSurroundClosest(matcher BracketMatcher, text buffer.Rope, r Range, kind TextObjectKind, count int) Range {
	anchor, head, err := FindNthClosestPairsPos(matcher, text, r, count)
	if err != nil {
		return r
	}

	return pairRange(text, anchor, head, kind)
}

func pairRange(text buffer.Rope, anchor, head int, kind TextObjectKind) Range {
	switch {
	case kind == Inside && anchor < head:
		return NewRange(text.NextGraphemeBoundary(anchor), head)
	case kind == Inside:
		return NewRange(anchor, text.NextGraphemeBoundary(head))
	case anchor < head:
		return NewRange(anchor, text.NextGraphemeBoundary(head))
	default:
		return NewRange(text.NextGraphemeBoundary(anchor), head)
	}
}
