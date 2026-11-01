package editor

import (
	"context"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

// semanticState holds a document's semantic tokens from its server, as
// theme scopes over char ranges, kept in place through edits until the
// next answer replaces them.
type semanticState struct {
	spans []HighlightSpan

	// edits is the change count the spans were asked at, valid whether
	// they still need a request.
	edits int
	valid bool

	// seq identifies the latest request so a stale answer is dropped,
	// pending the change count it was sent at, -1 for none.
	seq     int
	pending int
	cancel  context.CancelFunc
}

// semanticDebounce delays the request after the last change, the tokens
// of every keystroke are not worth asking for.
const semanticDebounce = 200 * time.Millisecond

// invalidateSemanticTokens forces a new request on the next refresh.
func (d *Document) invalidateSemanticTokens() {
	d.semantic.valid = false
}

// semanticHighlights returns the token spans meeting the char range.
func (d *Document) semanticHighlights(startChar, endChar int) []HighlightSpan {
	var out []HighlightSpan
	for _, s := range d.semantic.spans {
		if s.End > startChar && s.Start < endChar {
			out = append(out, s)
		}
	}

	return out
}

// mapSemanticTokens keeps the spans on their words through an edit: text
// typed before a word moves it, text typed at its end does not join it.
func (d *Document) mapSemanticTokens(changes *edit.ChangeSet) {
	spans := d.semantic.spans[:0]
	for _, s := range d.semantic.spans {
		s.Start, s.End = changes.MapPos(s.Start, edit.After), changes.MapPos(s.End, edit.Before)
		if s.End > s.Start {
			spans = append(spans, s)
		}
	}

	d.semantic.spans = spans
}

// refreshSemanticTokens schedules a request for the focused document when
// its tokens are older than its text.
func (e *Editor) refreshSemanticTokens() {
	if !e.Config.Editor.LSP.SemanticTokens {
		return
	}

	doc := e.Doc()
	client := doc.clientFor(featSemanticTokens)
	if client == nil {
		return
	}

	st := &doc.semantic
	if st.valid && st.edits == doc.edits {
		return
	}

	if st.cancel != nil && st.pending == doc.edits {
		return
	}

	if st.cancel != nil {
		st.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	st.cancel, st.pending = cancel, doc.edits
	docID := doc.ID
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(semanticDebounce):
		case <-ctx.Done():
			return
		}

		emit(func(e *Editor) { e.requestSemanticTokens(docID) })
	})
}

// requestSemanticTokens asks the server for the tokens of the whole
// document and turns them into scoped char spans.
func (e *Editor) requestSemanticTokens(docID DocID) {
	doc, ok := e.Docs[docID]
	if !ok {
		return
	}

	client := doc.clientFor(featSemanticTokens)
	if client == nil {
		return
	}

	legend, ok := client.SemanticTokensLegend(doc.uri, doc.languageID())
	if !ok {
		return
	}

	st := &doc.semantic
	st.seq++
	seq, edits := st.seq, doc.edits
	client.SemanticTokensFull(doc.uri, func(res *lsp.SemanticTokens, err error) {
		cur, ok := e.Docs[docID]
		if !ok || cur.semantic.seq != seq {
			return
		}

		st := &cur.semantic
		st.cancel = nil
		if err != nil {
			return
		}

		if cur.edits != edits {
			st.valid = false
			e.refreshSemanticTokens()

			return
		}

		// No answer, a text the server cannot read: the spans mapped
		// through the edits stay until it can.
		if res != nil {
			st.spans = decodeSemanticTokens(cur, res.Data, legend, client.Encoding)
		}

		st.edits, st.valid = edits, true
	})
}

// decodeSemanticTokens turns the relative five number groups into spans.
func decodeSemanticTokens(doc *Document, data []int, legend lsp.SemanticTokensLegend, enc lsp.Encoding) []HighlightSpan {
	text := doc.Text()
	out := make([]HighlightSpan, 0, len(data)/5)
	line, char := 0, 0
	for i := 0; i+4 < len(data); i += 5 {
		if data[i] != 0 {
			char = 0
		}

		line += data[i]
		char += data[i+1]
		typ := data[i+3]
		if typ < 0 || typ >= len(legend.TokenTypes) {
			continue
		}

		var mods []string
		for bit, name := range legend.TokenModifiers {
			if data[i+4]&(1<<bit) != 0 {
				mods = append(mods, name)
			}
		}

		scope := lsp.SemanticScope(legend.TokenTypes[typ], mods)
		if scope == "" {
			continue
		}

		start, ok := lsp.LSPToPos(text, lsp.Position{Line: line, Character: char}, enc)
		if !ok {
			continue
		}

		end, ok := lsp.LSPToPos(text, lsp.Position{Line: line, Character: char + data[i+2]}, enc)
		if !ok || end <= start {
			continue
		}

		out = append(out, HighlightSpan{Start: start, End: end, Scope: scope})
	}

	return out
}
