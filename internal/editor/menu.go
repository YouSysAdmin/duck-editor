package editor

import "github.com/YouSysAdmin/duck-editor/internal/keymap"

// Menu is a small list shown next to the cursor, used for code actions.
type Menu struct {
	Items []string

	// Kinds labels the items at the right, nil for none.
	Kinds    []string
	selected int
	onSelect func(cx *Context, index int)
}

// Selected returns the highlighted row.
func (m *Menu) Selected() int {
	return m.selected
}

func (e *Editor) openMenu(items []string, onSelect func(cx *Context, index int)) {
	if len(items) == 0 {
		return
	}

	e.Menu = &Menu{Items: items, onSelect: onSelect}
}

// handleKey processes menu keys. Unknown keys close the menu and are
// then handled normally.
func (m *Menu) handleKey(cx *Context, key keymap.Key) bool {
	e := cx.Editor
	switch {
	case key == keymap.Ctrl('n') || key == (keymap.Key{Code: keymap.KeyTab}) || key == (keymap.Key{Code: keymap.KeyDown}) || key == (keymap.Key{Code: 'j'}):
		m.selected = (m.selected + 1) % len(m.Items)
	case key == keymap.Ctrl('p') || key == (keymap.Key{Code: keymap.KeyTab, Mods: keymap.ModShift}) || key == (keymap.Key{Code: keymap.KeyUp}) || key == (keymap.Key{Code: 'k'}):
		m.selected = (m.selected - 1 + len(m.Items)) % len(m.Items)
	case key == (keymap.Key{Code: keymap.KeyEnter}):
		e.Menu = nil
		m.onSelect(cx, m.selected)
	case key == (keymap.Key{Code: keymap.KeyEsc}) || key == keymap.Ctrl('c'):
		e.Menu = nil
	default:
		e.Menu = nil

		return false
	}

	return true
}
