// Package editor holds the editor state, documents, and the commands that
// act on them.
package editor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

const defaultTabWidth = 4

// IndentStyle is the unit inserted for one indentation level.
type IndentStyle struct {
	Tabs  bool
	Width int
}

// String returns the indent unit as text.
func (s IndentStyle) String() string {
	if s.Tabs {
		return "\t"
	}

	return strings.Repeat(" ", s.Width)
}

// IndentWidth returns the visual width of one indent unit.
func (s IndentStyle) IndentWidth(tabWidth int) int {
	if s.Tabs {
		return tabWidth
	}

	return s.Width
}

// Document is an open file with its selection, undo history and view
// position. Changes are applied through transactions and composed until
// they are committed to history at a command boundary.
type Document struct {
	ID         DocID
	path       string
	text       buffer.Rope
	lineEnding buffer.LineEnding
	indent     IndentStyle
	tabWidth   int

	// selections holds one selection per view showing the document.
	// focused is the view that Selection and SetSelection act on.
	selections map[ViewID]edit.Selection
	focused    ViewID
	history    *edit.History
	changes    edit.ChangeSet
	oldState   *edit.State

	// lastSavedRevision and savedText describe the file as last written
	// or read. modified caches the comparison for one edits count.
	lastSavedRevision int
	savedText         buffer.Rope
	modified          modifiedCache

	// indentDetected is set when the indent style came from the file.
	indentDetected bool
	language       *syntax.Language
	syntax         *syntax.Syntax

	// RestoreCursor moves the cursor back one grapheme when leaving
	// append mode.
	RestoreCursor bool

	// lastAccess orders buffers for the buffer picker and ga, and
	// lastModified tracks the latest edit for gm.
	lastAccess   int64
	lastModified int64

	// onChange lets the editor map jumplists and sync servers.
	onChange func(d *Document, oldText buffer.Rope, changes *edit.ChangeSet)

	// edits counts text changes, used to detect stale async results.
	edits int

	// lastEdit is where the latest change ended, for goto_last_modification.
	lastEdit    int
	lastEditSet bool

	// Language server state.
	uri         string
	version     int
	clients     []docClient
	lspAttached bool
	diagnostics []Diagnostic
	snippet     *activeSnippet

	// readOnly refuses text changes, onReadOnly tells the user.
	readOnly   bool
	onReadOnly func()

	// tail follows a growing file, nil for other documents.
	tail *tailState

	// secret marks a file encrypted with sops or ansible-vault.
	secret *secretState

	// kube is set for a buffer holding a Kubernetes object.
	kube *kubeState

	// label names a buffer without a file, such as a diff.
	label string

	// noCheck skips the check of the next save, checkGen tells the
	// result of the last save from those of earlier ones.
	noCheck  bool
	checkGen int

	// historyHold delays history commits while above zero, a replayed
	// macro becomes one undo step.
	historyHold int

	// textCopy is the text as a string for searches.
	textCopy textCopy

	// autoClosed holds the positions of quotes the pair insertion added
	// in the current insert session.
	autoClosed []int
	hints      inlayHintState

	// Version control state for the diff gutter.
	diff  diffState
	blame blameState

	rainbow rainbowCache

	// diskModTime is the modification time of the file when it was last
	// read or written, to notice changes made by other programs.
	diskModTime time.Time

	// diskWarned is the file time the user was last warned about, so a
	// change is reported once.
	diskWarned time.Time

	// Per document overrides from .editorconfig.
	ecTrimTrailing *bool
	ecFinalNewline *bool
	ecTextWidth    int
	autoSaveCancel context.CancelFunc

	// host holds the file, the workspace host unless set when opened.
	host host.Host

	// asRoot reads and writes the file through sudo.
	asRoot bool

	// bom is set when the file starts with a UTF-8 byte order mark, kept
	// out of the text and written back on save.
	bom bool
}

