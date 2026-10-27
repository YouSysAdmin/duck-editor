package editor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/search"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

const (
	maxSearchFileSize = 2 << 20
	maxSearchResults  = 20000
	itemBatchSize     = 500
)

func init() {
	register("file_picker", "Open file picker", func(cx *Context) {
		filePicker(cx, cx.Editor.workspaceRoot())
	})
	register("file_picker_in_current_directory", "Open file picker at current working directory", func(cx *Context) {
		filePicker(cx, cx.Editor.WorkingDir())
	})
	register("file_picker_in_current_buffer_directory", "Open file picker at current buffer's directory", func(cx *Context) {
		// A scratch buffer has no directory, the working directory stands in.
		dir := cx.Editor.WorkingDir()
		if path := cx.Editor.Doc().Path(); path != "" {
			dir = filepath.Dir(path)
		}

		filePicker(cx, dir)
	})
	register("buffer_picker", "Open buffer picker", bufferPicker)
	register("jumplist_picker", "Open jumplist picker", jumplistPicker)
	register("global_search", "Global search in workspace folder", globalSearch)
	register("last_picker", "Open last picker", func(cx *Context) {
		if cx.Editor.LastPicker == nil {
			cx.Editor.SetError("no last picker")

			return
		}

		if p := cx.Editor.LastPicker; p.rerun != nil {
			p.rerun(cx)

			return
		}

		cx.Editor.openPicker(cx.Editor.LastPicker)
	})
}

// projectMarkers mark a project root for files of any language, next to
// the root files of the language. REVISION is what Capistrano leaves in
// a deployed release.
var projectMarkers = []string{"REVISION", "Gemfile", "go.mod", "package.json", "composer.json", "Cargo.toml", "pyproject.toml"}

// workspaceRoot is where pickers, global search and the explorer start:
// the project of the working directory, or the project of the current
// file when the session works in the home directory or / or the file
// lies outside that project, as after dk host with no path. After :cd
// the working directory always wins.
func (e *Editor) workspaceRoot() string {
	root := e.cwdRoot()
	if e.cwdChosen {
		return root
	}

	doc := e.Doc()
	path := doc.Path()
	if path == "" || e.hostOf(doc).Name() != e.Host.Name() {
		return root
	}

	home, _ := e.Host.Home()
	if _, inside := RelativeTo(root, path); inside && root != home && root != "/" {
		return root
	}

	return e.fileProjectRoot(doc, home)
}

// fileProjectRoot finds the project of a file: the nearest directory
// with .git or .duck, else the highest one with a root file of its
// language or a common project file, never home or / themselves, else
// the directory of the file.
func (e *Editor) fileProjectRoot(doc *Document, home string) string {
	markers := slices.Clone(projectMarkers)
	if doc.language != nil {
		markers = append(markers, doc.language.Roots...)
	}

	stops := []string{".git", ".duck"}
	per := len(stops) + len(markers)
	var dirs, candidates []string
	for dir := filepath.Dir(doc.Path()); dir != home && filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		for _, m := range append(slices.Clone(stops), markers...) {
			candidates = append(candidates, filepath.Join(dir, m))
		}
	}

	found, _ := e.Host.Exists(candidates)
	best := filepath.Dir(doc.Path())
	for i, dir := range dirs {
		row := found[min(i*per, len(found)):min((i+1)*per, len(found))]
		if slices.Contains(row[:min(len(stops), len(row))], true) {
			return dir
		}

		if slices.Contains(row, true) {
			best = dir
		}
	}

	return best
}

// cwdRoot is the git root holding the working directory, or the working
// directory itself.
func (e *Editor) cwdRoot() string {
	cwd := e.WorkingDir()
	var dirs, candidates []string
	for dir := cwd; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		candidates = append(candidates, filepath.Join(dir, ".git"))
		if filepath.Dir(dir) == dir {
			break
		}
	}

	found, _ := e.Host.Exists(candidates)
	for i, ok := range found {
		if ok {
			return dirs[i]
		}
	}

	return cwd
}

