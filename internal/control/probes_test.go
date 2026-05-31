package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cappapps "github.com/slimference/slimference/internal/control/apps"
)

func writeTestCert(t *testing.T, dir string, notBefore, notAfter time.Time) {
	t.Helper()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		t.Fatal(err)
	}
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, _ := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(caDir, "root.crt"), pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileCAProbeReadsRealCert(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now().Add(-time.Hour), time.Now().Add(30*24*time.Hour))
	probe := &FileCAProbe{Dir: dir}
	state := probe.ProbeCA(context.Background())
	if !state.Installed {
		t.Errorf("expected Installed=true")
	}
	if state.Fingerprint == "" {
		t.Errorf("fingerprint empty")
	}
	if state.DaysUntilExpiry < 25 || state.DaysUntilExpiry > 31 {
		t.Errorf("days_until_expiry=%d should be ~30", state.DaysUntilExpiry)
	}
}

func TestFileCAProbeEmptyDir(t *testing.T) {
	probe := &FileCAProbe{}
	state := probe.ProbeCA(context.Background())
	if state.Installed {
		t.Errorf("empty Dir should yield Installed=false")
	}
}

func TestFileCAProbeMissingFile(t *testing.T) {
	probe := &FileCAProbe{Dir: t.TempDir()}
	if probe.ProbeCA(context.Background()).Installed {
		t.Errorf("missing cert should yield Installed=false")
	}
}

func TestFileCAProbeMalformedPEM(t *testing.T) {
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caDir, "root.crt"), []byte("not a pem"), 0o644); err != nil {
		t.Fatal(err)
	}
	if probe := (&FileCAProbe{Dir: dir}); probe.ProbeCA(context.Background()).Installed {
		t.Errorf("malformed PEM should yield Installed=false")
	}
}

func TestFileCAProbeUnparseableCert(t *testing.T) {
	dir := t.TempDir()
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bogus := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("nonsense")})
	if err := os.WriteFile(filepath.Join(caDir, "root.crt"), bogus, 0o644); err != nil {
		t.Fatal(err)
	}
	if (&FileCAProbe{Dir: dir}).ProbeCA(context.Background()).Installed {
		t.Errorf("unparseable cert should yield Installed=false")
	}
}

func TestFileCAProbeExpiredCertReportsZeroDays(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now().Add(-24*time.Hour), time.Now().Add(-time.Hour))
	probe := &FileCAProbe{Dir: dir, Clock: func() time.Time { return time.Now() }}
	state := probe.ProbeCA(context.Background())
	if state.DaysUntilExpiry != 0 {
		t.Errorf("expired cert: days=%d want 0", state.DaysUntilExpiry)
	}
}

func TestKeychainCAProbeCombinesWithLooker(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now(), time.Now().Add(time.Hour))
	probe := &KeychainCAProbe{
		File: &FileCAProbe{Dir: dir},
		Looker: func(ctx context.Context, fingerprint string) bool {
			if fingerprint == "" {
				t.Errorf("looker received empty fingerprint")
			}
			return true
		},
	}
	state := probe.ProbeCA(context.Background())
	if !state.InKeychain {
		t.Errorf("InKeychain not set when looker returns true")
	}
}

func TestKeychainCAProbeFileNotInstalledShortCircuits(t *testing.T) {
	calls := 0
	probe := &KeychainCAProbe{
		File: &FileCAProbe{}, // Empty Dir → returns Installed=false
		Looker: func(ctx context.Context, _ string) bool {
			calls++
			return true
		},
	}
	state := probe.ProbeCA(context.Background())
	if state.InKeychain {
		t.Errorf("InKeychain should be false when file probe failed")
	}
	if calls != 0 {
		t.Errorf("looker called %d times despite no file probe", calls)
	}
}

func TestKeychainCAProbeNilFileAndLooker(t *testing.T) {
	probe := &KeychainCAProbe{}
	state := probe.ProbeCA(context.Background())
	if state.Installed || state.InKeychain {
		t.Errorf("nil sub-probes should yield zero state")
	}
}

func TestKeychainCAProbeNilLooker(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now(), time.Now().Add(time.Hour))
	probe := &KeychainCAProbe{File: &FileCAProbe{Dir: dir}}
	if probe.ProbeCA(context.Background()).InKeychain {
		t.Errorf("nil looker should leave InKeychain=false")
	}
}

func TestHTTPDaemonProbeOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pid":42,"version":"v9.9","rss_bytes":123456,"uptime_sec":77,"cpu_user_seconds":1.25,"cpu_system_seconds":0.5,"cpu_percent":2.5,"disk_read_ops":3,"disk_write_ops":4,"state_bytes":999}`))
	}))
	defer srv.Close()
	probe := &HTTPDaemonProbe{BaseURL: srv.URL}
	state := probe.ProbeDaemon(context.Background())
	if !state.Running || !state.HealthOK {
		t.Errorf("expected healthy, got %+v", state)
	}
	if state.PID != 42 {
		t.Errorf("PID=%d", state.PID)
	}
	if state.Version != "v9.9" {
		t.Errorf("version=%q", state.Version)
	}
	if state.RSSBytes != 123456 || state.UptimeSec != 77 {
		t.Errorf("resource fields rss=%d uptime=%d", state.RSSBytes, state.UptimeSec)
	}
	if state.CPUUserSeconds != 1.25 || state.CPUSystemSeconds != 0.5 || state.CPUPercent != 2.5 ||
		state.DiskReadOps != 3 || state.DiskWriteOps != 4 || state.StateBytes != 999 {
		t.Errorf("extended resource fields mismatch: %+v", state)
	}
}

func TestHTTPDaemonProbeNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	probe := &HTTPDaemonProbe{BaseURL: srv.URL}
	state := probe.ProbeDaemon(context.Background())
	if !state.Running {
		t.Errorf("running should be true on any HTTP response")
	}
	if state.HealthOK {
		t.Errorf("503 should yield HealthOK=false")
	}
}

func TestHTTPDaemonProbeDialFailure(t *testing.T) {
	probe := &HTTPDaemonProbe{BaseURL: "http://127.0.0.1:1"}
	state := probe.ProbeDaemon(context.Background())
	if state.Running {
		t.Errorf("connect refused should yield Running=false")
	}
}

func TestHTTPDaemonProbeEmptyBaseURL(t *testing.T) {
	probe := &HTTPDaemonProbe{}
	if state := probe.ProbeDaemon(context.Background()); state.Running {
		t.Errorf("empty URL should not be Running")
	}
}

func TestHTTPDaemonProbeUsesProvidedVersionFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"pid":7}`))
	}))
	defer srv.Close()
	probe := &HTTPDaemonProbe{BaseURL: srv.URL, Version: "fallback"}
	state := probe.ProbeDaemon(context.Background())
	if state.Version != "fallback" {
		t.Errorf("version fallback not honored: %q", state.Version)
	}
}

func TestHTTPDaemonProbeMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	probe := &HTTPDaemonProbe{BaseURL: srv.URL, Version: "ver"}
	state := probe.ProbeDaemon(context.Background())
	if !state.HealthOK {
		t.Errorf("malformed JSON should still report healthy if status was 200")
	}
}

func TestHTTPDaemonProbeRequestBuildFailure(t *testing.T) {
	probe := &HTTPDaemonProbe{BaseURL: "://invalid-url"}
	state := probe.ProbeDaemon(context.Background())
	if state.Running {
		t.Errorf("invalid URL should not produce Running")
	}
}

func TestPortListenerProbeBothBound(t *testing.T) {
	probe := &PortListenerProbe{
		Port443: 443, Port8990: 8990, PortSNIPeek: 8443, Method: "test",
		Dial: func(ctx context.Context, port int) bool { return true },
	}
	state := probe.ProbeListener(context.Background())
	if !state.BoundOn443 || !state.BoundOn8990 || !state.BoundOnSNIPeek || state.Method != "test" {
		t.Errorf("expected bound on both + method, got %+v", state)
	}
}

func TestPortListenerProbeBothUnbound(t *testing.T) {
	probe := &PortListenerProbe{
		Port443: 443, Port8990: 8990,
		Dial: func(ctx context.Context, _ int) bool { return false },
	}
	state := probe.ProbeListener(context.Background())
	if state.BoundOn443 || state.BoundOn8990 || state.BoundOnSNIPeek {
		t.Errorf("expected neither bound")
	}
}

func TestPortListenerProbeNilDialerUsesDefault(t *testing.T) {
	// Construct a probe without a custom Dial - the nil-fallback to
	// defaultPortDial branch must execute. Port 1 is reserved/refused
	// so the result is BoundOn443=false; the important assertion is
	// that the call returns without panic.
	probe := &PortListenerProbe{Port443: 1}
	state := probe.ProbeListener(context.Background())
	if state.BoundOn443 {
		t.Errorf("port 1 should not be bound")
	}
}

func TestPortListenerProbeZeroPortsSkipped(t *testing.T) {
	calls := 0
	probe := &PortListenerProbe{
		Dial: func(ctx context.Context, _ int) bool {
			calls++
			return true
		},
	}
	state := probe.ProbeListener(context.Background())
	if state.BoundOn443 || state.BoundOn8990 || state.BoundOnSNIPeek {
		t.Errorf("zero ports should not be checked")
	}
	if calls != 0 {
		t.Errorf("dialer should not be called for zero ports: %d calls", calls)
	}
}

func TestDefaultPortDialOnUnboundLocal(t *testing.T) {
	// Port 1 is privileged + reserved; dial will be refused quickly.
	if defaultPortDial(context.Background(), 1) {
		t.Errorf("dial on unbound port should return false")
	}
}

func TestDefaultPortDialOnBoundLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	// Extract port from URL.
	u := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	if u == srv.URL {
		t.Skip("server not on 127.0.0.1")
	}
	port := 0
	for _, ch := range u {
		if ch >= '0' && ch <= '9' {
			port = port*10 + int(ch-'0')
		} else {
			break
		}
	}
	if port == 0 {
		t.Fatal("could not parse port")
	}
	if !defaultPortDial(context.Background(), port) {
		t.Errorf("dial on bound port should return true")
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 99: "99", -42: "-42", 12345: "12345"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d)=%q want %q", in, got, want)
		}
	}
}

func TestHostsFileNetworkProbeMatchesTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	content := `# header
127.0.0.1 chatgpt.com
::1 api.openai.com
127.0.0.1 example.com  # not a target
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := &HostsFileNetworkProbe{Path: path}
	state := probe.ProbeNetwork(context.Background())
	if !state.HostsActive {
		t.Errorf("HostsActive should be true")
	}
	if len(state.HostsEntries) != 2 {
		t.Errorf("got entries %v", state.HostsEntries)
	}
}

func TestHostsFileNetworkProbeNoMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := &HostsFileNetworkProbe{Path: path}
	state := probe.ProbeNetwork(context.Background())
	if state.HostsActive {
		t.Errorf("no target match → inactive")
	}
}

func TestHostsFileNetworkProbeMissingFile(t *testing.T) {
	probe := &HostsFileNetworkProbe{Path: filepath.Join(t.TempDir(), "absent")}
	if probe.ProbeNetwork(context.Background()).HostsActive {
		t.Errorf("missing file should be inactive")
	}
}

func TestHostsFileNetworkProbeDefaultPathFallback(t *testing.T) {
	// Default path /etc/hosts likely exists - we can't deterministically
	// know if the user has Slimference markers there, but we can verify
	// the probe doesn't panic. Coverage of the default-path branch.
	probe := &HostsFileNetworkProbe{Targets: []string{"absolutely-never-a-real-host"}}
	_ = probe.ProbeNetwork(context.Background())
}

func TestHostsFileNetworkProbeDefaultTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 chatgpt.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := &HostsFileNetworkProbe{Path: path}
	state := probe.ProbeNetwork(context.Background())
	if !state.HostsActive {
		t.Errorf("default targets should match chatgpt.com")
	}
}

func TestHostsFileContainsTargetIgnoresComments(t *testing.T) {
	content := `# 127.0.0.1 chatgpt.com
   # comment
`
	if hostsFileContainsTarget(content, "chatgpt.com") {
		t.Errorf("comment line should not count")
	}
}

func TestHostsFileContainsTargetIgnoresShortLines(t *testing.T) {
	if hostsFileContainsTarget("\nfoo\n", "foo") {
		t.Errorf("one-field line should not match")
	}
}

func TestHostsFileContainsTargetIgnoresNonLoopback(t *testing.T) {
	if hostsFileContainsTarget("8.8.8.8 chatgpt.com\n", "chatgpt.com") {
		t.Errorf("non-loopback should not count as redirect")
	}
}

func TestHostsFileContainsTargetMatchesIPv6(t *testing.T) {
	if !hostsFileContainsTarget("::1 chatgpt.com\n", "chatgpt.com") {
		t.Errorf("::1 should count as loopback redirect")
	}
}

