package main

import "fmt"

// helpTopLevel returns the top-level usage banner. Kept under 50 lines so it
// stays scannable in a standard terminal window. Deeper detail lives in the
// per-subcommand help and in docs/documentation.md.
func helpTopLevel() string {
	return fmt.Sprintf(`slimference - Codex token-optimizing proxy (v%s)

USAGE:
  slimference                        Start TUI + proxy (requires TTY)
  slimference --no-tui               Run proxy foreground, no TUI
  slimference <subcommand> [args]    Run a subcommand
  slimference help [subcommand]      Show help for a subcommand

SUBCOMMANDS:
  doctor       Run diagnostics (config, ports, upstreams, CLI drift)
  filter       Layer-0 command filter (slimference filter -- <cmd>)
  install      Atomic Codex-only install (CA + launchd + Codex hooks)
  codex        Scoped Codex CLI/App routing (run|enable|disable|status)
  enable       Enable scoped Codex CLI/App route (alias: codex enable)
  disable      Disable scoped Codex CLI/App route (alias: codex disable)
  cert-trust   Open Keychain Access for the local CA trust step
  lab          Advanced global ChatGPT routing lab commands
  root-arm     Advanced global ChatGPT hosts + pfctl routing helper
  root-disarm  Remove global ChatGPT hosts + pfctl routing helper
  uninstall    Reverse the install plan
  status       Print install / daemon / routing state
  hook         Legacy hook install / remove / verify helpers
  rewrite      Rewrite captured output with the filter pipeline
  posttool     Codex PostToolUse hook entry point (stdin JSON)
  codexhook    Codex lifecycle hook entry points (stdin JSON)
  readhook     Codex Read-hook entry point (stdin JSON)
  expand       Retrieve an archived tool result by id
  expand-body  Retrieve one Go function/method body from an archived read
  checkpoint   Manage smart-compaction checkpoints
  gain         Report filter/cache/output/proxy token accounting
  plan         Dry-run cross-layer compression planner decisions
  quality      Print T77 quality signals (reread / cache spike / net savings)
  soak         T100b/T103c verdict from analytics+quality history
  stats        Print analytics snapshots (today|week|month|prompt-cache)
  debug        Decision-chain JSONL tools (paths|last|summary|tail|replay)
  service      Daemon lifecycle (install|uninstall|start|stop|status|logs)
  daemon       Run as a long-lived daemon (invoked by launchd/systemd)
  proxy        Legacy transparent/System-HTTPS-Proxy lifecycle and CLI env helpers
  integrate    Legacy advanced client wiring (status|install|remove|emergency-off)
  bypass       Toggle the master bypass flag (on|off|status)
  output-reduce Toggle T130 output-token discipline injection
  config       Config file tools (init|show)
  test         Upstream connectivity tests (minimax|anthropic|openai|intercept)
  completion   Emit shell completion script (bash)
  trust        Trust-model tools (from RTK port)
  version      Print version

GLOBAL FLAGS:
  --no-tui / --headless   Run proxy foreground, no BubbleTea UI
  --port <n>              Override listen port (default 8990)
  --no-layer1             Disable Layer 1 deterministic compression
  --no-layer2             Disable Layer 2 semantic summarisation
  --no-layer3             Disable Layer 3 response cache
  --sliding-window <n>    Override Layer 1 sliding window size
  --log-level <lvl>       debug | info | warn | error
  --help, -h, help        Show this help
  --version, -V           Print version

FIRST STEPS:
  1. slimference install      # Codex-only install, no hosts patch yet
  2. slimference status --preflight
  3. slimference codex run -- <prompt>
  4. slimference enable       # optional shared CLI/App route

GLOBAL LAB ONLY:
  slimference lab cert-trust
  slimference lab root-arm --global-chatgpt-hosts
  slimference lab enable

MORE:
  Config: ~/.config/slimference/config.toml (override via SLIMFERENCE_CONFIG)
  Install docs: docs/install.md
  Docs:   docs/documentation.md
  Spec:   spec+.md
`, version)
}

