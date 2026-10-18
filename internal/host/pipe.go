package host

import (
	"bytes"
	"io"
	"sync"
)

// bufPipe is a pipe whose writer never blocks, the output of a remote
// process waits in memory until its reader takes it.
type bufPipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	err    error
	closed bool
}

func newBufPipe() *bufPipe {
	p := &bufPipe{}
	p.cond = sync.NewCond(&p.mu)

	return p
}

func (p *bufPipe) Write(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.closed {
		p.buf.Write(data)
		p.cond.Broadcast()
	}
}

// CloseWithError ends the stream, readers get err after the buffered
// data, io.EOF when err is nil.
func (p *bufPipe) CloseWithError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	p.closed = true
	p.err = err
	if p.err == nil {
		p.err = io.EOF
	}

	p.cond.Broadcast()
}

func (p *bufPipe) Read(out []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for p.buf.Len() == 0 && !p.closed {
		p.cond.Wait()
	}

	if p.buf.Len() > 0 {
		return p.buf.Read(out)
	}

	return 0, p.err
}
