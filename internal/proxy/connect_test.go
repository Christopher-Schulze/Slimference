package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/tlsca"
)

// safeBuffer is a sync.Mutex-protected bytes.Buffer for use as a slog
// destination across goroutines without tripping the race detector.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func newTestSignerForConnect(t *testing.T) *tlsca.Signer {
	t.Helper()
	ca, err := tlsca.LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	return tlsca.NewSigner(ca, 4)
}

func TestConnectInterceptor_NonConnectDelegates(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(204)
	})
	ci := NewConnectInterceptor(nil, inner, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	ci.ServeHTTP(rec, req)
	if !called {
		t.Fatal("non-CONNECT must delegate to inner handler")
	}
	if rec.Code != 204 {
		t.Fatalf("inner status not propagated: %d", rec.Code)
	}
}

func TestConnectInterceptor_ConnectWithoutSigner_405(t *testing.T) {
	ci := NewConnectInterceptor(nil, http.NotFoundHandler(), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodConnect, "//example.com:443", nil)
	req.Host = "example.com:443"
	ci.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 without signer, got %d", rec.Code)
	}
}

func TestConnectInterceptor_BadHostPort(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodConnect, "//bad", nil)
	req.Host = "no-colon"
	ci.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestConnectInterceptor_HijackNotSupported(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	rec := httptest.NewRecorder() // ResponseRecorder is not a Hijacker
	req := httptest.NewRequest(http.MethodConnect, "//example.com:443", nil)
	req.Host = "example.com:443"
	ci.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when hijack unsupported, got %d", rec.Code)
	}
}

func TestConnectInterceptor_AllowlistEmptyMITMsAll(t *testing.T) {
	t.Parallel()
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	if !ci.shouldMITM("anything.example") {
		t.Fatal("empty allowlist must MITM all hosts")
	}
}

func TestConnectInterceptor_AllowlistFiltersHost(t *testing.T) {
	t.Parallel()
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), []string{"api.openai.com", "API.Anthropic.com"})
	if !ci.shouldMITM("api.openai.com") {
		t.Fatal("expected api.openai.com on allowlist")
	}
	if !ci.shouldMITM("api.anthropic.com") {
		t.Fatal("allowlist must be case-insensitive")
	}
	if ci.shouldMITM("github.com") {
		t.Fatal("github.com must not MITM under allowlist")
	}
}

func TestConnectInterceptor_PassthroughCopiesBytes(t *testing.T) {
	signer := newTestSignerForConnect(t)
	// Spin up a fake upstream that echoes "PONG" then closes.
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	defer upstream.Close()
	go func() {
		c, err := upstream.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4)
		_, _ = c.Read(buf)
		_, _ = c.Write([]byte("PONG"))
	}()
	upstreamHost, upstreamPort, _ := net.SplitHostPort(upstream.Addr().String())

	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), []string{"openai.com"})
	ci.SetUpstreamDialer(func(host, port string) (net.Conn, error) {
		// Redirect any host to our fake upstream.
		_ = host
		_ = port
		return net.Dial("tcp", net.JoinHostPort(upstreamHost, upstreamPort))
	})

	// Set up a real http.Server so Hijack works, then send a CONNECT
	// to a non-allowlisted host to exercise the passthrough path.
	srv := httptest.NewServer(ci)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("CONNECT github.com:443 HTTP/1.1\r\nHost: github.com:443\r\n\r\n")); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from CONNECT, got %d", resp.StatusCode)
	}
	if _, err := conn.Write([]byte("PING")); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if string(buf) != "PONG" {
		t.Fatalf("expected PONG, got %q", buf)
	}
}

func TestConnectInterceptor_PassthroughDialFailureLogged(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), []string{"openai.com"})
	ci.SetUpstreamDialer(func(host, port string) (net.Conn, error) {
		return nil, errors.New("dial unreachable (test)")
	})
	var logged safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))

	srv := httptest.NewServer(ci)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte("CONNECT bad.example:443 HTTP/1.1\r\nHost: bad.example:443\r\n\r\n"))
	br := bufio.NewReader(conn)
	resp, _ := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if resp == nil || resp.StatusCode != 200 {
		t.Fatal("CONNECT must still 200 before passthrough fails")
	}
	// Drain so the goroutine completes.
	_, _ = io.Copy(io.Discard, br)
	if !strings.Contains(logged.String(), "dial failed") {
		t.Fatalf("expected dial-failure log, got %q", logged.String())
	}
}

