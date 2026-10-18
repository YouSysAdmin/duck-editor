package edit

import (
	"reflect"
	"testing"
)

func TestSnippetRender(t *testing.T) {
	t.Parallel()

	cases := []struct {
		src  string
		opts RenderOptions
		text string
		want []Tabstop
	}{
		{"foo", RenderOptions{}, "foo", []Tabstop{{0, [][2]int{{3, 3}}}}},
		{"foo($1, $2)$0", RenderOptions{}, "foo(, )", []Tabstop{{1, [][2]int{{4, 4}}}, {2, [][2]int{{6, 6}}}, {0, [][2]int{{7, 7}}}}},
		{"${1:name} = ${2:value}", RenderOptions{}, "name = value", []Tabstop{{1, [][2]int{{0, 4}}}, {2, [][2]int{{7, 12}}}, {0, [][2]int{{12, 12}}}}},
		{"${1|one,two|}", RenderOptions{}, "one", []Tabstop{{1, [][2]int{{0, 3}}}, {0, [][2]int{{3, 3}}}}},
		{"$1 and $1", RenderOptions{}, " and ", []Tabstop{{1, [][2]int{{0, 0}, {5, 5}}}, {0, [][2]int{{5, 5}}}}},
		{"${1:outer ${2:inner}}", RenderOptions{}, "outer inner", []Tabstop{{1, [][2]int{{0, 11}}}, {2, [][2]int{{6, 11}}}, {0, [][2]int{{11, 11}}}}},
		{"\\$x \\} \\\\", RenderOptions{}, "$x } \\", []Tabstop{{0, [][2]int{{6, 6}}}}},
		{"$TM_FILENAME ${TM_LINE_INDEX} ${UNKNOWN:dflt}", RenderOptions{Var: func(name string) (string, bool) {
			switch name {
			case "TM_FILENAME":
				return "a.go", true
			case "TM_LINE_INDEX":
				return "7", true
			}

			return "", false
		}}, "a.go 7 dflt", []Tabstop{{0, [][2]int{{11, 11}}}}},
		{"if $1 {\n\t$0\n}", RenderOptions{Indent: "  ", IndentUnit: "    "}, "if  {\n      \n  }", []Tabstop{{1, [][2]int{{3, 3}}}, {0, [][2]int{{12, 12}}}}},
		{"${1/upcase/x/} $", RenderOptions{}, " $", []Tabstop{{1, [][2]int{{0, 0}}}, {0, [][2]int{{2, 2}}}}},
		{"${name/(.*)/${1:/upcase}/}", RenderOptions{Var: func(string) (string, bool) { return "val", true }}, "val", []Tabstop{{0, [][2]int{{3, 3}}}}},
	}
	for _, c := range cases {
		got := ParseSnippet(c.src).Render(c.opts)
		if got.Text != c.text {
			t.Errorf("%q text = %q, want %q", c.src, got.Text, c.text)
		}

		if !reflect.DeepEqual(got.Tabstops, c.want) {
			t.Errorf("%q tabstops = %v, want %v", c.src, got.Tabstops, c.want)
		}
	}
}
