package termux

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRewrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	prefix := "/data/data/com.termux/files/usr"
	script := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}

		return path
	}

	env := script("env-node", "#!/usr/bin/env node\nconsole.log(1)\n")
	sh := script("plain-sh", "#!/bin/sh\necho ok\n")
	tab := script("tab", "#!/bin/bash\t-e \n")
	local := script("local", "#!"+prefix+"/bin/python\n")
	binary := script("binary", "\x7fELF")
	termuxOnly := func(path string) bool {
		return path == prefix+"/bin/env" || path == prefix+"/bin/sh" || path == prefix+"/bin/bash" || path == prefix+"/bin/python"
	}

	tests := []struct {
		label  string
		name   string
		exists func(string) bool
		want   []string
	}{
		{"env with argument", env, termuxOnly, []string{prefix + "/bin/env", "node", env, "x"}},
		{"plain interpreter", sh, termuxOnly, []string{prefix + "/bin/sh", sh, "x"}},
		{"tab separates the argument", tab, termuxOnly, []string{prefix + "/bin/bash", "-e", tab, "x"}},
		{"interpreter already in the prefix", local, termuxOnly, []string{local, "x"}},
		{"no shebang", binary, termuxOnly, []string{binary, "x"}},
		{"system interpreter exists", sh, func(string) bool { return true }, []string{sh, "x"}},
		{"no copy under the prefix", sh, func(string) bool { return false }, []string{sh, "x"}},
		{"not found", filepath.Join(dir, "missing"), termuxOnly, []string{filepath.Join(dir, "missing"), "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			t.Parallel()

			name, args := rewrite(tt.name, []string{"x"}, prefix, tt.exists)
			if got := append([]string{name}, args...); !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteWithoutPrefix(t *testing.T) {
	t.Parallel()

	name, args := rewrite("sh", []string{"-c", "true"}, "", func(string) bool { return false })
	if name != "sh" || !slices.Equal(args, []string{"-c", "true"}) {
		t.Fatalf("got %q %q", name, args)
	}
}
