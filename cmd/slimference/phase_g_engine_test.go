package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control/apps"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/proxy/sniroute"
	"github.com/slimference/slimference/internal/proxy/transparent"
	"github.com/slimference/slimference/internal/tlsca"
)

// TestEngineWithFakeDispatcherBridgesBytes wires a transparent.Engine
// directly to a fake dispatcher whose UpstreamDial returns an
// in-memory pipe. This exercises the full TLS-MITM seam (peek + leaf
// signing + sniroute decision + dispatcher bridge) without making any
// real outbound network call.
//
// The startSNIPeekEngine wrapper is covered by the three smaller
// tests below; this one focuses on the on-wire behaviour.
func TestEngineWithFakeDispatcherBridgesBytes(t *testing.T) {
	dir := t.TempDir()

	// Pre-generate the CA.
	caDir := filepath.Join(dir, ".slimference")
	if err := os.MkdirAll(caDir, 0o755); err != nil {
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

	// Set up apps.Manager + Resolver.
	m, err := apps.NewManager(filepath.Join(dir, ".slimference", "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	resolver := sniroute.New(m)

	// In-memory upstream that records the bytes the dispatcher sent
	// us C2S and echoes anything it receives so we can also see S2C.
	upstreamRemote, upstreamLocal := net.Pipe()
	dispatcher := &proxy.PhaseFDispatcher{
		UpstreamDial: func(_ context.Context, _ string) (net.Conn, error) {
			return upstreamLocal, nil
		},
	}

	engine := &transparent.Engine{
		Listener:   ln,
		Resolver:   resolver,
		Certs:      tlsca.NewSigner(ca, 32),
		Dispatcher: dispatcher,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = engine.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
	})

	// Trust pool with our CA so the client accepts the leaf.
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	clientCfg := &tls.Config{
		RootCAs:    roots,
		ServerName: "chatgpt.com",
	}

	conn, err := tls.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port), clientCfg)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer conn.Close()

	if !conn.ConnectionState().HandshakeComplete {
		t.Fatal("handshake not complete")
	}

	// Send some bytes through the bridge; expect them on upstreamRemote.
	payload := []byte("PING")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("client write: %v", err)
	}
	buf := make([]byte, len(payload))
	if err := readAll(upstreamRemote, buf, 2*time.Second); err != nil {
		t.Fatalf("upstream read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("byte mismatch: got %q want %q", buf, payload)
	}

	// Counters tick.
	if got := dispatcher.Snapshot().PassthroughBridged; got != 1 {
		t.Errorf("passthrough counter=%d, want 1", got)
	}

	_ = conn.Close()
	_ = upstreamRemote.Close()
}

func readAll(c net.Conn, buf []byte, timeout time.Duration) error {
	if err := c.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	defer c.SetReadDeadline(time.Time{})
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return err
		}
	}
	return nil
}

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

// TestStartSNIPeekEngineDisabledByDefault verifies the engine is NOT
// started when SNIPeekMode is false.
func TestStartSNIPeekEngineDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	cfg := config.Defaults()
	// SNIPeekMode default is false.
	p := proxy.New(cfg)
	engine, cancel := startSNIPeekEngine(p, cfg, nil)
	if engine != nil {
		t.Fatal("expected nil engine when SNIPeekMode disabled")
	}
	if cancel != nil {
		t.Fatal("expected nil cancel")
	}
}

func TestStartSNIPeekEngineRejectsZeroPort(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cfg.Transparent.SNIPeekPort = 0
	p := proxy.New(cfg)
	engine, _ := startSNIPeekEngine(p, cfg, nil)
	if engine != nil {
		t.Fatal("expected nil engine when port = 0")
	}
}

func TestStartSNIPeekEngineBindFailure(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	// Hold a port so the engine cannot bind.
	port := pickFreePort(t)
	hold, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer hold.Close()

	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cfg.Transparent.SNIPeekPort = port
	p := proxy.New(cfg)
	engine, cancel := startSNIPeekEngine(p, cfg, nil)
	if engine != nil {
		t.Fatal("expected nil engine on bind failure")
	}
	if cancel != nil {
		t.Fatal("expected nil cancel on bind failure")
	}
}

func TestStartSNIPeekEngineCALoadFailure(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-as-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write home blocker: %v", err)
	}
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return homeFile, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	port := pickFreePort(t)
	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cfg.Transparent.SNIPeekPort = port
	p := proxy.New(cfg)
	engine, cancel := startSNIPeekEngine(p, cfg, nil)
	if engine != nil || cancel != nil {
		t.Fatalf("expected engine disabled on CA load failure, engine=%v cancel=%v", engine, cancel)
	}
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond); err == nil {
		t.Fatal("listener should be closed after CA load failure")
	}
}

// TestStartSNIPeekEngineSucceedsAndStops verifies the happy path of
// the wrapper: with SNIPeekMode true and a valid port, the wrapper
// constructs the engine, returns a non-nil cancel, and the cancel
// stops the goroutine without leaking.
func TestStartSNIPeekEngineSucceedsAndStops(t *testing.T) {
	dir := t.TempDir()
	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { osUserHomeDir = prev })

	caDir := filepath.Join(dir, ".slimference")
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := tlsca.LoadOrGenerateCA(caDir); err != nil {
		t.Fatalf("CA: %v", err)
	}

	port := pickFreePort(t)
	cfg := config.Defaults()
	cfg.Transparent.SNIPeekMode = true
	cfg.Transparent.SNIPeekPort = port

	p := proxy.New(cfg)
	engine, cancel := startSNIPeekEngine(p, cfg, nil)
	if engine == nil || cancel == nil {
		t.Fatal("expected non-nil engine and cancel")
	}
	// Cancel; subsequent dials should fail (engine listener closes on
	// ctx end).
	cancel()
	// Give Run a moment to unwind.
	time.Sleep(50 * time.Millisecond)
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond); err == nil {
		t.Errorf("expected dial to fail after cancel")
	}
}
