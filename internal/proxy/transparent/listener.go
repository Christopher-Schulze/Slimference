// Package transparent implements the T188 transparent TLS listener
// that anchors Phase G's MITM pipeline. It owns the path from
// `accept TCP connection` through `decide route` to either
// `MITM via Phase F pipeline` or `transparent TLS passthrough to real
// upstream`.
//
// Design constraints:
//
//   - Injectable. The actual `:443` bind is the caller's
//     responsibility; we receive a `net.Listener` and a TLS leaf
//     factory. Tests use a localhost loopback listener.
//   - Fail-open. Any error in routing, TLS handshake, or upstream
//     dial logs and falls back to "close cleanly". We never block
//     traffic; the Codex client retries against the real upstream
//     when our listener isn't healthy.
//   - Concurrent. Each accepted connection runs in its own goroutine
//     under a context derived from the engine's lifecycle.
//   - Bounded. A semaphore caps in-flight handshakes so a SYN flood
//     can't OOM us.
package transparent

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/proxy/sniroute"
)

// Dispatcher decides what to do with an accepted, TLS-terminated
// connection. Implementations receive both the routing Decision
// (so they can short-circuit without re-evaluating) and the
// originating Request (SNI, path, UA, etc.).
type Dispatcher interface {
	// Handle takes ownership of conn after TLS handshake. The caller
	// guarantees the TLS handshake completed cleanly. Handle MUST
	// return promptly when ctx is cancelled. Handle is responsible
	// for closing conn.
	Handle(ctx context.Context, decision sniroute.Decision,
		req sniroute.Request, conn net.Conn) error
}

// DispatcherFunc adapts a plain function to Dispatcher.
type DispatcherFunc func(context.Context, sniroute.Decision, sniroute.Request, net.Conn) error

// Handle implements Dispatcher.
func (f DispatcherFunc) Handle(ctx context.Context, d sniroute.Decision,
	r sniroute.Request, conn net.Conn) error {
	return f(ctx, d, r, conn)
}

// LeafCertProvider yields a per-SNI TLS leaf cert signed by our CA.
// internal/tlsca.Signer satisfies this with GetCertificate, but we
// abstract here so tests inject a static cert.
type LeafCertProvider interface {
	GetCertificate(hi *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// Resolver is the minimal contract the engine needs from the SNI
// router. internal/proxy/sniroute.Resolver satisfies this; tests
// inject stubs to exercise rare decision paths (Reject).
type Resolver interface {
	Resolve(sniroute.Request) sniroute.Decision
}

// Engine accepts TLS connections from a caller-provided net.Listener
// and dispatches each one to the routing+handler pipeline.
type Engine struct {
	Listener   net.Listener
	Resolver   Resolver
	Certs      LeafCertProvider
	Dispatcher Dispatcher
	// MaxConcurrent caps the in-flight handshakes. 0 disables the
	// cap. 256 is a safe default for a developer machine.
	MaxConcurrent int
	// HandshakeTimeout bounds the TLS handshake. Defaults to 5s.
	HandshakeTimeout time.Duration
	// ALPN advertised to clients. Defaults to {"http/1.1"} because the
	// Codex conversation transport is WebSocket-over-HTTP/1.1 and the
	// transparent dispatcher preserves the original Upgrade bytes.
	ALPN []string
	// OnError is called for every accept/handshake/dispatch error
	// so callers can surface them to slog/admin telemetry.
	// Defaults to a no-op.
	OnError func(err error)

	// Telemetry counters. Atomic increments; reads via Snapshot.
	accepted atomic.Int64
	served   atomic.Int64
	mitm     atomic.Int64
	passed   atomic.Int64
	rejected atomic.Int64
	errors   atomic.Int64
}

// Telemetry is the read-only snapshot of engine counters surfaced
// to /admin/status.
type Telemetry struct {
	Accepted    int64 `json:"accepted"`
	Served      int64 `json:"served"`
	MITM        int64 `json:"mitm"`
	Passthrough int64 `json:"passthrough"`
	Rejected    int64 `json:"rejected"`
	Errors      int64 `json:"errors"`
}

// Snapshot returns a read-only view of the counters.
func (e *Engine) Snapshot() Telemetry {
	return Telemetry{
		Accepted:    e.accepted.Load(),
		Served:      e.served.Load(),
		MITM:        e.mitm.Load(),
		Passthrough: e.passed.Load(),
		Rejected:    e.rejected.Load(),
		Errors:      e.errors.Load(),
	}
}

// Run accepts connections from the listener until ctx is cancelled
// or the listener returns an error. Each accepted connection is
// dispatched in its own goroutine. Run blocks until both:
//   - the listener stops accepting (returns an error or is closed); AND
//   - every in-flight handshake/dispatch returns.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.validate(); err != nil {
		return err
	}
	if e.MaxConcurrent == 0 {
		e.MaxConcurrent = 256
	}
	if e.HandshakeTimeout == 0 {
		e.HandshakeTimeout = 5 * time.Second
	}
	if e.OnError == nil {
		e.OnError = func(error) {}
	}
	if len(e.ALPN) == 0 {
		e.ALPN = []string{"http/1.1"}
	}

	sem := make(chan struct{}, e.MaxConcurrent)
	var wg sync.WaitGroup

	// Close the listener when ctx is cancelled so blocked Accept()
	// returns immediately.
	go func() {
		<-ctx.Done()
		_ = e.Listener.Close()
	}()

	for {
		raw, err := e.Listener.Accept()
		if err != nil {
			wg.Wait()
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		e.accepted.Add(1)
		sem <- struct{}{}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer func() { <-sem }()
			e.serve(ctx, c)
		}(raw)
	}
}

