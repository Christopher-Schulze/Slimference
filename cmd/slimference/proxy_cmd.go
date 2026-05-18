package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/integrate"
	"github.com/slimference/slimference/internal/tlsca"
	"github.com/slimference/slimference/internal/tlsdial"
	"github.com/slimference/slimference/internal/transparent"
)

// handleProxyCmd implements
// `slimference proxy <install|enable|disable|status|uninstall|env>`.
//
// Transparent mode is the system-wide intercept path: install once,
// enable to flip all HTTPS to Slimference, disable to drop back to
// direct, uninstall to remove every trace. Each subcommand is
// non-destructive by default and explains what it is about to do
// before touching the system.
func handleProxyCmd(args []string) {
	rc := proxyRun(args, proxyEnv{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Home:   os.Getenv("HOME"),
		CADirFn: func() string {
			return filepath.Join(os.Getenv("HOME"), ".slimference")
		},
		Network:     transparent.NewManager(),
		Keychain:    transparent.NewKeychain(),
		Launch:      transparent.NewLaunchAgent(),
		LoadCA:      tlsca.LoadOrGenerateCA,
		HealthCheck: defaultProxyHealthCheck,
		RunCommand:  defaultProxyCommandRunner,
	})
	if rc != 0 {
		exitFn(rc)
	}
}

type proxyEnv struct {
	Stdout      io.Writer
	Stderr      io.Writer
	Stdin       io.Reader
	Home        string
	CADirFn     func() string
	Network     proxyNetworkManager
	Keychain    proxyKeychain
	Launch      proxyLaunchAgent
	LoadCA      func(dir string) (*tlsca.CA, error)
	HealthCheck func(host, port string) error
	RunCommand  proxyCommandRunner
}

type proxyCommandRunner func(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error

// proxyNetworkManager is the subset of *transparent.Manager that the
// subcommand consumes. Defined here so tests can stub it.
type proxyNetworkManager interface {
	EnableHTTPS(host, port string) ([]string, error)
	Disable() ([]string, error)
	Status() transparent.Snapshot
}

// proxyKeychain is the subset of *transparent.Keychain consumed.
type proxyKeychain interface {
	Install(certPath string, scope transparent.Scope) error
	Uninstall(certSHA1 string, scope transparent.Scope) error
	IsTrusted(certPath string) (bool, error)
}

// proxyLaunchAgent is the subset of *transparent.LaunchAgent consumed.
type proxyLaunchAgent interface {
	Install(plistPath, daemonBinary, logDir string) error
	Uninstall(plistPath string) error
	IsInstalled(plistPath string) bool
}

func proxyRun(args []string, env proxyEnv) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "usage: slimference proxy <install|enable|disable|status|uninstall|env|run>")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "install":
		return proxyInstall(rest, env)
	case "enable":
		return proxyEnable(rest, env)
	case "disable":
		return proxyDisable(rest, env)
	case "status":
		return proxyStatus(rest, env)
	case "uninstall":
		return proxyUninstall(rest, env)
	case "env":
		return proxyEnvCmd(rest, env)
	case "run":
		return proxyRunClientCmd(rest, env)
	default:
		fmt.Fprintf(env.Stderr, "proxy: unknown subcommand %q\n", sub)
		return 2
	}
}

// flags is a tiny --yes / --system / --no-launchd parser shared by
// the subcommands.
type proxyFlags struct {
	yes       bool
	system    bool
	noLaunchd bool
	host      string
	port      string
}

func parseProxyFlags(args []string) (proxyFlags, error) {
	f := proxyFlags{host: "127.0.0.1", port: "8990"}
	for _, a := range args {
		switch {
		case a == "--yes":
			f.yes = true
		case a == "--system":
			f.system = true
		case a == "--no-launchd":
			f.noLaunchd = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("unknown flag %q", a)
		default:
			return f, fmt.Errorf("unexpected positional %q", a)
		}
	}
	return f, nil
}

