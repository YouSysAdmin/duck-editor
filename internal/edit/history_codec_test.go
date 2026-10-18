package edit

import (
	"encoding/json/v2"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

func TestHistoryRoundTrip(t *testing.T) {
	t.Parallel()

	h := NewHistory()
	state := State{Doc: buffer.NewRope("abc"), Selection: PointSelection(3)}
	for _, ch := range []string{"d", "é"} {
		end := state.Doc.Len()
		tx := ChangeText(state.Doc, []Change{{From: end, To: end, Text: ch}})
		h.CommitRevision(tx, state)
		tx.Apply(&state.Doc)
	}

	data, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}

	var back History
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}

	doc := state.Doc
	for range 2 {
		tx, ok := back.Undo()
		if !ok {
			t.Fatal("undo after decoding")
		}

		tx.Apply(&doc)
	}

	if doc.String() != "abc" {
		t.Fatalf("undone text %q", doc.String())
	}

	for _, bad := range []string{`{"format":99,"current":0,"revisions":[{}]}`, `{"format":1,"current":5,"revisions":[]}`} {
		if err := json.Unmarshal([]byte(bad), &back); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}
