package editor

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// diskCheckInterval is how often open files are compared with disk.
const diskCheckInterval = 3 * time.Second

// WatchFiles starts comparing open files with disk in the background,
// see CheckDiskChanges.
func (e *Editor) WatchFiles() {
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		ticker := time.NewTicker(diskCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				emit(func(e *Editor) { e.CheckDiskChanges() })
			case <-ctx.Done():
				return
			}
		}
	})
}

// FocusGained runs when the terminal gets the focus back: files and the
// git state may have changed meanwhile.
func (e *Editor) FocusGained() {
	e.CheckDiskChanges()
	e.refreshVCS()
}

// refreshVCS reloads the branch and committed base of the documents
// shown in a view.
func (e *Editor) refreshVCS() {
	shown := map[DocID]bool{}
	for _, v := range e.Views {
		shown[v.DocID] = true
	}

	for _, doc := range e.documentsByID() {
		if shown[doc.ID] {
			e.attachVCS(doc)
		}
	}
}

// CheckDiskChanges handles files another program changed: a buffer
// without own edits takes the new text, undo brings the old one back, a
// buffer with edits gets one warning per change.
// Files on a remote host are checked in the background, a slow
// connection must not hold up the editor.
func (e *Editor) CheckDiskChanges() {
	type remoteFile struct {
		id   DocID
		host host.Host
		path string
	}

	var remote []remoteFile
	for _, doc := range e.documentsByID() {
		if doc.Path() == "" || doc.diskModTime.IsZero() {
			continue
		}

		if h := doc.fileHost(); h.Name() != "" {
			remote = append(remote, remoteFile{doc.ID, h, doc.Path()})

			continue
		}

		info, err := doc.fileHost().Stat(doc.Path())
		switch {
		case err == nil:
			e.diskTime(doc, info.ModTime)
		case errors.Is(err, fs.ErrNotExist):
			e.diskGone(doc)
		}
	}

	if len(remote) == 0 || e.remoteDiskCheck {
		return
	}

	e.remoteDiskCheck = true
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		times := make([]time.Time, len(remote))
		gone := make([]bool, len(remote))
		for i, f := range remote {
			info, err := f.host.Stat(f.path)
			switch {
			case err == nil:
				times[i] = info.ModTime
			case errors.Is(err, fs.ErrNotExist):
				gone[i] = true
			}
		}

		emit(func(e *Editor) {
			e.remoteDiskCheck = false
			for i, f := range remote {
				doc, ok := e.Docs[f.id]
				switch {
				case !ok || doc.Path() != f.path:
				case gone[i]:
					e.diskGone(doc)
				case !times[i].IsZero():
					e.diskTime(doc, times[i])
				}
			}
		})
	})
}

// diskGone handles the file of doc deleted outside the editor: the
// buffer counts as modified against the missing file, so quitting asks
// before its text is lost.
func (e *Editor) diskGone(doc *Document) {
	doc.diskModTime = time.Time{}
	doc.markSavedAs(buffer.NewRope(""))
	e.SetError("'%s' was deleted on disk, :w writes it again", e.docName(doc))
}

// diskTime handles the modification time found for the file of doc. Any
// other time counts, a copy that keeps an older time changed it too.
func (e *Editor) diskTime(doc *Document, mod time.Time) {
	if doc.diskModTime.IsZero() || mod.Equal(doc.diskModTime) {
		return
	}

	// An encrypted file is decrypted again only on :reload, a key service
	// may ask for something.
	if !doc.IsModified() && doc.secret == nil {
		if err := reloadDocument(e, doc); err != nil {
			e.SetError("%v", err)

			return
		}

		// A checkout changes files together with the branch and base.
		e.attachVCS(doc)
		e.SetStatus("'%s' changed on disk and was reloaded", e.docName(doc))

		return
	}

	if doc.diskWarned.Equal(mod) {
		return
	}

	doc.diskWarned = mod
	e.SetError("'%s' changed on disk, :reload takes that version, :w! keeps yours", e.docName(doc))
}
