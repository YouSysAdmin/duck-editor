package editor

import (
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// autoPairs returns the pairs active for a document, or nil when disabled
// globally or by the language.
func (e *Editor) autoPairs(doc *Document) *edit.AutoPairs {
	if doc.language != nil && doc.language.AutoPairsOff {
		return nil
	}

	switch v := e.Config.Editor.AutoPairs.(type) {
	case bool:
		if !v {
			return nil
		}
	case map[string]any:
		if doc.language == nil || doc.language.AutoPairs == nil {
			pairs := make([]edit.Pair, 0, len(v))
			for open, closeV := range v {
				if closeStr, ok := closeV.(string); ok && open != "" && closeStr != "" {
					pairs = append(pairs, edit.Pair{Open: []rune(open)[0], Close: []rune(closeStr)[0]})
				}
			}

			return edit.NewAutoPairs(pairs)
		}
	}

	if doc.language != nil {
		if doc.language.AutoPairsOff {
			return nil
		}

		if len(doc.language.AutoPairs) > 0 {
			pairs := make([]edit.Pair, 0, len(doc.language.AutoPairs))
			for open, closeR := range doc.language.AutoPairs {
				pairs = append(pairs, edit.Pair{Open: open, Close: closeR})
			}

			return edit.NewAutoPairs(pairs)
		}
	}

	return edit.NewAutoPairs(edit.DefaultPairs)
}
