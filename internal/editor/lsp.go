package editor

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// Diagnostic is a server reported problem positioned in chars.
type Diagnostic struct {
	Range    edit.Range
	Severity int
	Message  string
	Source   string
	Code     string
	raw      lsp.Diagnostic
	client   *lsp.Client
}

// Language server feature names, as in the only-features and
// except-features lists of languages.yaml.
const (
	featFormat            = "format"
	featGotoDeclaration   = "goto-declaration"
	featGotoDefinition    = "goto-definition"
	featGotoType          = "goto-type-definition"
	featGotoReference     = "goto-reference"
	featGotoImpl          = "goto-implementation"
	featSignatureHelp     = "signature-help"
	featHover             = "hover"
	featDocumentHighlight = "document-highlight"
	featCompletion        = "completion"
	featCodeAction        = "code-action"
	featDocumentLinks     = "document-links"
	featWorkspaceCommand  = "workspace-command"
	featDocumentSymbols   = "document-symbols"
	featWorkspaceSymbols  = "workspace-symbols"
	featDiagnostics       = "diagnostics"
	featRename            = "rename-symbol"
	featInlayHints        = "inlay-hints"
	featLinkedEditing     = "linked-editing"
	featCallHierarchy     = "call-hierarchy"
)

// docClient is one language server attached to a document with the
// feature filter of its languages.yaml entry.
type docClient struct {
	client *lsp.Client
	only   []string
	except []string
}

func (dc docClient) allows(feature string) bool {
	if len(dc.only) > 0 {
		return slices.Contains(dc.only, feature)
	}

	return !slices.Contains(dc.except, feature)
}

// supportsFeature checks the server capability behind a feature.
func supportsFeature(c *lsp.Client, feature string) bool {
	caps := c.Capabilities
	switch feature {
	case featFormat:
		return lsp.Supports(caps.DocumentFormattingProvider)
	case featGotoDeclaration:
		return lsp.Supports(caps.DeclarationProvider)
	case featGotoDefinition:
		return lsp.Supports(caps.DefinitionProvider)
	case featGotoType:
		return lsp.Supports(caps.TypeDefinitionProvider)
	case featGotoReference:
		return lsp.Supports(caps.ReferencesProvider)
	case featGotoImpl:
		return lsp.Supports(caps.ImplementationProvider)
	case featSignatureHelp:
		return caps.SignatureHelpProvider != nil
	case featHover:
		return lsp.Supports(caps.HoverProvider)
	case featDocumentHighlight:
		return lsp.Supports(caps.DocumentHighlightProvider)
	case featCompletion:
		return caps.CompletionProvider != nil
	case featCodeAction:
		return lsp.Supports(caps.CodeActionProvider)
	case featDocumentLinks:
		return lsp.Supports(caps.DocumentLinkProvider)
	case featWorkspaceCommand:
		return caps.ExecuteCommandProvider != nil
	case featDocumentSymbols:
		return lsp.Supports(caps.DocumentSymbolProvider)
	case featWorkspaceSymbols:
		return lsp.Supports(caps.WorkspaceSymbolProvider)
	case featRename:
		return lsp.Supports(caps.RenameProvider)
	case featInlayHints:
		return lsp.Supports(caps.InlayHintProvider)
	case featLinkedEditing:
		return lsp.Supports(caps.LinkedEditingProvider)
	case featCallHierarchy:
		return lsp.Supports(caps.CallHierarchyProvider)
	case featDiagnostics:
		return true
	}

	return false
}

// clientFor returns the first ready attached server that offers a
// feature, nil when none does.
func (d *Document) clientFor(feature string) *lsp.Client {
	if clients := d.clientsFor(feature); len(clients) > 0 {
		return clients[0]
	}

	return nil
}

// clientsFor returns every ready attached server that offers a feature,
// in configuration order.
func (d *Document) clientsFor(feature string) []*lsp.Client {
	var out []*lsp.Client
	for _, dc := range d.clients {
		// Positions need the negotiated encoding, so only ready servers
		// take requests.
		if dc.allows(feature) && dc.client.Ready() && supportsFeature(dc.client, feature) {
			out = append(out, dc.client)
		}
	}

	return out
}

