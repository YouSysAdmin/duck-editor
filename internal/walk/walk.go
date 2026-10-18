package walk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Options control which files are listed.
type Options struct {
	// Hidden skips dot files and directories.
	Hidden bool

	// GitIgnore honors .gitignore files, including those of parent
	// directories up to the repository root.
	GitIgnore bool

	// Ignore honors .ignore files.
	Ignore bool

	// GitExclude honors .git/info/exclude.
	GitExclude bool

	// FollowSymlinks descends into symlinked directories.
	FollowSymlinks bool

	// MaxDepth limits recursion, zero means unlimited.
	MaxDepth int
}

// DefaultOptions are the file picker defaults.
func DefaultOptions() Options {
	return Options{Hidden: true, GitIgnore: true, Ignore: true, GitExclude: true, FollowSymlinks: true}
}

// Files walks root and calls fn with every file path, relative to root,
// in sorted directory order. It stops when fn returns false or ctx ends,
// returning the context error in the latter case. Unreadable directories
// are skipped.
func Files(ctx context.Context, root string, opts Options, fn func(rel string) bool) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	var matchers []*Matcher
	if opts.GitIgnore {
		matchers = append(matchers, parentIgnores(root)...)
	}

	if opts.GitExclude {
		if gitRoot, ok := findGitRoot(root); ok {
			if m := loadIgnoreFile(filepath.Join(gitRoot, ".git", "info", "exclude"), gitRoot); m != nil {
				matchers = append(matchers, m)
			}
		}
	}

	w := &walker{root: root, opts: opts, fn: fn, ctx: ctx, seen: map[string]bool{}}

	return w.walkDir(root, matchers, 0)
}

type walker struct {
	root string
	opts Options
	fn   func(rel string) bool
	ctx  context.Context
	seen map[string]bool
	done bool
}

func (w *walker) walkDir(dir string, matchers []*Matcher, depth int) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}

	if w.done || w.opts.MaxDepth > 0 && depth > w.opts.MaxDepth {
		return nil
	}

	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		if w.seen[resolved] {
			return nil
		}

		w.seen[resolved] = true
	}

	if w.opts.GitIgnore {
		if m := loadIgnoreFile(filepath.Join(dir, ".gitignore"), dir); m != nil {
			matchers = append(matchers, m)
		}
	}

	if w.opts.Ignore {
		if m := loadIgnoreFile(filepath.Join(dir, ".ignore"), dir); m != nil {
			matchers = append(matchers, m)
		}
	}

	// ReadDir returns the entries sorted by name.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if w.done {
			return nil
		}

		if err := w.ctx.Err(); err != nil {
			return err
		}

		name := entry.Name()
		if name == ".git" {
			continue
		}

		if w.opts.Hidden && strings.HasPrefix(name, ".") {
			continue
		}

		// Pipes, sockets and devices hold no text to pick or search.
		if entry.Type()&(os.ModeNamedPipe|os.ModeSocket|os.ModeDevice|os.ModeCharDevice) != 0 {
			continue
		}

		path := filepath.Join(dir, name)
		isDir := entry.IsDir()
		if !isDir && entry.Type()&os.ModeSymlink != 0 {
			if !w.opts.FollowSymlinks {
				continue
			}

			if info, err := os.Stat(path); err == nil && info.IsDir() {
				isDir = true
			}
		}

		if w.ignored(path, isDir, matchers) {
			continue
		}

		if isDir {
			if err := w.walkDir(path, matchers, depth+1); err != nil {
				return err
			}

			continue
		}

		rel, ok := relTo(w.root, path)
		if !ok {
			rel = name
		}

		if !w.fn(rel) {
			w.done = true

			return nil
		}
	}

	return nil
}

func (w *walker) ignored(path string, isDir bool, matchers []*Matcher) bool {
	ignored := false
	for _, m := range matchers {
		rel, ok := relTo(m.base, path)
		if !ok {
			continue
		}

		if ig, matched := m.Match(rel, isDir); matched {
			ignored = ig
		}
	}

	return ignored
}

// parentIgnores loads .gitignore files from the ancestors of root up to
// the repository root so nested project directories respect them.
func parentIgnores(root string) []*Matcher {
	gitRoot, ok := findGitRoot(root)
	if !ok {
		return nil
	}

	var out []*Matcher
	dir := gitRoot
	for dir != root {
		if m := loadIgnoreFile(filepath.Join(dir, ".gitignore"), dir); m != nil {
			out = append(out, m)
		}

		rel, err := filepath.Rel(dir, root)
		if err != nil || rel == "." {
			break
		}

		next := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		dir = filepath.Join(dir, next)
	}

	return out
}

func findGitRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}

		dir = parent
	}
}
