package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

func TestTextObjectWord(t *testing.T) {
	t.Parallel()

	type c struct {
		pos  int
		kind TextObjectKind
		want [2]int
	}
	tests := []struct {
		text  string
		cases []c
	}{
		{"cursor at beginning of doc", []c{{0, Inside, [2]int{0, 6}}, {0, Around, [2]int{0, 7}}}},
		{"cursor at middle of word", []c{
			{13, Inside, [2]int{10, 16}}, {10, Inside, [2]int{10, 16}}, {15, Inside, [2]int{10, 16}},
			{13, Around, [2]int{10, 17}}, {10, Around, [2]int{10, 17}}, {15, Around, [2]int{10, 17}},
		}},
		{"cursor between word whitespace", []c{{6, Inside, [2]int{6, 6}}, {6, Around, [2]int{6, 6}}}},
		{"cursor on word before newline\n", []c{
			{22, Inside, [2]int{22, 29}}, {28, Inside, [2]int{22, 29}}, {25, Inside, [2]int{22, 29}},
			{22, Around, [2]int{21, 29}}, {28, Around, [2]int{21, 29}}, {25, Around, [2]int{21, 29}},
		}},
		{"cursor on newline\nnext line", []c{{17, Inside, [2]int{17, 17}}, {17, Around, [2]int{17, 17}}}},
		{"cursor on word after newline\nnext line", []c{
			{29, Inside, [2]int{29, 33}}, {30, Inside, [2]int{29, 33}}, {32, Inside, [2]int{29, 33}},
			{29, Around, [2]int{29, 34}}, {30, Around, [2]int{29, 34}}, {32, Around, [2]int{29, 34}},
		}},
		{"cursor on #$%:;* punctuation", []c{
			{13, Inside, [2]int{10, 16}}, {10, Inside, [2]int{10, 16}}, {15, Inside, [2]int{10, 16}},
			{13, Around, [2]int{10, 17}}, {10, Around, [2]int{10, 17}}, {15, Around, [2]int{10, 17}},
		}},
		{"cursor on punc%^#$:;.tuation", []c{
			{14, Inside, [2]int{14, 21}}, {20, Inside, [2]int{14, 21}}, {17, Inside, [2]int{14, 21}},
			{14, Around, [2]int{14, 21}}, {20, Around, [2]int{14, 21}}, {17, Around, [2]int{14, 21}},
		}},
		{"cursor in   extra whitespace", []c{
			{9, Inside, [2]int{9, 9}}, {10, Inside, [2]int{10, 10}}, {11, Inside, [2]int{11, 11}},
			{9, Around, [2]int{9, 9}}, {10, Around, [2]int{10, 10}}, {11, Around, [2]int{11, 11}},
		}},
		{"cursor on word   with extra whitespace", []c{{11, Inside, [2]int{10, 14}}, {11, Around, [2]int{10, 17}}}},
		{"cursor at end with extra   whitespace", []c{{28, Inside, [2]int{27, 37}}, {28, Around, [2]int{24, 37}}}},
		{"cursor at end of doc", []c{{19, Inside, [2]int{17, 20}}, {19, Around, [2]int{16, 20}}}},
	}

	for _, tt := range tests {
		text := buffer.NewRope(tt.text)
		for _, cs := range tt.cases {
			got := TextObjectWord(text, NewRange(cs.pos, cs.pos+1), cs.kind, false)
			if got.Anchor != cs.want[0] || got.Head != cs.want[1] {
				t.Errorf("%q pos %d %v = %d/%d, want %v", tt.text, cs.pos, cs.kind, got.Anchor, got.Head, cs.want)
			}
		}
	}
}

func checkParagraph(t *testing.T, kind TextObjectKind, count int, cases [][2]string) {
	t.Helper()

	for _, c := range cases {
		text, sel := MustParseAnnotated(c[0])
		rope := buffer.NewRope(text)
		got := sel.Transform(func(r Range) Range {
			return TextObjectParagraph(rope, r, kind, count)
		})
		if actual := Annotate(rope, got); actual != c[1] {
			t.Errorf("paragraph %v x%d on %q = %q, want %q", kind, count, c[0], actual, c[1])
		}
	}
}

