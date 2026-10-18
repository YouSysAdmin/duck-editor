package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// location is a place in a file named by a line of program output.
type location struct {
	path      string
	line, col int // 1-based, zero when not given
	text      string
}

// locationLine matches file:line[:col][: text], the form compilers,
// linters and grep print, also with " - text" as tsc writes it.
var locationLine = regexp.MustCompile(`^\s*([^\s:][^:]*?):(\d+)(?::(\d+))?(?:(?::\s?|\s+-?\s*)(.*))?$`)

// parseLocation reads a location from a line of output, paths relative
// to dir.
func parseLocation(line, dir string) (location, bool) {
	m := locationLine.FindStringSubmatch(line)
	if m == nil {
		return location{}, false
	}

	l, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	path := m[1]
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}

	return location{path: path, line: l, col: c, text: strings.TrimSpace(m[4])}, true
}

func init() {
	register("goto_next_quickfix", "Goto next location of :make", func(cx *Context) { stepQuickfix(cx.Editor, cx.Count()) })
	register("goto_prev_quickfix", "Goto previous location of :make", func(cx *Context) { stepQuickfix(cx.Editor, -cx.Count()) })
	register("quickfix_picker", "Open the locations of :make in a picker", quickfixPicker)
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"make"},
		raw:   true,
		doc:   "Run a build command, make by default, and collect the file:line places it prints, ]q and [q go through them",
		run: func(cx *Context, args []string) error {
			cmd := "make"
			if len(args) > 0 {
				cmd = args[0]
			}

			return cx.Editor.runMake(cmd)
		},
	}, typedCommand{
		names: []string{"pick-from"},
		raw:   true,
		doc:   "Run a shell command and pick a line of its output, file:line lines open there and other lines open as a path",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return cmdError("usage: :pick-from <command>")
			}

			return cx.Editor.pickFrom(args[0])
		},
	})
}

// runCombined runs a shell command in the background, escape cancels it,
// and hands its output, stderr after stdout, to done on the editor
// thread.
func (e *Editor) runCombined(cmd string, done func(e *Editor, out string, err error)) error {
	cmd, err := e.expand(cmd)
	if err != nil {
		return err
	}

	e.runShellJob(cmd, buildTimeout, func(ctx context.Context, shell shellRunner) ([]string, error) {
		c := host.Command{Name: shell.argv[0], Args: append(append([]string{}, shell.argv[1:]...), cmd), Dir: shell.dir}
		stdout, stderr, err := shell.host.Run(ctx, c)

		return []string{string(stdout) + string(stderr)}, err
	}, func(e *Editor, results []string, err error) {
		done(e, results[0], err)
	})

	return nil
}

// runMake runs a build command and keeps the places it reports.
func (e *Editor) runMake(cmd string) error {
	dir := e.WorkingDir()

	return e.runCombined(cmd, func(e *Editor, out string, err error) {
		var locs []location
		for line := range strings.Lines(out) {
			if loc, ok := parseLocation(strings.TrimRight(line, "\r\n"), dir); ok {
				locs = append(locs, loc)
			}
		}

		e.quickfix, e.quickfixIdx = locs, -1
		switch {
		case len(locs) > 0:
			stepQuickfix(e, 1)
		case err != nil:
			e.SetError("%s failed, nothing to go to: %s", cmd, firstLines(strings.TrimSpace(out), 1))
		default:
			e.SetStatus("%s finished", cmd)
		}
	})
}

// stepQuickfix moves through the places of :make by delta, wrapping.
func stepQuickfix(e *Editor, delta int) {
	n := len(e.quickfix)
	if n == 0 {
		e.SetStatus("No locations, :make collects them")

		return
	}

	e.quickfixIdx = ((e.quickfixIdx+delta)%n + n) % n
	loc := e.quickfix[e.quickfixIdx]
	e.pushJump()
	if err := e.gotoLocation(loc, ActionReplace); err != nil {
		e.SetError("%v", err)

		return
	}

	e.SetStatus("[%d/%d] %s", e.quickfixIdx+1, n, loc.text)
}

// gotoLocation opens the file of a location at its line and column.
func (e *Editor) gotoLocation(loc location, action Action) error {
	doc, err := e.Open(loc.path, action)
	if err != nil {
		return err
	}

	if loc.line > 0 {
		text := doc.Text()
		l := min(loc.line-1, text.LenLines()-1)
		start := text.LineToChar(l)
		col := min(max(loc.col-1, 0), utf8.RuneCountInString(text.LineWithoutEnding(l)))
		doc.SetSelection(edit.PointSelection(start + col))
	}

	e.ensureCursorInViewCenter()

	return nil
}

func quickfixPicker(cx *Context) {
	e := cx.Editor
	if len(e.quickfix) == 0 {
		e.SetStatus("No locations, :make collects them")

		return
	}

	locs := e.quickfix
	items := make([]PickerItem, len(locs))
	for i, loc := range locs {
		items[i] = PickerItem{Label: locationLabelOf(e, loc), Path: loc.path, Line: loc.line - 1, Index: i}
	}

	e.openPicker(NewPicker("Make", items, func(cx *Context, item PickerItem, action PickerAction) {
		cx.Editor.quickfixIdx = item.Index
		if err := cx.Editor.gotoLocation(locs[item.Index], actionFor(action)); err != nil {
			cx.Editor.SetError("%v", err)
		}
	}))
}

// locationLabelOf shows a location relative to the working directory.
func locationLabelOf(e *Editor, loc location) string {
	rel := loc.path
	if r, ok := RelativeTo(e.WorkingDir(), loc.path); ok {
		rel = r
	}

	label := fmt.Sprintf("%s:%d", rel, loc.line)
	if loc.col > 0 {
		label += ":" + strconv.Itoa(loc.col)
	}

	if loc.text != "" {
		label += " " + loc.text
	}

	return label
}

// pickFrom lists the output lines of a command in a picker.
func (e *Editor) pickFrom(cmd string) error {
	dir := e.WorkingDir()

	return e.runCombined(cmd, func(e *Editor, out string, err error) {
		var locs []location
		var items []PickerItem
		for line := range strings.Lines(out) {
			line = strings.TrimRight(line, "\r\n")
			if strings.TrimSpace(line) == "" {
				continue
			}

			loc, ok := parseLocation(line, dir)
			if !ok {
				// Any other line names a file, as rg --files prints them.
				loc = location{path: line}
				if !filepath.IsAbs(line) {
					loc.path = filepath.Join(dir, line)
				}
			}

			items = append(items, PickerItem{Label: line, Path: loc.path, Line: loc.line - 1, Index: len(locs)})
			locs = append(locs, loc)
		}

		if len(items) == 0 {
			if err != nil {
				e.SetError("%s: %v", cmd, err)
			} else {
				e.SetStatus("%s printed nothing", cmd)
			}

			return
		}

		e.SetStatus("")
		e.openPicker(NewPicker(firstLines(cmd, 1), items, func(cx *Context, item PickerItem, action PickerAction) {
			if err := cx.Editor.gotoLocation(locs[item.Index], actionFor(action)); err != nil {
				cx.Editor.SetError("%v", err)
			}
		}))
	})
}
