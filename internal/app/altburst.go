package app

import (
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// altBurstWindow is how long an Alt plus character key waits for another
// key. The terminal reader holds a lone escape for a short while to see
// whether a sequence follows, so an escape another program types right
// before some text, as a zellij or tmux script does with ":open path",
// reaches us as Alt plus the first character followed by the rest at
// once. A real Alt chord is followed by nothing that fast.
const altBurstWindow = 5 * time.Millisecond

// altState is the Alt key waiting for the end of altBurstWindow.
type altState struct {
	pending *tea.KeyPressMsg
	gen     int
}

// altTimeoutMsg ends the wait for the pending Alt key of generation gen.
type altTimeoutMsg struct {
	gen int
}

// altText reports whether msg is Alt plus a printable character, the
// shape an escape followed by that character decodes to.
func altText(msg tea.KeyPressMsg) bool {
	return msg.Mod&^tea.ModShift == tea.ModAlt && msg.Code < unicode.MaxRune && unicode.IsPrint(msg.Code) && msg.Code != ' '
}

// keyPress handles a key, holding back an Alt plus character key until it
// is clear whether more keys follow at once.
func (m Model) keyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if pending := m.alt.pending; pending != nil {
		m.alt.pending = nil
		if msg.Mod == 0 || msg.Mod == tea.ModShift {
			// Plain text came in the same burst: the Alt key was an
			// escape followed by its character.
			m.editor.HandleKey(keymap.Key{Code: keymap.KeyEsc})
			m.editor.HandleKey(toKey(withoutAlt(*pending)))
		} else {
			// Another chord right behind it, as a repeated A-d, makes
			// it a chord too.
			m.editor.HandleKey(toKey(*pending))
		}
	}

	if altText(msg) {
		m.alt.gen++
		m.alt.pending = &msg
		gen := m.alt.gen

		return m, tea.Tick(altBurstWindow, func(time.Time) tea.Msg { return altTimeoutMsg{gen: gen} })
	}

	m.editor.HandleKey(toKey(msg))

	return m.afterEditor()
}

// altTimeout delivers the pending Alt key as the chord it is.
func (m Model) altTimeout(msg altTimeoutMsg) (tea.Model, tea.Cmd) {
	pending := m.alt.pending
	if pending == nil || msg.gen != m.alt.gen {
		return m, nil
	}

	m.alt.pending = nil
	m.editor.HandleKey(toKey(*pending))

	return m.afterEditor()
}

// withoutAlt is the key an Alt key stands for without the escape.
func withoutAlt(msg tea.KeyPressMsg) tea.KeyPressMsg {
	msg.Mod &^= tea.ModAlt
	msg.Text = string(msg.Code)
	if msg.Mod&tea.ModShift != 0 {
		msg.Text = strings.ToUpper(msg.Text)
	}

	return msg
}
