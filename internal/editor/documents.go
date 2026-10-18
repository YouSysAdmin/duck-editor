package editor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/editorconfig"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// Action says where a document opens.
type Action int

// Open actions.
const (
	// ActionReplace shows the document in the focused view.
	ActionReplace Action = iota

	// ActionHorizontalSplit opens a new view below.
	ActionHorizontalSplit

	// ActionVerticalSplit opens a new view to the right.
	ActionVerticalSplit

	// ActionLoad opens the document without showing it.
	ActionLoad
)

func (e *Editor) addDocument(doc *Document) {
	e.nextDoc++
	doc.ID = e.nextDoc
	doc.onChange = e.onDocumentChange
	if doc.host == nil {
		doc.host = e.Host
	}

	e.Docs[doc.ID] = doc
	doc.onReadOnly = func() { e.SetError("%s is read only", doc.displayName()) }
	e.applyDefaultLineEnding(doc)
	e.attachSyntax(doc)
	e.applyEditorConfig(doc)
	e.attachLSP(doc)
	e.attachVCS(doc)
	e.restoreUndo(doc)
	e.detectSecret(doc)
}

// applyDefaultLineEnding sets the configured line ending on documents
// that have no line breaks yet.
func (e *Editor) applyDefaultLineEnding(doc *Document) {
	if doc.LineCount() > 1 {
		return
	}

	switch strings.ToLower(e.Config.Editor.DefaultLineEnding) {
	case "crlf":
		doc.lineEnding = buffer.CRLF
	case "lf", "native", "":
		doc.lineEnding = buffer.LF
	}
}

// applyEditorConfig overrides indent, line ending and save behavior from
// .editorconfig files when the option is on.
func (e *Editor) applyEditorConfig(doc *Document) {
	if !e.Config.Editor.EditorConfig || doc.Path() == "" {
		return
	}

	props := editorconfig.Resolve(e.hostOf(doc), e.absPath(doc.Path()))
	switch props.IndentStyle {
	case "tab":
		doc.indent = IndentStyle{Tabs: true}
		doc.indentDetected = true
	case "space":
		width := props.IndentSize
		if width <= 0 {
			width = doc.indent.IndentWidth(doc.tabWidth)
		}

		doc.indent = IndentStyle{Width: width}
		doc.indentDetected = true
	}

	if props.TabWidth > 0 {
		doc.tabWidth = props.TabWidth
	}

	switch props.EndOfLine {
	case "lf":
		doc.lineEnding = buffer.LF
	case "crlf":
		doc.lineEnding = buffer.CRLF
	}

	doc.ecTrimTrailing = props.TrimTrailingWhitespace
	doc.ecFinalNewline = props.InsertFinalNewline
	doc.ecTextWidth = props.MaxLineLength
}

func (e *Editor) newView(doc DocID) *View {
	e.nextView++
	v := &View{ID: e.nextView, DocID: doc}
	e.Views[v.ID] = v
	d := e.Docs[doc]
	if sel, ok := d.selections[0]; ok && d.focused == 0 {
		// A document created outside a view keeps the selection set on it.
		d.selections[v.ID] = sel
		delete(d.selections, 0)
	}

	d.ensureView(v.ID)
	v.Jumps = newJumplist(Jump{Doc: doc, Selection: d.SelectionFor(v.ID)})

	return v
}

// focusView makes a view current.
func (e *Editor) focusView(id ViewID) {
	v, ok := e.Views[id]
	if !ok {
		return
	}

	e.Focus = id
	doc := e.Docs[v.DocID]
	doc.focused = id
	e.accessClock++
	doc.lastAccess = e.accessClock
}

// onDocumentChange maps the jumplists of all views through a change and
// keeps the language server in sync.
func (e *Editor) onDocumentChange(doc *Document, oldText buffer.Rope, changes *edit.ChangeSet) {
	for _, v := range e.Views {
		v.Jumps.mapChanges(doc.ID, changes)
	}

	if doc.snippet != nil {
		doc.snippet.mapChanges(changes)
	}

	doc.mapInlayHints(changes)
	doc.mapAutoClosed(changes)
	e.syncChange(doc, oldText, changes)
	e.scheduleDiff(doc)
	e.scheduleAutoSave(doc)
}

// DocumentByPath finds an open document of the workspace host by its
// path.
func (e *Editor) DocumentByPath(path string) (*Document, bool) {
	return e.documentOn(e.Host, e.absPath(path))
}

