package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// remoteTarget is a file or directory on another machine or in a
// container.
type remoteTarget struct {
	// kind is ssh, docker or kubectl.
	kind string

	// dest is the ssh destination, the container or the pod.
	dest string
	port string
	user string

	// identity is the ssh key file, passed with -i.
	identity string

	// namespace, context and container select a pod container.
	namespace string
	context   string
	container string

	// path is absolute, starts with ~ for the home directory, is
	// relative to the directory the agent starts in, or is empty for
	// that directory itself.
	path string
}

// parseRemote recognizes ssh://[user@]host[:port]/path,
// docker://[user@]container/path,
// kubectl://pod/path?namespace=ns&container=c&context=ctx and the scp
// style [user@]host:path. The scp style applies only when no local file
// is named by the argument or the part before the colon, and that part
// holds no slash.
func parseRemote(arg string) (remoteTarget, bool) {
	for _, kind := range []string{"ssh", "docker", "kubectl"} {
		if strings.HasPrefix(arg, kind+"://") {
			return parseRemoteURL(kind, arg)
		}
	}

	dest, path, ok := strings.Cut(arg, ":")
	if !ok || dest == "" || strings.Contains(dest, "/") {
		return remoteTarget{}, false
	}

	// A local file wins, also with a position after it, as f.txt:2:x.
	for _, local := range []string{arg, dest} {
		if _, err := os.Stat(local); err == nil {
			return remoteTarget{}, false
		}
	}

	return remoteTarget{kind: "ssh", dest: dest, path: path}, true
}

func parseRemoteURL(kind, arg string) (remoteTarget, bool) {
	u, err := url.Parse(arg)
	if err != nil || u.Hostname() == "" {
		return remoteTarget{}, false
	}

	t := remoteTarget{kind: kind, dest: u.Hostname(), port: u.Port(), path: u.Path}
	if u.User != nil {
		t.user = u.User.Username()
	}

	// A path after /~ is below the home directory.
	if rest, ok := strings.CutPrefix(t.path, "/~"); ok && (rest == "" || rest[0] == '/') {
		t.path = "~" + rest
	}

	q := u.Query()
	t.namespace, t.context, t.container = q.Get("namespace"), q.Get("context"), q.Get("container")

	return t, true
}

// connect creates the host of the target, the agent takes over the user
// environment in envMode.
func (t remoteTarget) connect(envMode string) *host.Remote {
	agents := filepath.Join(config.CacheDir(), "agent")
	agent := func(goos, goarch string, download bool) (string, error) {
		return host.AgentBinary(agents, version, goos, goarch, download)
	}

	args := []string{"-env", envMode}
	switch t.kind {
	case "docker":
		return host.NewDocker(t.dest, host.DockerOptions{User: t.user, Agent: agent, AgentArgs: args})
	case "kubectl":
		return host.NewKubectl(t.dest, host.KubectlOptions{
			Namespace: t.namespace,
			Context:   t.context,
			Container: t.container,
			Agent:     agent,
			AgentArgs: args,
		})
	}

	dest := t.dest
	if t.user != "" {
		dest = t.user + "@" + dest
	}

	return host.NewSSH(dest, host.SSHOptions{Port: t.port, Identity: t.identity, Agent: agent, AgentArgs: args})
}

// remoteEnvMode reads remote-env from the user config, which is all the
// config there is before the connection. The project config is on the
// other side.
func remoteEnvMode() (string, error) {
	cfg, err := config.LoadFiles(filepath.Join(config.Dir(), "config.yaml"))
	if err != nil {
		return "", err
	}

	if !slices.Contains(host.EnvModes, cfg.Editor.RemoteEnv) {
		return "", fmt.Errorf("remote-env is %q, use one of %s", cfg.Editor.RemoteEnv, strings.Join(host.EnvModes, ", "))
	}

	return cfg.Editor.RemoteEnv, nil
}

// editRemote connects to the machine of target and runs the editor
// there: files, searches, git, shell commands and language servers live
// on the remote side, the editor itself and its config stay here.
func editRemote(target remoteTarget, lang string) error {
	envMode, err := remoteEnvMode()
	if err != nil {
		return err
	}

	r := target.connect(envMode)
	fmt.Fprintf(os.Stderr, "dk: connecting to %s\n", r.Name())
	if err := r.Connect(context.Background()); err != nil {
		return err
	}

	defer func() { _ = r.Close() }()

	home, err := r.Home()
	if err != nil {
		return err
	}

	path := target.path
	switch {
	case path == "~" || strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[1:])
	case !filepath.IsAbs(path):
		path = filepath.Join(r.Cwd(), path)
	}

	info, err := r.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err == nil && info.IsDir() {
		return session(r, path, editor.FindWorkspaceConfig(r, path), sessionStart{doc: editor.NewDocument("", ""), explore: true, lang: lang})
	}

	dir := filepath.Dir(path)
	start := sessionStart{doc: editor.NewDocument("", ""), lang: lang}
	if doc, err := editor.OpenDocument(r, path); err != nil {
		start.errs = []string{err.Error()}
	} else {
		start.doc = doc
	}

	return session(r, dir, editor.FindWorkspaceConfig(r, dir), start)
}
