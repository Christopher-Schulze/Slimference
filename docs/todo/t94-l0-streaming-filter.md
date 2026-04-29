# TASK 94: Streaming-aware Layer 0 filter

Status: todo
Priority: P2
Scope: `internal/filter/`, `cmd/slimference/`
Driver: Long-running outputs (`tail -f`, `docker logs --follow`, slow test runners) skip Layer 0 entirely because filters assume "process exits, then filter". For these workflows the agent ingests the unfiltered tail.

---

## Problem

`slimference filter <cmd>` runs the subprocess, captures stdout, and filters at exit. For streaming tools the subprocess never exits naturally, so the filter never runs. The agent gets the raw stream.

## Target State

A new mode `slimference filter --stream <cmd>` runs the subprocess and applies filters in real time on a sliding window:

- Buffered window of last `[filter.stream] window_lines` lines (default 200).
- Rolling ANSI strip + dedup of consecutive identical lines.
- Periodic "flush" emits the compacted window every `[filter.stream] flush_interval` (default 2s) or when the window is full.
- On Ctrl-C / process exit, a final flush completes.

The same filter chain (built-ins + TOML) applies; only the pump strategy changes.

## Implementation Plan

### WP1 - Streaming pump
- New `internal/filter/streamfilter.go` with a goroutine that reads stdout, applies a windowed filter, emits compacted output.

### WP2 - Subcommand
- `slimference filter --stream <cmd>` opt-in; default behaviour unchanged.

### WP3 - Hook integration
- For Claude / Codex hooks that wrap streaming tools (rare today), the hook script can pass `--stream`.

### WP4 - Tests
- Synthetic generator emits 10k lines over 5s; assert window-size and dedup.

## Acceptance Criteria

- [ ] `slimference filter --stream tail -f /var/log/system.log` produces compacted, deduped lines without buffering the whole stream.
- [ ] Final flush on Ctrl-C produces a complete output.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Auto-detection of streaming tools (operator opts in via flag).
- Multiplexing multiple streams in a single filter pass.

## Validation

```
slimference filter --stream tail -f /tmp/test.log
go test ./internal/filter/...
```
