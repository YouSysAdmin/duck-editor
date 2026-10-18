package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCatalogueStyle keeps comments, spaces the entries of the servers and
// languages tables, writes a too wide flow list in block style, and leaves
// its own output as it is.
func TestCatalogueStyle(t *testing.T) {
	t.Parallel()

	in := `# Banner
language-servers:
  # Zed server
  # https://example.com/zed
  zed:
    command: zed
  # Alpha server
  alpha:
    command: alpha
languages:
  # Alpha language
  alpha:
    file-types: [aaaaaaaaaaa, bbbbbbbbbbbb, cccccccccccc, dddddddddddd, eeeeeeeeeeee, ffff]
    language-servers: [alpha]
`
	want := `# Banner
language-servers:
  # Alpha server
  alpha:
    command: alpha

  # Zed server
  # https://example.com/zed
  zed:
    command: zed

languages:
  # Alpha language
  alpha:
    file-types:
      - aaaaaaaaaaa
      - bbbbbbbbbbbb
      - cccccccccccc
      - dddddddddddd
      - eeeeeeeeeeee
      - ffff
    language-servers: [alpha]
`
	path := filepath.Join(t.TempDir(), "languages.yaml")
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := run(path, ""); err != nil {
			t.Fatal(err)
		}

		got, _ := os.ReadFile(path)
		if string(got) != want {
			t.Fatalf("catalogue:\n%s\nwant:\n%s", got, want)
		}
	}
}
