package edit

import (
	"slices"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// Transaction is one undoable unit of change with an optional selection to
// apply afterwards.
type Transaction struct {
	changes   ChangeSet
	selection *Selection
}

// FromChangeSet wraps a changeset.
func FromChangeSet(cs ChangeSet) Transaction {
	return Transaction{changes: cs}
}

// Changes returns the changeset.
func (t *Transaction) Changes() *ChangeSet {
	return &t.changes
}

// Selection returns the explicit selection, if any.
func (t *Transaction) Selection() (Selection, bool) {
	if t.selection == nil {
		return Selection{}, false
	}

	return *t.selection, true
}

// Apply edits doc in place and reports success.
func (t *Transaction) Apply(doc *buffer.Rope) bool {
	if t.changes.IsEmpty() {
		return true
	}

	return t.changes.Apply(doc)
}

// Invert builds the transaction that reverts this one.
func (t *Transaction) Invert(original buffer.Rope) Transaction {
	return Transaction{changes: t.changes.Invert(original)}
}

// Compose chains other after this transaction. The other selection wins.
func (t Transaction) Compose(other Transaction) Transaction {
	return Transaction{changes: t.changes.Compose(other.changes), selection: other.selection}
}

// WithSelection sets the selection to apply after the change.
func (t Transaction) WithSelection(sel Selection) Transaction {
	t.selection = &sel

	return t
}

// InsertAtEOF appends text to the end of the document. The changeset
// must already retain up to the end. The receiver keeps its operations.
func (t Transaction) InsertAtEOF(text string) Transaction {
	t.changes.ops = slices.Clone(t.changes.ops)
	t.changes.insert(text)

	return t
}

// ChangeText builds a transaction from sorted, non overlapping changes.
func ChangeText(doc buffer.Rope, changes []Change) Transaction {
	return FromChangeSet(ChangeSetFromChanges(doc.Len(), changes))
}

// ChangeIgnoreOverlapping builds a transaction from ranges that may
// overlap. A range starting before the previous one ended is skipped.
// The callback produces the replacement for every kept range.
func ChangeIgnoreOverlapping(doc buffer.Rope, ranges [][2]int, replacement func(idx, from, to int) string) Transaction {
	changes := make([]Change, 0, len(ranges))
	last := 0
	for i, r := range ranges {
		if r[0] < last {
			continue
		}

		changes = append(changes, Change{From: r[0], To: r[1], Text: replacement(i, r[0], r[1])})
		last = r[1]
	}

	return ChangeText(doc, changes)
}

// DeleteRanges builds a transaction from deletions that may overlap.
func DeleteRanges(doc buffer.Rope, deletions [][2]int) Transaction {
	cs := ChangeSet{ops: make([]Operation, 0, 2*len(deletions)+1)}
	last := 0
	for _, d := range deletions {
		from, to := d[0], d[1]
		if last > to {
			continue
		}

		from = max(from, last)
		cs.retain(from - last)
		cs.delete(to - from)
		last = to
	}

	cs.retain(doc.Len() - last)

	return FromChangeSet(cs)
}

// ChangeBySelection builds one change per range.
func ChangeBySelection(doc buffer.Rope, sel Selection, f func(Range) Change) Transaction {
	changes := make([]Change, 0, sel.Len())
	for _, r := range sel.Ranges() {
		changes = append(changes, f(r))
	}

	return ChangeText(doc, changes)
}

// ChangeByAndWithSelection builds one change per range and a new
// selection. When f returns ok=false for the new range, the old range is
// mapped through its own change instead.
func ChangeByAndWithSelection(doc buffer.Rope, sel Selection, f func(Range) (Change, Range, bool)) Transaction {
	endRanges := make([]Range, 0, sel.Len())
	offset := 0
	tx := ChangeBySelection(doc, sel, func(start Range) Change {
		change, end, ok := f(start)
		size := utf8.RuneCountInString(change.Text) - (change.To - change.From)
		if !ok {
			cs := ChangeSetFromChanges(doc.Len(), []Change{change})
			end = start.Map(&cs)
		}

		endRanges = append(endRanges, NewRange(end.Anchor+offset, end.Head+offset))
		offset += size

		return change
	})

	return tx.WithSelection(NewSelection(endRanges, sel.PrimaryIndex()))
}

// DeleteBySelection builds one deletion per range, merging overlaps.
func DeleteBySelection(doc buffer.Rope, sel Selection, f func(Range) (int, int)) Transaction {
	deletions := make([][2]int, 0, sel.Len())
	for _, r := range sel.Ranges() {
		from, to := f(r)
		deletions = append(deletions, [2]int{from, to})
	}

	return DeleteRanges(doc, deletions)
}

// DeleteByAndWithSelection builds one deletion per range plus a new
// selection, like ChangeByAndWithSelection but allowing overlaps.
func DeleteByAndWithSelection(doc buffer.Rope, sel Selection, f func(Range) (int, int, Range, bool)) Transaction {
	endRanges := make([]Range, 0, sel.Len())
	offset := 0
	last := 0
	tx := DeleteBySelection(doc, sel, func(start Range) (int, int) {
		from, to, end, ok := f(start)
		// Only the part past the previous deletion shifts later ranges.
		size := max(to-max(from, last), 0)
		if !ok {
			cs := ChangeSetFromChanges(doc.Len(), []Change{{From: from, To: to}})
			end = start.Map(&cs)
		}

		endRanges = append(endRanges, NewRange(max(end.Anchor-offset, 0), max(end.Head-offset, 0)))
		offset += size
		last = max(last, to)

		return from, to
	})

	return tx.WithSelection(NewSelection(endRanges, sel.PrimaryIndex()))
}
