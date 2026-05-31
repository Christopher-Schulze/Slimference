# TASK 253: Codex aggressive read compression (AST scan-mode, predictive post-edit, reasoning, patch-context dedup)

Status: [~] IN PROGRESS - first-read scan-mode IMPLEMENTED, PROVEN (savings + reconnect-safe
recovery + behavioral recovery n=2), policy-integrated (max mode, dormant in auto), and
instrumented; auto-promotion is now data-gated on real-workload re-read frequency. Other
sub-tasks (predictive post-edit, apply_patch dedup, reasoning) still queued.
Priority: P2 - high savings potential, highest drawdown risk, must be proof-gated
Scope: Codex-only WSS Phase-F. Compress reads more aggressively where it is provably
safe and recoverable.

## Why

Today the first read of a file passes full, and re-reads are only deduplicated. There
is more to take, but each item is lossy on first sight and therefore only safe behind
the t249 safety net (A/B harness + recoverable archive):

- When the model is exploring (scan/browse, not editing), it rarely needs full bodies;
  signatures + structure suffice. `internal/codecompact/api.go` already does AST-based
  compaction for Go (`renderGoSignature`, `goBodyLines`, `shouldIncludeGoBody`,
  `ExtractGoSymbolBody`); the file-read filter already uses it for large Go `cat`.
  Extending it to first-read scan-mode and more languages compresses large reads
  50-70% with recovery.
- The proxy SEES `apply_patch` (it parses the patch in `proxyLayer0EditPaths`), so it
  knows the new file content. A read immediately after a patch can be served as the
  known patched result instead of a full re-read.
- Codex Responses may carry reasoning items and apply_patch surrounding context that
  the server already holds; some of it is redundant.

## Acceptance

ALL sub-tasks start in shadow/proof mode. Promotion into `codex_savings_policy_mode=auto`
requires the t249 A/B harness to show no comprehension regression for the relevant
case, the recoverable-archive note to be live on the route, and the T258 policy matrix
to classify the workload as safe for that mechanism.

- First-read AST/structure scan-mode compression: large code reads in scan/browse
  intent return signatures + structure, recoverable via `local-archive://`,
  intent-gated (scan vs edit; edit/RecentlyEdited always passes full). Go via
  `codecompact`; extend to at least one more language.
- Predictive post-edit file state: a read right after an `apply_patch` to the same
  path is served as the known patched result (proxy-synthesized), with archive
  recovery; fail-open if the patch could not be applied deterministically.
- Reasoning-trace compaction: FIRST verify whether reasoning is actually present in the
  client->server `input` (it may be server-side only via `previous_response_id` and
  already discounted). Only then compact stale reasoning.
- apply_patch context dedup against known content.
- Coverage gate green; doctrine clean; every transform recoverable.

## Sub-Tasks

- [ ] Verify (capture-driven, content-free) whether reasoning items appear in the
      c2s `input`; document the finding before building any reasoning compaction.
- [x] First-read AST/structure scan-mode compression with intent gating + archive
      recovery (Go via `codecompact`, `MaxIncludedBodyLines=12`). Wired in
      `internal/proxy/layer0_proxy.go` as the `scan_read` mechanism; triple recovery
      (note + `local-archive://` ref + read-key registration). Recovery made
      reconnect-safe via persisted collapsed keys. Promoted to a `savingspolicy`
      decision (`ScanRead`, max mode only, dormant in auto). Proven live + instrumented.
      OPEN follow-ups: extend `codecompact` beyond Go for one more language; promote to
      auto once the re-read-frequency gate (below) clears.
- [ ] Extend scan-mode `codecompact` beyond Go for one more language; fixtures.
- [ ] Predictive post-edit file state from the parsed `apply_patch`; fail-open on
      ambiguity; fixtures.
- [ ] apply_patch context dedup against known content.
- [ ] Stale reasoning compaction (only if verified present in input).

## Target Metrics

- First-read scan-mode: 20-50% input-token reduction on exploration-heavy code-read
  captures while preserving exact edit/debug behavior.
- Predictive post-edit: 5-15% reduction on patch/read cycles; zero wrong-file or
  stale-version substitutions.
- apply_patch context dedup: 3-10% reduction where patch context repeats known file
  content.
- Reasoning compaction: only measured if c2s reasoning items are actually present;
  otherwise close that sub-task as "not applicable on current Codex WSS wire".

## Promotion Gates

- Capture gate: at least 2 CLI and 2 Desktop captures for each mechanism's workload
  class, plus one long mixed workday capture.
- Replay gate: `wss-ab-replay --fail-on-lost --json` reports `gate_passed=true`,
  `parse_failures=0`, `degraded_sessions=0`, and no unexpected elisions.
- Comprehension gate: A/B harness shows no missing model-facing facts compared with
  direct/full context. Any ambiguity keeps the mechanism shadow-only.
- Recency gate: no post-collapse re-read canary spike after enabling the mechanism.
- Recovery gate: every model-facing elision has a `local-archive://` recovery handle
  and the recovery note is injected only when needed.
