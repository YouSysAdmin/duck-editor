package host

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/search"
	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// Conn is a connection to an agent, positioned after its marker line.
type Conn struct {
	R     io.Reader
	W     io.Writer
	Close func() error
}

// Dialer opens a connection to an agent. interactive is set for the
// first connection, made before the editor takes the terminal, so the
// transport may ask for a password.
type Dialer func(ctx context.Context, interactive bool) (Conn, error)

const (
	// callTimeout bounds a request the editor waits for.
	callTimeout = 30 * time.Second

	// dialTimeout bounds one connection attempt.
	dialTimeout = 30 * time.Second

	// cacheTTL is how long file metadata is reused, the picker preview
	// asks for it on every frame.
	cacheTTL = time.Second

	// heartbeatInterval is how often the agent is pinged, a connection
	// that receives nothing for silenceLimit is dropped. A hung network
	// then blocks the editor for seconds instead of until ssh gives up.
	heartbeatInterval = 2 * time.Second
	silenceLimit      = 6 * time.Second
)

// reconnectDelays are the pauses between connection attempts, the last
// one repeats.
var reconnectDelays = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

// Remote is a host reached through an agent. It reconnects by itself
// after the connection drops, calls made meanwhile fail with
// ErrDisconnected.
type Remote struct {
	name string
	dial Dialer

	// terminal builds the local command running a script on the host
	// with a terminal, nil when the transport has none.
	terminal func(script string) []string

	mu       sync.Mutex
	conn     *remoteConn
	status   Status
	home     string
	cwd      string
	pid      int
	nextID   uint64
	closed   bool
	onStatus func(Status)
	cache    map[string]cacheEntry
}

var _ Host = (*Remote)(nil)

type cacheEntry struct {
	at   time.Time
	info *FileInfo
	err  error
	desc *Description
}

// remoteConn is one live connection.
type remoteConn struct {
	// lastRecv is when bytes last arrived, in unix nanoseconds.
	lastRecv atomic.Int64

	encMu   sync.Mutex
	enc     *gob.Encoder
	close   func() error
	mu      sync.Mutex
	calls   map[uint64]*call
	procs   map[uint64]*remoteProcess
	dropped bool
}

// call waits for the responses of one request.
type call struct {
	ch   chan response
	done chan struct{}
}

// NewRemote creates a host named name that connects through dial.
func NewRemote(name string, dial Dialer) *Remote {
	return &Remote{name: name, dial: dial, cache: map[string]cacheEntry{}}
}

// TerminalCommand returns the local command that runs script on the host
// with a terminal, for programs that take over the screen. It is nil
// when the transport cannot give one.
func (r *Remote) TerminalCommand(script string) []string {
	if r.terminal == nil {
		return nil
	}

	return r.terminal(script)
}

// OnStatus sets a function told about every connection change. It runs
// on a background goroutine.
func (r *Remote) OnStatus(fn func(Status)) {
	r.mu.Lock()
	r.onStatus = fn
	r.mu.Unlock()
}

// Connect makes the first connection.
func (r *Remote) Connect(ctx context.Context) error {
	return r.connect(ctx, true)
}

// ConnectQuiet makes the first connection without asking anything, for
// a connection made while the editor owns the terminal. A host that
// needs a password fails.
func (r *Remote) ConnectQuiet(ctx context.Context) error {
	return r.connect(ctx, false)
}

