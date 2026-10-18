package editor

import (
	"fmt"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/config"
)

// WorkspaceSource lists the workspaces of workspaces.yaml and the hosts
// of ~/.ssh/config, set by the program so tests read no user files.
type WorkspaceSource func() ([]config.Workspace, []string, error)

func init() {
	register("workspace_picker", "Pick a workspace or an ssh host to work in", func(cx *Context) { cx.Editor.workspacePicker() })
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"workspace", "ws"},
		doc:   "Switch to a workspace of workspaces.yaml or a host of ~/.ssh/config, picked from a list without a name",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			if len(args) == 0 {
				e.workspacePicker()

				return nil
			}

			w, err := e.findWorkspace(args[0])
			if err != nil {
				return err
			}

			return e.switchWorkspace(w)
		},
	})
}

// findWorkspace looks a name up among the workspaces, then the ssh hosts.
func (e *Editor) findWorkspace(name string) (config.Workspace, error) {
	if e.Workspaces == nil {
		return config.Workspace{}, cmdError("no workspaces in this session")
	}

	ws, hosts, err := e.Workspaces()
	if err != nil {
		return config.Workspace{}, err
	}

	for _, w := range ws {
		if w.Name == name {
			return w, nil
		}
	}

	for _, h := range hosts {
		if h == name {
			return config.Workspace{Name: h, Host: h}, nil
		}
	}

	return config.Workspace{}, fmt.Errorf("no workspace or ssh host named %s", name)
}

// workspacePicker lists the workspaces, then the ssh hosts that are not
// one already.
func (e *Editor) workspacePicker() {
	if e.Workspaces == nil {
		e.SetError("no workspaces in this session")

		return
	}

	ws, hosts, err := e.Workspaces()
	if err != nil {
		e.SetError("%v", err)

		return
	}

	known := map[string]bool{}
	all := make([]config.Workspace, 0, len(ws)+len(hosts))
	for _, w := range ws {
		known[w.Name] = true
		all = append(all, w)
	}

	for _, h := range hosts {
		if !known[h] {
			all = append(all, config.Workspace{Name: h, Host: h})
		}
	}

	if len(all) == 0 {
		e.SetStatus("no workspaces, add them to %s", config.WorkspacesPath())

		return
	}

	width := 0
	for _, w := range all {
		width = max(width, len(w.Name))
	}

	items := make([]PickerItem, len(all))
	for i, w := range all {
		where := w.Where()
		switch {
		case !known[w.Name]:
			where = "ssh host"
		case w.Project:
			where += "  (project)"
		}

		items[i] = PickerItem{Label: w.Name + strings.Repeat(" ", width-len(w.Name)+2) + where, Match: w.Name, Line: -1, Index: i}
	}

	p := NewPicker("Workspaces", items, func(cx *Context, item PickerItem, _ PickerAction) {
		if err := cx.Editor.switchWorkspace(all[item.Index]); err != nil {
			cx.Editor.SetError("%v", err)
		}
	})
	e.openPicker(p)
}

// switchWorkspace ends this session for another workspace, which the
// program opens next. Unsaved buffers stop it.
func (e *Editor) switchWorkspace(w config.Workspace) error {
	if err := buffersRemaining(e); err != nil {
		return err
	}

	e.nextWorkspace = &w
	e.quit = true

	return nil
}

// TakeNextWorkspace returns the workspace the session ended for, nil
// when it simply ended.
func (e *Editor) TakeNextWorkspace() *config.Workspace {
	w := e.nextWorkspace
	e.nextWorkspace = nil

	return w
}
