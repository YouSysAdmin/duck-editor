package lsp

import "strings"

// SemanticTokenTypes are the token types of the LSP specification, which
// the client declares it can show.
var SemanticTokenTypes = []string{
	"namespace", "type", "class", "enum", "interface", "struct", "typeParameter", "parameter", "variable",
	"property", "enumMember", "event", "function", "method", "macro", "keyword", "modifier", "comment",
	"string", "number", "regexp", "operator", "decorator",
}

// SemanticTokenModifiers are the modifiers of the specification.
var SemanticTokenModifiers = []string{
	"declaration", "definition", "readonly", "static", "deprecated", "abstract", "async", "modification",
	"documentation", "defaultLibrary",
}

// semanticScopes maps a token type to the theme scope of the same thing
// in the tree-sitter queries, so a server's token and a query capture
// share a color.
var semanticScopes = map[string]string{
	"namespace":     "namespace",
	"type":          "type",
	"class":         "type",
	"enum":          "type.enum",
	"interface":     "type",
	"struct":        "type",
	"typeParameter": "type.parameter",
	"parameter":     "variable.parameter",
	"variable":      "variable",
	"property":      "variable.other.member",
	"enumMember":    "type.enum.variant",
	"event":         "variable.other.member",
	"function":      "function",
	"method":        "function.method",
	"macro":         "function.macro",
	"keyword":       "keyword",
	"modifier":      "keyword.storage.modifier",
	"comment":       "comment",
	"string":        "string",
	"number":        "constant.numeric",
	"regexp":        "string.regexp",
	"operator":      "operator",
	"decorator":     "attribute",
}

// SemanticScope is the theme scope of a token, "" for a type the editor
// does not color. A default library name is a builtin, read only data a
// constant unless it is a builtin variable.
func SemanticScope(typ string, modifiers []string) string {
	scope := semanticScopes[typ]
	if scope == "" {
		return ""
	}

	has := func(m string) bool {
		for _, x := range modifiers {
			if x == m {
				return true
			}
		}

		return false
	}

	switch {
	case has("defaultLibrary") && (typ == "function" || typ == "variable" || typ == "type" || typ == "namespace"):
		return strings.SplitN(scope, ".", 2)[0] + ".builtin"
	case has("readonly") && typ == "variable":
		return "constant"
	}

	return scope
}
