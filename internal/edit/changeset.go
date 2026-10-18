package edit

import (
	"fmt"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// OpKind is the type of a changeset operation.
type OpKind uint8

// Operation kinds.
const (
	OpRetain OpKind = iota
	OpDelete
	OpInsert
)

// Operation is one step of a changeset walking the old document from left
// to right. N is the char count for all kinds, Text is set for inserts.
type Operation struct {
	Kind OpKind
	N    int
	Text string
}

// RetainOp keeps n chars.
func RetainOp(n int) Operation {
	return Operation{Kind: OpRetain, N: n}
}

// DeleteOp removes n chars.
func DeleteOp(n int) Operation {
	return Operation{Kind: OpDelete, N: n}
}

// InsertOp inserts text.
func InsertOp(s string) Operation {
	return Operation{Kind: OpInsert, N: utf8.RuneCountInString(s), Text: s}
}

// Assoc tells which side a position sticks to when text is inserted at it.
type Assoc uint8

// Association kinds.
const (
	// Before keeps the position before an insertion at that point.
	Before Assoc = iota

	// After moves the position past an insertion at that point.
	After

	// AfterWord acts like After when a word char is inserted after the
	// position, otherwise like Before.
	AfterWord

	// BeforeWord acts like Before when a word char is inserted before the
	// position, otherwise like After.
	BeforeWord

	// BeforeSticky acts like Before but keeps the offset into a same size
	// replacement.
	BeforeSticky

	// AfterSticky acts like After but keeps the offset into a same size
	// replacement.
	AfterSticky
)

func (a Assoc) stayAtGaps() bool {
	return a != BeforeWord && a != AfterWord
}

func (a Assoc) sticky() bool {
	return a == BeforeSticky || a == AfterSticky
}

func (a Assoc) insertOffset(s string) int {
	chars := utf8.RuneCountInString(s)
	switch a {
	case After, AfterSticky:
		return chars
	case AfterWord:
		n := 0
		for _, ch := range s {
			if !IsWordChar(ch) {
				break
			}

			n++
		}

		return n
	case BeforeWord:
		runes := []rune(s)
		n := 0
		for i := len(runes) - 1; i >= 0 && IsWordChar(runes[i]); i-- {
			n++
		}

		return chars - n
	default:
		return 0
	}
}

// Change is a replacement of the old text in [From, To) with Text. An
// empty Text is a deletion.
type Change struct {
	From int
	To   int
	Text string
}

// PosAssoc is a position to map together with its association.
type PosAssoc struct {
	Pos   *int
	Assoc Assoc
}

// ChangeSet describes an edit as a sequence of operations over a document
// of a known length.
type ChangeSet struct {
	ops      []Operation
	length   int
	lenAfter int
}

// NewChangeSet creates an empty changeset for a document of docLen chars.
func NewChangeSet(docLen int) ChangeSet {
	return ChangeSet{length: docLen, lenAfter: docLen}
}

// Ops returns the operations.
func (cs *ChangeSet) Ops() []Operation {
	return cs.ops
}

// Len is the required length of the document before the change.
func (cs *ChangeSet) Len() int {
	return cs.length
}

func (cs *ChangeSet) delete(n int) {
	if n == 0 {
		return
	}

	cs.length += n
	if last := len(cs.ops) - 1; last >= 0 && cs.ops[last].Kind == OpDelete {
		cs.ops[last].N += n

		return
	}

	cs.ops = append(cs.ops, DeleteOp(n))
}

func (cs *ChangeSet) insert(s string) {
	if s == "" {
		return
	}

	cs.lenAfter += utf8.RuneCountInString(s)

	n := len(cs.ops)
	switch {
	case n >= 1 && cs.ops[n-1].Kind == OpInsert:
		cs.ops[n-1].Text += s
		cs.ops[n-1].N += utf8.RuneCountInString(s)
	case n >= 2 && cs.ops[n-2].Kind == OpInsert && cs.ops[n-1].Kind == OpDelete:
		cs.ops[n-2].Text += s
		cs.ops[n-2].N += utf8.RuneCountInString(s)
	case n >= 1 && cs.ops[n-1].Kind == OpDelete:
		// Keep inserts before deletes so replacements read insert, delete.
		del := cs.ops[n-1]
		cs.ops[n-1] = InsertOp(s)
		cs.ops = append(cs.ops, del)
	default:
		cs.ops = append(cs.ops, InsertOp(s))
	}
}

func (cs *ChangeSet) retain(n int) {
	if n == 0 {
		return
	}

	cs.length += n
	cs.lenAfter += n
	if last := len(cs.ops) - 1; last >= 0 && cs.ops[last].Kind == OpRetain {
		cs.ops[last].N += n

		return
	}

	cs.ops = append(cs.ops, RetainOp(n))
}

// IsEmpty reports a changeset that does nothing.
func (cs *ChangeSet) IsEmpty() bool {
	if len(cs.ops) == 0 {
		return true
	}

	return len(cs.ops) == 1 && cs.ops[0] == RetainOp(cs.length)
}

// Compose combines this change (docA to docB) with other (docB to docC)
// into a change from docA to docC.
func (cs ChangeSet) Compose(other ChangeSet) ChangeSet {
	if cs.lenAfter != other.length {
		panic(fmt.Sprintf("edit: compose length mismatch %d vs %d", cs.lenAfter, other.length))
	}

	if len(cs.ops) == 0 {
		return other
	}

	if len(other.ops) == 0 {
		return cs
	}

	a := opQueue{ops: cs.ops}
	b := opQueue{ops: other.ops}
	out := ChangeSet{ops: make([]Operation, 0, len(cs.ops)+len(other.ops))}

	for {
		ha, okA := a.peek()
		hb, okB := b.peek()
		switch {
		case !okA && !okB:
			out.lenAfter = other.lenAfter
			if out.length != cs.length {
				panic("edit: compose produced inconsistent length")
			}

			return out
		case okA && ha.Kind == OpDelete:
			out.delete(ha.N)
			a.next()
		case okB && hb.Kind == OpInsert:
			out.insert(hb.Text)
			b.next()
		case !okA || !okB:
			panic("edit: compose ran out of operations on one side")
		case ha.Kind == OpRetain && hb.Kind == OpRetain:
			n := min(ha.N, hb.N)
			out.retain(n)
			a.consume(n)
			b.consume(n)
		case ha.Kind == OpInsert && hb.Kind == OpDelete:
			n := min(ha.N, hb.N)
			a.consume(n)
			b.consume(n)
		case ha.Kind == OpInsert && hb.Kind == OpRetain:
			n := min(ha.N, hb.N)
			out.insert(prefixChars(ha.Text, n))
			a.consume(n)
			b.consume(n)
		case ha.Kind == OpRetain && hb.Kind == OpDelete:
			n := min(ha.N, hb.N)
			out.delete(n)
			a.consume(n)
			b.consume(n)
		default:
			panic("edit: compose reached an impossible state")
		}
	}
}

// opQueue walks operations allowing partial consumption of the head.
type opQueue struct {
	ops  []Operation
	head Operation
	have bool
}

func (q *opQueue) peek() (Operation, bool) {
	if !q.have {
		if len(q.ops) == 0 {
			return Operation{}, false
		}

		q.head = q.ops[0]
		q.ops = q.ops[1:]
		q.have = true
	}

	return q.head, true
}

func (q *opQueue) next() {
	q.have = false
}

// consume removes n chars from the head, dropping it when exhausted.
func (q *opQueue) consume(n int) {
	if q.head.N == n {
		q.have = false

		return
	}

	q.head.N -= n
	if q.head.Kind == OpInsert {
		q.head.Text = q.head.Text[len(prefixChars(q.head.Text, n)):]
	}
}

func prefixChars(s string, n int) string {
	i := 0
	for ; n > 0 && i < len(s); n-- {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}

	return s[:i]
}

// Invert builds the changeset that undoes this one. original is the
// document before this change was applied.
func (cs *ChangeSet) Invert(original buffer.Rope) ChangeSet {
	if original.Len() != cs.length {
		panic(fmt.Sprintf("edit: invert length mismatch %d vs %d", original.Len(), cs.length))
	}

	out := ChangeSet{ops: make([]Operation, 0, len(cs.ops))}
	pos := 0
	for _, op := range cs.ops {
		switch op.Kind {
		case OpRetain:
			out.retain(op.N)
			pos += op.N
		case OpDelete:
			out.insert(original.Slice(pos, pos+op.N))
			pos += op.N
		case OpInsert:
			out.delete(op.N)
		}
	}

	return out
}

// Apply edits text in place. It returns false when the length does not
// match and leaves the text untouched.
func (cs *ChangeSet) Apply(text *buffer.Rope) bool {
	if text.Len() != cs.length {
		return false
	}

	pos := 0
	for _, op := range cs.ops {
		switch op.Kind {
		case OpRetain:
			pos += op.N
		case OpDelete:
			text.Remove(pos, pos+op.N)
		case OpInsert:
			text.Insert(pos, op.Text)
			pos += op.N
		}
	}

	return true
}

// MapPos maps a position in the old document to the new one.
func (cs *ChangeSet) MapPos(pos int, assoc Assoc) int {
	return cs.mapFrom(&mapCursor{}, pos, assoc)
}

// UpdatePositions maps every position in place. Sorted positions are
// mapped in one sweep over the operations, an earlier position than the
// previous one restarts the sweep.
func (cs *ChangeSet) UpdatePositions(positions []PosAssoc) {
	var c mapCursor
	for _, p := range positions {
		*p.Pos = cs.mapFrom(&c, *p.Pos, p.Assoc)
	}
}

// mapCursor is where a mapping sweep stands: the operation index, its
// start in the old and new document, and the last position mapped.
type mapCursor struct {
	i, oldPos, newPos int
	last              int
}

// mapFrom maps pos starting at the cursor and leaves the cursor at the
// start of the operation holding pos. Operations are only passed for
// positions beyond them, so a position not below the last one can
// continue from there.
func (cs *ChangeSet) mapFrom(c *mapCursor, pos int, assoc Assoc) int {
	if pos < c.last {
		*c = mapCursor{}
	}

	c.last = pos
	for ; c.i < len(cs.ops); c.i++ {
		op := cs.ops[c.i]
		switch op.Kind {
		case OpRetain:
			if pos < c.oldPos+op.N {
				return c.newPos + (pos - c.oldPos)
			}

			c.oldPos += op.N
			c.newPos += op.N
		case OpDelete:
			if pos < c.oldPos+op.N {
				return c.newPos
			}

			c.oldPos += op.N
		case OpInsert:
			if c.i+1 < len(cs.ops) && cs.ops[c.i+1].Kind == OpDelete {
				// Insert followed by delete is a replacement.
				del := cs.ops[c.i+1].N
				if pos < c.oldPos+del {
					if pos == c.oldPos && assoc.stayAtGaps() {
						return c.newPos
					}

					ins := assoc.insertOffset(op.Text)
					if del == ins && assoc.sticky() {
						return c.newPos + (pos - c.oldPos)
					}

					return c.newPos + ins
				}

				c.i++
				c.oldPos += del
			} else if pos == c.oldPos {
				return c.newPos + assoc.insertOffset(op.Text)
			}

			c.newPos += op.N
		}
	}

	if pos == c.oldPos {
		return c.newPos
	}

	panic(fmt.Sprintf("edit: position %d out of range for changeset len %d", pos, c.oldPos))
}

// Changes lists the edits as from, to, text triples in the old document.
func (cs *ChangeSet) Changes() []Change {
	out := make([]Change, 0, len(cs.ops))
	pos := 0
	for i := 0; i < len(cs.ops); i++ {
		op := cs.ops[i]
		switch op.Kind {
		case OpRetain:
			pos += op.N
		case OpDelete:
			out = append(out, Change{From: pos, To: pos + op.N})
			pos += op.N
		case OpInsert:
			start := pos
			if i+1 < len(cs.ops) && cs.ops[i+1].Kind == OpDelete {
				pos += cs.ops[i+1].N
				i++
			}

			out = append(out, Change{From: start, To: pos, Text: op.Text})
		}
	}

	return out
}

// ChangeSetFromChanges builds a changeset from sorted, non overlapping
// changes against a document of docLen chars.
func ChangeSetFromChanges(docLen int, changes []Change) ChangeSet {
	cs := ChangeSet{ops: make([]Operation, 0, 2*len(changes)+1)}
	last := 0
	for _, c := range changes {
		if c.From < last || c.From > c.To {
			panic(fmt.Sprintf("edit: changes must be sorted and ordered, got %d..%d after %d", c.From, c.To, last))
		}

		if c.To > docLen {
			panic(fmt.Sprintf("edit: change %d..%d past the end %d", c.From, c.To, docLen))
		}

		cs.retain(c.From - last)
		cs.insert(c.Text)
		cs.delete(c.To - c.From)
		last = c.To
	}

	cs.retain(docLen - last)

	return cs
}
