package editor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/vcs"
)

// shellBehavior says what happens to the output of a piped command.
type shellBehavior int

const (
	shellReplace shellBehavior = iota
	shellIgnore
	shellInsert
	shellAppend
)

// shellRegister keeps the history of shell prompts.
const shellRegister = '|'

func init() {
	register("shell_pipe", "Pipe selections through shell command", func(cx *Context) {
		shellPrompt(cx, "pipe:", shellReplace)
	})
	register("shell_pipe_to", "Pipe selections into shell command ignoring output", func(cx *Context) {
		shellPrompt(cx, "pipe-to:", shellIgnore)
	})
	register("shell_insert_output", "Insert shell command output before selections", func(cx *Context) {
		shellPrompt(cx, "insert-output:", shellInsert)
	})
	register("shell_append_output", "Append shell command output after selections", func(cx *Context) {
		shellPrompt(cx, "append-output:", shellAppend)
	})
	register("shell_keep_pipe", "Filter selections with shell predicate", shellKeepPipe)

	typedCommands = append(typedCommands, typedCommand{
		names: []string{"run-shell-command", "sh"},
		raw:   true,
		doc:   "Run a shell command and show its output in a popup",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return cmdError("usage: :sh <command>")
			}

			cx.Editor.runShellInBackground(strings.Join(args, " "))

			return nil
		},
	}, typedCommand{
		names: []string{"pipe"},
		raw:   true,
		doc:   "Pipe each selection to the shell command",
		run:   func(cx *Context, args []string) error { return shellTyped(cx, args, shellReplace) },
	}, typedCommand{
		names: []string{"pipe-to"},
		raw:   true,
		doc:   "Pipe each selection to the shell command, ignoring output",
		run:   func(cx *Context, args []string) error { return shellTyped(cx, args, shellIgnore) },
	}, typedCommand{
		names: []string{"insert-output"},
		raw:   true,
		doc:   "Run shell command, inserting output before each selection",
		run:   func(cx *Context, args []string) error { return shellTyped(cx, args, shellInsert) },
	}, typedCommand{
		names: []string{"append-output"},
		raw:   true,
		doc:   "Run shell command, appending output after each selection",
		run:   func(cx *Context, args []string) error { return shellTyped(cx, args, shellAppend) },
	})
}

func shellTyped(cx *Context, args []string, behavior shellBehavior) error {
	if len(args) == 0 {
		return cmdError("shell command missing")
	}

	return cx.Editor.shellApply(strings.Join(args, " "), behavior)
}

// shellError is a failed shell command, its output is returned beside it.
type shellError struct {
	msg string
	err error
}

func (e *shellError) Error() string { return e.msg }
func (e *shellError) Unwrap() error { return e.err }

// shellRunner runs commands through the shell on the workspace host.
type shellRunner struct {
	host host.Host
	dir  string

	// argv is the shell and its arguments before the command.
	argv []string
}

// shellCommand returns the configured shell, sh -c by default, running
// in the working directory of the workspace.
func (e *Editor) shellCommand() shellRunner {
	argv := []string{"sh", "-c"}
	if len(e.Config.Editor.Shell) > 0 {
		argv = e.Config.Editor.Shell
	}

	return shellRunner{host: e.Host, dir: e.workDir, argv: argv}
}

// shellTimeout bounds shell commands, buildTimeout the builds of :make.
const (
	shellTimeout = 30 * time.Second
	buildTimeout = 10 * time.Minute
)

// runShellWait runs a command the editor waits for, bounded by
// shellTimeout so a stuck command cannot freeze the editor.
func runShellWait(shell shellRunner, cmd string, input *string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	out, err := runShell(ctx, shell, cmd, input)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("shell command timed out after %s", shellTimeout)
	}

	return out, err
}

// runShell runs cmd through the shell with optional stdin and returns
// its stdout. A failing command reports its stderr or exit status.
func runShell(ctx context.Context, shell shellRunner, cmd string, input *string) (string, error) {
	if len(shell.argv) == 0 {
		return "", errors.New("no shell configured")
	}

	c := host.Command{Name: shell.argv[0], Args: append(append([]string{}, shell.argv[1:]...), cmd), Dir: shell.dir}
	if input != nil {
		c.Stdin = []byte(*input)
	}

	stdout, stderr, err := shell.host.Run(ctx, c)
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			var exitErr *host.ExitError
			if errors.As(err, &exitErr) {
				msg = fmt.Sprintf("shell command failed: status %d", exitErr.Code)
			} else {
				msg = err.Error()
			}
		}

		// The output comes along, a failing diff or grep still has some.
		return string(stdout), &shellError{msg: msg, err: err}
	}

	return string(stdout), nil
}