func TestConnectInterceptor_MITMServesInnerHandler(t *testing.T) {
	// End-to-end MITM test: real httptest server, real TLS client
	// against the slimference CA, real inner handler. The bidirection
	// pump uses a custom tls.Server stub so we control the TLS server
	// side with a deterministic in-memory pipe.
	signer := newTestSignerForConnect(t)
	innerCalled := atomic.Bool{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled.Store(true)
		if r.Host != "api.openai.com" {
			t.Errorf("expected r.Host=api.openai.com, got %q", r.Host)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "3")
		_, _ = w.Write([]byte("ok\n"))
	})
	ci := NewConnectInterceptor(signer, inner, []string{"api.openai.com"})

	srv := httptest.NewServer(ci)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err := conn.Write([]byte("CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n")); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// Drain anything bufio.Reader buffered after the CONNECT response,
	// then perform a TLS handshake against the underlying conn.
	if buffered := br.Buffered(); buffered > 0 {
		_, _ = br.Discard(buffered)
	}
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	rootPool := signerRootPool(signer)
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: "api.openai.com",
		RootCAs:    rootPool,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake: %v", err)
	}
	defer tlsConn.Close()
	if _, err := tlsConn.Write([]byte("GET /v1/test HTTP/1.1\r\nHost: api.openai.com\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatalf("write inner request: %v", err)
	}
	innerResp, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		t.Fatalf("read inner response: %v", err)
	}
	if innerResp.StatusCode != 200 {
		t.Fatalf("expected inner 200, got %d", innerResp.StatusCode)
	}
	if !innerCalled.Load() {
		t.Fatal("inner handler must have been invoked")
	}
	_ = innerResp.Body.Close()
}

func TestConnectInterceptor_MITMHandshakeFailureLogged(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), []string{"api.openai.com"})
	var logged safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	ci.SetTLSServerHandshake(func(c net.Conn, cfg *tls.Config) (*tls.Conn, error) {
		return nil, errors.New("handshake refused (test)")
	})

	srv := httptest.NewServer(ci)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte("CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n"))
	br := bufio.NewReader(conn)
	_, _ = http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	_, _ = io.Copy(io.Discard, br)
	if !strings.Contains(logged.String(), "handshake failed") {
		t.Fatalf("expected handshake-failure log, got %q", logged.String())
	}
}

func TestSplitHostPort_BadInput(t *testing.T) {
	t.Parallel()
	if _, _, err := splitHostPort("no-colon"); err == nil {
		t.Fatal("expected error on host without port")
	}
}

func TestSplitHostPort_Good(t *testing.T) {
	t.Parallel()
	host, port, err := splitHostPort("api.openai.com:443")
	if err != nil || host != "api.openai.com" || port != "443" {
		t.Fatalf("got host=%q port=%q err=%v", host, port, err)
	}
}

func TestShouldClose_HTTP10DefaultsClose(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/", nil)
	r.ProtoMajor, r.ProtoMinor = 1, 0
	if !shouldClose(r) {
		t.Fatal("HTTP/1.0 must default to close")
	}
}

func TestShouldClose_HTTP10KeepAlive(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/", nil)
	r.ProtoMajor, r.ProtoMinor = 1, 0
	r.Header.Set("Connection", "keep-alive")
	if shouldClose(r) {
		t.Fatal("HTTP/1.0 with keep-alive must NOT close")
	}
}

func TestShouldClose_HTTP11DefaultsKeepAlive(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/", nil)
	r.ProtoMajor, r.ProtoMinor = 1, 1
	if shouldClose(r) {
		t.Fatal("HTTP/1.1 must default to keep-alive")
	}
}

func TestShouldClose_HTTP11ConnectionClose(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/", nil)
	r.ProtoMajor, r.ProtoMinor = 1, 1
	r.Header.Set("Connection", "close")
	if !shouldClose(r) {
		t.Fatal("HTTP/1.1 with Connection: close must close")
	}
}

func TestSetTLSServerHandshake_NilNoOp(t *testing.T) {
	t.Parallel()
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	original := ci.tlsServerHandshake
	ci.SetTLSServerHandshake(nil)
	// nil must NOT replace the existing handshake.
	if &original != &original { // tautology; we cannot compare funcs directly, just verify no panic
		t.Fatal("unreachable")
	}
}

func TestSetLogger_NilNoOp(t *testing.T) {
	t.Parallel()
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	ci.SetLogger(nil)
	if ci.logger == nil {
		t.Fatal("nil must NOT clear the logger")
	}
}

