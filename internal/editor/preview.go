package editor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"

	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/vcs"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// maxPreviewSize is the largest text file a picker preview reads.
const maxPreviewSize = 1 << 20

// Preview is what the picker shows for an item: a document, the entries
// of a directory, or a note such as the kind of a binary file.
type Preview struct {
	Doc     *Document
	Entries []PreviewEntry
	Note    []string

	// plain marks a document read for a label only, highlighted once a
	// preview shows it.
	plain bool
}

// PreviewEntry is one entry of a directory preview.
type PreviewEntry struct {
	Name string
	Dir  bool

	// Link is where a symbolic link points.
	Link string
}

func previewKey(path string, diff bool) string {
	if diff {
		return "\x00diff " + path
	}

	return path
}

// Preview returns the cached preview of a file, ok is false while it is
// not prepared yet.
func (e *Editor) Preview(path string, diff bool) (*Preview, bool) {
	p, ok := e.previews[previewKey(path, diff)]
	if !ok || p == nil {
		return nil, false
	}

	return p, true
}

// PreparePreview loads the preview of a file in the background: its
// text, or with diff set its changes against the last commit, an open
// buffer counting with its unsaved edits. Reading and git run off the
// editor thread, a slow host must not hold up drawing.
func (e *Editor) PreparePreview(path string, diff bool) {
	key := previewKey(path, diff)
	if p, ok := e.previews[key]; ok {
		if p != nil && p.plain {
			p.plain = false
			if p.Doc.Text().Len() < asyncParseThreshold {
				e.attachSyntax(p.Doc)
			}
		}

		return
	}

	if e.previews == nil {
		e.previews = map[string]*Preview{}
	}

	open, isOpen := e.DocumentByPath(path)
	if isOpen && !diff {
		e.previews[key] = &Preview{Doc: open}

		return
	}

	e.previews[key] = nil
	gen := e.previewGen
	h, abs := e.Host, e.absPath(path)
	opts := walkOptions(e)
	current := ""
	if isOpen {
		current = open.Text().String()
	}

	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		var text string
		p := &Preview{}
		if diff {
			text = unifiedPreview(h, abs, current, isOpen)
		} else {
			text, p.Entries, p.Note = readPreview(h, abs, opts)
		}

		emit(func(e *Editor) {
			if e.previewGen != gen {
				return
			}

			if p.Note == nil && p.Entries == nil {
				p.Doc = e.previewDocument(h, abs, text, diff)
			}

			e.previews[key] = p
		})
	})
}

// readPreview reads a file for its preview, lists a directory the way
// the explorer does, or describes why a file is not shown as text.
func readPreview(h host.Host, path string, opts walk.Options) (string, []PreviewEntry, []string) {
	info, err := h.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil, []string{" the file does not exist, a link may point nowhere"}
	case err != nil:
		return "", nil, []string{" cannot read: " + err.Error()}
	}

	if info.IsDir() {
		entries, err := listPreview(h, path, opts)
		if err != nil {
			return "", nil, []string{" cannot list the directory: " + err.Error()}
		}

		return "", entries, nil
	}

	if !info.Mode.IsRegular() {
		return "", nil, []string{" " + fileKind(info.Mode) + ", not read"}
	}

	if desc, err := h.Describe(path); err == nil && desc.Binary {
		note := []string{" binary file, " + humanSize(info.Size)}
		if desc.Text != "" {
			note = append(note, " "+desc.Text)
		}

		return "", nil, note
	}

	if info.Size > maxPreviewSize {
		return "", nil, []string{" file too large to preview"}
	}

	data, err := h.ReadFile(path)
	if err != nil {
		return "", nil, []string{" cannot preview file"}
	}

	return string(data), nil, nil
}

// listPreview lists a directory, directories first, leaving out what the
// file-picker options hide. An unreadable directory lists nothing.
func listPreview(h host.Host, dir string, opts walk.Options) ([]PreviewEntry, error) {
	entries, err := h.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	exists := func(paths []string) []bool {
		found, _ := h.Exists(paths)
		if len(found) != len(paths) {
			return make([]bool, len(paths))
		}

		return found
	}
	filter := walk.NewDirFilter(dir, opts, exists, h.ReadFile)
	out := make([]PreviewEntry, 0, len(entries))
	for _, entry := range entries {
		if !filter.Skip(entry.Name, entry.IsDir()) {
			out = append(out, PreviewEntry{Name: entry.Name, Dir: entry.IsDir(), Link: entry.Link})
		}
	}

	slices.SortStableFunc(out, func(a, b PreviewEntry) int {
		switch {
		case a.Dir == b.Dir:
			return 0
		case a.Dir:
			return -1
		}

		return 1
	})

	return out, nil
}

// unifiedPreview is the diff of a file against the last commit, the
// current text given when the file is open.
func unifiedPreview(h host.Host, path, current string, isOpen bool) string {
	if !isOpen {
		data, err := h.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// A deleted file diffs against nothing.
		case err != nil:
			return "cannot read the file: " + err.Error() + "\n"
		default:
			current = string(data)
		}
	}

	var base string
	if data, err := vcs.Base(h, path); err == nil {
		base = string(data)
	}

	text := vcs.Unified(linesWithoutEndings(base), linesWithoutEndings(current), 3)
	if text == "" {
		text = "no changes\n"
	}

	return text
}

// linesWithoutEndings splits text into lines for a diff, a final line
// break is not a line of its own.
func linesWithoutEndings(s string) []string {
	lines := splitLines(s)
	if n := len(lines); n > 0 && lines[n-1] == "" {
		return lines[:n-1]
	}

	return lines
}

// previewDocument makes the document of a preview. Previews are not
// registered documents, so a background parse could never deliver, only
// small texts are highlighted and those on the spot.
func (e *Editor) previewDocument(h host.Host, path, text string, diff bool) *Document {
	var doc *Document
	if diff {
		doc = NewDocument("", text)
		if e.Syntax != nil && e.Syntax.Languages() != nil && doc.Text().Len() < asyncParseThreshold {
			if lang, ok := e.Syntax.Languages().Get("diff"); ok {
				e.attachLanguage(doc, lang)
			}
		}

		return doc
	}

	doc = newFileDocument(h, path, text)
	if doc.Text().Len() < asyncParseThreshold {
		e.attachSyntax(doc)
	}

	return doc
}

// humanSize formats a byte count with a binary unit.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// previewLineCount is how many lines the preview of a picker item has,
// one while it is not loaded.
func (e *Editor) previewLineCount(p *Picker, item PickerItem) int {
	if doc, ok := e.Docs[item.DocID]; ok && item.DocID != 0 {
		return doc.LineCount()
	}

	if item.Text != nil {
		return len(item.Text)
	}

	if pv, ok := e.Preview(p.PreviewPath(item), item.Diff); ok && pv.Doc != nil {
		return pv.Doc.LineCount()
	}

	return 1
}
