package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownBlocks(t *testing.T) {
	t.Parallel()

	text := "```go\nfunc Foo() **x**\n```\n\n\n# Title\n\nSome *text* here.\n\n- one\n- [x] done\n1. first\n\n> quoted\n\n---\n\nSetext\n---\n\nend\n\n"
	got := Markdown(text)
	want := []string{"func Foo() **x**", "", "Title", "", "Some text here.", "", "• one", "• [x] done", "1. first", "", "│ quoted", "", "", "", "Setext", "", "end"}
	if !slices.Equal(Strings(got), want) {
		t.Fatalf("lines\n%q\nwant\n%q", Strings(got), want)
	}

	scopes := map[int]string{0: scopeCodeBlock, 2: scopeHeading, 6: scopeList, 10: scopeQuote, 14: scopeHeading}
	for i, scope := range scopes {
		if got[i].Runs[0].Scope != scope {
			t.Errorf("line %d %q scope %q, want %q", i, got[i].String(), got[i].Runs[0].Scope, scope)
		}
	}

	if !got[12].Rule || got[13].Rule || got[7].Runs[1].Scope != scopeChecked {
		t.Fatalf("rule or task box missing: %+v", got)
	}

	if italic := got[4].Runs[1]; italic.Text != "text" || italic.Scope != scopeItalic {
		t.Fatalf("emphasis %+v", got[4].Runs)
	}
}

func TestMarkdownInline(t *testing.T) {
	t.Parallel()

	for in, want := range map[string][]Run{
		"deploy\\_to and keep\\_releases \\*":           {{Text: "deploy_to and keep_releases *"}},
		"a `code` b":                                    {{Text: "a "}, {Text: "code", Scope: scopeCode}, {Text: " b"}},
		"`` a`b `` x":                                   {{Text: "a`b", Scope: scopeCode}, {Text: " x"}},
		"**bold** and __b__ and *i* and _i_":            {{Text: "bold", Scope: scopeBold}, {Text: " and "}, {Text: "b", Scope: scopeBold}, {Text: " and "}, {Text: "i", Scope: scopeItalic}, {Text: " and "}, {Text: "i", Scope: scopeItalic}},
		"snake_case_name and 2 * 3 * 4":                 {{Text: "snake_case_name and 2 * 3 * 4"}},
		"see [the docs](https://x.y) and <https://a.b>": {{Text: "see "}, {Text: "the docs", Scope: scopeLinkText}, {Text: " and "}, {Text: "https://a.b", Scope: scopeLinkURL}},
		"![alt](img.png) [ref][1]":                      {{Text: "alt", Scope: scopeLinkText}, {Text: " "}, {Text: "ref", Scope: scopeLinkText}},
		"Vec<T> &lt;x&gt; &amp; a[0]":                   {{Text: "Vec<T> <x> & a[0]"}},
		"**`code` in bold**":                            {{Text: "code", Scope: scopeCode}, {Text: " in bold", Scope: scopeBold}},
		"~~gone~~ *unclosed":                            {{Text: "gone", Scope: scopeStrike}, {Text: " *unclosed"}},
	} {
		if got := inline(in); !slices.Equal(got, want) {
			t.Errorf("inline(%q)\n got %+v\nwant %+v", in, got, want)
		}
	}
}

func TestWrapLinesKeepsRuns(t *testing.T) {
	t.Parallel()

	line := Line{Runs: []Run{{Text: "one two "}, {Text: "three four", Scope: scopeBold}, {Text: "\tfive"}}}
	got := wrapLines([]Line{line, {Rule: true}}, 9)
	want := []string{"one two", "three", "four", "five", ""}
	if !slices.Equal(Strings(got), want) {
		t.Fatalf("wrapped %q, want %q", Strings(got), want)
	}

	if got[1].Runs[0].Scope != scopeBold || got[2].Runs[0].Scope != scopeBold || got[3].Runs[0].Scope != "" || !got[4].Rule {
		t.Fatalf("runs %+v", got)
	}

	// A word longer than the line is cut, a wide char on a narrow line
	// does not hang.
	if got := Strings(wrapLines([]Line{Plain("abcdefghij")}, 4)); !slices.Equal(got, []string{"abcd", "efgh", "ij"}) {
		t.Fatalf("cut %q", got)
	}

	if got := Strings(wrapLines([]Line{Plain("中文")}, 1)); !slices.Equal(got, []string{"中", "文"}) {
		t.Fatalf("wide %q", got)
	}
}

func TestRenderPopupStylesRuns(t *testing.T) {
	t.Parallel()

	th := DefaultTheme()
	lines := Markdown("# Head\n\n`code` and **bold**")
	out := ansi.Strip(RenderPopup(lines, 0, 20, 3, &th))
	if !strings.Contains(out, "Head") || !strings.Contains(out, "code and bold") {
		t.Fatalf("rendered %q", out)
	}
}
