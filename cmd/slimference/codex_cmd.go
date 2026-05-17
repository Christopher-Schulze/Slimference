package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/slimference/slimference/internal/codexroute"
	"github.com/slimference/slimference/internal/tlsca"
	"github.com/slimference/slimference/internal/transparent"
)

var (
	codexRouteHomeFn    = os.UserHomeDir
	codexRouteEnableFn  = codexroute.Enable
	codexRouteDisableFn = codexroute.Disable
	codexRouteInspectFn = codexroute.Inspect
	codexRouteHealthFn  = defaultProxyHealthCheck
	codexProxyRunFn     = proxyRun
)

type codexRouteFlags struct {
	host   string
	port   string
	json   bool
	dryRun bool
	help   bool
}

func handleCodexCmd(args []string) {
	exitFn(runCodexCmd(args, defaultInstallPrinter()))
}

func runCodexCmd(args []string, p installPrinter) int {
	if len(args) == 0 {
		fmt.Fprint(p.Out, codexHelpText)
		return 0
	}
	switch args[0] {
	case "run":
		return runCodexRunCmd(args[1:], p)
	case "enable":
		return runCodexEnableCmd(args[1:], p)
	case "disable":
		return runCodexDisableCmd(args[1:], p)
	case "status":
		return runCodexStatusCmd(args[1:], p)
	case "--help", "-h", "help":
		fmt.Fprint(p.Out, codexHelpText)
		return 0
	default:
		fmt.Fprintf(p.Err, "codex: unknown subcommand %q\n", args[0])
		fmt.Fprint(p.Err, codexUsageLine)
		return 2
	}
}

func runCodexRunCmd(args []string, p installPrinter) int {
	flags, direct, codexArgs := parseCodexRunFlags(args)
	if flags.help {
		fmt.Fprint(p.Out, codexRunHelpText)
		return 0
	}
	mode := "proxied"
	if direct {
		mode = "direct"
	} else if err := codexRouteHealthFn(flags.host, flags.port); err != nil {
		fmt.Fprintf(p.Err, "codex run: Slimference daemon unreachable at %s:%s (%v); falling back to direct Codex.\n",
			flags.host, flags.port, err)
		mode = "direct"
	}
	proxyArgs := []string{"run", "codex", "--" + mode, "--host=" + flags.host, "--port=" + flags.port}
	if len(codexArgs) > 0 {
		proxyArgs = append(proxyArgs, "--")
		proxyArgs = append(proxyArgs, codexArgs...)
	}
	return codexProxyRunFn(proxyArgs, codexProxyEnv(p))
}

func runCodexEnableCmd(args []string, p installPrinter) int {
	flags, err := parseCodexRouteFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex enable: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexEnableHelpText)
		return 0
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex enable: HOME unresolved")
		return 1
	}
	proxyURL := codexroute.ProxyURL(flags.host, flags.port)
	if flags.dryRun {
		fmt.Fprintf(p.Out, "Dry-run: would write scoped Codex route to %s\n\n%s",
			codexroute.ConfigPath(home), codexroute.PreviewBlock(proxyURL))
		return 0
	}
	evt, err := codexRouteEnableFn(home, proxyURL)
	if err != nil {
		fmt.Fprintf(p.Err, "codex enable: %v\n", err)
		return 1
	}
	if evt.Action == "skipped_codex_config_absent" {
		fmt.Fprintf(p.Err, "codex enable: %s does not exist; run Codex once or `slimference install` first. No files changed.\n", evt.Path)
		return 1
	}
	fmt.Fprintf(p.Out, "Codex route enabled: %s (%s)\n", evt.Path, evt.Action)
	fmt.Fprintln(p.Out, "Scope: Codex CLI + Codex Desktop App only. Browser ChatGPT and ChatGPT.app stay direct.")
	fmt.Fprintln(p.Out, "Desktop App: restart Codex.app/app-server so it reloads ~/.codex/config.toml.")
	return 0
}

func runCodexDisableCmd(args []string, p installPrinter) int {
	flags, err := parseCodexRouteFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex disable: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexDisableHelpText)
		return 0
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex disable: HOME unresolved")
		return 1
	}
	if flags.dryRun {
		fmt.Fprintf(p.Out, "Dry-run: would remove scoped Codex route from %s\n", codexroute.ConfigPath(home))
		return 0
	}
	evt, err := codexRouteDisableFn(home)
	if err != nil {
		fmt.Fprintf(p.Err, "codex disable: %v\n", err)
		return 1
	}
	fmt.Fprintf(p.Out, "Codex route disabled: %s (%s)\n", evt.Path, evt.Action)
	fmt.Fprintln(p.Out, "Codex now talks direct after the next CLI/App config reload.")
	return 0
}

func runCodexStatusCmd(args []string, p installPrinter) int {
	flags, err := parseCodexRouteFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex status: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexStatusHelpText)
		return 0
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex status: HOME unresolved")
		return 1
	}
	proxyURL := codexroute.ProxyURL(flags.host, flags.port)
	status, err := codexRouteInspectFn(home, proxyURL)
	if err != nil {
		fmt.Fprintf(p.Err, "codex status: %v\n", err)
		return 1
	}
	healthErr := codexRouteHealthFn(flags.host, flags.port)
	out := struct {
		Route  codexroute.Status `json:"route"`
		Daemon struct {
			Reachable bool   `json:"reachable"`
			Error     string `json:"error,omitempty"`
		} `json:"daemon"`
	}{Route: status}
	out.Daemon.Reachable = healthErr == nil
	if healthErr != nil {
		out.Daemon.Error = healthErr.Error()
	}
	if flags.json {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	renderCodexStatus(p.Out, out.Route, out.Daemon.Reachable, out.Daemon.Error)
	return 0
}

