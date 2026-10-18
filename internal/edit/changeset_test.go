package edit

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

func TestComposition(t *testing.T) {
	t.Parallel()

	a := ChangeSet{
		ops:      []Operation{RetainOp(5), InsertOp(" test!"), RetainOp(1), DeleteOp(2), InsertOp("abc")},
		length:   8,
		lenAfter: 15,
	}
	b := ChangeSet{
		ops:      []Operation{DeleteOp(10), InsertOp("世orld"), RetainOp(5)},
		length:   15,
		lenAfter: 10,
	}

	text := buffer.NewRope("hello xz")
	composed := a.Compose(b)
	if composed.length != 8 {
		t.Fatalf("len = %d, want 8", composed.length)
	}

	if !composed.Apply(&text) {
		t.Fatal("apply failed")
	}

	if text.String() != "世orld! abc" {
		t.Fatalf("got %q", text.String())
	}
}

func TestInvert(t *testing.T) {
	t.Parallel()

	changes := ChangeSet{
		ops:      []Operation{RetainOp(4), InsertOp("test"), DeleteOp(5), RetainOp(3)},
		length:   12,
		lenAfter: 11,
	}
	doc := buffer.NewRope("世界3 hello xz")
	revert := changes.Invert(doc)

	doc2 := doc
	changes.Apply(&doc2)

	if slices.Equal(changes.ops, revert.ops) || doc.String() == doc2.String() {
		t.Fatal("revert should differ from the change")
	}

	back := revert.Invert(doc2)
	if !slices.Equal(changes.ops, back.ops) || back.length != changes.length || back.lenAfter != changes.lenAfter {
		t.Fatalf("double invert mismatch: %v vs %v", changes.ops, back.ops)
	}

	revert.Apply(&doc2)
	if doc.String() != doc2.String() {
		t.Fatalf("revert did not restore: %q", doc2.String())
	}
}

func TestMapPos(t *testing.T) {
	t.Parallel()

	cs := ChangeSet{ops: []Operation{RetainOp(4), InsertOp("!!"), RetainOp(4)}, length: 8, lenAfter: 10}
	checks := []struct {
		pos   int
		assoc Assoc
		want  int
	}{
		{0, Before, 0},
		{4, Before, 4},
		{4, After, 6},
		{5, Before, 7},
	}
	for _, c := range checks {
		if got := cs.MapPos(c.pos, c.assoc); got != c.want {
			t.Errorf("insert map(%d,%v) = %d, want %d", c.pos, c.assoc, got, c.want)
		}
	}

	cs = ChangeSet{ops: []Operation{RetainOp(4), DeleteOp(4), RetainOp(4)}, length: 12, lenAfter: 8}
	checks = []struct {
		pos   int
		assoc Assoc
		want  int
	}{
		{0, Before, 0},
		{4, Before, 4},
		{5, Before, 4},
		{5, After, 4},
	}
	for _, c := range checks {
		if got := cs.MapPos(c.pos, c.assoc); got != c.want {
			t.Errorf("delete map(%d,%v) = %d, want %d", c.pos, c.assoc, got, c.want)
		}
	}

	// stays in between replacements
	cs = ChangeSet{ops: []Operation{InsertOp("ab"), DeleteOp(2), InsertOp("cd"), DeleteOp(2)}, length: 4, lenAfter: 4}
	if cs.MapPos(2, Before) != 2 || cs.MapPos(2, After) != 2 {
		t.Error("replacement gap mapping")
	}

	positions := []int{4, 2}
	cs.UpdatePositions([]PosAssoc{{&positions[0], After}, {&positions[1], After}})
	if positions[0] != 4 || positions[1] != 2 {
		t.Errorf("unsorted positions = %v", positions)
	}

	// stays at word boundary
	cs = ChangeSet{ops: []Operation{RetainOp(2), InsertOp(" ab"), RetainOp(2), InsertOp("de ")}, length: 4, lenAfter: 10}
	if got := cs.MapPos(2, BeforeWord); got != 3 {
		t.Errorf("BeforeWord = %d, want 3", got)
	}

	if got := cs.MapPos(4, AfterWord); got != 9 {
		t.Errorf("AfterWord = %d, want 9", got)
	}

	cs = ChangeSet{
		ops:      []Operation{RetainOp(1), InsertOp(" b"), DeleteOp(1), RetainOp(1), InsertOp("e "), DeleteOp(1)},
		length:   5,
		lenAfter: 7,
	}
	if got := cs.MapPos(1, BeforeWord); got != 2 {
		t.Errorf("BeforeWord replace = %d, want 2", got)
	}

	if got := cs.MapPos(3, AfterWord); got != 5 {
		t.Errorf("AfterWord replace = %d, want 5", got)
	}

	cs = ChangeSet{
		ops:      []Operation{RetainOp(1), InsertOp("a"), DeleteOp(2), RetainOp(1), InsertOp("e"), DeleteOp(1), RetainOp(1)},
		length:   5,
		lenAfter: 7,
	}
	if got := cs.MapPos(2, BeforeWord); got != 1 {
		t.Errorf("BeforeWord inside = %d, want 1", got)
	}

	if got := cs.MapPos(4, AfterWord); got != 4 {
		t.Errorf("AfterWord inside = %d, want 4", got)
	}
}

