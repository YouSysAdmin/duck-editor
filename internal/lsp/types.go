// Package lsp is a small Language Server Protocol client: JSON-RPC over
// stdio, the message types the editor uses and conversions between LSP
// positions and rope char indexes.
package lsp

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Position is a line and character offset in the server's encoding.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half open position range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location points into a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink is the richer definition result some servers return.
type LocationLink struct {
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// TextDocumentIdentifier names a document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier names a document version.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentItem describes an opened document.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentPositionParams is the common request payload.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// ContentChange is one incremental edit.
type ContentChange struct {
	Range *Range `json:"range,omitempty"`
	Text  string `json:"text"`
}

// DidChangeParams notifies about edits.
type DidChangeParams struct {
	TextDocument   VersionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []ContentChange                 `json:"contentChanges"`
}

// Diagnostic severities.
const (
	SeverityError   = 1
	SeverityWarning = 2
	SeverityInfo    = 3
	SeverityHint    = 4
)

// Diagnostic is one reported problem.
type Diagnostic struct {
	Range    Range          `json:"range"`
	Severity int            `json:"severity,omitzero"`
	Code     jsontext.Value `json:"code,omitempty"`
	Source   string         `json:"source,omitempty"`
	Message  string         `json:"message"`
	Tags     []int          `json:"tags,omitempty"`
	Data     jsontext.Value `json:"data,omitempty"`
}

// PublishDiagnosticsParams carries the diagnostics of one document.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// TextEdit replaces a range with new text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// InsertReplaceEdit is the alternative completion edit shape.
type InsertReplaceEdit struct {
	NewText string `json:"newText"`
	Insert  Range  `json:"insert"`
	Replace Range  `json:"replace"`
}

// Insert text formats.
const (
	InsertTextPlain   = 1
	InsertTextSnippet = 2
)

// CompletionItem is one completion candidate.
type CompletionItem struct {
	Label               string         `json:"label"`
	Kind                int            `json:"kind,omitzero"`
	Detail              string         `json:"detail,omitempty"`
	Documentation       jsontext.Value `json:"documentation,omitempty"`
	Preselect           bool           `json:"preselect,omitzero"`
	SortText            string         `json:"sortText,omitempty"`
	FilterText          string         `json:"filterText,omitempty"`
	InsertText          string         `json:"insertText,omitempty"`
	InsertTextFormat    int            `json:"insertTextFormat,omitzero"`
	TextEdit            jsontext.Value `json:"textEdit,omitempty"`
	AdditionalTextEdits []TextEdit     `json:"additionalTextEdits,omitempty"`
	Command             *Command       `json:"command,omitempty"`
	Data                jsontext.Value `json:"data,omitempty"`
}

// CompletionList is the completion response.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// Completion item kinds the editor creates itself.
const (
	CompletionKindText   = 1
	CompletionKindFile   = 17
	CompletionKindFolder = 19
)

// Completion trigger kinds.
const (
	CompletionInvoked          = 1
	CompletionTriggerCharacter = 2
)

// CompletionContext says why completion was requested.
type CompletionContext struct {
	TriggerKind      int    `json:"triggerKind"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

// CompletionParams requests completions.
type CompletionParams struct {
	TextDocumentPositionParams
	Context *CompletionContext `json:"context,omitempty"`
}

// MarkupContent is text with a kind of plaintext or markdown.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Hover is the hover response, Contents may be a string, MarkupContent
// or an array of marked strings.
type Hover struct {
	Contents jsontext.Value `json:"contents"`
	Range    *Range         `json:"range,omitempty"`
}

// ReferenceParams requests references.
type ReferenceParams struct {
	TextDocumentPositionParams
	Context struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

// RenameParams requests a rename.
type RenameParams struct {
	TextDocumentPositionParams
	NewName string `json:"newName"`
}

// WorkspaceEdit groups edits over documents.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []jsontext.Value      `json:"documentChanges,omitempty"`
}

// TextDocumentEdit is one entry of documentChanges.
type TextDocumentEdit struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version *int   `json:"version"`
	} `json:"textDocument"`
	Edits []jsontext.Value `json:"edits"`
}

// Command is a server side command.
type Command struct {
	Title     string           `json:"title"`
	Command   string           `json:"command"`
	Arguments []jsontext.Value `json:"arguments,omitempty"`
}

// CodeAction is a quick fix or refactoring.
type CodeAction struct {
	Title       string       `json:"title"`
	Kind        string       `json:"kind,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	IsPreferred bool         `json:"isPreferred,omitzero"`
	Disabled    *struct {
		Reason string `json:"reason"`
	} `json:"disabled,omitempty"`
	Edit    *WorkspaceEdit `json:"edit,omitempty"`
	Command *Command       `json:"command,omitempty"`
	Data    jsontext.Value `json:"data,omitempty"`
}

// CodeActionParams requests code actions for a range.
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
		Only        []string     `json:"only,omitempty"`

		// TriggerKind is CodeActionInvoked when the user asked for the
		// actions. Servers offer some refactorings only then.
		TriggerKind int `json:"triggerKind,omitempty"`
	} `json:"context"`
}