func TestTextObjectParagraph(t *testing.T) {
	t.Parallel()

	checkParagraph(t, Inside, 1, [][2]string{
		{"#[|]#", "#[|]#"},
		{"firs#[t|]#\n\nparagraph\n\n", "#[first\n|]#\nparagraph\n\n"},
		{"second\n\npa#[r|]#agraph\n\n", "second\n\n#[paragraph\n|]#\n"},
		{"#[f|]#irst char\n\n", "#[first char\n|]#\n"},
		{"last char\n#[\n|]#", "#[last char\n|]#\n"},
		{"empty to line\n#[\n|]#paragraph boundary\n\n", "empty to line\n\n#[paragraph boundary\n|]#\n"},
		{"line to empty\n\n#[p|]#aragraph boundary\n\n", "line to empty\n\n#[paragraph boundary\n|]#\n"},
	})
	checkParagraph(t, Inside, 2, [][2]string{
		{"last two\n\n#[p|]#aragraph\n\nwithout whitespaces\n\n", "last two\n\n#[paragraph\n\nwithout whitespaces\n|]#\n"},
		{"last two\n#[\n|]#paragraph\n\nwithout whitespaces\n\n", "last two\n\n#[paragraph\n\nwithout whitespaces\n|]#\n"},
	})
	checkParagraph(t, Around, 1, [][2]string{
		{"#[|]#", "#[|]#"},
		{"firs#[t|]#\n\nparagraph\n\n", "#[first\n\n|]#paragraph\n\n"},
		{"second\n\npa#[r|]#agraph\n\n", "second\n\n#[paragraph\n\n|]#"},
		{"#[f|]#irst char\n\n", "#[first char\n\n|]#"},
		{"last char\n#[\n|]#", "#[last char\n\n|]#"},
		{"empty to line\n#[\n|]#paragraph boundary\n\n", "empty to line\n\n#[paragraph boundary\n\n|]#"},
		{"line to empty\n\n#[p|]#aragraph boundary\n\n", "line to empty\n\n#[paragraph boundary\n\n|]#"},
	})
}

func TestTextObjectSurround(t *testing.T) {
	t.Parallel()

	type c struct {
		pos   int
		kind  TextObjectKind
		want  [2]int
		ch    rune
		count int
	}
	tests := []struct {
		text  string
		cases []c
	}{
		{"simple (single) surround pairs", []c{
			{3, Inside, [2]int{3, 3}, '(', 1}, {7, Inside, [2]int{8, 14}, ')', 1}, {10, Inside, [2]int{8, 14}, '(', 1}, {14, Inside, [2]int{8, 14}, ')', 1},
			{3, Around, [2]int{3, 3}, '(', 1}, {7, Around, [2]int{7, 15}, ')', 1}, {10, Around, [2]int{7, 15}, '(', 1}, {14, Around, [2]int{7, 15}, ')', 1},
		}},
		{"samexx 'single' surround pairs", []c{
			{3, Inside, [2]int{3, 3}, '\'', 1}, {7, Inside, [2]int{7, 7}, '\'', 1}, {10, Inside, [2]int{8, 14}, '\'', 1}, {14, Inside, [2]int{14, 14}, '\'', 1},
			{3, Around, [2]int{3, 3}, '\'', 1}, {7, Around, [2]int{7, 7}, '\'', 1}, {10, Around, [2]int{7, 15}, '\'', 1}, {14, Around, [2]int{14, 14}, '\'', 1},
		}},
		{"(nested (surround (pairs)) 3 levels)", []c{
			{0, Inside, [2]int{1, 35}, '(', 1}, {6, Inside, [2]int{1, 35}, ')', 1}, {8, Inside, [2]int{9, 25}, '(', 1}, {8, Inside, [2]int{9, 35}, ')', 2},
			{20, Inside, [2]int{9, 25}, '(', 2}, {20, Inside, [2]int{1, 35}, ')', 3},
			{0, Around, [2]int{0, 36}, '(', 1}, {6, Around, [2]int{0, 36}, ')', 1}, {8, Around, [2]int{8, 26}, '(', 1}, {8, Around, [2]int{8, 36}, ')', 2},
			{20, Around, [2]int{8, 26}, '(', 2}, {20, Around, [2]int{0, 36}, ')', 3},
		}},
		{"(mixed {surround [pair] same} line)", []c{
			{2, Inside, [2]int{1, 34}, '(', 1}, {9, Inside, [2]int{8, 28}, '{', 1}, {18, Inside, [2]int{18, 22}, '[', 1},
			{2, Around, [2]int{0, 35}, '(', 1}, {9, Around, [2]int{7, 29}, '{', 1}, {18, Around, [2]int{17, 23}, '[', 1},
		}},
		{"(stepped (surround) pairs (should) skip)", []c{{22, Inside, [2]int{1, 39}, '(', 1}, {22, Around, [2]int{0, 40}, '(', 1}}},
		{"[surround pairs{\non different]\nlines}", []c{
			{7, Inside, [2]int{1, 29}, '[', 1}, {15, Inside, [2]int{16, 36}, '{', 1},
			{7, Around, [2]int{0, 30}, '[', 1}, {15, Around, [2]int{15, 37}, '{', 1},
		}},
	}
	for _, tt := range tests {
		text := buffer.NewRope(tt.text)
		for _, cs := range tt.cases {
			got := TextObjectPairSurround(nil, text, Point(cs.pos), cs.kind, cs.ch, cs.count)
			if got.Anchor != cs.want[0] || got.Head != cs.want[1] {
				t.Errorf("%q pos %d %v %q x%d = %d/%d, want %v", tt.text, cs.pos, cs.kind, cs.ch, cs.count, got.Anchor, got.Head, cs.want)
			}
		}
	}
}

