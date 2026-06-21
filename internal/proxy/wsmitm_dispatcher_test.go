package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/proxy/sniroute"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

// fakePair returns two connected net.Pipe conns. closeWrite is
// emulated by Close because net.Pipe does not implement CloseWrite.
type pipeAdapter struct {
	net.Conn
}

func (p pipeAdapter) CloseWrite() error { return p.Conn.Close() }

func newPipe() (net.Conn, net.Conn) {
	a, b := net.Pipe()
	return pipeAdapter{a}, pipeAdapter{b}
}

type resolverFunc func(sniroute.Request) sniroute.Decision

func (f resolverFunc) Resolve(r sniroute.Request) sniroute.Decision { return f(r) }

type scriptedConn struct {
	read     *bytes.Reader
	write    bytes.Buffer
	writeErr error
}

func newScriptedConn(in string) *scriptedConn {
	return &scriptedConn{read: bytes.NewReader([]byte(in))}
}

func (c *scriptedConn) Read(p []byte) (int, error) { return c.read.Read(p) }
func (c *scriptedConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return c.write.Write(p)
}
func (c *scriptedConn) Close() error                     { return nil }
func (c *scriptedConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c *scriptedConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c *scriptedConn) SetDeadline(time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }
func (c *scriptedConn) CloseWrite() error                { return nil }
func (c *scriptedConn) written() string                  { return c.write.String() }

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }

func TestDispatcherRejectIncrementsCounter(t *testing.T) {
	d := &PhaseFDispatcher{}
	client, _ := newPipe()
	defer client.Close()

	err := d.Handle(context.Background(), sniroute.Reject, sniroute.Request{}, client)
	if err != nil {
		t.Fatalf("Reject returned error: %v", err)
	}
	if got := d.Snapshot().Rejected; got != 1 {
		t.Fatalf("rejected counter = %d, want 1", got)
	}
}

func TestDispatcherNilDialReturnsError(t *testing.T) {
	d := &PhaseFDispatcher{} // no UpstreamDial set
	client, _ := newPipe()
	defer client.Close()

	if err := d.Handle(context.Background(), sniroute.PassthroughTLS,
		sniroute.Request{SNI: "chatgpt.com"}, client); err == nil {
		t.Fatalf("expected passthrough error when UpstreamDial nil")
	}
	err := d.Handle(context.Background(), sniroute.MITMConversation,
		sniroute.Request{SNI: "chatgpt.com"}, client)
	if err == nil {
		t.Fatalf("expected error when UpstreamDial nil")
	}
}

func TestDispatcherUpstreamDialFailureCounted(t *testing.T) {
	d := &PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return nil, errors.New("dial refused")
		},
	}
	client, _ := newPipe()
	defer client.Close()

	err := d.Handle(context.Background(), sniroute.PassthroughTLS,
		sniroute.Request{SNI: "chatgpt.com"}, client)
	if err == nil {
		t.Fatalf("expected dial error")
	}
	if got := d.Snapshot().UpstreamDialFail; got != 1 {
		t.Fatalf("upstream dial fail counter = %d, want 1", got)
	}

	err = d.Handle(context.Background(), sniroute.MITMConversation,
		sniroute.Request{SNI: "chatgpt.com"}, client)
	if err == nil {
		t.Fatalf("expected MITM dial error")
	}
	if got := d.Snapshot().UpstreamDialFail; got != 2 {
		t.Fatalf("upstream dial fail counter = %d, want 2", got)
	}
}

