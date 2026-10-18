package editor

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

func init() {
	register("base64_encode", "Encode the selections as base64", func(cx *Context) {
		mapFragmentsErr(cx, func(s string) (string, error) { return base64.StdEncoding.EncodeToString([]byte(s)), nil })
	})
	register("base64_decode", "Decode base64 selections into text", func(cx *Context) { mapFragmentsErr(cx, decodeBase64) })
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"base64-encode", "b64e"},
		doc:   "Encode the selections as base64, for the data of a Kubernetes secret",
		run: func(cx *Context, _ []string) error {
			cx.Editor.execute(cx, "base64_encode")

			return nil
		},
	}, typedCommand{
		names: []string{"base64-decode", "b64d"},
		doc:   "Decode base64 selections into text, nothing changes when one is not base64",
		run: func(cx *Context, _ []string) error {
			cx.Editor.execute(cx, "base64_decode")

			return nil
		},
	})
}

// decodeBase64 reads standard or URL base64, padded or not. Bytes that
// are not text stay encoded, a buffer is no place for them.
func decodeBase64(s string) (string, error) {
	trimmed := strings.Join(strings.Fields(s), "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		data, err := enc.DecodeString(trimmed)
		if err != nil {
			continue
		}

		if !utf8.Valid(data) {
			return "", fmt.Errorf("%q decodes to binary data", firstLines(s, 1))
		}

		return string(data), nil
	}

	return "", fmt.Errorf("%q is not base64", firstLines(s, 1))
}

// mapFragmentsErr replaces every selected fragment with f(fragment), or
// nothing at all when f fails for one of them.
func mapFragmentsErr(cx *Context, f func(string) (string, error)) {
	doc := cx.Editor.Doc()
	text := doc.Text()
	ranges := doc.Selection().Ranges()
	out := make([]string, len(ranges))
	for i, r := range ranges {
		var err error
		if out[i], err = f(r.Fragment(text)); err != nil {
			cx.Editor.SetError("%v", err)

			return
		}
	}

	i := 0
	tx := edit.ChangeBySelection(text, doc.Selection(), func(r edit.Range) edit.Change {
		c := edit.Change{From: r.From(), To: r.To(), Text: out[i]}
		i++

		return c
	})
	doc.Apply(&tx)
	exitSelectMode(cx)
}
