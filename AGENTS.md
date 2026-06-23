# Slimference - Agent and Developer Rules

This document is **binding** for all automated agents (Codex, Claude Code,
Cursor, and others) and for humans working in this repository. Deviations
require explicit project approval.

---

## 0. Product Vision and Operating Doctrine (Binding, Non-Negotiable)

This section is the single source of product intent. Every agent and every
human reads it **first** and may not drift from it. If any other section, task
file, plan, prior commit, or convenience shortcut conflicts with it, this
section wins. It exists so that no agent ever forgets the vision again.

### 0.1 What Slimference is

Slimference makes **Codex** cheaper by reducing input/context tokens
("savings") while the model's product behavior stays **identical**. Codex is
the **sole focus** (Codex CLI + Codex Desktop App). Claude Code and other-agent
code may stay in the tree but are **not** worked on and are never the target
(see §10.0). The whole thing must run **perfectly** and be **autonomously,
perfectly developed**.

### 0.2 The one hard constraint — the Drawdown Policy

Savings exist **only** inside the drawdown policy (§3). A finished savings
mechanism must be **zero, near-zero, or controlled-near-zero drawdown**.
Concretely, no optimization may, in normal operation, cause the model to:

- become less intelligent, less reliable, or worse at its actual work;
- produce avoidable errors — target is **zero errors / zero upstream 400s**;
- degenerate, regress, or behave abnormally;
- lose or corrupt context, memory, recency, salience, or file/tool reality;
- hallucinate or reconstruct content incorrectly;
- lose any other capability.

A mechanism that risks any of these in normal operation is **not** a product
feature until the risk is engineered away and **proven** gone. Development
effort, captures, benchmarks, proofs, and longer engineering are **never**
drawdowns (§3).

### 0.3 The goal — maximum savings under that constraint, RTK-class and beyond

Within the drawdown policy we want savings **as high and as broad as possible**
— there is **no fixed ceiling**. The explicit benchmark is **RTK**: whatever RTK
achieves with its command/tool-output compaction, Slimference must **match and
exceed** — **better and broader** command/workload coverage. The primary metric
is the strongest practical **local** savings `S_local` (provider-cache
excluded; §3.2). The layered architecture (L1 server-state, L2 command-output,
L3 WSS history subset, provider cache, output-wire, and future levers) is the
means to get there.

### 0.4 Where it runs — product scope (hard boundary)

- **Codex Desktop App** must work; this requires the **WSS / WebSocket** path
  (WSS is the production transport, §10.1).
- **Codex CLI** must work.
- In the **final product**, Slimference is active **only** for sessions started
  via the **TUI / Launch Center**, which launch the Codex App or CLI through
  Slimference. In development/testing the equivalent is the explicit
  `slimference codex run` / scoped commands.
- A Codex session started **normally** (plain `codex`, or the App/CLI not
  launched via the TUI/command path) runs **direct and untouched**.
- **ChatGPT in the browser and the ChatGPT app are ALWAYS untouched** — no
  proxy, no hosts patch, no interception, no base-URL rewrite, ever (§10.0,
  §10.4). How the savings are achieved internally is open; this boundary is not.

### 0.5 Autonomous development, zero blockers

Development is **fully autonomous** and follows every rule here. "I need a new
capture / live data / a TTY" is **never** a blocker: capture genuine live
evidence autonomously via `slimference codex run -- codex exec ...` (§N). Prove
levers on real sessions; never stall waiting for input the agent can generate
itself. Autonomy never overrides the honesty rules (§0.8, §3.7.6).

### 0.6 Innovation doctrine — never discard a high-savings idea on first contact

A raw idea that looks lossy or drawdown-violating is a **starting point, not a
verdict**. The agent must actively **research and invent** levers, think out of
the box, and for risky ideas **design the additional mechanism, mitigation,
module, guard, recovery, or proof** that brings the complete design back inside
the drawdown policy — **no matter how expensive the engineering**. If the fully
engineered design is **proven** drawdown-policy-conformant on the exact
route/workload where it runs, it is a valid lever — potentially a dramatic one.
This reframing is **mandatory** in every analysis, research pass, and savings
evaluation (§3.3). The expectation is that this discipline yields
unconventional, innovative, new savings that naive analysis would have thrown
away.

### 0.7 Productive-first, no rabbit holes

Drive the high-leverage levers to **live proof first** (lever priority §3.6,
§3.7). Do **not** get lost in micro-optimizations or rabbit holes during
build-out. Micro-optimization is a **later phase**, only once the main levers
stand and are live-proven.

### 0.8 Honesty and fresh-eyes reality checks (binding)

Every savings claim must be **real, measured, attribution-correct, and
representative**. It may never be inflated by curated fixtures, self-reported
numbers, a gamed denominator, or by counting **Codex-native behavior** (e.g.
native `previous_response_id` continuation) as Slimference savings (§3.2,
§3.7.6, §3.7.7). Phasewise the agent must run **hardcore, self-critical reality
checks with fresh eyes**: re-derive the real product number from scratch,
distrust prior green cycles, and straighten out any BS before continuing. The
mandatory anti-masking / test-honesty check that makes this concrete is **§3.8**
— it is not optional.

### 0.9 Everything is planned and continuously evolved

All work is tracked as tasks/plans (`docs/todo/`, `docs/savings-ledger.md`).
The agent continuously **finds, documents, evaluates, and matures** new lever
ideas and keeps improving the product — without violating the No-New-Tooling
discipline (§3.7.2).

---

## 1. Normative Documents

