package edit

import (
	"unicode"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// Pair is an opening and closing char that are inserted together.
type Pair struct {
	Open  rune
	Close rune
}

// DefaultPairs are inserted when no language overrides them.
var DefaultPairs = []Pair{{'(', ')'}, {'{', '}'}, {'[', ']'}, {'\'', '\''}, {'"', '"'}, {'`', '`'}}

// AutoPairs looks pairs up by either side.
type AutoPairs struct {
	byChar map[rune]Pair
}

// NewAutoPairs builds the lookup from pairs.
func NewAutoPairs(pairs []Pair) *AutoPairs {
	ap := &AutoPairs{byChar: map[rune]Pair{}}
	for _, p := range pairs {
		ap.byChar[p.Open] = p
		if p.Open != p.Close {
			ap.byChar[p.Close] = p
		}
	}

	return ap
}

// Get returns the pair a char belongs to.
func (ap *AutoPairs) Get(ch rune) (Pair, bool) {
	p, ok := ap.byChar[ch]

	return p, ok
}

func (p Pair) same() bool {
	return p.Open == p.Close
}

func charAt(text buffer.Rope, pos int) (rune, bool) {
	if pos < 0 || pos >= text.Len() {
		return 0, false
	}

	return text.Char(pos), true
}

func (p Pair) shouldClose(text buffer.Rope, r Range) bool {
	cursor := r.Cursor(text)
	next, ok := charAt(text, cursor)
	should := !ok || !isAlnum(next)
	if p.same() {
		prev, ok := charAt(text, cursor-1)
		should = should && (!ok || !isAlnum(prev))
	}

	return should
}

func isAlnum(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsNumber(ch)
}

// HookInsert returns the change and new range for typing ch when a pair
// applies. autoClosed reports whether the quote at a position was put
// there by a pair insertion, only those are typed over. Nil counts every
// quote as such.
func (ap *AutoPairs) HookInsert(text buffer.Rope, r Range, ch rune, autoClosed func(pos int) bool) (Change, Range, bool) {
	if p, ok := ap.Get(ch); ok {
		switch {
		case p.same():
			return handleInsertSame(text, r, p, autoClosed)
		case p.Open == ch:
			return handleInsertOpen(text, r, p)
		default:
			return handleInsertClose(text, r, p)
		}
	}

	if unicode.IsSpace(ch) {
		return ap.handleInsertWhitespace(text, r, ch)
	}

	return Change{}, Range{}, false
}

// HookDelete removes both sides of an empty pair around the cursor.
func (ap *AutoPairs) HookDelete(text buffer.Rope, r Range) (int, int, Range, bool) {
	cursor := r.Cursor(text)
	cur, ok1 := charAt(text, cursor)
	prev, ok2 := charAt(text, cursor-1)
	if !ok1 || !ok2 {
		return 0, 0, Range{}, false
	}

	if text.Len() >= 4 && unicode.IsSpace(prev) && unicode.IsSpace(cur) {
		secondPrev, ok3 := charAt(text, text.NthPrevGraphemeBoundary(cursor, 2))
		secondNext, ok4 := charAt(text, text.NextGraphemeBoundary(cursor))
		if ok3 && ok4 {
			if p, ok := ap.Get(secondPrev); ok && p.Open == secondPrev && p.Close == secondNext {
				return handleDelete(text, r)
			}
		}
	}

	p, ok := ap.Get(cur)
	if !ok || p.Open != prev || p.Close != cur {
		return 0, 0, Range{}, false
	}

	return handleDelete(text, r)
}

func handleDelete(text buffer.Rope, r Range) (int, int, Range, bool) {
	cursor := r.Cursor(text)
	endNext := text.NextGraphemeBoundary(cursor)
	endPrev := text.PrevGraphemeBoundary(cursor)
	size := endNext - endPrev
	nextHead := text.NextGraphemeBoundary(r.Head) - size

	var nextAnchor int
	single := r.IsSingleGrapheme(text)
	switch {
	case r.Direction() == Forward && single:
		nextAnchor = r.Anchor - (endNext - cursor)
	case r.Direction() == Backward && single:
		nextAnchor = r.Anchor - (cursor - endPrev)
	case r.Direction() == Forward:
		nextAnchor = r.Anchor
	default:
		nextAnchor = r.Anchor - size
	}

	return endPrev, endNext, NewRange(max(nextAnchor, 0), max(nextHead, 0)), true
}

func (ap *AutoPairs) handleInsertWhitespace(text buffer.Rope, r Range, ch rune) (Change, Range, bool) {
	cursor := r.Cursor(text)
	cur, ok1 := charAt(text, cursor)
	prev, ok2 := charAt(text, cursor-1)
	if !ok1 || !ok2 {
		return Change{}, Range{}, false
	}

	p, ok := ap.Get(cur)
	if !ok || p.Open != prev || p.Close != cur {
		return Change{}, Range{}, false
	}

	return handleInsertSame(text, r, Pair{Open: ch, Close: ch}, nil)
}

// nextRange computes the selection after inserting lenInserted chars at
// the cursor of start.
func nextRange(text buffer.Rope, start Range, lenInserted int) Range {
	if start.Head == text.Len() && start.Anchor == text.Len() {
		return NewRange(start.Anchor+1, start.Head+1)
	}

	single := start.IsSingleGrapheme(text)

	if lenInserted == 0 {
		anchor := start.Anchor
		if single {
			anchor = text.NextGraphemeBoundary(start.Anchor)
		}

		return NewRange(anchor, text.NextGraphemeBoundary(start.Head))
	}

	if lenInserted == 1 {
		anchor := start.Anchor
		if single || start.Direction() == Backward {
			anchor++
		}

		return NewRange(anchor, start.Head+1)
	}

	var endHead int
	if start.Head == 0 || start.Direction() == Backward {
		endHead = start.Head + 1
	} else {
		endHead = text.PrevGraphemeBoundary(start.Head) + lenInserted
	}

	var endAnchor int
	switch {
	case start.Len() == 0:
		endAnchor = endHead
	case start.Len() == 1 && start.Direction() == Forward:
		endAnchor = endHead - 1
	case start.Len() == 1:
		endAnchor = endHead + 1
	case start.Direction() == Forward:
		if single {
			endAnchor = text.PrevGraphemeBoundary(start.Head) + 1
		} else {
			endAnchor = start.Anchor
		}
	default:
		if single {
			endAnchor = text.PrevGraphemeBoundary(start.Anchor) + lenInserted
		} else {
			endAnchor = start.Anchor + lenInserted
		}
	}

	return NewRange(endAnchor, endHead)
}

func handleInsertOpen(text buffer.Rope, r Range, p Pair) (Change, Range, bool) {
	cursor := r.Cursor(text)
	if _, ok := charAt(text, cursor); ok && !p.shouldClose(text, r) {
		return Change{}, Range{}, false
	}

	change := Change{From: cursor, To: cursor, Text: string([]rune{p.Open, p.Close})}

	return change, nextRange(text, r, 2), true
}

func handleInsertClose(text buffer.Rope, r Range, p Pair) (Change, Range, bool) {
	cursor := r.Cursor(text)
	if next, ok := charAt(text, cursor); !ok || next != p.Close {
		return Change{}, Range{}, false
	}

	return Change{From: cursor, To: cursor}, nextRange(text, r, 0), true
}

func handleInsertSame(text buffer.Rope, r Range, p Pair, autoClosed func(pos int) bool) (Change, Range, bool) {
	cursor := r.Cursor(text)
	if next, ok := charAt(text, cursor); ok && next == p.Open && (autoClosed == nil || autoClosed(cursor)) {
		return Change{From: cursor, To: cursor}, nextRange(text, r, 0), true
	}

	if !p.shouldClose(text, r) {
		return Change{}, Range{}, false
	}

	change := Change{From: cursor, To: cursor, Text: string([]rune{p.Open, p.Close})}

	return change, nextRange(text, r, 2), true
}
