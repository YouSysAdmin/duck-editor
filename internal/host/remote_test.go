package host

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/search"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// pipeAgents serves agents in process and lets a test cut the current
// connection.
type pipeAgents struct {
	mu    sync.Mutex
	cut   func() error
	dials int
}

func (a *pipeAgents) dial(context.Context, bool) (Conn, error) {
	toAgent, fromEditor := io.Pipe()
	fromAgent, toEditor := io.Pipe()
	go func() {
		_ = Serve(toAgent, toEditor)
		_ = toEditor.Close()
	}()

	br := bufio.NewReader(fromAgent)
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, agentMarker) {
		return Conn{}, errors.New("no marker")
	}

	closeAll := func() error {
		_ = fromEditor.Close()
		_ = fromAgent.Close()

		return nil
	}

	a.mu.Lock()
	a.cut = closeAll
	a.dials++
	a.mu.Unlock()

	return Conn{R: br, W: fromEditor, Close: closeAll}, nil
}

func connectPipe(t *testing.T) (*Remote, *pipeAgents) {
	t.Helper()

	agents := &pipeAgents{}
	r := NewRemote("test", agents.dial)
	if err := r.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = r.Close() })

	return r, agents
}

func TestRemoteFiles(t *testing.T) {
	t.Parallel()

	r, _ := connectPipe(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "a.txt")
	if err := r.MkdirAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}

	if err := r.WriteFile(path, []byte("hello\nworld\n"), true); err != nil {
		t.Fatal(err)
	}

	data, err := r.ReadFile(path)
	if err != nil || string(data) != "hello\nworld\n" {
		t.Fatalf("read %q %v", data, err)
	}

	info, err := r.Stat(path)
	if err != nil || info.Size != 12 || info.IsDir() {
		t.Fatalf("stat %+v %v", info, err)
	}

	if _, err := r.Stat(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file error %v", err)
	}

	found, err := r.Exists([]string{path, filepath.Join(dir, "missing")})
	if err != nil || !slices.Equal(found, []bool{true, false}) {
		t.Fatalf("exists %v %v", found, err)
	}

	entries, err := r.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name != "sub" || !entries[0].IsDir() {
		t.Fatalf("readdir %+v %v", entries, err)
	}

	moved := filepath.Join(dir, "b.txt")
	if err := r.Rename(path, moved); err != nil {
		t.Fatal(err)
	}

	var files []string
	if err := r.Walk(context.Background(), dir, walk.DefaultOptions(), func(rel string) bool {
		files = append(files, rel)

		return true
	}); err != nil || !slices.Equal(files, []string{"b.txt"}) {
		t.Fatalf("walk %v %v", files, err)
	}

	var matches []search.Match
	if err := r.Search(context.Background(), dir, walk.DefaultOptions(), search.Query{Pattern: "wor"}, func(m search.Match) bool {
		matches = append(matches, m)

		return true
	}); err != nil || len(matches) != 1 || matches[0].Line != 1 || matches[0].Start != 6 {
		t.Fatalf("search %+v %v", matches, err)
	}

	exe, _ := os.Executable()
	if desc, err := r.Describe(exe); err != nil || !desc.Binary {
		t.Fatalf("describe %+v %v", desc, err)
	}
}

func TestRemoteCommands(t *testing.T) {
	t.Parallel()

	r, _ := connectPipe(t)
	out, _, err := r.Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "tr a-z A-Z"}, Stdin: []byte("abc")})
	if err != nil || string(out) != "ABC" {
		t.Fatalf("run %q %v", out, err)
	}

	_, stderr, err := r.Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo oops >&2; exit 3"}})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 3 || strings.TrimSpace(string(stderr)) != "oops" {
		t.Fatalf("exit %v %q", err, stderr)
	}

	proc, err := r.Start(Command{Name: "cat"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := proc.Stdin().Write([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}

	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil || line != "ping\n" {
		t.Fatalf("process echo %q %v", line, err)
	}

	_ = proc.Stdin().Close()
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait %v", err)
	}
}

func TestRemoteReconnects(t *testing.T) {
	t.Parallel()

	r, agents := connectPipe(t)
	statuses := make(chan Status, 8)
	r.OnStatus(func(s Status) { statuses <- s })

	proc, err := r.Start(Command{Name: "cat"})
	if err != nil {
		t.Fatal(err)
	}

	agents.mu.Lock()
	_ = agents.cut()
	agents.mu.Unlock()

	if s := <-statuses; s.Connected {
		t.Fatalf("status after cut %+v", s)
	}

	if err := proc.Wait(); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("process after cut %v", err)
	}

	if _, err := r.ReadFile("/"); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("call while down %v", err)
	}

	select {
	case s := <-statuses:
		if !s.Connected {
			t.Fatalf("status after reconnect %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reconnect")
	}

	if _, err := r.Home(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessExitCarriesStderr(t *testing.T) {
	t.Parallel()

	r, _ := connectPipe(t)
	for name, h := range map[string]Host{"local": Local{}, "remote": r} {
		proc, err := h.Start(Command{Name: "sh", Args: []string{"-c", "echo boom >&2; exit 2"}})
		if err != nil {
			t.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, proc.Stdout())
		var exitErr *ExitError
		if err := proc.Wait(); !errors.As(err, &exitErr) || exitErr.Code != 2 || strings.TrimSpace(string(exitErr.Stderr)) != "boom" {
			t.Fatalf("%s: wait = %v", name, err)
		}
	}

	if r.PID() == 0 {
		t.Fatal("the agent reports no pid")
	}
}

func TestRemoteDial(t *testing.T) {
	t.Parallel()

	r, _ := connectPipe(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "free symbols")
	}))
	t.Cleanup(srv.Close)

	conn, err := r.Dial(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = conn.Close() }()

	if _, err := io.WriteString(conn, "GET / HTTP/1.0\r\nHost: x\r\n\r\n"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "free symbols" {
		t.Fatalf("body %q", body)
	}

	if _, err := r.Dial("127.0.0.1:1"); err == nil {
		t.Fatal("dialing a closed port fails")
	}
}

// TestRemoteStdinNeverStallsAgent writes to a process that does not read
// its stdin and checks that the agent still answers other requests, and
// that an empty write leaves stdin open.
func TestRemoteStdinNeverStallsAgent(t *testing.T) {
	t.Parallel()

	r, _ := connectPipe(t)
	sleeper, err := r.Start(Command{Name: "sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatal(err)
	}

	chunk := []byte(strings.Repeat("x", 1024))
	for range 5000 {
		if _, err := sleeper.Stdin().Write(chunk); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, _, err := r.Run(ctx, Command{Name: "true"}); err != nil {
		t.Fatalf("the agent stopped answering: %v", err)
	}

	_ = sleeper.Kill()
	_ = sleeper.Wait()

	cat, err := r.Start(Command{Name: "cat"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := cat.Stdin().Write(nil); err != nil {
		t.Fatal(err)
	}

	if _, err := cat.Stdin().Write([]byte("still open\n")); err != nil {
		t.Fatal(err)
	}

	line, err := bufio.NewReader(cat.Stdout()).ReadString('\n')
	if err != nil || line != "still open\n" {
		t.Fatalf("after an empty write %q %v", line, err)
	}

	_ = cat.Stdin().Close()
	_ = cat.Wait()
}
