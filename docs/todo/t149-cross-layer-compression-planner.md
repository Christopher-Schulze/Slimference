# TASK 149: Cross-layer compression planner and safety governor

Status: IN PROGRESS (T149a pure planner core, T149b dry-run flight/TUI integration, T149c plan inspect, T149d corpus planner replay, and T149e output-reduce cooldown facts landed 2026-05-14; behavior control pending)
Priority: P0
Scope: `internal/planner/`, `internal/proxy/handler.go`, `internal/compression/`, `internal/summarization/`, `internal/caching/`, `internal/outputreduce/`, `internal/wscompact/`, `internal/sessions/`, `internal/quality/`, `cmd/slimference/plan_cmd.go`, `docs/documentation.md`.

## Why

The project now has many powerful levers: L0 parsers, L1 deterministic compaction, L2 summarization, L3 provider cache/state reuse, output-reduce, and future WebSocket mutation. If each layer acts independently, they can fight each other:

- L1 may spend CPU compacting content L2 will summarize.
- L2 may summarize too early and cause extra tool calls.
- L3 cache hints may miss because L1 changed a stable prefix.
- Output-reduce may save text but trigger repair turns.
- WebSocket mutation may be unsafe for a drifted message shape.

The fix is a deterministic planner that chooses a per-request strategy before layers run.

## Target State

Every proxied request gets a `CompressionPlan`:

- route mode.
- provider/model capabilities.
- session/turn state.
- estimated input/output/cache opportunity.
- quality risk.
- latency budget.
- selected layer actions.
- fallback behavior.
- explanation for flight/debug/TUI.

Operators can inspect why Slimference did or did not compress a request.

## Work Packages

### WP1 - Plan model

- [x] Add `internal/planner`.
- [~] Core structs:
  - `RequestFacts`.
  - `ProviderCapabilities`.
  - `SessionFacts`.
  - `LayerDecision`.
  - `CompressionPlan`.
  - `SafetyGate`.
  - `PlanOutcome`.
- Implemented foundation uses `RequestFacts`, `LayerDecision`, and `CompressionPlan` now; provider/session/safety gate detail structs can be split out when handler integration needs them.
- [x] No globals; planner is deterministic for the same inputs.

### WP2 - Request fact extraction

- Collect:
  - provider/model.
  - route mode.
  - request token estimate.
  - response/cache support.
  - task shape.
  - content classes.
  - recent edit state.
  - WebSocket shape confidence.
  - L2 privacy policy.
  - live-corpus confidence.
- [x] Dry-run proxy bridge extracts current safe facts from HTTP summaries,
  local-cache hits, transparent CONNECT, and direct WebSocket routes:
  provider/model/route, token estimates, output estimate, task shape,
  content classes, recent re-read/edit signal, provider-cache support,
  previous-response availability, manual layer toggles, L2 enabled state, and
  WebSocket route class.
- [ ] Future fact extraction still needs session-owned edit-file state,
  live-corpus confidence lookup, and WebSocket shape registry results.
- [x] Output-reduce cooldown is now read from the T141 auto-tune tracker before
  profile selection. The planner records this as a `cheap_only`
  `quality_cooldown_soften_profile` decision so debug/corpus replay matches the
  real behavior: aggressive directives are softened, not silently kept.

### WP3 - Layer action selection

- [x] Decisions:
  - run L0 parser or bypass.
  - run L1 cheap/heavy/symbol/dictionary passes.
  - run L2 now/background/skip.
  - inject cache hints or skip.
  - use previous response ID or full context.
  - apply output-reduce profile.
  - WebSocket tunnel/inspect/shadow/mutate.
- [x] Each decision must include:
  - expected net saving.
  - confidence.
  - risk.
  - fallback.
  - telemetry label.
- Implemented foundation carries action, reason, expected saving, risk, and confidence; explicit fallback/telemetry fields can be expanded when wired to flight records.

### WP4 - Safety governor

- [x] Hard blocks:
  - unknown WebSocket shape -> no mutation.
  - active edit file -> no aggressive file slicing unless body available.
  - external L2 disabled/unacknowledged -> no external summary.
  - no provider cache support -> no cache hint.
  - quality bucket in cooldown -> soften or bypass.
  - negative-savings history -> bypass.
- [x] Soft gates:
  - small request -> cheap passes only.
  - high latency pressure -> skip expensive passes.
  - low live-corpus confidence -> shadow mode only.
- Implemented foundation covers manual disables, recent-edit L1/L2 safety, external L2 policy, provider-cache unsupported, output-reduce cooldown, negative-savings history, unknown WebSocket shape, low corpus confidence, and small-request bypass.

### WP5 - Explainability

- Add `slimference plan inspect` for dry-run request fixtures.
- [x] Flight records include compact dry-run plan summary:
  - selected actions.
  - skipped actions with reasons.
  - safety gates.
  - expected vs actual tokens.
- [x] `debug.RequestSummary` and normalized `flight` records carry the same
  content-free `plan` object. The planner event is emitted as
  `stage=planner`, `decision=advice_ready|blocked`.
- [x] TUI Debug renders compact per-flight plan decisions and plan-block count.
- [x] `slimference plan inspect` dry-runs the planner against explicit facts
  and optional request file/stdin token estimates; `--json` emits the raw
  `CompressionPlan`.

### WP6 - Integration

- [x] Handler attaches a dry-run planner output to completed request summaries.
  This is advice-only and never changes request bytes or layer behavior.
