# TASK 280: Final post-removal proof refresh

## Why

After Layer 2 is removed, the proof stack must be refreshed against the active
layers only. Release evidence should not accidentally count removed Layer 2
fixtures, metadata, validators, or savings fields.

## Acceptance

- `go test ./...` passes after Layer 2 removal.
- `go run ./scripts/ci` passes after Layer 2 removal.
- Benchmark, live-corpus, and release-proof reports list only active layers and
  active workload classes.
- Any remaining live-only evidence gaps are explicitly tracked as proof gaps,
  not implementation gaps.

## Sub-Tasks

- [ ] Re-run checked-in benchmark-corpus gates.
- [ ] Re-run release-proof report on clean matrices/resource bundles when live
  evidence is available.
- [ ] Update release notes or docs if measured active-layer numbers changed.

## Notes

This task starts only after T279 lands.

## Deviations

- None.
