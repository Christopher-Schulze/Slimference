# TASK 254: Codex server-state mirror (general differential transport)

Status: [ ] QUEUED - design-first, shadow-first, mutation gated by T257/T258
Priority: P2 - the architectural nordstern that generalizes all savings
Scope: Codex-only WSS Phase-F. Replace reactive per-frame compression with a
session-level model of server-side state, and reduce every client frame to pure
novelty against it.

## Why

Today compression is reactive per frame, and read-delta is a hand-built special case
of a much more general idea. Because the Codex Responses API keeps conversation state
server-side via `previous_response_id`, the proxy can maintain a precise MIRROR of what
the model already has (reconstructed from exactly the bytes the proxy forwarded along
the `previous_response_id` chain). With that mirror, EVERY client->server frame can be
diffed against "what the model already knows" and reduced to pure novelty - for all
content classes, not just file re-reads. read-delta, non-file dedup, and search-delta
all become special cases of one differential transport. The longer the session, the
more redundancy the mirror catches, so it is the one lever that breaks the
savings-scales-with-session-length ceiling.

## Acceptance

- A server-state mirror module reconstructs and tracks server-side conversation state
  from forwarded bytes along the `previous_response_id` chain, per session.
- A generalized differential-transport pass diffs each new client frame against the
  mirror and elides/references content the model provably already holds.
- read-delta (and ideally non-file/search dedup) are reframed as special cases on top
  of the mirror, without behavior regression on their existing fixtures.
- The mirror provably NEVER elides content the server lacks (no false-elision); on any
  ambiguity it fails open (passes full).
- The t249 A/B harness shows no comprehension regression.
- Coverage gate green; doctrine clean.

## Sub-Tasks

- [ ] Design the server-state mirror: structure, per-session lifecycle, how
      `previous_response_id` chaining maps to mirror updates, memory/disk bounds.
- [ ] Implement mirror tracking from forwarded bytes (content-free identity, not raw
      retention where avoidable).
- [ ] Implement the general differential pass (diff client frame vs mirror -> novelty
      + references); fail-open on ambiguity.
- [ ] Migrate read-delta onto the mirror as a special case; keep all existing read
      fixtures green.
- [ ] Extensive correctness tests + A/B harness run proving no false-elision and no
      comprehension regression.

## Target Metrics

- 15-40% additional billable-input reduction on long multi-turn sessions after
  existing read-delta, repeated-output, search-delta, and chunk-dedup mechanisms.
- Zero false-elision: no reference may point to content the upstream server did not
  receive earlier on the same `previous_response_id` chain.
- Zero hard failures: `parse_failures=0`, `degraded_sessions=0`,
  `compression_errors=0`; any mirror ambiguity full-passes.
- Mirror overhead budget: p95 reducer overhead below 10 ms per request on captured
  workday replays, unless a benchmark proves model latency dwarfs the added cost.

## Required Architecture

- Mirror input is only the exact bytes Slimference forwarded upstream. The mirror must
  never infer server state from local files, client intent, or unforwarded archive
  entries.
- State key is a composite of WSS session identity plus `previous_response_id` chain.
  Branching or missing response ids create separate mirror branches or full-pass.
- Mirror entries store content hashes, byte ranges, tool-output identities, archive
  ids, and provenance; raw payload retention must be bounded and only where needed for
  replay/recovery.
- Differential output must be lossless or archive-recoverable. Semantic summaries do
  not belong in the mirror pass.
- Existing mechanisms (`read_delta`, repeated non-file dedup, search-output delta,
  chunk dedup) must be expressible as mirror-backed special cases before any new
  generalized mutation is enabled.

## Execution Gates

- Design gate: a written mirror data model and failure-mode table is reviewed in this
  file before code mutation starts.
- Shadow gate: mirror observes at least 5 CLI/Desktop captures and reports potential
  savings without changing frames.
- Replay gate: shadow predictions exactly match known safe reducers on current
  fixtures; all mismatches become full-pass cases.
- Mutation gate: one narrow mechanism migrates to mirror-backed mutation with existing
  tests unchanged and new A/B replay proof.
- Auto-policy gate: T258 receives mirror risk/recovery/recency signals before any
  mirror-backed mechanism can run under default `auto`.

## Notes

- % impact: ~15-40% on long sessions (grows with session length); high effort.
- This is a TASK-SPLIT candidate: design first, then split the mirror, the diff pass,
  and the read-delta migration into separate TASKs if scope grows.
- Dependencies: HARD on t249 (A/B harness + recovery). Benefits from t251 state
  plumbing (bounded state, persistence).
- Doctrine: content-free where possible, fail-open, scoped; never elide what the
  server does not have.

## Deviations

(none)
