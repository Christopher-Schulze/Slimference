# T43 - CLI `--help`, Subcommand-Help, Onboarding Discovery

Status: todo
Priority: P0
Scope: `cmd/slimference/main.go`, `cmd/slimference/help.go` (new), `README.md`, `docs/documentation.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

Running `./slimference --help` (or `-h`, or `help`) currently tries to open
the TUI, which fails on a non-TTY with:

```
TUI error: could not open a new TTY: open /dev/tty: device not configured
```

For every new user this is the first impression: the tool looks broken.
There is no discoverable list of subcommands, flags, or a pointer to
`slimference doctor`. This is a product-UX failure, not a code bug, but it
gates any friction-less onboarding.

## Current State

- `cmd/slimference/main.go` routes `os.Args[1]` to subcommand handlers but
  falls through to `startTUI()` on no-arg / unknown-arg / help-flag.
- No `--help` / `-h` / `help` handling at the top level.
- Subcommands (`filter`, `hook`, `rewrite`, `gain`, `debug`, `service`,
  `doctor`, `expand`, `posttool`, `readhook`, `version`) have inline usage
  strings but are not listed anywhere centrally.
- README has a basic section but no reference table.

## Target State

- `slimference --help`, `slimference -h`, `slimference help` and
  `slimference help <subcmd>` all produce help text on **stdout**, exit 0,
  never touch the TUI.
- Top-level help lists every subcommand with a one-line summary and points
  to `slimference doctor`, the config path, and the spec.
- Each subcommand supports `<subcmd> --help` rendering its flags,
  arguments, exit codes, and examples.
- `slimference` with no args on a TTY still starts the TUI (unchanged); on
  a non-TTY it prints help and exits 2 with a hint to use `--no-tui`
  (covered in T44).
- Help text is rendered without colour by default; `--color=auto|always|never`
  flag honours env `NO_COLOR`.

## Design

### Top-level help (sample output)

```
slimference - Claude/Codex token-optimizing proxy

USAGE:
  slimference [flags]                start TUI (default)
  slimference <subcommand> [flags]
  slimference help [subcommand]

SUBCOMMANDS:
  doctor       Run diagnostics (config, ports, upstreams, CLI drift)
  filter       Layer-0 command filter: slimference filter -- <cmd>
  hook         Install / remove / verify Claude & Codex hooks
  rewrite      Rewrite captured output with a filter pipeline
  posttool     Codex PostToolUse entry point (stdin JSON)
  readhook     Claude Read-hook entry point
  expand       Retrieve archived tool result by ID
  gain         Report token-savings gains (JSON / Markdown)
  debug        Decision-chain JSONL tools (last/tail/summary/replay/paths)
  service      Daemon lifecycle (install/uninstall/start/stop/status/logs)
  version      Print build version and Git SHA

GLOBAL FLAGS:
  --config <path>     Config TOML path (default $XDG_CONFIG_HOME/slimference/config.toml)
  --no-tui            Run proxy foreground, no BubbleTea UI
  --log-level <lvl>   debug|info|warn|error (default info)
  --color <mode>      auto|always|never (default auto)
  -h, --help          Show help
  -V, --version       Show version

FIRST STEPS:
  1. slimference doctor          # verify your environment
  2. slimference hook install    # wire Claude & Codex
  3. slimference service install # macOS launchd daemon

MORE:
  Spec:       spec+.md
  Config:     ~/.slimference/config.toml
  Docs:       docs/documentation.md
```

### Code layout

New file `cmd/slimference/help.go` with:
- `topLevelHelp() string`
- `subcommandHelp(name string) string`
- `exitCode` constants

`main.go` early dispatch:

```go
if wantsHelp(args) {
    printHelp(args); os.Exit(0)
}
if wantsVersion(args) {
    printVersion(); os.Exit(0)
}
```

### TTY detection

Re-use `term.IsTerminal(int(os.Stdout.Fd()))`. On no-TTY + no args, print
help and exit 2 (rather than crashing TUI).

## Implementation Plan

### WP1 - Help framework
- Add `help.go` with top-level + per-subcommand strings.
- Pull per-subcommand usage from existing inline strings (DRY).

### WP2 - Argument parser shim
- Tiny flag parser that recognises `--help`, `-h`, `help`, `--version`, `-V`
  before subcommand dispatch.

### WP3 - TTY fallback
- On no-TTY without args, print help + hint, exit 2.

### WP4 - Subcommand-help parity
- Every subcommand's `<subcmd> --help` shows flags, args, examples, exit
  codes. Audit all 11 subcommands.

### WP5 - README + docs
- Rewrite README "Quick Start" to match the help output.
- `docs/documentation.md` § Getting Started updated.

### WP6 - Tests
- Golden-file test per help string (`testdata/help/top.txt`, `filter.txt`,
  etc.). Diff-check on CI.

---

## Subtasks

- [ ] Create `cmd/slimference/help.go` with topLevelHelp + perSubcommandHelp.
- [ ] Early-dispatch `--help`/`-h`/`help` before TUI/subcommand routing.
- [ ] Early-dispatch `--version`/`-V`.
- [ ] Non-TTY + no args prints help, exits 2, hints `--no-tui`.
- [ ] Ensure every subcommand has `<subcmd> --help`.
- [ ] Golden-file tests for help output.
- [ ] Rewrite README Quick-Start.
- [ ] Update `docs/documentation.md` § Getting Started.
- [ ] Add `--color` flag plumbed through to slog console formatter.

## Risks

- Help text rot: add golden test so text stays in sync.
- Over-verbose help alienates experienced users: keep top-level ≤ 40 lines,
  subcommand ≤ 25 lines, link to spec for depth.

## Acceptance Criteria

- [ ] `slimference --help` prints usage, exit 0.
- [ ] `slimference help doctor` prints doctor-specific usage, exit 0.
- [ ] `slimference` on a non-TTY prints help and exits 2.
- [ ] `slimference` on a TTY still starts TUI (unchanged).
- [ ] README Quick-Start matches `slimference --help`.
- [ ] Golden-file tests green, `go test ./...` green.

## Out of Scope

- Completion generation (covered by T32 / future zsh-fish work).
- Internationalisation of help text (English only per repo rules).

---

## Validation

```
./slimference --help
./slimference -h
./slimference help filter
./slimference --version
echo | ./slimference     # non-TTY, expect help + exit 2
go test ./cmd/slimference/...
```
