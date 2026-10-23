package lsp

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
)

// Registration is one entry of client/registerCapability.
type Registration struct {
	ID              string         `json:"id"`
	Method          string         `json:"method"`
	RegisterOptions jsontext.Value `json:"registerOptions,omitempty"`
}

// Unregistration is one entry of client/unregisterCapability.
type Unregistration struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

// registration is a capability a server registered at run time, for the
// documents its selector takes.
type registration struct {
	id       string
	method   string
	selector []DocumentFilter
	options  jsontext.Value
}

// providerFields maps a request method to its static capability.
var providerFields = map[string]func(*ServerCapabilities) *jsontext.Value{
	"textDocument/hover":                func(c *ServerCapabilities) *jsontext.Value { return &c.HoverProvider },
	"textDocument/completion":           func(c *ServerCapabilities) *jsontext.Value { return &c.CompletionProvider },
	"textDocument/signatureHelp":        func(c *ServerCapabilities) *jsontext.Value { return &c.SignatureHelpProvider },
	"textDocument/definition":           func(c *ServerCapabilities) *jsontext.Value { return &c.DefinitionProvider },
	"textDocument/declaration":          func(c *ServerCapabilities) *jsontext.Value { return &c.DeclarationProvider },
	"textDocument/typeDefinition":       func(c *ServerCapabilities) *jsontext.Value { return &c.TypeDefinitionProvider },
	"textDocument/implementation":       func(c *ServerCapabilities) *jsontext.Value { return &c.ImplementationProvider },
	"textDocument/references":           func(c *ServerCapabilities) *jsontext.Value { return &c.ReferencesProvider },
	"textDocument/rename":               func(c *ServerCapabilities) *jsontext.Value { return &c.RenameProvider },
	"textDocument/codeAction":           func(c *ServerCapabilities) *jsontext.Value { return &c.CodeActionProvider },
	"textDocument/formatting":           func(c *ServerCapabilities) *jsontext.Value { return &c.DocumentFormattingProvider },
	"textDocument/documentSymbol":       func(c *ServerCapabilities) *jsontext.Value { return &c.DocumentSymbolProvider },
	"workspace/symbol":                  func(c *ServerCapabilities) *jsontext.Value { return &c.WorkspaceSymbolProvider },
	"workspace/executeCommand":          func(c *ServerCapabilities) *jsontext.Value { return &c.ExecuteCommandProvider },
	"textDocument/documentHighlight":    func(c *ServerCapabilities) *jsontext.Value { return &c.DocumentHighlightProvider },
	"textDocument/documentLink":         func(c *ServerCapabilities) *jsontext.Value { return &c.DocumentLinkProvider },
	"textDocument/inlayHint":            func(c *ServerCapabilities) *jsontext.Value { return &c.InlayHintProvider },
	"textDocument/linkedEditingRange":   func(c *ServerCapabilities) *jsontext.Value { return &c.LinkedEditingProvider },
	"textDocument/prepareCallHierarchy": func(c *ServerCapabilities) *jsontext.Value { return &c.CallHierarchyProvider },
}

// syncMethods are the document notifications a registration turns on.
var syncMethods = map[string]bool{
	"textDocument/didOpen":   true,
	"textDocument/didClose":  true,
	"textDocument/didChange": true,
	"textDocument/didSave":   true,
}

// registrable reports whether a capability can be registered at run
// time: the feature providers, the document sync notifications and the
// configuration change the client sends anyway. The rest, such as file
// watching, is refused so the server knows it is not there. The
// initialize request announces dynamicRegistration for exactly these.
func registrable(method string) bool {
	return providerFields[method] != nil || syncMethods[method] || method == "workspace/didChangeConfiguration"
}

