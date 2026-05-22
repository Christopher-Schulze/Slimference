package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Codex Desktop scoped launcher (T228/T238).
//
// Codex Desktop App reads ~/.codex/config.toml `model_provider` for
// sideband endpoints (memories, plugins, login) but the ChatGPT-auth
// conversation traffic is hardcoded to chatgpt.com inside the Rust
// app-server binary. Persistent marker-block enable therefore cannot
// redirect conversation traffic on its own.
//
// This launcher spawns Codex.app's main binary directly with a scoped
// env. The production candidate uses HTTP(S)_PROXY against Slimference's
// loopback CONNECT ingress. The effect is process-local: only the
// spawned Codex.app inherits the env. Browser ChatGPT, ChatGPT.app,
// Claude Code, and any Codex.app launched later via Finder / Spotlight
// remain unaffected.
//
// EMPIRICAL FINDING (2026-05-18, against Codex.app 0.131.0-alpha.9 at
// /Applications/Codex.app/Contents/Resources/codex):
//   - env injection from this launcher reaches the Rust app-server
//     child process (verified via `ps eww`),
//   - proxy env reaches the app-server and is honored for CONNECT,
//     but Codex.app 0.131.0 closes the tunnel before application bytes
//     flow when Slimference presents its locally signed chatgpt.com leaf,
//   - `strings` of the Rust binary shows multiple hardcoded
//     `https://chatgpt.com/backend-api` URLs and exposes only these
//     override env vars: CODEX_REFRESH_TOKEN_URL_OVERRIDE (auth),
//     CODEX_ARC_MONITOR_ENDPOINT_OVERRIDE (telemetry),
//     CODEX_EXEC_SERVER_URL (exec-server), API_BASE_URL (generic).
//   - No CHATGPT_CODEX_BASE_URL / OPENAI_BASE_URL / OPENAI_API_BASE /
//     CHATGPT_BASE_URL handling exists in the current Codex Desktop
//     Rust binary for the conversation route.
//
// The base-URL env mode is therefore retained as a diagnostic surface
// (`--transport=base-url --probe`) and future-proof spawn path. The
// default proxy mode is the only current Desktop route candidate that
// does not require global hosts/pf/system-proxy changes.
//
// `--with-ca-env` is the preferred Desktop compatibility probe. It injects
// Codex's own process-local custom CA hook first, followed by generic CA bundle
// hints used by common TLS stacks. It is not a Desktop savings claim until a
// live Codex Desktop run proves bytes and WSS frames actually flow through
// Slimference.
//
// Reverse: quit Codex.app. Relaunching from Finder / Spotlight gives
// direct ChatGPT routing because no env is inherited.

const (
	defaultCodexDesktopAppPath     = "/Applications/Codex.app"
	defaultCodexDesktopExecRelPath = "Contents/MacOS/Codex"
	codexDesktopTransportProxy     = "proxy"
	codexDesktopTransportBaseURL   = "base-url"
)

var (
	codexDesktopAppPathFn = func() string { return defaultCodexDesktopAppPath }
	codexDesktopStatFn    = func(name string) (fs.FileInfo, error) { return os.Stat(name) }
	codexDesktopStartFn   = startCodexDesktopProcess
	codexDesktopBaseEnvFn = os.Environ
	codexDesktopCATrustFn = codexDesktopCATrustState
	codexDesktopRunningFn = runningCodexDesktopPIDs
)

var codexDesktopStartProbeDelay = 750 * time.Millisecond

// codexDesktopEnvOverrideKeys is the set of env names this launcher
// injects. We set ALL candidates defensively because no single name
// works against Codex 0.131.0-alpha.9 (see file-level finding). The
// last entry, API_BASE_URL, comes from `strings` analysis of the
// Codex Desktop Rust binary as a candidate the binary might honor in
// some future version.
var codexDesktopEnvOverrideKeys = []string{
	"CHATGPT_CODEX_BASE_URL",
	"OPENAI_BASE_URL",
	"OPENAI_API_BASE",
	"CHATGPT_BASE_URL",
	"API_BASE_URL",
}

