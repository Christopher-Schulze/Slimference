package proxy

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// resolveUpstreamIP returns one A-record for host via DNS-over-HTTPS,
// bypassing the local resolver (which under transparent-MITM hits
// /etc/hosts and points back at us, causing a self-loop).
//
// Cached for 5 minutes so the DoH endpoint isn't hammered. The first
// successful answer wins; we don't pick the "best" IP.
//
// On any error: returns "", err. Callers fall back to the system
// resolver, which IS still useful when /etc/hosts is clean (no MITM
// armed) or when DoH is blocked.
func resolveUpstreamIP(ctx context.Context, host string) (string, error) {
	if host == "" {
		return "", errors.New("empty host")
	}
	// Already an IP literal? Return as-is.
	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}
	if cached, ok := upstreamIPCache.get(host); ok {
		return cached, nil
	}
	// Query Cloudflare DoH. Wireshark-friendly: returns standard
	// JSON-over-DoH per RFC 8484.
	ip, err := dohResolveAFn(ctx, host)
	if err != nil {
		return "", err
	}
	upstreamIPCache.set(host, ip, 5*time.Minute)
	return ip, nil
}

// upstreamIPCache is a tiny TTL cache keyed by host.
type ipCache struct {
	mu sync.RWMutex
	v  map[string]ipEntry
}

type ipEntry struct {
	ip      string
	expires time.Time
}

func (c *ipCache) get(k string) (string, bool) {
	c.mu.RLock()
	e, ok := c.v[k]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.ip, true
}

func (c *ipCache) set(k, ip string, ttl time.Duration) {
	c.mu.Lock()
	if c.v == nil {
		c.v = make(map[string]ipEntry)
	}
	c.v[k] = ipEntry{ip: ip, expires: time.Now().Add(ttl)}
	c.mu.Unlock()
}

var upstreamIPCache = &ipCache{}

var (
	dohResolveAFn       = dohResolveA
	dohDialTLSContextFn = defaultDoHDialTLSContext
	dohServerAddrs      = []string{"1.1.1.1:443", "1.0.0.1:443"}
	upstreamTLSConfigFn = defaultUpstreamTLSConfig
)

// UpstreamResolutionCheck is one host's transparent-MITM upstream DNS
// preflight result. It is used by `slimference status --preflight` so
// operators can catch DoH/self-loop failures before arming Codex.
type UpstreamResolutionCheck struct {
	Host     string
	OK       bool
	IP       string
	Loopback bool
	Error    string
}

// PreflightUpstreamResolution resolves every host via the same DoH
// path the transparent dispatcher uses. A check is OK only when DoH
// returns a non-loopback A record; loopback would mean the daemon is
// about to dial itself once /etc/hosts is active.
func PreflightUpstreamResolution(ctx context.Context, hosts []string) []UpstreamResolutionCheck {
	out := make([]UpstreamResolutionCheck, 0, len(hosts))
	for _, host := range hosts {
		check := UpstreamResolutionCheck{Host: host}
		ip, err := resolveUpstreamIP(ctx, host)
		if err != nil {
			check.Error = err.Error()
			out = append(out, check)
			continue
		}
		check.IP = ip
		check.Loopback = isLoopbackIPString(ip)
		check.OK = ip != "" && !check.Loopback
		if !check.OK && check.Error == "" {
			check.Error = "resolved to loopback"
		}
		out = append(out, check)
	}
	return out
}

// dohResolveA queries Cloudflare's DNS-over-HTTPS endpoints for an A
// record. Uses RFC 8484 wire-format GET so the request is portable +
// cacheable. The DoH endpoints are dialled by IP (not by hostname) so
// this code path never depends on the system resolver.
//
// 1.1.1.1 is hard-coded because we cannot use the system resolver to
// find a DoH server while /etc/hosts is poisoned. 1.0.0.1 is the
// Cloudflare secondary endpoint and avoids a single-IP hard failure.
func dohResolveA(ctx context.Context, host string) (string, error) {
	query, err := buildDNSQuery(host)
	if err != nil {
		return "", err
	}
	var errs []string
	for _, addr := range dohServerAddrs {
		ip, err := dohResolveAAt(ctx, query, addr)
		if err == nil {
			return ip, nil
		}
		errs = append(errs, addr+": "+err.Error())
	}
	if len(errs) == 0 {
		return "", errors.New("doh: no endpoints configured")
	}
	return "", fmt.Errorf("doh: all endpoints failed: %s", strings.Join(errs, "; "))
}

