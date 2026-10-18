package lsp

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// Dispatcher runs a function on the editor thread. Every callback the
// client invokes goes through it so editor state is never touched from
// the reader goroutine.
type Dispatcher func(fn func())

// Client talks to one language server process.
type Client struct {
	Name string
	Root string

	// NoSnippets turns off snippet support in the initialize request.
	NoSnippets bool

	conn *Conn
	proc host.Process

	// pid is the process the server watches, the editor when zero.
	pid      int
	dispatch Dispatcher

	mu          sync.Mutex
	initialized bool
	failed      error

	// initErr is why the handshake failed, reported as the exit reason.
	initErr error
	pending []pendingCall
	config  any

	// Capabilities and Encoding are set on the editor thread when the
	// handshake completes and only read there.
	Capabilities ServerCapabilities
	Encoding     Encoding

	// Version is what the server reported about itself, empty when it
	// said nothing.
	Version string

	// OnDiagnostics receives publishDiagnostics, on the editor thread.
	OnDiagnostics func(params PublishDiagnosticsParams)

	// OnMessage receives window/showMessage, on the editor thread.
	OnMessage func(kind int, text string)

	// OnApplyEdit handles workspace/applyEdit, on the editor thread, and
	// reports whether the edit was applied.
	OnApplyEdit func(edit WorkspaceEdit) bool

	// OnShowDocument handles window/showDocument, on the editor thread,
	// and reports whether the document was shown.
	OnShowDocument func(p ShowDocumentParams) bool

	// OnExit runs when the server connection ends.
	OnExit func(err error)

	// OnInlayHintRefresh handles workspace/inlayHint/refresh, on the
	// editor thread.
	OnInlayHintRefresh func()

	// OnProgress receives $/progress reports, on the editor thread.
	OnProgress func(token string, p WorkDoneProgress)
}

// pendingCall is work waiting for the handshake. fail, when set, reports
// that the handshake or the connection failed instead.
type pendingCall struct {
	run  func()
	fail func(error)
}

// shutdownGrace is how long a server may take to exit after shutdown
// before it is killed.
const shutdownGrace = 2 * time.Second

// Options configure a server process.
type Options struct {
	Name        string
	Command     string
	Args        []string
	Environment map[string]string
	Root        string
	Config      any

	// Host runs the server, the local machine when nil.
	Host host.Host

	// NoSnippets tells the server not to send snippet completions.
	NoSnippets bool
}

// Start launches the server process and begins initialization.
func Start(opts Options, dispatch Dispatcher) (*Client, error) {
	h := opts.Host
	if h == nil {
		h = host.Local{}
	}

	path, err := h.LookPath(opts.Command)
	if err != nil {
		return nil, fmt.Errorf("language server %q not found", opts.Command)
	}

	env := make([]string, 0, len(opts.Environment))
	for k, v := range opts.Environment {
		env = append(env, k+"="+v)
	}

	proc, err := h.Start(host.Command{Name: path, Args: opts.Args, Dir: opts.Root, Env: env})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", opts.Command, err)
	}

	c := NewClient(opts.Name, opts.Root, proc.Stdout(), proc.Stdin(), proc.Stdin(), dispatch)
	c.proc = proc
	c.pid = h.PID()
	c.NoSnippets = opts.NoSnippets
	c.Initialize(opts.Config)

	return c, nil
}

// NewClient wraps an existing connection, used by tests with an in
// process server. Set the handler fields, then call Initialize, which
// starts reading and the handshake.
func NewClient(name, root string, r io.Reader, w io.Writer, closer io.Closer, dispatch Dispatcher) *Client {
	c := &Client{Name: name, Root: root, dispatch: dispatch}
	c.conn = NewConn(r, w, closer)
	c.conn.OnNotify = c.handleNotify
	c.conn.OnRequest = c.handleRequest
	c.conn.OnClose = func(err error) {
		c.fail(errors.New("language server exited"))
		// Wait only after reading stopped, it closes the stdout pipe. The
		// exit status says more than the end of the stream.
		var exited error
		if c.proc != nil {
			if exited = c.proc.Wait(); exited != nil {
				err = exited
			} else if errors.Is(err, io.EOF) {
				err = errors.New("exited")
			}
		}

		// A failed handshake reports itself, unless the process died with
		// a reason of its own, as an rbenv shim missing its Ruby.
		c.mu.Lock()
		if c.initErr != nil && exited == nil {
			err = c.initErr
		}

		c.mu.Unlock()
		c.dispatch(func() {
			if c.OnExit != nil {
				c.OnExit(err)
			}
		})
	}

	return c
}