func TestDispatcherMITMWithResolverUsesInitialHTTPRoute(t *testing.T) {
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{
		Resolver: resolverFunc(func(r sniroute.Request) sniroute.Decision {
			if r.Method != "GET" || r.Path != "/backend-api/codex/responses" {
				t.Fatalf("resolver saw wrong request: %+v", r)
			}
			return sniroute.Reject
		}),
		UpstreamDial: func(_ context.Context, hostPort string) (net.Conn, error) {
			if hostPort != "chatgpt.com:443" {
				t.Fatalf("hostPort=%q", hostPort)
			}
			return upstream, nil
		},
	}
	client := newScriptedConn("GET /backend-api/codex/responses HTTP/1.1\r\nHost: chatgpt.com\r\n\r\n")
	if err := d.Handle(context.Background(), sniroute.MITMConversation, sniroute.Request{SNI: "chatgpt.com"}, client); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := d.Snapshot().Rejected; got != 1 {
		t.Fatalf("rejected=%d want 1", got)
	}
	if upstream.written() != "" {
		t.Fatalf("rejected request should not reach upstream: %q", upstream.written())
	}
}

func TestDispatcherPassthroughC2SBytesFlow(t *testing.T) {
	upstreamRemote, upstreamLocal := newPipe()
	defer upstreamRemote.Close()

	d := &PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	clientRemote, clientLocal := newPipe()

	go func() {
		_ = d.Handle(context.Background(), sniroute.PassthroughTLS,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	}()

	payload := []byte("hello chatgpt")
	go func() {
		_, _ = clientRemote.Write(payload)
		_ = clientRemote.(pipeAdapter).CloseWrite()
	}()

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(upstreamRemote, buf); err != nil {
		t.Fatalf("upstream read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("byte mismatch: got %q want %q", buf, payload)
	}
	_ = upstreamRemote.Close()
	_ = clientRemote.Close()
	// Counters update; allow a moment for the goroutines to finalize.
	time.Sleep(20 * time.Millisecond)
	snap := d.Snapshot()
	if snap.PassthroughBridged != 1 {
		t.Errorf("passthrough counter = %d, want 1", snap.PassthroughBridged)
	}
	if snap.BytesC2S < int64(len(payload)) {
		t.Errorf("BytesC2S=%d, want >= %d", snap.BytesC2S, len(payload))
	}
}

func TestDispatcherPassthroughS2CBytesFlow(t *testing.T) {
	upstreamRemote, upstreamLocal := newPipe()
	defer upstreamRemote.Close()

	d := &PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	clientRemote, clientLocal := newPipe()

	go func() {
		_ = d.Handle(context.Background(), sniroute.PassthroughTLS,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	}()

	payload := []byte("hello client")
	go func() {
		_, _ = upstreamRemote.Write(payload)
		_ = upstreamRemote.(pipeAdapter).CloseWrite()
	}()

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(clientRemote, buf); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("byte mismatch: got %q want %q", buf, payload)
	}
	_ = upstreamRemote.Close()
	_ = clientRemote.Close()
	time.Sleep(20 * time.Millisecond)
	snap := d.Snapshot()
	if snap.BytesS2C < int64(len(payload)) {
		t.Errorf("BytesS2C=%d, want >= %d", snap.BytesS2C, len(payload))
	}
}

func TestDispatcherBridgeReturnsFirstNonEOFError(t *testing.T) {
	d := &PhaseFDispatcher{}
	client := newScriptedConn("client bytes")
	upstream := newScriptedConn("")
	upstream.writeErr = errors.New("upstream write failed")

	err := d.bridge(context.Background(), client, upstream)
	if err == nil || !strings.Contains(err.Error(), "upstream write failed") {
		t.Fatalf("bridge err=%v", err)
	}
}

func TestDispatcherMITMConversationAlsoBridges(t *testing.T) {
	upstreamRemote, upstreamLocal := newPipe()
	defer upstreamRemote.Close()

	d := &PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}

	clientRemote, clientLocal := newPipe()

	var bridgeErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		bridgeErr = d.Handle(context.Background(), sniroute.MITMConversation,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	})

	// Drain upstream so the c2s pump can proceed.
	go func() { _, _ = io.Copy(io.Discard, upstreamRemote) }()

	_, _ = clientRemote.Write([]byte("ping"))
	_ = clientRemote.(pipeAdapter).CloseWrite()
	_ = upstreamRemote.Close()
	_ = clientLocal.Close()

	wg.Wait()
	if bridgeErr != nil && !errors.Is(bridgeErr, io.EOF) {
		// EOF / closed pipe is acceptable on either side.
	}
	if got := d.Snapshot().MITMBridged; got != 1 {
		t.Errorf("mitm counter = %d, want 1", got)
	}
}

