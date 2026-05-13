package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/planner"
	"github.com/slimference/slimference/internal/tlsca"
)

// ConnectInterceptor terminates `CONNECT host:port` tunnels by either:
//
//   - signing a leaf certificate for the SNI presented in the ClientHello
//     and re-entering the proxy's normal `ServeHTTP` dispatch with the
//     decoded inner request (so Layer 0/1/2 compression runs unchanged);
//     this is the MITM path used for hosts on the allowlist
//     (`api.openai.com`, `api.anthropic.com`, `chatgpt.com`).
//
//   - dialing upstream `host:port` with raw TCP and pumping bytes
//     bidirectionally; this is the passthrough path used for any host
//     not on the allowlist so Slimference can be the system HTTPS
//     proxy without breaking iCloud, GitHub, package mirrors, etc.
//
// The interceptor is wired by ProxyMITMHandler() onto the same HTTP
// server that handles regular requests. When CONNECT is rejected at the
// allowlist, the connection is still hijacked - we never let the client
// retry without a tunnel.
type ConnectInterceptor struct {
	signer             *tlsca.Signer
	innerHandler       http.Handler
	allowlist          map[string]struct{}
	dialUpstream       func(host string, port string) (net.Conn, error)
	logger             *slog.Logger
	tlsServerHandshake func(net.Conn, *tls.Config) (*tls.Conn, error)
	webSocketTunnel    *WebSocketTunnel
	debugRecorder      *dbg.Recorder
}

// NewConnectInterceptor wires a CONNECT-aware handler around the given
// inner dispatch (typically the proxy's existing ServeHTTP). signer
// produces leaf certs on demand. allowlist is the set of hosts on
// which MITM is enabled; everything else passes through as raw TCP.
// Both signer and allowlist may be nil/empty: the resulting handler
// responds 405 to every CONNECT, which preserves the pre-T122 direct-
// mode semantics.
func NewConnectInterceptor(signer *tlsca.Signer, inner http.Handler, allowlist []string) *ConnectInterceptor {
	allow := make(map[string]struct{}, len(allowlist))
	for _, h := range allowlist {
		allow[strings.ToLower(strings.TrimSpace(h))] = struct{}{}
	}
	return &ConnectInterceptor{
		signer:             signer,
		innerHandler:       inner,
		allowlist:          allow,
		dialUpstream:       defaultDialUpstream,
		logger:             slog.Default(),
		tlsServerHandshake: defaultTLSServerHandshake,
		webSocketTunnel:    &WebSocketTunnel{Dialer: DefaultWebSocketDialer, Logger: slog.Default()},
	}
}

// defaultTLSServerHandshake is the production TLS handshake used by
// the MITM path. Extracted as a top-level function (instead of a
// closure inside NewConnectInterceptor) so its err-from-handshake
// branch is independently coverable by tests that drive it with a
// closed connection.
func defaultTLSServerHandshake(c net.Conn, cfg *tls.Config) (*tls.Conn, error) {
	tc := tls.Server(c, cfg)
	if err := tc.Handshake(); err != nil {
		return nil, err
	}
	return tc, nil
}

// SetLogger overrides the slog instance used for diagnostic output.
func (ci *ConnectInterceptor) SetLogger(l *slog.Logger) {
	if l != nil {
		ci.logger = l
	}
}

// SetUpstreamDialer overrides the dialer used for raw passthrough; tests
// pin this to a loopback target.
func (ci *ConnectInterceptor) SetUpstreamDialer(d func(host string, port string) (net.Conn, error)) {
	ci.dialUpstream = d
}

// SetTLSServerHandshake overrides the TLS handshake used for the MITM
// path; tests can pin this to a stub that returns immediately on a
// bidirectional in-memory pipe.
func (ci *ConnectInterceptor) SetTLSServerHandshake(fn func(net.Conn, *tls.Config) (*tls.Conn, error)) {
	if fn != nil {
		ci.tlsServerHandshake = fn
	}
}

