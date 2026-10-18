package walk

import (
	"path/filepath"
	"strings"
)

// DirFilter decides which entries of one directory a listing shows,
// with the rules Files applies. The ignore files are read through the
// given functions, so the directory may be on another host.
type DirFilter struct {
	dir      string
	opts     Options
	matchers []*Matcher
}

// NewDirFilter loads the ignore rules for dir the way Files applies them
// to a walk from the repository root, or from dir itself outside a
// repository. exists reports which of the paths exist, read returns a
// file.
func NewDirFilter(dir string, opts Options, exists func(paths []string) []bool, read func(path string) ([]byte, error)) *DirFilter {
	f := &DirFilter{dir: dir, opts: opts}
	if !opts.GitIgnore && !opts.Ignore && !opts.GitExclude {
		return f
	}

	// The repository root is the nearest ancestor holding .git, the rules
	// of every directory from there down to dir apply.
	var ancestors, candidates []string
	for d := dir; ; d = filepath.Dir(d) {
		ancestors = append(ancestors, d)
		candidates = append(candidates, filepath.Join(d, ".git"))
		if filepath.Dir(d) == d {
			break
		}
	}

	found := exists(candidates)
	top := 0
	gitRoot := ""
	for i, ok := range found {
		if ok {
			top, gitRoot = i, ancestors[i]

			break
		}
	}

	load := func(path, base string) {
		if data, err := read(path); err == nil {
			f.matchers = append(f.matchers, ParseIgnore(string(data), base))
		}
	}
	if opts.GitExclude && gitRoot != "" {
		load(filepath.Join(gitRoot, ".git", "info", "exclude"), gitRoot)
	}

	for i := top; i >= 0; i-- {
		d := ancestors[i]
		if opts.GitIgnore {
			load(filepath.Join(d, ".gitignore"), d)
		}

		if opts.Ignore {
			load(filepath.Join(d, ".ignore"), d)
		}
	}

	return f
}

// Skip reports whether an entry of the directory is left out.
func (f *DirFilter) Skip(name string, isDir bool) bool {
	if name == ".git" || f.opts.Hidden && strings.HasPrefix(name, ".") {
		return true
	}

	w := walker{}

	return w.ignored(filepath.Join(f.dir, name), isDir, f.matchers)
}
