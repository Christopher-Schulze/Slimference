package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/control/reversibility"
	"github.com/slimference/slimference/internal/install"
	"github.com/slimference/slimference/internal/proxy"
)

// installFlags is the shared flag set for install/uninstall/enable/
// disable. Each subcommand consumes the subset it needs.
type installFlags struct {
	dryRun       bool
	json         bool
	noHooks      bool
	withClaude   bool
	noAutoStart  bool
	withKeychain bool
	noKeychain   bool
	keepCA       bool
	systemScope  bool
	preflight    bool
	help         bool
	configPath   string
	binaryPath   string
	rest         []string
}

var installPlanFn = install.Plan
var preflightUpstreamResolutionFn = proxy.PreflightUpstreamResolution

func parseInstallFlags(args []string) (installFlags, error) {
	f := installFlags{}
	for _, a := range args {
		switch {
		case a == "--dry-run":
			f.dryRun = true
		case a == "--json":
			f.json = true
		case a == "--no-hooks":
			f.noHooks = true
		case a == "--with-claude":
			f.withClaude = true
		case a == "--no-autostart":
			f.noAutoStart = true
		case a == "--with-keychain":
			f.withKeychain = true
		case a == "--no-keychain":
			f.noKeychain = true
		case a == "--keep-ca":
			f.keepCA = true
		case a == "--system":
			f.systemScope = true
		case a == "--preflight":
			f.preflight = true
		case a == "--help", a == "-h":
			f.help = true
		case strings.HasPrefix(a, "--config="):
			f.configPath = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "--binary="):
			f.binaryPath = strings.TrimPrefix(a, "--binary=")
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("unknown flag %q", a)
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f, nil
}

// installPrinter is the small abstraction the subcommands write to.
// Tests inject a buffered writer; production uses os.Stdout/Stderr.
type installPrinter struct {
	Out io.Writer
	Err io.Writer
}

func defaultInstallPrinter() installPrinter {
	return installPrinter{Out: os.Stdout, Err: os.Stderr}
}

// handleInstallCmd dispatches `slimference install`. Constructs an
// install.Plan and Apply()s it, streaming per-Step status to stdout.
func handleInstallCmd(args []string) {
	exitFn(runInstallCmd(args, defaultInstallPrinter()))
}

func runInstallCmd(args []string, p installPrinter) int {
	f, err := parseInstallFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "install: %v\n", err)
		return 2
	}
	if f.help {
		fmt.Fprint(p.Out, installHelpText)
		return 0
	}
	opts := install.Options{
		SkipHooks:     f.noHooks,
		SkipAutoStart: f.noAutoStart,
		WithKeychain:  f.withKeychain,
		SkipKeychain:  f.noKeychain,
		BinaryPath:    f.binaryPath,
		Version:       version,
	}
	if f.systemScope {
		opts.KeychainScope = 1 // installsteps.ScopeSystem; avoid direct import
	}
	plan, err := installPlanFn(opts)
	if err != nil {
		fmt.Fprintf(p.Err, "install: %v\n", err)
		return 1
	}
	if f.dryRun {
		return renderPlanInspect(p, plan, f.json, "install")
	}
	fmt.Fprintln(p.Out, "slimference install")
	fmt.Fprintln(p.Out, "-------------------")
	res := plan.Apply(context.Background())
	renderApplyResult(p, res)
	if res.Err != nil {
		fmt.Fprintln(p.Err, "")
		fmt.Fprintln(p.Err, "Install failed. Run `slimference uninstall` to roll back applied steps.")
		return 3
	}
	fmt.Fprintln(p.Out, "")
	fmt.Fprintln(p.Out, "Install complete. Next:")
	fmt.Fprintln(p.Out, "  slimference status --preflight")
	fmt.Fprintln(p.Out, "  slimference codex run -- <prompt>")
	fmt.Fprintln(p.Out, "  slimference codex enable   # optional shared CLI/App route")
	fmt.Fprintln(p.Out, "")
	fmt.Fprintln(p.Out, "Global lab only:")
	fmt.Fprintln(p.Out, "  slimference lab cert-trust")
	fmt.Fprintln(p.Out, "  slimference lab root-arm --global-chatgpt-hosts")
	fmt.Fprintln(p.Out, "  slimference lab enable")
	return 0
}

