package editor

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// maxPositions bounds the remembered cursor positions, the oldest go.
const maxPositions = 1000

// maxUndoFileText is the largest file whose history is kept on disk.
const maxUndoFileText = 4 << 20

// savedPosition is where the cursor was when a file was left.
type savedPosition struct {
	Line int   `json:"line"`
	Col  int   `json:"col"`
	Time int64 `json:"time"`
}

// undoFile is the stored history of a file and the text it belongs to.
type undoFile struct {
	Hash    string        `json:"hash"`
	History *edit.History `json:"history"`
}

// SetStateDir turns session persistence on, storing in dir, and restores
// the documents already open.
func (e *Editor) SetStateDir(dir string) {
	e.stateDir = dir
	for _, doc := range e.documentsByID() {
		e.restoreUndo(doc)
		e.restorePosition(doc)
	}
}

func (e *Editor) persisting() bool {
	return e.stateDir != "" && e.Config.Editor.PersistSession
}

// restoreUndo gives a freshly opened file its stored history, when the
// file is still the text it was saved as.
func (e *Editor) restoreUndo(doc *Document) {
	if !e.persisting() || doc.Path() == "" {
		return
	}

	text := doc.Text().String()
	if data, err := os.ReadFile(e.undoPath(e.sessionKey(doc))); err == nil && len(text) <= maxUndoFileText {
		var stored undoFile
		if json.Unmarshal(data, &stored) == nil && stored.History != nil && stored.Hash == textHash(text) {
			doc.history = stored.History
			doc.markSaved()
		}
	}
}

// restorePosition puts the cursor of a file shown for the first time
// where it was left last time.
func (e *Editor) restorePosition(doc *Document) {
	if !e.persisting() || doc.Path() == "" {
		return
	}

	// A cursor something already moved stays where it is.
	t := doc.Text()
	pos, ok := e.loadPositions()[e.sessionKey(doc)]
	if !ok || doc.Selection().Primary().Cursor(t) != 0 {
		return
	}

	line := min(pos.Line, t.LenLines()-1)
	start := t.LineToChar(line)
	end := t.LineEndCharIndex(line)
	cursor := min(start+pos.Col, end)
	doc.SetSelection(edit.SingleSelection(cursor, cursor+min(1, t.Len()-cursor)))
}

// saveUndo stores the history of a just saved document.
func (e *Editor) saveUndo(doc *Document) {
	// The history of a decrypted secret holds its text.
	if !e.persisting() || doc.Path() == "" || doc.secret != nil {
		return
	}

	text := doc.Text().String()
	if len(text) > maxUndoFileText {
		return
	}

	data, err := json.Marshal(undoFile{Hash: textHash(text), History: doc.history})
	if err != nil {
		e.logf("undo history of %s: %v", doc.Path(), err)

		return
	}

	if err := writeFileAtomic(e.undoPath(e.sessionKey(doc)), data); err != nil {
		e.logf("undo history of %s: %v", doc.Path(), err)
	}
}

// rememberPosition notes the cursor of a document being left.
func (e *Editor) rememberPosition(doc *Document) {
	if !e.persisting() || doc.Path() == "" {
		return
	}

	t := doc.Text()
	cursor := doc.Selection().Primary().Cursor(t)
	line := t.CharToLine(cursor)
	e.loadPositions()[e.sessionKey(doc)] = savedPosition{Line: line, Col: cursor - t.LineToChar(line), Time: time.Now().Unix()}
}

// SaveSession records the positions of the open documents and writes
// the position file, called when the editor exits.
func (e *Editor) SaveSession() {
	if !e.persisting() {
		return
	}

	for _, doc := range e.Docs {
		e.rememberPosition(doc)
	}

	positions := e.loadPositions()
	if len(positions) > maxPositions {
		paths := slices.SortedFunc(maps.Keys(positions), func(a, b string) int {
			return cmp.Compare(positions[b].Time, positions[a].Time)
		})
		for _, path := range paths[maxPositions:] {
			delete(positions, path)
		}
	}

	data, err := json.Marshal(positions)
	if err == nil {
		err = writeFileAtomic(filepath.Join(e.stateDir, "positions.json"), data)
	}

	if err != nil {
		e.logf("cursor positions: %v", err)
	}

	e.saveBufferList()
}

