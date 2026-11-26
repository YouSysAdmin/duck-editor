package editor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/vcs"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// explorer lists one directory in a picker and moves between
// directories without closing it. Listings load in the background.
type explorer struct {
	picker *Picker
	host   host.Host

	// dir is the directory listed or being listed, listed the one the
	// items come from, empty before the first listing arrives.
	dir    string
	listed string

	// asRoot lists through sudo and opens and changes entries as root.
	asRoot bool

	// selectNext is the path to select once the listing arrives, else
	// the entry selected when the explorer was there last.
	selectNext string
}

// fileExplorer lists a directory, directories first. Picking a directory
// lists it, picking a file opens the file. A directory only root may read
// is listed through sudo after asking.
func fileExplorer(cx *Context, dir string) {
	openExplorer(cx.Editor, dir, "", false, false)
}

// openExplorer opens the explorer on dir with from selected. all lists
// the entries the file-picker options hide.
func openExplorer(e *Editor, dir, from string, asRoot, all bool) *explorer {
	x := &explorer{host: e.Host, asRoot: asRoot}
	p := NewPicker("", nil, func(cx *Context, item PickerItem, action PickerAction) { x.pick(cx, item, action) })
	p.explorer = x
	p.Paths = true
	p.multi = true
	p.showAll = all
	p.onClose = x.remember
	p.recursive = x.recursive
	p.relist = func(cx *Context) {
		item, _ := p.Selected()
		x.selectNext = item.Path
		x.reload(cx.Editor)
	}
	x.picker = p
	x.goTo(e, dir, from)

	return x
}

// spotKey names a directory of a host among the remembered selections.
func spotKey(h host.Host, dir string) string {
	return h.Name() + "\n" + dir
}

// remember keeps the entry selected in the listed directory without a
// filter, the explorer selects it when it comes back there.
func (x *explorer) remember(e *Editor) {
	item, ok := x.picker.Selected()
	if x.listed == "" || !ok || item.Label == "../" || len(x.picker.query) > 0 {
		return
	}

	if e.explorerSpots == nil {
		e.explorerSpots = map[string]string{}
	}

	e.explorerSpots[spotKey(x.host, x.listed)] = item.Path
}

// goTo lists dir with an empty filter and selects from.
func (x *explorer) goTo(e *Editor, dir, from string) {
	p := x.picker
	p.query, p.cursor = nil, 0
	x.dir = filepath.Clean(dir)
	x.selectNext = from
	x.reload(e)
}

// reload lists the directory again, the filter stays.
func (x *explorer) reload(e *Editor) {
	p := x.picker
	p.Title = x.title(e)
	p.load = x.loader(e, x.dir)
	e.openPicker(p)
}

func (x *explorer) title(e *Editor) string {
	rel := x.dir
	if r, ok := RelativeTo(e.WorkingDir(), x.dir); ok {
		rel = r
	}

	// The working directory itself is named, a dot says nothing.
	if rel == "." {
		rel = filepath.Base(x.dir)
	}

	title := "Explorer " + rel
	if x.asRoot {
		title += " as root"
	}

	if x.picker.showAll {
		title += " (all)"
	}

	return title
}

// loader lists dir in the background, then marks the entries git sees
// changed.
func (x *explorer) loader(e *Editor, dir string) func(ctx context.Context, gen int, emit func(JobResult)) {
	p, h, asRoot := x.picker, x.host, x.asRoot
	opts := walkOptions(e)
	if p.showAll {
		opts = unfiltered(opts)
	}

	git := !e.vcsDisabled && !asRoot

	return func(ctx context.Context, gen int, emit func(JobResult)) {
		items, err := listDir(h, dir, opts, asRoot)
		if ctx.Err() != nil {
			return
		}

		emit(func(e *Editor) {
			if p.generation != gen {
				return
			}

			if err != nil {
				x.failed(e, dir, err)

				return
			}

			x.listed = dir
			p.SetItems(items)
			// A filter typed while loading keeps its best match.
			if len(p.query) == 0 {
				p.selectPath(cmp.Or(x.selectNext, e.explorerSpots[spotKey(h, dir)]))
			}

			x.selectNext = ""
		})

		if err != nil || !git {
			return
		}

		kinds, err := vcs.DirStatus(ctx, h, dir)
		if err != nil || len(kinds) == 0 {
			return
		}

		emit(func(e *Editor) {
			if p.generation != gen {
				return
			}

			for i, item := range p.items {
				if item.Label != "../" {
					p.items[i].Status = kinds[strings.TrimSuffix(item.Label, "/")]
				}
			}
		})
	}
}