// documentOn finds an open document by host and absolute path.
func (e *Editor) documentOn(h host.Host, abs string) (*Document, bool) {
	for _, d := range e.Docs {
		if d.path == "" || e.hostOf(d).Name() != h.Name() {
			continue
		}

		if d.fileHost().Name() == "" {
			if p, err := filepath.Abs(d.path); err == nil && p == abs {
				return d, true
			}

			continue
		}

		if d.path == abs {
			return d, true
		}
	}

	return nil, false
}

// Open loads a file, reusing an already open document, and shows it
// according to action.
func (e *Editor) Open(path string, action Action) (*Document, error) {
	return e.openOn(e.Host, e.absPath(path), action)
}

// OpenLocal opens a file of the machine the editor runs on, such as the
// user config, also in a session on a remote host.
func (e *Editor) OpenLocal(path string, action Action) (*Document, error) {
	abs, err := localPath(path)
	if err != nil {
		return nil, err
	}

	return e.openOn(host.Local{}, abs, action)
}

func (e *Editor) openOn(h host.Host, abs string, action Action) (*Document, error) {
	doc, ok := e.documentOn(h, abs)
	if !ok {
		var err error
		doc, err = OpenDocument(h, abs)
		if err != nil {
			return nil, err
		}

		e.addDocument(doc)
		if action != ActionLoad {
			e.Switch(doc.ID, action)
			e.restorePosition(doc)
			e.EnsureCursorInView()

			return doc, nil
		}
	}

	e.Switch(doc.ID, action)

	return doc, nil
}

// NewScratch creates an empty document and shows it.
func (e *Editor) NewScratch(action Action) *Document {
	doc := NewDocument("", "")
	e.addDocument(doc)
	e.Switch(doc.ID, action)

	return doc
}

// Switch shows an open document according to action.
func (e *Editor) Switch(id DocID, action Action) {
	doc, ok := e.Docs[id]
	if !ok {
		return
	}

	switch action {
	case ActionLoad:
		return
	case ActionReplace:
		v := e.View()
		if v.DocID != id {
			v.docHistory = append(v.docHistory, v.DocID)
			v.DocID = id
			v.FirstLine, v.FirstCol = 0, 0
			doc.ensureView(v.ID)
		}

		e.focusView(v.ID)
	case ActionHorizontalSplit, ActionVerticalSplit:
		layout := LayoutHorizontal
		if action == ActionVerticalSplit {
			layout = LayoutVertical
		}

		cur := e.View()
		v := e.newView(id)
		if cur.DocID == id {
			// A split of the same document starts where the cursor is.
			doc.selections[v.ID] = doc.SelectionFor(cur.ID)
			v.FirstLine, v.FirstCol = cur.FirstLine, cur.FirstCol
		}

		e.Tree.Split(cur.ID, v.ID, layout)
		e.layout()
		e.focusView(v.ID)
	}

	e.Mode = Normal
	e.EnsureCursorInView()
}

// CloseView removes a view. Closing the last view quits the editor.
func (e *Editor) CloseView(id ViewID) {
	if _, ok := e.Views[id]; !ok {
		return
	}

	if e.Tree.Len() <= 1 {
		e.quit = true

		return
	}

	for _, d := range e.Docs {
		d.removeView(id)
	}

	next := e.Tree.Prev(id)
	e.Tree.Remove(id)
	delete(e.Views, id)
	e.layout()
	if e.Focus == id {
		e.focusView(next)
	}
}

// bufferModifiedError reports a document with unsaved changes.
type bufferModifiedError struct{ name string }

func (err bufferModifiedError) Error() string {
	return fmt.Sprintf("buffer %s has unsaved changes", err.name)
}

