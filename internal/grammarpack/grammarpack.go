// Package grammarpack serves the tree-sitter grammar libraries embedded
// into the binary. dlopen needs a file, so a library is extracted into a
// cache directory the first time it is used. The directory is named after
// a hash of the embedded set, a binary with other grammars uses a fresh
// one and stale directories are removed.
package grammarpack

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// ErrNotEmbedded is returned for a grammar the binary does not carry.
var ErrNotEmbedded = errors.New("grammar not embedded")

// suffix is the name suffix of an embedded library.
const suffix = ".so.gz"

// Pack is a set of compressed grammar libraries.
type Pack struct {
	files fs.FS

	once    sync.Once
	version string
}

// New wraps a file system holding <name>.so.gz files at its root.
func New(files fs.FS) *Pack {
	return &Pack{files: files}
}

// Default is the set embedded into this binary, empty when none was
// packed at build time.
var Default = New(embedded)

// Names lists the embedded grammars.
func (p *Pack) Names() []string {
	entries, err := fs.ReadDir(p.files, ".")
	if err != nil {
		return nil
	}

	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), suffix); ok && !e.IsDir() {
			names = append(names, name)
		}
	}

	return names
}

// Version identifies the embedded set, empty when it is empty.
func (p *Pack) Version() string {
	p.once.Do(func() {
		names := p.Names()
		if len(names) == 0 {
			return
		}

		slices.Sort(names)
		h := sha256.New()
		for _, name := range names {
			data, err := fs.ReadFile(p.files, name+suffix)
			if err != nil {
				continue
			}

			_, _ = fmt.Fprintf(h, "%s %d\n", name, len(data))
			_, _ = h.Write(data)
		}

		p.version = hex.EncodeToString(h.Sum(nil))[:16]
	})

	return p.version
}

// Path returns the file of grammar name below cacheRoot, extracting it
// when it is not there yet.
func (p *Pack) Path(cacheRoot, name string) (string, error) {
	version := p.Version()
	if version == "" || strings.ContainsAny(name, `/\`) {
		return "", ErrNotEmbedded
	}

	src := name + suffix
	if _, err := fs.Stat(p.files, src); err != nil {
		return "", ErrNotEmbedded
	}

	dir := filepath.Join(cacheRoot, version)
	dst := filepath.Join(dir, name+".so")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("grammar cache: %w", err)
	}

	removeStale(cacheRoot, version)
	if err := p.extract(src, dst); err != nil {
		return "", fmt.Errorf("extract grammar %s: %w", name, err)
	}

	return dst, nil
}

// extract decompresses src into dst through a temp file, so concurrent
// editors and interrupted runs never leave a partial library behind.
func (p *Pack) extract(src, dst string) error {
	in, err := p.files.Open(src)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	zr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".extract-*")
	if err != nil {
		return err
	}

	name := tmp.Name()
	_, err = io.Copy(tmp, zr)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = os.Chmod(name, 0o755)
	}

	if err == nil {
		err = os.Rename(name, dst)
	}

	if err != nil {
		_ = os.Remove(name)
	}

	return err
}

// removeStale deletes cache directories of other grammar sets. Libraries
// a running editor already loaded stay mapped after their files go.
func removeStale(cacheRoot, keep string) {
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() && e.Name() != keep && isVersion(e.Name()) {
			_ = os.RemoveAll(filepath.Join(cacheRoot, e.Name()))
		}
	}
}

// isVersion matches the directory names Version produces, so nothing
// else under the cache root is touched.
func isVersion(name string) bool {
	if len(name) != 16 {
		return false
	}

	_, err := hex.DecodeString(name)

	return err == nil
}

// sub returns the lib directory of an embedded file system.
func sub(files fs.FS) fs.FS {
	dir, err := fs.Sub(files, "lib")
	if err != nil {
		return files
	}

	return dir
}