| Source | Role |
|--------|------|
| `docs/spec.md` | Current **technical target specification** v3 (implementation-relevant). |
| `docs/install.md` | **Install/Uninstall SSOT** (Scoped Codex, 2026-05-17): humans and agents read this for `install`, `status --preflight`, scoped `codex run|enable|disable|status`, and global-lab `root-arm --global-chatgpt-hosts`. Meta-test `docs/install_spec_test.go` keeps spec and code synchronized. |

---

## 2. Local-Only Planning Surface

`docs/todo.md` is a local planning surface and is intentionally gitignored.
It is NOT tracked in git and will never be staged or committed — this is by
design, not a mistake. Do NOT attempt to `git add docs/todo.md` or force-add
it. Current agents start from `AGENTS.md`, `docs/spec.md`, and
`docs/install.md`; use `docs/todo.md` only when it exists in the local checkout.

---

## 3. Product Drawdown Definition (Binding)

A **drawdown** is exclusively a disadvantage in productive runtime behavior for
the user or the model. Development effort is **not** a drawdown. Captures,
benchmarks, proofs, tests, engineering effort, longer implementation time, and
more expensive verification do not count as drawdowns.

Unacceptable product drawdowns include:

- The model becomes less intelligent, less reliable, or worse at its actual
  work.
- The model loses context, memory, recency, salience, or relevant file/tool
  information.
- The model hallucinates, drifts away from real repository/file/tool reality, or
  reconstructs content incorrectly.
- Codex/agent workflow, UX, tool usage, recovery, compaction, or routing becomes
  worse, more confusing, more fragile, or slower in a user-relevant way during
  normal operation.
- Functions, memory, context-window usability, or model capabilities are
  restricted by an optimization.

Savings mechanisms may be default-on only when these product drawdowns are
eliminated or practically ruled out by deterministic guards, recovery,
fail-open behavior, and live proof. An optimization that only makes sense behind
a manual experiment switch or risks model quality in normal operation is not a
product feature.

### 3.1 Savings Regression Discipline (Binding)

Slimference's product goal is **maximum practical savings with zero or near-zero
product drawdown**. Stability fixes must preserve as much of the current savings
surface as possible. Agents must treat avoidable savings loss as a product
regression, not as harmless cleanup.

When fixing bugs, invalid requests, Codex drift, WSS/Desktop instability,
routing instability, or any other savings-related failure:

- Start from a clean, committed baseline. Check `git status` before edits. Do
  not stack unrelated or uncommitted experiment state.
- Use small local commits at meaningful hypothesis boundaries. Do not push
  broken or unverified experiment commits. Push only after the repository is
  back at a verified good state.
- Identify the exact failing mechanism, route, workload, and guard condition
  before disabling anything. Broad feature disables are forbidden unless the
  exact blast radius proves they are the smallest safe fix.
- Prefer the narrowest possible patch: exact predicate, exact route, exact
  workload, exact request shape, exact Codex version drift, exact tool-output
  class. Keep unrelated savings mechanisms active.
- Quantify the tradeoff every time savings are reduced: which layer/mechanism
  loses savings, which workloads are affected, whether input/output/cache
  savings change, and the expected practical impact.
- If a disabling or narrowing change does **not** fix the failure, revert that
  change before trying the next hypothesis. Do not accumulate permanent savings
  loss from disproven fixes.
- After a stability fix works, immediately look for safe recovery of savings:
  can the mechanism be re-enabled behind a tighter guard, exact state mirror,
  retry/replay, validation, fail-open path, or route/workload-specific proof?
- Never trade a small bug for a large permanent savings regression if a more
  precise fix is feasible. Engineering effort is acceptable; product drawdown
  and unnecessary savings loss are not.
- Report the outcome plainly to the user: root cause, exact fix, remaining
  disabled/narrowed behavior if any, savings impact, drawdown impact, tests run,
  installed binary status, commit, and push status.
- For every accepted good state after product-relevant changes, run the
  required gates, build/install the latest local binary with
  `go run ./scripts/build -restart`, verify `which slimference` and
  `slimference status --preflight`, then commit and push unless the user
  explicitly says not to push.

### 3.2 Local Savings Non-Regression (Binding)

Provider-cache savings are valuable but must never hide a local input-savings
regression. Reports, plans, guards, and reviews must treat local input
reduction (`S_local`, excluding provider-cache discount) as a first-class
product metric alongside combined billable savings.

The owner target is **>=48% local input reduction on longer eligible Codex
sessions without counting provider-cache discount** while preserving the
drawdown definition above. Until that target is live-proven or a specific
route/protocol ceiling is proven impossible, agents must keep searching for and
shipping default-on-safe local savings. A guard that prevents a real drawdown is
correct, but it must be the narrowest possible guard and must preserve
observation, cache seeding, telemetry, and future safe savings wherever those
actions do not mutate model-visible or upstream-visible bytes.

**Cache reporting mandate (binding):** Caching must hit as often and as
efficiently as possible — cache hit rates are a first-class optimization target.
However, every savings report, gate output, analytics event, and planning
document must **always report both numbers**: `S_local` (without
provider-cache discount) AND combined billable savings (with provider-cache
discount). The two numbers are complementary, not alternatives. A change that
improves cache hit rates but regresses `S_local` is a partial regression, not a
win. A change that improves `S_local` but hurts cache hit rates is a partial
win, not a full win. Both directions must be quantified in every savings report.
The standard CI gate and analytics output must include:
(1) `real_current_local_savings_ratio` (S_local, no cache),
(2) `provider_cache_read_tokens` / `provider_cached_tokens` (cache hit metrics),
(3) `net_billable_equivalent_estimate` (combined, with cache discount at 0.9x).

