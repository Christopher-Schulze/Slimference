# TASK T288: RTK breadth and Layer 3 renumbering

## Why

RTK has a broad command-specific filter catalog. Slimference already embeds the
same TOML filter filenames, but the safe product path must prove that this
breadth actually reaches the default hook rewrite path and the deterministic
Layer 1 tool-output classifier where applicable. The repo must also finish the
post-removal layer numbering: output/tool-surface reduction is now Layer 3, not
the retired output-layer name.

## Acceptance

- RTK TOML filter filename parity is verified against `research/rtk-ai/rtk/`.
- Safe TOML-backed commands are routed through `RewriteCommand` by default.
- Broad arbitrary-output commands stay out of default rewrite unless a specific
  drawdown-free guard exists.
- Layer 1 command classification covers the same obvious build/test/lint/search
  families without semantic summaries or lossy model-context replacement.
- The retired output-layer naming is removed from current product code, tests,
  scripts, and docs in favor of Layer 3.
- Focused tests, `go test ./...`, and `go run ./scripts/ci` pass.

## Sub-Tasks

- [x] Verify RTK catalog parity and identify safe breadth gaps.
- [x] Widen default rewrite coverage for safe TOML-backed commands.
- [x] Widen deterministic Layer 1 tool command classification.
- [x] Rename output/tool-surface reduction from the retired name to Layer 3.
- [x] Flush docs and run verification gates.
- [x] Commit the completed task.

## Notes

- RTK is a reference-only foreign checkout under `research/rtk-ai/rtk/` and must
  not be edited.
- Safe gap found: the embedded TOML catalog has RTK parity, but some TOML-only
  commands were not present in `filterableCommands`, so hook rewrite did not
  automatically pass them through `slimference filter`.
- Risk rule: broad `ssh` and `java` outputs are arbitrary user/application
  streams. They must not become default-on rewrite targets without a narrower
  command-shape guard.
- RTK TOML parity was verified by comparing 59 RTK filter filenames against the
  59 embedded Slimference TOML filter filenames; no filename diff.
- Added default rewrite coverage for safe RTK-breadth commands such as
  `ansible-playbook`, `basedpyright`, `brew`, `df`, `du`, `gcloud`, `gradlew`,
  `hadolint`, `jj`, `jq`, `just`, `mix`, `npx`, `nx`, `oxlint`,
  `pre-commit`, `rsync`, `shellcheck`, `swift`, `terraform`, `tofu`, `turbo`,
  `uv`, `xcodebuild`, and `yamllint`.
- Layer 1 classifier now recognizes obvious deterministic build/test/lint/search
  families across Go, Rust, .NET, Gradle, Maven, Swift, Mix, JavaScript package
  runners, xcodebuild, trunk, pio, turbo, nx, shellcheck, basedpyright, ty,
  yamllint, fd, du, journalctl, and related command heads.
- Output/tool-surface reduction now uses planner layer `l3_output`; runtime
  `SetLayerEnabled(3)` no longer aliases response/provider cache and controls
  output-reduce execution gates.
- Verification passed:
  - `go test ./internal/filter ./internal/compression`
  - `go test ./internal/filter ./internal/compression ./internal/planner ./internal/proxy ./internal/tui ./cmd/slimference ./scripts/benchmarks`
  - `go test ./...`
  - `go run ./scripts/ci` with total coverage 96.5% and all 8 steps passing.

## Deviations

None.
