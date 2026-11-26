package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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

	return root, parseStatus(out), nil
}

// InRepository reports whether dir lies in a git work tree: it or a
// directory above it holds .git, a directory or the file of a worktree
// or submodule. All candidates are checked in one Exists call.
func InRepository(h host.Host, dir string) bool {
	var candidates []string
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		candidates = append(candidates, filepath.Join(d, ".git"))
		if filepath.Dir(d) == d {
			break
		}
	}

	found, err := h.Exists(candidates)
	if err != nil {
		return false
	}

	return slices.Contains(found, true)
}

// DirStatus maps the entries of dir that differ from HEAD to their kind.
// A directory takes the kind of the files below it, ~ when they differ.
func DirStatus(ctx context.Context, h host.Host, dir string) (map[string]rune, error) {
	run := func(args ...string) ([]byte, error) {
		out, _, err := h.Run(ctx, host.Command{Name: "git", Args: append([]string{"-C", dir}, args...)})

		return out, err
	}

	// The prefix comes from git, so links in dir do not matter.
	out, err := run("rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}

	prefix := strings.TrimSpace(string(out))
	out, err = run("status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	if err != nil {
		return nil, err
	}

	kinds := map[string]rune{}
	for _, f := range parseStatus(out) {
		below, ok := strings.CutPrefix(f.Path, prefix)
		if !ok || below == "" {
			continue
		}

		name, _, nested := strings.Cut(strings.TrimSuffix(below, "/"), "/")
		kind := f.Kind
		if old, seen := kinds[name]; seen && old != kind && nested {
			kind = '~'
		}

		kinds[name] = kind
	}

	return kinds, nil
}

// parseStatus reads the output of git status --porcelain=v1 -z.
func parseStatus(out []byte) []FileStatus {
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

	return files
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