func (r *Remote) connect(ctx context.Context, interactive bool) error {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	c, err := r.dial(ctx, interactive)
	if err != nil {
		return err
	}

	conn := &remoteConn{close: c.Close, calls: map[uint64]*call{}, procs: map[uint64]*remoteProcess{}}
	conn.lastRecv.Store(time.Now().UnixNano())
	dec := gob.NewDecoder(recvReader{r: c.R, last: &conn.lastRecv})
	var h hello
	if err := dec.Decode(&h); err != nil {
		_ = c.Close()

		return fmt.Errorf("agent: %w", err)
	}

	if h.Protocol != protocolVersion {
		_ = c.Close()

		return fmt.Errorf("the agent of %s is from another Duck version, protocol %d instead of %d, a development build needs task agent", r.name, h.Protocol, protocolVersion)
	}

	conn.enc = gob.NewEncoder(c.W)
	r.mu.Lock()
	if r.closed || r.conn != nil {
		// Closed meanwhile, or another dial won the race: this agent is
		// not needed.
		closed := r.closed
		r.mu.Unlock()
		_ = c.Close()
		if closed {
			return ErrDisconnected
		}

		return nil
	}

	r.conn = conn
	r.home = h.Home
	r.cwd = h.Cwd
	r.pid = h.PID
	r.status = Status{Connected: true}
	r.cache = map[string]cacheEntry{}
	notify := r.onStatus
	r.mu.Unlock()

	go r.read(conn, dec)
	go r.heartbeat(conn)
	if notify != nil {
		notify(Status{Connected: true})
	}

	return nil
}

// read delivers responses until the connection ends, then drops it.
func (r *Remote) read(conn *remoteConn, dec *gob.Decoder) {
	for {
		var resp response
		if err := dec.Decode(&resp); err != nil {
			r.drop(conn, err)

			return
		}

		if resp.ID == 0 {
			conn.mu.Lock()
			p := conn.procs[resp.Proc]
			if resp.Done {
				delete(conn.procs, resp.Proc)
			}

			conn.mu.Unlock()

			if p != nil {
				p.deliver(resp)
			}

			continue
		}

		conn.mu.Lock()
		c := conn.calls[resp.ID]
		conn.mu.Unlock()
		if c == nil {
			continue
		}

		select {
		case c.ch <- resp:
		case <-c.done:
		}
	}
}

// recvReader notes when data arrives, a large message keeps a
// connection alive while it streams in.
type recvReader struct {
	r    io.Reader
	last *atomic.Int64
}

func (rr recvReader) Read(p []byte) (int, error) {
	n, err := rr.r.Read(p)
	if n > 0 {
		rr.last.Store(time.Now().UnixNano())
	}

	return n, err
}

// heartbeat pings the agent and drops the connection when nothing came
// back for silenceLimit.
func (r *Remote) heartbeat(conn *remoteConn) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// A write into a stalled transport blocks, so pings go out on their
	// own goroutine, one at a time, while the clock keeps running here.
	var pinging atomic.Bool
	for range ticker.C {
		if conn.isDropped() {
			return
		}

		if time.Since(time.Unix(0, conn.lastRecv.Load())) > silenceLimit {
			r.drop(conn, errors.New("no answer from "+r.name))

			return
		}

		// The answer only refreshes lastRecv, nobody waits for it.
		if pinging.CompareAndSwap(false, true) {
			go func() {
				_ = conn.send(request{Op: opPing})
				pinging.Store(false)
			}()
		}
	}
}

func (conn *remoteConn) isDropped() bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	return conn.dropped
}

// drop ends a connection: pending calls and processes fail and the
// reconnect loop starts.
func (r *Remote) drop(conn *remoteConn, err error) {
	conn.mu.Lock()
	if conn.dropped {
		conn.mu.Unlock()

		return
	}

	conn.dropped = true
	calls, procs := conn.calls, conn.procs
	conn.calls, conn.procs = map[uint64]*call{}, map[uint64]*remoteProcess{}
	conn.mu.Unlock()
	_ = conn.close()

	for _, c := range calls {
		close(c.done)
	}

	for _, p := range procs {
		p.fail(ErrDisconnected)
	}

	r.mu.Lock()
	if r.conn != conn {
		r.mu.Unlock()

		return
	}

	r.conn = nil
	msg := "connection lost"
	if err != nil && !errors.Is(err, io.EOF) {
		msg = err.Error()
	}

	r.status = Status{Err: msg}
	closed := r.closed
	notify := r.onStatus
	r.mu.Unlock()

	if closed {
		return
	}

	if notify != nil {
		notify(Status{Err: msg})
	}

	go r.reconnect()
}

