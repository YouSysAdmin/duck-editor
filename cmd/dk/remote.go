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
	"strconv"
	"strings"
	"sync"

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

// workspace describes the target as a workspace working in path.
func (t remoteTarget) workspace(path string) config.Workspace {
	w := config.Workspace{User: t.user, Identity: t.identity, Namespace: t.namespace, Context: t.context, Container: t.container, Path: path}
	switch t.kind {
	case "docker":
		w.Docker = t.dest
	case "kubectl":
		w.Kubectl = t.dest
	default:
		w.Host = t.dest
		w.Port, _ = strconv.Atoi(t.port)
	}

	return w
}

// parseRemote recognizes ssh://[user@]host[:port]/path,
// ssh://[user@]host[:port]:path, docker://[user@]container/path,
// kubectl://pod/path?namespace=ns&container=c&context=ctx and the scp
// style [user@]host:path. The scp style applies only when no local file
// is named by the argument or the part before the colon, and that part
// holds no slash.
func parseRemote(arg string) (remoteTarget, bool) {
	if base, path, ok := sshColonPath(arg); ok {
		t, ok := parseRemoteURL("ssh", base)
		t.path = path

		return t, ok
	}

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

// sshColonPath splits ssh://[user@]host[:port]:path into the URL without
// the path and the path, which is taken as is, ~ and relative paths
// included. Digits right after the host are the port. It reports false
// for the URL form, where the path follows a slash.
func sshColonPath(arg string) (string, string, bool) {
	rest, ok := strings.CutPrefix(arg, "ssh://")
	if !ok {
		return "", "", false
	}

	// The user ends at an @ before any colon or slash.
	start := 0
	if i := strings.IndexAny(rest, "@:/"); i >= 0 && rest[i] == '@' {
		start = i + 1
	}

	end := start
	if strings.HasPrefix(rest[start:], "[") {
		i := strings.IndexByte(rest[start:], ']')
		if i < 0 {
			return "", "", false
		}

		end += i + 1
	} else {
		i := strings.IndexAny(rest[start:], ":/")
		if i < 0 {
			return "", "", false
		}

		end += i
	}

	after, ok := strings.CutPrefix(rest[end:], ":")
	if !ok || end == start {
		return "", "", false
	}

	base := "ssh://" + rest[:end]
	digits := strings.IndexFunc(after, func(r rune) bool { return r < '0' || r > '9' })
	switch {
	case digits < 0 || (digits > 0 && after[digits] == '/'):
		// host:port or host:port/path
		return "", "", false
	case digits > 0 && after[digits] == ':':
		return base + ":" + after[:digits], after[digits+1:], true
	}

	return base, after, true
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
// environment in envMode. progress, when set, reports an agent upload.
func (t remoteTarget) connect(envMode string, progress host.ProgressFunc) *host.Remote {
	agents := filepath.Join(config.CacheDir(), "agent")
	agent := func(goos, goarch string, download bool) (string, error) {
		if download && progress != nil {
			fmt.Fprintf(os.Stderr, "dk: the host has no agent, preparing one for %s/%s\n", goos, goarch)
		}

		return host.AgentBinary(agents, version, goos, goarch, download)
	}

	args := []string{"-env", envMode}
	switch t.kind {
	case "docker":
		return host.NewDocker(t.dest, host.DockerOptions{User: t.user, Agent: agent, Progress: progress, AgentArgs: args})
	case "kubectl":
		return host.NewKubectl(t.dest, host.KubectlOptions{
			Namespace: t.namespace,
			Context:   t.context,
			Container: t.container,
			Agent:     agent,
			Progress:  progress,
			AgentArgs: args,
		})
	}

	dest := t.dest
	if t.user != "" {
		dest = t.user + "@" + dest
	}

	return host.NewSSH(dest, host.SSHOptions{Port: t.port, Identity: t.identity, Agent: agent, Progress: progress, AgentArgs: args})
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

	progress := &uploadProgress{}
	r := target.connect(envMode, progress.report)
	fmt.Fprintf(os.Stderr, "dk: connecting to %s\n", r.Name())
	err = r.Connect(context.Background())
	progress.end()
	if err != nil {
		return err
	}

	defer func() { _ = r.Close() }()

	home, err := r.Home()
	if err != nil {
		return err
	}

	path := host.ExpandTilde(home, target.path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Cwd(), path)
	}

	info, err := r.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err == nil && info.IsDir() {
		start := sessionStart{doc: editor.NewDocument("", ""), explore: true, lang: lang, place: target.workspace(path)}

		return session(r, path, editor.FindWorkspaceConfig(r, path), start)
	}

	dir := filepath.Dir(path)
	start := sessionStart{doc: editor.NewDocument("", ""), lang: lang, place: target.workspace(dir)}
	if doc, err := editor.OpenDocument(r, path); err != nil {
		start.errs = []string{err.Error()}
	} else {
		start.doc = doc
	}

	return session(r, dir, editor.FindWorkspaceConfig(r, dir), start)
}

// uploadProgress prints an agent upload on one terminal line.
type uploadProgress struct {
	mu   sync.Mutex
	open bool
}

func (p *uploadProgress) report(sent, size int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	const mb = 1 << 20
	pct := int64(100)
	if size > 0 {
		pct = sent * 100 / size
	}

	fmt.Fprintf(os.Stderr, "\r\033[Kdk: uploading the agent %.1f/%.1f MB %d%%", float64(sent)/mb, float64(size)/mb, pct)
	p.open = true
	if sent >= size {
		fmt.Fprintln(os.Stderr)
		p.open = false
	}
}

// end finishes a progress line an interrupted upload left open.
func (p *uploadProgress) end() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.open {
		fmt.Fprintln(os.Stderr)
		p.open = false
	}
}
