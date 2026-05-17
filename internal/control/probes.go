package control

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/control/apps"
)

// FileCAProbe implements CAProbe by reading the on-disk CA cert at
// `<Dir>/ca/root.crt`. It does NOT consult the system keychain - that
// check is in KeychainCAProbe, which combines with FileCAProbe via
// ComposedCAProbe.
type FileCAProbe struct {
	Dir   string
	Clock func() time.Time
}

// ProbeCA implements CAProbe.
func (p *FileCAProbe) ProbeCA(ctx context.Context) CAState {
	state := CAState{}
	if p.Dir == "" {
		return state
	}
	certPath := filepath.Join(p.Dir, "ca", "root.crt")
	data, err := os.ReadFile(certPath)
	if err != nil {
		return state
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return state
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return state
	}
	state.Installed = true
	state.NotBefore = cert.NotBefore
	state.NotAfter = cert.NotAfter
	sum := sha256.Sum256(cert.Raw)
	state.Fingerprint = hex.EncodeToString(sum[:])

	now := time.Now
	if p.Clock != nil {
		now = p.Clock
	}
	if days := int(cert.NotAfter.Sub(now()).Hours() / 24); days > 0 {
		state.DaysUntilExpiry = days
	}
	return state
}

// KeychainCAProbe wraps FileCAProbe and additionally consults the
// macOS Keychain via `security find-certificate`. To remain testable
// without touching the real Keychain, the probe accepts a Looker
// function that returns true when the named cert is currently trusted.
type KeychainCAProbe struct {
	File   *FileCAProbe
	Looker func(ctx context.Context, fingerprint string) bool
}

// ProbeCA implements CAProbe.
func (p *KeychainCAProbe) ProbeCA(ctx context.Context) CAState {
	state := CAState{}
	if p.File != nil {
		state = p.File.ProbeCA(ctx)
	}
	if !state.Installed || p.Looker == nil {
		return state
	}
	state.InKeychain = p.Looker(ctx, state.Fingerprint)
	return state
}

// HTTPDaemonProbe hits the daemon's `/admin/health` endpoint and
// reports back. Quick TCP-level fallback if the HTTP probe fails:
// presence on the listen port still indicates "something is bound,
// just not healthy".
type HTTPDaemonProbe struct {
	BaseURL string // e.g. "http://127.0.0.1:8990"
	Client  *http.Client
	Version string // populated by the daemon at build time
}