func TestTransactionChange(t *testing.T) {
	t.Parallel()

	doc := buffer.NewRope("hello world!\ntest 123")
	tx := ChangeText(doc, []Change{{1, 1, ""}, {6, 11, "void"}, {12, 17, ""}})
	tx.Apply(&doc)
	if doc.String() != "hello void! 123" {
		t.Fatalf("got %q", doc.String())
	}
}

func TestChangesIter(t *testing.T) {
	t.Parallel()

	doc := buffer.NewRope("hello world!\ntest 123")
	changes := []Change{{6, 11, "void"}, {12, 17, ""}}
	tx := ChangeText(doc, changes)
	if got := tx.Changes().Changes(); !slices.Equal(got, changes) {
		t.Fatalf("got %v, want %v", got, changes)
	}
}

func TestOptimizedComposition(t *testing.T) {
	t.Parallel()

	state := State{Doc: buffer.NewRope(""), Selection: PointSelection(0)}
	var composed *ChangeSet
	for _, ch := range []string{"h", "e", "l", "l", "o"} {
		tx := ChangeBySelection(state.Doc, state.Selection, func(r Range) Change {
			return Change{From: r.Head, To: r.Head, Text: ch}
		})
		tx.Apply(&state.Doc)
		state.Selection = state.Selection.Map(tx.Changes())
		if composed == nil {
			cs := *tx.Changes()
			composed = &cs
		} else {
			cs := composed.Compose(*tx.Changes())
			composed = &cs
		}
	}

	if state.Doc.String() != "hello" {
		t.Fatalf("doc = %q", state.Doc.String())
	}

	if !slices.Equal(composed.ops, []Operation{InsertOp("hello")}) {
		t.Fatalf("ops = %v", composed.ops)
	}
}

func TestCombineWithEmpty(t *testing.T) {
	t.Parallel()

	a := NewChangeSet(0)
	b := NewChangeSet(0)
	b.insert("a")
	if got := a.Compose(b); !slices.Equal(got.ops, []Operation{InsertOp("a")}) {
		t.Fatalf("ops = %v", got.ops)
	}

	const utf = "Hello, これはヘリックスエディターです！"
	c := NewChangeSet(0)
	c.insert(utf)
	got := a.Compose(c)
	if !slices.Equal(got.ops, []Operation{InsertOp(utf)}) || got.lenAfter != len([]rune(utf)) {
		t.Fatalf("utf8 compose: %v len %d", got.ops, got.lenAfter)
	}
}