// handleUninstallCmd dispatches `slimference uninstall`.
func handleUninstallCmd(args []string) {
	exitFn(runUninstallCmd(args, defaultInstallPrinter()))
}

func runUninstallCmd(args []string, p installPrinter) int {
	f, err := parseInstallFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "uninstall: %v\n", err)
		return 2
	}
	if f.help {
		fmt.Fprint(p.Out, uninstallHelpText)
		return 0
	}
	// Uninstall is intentionally a cleanup superset: default install no
	// longer trusts the CA in Keychain, but uninstall still attempts the
	// idempotent keychain Reverse so old or opt-in trust is removed.
	// --no-hooks / --no-autostart / --no-keychain skip the corresponding
	// Steps. --keep-ca additionally skips the keychain Step.
	opts := install.Options{
		SkipHooks:     f.noHooks,
		SkipAutoStart: f.noAutoStart,
		WithKeychain:  !f.noKeychain && !f.keepCA,
		SkipKeychain:  f.noKeychain || f.keepCA,
		BinaryPath:    f.binaryPath,
	}
	if f.systemScope {
		opts.KeychainScope = 1
	}
	plan, err := installPlanFn(opts)
	if err != nil {
		fmt.Fprintf(p.Err, "uninstall: %v\n", err)
		return 1
	}
	if f.dryRun {
		return renderPlanInspect(p, plan, f.json, "uninstall")
	}
	fmt.Fprintln(p.Out, "slimference uninstall")
	fmt.Fprintln(p.Out, "---------------------")
	res := plan.Reverse(context.Background())
	renderReverseResult(p, res)
	if res.Err() != nil {
		return 3
	}
	fmt.Fprintln(p.Out, "")
	fmt.Fprintln(p.Out, "Uninstall complete. Backups remain under ~/.slimference/backups/.")
	return 0
}

// handleEnableCmd enables the scoped Codex CLI/App route. It is
// intentionally not the global transparent MITM switch; that lab-only
// path lives behind `slimference lab enable`.
func handleEnableCmd(args []string) {
	exitFn(runEnableCmd(args, defaultInstallPrinter()))
}

func runEnableCmd(args []string, p installPrinter) int {
	return runCodexEnableCmd(args, p)
}

// handleDisableCmd disables the scoped Codex CLI/App route. Global
// transparent lab routing is disabled via `slimference lab disable`.
func handleDisableCmd(args []string) {
	exitFn(runDisableCmd(args, defaultInstallPrinter()))
}

func runDisableCmd(args []string, p installPrinter) int {
	return runCodexDisableCmd(args, p)
}

func runLabEnableCmd(args []string, p installPrinter) int {
	return setSNIPeekMode(args, p, true, "lab enable")
}

func runLabDisableCmd(args []string, p installPrinter) int {
	return setSNIPeekMode(args, p, false, "lab disable")
}

func setSNIPeekMode(args []string, p installPrinter, target bool, verb string) int {
	f, err := parseInstallFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "%s: %v\n", verb, err)
		return 2
	}
	if f.help {
		fmt.Fprint(p.Out, labEnableDisableHelpText)
		return 0
	}
	cfgPath := f.configPath
	if cfgPath == "" {
		cfgPath = enableDisableConfigPathFn()
	}
	if cfgPath == "" {
		fmt.Fprintf(p.Err, "%s: cannot resolve config path (HOME?)\n", verb)
		return 1
	}
	if err := patchSNIPeekMode(cfgPath, target); err != nil {
		fmt.Fprintf(p.Err, "%s: %v\n", verb, err)
		return 1
	}
	verbState := "DISABLED"
	if target {
		verbState = "ENABLED"
	}
	fmt.Fprintf(p.Out, "transparent.sni_peek_mode = %v written to %s\n", target, cfgPath)
	// Best-effort SIGHUP. Missing PID file = daemon not running; the
	// next start will pick up the new config naturally.
	if sent, err := signalDaemonReload(); err != nil {
		fmt.Fprintf(p.Out, "(daemon SIGHUP skipped: %v)\n", err)
	} else if sent {
		fmt.Fprintln(p.Out, "Daemon SIGHUP sent.")
	} else {
		fmt.Fprintln(p.Out, "Daemon not running. Start it with `slimference service start` to apply the change.")
	}
	fmt.Fprintf(p.Out, "Transparent MITM %s.\n", verbState)
	return 0
}

