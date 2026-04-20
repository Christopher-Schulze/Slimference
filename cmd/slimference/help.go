package main

import "fmt"

// helpTopLevel returns the top-level usage banner. Kept under 50 lines so it
// stays scannable in a standard terminal window. Deeper detail lives in the
// per-subcommand help and in docs/documentation.md.
func helpTopLevel() string {
	return fmt.Sprintf(`slimference - Claude/Codex token-optimizing proxy (v%s)

USAGE:
  slimference                        Start TUI + proxy (requires TTY)
  slimference --no-tui               Run proxy foreground, no TUI
  slimference <subcommand> [args]    Run a subcommand
  slimference help [subcommand]      Show help for a subcommand

SUBCOMMANDS:
  doctor       Run diagnostics (config, ports, upstreams, CLI drift)
  filter       Layer-0 command filter (slimference filter -- <cmd>)
  hook         Install / remove / verify Claude and Codex hooks
  rewrite      Rewrite captured output with the filter pipeline
  posttool     Codex PostToolUse hook entry point (stdin JSON)
  readhook     Claude Read-hook entry point (stdin JSON)
  expand       Retrieve an archived tool result by id
  checkpoint   Manage smart-compaction checkpoints
  gain         Report Layer-0 filter token-savings
  stats        Print analytics snapshots (today|week|month|prompt-cache)
  debug        Decision-chain JSONL tools (paths|last|summary|tail|replay)
  service      Daemon lifecycle (install|uninstall|start|stop|status|logs)
  daemon       Run as a long-lived daemon (invoked by launchd/systemd)
  integrate    Wire Claude Code and Codex to this proxy (status|install|remove|emergency-off)
  bypass       Toggle the master bypass flag (on|off|status)
  config       Config file tools (init|show)
  test         Upstream connectivity tests (minimax|anthropic|openai|intercept)
  completion   Emit shell completion script (bash)
  trust        Trust-model tools (from RTK port)
  version      Print version

GLOBAL FLAGS:
  --no-tui / --headless   Run proxy foreground, no BubbleTea UI
  --port <n>              Override listen port (default 8990)
  --no-layer1             Disable Layer 1 deterministic compression
  --no-layer2             Disable Layer 2 MiniMax summarisation
  --no-layer3             Disable Layer 3 response cache
  --sliding-window <n>    Override Layer 1 sliding window size
  --log-level <lvl>       debug | info | warn | error
  --help, -h, help        Show this help
  --version, -V           Print version

FIRST STEPS:
  1. slimference doctor         # verify config, ports, upstreams
  2. slimference hook install   # wire Claude and Codex hooks
  3. slimference service install (macOS)

MORE:
  Config: ~/.slimference/config.toml (override via SLIMFERENCE_CONFIG)
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

Run a quick diagnostic sweep. Checks: config file, listen port, MiniMax API
key, Anthropic/OpenAI upstream reachability, analytics log directory, and
Claude/Codex CLI version drift against the supported range.

Exits 0 on all checks green, 1 on any fail.
`
	case "filter":
		return `slimference filter -- <cmd> [args...]

Run <cmd> under the Layer-0 command filter. Captures full stdout+stderr,
applies the configured filter pipeline (24 built-ins plus TOML rules),
persists a row in filter.db, and prints the filtered output. The raw
output is kept in the tee directory for recovery if the filter fails.

Flags (before the double dash):
  --pipeline <name>    Use a named pipeline from config instead of defaults
  --project <dir>      Tag the row with a project label
  --dry-run            Run the command, emit filter stats, do not mutate output

The child's exit code is propagated verbatim.
`
	case "hook":
		return `slimference hook <install|remove|verify|status|check-upstream> [claude|codex]

install   Write Claude Code and/or Codex hook wrappers (SHA-256 pinned).
remove    Remove the wrappers again.
verify    Check checksums against what was installed.
status    Report installed / missing / drifted state.
check-upstream   Compare installed CLI version against the supported range.
`
	case "rewrite":
		return `slimference rewrite -- <cmd> [args...]

Pipe hook JSON on stdin with field "command", or pass the command after
'--'. Prints the rewritten command line. Used by Claude PreToolUse hooks.
`
	case "posttool":
		return `slimference posttool

Codex PostToolUse entry point. Reads hook JSON from stdin, compacts the
captured tool output, optionally archives oversized results, and prints a
compact additionalContext block for Codex to inject. Non-zero exit only on
hard I/O errors; business-level problems degrade to passthrough.
`
	case "readhook":
		return `slimference readhook

Claude Read-hook entry point. Reads hook JSON on stdin, looks up the
file in the Read-cache, emits a delta-encoded patch when available, and
updates the cache.
`
	case "expand":
		return `slimference expand <id>

Retrieve the full body of an archived tool result. Id is printed next to
the preview in-context as 'slim://tool/<id>'. Prints to stdout.
`
	case "checkpoint":
		return `slimference checkpoint <list|show|restore> [args]

Manage smart-compaction checkpoints. 'list' prints rankings,
'show <id>' prints the deterministic summary, 'restore <id>' emits the
full pre-compaction context for copy-paste.
`
	case "gain":
		return `slimference gain [today|week|month|all] [--by-command] [--csv] [--project <p>] [--json]

Aggregate Layer-0 filter.db rows into a savings report. --by-command
breaks down per parent command, --csv prints CSV, --json prints machine-
readable output. Optional $/M-token rate in config multiplies savings.
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
	case "config":
		return `slimference config <init|show>

init   Write a default config.toml to ~/.slimference/config.toml (respects
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

Wire Claude Code and Codex to run through this proxy. Install writes
ANTHROPIC_BASE_URL into your shell rc and openai_base_url +
chatgpt_base_url into ~/.codex/config.toml, installs both hooks, and
reports the resulting state. Every edit uses a fenced marker block so
re-running install is a no-op and remove is exact.

Flags:
  --dry-run            Print intended writes without touching anything.
  --client <name>      Narrow to claude | codex | daemon | all (default all).
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
		return `slimference bypass <on|off|status>

Toggle the master bypass flag on the running daemon. When on, the proxy
keeps accepting connections but forwards traffic byte-equal with zero
compression - useful when a request feels off and you want to rule
Slimference out instantly without uninstalling anything. Hot-reload;
no shell or client restart needed. Requires the daemon to be running.
`
	case "version":
		return fmt.Sprintf("slimference v%s\n", version)
	}
	return helpTopLevel()
}
