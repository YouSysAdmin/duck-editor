package editor

import (
	"context"
	"encoding/json/v2"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/fuzzy"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

// completionTriggerLen is how many word chars start automatic completion.
const completionTriggerLen = 2

// CompletionMenu is the list of candidates shown while typing.
type CompletionMenu struct {
	items    []lsp.CompletionItem
	filtered []int
	selected int

	// start is the char index where the completed word begins.
	start      int
	incomplete bool
	client     *lsp.Client
	docID      DocID

	// Preview state while cycling with preview-completion-insert.
	previewed       bool
	previewOriginal string
	previewEnd      int
}

// Items returns the filtered candidates in display order.
func (m *CompletionMenu) Items() []lsp.CompletionItem {
	out := make([]lsp.CompletionItem, len(m.filtered))
	for i, idx := range m.filtered {
		out[i] = m.items[idx]
	}

	return out
}

// Selected returns the highlighted row.
func (m *CompletionMenu) Selected() int {
	return m.selected
}

// Start returns the char index where the completion text starts.
func (m *CompletionMenu) Start() int {
	return m.start
}

func (m *CompletionMenu) filter(prefix string) {
	pattern := fuzzy.Parse(prefix)
	type scored struct {
		idx   int
		score int
	}
	// Scattered matches show only when nothing matches tightly.
	var out, loose []scored
	for i, item := range m.items {
		label := item.FilterText
		if label == "" {
			label = item.Label
		}

		score, _, tight, ok := pattern.MatchTight(label)
		if !ok {
			continue
		}

		if item.Preselect {
			score += 1000
		}

		if tight {
			out = append(out, scored{i, score})
		} else {
			loose = append(loose, scored{i, score})
		}
	}

	if len(out) == 0 {
		out = loose
	}

	if !pattern.IsEmpty() {
		slices.SortStableFunc(out, func(a, b scored) int { return b.score - a.score })
	} else {
		slices.SortStableFunc(out, func(a, b scored) int {
			return strings.Compare(sortKey(m.items[a.idx]), sortKey(m.items[b.idx]))
		})
	}

	m.filtered = m.filtered[:0]
	for _, s := range out {
		m.filtered = append(m.filtered, s.idx)
	}

	// Nothing is selected until Tab or C-n picks an item, as in Helix,
	// unless the server preselects the first one.
	m.selected = -1
	if len(m.filtered) > 0 && m.items[m.filtered[0]].Preselect {
		m.selected = 0
	}
}

func sortKey(item lsp.CompletionItem) string {
	if item.SortText != "" {
		return item.SortText
	}

	return item.Label
}

// handleKey processes menu keys and reports whether it consumed the key.
func (m *CompletionMenu) handleKey(cx *Context, key keymap.Key) bool {
	e := cx.Editor
	switch {
	case key == keymap.Ctrl('n') || key == (keymap.Key{Code: keymap.KeyTab}) || key == (keymap.Key{Code: keymap.KeyDown}):
		if len(m.filtered) > 0 {
			m.selected = (m.selected + 1) % len(m.filtered)
			m.preview(e)
		}

		return true
	case key == keymap.Ctrl('p') || key == (keymap.Key{Code: keymap.KeyTab, Mods: keymap.ModShift}) || key == (keymap.Key{Code: keymap.KeyUp}):
		if len(m.filtered) > 0 {
			m.selected = (max(m.selected, 0) - 1 + len(m.filtered)) % len(m.filtered)
			m.preview(e)
		}

		return true
	case key == (keymap.Key{Code: keymap.KeyEnter}) || key == keymap.Ctrl('y'):
		m.revertPreview(e)
		if m.selected < 0 || m.selected >= len(m.filtered) {
			// Without a chosen item Enter is a line break.
			e.Completion = nil

			return false
		}

		m.apply(e, m.items[m.filtered[m.selected]])
		e.Completion = nil

		return true
	case key == (keymap.Key{Code: keymap.KeyEsc}):
		// Escape closes the menu and still leaves insert mode.
		m.revertPreview(e)
		e.Completion = nil

		return false
	case key == keymap.Ctrl('c'):
		m.revertPreview(e)
		e.Completion = nil

		return true
	}

	m.revertPreview(e)

	return false
}

// preview inserts the selected item in place of the typed prefix when
// preview-completion-insert is on, to be reverted before the next step.
func (m *CompletionMenu) preview(e *Editor) {
	if !e.Config.Editor.PreviewCompletionInsert {
		return
	}

	m.revertPreview(e)
	doc := e.Doc()
	if doc.ID != m.docID || len(m.filtered) == 0 {
		return
	}

	if m.selected < 0 {
		return
	}

	item := m.items[m.filtered[m.selected]]
	label := item.InsertText
	if label == "" {
		label = item.Label
	}

	if item.InsertTextFormat == lsp.InsertTextSnippet {
		label = snippetToText(label)
	}

	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)
	if cursor < m.start {
		return
	}

	m.previewOriginal = text.Slice(m.start, cursor)
	m.previewEnd = m.start + charCount(label)
	m.previewed = true
	tx := edit.ChangeText(text, []edit.Change{{From: m.start, To: cursor, Text: label}}).WithSelection(edit.PointSelection(m.previewEnd))
	doc.Apply(&tx)
}

