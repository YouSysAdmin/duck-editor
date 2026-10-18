package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// ErrNotTracked is returned when a file has no committed base.
var ErrNotTracked = errors.New("file is not tracked")

// git runs git in dir on h and returns its output.
func git(h host.Host, dir string, args ...string) ([]byte, error) {
	out, _, err := h.Run(context.Background(), host.Command{Name: "git", Args: append([]string{"-C", dir}, args...)})

	return out, err
}

// Root returns the repository root holding path, or an error when the
// path is not inside a git work tree.
func Root(h host.Host, path string) (string, error) {
	dir := path
	if info, err := h.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}

	out, err := git(h, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

// Base returns the HEAD version of a file, path is absolute. The path is
// resolved by git relative to the file directory, so symlinked
// directories work.
func Base(h host.Host, path string) ([]byte, error) {
	out, err := git(h, filepath.Dir(path), "show", "HEAD:./"+filepath.Base(path))
	if err == nil {
		return out, nil
	}

	var exitErr *host.ExitError
	if !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("git show: %w", err)
	}

	return nil, fmt.Errorf("%w: %s", ErrNotTracked, strings.TrimSpace(string(exitErr.Stderr)))
}

// FileStatus is one entry of the working tree status.
type FileStatus struct {
	// Path is relative to the repository root.
	Path string

	// Kind is one of + (added), ~ (modified), - (deleted), > (renamed)
	// or ! (conflict).
	Kind rune
}

// Status lists the files that differ from HEAD, untracked ones included.
func Status(h host.Host, dir string) (string, []FileStatus, error) {
	root, err := Root(h, dir)
	if err != nil {
		return "", nil, err
	}

	out, err := git(h, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", nil, err
	}

	var files []FileStatus
	entries := bytes.Split(out, []byte{0})
	for i := 0; i < len(entries); i++ {
		entry := string(entries[i])
		if len(entry) < 4 {
			continue
		}

		code := entry[:2]
		path := entry[3:]
		kind := statusKind(code)
		if code[0] == 'R' || code[0] == 'C' {
			// Renames carry the original path in the next entry.
			i++
		}

		files = append(files, FileStatus{Path: path, Kind: kind})
	}

	return root, files, nil
}

func statusKind(code string) rune {
	switch {
	case strings.ContainsRune(code, 'U') || code == "AA" || code == "DD":
		return '!'
	case code == "??" || strings.ContainsRune(code, 'A'):
		return '+'
	case strings.ContainsRune(code, 'D'):
		return '-'
	case code[0] == 'R' || code[0] == 'C':
		return '>'
	default:
		return '~'
	}
}

// Branch returns the current branch name of the repository holding
// path, or the short commit hash when detached.
func Branch(h host.Host, path string) (string, error) {
	root, err := Root(h, path)
	if err != nil {
		return "", err
	}

	out, err := git(h, root, "symbolic-ref", "--short", "-q", "HEAD")
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		return strings.TrimSpace(string(out)), nil
	}

	out, err = git(h, root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}