// Initialize stores the server settings, starts reading and performs the
// handshake. Start calls it automatically.
func (c *Client) Initialize(config any) {
	c.config = config
	go c.conn.Serve()
	c.initialize()
}

// fail marks the client failed and reports it to the waiting calls.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.failed == nil {
		c.failed = err
	}

	pending := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, p := range pending {
		if p.fail != nil {
			p.fail(err)
		}
	}
}

func (c *Client) initialize() {
	// The server exits when this process is gone, on a remote host it
	// is the agent there.
	pid := c.pid
	if pid == 0 {
		pid = os.Getpid()
	}

	params := map[string]any{
		"processId": pid,
		"rootUri":   PathToURI(c.Root),
		"rootPath":  c.Root,
		"workspaceFolders": []WorkspaceFolder{{
			URI:  PathToURI(c.Root),
			Name: c.Root,
		}},
		"clientInfo":            map[string]any{"name": "duck-editor", "version": "0.1"},
		"initializationOptions": c.config,
		"capabilities": map[string]any{
			"general": map[string]any{
				"positionEncodings": []string{"utf-8", "utf-32", "utf-16"},
			},
			"workspace": map[string]any{
				"applyEdit":        true,
				"configuration":    true,
				"workspaceFolders": true,
				"workspaceEdit": map[string]any{
					"documentChanges":       true,
					"resourceOperations":    []string{},
					"failureHandling":       "abort",
					"normalizesLineEndings": false,
				},
				"symbol":    map[string]any{"dynamicRegistration": false},
				"inlayHint": map[string]any{"refreshSupport": true},
			},
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didSave": true},
				"linkedEditingRange": map[string]any{"dynamicRegistration": false},
				"completion": map[string]any{
					"completionItem": map[string]any{
						"snippetSupport":          !c.NoSnippets,
						"insertReplaceSupport":    true,
						"deprecatedSupport":       true,
						"documentationFormat":     []string{"markdown", "plaintext"},
						"resolveSupport":          map[string]any{"properties": []string{"documentation", "detail", "additionalTextEdits"}},
						"tagSupport":              map[string]any{"valueSet": []int{1}},
						"insertTextModeSupport":   map[string]any{"valueSet": []int{1}},
						"labelDetailsSupport":     false,
						"commitCharactersSupport": false,
					},
					"completionItemKind": map[string]any{},
					"contextSupport":     true,
				},
				"hover": map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"signatureHelp": map[string]any{
					"signatureInformation": map[string]any{
						"documentationFormat":    []string{"markdown", "plaintext"},
						"parameterInformation":   map[string]any{"labelOffsetSupport": true},
						"activeParameterSupport": true,
					},
					"contextSupport": true,
				},
				"inlayHint":         map[string]any{"dynamicRegistration": false},
				"definition":        map[string]any{"linkSupport": true},
				"declaration":       map[string]any{"linkSupport": true},
				"typeDefinition":    map[string]any{"linkSupport": true},
				"implementation":    map[string]any{"linkSupport": true},
				"callHierarchy":     map[string]any{"dynamicRegistration": false},
				"references":        map[string]any{},
				"documentHighlight": map[string]any{},
				"rename":            map[string]any{"dynamicRegistration": false, "prepareSupport": false},
				"codeAction": map[string]any{
					"codeActionLiteralSupport": map[string]any{
						"codeActionKind": map[string]any{"valueSet": []string{
							"", "quickfix", "refactor", "refactor.extract", "refactor.inline",
							"refactor.rewrite", "source", "source.organizeImports",
						}},
					},
					"dataSupport":        true,
					"resolveSupport":     map[string]any{"properties": []string{"edit", "command"}},
					"isPreferredSupport": true,
					"disabledSupport":    true,
				},
				"formatting":     map[string]any{},
				"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"publishDiagnostics": map[string]any{
					"versionSupport": true,
					"tagSupport":     map[string]any{"valueSet": []int{1, 2}},
				},
			},
			"window": map[string]any{"workDoneProgress": true, "showDocument": map[string]any{"support": true}},
		},
	}
	c.conn.Request("initialize", params, func(result jsontext.Value, err error) {
		var res InitializeResult
		if err == nil {
			if derr := json.Unmarshal(result, &res); derr != nil {
				err = fmt.Errorf("initialize result: %w", derr)
			}
		}

		if err != nil {
			// The server is of no use. Closing the connection ends the
			// read loop, whose OnClose reports the exit once.
			c.mu.Lock()
			c.initErr = err
			c.mu.Unlock()
			c.fail(err)
			_ = c.conn.Close()
			if c.proc != nil {
				_ = c.proc.Kill()
			}

			return
		}

		// The capabilities land on the editor thread, which is the only
		// reader, and the queued calls run there right after.
		c.dispatch(func() {
			c.mu.Lock()
			c.Capabilities = res.Capabilities
			c.Encoding = ParseEncoding(res.Capabilities.PositionEncoding)
			if res.ServerInfo != nil {
				c.Version = strings.TrimSpace(res.ServerInfo.Name + " " + shortVersion(res.ServerInfo.Version))
			}
			c.initialized = c.failed == nil
			pending := c.pending
			c.pending = nil
			c.mu.Unlock()
			if !c.initialized {
				return
			}

			_ = c.conn.Notify("initialized", map[string]any{})
			if c.config != nil {
				_ = c.conn.Notify("workspace/didChangeConfiguration", map[string]any{"settings": c.config})
			}

			for _, p := range pending {
				p.run()
			}
		})
	})
}