- Policy gate: T258 classifies the mechanism as auto-eligible only for workload
  classes where all above gates passed.

## Technical Design Requirements

- First-read scan-mode must distinguish scan/browse intent from edit/debug intent via
  parsed command shape, recent-edit state, and model-issued tool purpose. Edit/debug,
  recently edited files, small files, and unknown commands full-pass.
- Scan-mode output must be structured, neutral, archive-backed, and stable under
  replay. Go uses `internal/codecompact`; the second language must reuse an existing
  parser or a deterministic lightweight extractor with tests.
- Predictive post-edit may synthesize a known post-patch file state only when the
  patch applies deterministically to the last known file bytes. Any mismatch, fuzzy
  hunk, missing base, binary file, or multi-file ambiguity full-passes.
- apply_patch context dedup may remove only context that is byte-identical to known
  file state already forwarded to the server. It must preserve new/removed lines and
  hunk metadata.
- Reasoning compaction cannot be implemented from speculation. First capture the
  actual Codex c2s input shape; if reasoning is server-side only through
  `previous_response_id`, document that and do not build dead code.

## Notes

- % impact: first-read scan compression ~20-50% on exploration-heavy sessions, but
  highest drawdown -> strictly gated. Predictive post-edit ~5-15%. Reasoning ~0-15%
  (verify first). Patch-context dedup ~3-10%.
- Dependencies: HARD dependency on t249 (A/B harness + recoverable archive). Do not
  ship any of this default-on without the comprehension proof.
- Doctrine: content-free, fail-open, scoped; every aggressive transform must be
  recoverable so a loss is never permanent.

## Progress (2026-05-31) - first-read scan-mode

Commits (all CI-green):

- `d1fd30e` scan-mode read shadow measurement (env-gated `SLIMFERENCE_SCAN_SHADOW`,
  telemetry-only). Shadow proved 66-93% would-save on real Go reads.
- `1791832` scan-mode apply wired on Codex WSS reads, default-off behind
  `SLIMFERENCE_SCAN_APPLY`. `scan_read` mechanism registers the read key in
  `ReadDeltaKeys` so a re-read full-passes (the recovery path), plus the discoverable
  note and the `local-archive://` reference. Triple recovery.
- `51555bb` persist collapsed read keys (`toolusecache.CollapsedKeysDir`) so the re-read
  full-pass recovery survives a WSS socket reconnect. Without this, Desktop (which
  reconnects per turn) lost the recovery and a re-read got signatures again.
- `e7773d2` fix flaky Codex desktop-launcher probe-timing test (25ms->250ms) that was
  starved under parallel CI load. Production probe is 750ms; no weakening.
- `a7ffbc4` make scan-mode a `savingspolicy` decision: `CodexToolOutputDecision.ScanRead`,
  enabled only in `max` mode + `IsRead` + `ArchiveRecoveryAvailable` + not Loosened.
  Dormant in the default `auto` mode. Env flag stays as a force-on test override.
- `285fb5b` re-read frequency counters (`scan_reads_applied`, `scan_read_rereads`);
  scan-origin keys persisted (`toolusecache.ScanReadKeysDir`, reconnect-safe).
- `0666699` expose both counters in `/admin/state` savings telemetry.

Proofs (all live via Codex.app, Desktop, hard-verified):

- Savings: 66% on a 4593-line Go read (7305 billable tokens live, matched shadow).
- Recovery reconnect-safe: a re-read after a Desktop reconnect full-passes
  (`requests_modified=1`, billable did not double, `wss-ab-replay` lost=0). The first
  naive wiring doubled billable (recovery broke across reconnect); the persisted
  collapsed key fixed it.
- Behavioral recovery, n=2, two modalities: the model self-re-reads when it needs an
  elided body and recovers the exact facts. Probe 1: numeric primes 7919 + 104729
  (`deriveSessionToken`). Probe 2: string secrets `ORCHID` + `vault::<seg>::granted`
  (`validateLicenseKey`). Both: scan elided the >12-line body, `resolved` jumped (model
  re-read on its own with no instruction), re-reads full-passed, exact facts returned.
  Elision verified deterministically per probe via `codecompact.Compact` before each run.

## Auto-promotion gate (economics, the real blocker)

Recovery proves that missing bodies can be fetched again, but it does NOT make first-read
elision a product-default safety proof: the model still sees less file information until it
notices and re-reads. Auto-on-every-read is also net-positive on SAVINGS only if enough reads
never need their bodies. A body-needed read costs ~34% (scan) + ~100% (full re-read) = ~134%
vs a 100% baseline; a body-not-needed read saves ~66%. Break-even is at re-read rate
`B/A < 0.66`. The instrument measures the real rate:
`scan_reads_applied` (A) and `scan_read_rereads` (B) in `/admin/state` savings. To
finish: run real Codex work with scan on (`SLIMFERENCE_SCAN_APPLY=1` daemon, or `max`
mode), read `B/A`, and prove no first-read information weakening in the A/B harness. If
either proof fails, keep scan out of product auto. Do not flip auto on intuition.