var codexDesktopProxyEnvKeys = []string{
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"WSS_PROXY",
	"ALL_PROXY",
	"http_proxy",
	"https_proxy",
	"wss_proxy",
	"all_proxy",
	"NO_PROXY",
	"no_proxy",
	"CODEX_NETWORK_PROXY_ACTIVE",
}

var codexDesktopCAEnvKeys = []string{
	"CODEX_CA_CERTIFICATE",
	"SSL_CERT_FILE",
	"CURL_CA_BUNDLE",
	"REQUESTS_CA_BUNDLE",
	"NODE_EXTRA_CA_CERTS",
}

var codexDesktopSessionEnvPrefixes = []string{
	"CODEX_",
}

var codexDesktopWorkspaceEnvKeys = []string{
	"PWD",
	"OLDPWD",
}

type codexLaunchDesktopFlags struct {
	host                   string
	port                   string
	transport              string
	appPath                string
	extra                  []string
	probe                  bool
	withCAEnv              bool
	insecureSkipTrustCheck bool
	help                   bool
}

type codexDesktopCAState struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Trusted bool   `json:"trusted"`
	Error   string `json:"error,omitempty"`
}

func handleCodexLaunchDesktopCmd(args []string) {
	exitFn(runCodexLaunchDesktopCmd(args, defaultInstallPrinter()))
}

func runCodexLaunchDesktopCmd(args []string, p installPrinter) int {
	flags, err := parseCodexLaunchDesktopFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexLaunchDesktopHelpText)
		return 0
	}

	appPath := flags.appPath
	if appPath == "" {
		appPath = codexDesktopAppPathFn()
	}
	binary := filepath.Join(appPath, defaultCodexDesktopExecRelPath)
	if _, err := codexDesktopStatFn(binary); err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: Codex.app binary not found at %s: %v\n", binary, err)
		fmt.Fprintln(p.Err, "  install Codex.app or pass --app=<path-to-.app>.")
		return 1
	}

	proxyURL := fmt.Sprintf("http://%s:%s", flags.host, flags.port)
	overrideURL := proxyURL + "/backend-api/codex"
	env := buildCodexDesktopProxyEnv(proxyURL, codexDesktopBaseEnvFn(), flags.extra)
	ca := codexDesktopCATrustFn()
	if flags.transport == codexDesktopTransportProxy && flags.withCAEnv {
		env = buildCodexDesktopProxyEnv(proxyURL, codexDesktopBaseEnvFn(), nil)
		env = appendCodexDesktopCAEnv(env, ca.Path, flags.extra)
	}
	if flags.transport == codexDesktopTransportBaseURL {
		env = buildCodexDesktopLaunchEnv(overrideURL, codexDesktopBaseEnvFn(), flags.extra)
		ca = codexDesktopCAState{}
	}

	if flags.probe {
		return emitCodexDesktopProbe(p, binary, flags.transport, overrideURL, proxyURL, env, ca)
	}

	if flags.transport == codexDesktopTransportProxy && !flags.insecureSkipTrustCheck && flags.withCAEnv && !ca.Exists {
		fmt.Fprintf(p.Err, "codex launch-desktop: Slimference CA missing at %s; run `slimference install` first.\n", ca.Path)
		if ca.Error != "" {
			fmt.Fprintf(p.Err, "  CA probe: %s\n", ca.Error)
		}
		return 1
	}

	if flags.transport == codexDesktopTransportProxy && !flags.insecureSkipTrustCheck && !flags.withCAEnv && !ca.Trusted {
		if !ca.Exists {
			fmt.Fprintf(p.Err, "codex launch-desktop: Slimference CA missing at %s; run `slimference install` first.\n", ca.Path)
		} else {
			fmt.Fprintln(p.Err, "codex launch-desktop: Slimference CA is not trusted for TLS.")
			fmt.Fprintln(p.Err, "  preferred: retry with `--with-ca-env` for Codex process-local CA trust.")
			fmt.Fprintln(p.Err, "  fallback:  run `slimference cert-trust` for macOS Keychain trust.")
		}
		if ca.Error != "" {
			fmt.Fprintf(p.Err, "  trust probe: %s\n", ca.Error)
		}
		return 1
	}

	runningPIDs, err := codexDesktopRunningFn(binary)
	if err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: running-app probe failed: %v\n", err)
		return 1
	}
	if len(runningPIDs) > 0 {
		fmt.Fprintf(p.Err, "codex launch-desktop: Codex.app is already running (PID %s); quit it first so scoped Slimference env can be injected.\n", joinDesktopPIDs(runningPIDs))
		return 1
	}

	return codexDesktopStartFn(p, binary, env)
}

