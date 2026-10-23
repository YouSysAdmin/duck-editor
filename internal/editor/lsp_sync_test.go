package editor

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// TestLSPFullSync sends the whole text to a server that asked for full
// sync, without ranges.
func TestLSPFullSync(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n", func(fs *fakeServer) { fs.sync = lsp.SyncFull })
	e.feed(t, "ggOnew line<esc>")
	// Every key sends the whole text, the last one has it all.
	var last lsp.DidChangeParams
	for last.TextDocument.Version != e.Doc().version {
		last = waitChan(t, e, fs.changes)
		if len(last.ContentChanges) != 1 || last.ContentChanges[0].Range != nil {
			t.Fatalf("change %+v", last.ContentChanges)
		}
	}

	if last.ContentChanges[0].Text != "new line\npackage main\n" {
		t.Fatalf("text %q", last.ContentChanges[0].Text)
	}
}

// TestLSPNoSync sends no changes and no save to a server that takes
// none, and no didChange to one that only wants saves.
func TestLSPNoSync(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n", func(fs *fakeServer) {
		fs.sync = map[string]any{"openClose": true, "change": lsp.SyncNone, "save": false}
	})
	e.feed(t, "ggOnew line<esc>:w<ret>")
	if !e.Doc().IsModified() && e.Status().IsError {
		t.Fatalf("save failed: %v", e.Status().Text)
	}

	select {
	case change := <-fs.changes:
		t.Fatalf("didChange sent to a server without sync: %+v", change)
	case save := <-fs.saves:
		t.Fatalf("didSave sent to a server without save: %+v", save)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestLSPDidSaveText sends the text with didSave when the server asked
// for it.
func TestLSPDidSaveText(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n")
	doc := e.Doc()
	e.feed(t, "ggOx<esc>:w<ret>")
	if e.Status().IsError {
		t.Fatalf("save failed: %s", e.Status().Text)
	}

	if save := waitChan(t, e, fs.saves); save.URI != doc.uri || save.Text == nil || *save.Text != "// formatted\nx\npackage main\n" {
		t.Fatalf("save %s text %q", save.URI, *save.Text)
	}

	// The text is the saved one, the fake server formats on save.
	// Without includeText the text stays home.
	e2, fs2, _ := newLSPEditor(t, "package main\n", func(fs *fakeServer) {
		fs.sync = map[string]any{"openClose": true, "change": lsp.SyncIncremental, "save": true}
	})
	e2.feed(t, "ggOx<esc>:w<ret>")
	if save := waitChan(t, e2, fs2.saves); save.Text != nil {
		t.Fatalf("text sent without includeText: %q", *save.Text)
	}
}

// applyEdit sends a workspace/applyEdit for one document version and
// returns whether the editor applied it, else why not.
func (fs *fakeServer) applyEdit(t *testing.T, e *Editor, uri string, version int, text string) (bool, string) {
	t.Helper()

	type result struct {
		applied bool
		reason  string
	}

	applied := make(chan result, 1)
	params := map[string]any{"edit": map[string]any{"documentChanges": []any{map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": version},
		"edits":        []lsp.TextEdit{{NewText: text}},
	}}}}
	fs.conn.Request("workspace/applyEdit", params, func(raw jsontext.Value, err error) {
		var res struct {
			Applied       bool   `json:"applied"`
			FailureReason string `json:"failureReason"`
		}
		_ = json.Unmarshal(raw, &res)
		applied <- result{applied: err == nil && res.Applied, reason: res.FailureReason}
	})
	res := waitChan(t, e, applied)

	return res.applied, res.reason
}

// TestApplyEditChecksVersion refuses an edit made for an older text.
func TestApplyEditChecksVersion(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n")
	doc := e.Doc()
	if applied, reason := fs.applyEdit(t, e, doc.uri, 1, "// one\n"); !applied || doc.Line(0) != "// one" {
		t.Fatalf("current version refused: %s, text %q", reason, doc.Text().String())
	}

	// The edit above moved the document to version 2.
	applied, reason := fs.applyEdit(t, e, doc.uri, 1, "// stale\n")
	if applied || !strings.Contains(reason, "version 1, now 2") || doc.Line(0) != "// one" {
		t.Fatalf("stale edit: applied %v, reason %q, line %q", applied, reason, doc.Line(0))
	}
}

// showMessageRequest asks the editor a question and returns the answer
// the server got.
func (fs *fakeServer) showMessageRequest(message string, actions ...string) <-chan string {
	answer := make(chan string, 1)
	items := make([]lsp.MessageActionItem, len(actions))
	for i, a := range actions {
		items[i] = lsp.MessageActionItem{Title: a}
	}

	params := lsp.ShowMessageRequestParams{Type: 3, Message: message, Actions: items}
	fs.conn.Request("window/showMessageRequest", params, func(raw jsontext.Value, err error) {
		var item lsp.MessageActionItem
		if err != nil || string(raw) == "null" || json.Unmarshal(raw, &item) != nil {
			answer <- "<none>"

			return
		}

		answer <- item.Title
	})

	return answer
}

