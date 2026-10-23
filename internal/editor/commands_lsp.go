package editor

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// popupScrollStep is how many rows C-d and C-u scroll a popup.
const popupScrollStep = 5

// Popup is a transient text box shown near the cursor.
type Popup struct {
	Lines []ui.Line

	// Scroll is how many wrapped rows scrolled out at the top, C-d and
	// C-u move it.
	Scroll int
}

func init() {
	register("goto_definition", "Goto definition", func(cx *Context) {
		gotoLocations(cx, "textDocument/definition", featGotoDefinition)
	})
	register("goto_definition_hsplit", "Goto definition in a split below", func(cx *Context) {
		gotoLocationsIn(cx, "textDocument/definition", featGotoDefinition, ActionHorizontalSplit)
	})
	register("goto_definition_vsplit", "Goto definition in a split to the right", func(cx *Context) {
		gotoLocationsIn(cx, "textDocument/definition", featGotoDefinition, ActionVerticalSplit)
	})
	register("goto_declaration", "Goto declaration", func(cx *Context) {
		gotoLocations(cx, "textDocument/declaration", featGotoDeclaration)
	})
	register("goto_type_definition", "Goto type definition", func(cx *Context) {
		gotoLocations(cx, "textDocument/typeDefinition", featGotoType)
	})
	register("goto_implementation", "Goto implementation", func(cx *Context) {
		gotoLocations(cx, "textDocument/implementation", featGotoImpl)
	})
	register("goto_reference", "Goto references", func(cx *Context) {
		e := cx.Editor
		doc, client, ok := requireClient(cx, featGotoReference)
		if !ok {
			return
		}

		params := lsp.ReferenceParams{TextDocumentPositionParams: e.lspPosition(doc, client)}
		params.Context.IncludeDeclaration = true
		client.Locations("textDocument/references", params, func(locs []lsp.Location, err error) {
			showLocations(e, client, "References", locs, err)
		})
	})
	register("hover", "Show docs for item under cursor", hover)
	register("rename_symbol", "Rename symbol", renameSymbol)
	register("code_action", "Perform code action", codeAction)
	register("goto_next_diag", "Goto next diagnostic", func(cx *Context) { gotoDiag(cx, edit.Forward) })
	register("goto_prev_diag", "Goto previous diagnostic", func(cx *Context) { gotoDiag(cx, edit.Backward) })
	register("goto_first_diag", "Goto first diagnostic", func(cx *Context) { gotoDiagEdge(cx, true) })
	register("goto_last_diag", "Goto last diagnostic", func(cx *Context) { gotoDiagEdge(cx, false) })
	register("diagnostics_picker", "Open diagnostic picker", func(cx *Context) { diagnosticsPicker(cx, false) })
	register("workspace_diagnostics_picker", "Open workspace diagnostic picker", func(cx *Context) {
		diagnosticsPicker(cx, true)
	})
	register("symbol_picker", "Open symbol picker", symbolPicker)
	register("lsp_or_syntax_symbol_picker", "Open symbol picker", func(cx *Context) {
		// Without a server the tags of the syntax tree stand in.
		if cx.Editor.clientFor(cx.Editor.Doc(), featDocumentSymbols) == nil {
			syntaxSymbolPicker(cx)

			return
		}

		symbolPicker(cx)
	})
	register("workspace_symbol_picker", "Open workspace symbol picker", workspaceSymbolPicker)
	register("lsp_or_syntax_workspace_symbol_picker", "Open workspace symbol picker", func(cx *Context) {
		if cx.Editor.clientFor(cx.Editor.Doc(), featWorkspaceSymbols) == nil {
			syntaxWorkspaceSymbolPicker(cx)

			return
		}

		workspaceSymbolPicker(cx)
	})
	register("format_selections", "Format document", func(cx *Context) { formatDocument(cx.Editor, nil) })

	typedCommands = append(typedCommands, typedCommand{
		names: []string{"format", "fmt"},
		doc:   "Format the file using an external formatter or language server",
		run: func(cx *Context, _ []string) error {
			formatDocument(cx.Editor, nil)

			return nil
		},
	}, typedCommand{
		names: []string{"lsp-stop"},
		doc:   "Stops the language servers that are used by the current doc",
		run: func(cx *Context, _ []string) error {
			e := cx.Editor
			if _, stopped := e.stopDocServers(e.Doc()); !stopped {
				return cmdError("no language server running for this buffer")
			}

			return nil
		},
	}, typedCommand{
		names: []string{"lsp-restart"},
		doc:   "Restarts the language servers used by the current doc, also after they stopped or failed",
		run: func(cx *Context, _ []string) error {
			e := cx.Editor
			doc := e.Doc()
			if doc.language == nil || len(doc.language.LanguageServers) == 0 {
				return cmdError("no language server configured for this buffer")
			}

			affected, _ := e.stopDocServers(doc)
			// Documents of the language whose servers went away earlier
			// get them back too.
			for _, d := range e.documentsByID() {
				if d.language == doc.language && len(d.clients) == 0 && !slices.Contains(affected, d) {
					affected = append(affected, d)
				}
			}

			for _, name := range doc.language.LanguageServers {
				delete(e.serverStops, name)
			}

			for _, d := range affected {
				e.attachLSP(d)
			}

			return nil
		},
	})
}

