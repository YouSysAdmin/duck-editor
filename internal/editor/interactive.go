package editor

import (
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// ExecRequest is a program that takes over the terminal until it exits,
// such as lazygit or k9s.
type ExecRequest struct {
	Argv []string
	Dir  string
}

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"run-interactive", "ri"},
		raw:   true,
		doc:   "Run a program that takes over the terminal, such as lazygit, until it exits",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return cmdError("usage: :run-interactive <command>")
			}

			return cx.Editor.runInteractive(strings.Join(args, " "))
		},
	})
}

// runInteractive asks the UI to hand the terminal to a shell command. On
// a remote workspace the command runs there, through the shell that
// remote-env names so the user's PATH applies.
func (e *Editor) runInteractive(cmd string) error {
	cmd, err := e.expand(cmd)
	if err != nil {
		return err
	}

	if r, ok := e.Host.(*host.Remote); ok {
		shell := `exec "${SHELL:-sh}" -lic `
		switch e.Config.Editor.RemoteEnv {
		case "login":
			shell = `exec "${SHELL:-sh}" -lc `
		case "none":
			shell = "exec sh -c "
		}

		script := "cd " + host.ShellQuote(e.WorkingDir()) + " && " + shell + host.ShellQuote(cmd)
		argv := r.TerminalCommand(script)
		if argv == nil {
			return cmdError("this remote host cannot run interactive programs")
		}

		e.exec = &ExecRequest{Argv: argv}

		return nil
	}

	shell := e.shellCommand()
	e.exec = &ExecRequest{Argv: append(append([]string{}, shell.argv...), cmd), Dir: shell.dir}

	return nil
}

// TakeExec returns and clears a pending program for the terminal.
func (e *Editor) TakeExec() *ExecRequest {
	req := e.exec
	e.exec = nil

	return req
}

// ExecDone runs when the program gave the terminal back: it may have
// changed files and the git state, as lazygit does.
func (e *Editor) ExecDone(err error) {
	if err != nil {
		e.SetError("%v", err)
	}

	e.FocusGained()
}
