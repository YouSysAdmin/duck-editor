package editor

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// fakeServer answers a few LSP requests over in process pipes.
type fakeServer struct {
	conn     *lsp.Conn
	opened   chan string
	changes  chan lsp.DidChangeParams
	saves    chan fakeSave
	closed   chan string
	requests chan string

	// sync is the textDocumentSync capability, incremental with save
	// and text when nil.
	sync any

	// format answers textDocument/formatting when set.
	format func() []lsp.TextEdit

	// resolve advertises resolveProvider and answers completionItem/resolve
	// with documentation and an import edit.
	resolve bool
}

// fakeSave is a didSave the fake server received.
type fakeSave struct {
	URI  string
	Text *string
}

func startFakeServer(t *testing.T, opts ...func(*fakeServer)) (*fakeServer, io.Reader, io.Writer, io.Closer) {
	t.Helper()

	clientIn, serverOut := io.Pipe()
	serverIn, clientOut := io.Pipe()
	fs := &fakeServer{
		opened: make(chan string, 8), changes: make(chan lsp.DidChangeParams, 64),
		saves: make(chan fakeSave, 8), closed: make(chan string, 8), requests: make(chan string, 64),
		sync: map[string]any{"openClose": true, "change": lsp.SyncIncremental, "save": map[string]any{"includeText": true}},
	}
	for _, opt := range opts {
		opt(fs)
	}

	fs.conn = lsp.NewConn(serverIn, serverOut, nil)
	fs.conn.OnNotify = func(method string, params jsontext.Value) {
		switch method {
		case "textDocument/didOpen":
			var p struct {
				TextDocument lsp.TextDocumentItem `json:"textDocument"`
			}
			_ = json.Unmarshal(params, &p)
			fs.opened <- p.TextDocument.URI
		case "textDocument/didChange":
			var p lsp.DidChangeParams
			_ = json.Unmarshal(params, &p)
			fs.changes <- p
		case "textDocument/didSave":
			var p struct {
				TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
				Text         *string                    `json:"text"`
			}
			_ = json.Unmarshal(params, &p)
			fs.saves <- fakeSave{URI: p.TextDocument.URI, Text: p.Text}
		case "textDocument/didClose":
			var p struct {
				TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
			}
			_ = json.Unmarshal(params, &p)
			fs.closed <- p.TextDocument.URI
		}
	}
	fs.conn.OnRequest = func(method string, params jsontext.Value) (any, *lsp.ResponseError) {
		fs.requests <- method
		switch method {
		case "initialize":
			return map[string]any{"capabilities": map[string]any{
				"positionEncoding":           "utf-8",
				"textDocumentSync":           fs.sync,
				"completionProvider":         map[string]any{"triggerCharacters": []string{"."}, "resolveProvider": fs.resolve},
				"definitionProvider":         true,
				"hoverProvider":              true,
				"documentFormattingProvider": true,
				"renameProvider":             true,
				"codeActionProvider":         true,
				"referencesProvider":         true,
				"linkedEditingRangeProvider": true,
				"callHierarchyProvider":      true,
			}}, nil
		case "textDocument/definition":
			var p lsp.TextDocumentPositionParams
			_ = json.Unmarshal(params, &p)

			return []lsp.Location{{URI: p.TextDocument.URI, Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 9}}}}, nil
		case "textDocument/hover":
			return lsp.Hover{Contents: jsontext.Value(`{"kind":"markdown","value":"` + "```go\\nfunc foo()\\n```\\nDocs here" + `"}`)}, nil
		case "textDocument/completion":
			return lsp.CompletionList{Items: []lsp.CompletionItem{
				{Label: "fooBar", Kind: 3, Detail: "func()"},
				{
					Label: "fooBaz", Kind: 6, InsertText: "fooBaz()",
					TextEdit:            jsontext.Value(`{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":2}},"newText":"fooBaz()"}`),
					AdditionalTextEdits: []lsp.TextEdit{{Range: lsp.Range{Start: lsp.Position{Line: 0}, End: lsp.Position{Line: 0}}, NewText: "// x\n"}},
				},
				{Label: "other"},
				{Label: "run", Command: &lsp.Command{Command: "fake.after"}},
			}}, nil
		case "completionItem/resolve":
			var item lsp.CompletionItem
			_ = json.Unmarshal(params, &item)
			item.Documentation = jsontext.Value(`{"kind":"markdown","value":"Does **` + item.Label + `**."}`)
			item.AdditionalTextEdits = []lsp.TextEdit{{Range: lsp.Range{}, NewText: "// import " + item.Label + "\n"}}

			return item, nil
		case "workspace/executeCommand":
			return nil, nil
		case "textDocument/linkedEditingRange":
			// The tag names of "<a>x</a>" on the first line.
			return lsp.LinkedEditingRanges{Ranges: []lsp.Range{
				{Start: lsp.Position{Line: 0, Character: 1}, End: lsp.Position{Line: 0, Character: 2}},
				{Start: lsp.Position{Line: 0, Character: 6}, End: lsp.Position{Line: 0, Character: 7}},
			}}, nil
		case "textDocument/formatting":
			if fs.format != nil {
				return fs.format(), nil
			}

			return []lsp.TextEdit{{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 0}}, NewText: "// formatted\n"}}, nil
		case "textDocument/rename":
			var p lsp.RenameParams
			_ = json.Unmarshal(params, &p)

			return lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				p.TextDocument.URI: {{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 9}}, NewText: p.NewName}},
			}}, nil
		case "textDocument/codeAction":
			var p lsp.CodeActionParams
			_ = json.Unmarshal(params, &p)
			edit := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				p.TextDocument.URI: {{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 0}}, NewText: "// fixed\n"}},
			}}

			actions := []lsp.CodeAction{{Title: "Do nothing", Kind: "quickfix"}, {Title: "Fix it", Kind: "quickfix", Edit: &edit}}
			// Like gopls, refactorings come only when the user asked.
			if p.Context.TriggerKind == lsp.CodeActionInvoked {
				actions = append(actions, lsp.CodeAction{Title: "Fill struct", Kind: "refactor.rewrite.fillStruct"})
			}

			return actions, nil
		case "codeAction/resolve":
			var a lsp.CodeAction
			_ = json.Unmarshal(params, &a)

			return a, nil
		case "textDocument/prepareCallHierarchy":
			var p lsp.TextDocumentPositionParams
			_ = json.Unmarshal(params, &p)
			r := lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 9}}

			return []lsp.CallHierarchyItem{{Name: "main", Kind: 12, URI: p.TextDocument.URI, Range: r, SelectionRange: r}}, nil
		case "callHierarchy/incomingCalls":
			var p struct {
				Item lsp.CallHierarchyItem `json:"item"`
			}
			_ = json.Unmarshal(params, &p)
			caller := lsp.CallHierarchyItem{Name: "run", URI: p.Item.URI, SelectionRange: lsp.Range{Start: lsp.Position{Line: 0}}}
			call := lsp.Range{Start: lsp.Position{Line: 3, Character: 1}, End: lsp.Position{Line: 3, Character: 2}}

			return []lsp.CallHierarchyCall{{From: caller, FromRanges: []lsp.Range{call}}}, nil
		case "shutdown":
			return nil, nil
		}

		return nil, &lsp.ResponseError{Code: -32601, Message: "unsupported " + method}
	}
	go fs.conn.Serve()

	return fs, clientIn, clientOut, clientOut
}