func parseCodexRouteFlags(args []string) (codexRouteFlags, error) {
	f := codexRouteFlags{host: "127.0.0.1", port: "8990"}
	for _, a := range args {
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--json":
			f.json = true
		case a == "--dry-run":
			f.dryRun = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		default:
			return f, fmt.Errorf("unknown flag %q", a)
		}
	}
	return f, nil
}

func parseCodexRunFlags(args []string) (codexRouteFlags, bool, []string) {
	f := codexRouteFlags{host: "127.0.0.1", port: "8990"}
	direct := false
	var codexArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			codexArgs = append(codexArgs, args[i+1:]...)
			return f, direct, codexArgs
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--direct":
			direct = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		default:
			codexArgs = append(codexArgs, a)
		}
	}
	return f, direct, codexArgs
}

func codexProxyEnv(p installPrinter) proxyEnv {
	home := os.Getenv("HOME")
	caDir := filepath.Join(home, ".slimference")
	return proxyEnv{
		Stdout:      p.Out,
		Stderr:      p.Err,
		Stdin:       os.Stdin,
		Home:        home,
		CADirFn:     func() string { return caDir },
		Network:     transparent.NewManager(),
		Keychain:    transparent.NewKeychain(),
		Launch:      transparent.NewLaunchAgent(),
		LoadCA:      tlsca.LoadOrGenerateCA,
		HealthCheck: defaultProxyHealthCheck,
		RunCommand:  defaultProxyCommandRunner,
	}
}

func renderCodexStatus(w io.Writer, s codexroute.Status, daemonReachable bool, daemonErr string) {
	fmt.Fprintln(w, "Slimference Codex route")
	fmt.Fprintln(w, "-----------------------")
	fmt.Fprintf(w, "  Config   exists=%v enabled=%v complete=%v\n", s.Exists, s.Enabled, s.Complete)
	fmt.Fprintf(w, "  Path     %s\n", s.Path)
	fmt.Fprintf(w, "  BaseURL  %s\n", s.BaseURL)
	fmt.Fprintf(w, "  Daemon   reachable=%v\n", daemonReachable)
	if daemonErr != "" {
		fmt.Fprintf(w, "           %s\n", daemonErr)
	}
	if s.Conflict != "" {
		fmt.Fprintf(w, "  Conflict %s\n", s.Conflict)
	}
	if s.LegacyKeys {
		fmt.Fprintln(w, "  Legacy   openai_base_url/chatgpt_base_url present outside scoped route")
	}
	fmt.Fprintln(w)
	if s.Complete && daemonReachable {
		fmt.Fprintln(w, "Codex CLI/App route is ready after Codex reload.")
	} else if s.Enabled && !daemonReachable {
		fmt.Fprintln(w, "Route is configured but daemon is unreachable. Run `slimference codex disable` for direct fallback.")
	} else {
		fmt.Fprintln(w, "Route is disabled. Use `slimference codex run -- <prompt>` for one-shot scoped CLI, or `slimference codex enable` for CLI/App.")
	}
}

const codexUsageLine = "usage: slimference codex <run|enable|disable|status> [flags]\n"

const codexHelpText = `usage: slimference codex <run|enable|disable|status> [flags]

Codex-scoped routing. This is the product path: it only touches Codex
CLI / Codex Desktop App config and never routes Browser ChatGPT,
ChatGPT.app, Claude Code, or generic OpenAI tools through Slimference.

Commands:
  run       run one Codex CLI process through Slimference; fail-open direct
  enable    persist the shared Codex CLI/App provider route
  disable   remove the shared Codex CLI/App provider route
  status    show route config + daemon health
`

const codexRunHelpText = `usage: slimference codex run [--direct] [--host=127.0.0.1] [--port=8990] [-- <codex-args>...]

Runs one Codex CLI process. Default mode health-checks Slimference and
uses the scoped provider override. If the daemon is unreachable it
falls back to direct Codex automatically.
`

const codexEnableHelpText = `usage: slimference codex enable [--host=127.0.0.1] [--port=8990] [--dry-run]

Writes a marker-owned provider block into ~/.codex/config.toml:
model_provider="slimference-codex", base_url=http://127.0.0.1:8990/backend-api/codex,
requires_openai_auth=true, supports_websockets=false, wire_api="responses".

This is scoped to Codex. It does not touch /etc/hosts, pf, system proxy,
Browser ChatGPT, ChatGPT.app, Claude Code, or generic OpenAI clients.
`

const codexDisableHelpText = `usage: slimference codex disable [--dry-run]

Removes the marker-owned provider block from ~/.codex/config.toml.
`

const codexStatusHelpText = `usage: slimference codex status [--json] [--host=127.0.0.1] [--port=8990]

Shows whether the scoped Codex provider route is configured and whether
the Slimference daemon is reachable.
`
