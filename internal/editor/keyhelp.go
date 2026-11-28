package editor

import (
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// KeyHelp lists the keys of what has the focus, filtered by what was
// typed. It opens over everything and takes the keys until it closes.
type KeyHelp struct {
	Title    string
	sections []keymap.Section
	query    []rune
	scroll   int
}

func init() {
	register("key_help", "Show the keys of the current mode", func(cx *Context) { cx.Editor.openKeyHelp() })
}

// Query is the filter typed so far.
func (k *KeyHelp) Query() string {
	return string(k.query)
}

// Scroll is the first shown row.
func (k *KeyHelp) Scroll() int {
	return k.scroll
}

// Total counts the bindings without a filter.
func (k *KeyHelp) Total() int {
	n := 0
	for _, s := range k.sections {
		n += len(s.Entries)
	}

	return n
}

// Rows returns the rows matching the filter, every word of it found in
// the keys or the description, case ignored. A heading shows when one of
// its bindings matches, all of them when the heading itself matches.
func (k *KeyHelp) Rows() []ui.KeyHelpRow {
	terms := strings.Fields(strings.ToLower(string(k.query)))
	match := func(text string) bool {
		text = strings.ToLower(text)

		return !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, t) })
	}

	var rows []ui.KeyHelpRow
	for _, s := range k.sections {
		heading := s.Title
		if len(s.Prefixes) > 0 {
			heading += "  " + strings.Join(s.Prefixes, ", ")
		}

		all := match(heading)
		var entries []ui.KeyHelpRow
		for _, e := range s.Entries {
			keys := strings.Join(e.Keys, ", ")
			if all || match(keys+" "+e.Desc) {
				entries = append(entries, ui.KeyHelpRow{Keys: keys, Desc: e.Desc})
			}
		}

		if len(entries) > 0 {
			rows = append(rows, ui.KeyHelpRow{Heading: true, Desc: heading})
			rows = append(rows, entries...)
		}
	}

	return rows
}

// KeyHelpHeight is how many rows of the key help fit on the screen.
func (e *Editor) KeyHelpHeight() int {
	// The box leaves two rows around it and has a border, a filter line
	// and a rule.
	return max(e.height-8, 1)
}

// openKeyHelp shows the keys of the menu, picker or command line that is
// open, else of the mode.
func (e *Editor) openKeyHelp() {
	actionDoc := func(name string) string {
		if doc, ok := keymap.ActionDoc(name); ok {
			return doc
		}

		return name
	}
	commandDoc := func(name string) string {
		if strings.HasPrefix(name, ":") {
			return name
		}

		return describe(name)
	}
	overlay := func(mode keymap.Mode) []keymap.Section {
		return e.Keymaps.Map(mode).Sections(actionDoc)
	}

	help := &KeyHelp{}
	switch {
	case e.Menu != nil:
		help.Title, help.sections = "Keys: menu", overlay(keymap.Menu)
	case e.Picker != nil:
		help.Title, help.sections = "Keys: "+e.Picker.Title, overlay(keymap.Picker)
	case e.Prompt != nil:
		help.Title, help.sections = "Keys: command line", overlay(keymap.Prompt)
	case e.Mode == Insert:
		help.Title = "Keys: insert mode"
		help.sections = append(e.Keymaps.Map(keymap.Insert).Sections(commandDoc), overlay(keymap.Completion)...)
	case e.Mode == Select:
		help.Title, help.sections = "Keys: select mode", e.Keymaps.Map(keymap.Select).Sections(commandDoc)
	default:
		help.Title, help.sections = "Keys: normal mode", e.Keymaps.Map(keymap.Normal).Sections(commandDoc)
	}

	e.KeyHelp = help
}

// handleKey filters, scrolls or closes the key help.
func (k *KeyHelp) handleKey(e *Editor, key keymap.Key) {
	page := e.KeyHelpHeight()
	last := max(len(k.Rows())-page, 0)
	switch {
	case key.Mods == 0 && (key.Code == keymap.KeyEsc || key.Code == keymap.KeyF(1)),
		key == keymap.MustParse("C-c"):
		e.KeyHelp = nil
	case key.Mods == 0 && key.Code == keymap.KeyDown, key == keymap.MustParse("C-n"), key == keymap.MustParse("C-j"):
		k.scroll++
	case key.Mods == 0 && key.Code == keymap.KeyUp, key == keymap.MustParse("C-p"), key == keymap.MustParse("C-k"):
		k.scroll--
	case key.Mods == 0 && key.Code == keymap.KeyPageDown, key == keymap.MustParse("C-d"):
		k.scroll += max(page/2, 1)
	case key.Mods == 0 && key.Code == keymap.KeyPageUp, key == keymap.MustParse("C-u"):
		k.scroll -= max(page/2, 1)
	case key.Mods == 0 && key.Code == keymap.KeyHome:
		k.scroll = 0
	case key.Mods == 0 && key.Code == keymap.KeyEnd:
		k.scroll = last
	case key.Code == keymap.KeyBackspace, key == keymap.MustParse("C-h"):
		if len(k.query) > 0 {
			k.query = k.query[:len(k.query)-1]
			k.scroll = 0
		}
	case key.Mods&^keymap.ModShift == 0 && key.Code >= ' ' && key.Code < keymap.KeyBackspace:
		k.query = append(k.query, key.Code)
		k.scroll = 0
	}

	k.scroll = min(max(k.scroll, 0), max(len(k.Rows())-page, 0))
}
