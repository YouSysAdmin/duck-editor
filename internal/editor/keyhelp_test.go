package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// helpHas reports whether a row of the key help holds text.
func helpHas(k *KeyHelp, text string) bool {
	return slices.ContainsFunc(k.Rows(), func(r ui.KeyHelpRow) bool { return strings.Contains(r.Keys+" "+r.Desc, text) })
}

// TestKeyHelpModes lists the keys of the mode or overlay in focus and
// leaves an open picker as it was.
func TestKeyHelpModes(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.Resize(100, 40)
	e.feed(t, "<F1>")
	k := e.KeyHelp
	if k == nil || k.Title != "Keys: normal mode" || !helpHas(k, describe("task_picker")) || !helpHas(k, "Space  space") {
		t.Fatalf("normal key help %+v", k)
	}

	// Typed text filters and keys go to the help, not the editor.
	e.feed(t, "undo")
	rows := k.Rows()
	if len(rows) == 0 || len(rows) > 10 || !helpHas(k, describe("undo")) || helpHas(k, describe("task_picker")) {
		t.Fatalf("filtered rows %+v", rows)
	}

	if e.Doc().Text().String() != "a" || e.Mode != Normal {
		t.Fatal("keys reached the editor while the help was open")
	}

	e.feed(t, "<F1>")
	if e.KeyHelp != nil {
		t.Fatal("F1 should close the help")
	}

	e.feed(t, "i<F1>")
	if e.KeyHelp == nil || e.KeyHelp.Title != "Keys: insert mode" || !helpHas(e.KeyHelp, "completion_accept") && !helpHas(e.KeyHelp, "Insert the selected item") {
		t.Fatalf("insert key help %+v", e.KeyHelp)
	}

	e.feed(t, "<esc>")
	if e.KeyHelp != nil || e.Mode != Insert {
		t.Fatalf("esc should close only the help, mode %v", e.Mode)
	}

	e.feed(t, "<esc><space>b<F1>")
	if e.Picker == nil || e.KeyHelp == nil || e.KeyHelp.Title != "Keys: Buffers" || !helpHas(e.KeyHelp, "Close the selected buffer") {
		t.Fatalf("picker key help %+v", e.KeyHelp)
	}

	e.feed(t, "<esc>")
	if e.Picker == nil || e.KeyHelp != nil {
		t.Fatal("closing the help should leave the picker open")
	}

	e.feed(t, "<esc>:<F1>")
	if e.Prompt == nil || e.KeyHelp == nil || !helpHas(e.KeyHelp, "Run the command line") {
		t.Fatalf("prompt key help %+v", e.KeyHelp)
	}
}

// TestKeyHelpScroll keeps the scroll within the rows.
func TestKeyHelpScroll(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]#")
	e.Resize(100, 20)
	e.feed(t, "<F1><end>")
	k := e.KeyHelp
	if want := len(k.Rows()) - e.KeyHelpHeight(); k.Scroll() != want || want <= 0 {
		t.Fatalf("scrolled to %d of %d rows", k.Scroll(), len(k.Rows()))
	}

	e.feed(t, "<pagedown><down>")
	if k.Scroll() != len(k.Rows())-e.KeyHelpHeight() {
		t.Fatal("scrolled past the end")
	}

	e.feed(t, "<home><up>")
	if k.Scroll() != 0 {
		t.Fatal("scrolled before the start")
	}
}