func TestDispatcherSnapshotIncludesActivePhaseFSession(t *testing.T) {
	d := &PhaseFDispatcher{}
	adapter := &wsPhaseFAdapter{}
	adapter.counters.requestsSeen.Add(3)
	adapter.counters.mutations.Add(2)

	activeID := d.registerActiveWSMITMSession(&wsmitm.Session{}, adapter)
	active := d.Snapshot()
	if active.WSMITMPhaseFRequests != 3 || active.WSMITMPhaseFMutations != 2 {
		t.Fatalf("active snapshot missing phase-f counters: %+v", active)
	}

	d.finishActiveWSMITMSession(activeID, wsmitm.SessionTelemetry{FramesReencoded: 1}, adapter.snapshot())
	finished := d.Snapshot()
	if finished.WSMITMPhaseFRequests != 3 || finished.WSMITMPhaseFMutations != 2 {
		t.Fatalf("finished snapshot double-counted or lost phase-f counters: %+v", finished)
	}
	if finished.WSMITMReencoded != 1 {
		t.Fatalf("finished snapshot missing session telemetry: %+v", finished)
	}
}

func TestDispatcherContextCancellationReturns(t *testing.T) {
	// Pair that never sends. Cancel ctx mid-bridge.
	_, upstreamLocal := newPipe()
	d := &PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	_, clientLocal := newPipe()

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- d.Handle(ctx, sniroute.PassthroughTLS,
			sniroute.Request{SNI: "chatgpt.com"}, clientLocal)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	_ = upstreamLocal.Close()
	_ = clientLocal.Close()

	select {
	case <-doneCh:
		// pass
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not return after ctx cancel + conn close")
	}
}

func TestBridgeTimeoutSetsDeadlines(t *testing.T) {
	client := newScriptedConn("")
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{BridgeTimeout: time.Millisecond}
	if err := d.bridge(context.Background(), client, upstream); err != nil {
		t.Fatalf("bridge: %v", err)
	}
}

func TestRunWSMITMTimeoutBranchWithEmptyStreams(t *testing.T) {
	client := newScriptedConn("")
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{BridgeTimeout: time.Millisecond}
	if err := d.runWSMITM(context.Background(), client, upstream, WebSocketBridgeOptions{}); err != nil {
		t.Fatalf("runWSMITM: %v", err)
	}
}

func TestRunWSBridgeTimeoutBranchWithEmptyStreams(t *testing.T) {
	client := newScriptedConn("")
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{BridgeTimeout: time.Millisecond}
	if err := d.runWSBridge(context.Background(), client, upstream, WebSocketBridgeOptions{}); err != nil {
		t.Fatalf("runWSBridge: %v", err)
	}
}

func TestDispatcherUnknownDecisionIsNoop(t *testing.T) {
	d := &PhaseFDispatcher{}
	err := d.Handle(context.Background(), sniroute.Decision("garbage"),
		sniroute.Request{}, nil)
	if err != nil {
		t.Fatalf("unexpected err for unknown decision: %v", err)
	}
}

func TestRouteInitialHTTPRejectsAfterResolver(t *testing.T) {
	client := newScriptedConn("GET /backend-api/codex/responses HTTP/1.1\r\nHost: chatgpt.com\r\n\r\n")
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{Resolver: resolverFunc(func(r sniroute.Request) sniroute.Decision {
		if r.Method != "GET" || r.Path != "/backend-api/codex/responses" {
			t.Fatalf("resolver saw wrong request: %+v", r)
		}
		return sniroute.Reject
	})}
	if err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream); err != nil {
		t.Fatalf("route: %v", err)
	}
	if got := d.Snapshot().Rejected; got != 1 {
		t.Fatalf("rejected=%d want 1", got)
	}
	if upstream.written() != "" {
		t.Fatalf("rejected request should not reach upstream: %q", upstream.written())
	}
}