// NewDocument creates a document from text with the default indent
// style. Use OpenDocument to detect the indent of a file.
func NewDocument(path, text string) *Document {
	rope := buffer.NewRope(text)
	d := &Document{
		path:       path,
		text:       rope,
		lineEnding: buffer.DetectLineEnding(text),
		indent:     IndentStyle{Tabs: true},
		tabWidth:   defaultTabWidth,
		history:    edit.NewHistory(),
		changes:    edit.NewChangeSet(rope.Len()),
		selections: map[ViewID]edit.Selection{},
		savedText:  rope,
	}
	d.ensureView(0)

	return d
}

// ensureView creates the origin selection for a view that starts
// showing the document.
func (d *Document) ensureView(view ViewID) {
	if _, ok := d.selections[view]; !ok {
		origin := d.origin()
		d.selections[view] = edit.SingleSelection(origin.Anchor, origin.Head)
	}
}

// removeView forgets the selection of a closed view.
func (d *Document) removeView(view ViewID) {
	delete(d.selections, view)
}

// SelectionFor returns the selection of a view.
func (d *Document) SelectionFor(view ViewID) edit.Selection {
	d.ensureView(view)

	return d.selections[view]
}

// SetSelectionFor replaces the selection of a view after enforcing its
// invariants.
func (d *Document) SetSelectionFor(view ViewID, sel edit.Selection) {
	d.selections[view] = sel.EnsureInvariants(d.text)
}

// OpenDocument reads a file from h. A missing file yields an empty
// document bound to that path.
func OpenDocument(h host.Host, path string) (*Document, error) {
	text, err := readText(h, path)
	if err != nil {
		return nil, err
	}

	return newFileDocument(h, path, text), nil
}

// newFileDocument creates the document of a file on h read as text.
func newFileDocument(h host.Host, path, text string) *Document {
	text, bom := strings.CutPrefix(text, utf8BOM)
	doc := NewDocument(path, text)
	doc.host = h
	doc.bom = bom
	if indent, ok := detectIndent(text); ok {
		doc.indent = indent
		doc.indentDetected = true
	}

	doc.rememberDiskTime()

	return doc
}

// AsRoot reports whether the file is read and written through sudo.
func (d *Document) AsRoot() bool {
	return d.asRoot
}

// readText loads a file as text, a missing file is empty.
func readText(h host.Host, path string) (string, error) {
	// A pipe or a device would block the editor on the read.
	if info, err := h.Stat(path); err == nil {
		switch {
		case info.IsDir():
			return "", fmt.Errorf("%s is a directory", path)
		case !info.Mode.IsRegular():
			return "", fmt.Errorf("%s is not a regular file but a %s, it is not opened", path, fileKind(info.Mode))
		}
	}

	data, err := h.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}

	if errors.Is(err, fs.ErrPermission) {
		return "", fmt.Errorf("%s: %w, :sudo-open reads it as root", path, fs.ErrPermission)
	}

	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}

	return string(data), nil
}

// fileKind names the type of a special file.
func fileKind(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeNamedPipe != 0:
		return "named pipe"
	case mode&fs.ModeSocket != 0:
		return "socket"
	case mode&fs.ModeCharDevice != 0:
		return "character device"
	case mode&fs.ModeDevice != 0:
		return "device"
	}

	return "special file"
}

// HostName names the remote machine of the file, empty for a local one.
func (d *Document) HostName() string {
	return d.fileHost().Name()
}

// HostStatus tells whether the machine of the file can be reached.
func (d *Document) HostStatus() host.Status {
	return d.fileHost().Status()
}

// fileHost is the host holding the file, the local machine for a
// document that was never added to an editor.
func (d *Document) fileHost() host.Host {
	if d.host == nil {
		return host.Local{}
	}

	return d.host
}

// rememberDiskTime records the current modification time of the file.
func (d *Document) rememberDiskTime() {
	if info, err := d.fileHost().Stat(d.path); err == nil {
		d.diskModTime = info.ModTime
	} else {
		d.diskModTime = time.Time{}
	}
}

// ChangedOnDisk reports whether another program wrote the file since it
// was last read or saved here.
func (d *Document) ChangedOnDisk() bool {
	if d.path == "" || d.diskModTime.IsZero() {
		return false
	}

	info, err := d.fileHost().Stat(d.path)

	return err == nil && !info.ModTime.Equal(d.diskModTime)
}

