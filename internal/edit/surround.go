package edit

import (
	"errors"
	"slices"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// Surround errors.
var (
	ErrPairNotFound          = errors.New("surround pair not found around all cursors")
	ErrCursorOverlap         = errors.New("cursors overlap for a single surround pair range")
	ErrRangeExceedsText      = errors.New("cursor range exceeds text length")
	ErrCursorOnAmbiguousPair = errors.New("cursor on ambiguous surround pair")
)

// BracketMatcher finds matching brackets with a syntax tree. With fuzzy
// set, a cursor inside a pair resolves to the pair's closing bracket.
type BracketMatcher interface {
	MatchingBracket(pos int, fuzzy bool) (int, bool)
}

// FindNthClosestPairsPos finds the nth pair of any kind enclosing the
// range. It returns anchor and head, which keep the range direction.
func FindNthClosestPairsPos(matcher BracketMatcher, text buffer.Rope, r Range, skip int) (int, int, error) {
	if matcher != nil {
		return findNthClosestPairsTS(matcher, text, r, skip)
	}

	return findNthClosestPairsPlain(text, r, skip)
}

func findNthClosestPairsTS(matcher BracketMatcher, text buffer.Rope, r Range, skip int) (int, int, error) {
	opening := r.From()
	closing := r.To()
	search := closing

	for skip > 0 {
		var ok bool
		closing, ok = matcher.MatchingBracket(search, true)
		if !ok {
			return 0, 0, ErrPairNotFound
		}

		opening, ok = matcher.MatchingBracket(closing, false)
		if !ok {
			return 0, 0, ErrPairNotFound
		}

		if closing < opening {
			opening, closing = closing, opening
		}

		// The next search starts after this pair. A match that does not
		// move it forward would repeat forever.
		next := text.NextGraphemeBoundary(closing)
		if next <= search {
			return 0, 0, ErrPairNotFound
		}

		search = next
		// Pairs partially inside the selection do not count.
		if r.From() >= opening && closing >= r.To()-1 {
			skip--
		}
	}

	if r.Direction() == Forward {
		return opening, closing, nil
	}

	return closing, opening, nil
}

func findNthClosestPairsPlain(text buffer.Rope, r Range, skip int) (int, int, error) {
	stack := []rune{}
	pos := r.From()
	closePos := max(pos-1, 0)
	if pos == 0 {
		closePos = -1
	}

	for p := pos; p < text.Len(); p++ {
		ch := text.Char(p)
		closePos++

		if IsOpenBracket(ch) {
			stack = append(stack, ch)

			continue
		}

		if !IsCloseBracket(ch) {
			continue
		}

		open, closer := GetPair(ch)
		if len(stack) > 0 && stack[len(stack)-1] == open {
			stack = stack[:len(stack)-1]

			continue
		}

		openPos, ok := findNthOpenPair(text, open, closer, closePos, 1)
		if !ok || openPos > pos+1 || closePos < max(r.To()-1, 0) {
			continue
		}

		if skip > 1 {
			skip--

			continue
		}

		if r.Direction() == Forward {
			return openPos, closePos, nil
		}

		return closePos, openPos, nil
	}

	return 0, 0, ErrPairNotFound
}

// FindNthPairsPos finds the nth pair of ch around the range cursor.
// It returns anchor and head, which keep the range direction.
func FindNthPairsPos(matcher BracketMatcher, text buffer.Rope, ch rune, r Range, n int) (int, int, error) {
	if text.Len() < 2 {
		return 0, 0, ErrPairNotFound
	}

	if r.To() >= text.Len() {
		return 0, 0, ErrRangeExceedsText
	}

	open, closer := GetPair(ch)
	pos := r.Cursor(text)

	var openPos, closePos int
	var okOpen, okClose bool
	switch {
	case open == closer && pos < text.Len() && text.Char(pos) == open:
		// On a quote char it is ambiguous which side to search, ask the
		// syntax tree or fall back to plain bracket matching.
		var match int
		var ok bool
		if matcher != nil {
			match, ok = matcher.MatchingBracket(pos, true)
		} else {
			match, ok = FindMatchingBracketPlaintext(text, pos)
		}

		if !ok {
			return 0, 0, ErrCursorOnAmbiguousPair
		}

		if match > pos {
			openPos, closePos = pos, match
		} else {
			openPos, closePos = match, pos
		}

		okOpen, okClose = true, true
	case open == closer:
		openPos, okOpen = FindNthChar(n, text, func(c rune) bool { return c == open }, pos, Backward)
		closePos, okClose = FindNthChar(n, text, func(c rune) bool { return c == closer }, pos, Forward)
	default:
		openPos, okOpen = findNthOpenPair(text, open, closer, pos, n)
		closePos, okClose = findNthClosePair(text, open, closer, pos, n)
	}

	if !okOpen || !okClose {
		return 0, 0, ErrPairNotFound
	}

	if r.Direction() == Forward {
		return openPos, closePos, nil
	}

	return closePos, openPos, nil
}

// findNthOpenPair scans backwards from pos for the nth unbalanced open
// char, skipping nested pairs.
func findNthOpenPair(text buffer.Rope, open, closer rune, pos, n int) (int, bool) {
	if pos < 0 || pos >= text.Len() {
		return 0, false
	}

	if text.Char(pos) == open {
		return pos, true
	}

	p := pos
	for range n {
		stepOver := 0
		for {
			if p == 0 {
				return 0, false
			}

			p--
			c := text.Char(p)
			if c == closer {
				stepOver++
			} else if c == open {
				if stepOver == 0 {
					break
				}

				stepOver--
			}
		}
	}

	return p, true
}

// findNthClosePair scans forwards from pos for the nth unbalanced close
// char, skipping nested pairs.
func findNthClosePair(text buffer.Rope, open, closer rune, pos, n int) (int, bool) {
	if pos < 0 || pos >= text.Len() {
		return 0, false
	}

	if text.Char(pos) == closer {
		return pos, true
	}

	p := pos
	for range n {
		stepOver := 0
		for {
			p++
			if p >= text.Len() {
				return 0, false
			}

			c := text.Char(p)
			if c == open {
				stepOver++
			} else if c == closer {
				if stepOver == 0 {
					break
				}

				stepOver--
			}
		}
	}

	return p, true
}

// GetSurroundPos finds the surround positions around every range as a
// flat list of open, close pairs in text order per range. A zero ch picks
// the closest pair for each range.
func GetSurroundPos(matcher BracketMatcher, text buffer.Rope, sel Selection, ch rune, skip int) ([]int, error) {
	out := make([]int, 0, 2*sel.Len())
	for _, r := range sel.Ranges() {
		var a, h int
		var err error
		if ch != 0 {
			a, h, err = FindNthPairsPos(matcher, text, ch, r, skip)
		} else {
			a, h, err = FindNthClosestPairsPos(matcher, text, r, skip)
		}

		if err != nil {
			return nil, err
		}

		openPos, closePos := min(a, h), max(a, h)
		if slices.Contains(out, openPos) || slices.Contains(out, closePos) {
			return nil, ErrCursorOverlap
		}

		out = append(out, openPos, closePos)
	}

	return out, nil
}
