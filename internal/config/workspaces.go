package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Workspace is a place to work: a directory on this machine, on a host
// reached with ssh, in a container or in a pod.
type Workspace struct {
	Name string `yaml:"-"`

	// Host is an ssh destination, an address or a Host of ~/.ssh/config.
	// Port, User and Identity pass to ssh.
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Identity string `yaml:"identity"`

	// Docker names a running container, Kubectl a pod with the other
	// fields choosing its namespace, context and container.
	Docker    string `yaml:"docker"`
	Kubectl   string `yaml:"kubectl"`
	Namespace string `yaml:"namespace"`
	Context   string `yaml:"context"`
	Container string `yaml:"container"`

	// Path is the working directory, the home directory or the start
	// directory of the container when empty. A relative local path in a
	// project file starts at the project root.
	Path string `yaml:"path"`

	// Project is set for a workspace of a project .duck directory.
	Project bool `yaml:"-"`
}

// Where describes the place of a workspace for a picker.
func (w Workspace) Where() string {
	switch {
	case w.Docker != "":
		return "docker://" + w.Docker + w.Path
	case w.Kubectl != "":
		return "kubectl://" + w.Kubectl + w.Path
	case w.Host != "":
		return "ssh://" + w.Host + ":" + w.Path
	}

	return w.Path
}

// WorkspacesPath is the workspaces file of the user.
func WorkspacesPath() string {
	return filepath.Join(Dir(), "workspaces.yaml")
}

// LoadWorkspaceFiles reads the workspaces of the user file and of a
// project .duck directory, empty for none. A project workspace replaces
// one of the user with the same name.
func LoadWorkspaceFiles(user, projectDir string) ([]Workspace, error) {
	ws, err := LoadWorkspaces(user)
	if err != nil || projectDir == "" {
		return ws, err
	}

	project, err := LoadWorkspaces(filepath.Join(projectDir, "workspaces.yaml"))
	if err != nil {
		return nil, err
	}

	root := filepath.Dir(projectDir)
	for _, p := range project {
		p.Project = true
		local := p.Host == "" && p.Docker == "" && p.Kubectl == ""
		if local && p.Path != "" && !filepath.IsAbs(p.Path) && !strings.HasPrefix(p.Path, "~") {
			p.Path = filepath.Join(root, p.Path)
		}

		ws = slices.DeleteFunc(ws, func(w Workspace) bool { return w.Name == p.Name })
		ws = append(ws, p)
	}

	slices.SortFunc(ws, func(a, b Workspace) int { return strings.Compare(a.Name, b.Name) })

	return ws, nil
}

// LoadWorkspaces reads a workspaces file, a map from name to workspace,
// sorted by name. A missing file has none. Unknown keys are errors.
func LoadWorkspaces(path string) ([]Workspace, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var raw map[string]Workspace
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	out := make([]Workspace, 0, len(raw))
	for name, w := range raw {
		w.Name = name
		kinds := 0
		for _, set := range []bool{w.Host != "", w.Docker != "", w.Kubectl != ""} {
			if set {
				kinds++
			}
		}

		if kinds > 1 {
			return nil, fmt.Errorf("%s: workspace %s names more than one of host, docker and kubectl", path, name)
		}

		out = append(out, w)
	}

	slices.SortFunc(out, func(a, b Workspace) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

// SSHHosts lists the Host names of an ssh config file and the files it
// includes, without patterns, in file order.
func SSHHosts(path string) []string {
	var out []string
	seen := map[string]bool{}
	var read func(path string, depth int)
	read = func(path string, depth int) {
		f, err := os.Open(path)
		if err != nil || depth > 8 {
			return
		}

		defer func() { _ = f.Close() }()

		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(strings.ReplaceAll(sc.Text(), "=", " "))
			if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
				continue
			}

			switch strings.ToLower(fields[0]) {
			case "host":
				for _, name := range fields[1:] {
					if !strings.ContainsAny(name, "*?!") && !seen[name] {
						seen[name] = true
						out = append(out, name)
					}
				}
			case "include":
				for _, pattern := range fields[1:] {
					// Relative includes are relative to ~/.ssh.
					if strings.HasPrefix(pattern, "~/") {
						home, _ := os.UserHomeDir()
						pattern = filepath.Join(home, pattern[2:])
					} else if !filepath.IsAbs(pattern) {
						pattern = filepath.Join(filepath.Dir(path), pattern)
					}

					matches, _ := filepath.Glob(pattern)
					for _, m := range matches {
						read(m, depth+1)
					}
				}
			}
		}
	}
	read(path, 0)

	return out
}
