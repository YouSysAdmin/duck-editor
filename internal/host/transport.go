package host

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AgentFunc returns the local dk binary to install for a remote system,
// goos and goarch in Go terms. It is called with download false while
// the connection is being set up and true when the remote side has no
// agent yet.
type AgentFunc func(goos, goarch string, download bool) (string, error)

// ProgressFunc reports an agent upload, sent of size bytes. It is called
// from a background goroutine.
type ProgressFunc func(sent, size int64)

// SSHOptions configure a host reached with the ssh command.
type SSHOptions struct {
	// Port is passed with -p, Identity with -i when set.
	Port     string
	Identity string
	Agent    AgentFunc
	Progress ProgressFunc

	// AgentArgs follow "dk agent" on the remote side.
	AgentArgs []string
}

// NewSSH returns a host reached by running the ssh command, so the
// user's ssh config, keys and agent apply. dest is a host name or
// user@host. The host is named ssh://dest.
func NewSSH(dest string, opts SSHOptions) *Remote {
	d := &commandDialer{name: "ssh " + dest, argv: sshArgv(dest, opts), agent: opts.Agent, progress: opts.Progress, agentArgs: opts.AgentArgs, ids: map[string]fileID{}}
	name := "ssh://" + dest
	if opts.Port != "" {
		name += ":" + opts.Port
	}

	r := NewRemote(name, d.dial)
	r.terminal = func(script string) []string {
		args := append([]string{"ssh", "-t"}, opts.flags()...)

		return append(args, dest, "--", "sh -c "+ShellQuote(script))
	}

	return r
}

// flags are the ssh options for the port and the identity.
func (o SSHOptions) flags() []string {
	var args []string
	if o.Port != "" {
		args = append(args, "-p", o.Port)
	}

	if o.Identity != "" {
		args = append(args, "-i", o.Identity)
	}

	return args
}