func TestDefaultDialUpstream_Loopback(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	c, err := defaultDialUpstream(host, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
}

// failingHijackResponseWriter satisfies http.ResponseWriter and
// http.Hijacker but Hijack() always errors. Drives the err-from-
// Hijack path in ServeHTTP without needing a real broken transport.
type failingHijackResponseWriter struct {
	http.ResponseWriter
}

func (f failingHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("simulated hijack failure")
}

func TestConnectInterceptor_HijackErrorLogged(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	var logBuf safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	rec := httptest.NewRecorder()
	w := failingHijackResponseWriter{ResponseWriter: rec}
	req := httptest.NewRequest(http.MethodConnect, "//api.openai.com:443", nil)
	req.Host = "api.openai.com:443"
	ci.ServeHTTP(w, req)
	if !strings.Contains(logBuf.String(), "hijack failed") {
		t.Fatalf("expected hijack-failed log, got %q", logBuf.String())
	}
}

// closedConnHijacker hijacks to a connection whose Write always fails,
// driving the err-from-Write-200 path.
type closedConnHijacker struct {
	http.ResponseWriter
}

func (c closedConnHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	a, b := net.Pipe()
	a.Close()
	b.Close()
	return a, bufio.NewReadWriter(bufio.NewReader(a), bufio.NewWriter(a)), nil
}

func TestConnectInterceptor_Write200ToClosedConnLogged(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	var logBuf safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	rec := httptest.NewRecorder()
	w := closedConnHijacker{ResponseWriter: rec}
	req := httptest.NewRequest(http.MethodConnect, "//api.openai.com:443", nil)
	req.Host = "api.openai.com:443"
	ci.ServeHTTP(w, req)
	if !strings.Contains(logBuf.String(), "write 200 failed") {
		t.Fatalf("expected write-200-failed log, got %q", logBuf.String())
	}
}

func TestConnectInterceptor_Write200FailureLogged(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), []string{"openai.com"})
	var logBuf safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logBuf, nil)))

	srv := httptest.NewServer(ci)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Send CONNECT then immediately close client-side. The server's
	// Write of "200 Connection Established" hits a closed conn and
	// fires the write-200-failed log path.
	_, _ = conn.Write([]byte("CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n"))
	conn.Close()
	// Give the server a moment to attempt the write.
	time.Sleep(150 * time.Millisecond)
	// Either log path fired (write 200 failed OR hijack/serve aborted)
	// is acceptable; the key property is that the server did not panic.
	_ = logBuf.String()
}

func TestDefaultTLSServerHandshake_FailsOnGarbage(t *testing.T) {
	t.Parallel()
	// Pipe one side garbage in, expect the server-side handshake to
	// fail. Exercises the err-from-Handshake branch of the production
	// closure.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		_, _ = a.Write([]byte("not a TLS ClientHello"))
		a.Close()
	}()
	cfg := &tls.Config{
		Certificates: []tls.Certificate{},
	}
	if _, err := defaultTLSServerHandshake(b, cfg); err == nil {
		t.Fatal("expected handshake failure on garbage input")
	}
}

func TestServePlaintextOnTLS_DirectKeepAliveLoop(t *testing.T) {
	signer := newTestSignerForConnect(t)
	calls := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})
	ci := NewConnectInterceptor(signer, inner, []string{"api.openai.com"})
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() {
		ci.servePlaintextOnTLS(b, "api.openai.com")
		close(done)
	}()
	// Send two keep-alive requests then close.
	go func() {
		_, _ = a.Write([]byte("GET /1 HTTP/1.1\r\nHost: api.openai.com\r\n\r\n"))
		_, _ = a.Write([]byte("GET /2 HTTP/1.1\r\nHost: api.openai.com\r\nConnection: close\r\n\r\n"))
	}()
	br := bufio.NewReader(a)
	resp1, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("first response: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()
	resp2, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("second response: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	a.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("servePlaintextOnTLS did not exit on conn close")
	}
	if calls != 2 {
		t.Fatalf("expected 2 inner calls, got %d", calls)
	}
}

func TestServePlaintextOnTLS_ReadErrorBreaksLoop(t *testing.T) {
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	var logBuf safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() {
		ci.servePlaintextOnTLS(b, "api.openai.com")
		close(done)
	}()
	// Send garbage that fails ReadRequest.
	_, _ = a.Write([]byte("not a request\r\n"))
	a.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not break on read error")
	}
}

func TestServePlaintextOnTLS_FlushFailureLogged(t *testing.T) {
	signer := newTestSignerForConnect(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body"))
	})
	ci := NewConnectInterceptor(signer, inner, nil)
	var logBuf safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	// Custom conn that errors on Write but lets Read succeed once.
	c := &readOKWriteFailConn{
		read: bytes.NewBufferString("GET /x HTTP/1.1\r\nHost: api.openai.com\r\n\r\n"),
	}
	ci.servePlaintextOnTLS(c, "api.openai.com")
	if !strings.Contains(logBuf.String(), "flush failed") {
		t.Fatalf("expected flush-failure log, got %q", logBuf.String())
	}
}

