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
		check(mode.String(), tr)
	}
}
