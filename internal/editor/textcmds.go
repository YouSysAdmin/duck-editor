package editor

import (
	"context"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"uniq"},
		doc:   "Drop repeated lines from the selections, the first one stays. A single cursor works on the whole buffer",
		run: func(cx *Context, _ []string) error {
			uniqLines(cx.Editor)

			return nil
		},
	}, typedCommand{
		names: []string{"retab"},
		doc:   "Rewrite the indentation of the buffer in its indent style, tabs or spaces",
		run: func(cx *Context, _ []string) error {
			retab(cx.Editor)

			return nil
		},
	}, typedCommand{
		names: []string{"delete-file"},
		doc:   "Delete the file of the buffer after a y, then close the buffer",
		run: func(cx *Context, _ []string) error {
			return cx.Editor.deleteFile()
		},
	})
}

// uniqLines drops repeated lines inside each selection, or the whole
// buffer when there is one selection within a single line.
func uniqLines(e *Editor) {
	doc := e.Doc()
	text := doc.Text()
	sel := doc.Selection()
	whole := sel.Len() == 1 && text.CharToLine(sel.Primary().From()) == text.CharToLine(max(sel.Primary().To()-1, sel.Primary().From()))
	dropped := 0
	uniq := func(s string) string {
		lines := linesWithEndings(s)
		seen := map[string]bool{}
		var out strings.Builder
		for _, l := range lines {
			key := strings.TrimRight(l, "\r\n")
			if seen[key] {
				dropped++

				continue
			}

			seen[key] = true
			out.WriteString(l)
		}

		return out.String()
	}
	if whole {
		replaceByLines(doc, uniq(text.String()))
	} else {
		tx := edit.ChangeBySelection(text, sel, func(r edit.Range) edit.Change {
			return edit.Change{From: r.From(), To: r.To(), Text: uniq(r.Fragment(text))}
		})
		doc.Apply(&tx)
	}

	e.SetStatus("dropped %s", plural(dropped, "repeated line"))
}

// retab rewrites the leading whitespace of every line in the indent
// style of the buffer, keeping its visual width.
func retab(e *Editor) {
	doc := e.Doc()
	tabWidth := max(doc.TabWidth(), 1)
	tabs := doc.Indent().Tabs
	var out strings.Builder
	for _, line := range linesWithEndings(doc.Text().String()) {
		body := strings.TrimLeft(line, " \t")
		width := 0
		for _, c := range line[:len(line)-len(body)] {
			if c == '\t' {
				width += tabWidth - width%tabWidth
			} else {
				width++
			}
		}

		if strings.TrimSpace(body) == "" {
			// A blank line keeps nothing but its line break.
			width = 0
		}

		if tabs {
			out.WriteString(strings.Repeat("\t", width/tabWidth) + strings.Repeat(" ", width%tabWidth))
		} else {
			out.WriteString(strings.Repeat(" ", width))
		}

		out.WriteString(body)
	}

	replaceByLines(doc, out.String())
	style := "spaces"
	if tabs {
		style = "tabs"
	}

	e.SetStatus("indentation rewritten with %s", style)
}

// deleteFile removes the file of the buffer after a y and closes it.
func (e *Editor) deleteFile() error {
	doc := e.Doc()
	path := doc.Path()
	if path == "" || doc.tail != nil || doc.kube != nil {
		return errNoFile
	}

	h, docID, asRoot := e.hostOf(doc), doc.ID, doc.asRoot
	e.SetError("delete %s? y/n", path)
	e.onNextKey = func(cx *Context, key keymap.Key) {
		if ch, ok := key.Char(); !ok || ch != 'y' && ch != 'Y' {
			cx.Editor.SetStatus("not deleted")

			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
		defer cancel()

		cmd := host.Command{Name: "rm", Args: []string{"-f", "--", path}}
		if asRoot {
			cmd = host.Command{Name: sudoCommand, Args: append([]string{"-n", cmd.Name}, cmd.Args...)}
		}

		if _, stderr, err := h.Run(ctx, cmd); err != nil {
			cx.Editor.SetError("delete %s: %s", path, strings.TrimSpace(string(stderr)))

			return
		}

		_ = cx.Editor.CloseDocument(docID, true)
		cx.Editor.SetStatus("deleted %s", path)
	}

	return nil
}
