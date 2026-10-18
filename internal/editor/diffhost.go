package editor

import (
	"context"
	"fmt"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/vcs"
)

// HostConnector opens a host for a workspace without asking anything,
// set by the program, which knows the agents.
type HostConnector func(ctx context.Context, w config.Workspace) (host.Host, error)

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"diff-host", "dh"},
		doc:   "Compare the buffer with the same file, or the path given, on a workspace or ssh host, for configs that drifted apart",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return errArgument("diff-host", "a workspace or ssh host")
			}

			e := cx.Editor
			doc := e.Doc()
			path := doc.Path()
			if len(args) > 1 {
				path = args[1]
			}

			if path == "" {
				return errArgument("diff-host", "a path, the buffer has no file")
			}

			w, err := e.findWorkspace(args[0])
			if err != nil {
				return err
			}

			return e.diffHost(doc, w, path)
		},
	})
}

// diffHost reads path on the host of w in the background and shows its
// differences from the buffer in a read only diff buffer.
func (e *Editor) diffHost(doc *Document, w config.Workspace, path string) error {
	if e.ConnectHost == nil {
		return cmdError("no other hosts in this session")
	}

	h, cached := e.otherHosts[w.Name]
	connect := e.ConnectHost
	text := doc.Text().String()
	name := doc.displayName()
	where := w.Name + ":" + path
	e.SetStatus("reading %s", where)
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		var err error
		if !cached {
			h, err = connect(ctx, w)
		}

		var other []byte
		if err == nil {
			other, err = h.ReadFile(path)
		}

		emit(func(e *Editor) {
			if !cached && h != nil {
				if e.otherHosts == nil {
					e.otherHosts = map[string]host.Host{}
				}

				e.otherHosts[w.Name] = h
			}

			if err != nil {
				e.SetError("%s: %v", where, err)

				return
			}

			body := vcs.Unified(linesWithoutEndings(string(other)), linesWithoutEndings(text), 3)
			if body == "" {
				e.SetStatus("%s is the same as %s", name, where)

				return
			}

			diff := NewDocument("", fmt.Sprintf("--- %s\n+++ %s\n%s", where, name, body))
			diff.readOnly = true
			diff.label = "diff:" + where
			e.addDocument(diff)
			if e.Syntax != nil && e.Syntax.Languages() != nil {
				if lang, ok := e.Syntax.Languages().Get("diff"); ok {
					e.attachLanguage(diff, lang)
				}
			}

			e.Switch(diff.ID, ActionVerticalSplit)
			e.SetStatus("%s differs from %s", name, where)
		})
	})

	return nil
}

// CloseHosts ends the connections opened for other hosts.
func (e *Editor) CloseHosts() {
	for _, h := range e.otherHosts {
		if c, ok := h.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}

	e.otherHosts = nil
}
