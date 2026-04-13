# Slimference - Master TODO

**Normative Spec:** `spec+.md` · **Sequenz + vollständiges Onboarding:** `handover.md` (Repo-Root) §4 (steht über `spec+.md` §23); Kurzlink: `docs/HANDOVER.md`.  
**RTK (`rtk-master/`):** nur Referenz beim Portieren — keine zweite Spec.

---

## Implementierungs-Reihenfolge (Pflicht-Reihenfolge)

Abgleich mit **`handover.md` §4** — diese Reihenfolge **vor** lose Items aus den Unterabschnitten abarbeiten:

1. **Phase A — Layer 1 am bestehenden Proxy härten**  
   Low-hanging + L1.2–L1.6 + Pipeline-Reihenfolge (ANSI → … per `spec+.md` §3/§5), Exchange-Sliding-Window überall (`layer1`, `layer2`, Prompt-Cache-Grenze), Overflow §17.4, MinHash, `structure_*`, Tests anpassen.

2. **Phase B — Layer 0**  
   `internal/filter/`, `internal/hooks/`, `slimference filter|hook|rewrite`, SQLite `modernc.org/sqlite`, Tee, Permissions, TOML-DSL, 24 Filter, `slimference gain` Basis.

3. **Phase C — Layer 2 Erweiterungen**  
   Adaptives Fenster, Tool-Priorität für Summarization (wie `spec+.md` §6).

4. **Phase D — Advanced L1 + Debug**  
   L1.8–L1.14, `internal/debug/`, JSONL-Decision-Chain, CLI `debug *`.

5. **Phase E — Docs & Polish**  
   `docs/documentation.md`, `docs/map.md`, `docs/changelog.md`, Synergy-/Cascade-Doku, Risiko-Checks.

**Nuance-Querschnitt (nicht in einem Unterkapitel vergessen):** Provider-Invisibility §16.4 · Hot-Path &lt;5 ms · Exit-Codes Layer 0 · `encoding/json` nur · keine Proxy-Fingerprints · Tests/Fixtures unter `testdata/`.

---

## HANDOVER §6–§7 — Lückenlose Abdeckung (Reality Check)

Ergänzt Phasen A–E; Abgleich mit **`handover.md`** (u. a. §5–§8: Layout, Stand, Tests) und der Datei-Map dort, damit nichts nur „implizit“ bleibt. Überschneidungen mit „Low-Hanging“ / Layer-1 sind **absichtlich** (zweifache Sichtbarkeit).

### Config, Types, TUI, Modul
- [x] `internal/config/config.go` + `defaults.go`: `[filter]`, `[hooks]`, `[debug]` *(`decisions_log`; ENV: `SLIMFERENCE_HOOK_SLIMFERENCE_COMMAND`, `SLIMFERENCE_DEBUG_DECISIONS_LOG`)*
- [x] `internal/types/types.go`: `ToolResultType`, `ToolResultPriority` implementiert; `DecisionEntry` in `internal/debug/decisions.go`
- [x] `internal/tui/model.go` (+ `views.go`): Hook-Status-Indikator implementiert — `HookStatus`, `SetHookStatus`, `renderHookStatus`; Debug-View zeigt Session-Logs; beide mit 100% Coverage
- [x] `tui.ProxyInterface` (siehe `internal/tui/model.go`): vollstaendig implementiert - alle TUI-Aufrufe (SetProviderEnabled, GetAnalytics, GetLayer2Status, SessionLogger, etc.) in Interface + proxyAdapter abgedeckt; keine Import-Zyklen
- [x] `go.mod` / `go.sum`: `go mod tidy` erledigt, alle Deps reproduzierbar

### Bestehende Dateien anpassen (HANDOVER „Files to MODIFY”)
- [x] `cmd/slimference/main.go`: `filter`, `hook`, `rewrite`, `gain`, `debug` (`paths|last|summary|tail|replay`), `version` — vollständig implementiert
- [x] `internal/compression/layer1.go`: Pipeline `spec+.md` §3/§5 komplett; alle Sub-Layer integriert
- [x] `internal/compression/comment_strip.go` (10 Sprachen), `dedup.go` + `dedup_minhash.go` (MinHash/LSH)
- [x] `internal/compression/treesitter.go` → `structure.go` (Rename + alle Imports/Refs erledigt)
- [x] `internal/proxy/handler.go`: Overflow §17.4, Fenster / `CompressiblePrefixEnd` / Prompt-Cache konsistent
- [x] `go.mod`: `modernc.org/sqlite` drin, kein `gjson`

