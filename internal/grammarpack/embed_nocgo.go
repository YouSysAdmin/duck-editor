//go:build !cgo

package grammarpack

import "embed"

// embedded is empty without cgo, grammars cannot be loaded there.
var embedded embed.FS