// validate confirms the required dependencies are populated.
func (e *Engine) validate() error {
	if e.Listener == nil {
		return errors.New("transparent: Listener nil")
	}
	if e.Resolver == nil {
		return errors.New("transparent: Resolver nil")
	}
	if e.Certs == nil {
		return errors.New("transparent: Certs nil")
	}
	if e.Dispatcher == nil {
		return errors.New("transparent: Dispatcher nil")
	}
	return nil
}

// serve handles one accepted connection start to finish. Any error
// reaches OnError but does not propagate; the connection is closed
// quietly.
func (e *Engine) serve(ctx context.Context, raw net.Conn) {
	defer raw.Close()

	// Bound the entire handshake (peek + TLS) under HandshakeTimeout
	// so a stalled / silent client cannot wedge a goroutine.
	if e.HandshakeTimeout > 0 {
		_ = raw.SetDeadline(time.Now().Add(e.HandshakeTimeout))
	}

	// Peek the ClientHello to extract SNI before deciding to perform
	// the TLS handshake. This lets us skip handshake work entirely
	// for traffic we don't care about.
	br := bufio.NewReader(raw)
	sni, _, err := peekClientHelloSNI(br)
	if err != nil {
		e.errors.Add(1)
		e.OnError(fmt.Errorf("peek clienthello: %w", err))
		return
	}

	// Re-wrap the raw conn with the bufio.Reader so the TLS handshake
	// sees the bytes we peeked at.
	peeked := &peekedConn{Conn: raw, r: br}

	tlsCfg := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: e.Certs.GetCertificate,
		NextProtos:     e.ALPN,
	}
	tlsConn := tls.Server(peeked, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		e.errors.Add(1)
		e.OnError(fmt.Errorf("tls handshake: %w", err))
		return
	}
	_ = raw.SetDeadline(time.Time{})

	// Build the routing Request from what we have at this point.
	// Path / Method / UA fill in later from the first HTTP request
	// the dispatcher reads.
	req := sniroute.Request{
		SNI:            sni,
		IsLLMHostMatch: isKnownLLMHost(sni),
	}
	decision := e.Resolver.Resolve(req)

	switch decision {
	case sniroute.MITMConversation:
		e.mitm.Add(1)
	case sniroute.PassthroughTLS:
		e.passed.Add(1)
	case sniroute.Reject:
		e.rejected.Add(1)
		e.served.Add(1)
		return
	}

	if err := e.Dispatcher.Handle(ctx, decision, req, tlsConn); err != nil {
		e.errors.Add(1)
		e.OnError(fmt.Errorf("dispatch: %w", err))
	}
	e.served.Add(1)
}

// isKnownLLMHost returns true when sni is one of the hosts the
// router cares about. Used as an early-exit hint for the dispatcher.
func isKnownLLMHost(sni string) bool {
	switch strings.ToLower(sni) {
	case "chatgpt.com", "api.openai.com", "api.anthropic.com":
		return true
	}
	return false
}