// TestShowMessageRequestMenu offers the answers of a server question in
// a menu, closing it answers nothing.
func TestShowMessageRequestMenu(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n")
	answer := fs.showMessageRequest("Restart the server?", "Yes", "No")
	pump(t, e, func() bool { return e.Menu != nil })
	if e.Menu.Title != "fake: Restart the server?" || len(e.Menu.Items) != 2 {
		t.Fatalf("menu %+v", e.Menu)
	}

	e.feed(t, "j<ret>")
	if got := answerOf(t, e, answer); got != "No" {
		t.Fatalf("answer %q", got)
	}

	answer = fs.showMessageRequest("Install the tool?", "Install")
	pump(t, e, func() bool { return e.Menu != nil })
	e.feed(t, "<esc>")
	if got := answerOf(t, e, answer); got != "<none>" || e.Menu != nil {
		t.Fatalf("after esc: answer %q, menu %v", got, e.Menu)
	}

	// Without answers the message only shows.
	answer = fs.showMessageRequest("Done")
	if got := answerOf(t, e, answer); got != "<none>" || e.Status().Text != "fake: Done" {
		t.Fatalf("plain message: answer %q, status %+v", got, e.Status())
	}
}

// TestRegisterCapability turns a dynamic registration into a capability
// the editor uses and refuses what it cannot do.
func TestRegisterCapability(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n", func(fs *fakeServer) {
		fs.sync = map[string]any{"openClose": true, "change": lsp.SyncIncremental}
	})
	doc := e.Doc()
	client := doc.clients[0].client
	if doc.clientFor(featInlayHints) != nil || client.SyncFor(doc.uri).Save {
		t.Fatal("inlay hints or save supported before registration")
	}

	err := fs.register(t, e, []lsp.Registration{
		{ID: "1", Method: "textDocument/inlayHint"},
		{ID: "2", Method: "textDocument/didSave", RegisterOptions: jsontext.Value(`{"includeText":true}`)},
	})
	if err != nil || doc.clientFor(featInlayHints) != client || !client.SyncFor(doc.uri).Save || !client.SyncFor(doc.uri).IncludeText {
		t.Fatalf("registration failed: %v, hints %v, sync %+v", err, doc.clientFor(featInlayHints) == client, client.SyncFor(doc.uri))
	}

	err = fs.register(t, e, []lsp.Registration{{ID: "3", Method: "workspace/didChangeWatchedFiles"}, {ID: "4", Method: "textDocument/hover"}})
	if err == nil {
		t.Fatal("file watching registered")
	}

	// A selector keeps a registration away from other documents.
	err = fs.register(t, e, []lsp.Registration{{ID: "5", Method: "textDocument/documentLink", RegisterOptions: jsontext.Value(`{"documentSelector":[{"language":"typescript"}]}`)}})
	if err != nil || doc.clientFor(featDocumentLinks) != nil {
		t.Fatalf("typescript registration reached a go document: %v", err)
	}

	err = fs.register(t, e, []lsp.Registration{{ID: "6", Method: "textDocument/documentLink", RegisterOptions: jsontext.Value(`{"documentSelector":[{"language":"go","scheme":"file"}]}`)}})
	if err != nil || doc.clientFor(featDocumentLinks) != client {
		t.Fatalf("go registration missed a go document: %v", err)
	}

	done := make(chan error, 1)
	fs.conn.Request("client/unregisterCapability", map[string]any{"unregisterations": []lsp.Unregistration{{ID: "1", Method: "textDocument/inlayHint"}}}, func(_ jsontext.Value, err error) { done <- err })
	if err := waitChan(t, e, done); err != nil || doc.clientFor(featInlayHints) != nil {
		t.Fatalf("unregister: %v, hints %v", err, doc.clientFor(featInlayHints))
	}
}

// TestCompletionSortTextBreaksTies orders equal matches by sortText.
func TestCompletionSortTextBreaksTies(t *testing.T) {
	m := &CompletionMenu{items: []lsp.CompletionItem{
		{Label: "fooB", SortText: "2"},
		{Label: "fooA", SortText: "1"},
	}}
	m.filter("foo")
	if len(m.filtered) != 2 || m.items[m.filtered[0]].Label != "fooA" {
		t.Fatalf("order %v", m.filtered)
	}
}

// answerOf pumps the editor until the server got its answer.
func answerOf(t *testing.T, e *Editor, answer <-chan string) string {
	t.Helper()

	return waitChan(t, e, answer)
}

