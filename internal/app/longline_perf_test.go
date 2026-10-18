package app

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestPerfLongLine times frames with a line of a megabyte, with and
// without soft wrap. Set DUCK_PERF=1 to run it.
func TestPerfLongLine(t *testing.T) {
	if os.Getenv("DUCK_PERF") == "" {
		t.Skip("DUCK_PERF not set")
	}

	long := strings.Repeat("abc def ", 125000) + "\nnext\n"
	for _, wrap := range []bool{false, true} {
		m := newSized(t, long, 80, 24)
		m.editor.Config.Editor.SoftWrap.Enable = &wrap
		start := time.Now()
		_ = m.View()
		t.Logf("wrap %v first view %v", wrap, time.Since(start))

		start = time.Now()
		m = press(t, m, tea.Key{Code: 'l'}, tea.Key{Code: 'l'})
		_ = m.View()
		t.Logf("wrap %v two moves and view %v", wrap, time.Since(start))

		start = time.Now()
		m = press(t, m, tea.Key{Code: 'g'}, tea.Key{Code: 'l'})
		_ = m.View()
		t.Logf("wrap %v goto line end and view %v", wrap, time.Since(start))
	}
}
