package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadWorkspaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "workspaces.yaml")
	text := "worker:\n  host: 10.0.0.1\n  port: 2222\n  user: deploy\n  identity: ~/.ssh/worker\n  path: /srv/app\nmailyard:\n  path: /srv/mailyard\napp:\n  docker: app\n  path: /srv\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := LoadWorkspaces(path)
	if err != nil || len(ws) != 3 || ws[0].Name != "app" || ws[2].Name != "worker" || ws[2].Port != 2222 || ws[2].Identity != "~/.ssh/worker" {
		t.Fatalf("workspaces %+v %v", ws, err)
	}

	if ws[2].Where() != "ssh://10.0.0.1:/srv/app" || ws[1].Where() != "/srv/mailyard" || ws[0].Where() != "docker://app/srv" {
		t.Fatalf("where %q %q %q", ws[2].Where(), ws[1].Where(), ws[0].Where())
	}

	for _, bad := range []string{"x:\n  passwd: secret\n", "x:\n  host: a\n  docker: b\n"} {
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}

		if _, err := LoadWorkspaces(path); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}

	if ws, err := LoadWorkspaces(filepath.Join(dir, "missing.yaml")); err != nil || ws != nil {
		t.Fatalf("missing file %v %v", ws, err)
	}
}

func TestSSHHosts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config.d"), 0o755); err != nil {
		t.Fatal(err)
	}

	main := "Include config.d/*\nHost mac-mini\n  HostName 192.168.1.5\nHost *.internal db-? web1 web2\nHost=bastion\n"
	inc := "Host worker\n  User deploy\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.d", "work"), []byte(inc), 0o644); err != nil {
		t.Fatal(err)
	}

	want := []string{"worker", "mac-mini", "web1", "web2", "bastion"}
	if got := SSHHosts(filepath.Join(dir, "config")); !slices.Equal(got, want) {
		t.Fatalf("hosts %q want %q", got, want)
	}
}

// TestProjectWorkspaces lets a project file add and replace workspaces,
// its relative local paths starting at the project root.
func TestProjectWorkspaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	project := filepath.Join(dir, "infra", ".duck")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(user, []byte("worker:\n  host: old\nmine:\n  path: /srv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(project, "workspaces.yaml"), []byte("worker:\n  host: 10.0.0.1\nansible:\n  path: ansible\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := LoadWorkspaceFiles(user, project)
	if err != nil || len(ws) != 3 {
		t.Fatalf("workspaces %+v %v", ws, err)
	}

	byName := map[string]Workspace{}
	for _, w := range ws {
		byName[w.Name] = w
	}

	if w := byName["worker"]; w.Host != "10.0.0.1" || !w.Project {
		t.Fatalf("project did not replace worker: %+v", w)
	}

	if w := byName["ansible"]; w.Path != filepath.Join(dir, "infra", "ansible") {
		t.Fatalf("relative project path %q", w.Path)
	}

	if w := byName["mine"]; w.Project || w.Path != "/srv" {
		t.Fatalf("user workspace %+v", w)
	}
}