// ShowDocumentParams asks the client to show a file or a web page.
type ShowDocumentParams struct {
	URI       string `json:"uri"`
	External  bool   `json:"external,omitempty"`
	TakeFocus bool   `json:"takeFocus,omitempty"`
	Selection *Range `json:"selection,omitempty"`
}

// Code action trigger kinds.
const (
	CodeActionInvoked   = 1
	CodeActionAutomatic = 2
)

// FormattingOptions configure document formatting.
type FormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

// DocumentFormattingParams requests formatting.
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

// DocumentSymbol is a hierarchical symbol.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation is a flat symbol.
type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

// ShowMessageParams is window/showMessage.
type ShowMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

// MessageActionItem is one answer a window/showMessageRequest offers.
type MessageActionItem struct {
	Title string `json:"title"`
}

// ShowMessageRequestParams is window/showMessageRequest, a message with
// the answers the user may pick.
type ShowMessageRequestParams struct {
	Type    int                 `json:"type"`
	Message string              `json:"message"`
	Actions []MessageActionItem `json:"actions,omitempty"`
}

// ApplyWorkspaceEditParams is workspace/applyEdit.
type ApplyWorkspaceEditParams struct {
	Label string        `json:"label,omitempty"`
	Edit  WorkspaceEdit `json:"edit"`
}

// ConfigurationParams is workspace/configuration.
type ConfigurationParams struct {
	Items []struct {
		ScopeURI string `json:"scopeUri,omitempty"`
		Section  string `json:"section,omitempty"`
	} `json:"items"`
}

