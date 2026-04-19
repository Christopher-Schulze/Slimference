# Slimference - Savings Assessment

Date: 2026-04-19
Scope: complete repository state in `cmd/`, `internal/`, `scripts/`, `tests/`
Method: live verification against repository-native commands and checked-in fixtures

---

## Executive Summary

Slimference already contains the large savings levers. This is not a repo that
still needs its first serious compression pass. The stack is already built:

- Layer 0: pre-entry CLI filtering
- Layer 1: deterministic proxy-side compression
- Layer 2: MiniMax summarization
- Layer 3: response caching with Stage A and Stage B lookup

The practical question is no longer "is there any savings in here?" but:

1. how much is already proven
2. how much additional savings is still realistically available
3. which remaining work would move the number materially

Bottom line:

- Proven from checked-in session fixture: `40.67%` token savings end-to-end
- Documented and architecturally plausible in real coding sessions: `50-80%`
- With stable prompt-cache prefixes and repeat traffic: effective savings can
  climb much higher on cached prefixes
- Remaining unproven upside in the current codebase is likely incremental, not
  a second step-function: roughly `5-15%` relative improvement from here, not
  another fresh `50%`

---

## Hard Evidence

### Repository proof state

The repository is in a strong enough state that savings analysis is worth
taking seriously:

- `go test ./...` green
- `go test -race ./...` green
- `go test -count=1 -cover ./cmd/... ./internal/...` green at `100.0%`
- `go run ./scripts/ci` green
- `bun test tests/ts` green

This matters because the savings claims are backed by executable code paths, not
just whiteboard intent.

### Checked-in measured fixture

