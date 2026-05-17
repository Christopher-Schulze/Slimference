package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/install"
)

// regexpMustCompile / regexpQuoteMeta are tiny wrappers so the
// helper functions below don't drag "regexp" into every test file
// that imports cmd/slimference. They are package-private.
var regexpMustCompile = regexp.MustCompile
var regexpQuoteMeta = regexp.QuoteMeta
var installHostsPlanFn = install.HostsPlan

// applyHostsPatch runs the install.HostsPlan() Apply step against the
// real /etc/hosts file. Returns a cleanup function that reverses the
// patch (idempotent — safe to call multiple times).
//
// Errors are LOGGED, not propagated. Hosts-patch failure must NOT
// crash the daemon: the user explicitly mandated that "daemon up but
// hosts not patched" degrades gracefully to plain Codex traffic, never
// to breakage.
func applyHostsPatch(cfg *config.Config) func() {
	startProxyHostsArmed = false
	if cfg == nil || !cfg.Transparent.SNIPeekMode {
		return func() {}
	}
	plan, err := installHostsPlanFn(install.HostsOptions{})
	if err != nil {
		slog.Warn("hosts_lifecycle: build plan failed", "err", err)
		return func() {}
	}
	if res := plan.Apply(context.Background()); res.Err != nil {
		slog.Warn("hosts_lifecycle: apply failed - transparent mode degraded",
			"err", res.Err)
		return func() {}
	}
	startProxyHostsArmed = true
	slog.Info("hosts_lifecycle: patched /etc/hosts")
	cleaned := false
	var mu sync.Mutex
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if cleaned {
			return
		}
		cleaned = true
		if res := plan.Reverse(context.Background()); res.Err() != nil {
			slog.Warn("hosts_lifecycle: reverse failed - /etc/hosts may be dirty",
				"err", res.Err())
			return
		}
		startProxyHostsArmed = false
		slog.Info("hosts_lifecycle: reverted /etc/hosts")
	}
}

// reloadSNIPeekModeFromDisk re-reads the on-disk config.toml to pick
// up flips to cfg.Transparent.SNIPeekMode + SNIPeekPort. If the value
// changed, applies/reverts the hosts patch accordingly. Other config
// fields are NOT touched (safer to require process restart for
// arbitrary changes).
//
// Effects:
//   - SNIPeekMode flipped ON  → applyHostsPatch + start engine
//   - SNIPeekMode flipped OFF → revert hosts + stop engine
//   - No change              → no-op
func reloadSNIPeekModeFromDisk(cfg *config.Config) {
	if cfg == nil {
		return
	}
	// Use the loader's resolution chain so SIGHUP reads from the
	// SAME file `slimference enable` wrote to.
	info := config.ResolveConfigPath(config.LoadOptions{})
	cfgPath := info.ResolvedPath
	if cfgPath == "" {
		slog.Debug("sighup: no config file found; keeping current transparent state")
		return
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		slog.Warn("sighup: read config failed", "err", err)
		return
	}
	wantOn := snippetReadBool(data, "sni_peek_mode")
	if wantOn == nil {
		slog.Debug("sighup: sni_peek_mode not present in config; ignoring")
		return
	}
	currentlyArmed := startProxyHostsCleanupArmed()

	switch {
	case *wantOn && !currentlyArmed:
		slog.Info("sighup: arming transparent mode (sni_peek_mode → true)")
		cfg.Transparent.SNIPeekMode = true
		startProxyHostsCleanup = applyHostsPatch(cfg)
		if startProxySNICancel == nil && startProxyInstance != nil {
			_, startProxySNICancel = startSNIPeekEngineFn(startProxyInstance, cfg, startProxyAppsManager)
		}
	case !*wantOn && currentlyArmed:
		slog.Info("sighup: disarming transparent mode (sni_peek_mode → false)")
		cfg.Transparent.SNIPeekMode = false
		if startProxyHostsCleanup != nil {
			startProxyHostsCleanup()
			startProxyHostsCleanup = nil
		}
		startProxyHostsArmed = false
		if startProxySNICancel != nil {
			startProxySNICancel()
			startProxySNICancel = nil
		}
	default:
		cfg.Transparent.SNIPeekMode = *wantOn
		if !*wantOn && startProxySNICancel != nil {
			startProxySNICancel()
			startProxySNICancel = nil
		}
		slog.Debug("sighup: sni_peek_mode unchanged")
	}
}

// startProxyHostsCleanupArmed reports whether the hosts patch actually
// applied. A non-nil cleanup function alone is not enough: disabled
// and fail-open paths deliberately install no-op cleanups.
func startProxyHostsCleanupArmed() bool {
	return startProxyHostsArmed
}

// snippetReadBool extracts the boolean value of `key` from a TOML
// blob using a regex. Returns nil when the key is absent. Limitation:
// only finds the FIRST occurrence; assumes the key is in the
// [transparent] table since that's the only place we set it.
func snippetReadBool(data []byte, key string) *bool {
	re := regexpMustCompile(`(?m)^\s*` + regexpQuoteMeta(key) + `\s*=\s*(true|false)\s*(?:#.*)?$`)
	m := re.FindSubmatch(data)
	if m == nil {
		return nil
	}
	result := string(m[1]) == "true"
	return &result
}

// writePIDFile writes the current process PID to
// ~/.slimference/run/daemon.pid so the CLI can SIGHUP us. Returns a
// cleanup function that removes the file; safe to call multiple times.
// Errors are LOGGED, not propagated: a missing PID file just means
// the CLI's `slimference enable / disable` won't know we're running.
func writePIDFile() func() {
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		slog.Warn("pidfile: HOME unresolved - skipping")
		return func() {}
	}
	dir := filepath.Join(home, ".slimference", "run")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("pidfile: mkdir failed", "err", err)
		return func() {}
	}
	path := filepath.Join(dir, "daemon.pid")
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid+"\n"), 0o644); err != nil {
		slog.Warn("pidfile: write failed", "err", err)
		return func() {}
	}
	slog.Debug("pidfile: written", "path", path, "pid", pid)
	cleaned := false
	var mu sync.Mutex
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if cleaned {
			return
		}
		cleaned = true
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("pidfile: remove failed", "err", err)
		}
	}
}