- [ ] Handler asks planner before running compression pipeline.
- [ ] Existing layer APIs receive plan hints, not raw global config.
- Layer-local safety remains in place; planner is not the only guard.
- Preserve manual operator toggles: a disabled layer stays disabled.

### WP7 - Tests

- [x] Pure planner tests for every gate.
- [x] Bridge and flight tests prove dry-run plans are attached, cloned,
  redacted/content-free, and visible in flight events.
- Integration tests prove:
  - small request avoids overhead.
  - large tool output selects L0/L1.
  - long old prefix selects L2 when policy permits.
  - cache-capable stable prefix selects L3 hints.
  - unknown WebSocket shape stays tunnel/inspect.
  - output-reduce cooldown prevents aggressive profile.
  - manual disable overrides planner.

## Acceptance

- [x] Every currently recorded proxy route has a deterministic dry-run plan
  record: upstream, local cache, Stage-A cache hit, transparent CONNECT,
  direct WebSocket tunnel, and direct WebSocket fallback.
- [x] Planner decisions are visible in flight/debug/TUI.
- [ ] Manual layer toggles are still authoritative.
- [x] Safety gates prevent known bad combinations in the pure planner core.
- [ ] Layers no longer make conflicting independent choices where planner owns the decision.
- [x] T146 corpus can replay planned vs actual outcomes.
- [x] T141 auto-tune cooldown is visible to planner/flight summaries and covered
  by proxy integration tests.
- [x] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- This is the multiplier task: it may not save tokens alone, but it lets radical levers run safely.
- Expected direct gains: 5-15% from avoiding layer conflicts and negative-savings work.
- Expected strategic gain: makes T142-T148 default-on decisions defensible instead of config-spaghetti.

## Non-Goals

- Do not remove layer-local fallback checks.
- Do not let planner override explicit operator disables.
- Do not optimize for savings at the expense of task success.
- Do not claim provider billing savings without provider-reported usage.

## Implementation Notes

- 2026-05-14 T149a:
  - Added `internal/planner` with deterministic request planning for L0, L1, L2, L3, output-reduce, and WebSocket transport.
  - Planner inputs include provider/model/route, token estimates, content classes, manual disables, recent edit state, L2 policy/ack, provider-cache support, previous-response state, output-reduce cooldown, negative-savings history, WebSocket shape/mutation request, live-corpus confidence, and latency budget.
  - Decisions carry layer, action, reason, expected saving, risk, and confidence.
  - Hot-path behavior integration, `slimference plan inspect`, TUI rendering, and T146 planned-vs-actual replay remain pending.
  - Focus test: `go test ./internal/planner -cover` at 100%.
- 2026-05-14 T149b:
  - Added debug `PlanSummary` / `PlanDecisionSummary` and attached cloned dry-run plans to normalized flight records.
  - Added planner flight events so `debug flight tail --json` can show whether a request was merely advised or safety-blocked.
  - Added `internal/proxy/planner_bridge.go` to translate live proxy facts into planner input without importing prompt content into logs.
  - Wired dry-run plans into upstream/local-cache summaries, Stage-A cache hits, transparent CONNECT records, and direct WebSocket tunnel/fallback records.
  - TUI Debug view renders compact plan lines (`l0=run l1=cheap_only ...`) plus a plan-block counter.
  - Behavior remains unchanged: no layer execution is controlled by planner yet.
  - Focus tests: `go test ./internal/debug ./internal/proxy ./internal/planner ./internal/tui -cover`; all touched packages remain 100%.
- 2026-05-14 T149c:
  - Added `slimference plan inspect [flags] [-|<request-file>]`.
  - Supports provider/model/route, input/output token overrides, task shape,
    content classes, manual layer disables, recent-edit flag, L2 policy/ack
    flags, provider-cache / previous-response state, output cooldown,
    negative-savings history, WebSocket shape/mutation facts, live-corpus
    confidence, latency budget, and JSON output.
  - When `--input-tokens` is omitted and a file/stdin is provided, it estimates
    input tokens locally without upstream traffic.
  - Focus test: `go test ./cmd/slimference -cover` at 100%.
- 2026-05-14 T149d:
  - `scripts/benchmarks` now replays recorded `plan` / `flight.plan` objects from request-summary JSONL and compares them with observed layer activity.
  - Reports include planner request count, decision count, expected planner savings, expected-active/observed-active/missed active actions, bypass/tunnel actions that still saw activity, safety-blocked requests, action counts, and risk counts.
  - Category metadata supports `expected_planner_missed_max` and `expected_planner_bypass_applied_max`.
  - This closes evidence plumbing for planned-vs-actual. Behavior control remains pending and should not be enabled before live corpus gates are populated.
  - Focus test: `go test ./scripts/benchmarks -cover`.
- 2026-05-14 T149e:
  - Added `outputreduce.Tracker.InCooldown` and wired it into the proxy's
    planner facts before output-reduce profile selection.
  - Planner L4 cooldown behavior now says `cheap_only` with
    `quality_cooldown_soften_profile`, matching the T141 auto-tuner's real
    downgrade behavior instead of pretending the layer is fully bypassed.
  - Added proxy integration coverage proving an aggressive profile in cooldown
    is softened to standard and exposed in the attached plan summary.
  - Focus test: `go test ./internal/outputreduce ./internal/planner ./internal/proxy -cover`.
