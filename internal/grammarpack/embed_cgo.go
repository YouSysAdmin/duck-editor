//go:build cgo

package grammarpack

import "embed"

// libFiles holds lib/<name>.so.gz, only cgo builds can load them.
//
//go:embed lib
var libFiles embed.FS

var embedded = sub(libFiles)