// helpForSubcommand returns per-subcommand usage text. Unknown topics fall
// back to the top-level banner with a short hint.
func helpForSubcommand(topic string) string {
	switch topic {
	case "doctor":
		return `slimference doctor

Run a quick diagnostic sweep. Checks: config file, listen port, upstream
reachability, analytics log directory, and Codex CLI version drift against
the supported range.

Exits 0 on all checks green, 1 on any fail.
`
	case "filter":
		return `slimference filter [--stream] [--] <cmd> [args...]

ADVANCED manual wrapper. Run <cmd> under the Layer-0 command filter.
Captures full stdout+stderr, applies the configured filter pipeline
(24 built-ins plus TOML rules), persists a row in filter.db, and prints
the filtered output. The raw output is kept in the tee directory for
recovery if the child fails.

This is not the scoped Codex traffic path. The normal CLI route is
slimference codex run -- <prompt>. Hooks may call this
wrapper internally, and humans can use it for manual diagnostics.

Flags:
  --stream             T94 streaming-aware mode: ANSI strip + dedup
                       consecutive identical lines on the fly. Suitable
                       for tail -f / docker logs --follow style inputs.

The child's exit code is propagated verbatim.

Examples:
  slimference filter -- git status --short
  slimference filter -- go test ./...
  slimference filter -- rg "TODO|FIXME" .
  slimference filter --stream -- docker logs --follow app
`
	case "hook":
		return `slimference hook <install|remove|verify|status|check-upstream> [codex]

	install   Write Codex hook wrappers (SHA-256 pinned).
	          For Codex, enables only hooks=true; does not patch base URLs.
	          Codex hooks default to silent mode; set SLIMFERENCE_CODEX_HOOK_MODE=compact
	          only when you explicitly want visible PostToolUse replacement blocks.
	          Use SLIMFERENCE_CODEX_HOOK_MODE=aggressive for PreToolUse block-and-rerun.
	remove    Remove the wrappers again.
	verify    Check hook checksums only. Codex config-patch state lives under integrate.
	status    Report installed / missing / drifted state.
check-upstream   Compare installed CLI version against the supported range.

Claude Code hooks are parked in Slimference. Use RTK for Claude Code.
`
	case "codex":
		return codexHelpText
	case "rewrite":
		return `slimference rewrite -- <cmd> [args...]

Pipe hook JSON on stdin with field "command", or pass the command after
'--'. Prints the rewritten command line. Used by Claude PreToolUse hooks.
Compound commands are rewritten segment-by-segment when safe, e.g.
'git add . && go test ./...' becomes two explicit slimference filter
invocations. Unsafe or excluded commands pass through with exit code 1.
`
	case "posttool":
		return `slimference posttool

Codex PostToolUse entry point. Reads hook JSON from stdin, compacts the
captured tool output, archives oversized results, and returns no visible output
by default. With SLIMFERENCE_CODEX_HOOK_MODE=compact or aggressive, returns
continue:false with compact feedback so Codex can replace the original result.
Non-zero exit only on hard I/O errors; business-level problems degrade to
passthrough.
`
	case "codexhook":
		return `slimference codexhook <session-start|permission-request|user-prompt-submit|posttool-timeout|stop>

Internal Codex lifecycle entry points installed by 'slimference hook install codex'.
SessionStart records state silently by default, PermissionRequest allow/deny uses
the same local shell policy as Layer 0, UserPromptSubmit records a turn boundary,
PostTool timeout records fail-open telemetry, and Stop emits valid no-op JSON
for checkpoint/debug continuity. Set
SLIMFERENCE_CODEX_HOOK_MODE=debug to emit SessionStart debug context.
`
	case "readhook":
		return `slimference readhook codex

Codex Read-hook entry point. Reads hook JSON on stdin, looks up the
file in the Read-cache, emits a delta-encoded patch when available, and
updates the cache.

Claude Code is parked. Slimference does not install or expose Claude hooks
in the active Codex-only product path.
`
	case "expand":
		return `slimference expand <id>

Retrieve the full body of an archived tool result. Id is printed next to
the preview in-context as 'slim://tool/<id>'. Prints to stdout.
`
	case "expand-body":
		return `slimference expand-body <id> <go-symbol>

Retrieve one Go function or method body from an archived file-read result.
Use this when an AST-compacted preview omitted a body. Symbols may be plain
functions (Run) or methods (Service.Run / (*Service).Run). Prints to stdout.
`
	case "checkpoint":
		return `slimference checkpoint <list|show|restore> [args]

Manage smart-compaction checkpoints. 'list' prints rankings,
'show <id>' prints the deterministic summary, 'restore <id>' emits the
full pre-compaction context for copy-paste.
`
	case "gain":
		return `slimference gain [today|week|month|all] [--by-command|--by-parser|--cache|--output|--proxy] [--csv] [--project <p>] [--json]

Aggregate Layer-0 filter.db rows into a savings report. --by-command
breaks down per parent command, --by-parser groups by parser/tool family,
--cache reports provider prompt-cache tokens, --output reports T130
output-reduce overhead/observed-output telemetry, --proxy reports decision-log
flight accounting for real proxied LLM requests, --csv prints CSV, --json prints
machine-readable output. Optional $/M-token rate in config multiplies savings.
`
	case "savings":
		return `slimference savings [today|week|month|all] [--json|--csv] [--project <p>]

Unified savings view (T80) collapsing Layer-0 filter.db, proxy-side
compression analytics, and Layer-3 cache hits into one canonical
report in tokens and (when configured) USD/EUR.
`
	case "compress-preview":
		return `slimference compress-preview [--provider X] [--path P] [--diff] [--json] [-|<file>]

Run the deterministic Layer-1 pipeline against a request body locally
without paying for an upstream call (T82). --provider auto-detects when
omitted; --diff renders a unified diff between original and rewritten
body; --json emits the full PreviewResult.
`
	case "watch":
		return `slimference watch [--once] [--interval=N] [--endpoint URL]

Live one-line ticker against /admin/status (T79). Defaults to local
daemon on the configured port. Ctrl-C / SIGTERM cancels the loop.
`
	case "quality":
		return `slimference quality [--json] [--url <base>]

Render the T77 quality signals exposed at /admin/status.quality:
re-read counter (per-session), prompt-cache miss-spike alarm,
net-savings ratio. --json passes the raw block through. --url
overrides the daemon endpoint (default: http://127.0.0.1:<port>).
`
	case "plan":
		return `slimference plan inspect [flags] [-|<request-file>]

Run the cross-layer compression planner without sending a request. The command
accepts provider/model/route/token/cache/WebSocket facts, estimates input
tokens from an optional file/stdin when --input-tokens is omitted, and prints
the deterministic per-layer plan. Use --json for machine-readable output.
`
	case "soak":
		return `slimference soak [today|week|month|all] [--json]

Walk daily analytics snapshots over the chosen window and emit a
verdict on whether [compression.tuning] coordinator_enabled (T100)
or tool_prune_enabled (T103) can be flipped on. Looks at error
rate, prompt-cache trend, MiniMax failure rate, and overflow
retries. --json prints the structured SoakReport for scripting.
`
	case "stats":
		return `slimference stats <today|week|month|prompt-cache [today|week|month|all]> [--json|--csv]

Print analytics snapshots. 'prompt-cache' reports per-day hit-rate over
the selected window.
`
	case "debug":
		return `slimference debug <paths|last|summary|tail|replay> [args]

paths              Show resolved config, filter.db, tee, analytics paths.
last               Last Layer-0 row from filter.db (--json).
summary <window>   Aggregate filter_runs for today|week|month|all.
tail <n>           Newest N filter.db rows (default 20, max 500, --json).
replay <path>      Replay a decision-chain JSONL session, break down per
                   request.
`
	case "service":
		return `slimference service <install|uninstall|start|stop|restart|status|logs>

macOS: manages the slimference.plist launchd user agent.
Linux: manages the user-scoped systemd unit (when available).
'logs' tails stderr / stdout via the platform log sink.
`
	case "daemon":
		return `slimference daemon

Invoked by the OS service supervisor (launchd/systemd). Runs the proxy
foreground with JSON logging and platform-specific integration. Users
should prefer 'slimference service <verb>' or '--no-tui' instead.
`
	case "start":
		return `slimference start

Start the local Slimference daemon in the background. This only starts
the admin/proxy process. Scoped Codex routing uses slimference enable.
Global transparent lab routing additionally requires lab cert-trust,
lab root-arm --global-chatgpt-hosts, and lab enable.
`
	case "stop":
		return `slimference stop

Stop the local Slimference daemon. Clean shutdown reverts daemon-managed
hosts state before the process exits.
`
	case "restart":
		return `slimference restart

Restart the local Slimference daemon. Does not change root-arm or
cert-trust state.
`
	case "proxy":
		return `slimference proxy <install|enable|disable|status|uninstall|env|run> [args]

LEGACY/ADVANCED for lifecycle commands. Do not use proxy install/enable as
the default Codex install path.
The current Codex path is:
  slimference install
  slimference status --preflight
  slimference codex run -- <prompt>
  slimference codex enable   # optional shared CLI/App route
  slimference uninstall

The proxy lifecycle keeps the older System-HTTPS-Proxy helpers for
diagnostics only. The per-process Codex runner is now the scoped CLI path when
ChatGPT.app and Browser ChatGPT must stay untouched.

Codex CLI launch helpers for T140 split testing:
  slimference proxy env codex --direct [-- <codex-args>...]
  slimference proxy env codex --proxied [-- <codex-args>...]
  slimference proxy env codex --proxied-wss [-- <codex-args>...]
  slimference proxy env codex --transparent-proxied [-- <codex-args>...]
  slimference proxy run codex --proxied [-- <codex-args>...]

The direct helper clears HTTP(S)/ALL proxy env and sets NO_PROXY=*.
The proxied helper leaves the macOS System HTTPS proxy untouched and launches
Codex with a per-process custom provider named slimference-codex pointing at
the local daemon. --proxied disables Responses WebSockets so the CLI uses
HTTP directly. --proxied-wss enables Responses WebSockets for scoped WSS
certification and Phase-F frame mutation. The transparent-proxied helper is
the CONNECT/MITM variant for explicit CA-path tests. proxy env prints the exact
shell command. proxy run executes Codex directly with the same one-process
environment. Neither mode mutates Codex config.
`
	case "config":
		return `slimference config <init|show>

init   Write a default config.toml to ~/.config/slimference/config.toml (respects
       SLIMFERENCE_CONFIG).
show   Print the resolved effective config (TOML + ENV merged).
`
	case "test":
		return `slimference test <minimax|anthropic|openai|intercept>

Run a live connectivity test against the named upstream. 'intercept' runs
a transient in-process proxy to validate the full pipeline path.
`
	case "completion":
		return `slimference completion bash

Emit a bash completion script. Pipe to your completion dir.
`
	case "trust":
		return `slimference trust <subcmd>

Tools around the trust model ported from RTK. See docs/rtk-parity.md.
`
	case "integrate":
		return `slimference integrate <status|install|remove|emergency-off> [flags]

LEGACY/ADVANCED config-patch mode. Install can write Codex base URL fields
into ~/.codex/config.toml, then optionally install Codex hooks. This is kept
for manual fallback only.

The Phase H default Codex path is handled by:
  slimference install
  slimference codex run -- <prompt>
  slimference codex enable   # optional shared CLI/App route

The one-shot run path does not mutate Codex base URLs. The optional shared
route writes a separate marker-owned provider block. Every integrate edit uses
a fenced marker block so re-running install is a no-op and remove is exact.

Claude Code is parked: integrate no longer writes ANTHROPIC_BASE_URL or
~/.claude hooks. Use RTK for Claude Code.

Flags:
  --dry-run            Print intended writes without touching anything.
  --client <name>      Narrow to codex | daemon | all. claude is accepted as a parked no-op.
  --json               Emit machine-readable JSON.
  --no-hook            Skip hook install/remove (config only).
  --proxy-url <url>    Override http://127.0.0.1:8990.
  --force              Re-apply blocks even if already present (self-heal).

Verbs:
  status           Report per-client wiring state.
  install          Idempotent wire-up.
  remove           Clean tear-down (undo install).
  emergency-off    Panic button: unwire everything + stop the daemon.

See docs/integration.md for the failure-mode matrix.
`
	case "bypass":
		return `slimference bypass <on|off|status> [--duration=Ns|--next-request[=N]]

Toggle the master bypass flag on the running daemon (T67). When on, the
proxy keeps accepting connections but forwards traffic byte-equal with
zero compression — useful when a request feels off and you want to rule
Slimference out instantly without uninstalling anything. Hot-reload; no
shell or client restart needed. Requires the daemon to be running.

Optional T81 scoped flags auto-revert:
  --duration=30s|10m|1h    Revert after the duration elapses.
  --next-request[=N]       Revert after the next N requests (default 1).
`
	case "version":
		return fmt.Sprintf("slimference v%s\n", version)
	}
	return helpTopLevel()
}