### Neue Dateien (HANDOVER „Files to CREATE” — vollständig)
- [x] **Compression:** `ansi_strip`, `tool_classifier`, `tool_compressor`, `success_shortcircuit`, `image_replace`, `repeated_collapse`, `graph_pruning`, `prefilter_tag`, `dedup_minhash` — alle implementiert + getestet
- [x] **Summarization:** `adaptive_window.go`, `priority.go` — implementiert + getestet
- [x] **Filter:** alle 63 Dateien unter `internal/filter/` — implementiert + 100% Coverage
- [x] **Hooks:** `claude.go`, `codex.go`, `verify.go` + Tests unter `internal/hooks/` — implementiert + 100% Coverage
- [x] **Debug:** `decisions.go` + `session.go` unter `internal/debug/` — implementiert + getestet; JSONL-Chain vorhanden
- [x] **Analytics:** `internal/analytics/gain.go` (`slimference gain` today|week|month|all, `--json`)

### Tests (HANDOVER §6 — explizit)
- [x] `internal/filter/*_test.go` — vollständig, 100% Coverage
- [x] `internal/hooks/*_test.go` — vollständig, 100% Coverage
- [x] `internal/compression/` — alle Sub-Layer getestet, 100% Coverage
- [x] `internal/debug/decisions_test.go` — vorhanden + grün

### CI & Spec-Nebenbedingungen
- [x] CI (falls Repo CI nutzt): mindestens `go test ./cmd/... ./internal/...`; optional Coverage-Gate via `scripts/coverage/` — `scripts/ci/main.go` implementiert (vet + build + test + coverage gate)
- [x] Multi-Provider / OAuth-Passthrough: verifiziert — Authorization-Header korrekt 1:1 forwarded; Bug gefunden + gefixt: `handlePassthrough` fehlte Transfer-Encoding im Skip-Set (jetzt konsistent mit `doUpstreamRequest`); Anthropic + OpenAI routing + streaming korrekt

---

## Testing & Tooling (verbindlich: `AGENTS.md`)

- [x] **100 % Coverage (Go)** auf `cmd/`, `internal/` via `*_test.go` — erreicht (alle 18 Pakete grün)
- [x] Coverage-Gate: Go-Tool unter **`scripts/coverage/`** — `go run ./scripts/coverage -- -min=100` implementiert + getestet
- [x] Benchmarks: `scripts/benchmarks/main.go` — Runner fuer `go test -bench=.` ueber compression + filter; `internal/compression/bench_test.go` (8 Benchmarks: Compress_small/medium/large/code, StripANSI, StripComments, ExtractStructure); `internal/filter/bench_test.go` (7 Benchmarks: GitStatus, BuildOutput, JSONMinify, applyLayer0, Truncate); `go run ./scripts/benchmarks -- -benchtime=3s`
- [x] **Zusätzliche** Testsuites: **`tests/ts/`** (TypeScript) — 6 Tests mit `bun:test`: session fixture schema-Validierung (3 Tests) + CLI integration (3 Tests); alle grün
- [x] `tests/integration/` (Go), `tests/fixtures/`: 3 Integration-Tests (`//go:build integration`) grün: CompressesLargeConversation (ratio=0.80, layers=[1]), PassthroughNonCompressiblePath, HealthEndpoint; Fixtures: `sample_session.jsonl`, `sample_config.toml`
- [ ] Tests: Stil/Qualität wie `AGENTS.md` §5
- [ ] **`rtk-master/`**: nicht anfassen, nichts dorthin/davon verschieben (Fremdprojekt)

---

## Layer 0: Pre-Entry Filtering (RTK Integration in Go)