// buildCodexDesktopLaunchEnv constructs the env for the spawned
// Codex.app. It removes any existing entries for the override keys
// from the base env, appends our overrides, then appends operator
// extras (which may further override any key).
func buildCodexDesktopLaunchEnv(overrideURL string, base []string, extra []string) []string {
	base = sanitizeCodexDesktopBaseEnv(base)
	overrideKeys := make(map[string]struct{}, len(codexDesktopEnvOverrideKeys))
	for _, k := range codexDesktopEnvOverrideKeys {
		overrideKeys[k] = struct{}{}
	}

	out := make([]string, 0, len(base)+len(codexDesktopEnvOverrideKeys)+len(extra))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if _, hit := overrideKeys[kv[:eq]]; hit {
			continue
		}
		out = append(out, kv)
	}
	for _, k := range codexDesktopEnvOverrideKeys {
		out = append(out, k+"="+overrideURL)
	}
	out = append(out, extra...)
	return out
}

func buildCodexDesktopProxyEnv(proxyURL string, base []string, extra []string) []string {
	base = sanitizeCodexDesktopBaseEnv(base)
	overrideKeys := make(map[string]struct{}, len(codexDesktopProxyEnvKeys)+len(codexDesktopEnvOverrideKeys)+len(codexDesktopCAEnvKeys))
	for _, k := range codexDesktopProxyEnvKeys {
		overrideKeys[k] = struct{}{}
	}
	for _, k := range codexDesktopEnvOverrideKeys {
		overrideKeys[k] = struct{}{}
	}
	for _, k := range codexDesktopCAEnvKeys {
		overrideKeys[k] = struct{}{}
	}

	out := make([]string, 0, len(base)+len(codexDesktopProxyEnvKeys)+len(extra))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if _, hit := overrideKeys[kv[:eq]]; hit {
			continue
		}
		out = append(out, kv)
	}
	noProxy := "127.0.0.1,localhost,::1"
	out = append(out,
		"HTTP_PROXY="+proxyURL,
		"HTTPS_PROXY="+proxyURL,
		"WSS_PROXY="+proxyURL,
		"ALL_PROXY="+proxyURL,
		"http_proxy="+proxyURL,
		"https_proxy="+proxyURL,
		"wss_proxy="+proxyURL,
		"all_proxy="+proxyURL,
		"NO_PROXY="+noProxy,
		"no_proxy="+noProxy,
		"CODEX_NETWORK_PROXY_ACTIVE=1",
	)
	out = append(out, extra...)
	return out
}

func sanitizeCodexDesktopBaseEnv(base []string) []string {
	out := make([]string, 0, len(base))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if codexDesktopShouldDropInheritedEnv(kv[:eq]) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func codexDesktopShouldDropInheritedEnv(key string) bool {
	for _, prefix := range codexDesktopSessionEnvPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func codexDesktopDirectOpenEnv(base []string, dir string) []string {
	drop := make(map[string]struct{}, len(codexDesktopWorkspaceEnvKeys))
	for _, key := range codexDesktopWorkspaceEnvKeys {
		drop[key] = struct{}{}
	}
	out := make([]string, 0, len(base)+1)
	for _, kv := range sanitizeCodexDesktopBaseEnv(base) {
		eq := strings.IndexByte(kv, '=')
		if eq >= 0 {
			if _, hit := drop[kv[:eq]]; hit {
				continue
			}
		}
		out = append(out, kv)
	}
	if strings.TrimSpace(dir) != "" {
		out = append(out, "PWD="+dir)
	}
	return out
}

func appendCodexDesktopCAEnv(env []string, caPath string, extra []string) []string {
	keys := make(map[string]struct{}, len(codexDesktopCAEnvKeys))
	for _, k := range codexDesktopCAEnvKeys {
		keys[k] = struct{}{}
	}
	out := make([]string, 0, len(env)+len(codexDesktopCAEnvKeys)+len(extra))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if _, hit := keys[kv[:eq]]; hit {
			continue
		}
		out = append(out, kv)
	}
	if caPath != "" {
		for _, k := range codexDesktopCAEnvKeys {
			out = append(out, k+"="+caPath)
		}
	}
	out = append(out, extra...)
	return out
}

// filterCodexDesktopOverrideEnv returns only the entries that were
// injected by this launcher. Used by --probe so the operator sees the
// scoped override surface without dumping their entire shell env.
func filterCodexDesktopOverrideEnv(env []string) []string {
	keys := make(map[string]struct{}, len(codexDesktopEnvOverrideKeys))
	for _, k := range codexDesktopEnvOverrideKeys {
		keys[k] = struct{}{}
	}
	var out []string
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		if _, hit := keys[kv[:eq]]; hit {
			out = append(out, kv)
		}
	}
	return out
}

