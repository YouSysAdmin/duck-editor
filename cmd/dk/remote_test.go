package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/config"
)

func TestParseRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		arg    string
		want   remoteTarget
		remote bool
	}{
		{"box:proj/main.go", remoteTarget{kind: "ssh", dest: "box", path: "proj/main.go"}, true},
		{"dev@box:/etc/hosts", remoteTarget{kind: "ssh", dest: "dev@box", path: "/etc/hosts"}, true},
		{"box:", remoteTarget{kind: "ssh", dest: "box"}, true},
		{"box:~/proj", remoteTarget{kind: "ssh", dest: "box", path: "~/proj"}, true},
		{"ssh://dev@box:2222/srv/app", remoteTarget{kind: "ssh", dest: "box", user: "dev", port: "2222", path: "/srv/app"}, true},
		{"ssh://box/~/proj", remoteTarget{kind: "ssh", dest: "box", path: "~/proj"}, true},
		{"ssh://box", remoteTarget{kind: "ssh", dest: "box"}, true},
		{"docker://app", remoteTarget{kind: "docker", dest: "app"}, true},
		{"docker://node@app/workspaces/web", remoteTarget{kind: "docker", dest: "app", user: "node", path: "/workspaces/web"}, true},
		{
			"kubectl://api-7f9/srv?namespace=prod&container=app&context=eu",
			remoteTarget{kind: "kubectl", dest: "api-7f9", path: "/srv", namespace: "prod", container: "app", context: "eu"},
			true,
		},
		{"main.go", remoteTarget{}, false},
		{"dir/a:b", remoteTarget{}, false},
		{":x", remoteTarget{}, false},
	}
	for _, tt := range tests {
		got, ok := parseRemote(tt.arg)
		if ok != tt.remote || got != tt.want {
			t.Errorf("parseRemote(%q) = %+v %v, want %+v %v", tt.arg, got, ok, tt.want, tt.remote)
		}
	}
}

func TestParseRemoteLocalFileWins(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, "a:b"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := parseRemote("a:b"); ok {
		t.Fatal("an existing local file is not a remote target")
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, arg := range []string{"f.txt:2:abc", "f.txt:-3"} {
		if _, ok := parseRemote(arg); ok {
			t.Errorf("%s is the local f.txt, not a host", arg)
		}
	}
}

func TestSplitPosition(t *testing.T) {
	t.Parallel()

	for arg, want := range map[string][3]any{
		"main.go:12":     {"main.go", 12, 0},
		"dir/a.go:3:7":   {"dir/a.go", 3, 7},
		"box:proj":       {"", 0, 0},
		"main.go":        {"", 0, 0},
		"host:/etc/conf": {"", 0, 0},
	} {
		path, line, col, ok := splitPosition(arg)
		if ok != (want[0] != "") || path != want[0] || line != want[1] || col != want[2] {
			t.Errorf("splitPosition(%q) = %q %d %d %v", arg, path, line, col, ok)
		}
	}
}

// TestClassifyArg keeps remote targets that end in a port or a line
// number apart from file positions.
func TestClassifyArg(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"ssh://user@box:2222", "docker://ctr/app/main.go:12", "box:/etc/hosts"} {
		if _, target := classifyArg(arg); target == nil {
			t.Errorf("%s was taken for a local file", arg)
		}
	}

	file, target := classifyArg("main.go:12:3")
	if target != nil || file != (fileArg{"main.go", 12, 3}) {
		t.Fatalf("main.go:12:3 gave %+v %v", file, target)
	}
}

func TestWorkspaceTarget(t *testing.T) {
	t.Parallel()

	home, _ := os.UserHomeDir()
	target, _, err := workspaceTarget(config.Workspace{Name: "worker", Host: "10.0.0.1", Port: 2222, User: "deploy", Identity: "~/.ssh/w", Path: "/srv/app"})
	if err != nil || target == nil || target.kind != "ssh" || target.dest != "10.0.0.1" || target.port != "2222" || target.user != "deploy" || target.identity != filepath.Join(home, ".ssh/w") || target.path != "/srv/app" {
		t.Fatalf("ssh workspace %+v %v", target, err)
	}

	target, _, _ = workspaceTarget(config.Workspace{Name: "api", Kubectl: "api-1", Namespace: "prod", Path: "/app"})
	if target == nil || target.kind != "kubectl" || target.namespace != "prod" {
		t.Fatalf("pod workspace %+v", target)
	}

	target, local, err := workspaceTarget(config.Workspace{Name: "m", Path: "~/mailyard"})
	if err != nil || target != nil || local != filepath.Join(home, "mailyard") {
		t.Fatalf("local workspace %+v %q %v", target, local, err)
	}

	if _, _, err := workspaceTarget(config.Workspace{Name: "empty"}); err == nil {
		t.Fatal("a workspace without a place should fail")
	}
}