// reconnect tries again with growing pauses until a connection works or
// the host is closed.
func (r *Remote) reconnect() {
	for attempt := 0; ; attempt++ {
		time.Sleep(reconnectDelays[min(attempt, len(reconnectDelays)-1)])
		r.mu.Lock()
		closed, connected := r.closed, r.conn != nil
		r.mu.Unlock()
		if closed || connected {
			return
		}

		err := r.connect(context.Background(), false)
		if err == nil {
			return
		}

		r.mu.Lock()
		r.status.Err = err.Error()
		notify := r.onStatus
		r.mu.Unlock()
		if notify != nil {
			notify(Status{Err: err.Error()})
		}
	}
}

// Reconnect drops the current connection, if any, and connects again
// in the background.
func (r *Remote) Reconnect() {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn != nil {
		r.drop(conn, errors.New("reconnecting"))

		return
	}

	go func() {
		if err := r.connect(context.Background(), false); err != nil {
			r.mu.Lock()
			r.status.Err = err.Error()
			notify := r.onStatus
			r.mu.Unlock()
			if notify != nil {
				notify(Status{Err: err.Error()})
			}
		}
	}()
}

// Close ends the connection for good.
func (r *Remote) Close() error {
	r.mu.Lock()
	r.closed = true
	conn := r.conn
	r.mu.Unlock()
	if conn != nil {
		r.drop(conn, nil)
	}

	return nil
}

// begin registers a request on the live connection.
func (r *Remote) begin(req *request) (*remoteConn, *call, error) {
	r.mu.Lock()
	conn := r.conn
	r.nextID++
	req.ID = r.nextID
	r.mu.Unlock()
	if conn == nil {
		return nil, nil, ErrDisconnected
	}

	c := &call{ch: make(chan response, 16), done: make(chan struct{})}
	conn.mu.Lock()
	if conn.dropped {
		conn.mu.Unlock()

		return nil, nil, ErrDisconnected
	}

	conn.calls[req.ID] = c
	conn.mu.Unlock()

	if err := conn.send(*req); err != nil {
		conn.finish(req.ID)

		return nil, nil, ErrDisconnected
	}

	return conn, c, nil
}

func (conn *remoteConn) send(req request) error {
	conn.encMu.Lock()
	defer conn.encMu.Unlock()

	return conn.enc.Encode(req)
}

func (conn *remoteConn) finish(id uint64) {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if c, ok := conn.calls[id]; ok {
		delete(conn.calls, id)
		close(c.done)
	}
}

// call sends a request and waits for its single response. A context
// that never ends gets the call timeout.
func (r *Remote) call(ctx context.Context, req request) (response, error) {
	conn, c, err := r.begin(&req)
	if err != nil {
		return response{}, err
	}

	defer conn.finish(req.ID)

	var timeout <-chan time.Time
	if ctx.Done() == nil {
		timer := time.NewTimer(callTimeout)
		defer timer.Stop()

		timeout = timer.C
	}

	select {
	case resp := <-c.ch:
		return resp, fromWire(resp.Err)
	case <-c.done:
		return response{}, ErrDisconnected
	case <-ctx.Done():
		_ = conn.send(request{ID: req.ID, Op: opCancel})

		return response{}, ctx.Err()
	case <-timeout:
		return response{}, fmt.Errorf("%s: no answer within %s", r.name, callTimeout)
	}
}

// stream sends a request and hands every response to fn until the last
// one arrives, ctx ends or fn returns false.
func (r *Remote) stream(ctx context.Context, req request, fn func(response) bool) error {
	conn, c, err := r.begin(&req)
	if err != nil {
		return err
	}

	defer conn.finish(req.ID)

	for {
		select {
		case resp := <-c.ch:
			if resp.Done {
				return fromWire(resp.Err)
			}

			if !fn(resp) {
				_ = conn.send(request{ID: req.ID, Op: opCancel})

				return nil
			}
		case <-c.done:
			return ErrDisconnected
		case <-ctx.Done():
			_ = conn.send(request{ID: req.ID, Op: opCancel})

			return ctx.Err()
		}
	}
}

// changed forgets cached metadata after the files changed.
func (r *Remote) changed() {
	r.mu.Lock()
	r.cache = map[string]cacheEntry{}
	r.mu.Unlock()
}

func (r *Remote) cached(path string) (cacheEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.cache[path]

	return e, ok && time.Since(e.at) < cacheTTL
}