// Clients returns the attached language servers.
func (d *Document) Clients() []*lsp.Client {
	out := make([]*lsp.Client, 0, len(d.clients))
	for _, dc := range d.clients {
		out = append(out, dc.client)
	}

	return out
}

// Events delivers callbacks from background goroutines, such as LSP
// responses, to be run on the editor thread with ApplyResult.
func (e *Editor) Events() <-chan JobResult {
	if e.events == nil {
		e.events = make(chan JobResult, 256)
	}

	return e.events
}

// dispatch queues fn for the editor thread.
func (e *Editor) dispatch(fn func()) {
	e.Events()
	e.events <- func(*Editor) { fn() }
}

// lspRoot finds the project root for a document: the highest ancestor up
// to the nearest git root holding one of the language root markers, else
// the git root or the file directory.
func lspRoot(h host.Host, path string, lang *syntax.Language) string {
	dir := filepath.Dir(path)
	markers := append(slices.Clone(lang.Roots), ".git")
	var dirs, candidates []string
	for d := dir; ; d = filepath.Dir(d) {
		for _, marker := range markers {
			dirs = append(dirs, d)
			candidates = append(candidates, filepath.Join(d, marker))
		}

		if filepath.Dir(d) == d {
			break
		}
	}

	// Markers above the nearest repository belong to another project, a
	// stray package.json in the home directory must not win.
	found, _ := h.Exists(candidates)
	limit := len(found)
	gitRoot := ""
	for i, ok := range found {
		if ok && i%len(markers) == len(markers)-1 {
			gitRoot = dirs[i]
			limit = i + 1

			break
		}
	}

	best := ""
	for i, ok := range found[:limit] {
		if ok && i%len(markers) != len(markers)-1 {
			best = dirs[i]
		}
	}

	switch {
	case best != "":
		return best
	case gitRoot != "":
		return gitRoot
	default:
		return dir
	}
}

// lspRootFor prefers a configured workspace-lsp-roots entry holding the
// file, then the language root markers.
func (e *Editor) lspRootFor(doc *Document) string {
	abs := e.absPath(doc.Path())
	cwd := e.WorkingDir()
	for _, root := range e.Config.Editor.WorkspaceLspRoots {
		dir := root
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}

		if _, ok := RelativeTo(dir, abs); ok {
			return dir
		}
	}

	return lspRoot(e.hostOf(doc), abs, doc.language)
}

// clientFor attaches the document's servers on first use and returns the
// one offering the feature, or nil.
func (e *Editor) clientFor(doc *Document, feature string) *lsp.Client {
	if !doc.lspAttached {
		e.attachLSP(doc)
	}

	return doc.clientFor(feature)
}

// startServer returns the running server named name for the root,
// starting it when needed.
func (e *Editor) startServer(h host.Host, name, root string) *lsp.Client {
	srv, ok := e.Syntax.Languages().Server(name)
	if !ok || srv.Command == "" {
		return nil
	}

	if _, err := h.LookPath(srv.Command); err != nil {
		e.logf("language server %s: %v", name, err)

		return nil
	}

	key := h.Name() + "\x00" + name + "\x00" + root
	if e.lspClients == nil {
		e.lspClients = map[string]*lsp.Client{}
	}

	if client, ok := e.lspClients[key]; ok {
		return client
	}

	client, err := lsp.Start(lsp.Options{
		Name:        name,
		Command:     srv.Command,
		Args:        srv.Args,
		Environment: srv.Environment,
		Root:        root,
		Config:      srv.Config,
		Host:        h,
		NoSnippets:  !e.Config.Editor.LSP.Snippets,
	}, e.dispatch)
	if err != nil {
		e.SetError("%v", err)

		return nil
	}

	e.registerClient(key, client)
	client.OnShowDocument = func(p lsp.ShowDocumentParams) bool { return e.showDocument(h, client, p) }

	return client
}

