# TASK 231: M-series performance profile

Status: PLANNED - deliberately deferred until the product path is otherwise
release-ready, except for low-risk profiling scaffolding
Priority: P2 after T240 release proof and after T230 has a measurable output
baseline
Scope: macOS arm64 performance, binary build flags, hot-path profiling

## Why

Slimference runs on Apple Silicon. The binary is already a stripped single
Mach-O arm64 build and the current hot paths are fast enough that network/model
latency dominates. Blind SIMD/Rust/unsafe work is therefore the wrong default.

Performance work should be driven by real Codex HTTP/WSS sessions and pprof,
not guesses. The goal is less latency and fewer allocations without increasing
maintenance risk or binary complexity.

This task is not allowed to distract from WSS correctness, Desktop truth, or
daemon lifecycle. It becomes high-value only when the product path is stable
enough that performance profiles reflect the real final workload.

## Current Evidence

- Current local binary: single `darwin/arm64` Mach-O, stripped with
  `-trimpath -ldflags="-s -w"`, around 18 MB.
- Local Go env: `GOOS=darwin`, `GOARCH=arm64`, `GOARM64=v8.0`.
- Hardware checked: Apple M1 exposes AdvSIMD/NEON, crypto, SHA/AES, LSE.
- Quick benchmark showed `GOARM64=v8.1,crypto` is not a clear win on this code;
  some compression cases were slower.
- Existing benchmark runner covers compression/filter packages but not full
  Codex WSS live sessions.

## Target State

- Release build remains one binary.
- Build helper stays `go build -trimpath -ldflags="-s -w"` unless evidence says
  otherwise.
- Optional M-series flags are benchmark-gated, not assumed.
- pprof captures real scoped Codex HTTP and WSS sessions.
- Allocation reductions target measured hot spots only.
- No Rust port, SIMD, unsafe, assembly, architecture-specific build flags,
  pooling, or ring-buffer rewrite before profiles prove a real bottleneck and a
  simple Go fix is insufficient.

## Acceptance

- Add a repeatable local profile command or docs section for:
  CPU profile, heap profile, trace, and WSS-session benchmark.
- Record pprof findings from real Codex traffic after T209/T224.
- Optimize only functions that show meaningful self/alloc time in profiles.
- Keep changes pure Go unless a measured bottleneck cannot be solved safely.
- Any `unsafe`, assembly, SIMD, or new dependency requires:
  benchmark proof, fallback path, tests, and a documented reason.
- Do not split the binary.

## Sub-Tasks

- [ ] Add `scripts/benchmarks` mode for scoped Codex replay/WSS frame replay
  using captured or synthetic fixtures.
- [ ] Add pprof instructions or command wrapper for daemon CPU/heap profiles.
- [ ] Profile real T209/T224 CLI sessions after live proof.
- [ ] Re-run profiles after T243/T240 when the final WSS-first ladder and
  release ceremony are stable; only then decide what to optimize.
- [ ] Profile output-reduce v2 reducers when T230 lands.
- [ ] Investigate high-allocation candidates:
  JSON minify, ANSI strip long bodies, comment strip, WSS frame buffers,
  debug-flight serialization.
- [ ] Consider `sync.Pool` only for measured repeated large buffers.
- [ ] Re-test GOARM64 variants after real profiles; do not set `v8.1,crypto`
  by default unless it wins reproducibly.
- [ ] Document final build flags in `scripts/build` docs.

## Benefits

- Lower daemon CPU and memory under real Codex traffic.
- Better TUI/status confidence around performance.
- Avoids wasting time on SIMD theatre where Go/stdlib/network dominates.

## Drawdowns and Guards

- Over-optimizing unmeasured paths adds bugs. Guard: pprof first.
- Architecture-specific flags can hurt portability or performance. Guard:
  benchmark on target hardware before changing defaults.
- Unsafe/SIMD adds maintenance cost. Guard: require proof and fallback.
- Premature concurrency can create races and worse latency. Guard: only add
  goroutines/pools/ring buffers where pprof or benchmarks show contention,
  allocation churn, or large repeated buffers on the real WSS path.