// shellPrompt asks for a command and applies it to the selections.
func shellPrompt(cx *Context, prefix string, behavior shellBehavior) {
	e := cx.Editor
	e.openPrompt(prefix, func(cx *Context, input string, ev PromptEvent) {
		if ev != PromptValidate || strings.TrimSpace(input) == "" {
			return
		}

		cx.Editor.pushRegisterHistory(shellRegister, input)
		if err := cx.Editor.shellApply(input, behavior); err != nil {
			cx.Editor.SetError("%v", err)
		}
	}).withHistory(shellRegister)
}

// shellApply runs cmd for the selections. Piping behaviors feed each
// selection as stdin, the others run the command once and reuse the
// output. The selections end up on the inserted text.
func (e *Editor) shellApply(cmd string, behavior shellBehavior) error {
	cmd, err := e.expand(cmd)
	if err != nil {
		return err
	}

	pipe := behavior == shellReplace || behavior == shellIgnore
	doc := e.Doc()
	text := doc.Text()
	sel := doc.Selection()
	fragments := make([]string, sel.Len())
	for i, r := range sel.Ranges() {
		fragments[i] = r.Fragment(text)
	}

	docID, edits := doc.ID, doc.edits
	e.runShellJob(cmd, shellTimeout, func(ctx context.Context, shell shellRunner) ([]string, error) {
		// Insert and append run the command once and reuse its output.
		outputs := make([]string, len(fragments))
		for i, fragment := range fragments {
			if !pipe && i > 0 {
				outputs[i] = outputs[0]

				continue
			}

			var input *string
			if pipe {
				input = &fragment
			}

			out, err := runShell(ctx, shell, cmd, input)
			if err != nil {
				return nil, err
			}

			if !strings.HasSuffix(fragment, "\n") && strings.HasSuffix(out, "\n") {
				out = strings.TrimSuffix(strings.TrimSuffix(out, "\n"), "\r")
			}

			outputs[i] = out
		}

		return outputs, nil
	}, func(e *Editor, outputs []string, err error) {
		if err != nil {
			e.SetError("%v", err)

			return
		}

		cur, ok := e.Docs[docID]
		if !ok || behavior == shellIgnore {
			return
		}

		if cur.edits != edits {
			e.SetError("shell output dropped, the text changed meanwhile")

			return
		}

		changes := make([]edit.Change, 0, sel.Len())
		ranges := make([]edit.Range, 0, sel.Len())
		offset := 0
		for i, r := range sel.Ranges() {
			output := outputs[i]
			outLen := charCount(output)
			from, to := r.From(), r.From()
			deleted := 0
			switch behavior {
			case shellReplace:
				to, deleted = r.To(), r.Len()
			case shellAppend:
				from, to = r.To(), r.To()
			}

			anchor := to + offset - deleted
			next := edit.NewRange(anchor, anchor+outLen)
			if r.Head < r.Anchor {
				next = edit.NewRange(anchor+outLen, anchor)
			}

			ranges = append(ranges, next)
			offset += outLen - deleted
			changes = append(changes, edit.Change{From: from, To: to, Text: output})
		}

		tx := edit.ChangeText(cur.Text(), changes).WithSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
		cur.Apply(&tx)
		cur.AppendChangesToHistory()
	})

	return nil
}

