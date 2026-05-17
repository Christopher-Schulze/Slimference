package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"
)

const rawScopedWSSHeaderTimeout = 2 * time.Second

type rawScopedWSSListener struct {
	net.Listener
	Tunnel       *WebSocketTunnel
	UpstreamHost string
	OnIntercept  func(path string, header []byte)
}

func (l *rawScopedWSSListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		handled, replay := l.maybeHandle(conn)
		if handled {
			continue
		}
		return replay, nil
	}
}

func (l *rawScopedWSSListener) maybeHandle(conn net.Conn) (bool, net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(rawScopedWSSHeaderTimeout))
	header, err := readHTTPHeader(conn, initialHTTPHeaderLimit)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil && len(header) == 0 {
		return false, conn
	}
	replay := &prefetchedConn{Conn: conn, prefix: bytes.NewReader(header)}
	parsed, ok := parseHTTPRequestHeader(header)
	if !ok || !isRawScopedCodexWSS(parsed) || l.Tunnel == nil || l.UpstreamHost == "" {
		return false, replay
	}
	if l.OnIntercept != nil {
		l.OnIntercept(parsed.path, header)
	}
	go func() {
		defer conn.Close()
		l.Tunnel.ServeRawUpgrade(context.Background(), conn, header, l.UpstreamHost, parsed.path)
	}()
	return true, nil
}

type prefetchedConn struct {
	net.Conn
	prefix *bytes.Reader
}

func (c *prefetchedConn) Read(p []byte) (int, error) {
	if c.prefix != nil && c.prefix.Len() > 0 {
		n, err := c.prefix.Read(p)
		if errors.Is(err, io.EOF) {
			return n, nil
		}
		return n, err
	}
	return c.Conn.Read(p)
}

func isRawScopedCodexWSS(h parsedHTTPRequestHeader) bool {
	if !strings.EqualFold(h.method, "GET") || !h.websocket {
		return false
	}
	path := h.path
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if path != "/backend-api/codex/responses" {
		return false
	}
	for _, protocol := range strings.Split(h.subprotocol, ",") {
		if strings.HasPrefix(strings.TrimSpace(protocol), "responses_websockets") {
			return true
		}
	}
	return false
}