// registerClient installs the editor side handlers of a client.
func (e *Editor) registerClient(key string, client *lsp.Client) {
	e.lspClients[key] = client
	client.OnDiagnostics = func(params lsp.PublishDiagnosticsParams) { e.handleDiagnostics(client, params) }
	client.OnMessage = func(kind int, text string) {
		e.logf("%s: %s", client.Name, text)
		if !e.Config.Editor.LSP.DisplayMessages {
			return
		}

		if kind <= 2 {
			e.SetError("%s: %s", client.Name, text)
		} else {
			e.SetStatus("%s: %s", client.Name, text)
		}
	}
	client.OnApplyEdit = func(we lsp.WorkspaceEdit) bool {
		return e.applyWorkspaceEdit(client, we) == nil
	}
	client.OnInlayHintRefresh = func() {
		for _, doc := range e.Docs {
			if doc.clientFor(featInlayHints) == client {
				doc.invalidateInlayHints()
			}
		}

		e.refreshInlayHints()
	}
	client.OnProgress = func(token string, p lsp.WorkDoneProgress) { e.handleProgress(client, token, p) }
	client.OnExit = func(err error) {
		for _, doc := range e.Docs {
			doc.removeClient(client)
		}

		// A restart may already have registered a new client under the key.
		if e.lspClients[key] == client {
			delete(e.lspClients, key)
		}

		e.clearProgress(client)
		if err != nil && e.hostGone(err) {
			e.logf("language server %s stopped with the connection", client.Name)

			return
		}

		if err != nil {
			e.logf("language server %s stopped: %v", client.Name, err)
			reason := err.Error()
			var exitErr *host.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
				out := strings.TrimSpace(string(exitErr.Stderr))
				e.logf("language server %s error output:\n%s", client.Name, out)
				reason += ", " + stderrReason(out)
			}

			// :info tells why a configured server is not there.
			if e.serverStops == nil {
				e.serverStops = map[string]string{}
			}

			e.serverStops[client.Name] = reason

			e.SetError("language server %s stopped: %s", client.Name, reason)
		}
	}
}

// attachLSP starts every configured and available server of the
// document language and opens the document on them.
func (e *Editor) attachLSP(doc *Document) {
	doc.lspAttached = true
	// Decrypted secrets stay away from language servers.
	if e.lspDisabled || !e.Config.Editor.LSP.Enable || e.Syntax == nil || doc.language == nil || doc.Path() == "" || doc.secret != nil {
		return
	}

	root := e.lspRootFor(doc)
	for _, name := range doc.language.LanguageServers {
		client := e.startServer(e.hostOf(doc), name, root)
		if client == nil {
			continue
		}

		filter := doc.language.ServerFeatures[name]
		e.attachClient(doc, docClient{client: client, only: filter.Only, except: filter.Except})
	}
}

// attachClient opens the document on one server.
func (e *Editor) attachClient(doc *Document, dc docClient) {
	for _, cur := range doc.clients {
		if cur.client == dc.client {
			return
		}
	}

	doc.clients = append(doc.clients, dc)
	if doc.uri == "" {
		doc.uri = lsp.PathToURI(doc.Path())
		doc.version = 1
	}

	langID := ""
	if doc.language != nil {
		langID = doc.language.LanguageID
		if langID == "" {
			langID = lsp.LanguageID(doc.language.Name)
		}
	}

	dc.client.DidOpen(doc.uri, langID, doc.version, doc.Text().String())
}

// removeClient forgets a server and its diagnostics.
func (d *Document) removeClient(client *lsp.Client) {
	d.clients = slices.DeleteFunc(d.clients, func(dc docClient) bool { return dc.client == client })
	d.diagnostics = slices.DeleteFunc(d.diagnostics, func(diag Diagnostic) bool { return diag.client == client })
}

