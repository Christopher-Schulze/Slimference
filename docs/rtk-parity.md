# RTK Filter Provenance

Slimference no longer carries an embedded reference checkout for RTK. RTK is
not a runtime dependency, not an install prerequisite, and not a source tree
agents should restore during normal work.

RTK remains relevant only as provenance for filter ideas that have already been
ported into Slimference-owned Go code and embedded TOML fixtures:

- trust-model helpers in `internal/filter/trust.go`
- Terraform coverage in `internal/filter/builtin_terraform.go`
- Python traceback coverage in `internal/filter/builtin_python.go`
- RTK-derived TOML catalog fixtures under `internal/filter/builtins_toml/`
- safe `wc`, `find`/`fd`, and search-output shape hardening in the Layer-0
  reducers

Closed product decisions:

- Claude Code optimization is parked outside the Slimference product path.
- RTK aggressive code-signature summaries are not default product behavior
  because they can remove implementation details the model may need.
- RTK discover/learn/advisory surfaces are not part of the current Codex-first
  token-savings product.

Future RTK-related work should start from the Slimference-owned implementations
above and current live product constraints, not from recreating a vendored
reference tree.
