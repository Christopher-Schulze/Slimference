package proxy

import (
	"errors"
	"net"
	"sync"
)

// singleConnListener wraps a single net.Conn so it can be served by a
// fresh http.Server. The first Accept returns the wrapped connection;
// every subsequent Accept returns errSingleConnClosed so the server's
// accept loop exits cleanly. Used by the CONNECT-MITM dispatcher to
// re-enter http.Server semantics after the TLS upgrade.
type singleConnListener struct {
	conn net.Conn
	addr net.Addr

	mu     sync.Mutex
	served bool
	closed bool
}

var errSingleConnClosed = errors.New("single-conn listener closed")

func newSingleConnListener(c net.Conn) *singleConnListener {
	addr := c.LocalAddr()
	if addr == nil {
		addr = &net.TCPAddr{}
	}
	return &singleConnListener{conn: c, addr: addr}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, errSingleConnClosed
	}
	if l.served {
		return nil, errSingleConnClosed
	}
	l.served = true
	return l.conn, nil
}

func (l *singleConnListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return l.addr
}
