// Package clipboard reads and writes the system clipboard through the
// common external tools, picking one from the environment.
package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Provider moves text to and from the system clipboard. primary selects
// the X11 primary selection where it exists.
type Provider interface {
	Name() string
	Get(primary bool) (string, error)
	Set(primary bool, text string) error
}

// Command describes one external program.
type Command struct {
	Command string
	Args    []string
}

// Custom runs user configured programs. Paste writes to the clipboard,
// Yank reads it, the primary variants may be empty.
type Custom struct {
	Yank, Paste               Command
	PrimaryYank, PrimaryPaste Command
	name                      string
}

// Name implements Provider.
func (c *Custom) Name() string {
	if c.name != "" {
		return c.name
	}

	return "custom"
}

// selectionName names the clipboard or the primary selection.
func selectionName(primary bool) string {
	if primary {
		return "primary selection"
	}

	return "clipboard"
}

// Get implements Provider.
func (c *Custom) Get(primary bool) (string, error) {
	cmd := c.Yank
	if primary {
		cmd = c.PrimaryYank
	}

	if cmd.Command == "" {
		return "", fmt.Errorf("the clipboard provider cannot read the %s", selectionName(primary))
	}

	out, err := run(cmd, "", true)
	if err != nil {
		return "", err
	}

	return out, nil
}

// Set implements Provider.
func (c *Custom) Set(primary bool, text string) error {
	cmd := c.Paste
	if primary {
		cmd = c.PrimaryPaste
	}

	if cmd.Command == "" {
		return fmt.Errorf("the clipboard provider cannot write the %s", selectionName(primary))
	}

	_, err := run(cmd, text, false)

	return err
}

// waitDelay bounds how long a finished command may keep its pipes open.
const waitDelay = time.Second

// runTimeout bounds a clipboard command.
var runTimeout = 3 * time.Second

// run executes a clipboard command. Copy commands like xclip fork a
// process that keeps serving the selection, so their output is not
// captured, which would wait for that process.
func run(c Command, input string, capture bool) (string, error) {
	// A hung provider, such as xclip without a display, must not hang
	// the editor.
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	proc := exec.CommandContext(ctx, c.Command, c.Args...)
	proc.Stdin = strings.NewReader(input)
	proc.WaitDelay = waitDelay
	var stdout, stderr bytes.Buffer
	if capture {
		proc.Stdout = &stdout
	}

	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: no answer within %s", c.Command, runTimeout)
		}

		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}

		return "", fmt.Errorf("%s: %s", c.Command, msg)
	}

	return stdout.String(), nil
}

// Memory keeps the clipboard in the process. It backs the terminal
// escape provider, whose writes the UI forwards as OSC 52, and tests.
type Memory struct {
	Title    string
	Contents [2]string

	// OnSet is called with every write, nil when not needed.
	OnSet func(text string)
}

// Name implements Provider.
func (m *Memory) Name() string {
	if m.Title != "" {
		return m.Title
	}

	return "memory"
}

// Get implements Provider.
func (m *Memory) Get(primary bool) (string, error) {
	return m.Contents[index(primary)], nil
}

// Set implements Provider.
func (m *Memory) Set(primary bool, text string) error {
	m.Contents[index(primary)] = text
	if m.OnSet != nil && !primary {
		m.OnSet(text)
	}

	return nil
}

func index(primary bool) int {
	if primary {
		return 1
	}

	return 0
}

// builtins are the named providers of the clipboard-provider option.
var builtins = map[string]*Custom{
	"pasteboard": {
		name:  "pasteboard",
		Yank:  Command{"pbpaste", nil},
		Paste: Command{"pbcopy", nil},
	},
	"wayland": {
		name:         "wayland",
		Yank:         Command{"wl-paste", []string{"--no-newline"}},
		Paste:        Command{"wl-copy", []string{"--type", "text/plain"}},
		PrimaryYank:  Command{"wl-paste", []string{"-p", "--no-newline"}},
		PrimaryPaste: Command{"wl-copy", []string{"-p", "--type", "text/plain"}},
	},
	"x-clip": {
		name:         "x-clip",
		Yank:         Command{"xclip", []string{"-o", "-selection", "clipboard"}},
		Paste:        Command{"xclip", []string{"-i", "-selection", "clipboard"}},
		PrimaryYank:  Command{"xclip", []string{"-o"}},
		PrimaryPaste: Command{"xclip", []string{"-i"}},
	},
	"x-sel": {
		name:         "x-sel",
		Yank:         Command{"xsel", []string{"-o", "-b"}},
		Paste:        Command{"xsel", []string{"-i", "-b"}},
		PrimaryYank:  Command{"xsel", []string{"-o"}},
		PrimaryPaste: Command{"xsel", []string{"-i"}},
	},
	"tmux": {
		name:  "tmux",
		Yank:  Command{"tmux", []string{"save-buffer", "-"}},
		Paste: Command{"tmux", []string{"load-buffer", "-w", "-"}},
	},
	"termux": {
		name:  "termux",
		Yank:  Command{"termux-clipboard-get", nil},
		Paste: Command{"termux-clipboard-set", nil},
	},
	"win-32-yank": {
		name:  "win-32-yank",
		Yank:  Command{"win32yank.exe", []string{"-o", "--lf"}},
		Paste: Command{"win32yank.exe", []string{"-i", "--crlf"}},
	},
}

// xselWorks reports whether xsel reaches a clipboard, it is installed on
// machines without a display server too.
var xselWorks = func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	return exec.CommandContext(ctx, "xsel", "-o", "-b").Run() == nil
}

// Named returns a builtin provider by its option name.
func Named(name string) (Provider, bool) {
	if name == "termcode" {
		return &Memory{Title: "termcode"}, true
	}

	p, ok := builtins[name]

	return p, ok
}

// Detect picks the provider for this machine, falling
// back to the terminal escape provider.
func Detect() Provider {
	has := func(bin string) bool {
		_, err := exec.LookPath(bin)

		return err == nil
	}

	return detect(runtime.GOOS, os.Getenv, has)
}

func detect(goos string, getenv func(string) string, has func(string) bool) Provider {
	env := func(name string) bool { return getenv(name) != "" }
	// Over ssh the clipboard tools of this machine fill the clipboard of
	// the remote side. The terminal escape reaches the one in front of
	// the user.
	if env("SSH_TTY") || env("SSH_CONNECTION") {
		return &Memory{Title: "termcode"}
	}

	switch goos {
	case "windows":
		if has("win32yank.exe") {
			return builtins["win-32-yank"]
		}
	case "darwin":
		switch {
		case env("TMUX") && has("tmux"):
			return builtins["tmux"]
		case has("pbcopy") && has("pbpaste"):
			return builtins["pasteboard"]
		}
	default:
		switch {
		case has("termux-clipboard-set") && has("termux-clipboard-get"):
			return builtins["termux"]
		case env("TMUX") && has("tmux"):
			return builtins["tmux"]
		case env("WAYLAND_DISPLAY") && has("wl-copy") && has("wl-paste"):
			return builtins["wayland"]
		case env("DISPLAY") && has("xclip"):
			return builtins["x-clip"]
		case env("DISPLAY") && has("xsel") && xselWorks():
			return builtins["x-sel"]
		case has("win32yank.exe"):
			return builtins["win-32-yank"]
		}
	}

	return &Memory{Title: "termcode"}
}
