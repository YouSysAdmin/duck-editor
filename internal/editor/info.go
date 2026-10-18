package editor

import (
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// Info is the key hint box shown while a key sequence is pending or a
// sticky menu is active.
type Info struct {
	Title   string
	Entries []keymap.Entry
}

// infoFromNode lists the bindings of a keymap node.
func infoFromNode(node *keymap.Node) *Info {
	entries := node.Entries(func(command string) string {
		if strings.HasPrefix(command, ":") {
			return command
		}

		return describe(command)
	})

	return &Info{Title: node.Name, Entries: entries}
}

// updateInfo shows the pending node, the sticky node or nothing.
func (e *Editor) updateInfo(res keymap.Result) {
	if !e.Config.Editor.AutoInfo {
		e.Info = nil

		return
	}

	switch {
	case res.Kind == keymap.Pending && res.Node != nil:
		e.Info = infoFromNode(res.Node)
	case e.Keymaps.Sticky() != nil:
		e.Info = infoFromNode(e.Keymaps.Sticky())
	default:
		e.Info = nil
	}
}

// registersInfo lists the registers with a preview of their latest
// value, shown when selecting a register.
func (e *Editor) registersInfo(title string) *Info {
	var entries []keymap.Entry
	names := make([]rune, 0, len(e.Registers))
	for name := range e.Registers {
		if name == '+' || name == '*' {
			continue
		}

		names = append(names, name)
	}

	slices.Sort(names)
	for _, name := range names {
		values := e.Registers[name]
		preview := "<empty>"
		if len(values) > 0 {
			preview, _, _ = strings.Cut(values[0], "\n")
		}

		entries = append(entries, keymap.Entry{Keys: []string{string(name)}, Desc: preview})
	}

	for _, special := range []struct {
		name rune
		desc string
	}{{'_', "<empty>"}, {'#', "<selection indices>"}, {'.', "<selection contents>"}, {'%', "<document path>"}, {'+', "<system clipboard>"}, {'*', "<primary clipboard>"}} {
		entries = append(entries, keymap.Entry{Keys: []string{string(special.name)}, Desc: special.desc})
	}

	return &Info{Title: title, Entries: entries}
}

// showTransientInfo shows a box that the next key closes.
func (e *Editor) showTransientInfo(info *Info) {
	if !e.Config.Editor.AutoInfo {
		return
	}

	e.Info = info
	e.infoTransient = true
}
