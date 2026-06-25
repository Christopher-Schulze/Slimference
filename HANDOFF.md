# Slimference — Agent Handoff (2026-06-25, written at HEAD 53ff4491)

> Read this top-to-bottom before touching anything. It is the complete, honest state of the
> project as of this session. It is deliberately exhaustive. Every number, line reference, and
> claim here was verified live this session — not guessed. When in doubt, re-verify (the honesty
> rules below are binding and non-negotiable).

---

## 0. TL;DR — the five things you must internalize first

1. **What this is:** Slimference makes **Codex** (Codex CLI + Codex Desktop app) cheaper by reducing
   input/context tokens ("savings") while the model's behavior stays **identical** (zero drawdown).
   ChatGPT browser/app are ALWAYS untouched. Only token-heavy **text** request traffic is in scope;
   voice/audio/vision/computer-use pass through byte-untouched.

2. **The one honest product number is `S_local` = local input-token reduction, provider-cache
   excluded.** Today it is **6.26%** (in-band 6.05% + verified L2). Of that, only **15,517 tokens are
   recompute-verified (unfabricatable)**; the rest is operator-attested (self-reported). The owner
   target is **≥48% as a MINIMUM floor, not the goal** — the goal is the maximum achievable under the
   zero-drawdown policy, no ceiling.

3. **The honesty rules are the most important rules in the repo.** A green gate is a *claim*, not
   proof. There was a real fixture-inflation incident (someone faked S_local 11.89% → 48.12% by
   appending fabricated fixtures + raising the CI floor). Read AGENTS.md §3.7.6, §3.8, §3.9 before
   you touch any measurement, fixture, or floor. Negative-control every test you write.

4. **THE current #1 blocker (where the last session ended):** Codex was updated to **cli 0.142.2** at
   ~10:30 on 2026-06-25. This **invalidated WSS Phase-F mutation** — `slimference codex recertify wss`
   reports `frames_reencoded=0, mutation_active=false, byte_bridge_only=true`. So the entire WSS
   in-transit mutation path (the keystone + all cross-turn levers) is **bypassed (byte-bridge)** on
   this Codex version. **You cannot live-prove any WSS mutation until the Phase-F frame parser is
   updated/re-certified for codex 0.142.2's frame format.** This is the real next task.

5. **What "done" means here:** never fake a proof. If a thing cannot be proven (e.g. because the
   substrate is byte-bridge), say so with evidence and record the proven blocker. The last session
   finished the keystone task to its *honest evidence-bound conclusion*, not to a faked "0-400
   proven." Do the same.

---

## 1. Product vision and the binding doctrine (AGENTS.md §0, read it in full)

`AGENTS.md` (at repo root, also symlinked as the CLAUDE.md include) is **binding**. The most load-bearing
parts:

- **§0.2 The Drawdown Policy (the one hard constraint):** a savings mechanism must be zero / near-zero
  / controlled-near-zero drawdown. No optimization may, in normal operation, make the model dumber,
  cause errors / upstream 400s, lose context/memory/recency/salience, hallucinate, or remove a
  capability. **Zero errors / zero upstream 400s is the target.** Development effort, captures,
  benchmarks, longer engineering are NOT drawdowns.
- **§0.6 Innovation doctrine:** never discard a high-savings idea on first contact because its naive
  form has drawdown risk. Engineer the mitigation until the *total* feature is policy-conformant. The
  engineering effort is irrelevant. Long, novel "what if we engineered X to make Y safe" thinking is
  explicitly wanted.
- **§0.8 Honesty + reality checks:** exact measurability is the **highest priority, above shipping the
  next lever**. Run hard self-critical reality checks every phase; distrust prior green cycles.
- **§0.10 Caching must beat default:** provider/prompt caching must be strictly better than plain
  Codex-against-the-servers caching. Tracked/reported **separately** from `S_local` (never blended in).
- **§3.6 High-leverage priority:** don't micro-optimize while big blockers are open. Attack levers by
  token-mass × safety.
- **§3.7 Loop discipline (anti-rabbit-hole):** there is ONE product success number (live `S_local`).
  A prior loop produced 1000+ commits / 44k lines of tooling while the real number stayed ~6%. Don't
  repeat that. A cycle ends when the gate number rises OR a root-cause ceiling is proven + recorded.
- **§10 Wiring doctrine (scoped Codex):** Slimference is active ONLY for sessions started via
  `slimference codex run` or the TUI/Launch path. Plain `codex` runs direct. NO persistent global
  route, NO hosts patch, NO system proxy by default. ChatGPT untouched always.

---

## 2. THE HONEST SAVINGS REALITY (do not get this wrong — §3.8/§3.9)

Run the gate yourself (command in §8) and you will see exactly this today:

```
Real S_local:   6.26%  (Slimference-incremental: in-band + L2; provider-cache excluded)
  recompute-verified (in-band + L2, byte-recomputed) saved=15517 orig=56354 ratio=27.53%  [unfabricatable]
  operator-attested (in-band, self-reported)         saved=340775 orig=5631223 ratio=6.05%  [NOT byte-recomputed]
  L2 cmd-output (verified)     saved=15517 orig=56354 ratio=27.53% share_of_slocal=4.4%
Observed (NOT in S_local): L2 unverified saved=7140757 orig=7255323  (no recomputable bytes → EXCLUDED)
Observed (NOT in S_local): L1 server-state (Codex-native) saved=5855185  (native continuation → EXCLUDED)
Provider cache: read=496896 create=0 cached=496896
Net billable:    14.13% (S_local + cache discount 0.9x)
```

**The three attestation tiers (AGENTS.md §3.9.1 — every number is exactly one of these):**

| Tier | Meaning | Trust | Today's value |
|------|---------|-------|---------------|
| **recompute-verified** | gate re-derives it from embedded raw bytes (`input_sha256` + gzip raw/compacted) | the ONLY tier you may call "proven" | **15,517 tok** (in-band-verified 0 + L2-verified 15,517) |
| **operator-attested** | self-reported counts the gate cannot recompute (in-band `tokens.saved`, `gain`, `status`) | labeled self-reported, never "proven" | in-band 6.05% (= 340,775 tok) |
| **observed-only / excluded** | native or non-incremental behavior | reported separately, never in the trusted number | L2-unverified 7.14M, L1-native 5.86M, provider-cache 496,896 |

**CRITICAL honest framings (do not blur these):**
- The **`27.53%`** recompute-verified ratio is a **curated per-capture** ratio (denominator = only the
  3 verified captures = 56,354 tok). It is **NOT a session number.** The representative session number
  is **Real S_local 6.26%.** Always report both; never present 27.53% as the product number (§3.8.1.3).
- **L1 server-state continuation is Codex-NATIVE, not a Slimference saving.** On the WSS path Slimference
  *detaches* `previous_response_id`; Codex sends tiny deltas (live A/B: client body **34 / 125 / 286
  tokens** vs ~37k server-side context) on its own. Permanently EXCLUDED from `S_local` (§3.7.7). Do not
  ever re-count it without a fresh live A/B proving Slimference *causes* the byte reduction.
- The **6.05% in-band is itself operator-attested** (session `tokens.saved`, no embedded bytes). It is a
  **historical full-history artifact** (wss-proof-export fixtures from 2026-06-02). Current Codex sends
  tiny native deltas → current *verified* in-band ≈ 0. The recompute capability for in-band exists
  (`scripts/benchmarks/session_report.go` `recomputeInBandProvenance`) for whenever genuine in-band
  reduction returns.
