package host

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/search"
)

// streamBatch is how many paths or matches go into one message.
const streamBatch = 256

// Serve answers the requests read from r with responses written to w,
// running every operation on this machine. It returns when r ends and
// kills the processes it started.
func Serve(r io.Reader, w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s %d\n", agentMarker, protocolVersion); err != nil {
		return err
	}

	s := &server{enc: gob.NewEncoder(w), procs: map[uint64]*serverProc{}, cancels: map[uint64]context.CancelFunc{}}
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	if err := s.encode(hello{Protocol: protocolVersion, Home: home, Cwd: cwd, PID: os.Getpid()}); err != nil {
		return err
	}

	dec := gob.NewDecoder(r)
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			s.shutdown()
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		s.handle(req)
	}
}

type server struct {
	local   Local
	encMu   sync.Mutex
	enc     *gob.Encoder
	mu      sync.Mutex
	procs   map[uint64]*serverProc
	cancels map[uint64]context.CancelFunc
}

// serverProc is a process started for the editor. Its input goes
// through a queue so a process that does not read its stdin never stalls
// the request loop, and with it the heartbeat.
type serverProc struct {
	proc Process

	mu    sync.Mutex
	queue []stdinItem
	gone  bool

	// wake tells the writer about new items, done ends it with the
	// process.
	wake chan struct{}
	done chan struct{}
}

// stdinItem is data for the stdin of a process, or its end.
type stdinItem struct {
	data  []byte
	close bool
}

// push queues an item for the writer, dropped once the writer is gone.
func (p *serverProc) push(item stdinItem) {
	p.mu.Lock()
	if !p.gone {
		p.queue = append(p.queue, item)
	}

	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// writeInput feeds the queue to stdin until it is closed, a write fails
// or the process is done.
func (p *serverProc) writeInput() {
	defer func() {
		p.mu.Lock()
		p.gone, p.queue = true, nil
		p.mu.Unlock()
	}()

	for {
		select {
		case <-p.wake:
		case <-p.done:
			return
		}

		for {
			p.mu.Lock()
			items := p.queue
			p.queue = nil
			p.mu.Unlock()
			if len(items) == 0 {
				break
			}

			for _, item := range items {
				if item.close {
					_ = p.proc.Stdin().Close()

					return
				}

				if _, err := p.proc.Stdin().Write(item.data); err != nil {
					return
				}
			}
		}
	}
}

func (s *server) encode(v any) error {
	s.encMu.Lock()
	defer s.encMu.Unlock()

	return s.enc.Encode(v)
}

func (s *server) send(resp response) {
	_ = s.encode(resp)
}

func (s *server) handle(req request) {
	switch req.Op {
	case opStdin, opStdinClose, opKill:
		s.mu.Lock()
		p := s.procs[req.Proc]
		s.mu.Unlock()
		if p == nil {
			return
		}

		switch req.Op {
		case opStdin:
			p.push(stdinItem{data: req.Data})
		case opStdinClose:
			p.push(stdinItem{close: true})
		case opKill:
			_ = p.proc.Kill()
		}
	case opPing:
		s.send(response{ID: req.ID, Done: true})
	case opCancel:
		s.mu.Lock()
		cancel := s.cancels[req.ID]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	default:
		go s.run(req)
	}
}

// context returns a context the editor can cancel by request id.
func (s *server) context(id uint64) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[id] = cancel
	s.mu.Unlock()

	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.cancels, id)
		s.mu.Unlock()
	}
}

