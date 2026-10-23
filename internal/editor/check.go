package editor

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// savedCheck is a check to run on a file just written, with the content
// it had before for putting back.
type savedCheck struct {
	path    string
	old     []byte
	existed bool
	gen     int

	// asRoot reads, checks and puts back through sudo.
	asRoot bool
}

// prepareCheck reads the file before a save when its language has an
// installed check, nil when there is nothing to check. asRoot reads and
// checks through sudo, for a save as root.
func (e *Editor) prepareCheck(doc *Document, asRoot bool) *savedCheck {
	if doc.noCheck {
		doc.noCheck = false

		return nil
	}

	lang := doc.language
	if lang == nil || lang.Check.Command == "" || doc.Path() == "" {
		return nil
	}

	h := e.hostOf(doc)
	if _, err := h.LookPath(lang.Check.Command); err != nil {
		return nil
	}

	c := &savedCheck{path: doc.Path(), asRoot: asRoot}
	if asRoot {
		data, err := sudoRead(h, c.path)
		c.old, c.existed = []byte(data), err == nil && data != ""
	} else {
		data, err := h.ReadFile(c.path)
		c.old, c.existed = data, err == nil
	}

	doc.checkGen++
	c.gen = doc.checkGen

	return c
}

// runCheck validates the file in the background. When the check fails
// the old file goes back, the buffer stays modified and the places the
// check names go to ]q.
func (e *Editor) runCheck(doc *Document, c *savedCheck) {
	if c == nil {
		return
	}

	check := doc.language.Check
	args := make([]string, len(check.Args))
	for i, a := range check.Args {
		args[i] = strings.ReplaceAll(a, "{file}", c.path)
	}

	cmd := host.Command{Name: check.Command, Args: args, Dir: filepath.Dir(c.path)}
	if c.asRoot {
		cmd = host.Command{Name: sudoCommand, Args: append([]string{"-n", cmd.Name}, cmd.Args...), Dir: cmd.Dir}
	}

	h, docID, asRoot := e.hostOf(doc), doc.ID, c.asRoot
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		stdout, stderr, err := h.Run(ctx, cmd)
		out := strings.TrimSpace(string(stdout) + string(stderr))
		var exitErr *host.ExitError
		failed := errors.As(err, &exitErr)
		var restoreErr error
		if failed {
			restoreErr = restoreFile(h, c, asRoot)
		}

		emit(func(e *Editor) {
			doc, ok := e.Docs[docID]
			if !ok || doc.checkGen != c.gen {
				return
			}

			switch {
			case err == nil:
				e.SetStatus("'%s' written, %s passed", e.docName(doc), check.Command)
			case !failed:
				e.SetError("%s could not check '%s': %v", check.Command, e.docName(doc), err)
			case restoreErr != nil:
				e.SetError("%s failed and the old file could not be put back: %v", check.Command, restoreErr)
				e.showCheckOutput(out)
			default:
				doc.markSavedAs(buffer.NewRope(string(c.old)))
				doc.rememberDiskTime()
				e.SetError("%s failed, the old file is back, :w --no-check writes anyway: %s", check.Command, firstLines(out, 1))
				e.showCheckOutput(out)
			}
		})
	})
}

// restoreFile puts the content before a save back, or removes a file the
// save created.
func restoreFile(h host.Host, c *savedCheck, asRoot bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	switch {
	case asRoot && c.existed:
		return runSudo(h, string(c.old), "-n", "tee", "--", c.path)
	case asRoot:
		return runSudo(h, "", "-n", "rm", "-f", "--", c.path)
	case c.existed:
		return h.WriteFile(c.path, c.old, false)
	default:
		_, _, err := h.Run(ctx, host.Command{Name: "rm", Args: []string{"-f", "--", c.path}})

		return err
	}
}

// showCheckOutput shows what a failed check printed and keeps the places
// it names for ]q and space q.
func (e *Editor) showCheckOutput(out string) {
	if out == "" {
		return
	}

	e.Popup = &Popup{Lines: ui.PlainLines(strings.Split(out, "\n"))}
	var locs []location
	for line := range strings.Lines(out) {
		if loc, ok := findLocation(strings.TrimRight(line, "\r\n"), e.WorkingDir()); ok {
			locs = append(locs, loc)
		}
	}

	if len(locs) > 0 {
		e.quickfix, e.quickfixIdx = locs, -1
	}
}

// Places named inside a line: "in /etc/nginx/x.conf:12" as nginx writes
// them and "/etc/ssh/ssh_config: line 12:" as ssh and sshd do.
var (
	pathLineInside = regexp.MustCompile(`(?:^|\s)(/[^\s:]+):(\d+)(?::(\d+))?`)
	pathLineWord   = regexp.MustCompile(`(?:^|\s)(/[^\s:]+):? line (\d+)`)
)

// findLocation reads a place from a line of checker output, at its start
// as compilers write it or inside it.
func findLocation(line, dir string) (location, bool) {
	if loc, ok := parseLocation(line, dir); ok {
		return loc, true
	}

	for _, re := range []*regexp.Regexp{pathLineInside, pathLineWord} {
		if m := re.FindStringSubmatch(line); m != nil {
			l, _ := strconv.Atoi(m[2])
			col := 0
			if len(m) > 3 {
				col, _ = strconv.Atoi(m[3])
			}

			return location{path: m[1], line: l, col: col, text: strings.TrimSpace(line)}, true
		}
	}

	return location{}, false
}
