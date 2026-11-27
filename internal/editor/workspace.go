package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// WorkspaceSource lists the workspaces of workspaces.yaml, set by the
// program so tests read no user files.
type WorkspaceSource func() ([]config.Workspace, error)

// HostSource lists the hosts of ~/.ssh/config, set by the program.
type HostSource func() []string

func init() {
	register("workspace_picker", "Pick a workspace of workspaces.yaml to work in", func(cx *Context) { cx.Editor.workspacePicker() })
	register("host_picker", "Pick a host of ~/.ssh/config to work in", func(cx *Context) { cx.Editor.hostPicker() })
	typedCommands = append(typedCommands,
		typedCommand{
			names: []string{"workspace", "ws"},
			doc:   "Switch to a workspace of workspaces.yaml, picked from a list without a name",
			run: func(cx *Context, args []string) error {
				e := cx.Editor
				if len(args) == 0 {
					e.workspacePicker()

					return nil
				}

				w, err := e.findWorkspace(args[0], false)
				if err != nil {
					return err
				}

				return e.switchWorkspace(w)
			},
		},
		typedCommand{
			names: []string{"host", "hosts"},
			doc:   "Switch to a host of ~/.ssh/config, picked from a list without a name",
			run: func(cx *Context, args []string) error {
				e := cx.Editor
				if len(args) == 0 {
					e.hostPicker()

					return nil
				}

				if !slices.Contains(e.sshHosts(), args[0]) {
					return fmt.Errorf("no ssh host named %s", args[0])
				}

				return e.switchWorkspace(config.Workspace{Name: args[0], Host: args[0]})
			},
		},
		typedCommand{
			names: []string{"workspace-add", "ws-add"},
			doc:   "Add the place of this session to workspaces.yaml, named after the working directory without a name, --project writes the .duck/workspaces.yaml of the project",
			run: func(cx *Context, args []string) error {
				name, project := "", false
				for _, a := range args {
					switch {
					case a == "--project" || a == "-p":
						project = true
					case strings.HasPrefix(a, "-"):
						return fmt.Errorf("ws-add: unknown flag %s", a)
					case name != "":
						return errArgument("ws-add", "one name")
					default:
						name = a
					}
				}

				return cx.Editor.addWorkspace(name, project)
			},
		},
	)
}

// sshHosts lists the ssh hosts, none without a source.
func (e *Editor) sshHosts() []string {
	if e.SSHHosts == nil {
		return nil
	}

	return e.SSHHosts()
}

// findWorkspace looks a name up among the workspaces, then the ssh hosts
// when hosts is set.
func (e *Editor) findWorkspace(name string, hosts bool) (config.Workspace, error) {
	if e.Workspaces == nil {
		return config.Workspace{}, cmdError("no workspaces in this session")
	}

	ws, err := e.Workspaces()
	if err != nil {
		return config.Workspace{}, err
	}

	for _, w := range ws {
		if w.Name == name {
			return w, nil
		}
	}

	if hosts && slices.Contains(e.sshHosts(), name) {
		return config.Workspace{Name: name, Host: name}, nil
	}

	if hosts {
		return config.Workspace{}, fmt.Errorf("no workspace or ssh host named %s", name)
	}

	return config.Workspace{}, fmt.Errorf("no workspace named %s", name)
}

// isCurrent tells whether w is where this session works.
func (e *Editor) isCurrent(w config.Workspace) bool {
	p := e.Place
	if p.Name != "" {
		return w.Name == p.Name
	}

	if p.Path == "" && p.Host == "" && p.Docker == "" && p.Kubectl == "" {
		return false
	}

	home, _ := os.UserHomeDir()
	clean := func(w config.Workspace) string {
		if w.Host == "" && w.Docker == "" && w.Kubectl == "" && w.Path != "" {
			w.Path = filepath.Clean(host.ExpandTilde(home, w.Path))
		}

		return w.Where()
	}

	return clean(w) == clean(p)
}