When changing any savings-related path:

- Separate `S_local`, provider-cache discount, output savings, and combined
  savings. Do not use provider-cache wins to declare a local-savings goal met.
- Treat avoidable local-savings loss as a regression even when combined savings
  remain high due to provider caching.
- Prefer byte-equal observe-only learning over disabling a mechanism entirely
  whenever observation cannot affect model behavior, upstream state, routing,
  cache prefix bytes, or product latency in normal operation.
- Any new guard or widened guard must name the exact drawdown vector it prevents
  and the exact evidence proving that vector exists. Guards without a proven
  vector are handbrakes and must be removed or narrowed.
- Tests for guarded paths must prove both sides: forbidden mutation stays
  byte-equal, and safe observation/telemetry/seeding still happens when it can
  recover future local savings without drawdown.
- The standard live-corpus CI gate must include Promotion/Maxx proof breadth and
  an explicit `S_local` floor (`real_current_local_savings_ratio` plus
  `real_current_local_saved_tokens`). Updating that floor requires fresh
  evidence and must not count provider-cache discount as local savings.

### 3.3 Aggressive Savings Mitigation Doctrine (Binding)

Agents must not reject a high-savings idea merely because its first naive form
has drawdown risk. First identify the exact drawdown vector, then evaluate
whether additional engineering can eliminate or tightly control it while
preserving the product drawdown definition above. Valid mitigation patterns
include byte-equal fail-open paths, stateless detach, metadata-consistent
mutation, archive/replay recovery, shadow A/B proof, route-scoped demotion,
cache-bust accounting, bounded proof latches, and content-free live capture
gates.

Agents must actively search for aggressive, innovative, and composite savings
designs. A raw candidate that violates the drawdown policy is not the final
analysis state; it is the starting point for engineering. Reframe it into
narrower predicates, route-specific or request-shape-specific variants,
state-mirrored variants, capability-mirrored variants, stateless continuation
variants, recovery-backed variants, proof-latched variants, or other adapted
forms that can still produce savings while becoming policy-conformant. Small
policy-safe savings count: many narrow wins are valid product progress.

Complex mitigation work is allowed when it is the path to policy-conformant
savings. Larger mechanisms, helper systems, proof tooling, state mirrors,
metadata repair, replay/re-run recovery, capability recovery, telemetry, or
guard refinement are engineering cost, not product drawdown. The acceptance bar
does not change: the complete engineered design must still be default-on-safe,
measured, recoverable/fail-open, and exact to the route/request class where it
runs.

Agents must estimate potential savings before production-readiness evidence is
complete so work can be prioritized intelligently. Missing readiness evidence
means "not production-ready yet", not "zero savings" and not "finished".
Estimates may be rough when evidence is incomplete, but they must still be
attempted and labeled with confidence, assumptions, affected routes/workloads,
expected `S_local` impact, provider-cache impact, output impact, and remaining
readiness gaps. Product activation still requires evidence that the complete
mitigated design has zero, near-zero, or controlled-near-zero product drawdown
on the exact route/request class where it will run.

Potential estimates always assume the candidate has been fully engineered,
tested, guarded, measured, and proof-passed. The proof is the production
activation gate, not the value model. Never answer "0 potential" merely because
the current implementation is not proof-passed yet. Report two separate values:
`candidate_potential_if_completed` and `current_production_ready_savings`.
Before proof, the second may be `0`; the first must still be estimated from the
best available code, telemetry, route physics, comparable mechanisms, and
explicit assumptions. This separation is mandatory for every savings plan,
roadmap review, guard review, and candidate comparison.

Agent reports must not use missing-readiness language as a dismissal or value
judgment. Use `candidate`, `not production-ready yet`, `readiness gap`, and
`next engineering move` instead. A readiness gap changes activation status; it
does not erase the estimated savings value or remove the candidate from
planning.

Feature status must stay explicit:
`estimated_candidate` for a plausible candidate mechanism,
`engineered_pending_evidence` for implemented or partially implemented work
that still lacks the required readiness evidence, and `production_ready` only
after the required tests, live evidence, recovery/fail-open checks, and
non-regression measurements pass. Agents must not present an estimated or
engineered-pending feature as done, but they also must not ignore its estimated
value when planning.

A mechanism is forbidden only after the mitigated complete design still has
unacceptable product drawdown, or after live evidence proves the route/protocol
cannot support it. Until then, agents should keep searching for narrower
predicates, better recovery, state mirroring, metadata repair, replay/detach
strategies, or other engineering that turns an aggressive candidate into a
default-on-safe product mechanism.

### 3.4 Savings Non-Regression Measurement Loop (Binding)

No accepted product change may silently regress local savings, provider-cache
stability, output savings, tool-surface savings, routing safety, or the product
drawdown policy. Every savings-related change must be measured against the
current clean baseline with the relevant focused tests, live-corpus gate, and
route-specific proof tooling before it is called done.

If a change intentionally narrows or disables a savings path, the agent must
record the exact reason, drawdown vector, affected mechanism, affected route,
affected workload/request shape, expected `S_local` impact, provider-cache
impact, and the smallest recovery path. If the narrowing does not fix the
failure it targeted, it must be reverted or narrowed further before any other
savings loss is accepted.

When a guard blocks savings, the agent must verify whether byte-equal
observation, cache seeding, telemetry, candidate scoring, shadow evidence, or
future-proof state capture can remain active without changing model-visible
bytes, upstream-visible bytes, routing, cache-prefix bytes, or normal product
latency. Disabling those safe side effects is itself a local-savings regression.

