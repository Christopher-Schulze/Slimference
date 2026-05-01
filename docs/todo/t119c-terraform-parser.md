# TASK 119c: Terraform plan/apply structured compactor

Status: DONE 2026-05-01.
Priority: P1
Scope: `internal/filter/builtin_terraform.go`, `internal/filter/pipeline.go`, `internal/filter/builtin_terraform_test.go`.

---

## Driver

Terraform output is one of the larger Layer 0 levers in IaC sessions. A typical `terraform init` is 30-150 lines (banner + per-provider download + per-module download + success footer); `terraform plan` is 100-2000 lines with a small structural skeleton buried under per-attribute diff bodies; `terraform state list` runs into hundreds of resource addresses on real environments; `terraform output` enumerates every output value. Before this task only `plan/apply/destroy` had a real compactor; the rest fell through to passthrough.

## What shipped

- `TryCompactTerraformInit`: parses `init` output, keeps the success footer / error blocks, collapses per-provider install chatter into `- N provider(s) installed` and per-module download chatter into `- M module(s) downloaded`.
- `TryCompactTerraformValidate`: keeps `Success!` / `The configuration is valid` lines + every `│ Error:` block; drops decorative banners.
- `TryCompactTerraformStateList`: keeps the first 30 and last 5 resource addresses with a `... <N> more resources omitted ...` marker for the middle.
- `TryCompactTerraformOutput`: budgets 30 top-level `name = value` entries (multi-line objects/lists count as one entry, kept whole inside the budget); emits `... <N> more outputs omitted ...` for the rest. Skipped when `-json` / `--json` is in argv so downstream JSON consumers see byte-for-byte output.
- `TryCompactTerraformShow`: delegates to the same plan/apply compactor (same structural shape); `-json` passthrough; falls through cleanly when the body matches no plan shape.
- `TryCompactTerraformPlan`: extended to accept `refresh` alongside `plan/apply/destroy`.
- `pipeline.go`: five new dispatch entries (`terraform_init`, `terraform_validate`, `terraform_state_list`, `terraform_output`, `terraform_show`) ordered after `terraform_plan` so the most-specific match wins.
- 17 new tests covering each shape's success / failure / passthrough / non-terraform / unrecognised-body / JSON-flag paths plus a dispatcher integration test.

## Acceptance Criteria

- [x] `terraform init` provider/module install chatter collapses to count lines.
- [x] `terraform validate` keeps verdict + error blocks, drops banners.
- [x] `terraform state list` head/tail with omission marker for >35 resources.
- [x] `terraform output` 30-entry budget honouring multi-line object values; `-json` passthrough.
- [x] `terraform show` delegates to plan compactor; `-json` passthrough.
- [x] `terraform refresh` recognised as a plan/apply alias.
- [x] Coverage 100% in `internal/filter`; race-clean; 8-step CI gate green.
- [x] Leaf audit reports the five new entries as `real_parser`.

## Out of Scope

- `terraform import` and `terraform taint` are explicit, single-resource operations whose output is short by construction.
- `terraform graph` / `terraform providers schema` produce DOT / JSON, owned by the JSON minify path.
- Per-account-id scrubbing is T109's outbound redaction job, not Layer 0 compaction.