// handleDiagnostics replaces the diagnostics a server published before.
func (e *Editor) handleDiagnostics(client *lsp.Client, params lsp.PublishDiagnosticsParams) {
	path := lsp.URIToPath(params.URI)
	doc, ok := e.DocumentByPath(path)
	if !ok {
		// Files that are not open count for the workspace picker.
		if e.closedDiagnostics == nil {
			e.closedDiagnostics = map[string]map[*lsp.Client][]lsp.Diagnostic{}
		}

		if e.closedDiagnostics[path] == nil {
			e.closedDiagnostics[path] = map[*lsp.Client][]lsp.Diagnostic{}
		}

		e.closedDiagnostics[path][client] = params.Diagnostics

		return
	}

	if !slices.ContainsFunc(doc.clients, func(dc docClient) bool { return dc.client == client && dc.allows(featDiagnostics) }) {
		return
	}

	if params.Version != nil && *params.Version != doc.version {
		return
	}

	text := doc.Text()
	diags := slices.DeleteFunc(doc.diagnostics, func(d Diagnostic) bool { return d.client == client })
	for _, d := range params.Diagnostics {
		r, ok := lsp.LSPToRange(text, d.Range, client.Encoding)
		if !ok {
			continue
		}

		diags = append(diags, Diagnostic{
			Range:    pullBackFromEOF(text, r),
			Severity: d.Severity,
			Message:  d.Message,
			Source:   d.Source,
			Code:     strings.Trim(string(d.Code), "\""),
			raw:      d,
			client:   client,
		})
	}

	sortDiagnostics(diags)
	doc.diagnostics = diags
}

func sortDiagnostics(diags []Diagnostic) {
	for i := 1; i < len(diags); i++ {
		for j := i; j > 0 && diags[j].Range.From() < diags[j-1].Range.From(); j-- {
			diags[j], diags[j-1] = diags[j-1], diags[j]
		}
	}
}

// Diagnostics returns the stored diagnostics sorted by position.
func (d *Document) Diagnostics() []Diagnostic {
	return d.diagnostics
}

// DiagnosticsBySeverity counts diagnostics as hint, info, warning,
// error.
func (d *Document) DiagnosticsBySeverity() [4]int {
	var out [4]int
	for _, diag := range d.diagnostics {
		switch diag.Severity {
		case lsp.SeverityError:
			out[3]++
		case lsp.SeverityWarning:
			out[2]++
		case lsp.SeverityInfo:
			out[1]++
		default:
			out[0]++
		}
	}

	return out
}

// syncChange sends an incremental change to every server and maps the
// diagnostics.
func (e *Editor) syncChange(doc *Document, oldText buffer.Rope, changes *edit.ChangeSet) {
	for i := range doc.diagnostics {
		d := &doc.diagnostics[i]
		start, end := d.Range.From(), d.Range.To()
		changes.UpdatePositions([]edit.PosAssoc{{Pos: &start, Assoc: edit.After}, {Pos: &end, Assoc: edit.Before}})
		if end < start {
			end = start
		}

		d.Range = edit.NewRange(start, end)
	}

	if len(doc.clients) == 0 {
		return
	}

	doc.version++
	for _, dc := range doc.clients {
		newText := doc.Text()
		dc.client.DidChange(doc.uri, doc.version, func(enc lsp.Encoding) []lsp.ContentChange {
			return lsp.ChangesetToChanges(oldText, newText, changes, enc)
		})
	}
}

// lspPosition converts the primary cursor to the encoding of a server.
func (e *Editor) lspPosition(doc *Document, client *lsp.Client) lsp.TextDocumentPositionParams {
	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)

	return lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: doc.uri},
		Position:     lsp.PosToLSP(text, cursor, client.Encoding),
	}
}

