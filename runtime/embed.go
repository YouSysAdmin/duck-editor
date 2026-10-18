// Package runtime embeds the help page. The runtime directory also
// receives the compiled grammars, which are loaded from disk or from
// the copies embedded into the binary.
package runtime

import "embed"

// FS holds help.md.
//
//go:embed help.md
var FS embed.FS