// stopDocServers shuts down the servers of a document and detaches them
// from every document, returning the documents that lost one and
// whether there was any.
func (e *Editor) stopDocServers(doc *Document) ([]*Document, bool) {
	clients := doc.Clients()
	var affected []*Document
	for _, client := range clients {
		for _, d := range e.documentsByID() {
			if slices.ContainsFunc(d.clients, func(dc docClient) bool { return dc.client == client }) && !slices.Contains(affected, d) {
				affected = append(affected, d)
			}
		}

		found := false
		for key, c := range e.lspClients {
			if c == client {
				e.stopClient(key, client)
				found = true
			}
		}

		if !found {
			// A client the exit handler already forgot still has to go.
			for _, d := range e.documentsByID() {
				d.removeClient(client)
			}

			client.Stop()
		}
	}

	return affected, len(clients) > 0
}

// requireClient returns the server offering a feature for the current
// document, reporting when there is none.
func requireClient(cx *Context, feature string) (*Document, *lsp.Client, bool) {
	doc := cx.Editor.Doc()
	client := cx.Editor.clientFor(doc, feature)
	if client == nil {
		switch {
		case len(doc.clients) == 0:
			cx.Editor.SetError("no language server available for this buffer")
		case !slices.ContainsFunc(doc.clients, func(dc docClient) bool { return dc.client.Ready() }):
			cx.Editor.SetError("language server is still starting")
		default:
			cx.Editor.SetError("no language server supports %s", feature)
		}

		return nil, nil, false
	}

	return doc, client, true
}

// jumpToLocation opens the location and selects its range.
func (e *Editor) jumpToLocation(loc lsp.Location, client *lsp.Client, action Action) {
	path := lsp.URIToPath(loc.URI)
	if path == "" {
		e.SetError("unsupported location %s", loc.URI)

		return
	}

	e.pushJump()
	if _, err := e.Open(path, action); err != nil {
		e.SetError("%v", err)

		return
	}

	doc := e.Doc()
	r, ok := lsp.LSPToRange(doc.Text(), loc.Range, client.Encoding)
	if !ok {
		return
	}

	doc.SetSelection(edit.SingleSelection(r.Anchor, r.Head))
	e.ensureCursorInViewCenter()
}

func locationLabel(e *Editor, loc lsp.Location) string {
	path := lsp.URIToPath(loc.URI)
	rel := path
	if cwd := e.WorkingDir(); strings.HasPrefix(path, cwd+"/") {
		rel = strings.TrimPrefix(path, cwd+"/")
	}

	snippet := ""
	if doc, ok := e.DocumentByPath(path); ok && loc.Range.Start.Line < doc.LineCount() {
		snippet = strings.TrimSpace(doc.Line(loc.Range.Start.Line))
	} else if preview := e.PreviewDocument(path); preview != nil && loc.Range.Start.Line < preview.LineCount() {
		snippet = strings.TrimSpace(preview.Line(loc.Range.Start.Line))
	}

	return fmt.Sprintf("%s:%d:%d %s", rel, loc.Range.Start.Line+1, loc.Range.Start.Character+1, snippet)
}

// showLocations jumps to a single location or opens a picker.
func showLocations(e *Editor, client *lsp.Client, title string, locs []lsp.Location, err error) {
	showLocationsIn(e, client, title, locs, err, ActionReplace)
}