## Real-workload finding + sed extension (2026-05-31)

Live capture of a real codebase-exploration session (30 tool calls, content-free
command extraction) revealed how Codex actually reads: `sed -n '1,Np' file` is the
dominant pattern (11x), plus `rg` (search) and `find`. Codex NEVER uses `cat`
(0 calls). Because scan-mode's file-read path gated on `isFullFileCat`, scan was
structurally unable to fire on real Codex behavior: 0/30 reads scan-eligible,
`scan_reads_applied=0` confirmed live. scan-mode as originally built was effectively
dead code for Codex.

Meanwhile the lossless reducers performed well on the same real workload: 36533
billable tokens saved (Codex exec-envelope etc.) with `parse_failures=0`,
`compression_errors=0`, `degraded_sessions=0` - zero drift. The recurring
`400 invalid_request` that interrupted long sessions is UPSTREAM and correlates with
oversized requests (a single `rg "TODO|panic|..."` returned 42631 tokens); Slimference
stayed clean (2 mutations that session, 0 compression errors) and its compaction
REDUCES request size, making 400s less likely rather than more.

Fix (`a208abf`): scan-mode now fires on `sed` partial reads too. `sed` was added to
the `TryStripCommentsFileReadWithContext` command whitelist in
`internal/filter/builtin_read.go` (previously cat/head/tail only). For Go reads >=3000
bytes the regex `ExtractStructure` path (which, unlike the AST `codecompact`, handles
partial/invalid Go) elides bodies to signatures with the recovery note; the proven
collapsed-key re-read recovery applies (key includes the sed range); edit-mode /
recently-edited / force-full reads full-pass (risk-scope). Unit-proven; 1066 filter
tests green, no regression. Still gated by `ScanRead` (max mode / `SLIMFERENCE_SCAN_APPLY`),
so default-off in auto.

Product framing (per the user): a feature is useful only if it works automatically without
making the model dumber, less informed, less context-faithful, or more likely to hallucinate.
Recovery reduces the risk of first-read elision, but it is not enough for default-on by
itself because the model may not know that the missing body detail matters. Product auto
therefore prioritizes deterministic cache hits and repeat-output reuse; scan remains a
lab/max-only mechanism until it can meet that stricter bar.

Self-regulation BUILT (reconnect-safe, tested): per-session A set (scan-elided keys,
`toolusecache.ScanReadKeysDir`) and B set (those re-read, `toolusecache.ScanRereadKeysDir`)
persist across reconnect; `wsPhaseFAdapter.scanSelfRegBlock()` suppresses scan once
`|A| >= 6` and `|B|/|A| >= 0.5` (conservatively below the ~0.66 token break-even), wired
through `codexLayer0Request.ScanReadSelfRegBlock` so scan backs off before it keeps losing
tokens in a session. This guard now also applies to the `SLIMFERENCE_SCAN_APPLY` lab path.
Active whenever scan runs (currently max mode/lab only).

AUTO STILL BLOCKED - new finding (2026-05-31): flipping `ScanRead` into `auto` is NOT a
clean win. Scan-compacting the FIRST read cannibalizes the lossless read-delta/chunk-dedup
seeding: a later repeat read full-passes via scan recovery instead of deduping against the
full first read, so a repeat-read workload goes net-negative (~134% vs ~100% baseline) and
the proven lossless savings (36533 tokens/session) degrade. Multiple WSS tests (read-delta
seeding, auto chunk-dedup, A/B replay recoverable) broke when auto scanned - that is a real
regression signal, not test cosmetics. So scan stays max-only/lab-only; auto promotion is
blocked until a design proves both no first-read information weakening and no lossless-cache
cannibalization, not just the re-read economics.

Search-output compaction DONE (`1a2d478`) - the better, conflict-free lever. Root cause
(verified from the capture): `groupSearchResults` abandoned the whole output on the first
colon-less line, and Codex's truncated exec output always has one (cut-off tail + a leading
`Total output lines: N` header), so search grouping NEVER fired on real Codex searches. Fix:
skip colon-less noise lines, bail only when nothing parses or noise dominates
(`skipped*2 > nonEmpty`). On the real captured `rg` (402 matches, 79 files) this compacts
40 KB -> ~9 KB (78%). Default-auto in the filter pipeline; low-risk (match count +
which files kept, re-run the search to recover dropped matches); a search-output reducer, so
NO first-read-seeding conflict. Also shrinks requests, mitigating the upstream oversized-
request 400.

OPEN:
- Design the scan<->lossless interaction so auto-scan does not cannibalize read-delta/chunk
  seeding; only then promote first-read scan into auto (self-regulation is already built).
- Live-verify `sed` scan fires on a real Codex session (`applied>0`) under max.
- Fixed: the `Total output lines: N` envelope-header line is stripped as Codex metadata, not
  grouped as a fake search file.

## Deviations

(none)