// revertPreview restores the typed text under a preview.
func (m *CompletionMenu) revertPreview(e *Editor) {
	if !m.previewed {
		return
	}

	m.previewed = false
	doc := e.Doc()
	if doc.ID != m.docID {
		return
	}

	text := doc.Text()
	end := min(m.previewEnd, text.Len())
	tx := edit.ChangeText(text, []edit.Change{{From: m.start, To: end, Text: m.previewOriginal}}).WithSelection(edit.PointSelection(m.start + charCount(m.previewOriginal)))
	doc.Apply(&tx)
}

var snippetPlaceholder = regexp.MustCompile(`\$\{\d+:([^}]*)\}|\$\{\d+\}|\$\d+`)

// snippetToText flattens a snippet to plain text, keeping placeholder
// defaults and dropping tab stops.
func snippetToText(s string) string {
	return snippetPlaceholder.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "${") && strings.Contains(m, ":") {
			return strings.TrimSuffix(strings.SplitN(m, ":", 2)[1], "}")
		}

		return ""
	})
}

// apply inserts the chosen item at every cursor.
func (m *CompletionMenu) apply(e *Editor, item lsp.CompletionItem) {
	doc := e.Doc()
	if doc.ID != m.docID {
		return
	}

	text := doc.Text()
	var enc lsp.Encoding
	if m.client != nil {
		enc = m.client.Encoding
	}

	newText := item.InsertText
	if newText == "" {
		newText = item.Label
	}

	// Default replace range: the typed word before the cursor, grown back
	// over chars that start the inserted text such as the $ of $var.
	primaryCursor := doc.Selection().Primary().Cursor(text)
	start, end := m.start, primaryCursor
	lineStart := text.LineToChar(text.CharToLine(primaryCursor))
	for start > lineStart && strings.HasPrefix(newText, text.Slice(start-1, primaryCursor)) {
		start--
	}

	fallback := start
	if len(item.TextEdit) > 0 {
		var te lsp.TextEdit
		var ire lsp.InsertReplaceEdit
		switch {
		case json.Unmarshal(item.TextEdit, &te) == nil && te.Range != (lsp.Range{}):
			if r, ok := lsp.LSPToRange(text, te.Range, enc); ok {
				start, end = r.From(), r.To()
			}

			newText = te.NewText
		case json.Unmarshal(item.TextEdit, &ire) == nil:
			if r, ok := lsp.LSPToRange(text, ire.Insert, enc); ok {
				start, end = r.From(), r.To()
			}

			newText = ire.NewText
		}

		// A range off the cursor line or after the cursor is unusable,
		// one that ends before it misses what was typed since the request.
		lineEnd := lineStart + charCount(text.LineWithoutEnding(text.CharToLine(primaryCursor)))
		if start < lineStart || start > primaryCursor || end > lineEnd {
			start, end = fallback, primaryCursor
		}

		end = max(end, primaryCursor)
	}

	// Edits are relative to the primary cursor, replicate the same
	// offsets at every other cursor.
	before := primaryCursor - start
	after := end - primaryCursor
	if e.Config.Editor.CompletionReplace {
		// Replace the rest of the word after the cursor as well.
		tail := primaryCursor
		for tail < text.Len() && edit.IsWordChar(text.Char(tail)) {
			tail++
		}

		after = max(after, tail-primaryCursor)
	}

	// Additional edits, like imports, refer to the text the server saw.
	// They go first and the cursors move with them, so the offsets above
	// stay valid.
	if len(item.AdditionalTextEdits) > 0 {
		extra := lsp.TransactionFromEdits(text, item.AdditionalTextEdits, enc)
		doc.Apply(&extra)
		text = doc.Text()
	}

	if item.InsertTextFormat == lsp.InsertTextSnippet && e.Config.Editor.LSP.Snippets {
		e.insertSnippet(doc, newText, before, after)
	} else {
		if item.InsertTextFormat == lsp.InsertTextSnippet {
			newText = snippetToText(newText)
		}

		tx := edit.ChangeByAndWithSelection(text, doc.Selection(), func(r edit.Range) (edit.Change, edit.Range, bool) {
			cursor := r.Cursor(text)
			from := max(cursor-before, 0)
			to := min(cursor+after, text.Len())

			return edit.Change{From: from, To: to, Text: newText}, edit.Range{}, false
		})
		doc.Apply(&tx)
	}

	if item.Command != nil {
		e.runCompletionCommand(m.client, *item.Command)

		return
	}

	if e.Config.Editor.LSP.AutoSignatureHelp {
		e.requestSignatureHelp(lsp.SignatureTriggerInvoked, "")
	}
}