// showLocationsIn is showLocations with the action for a single one.
func showLocationsIn(e *Editor, client *lsp.Client, title string, locs []lsp.Location, err error, action Action) {
	if err != nil {
		e.SetError("%v", err)

		return
	}

	switch len(locs) {
	case 0:
		e.SetError("no %s found", strings.ToLower(title))
	case 1:
		e.jumpToLocation(locs[0], client, action)
	default:
		items := make([]PickerItem, 0, len(locs))
		for i, loc := range locs {
			path := lsp.URIToPath(loc.URI)
			items = append(items, PickerItem{Label: locationLabel(e, loc), Path: path, Line: loc.Range.Start.Line, Index: i})
		}

		p := NewPicker(title, items, func(cx *Context, item PickerItem, action PickerAction) {
			cx.Editor.jumpToLocation(locs[item.Index], client, actionFor(action))
		})
		e.openPicker(p)
	}
}

func gotoLocations(cx *Context, method, feature string) {
	gotoLocationsIn(cx, method, feature, ActionReplace)
}

// gotoLocationsIn jumps to a single location as action says, several
// open in a picker.
func gotoLocationsIn(cx *Context, method, feature string, action Action) {
	e := cx.Editor
	doc, client, ok := requireClient(cx, feature)
	if !ok {
		return
	}

	// The answer lands in the view that asked, also when the focus moved
	// to another split meanwhile.
	title := strings.TrimPrefix(method, "textDocument/")
	origin := e.Focus
	client.Locations(method, e.lspPosition(doc, client), func(locs []lsp.Location, err error) {
		if _, ok := e.Views[origin]; !ok {
			return
		}

		e.focusView(origin)
		showLocationsIn(e, client, title, locs, err, action)
	})
}

func hover(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	diags := diagnosticLines(doc)
	client := e.clientFor(doc, featHover)
	if client == nil {
		// Without a server the diagnostics under the cursor still show.
		if len(diags) > 0 {
			e.Popup = &Popup{Lines: ui.PlainLines(diags)}

			return
		}

		requireClient(cx, featHover)

		return
	}

	client.Hover(e.lspPosition(doc, client), func(h *lsp.Hover, err error) {
		if err != nil {
			e.SetError("%v", err)

			return
		}

		var text []ui.Line
		if h != nil {
			text = hoverLines(h)
		}

		lines := ui.PlainLines(diags)
		if len(text) > 0 {
			if len(lines) > 0 {
				lines = append(lines, ui.Line{})
			}

			lines = append(lines, text...)
		}

		if len(lines) == 0 {
			e.SetStatus("No hover information")

			return
		}

		e.Popup = &Popup{Lines: lines}
	})
}

// diagnosticLines describes the diagnostics under the primary cursor for
// the hover popup, one "severity: message (source)" entry each.
func diagnosticLines(doc *Document) []string {
	cursor := doc.Selection().Primary().Cursor(doc.Text())
	var lines []string
	for _, d := range doc.diagnostics {
		if d.Range.From() > cursor || cursor > d.Range.To() {
			continue
		}

		msg := severityWord(d.Severity) + ": " + strings.TrimSpace(d.Message)
		if d.Source != "" {
			msg += " (" + d.Source + ")"
		}

		lines = append(lines, strings.Split(msg, "\n")...)
	}

	return lines
}

// hoverLines lays the hover contents out for the popup: markdown is
// rendered, plain text and marked code are shown as they are.
func hoverLines(h *lsp.Hover) []ui.Line {
	var out []ui.Line
	var mc lsp.MarkupContent
	var str string
	var many []jsontext.Value
	switch {
	case json.Unmarshal(h.Contents, &mc) == nil && mc.Value != "":
		if mc.Kind == "plaintext" {
			out = ui.PlainLines(strings.Split(mc.Value, "\n"))
		} else {
			out = ui.Markdown(mc.Value)
		}
	case json.Unmarshal(h.Contents, &str) == nil:
		out = ui.Markdown(str)
	case json.Unmarshal(h.Contents, &many) == nil:
		for _, raw := range many {
			var s string
			var marked struct {
				Language string `json:"language"`
				Value    string `json:"value"`
			}
			switch {
			case json.Unmarshal(raw, &s) == nil:
				out = append(out, ui.Markdown(s)...)
			case json.Unmarshal(raw, &marked) == nil:
				out = append(out, ui.Code(marked.Value)...)
			}
		}
	}

	for len(out) > 0 && out[len(out)-1].String() == "" && !out[len(out)-1].Rule {
		out = out[:len(out)-1]
	}

	return out
}