// listDir lists the entries of dir as picker items, directories first,
// leaving out what opts hide.
func listDir(h host.Host, dir string, opts walk.Options, asRoot bool) ([]PickerItem, error) {
	var entries []host.DirEntry
	var err error
	if asRoot {
		entries, err = sudoList(h, dir)
	} else {
		entries, err = h.ReadDir(dir)
	}

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
	entries = slices.DeleteFunc(entries, func(entry host.DirEntry) bool { return filter.Skip(entry.Name, entry.IsDir()) })
	// Listings come sorted by name, directories go first.
	slices.SortStableFunc(entries, func(a, b host.DirEntry) int {
		switch {
		case a.IsDir() == b.IsDir():
			return 0
		case a.IsDir():
			return -1
		}

		return 1
	})

	items := make([]PickerItem, 0, len(entries)+1)
	if filepath.Dir(dir) != dir {
		items = append(items, PickerItem{Label: "../", Path: filepath.Dir(dir), Line: -1, Dir: true})
	}

	for _, entry := range entries {
		name := entry.Name
		if entry.IsDir() {
			name += "/"
		}

		item := PickerItem{Label: name, Path: filepath.Join(dir, entry.Name), Line: -1, Dir: entry.IsDir()}
		if entry.Link != "" {
			item.Note = "-> " + entry.Link
		}

		items = append(items, item)
	}

	return items, nil
}

// failed handles a directory that could not be listed: the explorer
// goes back to the directory it showed, or closes when it showed none.
// A permission error offers to list it as root.
func (x *explorer) failed(e *Editor, dir string, err error) {
	if errors.Is(err, fs.ErrPermission) && !x.asRoot {
		e.closePicker()
		all := x.picker.showAll
		retry := func(e *Editor) error {
			return e.withSudo(x.host, func(e *Editor) error {
				openExplorer(e, dir, "", true, all)

				return nil
			})
		}
		e.offerSudo(&Context{Editor: e}, err, dir, retry)

		return
	}

	if x.listed == "" {
		e.closePicker()
		e.SetError("%v", err)

		return
	}

	x.goTo(e, x.listed, "")
	e.SetError("%v", err)
}

// pick opens a picked entry, a directory is listed in place.
func (x *explorer) pick(cx *Context, item PickerItem, action PickerAction) {
	e := cx.Editor
	switch {
	case item.Label == "../":
		x.goTo(e, item.Path, x.listed)
	case item.Dir:
		x.goTo(e, item.Path, "")
	case x.asRoot:
		if err := e.sudoOpenPath(x.host, item.Path); err != nil {
			e.SetError("%v", err)
		}
	default:
		openItem(cx, item, action)
	}
}

// up lists the parent directory with the current one selected.
func (x *explorer) up(e *Editor) {
	if parent := filepath.Dir(x.dir); parent != x.dir {
		x.goTo(e, parent, x.dir)
	}
}

// handleAction runs the picker actions the explorer gives a meaning of
// its own and reports whether it took the action.
func (x *explorer) handleAction(cx *Context, action string) bool {
	p, e := x.picker, cx.Editor
	item, selected := p.Selected()
	switch action {
	case "picker_parent":
		x.up(e)
	case "picker_descend":
		p.accept(cx, PickerOpen)
	case "picker_delete_char_backward":
		if len(p.query) > 0 {
			return false
		}

		x.up(e)
	case "picker_complete":
		if len(p.query) == 0 || !selected || !item.Dir {
			return false
		}

		x.goTo(e, item.Path, "")
	case "picker_accept":
		if selected || len(p.query) == 0 || p.Loading || len(p.marks) > 0 {
			return false
		}

		x.createFromQuery(cx)
	case "picker_remove":
		x.confirmDelete(cx)
	case "picker_create":
		x.askCreate(cx)
	case "picker_rename":
		x.askMove(cx, false)
	case "picker_copy":
		x.askMove(cx, true)
	default:
		return false
	}

	return true
}

// slash takes a slash typed at the end of the filter as a step into the
// directory the filter names: an entry of that name or one starting with
// it, / for the root, ~ for home, .. for the parent. While a listing is
// still loading the name is taken as is. It reports whether it moved.
func (x *explorer) slash(cx *Context) bool {
	p, e := x.picker, cx.Editor
	if p.cursor != len(p.query) {
		return false
	}

	seg := string(p.query)
	var target, from string
	switch seg {
	case "":
		target = string(filepath.Separator)
	case ".":
		target = x.dir
	case "..":
		target, from = filepath.Dir(x.dir), x.dir
	case "~":
		home, err := x.host.Home()
		if err != nil {
			return false
		}

		target = home
	default:
		target = x.childDir(seg)
		if target == "" && p.Loading {
			target = filepath.Join(x.dir, seg)
		}
	}

	if target == "" {
		return false
	}

	x.goTo(e, target, from)

	return true
}

