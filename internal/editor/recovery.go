package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// SaveRecovery writes every buffer with unsaved changes into dir after a
// crash and returns the original names with the files written.
func (e *Editor) SaveRecovery(dir string) ([][2]string, error) {
	var saved [][2]string
	stamp := time.Now().Format("20060102-150405")
	for _, doc := range e.documentsByID() {
		// Decrypted secrets never land on disk in the clear.
		if !doc.IsModified() || doc.secret != nil {
			continue
		}

		if err := os.MkdirAll(dir, 0o700); err != nil {
			return saved, err
		}

		name := "scratch"
		if doc.Path() != "" {
			name = filepath.Base(doc.Path())
		}

		path := filepath.Join(dir, fmt.Sprintf("%s-%d-%s", stamp, doc.ID, name))
		if err := os.WriteFile(path, []byte(doc.Text().String()), 0o600); err != nil {
			return saved, err
		}

		saved = append(saved, [2]string{doc.displayName(), path})
	}

	return saved, nil
}

// JobPanic turns a panic of a background job into an error on the
// editor thread, with the stack in the log, so the editor keeps running.
func JobPanic(r any) JobResult {
	stack := debug.Stack()

	return func(e *Editor) {
		e.logf("background job panicked: %v\n%s", r, stack)
		e.SetError("background job failed: %v, :log-open has the details", r)
	}
}
