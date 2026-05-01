package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// WebSocketDialer captures the upstream-dial dependency that the
// WebSocket tunnel uses. Production wires this to a function that
// performs `tls.Dial("tcp", host:443, cfg)`; tests pin it to a
// pre-made loopback connection.
type WebSocketDialer func(host, port string) (net.Conn, error)

// WebSocketTunnel handles `Upgrade: websocket` requests intercepted
// by the MITM dispatch. Once the upgrade handshake succeeds against
// upstream, frames flow byte-for-byte in both directions until either
// side closes. Compression on `responses`-shaped streams is a follow-up
// (Layer 1/2 on WebSocket message boundaries); for now this is a pure
// tunnel so Codex Desktop's `responses_websocket` traffic completes
// rather than being denied.
type WebSocketTunnel struct {
	Dialer      WebSocketDialer
	Logger      *slog.Logger
	BypassPaths []string
}

// IsWebSocketUpgrade reports whether the request is asking for a
// WebSocket upgrade per RFC 6455.
func IsWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	if !connectionHasUpgrade(r.Header.Get("Connection")) {
		return false
	}
	return true
}

// connectionHasUpgrade is more lenient than EqualFold("upgrade") - a
// Connection header may be a comma-separated list with mixed casing
// (e.g. "keep-alive, Upgrade").
func connectionHasUpgrade(connHeader string) bool {
	for _, p := range strings.Split(connHeader, ",") {
		if strings.EqualFold(strings.TrimSpace(p), "upgrade") {
			return true
		}
	}
	return false
}

// IsAudioBypassPath returns true when the request URL matches any of
// the configured audio-bypass patterns. Audio paths (Realtime API,
// WebRTC signalling) are tunneled byte-for-byte without inspection so
// the latency budget is never affected by Slimference.
func (t *WebSocketTunnel) IsAudioBypassPath(p string) bool {
	for _, pattern := range t.BypassPaths {
		if pattern == "" {
			continue
		}
		if strings.Contains(p, pattern) {
			return true
		}
	}
	// Built-in baseline patterns for OpenAI Realtime / WebRTC.
	for _, pattern := range []string{"/v1/realtime", "/realtime", "webrtc"} {
		if strings.Contains(p, pattern) {
			return true
		}
	}
	return false
}

// ServeUpgrade handles the upgrade handshake against the upstream and
// then runs the bidirectional pump. The clientConn is the TLS-decoded
// stream from the MITM dispatch; the original Request carries the
// upgrade headers as written by the app.
//
// Provider-Invisibility (spec+.md §16.4): we do NOT add Slimference-
// identifying headers, do NOT override Connection / Host, and do NOT
// rewrite the WebSocket-Key. Headers go through verbatim.
func (t *WebSocketTunnel) ServeUpgrade(clientConn net.Conn, r *http.Request, host string) {
	if t.Dialer == nil {
		t.logf("websocket: no dialer configured; dropping upgrade", "host", host)
		return
	}
	upstream, err := t.Dialer(host, "443")
	if err != nil {
		t.logf("websocket: upstream dial failed", "host", host, "err", err)
		_ = writeBadGateway(clientConn)
		return
	}
	defer upstream.Close()

	if err := writeRequest(upstream, r, host); err != nil {
		t.logf("websocket: forward request failed", "host", host, "err", err)
		_ = writeBadGateway(clientConn)
		return
	}

	upstreamReader := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(upstreamReader, r)
	if err != nil {
		t.logf("websocket: read upstream response failed", "host", host, "err", err)
		_ = writeBadGateway(clientConn)
		return
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		// Upstream rejected the upgrade. Forward the response verbatim
		// so the client sees the real reason.
		_ = forwardResponse(clientConn, resp)
		return
	}
	// Forward the 101 response. If the write to the client fails, the
	// pump below will fail on the very next write and we exit cleanly;
	// no separate err handling needed.
	_ = forwardResponse(clientConn, resp)
	// Push any bytes already buffered by upstreamReader into the
	// client side first, then enter the bidirectional pump. If the
	// client write fails here the pump will return immediately on the
	// next iteration; no separate err handling needed.
	if buffered := upstreamReader.Buffered(); buffered > 0 {
		bytes, _ := upstreamReader.Peek(buffered)
		_, _ = clientConn.Write(bytes)
		_, _ = upstreamReader.Discard(buffered)
	}
	pipeBytes(clientConn, upstream)
}

func (t *WebSocketTunnel) logf(msg string, args ...any) {
	if t.Logger == nil {
		slog.Default().Debug(msg, args...)
		return
	}
	t.Logger.Debug(msg, args...)
}

func writeRequest(w io.Writer, r *http.Request, host string) error {
	// Reset URL host so the request line targets the upstream path.
	clone := r.Clone(r.Context())
	clone.Host = host
	clone.URL.Host = host
	clone.URL.Scheme = "http" // Write does not emit scheme; this avoids a panic on absolute URLs.
	clone.RequestURI = ""
	return clone.Write(w)
}

func forwardResponse(client net.Conn, resp *http.Response) error {
	return resp.Write(client)
}

func writeBadGateway(client net.Conn) error {
	_, err := client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
	return err
}

// DefaultWebSocketDialer is the production dial used when the system
// proxy routes traffic to Slimference. It performs TLS over TCP to
// the upstream host on port 443 with the host as SNI.
func DefaultWebSocketDialer(host, port string) (net.Conn, error) {
	addr := net.JoinHostPort(host, port)
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	return tls.Dial("tcp", addr, cfg)
}
