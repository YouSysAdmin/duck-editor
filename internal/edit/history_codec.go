package edit

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"
)

// historyFormat is bumped when the encoded layout changes, older files
// are then ignored.
const historyFormat = 1

type historyJSON struct {
	Format    int            `json:"format"`
	Current   int            `json:"current"`
	Revisions []revisionJSON `json:"revisions"`
}

type revisionJSON struct {
	Parent      int             `json:"parent"`
	LastChild   int             `json:"lastChild"`
	Time        int64           `json:"time"`
	Transaction transactionJSON `json:"tx"`
	Inversion   transactionJSON `json:"inv"`
}

type transactionJSON struct {
	Ops       []opJSON       `json:"ops"`
	Len       int            `json:"len"`
	LenAfter  int            `json:"lenAfter"`
	Selection *selectionJSON `json:"sel,omitempty"`
}

type opJSON struct {
	Kind OpKind `json:"k"`
	N    int    `json:"n,omitzero"`
	Text string `json:"t,omitempty"`
}

type selectionJSON struct {
	Ranges  [][2]int `json:"ranges"`
	Primary int      `json:"primary"`
}

// MarshalJSON encodes the whole revision tree.
func (h *History) MarshalJSON() ([]byte, error) {
	out := historyJSON{Format: historyFormat, Current: h.current, Revisions: make([]revisionJSON, len(h.revisions))}
	for i, r := range h.revisions {
		out.Revisions[i] = revisionJSON{
			Parent:      r.parent,
			LastChild:   r.lastChild,
			Time:        r.timestamp.UnixMilli(),
			Transaction: encodeTransaction(r.transaction),
			Inversion:   encodeTransaction(r.inversion),
		}
	}

	return json.Marshal(out)
}

// UnmarshalJSON restores a revision tree written by MarshalJSON. It
// rejects other formats and trees whose links do not hold together.
func (h *History) UnmarshalJSON(data []byte) error {
	var in historyJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}

	if in.Format != historyFormat {
		return fmt.Errorf("history format %d, want %d", in.Format, historyFormat)
	}

	n := len(in.Revisions)
	if n == 0 || in.Current < 0 || in.Current >= n {
		return errors.New("history has no valid current revision")
	}

	revisions := make([]revision, n)
	for i, r := range in.Revisions {
		if r.Parent < 0 || r.Parent >= n || r.LastChild < 0 || r.LastChild >= n || (i > 0 && r.Parent >= i) {
			return fmt.Errorf("history revision %d has broken links", i)
		}

		tx, err := decodeTransaction(r.Transaction)
		if err != nil {
			return fmt.Errorf("history revision %d: %w", i, err)
		}

		inv, err := decodeTransaction(r.Inversion)
		if err != nil {
			return fmt.Errorf("history revision %d: %w", i, err)
		}

		revisions[i] = revision{
			parent:      r.Parent,
			lastChild:   r.LastChild,
			transaction: tx,
			inversion:   inv,
			timestamp:   time.UnixMilli(r.Time),
		}
	}

	h.revisions, h.current = revisions, in.Current

	return nil
}

func encodeTransaction(t Transaction) transactionJSON {
	out := transactionJSON{Len: t.changes.length, LenAfter: t.changes.lenAfter, Ops: make([]opJSON, len(t.changes.ops))}
	for i, op := range t.changes.ops {
		out.Ops[i] = opJSON(op)
	}

	if t.selection != nil {
		sel := &selectionJSON{Primary: t.selection.primary}
		for _, r := range t.selection.ranges {
			sel.Ranges = append(sel.Ranges, [2]int{r.Anchor, r.Head})
		}

		out.Selection = sel
	}

	return out
}

func decodeTransaction(in transactionJSON) (Transaction, error) {
	cs := ChangeSet{length: in.Len, lenAfter: in.LenAfter, ops: make([]Operation, len(in.Ops))}
	before, after := 0, 0
	for i, op := range in.Ops {
		if op.Kind > OpInsert || op.N < 0 {
			return Transaction{}, errors.New("bad operation")
		}

		cs.ops[i] = Operation(op)
		switch op.Kind {
		case OpRetain:
			before += op.N
			after += op.N
		case OpDelete:
			before += op.N
		case OpInsert:
			after += op.N
		}
	}

	if before != in.Len || after != in.LenAfter {
		return Transaction{}, errors.New("operations do not add up to the lengths")
	}

	t := Transaction{changes: cs}
	if in.Selection != nil {
		if len(in.Selection.Ranges) == 0 || in.Selection.Primary < 0 || in.Selection.Primary >= len(in.Selection.Ranges) {
			return Transaction{}, errors.New("bad selection")
		}

		ranges := make([]Range, len(in.Selection.Ranges))
		for i, r := range in.Selection.Ranges {
			ranges[i] = NewRange(r[0], r[1])
		}

		sel := Selection{ranges: ranges, primary: in.Selection.Primary}
		t.selection = &sel
	}

	return t, nil
}