func filterCodexDesktopProxyEnv(env []string) []string {
	keys := make(map[string]struct{}, len(codexDesktopProxyEnvKeys)+len(codexDesktopCAEnvKeys))
	for _, k := range codexDesktopProxyEnvKeys {
		keys[k] = struct{}{}
	}
	for _, k := range codexDesktopCAEnvKeys {
		keys[k] = struct{}{}
	}
	var out []string
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		if _, hit := keys[kv[:eq]]; hit {
			out = append(out, kv)
		}
	}
	return out
}

type codexLaunchDesktopProbe struct {
	Binary      string              `json:"binary"`
	Transport   string              `json:"transport"`
	OverrideURL string              `json:"override_url,omitempty"`
	ProxyURL    string              `json:"proxy_url,omitempty"`
	EnvOverride []string            `json:"env_override"`
	CATrust     codexDesktopCAState `json:"ca_trust,omitempty"`
}

func emitCodexDesktopProbe(p installPrinter, binary, transport, overrideURL, proxyURL string, env []string, ca codexDesktopCAState) int {
	probe := codexLaunchDesktopProbe{
		Binary:      binary,
		Transport:   transport,
		OverrideURL: overrideURL,
		ProxyURL:    proxyURL,
		EnvOverride: filterCodexDesktopProxyEnv(env),
		CATrust:     ca,
	}
	if transport == codexDesktopTransportBaseURL {
		probe.ProxyURL = ""
		probe.EnvOverride = filterCodexDesktopOverrideEnv(env)
		probe.CATrust = codexDesktopCAState{}
	}
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(probe); err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: probe encode: %v\n", err)
		return 1
	}
	return 0
}

func codexDesktopCATrustState() codexDesktopCAState {
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return codexDesktopCAState{Error: "home unresolved"}
	}
	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	state := codexDesktopCAState{Path: cert}
	if _, err := os.Stat(cert); err != nil {
		state.Error = err.Error()
		return state
	}
	state.Exists = true
	trusted, err := newTransparentKeychainFn().IsTrusted(cert)
	if err != nil {
		state.Error = err.Error()
	}
	state.Trusted = trusted
	return state
}

func newCodexDesktopCommand(binary string, env []string) *exec.Cmd {
	cmd := exec.Command(binary)
	cmd.Dir = filepath.Dir(binary)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

func startCodexDesktopProcess(p installPrinter, binary string, env []string) int {
	cmd := newCodexDesktopCommand(binary, env)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: spawn failed: %v\n", err)
		return 1
	}
	pid := cmd.Process.Pid
	if codexDesktopStartProbeDelay > 0 {
		time.Sleep(codexDesktopStartProbeDelay)
		var status syscall.WaitStatus
		waitedPID, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if err != nil {
			_ = cmd.Process.Release()
			fmt.Fprintf(p.Err, "codex launch-desktop: start verification failed for PID %d: %v\n", pid, err)
			return 1
		}
		if waitedPID == pid {
			_ = cmd.Process.Release()
			fmt.Fprintf(p.Err, "codex launch-desktop: process exited during startup (PID %d, %s)\n", pid, formatCodexDesktopWaitStatus(status))
			return 1
		}
	}
	if err := cmd.Process.Release(); err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: release failed: %v\n", err)
	}
	fmt.Fprintf(p.Out, "Codex.app launched (PID %d) with scoped Slimference env.\n", pid)
	fmt.Fprintln(p.Out, "Scope: only this Codex.app inherits the env. Browser ChatGPT, ChatGPT.app, Claude untouched.")
	fmt.Fprintln(p.Out, "Reverse: quit Codex.app via Cmd+Q. Relaunch from Finder/Spotlight for direct routing.")
	return 0
}