### Core Infrastructure
- [x] `slimference filter <cmd>` subcommand: subprocess execution, stdout/stderr capture, classify + `RunPipeline` (ANSI strip, optional Git-status compact), exit code propagation, tee/recovery on failure, SQLite tracking, passthrough for unknown commands
- [x] Hook installation system (v1: Claude Code + Codex only per `spec+.md` §4.3): `slimference hook install|remove <claude|codex>`, generates shell scripts, patches settings.json / config files
- [x] Hook integrity verification: `slimference hook verify` - SHA-256 check on installed hook scripts
- [x] Command rewriting engine + `slimference rewrite <cmd>` (Hook-Pfad): JSON stdin extraction, exit 0/1/2/3 (allow / usage+JSON / deny / sudo-ask); vollständiger Shell-Tokenizer + compound split → später
- [x] Permission system (v1): `filter.DeniedShellCommand` / `AskRequired` + `filter`/`rewrite` vor Ausführung; `[filter] deny_patterns` + `.slimference/filters.toml` → `SetExtraDenyPatterns`; sudo → Exit 3 wenn `SLIMFERENCE_CONFIRM_SUDO` nicht gesetzt *— volles allow/ask/exclude-UI → offen*
- [x] TOML Filter DSL (§4.5): 8-stage pipeline in `internal/filter/filters_toml.go` + `RunPipeline` — `strip_ansi`, `replace`, `match_output` (+ `unless`), `strip_lines_matching`, `keep_lines_matching`, `truncate_lines_at`, `head_lines`/`tail_lines`, `max_lines`, `on_empty`; merged `deny_patterns` (Projekt + `~/.slimference/filters.toml`); Lookup-Reihenfolge Projekt → User
- [x] Filter dispatch priority: built-in (`TryCompactGitStatus`, …) **vor** TOML; `applyLayer0AfterANSI` in `pipeline.go`; danach `[filter] passthrough_max_chars` / `SLIMFERENCE_FILTER_PASSTHROUGH_MAX_CHARS` (`TruncateStdoutWithHint`, Default 2000; `0` = kein Limit)
- [x] Tee system: save raw unfiltered output to `~/.slimference/tee/` on filter failure, print hint to recovered file
- [x] SQLite tracking for filter savings: input_tokens, output_tokens, savings_pct, command, timestamp, project_path (`filter_runs` + `RecordFilterRun`)