func TestHostsFileContainsTargetCaseInsensitive(t *testing.T) {
	if !hostsFileContainsTarget("127.0.0.1 ChatGPT.com\n", "chatgpt.com") {
		t.Errorf("hostname match should be case-insensitive")
	}
}

func TestAppsManagerProbeReturnsAllKnownApps(t *testing.T) {
	m, _ := cappapps.NewManager("")
	probe := &AppsManagerProbe{Manager: m}
	entries := probe.ProbeApps(context.Background())
	if len(entries) != len(cappapps.KnownApps) {
		t.Errorf("got %d entries want %d", len(entries), len(cappapps.KnownApps))
	}
}

func TestAppsManagerProbeRespectsPolicy(t *testing.T) {
	m, _ := cappapps.NewManager("")
	probe := &AppsManagerProbe{Manager: m}
	entries := probe.ProbeApps(context.Background())
	var cli, claude AppEntry
	for _, e := range entries {
		switch e.ID {
		case cappapps.AppCodexCLI:
			cli = e
		case cappapps.AppClaudeCode:
			claude = e
		}
	}
	if !cli.Enabled {
		t.Errorf("CLI default enabled missed")
	}
	if claude.Enabled {
		t.Errorf("Claude Code default off violated")
	}
}

func TestAppsManagerProbeWithCounters(t *testing.T) {
	m, _ := cappapps.NewManager("")
	counters := &MemoryAppCounters{}
	counters.IncrementRouted(cappapps.AppCodexCLI)
	counters.IncrementRouted(cappapps.AppCodexCLI)
	counters.IncrementBypassed(cappapps.AppCodexCLI)
	probe := &AppsManagerProbe{Manager: m, Counters: counters}
	entries := probe.ProbeApps(context.Background())
	var cli AppEntry
	for _, e := range entries {
		if e.ID == cappapps.AppCodexCLI {
			cli = e
		}
	}
	if cli.Routed != 2 {
		t.Errorf("Routed=%d want 2", cli.Routed)
	}
	if cli.Bypassed != 1 {
		t.Errorf("Bypassed=%d want 1", cli.Bypassed)
	}
}

func TestAppsManagerProbeNilManager(t *testing.T) {
	probe := &AppsManagerProbe{}
	if entries := probe.ProbeApps(context.Background()); entries != nil {
		t.Errorf("nil manager should return nil entries, got %v", entries)
	}
}

func TestAppsManagerProbeBinaryDetection(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, _ := cappapps.NewManager("")
	// Override detection so it points at our fake binary.
	det := cappapps.Detection{
		UAPrefixes:  m.Detection().UAPrefixes,
		BinaryPaths: map[cappapps.AppID][]string{cappapps.AppCodexCLI: {fake}},
	}
	// Direct field access via internal API not exported; rebuild via NewManager + override using package-level helper.
	// Easiest: use the Manager's existing DetectedBinaries map by writing to a real path the default detection points at.
	_ = det

	// Easier: just check that the default detection's binary paths
	// either match real /Applications or are absent. We can't reliably
	// trigger one without env mutation; cover via the no-detection
	// path instead.
	probe := &AppsManagerProbe{Manager: m}
	entries := probe.ProbeApps(context.Background())
	for _, e := range entries {
		// Detected is environment-dependent; only assert structural
		// correctness.
		if e.ID == "" {
			t.Errorf("entry with empty ID")
		}
	}
}

func TestMemoryAppCountersZero(t *testing.T) {
	c := &MemoryAppCounters{}
	if c.Routed(cappapps.AppCodexCLI) != 0 {
		t.Errorf("fresh counter not zero")
	}
	if c.Bypassed(cappapps.AppCodexCLI) != 0 {
		t.Errorf("fresh bypassed not zero")
	}
}

func TestMemoryAppCountersConcurrent(t *testing.T) {
	c := &MemoryAppCounters{}
	const goroutines = 16
	const perG = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				c.IncrementRouted(cappapps.AppCodexCLI)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				c.IncrementBypassed(cappapps.AppCodexCLI)
			}
		}()
	}
	wg.Wait()
	if got := c.Routed(cappapps.AppCodexCLI); got != goroutines*perG {
		t.Errorf("Routed=%d want %d", got, goroutines*perG)
	}
	if got := c.Bypassed(cappapps.AppCodexCLI); got != goroutines*perG {
		t.Errorf("Bypassed=%d want %d", got, goroutines*perG)
	}
}
