package host

import (
	"errors"
	"io/fs"

	"github.com/YouSysAdmin/duck-editor/internal/search"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// protocolVersion changes whenever the messages below change, an agent
// of another version is replaced.
const protocolVersion = 5

// agentMarker starts the line an agent prints before the message stream,
// lines before it come from shell startup files and are skipped.
const agentMarker = "DUCK-AGENT"

// Operations of a request.
const (
	opReadFile   = "read"
	opWriteFile  = "write"
	opStat       = "stat"
	opExists     = "exists"
	opReadDir    = "readdir"
	opMkdirAll   = "mkdir"
	opRename     = "rename"
	opHome       = "home"
	opLookPath   = "lookpath"
	opRun        = "run"
	opStart      = "start"
	opStdin      = "stdin"
	opStdinClose = "stdin-close"
	opKill       = "kill"
	opWalk       = "walk"
	opSearch     = "search"
	opDescribe   = "describe"
	opCancel     = "cancel"
	opPing       = "ping"
	opDial       = "dial"
)

// hello is the first message of an agent.
type hello struct {
	Protocol int
	Home     string

	// Cwd is where the agent started, the home directory over ssh and
	// the working directory of a container.
	Cwd string

	// PID is the process id of the agent.
	PID int
}

// request is a message from the editor to the agent. Fields not used by
// an operation stay zero.
type request struct {
	ID     uint64
	Op     string
	Path   string
	Paths  []string
	To     string
	Data   []byte
	Atomic bool
	Cmd    Command
	Walk   walk.Options
	Query  search.Query

	// Proc names a started process for the process operations.
	Proc uint64
}

// response is a message from the agent. Streaming operations send
// several with Done set on the last one. Process output carries Proc
// and no ID.
type response struct {
	ID      uint64
	Done    bool
	Err     *wireError
	Data    []byte
	Stderr  []byte
	Info    FileInfo
	Entries []DirEntry
	Bools   []bool
	Paths   []string
	Matches []search.Match
	Text    string
	Desc    Description
	Proc    uint64
}

// wireError carries an error with enough of its kind for errors.Is.
type wireError struct {
	Kind   string
	Msg    string
	Code   int
	Stderr []byte
}

const (
	kindNotExist   = "notexist"
	kindExist      = "exist"
	kindPermission = "permission"
	kindExit       = "exit"
)

func toWire(err error) *wireError {
	if err == nil {
		return nil
	}

	w := &wireError{Msg: err.Error()}
	var exitErr *ExitError
	switch {
	case errors.As(err, &exitErr):
		w.Kind, w.Code, w.Stderr = kindExit, exitErr.Code, exitErr.Stderr
	case errors.Is(err, fs.ErrNotExist):
		w.Kind = kindNotExist
	case errors.Is(err, fs.ErrExist):
		w.Kind = kindExist
	case errors.Is(err, fs.ErrPermission):
		w.Kind = kindPermission
	}

	return w
}

// remoteError is an error that happened on the agent side.
type remoteError struct {
	msg  string
	kind error
}

func (e *remoteError) Error() string { return e.msg }
func (e *remoteError) Unwrap() error { return e.kind }

func fromWire(w *wireError) error {
	if w == nil {
		return nil
	}

	switch w.Kind {
	case kindExit:
		return &ExitError{Code: w.Code, Stderr: w.Stderr}
	case kindNotExist:
		return &remoteError{msg: w.Msg, kind: fs.ErrNotExist}
	case kindExist:
		return &remoteError{msg: w.Msg, kind: fs.ErrExist}
	case kindPermission:
		return &remoteError{msg: w.Msg, kind: fs.ErrPermission}
	}

	return errors.New(w.Msg)
}