// Ready reports whether the handshake finished.
func (c *Client) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.initialized && c.failed == nil
}

// Failed returns the startup or connection error, if any.
func (c *Client) Failed() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.failed
}

// whenReady runs fn now or after the handshake completes. When the
// client failed, or fails before that, fail runs with the error instead.
func (c *Client) whenReady(fn func(), fail func(error)) {
	c.mu.Lock()
	failed := c.failed
	if c.initialized || failed != nil {
		c.mu.Unlock()
		switch {
		case failed == nil:
			fn()
		case fail != nil:
			fail(failed)
		}

		return
	}

	c.pending = append(c.pending, pendingCall{run: fn, fail: fail})
	c.mu.Unlock()
}

// failTo reports an error to a callback on the editor thread.
func failTo[T any](c *Client, cb func(T, error)) func(error) {
	return func(err error) {
		var zero T
		c.dispatch(func() { cb(zero, err) })
	}
}

// Stop asks the server to shut down and kills it when it does not exit
// within the grace period.
func (c *Client) Stop() {
	c.conn.Request("shutdown", nil, func(jsontext.Value, error) {
		_ = c.conn.Notify("exit", nil)
		_ = c.conn.Close()
	})
	if c.proc != nil {
		proc := c.proc
		time.AfterFunc(shutdownGrace, func() { _ = proc.Kill() })
	}
}

func (c *Client) handleNotify(method string, params jsontext.Value) {
	switch method {
	case "textDocument/publishDiagnostics":
		var p PublishDiagnosticsParams
		if err := json.Unmarshal(params, &p); err != nil {
			return
		}

		c.dispatch(func() {
			if c.OnDiagnostics != nil {
				c.OnDiagnostics(p)
			}
		})
	case "window/showMessage":
		var p ShowMessageParams
		if err := json.Unmarshal(params, &p); err != nil {
			return
		}

		c.dispatch(func() {
			if c.OnMessage != nil {
				c.OnMessage(p.Type, p.Message)
			}
		})
	case "$/progress":
		var p ProgressParams
		if err := json.Unmarshal(params, &p); err != nil {
			return
		}

		token := strings.Trim(string(p.Token), "\"")
		c.dispatch(func() {
			if c.OnProgress != nil {
				c.OnProgress(token, p.Value)
			}
		})
	}
}

