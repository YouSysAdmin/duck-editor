package fuzzy

import "testing"

func TestMatchRanking(t *testing.T) {
	t.Parallel()

	p := Parse("edmd")
	a, _, ok := p.Match("internal/editor/model.go")
	if !ok {
		t.Fatal("expected match")
	}

	b, _, ok := p.Match("cmd/ce/main.go")
	if ok && b >= a {
		t.Fatalf("word start match should rank higher: %d vs %d", a, b)
	}

	p = Parse("main")
	exact, _, _ := p.Match("cmd/ce/main.go")
	spread, _, ok := p.Match("manifest/api/index.go")
	if !ok || spread >= exact {
		t.Fatalf("consecutive match should win: %d vs %d", exact, spread)
	}

	if _, _, ok := Parse("zzz").Match("abc"); ok {
		t.Fatal("no match expected")
	}
}

func TestSmartCaseAndPositions(t *testing.T) {
	t.Parallel()

	_, pos, ok := Parse("rd").Match("README.md")
	if !ok || len(pos) != 2 || pos[0] != 0 {
		t.Fatalf("smart case lower = %v %v", pos, ok)
	}

	if _, _, ok := Parse("RD").Match("readme.md"); ok {
		t.Fatal("uppercase pattern must be case sensitive")
	}

	_, pos, ok = Parse("é").Match("café.txt")
	if !ok || pos[0] != 3 {
		t.Fatalf("utf8 positions = %v %v", pos, ok)
	}
}

func TestTerms(t *testing.T) {
	t.Parallel()

	if _, _, ok := Parse("^cmd go$").Match("cmd/ce/main.go"); !ok {
		t.Fatal("prefix and suffix")
	}

	if _, _, ok := Parse("^ce").Match("cmd/ce/main.go"); ok {
		t.Fatal("prefix should fail")
	}

	if _, _, ok := Parse("'ce/ma").Match("cmd/ce/main.go"); !ok {
		t.Fatal("exact substring")
	}

	if _, _, ok := Parse("'cema").Match("cmd/ce/main.go"); ok {
		t.Fatal("exact substring should not fuzz")
	}

	if _, _, ok := Parse("go !test").Match("foo_test.go"); ok {
		t.Fatal("negation should exclude")
	}

	if _, _, ok := Parse("go !test").Match("foo.go"); !ok {
		t.Fatal("negation should keep")
	}

	if !Parse("   ").IsEmpty() {
		t.Fatal("blank pattern is empty")
	}
}

func TestFoldingChangesByteLength(t *testing.T) {
	t.Parallel()

	// U+023A is two bytes and lowercases to three.
	for _, q := range []string{"x", "'ⱥx", "^ⱥ", "x$", "ⱥⱥ"} {
		_, pos, ok := Parse(q).Match("ȺȺȺȺx")
		if !ok {
			t.Fatalf("%q should match", q)
		}

		for _, p := range pos {
			if p < 0 || p >= len("ȺȺȺȺx") {
				t.Fatalf("%q position %d out of range", q, p)
			}
		}
	}

	if _, pos, _ := Parse("x").Match("ȺȺȺȺx"); len(pos) != 1 || pos[0] != 8 {
		t.Fatalf("positions = %v", pos)
	}
}

// TestMatchTight tells runs that start words from letters strewn over
// a path, and finds a whole occurrence after a scattered one.
func TestMatchTight(t *testing.T) {
	t.Parallel()

	cases := []struct {
		query, text string
		tight       bool
	}{
		{"domain", "internal/domain/user.go", true},
		{"domain", "internal/models/smtpserver/alloweddomains_test.go", true},
		{"domain", "docs/themes/mailyard/assets/icons/database.svg", false},
		{"fpick", "internal/file_picker.go", true},
		{"intdom", "internal/domain/user.go", true},
		{"gc", "GoToConfig", true},
		{"dmain", "domain.go", false},
	}
	for _, c := range cases {
		_, _, tight, ok := Parse(c.query).MatchTight(c.text)
		if !ok || tight != c.tight {
			t.Errorf("%q in %q: tight %v ok %v, want tight %v", c.query, c.text, tight, ok, c.tight)
		}
	}

	// A scattered subsequence comes first, the whole word later.
	_, pos, tight, ok := Parse("domain").MatchTight("do_mail_in_x/domain")
	if !ok || !tight || pos[0] != len("do_mail_in_x/") {
		t.Fatalf("whole occurrence not preferred: %v %v", pos, tight)
	}
}
