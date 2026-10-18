//go:build !unix

package host

import "io/fs"

// sharedFile reports whether replacing the file would break it for others.
func sharedFile(fs.FileInfo) bool {
	return false
}