func (c *Client) handleRequest(method string, params jsontext.Value) (any, *ResponseError) {
	switch method {
	case "workspace/configuration":
		var p ConfigurationParams
		_ = json.Unmarshal(params, &p)
		out := make([]any, len(p.Items))
		for i, item := range p.Items {
			out[i] = configSection(c.config, item.Section)
		}

		return out, nil
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
		return nil, nil
	case "workspace/applyEdit":
		var p ApplyWorkspaceEditParams
		if err := json.Unmarshal(params, &p); err != nil {
			return map[string]any{"applied": false}, nil
		}

		done := make(chan bool, 1)
		c.dispatch(func() { done <- c.OnApplyEdit != nil && c.OnApplyEdit(p.Edit) })

		return map[string]any{"applied": <-done}, nil
	case "window/showMessageRequest":
		return nil, nil
	case "window/showDocument":
		var p ShowDocumentParams
		if err := json.Unmarshal(params, &p); err != nil {
			return map[string]any{"success": false}, nil
		}

		done := make(chan bool, 1)
		c.dispatch(func() { done <- c.OnShowDocument != nil && c.OnShowDocument(p) })

		return map[string]any{"success": <-done}, nil
	case "workspace/inlayHint/refresh":
		c.dispatch(func() {
			if c.OnInlayHintRefresh != nil {
				c.OnInlayHintRefresh()
			}
		})

		return nil, nil
	}

	return nil, &ResponseError{Code: -32601, Message: "method not found: " + method}
}

// configSection walks a dotted section path through the config map.
func configSection(config any, section string) any {
	if section == "" {
		return config
	}

	cur := config
	for part := range strings.SplitSeq(section, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}

		cur, ok = m[part]
		if !ok {
			return nil
		}
	}

	return cur
}

// request sends a request once the server is ready and decodes the
// result into out, delivering the callback on the editor thread.
func request[T any](c *Client, method string, params any, cb func(result T, err error)) {
	c.whenReady(func() {
		c.conn.Request(method, params, func(raw jsontext.Value, err error) {
			var out T
			if err == nil && len(raw) > 0 && string(raw) != "null" {
				err = json.Unmarshal(raw, &out)
			}

			c.dispatch(func() { cb(out, err) })
		})
	}, failTo(c, cb))
}

// DidOpen announces a document.
func (c *Client) DidOpen(uri, languageID string, version int, text string) {
	c.whenReady(func() {
		_ = c.conn.Notify("textDocument/didOpen", map[string]any{
			"textDocument": TextDocumentItem{URI: uri, LanguageID: languageID, Version: version, Text: text},
		})
	}, nil)
}

// DidChange sends incremental changes. They are computed when sent, so
// positions use the encoding the server negotiated.
func (c *Client) DidChange(uri string, version int, changes func(Encoding) []ContentChange) {
	c.whenReady(func() {
		_ = c.conn.Notify("textDocument/didChange", DidChangeParams{
			TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: version},
			ContentChanges: changes(c.Encoding),
		})
	}, nil)
}

// DidSave notifies a save.
func (c *Client) DidSave(uri string) {
	c.whenReady(func() {
		_ = c.conn.Notify("textDocument/didSave", map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}})
	}, nil)
}

// DidClose notifies a close.
func (c *Client) DidClose(uri string) {
	c.whenReady(func() {
		_ = c.conn.Notify("textDocument/didClose", map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}})
	}, nil)
}

// Completion requests completions.
func (c *Client) Completion(params CompletionParams, cb func(CompletionList, error)) {
	c.whenReady(func() {
		c.conn.Request("textDocument/completion", params, func(raw jsontext.Value, err error) {
			var list CompletionList
			if err == nil && len(raw) > 0 && string(raw) != "null" {
				// The result is a CompletionList or a bare item array.
				if raw[0] == '[' {
					err = json.Unmarshal(raw, &list.Items)
				} else {
					err = json.Unmarshal(raw, &list)
				}
			}

			c.dispatch(func() { cb(list, err) })
		})
	}, failTo(c, cb))
}

// Hover requests hover information.
func (c *Client) Hover(params TextDocumentPositionParams, cb func(*Hover, error)) {
	request(c, "textDocument/hover", params, cb)
}

