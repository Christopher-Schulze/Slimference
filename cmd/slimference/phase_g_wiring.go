package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/slimference/slimference/internal/codexroute"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/control/apps"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/proxy/sniroute"
	"github.com/slimference/slimference/internal/proxy/transparent"
	"github.com/slimference/slimference/internal/tlsca"
)

// startProxyAppsManager is the last apps.Manager that startProxyFn (or
// startProxyForDaemon) wired into the live proxy. The headless caller
// inspects it to install a SIGHUP-reload handler. Reset on every
// startProxyFn call; reads/writes are single-threaded (only set in
// startup, only read in the same goroutine).
var startProxyAppsManager *apps.Manager

// startProxySNICancel cancels the SNI-peek engine started by
// startSNIPeekEngine. nil when the engine was not started (config
// disabled, or bind failed). Reset on every startProxyFn call.
var startProxySNICancel context.CancelFunc

var startSNIPeekEngineFn = startSNIPeekEngine

// startProxyConfig is the runtime config object owned by the active
// daemon/headless proxy. SIGHUP reload only touches the small mutable
// subset that is designed for live flips (apps policy + SNI-peek mode).
var startProxyConfig *config.Config

// startProxyInstance is the active proxy instance. SIGHUP needs it
// when transparent mode flips on so the late-started SNI engine can
// attach the WSS dispatcher to the same proxy.
var startProxyInstance *proxy.Proxy

// startProxyHostsCleanup reverts the /etc/hosts patch applied by the
// daemon at startup. nil when the patch was not applied (config
// disabled, or apply failed - fail-open). Reset on every startProxyFn
// call.
var startProxyHostsCleanup func()

// startProxyHostsArmed records whether the hosts patch actually
// applied. A non-nil cleanup function is not enough because
// applyHostsPatch returns a no-op cleanup for disabled/fail-open paths.
var startProxyHostsArmed bool

// startProxyPIDCleanup removes the daemon reload PID file written by
// startProxyForDaemon. Foreground/TUI starts deliberately leave it nil
// so they cannot steal the daemon's SIGHUP target.
var startProxyPIDCleanup func()

// phaseGAppsPath returns the canonical per-app policy TOML path. It
// deliberately follows the XDG config location instead of the legacy
// ~/.slimference state dir so app routing has the same "one config
// home" operator story as config.toml. Empty means HOME is not
// resolvable and no XDG override exists; the manager then runs
// in-memory with the default Codex-on / Claude-off policy.
func phaseGAppsPath() string {
	if p := os.Getenv("SLIMFERENCE_CONFIG"); p != "" {
		return filepath.Join(filepath.Dir(p), "apps.toml")
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "slimference", "apps.toml")
	}
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "slimference", "apps.toml")
}

// wirePhaseG attaches the Phase G accessors and probe set to a fresh
// Proxy. Safe to call multiple times - SetAppsManager / SetStateProvider
// replace any prior wiring. Returns the constructed manager so the
// caller can keep a reference for SIGHUP-reload and tests.
//
// All errors are non-fatal: callers that cannot construct a manager
// (e.g. corrupt TOML) get nil back and log the cause. The proxy
// remains usable in legacy "all apps enabled" mode.
func wirePhaseG(p *proxy.Proxy, cfg *config.Config) *apps.Manager {
	if p == nil {
		return nil
	}
	path := phaseGAppsPath()
	m, err := apps.NewManager(path)
	if err != nil {
		slog.Warn("phase_g: apps manager init failed - falling back to legacy mode",
			"path", path, "err", err)
		return nil
	}
	p.SetAppsManager(m)
	probes := buildProbes(p, m, cfg)
	p.SetStateProvider(probes)
	return m
}

