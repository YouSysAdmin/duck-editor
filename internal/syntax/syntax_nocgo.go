//go:build !cgo

package syntax

import (
	"errors"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// Supported reports whether tree-sitter is compiled in.
const Supported = false

// Loader resolves languages to grammars and queries.
type Loader struct {
	runtimeDirs []string
	embedded    EmbeddedGrammars
	langs       *Languages
}

// NewLoader creates a loader over the runtime dirs and language config.
func NewLoader(runtimeDirs []string, langs *Languages) *Loader {
	return &Loader{runtimeDirs: runtimeDirs, langs: langs}
}

// ReplaceLanguages swaps the language configuration, used by config
// reloads. Already parsed grammars stay cached.
func (l *Loader) ReplaceLanguages(langs *Languages) {
	l.langs = langs
}

// Languages returns the language configuration.
func (l *Loader) Languages() *Languages { return l.langs }

// RuntimeDirs returns the runtime search path.
func (l *Loader) RuntimeDirs() []string { return l.runtimeDirs }

// Syntax is unavailable without cgo.
type Syntax struct{}

// Highlight is one styled byte range.
type Highlight struct {
	Start int
	End   int
	Scope string
}

// New always fails without cgo.
func New(*Loader, *Language, buffer.Rope) (*Syntax, error) {
	return nil, errors.New("tree-sitter requires cgo")
}

// Language returns nil.
func (s *Syntax) Language() *Language { return nil }

// Close does nothing.
func (s *Syntax) Close() {}

// Update does nothing.
func (s *Syntax) Update(buffer.Rope, buffer.Rope, *edit.ChangeSet) {}

// Highlights returns nothing.
func (s *Syntax) Highlights(int, int) []Highlight { return nil }

// MatchingBracket finds nothing.
func (s *Syntax) MatchingBracket(buffer.Rope, int, bool) (int, bool) { return 0, false }

// ExpandSelection returns the selection unchanged.
func (s *Syntax) ExpandSelection(_ buffer.Rope, sel edit.Selection) edit.Selection { return sel }

// ShrinkSelection returns the selection unchanged.
func (s *Syntax) ShrinkSelection(_ buffer.Rope, sel edit.Selection) edit.Selection { return sel }

// SelectSibling returns the selection unchanged.
func (s *Syntax) SelectSibling(_ buffer.Rope, sel edit.Selection, _ edit.Direction) edit.Selection {
	return sel
}

// SelectAllSiblings returns the selection unchanged.
func (s *Syntax) SelectAllSiblings(_ buffer.Rope, sel edit.Selection) edit.Selection { return sel }

// SelectAllChildren returns the selection unchanged.
func (s *Syntax) SelectAllChildren(_ buffer.Rope, sel edit.Selection) edit.Selection { return sel }

// MoveParentNodeEnd returns the selection unchanged.
func (s *Syntax) MoveParentNodeEnd(_ buffer.Rope, sel edit.Selection, _ edit.Direction, _ bool) edit.Selection {
	return sel
}

// TextObject finds nothing.
func (s *Syntax) TextObject(_ buffer.Rope, r edit.Range, _ edit.TextObjectKind, _ string) (edit.Range, bool) {
	return r, false
}

// GotoTextObject returns the range unchanged.
func (s *Syntax) GotoTextObject(_ buffer.Rope, r edit.Range, _ string, _ edit.Direction, _ int) edit.Range {
	return r
}

// Tag is a symbol definition found by the tags query.
type Tag struct {
	Kind  string
	Name  string
	Start int
	End   int
}

// Tags lists the symbol definitions, none without tree-sitter.
func (s *Syntax) Tags(buffer.Rope) []Tag { return nil }

// Scopes lists the node kinds at a position, none without tree-sitter.
func (s *Syntax) Scopes(buffer.Rope, int) []string { return nil }

// Subtree renders the tree around a range, empty without tree-sitter.
func (s *Syntax) Subtree(buffer.Rope, edit.Range) string { return "" }

// HighlightNamesAt lists the scopes at a position, none without tree-sitter.
func (s *Syntax) HighlightNamesAt(buffer.Rope, int) []string { return nil }

// Layers lists the parsed languages, none without tree-sitter.
func (s *Syntax) Layers() []string { return nil }

// HasIndentQuery reports an indent query, never without tree-sitter.
func (s *Syntax) HasIndentQuery() bool { return false }

// IndentFor computes an indent, never without tree-sitter.
func (s *Syntax) IndentFor(buffer.Rope, int, bool) (Indent, bool) { return Indent{Align: -1}, false }

// Indent is the result of the indent query.
type Indent struct {
	Level  int
	Align  int
	Opaque bool
}

// RainbowBrackets lists bracket depths, none without tree-sitter.
func (s *Syntax) RainbowBrackets(int, int) []Highlight { return nil }

// Check fails, grammars need a cgo build.
func (l *Loader) Check(*Language) error {
	return errors.New("built without cgo, no grammars")
}
