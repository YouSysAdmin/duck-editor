package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/fileinfo"
	"github.com/YouSysAdmin/duck-editor/internal/search"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// Local is the machine the editor runs on.
type Local struct{}

var _ Host = Local{}

// Name implements Host.
func (Local) Name() string {
	return ""
}

// Status implements Host, the local machine is always there.
func (Local) Status() Status {
	return Status{Connected: true}
}

// ReadFile implements Host.
func (Local) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile implements Host.
func (Local) WriteFile(path string, data []byte, atomic bool) error {
	if atomic {
		return writeAtomic(path, data)
	}

	return writePlain(path, data)
}

// Stat implements Host.
func (Local) Stat(path string) (FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}

	return fileInfo(info), nil
}

// Exists implements Host.
func (Local) Exists(paths []string) ([]bool, error) {
	return exist(paths), nil
}

func exist(paths []string) []bool {
	out := make([]bool, len(paths))
	for i, p := range paths {
		_, err := os.Stat(p)
		out[i] = err == nil
	}

	return out
}

func fileInfo(info fs.FileInfo) FileInfo {
	return FileInfo{Name: info.Name(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
}

// ReadDir implements Host.
func (Local) ReadDir(path string) ([]DirEntry, error) {
	entries, err := os.ReadDir(path)
	out := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		typ, link := entry.Type(), ""
		// A link to a directory lists as a directory.
		if typ&fs.ModeSymlink != 0 {
			full := filepath.Join(path, entry.Name())
			link, _ = os.Readlink(full)
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				typ |= fs.ModeDir
			}
		}

		out = append(out, DirEntry{Name: entry.Name(), Type: typ, Link: link})
	}

	return out, err
}

// MkdirAll implements Host.
func (Local) MkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// Rename implements Host.
func (Local) Rename(from, to string) error {
	return os.Rename(from, to)
}

// Home implements Host.
func (Local) Home() (string, error) {
	return os.UserHomeDir()
}

// PID implements Host.
func (Local) PID() int {
	return os.Getpid()
}

// LookPath implements Host.
func (Local) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// Run implements Host.
func (Local) Run(ctx context.Context, c Command) ([]byte, []byte, error) {
	cmd := command(ctx, c)
	// Children that keep the pipes open do not hold the command forever.
	cmd.WaitDelay = time.Second
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		err = &ExitError{Code: exitErr.ExitCode(), Stderr: stderr.Bytes()}
	}

	return stdout.Bytes(), stderr.Bytes(), err
}

func command(ctx context.Context, c Command) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}

	return cmd
}

// Start implements Host.
func (Local) Start(c Command) (Process, error) {
	cmd := command(context.Background(), c)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr := &tailBuffer{max: stderrTail}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", c.Name, err)
	}

	return &localProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

// stderrTail is how much error output of a started process is kept.
const stderrTail = 4 << 10

type localProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	stderr *tailBuffer
}

func (p *localProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *localProcess) Stdout() io.Reader     { return p.stdout }
func (p *localProcess) Kill() error           { return p.cmd.Process.Kill() }

// Wait returns an *ExitError with the end of stderr for a non zero exit.
func (p *localProcess) Wait() error {
	err := p.cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return &ExitError{Code: exitErr.ExitCode(), Stderr: []byte(p.stderr.String())}
	}

	if err != nil && p.stderr.String() != "" {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(p.stderr.String()))
	}

	return err
}

// Walk implements Host.
func (Local) Walk(ctx context.Context, root string, opts walk.Options, fn func(rel string) bool) error {
	return walk.Files(ctx, root, opts, fn)
}

// Search implements Host.
func (Local) Search(ctx context.Context, root string, opts walk.Options, q search.Query, fn func(search.Match) bool) error {
	return search.Files(ctx, root, opts, q, fn)
}

// Describe implements Host.
func (Local) Describe(path string) (Description, error) {
	return describe(path)
}

// sniffSize is how much of a file decides whether it is text.
const sniffSize = 8 << 10

func describe(path string) (Description, error) {
	f, err := os.Open(path)
	if err != nil {
		return Description{}, err
	}

	buf := make([]byte, sniffSize)
	n, err := io.ReadFull(f, buf)
	_ = f.Close()
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Description{}, err
	}

	if !buffer.LooksBinary(buf[:n]) {
		return Description{}, nil
	}

	text, _ := fileinfo.Describe(path)

	return Description{Binary: true, Text: text}, nil
}

// writePlain stores data at path, creating the file when needed.
func writePlain(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}

	return nil
}

// writeAtomic writes to a temp file next to path and renames it over the
// target, keeping the mode of the existing file. A symlink is followed so
// the link stays in place. A new file is written directly, which gives it
// the usual mode under the umask.
func writeAtomic(path string, data []byte) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	// A new file, or one shared through hard links or another owner, is
	// written in place.
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && sharedFile(info)) {
		return writePlain(path, data)
	}

	// The rename below only needs a writable directory, a file the user
	// may not write must refuse like a plain write would.
	probe, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}

	_ = probe.Close()

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		// Directories that refuse temp files still get a plain write.
		return writePlain(path, data)
	}

	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)

		return fmt.Errorf("write %q: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)

		return fmt.Errorf("write %q: %w", path, err)
	}

	if info != nil {
		_ = os.Chmod(name, info.Mode().Perm())
	}

	// A bind mounted file cannot be replaced, it is written in place.
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)

		return writePlain(path, data)
	}

	return nil
}
