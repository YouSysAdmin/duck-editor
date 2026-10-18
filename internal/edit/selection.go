package edit

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// Selection is one or more ranges, never empty, sorted and non overlapping
// after normalization. One range is the primary one.
type Selection struct {
	ranges  []Range
	primary int
}

// NewSelection builds a normalized selection. It panics on no ranges.
func NewSelection(ranges []Range, primary int) Selection {
	if len(ranges) == 0 {
		panic("edit: selection needs at least one range")
	}

	if primary < 0 || primary >= len(ranges) {
		panic("edit: primary index out of range")
	}

	return Selection{ranges: slices.Clone(ranges), primary: primary}.normalize()
}

// SingleSelection holds one range.
func SingleSelection(anchor, head int) Selection {
	return Selection{ranges: []Range{NewRange(anchor, head)}}
}

// PointSelection holds one cursor.
func PointSelection(pos int) Selection {
	return SingleSelection(pos, pos)
}

// Primary returns the primary range.
func (s Selection) Primary() Range {
	return s.ranges[s.primary]
}

// PrimaryIndex returns the index of the primary range.
func (s Selection) PrimaryIndex() int {
	return s.primary
}

// WithPrimaryIndex returns a copy with a different primary range.
func (s Selection) WithPrimaryIndex(idx int) Selection {
	if idx < 0 || idx >= len(s.ranges) {
		panic("edit: primary index out of range")
	}

	s.primary = idx

	return s
}

// Ranges returns the ranges. The slice must not be modified.
func (s Selection) Ranges() []Range {
	return s.ranges
}

// Len returns the number of ranges.
func (s Selection) Len() int {
	return len(s.ranges)
}

// Equal compares two selections.
func (s Selection) Equal(o Selection) bool {
	return s.primary == o.primary && slices.Equal(s.ranges, o.ranges)
}

// Push adds a range and makes it primary.
func (s Selection) Push(r Range) Selection {
	ranges := append(slices.Clone(s.ranges), r)

	return Selection{ranges: ranges, primary: len(ranges) - 1}.normalize()
}

// Remove drops the range at index. The selection must keep one range.
func (s Selection) Remove(index int) Selection {
	if len(s.ranges) <= 1 {
		panic("edit: cannot remove the last range")
	}

	ranges := slices.Delete(slices.Clone(s.ranges), index, index+1)
	primary := s.primary
	if index < primary || primary == len(ranges) {
		primary--
	}

	return Selection{ranges: ranges, primary: primary}
}

// Replace swaps the range at index.
func (s Selection) Replace(index int, r Range) Selection {
	ranges := slices.Clone(s.ranges)
	ranges[index] = r

	return Selection{ranges: ranges, primary: s.primary}.normalize()
}

// Map moves every range through a changeset and normalizes.
func (s Selection) Map(changes *ChangeSet) Selection {
	return s.MapNoNormalize(changes).normalize()
}

// MapNoNormalize moves every range through a changeset.
func (s Selection) MapNoNormalize(changes *ChangeSet) Selection {
	if changes.IsEmpty() {
		return s
	}

	ranges := slices.Clone(s.ranges)
	positions := make([]PosAssoc, 0, 2*len(ranges))
	for i := range ranges {
		r := &ranges[i]
		r.OldVisual = VisualPosition{}
		pair := r.posAssocs()
		positions = append(positions, pair[:]...)
	}

	changes.UpdatePositions(positions)

	return Selection{ranges: ranges, primary: s.primary}
}

// LineRanges returns the inclusive line spans of the ranges, merging
// spans that touch or overlap.
func (s Selection) LineRanges(text buffer.Rope) [][2]int {
	out := make([][2]int, 0, len(s.ranges))
	for _, r := range s.ranges {
		start, end := r.LineRange(text)
		if n := len(out); n > 0 && start-out[n-1][1] <= 1 {
			out[n-1][1] = end

			continue
		}

		out = append(out, [2]int{start, end})
	}

	return out
}

// Transform applies f to every range and normalizes.
func (s Selection) Transform(f func(Range) Range) Selection {
	ranges := make([]Range, len(s.ranges))
	for i, r := range s.ranges {
		ranges[i] = f(r)
	}

	return Selection{ranges: ranges, primary: s.primary}.normalize()
}

// TransformIter maps every range to zero or more ranges and normalizes.
// The result must not end up empty.
func (s Selection) TransformIter(f func(Range) []Range) Selection {
	ranges := make([]Range, 0, len(s.ranges))
	for _, r := range s.ranges {
		ranges = append(ranges, f(r)...)
	}

	if len(ranges) == 0 {
		panic("edit: transform produced an empty selection")
	}

	return Selection{ranges: ranges, primary: min(s.primary, len(ranges)-1)}.normalize()
}

// EnsureInvariants aligns ranges to graphemes, gives them a minimum width
// of one and normalizes.
func (s Selection) EnsureInvariants(text buffer.Rope) Selection {
	return s.Transform(func(r Range) Range {
		return r.MinWidth1(text).GraphemeAligned(text)
	})
}

// Fragments returns the text covered by each range.
func (s Selection) Fragments(text buffer.Rope) []string {
	out := make([]string, len(s.ranges))
	for i, r := range s.ranges {
		out[i] = r.Fragment(text)
	}

	return out
}

// Contains reports whether every range of o lies inside a range of s.
func (s Selection) Contains(o Selection) bool {
	i := 0
	for _, r := range o.ranges {
		for i < len(s.ranges) && s.ranges[i].To() < r.From() {
			i++
		}

		if i == len(s.ranges) || !s.ranges[i].ContainsRange(r) {
			return false
		}
	}

	return true
}

