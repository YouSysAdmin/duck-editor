package edit

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

func rangesString(sel Selection) string {
	parts := make([]string, 0, sel.Len())
	for _, r := range sel.Ranges() {
		parts = append(parts, fmt.Sprintf("%d/%d", r.Anchor, r.Head))
	}

	return strings.Join(parts, ",")
}

func TestNewSelectionPanicsOnEmpty(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	NewSelection(nil, 0)
}

func TestCreateNormalizesAndMerges(t *testing.T) {
	t.Parallel()

	sel := NewSelection([]Range{
		NewRange(10, 12), NewRange(6, 7), NewRange(4, 5), NewRange(3, 4),
		NewRange(0, 6), NewRange(7, 8), NewRange(9, 13), NewRange(13, 14),
	}, 0)
	if got := rangesString(sel); got != "0/6,6/7,7/8,9/13,13/14" {
		t.Fatalf("got %s", got)
	}

	sel = NewSelection([]Range{NewRange(0, 2), NewRange(1, 5), NewRange(4, 7)}, 2)
	if got := rangesString(sel); got != "0/7" || sel.PrimaryIndex() != 0 {
		t.Fatalf("got %s primary %d", got, sel.PrimaryIndex())
	}
}

func TestCreateMergesAdjacentPoints(t *testing.T) {
	t.Parallel()

	sel := NewSelection([]Range{
		NewRange(10, 12), NewRange(12, 12), NewRange(12, 12), NewRange(10, 10), NewRange(8, 10),
	}, 0)
	if got := rangesString(sel); got != "8/10,10/12,12/12" {
		t.Fatalf("got %s", got)
	}
}

func TestContains(t *testing.T) {
	t.Parallel()

	r := NewRange(10, 12)
	if r.Contains(9) || !r.Contains(10) || !r.Contains(11) || r.Contains(12) || r.Contains(13) {
		t.Fatal("forward contains")
	}

	r = NewRange(9, 6)
	if r.Contains(9) || !r.Contains(7) || !r.Contains(6) {
		t.Fatal("backward contains")
	}
}

func TestOverlaps(t *testing.T) {
	t.Parallel()

	overlaps := func(a, b [2]int) bool {
		return NewRange(a[0], a[1]).Overlaps(NewRange(b[0], b[1]))
	}
	no := [][2][2]int{
		{{0, 3}, {3, 6}}, {{0, 3}, {6, 3}}, {{3, 0}, {3, 6}}, {{3, 0}, {6, 3}},
		{{3, 6}, {0, 3}}, {{3, 6}, {3, 0}}, {{6, 3}, {0, 3}}, {{6, 3}, {3, 0}},
		{{0, 3}, {3, 3}}, {{3, 0}, {3, 3}}, {{3, 3}, {0, 3}}, {{3, 3}, {3, 0}},
		{{0, 0}, {1, 1}}, {{1, 1}, {0, 0}},
	}
	yes := [][2][2]int{
		{{0, 4}, {3, 6}}, {{0, 4}, {6, 3}}, {{4, 0}, {3, 6}}, {{4, 0}, {6, 3}},
		{{3, 6}, {0, 4}}, {{3, 6}, {4, 0}}, {{6, 3}, {0, 4}}, {{6, 3}, {4, 0}},
		{{1, 4}, {1, 1}}, {{4, 1}, {1, 1}}, {{1, 1}, {1, 4}}, {{1, 1}, {4, 1}},
		{{1, 4}, {3, 3}}, {{4, 1}, {3, 3}}, {{3, 3}, {1, 4}}, {{3, 3}, {4, 1}},
		{{1, 1}, {1, 1}},
	}
	for _, c := range no {
		if overlaps(c[0], c[1]) {
			t.Errorf("%v should not overlap", c)
		}
	}

	for _, c := range yes {
		if !overlaps(c[0], c[1]) {
			t.Errorf("%v should overlap", c)
		}
	}
}