// handleStatusCmd renders SetupState as a table or JSON.
func handleStatusCmd(args []string) {
	exitFn(runStatusCmd(args, defaultInstallPrinter()))
}

func runStatusCmd(args []string, p installPrinter) int {
	f, err := parseInstallFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "status: %v\n", err)
		return 2
	}
	if f.help {
		fmt.Fprint(p.Out, statusHelpText)
		return 0
	}
	state, err := fetchSetupState(2 * time.Second)
	if err != nil {
		fmt.Fprintf(p.Err, "status: %v\n", err)
		fmt.Fprintf(p.Err, "       daemon not running? try `slimference service start`.\n")
		return 1
	}
	if f.preflight || state.NetworkRedir.HostsActive || state.Listener.BoundOnSNIPeek {
		addStatusPreflight(&state, 2*time.Second)
	}
	if f.json {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(state)
		return 0
	}
	renderStatus(p, state)
	return 0
}

// renderPlanInspect renders a dry-run table.
func renderPlanInspect(p installPrinter, plan *reversibility.Plan, jsonOut bool, verb string) int {
	insp := plan.Inspect(context.Background())
	if jsonOut {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		out := struct {
			Verb  string            `json:"verb"`
			Order []string          `json:"order"`
			State map[string]string `json:"state"`
		}{Verb: verb, Order: insp.Order, State: make(map[string]string, len(insp.States))}
		for k, v := range insp.States {
			out.State[k] = v.String()
		}
		_ = enc.Encode(out)
		return 0
	}
	fmt.Fprintf(p.Out, "Dry-run for `slimference %s`:\n", verb)
	for _, name := range insp.Order {
		fmt.Fprintf(p.Out, "  [%-7s] %s\n", insp.States[name].String(), name)
	}
	return 0
}

// renderApplyResult prints each Step's apply outcome.
func renderApplyResult(p installPrinter, r reversibility.ApplyResult) {
	for _, step := range r.Applied {
		fmt.Fprintf(p.Out, "  [✓] %s\n", step)
	}
	for _, step := range r.RolledBack {
		fmt.Fprintf(p.Out, "  [↩] %s rolled back\n", step)
	}
	if r.Err != nil {
		fmt.Fprintf(p.Err, "  [✗] %v\n", r.Err)
	}
}

// renderReverseResult prints each Step's reverse outcome.
func renderReverseResult(p installPrinter, r reversibility.ReverseResult) {
	for _, step := range r.Reversed {
		fmt.Fprintf(p.Out, "  [✓] %s reverted\n", step)
	}
	for _, e := range r.Errors {
		fmt.Fprintf(p.Err, "  [✗] %v\n", e)
	}
}