func renameSymbol(cx *Context) {
	e := cx.Editor
	doc, client, ok := requireClient(cx, featRename)
	if !ok {
		return
	}

	text := doc.Text()
	word := edit.TextObjectWord(text, doc.Selection().Primary(), edit.Inside, false)
	current := word.Fragment(text)
	position := e.lspPosition(doc, client)
	if client.PrepareRenameSupported(doc.uri, doc.languageID()) {
		// Let the server say what is being renamed before asking.
		client.PrepareRename(position, func(pr *lsp.PrepareRename, err error) {
			if err != nil {
				e.SetError("%v", err)

				return
			}

			if pr == nil {
				e.SetError("nothing to rename at the cursor")

				return
			}

			placeholder := pr.Placeholder
			if placeholder == "" && pr.Range != nil {
				if r, ok := lsp.LSPToRange(doc.Text(), *pr.Range, client.Encoding); ok {
					placeholder = r.Fragment(doc.Text())
				}
			}

			if placeholder == "" {
				placeholder = current
			}

			renamePrompt(e, client, position, placeholder)
		})

		return
	}

	renamePrompt(e, client, position, current)
}

// renamePrompt asks for the new name and sends the rename.
func renamePrompt(e *Editor, client *lsp.Client, position lsp.TextDocumentPositionParams, current string) {
	e.openPrompt("rename-to:", func(cx *Context, input string, ev PromptEvent) {
		if ev != PromptValidate || input == "" {
			return
		}

		client.Rename(lsp.RenameParams{TextDocumentPositionParams: position, NewName: input}, func(we *lsp.WorkspaceEdit, err error) {
			if err != nil {
				e.SetError("%v", err)

				return
			}

			if we == nil {
				e.SetError("rename produced no edits")

				return
			}

			if _, err := e.applyWorkspaceEdit(client, *we); err != nil {
				e.SetError("%v", err)
			}
		})
	})
	for _, r := range current {
		e.Prompt.input = append(e.Prompt.input, r)
	}

	e.Prompt.cursor = len(e.Prompt.input)
}

func codeAction(cx *Context) {
	e := cx.Editor
	doc, client, ok := requireClient(cx, featCodeAction)
	if !ok {
		return
	}

	text := doc.Text()
	primary := doc.Selection().Primary()
	params := lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: doc.uri},
		Range:        lsp.RangeToLSP(text, primary, client.Encoding),
	}
	params.Context.Diagnostics = []lsp.Diagnostic{}
	params.Context.TriggerKind = lsp.CodeActionInvoked
	for _, d := range doc.diagnostics {
		if d.Range.Overlaps(primary) || d.Range.Contains(primary.Cursor(text)) {
			params.Context.Diagnostics = append(params.Context.Diagnostics, d.raw)
		}
	}

	client.CodeActions(params, func(actions []lsp.CodeAction, err error) {
		if err != nil {
			e.SetError("%v", err)

			return
		}

		// except-code-actions of the server leaves kinds out.
		if e.Syntax != nil && e.Syntax.Languages() != nil {
			if srv, ok := e.Syntax.Languages().Server(client.Name); ok {
				actions = slices.DeleteFunc(actions, func(a lsp.CodeAction) bool { return srv.HidesCodeAction(a.Kind) })
			}
		}

		if len(actions) == 0 {
			e.SetStatus("No code actions available")

			return
		}

		// Fixes first, then refactorings, then actions on the whole
		// file or package, the order of the server within each group.
		slices.SortStableFunc(actions, func(a, b lsp.CodeAction) int {
			return cmp.Compare(actionRank(a), actionRank(b))
		})
		titles := make([]string, len(actions))
		kinds := make([]string, len(actions))
		for i, a := range actions {
			titles[i] = a.Title
			if a.Disabled != nil {
				titles[i] += " (disabled)"
			}

			kinds[i] = actionGroup(a.Kind)
		}

		e.openMenu(titles, func(cx *Context, index int) {
			applyCodeAction(cx.Editor, client, actions[index])
		})
		e.Menu.Kinds = kinds
	})
}