Using the built-in reporting path:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
```

Measured result:

| Metric | Value |
| --- | --- |
| Requests | 3 |
| Original tokens | 8150 |
| Final tokens | 4835 |
| Saved tokens | 3315 |
| Savings ratio | 40.67% |
| Layer 1 saved | 2055 |
| Layer 2 saved | 1260 |
| Cache hits | 0 |

Interpretation:

- Even without any cache hit, the checked-in fixture shows material savings
- Layer 1 contributes the majority share in the sample
- Layer 2 adds another meaningful chunk instead of being cosmetic

### Integration proof

The integration test suite includes a real proxy-path check that verifies a
large 15-message conversation reaches the upstream with a shorter body than the
original request. That is weaker than a large benchmark corpus, but stronger
than unit-only evidence.

---

## Where The Savings Come From

### 1. Layer 0 is the highest-leverage structural win

Layer 0 is fundamentally different from the proxy layers because it prevents
junk from entering history at all. Once a tool result is compacted before the
agent sees it, every later request becomes cheaper too.

This is the most economically important design choice in the repo.

Strongest Layer-0 contributors in practice:

- build/test/lint output collapse
- git output compaction
- search result limiting
- JSON minification
- file-read comment stripping

### 2. Layer 1 is already broad and materially effective

Layer 1 is not one trick. It is a stacked deterministic pipeline:

- ANSI strip
- JSON compact
- comment strip
- exact and near dedup
- structure extraction
- delta encoding
- tool-aware compaction
- repeated-call collapse
- graph pruning
- prompt-cache optimization support

The checked-in fixture suggests the biggest concrete contributors are currently:

- dedup
- json_compact
- ansi_strip

That is a healthy sign. Those are low-risk, high-repeat mechanisms.

### 3. Layer 2 is a multiplier, not the foundation

Layer 2 matters, but it matters most after Layer 0 and Layer 1 remove noise.
That is the right architecture. MiniMax should compress information-dense
history, not raw terminal spam.

The repo already implements:

- sliding-window-based summary targeting
- validation
- operating modes
- incremental overlap thresholding
- repetition hints

This means L2 is already in the "refinement and proof" phase, not in the
"feature missing" phase.

### 4. Layer 3 can create extreme effective savings

When the same effective request reappears, Layer 3 skips work entirely. The
repo now also performs Stage A lookup before compression, which means repeated
identical original requests can skip the L1/L2 path as well.

This is where effective savings can become dramatic, but only when traffic has
real repetition.

---

## Realistic Savings Range

### Already defensible today

Without inventing numbers, the current repo state supports this range:

- conservative mixed usage: `35-55%`
- normal coding workflows with noisy tools: `50-70%`
- tool-heavy sessions with repeated builds/tests/searches: `60-80%`

Why this range is defensible:

- checked-in fixture already proves `40.67%`
- architecture is designed to amplify repeated tool-output patterns
- Layer 0 and Layer 1 are broad, deterministic, and cheap
- prompt-cache and response-cache paths create large upside when sessions are stable

### Where the "extreme" numbers come from

The repo documentation's very high stack-level examples only become believable
when all of these are true:

- hooks are actually installed and used
- the session is tool-heavy
- old prefixes stabilize
- Anthropic prompt caching gets long reusable prefixes
- Layer 3 sees repeated effective requests

That is possible, but it is workload-dependent. It is not the baseline number
to market without a real corpus report.

---

## Remaining Savings Potential

### My estimate

The current codebase likely still has roughly `5-15%` relative additional
savings available before diminishing returns dominate.

I do **not** think there is another untouched `30-50%` sitting in the repo.
The large levers are already present.

### Why the remaining upside is limited

- most obvious compaction surfaces are already implemented
- L1 already covers many high-frequency tool families
- L2 already has modes, validation, and incremental behavior
- L3 already does pre-compress and post-compress lookup
- the codebase is heavily tested, which usually means fewer giant blind spots

### What still looks unproven rather than absent

The main gap is evidence quality, not necessarily missing compression logic:

- no checked-in 100-session benchmark corpus
- checked-in `docs/benchmarks.md` exists now, but it is still fixture-scale evidence rather than a large real-session corpus
- no clear distribution view across short / medium / long sessions
- no hard public split between savings from L0, L1, L2, L3 across a real corpus

That means the repo may already be strong, but it cannot yet prove the strong
claims at production-marketing level.

---

## Top 3 Next Levers

### 1. Build the real benchmark corpus and evidence document

Priority: highest

Reason:

- This is the biggest gap between product claim and proof
- It converts "sounds plausible" into "measured on N real sessions"
- It will likely reveal which sub-layers actually pay for themselves

Expected benefit:

- not direct token savings by itself
- very high decision-value
- likely exposes the next real `5-10%` because it will show where the remaining noise still is

### 2. Push harder on repeated-tool and delta-style compression

Priority: high

Reason:

- repeated build/test/search/edit cycles are the natural shape of coding sessions
- this repo already has `repeated_collapse`, file-version delta tracking, tool
  keys, graph pruning, and staircase logic
- that means the next real gains are likely in tightening these repeat-aware paths,
  not inventing a brand-new layer

Expected benefit:

- meaningful on refactor/debug loops
- likely one of the few remaining areas with real headroom

### 3. Expand proof and observability around cache effectiveness

Priority: high

Reason:

- prompt-cache and response-cache can dwarf other savings when they hit
- today the code surfaces pieces of this, but the repo still lacks a single
  canonical benchmark report that shows how often cache paths truly fire
- without that, it is hard to know whether the most powerful economic lever is
  merely implemented or actually delivering

Expected benefit:

- may not change logic much
- can reveal whether cache hit-rate is the next biggest improvement frontier

---

## Critical Caveats

### 1. Current fixture evidence is real but too small

The checked-in fixture proves the pipeline works. It does not prove the median
real-world session.

### 2. Micro-benchmarks are not the same as savings benchmarks

The repo's Go benchmarks show the hot path is cheap. That is good. But those
benchmarks are not the same as end-to-end token-economics proof.

### 3. There is still minor documentation drift

One concrete example: `docs/documentation.md` still says "Go 1.24 or later"
while `go.mod` requires `go 1.25.0`.

This does not change savings, but it matters for trust in surrounding claims.

---

## Final Judgment

Slimference looks like a repo where the **architecture for savings is already
substantially complete**.

My direct answer to "wie viel Ersparnis ist drin?" is:

- already proven: about `40%+`
- realistically defensible in actual use: about `50-80%`
- in ideal repeated/cached workflows: potentially much more on effective cost
- additional upside still left in the current codebase: probably `5-15%` relative, if the next work is chosen well

The next smartest move is not blind feature expansion. It is building the hard
benchmark corpus and then optimizing only the parts that that corpus shows are
still leaking tokens.