// Commands of completion items the editor runs itself, servers send
// them to ask for the next step after an item was inserted.
const (
	commandTriggerParameterHints = "editor.action.triggerParameterHints"
	commandTriggerSuggest        = "editor.action.triggerSuggest"
)

// runCompletionCommand runs the command of an accepted item, editor
// commands here and the rest on the server.
func (e *Editor) runCompletionCommand(client *lsp.Client, cmd lsp.Command) {
	switch {
	case cmd.Command == commandTriggerParameterHints:
		e.requestSignatureHelp(lsp.SignatureTriggerInvoked, "")
	case cmd.Command == commandTriggerSuggest:
		e.requestCompletion(false)
	case client != nil:
		client.ExecuteCommand(cmd, func(err error) {
			if err != nil {
				e.SetError("%s: %v", cmd.Command, err)
			}
		})
	}
}

// afterInsertKey triggers or filters completion after a key in insert
// mode.
func (e *Editor) afterInsertKey(key keymap.Key) {
	if e.Mode != Insert {
		e.Completion = nil

		return
	}

	doc := e.Doc()
	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)

	if ch, ok := key.Char(); ok && key.Mods == 0 && ch == '/' && e.Config.Editor.PathCompletion && e.Config.Editor.AutoCompletion {
		// A new path segment lists the next directory.
		e.pathCompletion()

		return
	}

	if m := e.Completion; m != nil {
		if doc.ID != m.docID || cursor < m.start || m.client != nil && !wordText(text.Slice(m.start, cursor)) {
			// The cursor left the word the server completed, a trigger
			// char below may ask again.
			e.Completion = nil
		} else {
			m.filter(text.Slice(m.start, cursor))
			if m.incomplete {
				e.requestCompletion(false)
			}

			return
		}
	}

	ch, ok := key.Char()
	if !ok || key.Mods != 0 || !e.Config.Editor.AutoCompletion {
		return
	}

	triggerLen := e.Config.Editor.CompletionTriggerLen
	if triggerLen <= 0 {
		triggerLen = completionTriggerLen
	}

	client := doc.clientFor(featCompletion)
	if client != nil && slices.Contains(client.CompletionTriggers(), string(ch)) {
		e.scheduleTriggerCompletion()

		return
	}

	if edit.IsWordChar(ch) && wordStart(text, cursor) <= cursor-triggerLen {
		e.scheduleCompletion(client != nil)
	}
}

