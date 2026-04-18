# T32 - Bash Completion

Status: open
Priority: low
Scope: cmd/slimference, scripts/utils

---

## Problem

No shell completion exists today. The user only uses bash, so scope is
**bash only** - zsh and fish are explicitly out of scope for this task.

Every subcommand (`config`, `filter`, `hook`, `rewrite`, `gain`, `debug`,
`stats`, `daemon`, `doctor`, `test`, `version`) plus period keywords
(`today`, `week`, `month`, `all`) and `--json`/`--csv`/`--by-command` flags
are typed by hand.

---

## Desired End State

- `slimference completion bash` emits a sourceable bash completion script.
- Installation is a one-liner documented in `docs/documentation.md` and
  shown by `slimference completion bash --help`.
- Completion covers all top-level subcommands, nested subcommands (e.g.
  `debug tail`, `hook install claude`), and common flags.

---

## Work Packages

### WP1 - Command tree introspection

- Centralize the command tree as a static data structure (a small tree of
  `{name, children, flags}` structs). If not already present, add one.
- Ensure runtime dispatch in `main.go` consults the same tree so completion
  and behaviour cannot drift.

### WP2 - Completion generator

- Emit bash `complete -F` functions that read the tree.
- Handle contextual completion: after `hook install`, offer `claude|codex`;
  after `debug`, offer `paths|last|summary|tail|replay`; after a period
  keyword, offer period-specific flags.

### WP3 - Docs and install path

- `slimference completion bash > ~/.bash_completion.d/slimference` (or
  direct `source <(slimference completion bash)`).
- Documented in `docs/documentation.md` under a new "Shell integration"
  section.

### WP4 - Tests

- Golden-file test: generator output matches a committed fixture.
- Smoke test: sourcing the output in a bash subshell runs without syntax
  errors.

---

## Subtasks

- [ ] Centralize command tree.
- [ ] Implement bash completion emitter.
- [ ] Wire `slimference completion bash` subcommand.
- [ ] Golden-file test and smoke test.
- [ ] Docs section.

## Acceptance Criteria

- Sourcing the emitted script gives working tab completion for all
  subcommands and common flags.
- Coverage stays at 100 %.