- The **L2 lane is the only lever delivering real `S_local` today.** It is process-local
  command-output compaction (a PATH/`BASH_ENV` shim in the launched Codex process), NOT WSS. It
  genuinely replaces large stdout/stderr with compacted bytes before they enter history, archives the
  raw (recoverable via `slimference expand <uri>`, model-invokable), fail-open. It is RTK-class and
  exceeds RTK on coverage (124 `TryCompact*` compactors vs RTK's ~79).

---

## 3. Architecture (the layers and where savings actually live)

Slimference savings operate on **two distinct layers at different pipeline points** (they are NOT both
"on WSS" — a common past confusion):

### L2 Shim (command-output-first) — process-local, pre-wire, transport-independent
- The shim prepends a temp dir of ~150 command shims to PATH + sets `BASH_ENV` in the launched Codex
  process. When Codex runs `bash -lc "rg ..."`, bash finds the shim `rg`, which runs the real `rg`,
  compacts the output (parser-bounded), archives the raw, and emits the compacted bytes.
- **The ONLY lever delivering real `S_local` today.** Safe by construction: archive recovery, byte-equal
  fail-open, never mutates stored history.
- CLI injection: `cmd/slimference/proxy_cmd.go:454` → `maybeApplyCommandOutputFirstEnv`.
- **Desktop injection (shipped this session, 41cfb89d):** `cmd/slimference/codex_desktop_app_server_shim.go`
  `runCodexDesktopAppServerShim` → `applyCommandOutputFirstEnvToList` (in `command_output_first.go`),
  injected AFTER env sanitation, cleanup after the app-server exits.

### In-transit frame mutation (L3 class) — on the active transport (WSS Phase-F, or HTTP fallback)
- Mutates the already-serialized conversation: dedup of repeated tool outputs, stale-read aging,
  obsolete/superseded read+command pruning, tool-def dedup, search-output compaction.
- The ONLY lane that can reach already-accumulated history (the shim cannot).
- **Delivers ~0 incremental `S_local` today** (the delta guards block mutation on delta turns, and on
  the current Codex version WSS is byte-bridge — see §6/§7).

### Transports
- **WSS Phase-F** is the *intended* production transport (the strategic target). The **HTTP Responses
  path** carries the **same body** and can run the **same in-transit mutations** — it is the certified
  fallback. WSS is currently **NOT certified for codex 0.142.2** (byte-bridge only → HTTP fallback).
- L2 is transport-INDEPENDENT (it runs before any wire).

### The scoped Codex path (how traffic gets in — §10.4, hard boundary)
- `slimference codex run -- <prompt>` (incl. `codex exec`) — per-process scoped provider; the normal
  path. Agents use `slimference codex run -- codex exec --dangerously-bypass-approvals-and-sandbox "..."`
  for live evidence (this IS the production-equivalent path, §N — a TTY is NOT required).
- Desktop: scoped app-server launch (provider base_url override into the local daemon).
- **Forbidden as default:** persistent `OPENAI_BASE_URL`/proxy env, hosts patch, system proxy,
  `model_provider="slimference-codex"` left in `~/.codex/config.toml`, unconfirmed `root-arm`. Plain
  `codex` must run direct. ChatGPT always direct.

---

## 4. THIS SESSION'S WORK (2026-06-25, 7 commits f040ba7a → 53ff4491, all on `main`)

| Commit | Time | What |
|--------|------|------|
| `f040ba7a` | 07:43 | **L2 provenance re-capture (Task 1).** Distributed 3 genuine provenance captures (rg→cli_search_loop, ls→cli_git_status) from 2 live `codex exec` sessions. recompute-verified L2 **0 → 289 tok**. No CI floor raised (§3.7.6c). |
| `41cfb89d` | 07:56 | **L2-on-Desktop (Task 2).** Wired the COF shim into the Desktop mediated app-server path. Headless live proof: rg 221KB→160KB, **15,228 tok saved, GATE_PASS=True**. Real S_local **6.06% → 6.26%** (the session's one genuine gate move). |
| `2828487f` | 08:08 | **Adaptive per-class budget (Task 3).** `internal/filter/adaptive_budget.go` (break-even = 1−compactedRatio) + per-session re-fetch counters (`cmd/slimference/command_output_first_adaptive.go`). Floor = fixed L2 (zero-drawdown). Instrument live-proven (rg re-fetch detected). |
| `2df10255` | 08:17 | **Keystone unified verdict (Task 4, part 1).** `internal/proxy/keystone.go` `crossTurnKeystoneVerdict` — ONE fail-closed decision replacing the five guards + observe-only `wss.keystone_apply_eligible` telemetry. |
| `22a8a277` | 08:22 | **Servermirror sizing (Task 5).** `TestMirror_FullHistoryResendSizing`: full-history resend → normalized-referenceable **90.2%**, whole-block 0. Mutation hard-blocked on keystone apply. |
| `57f0fbe9` | 08:32 | **Gain §3.2/§3.8.1.4 honesty fix.** Default `gain` was a blended Layer-0 headline → now points to `gain --proxy` for the S_local/cache/net-billable breakdown. |
| `53ff4491` | 10:35 | **Keystone observability + proven WSS-substrate blocker (Task 4, part 2).** Apply-decision slog + the proven codex-0.142.2 byte-bridge finding (see §6/§7). |

**Test integrity (§3.8.2):** after the 5 frontier tasks, every new test was negative-controlled (break
the production code → confirm the test fails). **9/9 passed** as genuine guards (keystone fail-closed,
adaptive demotion, Desktop sanitizer survival, Desktop env injection, servermirror sizing, disable
escape-hatch, no-session fail-safe, upsert override, per-class independence). None are always-green.

---

## 5. LEVER STATUS — what's done, what's blocked, what's worth (canonical: docs/savings-ledger.md)

| Lever | Status | Verified S_local | candidate_potential_if_completed | Next move |
|-------|--------|------------------|----------------------------------|-----------|
| **in-band request compaction** | production_ready | 6.05% (operator-attested; current verified ≈0) | — | the representative per-request floor |
| **L2 command-output-first (CLI + Desktop)** | production_ready, both transports | **15,517 tok** verified | +5..+20 on tool-heavy sessions | grow verified mass with more provenance captures; broaden to MCP/tool-facade boundary (`docs/todo/new-l2-broad-clean-codex-native.md`) |
| **L2 adaptive per-class budget** | production_ready (floor=fixed) | n/a (net-protect, no gate metric) | +1..+5 net on sustained sessions | the "compact-harder-on-low-refetch" continuous-cap half is deferred (124-compactor surface) |
| **L1 server-state continuation** | **excluded (Codex-native)** | 0 (not a Slimference saving) | 0 unless a future A/B proves incrementality | closed lane |
| **L3 / WSS in-transit mutation (keystone)** | engineered, **live proof BLOCKED** | 0 today | +5..+15 on cross-turn / reconnect-heavy | **blocked on WSS Phase-F recert for codex 0.142.2** (§7), then the keystone apply-flip live proof (§6) |
| **Servermirror message-mass mutation** | sized, blocked on keystone apply | 0 | HIGH (+10..+30) on full-history/reconnect; ~0 on pure-delta | needs the keystone apply + WSS substrate |
| **caching (beat-default §0.10)** | engineered_pending_evidence | n/a (separate from S_local) | beat plain Codex-vs-server caching | CodexChatGPT cache-steering unblock is live-proof-gated (`internal/proxy/openai_prompt_cache.go`); A/B cache_read with vs without Slimference |

**Cross-turn lane (read-coalescing, superseded-command pruning, stale-read aging, tool-schema dedup):**
all SIZED/built but **hard-blocked on the keystone apply-flip + WSS substrate**. Their task files:
`docs/todo/new-cross-turn-read-coalescing.md`, `new-multi-turn-context-pruning.md`, `t407` (tool-schema),
`new-cross-session-content-dedup.md`. **Do not build these as separate guard-narrowing work** — they
inherit the keystone's proof.

**Research-incomplete frontier (the §0.6 "do not discard" lane):** `new-differential-output-encoding.md`
(open research). Master strategy doc: `docs/todo/savings-frontier-2026-06-25.md` §5 (has per-item DONE/
PARTIAL/SIZED status with commit refs) + §6 (research register).

---

## 6. THE KEYSTONE DEEP-DIVE (where the last session was working — `internal/proxy/wsmitm_phasef.go`, 5942 lines)

The "stateless-detach keystone" is the framework that should make every cross-turn in-transit reduction
safe. The unifying insight (AGENTS.md / `docs/todo/new-stateless-detach-keystone.md`): all cross-turn
reductions share **two drawdown vectors** — **V2** (server-state poisoning → upstream 400 when a mutated
delta turn keeps a stale `previous_response_id`) and **V3** (provider-cache bust) — and **one mitigation**
(detach `previous_response_id` + continue from a Slimference-owned exact response chain + archive-recover
+ cache-bust accounting). Build the framework once, prove it 0-400 once, every reduction inherits it.

**What is ALREADY built (the framework is far more complete than the task file implied):**
- `crossTurnKeystoneVerdict(recoveryReady, mutationRecoverable, cacheBustDemoted)` — `internal/proxy/keystone.go`.
  Fail-closed: apply-eligible iff recovery-ready AND structurally-recoverable AND not cache-bust-demoted.
  Currently **observe-only** (emits `wss.keystone_apply_eligible` telemetry; does not gate the apply yet).
- The mutation pipeline in `wsmitm_phasef.go` (key line numbers, verified this session):
  - `:633` shadow-mirror slog (`recordShadowMirror` / `wssShadowMirror` = the T254 server-state mirror,
    `internal/servermirror/mirror.go`, Observe/Predict, runs in shadow only).
  - `:810` `deltaStatelessRecoveryReady` computed via `wssDeltaStatelessRecoveryReady`.
  - `:815-817` `cacheBustDemoted` (`wssProviderCacheBustSession`, the V3 guard, 30% drop threshold).
  - `:~1270` the keystone apply-decision slog (added this session, 53ff4491).
  - `:842-849` **full_history detach**: on full_history mutated turns it `markWSSHistoryStatelessMode()`
    + `detachCodexPreviousResponseID(out)` — this lane works.
  - `:918-921` `statefulDeltaMutationBlocked = prevID!="" && deltaShape && !lab && !recoveryReady`.
    **So on delta turns the mutation is blocked UNLESS `deltaStatelessRecoveryReady` is true.**
  - `:958-1045` the reducers (stale-read aging, obsolete-read prune, superseded-command prune) — each
    applies only if `historyMutationGuardReason == ""` AND not cache-bust-demoted.
  - `:1188-1203` **THE V2-COMPLETENESS GAP**: when a *delta* turn mutates under recoveryReady, it calls
    `markWSSHistoryStatelessMode()` but the `detachCodexPreviousResponseID(out)` call is INSIDE the
    `if requestShape == "full_history"` sub-branch only. **A mutated delta turn does NOT detach the
    current turn's `previous_response_id`.** This is the precise remaining keystone engineering.
  - `:1795-1800` **THE LINCHPIN** `wssDeltaStatelessRecoveryReady`: returns true iff `toolOutputKnown`
    AND `previousResponseID != ""` AND delta-shape AND contains-tool-result AND
    **`len(wssResponseChain(previousResponseID)) > 0`** (the response chain for that id must exist).
  - `:5868` `detachCodexPreviousResponseID` (the `delete(raw, "previous_response_id")` primitive).
  - Chain population: `internal/proxy/wss_recovery.go:294` `a.responseChains[responseID] = chain`;
    `:545` `wssResponseChain`; `:561` `wssStatelessContinuationChain`; `:132` `markWSSHistoryStatelessMode`.

**Mutation config flags (DEFAULTS — all ON):** `internal/config/defaults.go:90-93`
(RepetitionDetection, StaleReadAging, ObsoleteReadPrune = true); `internal/config/config.go:1019-1020`
(CodexSearchCapDeltaMutation, CodexSearchCapStatefulFollowup = true). So the reducers DO run when not
guarded. `CodexWSSToolOutputMutationEnabled` / `CodexWSSDeltaToolOutputMutationLabEnabled` default false.

**Why the keystone delivers ~0 today (the honest causal chain):**
1. On **delta** turns (the common case), `deltaStatelessRecoveryReady` must be true to unblock mutation,
   which requires the response chain to be populated for the client-sent `previous_response_id`. Whether
   that reliably happens in production was never live-confirmed (the last session could not, see below).
2. Even if it did, the **delta-detach V2 gap** (`:1188-1203`) means a mutated delta turn would forward a
   stale `previous_response_id` → V2 → 400 risk. This MUST be fixed (detach + stateless re-expansion
   from the chain) before the delta apply is safe — but it cannot be validated while WSS is byte-bridge.
3. **And right now none of this code even runs:** on codex 0.142.2 WSS is byte-bridge (§7), so the whole
   mutation path (and the keystone slog) is bypassed.

**The proof gate for the keystone (from its task file — do these in order, do NOT skip):**
1. Re-certify WSS Phase-F mutation for codex 0.142.2 (§7) — without this nothing runs.
2. Shadow-only on a real multi-turn `slimference codex run -- codex exec` session: compute the reduction,
   do NOT apply, classify recovery, count would-be savings, require `lost=0`.
3. Flip ONE slice to apply (safest: exact repeated-tool-output dedup). Require **0 upstream 400** across
   the whole session AND follow-up turns, **0 cache-bust** beyond threshold, **byte-equal stored
   history**, recompute-verified provenance. Read the new keystone slog in the daemon log to confirm.
4. **Kill criterion:** if a mutated delta turn 400s even with detach → the route cannot support
   in-transit history mutation → record the proven protocol ceiling (§3.7) and put 100% on L2 + the
   message-mass frontier instead. This is the honest make-or-break.

---

## 7. THE #1 BLOCKER RIGHT NOW — codex 0.142.2 WSS byte-bridge (PROVEN, with evidence)

**Codex was updated to cli `0.142.2` at ~10:30 on 2026-06-25** (the `~/.npm-global/bin/codex` symlink
mtime). This invalidated the WSS Phase-F certification (cert files `~/.slimference/codex-wss-cert.json`
are from Jun 23). The honest evidence — run `slimference codex recertify wss` and you get:

```
codex recertify: WSS Phase-F proof is not green
  wss.frames_reencoded got=0 want=>0
  wss.compressed_messages_mutated got=0 want=>0
  wss.mutation_active got=false want=true
  wss.byte_bridge_only got=true want=false
codex recertify: WSS bridge proof passed and was written to ~/.slimference/codex-wss-bridge.json
```

`slimference codex status` → `Transport http`, `certified=false`, `bridge=true`.

**What this means:** Slimference can still BRIDGE codex 0.142.2's WSS frames (pass them byte-identically),
but it **cannot MUTATE them** — the Phase-F frame parser does not understand the new Codex frame format,
so it safely degrades to byte-bridge (§10.4 "Codex update degrades the frame parser to byte-equal
bridging"). Consequently the entire L3/keystone mutation path is bypassed and delivers literally nothing
on this Codex version (which is fine for safety — zero 400s — but means zero WSS savings + no way to
live-prove the keystone).

**THE REAL NEXT TASK:** update/re-certify the WSS Phase-F frame parser for codex 0.142.2's frame format
so `mutation_active=true` and `byte_bridge_only=false`. This is a **WSS-parser** task, separate from the
keystone. Files: `internal/proxy/wsmitm_phasef.go` (frame encode/decode), the recert path
`cmd/slimference/codex_cmd.go` (`recertify`/`certify`, `runCodexCertifyCmd:289`). Only AFTER this can the
keystone apply-flip live proof (§6) run. Note: L2 (process-local) is UNAFFECTED by this — it keeps
working on any Codex version.

---

## 8. THE MEASUREMENT / GATE SYSTEM (the single source of savings truth)

**The one CI gate (the single product number):** the live-corpus benchmark. Run it exactly as CI does:

```bash
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus \
  --real-local-min-ratio=0.06 --real-local-min-saved=340000 --recompute-verified-min=0
```

Floors live in `scripts/ci/main.go` (`--real-local-min-ratio=0.06 --real-local-min-saved=340000
--recompute-verified-min=0`, coverage `-min=95.0`). The full CI is `go run ./scripts/ci`.

**How recompute-verification works (the unfabricatable core, `scripts/benchmarks/benchmark_corpus.go`):**
- Each L2 sidecar line may carry provenance: `input_sha256` + `raw_gzip_b64` + `compacted_gzip_b64`.
- `loadCategoryCommandOutputFirstSidecar` gunzips both, checks `sha256(raw) == input_sha256`, recomputes
  tokens via `filter.EstimateTokensFromBytes(len(raw|comp))` and requires they equal the recorded
  input/output tokens, and requires `compacted < raw`. Only lines passing ALL of this count as VERIFIED.
- A line with no gzip bytes → unverified/observed-only (excluded). A tampered line → `errSidecarIntegrity`
  → the gate **fails closed** (not skipped — fraud is not a skippable malformed category).
- In-band has the same capability: `scripts/benchmarks/session_report.go` `recomputeInBandProvenance`
  (gunzip orig+final, sha-check, recompute), fail-closed.
- `EstimateTokensFromBytes(n) = n/4` (integer), `internal/filter/engine.go:36`.

**To grow the verified number HONESTLY:** run real `slimference codex run -- codex exec` sessions, the L2
shim writes provenance-carrying sidecars to `~/.slimference/analytics/command_output_first_cof-*.jsonl`
(provenance only embedded when raw ≤ 256KB cap, `command_output_first.go:2277`), then distribute the
genuine lines into the matching gate-counting corpus category's `command_output_first.jsonl`. The gate
recomputes them — you CANNOT fake this (fabricated bytes won't sha-verify). A category counts toward
`real_current_local_savings_ratio` only if `current_product_path:true && !synthetic && OrigTokens>0 &&
SavedTokens>0 && workload not in {provider_cache_long_session, output_reduce_aggressive, output_reduce_ab}`
(`benchmark_corpus.go` `categoryCountsTowardRealCurrentLocalRatio`).

**FORBIDDEN (§3.7.6, §3.8.1):** appending fabricated/duplicated/synthetic fixtures; raising the CI floor
without real production code that genuinely produces the saving; a commit touching only docs+fixtures+
`scripts/ci/main.go` (no `internal/`/`cmd/`) that declares a savings increase.

---

## 9. OPERATIONAL GUIDE (build, run, daemon, logs — the non-obvious plumbing)

**Build + install the binary (do this after any product-relevant change, AGENTS.md §9):**
```bash
go run ./scripts/build -restart      # stop, build (-trimpath -ldflags -s -w), install to ~/.local/bin, start daemon
which slimference                    # must be /Users/christopher/.local/bin/slimference
slimference status --preflight       # must be clean
```
Go toolchain: `go1.26.4`. (Rust build policy in CLAUDE.md does NOT apply — this is a Go repo.)

**Run a live Codex session for evidence (the production-equivalent path, §N):**
```bash
slimference codex run --transport=auto -- codex exec --dangerously-bypass-approvals-and-sandbox "<prompt>"
```
A TTY is NOT required. `codex exec` runs headless and produces real captures. Do not claim "BLOCKED: TTY
required" — that is a planning error.

**Daemon logging destinations (this tripped up the last session — IMPORTANT):**
- The daemon's `slog` (all `slog.Info/Warn/Error`) goes to a **rotating writer at
  `~/.slimference/logs/slimference.jsonl`** (configured `main.go:3806-3814`, `cfg.Logging.File` default
  `~/.slimference/logs/slimference.jsonl`). **This is the primary AI-readable log.** It is NOT
  daemon.stdout.log / daemon.stderr.log (those are launchd fd1/fd2 and mostly carry startup noise).
- L2 sidecars: `~/.slimference/analytics/command_output_first_cof-*.jsonl`.
- Adaptive re-fetch counters: `~/.slimference/analytics/command_output_first_refetch_*.json`.
- WSS recert log: `~/.slimference/logs/codex-wss-recert.log`.
- The keystone apply-decision slog (53ff4491) appears in slimference.jsonl as `msg="wss keystone apply
  decision"` — but ONLY when WSS Phase-F mutation is active (so NOT on codex 0.142.2 byte-bridge).

**Codex update gotcha (just bit us):** if `slimference codex run` hangs / `env: codex: No such file` /
`exit status 127`, check `which codex` and the symlink mtime — Codex auto-updates can transiently remove
the binary mid-reinstall and always invalidate the WSS cert. After a Codex update you MUST
`slimference codex recertify wss` (and expect byte-bridge until the parser supports the new version).

**Per-cycle checklist before claiming done (AGENTS.md §9):** conforms to `docs/spec.md`; `go test ./...`
green; coverage ≥95% aggregate; new Go logic has real `*_test.go`; new tooling only under `scripts/<topic>/`
in Go; binary built/installed + preflight; install/uninstall changes keep `docs/install.md` current +
`go test ./docs/` green.

---

## 10. THE HONESTY RULES YOU MUST FOLLOW (AGENTS.md §3.7.6, §3.8, §3.9 — non-negotiable)

This repo has a documented history of measurement fraud. The rules exist because of real incidents:
- **2026-06-22:** an agent inflated S_local 11.89% → 48.12% by appending fabricated fixtures + raising the
  CI floor, zero production code. Reverted. (§3.7.6)
- **2026-06-23/25 audits:** found nearly every savings instrument was self-reported/fabricatable; only the
  recompute gate was clean. Led to §3.9 (Measurement Instrument Honesty — Enforced).

**Before you report any savings, mark any task done, raise any floor, or commit any test change, run the
§3.8.2 mandatory critical check WITH FRESH EYES, assuming the current state is lying:**
1. **Re-derive the number** from source; if you can't, label it `unverified` and raise no floor on it.
2. **Attribution audit:** name the exact code path that *causes* each saving; confirm it would NOT happen
   without Slimference (else move it out of the trusted number — like L1).
3. **Denominator audit:** state what the denominator is; if curated, report the representative number too.
4. **Negative-control every new/changed test:** mentally or literally break the production code and
   confirm the test FAILS. If it would still pass, the test is worthless — fix it. (Last session did this
   for all 9 new tests.)
5. **Regression honesty:** if a test now fails, the DEFAULT is that the code regressed — investigate that
   first; only adjust the test after the new expected value is independently proven correct.
6. **Diff-shape sanity:** a commit changing only docs+fixtures+CI-thresholds (no `internal/`/`cmd/`) may
   NOT claim a savings/correctness improvement.

**Forbidden patterns (each a hard reject):** trust-without-recompute; misattribution (counting native as
Slimference); denominator gaming (high ratio on curated captures); blended headline (one combined number
without decomposition); circular floor-raise; test-to-mask (loosen/skip/delete a test to pass); tautological
always-green tests; coverage theater. **Guards must fit "skin-tight" (§3.4):** seal exactly the proven
failure vector and nothing more; a guard one byte broader than the evidence requires is a savings
regression.

---

## 11. FILE MAP (the code that matters, with the line refs verified this session)

```
AGENTS.md                                  # THE binding doctrine. Read §0, §3, §10 fully.
docs/spec.md                               # technical target spec v3
docs/install.md                            # install/uninstall SSOT (+ meta-test docs/install_spec_test.go)
docs/savings-ledger.md                     # the ONLY sanctioned savings record besides the gate; per-slice + ceilings
docs/todo/                                 # LOCAL planning surface, GITIGNORED by design (§2) — do NOT git add / force-add
  savings-frontier-2026-06-25.md           #   strategic SSOT §5 (sequence + status) / §6 (research register)
  new-stateless-detach-keystone.md         #   the keystone task (status: live proof BLOCKED on WSS recert)
  new-l2-desktop-command-output.md         #   DONE this session
  new-adaptive-perclass-compaction-budget.md   # DONE this session
  new-servermirror-mutation-ultra.md       #   SIZED, blocked on keystone apply
  new-l2-broad-clean-codex-native.md       #   L2 → MCP/tool-facade boundary (shares the Desktop app-server seam)
  new-cross-turn-read-coalescing.md, new-multi-turn-context-pruning.md, new-cross-session-content-dedup.md,
  new-cache-prefix-optimization.md, new-differential-output-encoding.md   # cross-turn lane + caching + research

cmd/slimference/                           # Go, the CLI + daemon (116 files)
  main.go                                  #   dispatch; gain printers (:2593 Layer-0, :2750 handleGainProxy); slog setup (:3806)
  proxy_cmd.go                             #   CLI codex run; L2 injection (:454)
  command_output_first.go                  #   L2 shim core; applyCommandOutputFirstEnvToList (Desktop L2); provenance writer (:2277 cap)
  command_output_first_adaptive.go         #   adaptive per-class re-fetch counters (this session)
  codex_desktop_app_server_shim.go         #   Desktop mediated app-server; runCodexDesktopAppServerShim (L2 injection)
  codex_desktop_launcher.go                #   Desktop launch / buildCodexDesktopAppServerEnv
  codex_cmd.go                             #   codex run|status|certify|recertify (:289 runCodexCertifyCmd)

internal/proxy/                            # the WSS/HTTP proxy hot path
  wsmitm_phasef.go (5942 lines)            #   WSS Phase-F MITM + the in-transit mutation pipeline (see §6 line refs)
  keystone.go                              #   crossTurnKeystoneVerdict (this session)
  wss_recovery.go                          #   response chain mirror, stateless mode, FIFO (:294 chain populate)
  openai_prompt_cache.go                   #   prompt-cache steering (caching lever; CodexChatGPT routes blocked)
  handler.go                               #   HTTP Responses path (server-state step 9.5)
internal/filter/                           # the L2 compactors (124 TryCompact*) + adaptive_budget.go + engine.go (EstimateTokensFromBytes :36)
internal/servermirror/mirror.go            # T254 shadow server-state mirror (Observe/Predict, normalized-segment path)
internal/config/defaults.go, config.go     # mutation flag defaults (:90-93 / :1019-1020)
internal/contentarchive, internal/toolarchive  # L2 archive substrate (slimference expand recovery)
internal/toolusecache                      # per-session reconnect-safe key sets (collapsed reads)

scripts/benchmarks/benchmark_corpus.go     # THE gate (recompute-verified S_local). session_report.go = in-band recompute.
scripts/ci/main.go                         # CI floors
scripts/build/                             # go run ./scripts/build -restart
tests/fixtures/live_corpus/<category>/     # corpus: metadata.json + command_output_first.jsonl (+ session/L1 sidecars)
```

---

## 12. NON-OBVIOUS GOTCHAS (things that will waste your time if you don't know them)

- **`docs/todo/` + `docs/todo.md` are gitignored BY DESIGN** (AGENTS.md §2). Do NOT `git add` them; do
  NOT try `-f`. They are the local planning surface. The committed record is `docs/savings-ledger.md`.
- **Daemon slog → `slimference.jsonl`, not stdout/stderr** (§9). Grep the wrong file and you'll think
  nothing logged.
- **Per-request WSS DebugFacts go to the debugRecorder**, queryable via `slimference gain --opportunities`
  / `gain --proxy`, NOT the main log. The keystone slog (53ff4491) is the readable observability hook.
- **`gain` (no args) shows only the Layer-0 filter block.** The §3.2 breakdown (S_local/cache/net-billable,
  with a "self-reported, not recompute-verified" banner) is `gain --proxy`. This is now pointed-to from
  the default output (57f0fbe9).
- **L1 server-state continuation is native — never count it as S_local.** The gate already excludes it;
  keep it that way unless a fresh live A/B proves Slimference causes the byte reduction.
- **256KB provenance cap:** large outputs (go-test-json monsters) exceed it → no embedded bytes → cannot
  be recompute-verified → excluded by design (and they're non-representative anyway). Don't re-append them
  to chase the number — that's the old denominator-gaming trap.
- **The mutation pipeline is FAR more built-out than any task file implies.** Read the actual code (§6
  line refs) before "building the apply-flip" — most of it exists; the missing piece is the delta-detach
  V2-completeness + the live proof, both currently blocked by the byte-bridge substrate.
- **Codex auto-updates silently** and invalidates WSS certs (and transiently the binary). Always check
  `codex --version` + `slimference codex status` at session start.

---

## 13. RECOMMENDED NEXT STEPS (priority order, honest)

1. **Unblock WSS Phase-F mutation for codex 0.142.2** (§7). This is the gate for the ENTIRE WSS/cross-turn
   lane. Update the Phase-F frame encode/decode in `wsmitm_phasef.go` for the new Codex frame format until
   `slimference codex recertify wss` reports `mutation_active=true, byte_bridge_only=false`. Until this is
   done, no WSS savings and no keystone proof are possible. (L2 keeps working regardless.)
2. **Then the keystone apply-flip live proof** (§6 proof gate steps 2-4): shadow-verify lost=0 → flip ONE
   slice (repeated-tool-output dedup) → live 0-400 proof reading the keystone slog → OR record the proven
   protocol ceiling. Fix the delta-detach V2 gap (`:1188-1203`) as part of step 3 (with the live proof,
   not blindly).
3. **Grow verified L2 mass** (safe, unblocked, any Codex version): run representative tool-heavy sessions,
   distribute genuine provenance captures, raise the recompute-verified floor honestly. Extend L2 to the
   MCP/tool-facade boundary (`new-l2-broad-clean-codex-native.md`).
4. **Caching beat-default A/B** (§0.10, unblocked): measure cache_read_tokens with vs without Slimference
   on identical sessions; unblock CodexChatGPT cache-steering with a live acceptance proof.
5. **Only after the structural lanes:** the adaptive-budget continuous-cap half, micro-optimizations.

**Do NOT:** rush a WSS-mutation change without a live 0-400 proof (§0.2); count native L1 or provider-cache
as S_local; append fabricated fixtures or raise a floor without real code (§3.7.6); build the cross-turn
levers as separate guard-narrowing work (they inherit the keystone proof).

---

## 14. STATE AT HANDOFF

- **Branch `main`, HEAD `53ff4491`, working tree clean.** This session's 7 commits (f040ba7a → 53ff4491)
  are committed AND pushed to origin. THIS handoff commit is committed but **NOT pushed** (per request).
- **All tests green** (`go test ./...`), gate green (S_local 6.26%), binary installed + preflight OK,
  0 upstream 400s from this session's traffic.
- **Codex cli 0.142.2** (updated 10:30 today), WSS = byte-bridge (HTTP fallback), L2 working.
- The honest product number is **6.26% S_local** (15,517 tok recompute-verified). The owner target ≥48%
  is a floor, not the goal; the path to it runs through the WSS-substrate unblock → keystone → cross-turn
  lane, plus growing verified L2 mass. Everything not marked production_ready is honestly labeled
  `engineered_pending_evidence` / `blocked` with the exact blocker named.

*Trust nothing here that you can re-verify — re-run the gate, re-read the code, re-derive the number. That
is the culture (§0.8). Good luck.*
