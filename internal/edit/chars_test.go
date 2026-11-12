package edit

import "testing"

func TestIndentWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want int
	}{
		{"", 0},
		{"x", 0},
		{"    x", 4},
		{"\tx", 4},
		{"\t\tx", 8},
		{"  \tx", 4},
		{" \t x", 5},
		{"\t  ", 6},
	}
	for _, tt := range tests {
		if got := IndentWidth(tt.line, 4); got != tt.want {
			t.Errorf("%q: got %d want %d", tt.line, got, tt.want)
		}
	}
}