// waitChan applies editor events until ch delivers a value.
func waitChan[T any](t *testing.T, e *Editor, ch <-chan T) T {
	t.Helper()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case v := <-ch:
			return v
		case r := <-e.Events():
			e.ApplyResult(r)
		case <-deadline:
			t.Fatal("timed out waiting for the server")

			var zero T

			return zero
		}
	}
}

// register sends a client/registerCapability and returns the answer.
func (fs *fakeServer) register(t *testing.T, e *Editor, regs []lsp.Registration) error {
	t.Helper()

	done := make(chan error, 1)
	fs.conn.Request("client/registerCapability", map[string]any{"registrations": regs}, func(_ jsontext.Value, err error) { done <- err })

	return waitChan(t, e, done)
}

// unregister sends a client/unregisterCapability and returns the answer.
func (fs *fakeServer) unregister(t *testing.T, e *Editor, ids ...string) error {
	t.Helper()

	var regs []lsp.Unregistration
	for _, id := range ids {
		regs = append(regs, lsp.Unregistration{ID: id})
	}

	done := make(chan error, 1)
	fs.conn.Request("client/unregisterCapability", map[string]any{"unregisterations": regs}, func(_ jsontext.Value, err error) { done <- err })

	return waitChan(t, e, done)
}

// TestReopenOnDidOpenRegistration opens the documents of a server that
// registers the open notification after the handshake, as servers on
// vscode-languageserver do, and closes them when it takes that back.
func TestReopenOnDidOpenRegistration(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n", func(fs *fakeServer) { fs.sync = map[string]any{} })
	doc := e.Doc()
	client := doc.clients[0].client
	e.feed(t, "ggOx<esc>")
	select {
	case <-fs.opened:
		t.Fatal("didOpen sent without openClose")
	case change := <-fs.changes:
		t.Fatalf("didChange sent without an open document: %+v", change)
	case <-time.After(100 * time.Millisecond):
	}

	err := fs.register(t, e, []lsp.Registration{
		{ID: "o", Method: "textDocument/didOpen"},
		{ID: "c", Method: "textDocument/didChange", RegisterOptions: jsontext.Value(`{"syncKind":2}`)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if uri := waitChan(t, e, fs.opened); uri != doc.uri || !client.Opened(doc.uri) {
		t.Fatalf("reopened %q", uri)
	}

	e.feed(t, "ggOy<esc>")
	if change := waitChan(t, e, fs.changes); len(change.ContentChanges) == 0 || change.ContentChanges[0].Range == nil {
		t.Fatalf("change after registration %+v", change)
	}

	if err := fs.unregister(t, e, "o"); err != nil || client.Opened(doc.uri) {
		t.Fatalf("unregister: %v, still open %v", err, client.Opened(doc.uri))
	}

	if uri := waitChan(t, e, fs.closed); uri != doc.uri {
		t.Fatalf("closed %q", uri)
	}
}

// TestApplyEditAbortsBeforeApplying leaves every document alone when one
// of the changes is stale, and names it.
func TestApplyEditAbortsBeforeApplying(t *testing.T) {
	e, fs, _ := newLSPEditor(t, "package main\n")
	doc := e.Doc()
	change := func(version int, text string) any {
		return map[string]any{
			"textDocument": map[string]any{"uri": doc.uri, "version": version},
			"edits":        []lsp.TextEdit{{NewText: text}},
		}
	}

	type result struct {
		Applied       bool   `json:"applied"`
		FailureReason string `json:"failureReason"`
		FailedChange  *int   `json:"failedChange"`
	}
	done := make(chan result, 1)
	params := map[string]any{"edit": map[string]any{"documentChanges": []any{change(1, "// a\n"), change(7, "// b\n")}}}
	fs.conn.Request("workspace/applyEdit", params, func(raw jsontext.Value, _ error) {
		var res result
		_ = json.Unmarshal(raw, &res)
		done <- res
	})
	res := waitChan(t, e, done)
	if res.Applied || res.FailedChange == nil || *res.FailedChange != 1 || doc.Line(0) != "package main" {
		t.Fatalf("result %+v, text %q", res, doc.Text().String())
	}
}

// TestWorkspaceFoldersRequest answers the root the server works in.
func TestWorkspaceFoldersRequest(t *testing.T) {
	e, fs, path := newLSPEditor(t, "package main\n")
	done := make(chan []lsp.WorkspaceFolder, 1)
	fs.conn.Request("workspace/workspaceFolders", nil, func(raw jsontext.Value, _ error) {
		var folders []lsp.WorkspaceFolder
		_ = json.Unmarshal(raw, &folders)
		done <- folders
	})
	folders := waitChan(t, e, done)
	if len(folders) != 1 || folders[0].URI != lsp.PathToURI(filepath.Dir(path)) {
		t.Fatalf("folders %+v", folders)
	}
}

// newRestartEditor lets the editor start its own servers, every start
// gets a fake one.
func newRestartEditor(t *testing.T) (*Editor, *[]*fakeServer) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
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
	fakes := &[]*fakeServer{}
	e.lspStart = func(opts lsp.Options, dispatch lsp.Dispatcher) (*lsp.Client, error) {
		fs, in, out, closer := startFakeServer(t)
		*fakes = append(*fakes, fs)
		c := lsp.NewClient(opts.Name, opts.Root, in, out, closer, dispatch)
		c.Initialize(opts.Config)

		return c, nil
	}
	if _, err := e.Open(path, ActionReplace); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(e.StopLanguageServers)

	return e, fakes
}

// ready pumps until every server of the document finished the handshake
// and received didOpen.
func ready(t *testing.T, e *Editor, fakes []*fakeServer) {
	t.Helper()

	doc := e.Doc()
	pump(t, e, func() bool {
		for _, c := range doc.Clients() {
			if !c.Ready() {
				return false
			}
		}

		return true
	})
	for _, fs := range fakes {
		waitChan(t, e, fs.opened)
	}
}

// TestLSPRestart brings the servers back after :lsp-stop and after a
// restart, with the document opened on the new ones.
func TestLSPRestart(t *testing.T) {
	e, fakes := newRestartEditor(t)
	doc := e.Doc()
	e.attachLSP(doc)
	want := len(doc.language.LanguageServers)
	if len(doc.Clients()) != want || len(*fakes) != want {
		t.Fatalf("%d servers started, want %d", len(doc.Clients()), want)
	}

	ready(t, e, *fakes)
	e.feed(t, ":lsp-stop<ret>")
	if len(doc.Clients()) != 0 || len(e.lspClients) != 0 || e.Status().IsError {
		t.Fatalf("after stop: %d clients, %d known, status %+v", len(doc.Clients()), len(e.lspClients), e.Status())
	}

	e.feed(t, ":lsp-restart<ret>")
	if len(doc.Clients()) != want || len(*fakes) != 2*want || e.Status().IsError {
		t.Fatalf("after restart from stopped: %d clients, %d starts, status %+v", len(doc.Clients()), len(*fakes), e.Status())
	}

	ready(t, e, (*fakes)[want:])
	old := doc.Clients()
	e.feed(t, ":lsp-restart<ret>")
	if len(doc.Clients()) != want || len(*fakes) != 3*want || slices.Contains(doc.Clients(), old[0]) {
		t.Fatalf("after restart from running: %d clients, %d starts", len(doc.Clients()), len(*fakes))
	}

	ready(t, e, (*fakes)[2*want:])
	// The exits of the stopped servers are no errors and keep the new
	// ones.
	pump(t, e, func() bool { return len(e.Events()) == 0 })
	if len(doc.Clients()) != want || e.Status().IsError || len(e.serverStops) != 0 {
		t.Fatalf("after exits: %d clients, status %+v, stops %v", len(doc.Clients()), e.Status(), e.serverStops)
	}
}

// TestCompletionResolve fills the selected item in through
// completionItem/resolve: its documentation shows beside the menu and
// its additional edits apply, also when Enter comes before the answer.
func TestCompletionResolve(t *testing.T) {
	e, _, _ := newLSPEditor(t, "package main\n\n", func(fs *fakeServer) { fs.resolve = true })
	doc := e.Doc()
	e.feed(t, "jifo")
	pump(t, e, func() bool { return e.Completion != nil })
	m := e.Completion
	if m.Doc() != nil {
		t.Fatal("documentation before a selection")
	}

	e.feed(t, "<C-n>")
	pump(t, e, func() bool { return len(m.resolving) == 0 })
	if lines := ui.Strings(m.Doc()); len(lines) != 3 || lines[0] != "func()" || lines[2] != "Does fooBar." {
		t.Fatalf("doc %q", lines)
	}

	e.feed(t, "<ret>")
	if e.Completion != nil || doc.Line(0) != "// import fooBar" || doc.Line(2) != "fooBar" {
		t.Fatalf("after accept: menu %v, text %q", e.Completion != nil, doc.Text().String())
	}

	// Enter right after the selection waits for the answer.
	e2, _, _ := newLSPEditor(t, "package main\n\n", func(fs *fakeServer) { fs.resolve = true })
	doc = e2.Doc()
	e2.feed(t, "jifo")
	pump(t, e2, func() bool { return e2.Completion != nil })
	m = e2.Completion
	e2.feed(t, "<C-n><ret>")
	if e2.Completion != m || !m.accepting {
		t.Fatalf("accept did not wait: menu %v, accepting %v", e2.Completion != nil, m.accepting)
	}

	pump(t, e2, func() bool { return e2.Completion == nil })
	if doc.Line(0) != "// import fooBar" || doc.Line(2) != "fooBar" {
		t.Fatalf("after the late answer: %q", doc.Text().String())
	}
}
