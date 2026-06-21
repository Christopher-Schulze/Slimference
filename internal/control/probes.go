package control

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/apps"
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
	for line := range strings.SplitSeq(content, "\n") {
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
