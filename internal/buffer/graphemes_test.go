package buffer

import "testing"

func TestGraphemeBoundaries(t *testing.T) {
	t.Parallel()

	r := NewRope("\r\nHi\r\n")

	next := []int{2, 2, 3, 4, 6, 6, 6}
	for i, want := range next {
		if got := r.NextGraphemeBoundary(i); got != want {
			t.Errorf("next(%d) = %d, want %d", i, got, want)
		}
	}

	prev := []int{0, 0, 0, 2, 3, 4, 4}
	for i, want := range prev {
		if got := r.PrevGraphemeBoundary(i); got != want {
			t.Errorf("prev(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestGraphemeCombiningAcrossLines(t *testing.T) {
	t.Parallel()

	// e + combining acute, newline, flag emoji made of two regional indicators
	r := NewRope("é\n\U0001F1FA\U0001F1E6x")

	if got := r.NextGraphemeBoundary(0); got != 2 {
		t.Errorf("next(0) = %d, want 2", got)
	}

	if got := r.PrevGraphemeBoundary(3); got != 2 {
		t.Errorf("prev(3) = %d, want 2", got)
	}

	if got := r.NextGraphemeBoundary(3); got != 5 {
		t.Errorf("next(3) = %d, want 5", got)
	}

	if got := r.PrevGraphemeBoundary(5); got != 3 {
		t.Errorf("prev(5) = %d, want 3", got)
	}

	if got := r.NthNextGraphemeBoundary(0, 3); got != 5 {
		t.Errorf("nth next = %d, want 5", got)
	}

	if got := r.NthPrevGraphemeBoundary(6, 2); got != 3 {
		t.Errorf("nth prev = %d, want 3", got)
	}

	if got := r.EnsureGraphemeBoundaryNext(1); got != 2 {
		t.Errorf("ensure next(1) = %d, want 2", got)
	}

	if got := r.EnsureGraphemeBoundaryPrev(4); got != 3 {
		t.Errorf("ensure prev(4) = %d, want 3", got)
	}
}