func proxyInstall(args []string, env proxyEnv) int {
	f, err := parseProxyFlags(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy install: %v\n", err)
		return 2
	}
	if env.Home == "" {
		fmt.Fprintln(env.Stderr, "proxy install: HOME unresolved")
		return 1
	}
	if env.CADirFn == nil {
		fmt.Fprintln(env.Stderr, "proxy install: CA directory function unresolved")
		return 1
	}

	caDir := env.CADirFn()
	ca, err := env.LoadCA(caDir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy install: load/generate CA: %v\n", err)
		return 1
	}
	fingerprint := tlsca.Fingerprint(ca)
	scope := transparent.ScopeUser
	if f.system {
		scope = transparent.ScopeSystem
	}
	fmt.Fprintln(env.Stdout, "Slimference transparent mode installer")
	fmt.Fprintln(env.Stdout, "--------------------------------------")
	fmt.Fprintf(env.Stdout, "CA fingerprint (SHA-256): %s\n", fingerprint)
	fmt.Fprintf(env.Stdout, "Trust scope:              %s\n", scope.String())
	fmt.Fprintf(env.Stdout, "System proxy target:      %s:%s\n", f.host, f.port)
	if !f.noLaunchd {
		fmt.Fprintf(env.Stdout, "Auto-start (launchd):     %s\n", DefaultPlistPath(env.Home))
	} else {
		fmt.Fprintln(env.Stdout, "Auto-start (launchd):     skipped (--no-launchd)")
	}

	certPath := filepath.Join(caDir, "ca", "root.crt")
	if err := env.Keychain.Install(certPath, scope); err != nil {
		fmt.Fprintf(env.Stderr, "proxy install: keychain trust failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, "Trust:                    OK")

	if !f.noLaunchd {
		plistPath := DefaultPlistPath(env.Home)
		// os.Executable returns the test binary path under go test and
		// the slimference binary path in production. Either way it is
		// what the launchd plist should exec. The error return is
		// undocumented to fire on the platforms we target.
		bin, _ := os.Executable()
		logDir := filepath.Join(caDir, "log")
		if err := env.Launch.Install(plistPath, bin, logDir); err != nil {
			fmt.Fprintf(env.Stderr, "proxy install: launchd: %v\n", err)
			return 1
		}
		fmt.Fprintln(env.Stdout, "Auto-start:               installed")
	}

	if !f.yes {
		fmt.Fprintln(env.Stdout, "")
		fmt.Fprintln(env.Stdout, "Install complete. Run `slimference proxy enable` when you")
		fmt.Fprintln(env.Stdout, "want HTTPS traffic to flow through Slimference.")
		return 0
	}
	// --yes implies enable as the final step.
	return enableHelper(env, f.host, f.port)
}

// DefaultPlistPath delegates to the transparent package; re-exported
// so tests can compose the same path for assertion.
func DefaultPlistPath(home string) string {
	return transparent.DefaultPlistPath(home)
}

func proxyEnable(args []string, env proxyEnv) int {
	f, err := parseProxyFlags(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy enable: %v\n", err)
		return 2
	}
	return enableHelper(env, f.host, f.port)
}

func enableHelper(env proxyEnv, host, port string) int {
	services, err := env.Network.EnableHTTPS(host, port)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy enable: %v\n", err)
		return 1
	}
	if len(services) == 0 {
		fmt.Fprintln(env.Stdout, "proxy enable: no active network services to flip")
		return 0
	}
	fmt.Fprintf(env.Stdout, "Routed %d service(s) through %s:%s:\n", len(services), host, port)
	for _, s := range services {
		fmt.Fprintf(env.Stdout, "  - %s\n", s)
	}
	return 0
}

func proxyDisable(args []string, env proxyEnv) int {
	if _, err := parseProxyFlags(args); err != nil {
		fmt.Fprintf(env.Stderr, "proxy disable: %v\n", err)
		return 2
	}
	cleared, err := env.Network.Disable()
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy disable: %v\n", err)
		return 1
	}
	if len(cleared) == 0 {
		fmt.Fprintln(env.Stdout, "proxy disable: no services were flipped")
		return 0
	}
	fmt.Fprintf(env.Stdout, "Cleared HTTPS proxy on %d service(s):\n", len(cleared))
	for _, s := range cleared {
		fmt.Fprintf(env.Stdout, "  - %s\n", s)
	}
	return 0
}

