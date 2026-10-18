package editor

import (
	"fmt"
	"slices"
	"strings"
)

// yankRingSize is how many yanks the history keeps.
const yankRingSize = 30

func init() {
	register("yank_history_picker", "Pick an earlier yank or delete and paste it", yankHistoryPicker)
}

// yankTo writes a yank or delete to a register and keeps it in the
// history of yanks.
func (e *Editor) yankTo(reg rune, values []string) error {
	if err := e.writeRegister(reg, values); err != nil {
		return err
	}

	if reg != '_' && strings.TrimSpace(strings.Join(values, "")) != "" {
		e.yankRing = slices.DeleteFunc(e.yankRing, func(v []string) bool { return slices.Equal(v, values) })
		e.yankRing = slices.Insert(e.yankRing, 0, slices.Clone(values))
		e.yankRing = e.yankRing[:min(len(e.yankRing), yankRingSize)]
	}

	return nil
}

// yankHistoryPicker lists earlier yanks, newest first. The picked one
// goes back into the yank register and is pasted after the selections.
func yankHistoryPicker(cx *Context) {
	e := cx.Editor
	if len(e.yankRing) == 0 {
		e.SetStatus("Nothing yanked yet")

		return
	}

	ring := e.yankRing
	items := make([]PickerItem, len(ring))
	for i, values := range ring {
		label := strings.Join(strings.Fields(strings.Join(values, " ")), " ")
		if len(values) > 1 {
			label = fmt.Sprintf("[%d] %s", len(values), label)
		}

		items[i] = PickerItem{Label: label, Line: -1, Index: i}
	}

	p := NewPicker("Yank history", items, func(cx *Context, item PickerItem, _ PickerAction) {
		values := ring[item.Index]
		if err := cx.Editor.yankTo(cx.Editor.yankRegister(), values); err != nil {
			cx.Editor.SetError("%v", err)

			return
		}

		pasteImpl(cx, values, pasteAfter, 1)
	})
	e.openPicker(p)
}
