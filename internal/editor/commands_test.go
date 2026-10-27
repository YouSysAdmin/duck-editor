package editor

import (
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// TestDefaultKeymapCommandsExist keeps the default key bindings in step
// with the registered commands.
func TestDefaultKeymapCommandsExist(t *testing.T) {
	t.Parallel()

	var check func(path string, tr *keymap.Trie)
	check = func(path string, tr *keymap.Trie) {
		for _, name := range append([]string{tr.Command}, tr.Sequence...) {
			if name == "" || strings.HasPrefix(name, ":") {
				continue
			}

			if _, ok := commands[name]; !ok {
				t.Errorf("%s binds unknown command %s", path, name)
			}
		}

		if tr.Node == nil {
			return
		}

		for _, key := range tr.Node.Keys() {
			child, _ := tr.Node.Get(key)
			check(path+" "+key.String(), child)
		}
	}

	for mode, tr := range keymap.Default() {
		if !mode.Overlay() {
			check(mode.String(), tr)

			continue
		}

		if err := keymap.CheckOverlay(mode, tr); err != nil {
			t.Errorf("%s: %v", mode, err)
		}

		// Every action has a default key.
		bound := tr.Bindings()
		for _, a := range keymap.Actions(mode) {
			if len(bound[a.Name]) == 0 {
				t.Errorf("%s action %s has no key", mode, a.Name)
			}
		}
	}
}