func actionFor(action PickerAction) Action {
	switch action {
	case PickerHSplit:
		return ActionHorizontalSplit
	case PickerVSplit:
		return ActionVerticalSplit
	default:
		return ActionReplace
	}
}

func openItem(cx *Context, item PickerItem, action PickerAction) {
	e := cx.Editor
	if item.DocID != 0 {
		e.Switch(item.DocID, actionFor(action))
	} else if item.Path != "" {
		if _, err := e.Open(item.Path, actionFor(action)); err != nil {
			path := e.absPath(item.Path)
			if !e.offerSudo(cx, err, filepath.Base(path), func(e *Editor) error { return e.sudoOpenPath(e.Host, path) }) {
				e.SetError("%v", err)
			}

			return
		}
	}

	if item.Range != nil {
		e.Doc().SetSelection(edit.SingleSelection(item.Range.Anchor, item.Range.Head))
	} else if item.Line >= 0 {
		text := e.Doc().Text()
		line := min(item.Line, text.LenLines()-1)
		pos := text.LineToChar(line)
		e.Doc().SetSelection(edit.PointSelection(pos))
	}

	e.ensureCursorInViewCenter()
}

func filePicker(cx *Context, root string) {
	e := cx.Editor
	h := e.Host
	if _, err := h.Stat(root); err != nil {
		e.SetError("workspace directory does not exist")

		return
	}

	// The title names the directory when it is not the workspace.
	title := "Files"
	if root != e.workspaceRoot() {
		title = "Files in " + e.displayDir(root)
	}

	p := NewPicker(title, nil, func(cx *Context, item PickerItem, action PickerAction) {
		openItem(cx, PickerItem{Path: filepath.Join(root, item.Label), Line: -1}, action)
	})
	p.previewBase = root
	p.Paths = true
	list := func() {
		opts := walkOptions(e)
		p.Title = title
		if p.showAll {
			opts = unfiltered(opts)
			p.Title += " (all)"
		}

		p.load = func(ctx context.Context, gen int, emit func(JobResult)) {
			batch := make([]PickerItem, 0, itemBatchSize)
			flush := func() {
				if len(batch) == 0 {
					return
				}

				emit(p.emitItems(gen, batch))
				batch = make([]PickerItem, 0, itemBatchSize)
			}
			err := h.Walk(ctx, root, opts, func(rel string) bool {
				batch = append(batch, PickerItem{Label: rel, Line: -1})
				if len(batch) >= itemBatchSize {
					flush()
				}

				return true
			})
			flush()
			reportJobError(emit, "listing files", err)
		}
	}
	list()
	p.relist = func(cx *Context) {
		list()
		cx.Editor.openPicker(p)
	}
	e.openPicker(p)
}

func walkOptions(e *Editor) walk.Options {
	opts := walk.DefaultOptions()
	fp := e.Config.Editor.FilePicker
	opts.Hidden = fp.Hidden
	opts.GitIgnore = fp.GitIgnore
	opts.Ignore = fp.Ignore
	opts.GitExclude = fp.GitExclude
	opts.FollowSymlinks = fp.FollowSymlinks
	opts.MaxDepth = fp.MaxDepth

	return opts
}

// unfiltered keeps the symlink and depth settings of opts and drops the
// rules that hide files.
func unfiltered(opts walk.Options) walk.Options {
	return walk.Options{FollowSymlinks: opts.FollowSymlinks, MaxDepth: opts.MaxDepth}
}

func bufferPicker(cx *Context) {
	e := cx.Editor
	items := []PickerItem{}
	for _, doc := range e.Documents() {
		flags := ""
		if doc.IsModified() {
			flags = " [+]"
		}

		line := doc.Selection().Primary().CursorLine(doc.Text())
		items = append(items, PickerItem{Label: e.docName(doc) + flags, DocID: doc.ID, Path: doc.Path(), Line: line})
	}

	p := NewPicker("Buffers", items, func(cx *Context, item PickerItem, action PickerAction) {
		cx.Editor.Switch(item.DocID, actionFor(action))
	})
	p.remove = func(cx *Context, item PickerItem) bool {
		if err := cx.Editor.CloseDocument(item.DocID, false); err != nil {
			cx.Editor.SetError("%v", err)

			return false
		}

		return true
	}
	p.rerun = bufferPicker
	p.Paths = true
	e.openPicker(p)
}