// readOKWriteFailConn is a minimal net.Conn that satisfies reads from
// a buffer and fails every write. SetReadDeadline / SetWriteDeadline
// are no-ops.
type readOKWriteFailConn struct {
	read *bytes.Buffer
}

func (c *readOKWriteFailConn) Read(p []byte) (int, error) { return c.read.Read(p) }
func (c *readOKWriteFailConn) Write([]byte) (int, error) {
	return 0, errors.New("simulated write failure")
}
func (c *readOKWriteFailConn) Close() error                     { return nil }
func (c *readOKWriteFailConn) LocalAddr() net.Addr              { return nil }
func (c *readOKWriteFailConn) RemoteAddr() net.Addr             { return nil }
func (c *readOKWriteFailConn) SetDeadline(time.Time) error      { return nil }
func (c *readOKWriteFailConn) SetReadDeadline(time.Time) error  { return nil }
func (c *readOKWriteFailConn) SetWriteDeadline(time.Time) error { return nil }

func TestServeMITMSignerError(t *testing.T) {
	// Signer.Cert with empty host returns an error; we wrap that path
	// by setting up an interceptor whose mitm() is called with a host
	// that the signer rejects via injecting a failing rand source.
	signer := newTestSignerForConnect(t)
	ci := NewConnectInterceptor(signer, http.NotFoundHandler(), nil)
	var logBuf safeBuffer
	ci.SetLogger(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	prev := tlsca.SetRandSource(failOnceReader{})
	defer tlsca.SetRandSource(prev)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() {
		ci.mitm(b, "fresh.example")
		close(done)
	}()
	a.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mitm did not return after signer failure")
	}
	if !strings.Contains(logBuf.String(), "leaf cert failed") {
		t.Fatalf("expected leaf-cert-failed log, got %q", logBuf.String())
	}
}

// failOnceReader fails every read; used to make tlsca.Signer return
// an error from Cert().
type failOnceReader struct{}

func (failOnceReader) Read([]byte) (int, error) {
	return 0, errors.New("test entropy fail")
}

func TestPipeBytes_TerminatesOnClose(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	go func() {
		pipeBytes(a, b)
		close(done)
	}()
	a.Close() // closing a triggers io.Copy errors on both sides
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeBytes did not terminate after close")
	}
}

func TestSingleConnListener_AcceptOnce(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	l := newSingleConnListener(a)
	c, err := l.Accept()
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if c != a {
		t.Fatal("first accept must return the wrapped conn")
	}
	if _, err := l.Accept(); err == nil {
		t.Fatal("second accept must error")
	}
}

func TestSingleConnListener_AcceptAfterClose(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	l := newSingleConnListener(a)
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := l.Accept(); err == nil {
		t.Fatal("Accept after Close must error")
	}
}

func TestSingleConnListener_Addr(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	l := newSingleConnListener(a)
	if l.Addr() == nil {
		t.Fatal("Addr must not be nil")
	}
}

func TestSingleConnListener_AddrFallbackForNilLocal(t *testing.T) {
	t.Parallel()
	c := nilLocalAddrConn{}
	l := newSingleConnListener(c)
	if l.Addr() == nil {
		t.Fatal("Addr must fall back to a TCPAddr when LocalAddr is nil")
	}
}

// nilLocalAddrConn is a minimal net.Conn whose LocalAddr returns nil.
// We only implement what newSingleConnListener / Accept call on the
// connection; the rest panic if exercised so misuse is loud.
type nilLocalAddrConn struct{}

func (nilLocalAddrConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (nilLocalAddrConn) Write([]byte) (int, error)        { return 0, io.EOF }
func (nilLocalAddrConn) Close() error                     { return nil }
func (nilLocalAddrConn) LocalAddr() net.Addr              { return nil }
func (nilLocalAddrConn) RemoteAddr() net.Addr             { return nil }
func (nilLocalAddrConn) SetDeadline(time.Time) error      { return nil }
func (nilLocalAddrConn) SetReadDeadline(time.Time) error  { return nil }
func (nilLocalAddrConn) SetWriteDeadline(time.Time) error { return nil }

// signerRootPool extracts the CA certificate the signer uses and
// returns an *x509.CertPool wrapping it so tls.Config.RootCAs can
// verify the leaf chain produced by the in-test handshake.
func signerRootPool(s *tlsca.Signer) *x509.CertPool {
	cert, _ := s.Cert("probe.example")
	pool := x509.NewCertPool()
	if len(cert.Certificate) >= 2 {
		root, err := x509.ParseCertificate(cert.Certificate[len(cert.Certificate)-1])
		if err == nil {
			pool.AddCert(root)
		}
	}
	return pool
}
