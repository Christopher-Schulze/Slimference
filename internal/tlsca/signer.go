package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// LeafValidity governs how long an on-the-fly leaf certificate is
// valid. Twenty-four hours is short enough that a stolen leaf is
// useless beyond the day, long enough to absorb operator clock skew.
const LeafValidity = 24 * time.Hour

// DefaultCertCacheSize bounds the in-process LRU keyed by SNI host.
// 256 distinct hosts is plenty for the LLM-tooling intercept surface;
// if an operator routes a heavier mix of HTTPS through Slimference the
// config knob `[transparent] cert_cache_size` overrides this.
const DefaultCertCacheSize = 256

// Signer hands out per-host leaf certificates signed by the CA.
// Concurrent callers are safe: GetCertificate is the hot path,
// protected by an RW mutex with a bounded LRU cache.
type Signer struct {
	ca      *CA
	maxSize int
	now     func() time.Time
	mu      sync.Mutex
	cache   map[string]*cachedLeaf
	order   []string
}

type cachedLeaf struct {
	cert *tls.Certificate
	exp  time.Time
}

// NewSigner returns a Signer backed by the given CA. cacheSize <= 0
// uses DefaultCertCacheSize. The `now` field defaults to time.Now and
// is overridable in tests so leaf-cert TTL boundaries can be exercised
// without sleeping.
func NewSigner(ca *CA, cacheSize int) *Signer {
	if cacheSize <= 0 {
		cacheSize = DefaultCertCacheSize
	}
	return &Signer{
		ca:      ca,
		maxSize: cacheSize,
		now:     time.Now,
		cache:   make(map[string]*cachedLeaf, cacheSize),
		order:   make([]string, 0, cacheSize),
	}
}

// SetClock is the test hook for injecting a deterministic time source.
func (s *Signer) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// GetCertificate is the entry point used by `tls.Config.GetCertificate`.
// It returns the leaf for `chi.ServerName`, generating + caching one
// the first time the host is seen.
func (s *Signer) GetCertificate(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := canonicalHost(chi.ServerName)
	if host == "" {
		return nil, fmt.Errorf("tlsca: empty SNI host")
	}
	return s.Cert(host)
}

// Cert is the imperative-style accessor used by code that already has
// a host string (e.g. CONNECT-target parsing). It is the same as
// GetCertificate plus host normalisation.
func (s *Signer) Cert(host string) (*tls.Certificate, error) {
	host = canonicalHost(host)
	if host == "" {
		return nil, fmt.Errorf("tlsca: empty host")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.cache[host]; ok && s.now().Before(entry.exp) {
		s.touchLocked(host)
		return entry.cert, nil
	}
	leaf, exp, err := s.signLeafLocked(host)
	if err != nil {
		return nil, err
	}
	s.storeLocked(host, &cachedLeaf{cert: leaf, exp: exp})
	return leaf, nil
}

func (s *Signer) signLeafLocked(host string) (*tls.Certificate, time.Time, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), randSource)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("tlsca: leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, time.Time{}, err
	}
	notBefore := s.now().Add(-1 * time.Hour)
	notAfter := s.now().Add(LeafValidity)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(randSource, tmpl, s.ca.Cert, &priv.PublicKey, s.ca.PrivateKey)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("tlsca: sign leaf: %w", err)
	}
	leaf := &tls.Certificate{
		Certificate: [][]byte{der, s.ca.Cert.Raw},
		PrivateKey:  priv,
	}
	return leaf, notAfter, nil
}

func (s *Signer) storeLocked(host string, entry *cachedLeaf) {
	if _, exists := s.cache[host]; exists {
		s.cache[host] = entry
		s.touchLocked(host)
		return
	}
	if len(s.cache) >= s.maxSize {
		// Evict least-recently-used.
		evict := s.order[0]
		s.order = s.order[1:]
		delete(s.cache, evict)
	}
	s.cache[host] = entry
	s.order = append(s.order, host)
}

func (s *Signer) touchLocked(host string) {
	for i, h := range s.order {
		if h == host {
			s.order = append(s.order[:i], s.order[i+1:]...)
			s.order = append(s.order, host)
			return
		}
	}
}

// Size returns the current number of cached leaf certificates. Used by
// `slimference proxy status` for diagnostics.
func (s *Signer) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cache)
}

// canonicalHost lowercases and strips a trailing dot / port so the
// cache key is stable regardless of the format the caller used.
// IPv6 literals in bracketed form (`[::1]:443`) are returned with the
// brackets intact and the port intact; we have no real-world need to
// MITM IPv6 hosts and stripping the brackets would change the cache
// key, so we leave bracketed forms alone.
func canonicalHost(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	h = strings.TrimSuffix(h, ".")
	if strings.HasPrefix(h, "[") {
		return h
	}
	if i := strings.LastIndex(h, ":"); i >= 0 {
		if _, err := parsePort(h[i+1:]); err == nil {
			h = h[:i]
		}
	}
	return h
}

func parsePort(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-digit")
		}
	}
	return 0, nil
}
