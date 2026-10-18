package host

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicModeAndSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fresh := filepath.Join(dir, "new.txt")
	if err := writeAtomic(fresh, []byte("a")); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(fresh); err != nil || info.Mode().Perm()&0o044 == 0 {
		t.Fatalf("new file mode %v %v", info.Mode(), err)
	}

	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unsupported")
	}

	if err := writeAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}

	data, _ := os.ReadFile(target)
	info, _ := os.Stat(target)
	if string(data) != "new" || info.Mode().Perm() != 0o600 {
		t.Fatalf("target %q mode %v", data, info.Mode())
	}
}

// TestWriteAtomicKeepsHardLinks writes a hard linked file in place so
// every name sees the new text.
func TestWriteAtomicKeepsHardLinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(file, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(file, link); err != nil {
		t.Skipf("hard links: %v", err)
	}

	if err := writeAtomic(file, []byte("new")); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(link); string(got) != "new" {
		t.Fatalf("link reads %q", got)
	}
}

// TestReadDirFollowsDirLinks lists a link to a directory as a directory,
// as Capistrano's current, and a link to a file or a dangling link as
// it is.
func TestReadDirFollowsDirLinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "release"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("release", filepath.Join(dir, "current")); err != nil {
		t.Skip("symlinks unsupported")
	}

	_ = os.Symlink("file", filepath.Join(dir, "filelink"))
	_ = os.Symlink("missing", filepath.Join(dir, "dangling"))
	entries, err := Local{}.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Name] = entry.IsDir()
	}

	want := map[string]bool{"release": true, "file": false, "current": true, "filelink": false, "dangling": false}
	if !maps.Equal(got, want) {
		t.Fatalf("dirs %v, want %v", got, want)
	}

	links := map[string]string{}
	for _, entry := range entries {
		links[entry.Name] = entry.Link
	}

	wantLinks := map[string]string{"release": "", "file": "", "current": "release", "filelink": "file", "dangling": "missing"}
	if !maps.Equal(links, wantLinks) {
		t.Fatalf("links %v, want %v", links, wantLinks)
	}
}

// TestWriteAtomicRefusesReadOnly keeps a file the user may not write,
// the rename would replace it otherwise.
func TestWriteAtomicRefusesReadOnly(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root writes anything")
	}

	path := filepath.Join(t.TempDir(), "ro.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o444); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomic(path, []byte("new")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("write gave %v", err)
	}

	if data, _ := os.ReadFile(path); string(data) != "keep" {
		t.Fatalf("file now %q", data)
	}
}
