package buffer

import (
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Grapheme boundary helpers. They work on the line around the position because
// grapheme clusters never span a line break except for CRLF, which stays inside one line's ending.

// PrevGraphemeBoundary returns the boundary before charIdx.
func (r Rope) PrevGraphemeBoundary(charIdx int) int {
	return r.NthPrevGraphemeBoundary(charIdx, 1)
}

// NthPrevGraphemeBoundary walks n grapheme boundaries backwards.
func (r Rope) NthPrevGraphemeBoundary(charIdx, n int) int {
	r.checkIndex(charIdx)
	for range n {
		if charIdx == 0 {
			return 0
		}

		line := r.CharToLine(charIdx)
		lineStart := r.LineToChar(line)
		if charIdx == lineStart {
			lineStart = r.LineToChar(line - 1)
		}

		charIdx = lineStart + lastBoundaryBefore(r.Slice(lineStart, charIdx))
	}

	return charIdx
}

// NextGraphemeBoundary returns the boundary after charIdx.
func (r Rope) NextGraphemeBoundary(charIdx int) int {
	return r.NthNextGraphemeBoundary(charIdx, 1)
}

// NthNextGraphemeBoundary walks n grapheme boundaries forwards.
func (r Rope) NthNextGraphemeBoundary(charIdx, n int) int {
	r.checkIndex(charIdx)
	total := r.Len()
	for range n {
		if charIdx >= total {
			return total
		}

		line := r.CharToLine(charIdx)
		lineStart := r.LineToChar(line)
		lineEnd := r.LineToChar(line + 1)
		charIdx = lineStart + firstBoundaryAfter(r.Slice(lineStart, lineEnd), charIdx-lineStart)
	}

	return charIdx
}

// EnsureGraphemeBoundaryNext returns charIdx if it is a boundary, else
// the next boundary.
func (r Rope) EnsureGraphemeBoundaryNext(charIdx int) int {
	if charIdx == 0 {
		return 0
	}

	return r.NextGraphemeBoundary(charIdx - 1)
}

// EnsureGraphemeBoundaryPrev returns charIdx if it is a boundary, else
// the previous boundary.
func (r Rope) EnsureGraphemeBoundaryPrev(charIdx int) int {
	if charIdx == r.Len() {
		return charIdx
	}

	return r.PrevGraphemeBoundary(charIdx + 1)
}

// lastBoundaryBefore returns the char offset where the last cluster of
// text starts. text must be non-empty.
func lastBoundaryBefore(text string) int {
	pos := 0
	last := 0
	state := -1
	for len(text) > 0 {
		var cluster string
		cluster, text, _, state = uniseg.FirstGraphemeClusterInString(text, state)
		last = pos
		pos += utf8.RuneCountInString(cluster)
	}

	return last
}

// firstBoundaryAfter returns the end of the cluster that contains char
// offset pos, which is the first boundary strictly after pos.
func firstBoundaryAfter(text string, pos int) int {
	end := 0
	state := -1
	for len(text) > 0 {
		var cluster string
		cluster, text, _, state = uniseg.FirstGraphemeClusterInString(text, state)
		end += utf8.RuneCountInString(cluster)
		if end > pos {
			return end
		}
	}

	return end
}
