# Slimference

Slimference is a local Go proxy and tool-output compressor for coding agents like Claude Code and Codex.

It reduces token waste in two places:

- Layer 0: before shell output ever enters the conversation
- Layers 1-3: after conversation history already exists, directly on API requests

The result is longer working sessions, less repeated context, and better visibility into what is being compressed.

## What It Does

Slimference combines three runtime surfaces:

- A local HTTP reverse proxy for Anthropic and OpenAI-compatible chat requests
- A Layer 0 CLI filter that compacts tool output before it reaches the model
- A Bubble Tea TUI for monitoring and operational control

In practice that means:

- old conversation history gets compacted before upstream requests are sent
- repeated or noisy tool output gets collapsed
- build, test, lint, git, search, JSON, logs, and file reads get specialized compression
- analytics, logs, and recent request activity are stored locally
- the daemon can run permanently in the background, while the TUI acts as a control window

## Architecture

Slimference is split into four effective layers:

1. Layer 0: CLI pre-entry filtering
   Shell commands are intercepted through hooks and compacted before their output is added to chat history.

2. Layer 1: deterministic compression
   Fast synchronous Go transforms like ANSI stripping, JSON compaction, deduplication, structure extraction, delta encoding, repeated tool collapse, and more.

3. Layer 2: MiniMax summarization
   Older conversation regions can be summarized asynchronously and reused from cache on later requests.

4. Layer 3: response caching
   Safe requests can be served from cache, and Anthropic prompt-cache breakpoints are optimized where possible.

## Runtime Model

Slimference now runs in a daemon-plus-monitor model:

- `slimference start`
  Starts the proxy daemon in the background.

- `slimference service install`
  Installs launchd auto-start on macOS so Slimference starts at login.

- `slimference`
  Opens the TUI as a monitoring and management window. It attaches to the running daemon instead of starting a second proxy.

The daemon keeps running, logging, and collecting analytics even when the TUI is closed.

## Quick Start

### 1. Build

```bash
go build -o ./slimference ./cmd/slimference
```

### 2. Check the setup

```bash
./slimference doctor
```

### 3. Start the daemon

```bash
./slimference start
```

### 4. Open the TUI

```bash
./slimference
```

### 5. Install hooks for your agent

```bash
./slimference hook install claude
./slimference hook install codex
```

### 6. Enable auto-start on login

```bash
./slimference service install
```

## TUI

The TUI is built with Bubble Tea and Lip Gloss.

It is meant to be the operator console for a running Slimference daemon:

- monitor provider and layer status
- view analytics and savings
- inspect recent request activity
- inspect local logs
- enable or disable providers and layers
- start, stop, restart, install, or uninstall the background service

Navigation highlights:

- `left` / `right`: switch views
- `up` / `down`: move inside setup selections
- `enter`: execute the selected setup action
- `c` / `x`: toggle Claude Code / Codex
- `1` / `2` / `3`: toggle compression layers
- `f`: flush caches
- `q`: quit the TUI

## Useful Commands

### Service and daemon

```bash
./slimference start
./slimference stop
./slimference restart
./slimference service status
./slimference service install
./slimference service uninstall
./slimference daemon logs --lines=100
```

### Layer 0 filtering and savings

```bash
./slimference filter -- git status
./slimference gain today
./slimference gain week --by-command
```

### Debug and diagnostics

```bash
./slimference doctor
./slimference debug paths
./slimference debug last
./slimference debug tail 20
./slimference stats today
./slimference version
```

## Local Data

Slimference writes local state under `~/.slimference/`.

Important paths include:

- `~/.slimference/logs/slimference.jsonl`
- `~/.slimference/logs/daemon.stdout.log`
- `~/.slimference/logs/daemon.stderr.log`
- `~/.slimference/analytics/`
- `~/.slimference/filter.db`
- `~/.slimference/tui_state.json`

## Supported Agent Surface

Current first-class hook support:

- Claude Code
- Codex

## Development

Run the full Go test suite:

```bash
go test ./...
```

Build the binary:

```bash
go build -o ./slimference ./cmd/slimference
```

The repository also includes Go tooling under [`scripts/`](./scripts/README.md) and deeper technical documentation in [`docs/documentation.md`](./docs/documentation.md).

## Notes

- `spec+.md` is the implementation-driving specification.
- `handover.md` is the operator and agent onboarding file.
- `rtk-master/` is reference material only and is not part of Slimference runtime code.