// applyWorkspaceEdit applies edits to open or loaded documents.
func (e *Editor) applyWorkspaceEdit(client *lsp.Client, we lsp.WorkspaceEdit) error {
	apply := func(uri string, edits []lsp.TextEdit) error {
		path := lsp.URIToPath(uri)
		if path == "" {
			return nil
		}

		doc, ok := e.DocumentByPath(path)
		if !ok {
			var err error
			doc, err = e.Open(path, ActionLoad)
			if err != nil {
				return err
			}
		}

		tx := lsp.TransactionFromEdits(doc.Text(), edits, client.Encoding)
		if len(edits) == 1 && doc.Text().LenBytes() < maxLineDiffBytes {
			// A server that replaces the whole file, as pylsp renames do,
			// gets diffed by lines so cursors and the view stay.
			scratch := NewDocument("", doc.Text().String())
			scratch.Apply(&tx)
			replaceByLines(doc, scratch.Text().String())
		} else {
			doc.Apply(&tx)
		}

		doc.AppendChangesToHistory()

		return nil
	}

	for uri, edits := range we.Changes {
		if err := apply(uri, edits); err != nil {
			return err
		}
	}

	// File creates, renames and deletes are not supported, the text
	// edits still apply and the rest is reported.
	skipped := 0
	for _, raw := range we.DocumentChanges {
		var tde lsp.TextDocumentEdit
		if err := json.Unmarshal(raw, &tde); err != nil || tde.TextDocument.URI == "" {
			skipped++

			continue
		}

		edits := make([]lsp.TextEdit, 0, len(tde.Edits))
		for _, rawEdit := range tde.Edits {
			var te lsp.TextEdit
			if json.Unmarshal(rawEdit, &te) == nil {
				edits = append(edits, te)
			}
		}

		if err := apply(tde.TextDocument.URI, edits); err != nil {
			return err
		}
	}

	if skipped > 0 {
		return fmt.Errorf("skipped %d unsupported file operations of the edit", skipped)
	}

	return nil
}

// closeLSP notifies every server that a document closed.
func (e *Editor) closeLSP(doc *Document) {
	for _, dc := range doc.clients {
		dc.client.DidClose(doc.uri)
	}

	doc.clients = nil
	doc.lspAttached = false
}

// StopLanguageServers shuts down all servers, called on exit.
func (e *Editor) StopLanguageServers() {
	for _, c := range e.lspClients {
		c.Stop()
	}
}

// progressKey identifies a task, tokens are unique per server only.
type progressKey struct {
	client *lsp.Client
	token  string
}

// progressItem is one running server task.
type progressItem struct {
	Title      string
	Message    string
	Percentage int
}

// handleProgress tracks $/progress reports for the statusline spinner.
func (e *Editor) handleProgress(client *lsp.Client, token string, p lsp.WorkDoneProgress) {
	if e.progress == nil {
		e.progress = map[progressKey]progressItem{}
	}

	key := progressKey{client: client, token: token}
	switch p.Kind {
	case "begin":
		e.progress[key] = progressItem{Title: p.Title, Message: p.Message, Percentage: p.Percentage}
	case "report":
		item := e.progress[key]
		if p.Message != "" {
			item.Message = p.Message
		}

		if p.Percentage > 0 {
			item.Percentage = p.Percentage
		}

		e.progress[key] = item
	case "end":
		delete(e.progress, key)
		if e.Config.Editor.LSP.DisplayProgressMessages && p.Message != "" {
			e.SetStatus("%s: %s", client.Name, p.Message)
		}

		return
	}

	if e.Config.Editor.LSP.DisplayProgressMessages {
		item := e.progress[key]
		text := item.Title
		if item.Message != "" {
			text += " " + item.Message
		}

		if item.Percentage > 0 {
			text += " " + strconv.Itoa(item.Percentage) + "%"
		}

		e.SetStatus("%s: %s", client.Name, strings.TrimSpace(text))
	}
}

func (e *Editor) clearProgress(client *lsp.Client) {
	maps.DeleteFunc(e.progress, func(key progressKey, _ progressItem) bool { return key.client == client })
}

// spinnerFrames animate while a server works.
var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Spinner returns the current spinner frame, empty when no server
// reports progress.
func (e *Editor) Spinner() string {
	if len(e.progress) == 0 {
		return ""
	}

	return spinnerFrames[(time.Now().UnixMilli()/100)%int64(len(spinnerFrames))]
}

