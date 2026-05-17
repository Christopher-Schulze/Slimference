# TASK 195: Resource footprint budget for Phase G

Status: PLANNING 2026-05-16
Priority: P1 (the "wenig Ressourcen" piece the user named)
Scope: `internal/proxy/`, `internal/control/state.go`, benchmark suite under
       `scripts/benchmarks/`

## Why

User said:

> "Und das würde auch nicht viele Ressourcen kosten, richtig?"

Slimference must run quietly in the background. A daemon that uses 1 GB of
RAM or spikes CPU on every conversation defeats the point. Phase G adds:

- Transparent :443 listener (TLS terminator)
- WebSocket-aware MITM
- DoH resolver
- Phase F pipeline on every request

Each adds CPU + memory. We must budget and enforce.

## Targets

| Metric                          | Budget     | Hard ceiling           |
|---------------------------------|------------|-----------------------|
| Steady-state RSS                | ≤ 150 MB   | 200 MB (auto-warn)     |
| Idle CPU                        | ≤ 0.3 %    | 1 % (auto-warn)        |
| Per-conversation-turn CPU cost  | ≤ 50 ms    | 200 ms (auto-warn)     |
| p50 added latency (first byte)  | ≤ 5 ms     | 15 ms                  |
| p95 added latency (first byte)  | ≤ 25 ms    | 60 ms                  |
| Cold-start (daemon launchd → ready) | ≤ 500 ms | 2 s                    |
| Binary size                     | ≤ 20 MB    | 25 MB                  |
| Goroutines steady-state         | ≤ 80       | 200                    |
| Goroutines per WS session       | ≤ 4        | 8                      |
| File descriptors steady-state   | ≤ 50       | 200                    |

"Auto-warn" means: the daemon emits a structured warning + the TUI shows
a yellow indicator. Doesn't crash; gives the operator a signal.

## Measurement plan

### Benchmarks (`scripts/benchmarks/`)

1. `cold_start.go` - launch the daemon, measure time-to-ready (first
   admin/health 200). Run 10× and take p50/p95.

2. `idle_footprint.go` - run the daemon for 10 minutes with zero
   traffic. Sample RSS + CPU every 5 s. Report mean + max.

3. `per_turn_overhead.go` - replay a captured conversation against a
   localhost fake upstream. Measure:
   - Wall-clock proxy overhead per turn.
   - CPU time per turn (via `/proc/self/stat` on Linux,
     `task_info()` on macOS).
   - Memory delta over a 100-turn run.

4. `latency_probe.go` - measure round-trip latency from a fake client
   to the real chatgpt.com, with and without our proxy in front. The
   delta is our added latency.

5. `large_conversation.go` - 1 000-message conversation (long history
   with many tool_result blocks). Measure pipeline pass-through cost.

### Runtime telemetry

Daemon already exposes `/admin/health` with PID + RSS. Extend with:

- `runtime.NumGoroutine()`
- `runtime.MemStats.HeapAlloc`, `HeapInuse`, `HeapObjects`
- `runtime.GC` last-pause-ns
- File descriptor count (Unix-specific syscall)
- Per-conversation-turn timing histograms (already partially exist as
  `pipelineHist`)

Surface these in `/admin/state.daemon` and the TUI status panel.

## Auto-degradation policy

When any hard ceiling is exceeded:

| Trigger                          | Action                                            |
|----------------------------------|---------------------------------------------------|
| RSS > 300 MB                     | Drop response-cache + flush sessions (free heap) |
| Goroutines > 200                 | Refuse new connections briefly (overload signal) |
| p95 added latency > 100 ms       | Disable Phase F transforms temporarily; alert    |
| CPU > 5 % sustained 60 s         | Log warning, surface in TUI                      |

These are documented in `docs/operations.md` (new) so operators know what
to expect.

## Implementation notes for cost reduction

- **DoH resolver cache**: TTL-respecting, in-memory, max 256 hosts.
  Avoids re-resolving chatgpt.com on every connection.
- **TLS leaf-cert cache**: already exists (`internal/tlsca/signer.go`
  with LRU 256). Keep as-is.
- **Connection-pool reuse to upstream**: per-host outbound HTTP/2 +
  WebSocket pools. Slow drain on idle (≤ 1 connection per host steady).
- **Phase F pipeline allocations**: every mechanism should reuse buffers
  where possible. The frame parser in wsmitm should use sync.Pool for
  envelope structs.
- **slog filtering**: drop debug logs in release builds. Trace logs only
  when `SLIMFERENCE_TRACE=1`.
- **No goroutine per request unless necessary**: reuse the relay
  goroutine; only spawn for genuinely parallel work (e.g. Layer-2
  summarization on background).
- **TUI rendering**: aggregator hits admin endpoints. Use cheap GET
  endpoints; cache snapshot for 250 ms in the TUI to avoid hammering.

## Sub-Tasks

- [ ] Implement benchmark suite under `scripts/benchmarks/`.
- [ ] Extend `/admin/health` with the new metrics.
- [ ] Add `daemon_footprint` block to `/admin/state` for TUI.
- [ ] Implement auto-degradation policy + tests.
- [ ] Document operator runbook under `docs/operations.md`.
- [ ] Configure CI to run benchmarks on PR and fail on > 25 % regression.

## Acceptance

- All metrics in the table above hold on a fresh Mac after 200
  conversation turns over Codex CLI + Desktop App.
- Auto-degradation triggers fire correctly in synthetic tests.
- Benchmark suite runs in < 5 minutes locally.
- CI regression gate green on the baseline.

## Notes

- These numbers are for a typical M-series MacBook. Document them as
  "macOS arm64 baseline". Different hardware → different absolute
  numbers but the same relative budget applies.
- A future optimisation: ship the binary split (T182) so the hook path
  is even cheaper. Not blocked by T195.

## Deviations

(none yet)
