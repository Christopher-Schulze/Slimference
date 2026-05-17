package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildDNSQueryShape(t *testing.T) {
	q, err := buildDNSQuery("example.com")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(q) < 12 {
		t.Fatal("too short")
	}
	// Header: ID(2) + flags(2) + counts(8) = 12 bytes
	// flags should have RD bit set (0x0100)
	if q[2] != 0x01 {
		t.Errorf("flags[0]=0x%02x want 0x01", q[2])
	}
	// QTYPE=A=1, QCLASS=IN=1 at the end
	last := q[len(q)-4:]
	if last[1] != 0x01 || last[3] != 0x01 {
		t.Errorf("trailing QTYPE/QCLASS wrong: %x", last)
	}
}

func TestBuildDNSQueryEmptyLabelHandled(t *testing.T) {
	if _, err := buildDNSQuery(strings.Repeat("a", 64) + ".com"); err == nil {
		t.Error("expected error on label >63 bytes")
	}
	if q, err := buildDNSQuery("example.com."); err != nil || q[len(q)-5] != 0 {
		t.Fatalf("trailing-dot query wrong len=%d err=%v", len(q), err)
	}
}

func TestParseDNSAnswerNoAnswer(t *testing.T) {
	// 12-byte header + qdcount=0, ancount=0
	msg := []byte{0, 0, 0x81, 0x80, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := parseDNSAnswer(msg); err == nil {
		t.Error("expected error on no answer")
	}
}

func TestParseDNSAnswerRejectsMalformedMessages(t *testing.T) {
	cases := []struct {
		name string
		msg  []byte
		want string
	}{
		{name: "short response", msg: []byte{0, 1}, want: "short response"},
		{name: "bad qname", msg: []byte{0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0, 63}, want: "parse qname"},
		{name: "bad aname", msg: []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 63}, want: "parse aname"},
		{name: "short answer header", msg: []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}, want: "short answer header"},
		{name: "short rdata", msg: []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4, 1}, want: "short rdata"},
		{name: "no a record", msg: []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 5, 0, 1, 0, 0, 0, 0, 0, 1, 0}, want: "no A record"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDNSAnswer(tc.msg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestParseDNSAnswerARecord(t *testing.T) {
	// Hand-crafted DNS response with a single A record for example.com → 93.184.216.34
	msg := []byte{
		0x00, 0x00, // ID
		0x81, 0x80, // flags
		0x00, 0x01, // qdcount
		0x00, 0x01, // ancount
		0x00, 0x00, // nscount
		0x00, 0x00, // arcount
		// Question: example.com A IN
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0x00, 0x01, // QTYPE A
		0x00, 0x01, // QCLASS IN
		// Answer: name pointer to question (0xc00c), type A, class IN, TTL 60, rdlen 4, rdata
		0xc0, 0x0c,
		0x00, 0x01, // TYPE A
		0x00, 0x01, // CLASS IN
		0x00, 0x00, 0x00, 0x3c, // TTL 60
		0x00, 0x04, // RDLEN 4
		93, 184, 216, 34,
	}
	ip, err := parseDNSAnswer(msg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ip != "93.184.216.34" {
		t.Errorf("got %q want 93.184.216.34", ip)
	}
}

func TestResolveUpstreamIPLiteralPassthrough(t *testing.T) {
	got, err := resolveUpstreamIP(context.Background(), "192.0.2.1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "192.0.2.1" {
		t.Errorf("got %q", got)
	}
}

func TestResolveUpstreamIPCacheHitBypassesDoH(t *testing.T) {
	upstreamIPCache = &ipCache{}
	t.Cleanup(func() { upstreamIPCache = &ipCache{} })
	upstreamIPCache.set("cached.example", "203.0.113.7", time.Minute)
	got, err := resolveUpstreamIP(context.Background(), "cached.example")
	if err != nil {
		t.Fatalf("cache hit returned err: %v", err)
	}
	if got != "203.0.113.7" {
		t.Fatalf("cache hit=%q", got)
	}
}

func TestResolveUpstreamIPDoHSuccessAndError(t *testing.T) {
	prevResolve := dohResolveAFn
	t.Cleanup(func() {
		dohResolveAFn = prevResolve
		upstreamIPCache = &ipCache{}
	})
	upstreamIPCache = &ipCache{}

	dohResolveAFn = func(_ context.Context, host string) (string, error) {
		if host != "fresh.example" {
			t.Fatalf("host=%q", host)
		}
		return "198.51.100.9", nil
	}
	got, err := resolveUpstreamIP(context.Background(), "fresh.example")
	if err != nil || got != "198.51.100.9" {
		t.Fatalf("resolve success got=%q err=%v", got, err)
	}
	dohResolveAFn = func(context.Context, string) (string, error) {
		t.Fatal("cache hit should not call DoH again")
		return "", nil
	}
	got, err = resolveUpstreamIP(context.Background(), "fresh.example")
	if err != nil || got != "198.51.100.9" {
		t.Fatalf("cached resolve got=%q err=%v", got, err)
	}

	dohResolveAFn = func(context.Context, string) (string, error) {
		return "", context.DeadlineExceeded
	}
	if _, err := resolveUpstreamIP(context.Background(), "fail.example"); err == nil {
		t.Fatal("expected propagated DoH error")
	}
}

func TestResolveUpstreamIPEmptyError(t *testing.T) {
	if _, err := resolveUpstreamIP(context.Background(), ""); err == nil {
		t.Error("expected error on empty host")
	}
}

func TestPreflightUpstreamResolutionRejectsLoopback(t *testing.T) {
	prevResolve := dohResolveAFn
	t.Cleanup(func() {
		dohResolveAFn = prevResolve
		upstreamIPCache = &ipCache{}
	})
	upstreamIPCache = &ipCache{}
	dohResolveAFn = func(_ context.Context, host string) (string, error) {
		if host == "loop.example" {
			return "127.0.0.1", nil
		}
		return "198.51.100.12", nil
	}
	got := PreflightUpstreamResolution(context.Background(), []string{"ok.example", "loop.example"})
	if len(got) != 2 {
		t.Fatalf("checks=%d", len(got))
	}
	if !got[0].OK || got[0].IP != "198.51.100.12" {
		t.Fatalf("first check wrong: %+v", got[0])
	}
	if got[1].OK || !got[1].Loopback || !strings.Contains(got[1].Error, "loopback") {
		t.Fatalf("loopback check wrong: %+v", got[1])
	}
}

func TestPreflightUpstreamResolutionRecordsResolveError(t *testing.T) {
	prevResolve := dohResolveAFn
	t.Cleanup(func() {
		dohResolveAFn = prevResolve
		upstreamIPCache = &ipCache{}
	})
	upstreamIPCache = &ipCache{}
	dohResolveAFn = func(context.Context, string) (string, error) {
		return "", errors.New("doh offline")
	}
	got := PreflightUpstreamResolution(context.Background(), []string{"api.openai.com"})
	if len(got) != 1 {
		t.Fatalf("checks=%d", len(got))
	}
	if got[0].OK || got[0].IP != "" || !strings.Contains(got[0].Error, "doh offline") {
		t.Fatalf("resolve-error check wrong: %+v", got[0])
	}
}

func TestIPCacheTTLExpiry(t *testing.T) {
	c := &ipCache{}
	if _, ok := c.get("missing"); ok {
		t.Fatal("empty cache should miss")
	}
	c.set("x", "1.2.3.4", 10*time.Millisecond)
	if ip, ok := c.get("x"); !ok || ip != "1.2.3.4" {
		t.Errorf("fresh: got %q ok=%v", ip, ok)
	}
	time.Sleep(15 * time.Millisecond)
	if _, ok := c.get("x"); ok {
		t.Error("expected miss after TTL")
	}
}

func TestSkipNamePointerAndFailure(t *testing.T) {
	if pos, ok := skipName([]byte{0xc0, 0x0c}, 0); !ok || pos != 2 {
		t.Fatalf("pointer skip pos=%d ok=%v", pos, ok)
	}
	if pos, ok := skipName([]byte{5, 'a'}, 0); ok || pos <= 0 {
		t.Fatalf("truncated name pos=%d ok=%v", pos, ok)
	}
}

func TestWrapTLSHandshakeFailureClosesRawConn(t *testing.T) {
	client, server := newPipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = server.Read(make([]byte, 1))
		_ = server.Close()
	}()
	_, err := wrapTLS(context.Background(), client, "example.com")
	if err == nil {
		t.Fatal("expected TLS handshake failure")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server side did not observe close")
	}
}

func TestWrapTLSSuccess(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	raw, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	prev := upstreamTLSConfigFn
	upstreamTLSConfigFn = func(host string) *tls.Config {
		return &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         host,
			NextProtos:         []string{"http/1.1"},
		}
	}
	t.Cleanup(func() { upstreamTLSConfigFn = prev })

	conn, err := wrapTLS(context.Background(), raw, "example.com")
	if err != nil {
		t.Fatalf("wrapTLS: %v", err)
	}
	defer conn.Close()
	if state := conn.(*tls.Conn).ConnectionState(); !state.HandshakeComplete {
		t.Fatal("handshake not complete")
	}
}

