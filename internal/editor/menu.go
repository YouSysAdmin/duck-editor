package editor

import "github.com/YouSysAdmin/duck-editor/internal/keymap"

// Menu is a small list shown next to the cursor, used for code actions
// and the questions of a server.
type Menu struct {
	Items []string

	// Title is a line above the items, empty for none.
	Title string

	// Kinds labels the items at the right, nil for none.
	Kinds    []string
	selected int
	onSelect func(cx *Context, index int)

	// onCancel runs when the menu closes without a choice.
	onCancel func()
}

// Selected returns the highlighted row.
func (m *Menu) Selected() int {
	return m.selected
}

func (e *Editor) openMenu(items []string, onSelect func(cx *Context, index int)) {
	e.openMenuWith("", items, onSelect, nil)
}

// openMenuWith opens a titled menu, onCancel runs when it closes
// without a choice. A menu open before is cancelled.
func (e *Editor) openMenuWith(title string, items []string, onSelect func(cx *Context, index int), onCancel func()) {
	if len(items) == 0 {
		if onCancel != nil {
			onCancel()
		}

		return
	}

	e.closeMenu()
	e.Menu = &Menu{Items: items, Title: title, onSelect: onSelect, onCancel: onCancel}
}

// closeMenu drops the menu without a choice.
func (e *Editor) closeMenu() {
	m := e.Menu
	if m == nil {
		return
	}

	e.Menu = nil
	if m.onCancel != nil {
		m.onCancel()
	}
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
		e.closeMenu()
	default:
		e.closeMenu()

		return false
	}

	return true
}