func TestDeleteRangesMergesOverlap(t *testing.T) {
	t.Parallel()

	doc := buffer.NewRope("0123456789")
	tx := DeleteRanges(doc, [][2]int{{1, 4}, {3, 6}, {8, 9}})
	tx.Apply(&doc)
	if doc.String() != "0679" {
		t.Fatalf("got %q", doc.String())
	}
}

func TestChangeByAndWithSelection(t *testing.T) {
	t.Parallel()

	doc := buffer.NewRope("ab cd")
	sel := NewSelection([]Range{NewRange(0, 2), NewRange(3, 5)}, 1)
	tx := ChangeByAndWithSelection(doc, sel, func(r Range) (Change, Range, bool) {
		return Change{From: r.From(), To: r.To(), Text: "xyz"}, Range{}, false
	})
	tx.Apply(&doc)
	got, ok := tx.Selection()
	if doc.String() != "xyz xyz" || !ok {
		t.Fatalf("doc %q ok %v", doc.String(), ok)
	}

	want := NewSelection([]Range{NewRange(0, 3), NewRange(4, 7)}, 1)
	if !got.Equal(want) {
		t.Fatalf("selection %v, want %v", got.Ranges(), want.Ranges())
	}
}

func TestInsertAtEOFKeepsReceiver(t *testing.T) {
	t.Parallel()

	doc := buffer.NewRope("ab")
	base := ChangeText(doc, []Change{{From: 2, To: 2, Text: "x"}})
	_ = base.InsertAtEOF("Y")

	text := doc
	base.Apply(&text)
	if text.String() != "abx" {
		t.Fatalf("receiver changed, applied %q", text.String())
	}
}

func TestDeleteNestedRanges(t *testing.T) {
	t.Parallel()

	doc := buffer.NewRope("0123456789")
	sel := NewSelection([]Range{NewRange(2, 3), NewRange(4, 5), NewRange(9, 10)}, 0)
	// The first deletion grows to 2..8 and swallows the second one.
	tx := DeleteByAndWithSelection(doc, sel, func(r Range) (int, int, Range, bool) {
		from, to := r.From(), r.To()
		if from == 2 {
			to = 8
		}

		return from, to, NewRange(from, from), true
	})
	text := doc
	tx.Apply(&text)
	got, _ := tx.Selection()
	last := got.Ranges()[len(got.Ranges())-1]
	if text.String() != "018" || last.From() != 3 {
		t.Fatalf("text %q selection %v", text.String(), got.Ranges())
	}
}

func TestUpdatePositionsMatchesMapPos(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(3, 4))
	assocs := []Assoc{Before, After, BeforeSticky, AfterSticky, BeforeWord, AfterWord}
	for range 500 {
		docLen := rng.IntN(20)
		doc := buffer.NewRope(strings.Repeat("ab cd", 4)[:docLen])
		var changes []Change
		for pos := 0; pos < docLen; {
			from := pos + rng.IntN(docLen-pos+1)
			to := min(from+rng.IntN(3), docLen)
			changes = append(changes, Change{From: from, To: to, Text: strings.Repeat("x", rng.IntN(3))})
			pos = to + 1
		}

		tx := ChangeText(doc, changes)
		cs := tx.Changes()

		n := 6
		values := make([]int, n)
		for i := range values {
			values[i] = rng.IntN(docLen + 1)
		}

		if rng.IntN(2) == 0 {
			slices.Sort(values)
		}

		positions := make([]PosAssoc, n)
		want := make([]int, n)
		for i := range values {
			assoc := assocs[rng.IntN(len(assocs))]
			want[i] = cs.MapPos(values[i], assoc)
			positions[i] = PosAssoc{Pos: &values[i], Assoc: assoc}
		}

		cs.UpdatePositions(positions)
		if !slices.Equal(values, want) {
			t.Fatalf("sweep %v, one by one %v, changes %v", values, want, changes)
		}
	}
}
