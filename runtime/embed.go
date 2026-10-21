// Package runtime embeds the runtime directory the editor ships: the
// help page, the language catalogue languages.yaml with every language,
// grammar source and language server, the tree-sitter queries of those
// languages and the built in theme. The directory also receives the
// compiled grammars, which are loaded from disk or from the copies
// embedded into the binary, never from here.
package runtime

import "embed"

// FS holds help.md, languages.yaml, queries/ and themes/default.yaml.
//
//go:embed help.md languages.yaml queries themes/default.yaml
var FS embed.FS
