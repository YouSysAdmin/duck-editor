// Package atomicfile writes files through a temp file in the target
// directory and a rename, so readers and interrupted runs never see a
// partial file.
package atomicfile

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces path with data. The directory has to exist.
func Write(path string, data []byte, perm fs.FileMode) error {
	return WriteFrom(path, bytes.NewReader(data), perm)
}

// WriteFrom replaces path with everything r yields. The temp file has a
// unique name, so writers of the same path do not disturb each other.
func WriteFrom(path string, r io.Reader, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	name := tmp.Name()
	_, err = io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = os.Chmod(name, perm)
	}

	if err == nil {
		err = os.Rename(name, path)
	}

	if err != nil {
		_ = os.Remove(name)
	}

	return err
}
