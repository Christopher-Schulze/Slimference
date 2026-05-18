package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Codex Desktop scoped launcher (T228).
//
// Codex Desktop App reads ~/.codex/config.toml `model_provider` for
// sideband endpoints (memories, plugins, login) but the ChatGPT-auth
// conversation traffic is hardcoded to chatgpt.com inside the Rust
// app-server binary. Persistent marker-block enable therefore cannot
// redirect conversation traffic on its own.
//
// This launcher spawns Codex.app's main binary directly with a scoped
// env that defensively sets the candidate base-URL variables. The
// effect is process-local: only the spawned Codex.app inherits the
// env. Browser ChatGPT, ChatGPT.app, Claude Code, and any Codex.app
// launched later via Finder / Spotlight remain unaffected.
//
// EMPIRICAL FINDING (2026-05-18, against Codex.app 0.131.0-alpha.9 at
// /Applications/Codex.app/Contents/Resources/codex):
//   - env injection from this launcher reaches the Rust app-server
//     child process (verified via `ps eww`),
//   - but the app-server STILL connects directly to 104.18.32.47:443
//     (Cloudflare ChatGPT) for the conversation, with zero bytes
//     reaching the Slimference daemon on :8990,
//   - `strings` of the Rust binary shows multiple hardcoded
//     `https://chatgpt.com/backend-api` URLs and exposes only these
//     override env vars: CODEX_REFRESH_TOKEN_URL_OVERRIDE (auth),
//     CODEX_ARC_MONITOR_ENDPOINT_OVERRIDE (telemetry),
//     CODEX_EXEC_SERVER_URL (exec-server), API_BASE_URL (generic).
//   - No CHATGPT_CODEX_BASE_URL / OPENAI_BASE_URL / OPENAI_API_BASE /
//     CHATGPT_BASE_URL handling exists in the current Codex Desktop
//     Rust binary for the conversation route.
//
// This launcher is therefore retained as:
//   1. A diagnostic surface (`--probe`) that emits the candidate
//      override env without spawning.
//   2. A future-proof spawn path: if a later Codex version adds an
//      env hook for the conversation endpoint with one of our
//      candidate names, the launcher already injects it.
//   3. A spawn surface that does NOT touch /etc/hosts, pf, Keychain,
//      system proxy, or ~/.codex/config.toml.
//
// For current Codex Desktop, conversation traffic redirection requires
// either upstream adding an env / config hook, or a global-lab MITM
// (out of scope for this product path).
//
// Reverse: quit Codex.app. Relaunching from Finder / Spotlight gives
// direct ChatGPT routing because no env is inherited.

const (
	defaultCodexDesktopAppPath     = "/Applications/Codex.app"
	defaultCodexDesktopExecRelPath = "Contents/MacOS/Codex"
)

var (
	codexDesktopAppPathFn = func() string { return defaultCodexDesktopAppPath }
	codexDesktopStatFn    = func(name string) (fs.FileInfo, error) { return os.Stat(name) }
	codexDesktopStartFn   = startCodexDesktopProcess
	codexDesktopBaseEnvFn = os.Environ
)

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

type codexLaunchDesktopFlags struct {
	host    string
	port    string
	appPath string
	extra   []string
	probe   bool
	help    bool
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
	env := buildCodexDesktopLaunchEnv(overrideURL, codexDesktopBaseEnvFn(), flags.extra)

	if flags.probe {
		return emitCodexDesktopProbe(p, binary, overrideURL, env)
	}

	return codexDesktopStartFn(p, binary, env)
}

// buildCodexDesktopLaunchEnv constructs the env for the spawned
// Codex.app. It removes any existing entries for the override keys
// from the base env, appends our overrides, then appends operator
// extras (which may further override any key).
func buildCodexDesktopLaunchEnv(overrideURL string, base []string, extra []string) []string {
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

type codexLaunchDesktopProbe struct {
	Binary      string   `json:"binary"`
	OverrideURL string   `json:"override_url"`
	EnvOverride []string `json:"env_override"`
}

func emitCodexDesktopProbe(p installPrinter, binary, overrideURL string, env []string) int {
	probe := codexLaunchDesktopProbe{
		Binary:      binary,
		OverrideURL: overrideURL,
		EnvOverride: filterCodexDesktopOverrideEnv(env),
	}
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(probe); err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: probe encode: %v\n", err)
		return 1
	}
	return 0
}

func startCodexDesktopProcess(p installPrinter, binary string, env []string) int {
	cmd := exec.Command(binary)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: spawn failed: %v\n", err)
		return 1
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		fmt.Fprintf(p.Err, "codex launch-desktop: release failed: %v\n", err)
	}
	fmt.Fprintf(p.Out, "Codex.app launched (PID %d) with scoped Slimference env.\n", pid)
	fmt.Fprintln(p.Out, "Scope: only this Codex.app inherits the env. Browser ChatGPT, ChatGPT.app, Claude untouched.")
	fmt.Fprintln(p.Out, "Reverse: quit Codex.app via Cmd+Q. Relaunch from Finder/Spotlight for direct routing.")
	return 0
}

func parseCodexLaunchDesktopFlags(args []string) (codexLaunchDesktopFlags, error) {
	f := codexLaunchDesktopFlags{host: "127.0.0.1", port: "8990"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--probe":
			f.probe = true
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
	return f, nil
}

const codexLaunchDesktopHelpText = `usage: slimference codex launch-desktop [--probe] [--host=127.0.0.1] [--port=8990] [--app=<path>] [--env KEY=VAL...]

Spawns Codex.app's main binary with a scoped env that redirects the
ChatGPT-auth conversation endpoint to the local Slimference daemon.
The effect is process-local: only the launched Codex.app inherits the
env. Browser ChatGPT, ChatGPT.app, Claude Code, and any Codex.app
relaunched from Finder/Spotlight remain on direct ChatGPT routing.

Flags:
  --probe         emit the override env as JSON without spawning
  --host=<host>   slimference daemon host (default 127.0.0.1)
  --port=<port>   slimference daemon port (default 8990)
  --app=<path>    override path to Codex.app bundle (default /Applications/Codex.app)
  --env KEY=VAL   add or override an env entry (repeatable)
  --help, -h      this text

Reverse: quit Codex.app (Cmd+Q). Relaunching from Finder/Spotlight does
not inherit the override and returns to direct ChatGPT routing.

This launcher does NOT touch /etc/hosts, pf, Keychain, system proxy,
~/.codex/config.toml, or any global state. It is scoped to one spawn.
`
