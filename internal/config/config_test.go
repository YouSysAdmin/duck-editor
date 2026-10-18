package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

func TestParse(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(`
theme: onedark
editor:
  scrolloff: 8
  line-number: relative
  lsp:
    display-messages: true
keys:
  normal:
    C-s: ":w"
    g: { q: goto_quit }
    space:
      B:
        name: Git blame
        command: ":sh git blame"
  insert:
    j: { k: normal_mode }
`)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Theme != "onedark" || cfg.Editor.Scrolloff != 8 {
		t.Fatalf("theme %q scrolloff %d", cfg.Theme, cfg.Editor.Scrolloff)
	}

	km := keymap.New(cfg.Keys)
	if res := km.Get(keymap.Normal, keymap.Ctrl('s')); res.Command != ":w" {
		t.Fatalf("C-s = %+v", res)
	}

	km.Get(keymap.Insert, keymap.Char('j'))
	if res := km.Get(keymap.Insert, keymap.Char('k')); res.Command != "normal_mode" {
		t.Fatalf("jk = %+v", res)
	}

	if res := km.Get(keymap.Normal, keymap.Char('h')); res.Command != "move_char_left" {
		t.Fatalf("defaults lost: %+v", res)
	}

	res := km.Get(keymap.Normal, keymap.Char(' '))
	named, ok := res.Node.Get(keymap.Char('B'))
	if res.Kind != keymap.Pending || !ok || named.Label != "Git blame" || named.Command != ":sh git blame" {
		t.Fatalf("named binding = %+v", named)
	}

	km.Get(keymap.Normal, keymap.Key{Code: keymap.KeyEsc})
	if _, err := Parse("keys:\n  normal:\n    B: { name: x, command: { y: z } }\n"); err == nil {
		t.Fatal("named binding with a sub keymap must fail")
	}
}

func TestParseDefaultsWhenEmpty(t *testing.T) {
	t.Parallel()

	cfg, err := Parse("")
	if err != nil || cfg.Editor.Scrolloff != 5 {
		t.Fatalf("cfg %+v err %v", cfg.Editor, err)
	}
}

func TestParseRejectsBadKeys(t *testing.T) {
	t.Parallel()

	if _, err := Parse("keys:\n  normal:\n    X-a: foo\n"); err == nil {
		t.Fatal("expected error for bad modifier")
	}

	if _, err := Parse("keys:\n  visual:\n    a: foo\n"); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestAdaptiveTheme(t *testing.T) {
	t.Parallel()

	cfg, err := Parse("theme: { light: l, dark: d }\n")
	if err != nil || cfg.Theme != "d" {
		t.Fatalf("theme %q err %v", cfg.Theme, err)
	}
}

func TestWorkspaceOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	ws := filepath.Join(dir, "ws.yaml")
	if err := os.WriteFile(user, []byte("theme: one\neditor:\n  scrolloff: 9\n  cursorline: true\nkeys:\n  normal:\n    C-s: \":w\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(ws, []byte("editor:\n  scrolloff: 2\nkeys:\n  normal:\n    C-q: \":q\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFiles(user, ws, filepath.Join(dir, "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// The workspace file overrides only what it mentions.
	if cfg.Theme != "one" || cfg.Editor.Scrolloff != 2 || !cfg.Editor.Cursorline {
		t.Fatalf("merged = theme %q scrolloff %d cursorline %v", cfg.Theme, cfg.Editor.Scrolloff, cfg.Editor.Cursorline)
	}

	km := keymap.New(cfg.Keys)
	if km.Get(keymap.Normal, keymap.Ctrl('s')).Command != ":w" || km.Get(keymap.Normal, keymap.Ctrl('q')).Command != ":q" {
		t.Fatal("key bindings of both files must apply")
	}
}

func TestUnknownConfigKeys(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"editor:\n  true-color: true\n",
		"editor:\n  kitty-keyboard-protocol: auto\n",
		"editor:\n  file-picker:\n    parents: false\n",
		"bogus: 1\n",
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%q should be rejected", text)
		}
	}

	if _, err := Parse(""); err != nil {
		t.Errorf("an empty file is fine: %v", err)
	}
}

// TestValidate accepts the defaults and names the option of a bad value.
func TestValidate(t *testing.T) {
	t.Parallel()

	if err := Default().Editor.Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}

	bad := Default().Editor
	bad.LineNumber = "garbage"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "line-number") {
		t.Fatalf("bad line-number: %v", err)
	}

	bad = Default().Editor
	bad.Scrolloff = -5
	if err := bad.Validate(); err == nil {
		t.Fatal("negative scrolloff passed")
	}
}