// actionGroup names the group of a code action kind for the menu.
func actionGroup(kind string) string {
	group, _, _ := strings.Cut(kind, ".")
	if group == "quickfix" {
		return "fix"
	}

	return group
}

// actionRank orders code actions: preferred fixes, fixes, refactorings,
// the rest, then source actions that work on the whole file or package.
func actionRank(a lsp.CodeAction) int {
	switch actionGroup(a.Kind) {
	case "fix":
		if a.IsPreferred {
			return 0
		}

		return 1
	case "refactor":
		return 2
	case "source":
		return 4
	}

	return 3
}

func applyCodeAction(e *Editor, client *lsp.Client, action lsp.CodeAction) {
	run := func(action lsp.CodeAction) {
		if action.Edit != nil {
			if _, err := e.applyWorkspaceEdit(client, *action.Edit); err != nil {
				e.SetError("%v", err)

				return
			}
		}

		if action.Command != nil {
			client.ExecuteCommand(*action.Command, func(err error) {
				if err != nil {
					e.SetError("%v", err)
				}
			})
		}
	}
	if action.Edit == nil && action.Command == nil {
		client.ResolveCodeAction(action, func(resolved lsp.CodeAction, err error) {
			if err != nil {
				e.SetError("%v", err)

				return
			}

			run(resolved)
		})

		return
	}

	run(action)
}

// formatDocument asks the server to format and applies the edits. then
// runs after the edits are applied or when formatting is unavailable.
func formatDocument(e *Editor, then func()) {
	doc := e.Doc()
	if e.hasFormatter(doc) {
		e.formatExternal(doc, then)

		return
	}

	client := e.clientFor(doc, featFormat)
	if client == nil {
		if then == nil {
			e.SetError("no formatter available for this buffer")
		} else {
			then()
		}

		return
	}

	docID := doc.ID
	version := doc.version
	params := lsp.DocumentFormattingParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: doc.uri},
		Options:      lsp.FormattingOptions{TabSize: doc.TabWidth(), InsertSpaces: !doc.Indent().Tabs},
	}
	client.Formatting(params, func(edits []lsp.TextEdit, err error) {
		cur, ok := e.Docs[docID]
		if !ok {
			return
		}

		switch {
		case err != nil:
			e.SetError("formatting failed: %v", err)
		case cur.version != version:
			e.SetError("formatting skipped, the text changed meanwhile")
		case len(edits) > 0:
			// Some servers send the whole file as one edit, which would
			// move every cursor. Only the lines that changed are replaced.
			tx := lsp.TransactionFromEdits(cur.Text(), edits, client.Encoding)
			next := cur.Text()
			if tx.Apply(&next) {
				replaceByLines(cur, next.String())
				cur.AppendChangesToHistory()
			}
		}

		if then != nil {
			then()
		}
	})
}

func gotoDiag(cx *Context, dir edit.Direction) {
	e := cx.Editor
	doc := e.Doc()
	diags := doc.diagnostics
	if len(diags) == 0 {
		e.SetStatus("no diagnostics")

		return
	}

	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)
	var target *Diagnostic
	if dir == edit.Forward {
		for i := range diags {
			if diags[i].Range.From() > cursor {
				target = &diags[i]

				break
			}
		}

		if target == nil {
			target = &diags[0]
			e.SetStatus("Wrapped around document")
		}
	} else {
		for i := len(diags) - 1; i >= 0; i-- {
			if diags[i].Range.From() < cursor {
				target = &diags[i]

				break
			}
		}

		if target == nil {
			target = &diags[len(diags)-1]
			e.SetStatus("Wrapped around document")
		}
	}

	selectDiagnostic(e, *target)
}

func gotoDiagEdge(cx *Context, first bool) {
	e := cx.Editor
	diags := e.Doc().diagnostics
	if len(diags) == 0 {
		e.SetStatus("no diagnostics")

		return
	}

	if first {
		selectDiagnostic(e, diags[0])
	} else {
		selectDiagnostic(e, diags[len(diags)-1])
	}
}

