// Package fuzzy scores candidates against a typed pattern for pickers,
// using fzf style rules: subsequence matching with bonuses for word
// starts and consecutive chars, smart case, and space separated terms.
package fuzzy

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Scores, tuned after fzf v2.
const (
	scoreMatch        = 16
	scoreGapStart     = -3
	scoreGapExtension = -1
	bonusBoundary     = scoreMatch / 2
	bonusNonWord      = scoreMatch / 2
	bonusCamel        = bonusBoundary - 1
	bonusConsecutive  = -(scoreGapStart + scoreGapExtension)
	bonusFirstChar    = 2
	bonusWhole        = bonusBoundary
)

// Term is one space separated part of a pattern.
type Term struct {
	Text   string
	Exact  bool
	Prefix bool
	Suffix bool
	Negate bool

	// ignoreCase is set for an all lowercase term, smart case.
	ignoreCase bool
}

// Pattern is a parsed query.
type Pattern struct {
	Terms []Term
}

// Parse splits a query into terms. A term starting with ' matches
// exactly, ^ anchors to the start, $ to the end and ! negates.
func Parse(query string) Pattern {
	var p Pattern
	for _, raw := range strings.Fields(query) {
		t := Term{Text: raw}
		if strings.HasPrefix(t.Text, "!") {
			t.Negate = true
			t.Text = t.Text[1:]
		}

		if strings.HasPrefix(t.Text, "'") {
			t.Exact = true
			t.Text = t.Text[1:]
		}

		if strings.HasPrefix(t.Text, "^") {
			t.Prefix = true
			t.Text = t.Text[1:]
		}

		if strings.HasSuffix(t.Text, "$") {
			t.Suffix = true
			t.Text = strings.TrimSuffix(t.Text, "$")
		}

		if t.Text == "" {
			continue
		}

		t.ignoreCase = strings.ToLower(t.Text) == t.Text
		p.Terms = append(p.Terms, t)
	}

	return p
}

// IsEmpty reports a pattern that matches everything.
func (p Pattern) IsEmpty() bool {
	return len(p.Terms) == 0
}

// Match scores text against the pattern. Positions are the byte offsets
// of matched chars for highlighting. A pattern without positive terms,
// empty or only negated, matches with a zero score.
func (p Pattern) Match(text string) (score int, positions []int, ok bool) {
	score, positions, _, ok = p.MatchTight(text)

	return score, positions, ok
}

// MatchTight is Match that also tells a tight match from a scattered
// one. A term matches tightly as one run of chars, or in runs that each
// start a word, such as fpick in file_picker. Single letters strewn over
// a path, d in docs and a in mailyard for domain, are scattered.
func (p Pattern) MatchTight(text string) (score int, positions []int, tight, ok bool) {
	tight = true
	for _, t := range p.Terms {
		s, pos, termTight, matched := t.match(text)
		if t.Negate {
			if matched {
				return 0, nil, false, false
			}

			continue
		}

		if !matched {
			return 0, nil, false, false
		}

		score += s
		positions = append(positions, pos...)
		tight = tight && termTight
	}

	return score, positions, tight, true
}

func (t Term) match(text string) (int, []int, bool, bool) {
	// Case is folded per rune so rune indexes line up with text.
	hay := []rune(text)
	if t.ignoreCase {
		for i, r := range hay {
			hay[i] = unicode.ToLower(r)
		}
	}

	pat := []rune(t.Text)
	byteOff := byteOffsets(text, len(hay))
	n := len(pat)

	switch {
	case t.Prefix && t.Suffix:
		if slices.Equal(hay, pat) {
			return scoreMatch*n + bonusBoundary, positionsOf(byteOff, 0, n), true, true
		}

		return 0, nil, false, false
	case t.Prefix:
		if len(hay) >= n && slices.Equal(hay[:n], pat) {
			return scoreMatch*n + bonusBoundary, positionsOf(byteOff, 0, n), true, true
		}

		return 0, nil, false, false
	case t.Suffix:
		if start := len(hay) - n; start >= 0 && slices.Equal(hay[start:], pat) {
			return scoreMatch * n, positionsOf(byteOff, start, n), true, true
		}

		return 0, nil, false, false
	case t.Exact:
		idx := runeIndex(hay, pat)
		if idx < 0 {
			return 0, nil, false, false
		}

		return scoreMatch*n + boundaryBonus(text, byteOff[idx]), positionsOf(byteOff, idx, n), true, true
	}

	return fuzzyMatch(text, hay, pat, byteOff)
}