func formatCodexDesktopWaitStatus(status syscall.WaitStatus) string {
	switch {
	case status.Exited():
		return fmt.Sprintf("exit=%d", status.ExitStatus())
	case status.Signaled():
		return fmt.Sprintf("signal=%s", status.Signal())
	default:
		return fmt.Sprintf("status=%d", status)
	}
}

func runningCodexDesktopPIDs(binary string) ([]int, error) {
	out, err := exec.Command("ps", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	want := filepath.Clean(binary)
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if filepath.Clean(fields[1]) == want {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func joinDesktopPIDs(pids []int) string {
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	return strings.Join(parts, ",")
}

func parseCodexLaunchDesktopFlags(args []string) (codexLaunchDesktopFlags, error) {
	f := codexLaunchDesktopFlags{host: "127.0.0.1", port: "8990", transport: codexDesktopTransportProxy}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--probe":
			f.probe = true
		case a == "--with-ca-env":
			f.withCAEnv = true
		case a == "--insecure-skip-cert-trust-check":
			f.insecureSkipTrustCheck = true
		case strings.HasPrefix(a, "--transport="):
			f.transport = strings.TrimPrefix(a, "--transport=")
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "--app="):
			f.appPath = strings.TrimPrefix(a, "--app=")
		case strings.HasPrefix(a, "--env="):
			kv := strings.TrimPrefix(a, "--env=")
			if !strings.Contains(kv, "=") {
				return f, fmt.Errorf("invalid --env value %q (must be KEY=VAL)", kv)
			}
			f.extra = append(f.extra, kv)
		default:
			return f, fmt.Errorf("unknown flag %q", a)
		}
	}
	switch f.transport {
	case codexDesktopTransportProxy, codexDesktopTransportBaseURL:
	default:
		return f, fmt.Errorf("invalid --transport %q (want proxy or base-url)", f.transport)
	}
	if f.withCAEnv && f.transport != codexDesktopTransportProxy {
		return f, fmt.Errorf("--with-ca-env requires --transport=proxy")
	}
	return f, nil
}

const codexLaunchDesktopHelpText = `usage: slimference codex launch-desktop [--transport=proxy|base-url] [--probe] [--with-ca-env] [--host=127.0.0.1] [--port=8990] [--app=<path>] [--env KEY=VAL...]

Spawns Codex.app's main binary with a scoped env. Default transport=proxy
sets HTTP(S)/WSS proxy variables only on the launched app process. Browser
ChatGPT, ChatGPT.app, Claude Code, and any Codex.app relaunched from
Finder/Spotlight remain on direct routing.

Flags:
  --transport=<mode>  proxy (default) or base-url diagnostic mode
  --probe             emit scoped env and CA state as JSON without spawning
  --with-ca-env       proxy-mode Desktop probe: inject CODEX_CA_CERTIFICATE
                      first, then SSL_CERT_FILE, CURL_CA_BUNDLE,
                      REQUESTS_CA_BUNDLE, NODE_EXTRA_CA_CERTS
  --host=<host>       slimference daemon host (default 127.0.0.1)
  --port=<port>       slimference daemon port (default 8990)
  --app=<path>        override path to Codex.app bundle (default /Applications/Codex.app)
  --env KEY=VAL       add or override an env entry (repeatable)
  --insecure-skip-cert-trust-check
                      spawn despite an untrusted CA; diagnostics only
  --help, -h          this text

Reverse: quit Codex.app (Cmd+Q). Relaunching from Finder/Spotlight does
not inherit the override and returns to direct ChatGPT routing.

This launcher does NOT touch /etc/hosts, pf, Keychain, system proxy,
~/.codex/config.toml, or any global state. It is scoped to one spawn.
`
