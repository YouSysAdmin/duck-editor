package walk

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDirFilter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, d := range []string{".git", "sub/build"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]string{
		".gitignore":     "*.log\n",
		"sub/.gitignore": "build/\n!keep.log\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	exists := func(paths []string) []bool {
		out := make([]bool, len(paths))
		for i, p := range paths {
			_, err := os.Stat(p)
			out[i] = err == nil
		}

		return out
	}
	f := NewDirFilter(filepath.Join(root, "sub"), DefaultOptions(), exists, os.ReadFile)
	cases := map[string]bool{"a.log": true, "keep.log": false, "build": true, "main.go": false, ".env": true}
	for name, want := range cases {
		if got := f.Skip(name, name == "build"); got != want {
			t.Errorf("Skip(%q) = %v want %v", name, got, want)
		}
	}

	all := NewDirFilter(filepath.Join(root, "sub"), Options{}, exists, os.ReadFile)
	if all.Skip("a.log", false) || all.Skip(".env", false) {
		t.Error("no options should skip nothing but .git")
	}
}

// TestDirFilterMatchesFiles hides in one directory what a walk from it
// leaves out, also outside a repository.
func TestDirFilterMatchesFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{".gitignore": "*.log\n", ".ignore": "secret*\n", "a.log": "", "secret.txt": "", "keep.go": "", ".hidden": ""}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	walked := map[string]bool{}
	if err := Files(context.Background(), root, DefaultOptions(), func(rel string) bool {
		walked[rel] = true

		return true
	}); err != nil {
		t.Fatal(err)
	}

	exists := func(paths []string) []bool {
		out := make([]bool, len(paths))
		for i, p := range paths {
			_, err := os.Stat(p)
			out[i] = err == nil
		}

		return out
	}
	f := NewDirFilter(root, DefaultOptions(), exists, os.ReadFile)
	for name := range files {
		if shown := !f.Skip(name, false); shown != walked[name] {
			t.Errorf("%s: explorer shows %v, the walk lists %v", name, shown, walked[name])
		}
	}
}

// TestInsideIgnoredDir lists a directory that a parent rule ignores when
// the walk or the explorer starts inside it.
func TestInsideIgnoredDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, d := range []string{".git", "build/sub", "out/sub"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]string{".gitignore": "build\nout/\n*.log\n", "build/main.go": "", "build/a.log": "", "build/sub/x.go": "", "out/main.go": ""}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var walked []string
	if err := Files(context.Background(), filepath.Join(root, "build"), DefaultOptions(), func(rel string) bool {
		walked = append(walked, rel)

		return true
	}); err != nil {
		t.Fatal(err)
	}

	if want := []string{"main.go", "sub/x.go"}; !slices.Equal(walked, want) {
		t.Errorf("walk inside an ignored directory = %v want %v", walked, want)
	}

	exists := func(paths []string) []bool {
		out := make([]bool, len(paths))
		for i, p := range paths {
			_, err := os.Stat(p)
			out[i] = err == nil
		}

		return out
	}
	for _, dir := range []string{"build", "out"} {
		f := NewDirFilter(filepath.Join(root, dir), DefaultOptions(), exists, os.ReadFile)
		if f.Skip("main.go", false) || f.Skip("sub", true) {
			t.Errorf("%s: entries of the opened directory are hidden", dir)
		}

		if !f.Skip("a.log", false) {
			t.Errorf("%s: other rules still apply", dir)
		}
	}
}
