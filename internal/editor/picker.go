package editor

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/fuzzy"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// PickerItem is one selectable entry.
type PickerItem struct {
	// Label is matched and displayed.
	Label string

	// Path and Line locate the preview, Line is zero based or -1.
	Path string
	Line int

	// Range selects text after opening, used by global search.
	Range *edit.Range

	// DocID refers to an open buffer when set.
	DocID DocID

	// Index points into the data of the code that built the picker.
	Index int

	// Diff previews the changes of the file against the last commit
	// instead of the file.
	Diff bool

	// Dir marks a directory entry of the explorer.
	Dir bool

	// Match is what the filter matches when set, a start of Label such
	// as a name before its description.
	Match string

	// Text is shown in the preview pane instead of a file, such as the
	// description of a command.
	Text []string

	// Note follows Label in a dim style and is not matched, such as the
	// target of a symbolic link.
	Note string
}

// PickerAction says how a picked item opens.
type PickerAction int

// Picker actions.
const (
	PickerOpen PickerAction = iota
	PickerHSplit
	PickerVSplit
)

// PickerMatch is a filtered item with its score and match positions.
type PickerMatch struct {
	Index     int
	Score     int
	Positions []int
}

// Picker is the fuzzy list overlay used for files, buffers and search
// results. Items may arrive from a background job while it is open.
type Picker struct {
	Title       string
	ShowPreview bool

	// Paths marks a list of file paths, cut at the start when too long so
	// the file names stay.
	Paths bool

	// Loading is true while a job is still delivering items.
	Loading bool

	items    []PickerItem
	matches  []PickerMatch
	query    []rune
	cursor   int
	selected int

	// offset is the first visible match row, maintained by the renderer
	// through EnsureVisible.
	offset int

	// previewBase resolves relative item paths for previews.
	previewBase string

	// previewScroll moves the preview of the item at previewScrollItem
	// by rows, another item starts unscrolled.
	previewScroll     int
	previewScrollItem int

	onSelect func(cx *Context, item PickerItem, action PickerAction)
	load     func(ctx context.Context, gen int, emit func(JobResult))
	cancel   context.CancelFunc

	// generation guards against results from a previous run.
	generation int

	// parent goes one directory up in pickers that browse directories.
	parent func(cx *Context)

	// remove takes the selected item away, C-x in the buffer picker,
	// and reports whether it went.
	remove func(cx *Context, item PickerItem) bool

	// rerun builds the picker again from the current state for
	// last_picker, for lists such as the open buffers that go stale.
	rerun func(cx *Context)
}

// NewPicker creates a picker over static items.
func NewPicker(title string, items []PickerItem, onSelect func(cx *Context, item PickerItem, action PickerAction)) *Picker {
	p := &Picker{Title: title, ShowPreview: true, onSelect: onSelect}
	p.SetItems(items)

	return p
}

// selectPath selects the item with path, if it is listed.
func (p *Picker) selectPath(path string) {
	if path == "" {
		return
	}

	for i, m := range p.matches {
		if p.items[m.Index].Path == path {
			p.selected = i

			return
		}
	}
}

// Previews reports whether the picker shows a preview pane: it is on and
// its items name files or buffers. A list of commands has nothing to
// preview.
func (p *Picker) Previews() bool {
	if !p.ShowPreview {
		return false
	}

	if p.previewBase != "" {
		return true
	}

	return slices.ContainsFunc(p.items, func(it PickerItem) bool { return it.Path != "" || it.DocID != 0 || it.Text != nil })
}

// Query returns the typed filter.
func (p *Picker) Query() string {
	return string(p.query)
}

// Cursor returns the prompt cursor in chars.
func (p *Picker) Cursor() int {
	return p.cursor
}

// Items returns all items.
func (p *Picker) Items() []PickerItem {
	return p.items
}

// Matches returns the filtered items in rank order.
func (p *Picker) Matches() []PickerMatch {
	return p.matches
}

// Selected returns the highlighted item.
func (p *Picker) Selected() (PickerItem, bool) {
	if p.selected < 0 || p.selected >= len(p.matches) {
		return PickerItem{}, false
	}

	idx := p.matches[p.selected].Index
	if idx < 0 || idx >= len(p.items) {
		return PickerItem{}, false
	}

	return p.items[idx], true
}

// PreviewScroll returns how many rows the preview of the selected item
// is scrolled from its default position.
func (p *Picker) PreviewScroll() int {
	if p.selected < 0 || p.selected >= len(p.matches) || p.matches[p.selected].Index != p.previewScrollItem {
		return 0
	}

	return p.previewScroll
}

// scrollPreview moves the preview of the selected item by rows, within
// lo and hi.
func (p *Picker) scrollPreview(rows, lo, hi int) {
	if p.selected < 0 || p.selected >= len(p.matches) {
		return
	}

	item := p.matches[p.selected].Index
	if item != p.previewScrollItem {
		p.previewScrollItem, p.previewScroll = item, 0
	}

	p.previewScroll = max(min(p.previewScroll+rows, hi), min(lo, hi))
}

