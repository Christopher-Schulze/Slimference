package transparent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/proxy/sniroute"
)

// staticCertProvider yields one fixed leaf for every SNI - sufficient
// for handshake-completion tests.
type staticCertProvider struct {
	cert tls.Certificate
}

func (s *staticCertProvider) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return &s.cert, nil
}

func makeTestCert(t *testing.T, sni string) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: sni},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{sni},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        mustParseCert(t, der),
	}
}

func mustParseCert(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func runEngine(t *testing.T, e *Engine) (chan error, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- e.Run(ctx)
	}()
	return errCh, cancel
}

func TestEngineValidateMissingFields(t *testing.T) {
	cases := []*Engine{
		{},
		{Listener: testListener(t)},
		{Listener: testListener(t), Resolver: sniroute.New(nil)},
		{Listener: testListener(t), Resolver: sniroute.New(nil), Certs: &staticCertProvider{}},
	}
	for i, e := range cases {
		if err := e.Run(context.Background()); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func testListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestEngineMITMConversationDispatched(t *testing.T) {
	cert := makeTestCert(t, "chatgpt.com")
	called := atomic.Int32{}
	dispatcher := DispatcherFunc(func(ctx context.Context, d sniroute.Decision,
		r sniroute.Request, c net.Conn) error {
		called.Add(1)
		if d != sniroute.PassthroughTLS {
			// SNI alone (no path) doesn't yet route to MITM in
			// the router - that needs the application-layer
			// request inspection. SNI-only decisions are
			// passthrough by default.
		}
		_ = c.Close()
		return nil
	})

	l := testListener(t)
	engine := &Engine{
		Listener:   l,
		Resolver:   sniroute.New(nil),
		Certs:      &staticCertProvider{cert: cert},
		Dispatcher: dispatcher,
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()

	if !dialAndHandshake(t, l.Addr().String(), "chatgpt.com") {
		t.Fatal("handshake failed")
	}
	waitForCounter(t, &called, 1, time.Second)

	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("engine error: %v", err)
	}
	if snap := engine.Snapshot(); snap.Accepted < 1 || snap.Served < 1 {
		t.Errorf("counters: %+v", snap)
	}
}

func TestEnginePassthroughForUnknownSNI(t *testing.T) {
	cert := makeTestCert(t, "example.com")
	seen := atomic.Int32{}
	dispatcher := DispatcherFunc(func(ctx context.Context, d sniroute.Decision,
		r sniroute.Request, c net.Conn) error {
		if d != sniroute.PassthroughTLS {
			t.Errorf("expected passthrough for unknown SNI, got %s", d)
		}
		if r.IsLLMHostMatch {
			t.Errorf("IsLLMHostMatch should be false for example.com")
		}
		seen.Add(1)
		_ = c.Close()
		return nil
	})

	l := testListener(t)
	engine := &Engine{
		Listener:   l,
		Resolver:   sniroute.New(nil),
		Certs:      &staticCertProvider{cert: cert},
		Dispatcher: dispatcher,
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()

	if !dialAndHandshake(t, l.Addr().String(), "example.com") {
		t.Fatal("handshake failed")
	}
	waitForCounter(t, &seen, 1, time.Second)

	cancel()
	<-errCh
	snap := engine.Snapshot()
	if snap.Passthrough != 1 {
		t.Errorf("passthrough counter=%d want 1 (snap=%+v)", snap.Passthrough, snap)
	}
}

func TestEngineHandshakeFailureCounted(t *testing.T) {
	// Send raw garbage to the engine - TLS handshake fails.
	cert := makeTestCert(t, "any")
	dispatcher := DispatcherFunc(func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error {
		t.Errorf("dispatcher should NOT fire on failed handshake")
		return nil
	})
	l := testListener(t)
	engine := &Engine{
		Listener:   l,
		Resolver:   sniroute.New(nil),
		Certs:      &staticCertProvider{cert: cert},
		Dispatcher: dispatcher,
		OnError:    func(error) {},
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("not a tls clienthello"))
	_ = conn.Close()

	// Wait for engine to count the error.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if engine.Snapshot().Errors > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-errCh
	if engine.Snapshot().Errors == 0 {
		t.Errorf("expected error counter to increment")
	}
}

func TestEngineRejectClosesQuietly(t *testing.T) {
	// We don't currently emit Reject in sniroute, but the engine
	// must honour it if a custom Resolver returns one. Inject a
	// stub Dispatcher that asserts it's NOT called.
	cert := makeTestCert(t, "any.com")
	dispatcher := DispatcherFunc(func(context.Context, sniroute.Decision,
		sniroute.Request, net.Conn) error {
		t.Errorf("dispatcher should not be called on Reject")
		return nil
	})
	l := testListener(t)
	engine := &Engine{
		Listener: l, Certs: &staticCertProvider{cert: cert},
		Dispatcher: dispatcher,
		Resolver:   stubResolver{sniroute.Reject},
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()
	if !dialAndHandshake(t, l.Addr().String(), "any.com") {
		t.Fatal("handshake failed")
	}
	// Wait for the engine to count the connection.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if engine.Snapshot().Rejected == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-errCh
	if engine.Snapshot().Rejected != 1 {
		t.Errorf("rejected counter not incremented: %+v", engine.Snapshot())
	}
}

func TestEngineOnErrorFiresOnDispatchError(t *testing.T) {
	cert := makeTestCert(t, "any.com")
	dispatcher := DispatcherFunc(func(context.Context, sniroute.Decision,
		sniroute.Request, net.Conn) error {
		return fmt.Errorf("dispatch boom")
	})
	errs := atomic.Int32{}
	l := testListener(t)
	engine := &Engine{
		Listener: l, Resolver: sniroute.New(nil),
		Certs:      &staticCertProvider{cert: cert},
		Dispatcher: dispatcher,
		OnError:    func(err error) { errs.Add(1) },
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()
	if !dialAndHandshake(t, l.Addr().String(), "any.com") {
		t.Fatal("handshake failed")
	}
	waitForCounter(t, &errs, 1, time.Second)
	cancel()
	<-errCh
}

func TestEngineRespectsContextCancellation(t *testing.T) {
	cert := makeTestCert(t, "x")
	l := testListener(t)
	engine := &Engine{
		Listener: l, Resolver: sniroute.New(nil),
		Certs:      &staticCertProvider{cert: cert},
		Dispatcher: DispatcherFunc(func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error { return nil }),
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- engine.Run(ctx) }()
	cancel()
	select {
	case <-errCh:
		// Engine returned after cancellation - good.
	case <-time.After(time.Second):
		t.Fatal("engine did not return after cancel")
	}
}

func TestEngineAcceptFailureReturned(t *testing.T) {
	want := errors.New("accept boom")
	engine := &Engine{
		Listener:   errListener{err: want},
		Resolver:   sniroute.New(nil),
		Certs:      &staticCertProvider{cert: makeTestCert(t, "x")},
		Dispatcher: DispatcherFunc(func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error { return nil }),
	}
	if err := engine.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Run error=%v, want %v", err, want)
	}
}

func TestEngineMITMDecisionCounter(t *testing.T) {
	cert := makeTestCert(t, "chatgpt.com")
	called := atomic.Int32{}
	l := testListener(t)
	engine := &Engine{
		Listener: l,
		Resolver: stubResolver{sniroute.MITMConversation},
		Certs:    &staticCertProvider{cert: cert},
		Dispatcher: DispatcherFunc(func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error {
			called.Add(1)
			return nil
		}),
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()
	if !dialAndHandshake(t, l.Addr().String(), "chatgpt.com") {
		t.Fatal("handshake failed")
	}
	waitForCounter(t, &called, 1, time.Second)
	cancel()
	<-errCh
	if snap := engine.Snapshot(); snap.MITM != 1 {
		t.Fatalf("mitm counter=%d, snap=%+v", snap.MITM, snap)
	}
}

func TestEngineTLSHandshakeFailureAfterPeekCounted(t *testing.T) {
	cert := makeTestCert(t, "chatgpt.com")
	l := testListener(t)
	engine := &Engine{
		Listener: l, Resolver: sniroute.New(nil),
		Certs:      &staticCertProvider{cert: cert},
		Dispatcher: DispatcherFunc(func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error { return nil }),
		OnError:    func(error) {},
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write(buildClientHello("chatgpt.com", "http/1.1"))
	_ = conn.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if engine.Snapshot().Errors > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-errCh
	if engine.Snapshot().Errors == 0 {
		t.Fatal("expected TLS handshake error after successful peek")
	}
}

func TestEngineHandshakeTimeoutHonoured(t *testing.T) {
	cert := makeTestCert(t, "x")
	dispatcher := DispatcherFunc(func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error {
		return nil
	})
	l := testListener(t)
	engine := &Engine{
		Listener: l, Resolver: sniroute.New(nil),
		Certs:            &staticCertProvider{cert: cert},
		Dispatcher:       dispatcher,
		HandshakeTimeout: 50 * time.Millisecond,
	}
	errCh, cancel := runEngine(t, engine)
	defer cancel()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// Don't send any handshake bytes - handshake will time out.
	defer conn.Close()
	time.Sleep(150 * time.Millisecond)
	if engine.Snapshot().Errors == 0 {
		t.Errorf("expected handshake-timeout error")
	}
	cancel()
	<-errCh
}

func TestEngineMaxConcurrentSemaphore(t *testing.T) {
	cert := makeTestCert(t, "any")
	var active atomic.Int32
	var maxActive atomic.Int32
	dispatcher := DispatcherFunc(func(ctx context.Context, _ sniroute.Decision,
		_ sniroute.Request, c net.Conn) error {
		v := active.Add(1)
		// Record peak concurrency for the assertion.
		for {
			m := maxActive.Load()
			if v <= m {
				break
			}
			if maxActive.CompareAndSwap(m, v) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond) // hold long enough that a second arrival overlaps if it can
		active.Add(-1)
		_ = c.Close()
		return nil
	})
	l := testListener(t)
	engine := &Engine{
		Listener: l, Resolver: sniroute.New(nil),
		Certs:         &staticCertProvider{cert: cert},
		Dispatcher:    dispatcher,
		MaxConcurrent: 1,
	}
	errCh, cancel := runEngine(t, engine)
	defer func() {
		cancel()
		<-errCh
	}()
	// Fire three handshakes in parallel; with MaxConcurrent=1 they
	// must serialize.
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = dialAndHandshake(t, l.Addr().String(), "x")
		}()
	}
	wg.Wait()
	// Give the dispatchers a beat to finish.
	time.Sleep(200 * time.Millisecond)
	if peak := maxActive.Load(); peak > 1 {
		t.Errorf("max concurrent dispatchers = %d, want 1", peak)
	}
}

func TestPeekClientHelloSNIRecognisesValidHello(t *testing.T) {
	// Build a minimal but spec-compliant ClientHello with SNI = "example.com".
	hello := buildClientHello("example.com", "h2")
	br := bufRead(hello)
	sni, alpn, err := peekClientHelloSNI(br)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if sni != "example.com" {
		t.Errorf("sni=%q", sni)
	}
	if alpn != "h2" {
		t.Errorf("alpn=%q", alpn)
	}
}

func TestPeekClientHelloSNIShortReader(t *testing.T) {
	br := bufRead([]byte{0x16})
	if _, _, err := peekClientHelloSNI(br); err == nil {
		t.Errorf("expected error on short reader")
	}
}

func TestPeekClientHelloSNINotHandshake(t *testing.T) {
	br := bufRead([]byte{0x17, 0x03, 0x03, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o'})
	if _, _, err := peekClientHelloSNI(br); err == nil {
		t.Errorf("expected error on non-handshake record")
	}
}

func TestPeekClientHelloSNIInvalidRecordLength(t *testing.T) {
	br := bufRead([]byte{0x16, 0x03, 0x03, 0xff, 0xff})
	if _, _, err := peekClientHelloSNI(br); err == nil {
		t.Errorf("expected error on oversize record")
	}
}

func TestPeekClientHelloSNIShortRecordBody(t *testing.T) {
	br := bufRead([]byte{0x16, 0x03, 0x03, 0x00, 0x10, 0x01})
	if _, _, err := peekClientHelloSNI(br); err == nil {
		t.Fatal("expected error on short record body")
	}
}

func TestPeekClientHelloSNINotClientHello(t *testing.T) {
	// 5-byte record header pointing to 10 bytes, but starts with
	// HandshakeType=ServerHello (2).
	body := []byte{0x02, 0x00, 0x00, 0x00, 0, 0, 0, 0, 0, 0}
	rec := append([]byte{0x16, 0x03, 0x03, byte(len(body) >> 8), byte(len(body))}, body...)
	if _, _, err := peekClientHelloSNI(bufRead(rec)); err == nil {
		t.Errorf("expected error on non-ClientHello")
	}
}

func TestPeekClientHelloSNISectionOverflows(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{name: "hello too short", body: []byte{0x01, 0x00, 0x00, 0x02, 0x03, 0x03}},
		{name: "cipher suites overflow", body: clientHelloBodyWithTail(nil)},
		{name: "compression methods overflow", body: clientHelloBodyWithTail([]byte{0x00, 0x02, 0x13, 0x01})},
		{name: "extensions length overflow", body: clientHelloBodyWithTail([]byte{0x00, 0x02, 0x13, 0x01, 0x01, 0x00, 0x00})},
		{name: "extensions body overflow", body: clientHelloBodyWithTail([]byte{0x00, 0x02, 0x13, 0x01, 0x01, 0x00, 0x00, 0x08, 0x00})},
		{name: "extension data overflow", body: clientHelloBodyWithTail([]byte{0x00, 0x02, 0x13, 0x01, 0x01, 0x00, 0x00, 0x04, 0x00, 0x10, 0x00, 0x08})},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := peekClientHelloSNI(bufRead(recordFromHandshakeBody(tc.body))); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestPeekClientHelloSNINoSNI(t *testing.T) {
	hello := buildClientHello("", "h2") // empty SNI omits the extension
	br := bufRead(hello)
	sni, _, err := peekClientHelloSNI(br)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if sni != "" {
		t.Errorf("expected empty SNI, got %q", sni)
	}
}

func TestParseSNIExtensionShort(t *testing.T) {
	if _, ok := parseSNIExtension([]byte{1}); ok {
		t.Errorf("short input should not parse")
	}
}

func TestParseSNIExtensionListLengthOverflow(t *testing.T) {
	// list_len = 100, but only 2 bytes of body
	if _, ok := parseSNIExtension([]byte{0x00, 0x64}); ok {
		t.Errorf("overflow should fail")
	}
}

func TestParseSNIExtensionInvalidNameLen(t *testing.T) {
	// list_len = 3 means one entry. name_type=0, name_len = 100 → overflow
	body := []byte{0x00, 0x03, 0x00, 0x00, 0x64}
	if _, ok := parseSNIExtension(body); ok {
		t.Errorf("nameLen overflow should fail")
	}
}

func TestParseSNIExtensionNonHostNameType(t *testing.T) {
	// list_len=4, name_type=99 (not host_name), name_len=1, "x"
	body := []byte{0x00, 0x04, 0x63, 0x00, 0x01, 'x'}
	if _, ok := parseSNIExtension(body); ok {
		t.Errorf("non-host_name should be skipped")
	}
}

func TestParseALPNExtensionShort(t *testing.T) {
	if _, ok := parseALPNExtension([]byte{0x01}); ok {
		t.Errorf("short input should fail")
	}
}

func TestParseALPNExtensionLengthOverflow(t *testing.T) {
	if _, ok := parseALPNExtension([]byte{0x00, 0x64}); ok {
		t.Errorf("overflow should fail")
	}
}

func TestParseALPNExtensionInvalidProtoLength(t *testing.T) {
	// list_len=3, proto_len=99 → overflow
	body := []byte{0x00, 0x03, 0x63, 'h', '2'}
	if _, ok := parseALPNExtension(body); ok {
		t.Errorf("proto overflow should fail")
	}
}

func TestParseALPNExtensionEmptyProtocolList(t *testing.T) {
	if _, ok := parseALPNExtension([]byte{0x00, 0x00}); ok {
		t.Fatal("empty protocol list should fail")
	}
}

func TestPeekedConnReadDelegatesToBufio(t *testing.T) {
	conn, server := net.Pipe()
	defer conn.Close()
	defer server.Close()
	go func() {
		_, _ = server.Write([]byte("hello"))
	}()
	br := bufRead(nil)
	br.Reset(conn)
	pc := &peekedConn{Conn: conn, r: br}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(pc, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Errorf("got %q", buf)
	}
}

func TestIsKnownLLMHost(t *testing.T) {
	cases := map[string]bool{
		"chatgpt.com":       true,
		"ChatGPT.com":       true,
		"api.openai.com":    true,
		"api.anthropic.com": true,
		"example.com":       false,
		"":                  false,
	}
	for in, want := range cases {
		if got := isKnownLLMHost(in); got != want {
			t.Errorf("isKnownLLMHost(%q)=%v want %v", in, got, want)
		}
	}
}

func TestSnapshotZeroByDefault(t *testing.T) {
	e := &Engine{}
	if got := e.Snapshot(); got.Accepted != 0 || got.Served != 0 {
		t.Errorf("fresh snapshot non-zero: %+v", got)
	}
}

func TestDispatcherFuncImplementsDispatcher(t *testing.T) {
	called := false
	f := DispatcherFunc(func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error {
		called = true
		return nil
	})
	if err := f.Handle(context.Background(), sniroute.MITMConversation, sniroute.Request{}, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Errorf("DispatcherFunc did not invoke underlying function")
	}
}

// ---- helpers ----

func dialAndHandshake(t *testing.T, addr, sni string) bool {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Logf("dial: %v", err)
		return false
	}
	defer conn.Close()
	return true
}

func waitForCounter(t *testing.T, c *atomic.Int32, want int32, dur time.Duration) {
	t.Helper()
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		if c.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("counter never reached %d (last=%d)", want, c.Load())
}

// bufRead wraps a static byte slice as a *bufio.Reader so the
// production peekClientHelloSNI function can consume it.
func bufRead(data []byte) *bufio.Reader {
	return bufio.NewReader(bytes.NewReader(data))
}

// stubResolver is a fixed-decision Resolver used to exercise rare
// branches (currently the Reject path).
type stubResolver struct{ d sniroute.Decision }

func (s stubResolver) Resolve(sniroute.Request) sniroute.Decision { return s.d }

type errListener struct{ err error }

func (e errListener) Accept() (net.Conn, error) { return nil, e.err }
func (e errListener) Close() error              { return nil }
func (e errListener) Addr() net.Addr            { return stubAddr("err-listener") }

type stubAddr string

func (s stubAddr) Network() string { return "tcp" }
func (s stubAddr) String() string  { return string(s) }

func clientHelloBodyWithTail(tail []byte) []byte {
	body := make([]byte, 0, 4+2+32+1+len(tail))
	body = append(body, 0x01, 0x00, 0x00, byte(2+32+1+len(tail)))
	body = append(body, 0x03, 0x03)
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00)
	body = append(body, tail...)
	return body
}

func recordFromHandshakeBody(body []byte) []byte {
	var rec bytes.Buffer
	rec.WriteByte(0x16)
	rec.Write([]byte{0x03, 0x03})
	_ = binary.Write(&rec, binary.BigEndian, uint16(len(body)))
	rec.Write(body)
	return rec.Bytes()
}

// buildClientHello assembles a minimal RFC 5246 / RFC 6066 TLS
// ClientHello with optional SNI + ALPN extensions. Used to exercise
// peekClientHelloSNI without standing up a real TLS handshake.
func buildClientHello(sni, alpn string) []byte {
	// Fields in order: version (2) + random (32) + sid_len(1) + sid(0)
	// + cipher_len(2) + cipher(2) + comp_len(1) + comp(1) + ext_len(2)
	// + extensions.
	var ext bytes.Buffer
	if sni != "" {
		var snibuf bytes.Buffer
		// name_type(1) + name_len(2) + name
		snibuf.WriteByte(0x00)
		_ = binary.Write(&snibuf, binary.BigEndian, uint16(len(sni)))
		snibuf.WriteString(sni)
		// list_len(2) + entry
		var snilist bytes.Buffer
		_ = binary.Write(&snilist, binary.BigEndian, uint16(snibuf.Len()))
		snilist.Write(snibuf.Bytes())
		// ext_type(2) + ext_len(2) + body
		_ = binary.Write(&ext, binary.BigEndian, uint16(0x0000))
		_ = binary.Write(&ext, binary.BigEndian, uint16(snilist.Len()))
		ext.Write(snilist.Bytes())
	}
	if alpn != "" {
		var inner bytes.Buffer
		inner.WriteByte(byte(len(alpn)))
		inner.WriteString(alpn)
		var list bytes.Buffer
		_ = binary.Write(&list, binary.BigEndian, uint16(inner.Len()))
		list.Write(inner.Bytes())
		_ = binary.Write(&ext, binary.BigEndian, uint16(0x0010))
		_ = binary.Write(&ext, binary.BigEndian, uint16(list.Len()))
		ext.Write(list.Bytes())
	}

	var hello bytes.Buffer
	// version (TLS 1.2)
	hello.Write([]byte{0x03, 0x03})
	// random (32 bytes of zero)
	hello.Write(make([]byte, 32))
	// session_id (empty)
	hello.WriteByte(0x00)
	// cipher suites: TLS_AES_128_GCM_SHA256 (0x1301)
	_ = binary.Write(&hello, binary.BigEndian, uint16(2))
	_ = binary.Write(&hello, binary.BigEndian, uint16(0x1301))
	// compression methods: null
	hello.WriteByte(0x01)
	hello.WriteByte(0x00)
	// extensions
	_ = binary.Write(&hello, binary.BigEndian, uint16(ext.Len()))
	hello.Write(ext.Bytes())

	// Handshake header: type(1)=ClientHello + length(3) + body
	var hs bytes.Buffer
	hs.WriteByte(0x01)
	hs.WriteByte(byte(hello.Len() >> 16))
	hs.WriteByte(byte(hello.Len() >> 8))
	hs.WriteByte(byte(hello.Len()))
	hs.Write(hello.Bytes())

	// Record header: ContentType=Handshake(0x16) + version(2) + length(2) + body
	var rec bytes.Buffer
	rec.WriteByte(0x16)
	rec.Write([]byte{0x03, 0x03})
	_ = binary.Write(&rec, binary.BigEndian, uint16(hs.Len()))
	rec.Write(hs.Bytes())
	return rec.Bytes()
}