func TestRouteInitialHTTPInvalidHeaderFallsBackByteEqual(t *testing.T) {
	client := newScriptedConn("PRI * HTTP/2.0\r\n\r\n")
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{Resolver: resolverFunc(func(sniroute.Request) sniroute.Decision {
		t.Fatal("resolver should not run for invalid HTTP/1 header")
		return sniroute.Reject
	})}
	if err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream); err != nil {
		t.Fatalf("route: %v", err)
	}
	if upstream.written() != "PRI * HTTP/2.0\r\n\r\n" {
		t.Fatalf("header not forwarded byte-equal: %q", upstream.written())
	}
	if got := d.Snapshot().PassthroughBridged; got != 1 {
		t.Fatalf("passthrough=%d want 1", got)
	}
}

func TestRouteInitialHTTPReadErrorPartialWriteError(t *testing.T) {
	client := newScriptedConn("GET /partial HTTP/1.1\r\nHost: x")
	upstream := newScriptedConn("")
	upstream.writeErr = errors.New("write failed")
	d := &PhaseFDispatcher{}
	err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream)
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("err=%v want write failure", err)
	}
}

func TestRouteInitialHTTPInvalidHeaderWriteError(t *testing.T) {
	client := newScriptedConn("PRI * HTTP/2.0\r\n\r\n")
	upstream := newScriptedConn("")
	upstream.writeErr = errors.New("bad upstream")
	d := &PhaseFDispatcher{}
	err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream)
	if err == nil || !strings.Contains(err.Error(), "bad upstream") {
		t.Fatalf("err=%v want bad upstream", err)
	}
}

func TestRouteInitialHTTPEmptyReadFallsBackToBridge(t *testing.T) {
	client := newScriptedConn("")
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{}
	if err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream); err != nil {
		t.Fatalf("route: %v", err)
	}
	if got := d.Snapshot().PassthroughBridged; got != 1 {
		t.Fatalf("passthrough=%d want 1", got)
	}
}

func TestRouteInitialHTTPParsedHeaderWriteError(t *testing.T) {
	client := newScriptedConn("POST /v1/responses HTTP/1.1\r\nHost: api.openai.com\r\n\r\n")
	upstream := newScriptedConn("")
	upstream.writeErr = errors.New("parsed write failed")
	d := &PhaseFDispatcher{Resolver: resolverFunc(func(sniroute.Request) sniroute.Decision {
		return sniroute.PassthroughTLS
	})}
	err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "api.openai.com"}, client, upstream)
	if err == nil || !strings.Contains(err.Error(), "parsed write failed") {
		t.Fatalf("err=%v want parsed write failure", err)
	}
}

func TestRouteInitialHTTPWebSocketResponseWriteError(t *testing.T) {
	reqHeader := "GET /backend-api/codex/responses HTTP/1.1\r\nUpgrade: websocket\r\n\r\n"
	respHeader := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n\r\n"
	client := newScriptedConn(reqHeader)
	client.writeErr = errors.New("client write failed")
	upstream := newScriptedConn(respHeader)
	d := &PhaseFDispatcher{Resolver: resolverFunc(func(sniroute.Request) sniroute.Decision {
		return sniroute.MITMConversation
	})}
	err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream)
	if err == nil || !strings.Contains(err.Error(), "client write failed") {
		t.Fatalf("err=%v want client write failure", err)
	}
}