### Built-in Filters (24 total)
- [x] F01: Git Status — `TryCompactGitStatus`: Porcelain → eine Zeile mit **staged / worktree / untracked / renamed / conflicts**-Zählern; `!!` ignoriert; rename (R/C) und conflict-Codes (UU/AA/AU/DD/etc.) vollständig erfasst
- [x] F02: Git Log — `TryCompactGitLog` + `compactGitLog`: leer → `[git log] empty`; non-empty → `[git log] N commit(s)\n  <hash7> <subject> [<files+ins/del>]` pro Commit
- [x] F03: Git Diff — `TryCompactGitDiff` + `compactGitDiff`: leer → `[git diff] empty`; non-empty → hunk-headers + +/- Zeilen ohne Kontext-Zeilen, je Datei stats
- [x] F04: Git Show — `TryCompactGitShow` + `compactGitShow`: leer → `[git show] empty`; hash+subject+stat-summary + compactGitDiff des Diff-Teils
- [x] F05: Git — `TryCompactGitF05`: empty → ok; up-to-date (push/pull/fetch/merge/rebase); push success → ref-update lines kompakt; fetch/pull success → `N updated, M new`; merge fast-forward → stat-summary; rebase success → ok
- [x] F06: File Read — `TryStripCommentsFileRead`: bei `cat`/`head`/`tail` mit **genau einer** Datei mit bekannter Extension → `compression.StripComments`; mehrere Dateien passthrough (kein false-positive Risiko)
- [x] F07: Build Output — `TryCompactBuildOutput`: 30+ Build-Tools erkannt (go, cargo, tsc, webpack, cmake, bazel, swift, etc.) + alle npx/pnpm exec/yarn Varianten; empty → `[tool] ok`; non-empty: `extractBuildErrors` → success-pattern → `ok`, error-keyword-lines → `FAILED\n<errors>` (nur wenn kuerzer)
- [x] F08: Test Output — `TryCompactTestOutput`: 40+ Test-Runner erkannt (go test, cargo, pytest, jest, vitest, playwright, etc.); empty → `ok`; `TryCompactGoTestJSON` fuer `-json` Format; `extractTestFailures` → all-pass → `ok (summary)`, failures → `FAILED\n<fail-lines>` (nur wenn kuerzer)
- [x] F09: Lint Output — `TryCompactLintOutput`: 50+ Linter erkannt (golangci-lint, clippy, eslint, ruff, etc.); empty → `[tool] ok`; non-empty: `truncateLintViolations` kuerzt auf max 60 non-empty Zeilen mit `... +N more violation(s)` (nur wenn kuerzer)
- [x] F10: Search Results — `TryCompactSearchOutput`: empty → `no matches`; non-empty grep-style (rg/grep/ag/ack/ugrep/git grep) → `groupSearchResults` gruppiert nach Datei mit Match-Counts + Limits (max 5 Dateien, 3 matches/Datei, nur wenn kuerzer)
- [x] F11: Directory Listing — `TryCompactLs`+`compactLsOutput`: empty → `[ls] empty`; >10 Eintraege → `[ls] N entries`; `TryCompactTree`+`compactTreeOutput`: empty → `[tree] empty`; non-empty → `[tree] N directories, M files` (summary line)
- [x] F12: Package Manager — `TryCompactPackageOutput`: 16+ Package-Manager erkannt (npm/pnpm/yarn/pip/cargo/go mod/bun/uv/etc.); empty → `[tool] ok`
- [x] F13: Docker/K8s — `TryCompactContainerOutput`: docker/nerdctl/podman/kubectl/helm; empty ps/images/list → `[tool] empty`; helm search → `no matches`
- [x] F14: JSON — `TryCompactJSONMinify`: gueltiges JSON → `json.Compact` (nur wenn kuerzer)
- [x] F15: Log Output — `TryCompactLogDedup`: aufeinanderfolgende gleiche Zeilen bei docker/podman/kubectl logs → `Zeile [xN]`
- [x] F16: AWS CLI — `TryCompactAwsJSON`: rekursiv ResponseMetadata/ResultMetadata/SdkHttpMetadata entfernt (nur wenn kuerzer)
- [x] F17: ANSI/Progress — `RunPipeline` wendet `compression.StripANSICodes` vor Built-ins an; abgedeckt
- [x] F18: GitHub/GitLab CLI — `TryCompactGhList`: 20+ gh-Subkommandos; `TryCompactGlabList`: 16+ glab-Subkommandos; empty → `[tool] empty`
- [x] F19: PostgreSQL — `TryCompactPsql`: empty → `[psql] ok`
- [x] F20: .NET — `TryCompactDotnet`: dotnet build/test/publish/pack; empty → ok; `extractBuildErrors`/`extractTestFailures` fuer non-empty (ueber buildToolLabel/testToolLabel)
- [x] F21: Ruby — `TryCompactRubyOutput`: rake/rspec (leer → ok); rubocop → via TryCompactLintOutput
- [x] F22: Go Test JSON — `TryCompactGoTestJSON`: go test -json Event-Stream → nur FAIL-Events extrahiert
- [x] F23: Python (mypy/pyright) — `TryCompactMypy`: empty → `[mypy] ok`; pyright/basedpyright/ty check ueber F09-Lint-Chain
- [x] F24: Formatters — `TryCompactFormatOutput`: 20+ Formatter erkannt (prettier/gofmt/rustfmt/clang-format/biome/black/ruff format/isort/etc.); empty → `[tool] ok`

### Analytics
- [x] `slimference gain` *Basis:* `internal/analytics/gain.go` — `filter.db`, today|week|month|all, `--json`/`--csv`/`--by-command`, `--project`, USD-Felder via Config/ENV *— echte API-Preise → offen*
- [x] Economics tracking: `cfg.Analytics.GainUSDPerMillionTokens` (TOML) + `SLIMFERENCE_GAIN_USD_PER_MILLION` (ENV) → `SavingsUsdEst` = tokens_saved_est / 1e6 * rate; ausgegeben in `slimference gain` Text/JSON/CSV; Validierung auf >= 0 in config.Load()

---

