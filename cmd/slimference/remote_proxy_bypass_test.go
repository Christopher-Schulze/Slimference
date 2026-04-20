package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
)

func TestRemoteProxyAdapter_Bypass_ReflectsCachedStatus(t *testing.T) {
	a := &remoteProxyAdapter{mu: sync.RWMutex{}}
	a.status.Bypass = true
	if !a.Bypass() {
		t.Fatal("cached bypass=true not surfaced")
	}
	a.status.Bypass = false
	if a.Bypass() {
		t.Fatal("cached bypass=false not surfaced")
	}
}

func TestRemoteProxyAdapter_SetBypass_PostsJSON(t *testing.T) {
	var captured struct {
		method  string
		path    string
		hdr     string
		payload string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.hdr = r.Header.Get("Content-Type")
		buf := make([]byte, 128)
		n, _ := r.Body.Read(buf)
		captured.payload = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	cfg := config.Defaults()
	cfg.Proxy.ListenAddress = "127.0.0.1"
	// ListenAddr() combines address + port. We use the test server's port.
	port := 0
	_, _ = u.User, u.Host
	// Parse port from URL host.
	parts := strings.Split(u.Host, ":")
	if len(parts) == 2 {
		// Atoi would pull it but we keep this robust against malformed hosts.
		_, _ = u.Scheme, u.Host
		// Just use the whole host via a one-shot integer parser.
		for _, c := range parts[1] {
			port = port*10 + int(c-'0')
		}
	}
	cfg.Proxy.ListenPort = port

	a := &remoteProxyAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 2 * time.Second},
	}
	a.SetBypass(true)

	if captured.method != http.MethodPost {
		t.Fatalf("method = %q", captured.method)
	}
	if captured.path != proxy.AdminBypassPath {
		t.Fatalf("path = %q", captured.path)
	}
	if captured.hdr != "application/json" {
		t.Fatalf("Content-Type = %q", captured.hdr)
	}
	if !strings.Contains(captured.payload, `"enabled":true`) {
		t.Fatalf("payload = %q", captured.payload)
	}
}

func TestRemoteProxyAdapter_SetBypass_UnreachableServerSwallowsError(t *testing.T) {
	cfg := config.Defaults()
	cfg.Proxy.ListenAddress = "127.0.0.1"
	cfg.Proxy.ListenPort = 1 // unroutable
	a := &remoteProxyAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 100 * time.Millisecond},
	}
	// Must not panic or block forever.
	a.SetBypass(true)
}

func TestRemoteProxyAdapter_SetBypass_InvalidAddressReturnsQuietly(t *testing.T) {
	cfg := config.Defaults()
	// Pathological listen address with a space - http.NewRequest fails on
	// the malformed URL. SetBypass must swallow the error silently.
	cfg.Proxy.ListenAddress = "not a valid host"
	cfg.Proxy.ListenPort = 0
	a := &remoteProxyAdapter{
		cfg:    cfg,
		client: &http.Client{Timeout: 100 * time.Millisecond},
	}
	a.SetBypass(false)
}
