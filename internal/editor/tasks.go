package editor

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

func init() {
	register("task_picker", "Pick a task of .duck/tasks.yaml to start in the background or to show its output", func(cx *Context) {
		if err := cx.Editor.taskPicker(); err != nil {
			cx.Editor.SetError("%v", err)
		}
	})
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"task"},
		doc:   "Start a task of .duck/tasks.yaml in the background, such as a development server. Without a name, pick one from the tasks of the project",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			if len(args) == 0 {
				return e.taskPicker()
			}

			t, err := e.findTask(args[0])
			if err != nil {
				return err
			}

			e.startTask(t)

			return nil
		},
	}, typedCommand{
		names: []string{"task-stop"},
		doc:   "Stop a running task. Without a name, the task of this buffer",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			doc, err := e.taskDocArg("task-stop", args)
			if err != nil {
				return err
			}

			if doc.tail.cancel == nil {
				e.SetStatus("task %s is not running", doc.tail.task)

				return nil
			}

			e.stopTail(doc)
			e.SetStatus("task %s stopped", doc.tail.task)

			return nil
		},
	}, typedCommand{
		names: []string{"task-restart"},
		doc:   "Stop a task and start it again with its command as the tasks file has it now. Without a name, the task of this buffer",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			name := ""
			if len(args) > 0 {
				name = args[0]
			} else if doc := e.Doc(); doc.tail != nil && doc.tail.task != "" {
				name = doc.tail.task
			}

			if name == "" {
				return errArgument("task-restart", "a task name")
			}

			t, err := e.findTask(name)
			if err != nil {
				return err
			}

			if doc := e.taskDoc(name); doc != nil {
				e.stopTail(doc)
			}

			e.startTask(t)

			return nil
		},
	}, typedCommand{
		names: []string{"task-output"},
		doc:   "Show the output of a task in a read only buffer",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			if len(args) == 0 {
				return errArgument("task-output", "a task name")
			}

			doc := e.taskDoc(args[0])
			if doc == nil {
				return cmdError("task " + args[0] + " has not run")
			}

			e.Switch(doc.ID, ActionReplace)

			return nil
		},
	})
}

// loadTasks reads the tasks of the project and keeps their names for
// completion.
func (e *Editor) loadTasks() ([]config.Task, error) {
	if e.TasksFile.Path == "" {
		return nil, cmdError("no project tasks, add them to .duck/tasks.yaml")
	}

	tasks, err := config.LoadTasks(e.TasksFile)
	if err != nil {
		return nil, err
	}

	e.taskNames = make([]string, 0, len(tasks))
	for _, t := range tasks {
		e.taskNames = append(e.taskNames, t.Name)
	}

	return tasks, nil
}

// findTask returns the task of the project named name.
func (e *Editor) findTask(name string) (config.Task, error) {
	tasks, err := e.loadTasks()
	if err != nil {
		return config.Task{}, err
	}

	i := slices.IndexFunc(tasks, func(t config.Task) bool { return t.Name == name })
	if i < 0 {
		return config.Task{}, cmdError("no task " + name + " in " + e.TasksFile.Path)
	}

	return tasks[i], nil
}

// taskNameList returns the task names for completion, read once.
func (e *Editor) taskNameList() []string {
	if e.taskNames == nil && e.TasksFile.Path != "" {
		_, _ = e.loadTasks()
	}

	return e.taskNames
}

// taskDoc returns the output buffer of a task, nil before it ran.
func (e *Editor) taskDoc(name string) *Document {
	for _, doc := range e.documentsByID() {
		if doc.tail != nil && doc.tail.task == name {
			return doc
		}
	}

	return nil
}

// taskDocArg returns the output buffer of the task named in args, or of
// the current buffer.
func (e *Editor) taskDocArg(cmd string, args []string) (*Document, error) {
	if len(args) == 0 {
		if doc := e.Doc(); doc.tail != nil && doc.tail.task != "" {
			return doc, nil
		}

		return nil, errArgument(cmd, "a task name")
	}

	doc := e.taskDoc(args[0])
	if doc == nil {
		return nil, cmdError("task " + args[0] + " has not run")
	}

	return doc, nil
}

// RunningTasks counts the tasks whose process runs.
func (e *Editor) RunningTasks() int {
	n := 0
	for _, doc := range e.Docs {
		if doc.tail != nil && doc.tail.task != "" && doc.tail.cancel != nil {
			n++
		}
	}

	return n
}

// taskRoot is the directory holding the .duck directory of the tasks.
func (e *Editor) taskRoot() string {
	return filepath.Dir(filepath.Dir(e.TasksFile.Path))
}