func TestGraphemeAligned(t *testing.T) {
	t.Parallel()

	s := buffer.NewRope("\r\nHi\r\n")
	cases := [][2]Range{
		{NewRange(0, 0), NewRange(0, 0)}, {NewRange(1, 1), NewRange(0, 0)}, {NewRange(2, 2), NewRange(2, 2)},
		{NewRange(3, 3), NewRange(3, 3)}, {NewRange(4, 4), NewRange(4, 4)}, {NewRange(5, 5), NewRange(4, 4)},
		{NewRange(6, 6), NewRange(6, 6)},
		{NewRange(0, 1), NewRange(0, 2)}, {NewRange(1, 2), NewRange(0, 2)}, {NewRange(2, 3), NewRange(2, 3)},
		{NewRange(3, 4), NewRange(3, 4)}, {NewRange(4, 5), NewRange(4, 6)}, {NewRange(5, 6), NewRange(4, 6)},
		{NewRange(0, 2), NewRange(0, 2)}, {NewRange(1, 3), NewRange(0, 3)}, {NewRange(2, 4), NewRange(2, 4)},
		{NewRange(3, 5), NewRange(3, 6)}, {NewRange(4, 6), NewRange(4, 6)},
		{NewRange(1, 0), NewRange(2, 0)}, {NewRange(2, 1), NewRange(2, 0)}, {NewRange(3, 2), NewRange(3, 2)},
		{NewRange(4, 3), NewRange(4, 3)}, {NewRange(5, 4), NewRange(6, 4)}, {NewRange(6, 5), NewRange(6, 4)},
		{NewRange(2, 0), NewRange(2, 0)}, {NewRange(3, 1), NewRange(3, 0)}, {NewRange(4, 2), NewRange(4, 2)},
		{NewRange(5, 3), NewRange(6, 3)}, {NewRange(6, 4), NewRange(6, 4)},
	}
	for _, c := range cases {
		if got := c[0].GraphemeAligned(s); got != c[1] {
			t.Errorf("%v aligned = %v, want %v", c[0], got, c[1])
		}
	}
}

func TestMinWidth1(t *testing.T) {
	t.Parallel()

	s := buffer.NewRope("\r\nHi\r\n")
	cases := [][2]Range{
		{NewRange(0, 0), NewRange(0, 2)}, {NewRange(1, 1), NewRange(1, 2)}, {NewRange(2, 2), NewRange(2, 3)},
		{NewRange(3, 3), NewRange(3, 4)}, {NewRange(4, 4), NewRange(4, 6)}, {NewRange(5, 5), NewRange(5, 6)},
		{NewRange(6, 6), NewRange(6, 6)},
		{NewRange(0, 1), NewRange(0, 1)}, {NewRange(5, 6), NewRange(5, 6)},
		{NewRange(1, 0), NewRange(1, 0)}, {NewRange(6, 5), NewRange(6, 5)},
	}
	for _, c := range cases {
		if got := c[0].MinWidth1(s); got != c[1] {
			t.Errorf("%v min width = %v, want %v", c[0], got, c[1])
		}
	}
}