// register stores one registration, on the editor thread. Its options
// carry the document selector and double as the capability value.
func (c *Client) register(r Registration) {
	var opts struct {
		DocumentSelector []DocumentFilter `json:"documentSelector"`
	}
	_ = json.Unmarshal(r.RegisterOptions, &opts)
	options := r.RegisterOptions
	if len(options) == 0 || string(options) == "null" {
		options = jsontext.Value("true")
	}

	c.registrations = slices.DeleteFunc(c.registrations, func(old registration) bool { return old.id == r.ID })
	c.registrations = append(c.registrations, registration{id: r.ID, method: r.Method, selector: opts.DocumentSelector, options: options})
}

// unregister drops a registration by id, on the editor thread, and
// closes the documents that lost their open and close notifications.
func (c *Client) unregister(u Unregistration) {
	c.registrations = slices.DeleteFunc(c.registrations, func(r registration) bool { return r.id == u.ID })
	for uri, d := range c.docs {
		if d.opened && !c.syncFor(uri).OpenClose {
			_ = c.conn.Notify("textDocument/didClose", map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}})
			d.opened = false
			c.docs[uri] = d
		}
	}
}

// Provider returns the capability of a request method for a document:
// the options a matching registration carries, else the static value,
// and whether the server offers it at all. On the editor thread.
func (c *Client) Provider(method, uri, languageID string) (jsontext.Value, bool) {
	for i := len(c.registrations) - 1; i >= 0; i-- {
		r := c.registrations[i]
		if r.method == method && matchesDocument(r.selector, uri, languageID) {
			return r.options, true
		}
	}

	if field := providerFields[method]; field != nil {
		if v := *field(&c.Capabilities); Supports(v) {
			return v, true
		}
	}

	return nil, false
}

// Provides reports whether the server offers a request for a document.
func (c *Client) Provides(method, uri, languageID string) bool {
	_, ok := c.Provider(method, uri, languageID)

	return ok
}

// providerOption decodes one field of a provider's options.
func providerOption[T any](c *Client, method, uri, languageID string, out *T) {
	v, ok := c.Provider(method, uri, languageID)
	if ok && len(v) > 0 && v[0] == '{' {
		_ = json.Unmarshal(v, out)
	}
}

// PrepareRenameSupported reports whether the server answers
// textDocument/prepareRename for a document.
func (c *Client) PrepareRenameSupported(uri, languageID string) bool {
	var obj struct {
		PrepareProvider bool `json:"prepareProvider"`
	}
	providerOption(c, "textDocument/rename", uri, languageID, &obj)

	return obj.PrepareProvider
}

// SignatureTriggers returns the chars that start signature help in a
// document.
func (c *Client) SignatureTriggers(uri, languageID string) (trigger, retrigger []string) {
	var obj struct {
		TriggerCharacters   []string `json:"triggerCharacters"`
		RetriggerCharacters []string `json:"retriggerCharacters"`
	}
	providerOption(c, "textDocument/signatureHelp", uri, languageID, &obj)

	return obj.TriggerCharacters, obj.RetriggerCharacters
}

// CompletionTriggers returns the server's trigger characters for a
// document.
func (c *Client) CompletionTriggers(uri, languageID string) []string {
	var obj struct {
		TriggerCharacters []string `json:"triggerCharacters"`
	}
	providerOption(c, "textDocument/completion", uri, languageID, &obj)

	return obj.TriggerCharacters
}

// WorkspaceCommands returns the commands workspace/executeCommand runs
// for a document.
func (c *Client) WorkspaceCommands(uri, languageID string) []string {
	var obj struct {
		Commands []string `json:"commands"`
	}
	providerOption(c, "workspace/executeCommand", uri, languageID, &obj)

	return obj.Commands
}

// CompletionResolves reports whether the server fills completion items
// in through completionItem/resolve for a document.
func (c *Client) CompletionResolves(uri, languageID string) bool {
	var obj struct {
		ResolveProvider bool `json:"resolveProvider"`
	}
	providerOption(c, "textDocument/completion", uri, languageID, &obj)

	return obj.ResolveProvider
}