// PrepareCallHierarchy asks for the function at a position, the start of
// the incoming and outgoing call requests.
func (c *Client) PrepareCallHierarchy(params TextDocumentPositionParams, cb func([]CallHierarchyItem, error)) {
	request(c, "textDocument/prepareCallHierarchy", params, cb)
}

// Calls asks for the callers of item, or its callees when incoming is
// false.
func (c *Client) Calls(item CallHierarchyItem, incoming bool, cb func([]CallHierarchyCall, error)) {
	method := "callHierarchy/outgoingCalls"
	if incoming {
		method = "callHierarchy/incomingCalls"
	}

	request(c, method, map[string]any{"item": item}, cb)
}

// Locations requests definition like results, normalizing LocationLink
// and single Location responses to a slice.
func (c *Client) Locations(method string, params any, cb func([]Location, error)) {
	c.whenReady(func() {
		c.conn.Request(method, params, func(raw jsontext.Value, err error) {
			locs, derr := decodeLocations(raw)
			if err == nil {
				err = derr
			}

			c.dispatch(func() { cb(locs, err) })
		})
	}, failTo(c, cb))
}

func decodeLocations(raw jsontext.Value) ([]Location, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	if raw[0] == '{' {
		var loc Location
		if err := json.Unmarshal(raw, &loc); err != nil {
			return nil, err
		}

		return []Location{loc}, nil
	}

	var links []LocationLink
	if err := json.Unmarshal(raw, &links); err == nil && len(links) > 0 && links[0].TargetURI != "" {
		out := make([]Location, len(links))
		for i, l := range links {
			out[i] = Location{URI: l.TargetURI, Range: l.TargetSelectionRange}
		}

		return out, nil
	}

	var locs []Location
	err := json.Unmarshal(raw, &locs)

	return locs, err
}

// Rename requests a workspace edit renaming the symbol.
func (c *Client) Rename(params RenameParams, cb func(*WorkspaceEdit, error)) {
	request(c, "textDocument/rename", params, cb)
}

// CodeActions requests code actions. Commands returned instead of
// actions are wrapped as actions.
func (c *Client) CodeActions(params CodeActionParams, cb func([]CodeAction, error)) {
	c.whenReady(func() {
		c.conn.Request("textDocument/codeAction", params, func(raw jsontext.Value, err error) {
			var items []jsontext.Value
			var actions []CodeAction
			if err == nil && len(raw) > 0 && string(raw) != "null" {
				if err = json.Unmarshal(raw, &items); err == nil {
					for _, item := range items {
						var action CodeAction
						if json.Unmarshal(item, &action) == nil && (action.Edit != nil || action.Command != nil || action.Kind != "" || action.Data != nil) {
							actions = append(actions, action)

							continue
						}

						var cmd Command
						if json.Unmarshal(item, &cmd) == nil && cmd.Command != "" {
							actions = append(actions, CodeAction{Title: cmd.Title, Command: &cmd})
						}
					}
				}
			}

			c.dispatch(func() { cb(actions, err) })
		})
	}, failTo(c, cb))
}

// ResolveCodeAction fills in a lazy code action.
func (c *Client) ResolveCodeAction(action CodeAction, cb func(CodeAction, error)) {
	request(c, "codeAction/resolve", action, cb)
}

// ExecuteCommand runs a server command.
func (c *Client) ExecuteCommand(cmd Command, cb func(error)) {
	c.whenReady(func() {
		c.conn.Request("workspace/executeCommand", map[string]any{
			"command":   cmd.Command,
			"arguments": cmd.Arguments,
		}, func(_ jsontext.Value, err error) {
			c.dispatch(func() { cb(err) })
		})
	}, func(err error) { c.dispatch(func() { cb(err) }) })
}

// Formatting requests whole document formatting edits.
func (c *Client) Formatting(params DocumentFormattingParams, cb func([]TextEdit, error)) {
	request(c, "textDocument/formatting", params, cb)
}

