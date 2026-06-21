package tlsca

import (
	"crypto/tls"
	"crypto/x509"
	"sync"
	"testing"
	"time"
)

func newTestSigner(t *testing.T, cacheSize int) *Signer {
	t.Helper()
	ca, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("CA: %v", err)
	}
	return NewSigner(ca, cacheSize)
}

func TestSigner_GetCertificate_PopulatesCache(t *testing.T) {
	s := newTestSigner(t, 4)
	cert, err := s.GetCertificate(&tls.ClientHelloInfo{ServerName: "api.openai.com"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(cert.Certificate) != 2 {
		t.Fatalf("expected leaf+CA chain, got %d cert bytes blocks", len(cert.Certificate))
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if leaf.Subject.CommonName != "api.openai.com" {
		t.Fatalf("expected CN=api.openai.com, got %q", leaf.Subject.CommonName)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "api.openai.com" {
		t.Fatalf("expected SAN dns api.openai.com, got %v", leaf.DNSNames)
	}
	if leaf.IsCA {
		t.Fatal("leaf must not have IsCA=true")
	}
	if s.Size() != 1 {
		t.Fatalf("expected cache size 1, got %d", s.Size())
	}
}

func TestSigner_CacheHitOnSecondLookup(t *testing.T) {
	s := newTestSigner(t, 4)
	a, _ := s.Cert("api.openai.com")
	b, _ := s.Cert("api.openai.com")
	if a != b {
		t.Fatal("expected cached pointer reuse on second lookup")
	}
}

func TestSigner_NormalisesHostBeforeLookup(t *testing.T) {
	s := newTestSigner(t, 4)
	a, _ := s.Cert("API.OpenAI.com")
	b, _ := s.Cert("api.openai.com.")
	c, _ := s.Cert("api.openai.com:443")
	if a != b || b != c {
		t.Fatal("upper-case / trailing-dot / port variants must hit the same cache entry")
	}
}

func TestSigner_LRUEvictionFiresAtCap(t *testing.T) {
	s := newTestSigner(t, 2)
	_, _ = s.Cert("a.example")
	_, _ = s.Cert("b.example")
	_, _ = s.Cert("c.example")
	if s.Size() != 2 {
		t.Fatalf("expected cap of 2, got %d", s.Size())
	}
	// `a.example` should have been evicted; re-requesting it issues a
	// fresh leaf.
	first, _ := s.Cert("a.example")
	again, _ := s.Cert("a.example")
	if first != again {
		t.Fatal("re-request after eviction should now be cached again")
	}
}

func TestSigner_CacheReplacementPreservesOrder(t *testing.T) {
	s := newTestSigner(t, 3)
	for _, h := range []string{"a", "b", "c"} {
		if _, err := s.Cert(h + ".example"); err != nil {
			t.Fatalf("seed %s: %v", h, err)
		}
	}
	// Touch `a` so it becomes most-recently-used.
	if _, err := s.Cert("a.example"); err != nil {
		t.Fatalf("touch a: %v", err)
	}
	// Add a fourth host; evict victim should be `b`.
	if _, err := s.Cert("d.example"); err != nil {
		t.Fatalf("d: %v", err)
	}
	if _, ok := s.cache["b.example"]; ok {
		t.Fatal("expected b.example to be evicted (LRU)")
	}
	if _, ok := s.cache["a.example"]; !ok {
		t.Fatal("expected a.example to remain (was touched)")
	}
}

func TestSigner_TTLRefreshOnExpiry(t *testing.T) {
	s := newTestSigner(t, 4)
	now := time.Now()
	clock := func() time.Time { return now }
	s.SetClock(clock)
	first, _ := s.Cert("api.openai.com")
	leafA, _ := x509.ParseCertificate(first.Certificate[0])
	// Move clock past the leaf's NotAfter.
	now = now.Add(LeafValidity + time.Hour)
	second, _ := s.Cert("api.openai.com")
	leafB, _ := x509.ParseCertificate(second.Certificate[0])
	if leafA.SerialNumber.Cmp(leafB.SerialNumber) == 0 {
		t.Fatal("expired leaf must have been re-signed with a new serial")
	}
}

func TestSigner_GetCertificateEmptySNI(t *testing.T) {
	s := newTestSigner(t, 2)
	if _, err := s.GetCertificate(&tls.ClientHelloInfo{ServerName: ""}); err == nil {
		t.Fatal("expected error on empty SNI")
	}
}

func TestSigner_CertEmptyHost(t *testing.T) {
	s := newTestSigner(t, 2)
	if _, err := s.Cert(""); err == nil {
		t.Fatal("expected error on empty host")
	}
}

func TestSigner_IPHostUsesIPSAN(t *testing.T) {
	s := newTestSigner(t, 2)
	cert, err := s.Cert("127.0.0.1")
	if err != nil {
		t.Fatalf("ip cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(leaf.IPAddresses) == 0 {
		t.Fatal("expected IP SAN, got DNS-only leaf")
	}
}

func TestSigner_ConcurrentAccessRaceClean(t *testing.T) {
	s := newTestSigner(t, 32)
	var wg sync.WaitGroup
	hosts := []string{"a.example", "b.example", "c.example", "d.example", "e.example", "f.example"}
	for i := range 64 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			host := hosts[idx%len(hosts)]
			if _, err := s.Cert(host); err != nil {
				t.Errorf("cert %s: %v", host, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestSigner_DefaultCacheSizeApplied(t *testing.T) {
	ca, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	s := NewSigner(ca, 0)
	if s.maxSize != DefaultCertCacheSize {
		t.Fatalf("expected default cache size %d, got %d", DefaultCertCacheSize, s.maxSize)
	}
}

func TestSigner_NegativeCacheSizeFallsBack(t *testing.T) {
	ca, err := LoadOrGenerateCA(t.TempDir())
	if err != nil {
		t.Fatalf("ca: %v", err)
	}
	s := NewSigner(ca, -1)
	if s.maxSize != DefaultCertCacheSize {
		t.Fatalf("negative cache size must fall back to default")
	}
}

func TestCanonicalHost_EdgeCases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"API.OpenAI.com", "api.openai.com"},
		{"api.openai.com.", "api.openai.com"},
		{"api.openai.com:443", "api.openai.com"},
		{"api.openai.com:abc", "api.openai.com:abc"}, // not a real port - left intact
		{"  api.openai.com  ", "api.openai.com"},
		{"", ""},
		{"[::1]:443", "[::1]:443"}, // IPv6 with brackets - not stripped
	}
	for _, tc := range cases {
		if got := canonicalHost(tc.in); got != tc.want {
			t.Errorf("canonicalHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParsePort_Validation(t *testing.T) {
	if _, err := parsePort(""); err == nil {
		t.Fatal("empty must error")
	}
	if _, err := parsePort("abc"); err == nil {
		t.Fatal("non-digit must error")
	}
	if _, err := parsePort("443"); err != nil {
		t.Fatalf("digit-only must succeed: %v", err)
	}
}
