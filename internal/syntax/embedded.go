package syntax

import "errors"

// EmbeddedGrammars returns the path of a grammar library shipped inside
// the binary, ErrGrammarNotEmbedded when it has none for name.
type EmbeddedGrammars func(name string) (string, error)

// ErrGrammarNotEmbedded reports a grammar the binary does not carry.
var ErrGrammarNotEmbedded = errors.New("grammar not embedded")

// WithEmbedded makes the loader fall back to grammars inside the binary
// when no runtime directory has a library.
func (l *Loader) WithEmbedded(embedded EmbeddedGrammars) *Loader {
	l.embedded = embedded

	return l
}
