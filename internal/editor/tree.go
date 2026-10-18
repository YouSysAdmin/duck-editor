package editor

import "slices"

// Layout is how a container arranges its children.
type Layout int

// Layouts. Horizontal stacks children top to bottom like hsplit,
// Vertical puts them side by side like vsplit.
const (
	LayoutHorizontal Layout = iota
	LayoutVertical
)

// Direction on screen for jumping between views.
type Direction int

// Screen directions.
const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

// Tree is the split layout of views.
type Tree struct {
	root *node
}

type node struct {
	view     ViewID
	isView   bool
	layout   Layout
	children []*node
	parent   *node
	area     Rect
}

// NewTree creates a layout holding one view.
func NewTree(view ViewID) *Tree {
	return &Tree{root: &node{view: view, isView: true}}
}

func (t *Tree) find(view ViewID) *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		if found != nil {
			return
		}

		if n.isView && n.view == view {
			found = n

			return
		}

		for _, c := range n.children {
			walk(c)
		}
	}
	walk(t.root)

	return found
}

// Views lists the views in layout order.
func (t *Tree) Views() []ViewID {
	var out []ViewID
	var walk func(n *node)
	walk = func(n *node) {
		if n.isView {
			out = append(out, n.view)

			return
		}

		for _, c := range n.children {
			walk(c)
		}
	}
	walk(t.root)

	return out
}

// Len returns the number of views.
func (t *Tree) Len() int {
	return len(t.Views())
}

// Split adds newView next to view with the given layout.
func (t *Tree) Split(view, newView ViewID, layout Layout) {
	n := t.find(view)
	if n == nil {
		return
	}

	leaf := &node{view: newView, isView: true}
	parent := n.parent
	if parent != nil && parent.layout == layout {
		idx := slices.Index(parent.children, n)
		leaf.parent = parent
		parent.children = slices.Insert(parent.children, idx+1, leaf)

		return
	}

	// Turn the view node into a container holding the old and new view.
	container := &node{layout: layout, parent: parent}
	old := &node{view: n.view, isView: true, parent: container}
	leaf.parent = container
	container.children = []*node{old, leaf}
	if parent == nil {
		t.root = container
	} else {
		idx := slices.Index(parent.children, n)
		parent.children[idx] = container
	}
}

// Remove drops a view, collapsing containers left with one child.
func (t *Tree) Remove(view ViewID) {
	n := t.find(view)
	if n == nil || n.parent == nil {
		return
	}

	parent := n.parent
	idx := slices.Index(parent.children, n)
	parent.children = slices.Delete(parent.children, idx, idx+1)
	if len(parent.children) != 1 {
		return
	}

	only := parent.children[0]
	only.parent = parent.parent
	if parent.parent == nil {
		t.root = only

		return
	}

	gidx := slices.Index(parent.parent.children, parent)
	parent.parent.children[gidx] = only
}

// Resize assigns screen areas to all views. Side by side children are
// separated by a one column border.
func (t *Tree) Resize(area Rect) {
	t.root.resize(area)
}

func (n *node) resize(area Rect) {
	n.area = area
	if n.isView {
		return
	}

	count := len(n.children)
	if count == 0 {
		return
	}

	if n.layout == LayoutVertical {
		total := max(area.W-(count-1), 0)
		x := area.X
		for i, c := range n.children {
			w := total / count
			if i < total%count {
				w++
			}

			c.resize(Rect{X: x, Y: area.Y, W: w, H: area.H})
			x += w + 1
		}

		return
	}

	y := area.Y
	for i, c := range n.children {
		h := area.H / count
		if i < area.H%count {
			h++
		}

		c.resize(Rect{X: area.X, Y: y, W: area.W, H: h})
		y += h
	}
}

// Area returns the area of a view after Resize.
func (t *Tree) Area(view ViewID) Rect {
	if n := t.find(view); n != nil {
		return n.area
	}

	return Rect{}
}

// Borders lists the vertical separator columns between side by side
// views as rectangles one cell wide.
func (t *Tree) Borders() []Rect {
	var out []Rect
	var walk func(n *node)
	walk = func(n *node) {
		if n.isView {
			return
		}

		if n.layout == LayoutVertical {
			for _, c := range n.children[:len(n.children)-1] {
				out = append(out, Rect{X: c.area.X + c.area.W, Y: n.area.Y, W: 1, H: n.area.H})
			}
		}

		for _, c := range n.children {
			walk(c)
		}
	}
	walk(t.root)

	return out
}

// Next returns the view after the given one in layout order, wrapping.
func (t *Tree) Next(view ViewID) ViewID {
	views := t.Views()
	idx := slices.Index(views, view)
	if idx < 0 || len(views) == 0 {
		return view
	}

	return views[(idx+1)%len(views)]
}

// Prev returns the view before the given one, wrapping.
func (t *Tree) Prev(view ViewID) ViewID {
	views := t.Views()
	idx := slices.Index(views, view)
	if idx < 0 || len(views) == 0 {
		return view
	}

	return views[(idx-1+len(views))%len(views)]
}

// Neighbor finds the view adjacent to the given one in a direction,
// preferring the one that contains the cursor row or column.
func (t *Tree) Neighbor(view ViewID, dir Direction, cursorX, cursorY int) (ViewID, bool) {
	cur := t.Area(view)
	best := ViewID(-1)
	bestDist := -1
	for _, other := range t.Views() {
		if other == view {
			continue
		}

		a := t.Area(other)
		var adjacent bool
		var dist int
		switch dir {
		case DirLeft:
			adjacent = a.X+a.W < cur.X && cursorY >= a.Y && cursorY < a.Y+a.H
			dist = cur.X - (a.X + a.W)
		case DirRight:
			adjacent = a.X > cur.X+cur.W-1 && cursorY >= a.Y && cursorY < a.Y+a.H
			dist = a.X - (cur.X + cur.W)
		case DirUp:
			adjacent = a.Y+a.H <= cur.Y && cursorX >= a.X && cursorX < a.X+a.W
			dist = cur.Y - (a.Y + a.H)
		case DirDown:
			adjacent = a.Y >= cur.Y+cur.H && cursorX >= a.X && cursorX < a.X+a.W
			dist = a.Y - (cur.Y + cur.H)
		}

		if adjacent && (bestDist < 0 || dist < bestDist) {
			best, bestDist = other, dist
		}
	}

	if best < 0 {
		return view, false
	}

	return best, true
}

// Swap exchanges the positions of two views in the layout.
func (t *Tree) Swap(a, b ViewID) {
	na, nb := t.find(a), t.find(b)
	if na == nil || nb == nil {
		return
	}

	na.view, nb.view = nb.view, na.view
}

// Transpose flips the split holding a view between side by side and
// stacked. It reports whether the view is in a split at all.
func (t *Tree) Transpose(view ViewID) bool {
	n := t.find(view)
	if n == nil || n.parent == nil {
		return false
	}

	if n.parent.layout == LayoutHorizontal {
		n.parent.layout = LayoutVertical
	} else {
		n.parent.layout = LayoutHorizontal
	}

	return true
}
