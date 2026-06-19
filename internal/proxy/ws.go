package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

// WebSocketDialer captures the upstream-dial dependency that the
// WebSocket tunnel uses. Production wires this to a function that
// performs `tls.Dial("tcp", host:443, cfg)`; tests pin it to a
// pre-made loopback connection.
type WebSocketDialer func(host, port string) (net.Conn, error)

// WebSocketFrameBridge receives the post-upgrade client and upstream
// streams. Implementations may parse and mutate frames or fall back to
// byte-equal forwarding.
type WebSocketFrameBridge func(ctx context.Context, client, upstream net.Conn, opts WebSocketBridgeOptions) error

// WebSocketBridgeOptions carries handshake metadata into the post-101
// frame bridge.
type WebSocketBridgeOptions struct {
	Extensions   wscompact.WSExtensionProfile
	UserAgent    string
	ClientFamily string
}

// WebSocketTunnel handles `Upgrade: websocket` requests intercepted
// by the MITM dispatch. Once the upgrade handshake succeeds against
// upstream, frames either pass through byte-equal or, for scoped Codex
// conversation WSS, run through the Phase-F frame bridge.
type WebSocketTunnel struct {
	Dialer      WebSocketDialer
	Logger      *slog.Logger
	BypassPaths []string
	Inspector   wscompact.Inspector
	FrameBridge WebSocketFrameBridge
	ByteBridge  WebSocketFrameBridge
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
// Provider-Invisibility (docs/spec.md §16.4): we do NOT add Slimference-
// identifying headers, do NOT override Connection / Host, and do NOT
// rewrite the WebSocket-Key. Headers go through verbatim.
func (t *WebSocketTunnel) ServeUpgrade(clientConn net.Conn, r *http.Request, host string) {
	t.ServeUpgradeWithBridge(clientConn, r, host, true)
}

// ServeUpgradeWithBridge is ServeUpgrade with an explicit frame-bridge gate.
// CONNECT/MITM callers use this to forward non-conversation WebSockets
// byte-equal while still allowing the scoped Codex conversation route to
// reuse Phase-F.
func (t *WebSocketTunnel) ServeUpgradeWithBridge(clientConn net.Conn, r *http.Request, host string, bridgeFrames bool) {
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
	if bridgeFrames && !t.IsAudioBypassPath(r.URL.Path) {
		frameBridge := t.FrameBridge
		if isCodexBridgePath(r.URL.Path) && t.ByteBridge != nil {
			frameBridge = t.ByteBridge
		}
		if frameBridge == nil {
			flushBufferedUpstream(clientConn, upstreamReader)
			if t.Inspector == nil {
				pipeBytes(clientConn, upstream)
				return
			}
			pipeWebSocketBytes(clientConn, upstream, r.URL.Path, t.Inspector)
			return
		}
		bridgeUpstream := net.Conn(upstream)
		if upstreamReader.Buffered() > 0 {
			bridgeUpstream = &bufferedReadConn{Conn: upstream, reader: upstreamReader}
		}
		userAgent := r.UserAgent()
		opts := WebSocketBridgeOptions{
			UserAgent:    userAgent,
			ClientFamily: normalizeCodexClientFamily(userAgent),
			Extensions: wscompact.NegotiatePermessageDeflate(
				strings.Join(r.Header.Values("Sec-WebSocket-Extensions"), ", "),
				strings.Join(resp.Header.Values("Sec-WebSocket-Extensions"), ", "),
			),
		}
		if err := frameBridge(r.Context(), clientConn, bridgeUpstream, opts); err != nil {
			t.logf("websocket: frame bridge ended", "host", host, "err", err)
		}
		return
	}
	flushBufferedUpstream(clientConn, upstreamReader)
	if t.Inspector == nil {
		pipeBytes(clientConn, upstream)
		return
	}
	pipeWebSocketBytes(clientConn, upstream, r.URL.Path, t.Inspector)
}

// ServeRawUpgrade handles a WebSocket Upgrade whose HTTP header was
// captured before net/http normalised it. It forwards the original
// header order/casing upstream, changing only authority where needed,
// then enters the same post-101 bridge as ServeUpgrade.
func (t *WebSocketTunnel) ServeRawUpgrade(ctx context.Context, clientConn net.Conn, rawHeader []byte, host, path string) {
	if t.Dialer == nil {
		t.logf("websocket raw: no dialer configured; dropping upgrade", "host", host)
		return
	}
	upstream, err := t.Dialer(host, "443")
	if err != nil {
		t.logf("websocket raw: upstream dial failed", "host", host, "err", err)
		_ = writeBadGateway(clientConn)
		return
	}
	defer upstream.Close()

	if _, err := upstream.Write(rewriteRawUpgradeHeader(rawHeader, host)); err != nil {
		t.logf("websocket raw: forward request failed", "host", host, "err", err)
		_ = writeBadGateway(clientConn)
		return
	}

	upstreamReader := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(upstreamReader, nil)
	if err != nil {
		t.logf("websocket raw: read upstream response failed", "host", host, "err", err)
		_ = writeBadGateway(clientConn)
		return
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = forwardResponse(clientConn, resp)
		return
	}
	_ = forwardResponse(clientConn, resp)
	if !t.IsAudioBypassPath(path) {
		frameBridge := t.FrameBridge
		if isCodexBridgePath(path) && t.ByteBridge != nil {
			frameBridge = t.ByteBridge
		}
		if frameBridge == nil {
			flushBufferedUpstream(clientConn, upstreamReader)
			if t.Inspector == nil {
				pipeBytes(clientConn, upstream)
				return
			}
			pipeWebSocketBytes(clientConn, upstream, path, t.Inspector)
			return
		}
		bridgeUpstream := net.Conn(upstream)
		if upstreamReader.Buffered() > 0 {
			bridgeUpstream = &bufferedReadConn{Conn: upstream, reader: upstreamReader}
		}
		userAgent := strings.Join(rawHTTPHeaderValues(rawHeader, "User-Agent"), ", ")
		opts := WebSocketBridgeOptions{
			UserAgent:    userAgent,
			ClientFamily: normalizeCodexClientFamily(userAgent),
			Extensions: wscompact.NegotiatePermessageDeflate(
				strings.Join(rawHTTPHeaderValues(rawHeader, "Sec-WebSocket-Extensions"), ", "),
				strings.Join(resp.Header.Values("Sec-WebSocket-Extensions"), ", "),
			),
		}
		if err := frameBridge(ctx, clientConn, bridgeUpstream, opts); err != nil {
			t.logf("websocket raw: frame bridge ended", "host", host, "err", err)
		}
		return
	}
	flushBufferedUpstream(clientConn, upstreamReader)
	if t.Inspector == nil {
		pipeBytes(clientConn, upstream)
		return
	}
	pipeWebSocketBytes(clientConn, upstream, path, t.Inspector)
}

type bufferedReadConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedReadConn) Read(p []byte) (int, error) {
	if c.reader != nil && c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}