## Layer 1 Enhancements (Post-Entry)

### New Sub-Layers
- [x] L1.7: ANSI Strip — `ansi_strip.go` implementiert + getestet
- [x] L1.8: Tool-Result-Classifier — `tool_classifier.go` implementiert + getestet
- [x] L1.9: Tool-Output-Compressor — `tool_compressor.go` implementiert + getestet
- [x] L1.10: Success Short-Circuit — `success_shortcircuit.go` implementiert + getestet
- [x] L1.11: Image Base64 Replacement — `image_replace.go` implementiert + getestet
- [x] L1.12: Repeated Tool Collapse — `repeated_collapse.go` implementiert + getestet
- [x] L1.13: Conversation Graph Pruning — `graph_pruning.go` implementiert + getestet
- [x] L1.14: Pre-Filtered Content Tagging — `prefilter_tag.go` implementiert + getestet

### Existing Sub-Layer Improvements
- [x] L1.2 Comment Strip: alle 10 Sprachen vorhanden (C, C++, Java, Ruby, Shell ergänzt)
- [x] L1.3 Dedup: MinHash/LSH in `dedup_minhash.go` — k=128, shingle=3, Jaccard 0.85
- [x] L1.4 Code Structure Extraction: regex-basiert in `structure.go`; 10 Sprachen; `structure_more.go` für weitere Pattern
- [x] L1.6 Prompt Cache: Breakpoint-Injektion nach Kompression verifiziert — `TestServeHTTP_promptCacheBreakpointsInjected` in `handler_compressible_test.go`

---

## Layer 2 Enhancements

- [x] Adaptive Sliding Window: `adaptive_window.go` — dynamische Fensteranpassung (3-7) nach Session-Komplexität
- [x] Tool Result Priority Classification: `priority.go` — HIGH/MEDIUM/LOW, aggressivere Kompression für LOW

---

## Debug & Observability System

- [x] `slimference debug last` — implementiert in `handleDebugLast` (main.go) mit `--json`
- [x] `slimference debug summary` — implementiert in `handleDebugSummary` (today|week|month|all)
- [x] `slimference debug tail` — implementiert in `handleDebugTail` (N Zeilen, `--json`)
- [x] `slimference debug paths` — zeigt Filter-DB, Tee-Dir, Config-Pfade
- [x] Filter decision chain logging: `internal/debug/decisions.go` — DecisionEntry, JSONL-Chain, Recorder
- [x] Structured JSONL output: `decisions.go` Recorder + JSONL-Format
- [x] Debug log level control: via `SLIMFERENCE_LOGGING_LEVEL` env + config `[logging] level` — abgedeckt; kein separater CLI-Flag nötig (kein Spec-Requirement)
- [x] Session replay: `slimference debug replay <session-file>` — vollstaendig implementiert: parst `RequestSummary`-JSONL, zeigt Tokens/Layers/Layer1-Breakdown/Layer2 pro Request + Gesamttotal; `ReplaySession()` in `internal/debug/session.go`; injectable `replaySessionFn`; 100% Coverage

---

## Low-Hanging Fruits (Existing Code)

- [x] dedup.go + dedup_minhash.go: MinHash/LSH (k=128, shingle=3, Jaccard 0.85) — implementiert + 100% Coverage
- [x] `structure.go` (ehem. treesitter.go): Rename erledigt; Regex-Muster für alle 10 Sprachen implementiert
- [x] comment_strip.go: C, C++, Java, Ruby, Shell — alle 10 Sprachen vorhanden (`spec+.md` §5.2)
- [x] layer1 / pipeline: ANSI zuerst (`spec+.md` §5.7), dann §3 Schritte 4a–4m — korrekte Reihenfolge in layer1.go
- [x] layer1: Success-Short-Circuit (`spec+.md` §5.10) — `success_shortcircuit.go` implementiert + getestet
- [x] **Sliding window:** `SlidingWindow` = Anzahl user-gestarteter Exchanges — in `layer1`, `layer2`, `handler` konsistent via `exchange_window.go`
- [x] config/types: `structure_*` / `was_structured` — config nutzt `structure_min_tokens`/`structure_languages`; types.go hat `WasStructured`
- [x] go.mod: `encoding/json` nur — kein gjson vorhanden
- [x] go.mod: `modernc.org/sqlite` — drin, kein mattn/go-sqlite3
- [x] handler.go: Context-Overflow §17.4 — aggressiver Re-Compress implementiert (Fenster 2, L2-Target 10 %, Fallback Roh-Body)
- [x] config/defaults.go: `structure_languages` auf alle 10 Sprachen erweitert (go, ts, js, rust, python, c, cpp, java, ruby, shell)
- [x] spec+.md: tree-sitter references replaced with regex-based extraction (DONE)
- [x] spec+.md: config variables renamed from tree_sitter_* to structure_* (DONE)
- [x] spec+.md: language support expanded to 10 in config defaults (DONE)
- [x] spec+.md: Section numbers updated (4->5, 5->6, etc.) (DONE)
- [x] spec+.md: All section cross-references updated (14.4->16.4) (DONE)