func proxyStatus(args []string, env proxyEnv) int {
	if _, err := parseProxyFlags(args); err != nil {
		fmt.Fprintf(env.Stderr, "proxy status: %v\n", err)
		return 2
	}
	caDir := env.CADirFn()
	ca, err := env.LoadCA(caDir)
	caState := "ok"
	caFP := ""
	if err != nil {
		caState = fmt.Sprintf("error: %v", err)
	} else {
		caFP = tlsca.Fingerprint(ca)
	}
	fmt.Fprintln(env.Stdout, "Slimference transparent mode status")
	fmt.Fprintln(env.Stdout, "-----------------------------------")
	fmt.Fprintf(env.Stdout, "CA:                  %s\n", caState)
	if caFP != "" {
		fmt.Fprintf(env.Stdout, "CA fingerprint:      %s\n", caFP)
	}
	printTransparentRuntimeStatus(env.Stdout)
	plistPath := DefaultPlistPath(env.Home)
	if env.Launch.IsInstalled(plistPath) {
		fmt.Fprintf(env.Stdout, "Auto-start:          installed (%s)\n", plistPath)
	} else {
		fmt.Fprintln(env.Stdout, "Auto-start:          not installed")
	}
	snap := env.Network.Status()
	if snap.UnreachableErr != nil {
		fmt.Fprintf(env.Stdout, "Network services:    error: %v\n", snap.UnreachableErr)
	} else if len(snap.Services) == 0 {
		fmt.Fprintln(env.Stdout, "Network services:    none active")
	} else {
		fmt.Fprintf(env.Stdout, "Network services:    %d active\n", len(snap.Services))
		daemonChecked := false
		for _, s := range snap.Services {
			active := "off"
			if s.HTTPSEnabled {
				active = fmt.Sprintf("ON %s:%s", s.HTTPSProxy, s.HTTPSPort)
				if isSlimferenceProxyTarget(s.HTTPSProxy, s.HTTPSPort) && !daemonChecked {
					daemonChecked = true
					printProxyDaemonStatus(env.Stdout, env.HealthCheck, s.HTTPSProxy, s.HTTPSPort)
				}
			}
			fmt.Fprintf(env.Stdout, "  - %-20s %s\n", s.Name, active)
		}
	}
	return 0
}

func printTransparentRuntimeStatus(w io.Writer) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(w, "Transparent config:  error: %v\n", err)
		return
	}
	state := "off"
	if cfg.Transparent.Enabled {
		state = "on"
	}
	fmt.Fprintf(w, "Transparent runtime: %s\n", state)
	resolver, err := tlsdial.NewResolver(cfg.Transparent.DefaultTLSProfile, cfg.Transparent.TLSProfiles)
	if err != nil {
		fmt.Fprintf(w, "TLS profiles:        error: %v\n", err)
		return
	}
	fmt.Fprintln(w, "TLS profiles:")
	for _, host := range cfg.Transparent.InterceptHosts {
		profile := resolver.Resolve(host)
		fmt.Fprintf(w, "  - %-20s %s\n", host, profile.Name)
	}
}

func printProxyDaemonStatus(w io.Writer, healthCheck func(host, port string) error, host, port string) {
	if healthCheck == nil {
		return
	}
	if err := healthCheck(host, port); err != nil {
		fmt.Fprintf(w, "Daemon:              unreachable at %s:%s (%v)\n", host, port, err)
		fmt.Fprintln(w, "Repair:              slimference proxy disable")
		return
	}
	fmt.Fprintf(w, "Daemon:              reachable at %s:%s\n", host, port)
}

func isSlimferenceProxyTarget(host, port string) bool {
	h := strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return port == "8990" && (h == "127.0.0.1" || h == "localhost" || h == "::1")
}

