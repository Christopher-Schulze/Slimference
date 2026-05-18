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

func TestNewUpstreamTransport_ProfiledTLSAlwaysOn(t *testing.T) {
	t.Parallel()
	resolver, err := tlsdial.NewResolver("chromium_stable", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	tr := newUpstreamTransport(cfg, resolver)
	if tr.DialTLSContext == nil {
		t.Fatal("scoped HTTP and transparent mode must share the profiled TLS dial path")
	}
	if _, err := tr.DialTLSContext(context.Background(), "tcp", "bad-address"); err == nil {
		t.Fatal("bad profiled TLS address must fail")
	}
	if _, err := tr.DialTLSContext(context.Background(), "tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("closed profiled TLS upstream must fail")
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

func TestNew_ScopedDesktopProxyWiresConnectOnlyWithExistingCA(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Transparent.Enabled = false
	cfg.Transparent.ScopedDesktopProxy = true
	cfg.Transparent.CADir = t.TempDir()
	if _, err := newTransparentSigner(cfg); err != nil {
		t.Fatalf("prepare CA: %v", err)
	}

	p := New(cfg)
	req := httptest.NewRequest(http.MethodConnect, "http://chatgpt.com:443", nil)
	req.Host = "chatgpt.com:443"
	rec := httptest.NewRecorder()
	p.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("CONNECT without Hijacker status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "hijack not supported") {
		t.Fatalf("CONNECT did not reach scoped interceptor, body = %q", rec.Body.String())
	}
}

func TestNew_ScopedDesktopProxyMissingCADoesNotGenerateOrWire(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Transparent.Enabled = false
	cfg.Transparent.ScopedDesktopProxy = true
	cfg.Transparent.CADir = t.TempDir()

	p := New(cfg)
	req := httptest.NewRequest(http.MethodConnect, "http://chatgpt.com:443", nil)
	req.Host = "chatgpt.com:443"
	rec := httptest.NewRecorder()
	p.server.Handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "hijack not supported") {
		t.Fatalf("missing CA must not wire scoped CONNECT interceptor")
	}
	if _, err := os.Stat(filepath.Join(cfg.Transparent.CADir, "ca", "root.crt")); !os.IsNotExist(err) {
		t.Fatalf("scoped desktop proxy must not generate CA on daemon start, stat err=%v", err)
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

func TestShouldBridgeCodexConversationWSS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		host  string
		path  string
		proto string
		want  bool
	}{
		{name: "conversation no protocol", host: "chatgpt.com", path: "/backend-api/codex/responses", want: true},
		{name: "conversation responses protocol", host: "chatgpt.com", path: "/backend-api/codex/responses", proto: "responses_websockets=2026-02-06", want: true},
		{name: "sideband", host: "chatgpt.com", path: "/backend-api/accounts/check", want: false},
		{name: "wrong protocol", host: "chatgpt.com", path: "/backend-api/codex/responses", proto: "chatgpt-sideband", want: false},
		{name: "wrong host", host: "api.openai.com", path: "/backend-api/codex/responses", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.proto != "" {
				req.Header.Set("Sec-WebSocket-Protocol", tc.proto)
			}
			if got := shouldBridgeCodexConversationWSS(tc.host, req); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
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
