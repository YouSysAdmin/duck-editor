package editor

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

func init() {
	register("syntax_symbol_picker", "Open symbol picker from syntax information", syntaxSymbolPicker)
	register("syntax_workspace_symbol_picker", "Open workspace symbol picker from syntax information", syntaxWorkspaceSymbolPicker)
	register("select_references_to_symbol_under_cursor", "Select symbol references", selectReferences)
	register("insert_char_interactive", "Insert an interactively-chosen char", func(cx *Context) { charInteractive(cx, false) })
	register("append_char_interactive", "Append an interactively-chosen char", func(cx *Context) { charInteractive(cx, true) })
	register("copy_between_registers", "Copy between two registers", copyBetweenRegisters)
	register("file_explorer", "Open file explorer in workspace root", func(cx *Context) {
		fileExplorer(cx, cx.Editor.workspaceRoot())
	})
	register("file_explorer_in_current_buffer_directory", "Open file explorer at current buffer's directory", func(cx *Context) {
		// The file of the buffer comes selected.
		dir, path := cx.Editor.WorkingDir(), cx.Editor.Doc().Path()
		if path != "" {
			dir = filepath.Dir(path)
		}

		openExplorer(cx.Editor, dir, path, false, false)
	})
	register("file_explorer_in_current_directory", "Open file explorer at current working directory", func(cx *Context) {
		fileExplorer(cx, cx.Editor.WorkingDir())
	})
}

// symbolItems lists the tags of a document as picker items.
func symbolItems(doc *Document, withFile bool) []PickerItem {
	if doc.syntax == nil {
		return nil
	}

	text := doc.Text()
	var items []PickerItem
	for _, tag := range doc.syntax.Tags(text) {
		label := fmt.Sprintf("%-9s %s", tag.Kind, tag.Name)
		if withFile {
			label = fmt.Sprintf("%-9s %s  %s", tag.Kind, tag.Name, doc.displayName())
		}

		r := edit.NewRange(tag.Start, tag.End)
		items = append(items, PickerItem{Label: label, DocID: doc.ID, Path: doc.Path(), Line: text.CharToLine(tag.Start), Range: &r})
	}

	return items
}

func syntaxSymbolPicker(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	if doc.syntax == nil {
		e.SetError("Syntax tree is not available on this buffer")

		return
	}

	items := symbolItems(doc, false)
	if len(items) == 0 {
		e.SetStatus("no symbols found, the language has no tags query")

		return
	}

	e.openPicker(NewPicker("Symbols", items, openItem))
}

// syntaxWorkspaceSymbolPicker parses every file of the workspace in the
// background and lists the symbol definitions of the languages that
// have a tags query.
func syntaxWorkspaceSymbolPicker(cx *Context) {
	e := cx.Editor
	if e.Syntax == nil || e.Syntax.Languages() == nil {
		e.SetError("no language configuration loaded")

		return
	}

	root := e.workspaceRoot()
	loader := e.Syntax
	p := NewPicker("Workspace symbols", nil, openItem)
	p.previewBase = root
	opts := walkOptions(e)
	h := e.Host
	p.load = func(ctx context.Context, gen int, emit func(JobResult)) {
		batch := make([]PickerItem, 0, itemBatchSize)
		flush := func() {
			if len(batch) == 0 {
				return
			}

			emit(p.emitItems(gen, batch))
			batch = make([]PickerItem, 0, itemBatchSize)
		}
		exists := cachedExists(h)
		err := h.Walk(ctx, root, opts, func(rel string) bool {
			path := filepath.Join(root, rel)
			lang, ok := loader.Languages().ForPathOn(path, exists)
			if !ok {
				return true
			}

			info, err := h.Stat(path)
			if err != nil || info.Size > 1<<20 {
				return true
			}

			data, err := h.ReadFile(path)
			if err != nil {
				return true
			}

			text := buffer.NewRope(string(data))
			syn, err := syntax.New(loader, lang, text)
			if err != nil {
				return true
			}

			for _, tag := range syn.Tags(text) {
				r := edit.NewRange(tag.Start, tag.End)
				batch = append(batch, PickerItem{
					Label: fmt.Sprintf("%-9s %s  %s:%d", tag.Kind, tag.Name, rel, text.CharToLine(tag.Start)+1),
					Path:  path, Line: text.CharToLine(tag.Start), Range: &r,
				})
			}

			syn.Close()
			if len(batch) >= itemBatchSize {
				flush()
			}

			return ctx.Err() == nil
		})
		flush()
		reportJobError(emit, "listing symbols", err)
	}
	e.openPicker(p)
}

// selectReferences selects every occurrence of the symbol under the
// cursor reported by the language server.
func selectReferences(cx *Context) {
	e := cx.Editor
	doc, client, ok := requireClient(cx, featDocumentHighlight)
	if !ok {
		return
	}

	docID := doc.ID
	client.DocumentHighlights(e.lspPosition(doc, client), func(highlights []lsp.DocumentHighlight, err error) {
		if err != nil {
			e.SetError("%v", err)

			return
		}

		cur, ok := e.Docs[docID]
		if !ok || len(highlights) == 0 {
			return
		}

		text := cur.Text()
		cursor := cur.Selection().Primary().Cursor(text)
		var ranges []edit.Range
		primary := 0
		for _, h := range highlights {
			r, ok := lsp.LSPToRange(text, h.Range, client.Encoding)
			if !ok {
				continue
			}

			if r.Contains(cursor) {
				primary = len(ranges)
			}

			ranges = append(ranges, r)
		}

		if len(ranges) == 0 {
			return
		}

		cur.SetSelection(edit.NewSelection(ranges, primary))
	})
}

// charInteractive inserts the next typed char before or after every
// selection.
func charInteractive(cx *Context, after bool) {
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		ch, ok := key.Char()
		if !ok {
			return
		}

		doc := cx.Editor.Doc()
		text := doc.Text()
		s := string(ch)
		tx := edit.ChangeByAndWithSelection(text, doc.Selection(), func(r edit.Range) (edit.Change, edit.Range, bool) {
			pos := r.From()
			if after {
				pos = r.To()
			}

			return edit.Change{From: pos, To: pos, Text: s}, edit.Range{}, false
		})
		doc.Apply(&tx)
	})
}

// copyBetweenRegisters reads a source and a destination register key.
func copyBetweenRegisters(cx *Context) {
	e := cx.Editor
	e.SetStatus("copy from register:")
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		src, ok := key.Char()
		if !ok {
			return
		}

		values, ok := e.readRegister(src)
		if !ok {
			e.SetError("register %c is empty", src)

			return
		}

		e.SetStatus("copy register %c to:", src)
		cx.OnNextKey(func(cx *Context, key keymap.Key) {
			dst, ok := key.Char()
			if !ok {
				return
			}

			if err := e.writeRegister(dst, values); err != nil {
				e.SetError("%v", err)

				return
			}

			e.SetStatus("Copied register %c to %c", src, dst)
		})
	})
}
