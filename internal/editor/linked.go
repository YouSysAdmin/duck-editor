package editor

import (
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

// linkedState tracks linked editing for one insert session: requested
// once on entering insert mode, added when extra cursors were placed.
type linkedState struct {
	requested bool
	added     bool
}

// requestLinkedEditing asks the server, once per insert session with a
// single cursor, which ranges change together with the one under the
// cursor, and puts a cursor in each so typing edits all of them.
func (e *Editor) requestLinkedEditing() {
	if e.Mode != Insert || e.linked.requested {
		return
	}

	e.linked.requested = true
	doc := e.Doc()
	if doc.Selection().Len() != 1 {
		return
	}

	client := doc.clientFor(featLinkedEditing)
	if client == nil {
		return
	}

	docID, edits := doc.ID, doc.edits
	client.LinkedEditingRanges(e.lspPosition(doc, client), func(res *lsp.LinkedEditingRanges, err error) {
		cur, ok := e.Docs[docID]
		if err != nil || res == nil || !ok || cur != e.Doc() || e.Mode != Insert || cur.edits != edits || cur.Selection().Len() != 1 {
			return
		}

		if sel, ok := linkedSelection(cur, client, res.Ranges); ok {
			cur.SetSelection(sel)
			e.linked.added = true
		}
	})
}

// linkedSelection copies the primary range into every linked range at
// the same offset, false when the cursor is in none of them.
func linkedSelection(doc *Document, client *lsp.Client, ranges []lsp.Range) (edit.Selection, bool) {
	text := doc.Text()
	primary := doc.Selection().Primary()
	cursor := primary.Cursor(text)
	var starts, ends []int
	own := -1
	for _, r := range ranges {
		cr, ok := lsp.LSPToRange(text, r, client.Encoding)
		if !ok {
			continue
		}

		if own < 0 && cr.From() <= cursor && cursor <= cr.To() {
			own = len(starts)
		}

		starts = append(starts, cr.From())
		ends = append(ends, cr.To())
	}

	if own < 0 || len(starts) < 2 {
		return edit.Selection{}, false
	}

	da, dh := primary.Anchor-starts[own], primary.Head-starts[own]
	out := []edit.Range{primary}
	for i := range starts {
		if i == own {
			continue
		}

		a, h := starts[i]+da, starts[i]+dh
		if min(a, h) < starts[i] || max(a, h) > ends[i]+1 || max(a, h) > text.Len() {
			continue
		}

		out = append(out, edit.NewRange(a, h))
	}

	if len(out) < 2 {
		return edit.Selection{}, false
	}

	return edit.NewSelection(out, 0), true
}

// endLinkedEditing drops the cursors linked editing added, back to the
// one the user had.
func (e *Editor) endLinkedEditing() {
	if e.linked.added {
		doc := e.Doc()
		doc.SetSelection(edit.NewSelection([]edit.Range{doc.Selection().Primary()}, 0))
	}

	e.linked = linkedState{}
}