Guards are not static handbrakes. Agents must continuously engineer guards
toward the loosest safe predicate that still prevents the proven drawdown or
error vector. That means splitting broad guards by route, request shape,
response lineage, command class, content class, cache-prefix scope, socket
state, proof state, and recovery availability whenever doing so preserves
safety and recovers savings. The target is maximum practical savings with
point-accurate protection: no upstream 400s, no invalid requests, no cache-bust
regression, no model-quality regression, and no tool/workflow degradation.

Guards exist to eliminate proven errors and drawdowns with the minimum possible
savings loss. No percentage point of local savings may be wasted by applying a
guard more broadly than the evidence requires. Do not preemptively suppress a
route, mechanism, workload, command class, content class, or request shape
merely because a hypothetical error might happen there. Use measured failure
evidence, counterfactual replay, guarded-potential accounting, and exact
request-class arithmetic to decide the smallest safe guard. If the same safety
can be achieved with a narrower predicate, observe-only path, recovery path,
proof latch, or scoped demotion, the broader guard is a savings regression.

If a guard remains broad, the agent must be able to explain why narrower
engineering is not yet production-ready, what evidence is missing, and what
mitigation would allow the next safe loosening. Broad guards without that live
failure evidence and next-readiness path are considered unfinished savings work.

### 3.5 Command-Output-First Savings Mandate (Binding)

Slimference must pursue an RTK-class command-output-first lane for Codex. The
preferred savings point is before large shell/tool output becomes durable
model-visible WSS history. If Codex exposes a hook, launcher shim, app-server
control point, PTY boundary, command wrapper, MCP/tool proxy, or process-local
subprocess boundary that can compact stdout/stderr before the model stores it,
agents must evaluate and engineer that path before spending comparable effort
on smaller downstream WSS cleanup.

The target is the same economic class as RTK's strongest surface: exact,
parser-bounded command-output compaction on reads, search, git, build, test,
lint, logs, JSON, tables, package managers, and CI-style transcripts. The Codex
implementation may be different from Claude/RTK hooks, but the product target is
not optional: recover large local `S_local` by intercepting or shaping command
output as early as the scoped Codex architecture permits.

Any command-output-first design must preserve:

- exact command, cwd, args, env-sensitive behavior, exit code, stdout/stderr
  semantics, ordering, and stream/error distinction;
- model access to all relevant failure, warning, source, diagnostic, path,
  line, count, and artifact facts;
- byte-equal fail-open on unknown command shapes, parser drift, unsupported
  shells, malformed streams, missing archives, unsafe source/report-file
  payloads, or capability ambiguity;
- scoped routing only: no persistent global proxy, base URL, system proxy,
  hosts patch, or unrelated app interception;
- local raw-output recovery through archive/tee/rerun when compacted output
  omits bytes, with retry/rerun cost counted as negative savings.

Lack of a ready Codex hook is not a stop condition. Agents must search for a
different engineering seam: scoped shell wrapper, command-runner proxy, PTY
capture, app-server shim, local tool facade, Codex hook configuration,
sideband recovery, or deterministic rewrite of command invocations emitted by
the scoped launcher. If all current Codex surfaces reject safe pre-output
interception, record the exact blocked control point and the next route to test.

### 3.6 High-Leverage Savings Priority (Binding)

Agents must not spend the main engineering loop on low-impact micro-polish while
large local-savings blockers remain open. Small wins are valid when they are
cheap, unblock a gate, expand a high-frequency parser class, or can be shipped
while waiting for live owner input. Small policy-safe wins should compound, but
they must be bundled efficiently with nearby high-leverage work instead of
becoming their own optimization rabbit hole. Until all materially sensible
savings lanes have been harvested or proven blocked, micro-optimizations are a
deferred phase, not the main task. Otherwise, prioritize structural moves that
can change `S_local` by double-digit points:

- command-output-first Codex interception;
- T354/Class-B/server-state continuation engineering;
- Desktop/Class-B distribution capture and route-specific unlocks;
- search-cap and captured-output promotion on common WSS shapes;
- stateful-safe parser classes only when they hit common real workloads or feed
  the larger unlocks;
- cache-bust and recovery guards only when they recover broad blocked surface.

Every task plan must name the expected local-savings order of magnitude before
implementation: low (<1 point), medium (1-5 points), high (5-15 points), or
major (15+ points) for the relevant route/workload. When a higher-leverage task
is blocked by required live input or a missing control point, the agent may work
the next best offline lever, but must keep the high-leverage blocker visible and
return to it as soon as the blocker is removable. After the structural lanes are
exhausted, agents should continue down the optimization stack and compound
smaller wins, including micro-optimizations, as long as each change remains
measured, policy-safe, and net-positive.

### 3.7 Loop Discipline and Anti-Rabbit-Hole Rules (Binding)

These rules exist because a prior autonomous loop produced 1000+ commits and
~44k lines of measurement tooling while the proven production `S_local` stayed
frozen at ~6% against the 48% target. The work was green every cycle but never
moved the product number. The following rules are binding to prevent that:

1. **Single-Gate rule.** There is exactly one product success number: live
   `S_local` (excluding provider-cache discount), measured by the standard
   live-corpus / live-capture gate. Every lever must be measured by that one
   gate. If a mechanism's savings are not counted by the gate, wiring it into
   the gate is the first task, not an afterthought. Work that cannot move the
   single gate is not product progress, regardless of how much code it adds.

