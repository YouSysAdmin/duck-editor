package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/YouSysAdmin/duck-editor/internal/editor"
)

// TestEscapeBurst replays what a script typing escape and a command
// produces: Alt plus ':' and the rest of the text right behind it.
func TestEscapeBurst(t *testing.T) {
	t.Parallel()

	m := newSized(t, "hello\n", 40, 10)
	m = press(t, m, tea.Key{Code: ':', Mod: tea.ModAlt})
	m = typeText(t, m, "quit")
	ed := m.Editor()
	if ed.Prompt == nil || ed.Prompt.Input() != "quit" {
		t.Fatalf("prompt %+v, want the command line with quit", ed.Prompt)
	}

	if got := ed.Doc().Text().String(); got != "hello\n" {
		t.Fatalf("text changed to %q", got)
	}
}

// TestAltChord keeps a lone Alt key a chord once nothing follows.
func TestAltChord(t *testing.T) {
	t.Parallel()

	m := newSized(t, "hello\n", 40, 10)
	m = typeText(t, m, "w")
	before := m.Editor().Doc().Selection().Primary()

	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: ';', Mod: tea.ModAlt}))
	if cmd == nil {
		t.Fatal("the Alt key waits for the burst window")
	}

	m = next.(Model)
	next, _ = m.Update(altTimeoutMsg{gen: m.alt.gen})
	m = next.(Model)

	after := m.Editor().Doc().Selection().Primary()
	if after.Anchor != before.Head || after.Head != before.Anchor || m.Editor().Prompt != nil {
		t.Fatalf("A-; should flip %+v, got %+v", before, after)
	}
}

// TestRepeatedAltChords keeps two Alt chords that arrive together as
// chords, a repeated A-d scrolls instead of closing the picker.
func TestRepeatedAltChords(t *testing.T) {
	t.Parallel()

	m := newSized(t, "hello\n", 40, 10)
	m = typeText(t, m, "w")
	before := m.Editor().Doc().Selection().Primary()
	m = press(t, m, tea.Key{Code: ';', Mod: tea.ModAlt})
	m = press(t, m, tea.Key{Code: ';', Mod: tea.ModAlt})
	next, _ := m.Update(altTimeoutMsg{gen: m.alt.gen})
	m = next.(Model)
	ed := m.Editor()
	// Two flips give the selection back, escape and ; would collapse it.
	if after := ed.Doc().Selection().Primary(); after != before || ed.Mode != editor.Normal {
		t.Fatalf("selection %+v, want %+v", after, before)
	}
}