// bufferList is the set of files open in a working directory.
type bufferList struct {
	Files   []string `json:"files"`
	Current string   `json:"current"`
}

// bufferListPath is the buffer list of the working directory on the
// workspace host.
func (e *Editor) bufferListPath() string {
	sum := sha256.Sum256([]byte(e.Host.Name() + e.WorkingDir()))

	return filepath.Join(e.stateDir, "sessions", hex.EncodeToString(sum[:16])+".json")
}

// saveBufferList records the files open on the workspace host. A session
// without files leaves the last list in place.
func (e *Editor) saveBufferList() {
	var list bufferList
	for _, doc := range e.documentsByID() {
		if doc.Path() != "" && e.hostOf(doc).Name() == e.Host.Name() {
			list.Files = append(list.Files, doc.Path())
		}
	}

	if len(list.Files) == 0 {
		return
	}

	if doc := e.Doc(); doc.Path() != "" {
		list.Current = doc.Path()
	}

	data, err := json.Marshal(list)
	if err == nil {
		err = writeFileAtomic(e.bufferListPath(), data)
	}

	if err != nil {
		e.logf("buffer list: %v", err)
	}
}

// RestoreSession opens the files of the last session in the working
// directory and shows the one that was current. An empty scratch buffer
// the editor started with goes away. It reports how many files opened.
func (e *Editor) RestoreSession() (int, error) {
	if e.stateDir == "" {
		return 0, cmdError("session state is off")
	}

	data, err := os.ReadFile(e.bufferListPath())
	if err != nil {
		return 0, cmdError("no saved session for " + e.WorkingDir())
	}

	var list bufferList
	if err := json.Unmarshal(data, &list); err != nil {
		return 0, err
	}

	start := e.Doc()
	found, _ := e.Host.Exists(list.Files)
	opened := 0
	for i, path := range list.Files {
		if i < len(found) && !found[i] {
			continue
		}

		if _, err := e.Open(path, ActionLoad); err == nil {
			opened++
		}
	}

	if list.Current != "" {
		if _, err := e.Open(list.Current, ActionReplace); err != nil {
			e.logf("restore %s: %v", list.Current, err)
		}
	}

	if opened > 0 && start.Path() == "" && !start.IsModified() && start.Text().Len() == 0 && e.Doc() != start {
		_ = e.CloseDocument(start.ID, false)
	}

	return opened, nil
}

func (e *Editor) loadPositions() map[string]savedPosition {
	if e.positions == nil {
		e.positions = map[string]savedPosition{}
		if data, err := os.ReadFile(filepath.Join(e.stateDir, "positions.json")); err == nil {
			_ = json.Unmarshal(data, &e.positions)
		}
	}

	return e.positions
}

// sessionKey names the file of doc in the session state, the path for a
// local file and the host URL with the path for a remote one.
func (e *Editor) sessionKey(doc *Document) string {
	if name := e.hostOf(doc).Name(); name != "" {
		return name + doc.Path()
	}

	return doc.Path()
}

func (e *Editor) undoPath(path string) string {
	sum := sha256.Sum256([]byte(path))

	return filepath.Join(e.stateDir, "undo", hex.EncodeToString(sum[:16])+".json")
}

func textHash(text string) string {
	sum := sha256.Sum256([]byte(text))

	return hex.EncodeToString(sum[:])
}

// writeFileAtomic writes data through a temp file in the same directory.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}

	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}

	if err != nil {
		_ = os.Remove(tmp.Name())
	}

	return err
}

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"session-restore"},
		doc:   "Open the files of the last session in the working directory",
		run: func(cx *Context, _ []string) error {
			n, err := cx.Editor.RestoreSession()
			if err != nil {
				return err
			}

			cx.Editor.SetStatus("restored %s", plural(n, "file"))

			return nil
		},
	})
}