// wordText reports whether s holds word chars only, the text typed
// into a completed word.
func wordText(s string) bool {
	for _, r := range s {
		if !edit.IsWordChar(r) {
			return false
		}
	}

	return true
}

// completionTriggerLen is how many word chars open the menu while
// typing.
func (e *Editor) completionTriggerLen() int {
	if n := e.Config.Editor.CompletionTriggerLen; n > 0 {
		return n
	}

	return completionTriggerLen
}

// triggerCompletionDelay is how long a trigger char such as . waits for
// its request. A request right behind the change may be answered from
// the text before it, ruby-lsp answers fmt. with nothing then.
const triggerCompletionDelay = 50 * time.Millisecond

// scheduleTriggerCompletion asks for completions after a trigger char
// once triggerCompletionDelay passed without more typing.
func (e *Editor) scheduleTriggerCompletion() {
	if e.completionCancel != nil {
		e.completionCancel()
	}

	// An idle timeout of zero asks at once, a shorter one bounds it.
	delay := min(triggerCompletionDelay, time.Duration(e.Config.Editor.IdleTimeout)*time.Millisecond)
	if delay <= 0 {
		e.requestCompletion(true)

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.completionCancel = cancel
	docID, edits := e.Doc().ID, e.Doc().edits
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}

		emit(func(e *Editor) {
			if e.Mode == Insert && e.Doc().ID == docID && e.Doc().edits == edits {
				e.requestCompletion(true)
			}
		})
	})
}

// scheduleCompletion waits idle-timeout milliseconds of no typing
// before asking for completions.
func (e *Editor) scheduleCompletion(viaServer bool) {
	if e.completionCancel != nil {
		e.completionCancel()
	}

	timeout := time.Duration(e.Config.Editor.IdleTimeout) * time.Millisecond
	if timeout <= 0 {
		if viaServer {
			e.requestCompletion(false)
		} else {
			e.wordCompletion()
		}

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.completionCancel = cancel
	docID := e.Doc().ID
	edits := e.Doc().edits
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(timeout):
		case <-ctx.Done():
			return
		}

		emit(func(e *Editor) {
			if e.Mode != Insert || e.Doc().ID != docID || e.Doc().edits != edits {
				return
			}

			if viaServer {
				e.requestCompletion(false)
			} else {
				e.wordCompletion()
			}
		})
	})
}

// pathCompletion offers the entries of the directory typed before the
// cursor.
func (e *Editor) pathCompletion() {
	doc := e.Doc()
	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)
	start := cursor
	for start > 0 {
		ch := text.Char(start - 1)
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '"' || ch == '\'' || ch == '(' || ch == '<' || ch == '[' {
			break
		}

		start--
	}

	typed := text.Slice(start, cursor)
	dir := e.resolvePath(doc, typed)
	entries, err := e.hostOf(doc).ReadDir(dir)
	if err != nil {
		return
	}

	var items []lsp.CompletionItem
	for _, entry := range entries {
		name := entry.Name
		kind := lsp.CompletionKindFile
		if entry.IsDir() {
			name += "/"
			kind = lsp.CompletionKindFolder
		}

		items = append(items, lsp.CompletionItem{Label: name, Kind: kind, InsertText: name})
	}

	if len(items) == 0 {
		return
	}

	menu := &CompletionMenu{items: items, start: cursor, docID: doc.ID}
	menu.filter("")
	e.Completion = menu
}

// wordCompletion offers the other words of the document once the typed
// prefix reaches the word completion trigger length.
func (e *Editor) wordCompletion() {
	e.wordCompletionFrom(e.Config.Editor.WordCompletion.TriggerLength)
}

