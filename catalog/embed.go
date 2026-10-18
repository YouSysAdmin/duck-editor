// Package catalog embeds the language catalogue the editor ships:
// languages.yaml with every language, grammar source and language
// server, and the tree-sitter queries of those languages.
package catalog

import "embed"

// FS holds languages.yaml and queries/.
//
//go:embed languages.yaml queries
var FS embed.FS
