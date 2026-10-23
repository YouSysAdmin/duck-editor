package lsp

import (
	"bufio"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResponseError is a JSON-RPC error object.
type ResponseError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    jsontext.Value `json:"data,omitempty"`
}

func (e *ResponseError) Error() string {
	return e.Message
}

type message struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      jsontext.Value `json:"id,omitempty"`
	Method  string         `json:"method,omitempty"`
	Params  jsontext.Value `json:"params,omitempty"`
	Result  jsontext.Value `json:"result,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
}

// ResponseHandler receives the result of a request.
type ResponseHandler func(result jsontext.Value, err error)

// AsyncResult is a result OnRequest returns to answer the request later,
// the connection keeps reading meanwhile. It is called with the function
// that sends the response, the first call wins.
type AsyncResult func(reply func(result any, rerr *ResponseError))

// Conn is a JSON-RPC connection with Content-Length framing.
type Conn struct {
	w       io.Writer
	r       *bufio.Reader
	closer  io.Closer
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int
	pending map[int]ResponseHandler

	// closed is set once the read loop ended or Close ran, requests
	// made after that fail right away.
	closed bool

	// OnNotify handles server notifications.
	OnNotify func(method string, params jsontext.Value)

	// OnRequest handles server requests and returns a result or error.
	OnRequest func(method string, params jsontext.Value) (any, *ResponseError)

	// OnClose runs when the read loop ends.
	OnClose func(err error)
}

// NewConn wraps a reader and writer. closer is closed by Close.
func NewConn(r io.Reader, w io.Writer, closer io.Closer) *Conn {
	return &Conn{w: w, r: bufio.NewReaderSize(r, 1<<20), closer: closer, pending: map[int]ResponseHandler{}}
}

// trafficLog appends every message to the file named by DUCK_LSP_LOG.
var trafficLog = func() io.Writer {
	path := os.Getenv("DUCK_LSP_LOG")
	if path == "" {
		return nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}

	return f
}()

var trafficMu sync.Mutex

func logTraffic(dir string, data []byte) {
	if trafficLog == nil {
		return
	}

	trafficMu.Lock()
	defer trafficMu.Unlock()
	_, _ = fmt.Fprintf(trafficLog, "%s %s %s\n", time.Now().Format("15:04:05.000"), dir, data)
}

// Close stops the connection and fails pending requests.
func (c *Conn) Close() error {
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = map[int]ResponseHandler{}
	c.mu.Unlock()
	for _, h := range pending {
		h(nil, errConnClosed)
	}

	if c.closer != nil {
		return c.closer.Close()
	}

	return nil
}

func (c *Conn) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	logTraffic("->", data)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}

	_, err = c.w.Write(data)

	return err
}

// errConnClosed answers requests on a connection that ended.
var errConnClosed = errors.New("connection closed")

// Request sends a request and calls handler with the response.
func (c *Conn) Request(method string, params any, handler ResponseHandler) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		if handler != nil {
			handler(nil, errConnClosed)
		}

		return
	}

	c.nextID++
	id := c.nextID
	if handler != nil {
		c.pending[id] = handler
	}

	c.mu.Unlock()

	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", id, method, params}
	if err := c.send(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		if handler != nil {
			handler(nil, err)
		}
	}
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", method, params}

	return c.send(msg)
}

// respond answers a server request. A response carries either a result
// or an error, never both.
func (c *Conn) respond(id jsontext.Value, result any, rerr *ResponseError) error {
	if rerr != nil {
		return c.send(struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      jsontext.Value `json:"id"`
			Error   *ResponseError `json:"error"`
		}{"2.0", id, rerr})
	}

	return c.send(struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      jsontext.Value `json:"id"`
		Result  any            `json:"result"`
	}{"2.0", id, result})
}

// Serve reads messages until the connection fails. It blocks, call it in
// a goroutine.
func (c *Conn) Serve() {
	err := c.readLoop()
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = map[int]ResponseHandler{}
	c.mu.Unlock()
	for _, h := range pending {
		h(nil, errConnClosed)
	}

	if c.OnClose != nil {
		c.OnClose(err)
	}
}

func (c *Conn) readLoop() error {
	for {
		data, err := c.readMessage()
		if err != nil {
			return err
		}

		logTraffic("<-", data)
		var msg message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch {
		case msg.Method != "" && len(msg.ID) > 0:
			var result any
			var rerr *ResponseError
			if c.OnRequest != nil {
				result, rerr = c.OnRequest(msg.Method, msg.Params)
			} else {
				rerr = &ResponseError{Code: -32601, Message: "method not found"}
			}

			if async, ok := result.(AsyncResult); ok && rerr == nil {
				id := msg.ID
				var once sync.Once
				async(func(result any, rerr *ResponseError) {
					once.Do(func() { _ = c.respond(id, result, rerr) })
				})

				continue
			}

			_ = c.respond(msg.ID, result, rerr)
		case msg.Method != "":
			if c.OnNotify != nil {
				c.OnNotify(msg.Method, msg.Params)
			}
		case len(msg.ID) > 0:
			id, err := strconv.Atoi(strings.TrimSpace(string(msg.ID)))
			if err != nil {
				continue
			}

			c.mu.Lock()
			handler := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if handler == nil {
				continue
			}

			if msg.Error != nil {
				handler(nil, msg.Error)
			} else {
				handler(msg.Result, nil)
			}
		}
	}
}

func (c *Conn) readMessage() ([]byte, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
		}
	}

	if length < 0 {
		return nil, errors.New("missing Content-Length header")
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return nil, err
	}

	return buf, nil
}