// wordCompletionFrom offers buffer words once minLen chars of the word
// are typed.
func (e *Editor) wordCompletionFrom(minLen int) {
	wc := e.Config.Editor.WordCompletion
	if !wc.Enable {
		return
	}

	doc := e.Doc()
	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)
	start := wordStart(text, cursor)
	if cursor-start < max(minLen, 1) {
		return
	}

	prefix := text.Slice(start, cursor)
	seen := map[string]bool{prefix: true}
	var items []lsp.CompletionItem
	for word := range wordsOf(text.String()) {
		if seen[word] || !strings.HasPrefix(strings.ToLower(word), strings.ToLower(prefix)) {
			continue
		}

		seen[word] = true
		items = append(items, lsp.CompletionItem{Label: word, Kind: lsp.CompletionKindText, InsertText: word})
	}

	if len(items) == 0 {
		return
	}

	menu := &CompletionMenu{items: items, start: start, docID: doc.ID}
	menu.filter(prefix)
	e.Completion = menu
}

// wordsOf yields the identifier like words of s in order.
func wordsOf(s string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		start := -1
		for i, ch := range s {
			if edit.IsWordChar(ch) {
				if start < 0 {
					start = i
				}

				continue
			}

			if start >= 0 {
				if !yield(s[start:i]) {
					return
				}

				start = -1
			}
		}

		if start >= 0 {
			yield(s[start:])
		}
	}
}

// wordStart returns the start of the word ending at pos.
func wordStart(text interface {
	Char(int) rune
}, pos int) int {
	start := pos
	for start > 0 && edit.IsWordChar(text.Char(start-1)) {
		start--
	}

	return start
}

// requestCompletion asks the server for candidates at the cursor.
func (e *Editor) requestCompletion(triggerChar bool) {
	doc := e.Doc()
	client := e.clientFor(doc, featCompletion)
	if client == nil {
		return
	}

	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)
	start := wordStart(text, cursor)
	docID := doc.ID
	version := doc.version

	params := lsp.CompletionParams{TextDocumentPositionParams: e.lspPosition(doc, client)}
	kind := lsp.CompletionInvoked
	if triggerChar {
		kind = lsp.CompletionTriggerCharacter
	}

	params.Context = &lsp.CompletionContext{TriggerKind: kind}
	if triggerChar && cursor > 0 {
		params.Context.TriggerCharacter = string(text.Char(cursor - 1))
	}

	client.Completion(params, func(list lsp.CompletionList, err error) {
		if err != nil || e.Mode != Insert || e.Doc().ID != docID {
			return
		}

		cur := e.Doc()
		if cur.version != version {
			// The text moved on, the menu would be stale.
			if e.Completion != nil {
				return
			}
		}

		if len(list.Items) == 0 {
			if e.Completion != nil && e.Completion.start == start {
				e.Completion = nil
			}

			// The server knows nothing here, as after a constant of an
			// unknown type, words of the buffer stand in.
			if e.Completion == nil && !triggerChar {
				e.wordCompletionFrom(e.completionTriggerLen())
			}

			return
		}

		menu := &CompletionMenu{items: list.Items, start: start, incomplete: list.IsIncomplete, client: client, docID: docID}
		curText := cur.Text()
		curPos := cur.Selection().Primary().Cursor(curText)
		if curPos < start || !wordText(curText.Slice(start, curPos)) {
			// Typing went past the word, as fmt.Println( before the
			// answer came.
			return
		}

		menu.filter(curText.Slice(start, curPos))
		if len(menu.filtered) == 0 {
			if e.Completion == nil && !triggerChar {
				e.wordCompletionFrom(e.completionTriggerLen())
			}

			return
		}

		e.Completion = menu
	})
}

func init() {
	register("completion", "Invoke completion popup", func(cx *Context) {
		if cx.Editor.Mode == Insert {
			cx.Editor.requestCompletion(false)
		}
	})
}