func TestDoHResolveAWithLocalTLSServer(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    []byte
		wantIP  string
		wantErr string
	}{
		{name: "success", status: http.StatusOK, body: dnsAResponse(93, 184, 216, 34), wantIP: "93.184.216.34"},
		{name: "non-ok", status: http.StatusTeapot, body: []byte("nope"), wantErr: "status 418"},
		{name: "bad-dns", status: http.StatusOK, body: []byte{1, 2}, wantErr: "short response"},
		{name: "truncated-body", status: http.StatusOK, body: []byte{1}, wantErr: "unexpected EOF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/dns-query" || r.URL.Query().Get("dns") == "" {
					t.Fatalf("unexpected request URL: %s", r.URL.String())
				}
				if got := r.Header.Get("Accept"); got != "application/dns-message" {
					t.Fatalf("accept=%q", got)
				}
				if tc.name == "truncated-body" {
					w.Header().Set("Content-Length", "10")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()

			prevDial := dohDialTLSContextFn
			dohDialTLSContextFn = func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := tls.Dialer{Config: &tls.Config{
					InsecureSkipVerify: true,
					NextProtos:         []string{"http/1.1"},
				}}
				return d.DialContext(ctx, "tcp", srv.Listener.Addr().String())
			}
			t.Cleanup(func() { dohDialTLSContextFn = prevDial })

			ip, err := dohResolveA(context.Background(), "example.com")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || ip != tc.wantIP {
				t.Fatalf("ip=%q err=%v want %q", ip, err, tc.wantIP)
			}
		})
	}
}