// SetWebSocketTunnel overrides the WebSocket upgrade relay used by the
// MITM path. Tests pin this to local pipes; production sets the TLS dialer.
func (ci *ConnectInterceptor) SetWebSocketTunnel(t *WebSocketTunnel) {
	if t != nil {
		ci.webSocketTunnel = t
	}
}

func (ci *ConnectInterceptor) SetDebugRecorder(r *dbg.Recorder) {
	ci.debugRecorder = r
}

// ServeHTTP fields the CONNECT request. For non-CONNECT methods the
// interceptor delegates straight to inner so a single dispatcher can
// front everything.
func (ci *ConnectInterceptor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		ci.innerHandler.ServeHTTP(w, r)
		return
	}
	if ci.signer == nil {
		http.Error(w, "CONNECT not supported in direct mode", http.StatusMethodNotAllowed)
		return
	}
	host, port, err := splitHostPort(r.Host)
	if err != nil {
		http.Error(w, "bad host:port", http.StatusBadRequest)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		// Once Hijack fails the response writer is unusable; log and
		// drop the request.
		ci.logger.Error("connect: hijack failed", "host", host, "err", err)
		return
	}
	defer clientConn.Close()
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		ci.logger.Error("connect: write 200 failed", "host", host, "err", err)
		return
	}
	if !ci.shouldMITM(host) {
		ci.recordFlight(host, "", "raw_passthrough", "host_not_intercepted", nil)
		ci.passthrough(clientConn, host, port)
		return
	}
	ci.recordFlight(host, "", "mitm_connect", "host_intercepted", nil)
	ci.mitm(clientConn, host)
}

// shouldMITM checks the allowlist. An empty allowlist means "MITM all
// hosts"; that is the operator opting into transparent mode for
// everything (rare, but supported). Hosts are compared lowercased.
func (ci *ConnectInterceptor) shouldMITM(host string) bool {
	if len(ci.allowlist) == 0 {
		return true
	}
	_, ok := ci.allowlist[strings.ToLower(host)]
	return ok
}

// passthrough is the raw-TCP relay for hosts off the allowlist. It is
// the property that lets transparent mode coexist with non-LLM HTTPS
// traffic on the same machine without breaking it.
func (ci *ConnectInterceptor) passthrough(client net.Conn, host, port string) {
	upstream, err := ci.dialUpstream(host, port)
	if err != nil {
		ci.logger.Error("connect passthrough: dial failed", "host", host, "port", port, "err", err)
		ci.recordFlight(host, "", "raw_passthrough", "dial_failed", err)
		return
	}
	defer upstream.Close()
	pipeBytes(client, upstream)
}

// mitm completes a TLS handshake against the client using a per-host
// leaf cert from the signer, then re-enters the proxy's normal HTTP
// dispatch. The decoded request flows through `innerHandler` exactly
// like a request that arrived on a config-patch direct port.
func (ci *ConnectInterceptor) mitm(client net.Conn, host string) {
	cert, err := ci.signer.Cert(host)
	if err != nil {
		ci.logger.Error("connect mitm: leaf cert failed", "host", host, "err", err)
		ci.recordFlight(host, "", "mitm_connect", "leaf_cert_failed", err)
		return
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
	}
	tlsConn, err := ci.tlsServerHandshake(client, tlsCfg)
	if err != nil {
		ci.logger.Debug("connect mitm: handshake failed", "host", host, "err", err)
		ci.recordFlight(host, "", "mitm_connect", "handshake_failed", err)
		return
	}
	defer tlsConn.Close()
	ci.servePlaintextOnTLS(tlsConn, host)
}

