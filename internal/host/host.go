// Package host runs file and process operations on the machine that
// holds the files, the local one or a remote one reached over ssh.
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/search"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// Host is a machine the editor works on.
type Host interface {
	// Name is empty for the local machine and a URL such as ssh://box
	// or docker://app otherwise.
	Name() string

	// Status tells whether the host can be reached.
	Status() Status

	ReadFile(path string) ([]byte, error)

	// WriteFile stores data at path. An atomic write goes through a temp
	// file renamed over the target, keeping the mode and symlinks.
	WriteFile(path string, data []byte, atomic bool) error

	Stat(path string) (FileInfo, error)

	// Exists reports for every path whether it exists, in one round
	// trip on a remote host.
	Exists(paths []string) ([]bool, error)

	// ReadDir lists a directory sorted by name.
	ReadDir(path string) ([]DirEntry, error)

	MkdirAll(path string) error
	Rename(from, to string) error

	// Home is the home directory of the user.
	Home() (string, error)

	// PID is a process on the host that lives as long as the editor's
	// work there, the editor itself or the agent. Language servers
	// watch it and exit when it is gone.
	PID() int

	LookPath(name string) (string, error)

	// Run runs a command to completion and returns its output. A non
	// zero exit is an *ExitError.
	Run(ctx context.Context, cmd Command) (stdout, stderr []byte, err error)

	// Start launches a long running command with stdin and stdout
	// pipes. The end of its stderr comes with the error of Wait.
	Start(cmd Command) (Process, error)

	// Walk lists the files below root like walk.Files.
	Walk(ctx context.Context, root string, opts walk.Options, fn func(rel string) bool) error

	// Search reports the lines of the files below root that match.
	Search(ctx context.Context, root string, opts walk.Options, q search.Query, fn func(search.Match) bool) error

	// Describe tells whether a file is binary and what it is.
	Describe(path string) (Description, error)
}

// Status is the connection state of a host.
type Status struct {
	Connected bool

	// Err is why the connection was lost.
	Err string
}

// FileInfo describes a file.
type FileInfo struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

// IsDir reports whether the file is a directory.
func (i FileInfo) IsDir() bool {
	return i.Mode.IsDir()
}

// DirEntry is one entry of a directory listing.
type DirEntry struct {
	Name string

	// Type holds the type bits of the mode, not the permissions.
	Type fs.FileMode

	// Link is where a symbolic link points, as written in the link.
	Link string
}

// IsDir reports whether the entry is a directory or a link to one.
func (d DirEntry) IsDir() bool {
	return d.Type.IsDir()
}

// Command is a program to run.
type Command struct {
	Name string
	Args []string

	// Dir is the working directory, the default one when empty.
	Dir string

	// Env adds KEY=value pairs to the inherited environment.
	Env []string

	// Stdin is fed to the program, nil gives it no input.
	Stdin []byte
}

// Process is a started command.
type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader

	// Wait waits for the exit after stdout was read to the end.
	Wait() error
	Kill() error
}

// Description says what a file holds.
type Description struct {
	Binary bool

	// Text describes a binary file, empty when the format is unknown.
	Text string
}

// ExitError is a command that exited with a non zero status.
type ExitError struct {
	Code   int
	Stderr []byte
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// ErrDisconnected is returned while a remote host cannot be reached.
var ErrDisconnected = errors.New("no connection")

// File names a file on a host, the local machine when Host is nil.
type File struct {
	Host Host
	Path string
}

// Read returns the content of the file.
func (f File) Read() ([]byte, error) {
	if f.Host == nil {
		return Local{}.ReadFile(f.Path)
	}

	return f.Host.ReadFile(f.Path)
}

// LocalFiles names local files.
func LocalFiles(paths ...string) []File {
	files := make([]File, 0, len(paths))
	for _, p := range paths {
		files = append(files, File{Path: p})
	}

	return files
}
