//go:build unix

package host

import (
	"io/fs"
	"os"
	"syscall"
)

// sharedFile reports whether replacing the file with a new one would
// break it for others: it has more hard links, or another owner the
// replacement would not keep.
func sharedFile(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}

	return st.Nlink > 1 || int(st.Uid) != os.Geteuid()
}