// CloseDocument closes a buffer. Views showing it switch to their
// previous document or close.
func (e *Editor) CloseDocument(id DocID, force bool) error {
	doc, ok := e.Docs[id]
	if !ok {
		return errors.New("buffer does not exist")
	}

	if !force && doc.IsModified() {
		return bufferModifiedError{name: e.docName(doc)}
	}

	e.stopTail(doc)

	if doc.Path() != "" {
		e.rememberPosition(doc)
		e.closed = append(e.closed, closedBuffer{path: doc.Path(), selection: doc.Selection()})
		if len(e.closed) > maxClosedBuffers {
			e.closed = e.closed[len(e.closed)-maxClosedBuffers:]
		}
	}

	for _, vid := range e.Tree.Views() {
		v := e.Views[vid]
		v.docHistory = slices.DeleteFunc(v.docHistory, func(d DocID) bool { return d == id })
		v.Jumps.RemoveDoc(id)
		if v.DocID != id {
			continue
		}

		if n := len(v.docHistory); n > 0 {
			prev := v.docHistory[n-1]
			v.docHistory = v.docHistory[:n-1]
			v.DocID = prev
			v.FirstLine, v.FirstCol = 0, 0
			e.Docs[prev].ensureView(v.ID)
			if e.Focus == v.ID {
				e.focusView(v.ID)
			}

			continue
		}

		// Another open buffer takes the window, the one used last.
		var other *Document
		for _, d := range e.Docs {
			if d.ID != id && (other == nil || d.lastAccess > other.lastAccess) {
				other = d
			}
		}

		if other != nil {
			v.DocID = other.ID
			v.FirstLine, v.FirstCol = 0, 0
			other.ensureView(v.ID)
			if e.Focus == v.ID {
				e.focusView(v.ID)
			}

			continue
		}

		if e.Tree.Len() == 1 {
			// Keep the window open on a fresh scratch buffer.
			scratch := NewDocument("", "")
			e.addDocument(scratch)
			v.DocID = scratch.ID
			v.FirstLine, v.FirstCol = 0, 0
			scratch.ensureView(v.ID)
			if e.Focus == v.ID {
				e.focusView(v.ID)
			}

			continue
		}

		e.CloseView(v.ID)
	}

	if doc.syntax != nil {
		doc.syntax.Close()
	}

	for _, cancel := range []context.CancelFunc{doc.diff.cancel, doc.autoSaveCancel, doc.hints.cancel, doc.blame.cancel} {
		if cancel != nil {
			cancel()
		}
	}

	e.closeLSP(doc)
	delete(e.Docs, id)
	e.EnsureCursorInView()

	return nil
}

// Documents lists open documents in order of last access, most recent
// first.
func (e *Editor) Documents() []*Document {
	out := make([]*Document, 0, len(e.Docs))
	for _, d := range e.Docs {
		out = append(out, d)
	}

	slices.SortFunc(out, func(a, b *Document) int {
		switch {
		case a.lastAccess > b.lastAccess:
			return -1
		case a.lastAccess < b.lastAccess:
			return 1
		default:
			return int(a.ID - b.ID)
		}
	})

	return out
}

// documentsByID lists open documents in creation order.
func (e *Editor) documentsByID() []*Document {
	out := make([]*Document, 0, len(e.Docs))
	for _, d := range e.Docs {
		out = append(out, d)
	}

	slices.SortFunc(out, func(a, b *Document) int { return int(a.ID - b.ID) })

	return out
}

// pushJump saves the current position of the focused view.
func (e *Editor) pushJump() {
	doc := e.Doc()
	doc.AppendChangesToHistory()
	e.View().Jumps.Push(Jump{Doc: doc.ID, Selection: doc.Selection()})
}

func (e *Editor) jumpTo(j Jump) {
	if _, ok := e.Docs[j.Doc]; !ok {
		return
	}

	v := e.View()
	if v.DocID != j.Doc {
		e.Switch(j.Doc, ActionReplace)
	}

	e.Doc().SetSelection(j.Selection)
}

// JumpBackward moves count jumps back in the focused view.
func (e *Editor) JumpBackward(count int) {
	doc := e.Doc()
	v := e.View()
	current := Jump{Doc: doc.ID, Selection: doc.Selection()}
	if j, ok := v.Jumps.Backward(current, count); ok {
		e.jumpTo(j)
	}
}

// JumpForward moves count jumps forward in the focused view.
func (e *Editor) JumpForward(count int) {
	if j, ok := e.View().Jumps.Forward(count); ok {
		e.jumpTo(j)
	}
}

// PreviewDocument reads a file on the spot for a line of it, such as a
// location label. The picker preview goes through PreparePreview.
func (e *Editor) PreviewDocument(path string) *Document {
	if doc, ok := e.DocumentByPath(path); ok {
		return doc
	}

	if p, ok := e.Preview(path, false); ok {
		return p.Doc
	}

	doc, err := OpenDocument(e.Host, e.absPath(path))
	if err != nil {
		return nil
	}

	if e.previews == nil {
		e.previews = map[string]*Preview{}
	}

	// Labels need a line only, the preview highlights it when shown.
	e.previews[previewKey(path, false)] = &Preview{Doc: doc, plain: true}

	return doc
}

// dropPreviews frees cached preview documents, results of previews still
// loading are dropped when they arrive.
func (e *Editor) dropPreviews() {
	for _, p := range e.previews {
		if p != nil && p.Doc != nil && p.Doc.syntax != nil && p.Doc.ID == 0 {
			p.Doc.syntax.Close()
		}
	}

	e.previews = nil
	e.previewGen++
}