func flushBufferedUpstream(clientConn net.Conn, upstreamReader *bufio.Reader) {
	if buffered := upstreamReader.Buffered(); buffered > 0 {
		bytes, _ := upstreamReader.Peek(buffered)
		_, _ = clientConn.Write(bytes)
		_, _ = upstreamReader.Discard(buffered)
	}
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
	clone.URL.Path = canonicalCodexBridgePath(clone.URL.Path)
	clone.URL.RawPath = ""
	clone.RequestURI = ""
	return clone.Write(w)
}

func rewriteRawUpgradeHeader(header []byte, host string) []byte {
	sep := "\r\n"
	text := string(header)
	if !strings.Contains(text, "\r\n") && strings.Contains(text, "\n") {
		sep = "\n"
	}
	lines := strings.Split(text, sep)
	if len(lines) == 0 {
		return append([]byte(nil), header...)
	}
	lines[0] = rewriteRequestLineTarget(lines[0])
	hostSeen := false
	for i := 1; i < len(lines); i++ {
		name, _, ok := strings.Cut(lines[i], ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "host") {
			continue
		}
		hostSeen = true
		lines[i] = name + ": " + host
	}
	if !hostSeen && len(lines) > 1 {
		next := make([]string, 0, len(lines)+1)
		next = append(next, lines[0], "Host: "+host)
		next = append(next, lines[1:]...)
		lines = next
	}
	return []byte(strings.Join(lines, sep))
}

func rawHTTPHeaderValues(header []byte, name string) []string {
	sep := "\r\n"
	text := string(header)
	if !strings.Contains(text, "\r\n") && strings.Contains(text, "\n") {
		sep = "\n"
	}
	var values []string
	for _, line := range strings.Split(text, sep) {
		field, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(field), name) {
			continue
		}
		values = append(values, strings.TrimSpace(value))
	}
	return values
}

func rewriteRequestLineTarget(line string) string {
	parts := strings.Split(line, " ")
	if len(parts) != 3 {
		return line
	}
	target := canonicalCodexBridgePath(normaliseHTTPRequestTarget(parts[1]))
	if target == parts[1] {
		return line
	}
	return parts[0] + " " + target + " " + parts[2]
}

func normaliseHTTPRequestTarget(target string) string {
	u, err := url.Parse(target)
	if err != nil || !u.IsAbs() {
		return target
	}
	out := u.RequestURI()
	if out == "" {
		return "/"
	}
	return out
}

func isCodexBridgePath(path string) bool {
	clean := path
	if idx := strings.Index(clean, "?"); idx >= 0 {
		clean = clean[:idx]
	}
	clean = strings.TrimSuffix(clean, "/")
	return clean == "/backend-api/codex-bridge/responses"
}

func canonicalCodexBridgePath(path string) string {
	if strings.HasPrefix(path, "/backend-api/codex-bridge/") {
		return "/backend-api/codex/" + strings.TrimPrefix(path, "/backend-api/codex-bridge/")
	}
	if path == "/backend-api/codex-bridge" {
		return "/backend-api/codex"
	}
	return path
}

func forwardResponse(client net.Conn, resp *http.Response) error {
	return resp.Write(client)
}

func writeBadGateway(client net.Conn) error {
	_, err := client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
	return err
}

func pipeWebSocketBytes(client, upstream net.Conn, route string, inspector wscompact.Inspector) {
	inspector = wscompact.RouteInspector(route, inspector)
	done := make(chan struct{}, 2)
	go func() {
		_, _ = wscompact.InspectStream(client, upstream, wscompact.DirectionServerToClient, inspector)
		done <- struct{}{}
	}()
	go func() {
		_, _ = wscompact.InspectStream(upstream, client, wscompact.DirectionClientToServer, inspector)
		done <- struct{}{}
	}()
	<-done
}

// DefaultWebSocketDialer is the production dial used when the system
// proxy routes traffic to Slimference. It performs TLS over TCP to
// the upstream host on port 443 with the host as SNI.
func DefaultWebSocketDialer(host, port string) (net.Conn, error) {
	addr := net.JoinHostPort(host, port)
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	return tls.Dial("tcp", addr, cfg)
}