// startTask runs a task in the background, its output goes to a buffer
// that is not shown. A running task is left alone.
func (e *Editor) startTask(t config.Task) {
	doc := e.taskDoc(t.Name)
	if doc != nil && doc.tail.cancel != nil {
		e.SetStatus("task %s is running", t.Name)

		return
	}

	shell := e.shellCommand()
	argv := append(slices.Clone(shell.argv), t.Command)
	dir := e.taskRoot()
	if t.Dir != "" {
		dir = t.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(e.taskRoot(), dir)
		}
	}

	command := func(int) host.Command {
		return host.Command{Name: argv[0], Args: argv[1:], Dir: dir, Env: t.Env, MergeStderr: true}
	}

	if doc == nil {
		e.openFollowDoc(&tailState{title: "task:" + t.Name, task: t.Name, host: e.Host, command: command}, "")
		e.SetStatus("task %s started", t.Name)

		return
	}

	doc.tail.command = command
	if doc.Text().Len() > 0 {
		e.appendShown(doc, "-- "+t.Command+"\n", e.tailLines())
	}

	e.startTail(doc, 0)
	e.SetStatus("task %s started", t.Name)
}

// taskPicker lists the tasks of the project with their state. Enter
// starts or stops a task and keeps the picker open, picker_show_output
// shows the output of a task that ran.
func (e *Editor) taskPicker() error {
	tasks, err := e.loadTasks()
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		return cmdError("no tasks in " + e.TasksFile.Path)
	}

	p := NewPicker("Tasks", e.taskItems(tasks), func(cx *Context, item PickerItem, _ PickerAction) {
		e := cx.Editor
		t := tasks[item.Index]
		if doc := e.taskDoc(t.Name); doc != nil && doc.tail.cancel != nil {
			e.stopTail(doc)
			e.SetStatus("task %s stopped", t.Name)

			return
		}

		e.startTask(t)
	})
	p.stay = func(PickerItem) bool { return true }
	p.output = func(cx *Context, item PickerItem) {
		e := cx.Editor
		name := tasks[item.Index].Name
		doc := e.taskDoc(name)
		if doc == nil {
			e.SetStatus("task %s has not run", name)

			return
		}

		e.closePicker()
		e.Switch(doc.ID, ActionReplace)
	}
	p.remove = func(cx *Context, item PickerItem) bool {
		if doc := cx.Editor.taskDoc(tasks[item.Index].Name); doc != nil && doc.tail.cancel != nil {
			cx.Editor.stopTail(doc)
			cx.Editor.SetStatus("task %s stopped", doc.tail.task)
		}

		return false
	}
	p.refresh = func(e *Editor) {
		row := p.selected
		p.SetItems(e.taskItems(tasks))
		p.selected = max(min(row, len(p.matches)-1), 0)
	}
	p.rerun = func(cx *Context) { _ = cx.Editor.taskPicker() }
	e.openPicker(p)

	return nil
}

// refreshPicker rebuilds the items of the open picker when it shows a
// state that changed.
func (e *Editor) refreshPicker() {
	if e.Picker != nil && e.Picker.refresh != nil {
		e.Picker.refresh(e)
	}
}

// taskItems describes tasks for the picker, the output buffer previews a
// task that ran.
func (e *Editor) taskItems(tasks []config.Task) []PickerItem {
	items := make([]PickerItem, 0, len(tasks))
	width := 0
	for i, t := range tasks {
		about := t.Description
		if about == "" {
			about = t.Command
		}

		item := PickerItem{Label: t.Name, Line: -1, Index: i, Note: about, Tag: "idle"}
		if doc := e.taskDoc(t.Name); doc != nil {
			item.DocID = doc.ID
			item.Line = max(doc.LineCount()-1, 0)
			item.Tag = doc.tail.exit
			switch {
			case doc.tail.cancel != nil:
				item.Tag, item.TagKind = "running", ui.TagGood
			case strings.HasPrefix(item.Tag, "exited"), item.Tag == "failed":
				item.TagKind = ui.TagBad
			}
		} else {
			item.Text = []string{t.Command}
			if t.Dir != "" {
				item.Text = append(item.Text, "in "+t.Dir)
			}

			if len(t.Env) > 0 {
				item.Text = append(item.Text, strings.Join(t.Env, " "))
			}
		}

		width = max(width, len(item.Tag))
		items = append(items, item)
	}

	// Padded tags keep the notes in one column.
	for i := range items {
		items[i].Tag += strings.Repeat(" ", width-len(items[i].Tag))
	}

	return items
}
