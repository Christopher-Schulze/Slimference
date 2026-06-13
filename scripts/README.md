# `scripts/` - Slimference Go Tooling

All repository tools (coverage gates, benchmark helpers, utilities, and one-off
maintenance commands) live here in topic-specific subdirectories, not in the
repository root.

## Subdirectories

| Path | Purpose |
|------|--------|
| `build/` | Builds a local single-file Slimference binary with release flags (`-trimpath -ldflags "-s -w"`); `--install` replaces the target binary via temp file plus atomic rename |
| `coverage/` | Evaluates coverage, enforces the current 95.0% aggregate gate, and mirrors local CI behavior |
| `benchmarks/` | Groups benchmarks and evaluates `go test -bench` output |
| `release/` | Builds macOS release artifacts with SHA256SUMS; the default target is darwin/arm64 |
| `utils/` | Small helper CLIs, one-off maintenance commands, and generators; `utils/indist_probe` is the tshark-based capture/diff tool for T224 |

Add more subdirectories only for a clear topic, for example `lint/`.

## Rules

- Implementation: **Go** (`.go`), see `AGENTS.md` section 3.
- Removed reference trees and retired third-party snapshots must not be
  recreated here.

## Usage

From the module root (`Slimference/`):

```bash
go run ./scripts/coverage/...    # sobald ein entrypoint existiert
```

Concrete commands:

