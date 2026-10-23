package lsp

import (
	"encoding/json/jsontext"
	"testing"
)

func TestParseSync(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]SyncOptions{
		``:                               {},
		`null`:                           {},
		`0`:                              {},
		`1`:                              {OpenClose: true, Change: SyncFull, Save: true},
		`2`:                              {OpenClose: true, Change: SyncIncremental, Save: true},
		`{"change":2}`:                   {Change: SyncIncremental},
		`{"openClose":false,"change":1}`: {Change: SyncFull},
		`{"openClose":true,"change":2,"save":true}`:      {OpenClose: true, Change: SyncIncremental, Save: true},
		`{"openClose":true,"change":2,"save":{}}`:        {OpenClose: true, Change: SyncIncremental, Save: true},
		`{"openClose":true,"save":{"includeText":true}}`: {OpenClose: true, Save: true, IncludeText: true},
		`{"openClose":true,"change":2,"save":false}`:     {OpenClose: true, Change: SyncIncremental},
	} {
		if got := parseSync(jsontext.Value(in)); got != want {
			t.Errorf("parseSync(%s) = %+v, want %+v", in, got, want)
		}
	}
}

// TestRegistrations resolves capabilities per document through the
// registrations and their selectors, by id.
func TestRegistrations(t *testing.T) {
	t.Parallel()

	c := &Client{Capabilities: ServerCapabilities{HoverProvider: jsontext.Value("true")}}
	c.register(Registration{ID: "1", Method: "textDocument/rename", RegisterOptions: jsontext.Value(`{"documentSelector":[{"language":"go"}],"prepareProvider":true}`)})
	c.register(Registration{ID: "2", Method: "textDocument/completion", RegisterOptions: jsontext.Value(`{"documentSelector":[{"pattern":"**/*.{ts,js}"}],"triggerCharacters":["."]}`)})
	c.register(Registration{ID: "3", Method: "textDocument/formatting"})
	c.register(Registration{ID: "4", Method: "textDocument/didChange", RegisterOptions: jsontext.Value(`{"documentSelector":null,"syncKind":1}`)})
	c.register(Registration{ID: "5", Method: "textDocument/didOpen", RegisterOptions: jsontext.Value(`{"documentSelector":[{"scheme":"file","language":"go"}]}`)})
	c.register(Registration{ID: "6", Method: "workspace/executeCommand", RegisterOptions: jsontext.Value(`{"commands":["a","b"]}`)})

	goFile, tsFile := "file:///p/x.go", "file:///p/src/x.ts"
	c.docs = map[string]docState{goFile: {languageID: "go"}, tsFile: {languageID: "typescript"}}
	if !c.Provides("textDocument/hover", tsFile, "typescript") || !c.Provides("textDocument/formatting", tsFile, "typescript") {
		t.Fatal("static hover or unselective formatting missing")
	}

	if !c.PrepareRenameSupported(goFile, "go") || c.PrepareRenameSupported(tsFile, "typescript") || c.Provides("textDocument/rename", tsFile, "typescript") {
		t.Fatal("rename selector by language")
	}

	if len(c.CompletionTriggers(tsFile, "typescript")) != 1 || c.Provides("textDocument/completion", goFile, "go") {
		t.Fatal("completion selector by pattern")
	}

	if got := c.WorkspaceCommands(goFile, "go"); len(got) != 2 {
		t.Fatalf("commands %v", got)
	}

	if got := c.syncFor(goFile); got != (SyncOptions{OpenClose: true, Change: SyncFull}) {
		t.Fatalf("go sync %+v", got)
	}

	if got := c.syncFor(tsFile); got != (SyncOptions{Change: SyncFull}) {
		t.Fatalf("ts sync %+v", got)
	}

	// Unregistering by id takes one registration, not every one of the
	// method.
	c.register(Registration{ID: "7", Method: "textDocument/rename"})
	c.unregister(Unregistration{ID: "7", Method: "textDocument/rename"})
	if !c.PrepareRenameSupported(goFile, "go") {
		t.Fatal("unregister dropped another id")
	}

	c.unregister(Unregistration{ID: "1", Method: "textDocument/rename"})
	c.unregister(Unregistration{ID: "4", Method: "textDocument/didChange"})
	if c.Provides("textDocument/rename", goFile, "go") || c.syncFor(goFile).Change != SyncNone {
		t.Fatalf("after unregister %+v", c.registrations)
	}

	for _, method := range []string{"workspace/didChangeWatchedFiles", "workspace/didChangeWorkspaceFolders", "textDocument/willSave"} {
		if registrable(method) {
			t.Errorf("%s registrable", method)
		}
	}
}

func TestGlobRegexp(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		glob, path string
		want       bool
	}{
		{"**/*.{ts,js}", "/p/src/a.ts", true},
		{"**/*.{ts,js}", "/a.js", true},
		{"**/*.{ts,js}", "/p/a.go", false},
		{"*.go", "/p/src/a.go", true},
		{"src/*.go", "/p/src/a.go", false},
		{"/p/src/?.go", "/p/src/a.go", true},
		{"/p/src/?.go", "/p/src/ab.go", false},
		{"**/test/**", "/p/test/x/y.go", true},
		{"[ab].go", "/p/b.go", true},
		{"[!ab].go", "/p/b.go", false},
	} {
		if got := globRegexp(tc.glob).MatchString(tc.path); got != tc.want {
			t.Errorf("glob %q on %q = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}