// Path returns the file path, empty for a scratch buffer.
func (d *Document) Path() string {
	return d.path
}

// SetPath binds the document to a file.
func (d *Document) SetPath(path string) {
	d.path = path
}

// Text returns a snapshot of the text.
func (d *Document) Text() buffer.Rope {
	return d.text
}

// LineEnding returns the line ending used for new lines.
func (d *Document) LineEnding() buffer.LineEnding {
	return d.lineEnding
}

// TabWidth returns the width of a tab.
func (d *Document) TabWidth() int {
	return d.tabWidth
}

// Indent returns the indent unit.
func (d *Document) Indent() IndentStyle {
	return d.indent
}

// Selection returns the selection of the focused view.
func (d *Document) Selection() edit.Selection {
	return d.SelectionFor(d.focused)
}

// SetSelection replaces the selection of the focused view after
// enforcing its invariants.
func (d *Document) SetSelection(sel edit.Selection) {
	d.SetSelectionFor(d.focused, sel)
}

// origin is where a single cursor goes on the first grapheme.
func (d *Document) origin() edit.Range {
	if d.text.Len() == 0 {
		return edit.NewRange(0, 0)
	}

	return edit.NewRange(0, 1).GraphemeAligned(d.text)
}

// Apply edits the text and records the change for the next history
// commit. It returns false when the transaction does not fit the text.
func (d *Document) Apply(tx *edit.Transaction) bool {
	if d.readOnly && !tx.Changes().IsEmpty() {
		if d.onReadOnly != nil {
			d.onReadOnly()
		}

		return false
	}

	before := edit.State{Doc: d.text, Selection: d.Selection()}
	if !d.applyImpl(tx) {
		return false
	}

	if tx.Changes().IsEmpty() {
		return true
	}

	if d.changes.IsEmpty() {
		d.oldState = &before
	}

	d.changes = d.changes.Compose(*tx.Changes())
	d.lastModified = d.lastAccess

	return true
}

// applyQuiet edits the text without undo history, also in a read only
// document, for text that comes from outside such as a followed file or
// a decrypted secret. The history starts over from the new text, earlier
// revisions no longer fit it.
func (d *Document) applyQuiet(tx *edit.Transaction) bool {
	if !d.applyImpl(tx) {
		return false
	}

	d.history = edit.NewHistory()
	d.changes = edit.NewChangeSet(d.text.Len())
	d.oldState = nil

	return true
}

// ReadOnly reports whether the document refuses edits.
func (d *Document) ReadOnly() bool {
	return d.readOnly
}

// TailPath is the followed file of a tail buffer, empty for others.
func (d *Document) TailPath() string {
	if d.tail == nil {
		return ""
	}

	return d.tail.path
}

// Title names the document for the statusline and the bufferline: its
// path, tail:path for a followed file, empty for a scratch buffer.
func (d *Document) Title() string {
	if d.tail != nil && d.tail.path != "" {
		return "tail:" + d.tail.path
	}

	if d.tail != nil {
		return d.tail.title
	}

	if d.kube != nil {
		return "k8s:" + d.kube.resource
	}

	if d.label != "" {
		return d.label
	}

	return d.path
}

func (d *Document) applyImpl(tx *edit.Transaction) bool {
	changes := tx.Changes()
	oldText := d.text
	if !changes.Apply(&d.text) {
		return false
	}

	if !changes.IsEmpty() {
		d.edits++
		if list := changes.Changes(); len(list) > 0 {
			last := list[len(list)-1]
			d.lastEdit = last.From + charCount(last.Text)
			d.lastEditSet = true
		}
	}

	if d.syntax != nil && !changes.IsEmpty() {
		d.syntax.Update(oldText, d.text, changes)
	}

	if changes.IsEmpty() {
		if sel, ok := tx.Selection(); ok {
			d.SetSelection(sel)
		}

		return true
	}

	for view, sel := range d.selections {
		d.selections[view] = sel.Map(changes).EnsureInvariants(d.text)
	}

	if d.onChange != nil {
		d.onChange(d, oldText, changes)
	}

	if sel, ok := tx.Selection(); ok {
		d.SetSelection(sel)
	}

	return true
}

