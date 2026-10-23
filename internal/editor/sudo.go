package editor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// sudoCommand runs commands as root, a variable so tests can fake it.
var sudoCommand = "sudo"

// sudoTimeout bounds a sudo call, a password check or a write.
const sudoTimeout = 30 * time.Second

func init() {
	typedCommands = append(typedCommands,
		typedCommand{
			names: []string{"sudo-write", "sw"},
			doc:   "Write the buffer as root through sudo, asking for the password when sudo needs it",
			run:   sudoWrite,
		},
		typedCommand{
			names: []string{"sudo-open", "so"},
			doc:   "Open a file as root through sudo, the current file without a path. Writes go through sudo too",
			run:   sudoOpen,
		},
	)
}

// withSudo runs fn once sudo works without a password on h. sudo runs
// non interactive so it never talks to the terminal: when it needs a
// password, a secret prompt asks for it and sudo -S -v checks it first.
func (e *Editor) withSudo(h host.Host, fn func(e *Editor) error) error {
	if _, err := h.LookPath(sudoCommand); err != nil {
		return cmdError("sudo is not installed")
	}

	if runSudo(h, "", "-n", "true") == nil {
		return fn(e)
	}

	e.openPrompt("[sudo] password: ", func(cx *Context, input string, ev PromptEvent) {
		if ev != PromptValidate {
			return
		}

		e := cx.Editor
		if err := runSudo(h, input+"\n", "-S", "-p", "", "-v"); err != nil {
			e.SetError("sudo: %v", err)

			return
		}

		if err := fn(e); err != nil {
			e.SetError("%v", err)
		}
	})
	e.Prompt.Secret = true

	return nil
}

// sudoWrite saves the buffer through sudo tee.
func sudoWrite(cx *Context, args []string) error {
	e := cx.Editor
	doc := e.Doc()
	path := doc.Path()
	if len(args) > 0 {
		path = e.resolvePath(doc, args[0])
	}

	if path == "" {
		return errNoFile
	}

	docID := doc.ID

	return e.withSudo(e.hostOf(doc), func(e *Editor) error {
		doc, ok := e.Docs[docID]
		if !ok {
			return nil
		}

		return sudoTee(e, doc, path)
	})
}

// sudoOpen reads a file as root and marks the buffer so writes and
// reloads go through sudo as well. Without a path it reopens the current
// file that way.
func sudoOpen(cx *Context, args []string) error {
	e := cx.Editor
	cur := e.Doc()
	h := e.Host
	path := cur.Path()
	if len(args) > 0 {
		path = e.absPath(e.resolvePath(cur, args[0]))
	} else {
		h = e.hostOf(cur)
	}

	if path == "" {
		return errNoFile
	}

	return e.sudoOpenPath(h, path)
}

// sudoOpenPath opens path of h as root, asking for the password when
// sudo needs it.
func (e *Editor) sudoOpenPath(h host.Host, path string) error {
	return e.withSudo(h, func(e *Editor) error {
		doc, ok := e.documentOn(h, path)
		switch {
		case !ok:
			text, err := sudoRead(h, path)
			if err != nil {
				return err
			}

			doc = newFileDocument(h, path, text)
			doc.asRoot = true
			e.addDocument(doc)
		case !doc.IsModified():
			// Reread as root, the open text may be stale or partial.
			doc.asRoot = true
			if err := reloadDocument(e, doc); err != nil {
				return err
			}
		default:
			doc.asRoot = true
		}

		e.pushJump()
		e.Switch(doc.ID, ActionReplace)
		e.SetStatus("'%s' opened as root", e.docName(doc))

		return nil
	})
}

// offerSudo turns a permission error into the question whether to retry
// as root, y runs retry. It reports whether err was a permission error.
func (e *Editor) offerSudo(cx *Context, err error, what string, retry func(e *Editor) error) bool {
	if !errors.Is(err, fs.ErrPermission) {
		return false
	}

	e.SetError("permission denied for %s, open it with sudo? y/n", what)
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		if ch, ok := key.Char(); !ok || ch != 'y' && ch != 'Y' {
			return
		}

		if err := retry(cx.Editor); err != nil {
			cx.Editor.SetError("%v", err)
		}
	})

	return true
}

// sudoList lists a directory as root, directories and links to them
// marked as such. ls fails on a dangling link but still lists the rest.
func sudoList(h host.Host, dir string) ([]host.DirEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sudoTimeout)
	defer cancel()

	out, stderr, err := h.Run(ctx, host.Command{Name: sudoCommand, Args: []string{"-n", "ls", "-1ApL", dir}})
	if err != nil && len(out) == 0 {
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return nil, fmt.Errorf("sudo list %s: %s", dir, msg)
		}

		return nil, fmt.Errorf("sudo list %s: %w", dir, err)
	}

	var entries []host.DirEntry
	for line := range strings.Lines(string(out)) {
		name := strings.TrimRight(line, "\n")
		switch {
		case name == "":
		case strings.HasSuffix(name, "/"):
			entries = append(entries, host.DirEntry{Name: strings.TrimSuffix(name, "/"), Type: fs.ModeDir})
		default:
			entries = append(entries, host.DirEntry{Name: name})
		}
	}

	return entries, nil
}

// sudoRead returns the text of a file read as root, empty for a file
// that does not exist yet.
func sudoRead(h host.Host, path string) (string, error) {
	if runSudo(h, "", "-n", "test", "-e", path) != nil {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), sudoTimeout)
	defer cancel()

	out, stderr, err := h.Run(ctx, host.Command{Name: sudoCommand, Args: []string{"-n", "cat", "--", path}})
	if err != nil {
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return "", fmt.Errorf("sudo read %s: %s", path, msg)
		}

		return "", fmt.Errorf("sudo read %s: %w", path, err)
	}

	return string(out), nil
}

// sudoTee writes the document to path as root and marks it saved.
func sudoTee(e *Editor, doc *Document, path string) error {
	if doc.secret != nil && path == doc.Path() {
		doc.asRoot = true

		return e.saveSecret(doc)
	}

	e.prepareForSave(doc)
	var check *savedCheck
	if path == doc.Path() {
		check = e.prepareCheck(doc, true)
	}

	if err := runSudo(e.hostOf(doc), string(doc.fileBytes()), "-n", "tee", "--", path); err != nil {
		return fmt.Errorf("sudo write %s: %w", path, err)
	}

	defer e.runCheck(doc, check)

	if path == doc.Path() {
		doc.AppendChangesToHistory()
		doc.markSaved()
		doc.rememberDiskTime()
		for _, client := range doc.Clients() {
			client.DidSave(doc.uri, doc.Text().String)
		}

		e.saveUndo(doc)
	}

	e.SetStatus("'%s' written as root, %dL", path, doc.LineCount())

	return nil
}

// runSudo runs sudo with stdin, discarding its output, and reports its
// error output on failure.
func runSudo(h host.Host, stdin string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sudoTimeout)
	defer cancel()

	_, stderr, err := h.Run(ctx, host.Command{Name: sudoCommand, Args: args, Stdin: []byte(stdin)})
	if err != nil {
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return errors.New(msg)
		}

		return err
	}

	return nil
}
