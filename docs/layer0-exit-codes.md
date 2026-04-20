# Layer-0 Exit-Code Matrix

Normative reference for what `slimference filter -- <cmd>` returns in every
scenario. Derived from `cmd/slimference/main.go::handleFilterCmd` and
`internal/filter/pipeline.go::RunPipeline`. Kept here as a standalone doc so
spec+.md stays lean. Verified by T63 regression tests under
`internal/filter/` and `cmd/slimference/`.

## Invariant

**Slimference never swallows a non-zero child exit.** Filter / tee /
recovery paths are degradation signals, not error translations. The child's
exit code is always propagated verbatim to the caller unless Slimference
itself could not start the child process (runErr).

## Matrix

| Scenario                                 | Child exit | Filter applied | Tee write | Slimference exit | stdout              | stderr hint                    |
|------------------------------------------|-----------:|---------------:|----------:|-----------------:|---------------------|--------------------------------|
| Clean run                                | 0          | ok             | skipped   | 0                | filtered bytes      | (child's)                       |
| Child non-zero, filter ok                | N          | ok             | ok        | N                | filtered bytes      | `saved raw output to <path>`   |
| Child ok, filter err                     | 0          | bypassed       | ok        | 0                | raw bytes (passthrough) | (child's)                   |
| Child ok, filter panic                   | 0          | bypassed       | ok        | 0                | raw bytes           | (child's)                       |
| Child err, filter err                    | N          | bypassed       | ok        | N                | raw bytes           | `saved raw output to <path>`   |
| Child err, filter err, tee err           | N          | bypassed       | err       | N                | raw bytes           | tee error note                  |
| Start failure (exec not found)           | -          | -              | -         | 1                | empty               | `filter: <start error>`         |
| Parent SIGTERM during child run          | -          | -              | -         | 143              | partial             | (context cancel)                |
| Parent SIGINT during child run           | -          | -              | -         | 130              | partial             | (context cancel)                |

## Notes

- The tee directory defaults to `~/.slimference/tee/` and is created on
  demand. When tee writing fails Slimference prints the write error to
  stderr but still propagates the child's exit code.
- The Layer-0 filter_runs SQLite row is recorded only when the child
  started (`pr.Err == nil`). A hard start failure never produces a row,
  since there is no real token accounting to capture.
- `slimference rewrite` uses the same propagation rules via the same
  pipeline, so the matrix applies unchanged.
- Signal handling follows the Go process conventions: SIGTERM (exit
  128+15 = 143) and SIGINT (128+2 = 130) map through the standard
  `exec.Cmd` mechanics; no custom signal wrangling is applied.

## Future work

- A `command_timeout_seconds` knob that SIGKILLs a runaway child after
  `N` seconds and exits 124 is proposed but not yet implemented. When
  added, this matrix must get a new row.
- Exit-code taxonomy for the HTTP proxy foreground mode lives separately
  in T44 / T60 documentation (0 clean, 1 boot fail, 6 shutdown timeout).