func (fs *fakeServer) publish(uri string, diags []lsp.Diagnostic) {
	_ = fs.conn.Notify("textDocument/publishDiagnostics", lsp.PublishDiagnosticsParams{URI: uri, Diagnostics: diags})
}

// pump applies editor events until cond holds or the timeout passes.
func pump(t *testing.T, e *Editor, cond func() bool) {
	t.Helper()

	deadline := time.After(3 * time.Second)
	for !cond() {
		select {
		case r := <-e.Events():
			e.ApplyResult(r)
		case <-deadline:
			t.Fatal("timed out waiting for editor event")
		}
	}
}

func newLSPEditor(t *testing.T, text string, opts ...func(*fakeServer)) (*Editor, *fakeServer, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	langs, err := syntax.LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Editor.IdleTimeout = 0
	e := New(NewDocument("", ""), cfg)
	e.Resize(80, 24)
	e.Syntax = syntax.NewLoader(nil, langs)
	e.lspDisabled = true

	fs, in, out, closer := startFakeServer(t, opts...)
	client := lsp.NewClient("fake", dir, in, out, closer, e.dispatch)
	client.Initialize(nil)
	if e.lspClients == nil {
		e.lspClients = map[string]*lsp.Client{}
	}

	e.registerClient("fake", client)

	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	pump(t, e, client.Ready)
	e.attachClient(doc, docClient{client: client})
	if client.SyncFor(doc.uri).OpenClose {
		select {
		case uri := <-fs.opened:
			if uri != doc.uri {
				t.Fatalf("opened %q want %q", uri, doc.uri)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("didOpen not received")
		}
	}

	t.Cleanup(func() { client.Stop() })

	return e, fs, path
}

func TestLSPDiagnosticsAndNavigation(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n\nfunc main() {\n\tx := 1\n}\n")
	doc := e.Doc()
	fs.publish(doc.uri, []lsp.Diagnostic{
		{Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 1}, End: lsp.Position{Line: 3, Character: 2}}, Severity: lsp.SeverityError, Message: "x declared and not used"},
		{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 7}}, Severity: lsp.SeverityWarning, Message: "warn"},
	})
	pump(t, e, func() bool { return len(doc.Diagnostics()) == 2 })
	var errs, warns int
	for _, d := range doc.Diagnostics() {
		switch d.Severity {
		case lsp.SeverityError:
			errs++
		case lsp.SeverityWarning:
			warns++
		}
	}

	if errs != 1 || warns != 1 {
		t.Fatalf("counts %d %d", errs, warns)
	}

	if doc.Diagnostics()[0].Message != "warn" {
		t.Fatal("diagnostics should be sorted by position")
	}

	e.feed(t, "]d")
	if got := e.annotated(); got != "package main\n\nfunc main() {\n\t#[x|]# := 1\n}\n" {
		t.Fatalf("]d = %q", got)
	}

	e.feed(t, "]d")
	if line := doc.Selection().Primary().CursorLine(doc.Text()); line != 0 {
		t.Fatalf("wrap to first diag, line %d", line)
	}

	// Edits shift diagnostics and are sent incrementally.
	e.feed(t, "ggOnew line<esc>")
	pump(t, e, func() bool { return len(fs.changes) > 0 })
	change := <-fs.changes
	if change.TextDocument.Version != 2 || len(change.ContentChanges) == 0 {
		t.Fatalf("change %+v", change)
	}

	if line := doc.Text().CharToLine(doc.Diagnostics()[1].Range.From()); line != 4 {
		t.Fatalf("diagnostic not shifted, line %d", line)
	}
}