func sshArgv(dest string, opts SSHOptions) func(bool, string) []string {
	return func(interactive bool, script string) []string {
		args := []string{"ssh", "-T", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3"}
		if !interactive {
			args = append(args, "-o", "BatchMode=yes")
		}

		args = append(args, opts.flags()...)

		// ssh hands the command to the login shell of the user as one
		// string, quoted here for a POSIX shell.
		return append(args, dest, "--", "sh -c "+ShellQuote(script))
	}
}

// DockerOptions configure a host that is a running container.
type DockerOptions struct {
	// User runs the agent as this user, the container default when empty.
	User      string
	Agent     AgentFunc
	Progress  ProgressFunc
	AgentArgs []string
}

// NewDocker returns a host that is a running container, reached with
// docker exec. The host is named docker://container.
func NewDocker(container string, opts DockerOptions) *Remote {
	d := &commandDialer{name: "docker " + container, argv: dockerArgv(container, opts), agent: opts.Agent, progress: opts.Progress, agentArgs: opts.AgentArgs, ids: map[string]fileID{}}

	r := NewRemote("docker://"+container, d.dial)
	r.terminal = func(script string) []string {
		args := dockerArgv(container, opts)(false, script)
		args[2] = "-it"

		return args
	}

	return r
}

func dockerArgv(container string, opts DockerOptions) func(bool, string) []string {
	return func(_ bool, script string) []string {
		args := []string{"docker", "exec", "-i"}
		if opts.User != "" {
			args = append(args, "-u", opts.User)
		}

		return append(args, container, "sh", "-c", script)
	}
}

// KubectlOptions configure a host that is a pod.
type KubectlOptions struct {
	Context   string
	Namespace string

	// Container picks one container of the pod, the default one when
	// empty.
	Container string
	Agent     AgentFunc
	Progress  ProgressFunc
	AgentArgs []string
}

// NewKubectl returns a host that is a running pod, reached with kubectl
// exec. The host is named kubectl://pod.
func NewKubectl(pod string, opts KubectlOptions) *Remote {
	d := &commandDialer{name: "kubectl " + pod, argv: kubectlArgv(pod, opts), agent: opts.Agent, progress: opts.Progress, agentArgs: opts.AgentArgs, ids: map[string]fileID{}}
	name := "kubectl://"
	if opts.Namespace != "" {
		name += opts.Namespace + "/"
	}

	r := NewRemote(name+pod, d.dial)
	r.terminal = func(script string) []string {
		args := kubectlArgv(pod, opts)(false, script)
		args[2] = "-it"

		return args
	}

	return r
}

func kubectlArgv(pod string, opts KubectlOptions) func(bool, string) []string {
	return func(_ bool, script string) []string {
		args := []string{"kubectl", "exec", "-i"}
		if opts.Context != "" {
			args = append(args, "--context", opts.Context)
		}

		if opts.Namespace != "" {
			args = append(args, "-n", opts.Namespace)
		}

		if opts.Container != "" {
			args = append(args, "-c", opts.Container)
		}

		return append(args, pod, "--", "sh", "-c", script)
	}
}

// commandDialer starts the agent through a command whose stdin and
// stdout reach the other side, such as ssh or docker exec.
type commandDialer struct {
	// name says what is connected to, for errors.
	name string

	// argv is the command line running a sh script on the other side.
	// interactive is set for the first connection, which may prompt.
	argv     func(interactive bool, script string) []string
	agent    AgentFunc
	progress ProgressFunc

	// agentArgs follow "dk agent" in the start script.
	agentArgs []string

	mu  sync.Mutex
	ids map[string]fileID
}

// fileID is the content hash of a local agent binary.
type fileID struct {
	size int64
	mod  time.Time
	id   string
}

// remoteSystems maps uname output to Go names.
var remoteSystems = []struct {
	uname, goos, goarch string
}{
	{"Linux x86_64", "linux", "amd64"},
	{"Linux aarch64", "linux", "arm64"},
	{"Linux arm64", "linux", "arm64"},
	{"Darwin arm64", "darwin", "arm64"},
	{"Darwin x86_64", "darwin", "amd64"},
}

// goSystem translates uname -s -m output.
func goSystem(uname string) (string, string, bool) {
	for _, s := range remoteSystems {
		if s.uname == uname {
			return s.goos, s.goarch, true
		}
	}

	return "", "", false
}

// needAgent is the answer of a remote side without a matching agent.
type needAgent struct {
	uname string

	// gzip is set when the remote side can unpack a gzip stream, the
	// agent is then sent compressed.
	gzip bool
}

func (d *commandDialer) dial(ctx context.Context, interactive bool) (Conn, error) {
	for range 2 {
		conn, need, err := d.start(ctx, interactive)
		if err != nil {
			return Conn{}, err
		}

		if need == nil {
			return conn, nil
		}

		if err := d.install(ctx, interactive, *need); err != nil {
			return Conn{}, err
		}
	}

	return Conn{}, errors.New("the agent did not start after installing it")
}

// ShellQuote quotes s as one word for a POSIX shell.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// startScript execs the agent installed for the remote system, or
// reports the system when there is none.
func (d *commandDialer) startScript() string {
	var b strings.Builder
	b.WriteString(`case "$(uname -s) $(uname -m)" in` + "\n")
	for _, s := range remoteSystems {
		if id := d.agentID(s.goos, s.goarch); id != "" {
			fmt.Fprintf(&b, "%q) id=%s ;;\n", s.uname, id)
		}
	}

	b.WriteString("*) id= ;;\nesac\n")
	b.WriteString(`p="${HOME:-/tmp}/.cache/duck/agent/$id/dk"` + "\n")
	args := ""
	for _, a := range d.agentArgs {
		args += " " + ShellQuote(a)
	}

	b.WriteString(`if [ -n "$id" ] && [ -x "$p" ]; then exec "$p" agent` + args + "; fi\n")
	b.WriteString(`z=; command -v gzip >/dev/null 2>&1 && z=" gzip"` + "\n")
	b.WriteString(`echo "DUCK-NEED $(uname -s) $(uname -m)$z"` + "\n")

	return b.String()
}

// agentID returns the content hash of the local agent for a system, ""
// when there is none without a download.
func (d *commandDialer) agentID(goos, goarch string) string {
	if d.agent == nil {
		return ""
	}

	path, err := d.agent(goos, goarch, false)
	if err != nil {
		return ""
	}

	id, err := d.hash(path)
	if err != nil {
		return ""
	}

	return id
}

// hash returns the content id of a file, cached by size and time.
func (d *commandDialer) hash(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	d.mu.Lock()
	cached, ok := d.ids[path]
	d.mu.Unlock()
	if ok && cached.size == info.Size() && cached.mod.Equal(info.ModTime()) {
		return cached.id, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	id := hex.EncodeToString(h.Sum(nil))[:16]
	d.mu.Lock()
	d.ids[path] = fileID{size: info.Size(), mod: info.ModTime(), id: id}
	d.mu.Unlock()

	return id, nil
}

// start runs the start script. It returns a connection when an agent
// answered, or what system needs one.
func (d *commandDialer) start(ctx context.Context, interactive bool) (Conn, *needAgent, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	argv := d.argv(interactive, d.startScript())
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Conn{}, nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Conn{}, nil, err
	}

	stderr := &tailBuffer{max: 4 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return Conn{}, nil, fmt.Errorf("%s: %w", d.name, err)
	}

	stop := func() error {
		_ = stdin.Close()
		_ = cmd.Process.Kill()

		return cmd.Wait()
	}

	type answer struct {
		line string
		err  error
	}

	br := bufio.NewReaderSize(stdout, 64<<10)
	answers := make(chan answer, 1)
	go func() {
		// Shell startup files may print before the script runs.
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				answers <- answer{err: err}

				return
			}

			if strings.HasPrefix(line, agentMarker) || strings.HasPrefix(line, "DUCK-NEED ") {
				answers <- answer{line: strings.TrimSpace(line)}

				return
			}
		}
	}()

	var a answer
	select {
	case a = <-answers:
	case <-ctx.Done():
		_ = stop()

		return Conn{}, nil, fmt.Errorf("%s: %w", d.name, ctx.Err())
	}

	switch {
	case a.err != nil:
		_ = stop()

		return Conn{}, nil, d.failure(stderr)
	case strings.HasPrefix(a.line, "DUCK-NEED "):
		_ = stop()

		uname, gz := strings.CutSuffix(strings.TrimPrefix(a.line, "DUCK-NEED "), " gzip")

		return Conn{}, &needAgent{uname: uname, gzip: gz}, nil
	}

	return Conn{R: br, W: stdin, Close: stop}, nil, nil
}