// SelectedIndex returns the highlighted row in the match list.
func (p *Picker) SelectedIndex() int {
	return p.selected
}

// Offset returns the first visible row.
func (p *Picker) Offset() int {
	return p.offset
}

// EnsureVisible scrolls the list so the selection fits in height rows.
func (p *Picker) EnsureVisible(height int) {
	if height <= 0 {
		return
	}

	if p.selected < p.offset {
		p.offset = p.selected
	}

	if p.selected >= p.offset+height {
		p.offset = p.selected - height + 1
	}

	p.offset = max(min(p.offset, max(len(p.matches)-height, 0)), 0)
}

// SetItems replaces the items and refilters, keeping the selected item
// when it is still there.
func (p *Picker) SetItems(items []PickerItem) {
	keep, _ := p.Selected()
	p.items = items
	p.refilter(keep)
}

// AddItems appends items and refilters, keeping the selection on the
// item it was on while results stream in.
func (p *Picker) AddItems(items []PickerItem) {
	keep, _ := p.Selected()
	p.items = append(p.items, items...)
	p.refilter(keep)
}

// filter matches the items against a changed query, the best match
// comes first and is selected.
func (p *Picker) filter() {
	p.refilter(PickerItem{})
}

// refilter matches the items again and selects keep when it matches.
func (p *Picker) refilter(keep PickerItem) {
	pattern := fuzzy.Parse(string(p.query))

	// Scattered matches show only when nothing matches tightly, see
	// fuzzy.Pattern.MatchTight.
	p.matches = p.matches[:0]
	var loose []PickerMatch
	for i, item := range p.items {
		// The way up of the explorer is no match for a query.
		if p.parent != nil && item.Label == "../" && !pattern.IsEmpty() {
			continue
		}

		text := item.Label
		if item.Match != "" {
			text = item.Match
		}

		score, positions, tight, ok := pattern.MatchTight(text)
		switch {
		case !ok:
		case tight:
			p.matches = append(p.matches, PickerMatch{Index: i, Score: score, Positions: positions})
		default:
			loose = append(loose, PickerMatch{Index: i, Score: score, Positions: positions})
		}
	}

	if len(p.matches) == 0 {
		p.matches = append(p.matches, loose...)
	}

	if !pattern.IsEmpty() {
		slices.SortStableFunc(p.matches, func(a, b PickerMatch) int { return b.Score - a.Score })
	}

	p.selected = 0
	if keep.Label != "" {
		for i, m := range p.matches {
			if it := p.items[m.Index]; it.Label == keep.Label && it.Path == keep.Path && it.Line == keep.Line && it.DocID == keep.DocID && it.Index == keep.Index {
				p.selected = i

				break
			}
		}
	}
}

func (p *Picker) move(delta int) {
	if len(p.matches) == 0 {
		p.selected = 0

		return
	}

	p.selected = (p.selected + delta) % len(p.matches)
	if p.selected < 0 {
		p.selected += len(p.matches)
	}
}

