package host

import (
	"slices"
	"strings"
	"testing"
)

func TestTransportCommandLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv func(bool, string) []string
		want []string
	}{
		{
			"ssh",
			sshArgv("dev@box", SSHOptions{Port: "2222"}),
			[]string{"ssh", "-T", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3", "-o", "BatchMode=yes", "-p", "2222", "dev@box", "--", `sh -c 'echo '\''hi'\'''`},
		},
		{
			"docker",
			dockerArgv("app", DockerOptions{User: "node"}),
			[]string{"docker", "exec", "-i", "-u", "node", "app", "sh", "-c", "echo 'hi'"},
		},
		{
			"kubectl",
			kubectlArgv("api", KubectlOptions{Context: "eu", Namespace: "prod", Container: "web"}),
			[]string{"kubectl", "exec", "-i", "--context", "eu", "-n", "prod", "-c", "web", "api", "--", "sh", "-c", "echo 'hi'"},
		},
	}
	for _, tt := range tests {
		if got := tt.argv(false, "echo 'hi'"); !slices.Equal(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

func TestStartScriptWithoutAgents(t *testing.T) {
	t.Parallel()

	d := &commandDialer{ids: map[string]fileID{}, agent: func(goos, goarch string, _ bool) (string, error) {
		return "testdata/missing-" + goos + "-" + goarch, nil
	}}
	script := d.startScript()
	// No local binary exists, so no system gets an agent id.
	if strings.Contains(script, `") id=`) || !strings.Contains(script, `exec "$p" agent`) || !strings.Contains(script, "DUCK-NEED") {
		t.Fatalf("start script:\n%s", script)
	}
}

func TestStartScriptPassesAgentArgs(t *testing.T) {
	t.Parallel()

	d := &commandDialer{ids: map[string]fileID{}, agentArgs: []string{"-env", "login"}}
	if script := d.startScript(); !strings.Contains(script, `exec "$p" agent '-env' 'login'; fi`) {
		t.Fatalf("start script:\n%s", script)
	}
}

// TestTerminalCommand asks every transport for a terminal.
func TestTerminalCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		r    *Remote
		want []string
	}{
		{NewSSH("u@box", SSHOptions{Port: "2222"}), []string{"ssh", "-t", "-p", "2222", "u@box", "--", "sh -c 'lazygit'"}},
		{NewDocker("app", DockerOptions{User: "dev"}), []string{"docker", "exec", "-it", "-u", "dev", "app", "sh", "-c", "lazygit"}},
		{NewKubectl("pod", KubectlOptions{Namespace: "ns"}), []string{"kubectl", "exec", "-it", "-n", "ns", "pod", "--", "sh", "-c", "lazygit"}},
	}
	for _, c := range cases {
		if got := c.r.TerminalCommand("lazygit"); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q want %q", c.r.Name(), got, c.want)
		}
	}
}

// TestFailureExplainsBrokenAgent says why an installed agent does not
// start instead of the bare not found of the shell.
func TestFailureExplainsBrokenAgent(t *testing.T) {
	t.Parallel()

	d := &commandDialer{name: "docker ctr"}
	stderr := &tailBuffer{max: 4 << 10}
	_, _ = stderr.Write([]byte("sh: exec: line 8: /root/.cache/duck/agent/2cb800bd69fd5fab/dk: not found\n"))
	if err := d.failure(stderr); !strings.Contains(err.Error(), "C library") {
		t.Fatalf("error %v", err)
	}

	plain := &tailBuffer{max: 4 << 10}
	_, _ = plain.Write([]byte("Error: No such container: ctr\n"))
	if err := d.failure(plain); err.Error() != "docker ctr: Error: No such container: ctr" {
		t.Fatalf("error %v", err)
	}
}
