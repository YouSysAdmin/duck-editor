package vcs

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func lines(s string) []string {
	if s == "" {
		return nil
	}

	return strings.Split(s, "\n")
}

func TestDiff(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b string
		want []Hunk
	}{
		{"a\nb\nc", "a\nb\nc", nil},
		{"a\nb\nc", "a\nx\nc", []Hunk{{1, 2, 1, 2}}},
		{"a\nc", "a\nb\nc", []Hunk{{1, 1, 1, 2}}},
		{"a\nb\nc", "a\nc", []Hunk{{1, 2, 1, 1}}},
		{"a\nb\nc\nd", "a\nx\ny\nd", []Hunk{{1, 3, 1, 3}}},
		{"", "a\nb", []Hunk{{0, 0, 0, 2}}},
		{"a\nb", "", []Hunk{{0, 2, 0, 0}}},
		{"a\nb\nc\nd\ne", "a\nx\nc\nd\ny\ne", []Hunk{{1, 2, 1, 2}, {4, 4, 4, 5}}},
	}
	for _, c := range cases {
		got := Diff(lines(c.a), lines(c.b))
		if !slices.Equal(got, c.want) {
			t.Errorf("Diff(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestDiffApplies(t *testing.T) {
	t.Parallel()

	a := lines("one\ntwo\nthree\nfour\nfive\nsix\nseven")
	b := lines("zero\none\nthree\nfour\n4.5\nfive\nseven\neight")
	hunks := Diff(a, b)
	// Rebuild b from a and the hunks.
	var out []string
	ai := 0
	for _, h := range hunks {
		out = append(out, a[ai:h.AStart]...)
		out = append(out, b[h.BStart:h.BEnd]...)
		ai = h.AEnd
	}

	out = append(out, a[ai:]...)
	if !slices.Equal(out, b) {
		t.Fatalf("hunks %v rebuild %v", hunks, out)
	}
}

func TestDiffRebuildsTarget(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	words := []string{"a", "b", "c", "d"}
	gen := func() []string {
		out := make([]string, rng.IntN(30))
		for i := range out {
			out[i] = words[rng.IntN(len(words))]
		}

		return out
	}
	for range 2000 {
		a, b := gen(), gen()
		var got []string
		prev := 0
		for _, h := range Diff(a, b) {
			got = append(got, a[prev:h.AStart]...)
			got = append(got, b[h.BStart:h.BEnd]...)
			prev = h.AEnd
		}

		got = append(got, a[prev:]...)
		if !slices.Equal(got, b) {
			t.Fatalf("diff of %v and %v rebuilds %v", a, b, got)
		}
	}
}
