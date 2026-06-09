# Slimference - Agent and Developer Rules

This document is **binding** for all automated agents (Codex, Claude Code,
Cursor, and others) and for humans working in this repository. Deviations
require explicit project approval.

---

## 1. Normative Documents

| Source | Role |
|--------|------|
| `docs/spec.md` | Current **technical target specification** v3 (implementation-relevant). |
| `docs/install.md` | **Install/Uninstall SSOT** (Scoped Codex, 2026-05-17): humans and agents read this for `install`, `status --preflight`, scoped `codex run|enable|disable|status`, and global-lab `root-arm --global-chatgpt-hosts`. Meta-test `docs/install_spec_test.go` keeps spec and code synchronized. |

---

## 2. Local-Only Planning Surface

`docs/todo.md` is a local planning surface and is not part of the public
documentation set. Current agents start from `agents.md`, `docs/spec.md`, and
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

*Changes to these rules are recorded in the Git history of `agents.md`.*