func TestDoHResolveAFallsBackToSecondaryEndpoint(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(dnsAResponse(93, 184, 216, 34))
	}))
	defer srv.Close()

	prevDial := dohDialTLSContextFn
	prevAddrs := dohServerAddrs
	var seen []string
	dohServerAddrs = []string{"primary.example:443", "secondary.example:443"}
	dohDialTLSContextFn = func(ctx context.Context, _, addr string) (net.Conn, error) {
		seen = append(seen, addr)
		if addr == "primary.example:443" {
			return nil, errors.New("primary down")
		}
		d := tls.Dialer{Config: &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"http/1.1"},
		}}
		return d.DialContext(ctx, "tcp", srv.Listener.Addr().String())
	}
	t.Cleanup(func() {
		dohDialTLSContextFn = prevDial
		dohServerAddrs = prevAddrs
	})

	ip, err := dohResolveA(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("fallback resolve: %v", err)
	}
	if ip != "93.184.216.34" {
		t.Fatalf("ip=%q", ip)
	}
	if len(seen) != 2 || seen[0] != "primary.example:443" || seen[1] != "secondary.example:443" {
		t.Fatalf("endpoint sequence=%v", seen)
	}
}

func TestDoHResolveAEndpointConfigurationErrors(t *testing.T) {
	prevDial := dohDialTLSContextFn
	prevAddrs := dohServerAddrs
	t.Cleanup(func() {
		dohDialTLSContextFn = prevDial
		dohServerAddrs = prevAddrs
	})

	dohServerAddrs = nil
	if _, err := dohResolveA(context.Background(), "example.com"); err == nil || !strings.Contains(err.Error(), "no endpoints") {
		t.Fatalf("empty endpoint err=%v", err)
	}

	dohServerAddrs = []string{"primary.example:443", "secondary.example:443"}
	dohDialTLSContextFn = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("endpoint down")
	}
	if _, err := dohResolveA(context.Background(), "example.com"); err == nil || !strings.Contains(err.Error(), "all endpoints failed") || !strings.Contains(err.Error(), "secondary.example") {
		t.Fatalf("all-failed err=%v", err)
	}
}

func TestDefaultDoHDialTLSContextErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := defaultDoHDialTLSContext(ctx, "", ""); err == nil {
		t.Fatal("expected canceled dial error")
	}

	prevAddrs := dohServerAddrs
	dohServerAddrs = nil
	if _, err := defaultDoHDialTLSContext(context.Background(), "", ""); err == nil || !strings.Contains(err.Error(), "no endpoint") {
		t.Fatalf("empty endpoint err=%v", err)
	}
	dohServerAddrs = prevAddrs

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err == nil {
			_ = c.Close()
		}
	}()
	if _, err := defaultDoHDialTLSContext(context.Background(), "", ln.Addr().String()); err == nil {
		t.Fatal("expected handshake error against plain TCP")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("plain TCP server did not accept test connection")
	}
}

func dnsAResponse(a, b, c, d byte) []byte {
	return []byte{
		0x00, 0x00,
		0x81, 0x80,
		0x00, 0x01,
		0x00, 0x01,
		0x00, 0x00,
		0x00, 0x00,
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0x00, 0x01,
		0x00, 0x01,
		0xc0, 0x0c,
		0x00, 0x01,
		0x00, 0x01,
		0x00, 0x00, 0x00, 0x3c,
		0x00, 0x04,
		a, b, c, d,
	}
}

// TestDoHResolveLive hits the real Cloudflare DoH endpoint. Skipped
// without network. Guarded against flakiness with a short timeout.
func TestDoHResolveLive(t *testing.T) {
	if testing.Short() {
		t.Skip("network test; skipped in -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ip, err := dohResolveA(ctx, "example.com")
	if err != nil {
		t.Skipf("DoH unreachable (offline?): %v", err)
	}
	if ip == "" {
		t.Error("empty IP")
	}
}
