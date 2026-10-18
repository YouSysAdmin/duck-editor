package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSops stands in for sops: values are kept as ENC[AES256_GCM,data:V]
// and edit mode runs EDITOR on the decrypted text, as sops does.
const fakeSops = `#!/bin/sh
decrypt() { grep -v '^sops:' "$1" | grep -v '^  mac:' | sed 's/ENC\[AES256_GCM,data:\([^]]*\)\]/\1/'; }
if [ "$1" = --decrypt ]; then decrypt "$3"; exit; fi
f=$1
t=$(mktemp)
decrypt "$f" > "$t"
cp "$t" "$t.before"
$EDITOR "$t"
if cmp -s "$t" "$t.before"; then exit 200; fi
sed 's/^\([a-z]*\): \(.*\)$/\1: ENC[AES256_GCM,data:\2]/' "$t" > "$f"
printf 'sops:\n  mac: ENC[AES256_GCM,data:mac]\n' >> "$f"
`

// TestSopsSecret decrypts an encrypted file into a read only buffer that
// opens up once decrypted, encrypts it again on save and keeps the clear
// text out of the undo files and the crash recovery.
func TestSopsSecret(t *testing.T) {
	bin, dir, state := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sops"), []byte(fakeSops), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(dir, "secrets.yaml")
	encrypted := "password: ENC[AES256_GCM,data:hunter2]\nsops:\n  mac: ENC[AES256_GCM,data:mac]\n"
	if err := os.WriteFile(path, []byte(encrypted), 0o600); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	e.SetStateDir(state)
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	if doc.secret == nil || !doc.ReadOnly() {
		t.Fatalf("encrypted file not taken as a secret, read only %v", doc.ReadOnly())
	}

	runJobs(e)
	if got := doc.Text().String(); got != "password: hunter2\n" || doc.ReadOnly() || doc.IsModified() {
		t.Fatalf("decrypted %q, read only %v, modified %v, status %+v", got, doc.ReadOnly(), doc.IsModified(), e.Status())
	}

	// Undo goes back to the decrypted text, not to the encrypted one.
	replaceByLines(doc, "password: oops\n")
	doc.AppendChangesToHistory()
	e.feed(t, "u")
	if got := doc.Text().String(); got != "password: hunter2\n" {
		t.Fatalf("undo gave %q", got)
	}

	replaceByLines(doc, "password: s3cret\n")
	if saved, _ := e.SaveRecovery(t.TempDir()); len(saved) != 0 {
		t.Fatalf("crash recovery wrote a secret: %v", saved)
	}

	e.runTyped(&Context{Editor: e}, "w")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "data:s3cret]") || strings.Contains(string(data), "password: s3cret\n") || doc.IsModified() {
		t.Fatalf("saved %q, modified %v, status %+v", data, doc.IsModified(), e.Status())
	}

	if entries, _ := os.ReadDir(filepath.Join(state, "undo")); len(entries) != 0 {
		t.Fatalf("undo history of a secret written: %d files", len(entries))
	}

	// A save without changes is fine, sops reports it with 200.
	e.runTyped(&Context{Editor: e}, "w")
	if e.Status().IsError {
		t.Fatalf("unchanged save: %+v", e.Status())
	}
}

func TestSecretTool(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"$ANSIBLE_VAULT;1.1;AES256\n6136...":                        "ansible-vault",
		"a: ENC[AES256_GCM,data:x]\nsops:\n  mac: x\n":              "sops",
		`{"a": "ENC[AES256_GCM,data:x]", "sops": {}}`:               "sops",
		"A=ENC[AES256_GCM,data:x]\nsops_mac=ENC[AES256_GCM,data:y]": "sops",
		"password: ENC[AES256_GCM,data:x] is a note\n":              "",
		"plain: text\n": "",
	}
	for text, want := range cases {
		if got := secretTool(text); got != want {
			t.Errorf("%q: %q want %q", text, got, want)
		}
	}
}