func selectDiagnostic(e *Editor, d Diagnostic) {
	doc := e.Doc()
	r := d.Range
	if r.IsEmpty() {
		r = edit.NewRange(r.From(), r.From())
	}

	doc.SetSelection(edit.SingleSelection(r.Anchor, r.Head))
	e.ensureCursorInViewCenter()
	e.SetStatus("%s", d.Message)
}

// severityWord names a severity in full.
func severityWord(sev int) string {
	switch sev {
	case lsp.SeverityError:
		return "error"
	case lsp.SeverityWarning:
		return "warning"
	case lsp.SeverityInfo:
		return "info"
	default:
		return "hint"
	}
}

func severityName(sev int) string {
	switch sev {
	case lsp.SeverityError:
		return "E"
	case lsp.SeverityWarning:
		return "W"
	case lsp.SeverityInfo:
		return "I"
	default:
		return "H"
	}
}

func diagnosticsPicker(cx *Context, workspace bool) {
	e := cx.Editor
	docs := []*Document{e.Doc()}
	if workspace {
		docs = e.documentsByID()
	}

	items := []PickerItem{}
	for _, doc := range docs {
		text := doc.Text()
		for _, d := range doc.diagnostics {
			line := text.CharToLine(min(d.Range.From(), text.Len()))
			label := fmt.Sprintf("%s %s:%d %s", severityName(d.Severity), e.docName(doc), line+1, strings.ReplaceAll(d.Message, "\n", " "))
			items = append(items, PickerItem{Label: label, DocID: doc.ID, Path: doc.Path(), Line: line, Range: &edit.Range{Anchor: d.Range.Anchor, Head: d.Range.Head}})
		}
	}

	if workspace {
		items = append(items, e.closedDiagnosticItems()...)
	}

	if len(items) == 0 {
		e.SetStatus("no diagnostics")

		return
	}

	title := "Diagnostics"
	if workspace {
		title = "Workspace diagnostics"
	}

	p := NewPicker(title, items, openItem)
	e.openPicker(p)
}

// closedDiagnosticItems lists the diagnostics of files that are not open,
// as the servers reported them.
func (e *Editor) closedDiagnosticItems() []PickerItem {
	var items []PickerItem
	for _, path := range slices.Sorted(maps.Keys(e.closedDiagnostics)) {
		if _, open := e.DocumentByPath(path); open {
			continue
		}

		for _, diags := range e.closedDiagnostics[path] {
			for _, d := range diags {
				line := d.Range.Start.Line
				label := fmt.Sprintf("%s %s:%d %s", severityName(d.Severity), e.displayDir(path), line+1, strings.ReplaceAll(d.Message, "\n", " "))
				items = append(items, PickerItem{Label: label, Path: path, Line: line})
			}
		}
	}

	return items
}

func symbolPicker(cx *Context) {
	e := cx.Editor
	doc, client, ok := requireClient(cx, featDocumentSymbols)
	if !ok {
		return
	}

	client.DocumentSymbols(doc.uri, func(symbols []lsp.SymbolInformation, err error) {
		if err != nil {
			e.SetError("%v", err)

			return
		}

		entries := make([]symbolEntry, len(symbols))
		for i, s := range symbols {
			entries[i] = symbolEntry{info: s, client: client}
		}

		showSymbols(e, "Symbols", entries)
	})
}

// symbolEntry is a symbol with the server that reported it, positions
// are in that server's encoding.
type symbolEntry struct {
	info   lsp.SymbolInformation
	client *lsp.Client
}

func showSymbols(e *Editor, title string, entries []symbolEntry) {
	items := make([]PickerItem, 0, len(entries))
	cwd := e.WorkingDir()
	for i, entry := range entries {
		s := entry.info
		path := lsp.URIToPath(s.Location.URI)
		label := s.Name
		if s.ContainerName != "" {
			label = s.ContainerName + "." + s.Name
		}

		rel := path
		if r, ok := RelativeTo(cwd, path); ok {
			rel = r
		}

		items = append(items, PickerItem{
			Label: fmt.Sprintf("%-6s %s  %s:%d", symbolKind(s.Kind), label, rel, s.Location.Range.Start.Line+1),
			Path:  path,
			Line:  s.Location.Range.Start.Line,
			Index: i,
		})
	}

	p := NewPicker(title, items, func(cx *Context, item PickerItem, action PickerAction) {
		entry := entries[item.Index]
		cx.Editor.jumpToLocation(entry.info.Location, entry.client, actionFor(action))
	})
	e.openPicker(p)
}