2. **No-New-Tooling rule.** Do not add a new measurement, proof, ranking,
   inventory, or "headroom" tool without deleting or merging an equal-or-greater
   amount of existing tooling. Proof/measurement infrastructure is engineering
   cost, never a product deliverable. Estimating `candidate_potential` is
   required (§3.3) but must be a short paragraph, not a new tool. When in doubt,
   one consolidated live gate plus `docs/savings-ledger.md` is the only
   sanctioned savings-measurement surface.

3. **Loop-Termination rule.** A work cycle terminates successfully when EITHER
   the single live `S_local` gate number rises (commit the slice) OR a concrete
   root-cause ceiling is proven and recorded in `docs/savings-ledger.md` (close
   the lane). A cycle must NOT be kept open merely because other unblocked tasks
   exist. A disproven hypothesis is reverted in the same cycle, never
   accumulated as dead code. No task may be defined as "always active".

4. **Lever-Priority rule.** Attack levers in order of token-mass × safety, not
   in order of how easy a commit is to make. The current ranking is fixed until
   re-proven: (L1) server-state continuation, (L2) command-output-first, (L3)
   WSS history mutation. Broad WSS history/structure mutation on delta turns and
   all micro-optimization are forbidden as main-loop work until L1 and L2 are
   live-proven on a real session.

5. **Handbrake rule.** A savings mechanism may be default-off only with a named,
   live-proven drawdown vector (§3.4). "Traffic shape stays unchanged until you
   flip the switch" and similar are NOT proven vectors; they are unfinished
   work. Default-off switches without a proven vector must be scheduled for
   drawdown-safe activation, not treated as permanent.

6. **No-Fixture-Inflation rule (binding, post-2026-06-22 incident).** An
   autonomous agent produced 17 commits that inflated `S_local` from 11.89% to
   48.12% by appending fabricated fixture data to `server_state_continuation.jsonl`
   files and raising the CI floor to match — with zero production code changes.
   This was circular measurement manipulation: append fixture data → raise CI
   floor → declare higher S_local. To prevent recurrence: (a) every fixture line
   in `tests/fixtures/live_corpus/` must trace to a real `slimference codex run`
   session capture — fabricated, duplicated, or synthetic data is forbidden;
   (b) raising the CI floor in `scripts/ci/main.go` is forbidden without a
   corresponding real production code change that genuinely produces the savings
   being measured; (c) a commit that modifies only docs, test fixtures, and
   `scripts/ci/main.go` (no `internal/` or `cmd/` production code) must not
   declare a savings increase. Violating this rule is the most severe savings
   violation — it produces zero real savings while making CI pass on fake data,
   masking real regressions.

7. **No-Misattribution rule (binding, post-2026-06-23 integrity review).**
   Only **incremental Slimference-caused** byte reduction may be counted as
   `S_local`. Behavior that Codex/the provider would exhibit **without**
   Slimference is not a Slimference saving and must be reported separately.
   The concrete proven case: **L1 server-state continuation.** On the WSS path
   `recordWSSProviderUsage` reads `previous_response_id` from the incoming Codex
   request and counts `provider_input_tokens − body_tokens`. Slimference does
   **not** inject that id on the WSS path; Codex uses Responses-API server-state
   natively. Until an A/B (identical prompt through Slimference vs direct
   `codex`, comparing client-sent request bytes) proves Codex resends full
   history without Slimference, L1 is `engineered_pending_evidence` and is
   **excluded from the trusted product number**. The corpus gate prints the
   `S_local` decomposition (in-band / L2 / L1) for exactly this reason.
   Additionally: the gate currently **sums self-reported sidecar token counts
   without recomputation**, so a number is not trusted merely because the gate
   is green. Raising the CI floor on appended captures is forbidden until the
   gate recomputes savings from recoverable raw bytes (see
   `docs/todo/integrity-slocal-attribution-and-recompute.md`). Reporting a
   blended headline `S_local` as one product figure, without its decomposition
   and attribution, is a No-Misattribution violation.

### 3.8 Anti-Masking and Test-Honesty Protocol (Binding, Mandatory Critical Check)

A green gate, a passing test, and a rising number are **claims, not proof**.
Before an agent reports any savings, marks any task done, raises any floor, or
commits any test change, it MUST run the critical check below **with fresh eyes**
and assume the current state is lying until proven otherwise. These rules name
the exact failure modes already found in this repository (2026-06-23 review) so
they can never recur silently.

**3.8.1 Forbidden patterns (each is a hard reject, not a style nit):**

1. **Trust-without-recompute.** Counting a saving from a self-reported number
   (e.g. summing `saved_tokens`/`input_tokens` from a sidecar jsonl) when the
   gate could recompute it from recoverable raw bytes. A metric the agent cannot
   independently re-derive is unverified and may not be reported as proven.
2. **Misattribution.** Counting Codex-native or provider-native behavior (native
   `previous_response_id` continuation, provider-cache discount, anything that
   happens without Slimference) as Slimference `S_local` (§3.7.7).
3. **Denominator gaming.** Reporting a high ratio whose denominator is a curated
   set of high-compaction captures rather than a real session's full input. A
   98%-style ratio on hand-picked huge outputs is not a product number; the
   representative figure (e.g. in-band per-request) must be reported alongside.
4. **Blended headline.** Presenting one combined `S_local` percentage without its
   per-source decomposition and attribution labels.
5. **Circular floor raise.** Appending captures/fixtures and then raising the CI
   floor to match, with no production code change that genuinely produces the
   saving (§3.7.6).
6. **Test-to-mask.** Changing a test's expected values, loosening an assertion,
   skipping, deleting, or `t.Skip`-ing a test so a regression "passes" instead of
   fixing the regression. Editing a golden/expected value is only allowed when
   the new value is independently proven correct and the reason is recorded in
   the commit message.
