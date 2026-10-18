package lsp

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"strings"
	"testing"
	"time"
)

// TestInitializeSnippetSupport advertises snippets only when they are on.
func TestInitializeSnippetSupport(t *testing.T) {
	t.Parallel()

	for _, off := range []bool{false, true} {
		clientIn, serverOut := io.Pipe()
		serverIn, clientOut := io.Pipe()
		got := make(chan bool, 1)
		server := NewConn(serverIn, serverOut, nil)
		server.OnRequest = func(method string, params jsontext.Value) (any, *ResponseError) {
			if method == "initialize" {
				var p struct {
					Capabilities struct {
						TextDocument struct {
							Completion struct {
								CompletionItem struct {
									SnippetSupport bool `json:"snippetSupport"`
								} `json:"completionItem"`
							} `json:"completion"`
						} `json:"textDocument"`
					} `json:"capabilities"`
				}
				_ = json.Unmarshal(params, &p)
				got <- p.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport
			}

			return map[string]any{"capabilities": map[string]any{}}, nil
		}
		go server.Serve()

		c := NewClient("fake", "/", clientIn, clientOut, clientOut, func(fn func()) { fn() })
		c.NoSnippets = off
		c.Initialize(nil)
		select {
		case snippets := <-got:
			if snippets == off {
				t.Fatalf("NoSnippets %v sent snippetSupport %v", off, snippets)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no initialize request")
		}

		_ = clientOut.Close()
		_ = serverOut.Close()
	}
}

// TestInitializeFailureStopsServer closes the connection when the
// handshake fails and reports the exit once, with the handshake error.
func TestInitializeFailureStopsServer(t *testing.T) {
	t.Parallel()

	clientIn, serverOut := io.Pipe()
	serverIn, clientOut := io.Pipe()
	server := NewConn(serverIn, serverOut, nil)
	server.OnRequest = func(method string, _ jsontext.Value) (any, *ResponseError) {
		return nil, &ResponseError{Code: -32602, Message: "bad options"}
	}
	server.OnClose = func(error) { _ = serverOut.Close() }
	go server.Serve()

	exits := make(chan error, 4)
	c := NewClient("fake", "/", clientIn, clientOut, clientOut, func(fn func()) { fn() })
	c.OnExit = func(err error) { exits <- err }
	c.Initialize(nil)
	select {
	case err := <-exits:
		if err == nil || !strings.Contains(err.Error(), "bad options") {
			t.Fatalf("exit reason %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no exit reported")
	}

	select {
	case err := <-exits:
		t.Fatalf("exit reported twice: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	// A request on the dead connection fails right away.
	answered := make(chan error, 1)
	c.conn.Request("textDocument/hover", nil, func(_ jsontext.Value, err error) { answered <- err })
	select {
	case err := <-answered:
		if err == nil {
			t.Fatal("request on a closed connection succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("request on a closed connection never answered")
	}
}

func TestShortVersion(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		`{"GoVersion":"go1.27.0","Main":{"Path":"golang.org/x/tools/gopls","Version":"v0.23.0"}}`: "v0.23.0",
		"1.2.3":                 "1.2.3",
		"":                      "",
		"3.0\nbuilt by someone": "3.0",
	} {
		if got := shortVersion(in); got != want {
			t.Errorf("shortVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
