package editor

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

// SignatureHelp is the signature popup shown while typing a call.
type SignatureHelp struct {
	// Label is the active signature, ParamStart and ParamEnd delimit the
	// active parameter in chars.
	Label      string
	ParamStart int
	ParamEnd   int

	// Docs holds the documentation lines of the signature or parameter.
	Docs []string

	// Index and Count locate the active signature among the overloads.
	Index int
	Count int

	raw   *lsp.SignatureHelp
	docID DocID
}

func init() {
	register("signature_help", "Show signature help", func(cx *Context) {
		cx.Editor.requestSignatureHelp(lsp.SignatureTriggerInvoked, "")
	})
}

// afterInsertSignature re-requests or triggers signature help after a
// typed char in insert mode.
func (e *Editor) afterInsertSignature(key keymap.Key) {
	if !e.Config.Editor.LSP.AutoSignatureHelp {
		return
	}

	doc := e.Doc()
	if e.SignatureHelp != nil && e.SignatureHelp.docID != doc.ID {
		e.SignatureHelp = nil
	}

	client := doc.clientFor(featSignatureHelp)
	if client == nil {
		return
	}

	ch, ok := key.Char()
	ok = ok && key.Mods == 0
	triggers, retriggers := client.SignatureTriggers()
	switch {
	case e.SignatureHelp != nil:
		// Any key asks again, the server closes the popup once the cursor
		// leaves the call, after Enter or an arrow too.
		kind := lsp.SignatureTriggerContentChange
		char := ""
		if ok && (slices.Contains(triggers, string(ch)) || slices.Contains(retriggers, string(ch))) {
			kind = lsp.SignatureTriggerCharacter
			char = string(ch)
		}

		e.scheduleSignatureHelp(kind, char)
	case ok && slices.Contains(triggers, string(ch)):
		e.scheduleSignatureHelp(lsp.SignatureTriggerCharacter, string(ch))
	}
}

// scheduleSignatureHelp asks for signatures a moment after the change,
// like completions after a trigger char, so the server answers from the
// new text. More typing in between asks again instead.
func (e *Editor) scheduleSignatureHelp(kind int, char string) {
	if e.signatureCancel != nil {
		e.signatureCancel()
	}

	delay := min(triggerCompletionDelay, time.Duration(e.Config.Editor.IdleTimeout)*time.Millisecond)
	if delay <= 0 {
		e.requestSignatureHelp(kind, char)

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.signatureCancel = cancel
	docID, edits := e.Doc().ID, e.Doc().edits
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}

		emit(func(e *Editor) {
			if e.Mode == Insert && e.Doc().ID == docID && e.Doc().edits == edits {
				e.requestSignatureHelp(kind, char)
			}
		})
	})
}

// requestSignatureHelp asks the server for the signatures at the cursor.
func (e *Editor) requestSignatureHelp(kind int, triggerChar string) {
	doc := e.Doc()
	client := e.clientFor(doc, featSignatureHelp)
	if client == nil {
		return
	}

	e.sigSeq++
	seq := e.sigSeq
	docID := doc.ID
	params := lsp.SignatureHelpParams{TextDocumentPositionParams: e.lspPosition(doc, client)}
	params.Context = &lsp.SignatureHelpContext{TriggerKind: kind, TriggerCharacter: triggerChar, IsRetrigger: e.SignatureHelp != nil}
	if e.SignatureHelp != nil {
		params.Context.ActiveSignatureHelp = e.SignatureHelp.raw
	}

	client.SignatureHelp(params, func(help *lsp.SignatureHelp, err error) {
		if seq != e.sigSeq || e.Mode != Insert || e.Doc().ID != docID {
			return
		}

		if err != nil || help == nil || len(help.Signatures) == 0 {
			e.SignatureHelp = nil

			return
		}

		e.SignatureHelp = newSignatureHelp(help, docID, e.Config.Editor.LSP.DisplaySignatureHelpDocs)
	})
}

// newSignatureHelp picks the active signature and parameter.
func newSignatureHelp(help *lsp.SignatureHelp, docID DocID, withDocs bool) *SignatureHelp {
	active := 0
	if help.ActiveSignature != nil && *help.ActiveSignature < len(help.Signatures) {
		active = *help.ActiveSignature
	}

	sig := help.Signatures[active]
	paramIdx := -1
	switch {
	case sig.ActiveParameter != nil:
		paramIdx = *sig.ActiveParameter
	case help.ActiveParameter != nil:
		paramIdx = *help.ActiveParameter
	}

	if (paramIdx < 0 || paramIdx >= len(sig.Parameters)) && len(sig.Parameters) > 0 {
		paramIdx = 0
	}

	sh := &SignatureHelp{Label: sig.Label, Index: active, Count: len(help.Signatures), raw: help, docID: docID}
	if withDocs {
		sh.Docs = markupLines(sig.Documentation)
	}

	if paramIdx >= 0 && paramIdx < len(sig.Parameters) {
		param := sig.Parameters[paramIdx]
		sh.ParamStart, sh.ParamEnd = parameterOffsets(sig.Label, param.Label)
		if withDocs {
			if lines := markupLines(param.Documentation); len(lines) > 0 {
				sh.Docs = append(lines, sh.Docs...)
			}
		}
	}

	return sh
}

// parameterOffsets resolves a parameter label to char offsets. Offset
// pairs count UTF-16 units per the protocol.
func parameterOffsets(label string, param jsontext.Value) (int, int) {
	var name string
	if json.Unmarshal(param, &name) == nil {
		if name == "" {
			return 0, 0
		}

		// Search after the opening paren so a parameter named like the
		// function, as in "x(x int)", matches the parameter.
		from := strings.IndexByte(label, '(') + 1
		idx := strings.Index(label[from:], name)
		if idx < 0 {
			return 0, 0
		}

		start := utf8.RuneCountInString(label[:from+idx])

		return start, start + utf8.RuneCountInString(name)
	}

	var pair [2]int
	if json.Unmarshal(param, &pair) != nil {
		return 0, 0
	}

	units := utf16.Encode([]rune(label))
	toChars := func(u int) int {
		u = min(max(u, 0), len(units))

		return len(utf16.Decode(units[:u]))
	}

	return toChars(pair[0]), toChars(pair[1])
}

// markupLines flattens a string or MarkupContent documentation value.
func markupLines(raw jsontext.Value) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var text string
	var mc lsp.MarkupContent
	markdown := false
	switch {
	case json.Unmarshal(raw, &text) == nil:
	case json.Unmarshal(raw, &mc) == nil:
		text, markdown = mc.Value, mc.Kind == "markdown"
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	if !markdown {
		return strings.Split(text, "\n")
	}

	return markdownLines(text)
}