func TestLSPGotoDefinitionHoverRename(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n\nfunc main() {\n\tx := 1\n}\n")
	doc := e.Doc()

	e.feed(t, "gd")
	pump(t, e, func() bool { return doc.Selection().Primary().From() != 0 })
	if got := doc.Selection().Primary().Fragment(doc.Text()); got != "main" {
		t.Fatalf("definition selected %q", got)
	}

	e.feed(t, "<C-o>")
	if doc.Selection().Primary().From() != 0 {
		t.Fatal("jumplist should return to the start")
	}

	e.feed(t, " k")
	pump(t, e, func() bool { return e.Popup != nil })
	if len(e.Popup.Lines) != 2 || e.Popup.Lines[0].String() != "func foo()" {
		t.Fatalf("popup %v", e.Popup.Lines)
	}

	e.feed(t, "j")
	if e.Popup != nil {
		t.Fatal("key should close popup")
	}

	e.feed(t, "k r")
	if e.Prompt == nil || e.Prompt.Input() != "package" {
		t.Fatalf("rename prompt %+v", e.Prompt)
	}

	e.feed(t, "<C-u>renamed<ret>")
	pump(t, e, func() bool { return doc.Line(2) != "func main() {" })
	if doc.Line(2) != "func renamed() {" {
		t.Fatalf("rename result %q", doc.Line(2))
	}

	_ = fs
}