// peekedConn wraps a net.Conn with a buffered reader so subsequent
// reads (TLS handshake, application data) see the bytes we peeked at
// during ClientHello inspection.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (p *peekedConn) Read(b []byte) (int, error) { return p.r.Read(b) }

// peekClientHelloSNI reads enough bytes to parse the SNI extension
// from a TLS ClientHello without consuming them from the underlying
// reader. The bufio.Reader's internal buffer holds the bytes for the
// subsequent handshake to consume.
//
// Returns ("", "", err) on parse failure - callers treat that as a
// fatal "this isn't a TLS connection".
func peekClientHelloSNI(br *bufio.Reader) (string, string, error) {
	hdr, err := br.Peek(5)
	if err != nil {
		return "", "", err
	}
	if hdr[0] != 0x16 { // TLS Handshake
		return "", "", errors.New("not a TLS handshake")
	}
	recordLen := int(binary.BigEndian.Uint16(hdr[3:5]))
	if recordLen < 4 || recordLen > 16384 {
		return "", "", errors.New("invalid record length")
	}
	full, err := br.Peek(5 + recordLen)
	if err != nil {
		return "", "", err
	}
	hello := full[5:]
	if hello[0] != 0x01 { // ClientHello
		return "", "", errors.New("not a ClientHello")
	}
	// hello[1:4] is uint24 length. Skip header, version (2), random
	// (32) to reach session_id.
	off := 4 + 2 + 32
	if off >= len(hello) {
		return "", "", errors.New("hello too short for session_id")
	}
	sidLen := int(hello[off])
	off++
	off += sidLen
	if off+2 > len(hello) {
		return "", "", errors.New("cipher suites overflow")
	}
	cipherLen := int(binary.BigEndian.Uint16(hello[off : off+2]))
	off += 2 + cipherLen
	if off+1 > len(hello) {
		return "", "", errors.New("compression methods overflow")
	}
	compLen := int(hello[off])
	off++
	off += compLen
	if off+2 > len(hello) {
		return "", "", errors.New("extensions length overflow")
	}
	extLen := int(binary.BigEndian.Uint16(hello[off : off+2]))
	off += 2
	end := off + extLen
	if end > len(hello) {
		return "", "", errors.New("extensions body overflow")
	}
	sni := ""
	alpn := ""
	for off+4 <= end {
		extType := binary.BigEndian.Uint16(hello[off : off+2])
		extDataLen := int(binary.BigEndian.Uint16(hello[off+2 : off+4]))
		off += 4
		if off+extDataLen > end {
			return "", "", errors.New("extension data overflow")
		}
		body := hello[off : off+extDataLen]
		switch extType {
		case 0x0000: // SNI
			if s, ok := parseSNIExtension(body); ok {
				sni = s
			}
		case 0x0010: // ALPN
			if a, ok := parseALPNExtension(body); ok {
				alpn = a
			}
		}
		off += extDataLen
	}
	return sni, alpn, nil
}

func parseSNIExtension(body []byte) (string, bool) {
	if len(body) < 2 {
		return "", false
	}
	listLen := int(binary.BigEndian.Uint16(body[:2]))
	if 2+listLen > len(body) {
		return "", false
	}
	off := 2
	for off+3 <= 2+listLen {
		nameType := body[off]
		nameLen := int(binary.BigEndian.Uint16(body[off+1 : off+3]))
		off += 3
		if off+nameLen > len(body) {
			return "", false
		}
		if nameType == 0 { // host_name
			return string(body[off : off+nameLen]), true
		}
		off += nameLen
	}
	return "", false
}

func parseALPNExtension(body []byte) (string, bool) {
	if len(body) < 2 {
		return "", false
	}
	listLen := int(binary.BigEndian.Uint16(body[:2]))
	if 2+listLen > len(body) {
		return "", false
	}
	off := 2
	if off+1 > 2+listLen {
		return "", false
	}
	protoLen := int(body[off])
	off++
	if off+protoLen > len(body) {
		return "", false
	}
	return string(body[off : off+protoLen]), true
}

// Used to silence unused-import warnings when the io package is later
// referenced by Dispatcher implementations that copy data.
var _ = io.EOF