// renderStatus prints SetupState as a colored-light table.
func renderStatus(p installPrinter, s control.SetupState) {
	mark := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	fmt.Fprintln(p.Out, "Slimference status")
	fmt.Fprintln(p.Out, "------------------")
	fmt.Fprintf(p.Out, "  CA       %s installed=%v in_keychain=%v fingerprint=%s\n",
		mark(s.CA.Installed), s.CA.Installed, s.CA.InKeychain, s.CA.Fingerprint)
	fmt.Fprintf(p.Out, "  Daemon   %s running=%v pid=%d health=%v\n",
		mark(s.Daemon.Running && s.Daemon.HealthOK), s.Daemon.Running, s.Daemon.PID, s.Daemon.HealthOK)
	if notice := staleSlimferenceProcessNoticeFn(); notice != "" {
		fmt.Fprintf(p.Out, "      stale process: %s\n", notice)
	}
	fmt.Fprintf(p.Out, "  Listener %s :443=%v :8443=%v :%d=%v\n",
		mark(s.Listener.BoundOn443 || s.Listener.BoundOnSNIPeek),
		s.Listener.BoundOn443, s.Listener.BoundOnSNIPeek, 8990, s.Listener.BoundOn8990)
	fmt.Fprintf(p.Out, "  Network  %s hosts_active=%v entries=%d\n",
		mark(s.NetworkRedir.HostsActive), s.NetworkRedir.HostsActive, len(s.NetworkRedir.HostsEntries))
	auto := s.CodexRoute.AutoTransport
	if auto == "" {
		auto = "unknown"
	}
	transport := s.CodexRoute.Transport
	if transport == "" {
		transport = "off"
	}
	fmt.Fprintf(p.Out, "  Codex    %s route_enabled=%v complete=%v transport=%s auto=%s wss_certified=%v daemon=%v\n",
		mark(s.CodexRoute.Complete && s.CodexRoute.DaemonReachable),
		s.CodexRoute.Enabled, s.CodexRoute.Complete, transport, auto,
		s.CodexRoute.WSSCertified, s.CodexRoute.DaemonReachable)
	if s.CodexRoute.FallbackReason != "" {
		fmt.Fprintf(p.Out, "      auto fallback: %s\n", s.CodexRoute.FallbackReason)
	}
	if s.CodexRoute.DaemonError != "" {
		fmt.Fprintf(p.Out, "      route daemon error: %s\n", s.CodexRoute.DaemonError)
	}
	fmt.Fprintln(p.Out, "  Apps")
	for _, a := range s.Apps {
		fmt.Fprintf(p.Out, "      %-20s enabled=%v detected=%v routed=%d bypassed=%d\n",
			a.ID, a.Enabled, a.Detected, a.Routed, a.Bypassed)
	}
	fmt.Fprintf(p.Out, "  Savings  output_tokens_saved=%d streamcut_fires=%d repdet=%d\n",
		s.Savings.OutputTokensSaved, s.Savings.StreamcutFires, s.Savings.RepdetRewrites)
	if len(s.Preflight.DoH) > 0 {
		fmt.Fprintln(p.Out, "  Preflight")
		allOK := true
		for _, e := range s.Preflight.DoH {
			if !e.OK {
				allOK = false
			}
			status := "ok"
			if !e.OK {
				status = "fail"
			}
			detail := e.IP
			if e.Error != "" {
				detail = e.Error
			}
			fmt.Fprintf(p.Out, "      doh %-16s %s %s\n", e.Host, status, detail)
		}
		if !allOK {
			fmt.Fprintln(p.Out, "      DoH preflight failed: do not live-arm Codex until upstream DNS works.")
		}
	}
	if s.NetworkRedir.HostsActive {
		fmt.Fprintln(p.Out, "")
		if s.Listener.BoundOn443 || s.Listener.BoundOnSNIPeek {
			fmt.Fprintln(p.Out, "Transparent MITM ARMED. Codex traffic flows through Slimference.")
		} else {
			fmt.Fprintln(p.Out, "Transparent MITM ROUTING ACTIVE, but no SNI listener is reachable.")
			fmt.Fprintln(p.Out, "Run `slimference root-disarm` from a recovery shell if Codex cannot connect.")
		}
	} else {
		fmt.Fprintln(p.Out, "")
		fmt.Fprintln(p.Out, "Transparent MITM DISARMED.")
		fmt.Fprintln(p.Out, "Scoped Codex CLI: `slimference codex run -- <prompt>`.")
		fmt.Fprintln(p.Out, "Scoped Codex CLI/App: `slimference enable`.")
		fmt.Fprintln(p.Out, "Global lab only: `lab cert-trust`, `lab root-arm --global-chatgpt-hosts`, then `lab enable`.")
	}
}

func addStatusPreflight(s *control.SetupState, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	checks := preflightUpstreamResolutionFn(ctx, install.DefaultHostsTargets())
	s.Preflight.DoH = make([]control.DoHPreflightEntry, 0, len(checks))
	for _, c := range checks {
		s.Preflight.DoH = append(s.Preflight.DoH, control.DoHPreflightEntry{
			Host:     c.Host,
			OK:       c.OK,
			IP:       c.IP,
			Loopback: c.Loopback,
			Error:    c.Error,
		})
	}
}