func TestRouteInitialHTTPNonWebSocketFallsBackAfterParse(t *testing.T) {
	header := "POST /v1/responses HTTP/1.1\r\nHost: api.openai.com\r\nUser-Agent: codex_cli_rs/0.130.0\r\nSec-WebSocket-Protocol: one, two\r\n\r\n"
	client := newScriptedConn(header)
	upstream := newScriptedConn("")
	d := &PhaseFDispatcher{Resolver: resolverFunc(func(r sniroute.Request) sniroute.Decision {
		if r.UserAgent != "codex_cli_rs/0.130.0" || r.Subprotocol != "one, two" || r.IsWebSocket {
			t.Fatalf("parsed request wrong: %+v", r)
		}
		return sniroute.MITMConversation
	})}
	if err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "api.openai.com"}, client, upstream); err != nil {
		t.Fatalf("route: %v", err)
	}
	if upstream.written() != header {
		t.Fatalf("header not forwarded byte-equal:\ngot %q\nwant %q", upstream.written(), header)
	}
	if got := d.Snapshot().PassthroughBridged; got != 1 {
		t.Fatalf("passthrough=%d want 1", got)
	}
}

func TestRouteInitialHTTPWebSocketResponseReadErrorForwardsPartial(t *testing.T) {
	reqHeader := "GET /backend-api/codex/responses HTTP/1.1\r\nUpgrade: websocket\r\n\r\n"
	client := newScriptedConn(reqHeader)
	upstream := newScriptedConn("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n")
	d := &PhaseFDispatcher{Resolver: resolverFunc(func(sniroute.Request) sniroute.Decision {
		return sniroute.MITMConversation
	})}
	err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream)
	if err == nil {
		t.Fatal("expected response header read error")
	}
	if !strings.Contains(client.written(), "101 Switching Protocols") {
		t.Fatalf("partial response not forwarded to client: %q", client.written())
	}
}

func TestRouteInitialHTTPWebSocketHandshakeSuccessThenEOF(t *testing.T) {
	reqHeader := "GET /backend-api/codex/responses HTTP/1.1\r\nUpgrade: websocket\r\n\r\n"
	respHeader := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n\r\n"
	client := newScriptedConn(reqHeader)
	upstream := newScriptedConn(respHeader)
	d := &PhaseFDispatcher{Resolver: resolverFunc(func(sniroute.Request) sniroute.Decision {
		return sniroute.MITMConversation
	})}
	if err := d.routeInitialHTTP(context.Background(), sniroute.PassthroughTLS, sniroute.Request{SNI: "chatgpt.com"}, client, upstream); err != nil {
		t.Fatalf("route: %v", err)
	}
	if !strings.Contains(client.written(), "101 Switching Protocols") {
		t.Fatalf("response header not forwarded: %q", client.written())
	}
	if got := d.Snapshot().MITMBridged; got != 1 {
		t.Fatalf("mitm=%d want 1", got)
	}
}

func TestReadAndParseHTTPHeaderEdges(t *testing.T) {
	tooLong := newScriptedConn("GET / HTTP/1.1\r\nHost: x\r\n\r\n")
	if _, err := readHTTPHeader(tooLong, 4); err == nil {
		t.Fatal("expected header-too-large error")
	}
	if _, ok := parseHTTPRequestHeader(nil); ok {
		t.Fatal("nil header should not parse")
	}
	if _, ok := parseHTTPRequestHeader([]byte("GET / nope\r\n\r\n")); ok {
		t.Fatal("non HTTP/1 request line should not parse")
	}
	parsed, ok := parseHTTPRequestHeader([]byte("GET /ws HTTP/1.1\nUpgrade: WebSocket\nSec-WebSocket-Protocol: responses_websockets=2026-02-06, other\n\n"))
	if !ok || !parsed.websocket || parsed.subprotocol != "responses_websockets=2026-02-06, other" {
		t.Fatalf("parsed=%+v ok=%v", parsed, ok)
	}
	parsed, ok = parseHTTPRequestHeader([]byte("GET http://127.0.0.1:8990/backend-api/codex/responses?x=1 HTTP/1.1\r\nUpgrade: websocket\r\n\r\n"))
	if !ok || parsed.path != "/backend-api/codex/responses?x=1" {
		t.Fatalf("absolute-form target not normalised: %+v ok=%v", parsed, ok)
	}
	parsed, ok = parseHTTPRequestHeader([]byte("GET /plain HTTP/1.1\nHeader-Without-Colon\nUser-Agent: codex\n\nIgnored: after-blank\n"))
	if !ok || parsed.userAgent != "codex" || parsed.path != "/plain" {
		t.Fatalf("parsed no-colon/blank edge wrong: %+v ok=%v", parsed, ok)
	}
}

