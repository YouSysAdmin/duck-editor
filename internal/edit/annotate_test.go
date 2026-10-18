package edit

import (
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

func TestParseAnnotated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		text string
		sel  Selection
	}{
		{"#[|h]#ello", "hello", SingleSelection(1, 0)},
		{"#[h|]#ello", "hello", SingleSelection(0, 1)},
		{"#[|hell]#o", "hello", SingleSelection(4, 0)},
		{"#[hell|]#o", "hello", SingleSelection(0, 4)},
		{"#[a|]#b#(|c)#", "abc", NewSelection([]Range{NewRange(0, 1), NewRange(3, 2)}, 0)},
		{"a#[|\n]#\nb", "a\nb", SingleSelection(2, 1)},
		{"a#[\r\n|]#\r\nb", "a\r\nb", SingleSelection(1, 3)},
		{"x # y #[|z]#", "x # y z", SingleSelection(7, 6)},
	}
	for _, c := range cases {
		text, sel, err := ParseAnnotated(c.in)
		if err != nil {
			t.Errorf("parse %q: %v", c.in, err)

			continue
		}

		if text != c.text || !sel.Equal(c.sel) {
			t.Errorf("parse %q = %q %v, want %q %v", c.in, text, sel.Ranges(), c.text, c.sel.Ranges())
		}
	}

	for _, bad := range []string{"hello", "#[|hello", "#[a|]# #[b|]#"} {
		if _, _, err := ParseAnnotated(bad); err == nil {
			t.Errorf("parse %q should fail", bad)
		}
	}
}

func TestAnnotateRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []string{
		"#[a|]#b#(|c)#",
		"#[|h]#ello",
		"he#(l|)#lo wor#[|ld]#",
		"#[|日本]#語",
	}
	for _, c := range cases {
		text, sel := MustParseAnnotated(c)
		if got := Annotate(buffer.NewRope(text), sel); got != c {
			t.Errorf("round trip %q = %q", c, got)
		}
	}
}
