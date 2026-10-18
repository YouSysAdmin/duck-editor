package ui

import (
	"strings"
	"testing"
)

func TestLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		line      string
		wantCols  []int
		wantWidth int
	}{
		{name: "ascii", line: "abc", wantCols: []int{0, 1, 2}, wantWidth: 3},
		{name: "tab at start", line: "\ta", wantCols: []int{0, 4}, wantWidth: 5},
		{name: "tab mid", line: "ab\tc", wantCols: []int{0, 1, 2, 4}, wantWidth: 5},
		{name: "wide", line: "a中b", wantCols: []int{0, 1, 3}, wantWidth: 4},
		{name: "combining", line: "éx", wantCols: []int{0, 1}, wantWidth: 2},
		{name: "control", line: "\x01a", wantCols: []int{0, 2}, wantWidth: 3},
		{name: "empty", line: "", wantCols: []int{}, wantWidth: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			glyphs := Layout(tt.line, 4)
			if len(glyphs) != len(tt.wantCols) {
				t.Fatalf("got %d glyphs, want %d", len(glyphs), len(tt.wantCols))
			}

			for i, want := range tt.wantCols {
				if glyphs[i].Col != want {
					t.Errorf("glyph %d col = %d, want %d", i, glyphs[i].Col, want)
				}
			}

			if got := LineWidth(glyphs); got != tt.wantWidth {
				t.Errorf("width = %d, want %d", got, tt.wantWidth)
			}
		})
	}
}

func TestIndexAtColumn(t *testing.T) {
	t.Parallel()

	glyphs := Layout("ab\tc", 4)

	tests := []struct {
		col  int
		want int
	}{
		{col: 0, want: 0},
		{col: 1, want: 1},
		{col: 2, want: 2},
		{col: 3, want: 2},
		{col: 4, want: 3},
		{col: 99, want: 3},
	}

	for _, tt := range tests {
		if got := IndexAtColumn(glyphs, tt.col); got != tt.want {
			t.Errorf("IndexAtColumn(%d) = %d, want %d", tt.col, got, tt.want)
		}
	}

	if got := IndexAtColumn(nil, 5); got != 0 {
		t.Errorf("empty line index = %d, want 0", got)
	}
}

// Layout lays out a line without whitespace rendering.
func Layout(line string, tabWidth int) []Glyph {
	return LayoutWith(line, LayoutOptions{TabWidth: tabWidth})
}

// IndexAtColumn returns the grapheme index covering visual column col.
// Columns past the end map to the last glyph, or zero on an empty line.
func IndexAtColumn(glyphs []Glyph, col int) int {
	if len(glyphs) == 0 {
		return 0
	}

	for i, g := range glyphs {
		if col < g.Col+g.Width {
			return i
		}
	}

	return len(glyphs) - 1
}

func TestControlCharactersEscaped(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for _, g := range Layout("a\x1b\x7f\u009b\u0085b", 4) {
		sb.WriteString(g.Text)
	}

	if got := sb.String(); got != "a^[^?<9B><85>b" {
		t.Fatalf("rendered %q", got)
	}
}

func TestInvalidAndZeroWidthEscaped(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	width := 0
	for _, g := range Layout("a\xffb\u200bc", 4) {
		sb.WriteString(g.Text)
		width += g.Width
	}

	if got := sb.String(); got != "a<FF>b<200B>c" || width != len(got) {
		t.Fatalf("rendered %q width %d", got, width)
	}
}