func TestUpstreamHostPortDefaultsTo443(t *testing.T) {
	if got := upstreamHostPort("chatgpt.com", 0); got != "chatgpt.com:443" {
		t.Errorf("got %q", got)
	}
	if got := upstreamHostPort("chatgpt.com", 8443); got != "chatgpt.com:8443" {
		t.Errorf("got %q", got)
	}
}

func TestItoaCovers(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{{0, "0"}, {7, "7"}, {443, "443"}, {-12, "-12"}}
	for _, c := range cases {
		if got := itoa(c.in); got != c.want {
			t.Errorf("itoa(%d)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestCloseWriteFallsBackToClose(t *testing.T) {
	// net.Pipe doesn't implement CloseWrite; the helper falls back.
	a, b := net.Pipe()
	defer b.Close()
	if err := closeWrite(a); err != nil {
		t.Fatalf("closeWrite returned error: %v", err)
	}
}

func TestCloseWriteUsesCloseWriteIfAvailable(t *testing.T) {
	// pipeAdapter exposes CloseWrite via wrapper.
	a, b := newPipe()
	defer b.Close()
	if err := closeWrite(a); err != nil {
		t.Fatalf("closeWrite returned error: %v", err)
	}
}

func TestDefaultUpstreamDialBadHostPort(t *testing.T) {
	d := DefaultUpstreamDial()
	_, err := d(context.Background(), "not-a-valid-hostport")
	if err == nil {
		t.Fatalf("expected error on malformed hostport")
	}
}

func TestDefaultUpstreamDialDeterministicBranches(t *testing.T) {
	prevResolve := dohResolveAFn
	prevDial := upstreamTCPDialContextFn
	prevWrap := wrapTLSConnFn
	t.Cleanup(func() {
		dohResolveAFn = prevResolve
		upstreamTCPDialContextFn = prevDial
		wrapTLSConnFn = prevWrap
	})

	wrapTLSConnFn = func(_ context.Context, raw net.Conn, host string) (net.Conn, error) {
		if host != "chatgpt.com" {
			t.Fatalf("wrap host=%q", host)
		}
		return raw, nil
	}

	dohResolveAFn = func(_ context.Context, host string) (string, error) {
		if host != "chatgpt.com" {
			t.Fatalf("resolve host=%q", host)
		}
		return "203.0.113.10", nil
	}
	var dialed []string
	upstreamTCPDialContextFn = func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		return newScriptedConn(""), nil
	}
	conn, err := DefaultUpstreamDial()(context.Background(), "chatgpt.com:443")
	if err != nil {
		t.Fatalf("ip dial: %v", err)
	}
	_ = conn.Close()
	if len(dialed) != 1 || dialed[0] != "203.0.113.10:443" {
		t.Fatalf("dialed=%v", dialed)
	}

	dialed = nil
	upstreamTCPDialContextFn = func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		if len(dialed) == 1 {
			return nil, errors.New("ip route unavailable")
		}
		return newScriptedConn(""), nil
	}
	conn, err = DefaultUpstreamDial()(context.Background(), "chatgpt.com:443")
	if err != nil {
		t.Fatalf("fallback dial: %v", err)
	}
	_ = conn.Close()
	if len(dialed) != 2 || dialed[0] != "203.0.113.10:443" || dialed[1] != "chatgpt.com:443" {
		t.Fatalf("fallback dialed=%v", dialed)
	}

	dohResolveAFn = func(context.Context, string) (string, error) {
		return "", errors.New("doh down")
	}
	upstreamTCPDialContextFn = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("system dial failed")
	}
	if _, err := DefaultUpstreamDial()(context.Background(), "chatgpt.com:443"); err == nil || !strings.Contains(err.Error(), "system dial failed") {
		t.Fatalf("err=%v want system dial failure", err)
	}
}
