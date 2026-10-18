package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeTools puts scripts named like the log programs first in PATH, each
// prints its name and arguments.
func fakeTools(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"journalctl", "docker", "kubectl"} {
		script := "#!/bin/sh\necho " + name + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	list := "#!/bin/sh\necho 'nginx.service loaded active running A high performance web server'\necho 'ssh.service loaded active running OpenBSD Secure Shell server'\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(list), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestLogCommands builds the program lines of the log commands, picks a
// unit from the list and passes options through.
func TestLogCommands(t *testing.T) {
	fakeTools(t)

	cases := []struct{ cmd, title, want string }{
		{"journal nginx.service -p err", "journal:nginx.service", "journalctl --no-pager -f -o short-iso -n 10000 -u nginx.service -p err\n"},
		{"journal -p warning", "journal", "journalctl --no-pager -f -o short-iso -n 10000 -p warning\n"},
		{"dlogs web --timestamps", "docker:web", "docker logs -f --tail 10000 --timestamps web\n"},
		{"klogs api-7d9 -n prod -c app", "k8s:api-7d9", "kubectl logs -f --tail 10000 -n prod -c app api-7d9\n"},
	}
	for _, c := range cases {
		e := newTestEditor(t, "#[a|]#")
		e.runTyped(&Context{Editor: e}, c.cmd)
		doc := e.Doc()
		runJobs(e)
		if doc.Title() != c.title || doc.Text().String() != c.want {
			t.Errorf("%s: title %q text %q, status %+v", c.cmd, doc.Title(), doc.Text().String(), e.Status())
		}
	}

	e := newTestEditor(t, "#[a|]#")
	e.runTyped(&Context{Editor: e}, "journal")
	runJobs(e)
	if e.Picker == nil || len(e.Picker.Items()) != 2 {
		t.Fatalf("unit picker %+v status %+v", e.Picker, e.Status())
	}

	e.feed(t, "ssh<ret>")
	runJobs(e)
	if got := e.Doc().Text().String(); got != "journalctl --no-pager -f -o short-iso -n 10000 -u ssh.service\n" {
		t.Fatalf("picked unit followed %q", got)
	}
}