// WorkspaceFolder is a root the server works in.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// ServerCapabilities is the subset of initialize result we look at.
type ServerCapabilities struct {
	PositionEncoding           string         `json:"positionEncoding,omitempty"`
	TextDocumentSync           jsontext.Value `json:"textDocumentSync,omitempty"`
	CompletionProvider         jsontext.Value `json:"completionProvider,omitempty"`
	HoverProvider              jsontext.Value `json:"hoverProvider,omitempty"`
	DefinitionProvider         jsontext.Value `json:"definitionProvider,omitempty"`
	DeclarationProvider        jsontext.Value `json:"declarationProvider,omitempty"`
	TypeDefinitionProvider     jsontext.Value `json:"typeDefinitionProvider,omitempty"`
	ImplementationProvider     jsontext.Value `json:"implementationProvider,omitempty"`
	ReferencesProvider         jsontext.Value `json:"referencesProvider,omitempty"`
	RenameProvider             jsontext.Value `json:"renameProvider,omitempty"`
	CodeActionProvider         jsontext.Value `json:"codeActionProvider,omitempty"`
	DocumentFormattingProvider jsontext.Value `json:"documentFormattingProvider,omitempty"`
	DocumentSymbolProvider     jsontext.Value `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider    jsontext.Value `json:"workspaceSymbolProvider,omitempty"`
	ExecuteCommandProvider     jsontext.Value `json:"executeCommandProvider,omitempty"`
	DocumentHighlightProvider  jsontext.Value `json:"documentHighlightProvider,omitempty"`
	DocumentLinkProvider       jsontext.Value `json:"documentLinkProvider,omitempty"`
	InlayHintProvider          jsontext.Value `json:"inlayHintProvider,omitempty"`
	LinkedEditingProvider      jsontext.Value `json:"linkedEditingRangeProvider,omitempty"`
	CallHierarchyProvider      jsontext.Value `json:"callHierarchyProvider,omitempty"`
	SignatureHelpProvider      jsontext.Value `json:"signatureHelpProvider,omitempty"`
}

// InitializeResult is the initialize response.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *struct {
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
	} `json:"serverInfo,omitempty"`
}

// Supports reports whether a capability value is present and not false.
func Supports(v jsontext.Value) bool {
	s := string(v)

	return s != "" && s != "false" && s != "null"
}

// ParameterInformation is one parameter of a signature. Label is either
// a string or a [start, end] offset pair into the signature label.
type ParameterInformation struct {
	Label         jsontext.Value `json:"label"`
	Documentation jsontext.Value `json:"documentation,omitempty"`
}

// SignatureInformation describes one callable signature.
type SignatureInformation struct {
	Label           string                 `json:"label"`
	Documentation   jsontext.Value         `json:"documentation,omitempty"`
	Parameters      []ParameterInformation `json:"parameters,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// SignatureHelp is the signature help response.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature *int                   `json:"activeSignature,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// Signature help trigger kinds.
const (
	SignatureTriggerInvoked       = 1
	SignatureTriggerCharacter     = 2
	SignatureTriggerContentChange = 3
)

// SignatureHelpContext says why signature help was requested.
type SignatureHelpContext struct {
	TriggerKind         int            `json:"triggerKind"`
	TriggerCharacter    string         `json:"triggerCharacter,omitempty"`
	IsRetrigger         bool           `json:"isRetrigger"`
	ActiveSignatureHelp *SignatureHelp `json:"activeSignatureHelp,omitempty"`
}

// SignatureHelpParams requests signature help.
type SignatureHelpParams struct {
	TextDocumentPositionParams
	Context *SignatureHelpContext `json:"context,omitempty"`
}

// Inlay hint kinds.
const (
	InlayHintType      = 1
	InlayHintParameter = 2
)

// InlayHintLabelPart is one piece of a structured inlay hint label.
type InlayHintLabelPart struct {
	Value string `json:"value"`
}

// InlayHint is virtual text shown at a position. Label is a string or an
// array of label parts.
type InlayHint struct {
	Position     Position       `json:"position"`
	Label        jsontext.Value `json:"label"`
	Kind         int            `json:"kind,omitzero"`
	PaddingLeft  bool           `json:"paddingLeft,omitzero"`
	PaddingRight bool           `json:"paddingRight,omitzero"`
}

// LabelText flattens the hint label to a string.
func (h InlayHint) LabelText() string {
	var s string
	if json.Unmarshal(h.Label, &s) == nil {
		return s
	}

	var parts []InlayHintLabelPart
	if json.Unmarshal(h.Label, &parts) == nil {
		var out []byte
		for _, p := range parts {
			out = append(out, p.Value...)
		}

		return string(out)
	}

	return ""
}

// InlayHintParams requests the hints of a range.
type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

// DocumentHighlight is one occurrence of the symbol under the cursor.
type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitzero"`
}

// WorkDoneProgress is the value of a $/progress notification.
type WorkDoneProgress struct {
	Kind       string `json:"kind"`
	Title      string `json:"title,omitempty"`
	Message    string `json:"message,omitempty"`
	Percentage int    `json:"percentage,omitzero"`
}

// ProgressParams is $/progress.
type ProgressParams struct {
	Token jsontext.Value   `json:"token"`
	Value WorkDoneProgress `json:"value"`
}

// DocumentLink is a link in the document text.
type DocumentLink struct {
	Range  Range  `json:"range"`
	Target string `json:"target,omitempty"`
}

// PrepareRename is the prepareRename response in its range form, the
// placeholder form adds the text to rename.
type PrepareRename struct {
	Range       *Range `json:"range,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// LinkedEditingRanges are ranges edited as one.
type LinkedEditingRanges struct {
	Ranges      []Range `json:"ranges"`
	WordPattern string  `json:"wordPattern,omitempty"`
}

// CallHierarchyItem is a function or method in a call hierarchy.
type CallHierarchyItem struct {
	Name           string         `json:"name"`
	Kind           int            `json:"kind"`
	Detail         string         `json:"detail,omitempty"`
	URI            string         `json:"uri"`
	Range          Range          `json:"range"`
	SelectionRange Range          `json:"selectionRange"`
	Data           jsontext.Value `json:"data,omitempty"`
}

// CallHierarchyCall is a caller or callee with the ranges of the calls,
// which lie in the caller.
type CallHierarchyCall struct {
	From       CallHierarchyItem `json:"from"`
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}