---

## Synergy Optimizations

- [x] Cascade effect documentation: Section 17 in docs/documentation.md — L0→L1 cascade, dedup/delta/cache amplification explained mit Beispielen und Tabelle
- [x] Response cache key stability: Section 17.2 — deterministische Compact-Strings erhoehen Cache-Trefferquote 5% → 30-40%
- [x] MiniMax input reduction: Section 17.3 — L0-gefilterte Nachrichten reduzieren MiniMax-Input 5-10x, bessere Qualitaet
- [x] Prompt cache prefix extension: Section 17.4 — Cache-Prefix-Erweiterung durch stabile Compact-Outputs; typisch 8-15 vs 1-3 Nachrichten gecacht

---

## Project Structure Updates

- [x] `internal/filter/` — 63 Dateien, vollständig implementiert + 100% Coverage
- [x] `internal/hooks/` — claude.go, codex.go, verify.go + Tests + 100% Coverage
- [x] `internal/debug/` — decisions.go, session.go + Tests + 100% Coverage
- [x] `internal/filter/filters_toml.go` (TOML filter DSL engine) — implementiert + 100% Coverage
- [x] `cmd/slimference/main.go` mit allen Subcommands: filter, hook, rewrite, gain, debug — vollständig
- [x] `.slimference/filters.toml` support — `project_filters.go` + `LoadMergedDenyPatterns` implementiert

---

## Documentation Updates

- [x] spec+.md: complete extended spec with all Layer 0 + enhancements + debug system (DONE)
- [x] docs/todo.md: master TODO created (DONE)
- [x] `handover.md` (Repo-Root): vollständiges Agent-Briefing; `docs/HANDOVER.md` = Alias (DONE)
- [x] docs/documentation.md: vollständig aktualisiert - v1.3.1 mit allen Layern, CLI Commands, Hook-Status, Test-Status, Package Structure
- [x] docs/map.md: aktualisiert mit allen neuen Packages + Funktionen (hooks/, filter/, tui HookStatus, etc.)
- [ ] docs/changelog.md: v2.0.0-Eintrag wenn Major-Release erfolgt

---

## Risk Mitigations

- [x] Conversation Graph Pruning: `messageReferencesIndex` prueft "message N" / "msg N" / "[N]" Patterns in spaeteren Nachrichten vor dem Prune — implementiert in `graph_pruning.go:PruneRedundant`
- [x] Image Base64 Replace: PNG-Dimensionen werden immer extrahiert; Terminal-Screenshot-Heuristic (>30% printable ASCII) greift fuer Text-Data-URIs und SVG, nicht fuer binaere PNGs (bekannte Limitation, kein Bug - sicheres Fallback: Dimensionen + Groesse)
- [x] Filter false-positive handling: ([]byte, bool)-Pattern + Length-Check vor jeder Transformation — kein Filter kann aktiviert werden ohne kuerzeres Ergebnis; passthrough bei allen JSON/Parse-Fehlern verifiziert
- [x] Provider invisibility: Headers 1:1 forwarded (nur Hop-by-Hop-Header geloescht); keine eigenen Header, kein User-Agent-Umbau, URL-Pfad + Query unveraendert, Streaming-Relay ohne Buffering — verifiziert gegen spec+.md §16.4
