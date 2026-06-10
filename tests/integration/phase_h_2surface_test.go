//go:build integration

package integration_test

// Phase H 2-surface end-to-end tests (T204). Each test exercises the
// FULL pipeline: TLS handshake into transparent.Engine → SNI peek →
// sniroute Resolver decision → PhaseFDispatcher byte-bridge to a
// fake upstream. No real network calls.
//
// Three scenarios:
//   - Codex CLI       (SNI chatgpt.com, /backend-api/codex/responses, WSS subprotocol)
//   - Codex Desktop   (SNI chatgpt.com, /backend-api/codex/realtime)
//   - Claude Code     (SNI api.anthropic.com, /v1/messages, POST)
//
// Phase C1 currently routes purely on SNI (Path/Method/UA are
// post-handshake metadata the dispatcher does not yet read). The
// SNI-only decisions are verified here; the richer per-path / per-UA
// decisions live in `internal/proxy/sniroute/sniroute_test.go` and
// will be exercised end-to-end once Phase C2 lands.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/sniroute"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/transparent"
	"github.com/Christopher-Schulze/Slimference/internal/tlsca"
)

type phaseHFixture struct {
	t          *testing.T
	dir        string
	ca         *tlsca.CA
	engine     *transparent.Engine
	dispatcher *proxy.PhaseFDispatcher
	port       int
	upstream   net.Conn // peer end of the dispatcher's bridge
	cancel     context.CancelFunc
}

func newPhaseHFixture(t *testing.T) *phaseHFixture {
	t.Helper()
	dir := t.TempDir()
	caDir := filepath.Join(dir, ".slimference")
	if err := osMkdirAll(caDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ca, err := tlsca.LoadOrGenerateCA(caDir)
	if err != nil {
		t.Fatalf("CA: %v", err)
	}

	port := pickFreePort(t)
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	m, err := apps.NewManager(filepath.Join(caDir, "apps.toml"))
	if err != nil {
		t.Fatalf("apps.NewManager: %v", err)
	}

	upstreamRemote, upstreamLocal := net.Pipe()
	dispatcher := &proxy.PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}
	engine := &transparent.Engine{
		Listener:   ln,
		Resolver:   sniroute.New(m),
		Certs:      tlsca.NewSigner(ca, 32),
		Dispatcher: dispatcher,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = engine.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		_ = upstreamRemote.Close()
	})
	// Brief wait for the listener loop.
	time.Sleep(30 * time.Millisecond)

	return &phaseHFixture{
		t:          t,
		dir:        dir,
		ca:         ca,
		engine:     engine,
		dispatcher: dispatcher,
		port:       port,
		upstream:   upstreamRemote,
		cancel:     cancel,
	}
}

func (f *phaseHFixture) dialClient(sni string) *tls.Conn {
	f.t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(f.ca.Cert)
	conn, err := tls.Dial("tcp", "127.0.0.1:"+strconv.Itoa(f.port), &tls.Config{
		RootCAs:    roots,
		ServerName: sni,
	})
	if err != nil {
		f.t.Fatalf("client TLS dial: %v", err)
	}
	return conn
}

func (f *phaseHFixture) expectByteFlow(client *tls.Conn, payload string) {
	f.t.Helper()
	go func() {
		_, _ = client.Write([]byte(payload))
		_ = client.CloseWrite()
	}()
	buf := make([]byte, len(payload))
	if err := f.upstream.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		f.t.Fatalf("set deadline: %v", err)
	}
	defer f.upstream.SetReadDeadline(time.Time{})
	if _, err := io.ReadFull(f.upstream, buf); err != nil {
		f.t.Fatalf("upstream read: %v", err)
	}
	if string(buf) != payload {
		f.t.Fatalf("bridge mangled bytes: got %q want %q", buf, payload)
	}
}

// TestCodexCLIConversationEndToEnd covers the SNI=chatgpt.com path.
// Phase C1 routes on SNI alone; full per-path Decision happens in C2.
func TestCodexCLIConversationEndToEnd(t *testing.T) {
	f := newPhaseHFixture(t)
	conn := f.dialClient("chatgpt.com")
	defer conn.Close()
	f.expectByteFlow(conn, "GET /backend-api/codex/responses HTTP/1.1\r\nHost: chatgpt.com\r\nUpgrade: websocket\r\nSec-WebSocket-Protocol: responses_websockets-2026-02-06\r\n\r\n")
	snap := f.dispatcher.Snapshot()
	if snap.PassthroughBridged+snap.MITMBridged < 1 {
		t.Errorf("no bridge counted: %+v", snap)
	}
}

// TestCodexDesktopRealtimeEndToEnd covers the realtime sideband path.
// chatgpt.com SNI, expected to bridge.
func TestCodexDesktopRealtimeEndToEnd(t *testing.T) {
	f := newPhaseHFixture(t)
	conn := f.dialClient("chatgpt.com")
	defer conn.Close()
	f.expectByteFlow(conn, "POST /backend-api/codex/realtime HTTP/1.1\r\nHost: chatgpt.com\r\nContent-Length: 0\r\n\r\n")
}

// TestClaudeCodeMessagesEndToEnd covers SNI=api.anthropic.com.
func TestClaudeCodeMessagesEndToEnd(t *testing.T) {
	f := newPhaseHFixture(t)
	conn := f.dialClient("api.anthropic.com")
	defer conn.Close()
	f.expectByteFlow(conn, "POST /v1/messages HTTP/1.1\r\nHost: api.anthropic.com\r\nContent-Length: 0\r\n\r\n")
}

// TestUnknownSNIStillBridges verifies the fail-open semantics: an SNI
// we don't recognize still gets a TLS handshake + a byte-bridge. The
// engine does not reject — that's by design.
func TestUnknownSNIStillBridges(t *testing.T) {
	f := newPhaseHFixture(t)
	conn := f.dialClient("example.com")
	defer conn.Close()
	f.expectByteFlow(conn, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
}

// (Removed TestNoLegacySurfacesInIntegrationTests: scanned the host
// process environment, which is brittle — a developer or CI runner
// with HTTPS_PROXY set in their shell for unrelated reasons would
// fail it. The intent is now documented in AGENTS.md §9: this fixture
// does not call t.Setenv on any legacy surface, and that contract is
// enforced by code review rather than a runtime probe.)

// Helpers — small indirections so the test file does not need to
// import os twice or fight import grouping.

func osMkdirAll(path string, mode uint32) error { return mkdirAll(path, mode) }

// mkdirAll defined in helpers.go so this file stays focused
// on test scenarios.

// pickFreePort + helpers live alongside the cmd/slimference tests;
// we redefine a local copy here to keep this integration package
// self-contained.
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// concurrency helpers — net.Pipe writes are synchronous, so chaining
// a goroutine to write and a deadline-bounded read is the common pattern.
var (
	_ = (*sync.Mutex)(nil)
	_ = (*tls.Conn)(nil)
)