// reloadLanguages swaps the language configuration and brings open
// documents and running servers up to date: documents get the new entry
// of their language, a language chosen with :set-language stays, servers
// whose definition changed or vanished restart and servers no document
// uses any more stop.
func (e *Editor) reloadLanguages(langs *syntax.Languages) {
	old := e.Syntax.Languages()
	e.Syntax.ReplaceLanguages(langs)

	for key, client := range e.lspClients {
		name, _, _ := strings.Cut(key, "\x00")
		before, _ := old.Server(name)
		after, ok := langs.Server(name)
		if !e.Config.Editor.LSP.Enable || !ok || !reflect.DeepEqual(before, after) {
			e.stopClient(key, client)
		}
	}

	for _, doc := range e.documentsByID() {
		if doc.tail != nil {
			// A tail buffer keeps no tree, see windowHighlights.
			if doc.language != nil {
				doc.language, _ = langs.Get(doc.language.Name)
			}

			continue
		}

		e.closeLSP(doc)
		if doc.language == nil {
			e.attachSyntax(doc)
		} else if lang, ok := langs.Get(doc.language.Name); ok {
			e.attachLanguage(doc, lang)
		} else {
			if doc.syntax != nil {
				doc.syntax.Close()
			}

			doc.language, doc.syntax = nil, nil
			e.attachSyntax(doc)
		}

		e.attachLSP(doc)
	}

	used := map[*lsp.Client]bool{}
	for _, doc := range e.Docs {
		for _, client := range doc.Clients() {
			used[client] = true
		}
	}

	for key, client := range e.lspClients {
		if !used[client] {
			e.stopClient(key, client)
		}
	}
}

// stopClient shuts a server down and forgets it, so the next attach
// starts a fresh one.
func (e *Editor) stopClient(key string, client *lsp.Client) {
	delete(e.lspClients, key)
	for _, doc := range e.Docs {
		doc.removeClient(client)
	}

	e.clearProgress(client)
	client.Stop()
}

// pullBackFromEOF moves a range that lies at the very end of a text
// ending in a line break onto that break. Servers report errors such as
// a missing closing quote there, on the empty line after the last one,
// where nothing can be seen or edited.
func pullBackFromEOF(text buffer.Rope, r edit.Range) edit.Range {
	end := text.Len()
	if r.From() < end || end == 0 || text.Char(end-1) != '\n' {
		return r
	}

	last := text.LenLines() - 2
	pos := text.LineEndCharIndex(last)

	return edit.NewRange(pos, pos)
}

// showDocument opens what a server asks for: a file of the server's host
// in the editor, with the given selection, anything else in the browser.
// A page a remote server serves on its loopback address cannot be reached
// from here and is reported instead.
func (e *Editor) showDocument(h host.Host, client *lsp.Client, p lsp.ShowDocumentParams) bool {
	if path := lsp.URIToPath(p.URI); path != "" && !p.External {
		e.pushJump()
		doc, err := e.openOn(h, path, ActionReplace)
		if err != nil {
			e.SetError("%v", err)

			return false
		}

		if p.Selection != nil {
			if r, ok := lsp.LSPToRange(doc.Text(), *p.Selection, client.Encoding); ok {
				doc.SetSelection(edit.SingleSelection(r.Anchor, r.Head))
				e.ensureCursorInViewCenter()
			}
		}

		return true
	}

	target := p.URI
	if h.Name() != "" && loopbackURL(target) {
		local, err := e.forwardURL(h, target)
		if err != nil {
			e.SetError("%s page on %s: %v", client.Name, h.Name(), err)

			return false
		}

		target = local
	}

	e.openExternal(target)

	return true
}

// loopbackURL reports whether a URL points at the machine it runs on.
func loopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}

	return false
}

// stderrReason picks the line of a server's error output that says what
// went wrong: one naming an error, else the last line. The output is the
// end of the stream, its first line may be cut.
func stderrReason(out string) string {
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		lower := strings.ToLower(l)
		if strings.Contains(lower, "error") || strings.Contains(lower, "not installed") || strings.Contains(lower, "not found") {
			return l
		}
	}

	return strings.TrimSpace(lines[len(lines)-1])
}
