// Package edit holds the selection and change model: ranges, selections,
// changesets, transactions and undo history. Positions are char indexes
// into a buffer.Rope.
package edit

import (
	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// Direction of a range or motion.
type Direction int

// Directions.
const (
	Forward Direction = iota
	Backward
)

// VisualPosition remembers where a cursor was on screen before a vertical
// move so that the column can be restored. Valid is false when unknown.
type VisualPosition struct {
	Row   int
	Col   int
	Valid bool
}

// Range is a single selection range between an anchor and a head using gap
// indexing. The head moves when extending, the anchor stays. Ranges are
// inclusive on the left and exclusive on the right regardless of
// direction. A user facing range has a block cursor on the head side that
// covers one grapheme inward from the edge.
type Range struct {
	Anchor    int
	Head      int
	OldVisual VisualPosition
}

// NewRange creates a range.
func NewRange(anchor, head int) Range {
	return Range{Anchor: anchor, Head: head}
}

// Point creates a zero width range.
func Point(head int) Range {
	return NewRange(head, head)
}

// From is the smaller end.
func (r Range) From() int {
	return min(r.Anchor, r.Head)
}

// To is the larger end.
func (r Range) To() int {
	return max(r.Anchor, r.Head)
}

// Len is the width in chars.
func (r Range) Len() int {
	return r.To() - r.From()
}

// IsEmpty reports a zero width range.
func (r Range) IsEmpty() bool {
	return r.Anchor == r.Head
}

// Direction is Backward when the head is before the anchor.
func (r Range) Direction() Direction {
	if r.Head < r.Anchor {
		return Backward
	}

	return Forward
}

// Flip swaps anchor and head.
func (r Range) Flip() Range {
	return Range{Anchor: r.Head, Head: r.Anchor, OldVisual: r.OldVisual}
}

// WithDirection flips the range when needed so it points in d.
func (r Range) WithDirection(d Direction) Range {
	if r.Direction() == d {
		return r
	}

	return r.Flip()
}

// LineRange returns the inclusive range of lines the range touches.
func (r Range) LineRange(text buffer.Rope) (int, int) {
	from := r.From()
	to := r.To()
	if !r.IsEmpty() {
		to = max(text.PrevGraphemeBoundary(to), from)
	}

	return text.CharToLine(from), text.CharToLine(to)
}

// Overlaps reports whether two ranges share text or a zero width range
// sits on the other's left edge.
func (r Range) Overlaps(o Range) bool {
	return r.From() == o.From() || (r.To() > o.From() && o.To() > r.From())
}

// ContainsRange reports whether o lies inside r.
func (r Range) ContainsRange(o Range) bool {
	return r.From() <= o.From() && r.To() >= o.To()
}

// Contains reports whether pos lies inside the range.
func (r Range) Contains(pos int) bool {
	return r.From() <= pos && pos < r.To()
}

// Map moves the range through a changeset so it covers the same text
// after the changes are applied.
func (r Range) Map(changes *ChangeSet) Range {
	if changes.IsEmpty() {
		return r
	}

	positions := r.posAssocs()
	changes.UpdatePositions(positions[:])
	r.OldVisual = VisualPosition{}

	return r
}

// posAssocs pairs the ends of the range, start first, with associations
// that keep the range around the same text: the start sticks after an
// insert at it, the end before one.
func (r *Range) posAssocs() [2]PosAssoc {
	switch {
	case r.Anchor == r.Head:
		return [2]PosAssoc{{&r.Anchor, AfterSticky}, {&r.Head, AfterSticky}}
	case r.Anchor < r.Head:
		return [2]PosAssoc{{&r.Anchor, AfterSticky}, {&r.Head, BeforeSticky}}
	default:
		return [2]PosAssoc{{&r.Head, AfterSticky}, {&r.Anchor, BeforeSticky}}
	}
}

// Extend grows the range to cover at least from..to keeping its direction.
func (r Range) Extend(from, to int) Range {
	if r.Anchor <= r.Head {
		return Range{Anchor: min(r.Anchor, from), Head: max(r.Head, to)}
	}

	return Range{Anchor: max(r.Anchor, to), Head: min(r.Head, from)}
}

// Merge returns a range covering both inputs, keeping a backward direction
// only when both inputs point backwards.
func (r Range) Merge(o Range) Range {
	if r.Anchor > r.Head && o.Anchor > o.Head {
		return Range{Anchor: max(r.Anchor, o.Anchor), Head: min(r.Head, o.Head)}
	}

	return Range{Anchor: min(r.From(), o.From()), Head: max(r.To(), o.To())}
}

// Fragment returns the covered text.
func (r Range) Fragment(text buffer.Rope) string {
	return text.Slice(r.From(), r.To())
}

// GraphemeAligned shifts the ends outward onto grapheme boundaries. Zero
// width ranges stay zero width.
func (r Range) GraphemeAligned(text buffer.Rope) Range {
	var anchor, head int
	switch {
	case r.Anchor == r.Head:
		anchor = text.EnsureGraphemeBoundaryPrev(r.Anchor)
		head = anchor
	case r.Anchor < r.Head:
		anchor = text.EnsureGraphemeBoundaryPrev(r.Anchor)
		head = text.EnsureGraphemeBoundaryNext(r.Head)
	default:
		anchor = text.EnsureGraphemeBoundaryNext(r.Anchor)
		head = text.EnsureGraphemeBoundaryPrev(r.Head)
	}

	out := Range{Anchor: anchor, Head: head}
	if anchor == r.Anchor {
		out.OldVisual = r.OldVisual
	}

	return out
}

// MinWidth1 widens a zero width range forward by one grapheme. It only
// fails at the end of the text.
func (r Range) MinWidth1(text buffer.Rope) Range {
	if r.Anchor != r.Head {
		return r
	}

	return Range{Anchor: r.Anchor, Head: text.NextGraphemeBoundary(r.Head), OldVisual: r.OldVisual}
}

// Cursor is the left side of the block cursor.
func (r Range) Cursor(text buffer.Rope) int {
	if r.Head > r.Anchor {
		return text.PrevGraphemeBoundary(r.Head)
	}

	return r.Head
}

// PutCursor places the block cursor at charIdx, optionally extending from
// the anchor with one width semantics on both ends.
func (r Range) PutCursor(text buffer.Rope, charIdx int, extend bool) Range {
	if !extend {
		return Point(charIdx)
	}

	anchor := r.Anchor
	switch {
	case r.Head >= r.Anchor && charIdx < r.Anchor:
		anchor = text.NextGraphemeBoundary(r.Anchor)
	case r.Head < r.Anchor && charIdx >= r.Anchor:
		anchor = text.PrevGraphemeBoundary(r.Anchor)
	}

	if anchor <= charIdx {
		return NewRange(anchor, text.NextGraphemeBoundary(charIdx))
	}

	return NewRange(anchor, charIdx)
}

// CursorLine is the line the block cursor sits on.
func (r Range) CursorLine(text buffer.Rope) int {
	return text.CharToLine(r.Cursor(text))
}

// IsSingleGrapheme reports whether the range covers exactly one cluster.
func (r Range) IsSingleGrapheme(text buffer.Rope) bool {
	if r.IsEmpty() {
		return false
	}

	return text.NextGraphemeBoundary(r.From()) == r.To()
}