// childDir finds the listed directory named seg, or the selected one
// whose name starts with seg, ignoring case when seg is lower case.
func (x *explorer) childDir(seg string) string {
	p := x.picker
	for _, item := range p.items {
		if item.Dir && item.Label == seg+"/" {
			return item.Path
		}
	}

	item, ok := p.Selected()
	if !ok || !item.Dir || item.Label == "../" {
		return ""
	}

	name := item.Label
	if !strings.ContainsFunc(seg, unicode.IsUpper) {
		name = strings.ToLower(name)
	}

	if strings.HasPrefix(name, seg) {
		return item.Path
	}

	return ""
}

// recursive replaces the explorer with the file picker over everything
// below its directory, the filter stays.
func (x *explorer) recursive(cx *Context) {
	query := slices.Clone(x.picker.query)
	cx.Editor.closePicker()
	if fp := filePicker(cx, x.dir); fp != nil {
		fp.query, fp.cursor = query, len(query)
		fp.filter()
	}
}

// resolve turns typed input into a path, relative to the listed
// directory, ~ for home.
func (x *explorer) resolve(input string) (string, error) {
	path, err := host.ExpandHome(x.host, strings.TrimSpace(input))
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(x.dir, path)
	}

	return filepath.Clean(path), nil
}

// childOf names the entry of dir that holds path, path itself when it
// lies elsewhere.
func childOf(dir, path string) string {
	rel, ok := RelativeTo(dir, path)
	if !ok || rel == "." {
		return path
	}

	first, _, _ := strings.Cut(rel, string(filepath.Separator))

	return filepath.Join(dir, first)
}

// targets are the entries an operation acts on: the marked ones, or
// the selected one.
func (x *explorer) targets() []PickerItem {
	p := x.picker
	if len(p.marks) > 0 {
		return slices.Clone(p.marks)
	}

	if item, ok := p.Selected(); ok && item.Label != "../" {
		return []PickerItem{item}
	}

	return nil
}

// createFromQuery opens a new file named by the filter, nothing listed
// matches it. A name ending in a slash creates the directory and lists
// it.
func (x *explorer) createFromQuery(cx *Context) {
	e := cx.Editor
	input := string(x.picker.query)
	path, err := x.resolve(input)
	if err != nil {
		e.SetError("%v", err)

		return
	}

	if strings.HasSuffix(input, "/") {
		x.run(e, "create "+input, path, func(ctx context.Context) error { return x.mkdir(ctx, path) }, nil)
		x.dir = path

		return
	}

	e.closePicker()
	if x.asRoot {
		if err := e.sudoOpenPath(x.host, path); err != nil {
			e.SetError("%v", err)
		}

		return
	}

	openItem(cx, PickerItem{Path: path, Line: -1}, PickerOpen)
	e.SetStatus("new file %s, written on save", e.displayDir(path))
}

// ask closes the explorer for a prompt with value typed in. fn runs on
// the input, the explorer comes back when the prompt is left.
func (x *explorer) ask(cx *Context, prefix, value string, fn func(e *Editor, input string)) {
	e := cx.Editor
	e.closePicker()
	pr := e.openPrompt(prefix, func(cx *Context, input string, ev PromptEvent) {
		switch {
		case ev == PromptAbort, ev == PromptValidate && strings.TrimSpace(input) == "":
			x.reload(cx.Editor)
		case ev == PromptValidate:
			fn(cx.Editor, input)
		}
	})
	pr.input = []rune(value)
	pr.cursor = len(pr.input)
}

// askCreate asks for the name of a new file, or of a directory when it
// ends in a slash, and creates it with missing parents.
func (x *explorer) askCreate(cx *Context) {
	x.ask(cx, "create: ", string(x.picker.query), func(e *Editor, input string) {
		path, err := x.resolve(input)
		if err != nil {
			e.SetError("%v", err)
			x.reload(e)

			return
		}

		dir := strings.HasSuffix(input, "/")
		x.run(e, "create "+input, path, func(ctx context.Context) error {
			if found, _ := x.host.Exists([]string{path}); len(found) == 1 && found[0] {
				return fmt.Errorf("%s exists", path)
			}

			if dir {
				return x.mkdir(ctx, path)
			}

			if err := x.mkdir(ctx, filepath.Dir(path)); err != nil {
				return err
			}

			if x.asRoot {
				return x.tool(ctx, "touch", "--", path)
			}

			return x.host.WriteFile(path, nil, false)
		}, nil)
	})
}