func TestSelectOnMatches(t *testing.T) {
	t.Parallel()

	r := buffer.NewRope("Nobody expects the Spanish inquisition")
	got, ok := SelectOnMatches(r, SingleSelection(0, r.Len()), regexp.MustCompile(`[A-Z][a-z]*`))
	if !ok || !got.Equal(NewSelection([]Range{NewRange(0, 6), NewRange(19, 26)}, 0)) {
		t.Fatalf("got %v %v", got.Ranges(), ok)
	}

	r = buffer.NewRope("This\nString\n\ncontains multiple\nlines")
	startOfLine := regexp.MustCompile(`(?m)^`)
	endOfLine := regexp.MustCompile(`(?m)$`)

	check := func(sel Selection, re *regexp.Regexp, want Selection, wantOk bool) {
		t.Helper()

		got, ok := SelectOnMatches(r, sel, re)
		if ok != wantOk || (ok && !got.Equal(want)) {
			t.Errorf("select %v with %s = %v,%v want %v,%v", sel.Ranges(), re, got.Ranges(), ok, want.Ranges(), wantOk)
		}
	}
	check(SingleSelection(0, 4), startOfLine, SingleSelection(0, 0), true)
	check(SingleSelection(0, 4), endOfLine, Selection{}, false)
	check(SingleSelection(0, 5), startOfLine, SingleSelection(0, 0), true)
	check(SingleSelection(0, 5), endOfLine, SingleSelection(4, 4), true)
	check(SingleSelection(0, 6), startOfLine, NewSelection([]Range{Point(0), Point(5)}, 0), true)
	check(SingleSelection(0, 6), endOfLine, SingleSelection(4, 4), true)
	check(SingleSelection(0, r.Len()), regexp.MustCompile(`(?m)^[a-z ]*$`),
		NewSelection([]Range{Point(12), NewRange(13, 30), NewRange(31, 36)}, 0), true)
}

func TestLineRange(t *testing.T) {
	t.Parallel()

	s := buffer.NewRope("\r\nHi\r\nthere!")
	cases := []struct {
		r    Range
		want [2]int
	}{
		{NewRange(0, 0), [2]int{0, 0}}, {NewRange(1, 1), [2]int{0, 0}}, {NewRange(2, 2), [2]int{1, 1}}, {NewRange(3, 3), [2]int{1, 1}},
		{NewRange(0, 1), [2]int{0, 0}}, {NewRange(0, 2), [2]int{0, 0}}, {NewRange(0, 3), [2]int{0, 1}}, {NewRange(1, 2), [2]int{0, 0}},
		{NewRange(2, 3), [2]int{1, 1}}, {NewRange(3, 8), [2]int{1, 2}}, {NewRange(0, 12), [2]int{0, 2}},
		{NewRange(1, 0), [2]int{0, 0}}, {NewRange(2, 0), [2]int{0, 0}}, {NewRange(3, 0), [2]int{0, 1}}, {NewRange(2, 1), [2]int{0, 0}},
		{NewRange(3, 2), [2]int{1, 1}}, {NewRange(8, 3), [2]int{1, 2}}, {NewRange(12, 0), [2]int{0, 2}},
	}
	for _, c := range cases {
		a, b := c.r.LineRange(s)
		if [2]int{a, b} != c.want {
			t.Errorf("%v line range = %d,%d want %v", c.r, a, b, c.want)
		}
	}
}