func dohResolveAAt(ctx context.Context, query []byte, serverAddr string) (string, error) {
	encoded := base64.RawURLEncoding.EncodeToString(query)

	// Dial the DoH endpoint directly by IP, then TLS-handshake with
	// ServerName cloudflare-dns.com. Bypass any system proxy.
	transport := &http.Transport{
		Proxy:                 nil,
		DialTLSContext:        dohDialTLSContextFn,
		ResponseHeaderTimeout: 4 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
	urlHost := serverAddr
	if hostOnly, port, err := net.SplitHostPort(serverAddr); err == nil && port == "443" {
		urlHost = hostOnly
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+urlHost+"/dns-query?dns="+encoded, nil)
	req.Header.Set("Accept", "application/dns-message")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("doh: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	return parseDNSAnswer(body)
}

func defaultDoHDialTLSContext(ctx context.Context, _, addr string) (net.Conn, error) {
	if addr == "" {
		if len(dohServerAddrs) == 0 {
			return nil, errors.New("doh: no endpoint")
		}
		addr = dohServerAddrs[0]
	}
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(raw, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: "cloudflare-dns.com",
		// Force HTTP/1.1 — net/http.Transport doesn't speak
		// h2 over a hand-rolled DialTLSContext without
		// extra wiring. /dns-query supports both.
		NextProtos: []string{"http/1.1"},
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tlsConn, nil
}

func isLoopbackIPString(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.IsLoopback()
}

// buildDNSQuery assembles a minimal A-record query for host. RFC 1035
// header + question section, no additional records.
func buildDNSQuery(host string) ([]byte, error) {
	if !strings.HasSuffix(host, ".") {
		host = host + "."
	}
	var buf []byte
	// Header: id=0, flags=0x0100 (RD=1), 1 question, 0 ans/auth/add.
	buf = append(buf,
		0x00, 0x00, // ID
		0x01, 0x00, // flags
		0x00, 0x01, // qdcount=1
		0x00, 0x00, // ancount
		0x00, 0x00, // nscount
		0x00, 0x00, // arcount
	)
	// Question: name labels.
	for _, label := range strings.Split(host, ".") {
		if label == "" {
			buf = append(buf, 0x00)
			continue
		}
		if len(label) > 63 {
			return nil, errors.New("doh: label too long")
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0x00, 0x01, 0x00, 0x01) // QTYPE=A, QCLASS=IN
	return buf, nil
}

// parseDNSAnswer walks the answer section and returns the first A
// record's IP. Tolerant of additional record types in the answer.
func parseDNSAnswer(msg []byte) (string, error) {
	if len(msg) < 12 {
		return "", errors.New("doh: short response")
	}
	qdcount := binary.BigEndian.Uint16(msg[4:6])
	ancount := binary.BigEndian.Uint16(msg[6:8])
	if ancount == 0 {
		return "", errors.New("doh: no answer")
	}
	pos := 12
	// Skip question section.
	for i := uint16(0); i < qdcount; i++ {
		var ok bool
		pos, ok = skipName(msg, pos)
		if !ok {
			return "", errors.New("doh: parse qname")
		}
		pos += 4 // QTYPE + QCLASS
	}
	// Walk answers.
	for i := uint16(0); i < ancount; i++ {
		var ok bool
		pos, ok = skipName(msg, pos)
		if !ok {
			return "", errors.New("doh: parse aname")
		}
		if pos+10 > len(msg) {
			return "", errors.New("doh: short answer header")
		}
		typ := binary.BigEndian.Uint16(msg[pos : pos+2])
		rdlen := binary.BigEndian.Uint16(msg[pos+8 : pos+10])
		pos += 10
		if pos+int(rdlen) > len(msg) {
			return "", errors.New("doh: short rdata")
		}
		if typ == 1 && rdlen == 4 {
			ip := net.IPv4(msg[pos], msg[pos+1], msg[pos+2], msg[pos+3])
			return ip.String(), nil
		}
		pos += int(rdlen)
	}
	return "", errors.New("doh: no A record")
}

// skipName advances pos past one DNS name, handling compression
// pointers per RFC 1035 §4.1.4.
func skipName(msg []byte, pos int) (int, bool) {
	for pos < len(msg) {
		b := msg[pos]
		if b == 0 {
			return pos + 1, true
		}
		if b&0xc0 == 0xc0 {
			return pos + 2, true
		}
		pos += int(b) + 1
	}
	return pos, false
}

// wrapTLS wraps a raw conn with a TLS client handshake. SNI is set
// to host so the upstream returns its real certificate (not a
// wildcard or load-balancer default).
func wrapTLS(ctx context.Context, raw net.Conn, host string) (net.Conn, error) {
	tlsConn := tls.Client(raw, upstreamTLSConfigFn(host))
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tlsConn, nil
}

func defaultUpstreamTLSConfig(host string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: host,
		// The transparent dispatcher preserves HTTP/1.1 Upgrade bytes
		// for Codex WSS. Keep upstream ALPN on http/1.1 so we never
		// write HTTP/1.1 bytes into an h2-negotiated TLS connection.
		NextProtos: []string{"http/1.1"},
	}
}