// runShellJob runs shell work in the background so a slow command does
// not freeze the editor, escape in normal mode cancels it and timeout
// bounds it. done runs on the editor thread with the results and the
// error of the work, a failing command still has output worth showing.
func (e *Editor) runShellJob(cmd string, timeout time.Duration, work func(ctx context.Context, shell shellRunner) ([]string, error), done func(e *Editor, results []string, err error)) {
	if e.shellCancel != nil {
		e.shellCancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	e.shellCancel = cancel
	e.shellGen++
	gen := e.shellGen
	shell := e.shellCommand()
	e.SetStatus("running %s, esc cancels", firstLines(cmd, 1))
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		results, err := work(ctx, shell)
		// Read right away, the apply may run after the deadline passed.
		ctxErr := ctx.Err()
		emit(func(e *Editor) {
			if e.shellGen != gen {
				return
			}

			e.shellCancel = nil
			cancel()
			switch {
			case errors.Is(ctxErr, context.Canceled):
				e.SetStatus("shell command cancelled")
			case errors.Is(ctxErr, context.DeadlineExceeded):
				e.SetError("shell command timed out after %s", timeout)
			default:
				e.SetStatus("")
				done(e, results, err)
			}
		})
	})
}

// cancelShell stops a running shell job, reporting whether there was one.
func (e *Editor) cancelShell() bool {
	if e.shellCancel == nil {
		return false
	}

	e.shellCancel()
	e.shellCancel = nil
	e.shellGen++
	e.SetStatus("shell command cancelled")

	return true
}

// shellKeepPipe keeps the selections for which the command succeeds.
func shellKeepPipe(cx *Context) {
	e := cx.Editor
	e.openPrompt("keep-pipe:", func(cx *Context, input string, ev PromptEvent) {
		if ev != PromptValidate || strings.TrimSpace(input) == "" {
			return
		}

		e := cx.Editor
		e.pushRegisterHistory(shellRegister, input)
		cmd, err := e.expand(input)
		if err != nil {
			e.SetError("%v", err)

			return
		}

		doc := e.Doc()
		text := doc.Text()
		sel := doc.Selection()
		fragments := make([]string, sel.Len())
		for i, r := range sel.Ranges() {
			fragments[i] = r.Fragment(text)
		}

		docID, edits := doc.ID, doc.edits
		e.runShellJob(cmd, shellTimeout, func(ctx context.Context, shell shellRunner) ([]string, error) {
			// A result per selection, empty when the command failed.
			keep := make([]string, len(fragments))
			for i := range fragments {
				if _, err := runShell(ctx, shell, cmd, &fragments[i]); err == nil {
					keep[i] = "y"
				} else if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}

			return keep, nil
		}, func(e *Editor, keep []string, err error) {
			if err != nil {
				e.SetError("%v", err)

				return
			}

			cur, ok := e.Docs[docID]
			if !ok || cur.edits != edits {
				return
			}

			var kept []edit.Range
			primary := -1
			for i, r := range sel.Ranges() {
				if keep[i] == "" {
					continue
				}

				kept = append(kept, r)
				if i >= sel.PrimaryIndex() && primary < 0 {
					primary = len(kept) - 1
				}
			}

			if len(kept) == 0 {
				e.SetError("No selections remaining")

				return
			}

			if primary < 0 {
				primary = len(kept) - 1
			}

			cur.SetSelection(edit.NewSelection(kept, primary))
		})
	}).withHistory(shellRegister)
}

// runShellInBackground runs cmd without touching the document and shows
// its output in a popup when done.
func (e *Editor) runShellInBackground(cmd string) {
	cmd, err := e.expand(cmd)
	if err != nil {
		e.SetError("%v", err)

		return
	}

	e.runShellJob(cmd, shellTimeout, func(ctx context.Context, shell shellRunner) ([]string, error) {
		output, err := runShell(ctx, shell, cmd, nil)

		return []string{output}, err
	}, func(e *Editor, results []string, err error) {
		if output := results[0]; strings.TrimSpace(output) != "" {
			e.Popup = &Popup{Lines: strings.Split(strings.TrimRight(output, "\n"), "\n")}
		}

		if err != nil {
			e.SetError("%v", err)

			return
		}

		e.SetStatus("Command run")
	})
}

// expand replaces %{variable}, %sh{command}, %u{hex} and %reg{r} in s.
// %% is a literal percent sign.
func (e *Editor) expand(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}

	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '%' {
			out.WriteRune(runes[i])

			continue
		}

		if i+1 < len(runes) && runes[i+1] == '%' {
			out.WriteRune('%')
			i++

			continue
		}

		kind, content, end, err := parseExpansion(runes, i)
		if err != nil {
			return "", err
		}

		value, err := e.expandToken(kind, content)
		if err != nil {
			return "", err
		}

		out.WriteString(value)
		i = end
	}

	return out.String(), nil
}

