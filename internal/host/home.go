package host

import (
	"path/filepath"
	"strings"
)

// HasTilde reports whether path starts in the home directory, as ~ or
// ~/rest.
func HasTilde(path string) bool {
	return path == "~" || strings.HasPrefix(path, "~/")
}

// ExpandTilde replaces a leading ~ of path with home.
func ExpandTilde(home, path string) string {
	if !HasTilde(path) {
		return path
	}

	return filepath.Join(home, path[1:])
}

// ExpandHome replaces a leading ~ of path with the home directory of h.
// The path comes back unchanged with the error when h has none.
func ExpandHome(h Host, path string) (string, error) {
	if !HasTilde(path) {
		return path, nil
	}

	home, err := h.Home()
	if err != nil {
		return path, err
	}

	return filepath.Join(home, path[1:]), nil
}