// failure turns the error output of the transport into an error.
func (d *commandDialer) failure(stderr *tailBuffer) error {
	msg := strings.TrimSpace(stderr.String())
	if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
		msg = msg[i+1:]
	}

	if msg == "" {
		msg = "connection closed"
	}

	// A shell reports a binary whose loader is missing as not found.
	if strings.Contains(msg, "/.cache/duck/agent/") {
		return fmt.Errorf("%s: the Duck agent installed there does not start (%s), most likely it was built for glibc and the system has another C library, as Alpine, a Duck release with static agents installs one that runs there", d.name, msg)
	}

	return fmt.Errorf("%s: %s", d.name, msg)
}

// install copies the agent for the remote system to the remote side.
func (d *commandDialer) install(ctx context.Context, interactive bool, need needAgent) error {
	goos, goarch, ok := goSystem(need.uname)
	if !ok {
		return fmt.Errorf("%s runs %s, Duck has no build for it", d.name, need.uname)
	}

	if d.agent == nil {
		return fmt.Errorf("no dk binary for %s/%s", goos, goarch)
	}

	path, err := d.agent(goos, goarch, true)
	if err != nil {
		return err
	}

	id, err := d.hash(path)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	upload := &uploadReader{r: f, size: info.Size(), report: d.progress}
	done := make(chan struct{})
	defer close(done)

	go upload.watch(done, uploadStall, func() { cancel(errUploadStalled) })

	var stdin io.Reader = upload
	receive := "cat"
	if need.gzip {
		pr, pw := io.Pipe()
		defer func() { _ = pr.Close() }()

		go func() {
			zw := gzip.NewWriter(pw)
			_, err := io.Copy(zw, upload)
			if err == nil {
				err = zw.Close()
			}

			_ = pw.CloseWithError(err)
		}()

		stdin = pr
		receive = "gzip -dc"
	}

	script := `d="${HOME:-/tmp}/.cache/duck/agent/` + id + `"; mkdir -p "$d" && ` + receive + ` > "$d/dk.tmp" && chmod 755 "$d/dk.tmp" && mv "$d/dk.tmp" "$d/dk"`
	argv := d.argv(interactive, script)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = time.Second
	cmd.Stdin = stdin
	stderr := &tailBuffer{max: 4 << 10}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(context.Cause(ctx), errUploadStalled) {
			return fmt.Errorf("install agent: %s: the upload stalled, nothing was sent for %s", d.name, uploadStall)
		}

		return fmt.Errorf("install agent: %w", d.failure(stderr))
	}

	return nil
}

var errUploadStalled = errors.New("agent upload stalled")

// uploadReader counts the bytes of an agent upload and reports them.
type uploadReader struct {
	r      io.Reader
	size   int64
	report ProgressFunc

	sent     atomic.Int64
	lastRead atomic.Int64
	reported time.Time
}

func (u *uploadReader) Read(p []byte) (int, error) {
	n, err := u.r.Read(p)
	now := time.Now()
	u.lastRead.Store(now.UnixNano())
	sent := u.sent.Add(int64(n))
	if u.report != nil && (sent == u.size || now.Sub(u.reported) >= 100*time.Millisecond) {
		u.reported = now
		u.report(sent, u.size)
	}

	return n, err
}

// watch calls stalled when reads started and then stopped for limit
// before the whole file was read. The wait before the first read, such
// as a password prompt, is not counted.
func (u *uploadReader) watch(done <-chan struct{}, limit time.Duration, stalled func()) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}

		last := u.lastRead.Load()
		if last == 0 || u.sent.Load() >= u.size {
			continue
		}

		if time.Since(time.Unix(0, last)) > limit {
			stalled()

			return
		}
	}
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buf.Write(p)
	if extra := t.buf.Len() - t.max; extra > 0 {
		t.buf.Next(extra)
	}

	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.buf.String()
}