func TestSelectionLineRanges(t *testing.T) {
	t.Parallel()

	text, sel := MustParseAnnotated(`                                           L0
            #[|these]# line #(|ranges)# are #(|merged)#   L1
                                                          L2
            single one-line #(|range)#                    L3
                                                          L4
            single #(|multiline                           L5
            range)#                                       L6
                                                          L7
            these #(|multiline                            L8
            ranges)# are #(|also                          L9
            merged)#                                      L10
                                                          L11
            adjacent #(|ranges)#                          L12
            are merged #(|the same way)#                  L13
            `)
	want := [][2]int{{1, 1}, {3, 3}, {5, 6}, {8, 10}, {12, 13}}
	if got := sel.LineRanges(buffer.NewRope(text)); !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCursor(t *testing.T) {
	t.Parallel()

	s := buffer.NewRope("\r\nHi\r\nthere!")
	cases := []struct {
		r    Range
		want int
	}{
		{NewRange(0, 0), 0}, {NewRange(2, 2), 2}, {NewRange(3, 3), 3},
		{NewRange(0, 2), 0}, {NewRange(0, 3), 2}, {NewRange(3, 6), 4},
		{NewRange(2, 0), 0}, {NewRange(6, 2), 2}, {NewRange(6, 3), 3},
	}
	for _, c := range cases {
		if got := c.r.Cursor(s); got != c.want {
			t.Errorf("%v cursor = %d want %d", c.r, got, c.want)
		}
	}
}

func TestPutCursor(t *testing.T) {
	t.Parallel()

	s := buffer.NewRope("\r\nHi\r\nthere!")
	cases := []struct {
		r    Range
		pos  int
		want Range
	}{
		{NewRange(0, 0), 0, NewRange(0, 2)}, {NewRange(0, 0), 2, NewRange(0, 3)}, {NewRange(2, 3), 4, NewRange(2, 6)},
		{NewRange(2, 8), 4, NewRange(2, 6)}, {NewRange(8, 8), 4, NewRange(9, 4)},
		{NewRange(3, 6), 0, NewRange(4, 0)}, {NewRange(3, 6), 2, NewRange(4, 2)}, {NewRange(3, 6), 3, NewRange(3, 4)},
		{NewRange(3, 6), 4, NewRange(3, 6)}, {NewRange(3, 6), 6, NewRange(3, 7)}, {NewRange(3, 6), 8, NewRange(3, 9)},
		{NewRange(6, 3), 0, NewRange(6, 0)}, {NewRange(6, 3), 2, NewRange(6, 2)}, {NewRange(6, 3), 3, NewRange(6, 3)},
		{NewRange(6, 3), 4, NewRange(6, 4)}, {NewRange(6, 3), 6, NewRange(4, 7)}, {NewRange(6, 3), 8, NewRange(4, 9)},
	}
	for _, c := range cases {
		if got := c.r.PutCursor(s, c.pos, true); got != c.want {
			t.Errorf("%v put %d = %v want %v", c.r, c.pos, got, c.want)
		}
	}
}

func TestSplitOnMatches(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope(" abcd efg wrs   xyz 123 456")
	sel := NewSelection([]Range{NewRange(0, 9), NewRange(11, 20)}, 0)
	result := SplitOnMatches(text, sel, regexp.MustCompile(`\s+`))
	want := []Range{NewRange(0, 0), NewRange(1, 5), NewRange(6, 9), NewRange(11, 13), NewRange(16, 19)}
	if !slices.Equal(result.Ranges(), want) {
		t.Fatalf("got %v", result.Ranges())
	}

	if got := result.Fragments(text); !slices.Equal(got, []string{"", "abcd", "efg", "rs", "xyz"}) {
		t.Fatalf("fragments %q", got)
	}
}

func TestSplitOnNewline(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("ab\r\ncd\nef")
	result := SplitOnNewline(text, SingleSelection(0, text.Len()))
	want := []Range{NewRange(0, 2), NewRange(4, 6), NewRange(7, 9)}
	if !slices.Equal(result.Ranges(), want) {
		t.Fatalf("got %v", result.Ranges())
	}
}

func TestMergeConsecutiveRanges(t *testing.T) {
	t.Parallel()

	sel := NewSelection([]Range{NewRange(0, 1), NewRange(1, 10), NewRange(15, 20), NewRange(25, 26), NewRange(26, 30)}, 4)
	result := sel.MergeConsecutiveRanges()
	if !slices.Equal(result.Ranges(), []Range{NewRange(0, 10), NewRange(15, 20), NewRange(25, 30)}) || result.PrimaryIndex() != 2 {
		t.Fatalf("got %v primary %d", result.Ranges(), result.PrimaryIndex())
	}

	result = NewSelection([]Range{NewRange(0, 1)}, 0).MergeConsecutiveRanges()
	if !slices.Equal(result.Ranges(), []Range{NewRange(0, 1)}) || result.PrimaryIndex() != 0 {
		t.Fatal("single range")
	}

	sel = NewSelection([]Range{
		NewRange(0, 1), NewRange(1, 5), NewRange(5, 8), NewRange(8, 10), NewRange(10, 15), NewRange(18, 25),
	}, 3)
	result = sel.MergeConsecutiveRanges()
	if !slices.Equal(result.Ranges(), []Range{NewRange(0, 15), NewRange(18, 25)}) || result.PrimaryIndex() != 0 {
		t.Fatalf("got %v primary %d", result.Ranges(), result.PrimaryIndex())
	}
}

func TestSelectionContains(t *testing.T) {
	t.Parallel()

	contains := func(a, b [][2]int) bool {
		toSel := func(v [][2]int) Selection {
			ranges := make([]Range, len(v))
			for i, r := range v {
				ranges[i] = NewRange(r[0], r[1])
			}

			return NewSelection(ranges, 0)
		}

		return toSel(a).Contains(toSel(b))
	}

	if !contains([][2]int{{1, 1}}, [][2]int{{1, 1}}) {
		t.Error("exact")
	}

	if !contains([][2]int{{1, 1}, {2, 2}, {3, 3}}, [][2]int{{2, 2}}) {
		t.Error("larger contains smaller")
	}

	if !contains([][2]int{{1, 1}, {2, 2}}, [][2]int{{1, 1}, {2, 2}}) {
		t.Error("multiple")
	}

	if contains([][2]int{{1, 1}}, [][2]int{{1, 1}, {2, 2}}) {
		t.Error("smaller cannot contain bigger")
	}

	if !contains([][2]int{{1, 1}, {2, 4}, {5, 6}, {7, 9}, {10, 13}}, [][2]int{{3, 4}, {7, 9}}) {
		t.Error("subset ranges")
	}

	if contains([][2]int{{1, 1}, {5, 6}}, [][2]int{{1, 6}}) {
		t.Error("spanning gap")
	}

	if !contains([][2]int{{1, 4}, {7, 10}}, [][2]int{{1, 2}, {3, 4}, {7, 9}}) {
		t.Error("many inside few")
	}
}

func TestPushRemoveReplace(t *testing.T) {
	t.Parallel()

	sel := SingleSelection(0, 1).Push(NewRange(5, 6)).Push(NewRange(2, 3))
	if rangesString(sel) != "0/1,2/3,5/6" || sel.PrimaryIndex() != 1 {
		t.Fatalf("push: %s primary %d", rangesString(sel), sel.PrimaryIndex())
	}

	sel = sel.Remove(0)
	if rangesString(sel) != "2/3,5/6" || sel.PrimaryIndex() != 0 {
		t.Fatalf("remove: %s primary %d", rangesString(sel), sel.PrimaryIndex())
	}

	sel = sel.Replace(1, NewRange(3, 4))
	if rangesString(sel) != "2/3,3/4" {
		t.Fatalf("replace: %s", rangesString(sel))
	}
}

func TestRegexSeesSurroundings(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("foo bar\nbaz qux\n")
	// The selection covers "bar\nbaz": ^ matches only at the real line
	// start inside it, not at the selection start.
	sel := SingleSelection(4, 11)
	got, ok := SelectOnMatches(text, sel, regexp.MustCompile(`(?m)^\w+`))
	if !ok || got.Len() != 1 || got.Primary().From() != 8 || got.Primary().To() != 11 {
		t.Fatalf("anchored select = %v ok %v", got.Ranges(), ok)
	}

	// $ matches the end of the first line inside the range.
	got, ok = SelectOnMatches(text, sel, regexp.MustCompile(`(?m)r$`))
	if !ok || got.Primary().From() != 6 {
		t.Fatalf("end anchor = %v ok %v", got.Ranges(), ok)
	}

	split := SplitOnMatches(text, SingleSelection(0, 15), regexp.MustCompile(`\s+`))
	if split.Len() != 4 || split.Ranges()[1].Fragment(text) != "bar" {
		t.Fatalf("split = %v", split.Fragments(text))
	}
}