func defaultProxyHealthCheck(host, port string) error {
	client := &http.Client{
		Timeout: 750 * time.Millisecond,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	resp, err := client.Get("http://" + net.JoinHostPort(strings.Trim(host, "[]"), port) + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return fmt.Errorf("health returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func proxyUninstall(args []string, env proxyEnv) int {
	f, err := parseProxyFlags(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy uninstall: %v\n", err)
		return 2
	}
	// Disable first so the system never points at a nonexistent CA.
	if _, derr := env.Network.Disable(); derr != nil {
		fmt.Fprintf(env.Stderr, "proxy uninstall: warning: disable failed: %v\n", derr)
	}
	scope := transparent.ScopeUser
	if f.system {
		scope = transparent.ScopeSystem
	}
	caDir := env.CADirFn()
	ca, err := env.LoadCA(caDir)
	if err == nil {
		sha1 := tlsca.SHA1Fingerprint(ca.Cert)
		// `security delete-certificate` wants the SHA1 without colons.
		sha1 = strings.ReplaceAll(sha1, ":", "")
		if uerr := env.Keychain.Uninstall(sha1, scope); uerr != nil {
			fmt.Fprintf(env.Stderr, "proxy uninstall: warning: keychain remove failed: %v\n", uerr)
		}
	}
	plistPath := DefaultPlistPath(env.Home)
	if env.Launch.IsInstalled(plistPath) {
		if uerr := env.Launch.Uninstall(plistPath); uerr != nil {
			fmt.Fprintf(env.Stderr, "proxy uninstall: warning: launchd remove failed: %v\n", uerr)
		}
	}
	fmt.Fprintln(env.Stdout, "Slimference transparent mode: uninstall complete.")
	fmt.Fprintln(env.Stdout, "Note: the CA files in ~/.slimference/ca remain on disk so")
	fmt.Fprintln(env.Stdout, "you can re-install without regenerating. Delete the directory")
	fmt.Fprintln(env.Stdout, "manually if you want a fully clean slate.")
	_ = ctxBackground
	return 0
}

func proxyEnvCmd(args []string, env proxyEnv) int {
	mode, host, port, codexArgs, ok := parseCodexProxyArgs(args, env.Stderr, "env")
	if !ok {
		return 2
	}
	command := codexEnvCommand(mode, host, port, codexArgs)
	switch mode {
	case "direct":
		fmt.Fprintln(env.Stdout, "# Codex CLI direct mode: use while macOS System HTTPS proxy is armed for Codex App testing.")
	case "proxied":
		fmt.Fprintln(env.Stdout, "# Codex CLI proxied mode: per-process custom provider override; macOS System HTTPS proxy remains untouched.")
		fmt.Fprintln(env.Stdout, "# WebSockets are disabled for this one Codex process so requests use HTTP directly instead of retrying fallback.")
		fmt.Fprintln(env.Stdout, "# Codex App stays direct unless you separately run `slimference proxy enable`.")
	case "proxied-wss":
		fmt.Fprintln(env.Stdout, "# Codex CLI scoped WSS mode: per-process custom provider override with Responses WebSockets enabled.")
		fmt.Fprintln(env.Stdout, "# macOS system routing remains untouched; only this Codex process uses Slimference.")
	case "transparent-proxied":
		fmt.Fprintln(env.Stdout, "# Codex CLI transparent-proxied mode: process-local HTTP(S)_PROXY through CONNECT/MITM.")
		fmt.Fprintln(env.Stdout, "# Requires a running Slimference daemon with [transparent].enabled=true and the local CA trusted.")
	}
	fmt.Fprintln(env.Stdout, "# Flight logs require the running Slimference daemon to have [debug].decisions_log or SLIMFERENCE_DEBUG_DECISIONS_LOG configured.")
	fmt.Fprintln(env.Stdout, shellJoin(command))
	return 0
}

func proxyRunClientCmd(args []string, env proxyEnv) int {
	mode, host, port, codexArgs, ok := parseCodexProxyArgs(args, env.Stderr, "run")
	if !ok {
		return 2
	}
	command := codexEnvCommand(mode, host, port, codexArgs)
	runner := env.RunCommand
	if runner == nil {
		runner = defaultProxyCommandRunnerFunc
	}
	if err := runner(command[0], command[1:], env.Stdin, env.Stdout, env.Stderr); err != nil {
		fmt.Fprintf(env.Stderr, "proxy run codex: %v\n", err)
		return 1
	}
	return 0
}

func parseCodexProxyArgs(args []string, stderr io.Writer, verb string) (mode, host, port string, codexArgs []string, ok bool) {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "usage: slimference proxy %s codex <--direct|--proxied|--proxied-wss|--transparent-proxied> [--host=127.0.0.1] [--port=8990] [-- <codex-args>...]\n", verb)
		return "", "", "", nil, false
	}
	client, rest := args[0], args[1:]
	if client != "codex" {
		fmt.Fprintf(stderr, "proxy %s: unsupported client %q (supported: codex)\n", verb, client)
		return "", "", "", nil, false
	}
	host = "127.0.0.1"
	port = "8990"
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "--" {
			codexArgs = append(codexArgs, rest[i+1:]...)
			break
		}
		switch {
		case a == "--direct":
			if mode != "" {
				fmt.Fprintf(stderr, "proxy %s codex: choose exactly one of --direct, --proxied, --proxied-wss, or --transparent-proxied\n", verb)
				return "", "", "", nil, false
			}
			mode = "direct"
		case a == "--proxied":
			if mode != "" {
				fmt.Fprintf(stderr, "proxy %s codex: choose exactly one of --direct, --proxied, --proxied-wss, or --transparent-proxied\n", verb)
				return "", "", "", nil, false
			}
			mode = "proxied"
		case a == "--proxied-wss":
			if mode != "" {
				fmt.Fprintf(stderr, "proxy %s codex: choose exactly one of --direct, --proxied, --proxied-wss, or --transparent-proxied\n", verb)
				return "", "", "", nil, false
			}
			mode = "proxied-wss"
		case a == "--transparent-proxied":
			if mode != "" {
				fmt.Fprintf(stderr, "proxy %s codex: choose exactly one of --direct, --proxied, --proxied-wss, or --transparent-proxied\n", verb)
				return "", "", "", nil, false
			}
			mode = "transparent-proxied"
		case strings.HasPrefix(a, "--host="):
			host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "proxy %s codex: unknown flag %q\n", verb, a)
			return "", "", "", nil, false
		default:
			codexArgs = append(codexArgs, a)
		}
	}
	if mode == "" {
		fmt.Fprintf(stderr, "proxy %s codex: choose exactly one of --direct, --proxied, --proxied-wss, or --transparent-proxied\n", verb)
		return "", "", "", nil, false
	}
	return mode, host, port, codexArgs, true
}

