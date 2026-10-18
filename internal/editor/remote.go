package editor

import (
	"errors"
	"io"
	"net"
	"net/url"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// statusNotifier is a host that reports connection changes.
type statusNotifier interface {
	OnStatus(fn func(host.Status))
}

// reconnecter is a host that can be told to connect again.
type reconnecter interface {
	Reconnect()
}

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"reconnect"},
		doc:   "Connect to the remote host again",
		run: func(cx *Context, _ []string) error {
			r, ok := cx.Editor.Host.(reconnecter)
			if !ok {
				return cmdError("the workspace is local")
			}

			r.Reconnect()
			cx.Editor.SetStatus("%s: connecting", cx.Editor.Host.Name())

			return nil
		},
	})
}

// WatchHost follows the connection of a remote workspace host. The host
// reconnects by itself, the editor warns while it is down and restarts
// what lived on the other side once it is back.
func (e *Editor) WatchHost() {
	n, ok := e.Host.(statusNotifier)
	if !ok {
		return
	}

	n.OnStatus(func(s host.Status) {
		e.events <- func(e *Editor) { e.hostStatusChanged(s) }
	})
}

func (e *Editor) hostStatusChanged(s host.Status) {
	name := e.Host.Name()
	wasDown := e.hostDown
	e.hostDown = !s.Connected
	switch {
	case !s.Connected && !wasDown:
		e.logf("%s: connection lost: %s", name, s.Err)
		e.SetError("%s: no connection, reconnecting", name)
	case !s.Connected:
		e.logf("%s: reconnect failed: %s", name, s.Err)
	case wasDown:
		e.SetStatus("%s: connected again", name)
		e.restoreRemote()
	}
}

// restoreRemote restarts the language servers and version control state
// of the remote documents after a reconnect and catches up with changes
// made meanwhile.
func (e *Editor) restoreRemote() {
	for _, doc := range e.documentsByID() {
		if doc.Path() == "" || e.hostOf(doc) != e.Host {
			continue
		}

		e.attachLSP(doc)
		e.attachVCS(doc)
	}

	e.resumeTails()
	e.CheckDiskChanges()
}

// hostGone reports whether err comes from a lost connection, which the
// connection warning already covers.
func (e *Editor) hostGone(err error) bool {
	return errors.Is(err, host.ErrDisconnected) || !e.Host.Status().Connected
}

// dialer is a host that opens TCP connections on its side.
type dialer interface {
	Dial(addr string) (io.ReadWriteCloser, error)
}

// forwardURL makes a page served on the loopback address of a remote host
// reachable from here: a local port relays every connection through the
// agent. The port stays open for later pages of the same server.
func (e *Editor) forwardURL(h host.Host, raw string) (string, error) {
	d, ok := h.(dialer)
	if !ok {
		return "", errors.New("the host cannot forward connections")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	remote := u.Host
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}

		remote = net.JoinHostPort(u.Hostname(), port)
	}

	key := h.Name() + " " + remote
	local, ok := e.forwards[key]
	if !ok {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}

		go relay(l, d, remote)

		if e.forwards == nil {
			e.forwards = map[string]string{}
		}

		local = l.Addr().String()
		e.forwards[key] = local
	}

	u.Host = local

	return u.String(), nil
}

// relay copies every connection accepted on l to remote through d, both
// ways, until either side closes.
func relay(l net.Listener, d dialer, remote string) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}

		go func() {
			defer func() { _ = c.Close() }()

			r, err := d.Dial(remote)
			if err != nil {
				return
			}

			defer func() { _ = r.Close() }()

			done := make(chan struct{}, 2)
			go func() {
				_, _ = io.Copy(r, c)
				done <- struct{}{}
			}()
			go func() {
				_, _ = io.Copy(c, r)
				done <- struct{}{}
			}()
			<-done
		}()
	}
}