// buildProbes assembles the control.Probes set used by /admin/state.
// Probe-set construction is intentionally cheap: the actual filesystem
// / port / process probes execute only when the admin endpoint is hit.
//
// Any probe slot may be nil; control.Build treats nil as "unknown" and
// emits the zero value. Tests inject custom probes; production passes
// the real ones.
func buildProbes(p *proxy.Proxy, m *apps.Manager, cfg *config.Config) *control.Probes {
	if p == nil {
		return &control.Probes{}
	}
	home, _ := osUserHomeDir()
	dataDir := ""
	if home != "" {
		dataDir = filepath.Join(home, ".slimference")
	}

	probes := &control.Probes{
		CA:           &control.FileCAProbe{Dir: dataDir},
		Daemon:       proxy.DaemonProbe{Proxy: p},
		Listener:     &control.PortListenerProbe{Port443: 443, Port8990: cfg.Proxy.ListenPort, PortSNIPeek: cfg.Transparent.SNIPeekPort},
		NetworkRedir: &control.HostsFileNetworkProbe{},
		Apps:         &control.AppsManagerProbe{Manager: m, Counters: phaseGCounters(p)},
		CodexRoute: &codexRouteProbe{
			home:               home,
			proxyURL:           codexroute.ProxyURL("127.0.0.1", fmt.Sprintf("%d", cfg.Proxy.ListenPort)),
			codexVersionFn:     codexVersionFn,
			slimferenceVersion: version,
			healthFn:           codexRouteHealthFn,
			port:               fmt.Sprintf("%d", cfg.Proxy.ListenPort),
		},
		Savings: &proxy.SavingsProbe{
			Proxy:               p,
			USDPerMillionTokens: cfg.Analytics.GainUSDPerMillionTokens,
		},
		Indist: proxy.NoopIndistProbe{},
		WSS:    proxy.WSSProbe{Proxy: p},
	}
	return probes
}

// phaseGCounters returns nil today because per-app routing counters
// are wired in Phase C. The probe ABI accepts nil and omits the
// routed/bypassed fields when so.
func phaseGCounters(p *proxy.Proxy) control.AppCounters {
	_ = p
	return nil
}

// reloadAppsManager is invoked from the SIGHUP handler. It re-reads
// apps.toml and propagates the new Policy to every cached reference.
// Errors are logged but never propagated - a malformed file should
// not crash the daemon mid-flight.
func reloadAppsManager(m *apps.Manager) {
	if m == nil {
		return
	}
	if _, err := m.Reload(); err != nil {
		slog.Warn("phase_g: apps reload failed", "err", err)
		return
	}
	slog.Info("phase_g: apps policy reloaded")
}

// startSNIPeekEngine constructs and runs a transparent.Engine listening
// on cfg.Transparent.SNIPeekPort. Returns the running engine + cancel
// func so the caller can shut it down on signal. Returns (nil, nil)
// when SNIPeekMode is disabled or the listen socket cannot be bound.
//
// Errors are logged but not fatal: missing port (e.g. EACCES on 443)
// degrades the proxy to legacy CONNECT-mode without taking the daemon
// down.
func startSNIPeekEngine(p *proxy.Proxy, cfg *config.Config, m *apps.Manager) (*transparent.Engine, context.CancelFunc) {
	if cfg == nil || !cfg.Transparent.SNIPeekMode || cfg.Transparent.SNIPeekPort == 0 {
		return nil, nil
	}
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Transparent.SNIPeekPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("sni_peek: bind failed - SNI-peek mode disabled this run",
			"addr", addr, "err", err)
		return nil, nil
	}

	// CA Signer for leaf certificates.
	home, _ := osUserHomeDir()
	caDir := filepath.Join(home, ".slimference")
	ca, err := tlsca.LoadOrGenerateCA(caDir)
	if err != nil {
		slog.Warn("sni_peek: CA load failed - SNI-peek mode disabled",
			"ca_dir", caDir, "err", err)
		_ = ln.Close()
		return nil, nil
	}
	signer := tlsca.NewSigner(ca, cfg.Transparent.CertCacheSize)

	resolver := sniroute.New(m)
	dispatcher := p.WSSDispatcher()
	if dispatcher == nil {
		dispatcher = &proxy.PhaseFDispatcher{}
	}
	dispatcher.Proxy = p
	dispatcher.UpstreamDial = proxy.DefaultUpstreamDial()
	dispatcher.Resolver = resolver
	p.SetWSSDispatcher(dispatcher)
	engine := &transparent.Engine{
		Listener:   ln,
		Resolver:   resolver,
		Certs:      signer,
		Dispatcher: dispatcher,
		OnError: func(err error) {
			slog.Warn("sni_peek: engine error", "err", err)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go runSNIPeekEngine(ctx, engine, addr)
	return engine, cancel
}

func runSNIPeekEngine(ctx context.Context, engine *transparent.Engine, addr string) {
	slog.Info("sni_peek: engine listening", "addr", addr)
	if err := engine.Run(ctx); err != nil {
		slog.Warn("sni_peek: engine stopped", "err", err)
	}
}

// ensureSlimDataDir creates ~/.slimference if missing. Called before
// the apps manager so its first SetEnabled write does not fail. Errors
// are reported but not fatal: the manager works in-memory if the
// directory cannot be created.
func ensureSlimDataDir() string {
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".slimference")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("phase_g: mkdir slimference data dir failed", "dir", dir, "err", err)
	}
	return dir
}