// Name implements Host.
func (r *Remote) Name() string {
	return r.name
}

// Status implements Host.
func (r *Remote) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.status
}

// ReadFile implements Host.
func (r *Remote) ReadFile(path string) ([]byte, error) {
	resp, err := r.call(context.Background(), request{Op: opReadFile, Path: path})

	return resp.Data, err
}

// WriteFile implements Host.
func (r *Remote) WriteFile(path string, data []byte, atomic bool) error {
	defer r.changed()

	_, err := r.call(context.Background(), request{Op: opWriteFile, Path: path, Data: data, Atomic: atomic})

	return err
}

// Stat implements Host.
func (r *Remote) Stat(path string) (FileInfo, error) {
	if e, ok := r.cached(path); ok && (e.info != nil || e.err != nil) {
		if e.err != nil {
			return FileInfo{}, e.err
		}

		return *e.info, nil
	}

	resp, err := r.call(context.Background(), request{Op: opStat, Path: path})
	if errors.Is(err, ErrDisconnected) {
		return FileInfo{}, err
	}

	r.mu.Lock()
	e := r.cache[path]
	e.at, e.info, e.err = time.Now(), &resp.Info, err
	if err != nil {
		e.info = nil
	}

	r.cache[path] = e
	r.mu.Unlock()

	return resp.Info, err
}

// Exists implements Host.
func (r *Remote) Exists(paths []string) ([]bool, error) {
	resp, err := r.call(context.Background(), request{Op: opExists, Paths: paths})
	if err != nil {
		return make([]bool, len(paths)), err
	}

	return resp.Bools, nil
}

// ReadDir implements Host.
func (r *Remote) ReadDir(path string) ([]DirEntry, error) {
	resp, err := r.call(context.Background(), request{Op: opReadDir, Path: path})

	return resp.Entries, err
}

// MkdirAll implements Host.
func (r *Remote) MkdirAll(path string) error {
	defer r.changed()

	_, err := r.call(context.Background(), request{Op: opMkdirAll, Path: path})

	return err
}

// Rename implements Host.
func (r *Remote) Rename(from, to string) error {
	defer r.changed()

	_, err := r.call(context.Background(), request{Op: opRename, Path: from, To: to})

	return err
}

// Home implements Host, the home directory reported when connecting.
func (r *Remote) Home() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.home == "" {
		return "", ErrDisconnected
	}

	return r.home, nil
}

// PID implements Host, the agent of the current connection.
func (r *Remote) PID() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.pid
}

// Cwd is the directory the agent started in.
func (r *Remote) Cwd() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.cwd
}

// LookPath implements Host.
func (r *Remote) LookPath(name string) (string, error) {
	resp, err := r.call(context.Background(), request{Op: opLookPath, Path: name})

	return resp.Text, err
}

// Run implements Host. The context bounds the command, not the call
// timeout.
func (r *Remote) Run(ctx context.Context, cmd Command) ([]byte, []byte, error) {
	defer r.changed()

	resp, err := r.call(ctx, request{Op: opRun, Cmd: cmd})
	if err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			return resp.Data, exitErr.Stderr, err
		}

		return nil, nil, err
	}

	return resp.Data, resp.Stderr, nil
}

// Walk implements Host.
func (r *Remote) Walk(ctx context.Context, root string, opts walk.Options, fn func(rel string) bool) error {
	return r.stream(ctx, request{Op: opWalk, Path: root, Walk: opts}, func(resp response) bool {
		for _, p := range resp.Paths {
			if !fn(p) {
				return false
			}
		}

		return true
	})
}

// Search implements Host.
func (r *Remote) Search(ctx context.Context, root string, opts walk.Options, q search.Query, fn func(search.Match) bool) error {
	return r.stream(ctx, request{Op: opSearch, Path: root, Walk: opts, Query: q}, func(resp response) bool {
		for _, m := range resp.Matches {
			if !fn(m) {
				return false
			}
		}

		return true
	})
}