// ProbeDaemon implements DaemonProbe.
func (p *HTTPDaemonProbe) ProbeDaemon(ctx context.Context) DaemonState {
	state := DaemonState{Version: p.Version}
	if p.BaseURL == "" {
		return state
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 500 * time.Millisecond}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/admin/health", nil)
	if err != nil {
		return state
	}
	resp, err := client.Do(req)
	if err != nil {
		return state
	}
	defer resp.Body.Close()
	state.Running = true
	state.HealthOK = resp.StatusCode == http.StatusOK
	var body struct {
		PID     int    `json:"pid"`
		Version string `json:"version"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	state.PID = body.PID
	if body.Version != "" {
		state.Version = body.Version
	}
	return state
}

// PortListenerProbe checks which ports the daemon currently has bound.
// Net.Dial against 127.0.0.1:<port> with a 50 ms timeout - a refused
// connection means "no listener"; a successful TCP handshake means
// "something is listening" (it may not be ours, but in the locally-
// scoped slimference world that's specific enough).
type PortListenerProbe struct {
	Port443     int // privileged direct bind, usually 443
	Port8990    int // admin/proxy HTTP port, usually 8990
	PortSNIPeek int // unprivileged transparent listener, usually 8443
	Method      string
	Dial        func(ctx context.Context, port int) bool
}

// ProbeListener implements ListenerProbe.
func (p *PortListenerProbe) ProbeListener(ctx context.Context) ListenerState {
	state := ListenerState{Method: p.Method}
	dial := p.Dial
	if dial == nil {
		dial = defaultPortDial
	}
	if p.Port443 != 0 {
		state.BoundOn443 = dial(ctx, p.Port443)
	}
	if p.Port8990 != 0 {
		state.BoundOn8990 = dial(ctx, p.Port8990)
	}
	if p.PortSNIPeek != 0 {
		state.BoundOnSNIPeek = dial(ctx, p.PortSNIPeek)
	}
	return state
}

func defaultPortDial(ctx context.Context, port int) bool {
	d := net.Dialer{Timeout: 50 * time.Millisecond}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func itoa(i int) string {
	// inline strconv.Itoa to avoid an import for this single use
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// HostsFileNetworkProbe reads the hosts file and reports which target
// hostnames currently resolve to 127.0.0.1 via that file.
type HostsFileNetworkProbe struct {
	Path    string // defaults to /etc/hosts
	Targets []string
}

// ProbeNetwork implements NetworkProbe.
func (p *HostsFileNetworkProbe) ProbeNetwork(ctx context.Context) NetworkState {
	state := NetworkState{}
	path := p.Path
	if path == "" {
		path = "/etc/hosts"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	want := p.Targets
	if len(want) == 0 {
		want = []string{"chatgpt.com", "api.openai.com"}
	}
	found := make([]string, 0, len(want))
	for _, t := range want {
		if hostsFileContainsTarget(string(data), t) {
			found = append(found, t)
		}
	}
	state.HostsEntries = found
	state.HostsActive = len(found) > 0
	return state
}

// hostsFileContainsTarget reports whether the hosts file actively
// redirects `target` to a loopback address. Comments and lines that
// don't reference `target` are ignored.
func hostsFileContainsTarget(content, target string) bool {
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) < 2 {
			continue
		}
		if !strings.HasPrefix(fields[0], "127.") && fields[0] != "::1" {
			continue
		}
		for _, host := range fields[1:] {
			if strings.EqualFold(host, target) {
				return true
			}
		}
	}
	return false
}

// AppsManagerProbe wires the apps.Manager into the SetupState
// snapshot. It returns one AppEntry per known app with its current
// Enabled flag and Detected flag (binary on disk).
type AppsManagerProbe struct {
	Manager *apps.Manager
	// Routed / Bypassed counters are sourced from a per-app counter
	// map. Optional: leave nil to omit the counts.
	Counters AppCounters
}

// AppCounters is the shape implementations must satisfy when they
// want their routed/bypassed counts surfaced in AppEntry.
type AppCounters interface {
	Routed(id apps.AppID) int64
	Bypassed(id apps.AppID) int64
}

// ProbeApps implements AppsProbe.
func (p *AppsManagerProbe) ProbeApps(ctx context.Context) []AppEntry {
	if p.Manager == nil {
		return nil
	}
	pol := p.Manager.Policy()
	detected := p.Manager.DetectedBinaries()
	entries := make([]AppEntry, 0, len(apps.KnownApps))
	for _, id := range apps.KnownApps {
		entry := AppEntry{ID: id, Enabled: pol.IsEnabled(id)}
		if paths, ok := detected[id]; ok && len(paths) > 0 {
			entry.Detected = true
			entry.BinPath = paths[0]
		}
		if p.Counters != nil {
			entry.Routed = p.Counters.Routed(id)
			entry.Bypassed = p.Counters.Bypassed(id)
		}
		entries = append(entries, entry)
	}
	return entries
}

// MemoryAppCounters is an atomic in-memory implementation of
// AppCounters. The proxy increments it on every routed / bypassed
// connection.
type MemoryAppCounters struct {
	routed   sync.Map // map[apps.AppID]*atomic.Int64
	bypassed sync.Map
}

// IncrementRouted bumps the routed counter for `id` by 1.
func (c *MemoryAppCounters) IncrementRouted(id apps.AppID) { c.bump(&c.routed, id) }

// IncrementBypassed bumps the bypassed counter for `id` by 1.
func (c *MemoryAppCounters) IncrementBypassed(id apps.AppID) { c.bump(&c.bypassed, id) }

// Routed implements AppCounters.
func (c *MemoryAppCounters) Routed(id apps.AppID) int64 { return c.load(&c.routed, id) }

// Bypassed implements AppCounters.
func (c *MemoryAppCounters) Bypassed(id apps.AppID) int64 { return c.load(&c.bypassed, id) }

func (c *MemoryAppCounters) bump(m *sync.Map, id apps.AppID) {
	v, _ := m.LoadOrStore(id, &atomic.Int64{})
	v.(*atomic.Int64).Add(1)
}

func (c *MemoryAppCounters) load(m *sync.Map, id apps.AppID) int64 {
	v, ok := m.Load(id)
	if !ok {
		return 0
	}
	return v.(*atomic.Int64).Load()
}
