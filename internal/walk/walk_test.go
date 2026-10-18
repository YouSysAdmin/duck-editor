package walk

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestIgnorePatterns(t *testing.T) {
	t.Parallel()

	m := ParseIgnore(`
# comment
*.log
/build
docs/*.md
!docs/keep.md
node_modules/
**/tmp
`, "/repo")
	cases := map[string]bool{
		"a.log":              true,
		"src/b.log":          true,
		"build":              true,
		"src/build":          false,
		"docs/x.md":          true,
		"docs/keep.md":       false,
		"src/docs/x.md":      false,
		"tmp":                true,
		"a/b/tmp":            true,
		"main.go":            false,
		"node_modules/x.js":  false,
		"src/node_modules":   false,
		"other_modules/x.js": false,
	}
	for rel, want := range cases {
		got, _ := m.Match(rel, false)
		if got != want {
			t.Errorf("Match(%q) = %v, want %v", rel, got, want)
		}
	}

	if got, _ := m.Match("node_modules", true); !got {
		t.Error("directory only rule should match directories")
	}

	if got, _ := m.Match("src/node_modules", true); !got {
		t.Error("unanchored directory rule should match nested directories")
	}
}

func TestFilesWalk(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "*.log\nbuild/\n")
	write(".git/HEAD", "ref")
	write(".git/info/exclude", "secret.txt\n")
	write("main.go", "")
	write("a.log", "")
	write("build/out.bin", "")
	write("secret.txt", "")
	write(".hidden/x", "")
	write("sub/.gitignore", "*.tmp\n")
	write("sub/keep.txt", "")
	write("sub/drop.tmp", "")
	write("sub/deep/z.go", "")

	var got []string
	err := Files(context.Background(), root, DefaultOptions(), func(rel string) bool {
		got = append(got, rel)

		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"main.go", "sub/deep/z.go", "sub/keep.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}

	// Walking from a subdirectory still honors the parent ignore file.
	got = nil
	write("sub/x.log", "")
	err = Files(context.Background(), filepath.Join(root, "sub"), DefaultOptions(), func(rel string) bool {
		got = append(got, rel)

		return true
	})
	if err != nil || slices.Contains(got, "x.log") {
		t.Fatalf("parent ignore not applied: %v %v", got, err)
	}

	// Stopping early.
	count := 0
	_ = Files(context.Background(), root, DefaultOptions(), func(string) bool {
		count++

		return false
	})
	if count != 1 {
		t.Fatalf("early stop visited %d", count)
	}
}

func TestIgnoreEdgeCases(t *testing.T) {
	t.Parallel()

	// An invalid class is skipped instead of panicking.
	m := ParseIgnore("[z-a]\nfé[é]x\n", "/repo")
	if got, _ := m.Match("féé x", false); got {
		t.Error("space should not match")
	}

	if got, _ := m.Match("fééx", false); !got {
		t.Error("class after a multibyte rune should match")
	}

	if _, ok := relTo("/repo", "/repo/..env"); !ok {
		t.Error("a name starting with two dots is below the base")
	}

	if _, ok := relTo("/repo", "/other"); ok {
		t.Error("a sibling is not below the base")
	}
}