7. **Tautological / always-green tests.** Tests that assert a value the code just
   wrote back to itself, mock away the behavior under test, never exercise the
   failure branch, or would still pass if the production logic were deleted.
   Every behavior test must be able to **fail** when the behavior breaks.
8. **Coverage theater.** Adding tests purely to move the coverage number without
   exercising real product paths, safety branches, or failure modes (§7).

**3.8.2 Mandatory critical check (run every cycle before claiming done):**

- **Re-derive the number.** Independently recompute the headline metric from
  source; if you cannot, label it `unverified` and do not raise any floor on it.
- **Attribution audit.** For every counted saving, name the exact code path that
  *causes* it and confirm it would NOT happen without Slimference. If it is
  observe-only of native behavior, move it out of the trusted number.
- **Denominator audit.** State what the denominator is and whether it represents
  a real session. If curated, report the representative number too.
- **Negative-control the tests.** For any new/changed test, mentally (or
  literally) break the production code and confirm the test would fail. If it
  would still pass, the test is worthless — fix it.
- **Regression honesty.** If a test now fails, the default is that the code
  regressed. Investigate the regression first; only adjust the test after the
  new expected value is independently proven correct.
- **Diff-shape sanity.** A commit that changes only docs, fixtures, and CI
  thresholds (no `internal/`/`cmd/` production code) may NOT claim a savings or
  correctness improvement (§3.7.6c).

**3.8.3 Reporting rule.** Every savings/quality report must explicitly state, in
plain language, which numbers are **independently verified**, which are
**self-reported/unverified**, which are **Slimference-incremental** vs
**native**, and what the **representative** (not curated-best) figure is. Hiding
any of these is itself a masking violation. When in doubt, distrust the green
state and say so.

## 4. New Product Features: Always-On-Safe or Do Not Build

New savings/product mechanisms are built only when they are **default-on** for
the normal product path or can be enabled automatically and safely. A new
mechanism that is predictably permanent `default-off`, manually promoted,
experimental, or not broadly usable because of model-quality risk is not an
allowed product feature.

Existing legacy, lab, proof, and operator paths may remain in the code as long
as they are isolated, documented, and not sold as the default product path. New
work on such paths requires explicit project approval. Standard work focuses on
deterministic, recoverable/fail-open, drawdownless levers that run in daily use
without user or model-quality loss.

---

## 5. Languages: Production Code, Tooling, Tests

### 5.1 Production Code

- **`cmd/`** and **`internal/`**: **Go only** (`go 1.25.0` according to
  `go.mod`).
- **JSON:** `encoding/json` (standard library). See `docs/spec.md`.

### 5.2 Tooling Under `scripts/` (Required Location for Repository Tools)

- **All** new helpers, checks, and small CLIs that are **not** part of the
  runtime binary live under **`scripts/`** in **thematic subdirectories**, never
  loose in the repository root.
- **Implementation:** **Go** (`.go`), as packages under `scripts/<topic>/`,
  runnable from the module root with `go run ./scripts/<topic>/...` (or
  `package main` + `go install` according to `scripts/README.md` convention).
- **No** new shell scripts, **no** new Python/Node for Slimference repository
  tooling. Exceptions require explicit project approval.

**Standard subdirectories (extend as needed, always name them thematically):**

| Directory | Contents |
|-----------|----------|
| `scripts/coverage/` | Coverage evaluation, gates, threshold comparison (currently 95.0% aggregate gate for CI/local). |
| `scripts/benchmarks/` | Benchmark runners, `go test -bench` evaluation, comparison runs. |
| `scripts/utils/` | Small helper CLIs (codegen, one-off migrations, diagnostics). |

Additional subdirectories follow the same pattern (for example
`scripts/lint/`, `scripts/release/`). **Never** dump everything into one
generic catch-all directory.

- Document the **purpose** of each subdirectory briefly in `scripts/README.md`;
  for new tools, also use a README in the subdirectory or a short Go package
  comment/doc.

### 5.3 `scripts/` Is Optional Only in This Sense

The **binary** runs without `scripts/`. **`scripts/`** is **not** optional for
discipline: anyone measuring coverage, running gates, or bundling benchmarks
puts that work **there**, not scattered through the repository root.

---

## 6. Tests: Go (Required for Coverage) + TypeScript Under `tests/`

The requirements for **high meaningful Go coverage** and **tests under
`tests/` in TypeScript** are combined without gaps as follows:

### 6.1 Go: Unit and Package Tests (Indispensable)

- **`internal/**` and `cmd/**`**: unit and white-box tests in **`*_test.go`**
  **next to the source code** (Go standard layout).
- **Reason:** unexported symbols, `go test ./...`, and **coverage accounting**
  for exactly these packages. Pure TS tests cannot replace that.
- **Target:** at least **95.0% aggregate statement coverage** on all
  production-relevant Go code (`cmd/`, `internal/`), measurable with
  `go test -cover`, without permanently excluding files (exceptions only as in
  Section 7 below). New and changed complex logic needs real behavior-relevant
  tests even when the aggregate gate is already green.

### 6.2 TypeScript: Additional Tests Under `tests/ts/`

- **Supplementary** TypeScript test suites live under **`tests/ts/`** (for
  example Vitest/Jest; define the configuration when introducing them).
- Use cases include E2E against the HTTP API, contract tests, or
  agent-friendly scenarios. They are **additional** to Go tests, **not** a
  replacement for package coverage in `internal/`.
- **`tests/integration/`** (Go) remains reserved for **Go integration tests** if
  used.
