package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// checkedLanguage rejects files holding the word bad, naming the line.
func checkedLanguage() *syntax.Language {
	script := `if grep -n bad "$1" | head -1 | grep -q .; then n=$(grep -n bad "$1" | head -1 | cut -d: -f1); echo "$1:$n: bad value" >&2; exit 1; fi`

	return &syntax.Language{Name: "conf", Check: syntax.Formatter{Command: "sh", Args: []string{"-c", script, "sh", "{file}"}}}
}

// TestCheckAfterSave puts the old file back when the check fails, keeps
// a passing save, and writes anyway with --no-check.
func TestCheckAfterSave(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.conf")
	if err := os.WriteFile(path, []byte("good\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := newTestEditor(t, "#[a|]#")
	doc, err := e.Open(path, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	doc.language = checkedLanguage()
	disk := func() string {
		data, _ := os.ReadFile(path)

		return string(data)
	}
	save := func(text, cmd string) {
		t.Helper()

		replaceByLines(doc, text)
		e.runTyped(&Context{Editor: e}, cmd)
		runJobs(e)
	}

	save("good\nbad\n", "w")
	if disk() != "good\n" || !doc.IsModified() || !e.Status().IsError {
		t.Fatalf("failed check left %q, modified %v, status %+v", disk(), doc.IsModified(), e.Status())
	}

	if len(e.quickfix) != 1 || e.quickfix[0].line != 2 {
		t.Fatalf("quickfix %+v", e.quickfix)
	}

	save("fine\n", "w")
	if disk() != "fine\n" || doc.IsModified() || !strings.Contains(e.Status().Text, "passed") {
		t.Fatalf("passing check left %q, status %+v", disk(), e.Status())
	}

	save("bad\n", "w --no-check")
	if disk() != "bad\n" || doc.IsModified() {
		t.Fatalf("--no-check left %q", disk())
	}

	// A new file that fails is removed again.
	fresh := filepath.Join(dir, "new.conf")
	doc2, err := e.Open(fresh, ActionReplace)
	if err != nil {
		t.Fatal(err)
	}

	doc2.language = checkedLanguage()
	replaceByLines(doc2, "bad\n")
	e.runTyped(&Context{Editor: e}, "w")
	runJobs(e)
	if _, err := os.Stat(fresh); !os.IsNotExist(err) || !doc2.IsModified() {
		t.Fatalf("a failing new file stays on disk: %v", err)
	}
}

func TestFindLocation(t *testing.T) {
	t.Parallel()

	cases := map[string]location{
		`nginx: [emerg] unknown directive "foo" in /etc/nginx/conf.d/x.conf:12`: {path: "/etc/nginx/conf.d/x.conf", line: 12},
		"/etc/ssh/sshd_config line 7: Bad configuration option: Foo":            {path: "/etc/ssh/sshd_config", line: 7},
		"/home/u/.ssh/config: line 3: Bad configuration option: bogus":          {path: "/home/u/.ssh/config", line: 3},
		"/etc/sudoers.d/x:3:12: syntax error":                                   {path: "/etc/sudoers.d/x", line: 3, col: 12},
	}
	for line, want := range cases {
		got, ok := findLocation(line, "/w")
		if !ok || got.path != want.path || got.line != want.line || got.col != want.col {
			t.Errorf("%q gave %+v %v", line, got, ok)
		}
	}
}
