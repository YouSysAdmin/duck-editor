package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSudo installs a sudo stand in: -n commands fail unless the password
// was given before, -S -v checks it against "secret". cat and tee get
// past the mode of a file the way root would.
func fakeSudo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	script := `#!/bin/sh
state="` + dir + `/ok"
case "$1" in
-S) read pw; [ "$pw" = secret ] || { echo "Sorry, try again." >&2; exit 1; }; touch "$state" ;;
-n) shift
    [ -f "$state" ] || { echo "a password is required" >&2; exit 1; }
    case "$1" in
    true) exit 0 ;;
    test) test "$2" "$3" ;;
    cat) chmod u+r "$3"; cat "$3"; chmod u-r "$3" ;;
    tee) chmod u+w "$3" 2>/dev/null; cat > "$3" ;;
    ls) chmod u+rx "$3"; ls "$2" "$3"; chmod u-rx "$3" ;;
    esac ;;
esac
`
	path := filepath.Join(dir, "sudo")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestSudoWrite(t *testing.T) {
	old := sudoCommand
	sudoCommand = fakeSudo(t)
	t.Cleanup(func() { sudoCommand = old })

	path := filepath.Join(t.TempDir(), "conf")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[x|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	e.feed(t, "ib<esc>:sudo-write<ret>")
	if e.Prompt == nil || !e.Prompt.Secret {
		t.Fatal("a password prompt opens when sudo needs one")
	}

	e.feed(t, "wrong<ret>")
	if !e.Status().IsError || !doc.IsModified() {
		t.Fatalf("a wrong password writes nothing: %q", e.Status().Text)
	}

	e.feed(t, ":sudo-write<ret>")
	if got := e.Prompt.Display(); got != "" {
		t.Fatalf("display = %q", got)
	}

	e.feed(t, "secret")
	if got := e.Prompt.Display(); got != "******" {
		t.Fatalf("the password is masked, shown %q", got)
	}

	e.feed(t, "<ret>")
	data, _ := os.ReadFile(path)
	if string(data) != "ba\n" || doc.IsModified() {
		t.Fatalf("file %q modified %v status %q", data, doc.IsModified(), e.Status().Text)
	}

	// Cached credentials write without asking.
	e.feed(t, "ic<esc>:sudo-write<ret>")
	if e.Prompt != nil {
		t.Fatal("no prompt while sudo works without a password")
	}

	if data, _ := os.ReadFile(path); string(data) != "bca\n" {
		t.Fatalf("second write %q", data)
	}
}

func TestSudoOpen(t *testing.T) {
	old := sudoCommand
	sudoCommand = fakeSudo(t)
	t.Cleanup(func() { sudoCommand = old })

	path := filepath.Join(t.TempDir(), "user-data")
	if err := os.WriteFile(path, []byte("secret data\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	e := newTestEditor(t, "#[x|]#")
	if _, err := e.Open(path, ActionReplace); err == nil || !strings.Contains(err.Error(), ":sudo-open") {
		t.Fatalf("plain open = %v, want a hint at sudo-open", err)
	}

	e.feed(t, ":sudo-open "+path+"<ret>")
	if e.Prompt == nil || !e.Prompt.Secret {
		t.Fatal("sudo asks for the password")
	}

	e.feed(t, "secret<ret>")
	doc := e.Doc()
	if doc.Path() != path || !doc.AsRoot() || doc.Text().String() != "secret data\n" {
		t.Fatalf("opened %q root %v text %q status %+v", doc.Path(), doc.AsRoot(), doc.Text().String(), e.Status())
	}

	// A plain write goes through sudo for a root file.
	e.feed(t, "ione <esc>:w<ret>")
	if doc.IsModified() {
		t.Fatalf("write failed: %+v", e.Status())
	}

	_ = os.Chmod(path, 0o644)
	if data, _ := os.ReadFile(path); string(data) != "one secret data\n" {
		t.Fatalf("file %q", data)
	}
}

func TestSudoFromPickers(t *testing.T) {
	old := sudoCommand
	sudoCommand = fakeSudo(t)
	t.Cleanup(func() { sudoCommand = old })

	dir := filepath.Join(t.TempDir(), "instance")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	locked := filepath.Join(dir, "user-data.txt")
	if err := os.WriteFile(locked, []byte("#!/bin/sh\n"), 0); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
		_ = os.Chmod(locked, 0o644)
	})

	// A root only file picked in the explorer asks before using sudo.
	e := newTestEditor(t, "#[x|]#")
	fileExplorer(&Context{Editor: e}, dir)
	e.feed(t, "user<ret>")
	if st := e.Status(); !st.IsError || !strings.Contains(st.Text, "sudo? y/n") {
		t.Fatalf("status %+v", st)
	}

	e.feed(t, "y")
	e.feed(t, "secret<ret>")
	if doc := e.Doc(); doc.Path() != locked || !doc.AsRoot() {
		t.Fatalf("opened %q root %v status %+v", doc.Path(), doc.AsRoot(), e.Status())
	}

	// A root only directory is listed through sudo after asking.
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}

	fileExplorer(&Context{Editor: e}, dir)
	e.feed(t, "y")
	if e.Picker == nil || !strings.HasSuffix(e.Picker.Title, "as root") || len(e.Picker.Items()) != 2 || e.Picker.Items()[1].Label != "user-data.txt" {
		t.Fatalf("picker %+v status %+v", e.Picker, e.Status())
	}
}
