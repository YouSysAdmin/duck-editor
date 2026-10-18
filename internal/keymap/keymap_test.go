package keymap

import "testing"

func TestParseKey(t *testing.T) {
	t.Parallel()

	cases := map[string]Key{
		"a":        Char('a'),
		"A":        Char('A'),
		"S-a":      Char('A'),
		"C-x":      Ctrl('x'),
		"A-ret":    {Code: KeyEnter, Mods: ModAlt},
		"C-A-del":  {Code: KeyDelete, Mods: ModAlt | ModCtrl},
		"space":    Char(' '),
		"minus":    Char('-'),
		"-":        Char('-'),
		"A-minus":  Alt('-'),
		"F12":      {Code: KeyF(12)},
		"esc":      {Code: KeyEsc},
		"S-tab":    {Code: KeyTab, Mods: ModShift},
		"Meta-x":   {Code: 'x', Mods: ModSuper},
		"ü":        Char('ü'),
		"A-S-down": {Code: KeyDown, Mods: ModAlt | ModShift},
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v, want %v", in, got, err, want)
		}
	}

	for _, bad := range []string{"", "C-", "X-a", "C-C-a", "F99", "foo"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}

	for _, s := range []string{"C-x", "A-ret", "space", "F3", "a", "S-tab"} {
		if got := MustParse(s).String(); got != s {
			t.Errorf("round trip %q = %q", s, got)
		}
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

	km := New(Default())

	res := km.Get(Normal, Char('h'))
	if res.Kind != Matched || res.Command != "move_char_left" {
		t.Fatalf("h = %+v", res)
	}

	res = km.Get(Normal, Char('g'))
	if res.Kind != Pending || res.Node.Name != "Goto" {
		t.Fatalf("g = %+v", res)
	}

	res = km.Get(Normal, Char('e'))
	if res.Kind != Matched || res.Command != "goto_last_line" || len(km.Pending()) != 0 {
		t.Fatalf("ge = %+v pending %v", res, km.Pending())
	}

	km.Get(Normal, Char('g'))
	res = km.Get(Normal, Char('!'))
	if res.Kind != Cancelled || len(res.Keys) != 2 {
		t.Fatalf("g! = %+v", res)
	}

	km.Get(Normal, Char('g'))
	res = km.Get(Normal, Key{Code: KeyEsc})
	if res.Kind != Cancelled || len(res.Keys) != 1 {
		t.Fatalf("g esc = %+v", res)
	}

	if res := km.Get(Normal, Char('ß')); res.Kind != NotFound {
		t.Fatalf("unbound = %+v", res)
	}

	if res := km.Get(Select, Char('w')); res.Command != "extend_next_word_start" {
		t.Fatalf("select w = %+v", res)
	}

	if res := km.Get(Select, Char('d')); res.Command != "delete_selection" {
		t.Fatalf("select d inherits normal: %+v", res)
	}

	if res := km.Get(Normal, Char('w')); res.Command != "move_next_word_start" {
		t.Fatalf("normal w must stay unchanged: %+v", res)
	}
}

func TestSticky(t *testing.T) {
	t.Parallel()

	km := New(Default())
	res := km.Get(Normal, Char('Z'))
	if res.Kind != Pending || km.Sticky() == nil {
		t.Fatalf("Z = %+v", res)
	}

	if res := km.Get(Normal, Char('j')); res.Command != "scroll_down" {
		t.Fatalf("Zj = %+v", res)
	}

	if res := km.Get(Normal, Char('j')); res.Command != "scroll_down" {
		t.Fatalf("sticky repeat = %+v", res)
	}

	km.Get(Normal, Key{Code: KeyEsc})
	if km.Sticky() != nil {
		t.Fatal("esc should clear sticky")
	}
}

func TestUserMerge(t *testing.T) {
	t.Parallel()

	user, err := FromValue(map[string]any{
		"C-s": "save",
		"g":   map[string]any{"q": "goto_quit"},
		"x":   []any{"extend_line_below", "yank"},
	})
	if err != nil {
		t.Fatal(err)
	}

	defaults := Default()
	MergeUser(defaults, map[Mode]*Trie{Normal: user})
	km := New(defaults)

	if res := km.Get(Normal, Ctrl('s')); res.Command != "save" {
		t.Fatalf("override = %+v", res)
	}

	km.Get(Normal, Char('g'))
	if res := km.Get(Normal, Char('q')); res.Command != "goto_quit" {
		t.Fatalf("nested add = %+v", res)
	}

	km.Get(Normal, Char('g'))
	if res := km.Get(Normal, Char('g')); res.Command != "goto_file_start" {
		t.Fatalf("nested keep = %+v", res)
	}

	if res := km.Get(Normal, Char('x')); res.Kind != MatchedSequence || len(res.Sequence) != 2 {
		t.Fatalf("sequence = %+v", res)
	}
}

func TestParseMacro(t *testing.T) {
	t.Parallel()

	keys, err := ParseMacro("ihi<esc><C-w>3-<ret>")
	if err != nil {
		t.Fatal(err)
	}

	want := []Key{Char('i'), Char('h'), Char('i'), {Code: KeyEsc}, Ctrl('w'), Char('3'), Char('-'), {Code: KeyEnter}}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d", len(keys), len(want))
	}

	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("key %d = %v, want %v", i, keys[i], want[i])
		}
	}

	if _, err := ParseMacro("a<C-x"); err == nil {
		t.Error("expected error for unterminated group")
	}
}
