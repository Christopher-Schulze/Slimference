package tlsdial

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestResolveProfile_Aliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{name: "", want: "chromium_stable"},
		{name: "chrome", want: "chromium_stable"},
		{name: "node_stable", want: "chromium_stable"},
		{name: "python_requests", want: "chromium_stable"},
		{name: "go_stdlib", want: "go_stdlib"},
		{name: "chrome_131", want: "chrome_131"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveProfile(tc.name)
			if err != nil {
				t.Fatalf("ResolveProfile(%q): %v", tc.name, err)
			}
			if got.Name != tc.want {
				t.Fatalf("ResolveProfile(%q) = %q, want %q", tc.name, got.Name, tc.want)
			}
		})
	}
}

func TestAliasTarget(t *testing.T) {
	t.Parallel()
	if target, ok := AliasTarget(""); !ok || target != "chromium_stable" {
		t.Fatalf("default alias target = %q %v", target, ok)
	}
	if target, ok := AliasTarget("node_stable"); !ok || target != "chromium_stable" {
		t.Fatalf("node_stable alias target = %q %v", target, ok)
	}
	if target, ok := AliasTarget("chromium_stable"); ok || target != "" {
		t.Fatalf("concrete profile reported alias target = %q %v", target, ok)
	}
}

func TestResolveProfile_Unknown(t *testing.T) {
	t.Parallel()
	if _, err := ResolveProfile("bogus"); err == nil {
		t.Fatal("unknown profile must fail")
	}
}

func TestResolver_HostOverride(t *testing.T) {
	t.Parallel()
	resolver, err := NewResolver("chromium_stable", map[string]string{
		"api.openai.com": "go_stdlib",
		" ":              "go_stdlib",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolver.Resolve("api.openai.com:443"); got.Name != "go_stdlib" {
		t.Fatalf("host override = %q, want go_stdlib", got.Name)
	}
	if got := resolver.Resolve("chatgpt.com"); got.Name != "chromium_stable" {
		t.Fatalf("default profile = %q, want chromium_stable", got.Name)
	}
}

func TestResolver_InvalidProfile(t *testing.T) {
	t.Parallel()
	if _, err := NewResolver("missing", nil); err == nil {
		t.Fatal("invalid default profile must fail")
	}
	if _, err := NewResolver("go_stdlib", map[string]string{"x.test": "missing"}); err == nil {
		t.Fatal("invalid host profile must fail")
	}
}

func TestProfileNames_IncludesAliases(t *testing.T) {
	t.Parallel()
	names := ProfileNames()
	for _, want := range []string{"chromium_stable", "node_stable", "python_requests"} {
		if !containsString(names, want) {
			t.Fatalf("ProfileNames missing %q in %v", want, names)
		}
	}
}

func TestCatalogMetadata(t *testing.T) {
	t.Parallel()
	info := Catalog()
	if info.Version == "" || info.Generated.IsZero() || info.MaxAgeDays <= 0 {
		t.Fatalf("bad catalog info: %+v", info)
	}
	if !containsString(info.Concrete, "chromium_stable") || !containsString(info.Aliases, "node_stable") {
		t.Fatalf("catalog missing expected names: %+v", info)
	}
	if CatalogStale(info.Generated.Add(24 * time.Hour)) {
		t.Fatal("fresh catalog reported stale")
	}
	if !CatalogStale(info.Generated.Add(time.Duration(info.MaxAgeDays+1) * 24 * time.Hour)) {
		t.Fatal("old catalog not reported stale")
	}
	if CatalogStale(info.Generated.Add(-24 * time.Hour)) {
		t.Fatal("future clock skew should not report stale")
	}
}

func TestDial_StdlibAndUTLSProfiles(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	oldStdlib := newStdlibConfig
	oldUTLS := newUTLSConfig
	newStdlibConfig = func(host string) *tls.Config {
		cfg := oldStdlib(host)
		cfg.InsecureSkipVerify = true
		return cfg
	}
	newUTLSConfig = func(host string) *utls.Config {
		cfg := oldUTLS(host)
		cfg.InsecureSkipVerify = true
		return cfg
	}
	defer func() {
		newStdlibConfig = oldStdlib
		newUTLSConfig = oldUTLS
	}()

	stdlibProfile, err := ResolveProfile("go_stdlib")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := Dial(context.Background(), "tcp", host, port, stdlibProfile)
	if err != nil {
		t.Fatalf("stdlib Dial: %v", err)
	}
	_ = conn.Close()

	chromiumProfile, err := ResolveProfile("chromium_stable")
	if err != nil {
		t.Fatal(err)
	}
	conn, err = Dial(context.Background(), "tcp", host, port, chromiumProfile)
	if err != nil {
		t.Fatalf("utls Dial: %v", err)
	}
	_ = conn.Close()
}

func TestDial_DialError(t *testing.T) {
	t.Parallel()
	profile, err := ResolveProfile("go_stdlib")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(context.Background(), "tcp", "127.0.0.1", "1", profile); err == nil {
		t.Fatal("Dial to closed port must fail")
	}
}

func TestDial_HandshakeErrors(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	stdlibProfile, err := ResolveProfile("go_stdlib")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(context.Background(), "tcp", host, port, stdlibProfile); err == nil {
		t.Fatal("stdlib handshake with untrusted cert must fail")
	}

	chromiumProfile, err := ResolveProfile("chromium_stable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(context.Background(), "tcp", host, port, chromiumProfile); err == nil {
		t.Fatal("utls handshake with untrusted cert must fail")
	}
}

func TestDial_UTLSHandshakeHonorsContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	profile, err := ResolveProfile("chromium_stable")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = Dial(ctx, "tcp", host, port, profile)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Dial error = %v, want context deadline", err)
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	default:
	}
}

func TestDial_UTLSContextCanceledAfterHandshake(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	oldUTLS := newUTLSConfig
	oldHook := afterUTLSHandshake
	ctx, cancel := context.WithCancel(context.Background())
	newUTLSConfig = func(host string) *utls.Config {
		cfg := oldUTLS(host)
		cfg.InsecureSkipVerify = true
		return cfg
	}
	afterUTLSHandshake = cancel
	defer func() {
		newUTLSConfig = oldUTLS
		afterUTLSHandshake = oldHook
	}()
	profile, err := ResolveProfile("chromium_stable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(ctx, "tcp", host, port, profile); !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial error = %v, want context canceled", err)
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