func (p *Picker) handleKey(cx *Context, key keymap.Key, pageSize int) {
	e := cx.Editor
	ch, isChar := key.Char()
	plain := key.Mods&(keymap.ModCtrl|keymap.ModAlt|keymap.ModSuper) == 0

	switch {
	case key == (keymap.Key{Code: keymap.KeyEsc}) || key == keymap.Ctrl('c'):
		e.closePicker()
	case key == (keymap.Key{Code: keymap.KeyEnter}):
		p.accept(cx, PickerOpen)
	case key == keymap.Ctrl('s'):
		p.accept(cx, PickerHSplit)
	case key == keymap.Ctrl('v'):
		p.accept(cx, PickerVSplit)
	case key == (keymap.Key{Code: keymap.KeyTab, Mods: keymap.ModShift}) || key == (keymap.Key{Code: keymap.KeyUp}) || key == keymap.Ctrl('p') || key == keymap.Ctrl('k'):
		p.move(-1)
	case key == (keymap.Key{Code: keymap.KeyTab}) || key == (keymap.Key{Code: keymap.KeyDown}) || key == keymap.Ctrl('n') || key == keymap.Ctrl('j'):
		p.move(1)
	case key == (keymap.Key{Code: keymap.KeyPageUp}) || key == keymap.Ctrl('u'):
		p.selected = max(p.selected-max(pageSize, 1), 0)
	case key == (keymap.Key{Code: keymap.KeyPageDown}) || key == keymap.Ctrl('d'):
		p.selected = min(p.selected+max(pageSize, 1), max(len(p.matches)-1, 0))
	case key == keymap.Alt('d') || key == keymap.Alt('u'):
		rows := max(pageSize/2, 1)
		if key == keymap.Alt('u') {
			rows = -rows
		}

		// The preview stays between its first and last line.
		item, _ := p.Selected()
		start := max(item.Line, 0) - pageSize/2
		p.scrollPreview(rows, -start, e.previewLineCount(p, item)-1-start)
	case key == (keymap.Key{Code: keymap.KeyHome}):
		p.selected = 0
	case key == (keymap.Key{Code: keymap.KeyEnd}):
		p.selected = max(len(p.matches)-1, 0)
	case key == keymap.Ctrl('t'):
		p.ShowPreview = !p.ShowPreview
	case p.parent != nil && (key == keymap.Alt('h') || key == (keymap.Key{Code: keymap.KeyBackspace}) && len(p.query) == 0):
		p.parent(cx)
	case p.parent != nil && key == keymap.Alt('l'):
		p.accept(cx, PickerOpen)
	case p.remove != nil && key == keymap.Ctrl('x'):
		if item, ok := p.Selected(); ok && p.remove(cx, item) {
			row := p.selected
			p.items = slices.DeleteFunc(p.items, func(it PickerItem) bool { return it.DocID == item.DocID && it.Path == item.Path })
			p.filter()
			p.selected = max(min(row, len(p.matches)-1), 0)
		}
	case key == (keymap.Key{Code: keymap.KeyBackspace}) || key == keymap.Ctrl('h'):
		if p.cursor > 0 {
			p.query = slices.Delete(p.query, p.cursor-1, p.cursor)
			p.cursor--
			p.filter()
		}
	case key == (keymap.Key{Code: keymap.KeyDelete}):
		if p.cursor < len(p.query) {
			p.query = slices.Delete(p.query, p.cursor, p.cursor+1)
			p.filter()
		}
	case key == (keymap.Key{Code: keymap.KeyLeft}) || key == keymap.Ctrl('b'):
		p.cursor = max(p.cursor-1, 0)
	case key == (keymap.Key{Code: keymap.KeyRight}) || key == keymap.Ctrl('f'):
		p.cursor = min(p.cursor+1, len(p.query))
	case key == keymap.Ctrl('a'):
		p.cursor = 0
	case key == keymap.Ctrl('e'):
		p.cursor = len(p.query)
	case key == keymap.Ctrl('w') || key == keymap.Alt(keymap.KeyBackspace):
		start := p.cursor
		for start > 0 && unicode.IsSpace(p.query[start-1]) {
			start--
		}

		for start > 0 && !unicode.IsSpace(p.query[start-1]) {
			start--
		}

		p.query = slices.Delete(p.query, start, p.cursor)
		p.cursor = start
		p.filter()
	case isChar && plain:
		p.query = slices.Insert(p.query, p.cursor, ch)
		p.cursor++
		p.filter()
	}
}

// insert puts text into the query at the cursor and filters again.
func (p *Picker) insert(text string) {
	if text == "" {
		return
	}

	// A query is one line, pasted tabs and line breaks become spaces.
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, text))
	p.query = slices.Insert(p.query, p.cursor, runes...)
	p.cursor += len(runes)
	p.filter()
}

func (p *Picker) accept(cx *Context, action PickerAction) {
	item, ok := p.Selected()
	e := cx.Editor
	e.closePicker()
	if !ok || p.onSelect == nil {
		return
	}

	p.onSelect(cx, item, action)
}

// openPicker shows a picker and starts its loader, if any.
func (e *Editor) openPicker(p *Picker) {
	if e.Picker != nil {
		e.closePicker()
	}

	e.Picker = p
	e.LastPicker = p
	if p.load != nil {
		p.generation++
		gen := p.generation
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		p.Loading = true
		load := p.load
		// A rerun through last_picker starts from an empty list.
		p.items, p.matches = nil, nil
		p.filter()
		e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
			load(ctx, gen, emit)
			emit(func(e *Editor) {
				if p.generation == gen {
					p.Loading = false
				}
			})
		})
	}
}

// closePicker hides the picker and stops its loader.
func (e *Editor) closePicker() {
	if e.Picker == nil {
		return
	}

	if e.Picker.cancel != nil {
		e.Picker.cancel()
		e.Picker.cancel = nil
	}

	e.Picker.Loading = false
	e.Picker = nil
	e.dropPreviews()
}

// emitItems returns a JobResult adding items to the picker when it is
// still the active generation.
func (p *Picker) emitItems(gen int, items []PickerItem) JobResult {
	return func(e *Editor) {
		if p.generation != gen {
			return
		}

		p.AddItems(items)
	}
}

// PreviewPath returns the file to preview for an item.
func (p *Picker) PreviewPath(item PickerItem) string {
	if item.Path == "" {
		if item.DocID == 0 && p.previewBase != "" {
			return filepath.Join(p.previewBase, item.Label)
		}

		return ""
	}

	if !filepath.IsAbs(item.Path) && p.previewBase != "" {
		return filepath.Join(p.previewBase, item.Path)
	}

	return item.Path
}