// DocumentSymbols requests the symbols of a document, flattening
// hierarchical results.
func (c *Client) DocumentSymbols(uri string, cb func([]SymbolInformation, error)) {
	c.whenReady(func() {
		params := map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}}
		c.conn.Request("textDocument/documentSymbol", params, func(raw jsontext.Value, err error) {
			var out []SymbolInformation
			if err == nil && len(raw) > 0 && string(raw) != "null" {
				var nested []DocumentSymbol
				hierarchical := json.Unmarshal(raw, &nested) == nil && len(nested) > 0 &&
					(nested[0].SelectionRange != (Range{}) || nested[0].Range != (Range{}))
				if hierarchical {
					flatten(uri, "", nested, &out)
				} else {
					err = json.Unmarshal(raw, &out)
				}
			}

			c.dispatch(func() { cb(out, err) })
		})
	}, failTo(c, cb))
}

func flatten(uri, container string, symbols []DocumentSymbol, out *[]SymbolInformation) {
	for _, s := range symbols {
		*out = append(*out, SymbolInformation{
			Name:          s.Name,
			Kind:          s.Kind,
			Location:      Location{URI: uri, Range: s.SelectionRange},
			ContainerName: container,
		})
		flatten(uri, s.Name, s.Children, out)
	}
}

// WorkspaceSymbols queries symbols across the workspace.
func (c *Client) WorkspaceSymbols(query string, cb func([]SymbolInformation, error)) {
	request(c, "workspace/symbol", map[string]any{"query": query}, cb)
}

// SignatureHelp requests the signatures at a position.
func (c *Client) SignatureHelp(params SignatureHelpParams, cb func(*SignatureHelp, error)) {
	request(c, "textDocument/signatureHelp", params, cb)
}

// InlayHints requests the hints of a range.
func (c *Client) InlayHints(params InlayHintParams, cb func([]InlayHint, error)) {
	request(c, "textDocument/inlayHint", params, cb)
}

// DocumentLinks requests the links of a document.
func (c *Client) DocumentLinks(uri string, cb func([]DocumentLink, error)) {
	request(c, "textDocument/documentLink", map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}}, cb)
}

// PrepareRename asks whether the symbol at a position can be renamed
// and what its current text is.
func (c *Client) PrepareRename(params TextDocumentPositionParams, cb func(*PrepareRename, error)) {
	request(c, "textDocument/prepareRename", params, cb)
}

// PrepareRenameSupported reports the server capability.
func (c *Client) PrepareRenameSupported() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	var obj struct {
		PrepareProvider bool `json:"prepareProvider"`
	}

	return json.Unmarshal(c.Capabilities.RenameProvider, &obj) == nil && obj.PrepareProvider
}

// DocumentHighlights requests the occurrences of the symbol at a position.
func (c *Client) DocumentHighlights(params TextDocumentPositionParams, cb func([]DocumentHighlight, error)) {
	request(c, "textDocument/documentHighlight", params, cb)
}

// SignatureTriggers returns the chars that start signature help.
func (c *Client) SignatureTriggers() (trigger, retrigger []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Capabilities.SignatureHelpProvider == nil {
		return nil, nil
	}

	return c.Capabilities.SignatureHelpProvider.TriggerCharacters, c.Capabilities.SignatureHelpProvider.RetriggerCharacters
}

// CompletionTriggers returns the server's trigger characters.
func (c *Client) CompletionTriggers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Capabilities.CompletionProvider == nil {
		return nil
	}

	return c.Capabilities.CompletionProvider.TriggerCharacters
}

// LinkedEditingRanges asks for the ranges that change together with the
// one at a position, such as the names of an opening and closing tag.
func (c *Client) LinkedEditingRanges(params TextDocumentPositionParams, cb func(*LinkedEditingRanges, error)) {
	request(c, "textDocument/linkedEditingRange", params, cb)
}

// shortVersion keeps a version a person reads: gopls sends its whole
// build info as JSON, whose main module version is the useful part.
func shortVersion(v string) string {
	var build struct {
		Main struct {
			Version string
		}
	}

	if strings.HasPrefix(v, "{") && json.Unmarshal([]byte(v), &build) == nil {
		return build.Main.Version
	}

	v, _, _ = strings.Cut(v, "\n")
	if len(v) > 40 {
		v = v[:40]
	}

	return strings.TrimSpace(v)
}