// MergeRanges replaces all ranges with one spanning first to last.
func (s Selection) MergeRanges() Selection {
	first := s.ranges[0]
	last := s.ranges[len(s.ranges)-1]

	return Selection{ranges: []Range{first.Merge(last)}}
}

// MergeConsecutiveRanges joins ranges that share an edge.
func (s Selection) MergeConsecutiveRanges() Selection {
	primary := s.ranges[s.primary]
	ranges := make([]Range, 0, len(s.ranges))
	for _, cur := range s.ranges {
		if n := len(ranges); n > 0 && ranges[n-1].To() == cur.From() {
			merged := cur.Merge(ranges[n-1])
			if ranges[n-1] == primary || cur == primary {
				primary = merged
			}

			ranges[n-1] = merged

			continue
		}

		ranges = append(ranges, cur)
	}

	return Selection{ranges: ranges, primary: slices.Index(ranges, primary)}
}

// normalize sorts ranges by start and merges overlapping ones while
// tracking the primary range. It works on a copy, the receiver may share
// its ranges with a selection the caller still holds.
func (s Selection) normalize() Selection {
	if len(s.ranges) < 2 {
		return s
	}

	primary := s.ranges[s.primary]
	ranges := slices.Clone(s.ranges)
	slices.SortStableFunc(ranges, func(a, b Range) int {
		return cmp.Compare(a.From(), b.From())
	})

	out := ranges[:1]
	for _, cur := range ranges[1:] {
		prev := &out[len(out)-1]
		if prev.Overlaps(cur) {
			merged := cur.Merge(*prev)
			if *prev == primary || cur == primary {
				primary = merged
			}

			*prev = merged

			continue
		}

		out = append(out, cur)
	}

	return Selection{ranges: out, primary: slices.Index(out, primary)}
}

// SplitOnNewline splits every range at line endings, dropping the endings.
func SplitOnNewline(text buffer.Rope, s Selection) Selection {
	result := make([]Range, 0, s.Len())
	for _, sel := range s.ranges {
		if sel.IsEmpty() {
			result = append(result, sel)

			continue
		}

		start := sel.From()
		end := sel.To()
		fragment := text.Slice(start, end)
		for {
			idx := strings.IndexByte(fragment, '\n')
			if idx < 0 {
				break
			}

			line := fragment[:idx+1]
			ending := 1
			if strings.HasSuffix(line, "\r\n") {
				ending = 2
			}

			lineChars := utf8.RuneCountInString(line)
			result = append(result, NewRange(start, start+lineChars-ending))
			start += lineChars
			fragment = fragment[idx+1:]
		}

		if start < end {
			result = append(result, NewRange(start, end))
		}
	}

	return NewSelection(result, 0)
}

// allMatches runs the regex over the whole text once and returns the
// matches as char ranges, so anchors and lookaround see the real
// surroundings of every range.
func allMatches(text buffer.Rope, re *regexp.Regexp) [][2]int {
	whole := text.String()
	found := re.FindAllStringIndex(whole, -1)
	out := make([][2]int, 0, len(found))
	for _, m := range found {
		out = append(out, [2]int{text.ByteToChar(m[0]), text.ByteToChar(m[1])})
	}

	return out
}

// matchesWithin returns the matches that lie inside [from, to).
func matchesWithin(matches [][2]int, from, to int) [][2]int {
	var out [][2]int
	for _, m := range matches {
		if m[0] >= from && m[1] <= to {
			out = append(out, m)
		}
	}

	return out
}

// SelectOnMatches replaces every range with the regex matches inside it.
// It returns false when nothing matched.
func SelectOnMatches(text buffer.Rope, s Selection, re *regexp.Regexp) (Selection, bool) {
	result := make([]Range, 0, s.Len())
	matches := allMatches(text, re)
	for _, sel := range s.ranges {
		for _, m := range matchesWithin(matches, sel.From(), sel.To()) {
			r := NewRange(m[0], m[1])
			// Drop empty matches right at the range end.
			if r != Point(sel.To()) {
				result = append(result, r)
			}
		}
	}

	if len(result) == 0 {
		return Selection{}, false
	}

	return NewSelection(result, 0), true
}

// SplitOnMatches splits every range at regex matches.
func SplitOnMatches(text buffer.Rope, s Selection, re *regexp.Regexp) Selection {
	result := make([]Range, 0, s.Len())
	matches := allMatches(text, re)
	for _, sel := range s.ranges {
		if sel.IsEmpty() {
			result = append(result, sel)

			continue
		}

		from := sel.From()
		end := sel.To()
		start := from
		for _, m := range matchesWithin(matches, from, end) {
			result = append(result, NewRange(start, m[0]))
			start = m[1]
		}

		if start < end {
			result = append(result, NewRange(start, end))
		}
	}

	return NewSelection(result, 0)
}

// KeepOrRemoveMatches keeps the ranges that match the regex, or with remove
// set the ones that do not. It returns false when no range is left.
func KeepOrRemoveMatches(text buffer.Rope, s Selection, re *regexp.Regexp, remove bool) (Selection, bool) {
	result := make([]Range, 0, s.Len())
	for _, sel := range s.ranges {
		if re.MatchString(sel.Fragment(text)) != remove {
			result = append(result, sel)
		}
	}

	if len(result) == 0 {
		return Selection{}, false
	}

	return NewSelection(result, 0), true
}