func defaultProxyCommandRunner(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

var defaultProxyCommandRunnerFunc proxyCommandRunner = defaultProxyCommandRunner

func codexEnvCommand(mode, host, port string, codexArgs []string) []string {
	base := []string{"env"}
	switch mode {
	case "direct":
		base = append(base,
			"-u", "HTTP_PROXY",
			"-u", "HTTPS_PROXY",
			"-u", "ALL_PROXY",
			"-u", "http_proxy",
			"-u", "https_proxy",
			"-u", "all_proxy",
			"NO_PROXY=*",
			"no_proxy=*",
		)
	case "proxied", "proxied-wss":
		target := "http://" + net.JoinHostPort(host, port)
		supportsWebSockets := "false"
		if mode == "proxied-wss" {
			supportsWebSockets = "true"
		}
		base = append(base,
			"-u", "HTTP_PROXY",
			"-u", "HTTPS_PROXY",
			"-u", "ALL_PROXY",
			"-u", "http_proxy",
			"-u", "https_proxy",
			"-u", "all_proxy",
			"NO_PROXY=127.0.0.1,localhost,::1",
			"no_proxy=127.0.0.1,localhost,::1",
		)
		base = append(base,
			"codex",
			"-c", "model_provider="+strconv.Quote("slimference-codex"),
			"-c", "model_providers.slimference-codex.name="+strconv.Quote("Slimference"),
			"-c", "model_providers.slimference-codex.base_url="+strconv.Quote(integrate.CodexOpenAIBaseURL(target)),
			"-c", "model_providers.slimference-codex.requires_openai_auth=true",
			"-c", "model_providers.slimference-codex.supports_websockets="+supportsWebSockets,
			"-c", "model_providers.slimference-codex.wire_api="+strconv.Quote("responses"),
		)
		base = append(base, codexArgs...)
		return base
	case "transparent-proxied":
		target := "http://" + net.JoinHostPort(host, port)
		base = append(base,
			"-u", "NO_PROXY",
			"-u", "no_proxy",
			"HTTP_PROXY="+target,
			"HTTPS_PROXY="+target,
			"ALL_PROXY="+target,
			"http_proxy="+target,
			"https_proxy="+target,
			"all_proxy="+target,
		)
	}
	base = append(base, "codex")
	base = append(base, codexArgs...)
	return base
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' ||
			r >= 'a' && r <= 'z' ||
			r >= '0' && r <= '9' ||
			r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '=')
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// ctxBackground is referenced by uninstall just to keep the context
// import slot active for future expansions.
var ctxBackground = context.Background()
