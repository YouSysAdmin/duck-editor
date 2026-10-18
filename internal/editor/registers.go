package editor

import (
	"strconv"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/clipboard"
)

// Clipboard registers, backed by the system clipboard.
const (
	clipboardRegister = '+'
	primaryRegister   = '*'
)

// Clipboard returns the active provider, detecting one on first use.
func (e *Editor) Clipboard() clipboard.Provider {
	if e.clipboard == nil {
		e.clipboard = e.configuredClipboard()
	}

	return e.clipboard
}

// SetClipboard replaces the provider, used by the UI and tests.
func (e *Editor) SetClipboard(p clipboard.Provider) {
	e.clipboard = p
}

func (e *Editor) configuredClipboard() clipboard.Provider {
	switch v := e.Config.Editor.ClipboardProvider.(type) {
	case string:
		if p, ok := clipboard.Named(v); ok {
			return e.withTerminalFallback(p)
		}

		e.SetError("unknown clipboard provider %q", v)
	case map[string]any:
		custom := &clipboard.Custom{}
		for key, target := range map[string]*clipboard.Command{
			"yank": &custom.Yank, "paste": &custom.Paste,
			"primary-yank": &custom.PrimaryYank, "primary-paste": &custom.PrimaryPaste,
		} {
			if m, ok := v[key].(map[string]any); ok {
				target.Command, _ = m["command"].(string)
				if args, ok := m["args"].([]any); ok {
					for _, a := range args {
						if s, ok := a.(string); ok {
							target.Args = append(target.Args, s)
						}
					}
				}
			}
		}

		return custom
	}

	return e.withTerminalFallback(clipboard.Detect())
}

// withTerminalFallback queues the writes of the terminal escape provider
// so the UI can send them to the terminal.
func (e *Editor) withTerminalFallback(p clipboard.Provider) clipboard.Provider {
	if mem, ok := p.(*clipboard.Memory); ok && mem.Title == "termcode" {
		mem.OnSet = func(text string) { e.clipboardWrites = append(e.clipboardWrites, text) }
	}

	return p
}

// TakeClipboardWrites returns the texts the terminal escape provider
// stored since the last call.
func (e *Editor) TakeClipboardWrites() []string {
	out := e.clipboardWrites
	e.clipboardWrites = nil

	return out
}

// writeRegister stores values. The clipboard registers also copy the
// values joined by newlines to the system clipboard, the black hole
// register discards everything.
func (e *Editor) writeRegister(reg rune, values []string) error {
	switch reg {
	case '_':
		return nil
	case clipboardRegister, primaryRegister:
		if err := e.Clipboard().Set(reg == primaryRegister, strings.Join(values, "\n")); err != nil {
			return err
		}
	}

	e.Registers[reg] = values

	return nil
}

// readRegister returns the values of a register. The clipboard registers
// read the system clipboard and keep the yanked split when the contents
// still match, so multiple cursors paste back their own pieces.
func (e *Editor) readRegister(reg rune) ([]string, bool) {
	switch reg {
	case '_':
		return nil, false
	case clipboardRegister, primaryRegister:
		content, err := e.Clipboard().Get(reg == primaryRegister)
		if err != nil {
			e.SetError("%v", err)

			return nil, false
		}

		if cached := e.Registers[reg]; len(cached) > 0 && strings.Join(cached, "\n") == content {
			return cached, true
		}

		if content == "" {
			return nil, false
		}

		return []string{content}, true
	case '%':
		return []string{e.docName(e.Doc())}, true
	case '.':
		doc := e.Doc()

		return doc.Selection().Fragments(doc.Text()), true
	case '#':
		n := e.Doc().Selection().Len()
		out := make([]string, n)
		for i := range n {
			out[i] = strconv.Itoa(i + 1)
		}

		return out, true
	}

	values, ok := e.Registers[reg]

	return values, ok && len(values) > 0
}
