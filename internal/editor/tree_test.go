package editor

import (
	"slices"
	"testing"
)

func TestTreeSplitRemove(t *testing.T) {
	t.Parallel()

	tr := NewTree(1)
	tr.Split(1, 2, LayoutVertical)
	tr.Split(2, 3, LayoutVertical)
	if got := tr.Views(); !slices.Equal(got, []ViewID{1, 2, 3}) {
		t.Fatalf("views = %v", got)
	}

	tr.Split(2, 4, LayoutHorizontal)
	if got := tr.Views(); !slices.Equal(got, []ViewID{1, 2, 4, 3}) {
		t.Fatalf("views after hsplit = %v", got)
	}

	tr.Resize(Rect{0, 0, 80, 20})
	a1, a2, a4, a3 := tr.Area(1), tr.Area(2), tr.Area(4), tr.Area(3)
	if a1.X != 0 || a1.H != 20 || a2.Y != 0 || a4.Y != a2.H || a2.H+a4.H != 20 {
		t.Fatalf("areas %v %v %v %v", a1, a2, a4, a3)
	}

	if a1.W+a2.W+a3.W+2 != 80 {
		t.Fatalf("widths %d %d %d", a1.W, a2.W, a3.W)
	}

	if len(tr.Borders()) != 2 {
		t.Fatalf("borders = %v", tr.Borders())
	}

	if n, ok := tr.Neighbor(1, DirRight, 0, 2); !ok || n != 2 {
		t.Fatalf("right of 1 = %d %v", n, ok)
	}

	if n, ok := tr.Neighbor(1, DirRight, 0, 15); !ok || n != 4 {
		t.Fatalf("right of 1 low = %d %v", n, ok)
	}

	if n, ok := tr.Neighbor(4, DirUp, a4.X+1, a4.Y); !ok || n != 2 {
		t.Fatalf("up of 4 = %d %v", n, ok)
	}

	if _, ok := tr.Neighbor(1, DirLeft, 0, 0); ok {
		t.Fatal("nothing left of 1")
	}

	tr.Remove(4)
	if got := tr.Views(); !slices.Equal(got, []ViewID{1, 2, 3}) {
		t.Fatalf("views after remove = %v", got)
	}

	tr.Remove(1)
	tr.Remove(2)
	if got := tr.Views(); !slices.Equal(got, []ViewID{3}) || !tr.root.isView {
		t.Fatalf("collapse = %v", got)
	}

	if tr.Next(3) != 3 || tr.Prev(3) != 3 {
		t.Fatal("single view wraps to itself")
	}
}
