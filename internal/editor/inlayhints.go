package editor

import (
	"context"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

// InlayHint is virtual text anchored before a char position.
type InlayHint struct {
	Pos  int
	Text string
	Kind int
}

// inlayHintState tracks what the stored hints cover.
type inlayHintState struct {
	hints []InlayHint

	// from and to are the line range of the last request, edits the
	// change count it was made at.
	from, to int
	edits    int
	valid    bool

	// seq identifies the latest request so stale replies are dropped,
	// inflight describes the request still waiting for its reply.
	seq      int
	inflight *inlayRequest
	cancel   context.CancelFunc
}

type inlayRequest struct {
	from, to, edits int
}

// inlayHintDebounce delays the request after the last change or scroll.
const inlayHintDebounce = 150 * time.Millisecond

// InlayHints returns the hints known for the document.
func (d *Document) InlayHints() []InlayHint {
	return d.hints.hints
}

// invalidateInlayHints forces a new request on the next refresh.
func (d *Document) invalidateInlayHints() {
	d.hints.valid = false
}

// mapInlayHints keeps hint positions in place through an edit. Type
// hints stick to the text before them, parameter hints to the text
// after.
func (d *Document) mapInlayHints(changes *edit.ChangeSet) {
	for i := range d.hints.hints {
		h := &d.hints.hints[i]
		assoc := edit.After
		if h.Kind == lsp.InlayHintParameter {
			assoc = edit.Before
		}

		h.Pos = changes.MapPos(h.Pos, assoc)
	}
}

// refreshInlayHints schedules a hint request for the focused view when
// the stored hints no longer match the text or the visible lines.
func (e *Editor) refreshInlayHints() {
	if !e.Config.Editor.LSP.DisplayInlayHints {
		return
	}

	v := e.View()
	doc := e.Doc()
	client := doc.clientFor(featInlayHints)
	if client == nil {
		return
	}

	height := v.TextHeight()
	from := max(v.FirstLine-height, 0)
	to := min(v.FirstLine+2*height, doc.LineCount())
	st := &doc.hints
	visibleEnd := min(v.FirstLine+height, doc.LineCount())
	if st.valid && st.edits == doc.edits && st.from <= v.FirstLine && st.to >= visibleEnd {
		return
	}

	if r := st.inflight; r != nil && r.edits == doc.edits && r.from <= v.FirstLine && r.to >= visibleEnd {
		return
	}

	if st.cancel != nil {
		st.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	docID := doc.ID
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(inlayHintDebounce):
		case <-ctx.Done():
			return
		}

		emit(func(e *Editor) { e.requestInlayHints(docID, from, to) })
	})
}

// requestInlayHints asks for the hints of a line range.
func (e *Editor) requestInlayHints(docID DocID, from, to int) {
	doc, ok := e.Docs[docID]
	if !ok {
		return
	}

	client := doc.clientFor(featInlayHints)
	if client == nil {
		return
	}

	text := doc.Text()
	to = min(to, doc.LineCount())
	st := &doc.hints
	st.seq++
	seq := st.seq
	edits := doc.edits
	st.inflight = &inlayRequest{from: from, to: to, edits: edits}
	endChar := text.Len()
	if to < doc.LineCount() {
		endChar = text.LineToChar(to)
	}

	params := lsp.InlayHintParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: doc.uri},
		Range:        lsp.RangeToLSP(text, edit.NewRange(text.LineToChar(min(from, doc.LineCount()-1)), endChar), client.Encoding),
	}
	client.InlayHints(params, func(hints []lsp.InlayHint, err error) {
		cur, ok := e.Docs[docID]
		if !ok || cur.hints.seq != seq {
			return
		}

		cur.hints.inflight = nil
		if err != nil {
			return
		}

		if cur.edits != edits {
			// The text moved on, ask again for the current state.
			cur.hints.valid = false
			e.refreshInlayHints()

			return
		}

		curText := cur.Text()
		out := make([]InlayHint, 0, len(hints))
		for _, h := range hints {
			pos, ok := lsp.LSPToPos(curText, h.Position, client.Encoding)
			if !ok {
				continue
			}

			label := h.LabelText()
			if h.PaddingLeft {
				label = " " + label
			}

			if h.PaddingRight {
				label += " "
			}

			out = append(out, InlayHint{Pos: pos, Text: label, Kind: h.Kind})
		}

		// The debounce timer of a newer refresh keeps its cancel func.
		st := &cur.hints
		st.hints, st.from, st.to, st.edits, st.valid = out, from, to, edits, true
	})
}