func TestLSPCompletion(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n\n")
	doc := e.Doc()

	e.feed(t, "jifo")
	pump(t, e, func() bool { return e.Completion != nil })
	items := e.Completion.Items()
	if len(items) != 2 || items[0].Label != "fooBar" {
		t.Fatalf("items %v", items)
	}

	e.feed(t, "<C-n><C-n><ret>")
	if e.Completion != nil {
		t.Fatal("menu should close after accept")
	}

	if doc.Line(0) != "// x" || doc.Line(2) != "fooBaz()" {
		t.Fatalf("applied %q", doc.Text().String())
	}

	// Escape closes without applying and leaves insert mode, as in
	// Helix, and the trigger char requests again.
	e.feed(t, "<esc>ofo")
	pump(t, e, func() bool { return e.Completion != nil })
	e.feed(t, "<esc>")
	if e.Completion != nil || e.Mode != Normal {
		t.Fatal("esc should close the menu and leave insert mode")
	}

	e.feed(t, "a.")
	pump(t, e, func() bool { return e.Completion != nil })
	if len(e.Completion.Items()) != 4 {
		t.Fatalf("trigger char should list everything, got %d", len(e.Completion.Items()))
	}

	// Accepting an item with a command runs it on the server.
	e.feed(t, "ru")
	pump(t, e, func() bool { return e.Completion != nil && len(e.Completion.Items()) == 1 })
	e.feed(t, "<C-n><ret>")
	deadline := time.After(3 * time.Second)
	for {
		select {
		case method := <-fs.requests:
			if method == "workspace/executeCommand" {
				return
			}
		case <-deadline:
			t.Fatal("the item command was not executed")
		}
	}
}

func TestLSPFormatOnSave(t *testing.T) {
	e, _, path := newLSPEditor(t, "package main\n")
	doc := e.Doc()
	e.feed(t, "ix<esc>:w<ret>")
	pump(t, e, func() bool { return !doc.IsModified() })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "// formatted\nxpackage main\n" {
		t.Fatalf("saved %q", data)
	}
}

func TestLSPCodeActionMenu(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n")
	doc := e.Doc()
	e.feed(t, " a")
	pump(t, e, func() bool { return e.Menu != nil })
	if len(e.Menu.Items) != 3 || e.Menu.Items[2] != "Fill struct" || e.Picker != nil {
		t.Fatalf("menu %+v picker %v", e.Menu, e.Picker)
	}

	e.feed(t, "<C-n><ret>")
	pump(t, e, func() bool { return doc.Line(0) == "// fixed" })
	if e.Menu != nil {
		t.Fatal("menu should close")
	}

	e.feed(t, " a")
	pump(t, e, func() bool { return e.Menu != nil })
	e.feed(t, "<esc>")
	if e.Menu != nil {
		t.Fatal("esc closes the menu")
	}
}

func TestLSPLinkedEditing(t *testing.T) {
	e, _, _ := newLSPEditor(t, "<a>x</a>\n")
	doc := e.Doc()
	e.feed(t, "li")
	pump(t, e, func() bool { return doc.Selection().Len() == 2 })
	e.feed(t, "b")
	if got := doc.Text().String(); got != "<ba>x</ba>\n" {
		t.Fatalf("linked insert = %q", got)
	}

	e.feed(t, "<esc>")
	if doc.Selection().Len() != 1 {
		t.Fatal("leaving insert mode drops the linked cursors")
	}
}