// Describe implements Host.
func (r *Remote) Describe(path string) (Description, error) {
	if e, ok := r.cached(path); ok && e.desc != nil {
		return *e.desc, nil
	}

	resp, err := r.call(context.Background(), request{Op: opDescribe, Path: path})
	if err != nil {
		return Description{}, err
	}

	r.mu.Lock()
	e := r.cache[path]
	if time.Since(e.at) >= cacheTTL {
		e = cacheEntry{}
	}

	e.at, e.desc = time.Now(), &resp.Desc
	r.cache[path] = e
	r.mu.Unlock()

	return resp.Desc, nil
}

// Start implements Host.
func (r *Remote) Start(cmd Command) (Process, error) {
	r.mu.Lock()
	conn := r.conn
	r.nextID++
	id := r.nextID
	r.mu.Unlock()
	if conn == nil {
		return nil, ErrDisconnected
	}

	p := &remoteProcess{conn: conn, id: id, out: newBufPipe(), exit: make(chan error, 1)}
	conn.mu.Lock()
	conn.procs[id] = p
	conn.mu.Unlock()

	if _, err := r.call(context.Background(), request{Op: opStart, Cmd: cmd, Proc: id}); err != nil {
		conn.mu.Lock()
		delete(conn.procs, id)
		conn.mu.Unlock()

		return nil, fmt.Errorf("start %s: %w", cmd.Name, err)
	}

	return p, nil
}

// Dial connects to a TCP address as seen from the agent, such as a web
// page a language server serves on the loopback address there.
func (r *Remote) Dial(addr string) (io.ReadWriteCloser, error) {
	r.mu.Lock()
	conn := r.conn
	r.nextID++
	id := r.nextID
	r.mu.Unlock()
	if conn == nil {
		return nil, ErrDisconnected
	}

	p := &remoteProcess{conn: conn, id: id, out: newBufPipe(), exit: make(chan error, 1)}
	conn.mu.Lock()
	conn.procs[id] = p
	conn.mu.Unlock()

	if _, err := r.call(context.Background(), request{Op: opDial, Path: addr, Proc: id}); err != nil {
		conn.mu.Lock()
		delete(conn.procs, id)
		conn.mu.Unlock()

		return nil, fmt.Errorf("dial %s on %s: %w", addr, r.name, err)
	}

	return remoteConnStream{p}, nil
}

// remoteConnStream is a connection made by the agent.
type remoteConnStream struct {
	p *remoteProcess
}

func (c remoteConnStream) Read(b []byte) (int, error)  { return c.p.out.Read(b) }
func (c remoteConnStream) Write(b []byte) (int, error) { return processInput(c).Write(b) }
func (c remoteConnStream) Close() error                { return c.p.Kill() }

// remoteProcess is a process running on the agent side.
type remoteProcess struct {
	conn     *remoteConn
	id       uint64
	out      *bufPipe
	exit     chan error
	exitOnce sync.Once
}

func (p *remoteProcess) deliver(resp response) {
	if resp.Done {
		p.out.CloseWithError(nil)
		p.exitOnce.Do(func() { p.exit <- fromWire(resp.Err) })

		return
	}

	p.out.Write(resp.Data)
}

func (p *remoteProcess) fail(err error) {
	p.out.CloseWithError(err)
	p.exitOnce.Do(func() { p.exit <- err })
}

func (p *remoteProcess) Stdin() io.WriteCloser { return processInput{p} }
func (p *remoteProcess) Stdout() io.Reader     { return p.out }

func (p *remoteProcess) Wait() error {
	err := <-p.exit
	p.exit <- err

	return err
}

func (p *remoteProcess) Kill() error {
	return p.conn.send(request{Op: opKill, Proc: p.id})
}

// processInput forwards writes to the stdin of a remote process.
type processInput struct {
	p *remoteProcess
}

func (in processInput) Write(data []byte) (int, error) {
	// gob sends an empty slice as nil, there is nothing to send anyway.
	if len(data) == 0 {
		return 0, nil
	}

	if err := in.p.conn.send(request{Op: opStdin, Proc: in.p.id, Data: data}); err != nil {
		return 0, ErrDisconnected
	}

	return len(data), nil
}

func (in processInput) Close() error {
	return in.p.conn.send(request{Op: opStdinClose, Proc: in.p.id})
}
