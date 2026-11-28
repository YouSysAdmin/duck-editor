package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/yamlnode"
)

// Task is a named command of a project, run in the background by :task,
// such as a development server.
type Task struct {
	Name string

	// Command runs through the shell.
	Command string

	// Dir is the working directory, relative to the project root, the
	// root when empty.
	Dir string

	// Env adds KEY=value pairs to the environment, sorted by key.
	Env []string

	// Description says what the task does, shown in the picker.
	Description string
}

// taskKeys are the keys a task table may hold.
var taskKeys = []string{"command", "description", "dir", "env"}

// LoadTasks reads a tasks file, a map from name to a command or to a
// table with command, dir, env and description, in file order. A missing
// file has none.
func LoadTasks(f host.File) ([]Task, error) {
	data, err := f.Read()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	tasks, err := parseTasks(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}

	return tasks, nil
}

func parseTasks(data []byte) ([]Task, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	if len(doc.Content) == 0 {
		return nil, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: a map from task name to command", root.Line)
	}

	var tasks []Task
	for k, v := range yamlnode.Pairs(root) {
		t, err := parseTask(k.Value, v)
		if err != nil {
			return nil, fmt.Errorf("line %d: task %s: %w", v.Line, k.Value, err)
		}

		tasks = append(tasks, t)
	}

	return tasks, nil
}

func parseTask(name string, v *yaml.Node) (Task, error) {
	t := Task{Name: name}
	switch v.Kind {
	case yaml.ScalarNode:
		t.Command = v.Value
	case yaml.MappingNode:
		for key := range yamlnode.Pairs(v) {
			if !slices.Contains(taskKeys, key.Value) {
				return t, fmt.Errorf("unknown key %q", key.Value)
			}
		}

		var raw struct {
			Command     string            `yaml:"command"`
			Dir         string            `yaml:"dir"`
			Env         map[string]string `yaml:"env"`
			Description string            `yaml:"description"`
		}
		if err := v.Decode(&raw); err != nil {
			return t, err
		}

		t.Command, t.Dir, t.Description = raw.Command, raw.Dir, raw.Description
		for _, key := range slices.Sorted(maps.Keys(raw.Env)) {
			t.Env = append(t.Env, key+"="+raw.Env[key])
		}
	default:
		return t, errors.New("a command or a table")
	}

	if t.Command == "" {
		return t, errors.New("no command")
	}

	return t, nil
}
