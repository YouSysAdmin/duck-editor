package edit

import (
	"unicode"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// Movement tells whether a motion moves the cursor or extends the range.
type Movement int

// Movement kinds.
const (
	Move Movement = iota
	Extend
)

// MoveHorizontally moves the block cursor by count graphemes.
func MoveHorizontally(text buffer.Rope, r Range, dir Direction, count int, behaviour Movement) Range {
	pos := r.Cursor(text)
	var newPos int
	if dir == Forward {
		newPos = text.NthNextGraphemeBoundary(pos, count)
	} else {
		newPos = text.NthPrevGraphemeBoundary(pos, count)
	}

	return r.PutCursor(text, newPos, behaviour == Extend)
}

// MoveVertically moves the block cursor by count lines keeping the visual
// column remembered in OldVisual.
func MoveVertically(text buffer.Rope, r Range, dir Direction, count int, behaviour Movement, tabWidth int) Range {
	pos := r.Cursor(text)
	visual := VisualCoordsAtPos(text, pos, tabWidth)
	newCol := visual.Col
	if r.OldVisual.Valid {
		newCol = r.OldVisual.Col
	}

	lineIdx := visual.Row
	var newLine int
	if dir == Forward {
		newLine = min(lineIdx+count, text.LenLines()-1)
	} else {
		newLine = max(lineIdx-count, 0)
	}

	newPos := PosAtVisualCoords(text, Position{Row: newLine, Col: newCol}, tabWidth)

	// Do not extend onto an empty trailing line.
	if behaviour == Extend && text.Line(newLine) == "" {
		return r
	}

	out := r.PutCursor(text, newPos, behaviour == Extend)
	out.OldVisual = VisualPosition{Row: 0, Col: newCol, Valid: true}

	return out
}

// WordMotionTarget is the kind of word boundary a motion stops at.
type WordMotionTarget int

// Word motion targets. Long words are delimited by whitespace only, sub
// words also split on underscores and case changes.
const (
	NextWordStart WordMotionTarget = iota
	NextWordEnd
	PrevWordStart
	PrevWordEnd
	NextLongWordStart
	NextLongWordEnd
	PrevLongWordStart
	PrevLongWordEnd
	NextSubWordStart
	NextSubWordEnd
	PrevSubWordStart
	PrevSubWordEnd
)

func (t WordMotionTarget) isPrev() bool {
	switch t {
	case PrevWordStart, PrevLongWordStart, PrevSubWordStart, PrevWordEnd, PrevLongWordEnd, PrevSubWordEnd:
		return true
	}

	return false
}

// WordMove performs count word motions towards target.
func WordMove(text buffer.Rope, r Range, count int, target WordMotionTarget) Range {
	isPrev := target.isPrev()
	if (isPrev && r.Head == 0) || (!isPrev && r.Head == text.Len()) {
		return r
	}

	// Normalize to block cursor semantics, the anchor is irrelevant.
	var start Range
	switch {
	case isPrev && r.Anchor < r.Head:
		start = NewRange(r.Head, text.PrevGraphemeBoundary(r.Head))
	case isPrev:
		start = NewRange(text.NextGraphemeBoundary(r.Head), r.Head)
	case r.Anchor < r.Head:
		start = NewRange(text.PrevGraphemeBoundary(r.Head), r.Head)
	default:
		start = NewRange(r.Head, text.NextGraphemeBoundary(r.Head))
	}

	cur := start
	for range count {
		next := rangeToTarget(text, target, cur)
		if next == cur {
			break
		}

		cur = next
	}

	return cur
}

// charCursor walks the chars of a rope, next in its direction and prev
// back the other way. rev makes the direction backwards.
type charCursor struct {
	text buffer.Rope
	pos  int
	rev  bool
}

func (c *charCursor) next() (rune, bool) {
	if c.rev {
		return c.backward()
	}

	return c.forward()
}

func (c *charCursor) prev() (rune, bool) {
	if c.rev {
		return c.forward()
	}

	return c.backward()
}

func (c *charCursor) forward() (rune, bool) {
	if c.pos >= c.text.Len() {
		return 0, false
	}

	ch := c.text.Char(c.pos)
	c.pos++

	return ch, true
}

func (c *charCursor) backward() (rune, bool) {
	if c.pos <= 0 {
		return 0, false
	}

	c.pos--

	return c.text.Char(c.pos), true
}

// rangeToTarget advances from the head of origin to the next target
// boundary. The anchor only changes when the head starts on a boundary.
func rangeToTarget(text buffer.Rope, target WordMotionTarget, origin Range) Range {
	isPrev := target.isPrev()
	chars := &charCursor{text: text, pos: origin.Head, rev: isPrev}

	advance := func(idx int) int {
		if isPrev {
			return max(idx-1, 0)
		}

		return idx + 1
	}

	anchor := origin.Anchor
	head := origin.Head

	var prevCh rune
	havePrev := false
	if ch, ok := chars.prev(); ok {
		prevCh, havePrev = ch, true
		chars.next()
	}

	// Skip any initial newline characters.
	for {
		ch, ok := chars.next()
		if !ok {
			break
		}

		if IsLineEnding(ch) {
			prevCh, havePrev = ch, true
			head = advance(head)

			continue
		}

		chars.prev()

		break
	}

	if havePrev && IsLineEnding(prevCh) {
		anchor = head
	}

	headStart := head
	for {
		nextCh, ok := chars.next()
		if !ok {
			break
		}

		if !havePrev || reachedTarget(target, prevCh, nextCh) {
			if head == headStart {
				anchor = head
			} else {
				break
			}
		}

		prevCh, havePrev = nextCh, true
		head = advance(head)
	}

	return NewRange(anchor, head)
}

func isWordBoundary(a, b rune) bool {
	return CategorizeChar(a) != CategorizeChar(b)
}

func isLongWordBoundary(a, b rune) bool {
	ca, cb := CategorizeChar(a), CategorizeChar(b)
	if (ca == CatWord && cb == CatPunctuation) || (ca == CatPunctuation && cb == CatWord) {
		return false
	}

	return ca != cb
}

func isSubWordBoundary(a, b rune, dir Direction) bool {
	ca, cb := CategorizeChar(a), CategorizeChar(b)
	if ca == CatWord && cb == CatWord {
		if (a == '_') != (b == '_') {
			return true
		}

		if dir == Forward {
			return unicode.IsLower(a) && unicode.IsUpper(b)
		}

		return unicode.IsUpper(a) && unicode.IsLower(b)
	}

	return ca != cb
}

func reachedTarget(target WordMotionTarget, prev, next rune) bool {
	switch target {
	case NextWordStart, PrevWordEnd:
		return isWordBoundary(prev, next) && (IsLineEnding(next) || !unicode.IsSpace(next))
	case NextWordEnd, PrevWordStart:
		return isWordBoundary(prev, next) && (!unicode.IsSpace(prev) || IsLineEnding(next))
	case NextLongWordStart, PrevLongWordEnd:
		return isLongWordBoundary(prev, next) && (IsLineEnding(next) || !unicode.IsSpace(next))
	case NextLongWordEnd, PrevLongWordStart:
		return isLongWordBoundary(prev, next) && (!unicode.IsSpace(prev) || IsLineEnding(next))
	case NextSubWordStart:
		return isSubWordBoundary(prev, next, Forward) && (IsLineEnding(next) || !(unicode.IsSpace(next) || next == '_'))
	case PrevSubWordEnd:
		return isSubWordBoundary(prev, next, Backward) && (IsLineEnding(next) || !(unicode.IsSpace(next) || next == '_'))
	case NextSubWordEnd:
		return isSubWordBoundary(prev, next, Forward) && (!(unicode.IsSpace(prev) || prev == '_') || IsLineEnding(next))
	case PrevSubWordStart:
		return isSubWordBoundary(prev, next, Backward) && (!(unicode.IsSpace(prev) || prev == '_') || IsLineEnding(next))
	}

	return false
}

// MovePrevParagraph moves to the start of the previous paragraph.
func MovePrevParagraph(text buffer.Rope, r Range, count int, behaviour Movement) Range {
	line := r.CursorLine(text)
	firstChar := text.LineToChar(line) == r.Cursor(text)
	prevLineEmpty := text.IsLineBlank(max(line-1, 0))
	currLineEmpty := text.IsLineBlank(line)
	prevEmptyToLine := prevLineEmpty && !currLineEmpty

	if prevEmptyToLine && !firstChar {
		line++
	}

	// line is exclusive here, walk the lines above it backwards
	lastLine := line
	for range count {
		for line > 0 && text.IsLineBlank(line-1) {
			line--
		}

		for line > 0 && !text.IsLineBlank(line-1) {
			line--
		}

		if line == lastLine {
			break
		}

		lastLine = line
	}

	head := text.LineToChar(line)
	var anchor int
	switch {
	case behaviour == Move && prevEmptyToLine && firstChar:
		anchor = r.Cursor(text)
	case behaviour == Move:
		anchor = r.Head
	default:
		anchor = r.PutCursor(text, head, true).Anchor
	}

	return NewRange(anchor, head)
}

// MoveNextParagraph moves to the start of the next paragraph.
func MoveNextParagraph(text buffer.Rope, r Range, count int, behaviour Movement) Range {
	line := r.CursorLine(text)
	lastLineIdx := text.LenLines() - 1
	lineEnd := text.LineToChar(line + 1)
	lastChar := text.PrevGraphemeBoundary(lineEnd) == r.Cursor(text)
	currLineEmpty := text.IsLineBlank(line)
	nextLineEmpty := text.IsLineBlank(min(line+1, lastLineIdx))
	currEmptyToLine := currLineEmpty && !nextLineEmpty

	if currEmptyToLine && lastChar {
		line++
	}

	lastLine := line
	for range count {
		for line < text.LenLines() && !text.IsLineBlank(line) {
			line++
		}

		for line < text.LenLines() && text.IsLineBlank(line) {
			line++
		}

		if line == lastLine {
			break
		}

		lastLine = line
	}

	head := text.LineToChar(min(line, text.LenLines()))
	var anchor int
	switch {
	case behaviour == Move && currEmptyToLine && lastChar:
		anchor = r.Head
	case behaviour == Move:
		anchor = r.Cursor(text)
	default:
		anchor = r.PutCursor(text, head, true).Anchor
	}

	return NewRange(anchor, head)
}
