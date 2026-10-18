package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/editor"
)

func TestViewWithPickerDoesNotHang(t *testing.T) {
	m := New(editor.New(editor.NewDocument("x.go", "package x\n"), config.Default()))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = next.(Model)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = m.View()
		next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "}))
		m = next.(Model)
		next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'b', Text: "b"}))
		m = next.(Model)
		_ = m.View()
		for _, r := range "xx" {
			next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
			m = next.(Model)
			_ = m.View()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("View or Update hangs with the picker open")
	}
}