- **`tests/fixtures/`**: shared files for **Go and/or TS tests** as needed.

### 6.3 Package-Local Fixtures

- Small, package-specific inputs stay in **`testdata/`** next to the respective
  Go package.

### 6.4 Do Not Move

- Existing **`internal/.../*_test.go`** and **`cmd/.../*_test.go`** files are
  **not** moved to `tests/`; doing so breaks Go idiom and coverage.

---

## 7. Test Coverage (Go): **95.0%+ Aggregate, No Cheating**

- **Target:** at least 95.0% aggregate on `cmd/` + `internal/` as above.
  Important product paths, safety branches, routing/fallback decisions, and
  regression risks need real tests. Artificial tests only for the coverage
  number, tests for OS error edges that cannot be sensibly triggered, and
  always-green assertions are not the goal.
- **No cheating:** no permanent exclusion of whole packages from coverage
  without a ticket; generated code only with clear labeling and, if necessary,
  approval.
- **Quality:** table-driven where sensible, `t.Parallel()` where safe, hard
  edge/error cases, deterministic (no flaky tests), meaningful failure messages.
- **Local/CI check:** for example
  `go test ./... -covermode=atomic -coverprofile=coverage.out`; a gate may be
  implemented under **`scripts/coverage/`**.

---

## 8. TypeScript Files

- Keep TypeScript/JavaScript test code under **`tests/ts/`** unless the user
  explicitly approves a new product/runtime TS surface.
- If older TS tests are migrated to Go or consolidated under `tests/ts/`, track
  that concretely in **`docs/todo.md`**.

---

## 9. Short Pre-Merge Checklist

- [ ] Conforms to `docs/spec.md`.
- [ ] `go test ./...` is green; **Go coverage** matches the project goals
      (95.0%+ aggregate gate).
- [ ] New **Go** logic has hard `*_test.go` tests.
- [ ] New **tooling** lives only under **`scripts/<topic>/`**, preferably in Go.
- [ ] After product-relevant code/TUI/CLI/install changes: build/install the
      current binary with `go run ./scripts/build -restart`, then verify
      `which slimference` and `slimference status --preflight`. `slimference`
      in the terminal must start the newest local build, not an old artifact.
- [ ] Optional: **`tests/ts/`** tests added as a supplement, never replacing Go
      coverage.
- [ ] For install/uninstall changes: `docs/install.md` is current and meta-test
      `go test ./docs/` is green.

---

## 10. Wiring Doctrine (Scoped Codex, Phase I, 2026-05-17)

### 10.0 Scope (Binding)

Slimference affects **only Codex CLI and Codex Desktop app**. ChatGPT app and
ChatGPT website must remain **completely untouched** — no proxying, no hosts
patching, no traffic interception, no base URL rewriting. This is a hard
product boundary, not a configuration preference.

Sessions run through Slimference **only** when started via:
- `slimference codex run -- <prompt>` (CLI, including `codex exec` headless)
- TUI / Launch Center start path
- `slimference codex enable` (advanced shared route, explicitly opt-in)

Any other Codex usage (direct `codex` in terminal without `slimference codex run`,
ChatGPT app, ChatGPT website) must run **direct** with zero Slimference
interference.

**Final product binding rule:** In the final product, Codex CLI and Codex
Desktop App are used through Slimference **only** when explicitly invoked via
`slimference codex run` or the TUI/Launch Center path. A user running `codex`
directly in the terminal (without `slimference codex run`) must get plain
Codex with zero Slimference interference. ChatGPT app and browser ChatGPT
must always run normally without Slimference. Agents may use `slimference
codex run -- codex exec` for testing and live-proof — this is the equivalent
of the production path and produces genuine evidence.

### 10.1 Transport (Binding)

**WSS is the production transport.** All savings mechanisms (L1, L2, L3,
repdet, pruning, output-wire) must be wired into the WSS Phase-F path and
must fire there during normal `slimference codex run` sessions. WSS is where
production savings come from.

**HTTP is test/research only.** The HTTP proxy path exists for:
- Initial bring-up and protocol research
- Fallback when WSS Phase-F certification is not green (bridge mode)
- Isolated component testing

HTTP must not be the primary savings path in production. If WSS Phase-F is
not certified for the current Codex version, the priority is to recertify
WSS, not to optimize HTTP as a permanent substitute. Savings measured on the
HTTP path are valid evidence but are not the production target.

### 10.2 Savings Architecture (Binding)

Slimference savings operate on **two layers**, both on the WSS path:

1. **L2 Shim (Command-Output-First):** Compacts shell/tool output *before*
   it enters the WSS frame. Runs as a PATH/`BASH_ENV` shim in the Codex
   process. This is the **primary local savings surface** today (~70% of
   `S_local`). It is safe by construction: archive recovery, byte-equal
   fail-open, never mutates stored history. All savings mechanisms must
   fire here for every `slimference codex run` session.

2. **WSS Proxy MITM (Phase-F):** Mutates WSS frames in transit. This is the
   **secondary savings surface** (~2% of non-control token volume today).
   It adds savings on top of L2 by deduplicating repeated tool outputs,
   aging stale reads, pruning obsolete reads, and compacting search output.
   All guards must be engineered toward the loosest safe predicate (§3.4).

**Both layers must be active and measured.** A regression in either layer is
a product regression. The WSS Proxy MITM is where the largest *untapped*
savings potential remains — the two main guards
(`wss_stateful_delta_mutation_proof_gate` and
`wss_full_history_downstream_delta_proof_gate`) currently block ~485K tokens
that could be saved. Narrowing these guards is the highest-leverage work.