// servePlaintextOnTLS reads HTTP/1.x requests off the TLS stream and
// routes each through the inner handler. We do NOT use http.Server
// here because its Serve loop closes the connection as soon as Accept
// returns an error, racing with in-flight handlers; instead we drive
// the request/response cycle directly so the conn lifetime is bound
// to ours.
func (ci *ConnectInterceptor) servePlaintextOnTLS(tlsConn net.Conn, host string) {
	br := bufio.NewReader(tlsConn)
	for {
		_ = tlsConn.SetReadDeadline(time.Now().Add(120 * time.Second))
		req, err := http.ReadRequest(br)
		if err != nil {
			if err != io.EOF {
				ci.logger.Debug("connect mitm: read request ended", "host", host, "err", err)
			}
			return
		}
		req.Host = host
		req.URL.Host = host
		req.URL.Scheme = "https"
		req.RequestURI = ""
		if IsWebSocketUpgrade(req) && ci.webSocketTunnel != nil {
			ci.recordFlight(host, req.URL.Path, "websocket_tunnel", "upgrade", nil)
			clientConn := net.Conn(tlsConn)
			if br.Buffered() > 0 {
				clientConn = &bufferedNetConn{Conn: tlsConn, reader: br}
			}
			ci.webSocketTunnel.ServeUpgrade(clientConn, req, host)
			return
		}
		rw := newMITMResponseWriter(tlsConn)
		ci.innerHandler.ServeHTTP(rw, req)
		if err := rw.finish(); err != nil {
			ci.logger.Debug("connect mitm: response flush failed", "host", host, "err", err)
			return
		}
		if rw.streamed() {
			return
		}
		// Drain request body so the next request can be read.
		if req.Body != nil {
			_, _ = io.Copy(io.Discard, req.Body)
			_ = req.Body.Close()
		}
		if shouldClose(req) {
			return
		}
	}
}

func (ci *ConnectInterceptor) recordFlight(host, path, routeMode, reason string, err error) {
	if ci.debugRecorder == nil {
		return
	}
	var errorsOut []string
	if err != nil {
		errorsOut = []string{err.Error()}
	}
	ci.debugRecorder.Record(dbg.RequestSummary{
		RequestID:    fmt.Sprintf("connect-%d", time.Now().UnixNano()),
		Timestamp:    time.Now().UTC(),
		Source:       "transparent_connect",
		Provider:     providerForConnectHost(host),
		Host:         host,
		Path:         path,
		RouteMode:    routeMode,
		BypassReason: reason,
		Errors:       errorsOut,
		Plan: debugPlanSummary(planner.Plan(planner.RequestFacts{
			Provider:             providerForConnectHost(host),
			RouteMode:            routeMode,
			ContentClasses:       []string{"transparent_connect"},
			LiveCorpusConfidence: "unknown",
		})),
	})
}

func providerForConnectHost(host string) string {
	switch strings.ToLower(host) {
	case "api.anthropic.com":
		return "anthropic"
	case "api.openai.com":
		return "openai"
	case "chatgpt.com":
		return "codex_chatgpt"
	default:
		return "unknown"
	}
}

type bufferedNetConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedNetConn) Read(p []byte) (int, error) {
	if c.reader != nil && c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}

// shouldClose mirrors net/http.shouldClose: HTTP/1.0 closes by
// default, HTTP/1.1 keeps alive unless `Connection: close` is set.
func shouldClose(req *http.Request) bool {
	if req.ProtoMajor == 1 && req.ProtoMinor == 0 {
		return !strings.EqualFold(req.Header.Get("Connection"), "keep-alive")
	}
	return strings.EqualFold(req.Header.Get("Connection"), "close")
}

// pipeBytes copies bytes between two connections until either side
// closes. It returns when both directions have terminated.
func pipeBytes(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
}

// defaultDialUpstream is the production TCP dialer used by passthrough.
func defaultDialUpstream(host, port string) (net.Conn, error) {
	return net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
}

// splitHostPort splits "host:port" with a sensible error message.
func splitHostPort(hostport string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(hostport)
	if err != nil {
		return "", "", fmt.Errorf("connect: bad host:port %q: %w", hostport, err)
	}
	return host, port, nil
}
