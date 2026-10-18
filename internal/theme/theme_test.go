package theme

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestParseStyles(t *testing.T) {
	t.Parallel()

	th, err := Parse("test", `
ui.text: text
ui.selection: { bg: surface }
keyword: { fg: "#ff0000", modifiers: [bold, italic] }
keyword.control: blue
markup.link: { fg: text, underline: { color: surface, style: curl } }
bad: { fg: nope }

palette:
  text: "#cdd6f4"
  surface: "#313244"
`)
	if err != nil {
		t.Fatal(err)
	}

	if len(th.Warnings) != 1 {
		t.Fatalf("warnings = %v", th.Warnings)
	}

	text := th.Get("ui.text")
	if text.Fg != (color.RGBA{R: 0xcd, G: 0xd6, B: 0xf4, A: 0xff}) {
		t.Fatalf("ui.text fg = %v", text.Fg)
	}

	kw := th.Get("keyword")
	if !kw.Has(ModBold) || !kw.Has(ModItalic) || kw.Fg != (color.RGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("keyword = %+v", kw)
	}

	if th.Get("keyword.control").Fg != Indexed(4) {
		t.Fatal("ansi name")
	}

	// Fallback through dot separated scopes.
	if got := th.Get("keyword.control.import"); got.Fg != Indexed(4) {
		t.Fatalf("fallback = %+v", got)
	}

	if got := th.Get("keyword.function"); !got.Has(ModBold) {
		t.Fatalf("fallback to keyword = %+v", got)
	}

	if _, ok := th.TryGet("comment"); ok {
		t.Fatal("comment should be missing")
	}

	link := th.Get("markup.link")
	if !link.Has(ModUnderlined) || link.UnderlineStyle != "curl" {
		t.Fatalf("underline = %+v", link)
	}
}

func TestPatch(t *testing.T) {
	t.Parallel()

	base := Style{Fg: Indexed(1), Add: ModBold}
	over := Style{Bg: Indexed(2), Add: ModItalic}
	got := base.Patch(over)
	if got.Fg != Indexed(1) || got.Bg != Indexed(2) || !got.Has(ModBold) || !got.Has(ModItalic) {
		t.Fatalf("patch = %+v", got)
	}
}

func TestBuiltinThemes(t *testing.T) {
	t.Parallel()

	d := Default()
	if len(d.Warnings) != 0 {
		t.Fatalf("default warnings: %v", d.Warnings)
	}

	if _, ok := d.TryGet("ui.selection"); !ok {
		t.Fatal("default theme lacks ui.selection")
	}

	if _, err := NewLoader(nil).Load("ololo"); err == nil {
		t.Fatal("only default is built in")
	}
}

func TestLoaderInherits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}

	child := `inherits: default
ui.text: mine
palette:
  mine: "#010203"
`
	if err := os.WriteFile(filepath.Join(themes, "child.yaml"), []byte(child), 0o644); err != nil {
		t.Fatal(err)
	}

	grand := `inherits: child
keyword: mine
`
	if err := os.WriteFile(filepath.Join(themes, "grand.yaml"), []byte(grand), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader([]string{dir})
	th, err := l.Load("grand")
	if err != nil {
		t.Fatal(err)
	}

	want := color.RGBA{R: 1, G: 2, B: 3, A: 0xff}
	if th.Get("ui.text").Fg != want || th.Get("keyword").Fg != want {
		t.Fatalf("inherited styles: %+v %+v", th.Get("ui.text"), th.Get("keyword"))
	}

	if _, ok := th.TryGet("ui.selection"); !ok {
		t.Fatal("grandparent default styles lost")
	}

	if len(th.Warnings) != 0 {
		t.Fatalf("warnings: %v", th.Warnings)
	}

	if _, err := l.Load("missing"); err == nil {
		t.Fatal("expected error for missing theme")
	}
}

func TestNestedScopes(t *testing.T) {
	t.Parallel()

	th, err := Parse("nested", `
keyword:
  fg: "#0000ff"
  modifiers: [bold]
  control:
    conditional: { fg: "#00ff00", modifiers: [italic] }
markup:
  heading:
    1: "#ff0000"
ui.text: "#010101"
palette: {}
`)
	if err != nil {
		t.Fatal(err)
	}

	if len(th.Warnings) != 0 {
		t.Fatalf("warnings: %v", th.Warnings)
	}

	if kw := th.Get("keyword"); !kw.Has(ModBold) || kw.Fg != (color.RGBA{B: 0xff, A: 0xff}) {
		t.Fatalf("keyword = %+v", kw)
	}

	if c := th.Get("keyword.control.conditional"); !c.Has(ModItalic) || c.Fg != (color.RGBA{G: 0xff, A: 0xff}) {
		t.Fatalf("conditional = %+v", c)
	}

	if h := th.Get("markup.heading.1"); h.Fg != (color.RGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("numeric child = %+v", h)
	}

	if th.Get("ui.text").Fg != (color.RGBA{R: 1, G: 1, B: 1, A: 0xff}) {
		t.Fatal("dotted key")
	}
}

func TestRainbowStyles(t *testing.T) {
	t.Parallel()

	th, err := Parse("r", "rainbow: [\"#ff0000\", \"#00ff00\"]\nui.text: \"#ffffff\"\n")
	if err != nil || len(th.Warnings) != 0 {
		t.Fatalf("err %v warnings %v", err, th.Warnings)
	}

	if th.RainbowCount() != 2 || th.Get("rainbow.1").Fg != (color.RGBA{G: 0xff, A: 0xff}) {
		t.Fatalf("rainbow = %d %+v", th.RainbowCount(), th.Get("rainbow.1"))
	}

	if Default().RainbowCount() != 6 {
		t.Fatalf("default rainbow count = %d", Default().RainbowCount())
	}
}