// byteOffsets maps every rune index of text, and the end, to its byte
// offset.
func byteOffsets(text string, runes int) []int {
	out := make([]int, 0, runes+1)
	for i := range text {
		out = append(out, i)
	}

	return append(out, len(text))
}

// positionsOf returns the byte offsets of n runes from rune index start.
func positionsOf(byteOff []int, start, n int) []int {
	return slices.Clone(byteOff[start : start+n])
}

// runeIndex finds pat in hay, -1 when absent.
func runeIndex(hay, pat []rune) int {
	for i := 0; i+len(pat) <= len(hay); i++ {
		if slices.Equal(hay[i:i+len(pat)], pat) {
			return i
		}
	}

	return -1
}

// charClass groups chars for boundary bonuses.
type charClass int

const (
	classNonWord charClass = iota
	classLower
	classUpper
	classDigit
	classDelimiter
)

func classify(r rune) charClass {
	switch {
	case r == '/' || r == '.' || r == ',' || r == ':' || r == '-' || r == '_' || r == ' ':
		return classDelimiter
	case unicode.IsLower(r):
		return classLower
	case unicode.IsUpper(r):
		return classUpper
	case unicode.IsDigit(r):
		return classDigit
	default:
		return classNonWord
	}
}

// boundaryBonus rewards a match that starts a word.
func boundaryBonus(text string, idx int) int {
	if idx == 0 {
		return bonusBoundary + bonusFirstChar
	}

	prev, _ := utf8.DecodeLastRuneInString(text[:idx])
	cur, _ := utf8.DecodeRuneInString(text[idx:])
	pc, cc := classify(prev), classify(cur)
	switch {
	case pc == classDelimiter || pc == classNonWord:
		return bonusBoundary
	case pc == classLower && cc == classUpper:
		return bonusCamel
	case pc != classDigit && cc == classDigit:
		return bonusCamel
	}

	return 0
}

// fuzzyMatch finds the needle as a subsequence. It scans forward for the
// leftmost match, then backward from its end for the tightest one, and
// also tries every place the needle occurs whole. The best scoring of
// those wins, scored with boundary and consecutive bonuses.
func fuzzyMatch(text string, hay, pat []rune, byteOff []int) (int, []int, bool, bool) {
	if len(pat) == 0 {
		return 0, nil, true, true
	}

	// Forward pass: leftmost end.
	pi := 0
	end := -1
	for i, r := range hay {
		if r == pat[pi] {
			pi++
			if pi == len(pat) {
				end = i

				break
			}
		}
	}

	if end < 0 {
		return 0, nil, false, false
	}

	// Backward pass: tightest start ending at end.
	runeIdx := make([]int, len(pat))
	pi = len(pat) - 1
	for i := end; i >= 0 && pi >= 0; i-- {
		if hay[i] == pat[pi] {
			runeIdx[pi] = i
			pi--
		}
	}

	best, bestScore := runeIdx, scoreRunes(text, runeIdx, byteOff)
	for start := 0; start+len(pat) <= len(hay); start++ {
		if !slices.Equal(hay[start:start+len(pat)], pat) {
			continue
		}

		whole := make([]int, len(pat))
		for k := range whole {
			whole[k] = start + k
		}

		// The query typed out whole beats the same letters spread over
		// word starts.
		if s := scoreRunes(text, whole, byteOff) + bonusWhole; s > bestScore {
			best, bestScore = whole, s
		}
	}

	positions := make([]int, len(best))
	for k, ri := range best {
		positions[k] = byteOff[ri]
	}

	// Shorter candidates rank higher when the match is otherwise equal.
	return bestScore - len(hay)/8, positions, isTight(text, best, byteOff), true
}

// scoreRunes scores matched rune indexes, ascending.
func scoreRunes(text string, runeIdx, byteOff []int) int {
	score := 0
	prevIdx := -2
	for k, ri := range runeIdx {
		score += scoreMatch
		switch {
		case ri == prevIdx+1:
			score += bonusConsecutive
		case k > 0:
			score += scoreGapStart + scoreGapExtension*(ri-prevIdx-2)
		}

		// boundaryBonus already rewards a match on the first char.
		score += boundaryBonus(text, byteOff[ri])

		prevIdx = ri
	}

	return score
}

// isTight reports matched rune indexes that form one run, or runs that
// each after the first start a word.
func isTight(text string, runeIdx, byteOff []int) bool {
	for k := 1; k < len(runeIdx); k++ {
		ri := runeIdx[k]
		if ri != runeIdx[k-1]+1 && boundaryBonus(text, byteOff[ri]) == 0 {
			return false
		}
	}

	return true
}