// enableDisableConfigPath returns the slimference config.toml path
// the running daemon will actually read. This must match the loader's
// precedence chain (env > XDG > legacy) so `slimference lab enable`
// writes to the exact file the daemon reads on SIGHUP. Falls back to the
// canonical XDG config path when no file exists yet.
func enableDisableConfigPath() string {
	if env := strings.TrimSpace(os.Getenv("SLIMFERENCE_CONFIG")); env != "" {
		return env
	}
	info := config.ResolveConfigPath(config.LoadOptions{})
	if info.Source == "xdg" && info.ResolvedPath != "" {
		return info.ResolvedPath
	}
	return config.XDGConfigPath()
}

var enableDisableConfigPathFn = enableDisableConfigPath

const installHelpText = `usage: slimference install [flags]

Atomic, reversible install. Performs:
  1. ca.generate     local CA under ~/.slimference/ca
  2. launchd.install ~/Library/LaunchAgents/com.slimference.proxy.plist
  3. hooks.codex     ~/.codex hook scripts + hooks.json entries

Keychain trust is NOT part of the default install. CLI WSS and the preferred
Codex Desktop app-server shim do not need it. CA trust stays available only for
Desktop/lab proxy diagnostics.

Claude Code is parked: install never writes ~/.claude or Claude hooks.

Does NOT touch /etc/hosts. Global transparent lab routing requires
` + "`slimference root-arm --global-chatgpt-hosts`" + ` explicitly.
Does NOT touch OPENAI_API_BASE or HTTPS_PROXY.

Flags:
  --dry-run         show what would happen without changing anything
  --json            machine-readable output (with --dry-run or --status)
  --no-hooks        skip the hook integrations
  --with-claude     accepted for old scripts; no-op while Claude is parked
  --no-autostart    skip the launchd plist install
  --with-keychain   opt into macOS Keychain trust for Desktop/lab fallback
  --no-keychain     accepted for old scripts; default install already skips Keychain
  --system          with --with-keychain, install CA into System Keychain
  --binary=PATH     explicit stable slimference binary for hooks and launchd
  --help, -h        this text
`

const uninstallHelpText = `usage: slimference uninstall [flags]

Reverses the install plan in LIFO order:
  1. hooks.codex reverted
  2. launchd unloaded + plist removed
  3. CA trust removed from Keychain when present
  4. CA material rotated aside to ~/.slimference/ca.bak.<unix>/

Claude Code is parked: uninstall never writes or removes ~/.claude.

Flags:
  --dry-run         show what would happen without changing anything
  --keep-ca         skip Keychain trust cleanup; CA material still rotates aside
  --no-keychain     skip Keychain trust cleanup
  --with-claude     accepted for old scripts; no-op while Claude is parked
  --system          uninstall from the system Keychain
  --binary=PATH     explicit stable slimference binary for plan resolution
  --json            machine-readable output (with --dry-run)
  --help, -h        this text
`

const enableDisableHelpText = `usage: slimference enable | disable [flags]

Enables / disables the scoped Codex CLI/App route in ~/.codex/config.toml.
This is the normal Codex-only path. It does not touch /etc/hosts, pfctl,
system proxy settings, ChatGPT.app, browser ChatGPT, or Claude Code.

Equivalent explicit commands:
  slimference codex enable [flags]
  slimference codex disable [flags]

Flags:
  --transport=http|wss  route transport (default http until live WSS certification)
  --host=HOST           Slimference daemon host (default 127.0.0.1)
  --port=PORT           Slimference daemon port (default 8990)
  --dry-run             show the config block without writing
  --help, -h            this text
`

const labEnableDisableHelpText = `usage: slimference lab enable | disable [flags]

Advanced/global lab only. Arms / disarms daemon-side SNI-peek mode by
writing cfg.Transparent.SNIPeekMode to the resolved config path and
signaling the daemon (SIGHUP). This alone still does not patch hosts.

If the daemon is not running, the flag is still written; the next
` + "`slimference service start`" + ` picks it up.

Full global transparent lab routing additionally requires:
  slimference lab cert-trust
  slimference lab root-arm --global-chatgpt-hosts

Flags:
  --config=PATH     override config.toml location
  --help, -h        this text
`

const statusHelpText = `usage: slimference status [flags]

Renders the current SetupState (CA, daemon, listener, network,
per-app routing, savings) as a table or JSON.

Flags:
  --json            machine-readable JSON output
  --preflight       run DoH upstream checks for Codex hosts
  --help, -h        this text
`