// specSelection builds a selection and expectations from a spec where ^ is
// a cursor and _ an expected index.
func specSelection(t *testing.T, text, spec string) (buffer.Rope, Selection, []int) {
	t.Helper()

	if len(text) != len(spec) {
		t.Fatalf("spec length mismatch")
	}

	ranges := []Range{}
	expect := []int{}
	for i, ch := range spec {
		switch ch {
		case '^':
			ranges = append(ranges, Point(i))
		case '_':
			expect = append(expect, i)
		}
	}

	return buffer.NewRope(text), NewSelection(ranges, 0), expect
}

func TestGetSurroundPos(t *testing.T) {
	t.Parallel()

	text, sel, want := specSelection(t, "(some) (chars)\n(newline)", "_ ^  _ _ ^   _\n_    ^  _")
	got, err := GetSurroundPos(nil, text, sel, '(', 1)
	if err != nil || strings.Trim(strings.Join(strings.Fields(fmtInts(got)), ","), "") != fmtInts(want) {
		t.Fatalf("got %v err %v want %v", got, err, want)
	}

	text, sel, _ = specSelection(t, "[some]\n(chars)xx\n(newline)", "  ^   \n  ^      \n         ")
	if _, err := GetSurroundPos(nil, text, sel, '(', 1); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("different chars: %v", err)
	}

	text, sel, _ = specSelection(t, "[some]\n(chars)xx\n(newline)", "      \n       ^ \n      ^  ")
	if _, err := GetSurroundPos(nil, text, sel, '(', 1); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("overlapping chars: %v", err)
	}

	text, sel, _ = specSelection(t, "[some]\n(chars)xx\n(newline)", "  ^^  \n         \n         ")
	if _, err := GetSurroundPos(nil, text, sel, '[', 1); !errors.Is(err, ErrCursorOverlap) {
		t.Fatalf("cursor overlap: %v", err)
	}
}

func TestFindNthPairsQuotes(t *testing.T) {
	t.Parallel()

	text, sel, want := specSelection(t, "some 'quoted text' on this 'line'\n'and this one'", "     _        ^  _               \n              ")
	a, h, err := FindNthPairsPos(nil, text, '\'', sel.Primary(), 1)
	if err != nil || a != want[0] || h != want[1] {
		t.Fatalf("quotes: %d %d %v", a, h, err)
	}

	text, sel, want = specSelection(t, "some 'nested 'quoted' text' on this 'line'\n'and this one'", "     _           ^        _               \n              ")
	a, h, err = FindNthPairsPos(nil, text, '\'', sel.Primary(), 2)
	if err != nil || a != want[0] || h != want[1] {
		t.Fatalf("nested quotes: %d %d %v", a, h, err)
	}

	text, sel, _ = specSelection(t, "some 'nested 'quoted' text' on this 'line'\n'and this one'", "                    ^                     \n              ")
	if _, _, err := FindNthPairsPos(nil, text, '\'', sel.Primary(), 1); !errors.Is(err, ErrCursorOnAmbiguousPair) {
		t.Fatalf("ambiguous: %v", err)
	}

	text, sel, _ = specSelection(t, "(a)c)", "^^^^^")
	if _, _, err := FindNthClosestPairsPos(nil, text, sel.Primary(), 1); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("closest: %v", err)
	}
}

func TestFindMatchingBracketPlaintext(t *testing.T) {
	t.Parallel()

	if _, ok := FindMatchingBracketPlaintext(buffer.NewRope(""), 0); ok {
		t.Fatal("empty")
	}

	check := func(input string, pos, want int) {
		t.Helper()
		text := buffer.NewRope(input)
		got, ok := FindMatchingBracketPlaintext(text, pos)
		if !ok || got != want {
			t.Errorf("%q at %d = %d,%v want %d", input, pos, got, ok, want)
		}

		back, ok := FindMatchingBracketPlaintext(text, want)
		if !ok || back != pos {
			t.Errorf("%q symmetric at %d = %d,%v want %d", input, want, back, ok, pos)
		}
	}
	check("(hello)", 0, 6)
	check("((hello))", 0, 8)
	check("((hello))", 1, 7)
	check("(((hello)))", 2, 8)
	check("key: ${value}", 6, 12)
	check("key: ${value} # (some comment)", 16, 29)
	check("(paren (paren {bracket}))", 0, 24)
	check("(paren (paren {bracket}))", 7, 23)
	check("(paren (paren {bracket}))", 14, 22)
	check("(prev line\n ) (middle) ( \n next line)", 0, 12)
	check("(prev line\n ) (middle) ( \n next line)", 14, 21)
	check("(prev line\n ) (middle) ( \n next line)", 23, 36)
}

func fmtInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strings.TrimSpace(strings.Repeat(" ", 0) + itoa(n))
	}

	return strings.Join(parts, ",")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}

	return s
}