// workspacePicker lists the workspaces of the project when it has any,
// else those of the user. picker_toggle_all lists both. The current
// workspace goes last.
func (e *Editor) workspacePicker() {
	if e.Workspaces == nil {
		e.SetError("no workspaces in this session")

		return
	}

	all, err := e.Workspaces()
	if err != nil {
		e.SetError("%v", err)

		return
	}

	if len(all) == 0 {
		e.SetStatus("no workspaces, add them to %s or with :ws-add", config.WorkspacesPath())

		return
	}

	project := slices.ContainsFunc(all, func(w config.Workspace) bool { return w.Project })
	var shown []config.Workspace
	p := NewPicker("Workspaces", nil, func(cx *Context, item PickerItem, _ PickerAction) {
		if err := cx.Editor.switchWorkspace(shown[item.Index]); err != nil {
			cx.Editor.SetError("%v", err)
		}
	})
	list := func() {
		shown = all
		p.Title = "Workspaces"
		switch {
		case project && p.showAll:
			p.Title += " (all)"
		case project:
			shown = slices.DeleteFunc(slices.Clone(all), func(w config.Workspace) bool { return !w.Project })
			p.Title += " of the project"
		}

		var items []PickerItem
		shown, items = e.workspaceItems(shown, project && p.showAll)
		p.SetItems(items)
	}
	list()

	if project {
		p.relist = func(*Context) { list() }
	}

	e.openPicker(p)
}

// workspaceItems orders ws with the current one last and builds the
// picker rows, tagging project entries when tag is set.
func (e *Editor) workspaceItems(ws []config.Workspace, tag bool) ([]config.Workspace, []PickerItem) {
	current := slices.IndexFunc(ws, e.isCurrent)
	if current >= 0 {
		ws = append(slices.Delete(slices.Clone(ws), current, current+1), ws[current])
	}

	nameWidth, descWidth := 0, 0
	for _, w := range ws {
		nameWidth = max(nameWidth, len(w.Name))
		descWidth = max(descWidth, len(w.Description))
	}

	items := make([]PickerItem, len(ws))
	for i, w := range ws {
		mark := "  "
		if current >= 0 && i == len(ws)-1 {
			mark = "* "
		}

		match := mark + w.Name
		pad := nameWidth - len(w.Name) + 2
		if descWidth > 0 {
			match += strings.Repeat(" ", pad) + w.Description
			pad = descWidth - len(w.Description) + 2
		}

		where := w.Where()
		if tag && w.Project {
			where += "  (project)"
		}

		items[i] = PickerItem{Label: match + strings.Repeat(" ", pad) + where, Match: match, Line: -1, Index: i}
	}

	return ws, items
}

// hostPicker lists the hosts of ~/.ssh/config.
func (e *Editor) hostPicker() {
	if e.SSHHosts == nil {
		e.SetError("no ssh hosts in this session")

		return
	}

	hosts := e.SSHHosts()
	if len(hosts) == 0 {
		e.SetStatus("no hosts in ~/.ssh/config")

		return
	}

	items := make([]PickerItem, len(hosts))
	for i, h := range hosts {
		items[i] = PickerItem{Label: h, Line: -1, Index: i}
	}

	p := NewPicker("SSH hosts", items, func(cx *Context, item PickerItem, _ PickerAction) {
		h := hosts[item.Index]
		if err := cx.Editor.switchWorkspace(config.Workspace{Name: h, Host: h}); err != nil {
			cx.Editor.SetError("%v", err)
		}
	})
	e.openPicker(p)
}

// addWorkspace writes the place of this session to the workspaces file
// of the user, or of the project when project is set. An empty name
// takes the base name of the working directory.
func (e *Editor) addWorkspace(name string, project bool) error {
	file := e.WorkspacesFile
	if project {
		file = e.ProjectWorkspacesFile
	}

	if file == "" && project {
		return cmdError("no project .duck directory in this session")
	}

	if file == "" {
		return cmdError("no workspaces file in this session")
	}

	w := e.Place
	if w.Path == "" && w.Host == "" && w.Docker == "" && w.Kubectl == "" {
		return cmdError("this session has no place to save as a workspace")
	}

	if name == "" {
		name = filepath.Base(e.WorkingDir())
	}

	w.Name, w.Project, w.Path = name, false, e.WorkingDir()
	local := w.Host == "" && w.Docker == "" && w.Kubectl == ""
	if project && local {
		// A local path inside the project is kept relative to its root.
		root := filepath.Dir(filepath.Dir(file))
		if rel, err := filepath.Rel(root, w.Path); err == nil && filepath.IsLocal(rel) {
			w.Path = rel
		}
	}

	if err := config.AddWorkspace(file, w); err != nil {
		return err
	}

	e.Place.Name = name
	e.SetStatus("added workspace %s to %s", name, file)

	return nil
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