// askMove asks where the targets go and moves or copies them there.
// One entry takes a new name, several go into a directory.
func (x *explorer) askMove(cx *Context, copying bool) {
	targets := x.targets()
	if len(targets) == 0 {
		return
	}

	verb := "rename"
	if copying {
		verb = "copy"
	}

	prefix, value := verb+": ", strings.TrimSuffix(displayPath(x.dir, targets[0].Path), "/")
	if len(targets) > 1 {
		prefix = fmt.Sprintf("%s %d entries to: ", strings.Replace(verb, "rename", "move", 1), len(targets))
		value = x.dir + string(filepath.Separator)
	}

	x.ask(cx, prefix, value, func(e *Editor, input string) {
		dest, err := x.resolve(input)
		if err != nil {
			e.SetError("%v", err)
			x.reload(e)

			return
		}

		var moved [][2]string
		into := len(targets) > 1 || isDir(x.host, dest)
		op := func(ctx context.Context) error {
			if into {
				if err := x.mkdir(ctx, dest); err != nil {
					return err
				}
			}

			for _, t := range targets {
				to := dest
				if into {
					to = filepath.Join(dest, filepath.Base(t.Path))
				}

				if to == t.Path {
					continue
				}

				if found, _ := x.host.Exists([]string{to}); len(found) == 1 && found[0] {
					return fmt.Errorf("%s exists", to)
				}

				if err := x.mkdir(ctx, filepath.Dir(to)); err != nil {
					return err
				}

				if err := x.transfer(ctx, t.Path, to, copying); err != nil {
					return err
				}

				moved = append(moved, [2]string{t.Path, to})
			}

			return nil
		}
		done := func(e *Editor) {
			if copying {
				return
			}

			for _, m := range moved {
				e.followMove(x.host, m[0], m[1])
			}
		}
		sel := dest
		if into && len(targets) == 1 {
			sel = filepath.Join(dest, filepath.Base(targets[0].Path))
		}

		x.run(e, verb, sel, op, done)
	})
}

// displayPath shows path relative to dir when it lies inside.
func displayPath(dir, path string) string {
	if rel, ok := RelativeTo(dir, path); ok {
		return rel
	}

	return path
}

// confirmDelete asks before removing the targets with everything below
// them.
func (x *explorer) confirmDelete(cx *Context) {
	targets := x.targets()
	if len(targets) == 0 {
		return
	}

	e := cx.Editor
	e.closePicker()
	what := strings.TrimSuffix(targets[0].Label, "/")
	if len(targets) > 1 {
		what = fmt.Sprintf("%d entries", len(targets))
	}

	e.SetError("delete %s? y/n", what)
	e.onNextKey = func(cx *Context, key keymap.Key) {
		e := cx.Editor
		if ch, ok := key.Char(); !ok || ch != 'y' && ch != 'Y' {
			e.SetStatus("not deleted")
			x.reload(e)

			return
		}

		x.run(e, "delete", "", func(ctx context.Context) error {
			for _, t := range targets {
				if err := x.tool(ctx, "rm", "-r", "--", t.Path); err != nil {
					return err
				}
			}

			return nil
		}, nil)
	}
}

// run changes the files in the background, then shows the explorer
// again with the entry holding sel selected, unless something else
// opened meanwhile. done runs first on the editor, also after an error.
func (x *explorer) run(e *Editor, what, sel string, op func(ctx context.Context) error, done func(e *Editor)) {
	e.closePicker()
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		defer cancel()

		err := op(ctx)
		emit(func(e *Editor) {
			if done != nil {
				done(e)
			}

			if err != nil {
				e.SetError("%s: %v", what, err)
			} else {
				x.picker.marks = nil
			}

			if e.Picker == nil && e.Prompt == nil && e.Menu == nil {
				x.goTo(e, x.dir, childOf(x.dir, sel))
			}
		})
	})
}

// mkdir creates dir with its parents.
func (x *explorer) mkdir(ctx context.Context, dir string) error {
	if x.asRoot {
		return x.tool(ctx, "mkdir", "-p", "--", dir)
	}

	return x.host.MkdirAll(dir)
}

// transfer moves or copies from to the free path to.
func (x *explorer) transfer(ctx context.Context, from, to string, copying bool) error {
	switch {
	case copying:
		return x.tool(ctx, "cp", "-R", "--", from, to)
	case x.asRoot:
		return x.tool(ctx, "mv", "--", from, to)
	}

	return x.host.Rename(from, to)
}

// tool runs a file tool on the host, through sudo for a root explorer,
// and returns its complaint on failure.
func (x *explorer) tool(ctx context.Context, name string, args ...string) error {
	cmd := host.Command{Name: name, Args: args}
	if x.asRoot {
		cmd = host.Command{Name: sudoCommand, Args: append([]string{"-n", name}, args...)}
	}

	_, stderr, err := x.host.Run(ctx, cmd)
	if err != nil {
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return errors.New(msg)
		}

		return err
	}

	return nil
}