// workspaceSymbolPicker asks every server that offers workspace symbols
// and shows the merged answers once all of them replied.
func workspaceSymbolPicker(cx *Context) {
	e := cx.Editor
	doc, _, ok := requireClient(cx, featWorkspaceSymbols)
	if !ok {
		return
	}

	clients := doc.clientsFor(featWorkspaceSymbols)
	e.openPrompt("symbol:", func(cx *Context, input string, ev PromptEvent) {
		if ev != PromptValidate {
			return
		}

		var entries []symbolEntry
		var errs []error
		seen := map[string]bool{}
		pending := len(clients)
		for _, client := range clients {
			client.WorkspaceSymbols(input, func(symbols []lsp.SymbolInformation, err error) {
				pending--
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", client.Name, err))
				}

				for _, s := range symbols {
					// Two servers often know the same symbol.
					key := fmt.Sprintf("%s|%s|%d|%d", s.Name, s.Location.URI, s.Location.Range.Start.Line, s.Location.Range.Start.Character)
					if !seen[key] {
						seen[key] = true
						entries = append(entries, symbolEntry{info: s, client: client})
					}
				}

				if pending > 0 {
					return
				}

				if len(entries) == 0 && len(errs) > 0 {
					e.SetError("%v", errors.Join(errs...))

					return
				}

				showSymbols(e, "Workspace symbols", entries)
			})
		}
	})
}

var symbolKinds = []string{"", "file", "module", "ns", "pkg", "class", "method", "prop", "field", "ctor", "enum", "iface", "func", "var", "const", "str", "num", "bool", "array", "obj", "key", "null", "member", "struct", "event", "op", "tparam"}

func symbolKind(k int) string {
	if k > 0 && k < len(symbolKinds) {
		return symbolKinds[k]
	}

	return "?"
}

// RelativeTo returns path relative to base when it lies inside base.
func RelativeTo(base, path string) (string, bool) {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return rel, true
}

// formatExternal pipes the text through the language formatter command
// in the background and applies the result as a line diff so selections
// survive.
func (e *Editor) formatExternal(doc *Document, then func()) {
	f := doc.language.Formatter
	docID := doc.ID
	edits := doc.edits
	text := doc.Text()
	input := text.String()
	// Formatters look for their config from the working directory, so
	// they run in the project root of the file.
	dir := ""
	if doc.Path() != "" {
		dir = lspRoot(e.hostOf(doc), doc.Path(), doc.language)
	}

	h := e.hostOf(doc)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		// A hung formatter must not hold up the save for good.
		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		stdout, stderr, err := h.Run(ctx, host.Command{Name: f.Command, Args: f.Args, Dir: dir, Stdin: []byte(input)})
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("no answer within %s", shellTimeout)
		}

		emit(func(e *Editor) {
			cur, ok := e.Docs[docID]
			if !ok {
				return
			}

			switch {
			case err != nil:
				msg := strings.TrimSpace(string(stderr))
				if msg == "" {
					msg = err.Error()
				}

				e.SetError("formatter %s failed: %s", f.Command, firstLines(msg, 3))
			case cur.edits != edits:
				e.SetError("formatting skipped, the text changed meanwhile")
			case len(stdout) == 0 && strings.TrimSpace(input) != "":
				// A formatter that writes nothing, or writes the file in
				// place, must not empty the buffer.
				e.SetError("formatter %s printed nothing, the text is left as it is", f.Command)
			default:
				replaceByLines(cur, string(stdout))
				cur.AppendChangesToHistory()
			}

			if then != nil {
				then()
			}
		})
	})
}

// hasFormatter reports whether the language of doc names a formatter
// that is installed on the host of the file. A missing one leaves the
// work to the language servers, like a missing server is skipped.
func (e *Editor) hasFormatter(doc *Document) bool {
	if doc.language == nil || doc.language.Formatter.Command == "" {
		return false
	}

	_, err := e.hostOf(doc).LookPath(doc.language.Formatter.Command)

	return err == nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}

	return strings.Join(lines, "\n")
}