func jumplistPicker(cx *Context) {
	e := cx.Editor
	items := []PickerItem{}
	for _, j := range e.View().Jumps.Jumps() {
		doc, ok := e.Docs[j.Doc]
		if !ok {
			continue
		}

		text := doc.Text()
		if j.Selection.Primary().To() > text.Len() {
			continue
		}

		line := j.Selection.Primary().CursorLine(text)
		snippet := strings.TrimSpace(doc.Line(line))
		items = append(items, PickerItem{
			Label: fmt.Sprintf("%s:%d %s", e.docName(doc), line+1, snippet),
			DocID: doc.ID,
			Path:  doc.Path(),
			Line:  line,
			Range: &edit.Range{Anchor: j.Selection.Primary().Anchor, Head: j.Selection.Primary().Head},
		})
	}

	p := NewPicker("Jumplist", items, openItem)
	p.rerun = jumplistPicker
	e.openPicker(p)
}

func globalSearch(cx *Context) {
	e := cx.Editor
	root := e.workspaceRoot()
	e.openPrompt("global-search:", func(cx *Context, input string, ev PromptEvent) {
		if ev != PromptValidate || input == "" {
			return
		}

		re, err := searchRegexWith(input, cx.Editor.Config.Editor.Search.SmartCase)
		if err != nil {
			cx.Editor.SetError("invalid regex: %v", err)

			return
		}

		cx.Editor.pushRegisterHistory('/', input)
		startGlobalSearch(cx, root, re)
	}).withHistory('/')
}

func startGlobalSearch(cx *Context, root string, re *regexp.Regexp) {
	e := cx.Editor
	p := NewPicker("Global search", nil, openItem)
	p.previewBase = root
	opts := walkOptions(e)
	h := e.Host
	q := search.Query{Pattern: re.String(), MaxFileSize: maxSearchFileSize}
	p.load = func(ctx context.Context, gen int, emit func(JobResult)) {
		batch := make([]PickerItem, 0, itemBatchSize)
		total := 0
		flush := func() {
			if len(batch) == 0 {
				return
			}

			emit(p.emitItems(gen, batch))
			batch = make([]PickerItem, 0, itemBatchSize)
		}
		err := h.Search(ctx, root, opts, q, func(m search.Match) bool {
			r := edit.NewRange(m.Start, m.End)
			batch = append(batch, PickerItem{
				Label: fmt.Sprintf("%s:%d: %s", m.Rel, m.Line+1, m.Text),
				Path:  filepath.Join(root, m.Rel),
				Line:  m.Line,
				Range: &r,
			})
			total++
			if len(batch) >= itemBatchSize {
				flush()
			}

			return total < maxSearchResults && ctx.Err() == nil
		})
		flush()
		if total >= maxSearchResults {
			emit(func(*Editor) {
				p.Title = fmt.Sprintf("Global search, first %d matches", maxSearchResults)
			})
		}

		reportJobError(emit, "searching", err)
	}
	e.openPicker(p)
}

// displayDir shortens a directory for titles: relative to the working
// directory when inside it, with ~ for the home directory otherwise.
func (e *Editor) displayDir(dir string) string {
	if rel, ok := RelativeTo(e.WorkingDir(), dir); ok {
		return rel
	}

	if home, err := e.Host.Home(); err == nil {
		if rel, ok := RelativeTo(home, dir); ok {
			return filepath.Join("~", rel)
		}
	}

	return dir
}

// reportJobError shows why a background listing failed, except when it
// was cancelled or the connection went, which is reported on its own.
func reportJobError(emit func(JobResult), what string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}

	emit(func(e *Editor) {
		if !e.hostGone(err) {
			e.SetError("%s: %v", what, err)
		}
	})
}