var expansionPairs = map[rune]rune{'{': '}', '[': ']', '(': ')', '<': '>', '\'': '\'', '"': '"', '|': '|'}

// parseExpansion reads %kind{content} starting at the percent sign and
// returns the kind, the raw content and the index of the closing
// delimiter. Bracket delimiters nest.
func parseExpansion(runes []rune, start int) (string, string, int, error) {
	i := start + 1
	for i < len(runes) && (runes[i] >= 'a' && runes[i] <= 'z') {
		i++
	}

	kind := string(runes[start+1 : i])
	if i >= len(runes) {
		return "", "", 0, fmt.Errorf("unterminated expansion at %q, %%%% is a literal %%", string(runes[start:]))
	}

	open := runes[i]
	closing, ok := expansionPairs[open]
	if !ok {
		return "", "", 0, fmt.Errorf("expansion needs a delimiter after %%%s, %%%% is a literal %%", kind)
	}

	depth := 0
	for j := i + 1; j < len(runes); j++ {
		switch {
		case runes[j] == open && open != closing:
			depth++
		case runes[j] == closing:
			if depth == 0 {
				return kind, string(runes[i+1 : j]), j, nil
			}

			depth--
		}
	}

	return "", "", 0, fmt.Errorf("unterminated expansion %%%s%c", kind, open)
}

func (e *Editor) expandToken(kind, content string) (string, error) {
	switch kind {
	case "":
		return e.expandVariable(strings.TrimSpace(content))
	case "sh":
		inner, err := e.expand(content)
		if err != nil {
			return "", err
		}

		output, err := runShellWait(e.shellCommand(), inner, nil)
		if err != nil {
			return "", err
		}

		return strings.TrimSuffix(strings.TrimSuffix(output, "\n"), "\r"), nil
	case "u":
		code, err := strconv.ParseUint(strings.TrimSpace(content), 16, 32)
		if err != nil {
			return "", fmt.Errorf("could not interpret %q as a Unicode character code", content)
		}

		return string(rune(code)), nil
	case "reg":
		name := []rune(content)
		if len(name) != 1 {
			return "", fmt.Errorf("invalid register %q: should be a single character", content)
		}

		values, _ := e.readRegister(name[0])

		return strings.Join(values, "\n"), nil
	}

	return "", fmt.Errorf("unknown expansion kind %q", kind)
}

func (e *Editor) expandVariable(name string) (string, error) {
	doc := e.Doc()
	text := doc.Text()
	primary := doc.Selection().Primary()
	switch name {
	case "cursor_line":
		return strconv.Itoa(primary.CursorLine(text) + 1), nil
	case "cursor_column":
		return strconv.Itoa(edit.CoordsAtPos(text, primary.Cursor(text)).Col + 1), nil
	case "buffer_name":
		if doc.Path() == "" {
			return "[scratch]", nil
		}

		if rel, ok := RelativeTo(e.WorkingDir(), doc.Path()); ok {
			return rel, nil
		}

		return doc.Path(), nil
	case "file_path_absolute":
		if doc.Path() == "" {
			return e.WorkingDir(), nil
		}

		return doc.Path(), nil
	case "line_ending":
		return string(doc.LineEnding()), nil
	case "buffer_directory":
		if doc.Path() == "" {
			return e.WorkingDir(), nil
		}

		return filepath.Dir(doc.Path()), nil
	case "current_working_directory":
		return e.WorkingDir(), nil
	case "workspace_directory":
		if root, err := vcs.Root(e.Host, e.WorkingDir()); err == nil {
			return root, nil
		}

		return e.WorkingDir(), nil
	case "language":
		if doc.language != nil {
			return doc.language.Name, nil
		}

		return "text", nil
	case "selection":
		return primary.Fragment(text), nil
	case "selection_line_start":
		start, _ := primary.LineRange(text)

		return strconv.Itoa(start + 1), nil
	case "selection_line_end":
		_, end := primary.LineRange(text)

		return strconv.Itoa(end + 1), nil
	}

	return "", fmt.Errorf("unknown variable %q", name)
}
