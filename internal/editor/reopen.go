package editor

import "github.com/YouSysAdmin/duck-editor/internal/edit"

// closedBuffer remembers a closed file and where its cursor was.
type closedBuffer struct {
	path      string
	selection edit.Selection
}

// maxClosedBuffers bounds the reopen list.
const maxClosedBuffers = 50

func init() {
	register("reopen_buffer", "Reopen the most recently closed buffer", reopenBuffer)
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"reopen"},
		doc:   "Reopen the most recently closed buffer",
		run: func(cx *Context, _ []string) error {
			reopenBuffer(cx)

			return nil
		},
	})
}

// reopenBuffer opens the newest closed file that is not open again,
// with the selection it had.
func reopenBuffer(cx *Context) {
	e := cx.Editor
	for len(e.closed) > 0 {
		last := e.closed[len(e.closed)-1]
		e.closed = e.closed[:len(e.closed)-1]
		if _, open := e.DocumentByPath(last.path); open {
			continue
		}

		doc, err := e.Open(last.path, ActionReplace)
		if err != nil {
			e.SetError("%v", err)

			return
		}

		doc.SetSelection(last.selection.EnsureInvariants(doc.Text()))
		e.EnsureCursorInView()

		return
	}

	e.SetError("no closed buffer to reopen")
}
