package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/tlsdial"
)

func TestNewUpstreamTransport_TransparentGate(t *testing.T) {
	t.Parallel()
	resolver, err := tlsdial.NewResolver("chromium_stable", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	if tr := newUpstreamTransport(cfg, resolver); tr.DialTLSContext != nil {
		t.Fatal("direct mode must keep the standard TLS dial path")
	}
	cfg.Transparent.Enabled = true
	tr := newUpstreamTransport(cfg, resolver)
	if tr.DialTLSContext == nil {
		t.Fatal("transparent mode must install the profiled TLS dial path")
	}
	if _, err := tr.DialTLSContext(context.Background(), "tcp", "bad-address"); err == nil {
		t.Fatal("bad transparent TLS address must fail")
	}
	if _, err := tr.DialTLSContext(context.Background(), "tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("closed transparent TLS upstream must fail")
	}
}

func TestNew_TransparentEnabledWiresConnectInterceptor(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Transparent.Enabled = true
	cfg.Transparent.CADir = t.TempDir()

	p := New(cfg)
	req := httptest.NewRequest(http.MethodConnect, "http://api.openai.com:443", nil)
	req.Host = "api.openai.com:443"
	rec := httptest.NewRecorder()

	p.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("CONNECT without Hijacker status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "hijack not supported") {
		t.Fatalf("CONNECT did not reach interceptor, body = %q", rec.Body.String())
	}
}

func TestNew_TransparentInvalidCADirFallsBackToMux(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Transparent.Enabled = true
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Transparent.CADir = filePath

	p := New(cfg)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	p.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fallback mux health status = %d, want 200", rec.Code)
	}
}

func TestNew_InvalidTLSProfileFallsBackToStdlibResolver(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Transparent.DefaultTLSProfile = "missing"
	p := New(cfg)
	if p == nil || p.httpClients == nil {
		t.Fatal("New must tolerate invalid transparent TLS profile config")
	}
}

func TestNewTransparentSigner_ExplicitCADir(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Transparent.CADir = t.TempDir()
	signer, err := newTransparentSigner(cfg)
	if err != nil {
		t.Fatalf("newTransparentSigner: %v", err)
	}
	if _, err := signer.Cert("api.openai.com"); err != nil {
		t.Fatalf("sign leaf: %v", err)
	}
}

func TestNewTransparentSigner_DefaultCADir(t *testing.T) {
	old := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	defer func() { proxyUserHomeDir = old }()
	cfg := config.Defaults()
	cfg.Transparent.CADir = ""
	if _, err := newTransparentSigner(cfg); err != nil {
		t.Fatalf("newTransparentSigner default dir: %v", err)
	}
}

func TestNewTransparentSigner_HomeError(t *testing.T) {
	old := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return "", os.ErrPermission }
	defer func() { proxyUserHomeDir = old }()
	cfg := config.Defaults()
	cfg.Transparent.CADir = ""
	if _, err := newTransparentSigner(cfg); err == nil {
		t.Fatal("home lookup failure must fail")
	}
}

func TestNewProfiledWebSocketDialer_DialError(t *testing.T) {
	t.Parallel()
	resolver, err := tlsdial.NewResolver("go_stdlib", nil)
	if err != nil {
		t.Fatal(err)
	}
	dialer := newProfiledWebSocketDialer(resolver)
	if _, err := dialer("127.0.0.1", "1"); err == nil {
		t.Fatal("closed upstream port must fail")
	}
}