// AppendChangesToHistory commits the pending changes as one revision.
func (d *Document) AppendChangesToHistory() {
	if d.historyHold > 0 {
		return
	}

	d.commitHistory()
}

// commitHistory turns the pending changes into one undo step, also
// while commits are held.
func (d *Document) commitHistory() {
	if d.changes.IsEmpty() {
		return
	}

	changes := d.changes
	d.changes = edit.NewChangeSet(d.text.Len())
	tx := edit.FromChangeSet(changes).WithSelection(d.Selection())

	oldState := d.oldState
	d.oldState = nil
	if oldState == nil {
		panic("editor: no old state for history commit")
	}

	d.history.CommitRevision(tx, *oldState)
}

func (d *Document) undoRedo(undo bool) bool {
	if undo {
		d.commitHistory()
	} else if !d.changes.IsEmpty() {
		return false
	}

	var tx edit.Transaction
	var ok bool
	if undo {
		tx, ok = d.history.Undo()
	} else {
		tx, ok = d.history.Redo()
	}

	if !ok {
		return false
	}

	success := d.applyImpl(&tx)
	if success {
		d.changes = edit.NewChangeSet(d.text.Len())
	}

	return success
}

// Undo reverts the last revision.
func (d *Document) Undo() bool {
	return d.undoRedo(true)
}

// Redo reapplies the next revision.
func (d *Document) Redo() bool {
	return d.undoRedo(false)
}

func (d *Document) earlierLater(uk edit.UndoKind, earlier bool) bool {
	if earlier {
		d.AppendChangesToHistory()
	} else if !d.changes.IsEmpty() {
		return false
	}

	var txns []edit.Transaction
	if earlier {
		txns = d.history.Earlier(uk)
	} else {
		txns = d.history.Later(uk)
	}

	success := false
	for i := range txns {
		if d.applyImpl(&txns[i]) {
			success = true
		}
	}

	if success {
		d.changes = edit.NewChangeSet(d.text.Len())
	}

	return success
}

// Earlier moves back in history by steps or time.
func (d *Document) Earlier(uk edit.UndoKind) bool {
	return d.earlierLater(uk, true)
}

// Later moves forward in history by steps or time.
func (d *Document) Later(uk edit.UndoKind) bool {
	return d.earlierLater(uk, false)
}

// IsModified reports unsaved changes. The text decides rather than the
// history, so edits that cancel out, such as toggling a comment twice,
// leave the document unmodified.
func (d *Document) IsModified() bool {
	if d.history.CurrentRevision() == d.lastSavedRevision && d.changes.IsEmpty() {
		return false
	}

	if !d.modified.valid || d.modified.edits != d.edits {
		d.modified = modifiedCache{valid: true, edits: d.edits, value: !d.text.Equal(d.savedText)}
	}

	return d.modified.value
}

// modifiedCache remembers whether the text differed from the saved one
// at an edits count.
type modifiedCache struct {
	valid bool
	edits int
	value bool
}

// markSaved records the current text as the one on disk.
func (d *Document) markSaved() {
	d.lastSavedRevision = d.history.CurrentRevision()
	d.savedText = d.text
	d.modified = modifiedCache{}
}

// markSavedAs records text as what the file holds, after a save was
// taken back, so the buffer counts as modified against it.
func (d *Document) markSavedAs(text buffer.Rope) {
	d.lastSavedRevision = -1
	d.savedText = text
	d.modified = modifiedCache{}
}

// Save writes the text to the document path.
func (d *Document) Save() error {
	return d.SaveWith(true)
}

// SaveWith writes the text, atomically through a temp file when asked.
func (d *Document) SaveWith(atomic bool) error {
	if d.path == "" {
		return errors.New("no file name")
	}

	d.AppendChangesToHistory()
	if err := d.fileHost().WriteFile(d.path, d.fileBytes(), atomic); err != nil {
		return err
	}

	d.markSaved()
	d.rememberDiskTime()

	return nil
}

// lastEditPos returns where the latest change ended.
func (d *Document) lastEditPos() (int, bool) {
	return d.lastEdit, d.lastEditSet
}