```bash
go run ./scripts/build --install                # Optimized binary to ~/.local/bin/slimference
go run ./scripts/build --restart                # Safe local update: stop -> build -> atomic install -> start
go run ./scripts/build --out ./slimference      # Optimized local binary
go run ./scripts/release --version v0.6.0       # macOS arm64 release tarball + SHA256SUMS
go run ./scripts/release --version v0.6.0 --targets=darwin/arm64,darwin/amd64  # Public macOS release set
go run ./scripts/coverage -min=95.0              # Coverage-Gate (aggregate)
go run ./scripts/benchmarks                      # Hot-path Benchmarks (3s): compression/filter/proxy/readcache/archive/chunk/planner
go run ./scripts/benchmarks -benchtime=1s        # Faster run
go run ./scripts/benchmarks -count=3             # Three rounds for stability
go run ./scripts/benchmarks -pkg=compression     # Compression package only
go run ./scripts/benchmarks -pkg=proxy           # Codex/WSS Layer-0 hot path only
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex   # CI-enforced regression gate
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --promotion-check # Base CLI/Desktop release corpus gate
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --maxx-check      # Base gate plus chunk/tool/provider-cache/resource mechanism breadth; WSS output-reduce rows are historical diagnostics after T330
go run ./scripts/utils session-report ~/.slimference/analytics/2026-04-17.jsonl
go run ./scripts/utils decision-report ~/.slimference/logs/decisions.jsonl --json
go run ./scripts/utils filter-report ~/.slimference/filter.db --csv
go run ./scripts/utils combined-report ~/.slimference/analytics/2026-04-17.jsonl \
  ~/.slimference/logs/decisions.jsonl \
  ~/.slimference/filter.db
go run ./scripts/utils aggregate-savings                                              # live admin/state honest aggregate
go run ./scripts/utils aggregate-savings --filter-db=~/.slimference/filter.db --period=today
go run ./scripts/utils aggregate-savings --admin-state-file=admin-state.json --json   # offline mode
go run ./scripts/utils workday-savings start                                         # baseline for real workday savings
go run ./scripts/utils workday-savings finish --filter-db=~/.slimference/filter.db   # flush-aware window delta
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --json         # content-free WSS route/session/observed+resolved-shape/shape-economics/full-history-cost/re-read/history-reducer/footprint-economics/shadow-mirror density audit
go run ./scripts/utils wss-local-gap ~/.slimference/debug/decisions.jsonl --min-local-ratio=0.48 --json # strict AGENTS.md 3.2 S_local gap ledger; provider-cache is separate; no-evidence rows are grouped by content-free debug facts
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --since=2026-05-30T00:30:00Z --expect-distinct-sessions=2 --min-phasef=2  # fresh session-key gate
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --since=2026-05-30T00:30:00Z --min-phasef=2 --require-savings  # fresh savings gate
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --since=2026-05-30T00:30:00Z --min-phasef=2 --require-history-evidence  # fresh T353 history reducer calibration gate
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --since=2026-05-30T00:30:00Z --min-phasef=2 --require-footprint-evidence  # fresh T359 footprint + remaining-turn calibration gate
go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl --since=2026-05-30T00:30:00Z --min-phasef=2 --min-full-history=1  # fresh T354 Class-B/full-history capture gate
go run ./scripts/utils search-cap-profile --command 'rg -n needle internal' --input /tmp/rg-output.txt --require-applicable --require-aggressive-savings # offline T359 search-cap stdout profile comparison, content-free report
go run ./scripts/utils search-cap-profile --frames ~/.slimference/captures/codex-wss-frames.jsonl --require-applicable --require-aggressive-savings --json # offline T359 WSS-frame search-cap profile; extracts resolved search tool outputs only
go run ./scripts/utils search-cap-profile --frames ~/.slimference/captures/codex-wss-frames.jsonl --candidate=25:15 --candidate=20:10 --min-candidate-retained-pct=40 --require-aggressive-savings --json # proof-only T359 cap sweep; fails candidates that save by hiding too much search evidence
go run ./scripts/utils search-cap-proof --frames ~/.slimference/captures/codex-wss-frames.jsonl --candidate=30:15 --candidate=25:15 --min-candidate-retained-pct=40 --min-search-outputs=2 --min-extra-reducer-tokens=1 --json # combined T359 promotion gate: profile breadth, retention, WSS replay lost=0/upstream-error-free, and positive extra savings
go run ./scripts/utils codex-capture-run --binary ~/.local/bin/slimference --port=8991 --capture ~/.slimference/captures/repeat.jsonl --matrix-row /tmp/proof-matrix.jsonl --id cli-repeat --workload-class repeat_full_read --expected-reducer read_delta --codex-timeout=180s --exit-marker CAPTURE_DONE --exit-marker-count=2 --quiet-codex-output -- exec "Run exactly two shell tool calls and do not modify files. First tool call cmd exactly: cat AGENTS.md Second tool call cmd exactly: cat AGENTS.md Then final message exactly CAPTURE_DONE" # records live billable input-token delta plus replay lost=0 bytes on a managed proof daemon; --port can use a separate local port with isolated daemon PID/lock state so an existing normal daemon on 8990 is not stopped; --expected-reducer is enforced against live admin-state before PASS, but the evidence row is still appended for failed expected-reducer runs; marker exit watches both the PTY log and captured function_call_output frames
go run ./scripts/utils wss-ab-replay captures/codex-wss-frames.jsonl --fail-on-lost # product-default WSS replay gate with safe read-delta savings
go run ./scripts/utils wss-ab-replay captures/codex-wss-frames.jsonl --json          # machine-readable A/B report
go run ./scripts/utils wss-ab-replay captures/codex-wss-frames.jsonl --search-cap-files=25 --search-cap-matches=15 --fail-on-lost --fail-on-upstream-error --json # proof-only T359 search-cap replay; product defaults stay unchanged
go run ./scripts/utils wss-ab-replay captures/codex-wss-frames.jsonl --tool-output-mutation --fail-on-lost --json # lab/proof WSS reducer replay for non-delta tool-output mutation; previous_response_id delta mutation needs --delta-tool-output-mutation-lab and is only for reproducing known T354 400s
go run ./scripts/utils wss-proof-matrix captures/proof-matrix.jsonl --require-live-token-delta --json # T257 release proof gate: real live token deltas required
go run ./scripts/utils wss-proof-matrix captures/search-proof.jsonl --require-live-token-delta --required-workload=search_loop --min-captures=2 --min-cli=1 --min-desktop=1 --min-positive=2 --expected-reducer captured_output --search-cap-candidate=30:15 --search-cap-candidate=25:15 --json # focused search-loop mechanism gate with optional T359 search-cap promotion proof, not a release substitute; coverage/savings aggregates count only row-gate-passed captures; search-cap defaults fail closed at 40% retained matches, >=2 resolved search outputs, +1 extra reducer token, and captured-output delta mutation proof
go run ./scripts/utils wss-proof-inventory ~/.slimference/captures --json # content-free inventory of local proof matrix rows plus per-maxx workload complete/missing-signal status; ignores raw WSS frame payloads
go run ./scripts/utils wss-proof-export-corpus ~/.slimference/captures tests/fixtures/live_corpus --json # export content-free proof-matrix live deltas into benchmark-corpus categories; uses absolute saved-token/mechanism gates, never raw WSS frames or prompts
go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures /tmp/slimference-clean-release-matrix.jsonl --json # write a strict release matrix from proof rows only; skips stale diagnostic rows, expected-zero local-savings violations, host-budget attention rows, safety issues, and rows without an economic signal
go run ./scripts/utils wss-proof-live-row --matrix-row /tmp/desktop-proof.jsonl --frames ~/.slimference/captures/desktop-proof.frames.jsonl --client desktop --workload-class tool_heavy --expected-reducer tool_prune --expected-reducer tool_prune_tokens_saved --expected-reducer host_budget_ok # append current Desktop admin-state/status counters as a content-free matrix row when codex-capture-run cannot own the app process
go run ./scripts/utils codex-capture-run --resource-profile-proof ~/.slimference/captures/host-resource-codex_cli-YYYYMMDD --workload-class host_resource_long_workday --expected-reducer host_budget_ok --codex-timeout=180s --exit-marker HOST_RESOURCE_DONE --quiet-codex-output -- exec "<real host-resource workload prompt ending with HOST_RESOURCE_DONE>" # automated CLI resource bundle: frames, matrix, aggregate before/after including effective_rss/go_retained host-budget proof, ps before/after, macOS sample, workday-finish
go run ./scripts/utils release-proof-report /tmp/slimference-clean-release-matrix.jsonl --resource-profile-proof ~/.slimference/captures/host-resource-codex_cli-YYYYMMDD --resource-profile-proof ~/.slimference/captures/host-resource-codex_desktop-YYYYMMDD --search-cap-proof-report ~/.slimference/captures/release-proof-YYYYMMDD-search-cap.json --codex-status-before ~/.slimference/captures/release-proof-YYYYMMDD-codex-status-before.json --codex-status-after ~/.slimference/captures/release-proof-YYYYMMDD-codex-status-after.json --json > ~/.slimference/captures/release-proof-YYYYMMDD-final.json # final content-free proof summary over a clean release matrix plus focused search-cap proof; config latch points at this final JSON, not the focused matrix report
go run ./scripts/utils local-artifact-hygiene --json # fail-closed check for known stale local build/test/release scratch artifacts in the repo root
go run ./scripts/utils local-artifact-hygiene --clean # remove only known untracked generated artifacts; tracked candidates are reported but never deleted
go run ./scripts/utils wss-output-reduce-ab-report /tmp/output-reduce-ab-matrix.jsonl --min-net-tokens=1 --json # historical/diagnostic output-reduce A/B gate for archived WSS directive rows; Codex WSS runtime no longer injects model-facing output-reduce directives after T330
go run ./scripts/verify -mode host-resource-plan -client codex_desktop # T272 content-free resource/profile proof ceremony: admin state, ps, macOS sample, workday finish, WSS host_budget_ok + positive live economic-token gate
SLIMFERENCE_OUTPUT_REDUCE_PROFILE=codex_aggressive SLIMFERENCE_OUTPUT_REDUCE_MIN_INPUT_TOKENS=1 go run ./scripts/utils codex-capture-run --transport=wss --matrix-row /tmp/output-reduce-proof.jsonl --workload-class output_reduce_aggressive --ab-pair-id output-status-YYYYMMDD --ab-variant directive --expected-reducer output_reduce_injected --expected-reducer host_budget_ok -- exec <prompt> # historical WSS directive proof shape only; do not use as current Codex WSS product proof because T330 disables this model-facing WSS injection path
SLIMFERENCE_TOOL_PRUNE_ENABLED=1 SLIMFERENCE_TOOL_PRUNE_IDLE_THRESHOLD_TURNS=1 go run ./scripts/utils codex-capture-run --transport=wss --matrix-row /tmp/tool-prune-proof.jsonl --workload-class tool_heavy --expected-reducer tool_prune --expected-reducer tool_prune_tokens_saved --expected-reducer host_budget_ok -- exec <prompt> # focused tool-schema pruning proof, scoped to this daemon process; idle threshold is scoped to the proof daemon only
go run ./scripts/utils codex-capture-run --transport=wss --matrix-row /tmp/chunk-proof.jsonl --expected-reducer chunk_dedup --expected-reducer chunk_dedup_refs --expected-reducer host_budget_ok -- exec <prompt> # focused mechanism proofs can force wss; matrix rows can gate chunk refs, tool_prune, tool_prune_tokens_saved, output_reduce_skipped/downgraded, stop_seq, streamcut, repdet, stale_read, obsolete_prune, beterse, provider_cache_read/create, and host_budget_ok
go run ./scripts/utils codex-capture-run --transport=wss --matrix-row /tmp/chunk-policy.jsonl --expected-reducer chunk_dedup -- exec <prompt> # prints live proxy_layer0_policy/cache deltas so zero live savings can be attributed to allow/block/full_pass/miss reasons without raw payloads
go run ./scripts/utils tls-probe --profile=chromium_stable --json
go run ./scripts/utils/indist_probe capture --label codex-native-direct --out ~/.slimference/captures/indist/codex-native-direct.json --iface en0 --host chatgpt.com --port 443
go run ./scripts/utils/indist_probe diff ~/.slimference/captures/indist/codex-native-direct.json ~/.slimference/captures/indist/slimference-scoped-wss.json
```
