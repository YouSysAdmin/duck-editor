package editor

import (
	"strings"
)

func init() {
	for _, c := range []struct {
		prefix string
		reg    rune
		label  string
	}{{"clipboard", clipboardRegister, "system clipboard"}, {"primary-clipboard", primaryRegister, "primary clipboard"}} {
		typedCommands = append(typedCommands, typedCommand{
			names: []string{c.prefix + "-yank"},
			doc:   "Yank main selection into " + c.label,
			run: func(cx *Context, _ []string) error {
				yankPrimary(cx.Editor, c.reg)

				return nil
			},
		}, typedCommand{
			names: []string{c.prefix + "-yank-join"},
			doc:   "Yank joined selections into " + c.label + ", a separator may be given",
			run: func(cx *Context, args []string) error {
				yankJoined(cx.Editor, joinSeparator(cx.Editor, args), c.reg)

				return nil
			},
		}, typedCommand{
			names: []string{c.prefix + "-paste-after"},
			doc:   "Paste " + c.label + " after selections",
			run: func(cx *Context, _ []string) error {
				paste(cx, c.reg, pasteAfter)

				return nil
			},
		}, typedCommand{
			names: []string{c.prefix + "-paste-before"},
			doc:   "Paste " + c.label + " before selections",
			run: func(cx *Context, _ []string) error {
				paste(cx, c.reg, pasteBefore)

				return nil
			},
		}, typedCommand{
			names: []string{c.prefix + "-paste-replace"},
			doc:   "Replace selections with content of " + c.label,
			run: func(cx *Context, _ []string) error {
				replaceWithRegister(cx, c.reg)

				return nil
			},
		})
	}

	typedCommands = append(typedCommands, typedCommand{
		names: []string{"yank-join"},
		doc:   "Yank joined selections, a separator may be given",
		run: func(cx *Context, args []string) error {
			yankJoined(cx.Editor, joinSeparator(cx.Editor, args), cx.Register(cx.Editor.yankRegister()))

			return nil
		},
	}, typedCommand{
		names: []string{"show-clipboard-provider"},
		doc:   "Show clipboard provider name in status bar",
		run: func(cx *Context, _ []string) error {
			cx.Editor.SetStatus("clipboard provider: %s", cx.Editor.Clipboard().Name())

			return nil
		},
	})
}

// joinSeparator is the separator of the yank-join commands: the
// arguments, or the document line ending without them.
func joinSeparator(e *Editor, args []string) string {
	if len(args) == 0 {
		return string(e.Doc().LineEnding())
	}

	return strings.Join(args, " ")
}