func TestLSPShowDocument(t *testing.T) {
	e, _, path := newLSPEditor(t, "package main\n")
	client := e.lspClients["fake"]
	pump(t, e, func() bool { return client.Encoding != 0 })

	other := filepath.Join(filepath.Dir(path), "other.go")
	if err := os.WriteFile(other, []byte("package main\n\nfunc other() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sel := lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 10}}
	if !e.showDocument(host.Local{}, client, lsp.ShowDocumentParams{URI: lsp.PathToURI(other), Selection: &sel}) {
		t.Fatal("a file is shown in the editor")
	}

	doc := e.Doc()
	if doc.Path() != other || doc.Selection().Primary().Fragment(doc.Text()) != "other" {
		t.Fatalf("shown %q with %q selected", doc.Path(), doc.Selection().Primary().Fragment(doc.Text()))
	}

	// A page on the loopback address of a remote machine opens through a
	// local port that relays to it.
	opened := make(chan string, 1)
	e.openURL = func(_ context.Context, target string) error {
		opened <- target

		return nil
	}

	remote := host.NewRemote("ssh://box", nil)
	if !e.showDocument(remote, client, lsp.ShowDocumentParams{URI: "http://127.0.0.1:4321/free?pkg=main"}) {
		t.Fatalf("remote page not shown: %+v", e.Status())
	}

	runJobs(e)
	got := <-opened
	if !strings.HasPrefix(got, "http://127.0.0.1:") || strings.Contains(got, ":4321/") || !strings.HasSuffix(got, "/free?pkg=main") {
		t.Fatalf("opened %q", got)
	}
}

func TestLSPCodeActionExcept(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n")
	path := filepath.Join(t.TempDir(), "languages.yaml")
	if err := os.WriteFile(path, []byte("language-servers:\n  fake:\n    command: fake\n    except-code-actions: [refactor]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	langs, err := syntax.LoadLanguages(path)
	if err != nil {
		t.Fatal(err)
	}

	e.Syntax = syntax.NewLoader(nil, langs)
	e.feed(t, " a")
	pump(t, e, func() bool { return e.Menu != nil })
	if slices.Contains(e.Menu.Items, "Fill struct") || len(e.Menu.Items) != 2 {
		t.Fatalf("menu %v keeps an excluded kind", e.Menu.Items)
	}
}

func TestHoverMarkdownEscapes(t *testing.T) {
	t.Parallel()

	md := lsp.Hover{Contents: jsontext.Value(`{"kind":"markdown","value":"name, deploy\\_to, keep\\_releases \\*\n` + "```go\\nx := `a\\\\_b`\\n```" + `"}`)}
	if got := strings.Join(ui.Strings(hoverLines(&md)), "\n"); got != "name, deploy_to, keep_releases *\nx := `a\\_b`" {
		t.Fatalf("markdown hover = %q", got)
	}

	plain := lsp.Hover{Contents: jsontext.Value(`{"kind":"plaintext","value":"C:\\_dir"}`)}
	if got := strings.Join(ui.Strings(hoverLines(&plain)), "\n"); got != `C:\_dir` {
		t.Fatalf("plain hover = %q", got)
	}
}

// TestLSPGotoLandsInOriginView jumps in the view that asked, even when
// the focus moved to another split while the server answered.
func TestLSPGotoLandsInOriginView(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n\nfunc main() {\n\tx := 1\n}\n")
	origin := e.Focus
	e.feed(t, "gd<C-w>v")
	if e.Focus == origin {
		t.Fatal("split did not take the focus")
	}

	pump(t, e, func() bool { return e.Focus == origin })
	if got := e.Doc().Selection().Primary().Fragment(e.Doc().Text()); got != "main" {
		t.Fatalf("definition selected %q", got)
	}
}

// TestLSPRootStopsAtRepository ignores markers above the git root.
func TestLSPRootStopsAtRepository(t *testing.T) {
	t.Parallel()

	top := t.TempDir()
	repo := filepath.Join(top, "repo")
	pkg := filepath.Join(repo, "pkg")
	for _, dir := range []string{filepath.Join(repo, ".git"), pkg} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for _, marker := range []string{filepath.Join(top, "go.mod"), filepath.Join(pkg, "go.mod")} {
		if err := os.WriteFile(marker, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	lang := &syntax.Language{Roots: []string{"go.mod"}}
	if got := lspRoot(host.Local{}, filepath.Join(pkg, "a.go"), lang); got != pkg {
		t.Fatalf("root %q want %q", got, pkg)
	}

	if err := os.Remove(filepath.Join(pkg, "go.mod")); err != nil {
		t.Fatal(err)
	}

	if got := lspRoot(host.Local{}, filepath.Join(pkg, "a.go"), lang); got != repo {
		t.Fatalf("root %q want the repository %q", got, repo)
	}
}

// TestCompletionReplaceRange covers items whose text starts before the
// typed word and edit ranges that do not fit the cursor.
func TestCompletionReplaceRange(t *testing.T) {
	t.Parallel()

	apply := func(in string, start int, item lsp.CompletionItem) string {
		t.Helper()

		e := newTestEditor(t, in)
		m := &CompletionMenu{start: start, docID: e.Doc().ID}
		m.apply(e, item)

		return e.Doc().Text().String()
	}

	if got := apply("$va#[\n|]#", 1, lsp.CompletionItem{Label: "$var"}); got != "$var\n" {
		t.Fatalf("prefix before the word: %q", got)
	}

	// The range ends before the cursor, the chars typed since go too.
	te := jsontext.Value(`{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":2}},"newText":"abcdef"}`)
	if got := apply("abcd#[\n|]#", 0, lsp.CompletionItem{Label: "abcdef", TextEdit: te}); got != "abcdef\n" {
		t.Fatalf("short range: %q", got)
	}

	// A range on another line falls back to the typed word.
	te = jsontext.Value(`{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":1}},"newText":"abcdef"}`)
	if got := apply("abc#[\n|]#x\n", 0, lsp.CompletionItem{Label: "abcdef", TextEdit: te}); got != "abcdef\nx\n" {
		t.Fatalf("range off the line: %q", got)
	}
}

// TestLSPFormatKeepsCursor applies a whole file edit by changed lines so
// the cursor stays where it was.
func TestLSPFormatKeepsCursor(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n\nfunc a() {\n}\n\nfunc b() {\nx\n}\n")
	fs.format = func() []lsp.TextEdit {
		return []lsp.TextEdit{{
			Range:   lsp.Range{Start: lsp.Position{Line: 0}, End: lsp.Position{Line: 8}},
			NewText: "package main\n\nfunc a() {\n}\n\nfunc b() {\n\tx\n}\n",
		}}
	}
	doc := e.Doc()
	e.feed(t, "6j")
	e.feed(t, ":format<ret>")
	pump(t, e, func() bool { return strings.Contains(doc.Text().String(), "\tx") })
	if line := doc.Selection().Primary().CursorLine(doc.Text()); line != 6 {
		t.Fatalf("cursor moved to line %d", line)
	}
}

// TestHoverShowsDiagnostics puts the diagnostics under the cursor above
// the hover text, and shows them alone without a server.
func TestHoverShowsDiagnostics(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n\nfunc main() {\n\tx := 1\n}\n")
	doc := e.Doc()
	fs.publish(doc.uri, []lsp.Diagnostic{{
		Range:    lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 7}},
		Severity: lsp.SeverityWarning, Message: "watch out", Source: "vet",
	}})
	pump(t, e, func() bool { return len(doc.Diagnostics()) == 1 })
	e.feed(t, " k")
	pump(t, e, func() bool { return e.Popup != nil })
	want := []string{"warning: watch out (vet)", "", "func foo()", "Docs here"}
	if !slices.Equal(ui.Strings(e.Popup.Lines), want) {
		t.Fatalf("popup %q", e.Popup.Lines)
	}

	plain := newTestEditor(t, "#[a|]#bc")
	plain.Doc().diagnostics = []Diagnostic{{Range: edit.NewRange(0, 1), Severity: lsp.SeverityError, Message: "bad"}}
	plain.feed(t, " k")
	if plain.Popup == nil || !slices.Equal(ui.Strings(plain.Popup.Lines), []string{"error: bad"}) {
		t.Fatalf("popup without a server %+v status %+v", plain.Popup, plain.Status())
	}
}

// TestLSPIncomingCalls lists the callers and opens one at its call.
func TestLSPIncomingCalls(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n\nfunc main() {\n\tx := 1\n}\n")
	e.feed(t, " i")
	pump(t, e, func() bool { return e.Picker != nil })
	if e.Picker.Title != "Callers of main" || len(e.Picker.Items()) != 1 || !strings.HasPrefix(e.Picker.Items()[0].Label, "run ") {
		t.Fatalf("picker %q %+v", e.Picker.Title, e.Picker.Items())
	}

	e.feed(t, "<ret>")
	if got := e.annotated(); got != "package main\n\nfunc main() {\n\t#[x|]# := 1\n}\n" {
		t.Fatalf("opened at %q", got)
	}
}

// TestLSPGotoDefinitionSplit opens the definition in a new split.
func TestLSPGotoDefinitionSplit(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n\nfunc main() {\n\tx := 1\n}\n")
	e.feed(t, "<C-w>D")
	pump(t, e, func() bool { return len(e.Views) == 2 })
	if got := e.Doc().Selection().Primary().Fragment(e.Doc().Text()); got != "main" {
		t.Fatalf("definition selected %q", got)
	}
}

// TestCompletionClosesPastTheWord closes the menu on a char that ends
// the word, and drops an answer that comes after typing moved past it,
// as fmt.Println( before the server replied.
func TestCompletionClosesPastTheWord(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n\n")
	e.feed(t, "jo.")
	pump(t, e, func() bool { return e.Completion != nil })
	e.feed(t, "fo(")
	if e.Completion != nil {
		t.Fatal("( keeps the menu open")
	}

	e.feed(t, "<esc>o.x(")
	deadline := time.After(500 * time.Millisecond)
	for done := false; !done; {
		select {
		case r := <-e.Events():
			e.ApplyResult(r)
		case <-deadline:
			done = true
		}
	}

	if e.Completion != nil {
		t.Fatalf("a late answer opened the menu with %d items", len(e.Completion.Items()))
	}
}

// TestCompletionFallsBackToWords offers words of the buffer when the
// server has nothing for the typed prefix.
func TestCompletionFallsBackToWords(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n\n// setValue getValue\n")
	e.feed(t, "Goset")
	pump(t, e, func() bool { return e.Completion != nil })
	items := e.Completion.Items()
	if len(items) != 1 || items[0].Label != "setValue" {
		t.Fatalf("items %+v", items)
	}
}

// TestTriggerCharWaitsBeforeRequest asks for completions a moment after
// a trigger char, not in the same instant as the change it made.
func TestTriggerCharWaitsBeforeRequest(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n\n")
	e.Config.Editor.IdleTimeout = 250
	e.feed(t, "jo.")
	select {
	case method := <-fs.requests:
		if method == "textDocument/completion" {
			t.Fatal("completion was asked right with the change")
		}
	case <-time.After(20 * time.Millisecond):
	}

	runJobs(e)
	pump(t, e, func() bool { return e.Completion != nil })
}
