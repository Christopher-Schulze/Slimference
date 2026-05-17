# Third-Party Attribution

## RTK filter catalog (MIT)

The TOML files under `internal/filter/builtins_toml/` are derived from
the RTK project (https://github.com/rtk-ai/rtk), licensed under MIT.
Slimference embeds them verbatim via `//go:embed` to ship a broad
out-of-box Layer-0 filter catalog covering 50+ command-line tools
(gradle, mvn, dotnet, terraform, helm, gcloud, ansible, gcc, basedpyright,
biome, oxlint, hadolint, markdownlint, yamllint, shellcheck, make, just,
task, turbo, nx, mise, mix-*, brew-install, composer-install,
poetry-install, uv-sync, ollama, jj, jq, pre-commit, quarto, liquibase,
df, du, stat, ps, ping, rsync, ssh, sops, skopeo, fail2ban-client,
iptables, systemctl-status, jira, yadm, spring-boot, shopify-theme,
pio-run, trunk-build, swift-build, xcodebuild, mvn-build,
tofu-{fmt,init,plan,validate}, ty).

RTK's filter DSL schema (description, match_command, strip_ansi, replace,
match_output, strip_lines_matching, keep_lines_matching,
truncate_lines_at, head_lines, tail_lines, max_lines, on_empty) is
field-compatible with Slimference's own `FilterRule` (`internal/filter/
filters_toml.go`), so port required no semantic translation.

The accompanying `[[tests.X]]` snapshot fixtures are also ported and
executed as Go table-driven tests in
`internal/filter/builtins_toml_test.go`. Trailing-newline-only diffs are
normalized because Slimference's `ApplyTOMLRule` strips the trailing
'\n' during line-rejoin while RTK preserves it; the byte-difference is
semantically neutral for downstream LLM consumers.

### RTK upstream

- Repo: https://github.com/rtk-ai/rtk
- License: MIT
- Filter directory: `src/filters/*.toml`
- Snapshot fixture format: `[[tests.NAME]]` blocks within each filter
  file

### Slimference modifications

- Files copied verbatim. No edits in `builtins_toml/*.toml`.
- Embedded at build time via `//go:embed builtins_toml/*.toml`.
- Loaded into a sorted, regex-pre-compiled slice; matched
  lock-free after first call (sync.Once).
- Pipeline priority (descending): hand-written Go compactors >
  project/user TOML > embedded RTK TOML > truncate fallback.
- Telemetry: filter_runs SQLite rows tag the match source as
  `builtin_toml:NAME` so analytics can show which ecosystem of the
  catalog matched per session.

## License notice for redistribution

When redistributing Slimference binaries, ensure this NOTICE.md and the
RTK MIT LICENSE text travel with the binary (Apache-2.0/MIT/BSD-style
attribution requirements).