func (s *server) run(req request) {
	resp := response{ID: req.ID, Done: true}
	var err error
	switch req.Op {
	case opReadFile:
		resp.Data, err = s.local.ReadFile(req.Path)
	case opWriteFile:
		err = s.local.WriteFile(req.Path, req.Data, req.Atomic)
	case opStat:
		resp.Info, err = s.local.Stat(req.Path)
	case opExists:
		resp.Bools, err = s.local.Exists(req.Paths)
	case opReadDir:
		resp.Entries, err = s.local.ReadDir(req.Path)
	case opMkdirAll:
		err = s.local.MkdirAll(req.Path)
	case opRename:
		err = s.local.Rename(req.Path, req.To)
	case opHome:
		resp.Text, err = s.local.Home()
	case opLookPath:
		resp.Text, err = s.local.LookPath(req.Path)
	case opDescribe:
		resp.Desc, err = s.local.Describe(req.Path)
	case opRun:
		ctx, done := s.context(req.ID)
		resp.Data, resp.Stderr, err = s.local.Run(ctx, req.Cmd)
		done()
	case opStart:
		var proc Process
		if proc, err = s.local.Start(req.Cmd); err == nil {
			s.start(req, proc)

			return
		}
	case opDial:
		var conn net.Conn
		if conn, err = net.DialTimeout("tcp", req.Path, 10*time.Second); err == nil {
			s.start(req, tcpProcess{conn})

			return
		}
	case opWalk:
		err = s.walk(req)
	case opSearch:
		err = s.search(req)
	default:
		err = fmt.Errorf("unknown operation %q", req.Op)
	}

	resp.Err = toWire(err)
	s.send(resp)
}

func (s *server) walk(req request) error {
	ctx, done := s.context(req.ID)
	defer done()

	batch := make([]string, 0, streamBatch)
	err := s.local.Walk(ctx, req.Path, req.Walk, func(rel string) bool {
		batch = append(batch, rel)
		if len(batch) == streamBatch {
			s.send(response{ID: req.ID, Paths: batch})
			batch = make([]string, 0, streamBatch)
		}

		return true
	})
	if len(batch) > 0 {
		s.send(response{ID: req.ID, Paths: batch})
	}

	return err
}

func (s *server) search(req request) error {
	ctx, done := s.context(req.ID)
	defer done()

	var batch []search.Match
	err := s.local.Search(ctx, req.Path, req.Walk, req.Query, func(m search.Match) bool {
		batch = append(batch, m)
		if len(batch) == streamBatch {
			s.send(response{ID: req.ID, Matches: batch})
			batch = nil
		}

		return true
	})
	if len(batch) > 0 {
		s.send(response{ID: req.ID, Matches: batch})
	}

	return err
}

// start streams the output of a started process, or a connection, as
// messages carrying the process id the editor chose.
func (s *server) start(req request, proc Process) {
	p := &serverProc{proc: proc, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.mu.Lock()
	s.procs[req.Proc] = p
	s.mu.Unlock()
	s.send(response{ID: req.ID, Done: true})

	go p.writeInput()

	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := proc.Stdout().Read(buf)
			if n > 0 {
				s.send(response{Proc: req.Proc, Data: append([]byte(nil), buf[:n]...)})
			}

			if err != nil {
				break
			}
		}

		werr := proc.Wait()
		close(p.done)
		s.mu.Lock()
		delete(s.procs, req.Proc)
		s.mu.Unlock()
		s.send(response{Proc: req.Proc, Done: true, Err: toWire(werr)})
	}()
}

// shutdown kills the started processes and stops running requests.
func (s *server) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.procs {
		_ = p.proc.Kill()
	}

	for _, cancel := range s.cancels {
		cancel()
	}
}

// tcpProcess lets a connection travel the way process pipes do: stdin
// writes to it, closing stdin closes its write side, stdout reads it.
type tcpProcess struct {
	conn net.Conn
}

func (t tcpProcess) Stdin() io.WriteCloser { return tcpInput(t) }
func (t tcpProcess) Stdout() io.Reader     { return t.conn }
func (t tcpProcess) Wait() error           { return t.conn.Close() }
func (t tcpProcess) Kill() error           { return t.conn.Close() }

type tcpInput tcpProcess

func (t tcpInput) Write(p []byte) (int, error) { return t.conn.Write(p) }

func (t tcpInput) Close() error {
	if c, ok := t.conn.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}

	return nil
}
