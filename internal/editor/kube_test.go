package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// fakeKubectl keeps one object in $KSTATE: get prints it, diff compares
// stdin with it and exits 1 on a difference, replace stores stdin.
const fakeKubectl = `#!/bin/sh
case "$1" in
get) cat "$KSTATE" ;;
diff) t=$(mktemp); cat > "$t"; if cmp -s "$t" "$KSTATE"; then exit 0; fi; diff -u "$KSTATE" "$t"; exit 1 ;;
replace) cat > "$KSTATE.new"; mv "$KSTATE.new" "$KSTATE"; echo replaced ;;
esac
`

// TestKubeEdit opens a secret with its data decoded, shows the diff on
// save and replaces the object only after a yes.
func TestKubeEdit(t *testing.T) {
	bin := t.TempDir()
	state := filepath.Join(t.TempDir(), "object.yaml")
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil {
		t.Fatal(err)
	}

	secret := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  resourceVersion: \"7\"\ndata:\n  password: czNjcmV0\n  cert: /w==\n"
	if err := os.WriteFile(state, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KSTATE", state)

	e := newTestEditor(t, "#[a|]#")
	e.runTyped(&Context{Editor: e}, "kedit secret/db -n prod")
	runJobs(e)
	doc := e.Doc()
	text := doc.Text().String()
	if doc.Title() != "k8s:secret/db" || !strings.Contains(text, "stringData:\n  password: s3cret\n") || !strings.Contains(text, "cert: /w==") {
		t.Fatalf("opened %q as %q, status %+v", doc.Title(), text, e.Status())
	}

	replaceByLines(doc, strings.Replace(text, "password: s3cret", "password: n3w", 1))
	e.runTyped(&Context{Editor: e}, "w")
	runJobs(e)
	if e.Popup == nil || !strings.Contains(strings.Join(ui.Strings(e.Popup.Lines), "\n"), "+  password: n3w") {
		t.Fatalf("no diff shown: %+v, status %+v", e.Popup, e.Status())
	}

	e.feed(t, "n")
	runJobs(e)
	if data, _ := os.ReadFile(state); string(data) != secret || !doc.IsModified() {
		t.Fatalf("a no applied the object: %q", data)
	}

	e.runTyped(&Context{Editor: e}, "w")
	runJobs(e)
	e.feed(t, "y")
	runJobs(e)
	if data, _ := os.ReadFile(state); !strings.Contains(string(data), "password: n3w") || doc.IsModified() {
		t.Fatalf("after yes the cluster has %q, modified %v, status %+v", data, doc.IsModified(), e.Status())
	}

	// Saving again with no change says so and applies nothing.
	e.runTyped(&Context{Editor: e}, "w")
	runJobs(e)
	if !strings.Contains(e.Status().Text, "no changes") {
		t.Fatalf("unchanged save: %+v", e.Status())
	}
}

// TestSecretListToStringData decodes every Secret of a List.
func TestSecretListToStringData(t *testing.T) {
	t.Parallel()

	list := "apiVersion: v1\nkind: List\nitems:\n  - apiVersion: v1\n    kind: Secret\n    metadata:\n      name: a\n    data:\n      user: YWRtaW4=\n  - apiVersion: v1\n    kind: ConfigMap\n    metadata:\n      name: c\n    data:\n      k: YWRtaW4=\n  - apiVersion: v1\n    kind: Secret\n    metadata:\n      name: b\n    data:\n      cert: /w==\n      token: dG9r\n"
	got := secretToStringData(list)
	for _, want := range []string{"stringData:\n      user: admin\n", "k: YWRtaW4=", "cert: /w==", "stringData:\n      token: tok\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}