### 10.3 Savings Priority Order (Binding)

1. **Max local savings without caching (`S_local`)** — the primary product
   metric. Push every lever: L2 shim expansion, WSS proxy guard narrowing,
   L3 safe subset, output-wire, multi-turn pruning, innovative levers.
2. **Then optimize caching to be better than normal** — cache hit rates are
   a first-class target. Cache-prefix optimization, 24h retention, cache-bust
   guards. Caching must be ultra-optimized so combined billable savings exceed
   what a normal Codex user gets. But cache wins never mask `S_local`
   regressions (§3.2).
3. **All levers super-engineered** — every savings mechanism must be
   default-on-safe, measured, recoverable/fail-open, and exact to the
   route/request class where it runs. No handbrakes without proven vectors.
4. **Innovative levers** — agents must actively search for new savings
   surfaces: command-output-first expansion, cross-turn repdet, superseded
   command pruning, stateful-safe parser classes, archive-backed mutation,
   state mirrors, capability mirrors, and other novel approaches (§3.3).
5. **Everything stable** — no upstream 400s, no invalid requests, no
   cache-bust regression, no model-quality regression, no tool/workflow
   degradation. Stability is not optional.

### 10.4 Signal and Traffic Wiring

Slimference may touch the user stack by default only in ways that leave
ChatGPT.app and browser ChatGPT normal:

1. **Signal IN**: Codex hooks in `~/.codex/hooks.json` plus
   `~/.codex/config.toml` `[features].hooks=true`. Out-of-band subprocess
   calls, never over the network. Claude Code hooks remain in the code but are
   default-off and explicitly opt-in only.
2. **Traffic IN (scoped CLI)**: `slimference codex run -- <prompt>` starts only
   that Codex CLI process with the local `slimference-codex` provider. No
   `/etc/hosts`, no pfctl, no system proxy, no browser/ChatGPT.app blast
   radius.
3. **Traffic IN (global lab only)**: transparent SNI-MITM (`/etc/hosts` + CA in
   Keychain + port 443/8443) stays in the code but is no longer a default test
   path. `slimference root-arm` requires explicit `--global-chatgpt-hosts`
   because `chatgpt.com` is machine-wide and also affects browser ChatGPT and
   ChatGPT.app.

**Forbidden as default install / default test:**

- persistent `OPENAI_API_BASE` / `OPENAI_BASE_URL` / `CHATGPT_BASE_URL`
  environment variables
- persistent `HTTPS_PROXY` / `HTTP_PROXY` environment variables
- persistent `openai_base_url` field in `~/.codex/config.toml`
- persistent `model_provider="slimference-codex"` or a marker-owned
  Slimference provider route block in `~/.codex/config.toml` for tests,
  captures, or agent convenience
- macOS system network proxy settings
- unconfirmed `slimference root-arm` without `--global-chatgpt-hosts`

These paths remain in the code as **Legacy/Advanced**: operators who set them
manually still receive service. But no `slimference install` arms them, no TUI
offers them, and no integration test drives them as the primary path. The
per-process Codex CLI runner is the exception because it is not persistent and
is scoped to exactly one Codex process.

**Agent test rule:** agents must not activate or leave behind a persistent
global Codex route for their own Slimference/Codex tests. Normal `codex` in the
terminal must run direct. Tests/captures run through scoped commands such as
`slimference codex run -- <prompt>` or through the Launch Center/TUI start path.
If a test exceptionally needs route-arming verification, the user must approve
that explicitly; then immediately run `slimference disable` and verify with
`slimference codex status` that `enabled=false`.

**Single Entry Point:** the subcommands `slimference install`, `uninstall`,
`status`, plus `slimference codex run|status`, are the normal Codex path.
`slimference codex enable|disable` is the advanced shared Codex route path and
must never be presented in UI/CLI/docs as a required normal state.
`cert-trust`, `root-arm --global-chatgpt-hosts`, transparent `enable`,
transparent `disable`, and `root-disarm` are global lab/certification commands.
`proxy run`, `integrate`, and persistent proxy/URL patches outside the
marker-owned Codex route block remain legacy.

**Fail-open mandate:** `slimference codex run` falls back directly to unfiltered
Codex on daemon failure. `slimference codex enable` remains reversible through
`slimference codex disable`; browser/ChatGPT.app always stay direct. Global lab
path with daemon down reverts the hosts patch on clean shutdown; Codex update
degrades the frame parser to byte-equal bridging. These properties are
documented in `docs/install.md` and verified in tests.

**Drift ban:** changes that extend the default install set with a third surface
are reviewable **only** with an explicit `Phase-H-Override` tag in the change
description.

---

## N. Autonomous Codex Operation (Binding)

Automated agents (Devin, Claude Code, Cursor, and others) are **explicitly
permitted** to operate `codex` and `slimference codex run` autonomously,
including non-interactive `codex exec` sessions. A TTY is NOT required for
`codex exec` — it runs headless and produces real captures.

Agents must NOT treat "TTY required" or "needs interactive terminal" as a
blocker for live-proof work. The correct approach is:

```
slimference codex run --transport=auto -- codex exec --dangerously-bypass-approvals-and-sandbox "prompt"
```

This produces genuine L1 + L2 sidecar captures that can be distributed to
gate-counted corpus categories. All captures from `codex exec` are real
production evidence, not synthetic.

If an agent claims a task is "BLOCKED: TTY required", that is a planning
error. The agent should immediately attempt `codex exec` instead.

---

*Changes to these rules are recorded in the Git history of `AGENTS.md`.*
