package editor

import (
	"context"
	"strconv"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// logSource is a kind of log stream with a picker for what to follow.
type logSource struct {
	// prefix starts the buffer title, such as journal.
	prefix string

	// list runs on the host and prints one target per line, the first
	// field names it, the rest describes it.
	list func(args []string) host.Command

	// stream follows target, empty for all of it, beginning with the
	// last lines.
	stream func(target string, args []string, lines int) host.Command
}

var (
	journalSource = logSource{
		prefix: "journal",
		list: func([]string) host.Command {
			return shellScript("exec systemctl list-units --type=service --all --no-legend --plain --no-pager 2>&1")
		},
		stream: func(unit string, args []string, lines int) host.Command {
			script := `n=$1 u=$2; shift 2
if [ -n "$u" ]; then set -- -u "$u" "$@"; fi
exec journalctl --no-pager -f -o short-iso -n "$n" "$@" 2>&1`

			return shellScript(script, append([]string{strconv.Itoa(lines), unit}, args...)...)
		},
	}
	dockerSource = logSource{
		prefix: "docker",
		list: func([]string) host.Command {
			return shellScript(`exec docker ps --format '{{.Names}} {{.Image}} {{.Status}}' 2>&1`)
		},
		stream: func(name string, args []string, lines int) host.Command {
			script := `n=$1 c=$2; shift 2; exec docker logs -f --tail "$n" "$@" "$c" 2>&1`

			return shellScript(script, append([]string{strconv.Itoa(lines), name}, args...)...)
		},
	}
	kubeSource = logSource{
		prefix: "k8s",
		list: func(args []string) host.Command {
			return shellScript(`exec kubectl get pods --no-headers "$@" 2>&1`, args...)
		},
		stream: func(pod string, args []string, lines int) host.Command {
			script := `n=$1 p=$2; shift 2; exec kubectl logs -f --tail "$n" "$@" "$p" 2>&1`

			return shellScript(script, append([]string{strconv.Itoa(lines), pod}, args...)...)
		},
	}
)

// shellScript runs a POSIX shell script on the host with args as its
// positional parameters, so they need no quoting.
func shellScript(script string, args ...string) host.Command {
	return host.Command{Name: "sh", Args: append([]string{"-c", script, "sh"}, args...)}
}

func init() {
	journal := func(asRoot bool) func(cx *Context, args []string) error {
		return func(cx *Context, args []string) error {
			e := cx.Editor
			open := func(e *Editor) error {
				e.openLogs(journalSource, args, asRoot)

				return nil
			}
			if asRoot {
				return e.withSudo(e.Host, open)
			}

			return open(e)
		}
	}
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"journal"},
		doc:   "Follow the systemd journal of a unit, picked from a list without one, or of everything with only journalctl options such as -p err",
		run:   journal(false),
	}, typedCommand{
		names: []string{"sudo-journal"},
		doc:   "Follow the systemd journal as root, for the messages of every user and the system",
		run:   journal(true),
	}, typedCommand{
		names: []string{"docker-logs", "dlogs"},
		doc:   "Follow the logs of a container, picked from the running ones without a name",
		run: func(cx *Context, args []string) error {
			cx.Editor.openLogs(dockerSource, args, false)

			return nil
		},
	}, typedCommand{
		names: []string{"kube-logs", "klogs"},
		doc:   "Follow the logs of a pod, picked from a list without a name. kubectl options such as -n ns or -c container pass through",
		run: func(cx *Context, args []string) error {
			cx.Editor.openLogs(kubeSource, args, false)

			return nil
		},
	})
}

// openLogs follows the target named first in args, or picks one from the
// list of the source. The other args go to the program. A journal
// started with options only follows all of it.
func (e *Editor) openLogs(src logSource, args []string, asRoot bool) {
	var target string
	opts := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, opts = args[0], args[1:]
	}

	if target != "" || src.prefix == "journal" && len(opts) > 0 {
		e.followLogs(src, target, opts, asRoot)

		return
	}

	h := e.Host
	cmd := src.list(opts)
	e.SetStatus("listing %s targets", src.prefix)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		out, _, err := h.Run(ctx, cmd)
		emit(func(e *Editor) {
			var items []PickerItem
			for line := range strings.Lines(string(out)) {
				if fields := strings.Fields(line); len(fields) > 0 {
					items = append(items, PickerItem{Label: strings.Join(fields, " "), Line: -1, Index: len(items)})
				}
			}

			if len(items) == 0 {
				if err != nil {
					e.SetError("%s: %v", src.prefix, firstLines(strings.TrimSpace(string(out)), 1))
				} else {
					e.SetStatus("nothing to follow")
				}

				return
			}

			e.SetStatus("")
			p := NewPicker("Follow "+src.prefix, items, func(cx *Context, item PickerItem, _ PickerAction) {
				cx.Editor.followLogs(src, strings.Fields(item.Label)[0], opts, asRoot)
			})
			e.openPicker(p)
		})
	})
}

// followLogs opens the buffer that follows one log stream.
func (e *Editor) followLogs(src logSource, target string, opts []string, asRoot bool) {
	title := src.prefix + ":" + target
	if target == "" {
		title = src.prefix
	}

	if asRoot {
		title += " as root"
	}

	t := &tailState{title: title, host: e.Host, command: func(lines int) host.Command {
		cmd := src.stream(target, opts, lines)
		if asRoot {
			cmd.Args = append([]string{"-n", cmd.Name}, cmd.Args...)
			cmd.Name = sudoCommand
		}

		return cmd
	}}
	if src.prefix == "journal" && !asRoot {
		// journald hides the system messages from users outside its
		// groups and says so on the first lines.
		t.notice = func(e *Editor, text string) {
			if strings.Contains(text, "You are currently not seeing messages") {
				e.SetStatus("the journal hides system messages from this user, :sudo-journal %s shows them", strings.Join(append([]string{target}, opts...), " "))
			}
		}
	}

	e.openFollow(t, "")
}