// LineCount implements ui.Lines.
func (d *Document) LineCount() int {
	return d.text.LenLines()
}

// Line implements ui.Lines and returns line i without its ending.
func (d *Document) Line(i int) string {
	if i < 0 || i >= d.text.LenLines() {
		return ""
	}

	return d.text.LineWithoutEnding(i)
}

// LineStart implements ui.Lines and returns the char index of line i.
func (d *Document) LineStart(i int) int {
	return d.text.LineToChar(i)
}

// HasLineEnding reports whether line i ends with a line break.
func (d *Document) HasLineEnding(i int) bool {
	return buffer.LineEndingOf(d.text.Line(i)) != ""
}

// detectIndent guesses the indent style from the leading whitespace of
// the first lines of text. It reports false when nothing is indented.
func detectIndent(text string) (IndentStyle, bool) {
	tabs := 0
	spaceCounts := map[int]int{}
	prevSpaces := 0
	lines := 0
	for line := range strings.SplitSeq(text, "\n") {
		if lines++; lines > 1000 {
			break
		}

		if line == "" {
			continue
		}

		if line[0] == '\t' {
			tabs++

			continue
		}

		spaces := 0
		for spaces < len(line) && line[spaces] == ' ' {
			spaces++
		}

		if spaces == len(line) {
			continue
		}

		if diff := spaces - prevSpaces; diff > 1 && diff <= 8 {
			spaceCounts[diff]++
		} else if spaces > 1 && spaces <= 8 && prevSpaces == 0 {
			spaceCounts[spaces]++
		}

		prevSpaces = spaces
	}

	bestWidth, bestCount := 0, 0
	for width, count := range spaceCounts {
		if count > bestCount || (count == bestCount && width < bestWidth) {
			bestWidth, bestCount = width, count
		}
	}

	switch {
	case tabs == 0 && bestCount == 0:
		return IndentStyle{}, false
	case tabs >= bestCount:
		return IndentStyle{Tabs: true}, true
	default:
		return IndentStyle{Width: bestWidth}, true
	}
}

// indentLevelForLine returns how many indent units start the line.
func indentLevelForLine(line string, tabWidth, indentWidth int) int {
	width := 0
	for _, ch := range line {
		switch ch {
		case ' ':
			width++
		case '\t':
			width += tabWidth - width%tabWidth
		default:
			return width / max(indentWidth, 1)
		}
	}

	return width / max(indentWidth, 1)
}

// indentForLine returns the indentation to use for a new line based on
// the line at index.
func (d *Document) indentForLine(line int) string {
	level := indentLevelForLine(d.text.Line(line), d.tabWidth, d.indent.IndentWidth(d.tabWidth))

	return strings.Repeat(d.indent.String(), level)
}

func charCount(s string) int {
	return utf8.RuneCountInString(s)
}

// displayName renders a short path for messages.
func (d *Document) displayName() string {
	if title := d.Title(); title != "" {
		return title
	}

	return "[scratch]"
}

// mapAutoClosed moves the auto closed quote positions through a change.
func (d *Document) mapAutoClosed(changes *edit.ChangeSet) {
	for i, pos := range d.autoClosed {
		d.autoClosed[i] = changes.MapPos(pos, edit.After)
	}
}

// docName is the path of a document as people read it: relative to the
// working directory inside it, with ~ for the home directory, else
// absolute.
func (e *Editor) docName(doc *Document) string {
	if doc.Path() == "" || doc.label != "" {
		return doc.displayName()
	}

	return e.displayDir(doc.Path())
}

// utf8BOM is the byte order mark some editors put before UTF-8 text.
const utf8BOM = "\uFEFF"

// fileBytes is the text as the file stores it, with the byte order mark
// it was read with.
func (d *Document) fileBytes() []byte {
	if d.bom {
		return []byte(utf8BOM + d.text.String())
	}

	return []byte(d.text.String())
}

// NewPipedDocument holds text read from a pipe. It exists nowhere else,
// so it counts as unsaved and quitting asks before dropping it.
func NewPipedDocument(text string) *Document {
	d := NewDocument("", text)
	d.markSavedAs(buffer.NewRope(""))

	return d
}
