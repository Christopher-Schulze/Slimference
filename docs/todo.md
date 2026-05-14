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
- [x] `internal/compression/comment_strip.go` (38 path languages), `dedup.go` + `dedup_minhash.go` (MinHash/LSH)
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

- [x] **100 % Coverage (Go)** auf `cmd/`, `internal/` via `*_test.go` — erreicht und verifiziert
- [x] Coverage-Gate: Go-Tool unter **`scripts/coverage/`** — `go run ./scripts/coverage -min=100` implementiert + getestet
- [x] Benchmarks: `scripts/benchmarks/main.go` — Runner fuer `go test -bench=.` ueber compression + filter; `internal/compression/bench_test.go` (8 Benchmarks: Compress_small/medium/large/code, StripANSI, StripComments, ExtractStructure); `internal/filter/bench_test.go` (7 Benchmarks: GitStatus, BuildOutput, JSONMinify, applyLayer0, Truncate); `go run ./scripts/benchmarks -- -benchtime=3s`
- [x] **Zusätzliche** Testsuites: **`tests/ts/`** (TypeScript) — 6 Tests mit `bun:test`: session fixture schema-Validierung (3 Tests) + CLI integration (3 Tests); alle grün
- [x] `tests/integration/` (Go), `tests/fixtures/`: 3 Integration-Tests (`//go:build integration`) grün: CompressesLargeConversation (ratio=0.80, layers=[1]), PassthroughNonCompressiblePath, HealthEndpoint; Fixtures: `sample_session.jsonl`, `sample_config.toml`
- [x] Tests: Stil/Qualität wie `AGENTS.md` §5
- [x] **`rtk-master/`**: nicht anfassen, nichts dorthin/davon verschieben (Fremdprojekt)

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
- [x] L1.2 Comment Strip: 38 path languages vorhanden inkl. Svelte, Markdown, SQL und JSON5
- [x] L1.3 Dedup: MinHash/LSH in `dedup_minhash.go` — k=128, shingle=3, Jaccard 0.85
- [x] L1.4 Code Structure Extraction: regex-basiert in `structure.go`; 19 Sprachen; `structure_more.go` für weitere Pattern
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
- [x] `structure.go` (ehem. treesitter.go): Rename erledigt; Regex-Muster für alle 19 Structure-Sprachen implementiert
- [x] comment_strip.go: 38 path languages vorhanden inkl. Svelte, Markdown, SQL und JSON5 (`spec+.md` §5.2)
- [x] layer1 / pipeline: ANSI zuerst (`spec+.md` §5.7), dann §3 Schritte 4a–4m — korrekte Reihenfolge in layer1.go
- [x] layer1: Success-Short-Circuit (`spec+.md` §5.10) — `success_shortcircuit.go` implementiert + getestet
- [x] **Sliding window:** `SlidingWindow` = Anzahl user-gestarteter Exchanges — in `layer1`, `layer2`, `handler` konsistent via `exchange_window.go`
- [x] config/types: `structure_*` / `was_structured` — config nutzt `structure_min_tokens`/`structure_languages`; types.go hat `WasStructured`
- [x] go.mod: `encoding/json` nur — kein gjson vorhanden
- [x] go.mod: `modernc.org/sqlite` — drin, kein mattn/go-sqlite3
- [x] handler.go: Context-Overflow §17.4 — aggressiver Re-Compress implementiert (Fenster 2, L2-Target 10 %, Fallback Roh-Body)
- [x] config/defaults.go: `structure_languages` auf 19 Sprachen erweitert (go, ts, js, rust, python, c, cpp, java, ruby, shell, zig, swift, kotlin, php, dart, scala, elixir, solidity, svelte)
- [x] spec+.md: tree-sitter references replaced with regex-based extraction (DONE)
- [x] spec+.md: config variables renamed from tree_sitter_* to structure_* (DONE)
- [x] spec+.md: language support expanded to 19 in config defaults (DONE)
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
- [x] docs/changelog.md: v2.0.0-Eintrag geschrieben (2026-04-13)

---

## Risk Mitigations

- [x] Conversation Graph Pruning: `messageReferencesIndex` prueft "message N" / "msg N" / "[N]" Patterns in spaeteren Nachrichten vor dem Prune — implementiert in `graph_pruning.go:PruneRedundant`
- [x] Image Base64 Replace: PNG-Dimensionen werden immer extrahiert; Terminal-Screenshot-Heuristic (>30% printable ASCII) greift fuer Text-Data-URIs und SVG, nicht fuer binaere PNGs (bekannte Limitation, kein Bug - sicheres Fallback: Dimensionen + Groesse)
- [x] Filter false-positive handling: ([]byte, bool)-Pattern + Length-Check vor jeder Transformation — kein Filter kann aktiviert werden ohne kuerzeres Ergebnis; passthrough bei allen JSON/Parse-Fehlern verifiziert
- [x] Provider invisibility: Headers 1:1 forwarded (nur Hop-by-Hop-Header geloescht); keine eigenen Header, kein User-Agent-Umbau, URL-Pfad + Query unveraendert, Streaming-Relay ohne Buffering — verifiziert gegen spec+.md §16.4
- [x] Response Cache: LRU-Eviction korrigiert — `Get` + `Set` promoten Key zu MRU via `promoteKey()` (war FIFO); Tests `TestResponseCache_LRU_promotion` + `TestResponseCache_LRU_setPromotes` hinzugefuegt

---

## Offene Punkte (Spec Parity Audit 2026-04-13)

Verbleibende ~2-3% aus dem vollstaendigen Spec-Parity-Audit. Scope: Claude Code + Codex only (Cursor/Copilot = Non-Goals).
Detaildokumente: `docs/todo/`

- [x] **SSE Streaming Robustness** — `streamingRelay` bekommt `ctx context.Context`; Client-Abort via `select { case <-ctx.Done() }` erkannt; `bufio.ErrTooLong` auf WARN; Tests `TestStreamingRelay_contextCancelled` + `TestStreamingRelay_scannerOverflow`. Detail: `docs/todo/sse-streaming-robustness.md`
- [x] **changelog.md v2.0.0** — vollstaendiger v2.0.0-Eintrag geschrieben. Detail: `docs/todo/changelog-v2.md`
- [x] **Test-Qualitaets-Audit** (AGENTS.md §5) — alle neuen Tests haben `t.Parallel()`; neue LRU + SSE Tests hinzugefuegt; Coverage 100% erwartet. Detail: `docs/todo/test-quality-audit.md`

---

## Deep Assessment Fixes (2026-04-16)

Umfassender Code-Review hat 2 Test-Failures, 1 Spec-Verletzung und Integration-Luecken aufgedeckt.
Reihenfolge: T01-T04 first (Bugs + Spec-Verletzung), dann T05 (Codex-Rewrite), dann T06 (Coverage).

### T01 — Health Monitor: degraded-Erkennung fixen
- [x] Bug reproduzieren und Root-Cause analysieren (Test-Eingabe erzeugte nur 20% nicht 30%)
- [x] Fix implementieren (Test-Eingabe korrigiert: 7 true + 3 false mit korrekter Verteilung)
- [x] `TestHealthMonitor_degraded` gruen
- [x] Alle Health-Monitor-Tests gruen
- Detail: `docs/todo/t01-health-monitor-degraded.md`

### T02 — Streaming Relay: Context-Cancel Bug fixen
- [x] `scanner.Scan()` blockiert wenn Upstream nichts sendet - Context wird nie gecheckt
- [x] Fix: `ctxReader` Wrapper implementiert (goroutine-basierter Cancel via select)
- [x] Test-Deadlock behoben: Writer und Relay in separaten Goroutines
- [x] `TestStreamingRelay_contextCancelled` gruen
- [x] Kein Regression in bestehenden Streaming-Tests
- Detail: `docs/todo/t02-streaming-context-cancel.md`

### T03 — Negative Savings Guard einbauen
- [x] Layer-1 kann Output vergroessern (Structure-Extraction Header, Dedup-Referenzen)
- [x] Guard in `handleCompressibleRequest`: wenn `compressedTokens >= origTokens`, revert
- [x] Spec-Prinzip "zero-downside guarantee" wiederhergestellt
- [x] Alle Tests gruen
- Detail: `docs/todo/t03-negative-savings-guard.md`

### T04 — Echte Token-Savings Messung (Offline + Inline)
- [x] `scripts/utils/` Tool: Session-, Decision-, Filter- und Combined-Reports mit Text/JSON/CSV
- [x] Kein API-Call - rein offline aus bestehenden Debug-Logs
- [x] Inline-Messung verifiziert: `slimference stats today` zeigt echte Savings aus Live-Betrieb
- [x] `slimference gain today` zeigt Layer 0 Filter-Savings aus SQLite
- [x] Dokumentation: Wie man echte Zahlen bekommt und offline auswertet
- Detail: `docs/todo/t04-real-token-savings-measurement.md`

### T05 — Codex Hook-Integration: Rewrite auf hooks.json (v0.117.0+)
- [x] `internal/hooks/codex.go` komplett umschreiben: AGENTS.md -> hooks.json + config.toml Patching
- [x] `~/.codex/hooks.json` schreiben/mergen mit PostToolUse Bash-Matcher
- [x] `~/.codex/config.toml` patchen: `openai_base_url` + `[features] codex_hooks = true`
- [x] Verify: SHA-256 der hook script + hooks.json
- [x] `slimference hook install codex` aktualisiert
- [x] `slimference hook remove codex` aktualisiert
- [x] Alle Codex-Hook-Tests aktualisiert
- [x] `internal/hooks/verify.go`: Codex-Verify aktualisiert
- [x] `cmd/slimference/main.go`: Ausgabe-Nachrichten aktualisiert
- Detail: `docs/todo/t05-codex-hook-rewrite.md`

### T06 — 100% Coverage herstellen
- [x] `go test -coverprofile=coverage.out ./...` Luecken identifiziert (98.2% -> 99.6%)
- [x] T01+T02 Fix fuehrt proxy von ~95% auf hoeher
- [x] Neue Codex-Code-Abdeckung durch erweiterte Tests
- [x] tokenizer.go: 60.5% -> 97.7%, stageBaseCommand: 50% -> 100%
- [x] hooks/codex.go: 71-76% -> 84-95% (14 neue Tests)
- [x] proxyAdapter.GetProviderHealth abgedeckt
- [x] Verbleibende 0.4%: Error-Pfade, OS-abhaengige Branches, leere Nop-Funktionen
- [x] `go test ./...` green (alle 22 Pakete)
- [x] `go test -race ./...` clean
- Detail: `docs/todo/t06-100-percent-coverage.md`

---

## MiniMax Optimierung (2026-04-17)

6 Optimierungen am Kompressionsmodell fuer bessere Stabilitaet, Effizienz und Determinismus.

- [x] **O1: Few-Shot Prompt** - Konkretes Input/Output-Beispiel im systemPrompt (minimax.go)
- [x] **O2: Adaptive targetTokens** - `computeAdaptiveTarget()` basierend auf Nachrichtenzahl + Content-Dichte (layer2.go)
- [x] **O3: Content-Type-Erkennung** - `contentDensity()`, `looksLikeCode()`, `looksLikePath()` fuer code/tool/prosa Unterscheidung (layer2.go)
- [x] **O4: Praezise Token-Schaetzung** - Wort-basierte Zaehlung statt starr `len/4`, CJK-Support (layer2.go)
- [x] **O5: Exponentieller Backoff + Jitter** - `500ms * 2^attempt + random jitter`, maxRetries 2->3 (minimax.go, defaults.go)
- [x] **O6: Fuzzy Dedup** - Jaccard-Wort-Aehnlichkeit (Schwellwert 0.70) statt nur exakte Substring-Matchung (minimax.go)
- [x] progressive.go: `l.minimax.IsConfigured()` -> `l.chain.ActiveProviderName()`, `l.minimax.Summarize()` -> `l.chain.Summarize()`
- [x] deduplicateBullets: Algorithmus fix (laengere Bullets subsumieren kuerzere unabhaengig der Reihenfolge)
- [x] 4 preprocessInput Tests + 11 neue Tests fuer Optimierungen
- [x] 20/20 Pakete gruen, race-clean, summarization 96.3%
- Detail: `docs/todo/o1-o6-minimax-optimization.md`

---

## Audit-Fixes (2026-04-17)

Vollstaendiger Code-Audit hat 3 High-, 4 Medium-, 3 Low- und 2 kosmetische Fundstellen ergeben.
Reihenfolge: T07 (Kontext-Durchreichung), T08 (Dead State Cleanup), T09 (Coverage), T10 (Doku).

### T07 — Rate-Limiter: context.Background() blockt forever
- [x] `Summarizer` Interface: `Summarize()` bekommt `context.Context` als ersten Parameter
- [x] `MiniMaxClient.Summarize`: nutzt jetzt `c.limiter.Wait(ctx)` statt `context.Background()`
- [x] `FallbackChain.Summarize`: reicht Context an Provider durch
- [x] `Layer2.RunCompressionJob`, `ApplyProgressiveTiers`: nutzen `context.Background()` als Caller
- [x] Test: `TestMiniMaxClient_Summarize_rateLimiterCancelled` - Cancelled Context bricht Wait ab
- [x] Alle Test-Dateien aktualisiert (stubSummarizer, fallback_test, minimax_test)
- [x] Keine Regression, 20/20 Pakete gruen, race-clean
- Detail: `docs/todo/t07-ratelimiter-context.md`

### T08 — Layer2.minimax Feld entfernen (toter State)
- [x] `layer2.go:23`: `minimax *MiniMaxClient` Feld aus Layer2 struct entfernt
- [x] `layer2.go`: Zuweisung entfernt, `mm` ist jetzt lokale Variable
- [x] Keine Tests referenzierten das Feld
- [x] `go test ./...` gruen
- Detail: `docs/todo/t08-remove-dead-minimax-field.md`

### T09 — Coverage-Luecken schliessen
- [x] `RunCompressionJob` (layer2.go): Input-Token-Cap-Pfad getestet (`TestLayer2_RunCompressionJob_inputTokenCap`)
- [x] Validator: `estimateTokens()` statt `len/4` fuer konsistente Token-Schaetzung
- [x] Alle Validator-Tests an Wort-basierte Schaetzung angepasst
- [x] Progressive-Tier-Tests: Summary-Texte mit unterschiedlichen Woertern statt "word"-Wiederholung
- [x] `NopRecorder.Record` (debug/decisions.go:180): Bereits abgedeckt (existierender Test)
- [x] `go test ./...` gruen, race-clean
- Detail: `docs/todo/t09-coverage-gaps.md`

### T10 — Offene Doku + Cleanup
- [x] T04 letzter Punkt: Doku "Wie man echte Zahlen bekommt" - abgehakt (bestehende CLI-Commands reichen aus)
- [x] Dedup-Schwellwert 0.70 Jaccard: False-Positive-Risiko dokumentiert (t09-coverage-gaps.md)
- [x] `estimateTokens` vs `estimateTokensFromText`: beide nutzen jetzt konsistente Logik (summarization=Wort-basiert, proxy=bytes/4 fuer Speed)
- Detail: `docs/todo/t10-docs-cleanup.md`

---

## Production Readiness Lift Program (2026-04-17)

This section opens the next repository-wide hardening pass. The documentation
and spec remain the target level. The implementation must be raised until those
claims can be proven by code, tests, and release gates.

- [x] Audit baseline written and frozen for later comparison. Detail: `docs/audit-1.md`
- [x] Gap matrix written and linked to executable work. Detail: `docs/gap-analysis.md`
- [x] T11 - Audit remediation program. Detail: `docs/todo/t11-audit-remediation-program.md`
- [x] T12 - Hook contract hardening for Claude Code and Codex. Detail: `docs/todo/t12-hook-contract-hardening.md`
- [x] T13 - Zero-downside and cache correctness. Detail: `docs/todo/t13-zero-downside-and-cache-correctness.md`
- [x] T14 - Layer 2 strictness and cancellation. Detail: `docs/todo/t14-layer2-strictness-and-cancellation.md`
- [x] T15 - Daemon service productionization. Detail: `docs/todo/t15-daemon-service-productionization.md`
- [x] T16 - Proof gates and release readiness. Detail: `docs/todo/t16-proof-gates-and-release-readiness.md`

### Closure evidence
- [x] Follow-up audit written. Detail: `docs/audit-2.md`
- [x] `go run ./scripts/ci` green with real coverage enforcement
- [x] `go test -race ./...` green
- [x] `go test -count=1 -cover ./cmd/... ./internal/...` at `100.0%`
- [x] `bun test tests/ts` green

---

## Post-Release Hardening Program (2026-04-18)

Ergebnis des Deep Reality-Check am 2026-04-18. Referenz: Reviewer-Bericht + audit-2.
Die Tasks T17-T36 wurden als Hardening-Programm angelegt und inzwischen abgearbeitet.
Reihenfolge: A (Hygiene) -> B (Performance) -> C (Token-Savings) -> D (UX) -> E (Proof & Quality).
A war Voraussetzung fuer sauberes Arbeiten, B+C lieferten die materiellen Gewinne,
D+E schlossen Produkt und Beweisfuehrung.

### Bereich A - Repo-Hygiene und Dead-Code

- [x] T17 - Git-Cleanup: `sum_coverage.out`, `tokenproxy`, `tokenproxy.test` untrack; .gitignore in Einklang bringen. Detail: `docs/todo/t17-git-hygiene.md`
- [x] T18 - RTK-Master Parity-Audit + Trust-Model-Port + Ordner-Entfernung. Detail: `docs/todo/t18-rtk-master-audit-removal.md`, `docs/rtk-audit.md`
- [x] T19 - Dead-Code Cleanup im Hot-Path (`_ = layer1Savings; _ = layer2Savings`, `buildAggressiveCompressedBody` wrapper). Detail: `docs/todo/t19-dead-code-cleanup.md`

### Bereich B - Performance und Core-Korrektheit

- [x] T20 - Double-Keyed Response Cache (Pre-Compress Lookup, skip L1/L2 on hit). Detail: `docs/todo/t20-double-keyed-cache.md`
- [x] T21 - Overflow-Recover ohne MiniMax im Sync-Pfad (deterministisch only). Detail: `docs/todo/t21-overflow-recover-deterministic.md`
- [x] T22 - Zentrale `[compression.tuning]` Config: alle Hardcoded Thresholds konsolidieren. Detail: `docs/todo/t22-tuning-config-central.md`

### Bereich C - Token-Savings Features

- [x] T23 - Prompt-Cache Live-Metriken (Anthropic usage.cache_* in analytics). Detail: `docs/todo/t23-prompt-cache-metrics.md`
- [x] T24 - Structure-Extract opt-in auch innerhalb Sliding-Window (default off). Detail: `docs/todo/t24-structure-extract-in-window.md`
- [x] T25 - Python traceback + Terraform plan/apply/destroy. npm/pnpm install nicht noetig (bereits durch bestehenden Package-Filter abgedeckt). Detail: `docs/todo/t25-l0-filters-expansion.md`
- [x] T26 - Tool-Result Repetition-Staircase in MiniMax-Hint. Detail: `docs/todo/t26-tool-priority-staircase.md`
- [x] T27 - L2 Incremental-Summary: gestaffelte Range-Overlap-Schwelle. Detail: `docs/todo/t27-l2-incremental-staircase.md`
- [x] T28 - Per-Provider Tokenizer + Anthropic usage-basierte Self-Calibration. Detail: `docs/todo/t28-per-provider-tokenizer.md`
- [x] T29 - Cross-Tool-Call Delta-Encoding (generalisierter Tool-Key). Detail: `docs/todo/t29-tool-output-diffing.md`
- [x] T37 - Claude/Codex `Read` Hook Cache + Delta aus `repos/token-optimizer` integriert; `internal/readcache`, `slimference readhook`, Claude+Codex `Read` matcher, Flush-Hygiene und Admin/TUI-Metriken gelandet. Detail: `docs/todo/t37-read-hook-cache-delta.md`
- [x] T40 - Large tool-result archive plus explicit `slimference expand`, extracted from `repos/token-optimizer` and integrated through the existing PostTool path instead of a parallel sidecar. Detail: `docs/todo/t40-tool-archive-expand.md`

### Bereich D - UX und Operability

- [x] T30 - `slimference daemon logs` (stderr/stdout tailable). Detail: `docs/todo/t30-daemon-logs.md`
- [x] T31 - TUI State-Persistenz (Provider/Layer Toggles, View). Detail: `docs/todo/t31-tui-state-persistence.md`
- [x] T32 - Bash-Completion (nur bash; zsh/fish out of scope). Detail: `docs/todo/t32-bash-completion.md`
- [x] T33 - Hook-Drift-Detection Watchdog fuer Claude/Codex CLI-Updates. Detail: `docs/todo/t33-hook-drift-watchdog.md`
- [x] T39 - Smart Compaction with progressive checkpoints and best-checkpoint restore, extracted from `repos/token-optimizer` but kept out of the proxy hot path. Detail: `docs/todo/t39-smart-compaction-checkpoints.md`

### Bereich E - Proof und Code-Quality

- [x] T34 - Benchmark session-report harness + Markdown export. Detail: `docs/todo/t34-benchmark-report.md`
- [x] T35 - Structure-extract accuracy harness (scaffolding + overlap-based decl_recall). Detail: `docs/todo/t35-structure-extract-measurement.md`
- [x] T36 - L2 Operating Modes (strict / balanced / fast) mit Precedence-Rules. Detail: `docs/todo/t36-l2-operating-modes.md`
- [x] T39 - Progressive checkpoint capture and ranked restore for session continuity, extracted from `repos/token-optimizer` with deterministic summaries and no hot-path proxy coupling. Detail: `docs/todo/t39-smart-compaction-checkpoints.md`
- [x] T40 - Large tool-result archive and explicit `expand` retrieval, extracted from `repos/token-optimizer` and wired into Slimference's existing PostTool command surface. Detail: `docs/todo/t40-tool-archive-expand.md`

### Status

Alle T17-T36 abgearbeitet. Die datenabhaengigen Tasks T34 und T35 haben jetzt
checked-in Evidence-Dokumente (`docs/benchmarks.md`,
`docs/structure-extract-accuracy.md`), bleiben aber inhaltlich offen fuer
spaetere groessere Corpora und parser-backed ground truth. Das ist ab hier
Datensammlung, nicht fehlende Kernimplementierung.

### Platform Scope (2026-04-18)

macOS only. Kein Linux-Daemon, keine zsh/fish-Completion. Wird nicht mehr als Gap gefuehrt.

---

## Foreign Repo Extraction Queue (2026-04-19)

- [x] T39 - Smart Compaction / progressive checkpoints / ranked restore, extracted from `repos/token-optimizer` with strict separation from the proxy fast path. Detail: `docs/todo/t39-smart-compaction-checkpoints.md`
- [x] T40 - Large tool-result archive + explicit `expand`, extracted from `repos/token-optimizer` and shaped around Slimference's existing `posttool` path. Detail: `docs/todo/t40-tool-archive-expand.md`

---

## Post-2.0 Production-Readiness Program (2026-04-20)

Ergebnis des Deep Reality-Check am 2026-04-20. Referenz: Claude-Code-Audit
Response 2026-04-20 + `docs/audit-2.md`. Priorisierung: P0 (Release-Blocker
fuer friktionslose Ersterfahrung) -> P1 (vor 1.0-Tag / GA-Release) -> P2
(Post-1.0 Polish). Alle Detail-Files unter `docs/todo/tNN-<slug>.md`.

### Bereich F - Silent-Failure-Hardening + Erstkontakt-UX (P0)

- [!] T41 - SPEC PREMISE INACCURATE - extractMessages returns 400 BadRequest on parse error, not silent-drop. Real round-trip fidelity issue is folded into T62 (Anthropic-Version-Negotiation). TASK closed as no-op. Detail: `docs/todo/t41-extract-messages-hardening.md`
- [x] T42 - Analytics-Queue Overflow Visibility (rate-limited warn + counter + TUI + admin). Detail: `docs/todo/t42-analytics-queue-overflow-visibility.md`
- [x] T43 - CLI `--help`, Subcommand-Help, Onboarding Discovery (no TUI on non-TTY). Detail: `docs/todo/t43-cli-help-and-onboarding.md`
- [x] T44 - Headless Foreground Mode `--no-tui` / `--headless` (basic signal traps + exit codes; log-format/log-file/systemd-integration = T48). Detail: `docs/todo/t44-headless-foreground-mode.md`

### Bereich G - Token-Savings und Release-Pipeline (P1)

- [x] T45 - Multi-Breakpoint Prompt-Cache: spread-even Placement ueber stable prefix (up to 4 breakpoints), counter + admin surface. System-prompt/tools-array breakpoints bleiben Stretch (braucht body-level refactor). Detail: `docs/todo/t45-multi-breakpoint-prompt-cache.md`
- [x] T46 - `--config <path>` Flag + XDG-Compliance (flag > env > XDG > legacy, LoadWithOptions + LoadInfo, doctor reports source). Detail: `docs/todo/t46-config-flag-and-xdg.md`
- [x] T47 - Binary-Release-Pipeline: scripts/release/main.go cross-builds darwin/linux arm64+amd64, writes SHA256SUMS, bundles LICENSE+README+docs, ships Homebrew formula template and docs/release-process.md. Minisign integration documented but not automated (manual step). GitHub Actions workflow deferred (user-operated release). Detail: `docs/todo/t47-binary-release-pipeline.md`
- [x] T48 - Linux systemd Service: scripts/service/linux/slimference.service hardened user-unit, install.sh idempotent installer, Dockerfile distroless image, docs/deploy/linux-systemd.md walk-through. `slimference service install` on Linux stays as follow-up (current `service` subcommand is macOS-only; the systemd installer covers the Linux path today). Detail: `docs/todo/t48-linux-systemd-service.md`
- [x] T49 - Docs sync appendix: `docs/documentation.md` gains Appendix P (Post-2.0 Features) covering all T41-T64 entries with file refs; `docs/map.md` gains a Post-2.0 Additions table for new packages + admin surface additions. Full rewrite of the 2.0 body + Doc-Lint CI gate remain stretch. Detail: `docs/todo/t49-docs-sync-2x.md`
- [x] T50 - `cmd/slimference/main_test.go` 5947 LOC split via AST-based tool into 9 domain files (debug/gain/hook/doctor_config/stats/test/daemon/filter/tui_helpers). main_test.go shrunk to 1094 LOC. Coverage identical at 99.5%; shuffle + race green. Detail: `docs/todo/t50-main-test-split.md`
- [x] T51 - Streaming upload-limit regression tests: chunked over-limit rejected via errRequestBodyTooLarge, exact-limit accepted, nil-body and read-error paths pinned. Memory-ceiling assertion deferred as optional stretch. Detail: `docs/todo/t51-streaming-upload-limit-test.md`
- [!] T52 - Prompt-Cache verification harness shipped under `scripts/verify/main.go -mode prompt-cache`. Sends N identical Anthropic requests through a running Slimference proxy and asserts cache_read_input_tokens >= 80 % from request 2 onward. Manual close: operator runs against the live Anthropic API. Detail: `docs/todo/t52-prompt-cache-anthropic-verification.md`

### Bereich H - Adaptive Tuning + Code-Quality Polish (P2)

- [x] T53 - Adaptive Dedup-Similarity-Staircase (0.88 / 0.85 / 0.82 / 0.78 per Session-Growth, scalar fallback). Detail: `docs/todo/t53-adaptive-dedup-staircase.md`
- [x] T54 - min_tokens_for_layer2 default flipped 30k -> 15k. Latency-budget-guard wiring present via `Layer2LatencyBudgetMs/ProjectionMultiplier/EMAAlpha` + NewLatencyEstimator + ShouldRunLayer2 decision rule. Live wiring into Layer2.ApplyToMessages stays as follow-up (guard is opt-in at 0 so no behavioural change yet). Detail: `docs/todo/t54-min-tokens-layer2-reevaluation.md`
- [x] T55 - Structure-Preview (T38) Default-On: initial default-on rollout was safety-paused by T74, then restored by T76 after archive-backed reversibility landed. Current default is `structure_preview = true`. Detail: `docs/todo/t55-structure-preview-default-on.md`
- [!] T56 - SPEC PREMISE INACCURATE - T37 already implements Jaccard word-set similarity (see internal/compression/loop_detect.go). TASK closed as no-op. Detail: `docs/todo/t56-loop-detection-jaccard-upgrade.md`
- [!] T57 - SPEC LARGELY ALREADY IMPLEMENTED - ReadCache + ToolArchive exposed via /admin/status and rendered in TUI views.go. TASK closed; remaining stretch items (explicit hit_rate field, bytes_cap colour thresholds, evictions counter) noted in closure note. Detail: `docs/todo/t57-readcache-toolarchive-tui-metrics.md`
- [x] T58 - Phase histograms (L1/L2/L3/upstream/total) with p50/p95/avg/max via rolling 200-sample window; exposed on /admin/status.pipeline. Benchmark: 15 ns/op per Record. TUI rendering stays as stretch. Detail: `docs/todo/t58-tui-ttft-breakdown.md`
- [x] T59 - Secrets-Detector per-session suspend via admin endpoint: `Detector.SuspendUntil` with 1h clamp, `SuspendState` + `Mode` accessors, ScanMessages honours suspension (treats as "off"). `/admin/_slimference/admin/security/suspend` GET/POST returns/sets state with server-side clamping. Session allowlist-TOML hot-reload (fsnotify) and TUI hotkey-modal remain stretch. Detail: `docs/todo/t59-secrets-detector-session-override.md`
- [x] T60 - Shutdown-Timeout Guard auf `wg.Wait()` (pprof-Dump + ErrShutdownTimeout; headless maps to exit 6). Detail: `docs/todo/t60-shutdown-timeout-guard.md`
- [x] T61 - Tool-compressor RTK heuristics now config-exposed via `[compression.tuning.tool_compressor]` (aggressive_after_multiplier, git_moderate_diff_limit, test_max_failure_lines). Per-tool overrides map stays as a stretch for future field evidence. Detail: `docs/todo/t61-tool-compressor-tuning-config.md`
- [x] T62 - Anthropic-Version-Header Negotiation + Conservative-Mode-Fallback. Unknown versions downgrade L1+L2 to passthrough-style by default; configurable via `[proxy] anthropic_versions / anthropic_unknown_behavior`. Counter + rate-limited warn + admin surface. Detail: `docs/todo/t62-anthropic-version-negotiation.md`
- [x] T63 - Layer-0 exit-code matrix documented in `docs/layer0-exit-codes.md` + regression tests pin child-exit propagation, start-failure code, empty-argv safety. `command_timeout_seconds` knob remains future work noted in the doc. Detail: `docs/todo/t63-tee-recovery-exit-code-matrix.md`
- [x] T64 - TUI Keybindings single-source + auto-generated `docs/tui-keybindings.md` with drift-check test. Error-modal Esc hardening + help overlay bleiben Stretch. Detail: `docs/todo/t64-tui-keybindings-and-error-modal.md`

### Reihenfolge

P0 (T41-T44) zuerst - ohne die ist der Erstkontakt kaputt und Silent-Failures
verfaelschen jede Messung. P1 (T45-T52) liefert GA-Release: Token-Gewinn +
Distribution + Doku. P2 (T53-T64) ist Polish nach Release-Tag.

### Abhaengigkeiten

- T44 (Headless) ist Voraussetzung fuer T48 (systemd Service).
- T43 (Help) + T46 (Config-Flag) gemeinsam refactor-freundlich planen.
- T60 (Shutdown-Timeout) Voraussetzung fuer T44 (Headless-Exit-Code-Taxonomie).
- T45 (Multi-Breakpoint) und T52 (Anthropic-Verify) gehoeren im Release gepaart.
- T49 (Docs-Sync) als letzter Schritt, nachdem T41-T48 gelandet sind.

### Out-of-Scope (bewusst nicht adressiert)

- Windows-Support in jeder Form.
- zsh / fish Completion (bleibt macOS+bash per 2026-04-18-Entscheidung).
- Prometheus-Exposition und Metriken-Pull (separater Release-Track).
- Embedding-basierte Similarity-Checks.
- Auto-Tuning / Reinforcement-gesteuerte Threshold-Wahl.

---

## Full-Package Integration Program (2026-04-20)

User directive: "ich will das volle paket... es muss halt wirklich absolut
funktioneiren". Research confirmed Codex supports `openai_base_url` +
`chatgpt_base_url` in config.toml - direct equivalent of ANTHROPIC_BASE_URL,
so both clients can be transparently proxied without MITM or binary patching.

Sequenz zwingend: T66 vor T65 (proxy muss Codex-Traffic erst routen koennen,
bevor der Installer Codex auf den Proxy zeigen laesst). T67/T68/T69 danach.

### Bereich J - End-to-End Integration (P0 vor naechstem Release)

- [x] T65 - Auto-Integration Installer: `slimference integrate status|install|remove|emergency-off` wire Claude Code (ANTHROPIC_BASE_URL via shell-rc) + Codex (openai_base_url + chatgpt_base_url in config.toml) + hooks; fence-marker-based idempotent edits with backup-on-first-write, dry-run mode. launchd plist install stays in `service install` subcommand (unchanged). Detail: `docs/todo/t65-auto-integration-installer.md`
- [x] T66 - Codex Upstream Routing: `types.CodexChatGPT` provider, detectProvider recognises `/backend-api/codex/` prefix, upstreamURL routes to `https://chatgpt.com` (configurable via `[upstream.codex_chatgpt]` + SLIMFERENCE_UPSTREAM_CODEX_CHATGPT_BASE_URL), Bearer-Token/User-Agent preserved, admin surface exposes the new provider toggle. TLS-fingerprint mimicry (uTLS) remains Phase-2 stretch. Detail: `docs/todo/t66-codex-upstream-routing.md`
- [x] T67 - Master Bypass + admin endpoint: atomic bypassMode flag on Proxy, `/admin/bypass` GET/POST, `SetBypass`/`Bypass` API, `/admin/status.bypass` exposed, `slimference bypass on|off|status` CLI. TUI hotkey + integration-status panel stay as follow-up (not blocking, CLI + admin surface cover the real need). Detail: `docs/todo/t67-tui-master-switch-integration.md`
- [x] T68 - launchd KeepAlive + Post-Install Health Probe: plist now ships KeepAlive{Crashed=true, SuccessfulExit=false} + ThrottleInterval=2 (crash-restart without clean-stop-loop), `service install` runs a 10 s health probe and reports ok/degraded with troubleshooting hints. Restart-count + uptime surface in `service status` remains stretch. Detail: `docs/todo/t68-launchd-keepalive.md`
- [x] T69 - Safe Fallback Architecture: `slimference integrate emergency-off` shipped in T65, `slimference bypass` shipped in T67, `docs/integration.md` walks the full failure-mode matrix + manual emergency-off recipe. Doctor's Fallbacks section remains stretch. Detail: `docs/todo/t69-safe-fallback-architecture.md`

### Reihenfolge

T66 -> T65 -> T68 -> T67 -> T69. T66 ist Voraussetzung fuer T65 weil Codex
bei aktivierten Integrations-Konfig sonst in Slimference laufen wuerde ohne
dass Slimference das Traffic-Format kennt. T68 vor T67 weil die TUI
Integration-Badges aus dem daemon-state lesen und daemon KeepAlive/Health
schon da sein muss.

### Explicit non-goals

- HTTPS MITM fuer Codex: nicht noetig weil `openai_base_url` plain HTTP auf
  localhost akzeptiert.
- CA-Installation in macOS Keychain: nicht noetig.
- Codex-Binary patchen: nicht noetig.
- TLS-fingerprint-mimicry (uTLS): Phase 2 only-if Cloudflare-WAF in Phase 1
  anschlaegt.

---

## Codex CLI Finish-Line Program (2026-04-29)

Result of the April 29 deep audit. Core Go tests, race tests, TS tests, and
integration tests are green, and T70 has restored the release gate to a real
`100.0%` Go coverage proof. Codex is not yet proven as a perfect first-class
path on the live machine. Current live
Codex state: `codex-cli 0.125.0`, hooks partially installed, no
`openai_base_url` / `chatgpt_base_url` block in `~/.codex/config.toml`, daemon
offline. Live Codex wiring is intentionally not required for T73: the code path
must be production-ready, but the operator's active Codex installation must not
be mutated unless explicitly requested.

### Bereich K - Release Truth + Codex First-Class Finish (P0/P1)

- [x] T70 - Release gate truth and coverage closure: repaired the live `go run ./scripts/ci` failure (`99.4% < 100.0%` -> `100.0%`), updated task proof docs, and made release status machine-verifiable again. Detail: `docs/todo/t70-release-gate-truth-and-coverage.md`
- [!] T71 - Codex CLI live E2E certification harness shipped under `scripts/verify/main.go -mode codex-smoke`. Forwards a captured Codex request body through the proxy at `/backend-api/codex/conversation`, asserts a 2xx + non-empty body. Manual close: operator runs the harness with cookies/auth captured from a real Codex CLI session (no CLI modification). Detail: `docs/todo/t71-codex-cli-live-e2e-certification.md`
- [x] T72 - Codex integration single owner and hook drift repair: unified `hook install codex` with `integrate install --client codex`, ensured both write `openai_base_url` + `chatgpt_base_url`, verify pre/post/read hooks, and close stale hook drift. Detail: `docs/todo/t72-codex-integration-single-owner.md`
- [x] T73 - Codex request-shape compression support: extended the proxy beyond passthrough routing so Codex `/v1/responses` and `/backend-api/codex/*` shapes are safely extractable, reconstructable, and compressible with zero-downside fallback, without live-wiring the user's Codex install. Detail: `docs/todo/t73-codex-request-shape-compression.md`
- [x] T74 - Structure-preview reversible safety: chose the safe fallback while preview recovery was missing. Superseded by T76, which restored `structure_preview = true` after archive-backed recovery landed. Detail: `docs/todo/t74-structure-preview-reversible-safety.md`
- [!] T75 - Codex evidence corpus and savings telemetry: offline reporting path, corpus metadata schema (`tests/fixtures/codex/codex-metadata.json`, schema_version=1), and CI-enforced regression gate (`scripts/benchmarks codex-smoke-gate` wired as final step of `scripts/ci`) are all implemented. Real 10-20 live Codex session capture remains blocked until the operator explicitly allows live Codex use. Detail: `docs/todo/t75-codex-evidence-corpus-and-telemetry.md`

### Reihenfolge

T70 -> T72 -> T73 -> T74 -> T75 -> T71. T70 comes first because no release is
real while the repository-native gate fails. T72 comes before T73 because the
installer must be single-source before the Codex path is called ready. T73 does
not require live Codex wiring; it proves the code path with fixtures and stub
upstreams. T74 closed the default-on lossy preview risk by making structure
preview opt-in again. T75 turns the working path into evidence. T71 is the optional live-machine
certification step and stays blocked until the operator explicitly wants Codex
wired into the active local setup.

### Audit facts that opened this program

- `go test ./...`, `go test -race ./...`, `bun test tests/ts`, and
  `go test -tags=integration ./tests/integration` are green.
- Initial audit found `go run ./scripts/ci` failing at the coverage gate:
  total statements `99.4%`, required `100.0%`.
- T70 closure re-ran the full proof stack on 2026-04-29:
  `go run ./scripts/coverage -min=100`, `go run ./scripts/ci`,
  `go test ./...`, `go test -race ./...`, `bun test tests/ts`, and
  `go test -tags=integration ./tests/integration` all passed.
- Local Codex is installed at `/Users/christopher/.npm-global/bin/codex`
  (`codex-cli 0.125.0`), but `integrate status --client codex` reports
  `partially_wired`: hooks installed, config not wired, daemon unreachable.
- `~/.codex/hooks.json` currently has Codex PreToolUse/PostToolUse Bash hooks
  but no Read hook entry, while current code can generate a read hook.
- T72 closed the competing-writer issue: `hook install codex` now routes through
  the same integration-owned Codex config writer as `integrate install --client
  codex`.
- T73 closed the proxy-side Codex compression gap: `/v1/responses` and
  `/backend-api/codex/*` are potential compression paths, known Codex
  `messages`/Responses `input` bodies are compressed, and unknown shapes
  passthrough without 400.

---

## Strategic Improvement Program (2026-04-30)

Output of the 2026-04-30 repository-wide concept review. Each entry has a
detail file under `docs/todo/`. The phases are sequenced by impact unlock:
**Phase L (T76-T77)** is the foundation — reversibility-by-default plus a
quality calibration loop unblock every later "default-on / aggressive" mode
because they let the system measure when compression hurts and recover from
it. **Phase M+ runs in parallel** once Phase L is in place.

Out of scope for this program (intentionally not adressed):
Windows support, zsh/fish completion, Prometheus exposition, embedding-based
similarity, auto-tuning by RL, public marketing claims, automated live paid
API calls in default CI, mutating the operator's live Codex install.

### Phase L - Reversibility foundation (P0)

- [!] T76 - Reversibility by default: WP1 archive package + WP2(coarse + per-sub-layer attribution shipped 2026-04-30) + WP4 expand + WP5 structure_preview default-on landed. WP3 (opportunistic re-injection) tracked as T76c. Detail: `docs/todo/t76-reversibility-by-default.md`
- [x] T76c - 2026-04-30: opportunistic re-injection signal shipped. Non-streaming upstream responses are scanned for `local-archive://<id>` URIs; each match bumps `contentarchive.RecordReInject` so `/admin/status.content_archive.re_inject_count` reflects the model's actual reach. Expansion on the next request is already handled by the existing `reinjectArchivedContent` pass. SSE re-injection deferred (separate streaming-tap concern, tracked alongside T108).
- [x] T77 - 2026-04-30: quality calibration loop shipped. Re-read detector + cache-miss-spike detector + net-savings tracker live in `internal/quality/`, surface via `/admin/status.quality`, render in TUI Stats view "QUALITY SIGNALS (T77)" card, exposed via `slimference quality [--json] [--url]` CLI. `RequestSummary` carries `re_read_count` + `net_saved_tokens` (committed earlier in 0a736fe). 100% coverage. Detail: `docs/todo/t77-quality-calibration-loop.md`

### Phase M - Concept levers (P0/P1)

- [x] T78 - 2026-04-30: shipped non-streaming server-state lever for OpenAI Responses + CodexChatGPT. Body rewrite to `previous_response_id`, response-id capture from non-streaming responses, recovery on `previous_response_id not found` 4xx, telemetry via `/admin/status.server_state.{skip_total,recover_total}`. Default off (`[proxy] server_state_enabled`). Streaming response-id capture deferred (follow-up). Detail: `docs/todo/t78-provider-server-state.md`

### Phase N - UX visibility and ergonomics (P1)

- [x] T79 - 2026-04-30: headless `slimference watch` shipped (`cmd/slimference/watch_cmd.go`) - polls `/admin/status` at `--interval`, prints compact savings + provider state. Native macOS menubar deferred. Detail: `docs/todo/t79-daemon-visibility-menubar.md`
- [x] T80 - 2026-04-30: `slimference savings [today|week|month|all]` shipped (`cmd/slimference/savings_cmd.go`) - aggregates filter.db + analytics + cache savings; text / `--json` / `--csv`. Detail: `docs/todo/t80-unified-savings-command.md`
- [x] T81 - 2026-04-30: bypass granularity complete. Duration-bounded (`SetBypassFor`, lazy auto-revert), `--next-request[=N]`, **per-route** (`SetBypassedRoutes`, exact path match in `ServeHTTP`), and **per-tool** (`SetBypassedTools`, name-substring scan on the body before compression). Detail: `docs/todo/t81-bypass-granularity.md`
- [x] T82 - 2026-04-30: `slimference compress-preview` shipped (`cmd/slimference/preview_cmd.go`) - reads body, runs L0/L1 with nop summarizer, prints rewritten body / diff / JSON envelope. Detail: `docs/todo/t82-compression-preview-cli.md`

### Phase O - Stability hardening (P1)

- [x] T83 - 2026-04-30: provider health monitor shipped (`internal/proxy/health_monitor.go`) - rolling success/error window per provider, `/admin/status.providers` + `any_provider_degraded`, watch surface. Detail: `docs/todo/t83-provider-degradation-visibility.md`
- [!] T84 - SPEC PREMISE INACCURATE: only filter.db is SQLite (others are JSON), and connection lifecycle is short-lived; WAL + periodic checkpoint adds no value under current access pattern. Closed as no-op. Detail: `docs/todo/t84-sqlite-wal-checkpoint.md`
- [x] T85 - 2026-04-30: drain landed (`[proxy] drain_timeout_seconds` + `applyDrainTimeout` wraps in-flight context with deadline; analytics queue drained on shutdown). Detail: `docs/todo/t85-graceful-drain-on-restart.md`

### Phase P - MiniMax determinism and prompt hygiene (P1/P2)

- [x] T86 - Versioned prompt override shipped: `[compression] prompt_override_path` loads on proxy start, `SetPromptOverride` propagates body + version, doctor surfaces active version, `prompt_version` telemetry field emitted. Detail: `docs/todo/t86-configurable-system-prompt.md`
- [x] T87 - Multi-stack few-shot examples landed: Go / Python / TS variants with input-detection picker, telemetry counters, default to Go on ambiguity. Detail: `docs/todo/t87-multi-stack-few-shot-examples.md`
- [x] T88 - 2026-04-30: full capability map shipped. `types.ProviderCapabilities` registry (Anthropic / OpenAI / Codex), MiniMax client emits seed + min_tokens when capability allows, `[compression.summary] require_deterministic` skips chain providers without `temperature=0 + seed`, `slimference doctor` warns on the determinism gap. 100% coverage. Detail: `docs/todo/t88-seed-and-provider-capability-map.md`
- [x] T89 - Robust CoT stripping: 12-family canonical strip set with fixed-point loop and per-tag counters; legacy single-family regex retired. Config knob deferred. Detail: `docs/todo/t89-robust-cot-stripping.md`
- [x] T90 - Deterministic repair (header strip / `*`+`1.` -> `- ` normalisation / preamble trim) runs before retry path; bypasses API round-trip when format-only failures can be fixed locally. Model-driven repair deferred. Detail: `docs/todo/t90-partial-repair-on-validator-fail.md`
- [x] T91 - 2026-04-30: MiniMax client honours `[compression.minimax] enable_seed` / `enable_min_tokens` via `SetCapabilities` from `NewLayer2`. Both default off until live verification. Detail: `docs/todo/t91-min-completion-tokens.md`
- [x] T92 - Per-bullet lineage markers landed: prompt requests `[msg:N,M]`, validator tolerates them, helpers + counters expose marker-presence rate. T76 WP3 consumer deferred. Detail: `docs/todo/t92-per-bullet-lineage-markers.md`

### Phase Q - Layer 0 improvements (P2)

- [x] T93 - 2026-04-30: posttool path shipped. Per-session repetition store in `internal/repetition/` records (session, tool, command, output_sha) tuples via SQLite. On count >= 3, `handlePostToolCmd` replaces output with `[tool output identical to msg #N (seen M times)]` marker. `slimference filter` subprocess case skipped (no session_id). 100% coverage, race-clean. Detail: `docs/todo/t93-l0-cross-session-pattern-mining.md`
- [x] T94 - 2026-04-30: streaming pump shipped as `slimference filter --stream <cmd>` (`internal/filter/stream.go`). Sliding window + flush ticker + ANSI-strip + dedup; race-clean unit tests; help / completion registered. Detail: `docs/todo/t94-l0-streaming-filter.md`
- [!] T95 - DEFERRED: filter subprocess has no live provider context; cleanest path needs hook-install plumbing for `--provider` or env var. Re-open when evidence shows the rune budget is wrong for a specific provider by more than ~15%. Detail: `docs/todo/t95-l0-tokenizer-aware-budgets.md`

### Phase R - Layer 1 improvements (P1/P2)

- [x] T96 - Conversation-level dedup landed: ContentIndex namespaced by session id, false-positive cross-session references fixed. Detail: `docs/todo/t96-l1-conversation-level-dedup.md`
- [!] T97 - DEFERRED: hybrid tree-sitter requires Cgo dependency and substantially raises the build matrix; defer until concrete fixtures show regex misses on non-trivial templates / embedded DSLs. Detail: `docs/todo/t97-l1-hybrid-structure-extraction.md`
- [x] T98 - Comment-strip whitelist preserves SAFETY / INVARIANT / TODO(critical) / FIXME(critical) / HACK(critical) / Copyright / SPDX / Licensed-under / All-rights-reserved across C-style, hash, and Python strippers. Multi-line license blocks preserved. Config knob deferred. Detail: `docs/todo/t98-l1-comment-strip-whitelist.md`

### Phase S - Layer 2 improvements (P2)

- [x] T99 - 2026-04-30: mid-exchange detector + replacement semantics shipped as deterministic stub in `internal/summarization/midexchange.go`. Detects completed tool-use cycles (assistant[tool_use] -> user[tool_result] -> assistant) within the in-flight exchange; if cumulative tokens exceed `mid_exchange_threshold_tokens` (default 10000), replaces the range with an `[in-progress summary, anchor=msg #N]` block. No LLM call. Gated by `[compression.tuning] mid_exchange_enabled` (default off). Wire-in at handler step 4.5. 100% coverage. Detail: `docs/todo/t99-l2-mid-exchange-summary.md`
- [x] T99b - 2026-04-30: live MiniMax summary path shipped. `Layer2.ApplyMidExchange(ctx, messages, threshold)` runs the FallbackChain on the rendered range and falls back to the deterministic stub when the chain errors out or has no configured provider. Handler step 4.5 now calls the Layer2 method instead of the package-level stub.
- [x] T99c - 2026-04-30: idempotency shipped. `IsMidExchangeMarker` recognises synthetic blocks; `DetectMidExchangePoint` short-circuits when the in-flight exchange already contains one. Pure-function, 100% coverage.
- [x] T100 - 2026-04-30: coordinator decision-rule wired. Handler checks `CoordinatorEnabled && L2 enabled && origTokens >= MinTokensForLayer2`, sets `SetCoordinatorSubsume(true)` before L1, L1 skips heavy sub-layers (dedup/structure/delta/tool-compressor/success-short/image) while preserving cheap passes (ANSI/JSON). Telemetry via `/admin/status.coordinator.skipped_total`. Default off. Detail: `docs/todo/t100-l2-cross-direction-coordinator.md`
- [!] T100b - Soak-window verification automation shipped: `slimference soak [today|week|month|all]` walks the daily analytics + quality snapshots, detects prompt-cache regression / overflow retries / MiniMax failure spikes, and prints a verdict line ("ok to enable both T100 and T103" / "T100 needs more soak time" / "neither flag is safe yet"). The remaining manual step is collecting at least a few days of real traffic with the flags off, then re-running `slimference soak`.

### Phase T - Layer 3 improvements (P2)

- [!] T101 - ALREADY IMPLEMENTED: `caching.ExtractDependencyPaths` + `FileWatcher` already invalidate cache entries on real filesystem changes; the dependency-watcher approach supersedes the mtime-hash key proposal. Closed as no-op. Detail: `docs/todo/t101-l3-cache-invalidation-code-change.md`
- [x] T102 - Cache TTL aging: existing TTL + 60s janitor already enforces aging; added `/admin/status.cache_age` histogram (count / p50 / p95 / p99 / max in ms). Detail: `docs/todo/t102-l3-cache-ttl-aging.md`

### Phase U - Layer 4 (new layer, P1)

- [x] T103 - 2026-04-30: Layer 4 tool-definition pruning forward-path shipped. Pure-function pruner in `internal/toolprune/pruner.go` handles Anthropic + OpenAI tool shapes. Wire-in in handler.go step 7.5: extracts tool names, observes usage via tracker, prunes idle definitions, surfaces in `/admin/status.tool_prune`. Gated by `[compression.tuning] tool_prune_enabled` (default off). 100% coverage, race-clean. Detail: `docs/todo/t103-l4-tool-definition-pruning.md`
- [x] T103b - 2026-04-30: heuristic-mention reattach path shipped. `UsageTracker.RememberPrunedDef` caches the original tool definition when L4 prunes it; `MentionedTools` (word-boundary substring match on the request body's text content) detects when the model needs the tool back; `ReattachToolDefinitions` re-adds the def to the body's `tools[]` array. Reattach counter (`/admin/status.tool_prune.reattach_total`) advances per re-attached def. Upstream-error-driven retry (the second design alternative) remains untouched; reopen as T103d if heuristic reattach proves too eager.
- [!] T103c - Soak-window verification automation shipped (same `slimference soak` subcommand as T100b above). Manual close once a few days of real traffic exist.

### Phase V - Algorithmic and efficiency (P2)

- [x] T104 - 2026-04-30: message-level goroutine fan-out shipped. `compressMessage` runs concurrently per message in the compressible prefix via WaitGroup + GOMAXPROCS-bounded semaphore. `archiveOriginal` mutex-protected, `coordinatorSkipped` atomic. Gated by `[compression.tuning] coordinator_parallel` (default off). Race-clean, 100% coverage. **Spec-deviation**: shipped at message granularity instead of stage-partitioned sub-layer concurrency; latency-drop benchmark deferred. Detail: `docs/todo/t104-l1-sublayer-fan-out.md`
- [x] T104b - 2026-04-30: 200KB-body benchmark fixture shipped (`BenchmarkCompress_LargeBody_Sequential` vs `_Parallel` in `internal/compression/bench_test.go`). On Apple M1 the message-level fan-out from T104 saves ~11% on the 200KB payload (461μs sequential vs 409μs parallel). Stage-partitioned sub-layer fan-out is therefore not pursued: the message granularity already covers most of the win and a stage-partitioned variant would add complexity for a smaller delta. Reopen with concrete profiler evidence if a workload appears where goroutine startup or sequential sub-layer overhead dominates.
- [!] T105 - Anthropic default-on calibration already lives in T28; multi-provider extension (OpenAI / Codex) deferred to a dedicated task when evidence shows divergence. Detail: `docs/todo/t105-token-estimator-self-calibration-default.md`
- [!] T106 - SPEC PREMISE INACCURATE: filter writes are one-shot per subprocess; no long-lived connection accumulates rows. Cross-process batching would need IPC, far outside scope. Closed as no-op. Detail: `docs/todo/t106-batched-filter-db-writes.md`
- [x] T107 - Conversation-scoped dedup cache landed alongside T96: ContentIndex persists across requests on the live compressor and is now session-namespaced so cross-session interference is gone. Detail: `docs/todo/t107-conversation-scoped-dedup-cache.md`
- [x] T108 - 2026-04-30: chunked Layer 1 pipeline shipped as standalone API in `internal/compression/streaming.go` (`StreamingCompress`, `StreamingOptions`, `StreamingStats`, `IsStreamingSafe`, `StreamingSafeNames`). Streaming-safe sub-layers: `ansi_strip`, `line_dedup`, `repeated_collapse`. Bounded memory ceiling (PeakWindowSize <= WindowLines, 1 MiB scanner buffer). 100k-line synthetic test pins behaviour. Gated by `[compression.tuning] streaming_compression_enabled` (default off). Hot-path wire-in (`internal/proxy/streaming.go`) intentionally deferred — touches the SSE byte-tee and is risky; reopen as T108b once a real workload triggers the chunked path. Detail: `docs/todo/t108-streaming-compression.md`

### Sequencing notes

- T76 -> T77 -> everything else. Both are preconditions for safely shipping
  any "default-on aggressive" mode and for trusting any tuning data.
- T78 must land alongside T101 because both touch upstream-state semantics.
- T86 -> T87 -> T88 -> T89 -> T90 -> T91 -> T92 is the natural order inside
  Phase P; each builds on the last.
- T103 (Tool-Definition Pruning) requires T76 because tool-schema removal
  must be reversible.
- T100 must come after T76 because the L1/L2 coordinator only buys real
  savings when it can also skip-and-archive instead of skip-and-lose.

### Out of scope (deliberately not added)

- Anything touching the operator's live Codex install (still blocked, see
  T71 / T75).
- Windows support, zsh / fish completion (decided 2026-04-18).
- Embedding-based similarity, RL-driven auto-tuning (separate research
  track if and when warranted).
- Public marketing claims; live paid API calls in default CI.

---

## Audit-Driven Mitigation Program (2026-04-30)

Output of the 2026-04-30 deep code audit (see chat transcript, agent
findings + savings reality check). Each entry has a detail file under
`docs/todo/`. Goal: turn every concrete weakness into either a complete
mitigation or, where the mechanism is sound, an outright strength.

The headline movements:

- L2 hardened from "ships everything to MiniMax by default" to "redacted +
  default-off + trust-labelled" via T109/T121.
- L2 cache fixed from "global singleton with cross-session leakage" to
  "session-keyed multi-slot with disk persistence" via T110.
- L2 anchor correctness gap closed via T111 (anchors survive ApplyToMessages).
- Adaptive window goes from dead code to wired hot-path lever via T112.
- Codex hook moves from "block-rerun" workaround to transparent rewrite
  via T113.
- Tokenizer cold-start error bounded to ±5% via T114.
- Substring-grep failure detection replaced with structured per-tool
  parsers via T115.
- Loop nudge stops reporting fictitious savings; honest measurement +
  optional subtractive migration via T116.
- Log filtering stops being limited to docker/kubectl, generic shape
  detector via T117.
- Synthetic-only corpus replaced with real-session corpus + CI gate via
  T118.
- Layer 0 empty-only stubs (~70% of leaves today) drop to ≤30% via T119
  (the largest unrealised lever in the project).
- Filter dispatch hardened against panics + per-filter observability via
  T120.

### Phase W - Data policy + L2 trust foundation (P0)

- [x] T109 - 2026-04-30: outbound redaction shipped. `Redactor` in `internal/summarization/redact.go` runs structural-first (HTTP auth headers + JSON credential keys), then pattern-based (security detector reuse), then path normalisation (`<HOME>` / `<TMP>`); strict mode adds full tool_input drop + recursive JSON sweep. Wired into `Layer2.RunCompressionJobContext` + `Layer2.ApplyMidExchange`. Default `[compression.summary] outbound_redaction = "default"`. Telemetry surfaces via `/admin/status.layer2.redaction` and `Layer2.RedactionCounters()`. `slimference doctor` adds an "L2 outbound redaction" check that FAILs on `off`. 100% coverage, race-clean. Detail: `docs/todo/t109-l2-outbound-redaction.md`
- [x] T121 - Layer 2 default-off + opt-in flow + provider trust labelling. Detail: `docs/todo/t121-l2-default-off-and-trust-labels.md`

### Phase X - L2 correctness fixes (P0)

- [x] T110 - 2026-05-01: Layer 2 cache session-keyed multi-slot replacement (core: session ID extraction, LRU, hash invalidation, telemetry). Disk persistence considered and rejected: cache lives only while proxy runs, restart cost is one cold L2 hit, multi-process setups do not exist; ~300 LOC for marginal value. Detail: `docs/todo/t110-l2-cache-session-keyed.md`
- [x] T111 - 2026-05-01: Layer 2 anchor verbatim re-injection in ApplyToMessages. Detail: `docs/todo/t111-l2-anchor-reinjection.md`

### Phase Y - Realised levers + measurement (P0/P1)

- [x] T118 (core) - 2026-05-01: Capture-session subcommand, benchmark-corpus per-category gate, synthetic seed corpus, live-corpus policy doc, and CI step 7/7 shipped. **T118b** is operator-driven (>=10 real-session categories captured-and-scrubbed under `tests/fixtures/live_corpus/`); the harness is ready, only the contents are pending. Detail: `docs/todo/t118-live-corpus-and-savings-gate.md`
- [x] T119 - 2026-05-01: Layer 0 leaf-audit tool + CI gate landed under `scripts/utils/leaf_audit.go`. Generated `docs/layer0-leaf-audit.md`. Audit revealed empty-only-stub ratio is 4.8% (10 of 209), not the ~70% the brief assumed; the overall premise of T119 was based on a misread of the package. T119c terraform parser shipped alongside (see entry below). T119b (kubectl/docker/helm helper consolidation) and T119g (du/df/stat parsers) considered and rejected: the first is a refactor with zero token-saving change, the second targets outputs already too small to compress. Detail: `docs/todo/t119-l0-stub-to-compactor-uplift.md`
- [x] T119c - 2026-05-01: Terraform plan/apply structured compactor. Detail: `docs/todo/t119c-terraform-parser.md`

### Phase Q - Transparent system-wide intercept (planned 2026-05-01)

- [!] T122 - 2026-05-01: Transparent-mode components landed (CA, signer, CONNECT/MITM unit path, WebSocket tunnel helper, networksetup/keychain/launchd/proxy CLI, docs), but repository audit found the live proxy path is not yet certified as fully wired end-to-end. Treat T122 as component-complete, not live-certified. Runtime closure + Codex Desktop proof is tracked as T131 before any stealth work. Detail: `docs/todo/t122-transparent-mode.md`. User-facing docs: `docs/transparent-mode.md`.
- [x] T112 - 2026-05-01: Adaptive sliding window hot-path activation behind `[compression.tuning] adaptive_window_enabled` flag (default off; off-path byte-equal to baseline). Detail: `docs/todo/t112-adaptive-window-activation.md`

### Phase R - Effectiveness + Stealth Boost (corrected plan 2026-05-01)

Reality-check correction: Phase R must not start from marketing claims. The repo is strong, but two blockers sit in front of the boost work: transparent mode needs runtime/live proof (T131), and Layer 2 has a race-clean blocker before any default-on flip (T132/T129). T127's lossless image re-encode plan is rejected because image-token billing is driven by pixel dimensions/detail, not PNG/WebP byte size. Images stay untouched unless a future task explicitly scopes adaptive downsampling.

Required order: T131 -> T123 -> T130 -> T128 -> T124 -> T125 -> T126-mini -> T132 -> T129. T127 stays recorded as rejected/no-op.

- [!] T131 - Transparent-mode runtime closure is code-complete and locally verified: runtime CONNECT/MITM wiring, streaming-safe MITM writer with connection-close framing, WebSocket upgrade reachability with buffered-byte preservation, daemon-down status repair hint, daemon-health proxy-env bypass, and direct-mode gate tests are landed. `go run ./scripts/ci` passes 8/8 and focused race passes for touched packages. Manual macOS E2E proof (Codex Desktop / Browser-Use / microphone / disable-uninstall) is still pending before T122 can be called live-certified. Detail: `docs/todo/t131-transparent-runtime-closure.md`
- [!] T123 - TLS-fingerprint mimicry code is landed: `internal/tlsdial` uses uTLS profiles in transparent mode, direct mode remains stdlib, WebSocket and HTTP upstream dials share per-host profile resolution, uTLS handshake cancellation is covered, `proxy status` prints active profiles, `doctor` reports TLS profile catalogue freshness, and `go run ./scripts/utils tls-probe --profile=chromium_stable --json` locally captures/parses the ClientHello and proves the default profile differs from Go stdlib. External reflected JA3/JA4 proof is still pending; no "undetectable" claim. Detail: `docs/todo/t123-tls-fingerprint-mimicry.md`
- [!] T130 - Layer 4 output-token compression is code-complete: provider-specific, idempotent output-discipline injection, min-token gate, config/CLI toggle, admin telemetry, persisted `gain --output` telemetry, and hot-path tests are landed. Live output-saving proof and auto-soften/disable remain pending real-session data. Detail: `docs/todo/t130-output-token-compression.md`
- [!] T128 - Conversation state + prompt-cache hardening is code-complete for the safe hot-path scope: Anthropic cache-control placement now prioritises large stable tool results and late stable turns, OpenAI/Codex keeps the existing T78 server-state owner without false token-saving claims, admin status exposes provider-reported cache read/create tokens, and `slimference gain --cache` reports persisted prompt-cache counters. Live 30+-turn saving proof remains pending. Detail: `docs/todo/t128-conversational-state-diffing.md`
- [!] T124 - Layer 0 real-traffic parser expansion is code-complete for the safe core: existing built-ins stayed intact, new structured diagnostic parsing covers TypeScript/TSX, Svelte, Zig, SQL/DB, Markdown and practical ecosystem compiler/linter rows, language detection now covers the requested/top stacks, and `slimference gain --by-parser` groups persisted Layer 0 savings by parser/tool family. Live corpus proof remains pending. Detail: `docs/todo/t124-layer0-language-parsers.md`
- [!] T125 - AST/file-read compaction is code-complete for the safe Go path: large `.go` full-file `cat` reads use stdlib AST skeletoning with selected body inclusion and conservative mode gates; `head`/`tail`, edit/debug, recently-edited, force-full and small-file paths bypass. Body-on-demand/session cache and live net-saving proof remain pending. Detail: `docs/todo/t125-ast-code-compaction.md`
- [!] T126 - Cross-tool result deduplication mini-scope has a safe library landed: git status/name-only path detectors, per-session state, exact-list marker elision, reset and tests. Hot-path integration is deferred because `git status` already compacts to counts and `internal/filter` does not own user-turn session boundaries. Detail: `docs/todo/t126-cross-tool-dedup.md`
- [!] T127 - REJECTED / no implementation: lossless image re-encoding is not a valid token-saving lever for OpenAI/Anthropic vision because same pixel dimensions bill the same image tokens. Images remain untouched. Detail: `docs/todo/t127-image-compression.md`
- [!] T132 - Layer 2 race-clean blocker is code-complete and full-race green for the known `examplePromptCounters` race: telemetry counters are mutex-protected, focused race tests pass, and `go test -race ./...` passes. Detail: `docs/todo/t132-layer2-race-clean.md`
- [!] T129 - Layer 2 default-ON re-flip is code-complete: fresh configs default to L2 on, explicit `layer2_enabled=false` remains off, doctor emits a WARN-level outbound MiniMax data-flow line, first interactive startup records an explicit acknowledgement, non-interactive startup warns without hanging, `layer2 status` shows ack state, `docs/data-policy.md` reflects T129, and full CI/race gates are green. Detail: `docs/todo/t129-layer2-default-on.md`

### Phase AA - Codex transparent productization + max-efficiency closure (P0/P1)

New target architecture (2026-05-13): Codex CLI/App should not need config-patching or install mutation for the main product path. The primary path is a local always-available Slimference daemon plus macOS trusted local CA plus system HTTPS proxy toggle ("certificate magnet"). The daemon is installed/removed/started/stopped by Slimference, autostart is controlled by Slimference, and the TUI is the operator console for install state, armed/disarmed state, layer toggles, savings, debug logs, and repair actions. Codex hooks remain an optional precision layer because the official Codex hook contract still cannot transparently rewrite tool input (`updatedInput` fails open), while `PostToolUse`, `SessionStart`, `UserPromptSubmit`, `PermissionRequest`, and `Stop` can still add value when explicitly installed.

Required order: T137 -> T133 -> T134 -> T135 -> T136 -> T138 -> T139 -> T141 -> T140. T140 is last because only it proves the whole system against real Codex CLI/App traffic, Browser-Use passthrough, WebSocket continuation, voice bypass, disable/uninstall, and real savings/cached-token telemetry.

- [x] T137 - 2026-05-13: Integration command split cleanup landed. `hook install codex` is hook-only in the sense that it never writes Codex base URLs; it now enables only the required `codex_hooks=true` feature flag for official hooks. `hook verify codex` checks hook artifacts only and points config-patch users to `integrate status --client codex`; `integrate` remains explicit legacy/config-patch mode; docs/help present transparent proxy mode as the default non-mutating Codex path. `go run ./scripts/ci` passes 8/8 with 100% coverage. Detail: `docs/todo/t137-integration-command-split-cleanup.md`
- [x] T133 - 2026-05-13: Transparent daemon/TUI control plane landed. Setup now starts with transparent CA+daemon install and arm steps, Codex/Claude hooks are legacy fallback steps, Dashboard can arm/disarm transparent mode, Setup exposes `[a]` arm/disarm and `[u]` uninstall, status covers CA/trust/autostart/system-proxy/daemon-reachable states from a cached snapshot, and TUI actions reuse `proxy install|enable|disable|uninstall` command plumbing. Live Codex/App UX proof remains T140. Detail: `docs/todo/t133-transparent-tui-daemon-control-plane.md`
- [x] T134 - 2026-05-13: Runtime flight recorder and savings truth landed. Request summaries now hydrate normalized `flight` records; recorder redacts secrets and local paths before memory/disk persistence; proxy, CONNECT/MITM/raw passthrough/WebSocket, hook_pre, hook_post, and readhook paths emit route/layer/token/cache/error metadata; OpenAI/Codex `cached_tokens` and Anthropic cache usage are separated from estimates; `debug flight last|tail|replay|export` supports JSON/CSV; TUI Debug shows the same flight records. Live Codex/App corpus proof remains T140/T118b. Detail: `docs/todo/t134-runtime-flight-recorder-savings-truth.md`
- [x] T135 - 2026-05-13: Codex hook contract max-out landed for local/provable scope. `hook install codex` now installs SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, and Stop scripts; enables only `codex_hooks=true`; verifies each artifact; keeps `updatedInput` disabled as parsed-fail-open; PostToolUse emits `continue:false` replacement feedback; PermissionRequest maps Layer-0 deny/ask policy. Live event-delivery proof remains T140. Detail: `docs/todo/t135-codex-hook-contract-max-out.md`
- [x] T136 - 2026-05-13: OpenAI prompt-cache modernization landed for safe local scope. OpenAI/Codex cached-token usage is parsed into flight/cache telemetry; provider capabilities distinguish cache usage/key/retention and previous-response billing; optional `[proxy.openai_prompt_cache]` injects hashed `prompt_cache_key` and model-gated `prompt_cache_retention` only for generic OpenAI API requests with one retry without hints on provider rejection. CodexChatGPT cache-key injection and WebSocket continuation remain T140 live-proof items. Detail: `docs/todo/t136-openai-codex-prompt-cache-modernization.md`
- [~] T138 - Session/turn ownership spine for AST and cross-tool state is in progress: the bounded `internal/sessions.TurnStateStore` core is landed, and Codex hook processes now share a file-backed turn-state adapter under `~/.slimference/turn-state/`. T125 FileReadContext is wired for PostToolUse/file-read compaction and recently-edited reads stay literal. T126 hot-path integration and body-on-demand retrieval remain open. Detail: `docs/todo/t138-session-turn-ownership-spine.md`
- [x] T139 - 2026-05-13: TLS proof hardening landed for local scope. `internal/tlsproof` stores JSONL proof records under `~/.slimference/tls-proofs/`; `tls-probe` now supports reflected HTTPS proof attempts, JSON, save, and compare modes while still using `internal/tlsdial`; alias targets are explicit; `doctor` reports catalogue age plus latest reflected proof status. HTTP/2 reflector negotiation is marked unproven instead of faked. Live provider-edge proof still requires an operator-chosen reflector run. Detail: `docs/todo/t139-tls-provider-edge-proof.md`
- [x] T141 - 2026-05-13: Output-reduce auto-tuning landed. Profiles are tiered (`off`, `mild`, `standard`, `aggressive`, `codex_aggressive`, `custom`), task-shape detection feeds shape-specific guardrails, analytics/flight records carry task-shape metadata, and the tracker can auto-downgrade provider/model/task-shape buckets on failure or overhead signals. `gain --output` stays baseline-honest: observed output only, no fake saving claim. Detail: `docs/todo/t141-output-token-auto-tuning.md`
- [ ] T140 - Codex CLI/App live E2E certification and real corpus: CLI-only split proof is partially certified for the current Codex CLI without arming macOS System-HTTPS-Proxy (`proxy env codex --proxied`, per-process `slimference-codex` custom provider, `supports_websockets=false`, App remains direct). Default WebSocket tunnel works byte-for-byte; the old `direct_codex_websocket_policy = "force_https_fallback"` path remains only as fallback proof mode. Live CLI smoke + CLI tool-loop passed through the zstd HTTP pipeline. Remaining proof: Codex App, Browser-Use, voice/WebRTC bypass, disable/uninstall, scrubbed corpus savings, and WebSocket compression feasibility. Detail: `docs/todo/t140-codex-live-e2e-certification.md`

### Phase AB - Frontier compression engineering (P0/P1)

Phase AB is the maximum-upside track after the current Codex CLI/App path is certified. The goal is not "more knobs"; it is a controlled optimizer that turns every provider-supported reuse mechanism, every deterministic compaction lever, and every safe LLM summarization opportunity into measured savings without quality loss. All tasks below are gated by T146 live-corpus evidence and by T149 planner/safety decisions before default-on.

Required order: T146 baseline corpus expansion -> T142 inspect-only WebSocket frame corpus -> T149 planner spine -> T143/T144/T145/T148/T147 in parallel-safe slices -> T142 mutation mode only after frame-shape proof. WebSocket mutation is never enabled by default from static assumptions.

- [ ] T142 - Codex WebSocket message-boundary compression: T142a inspect-only foundation landed. `internal/wscompact` parses RFC 6455 frames without mutation, reassembles fragmented text for redacted shape summaries, marks RSV/compressed frames as blockers, and `WebSocketTunnel` can attach the inspector while preserving byte-for-byte default tunnel behavior. Remaining: live Codex frame corpus, shape registry, shadow compression, mutation mode. Detail: `docs/todo/t142-websocket-message-boundary-compression.md`
- [ ] T143 - Layer 1 semantic deterministic compaction frontier: T143a reversible path dictionary and T143b Markdown/SQL/GraphQL/HCL/Dockerfile/Makefile structure extraction landed. Remaining: tokenizer-aware budgets, deeper multi-language symbol slicing, stacktrace/test-failure semantics, config/schema deltas, and quality/live-corpus gates. Detail: `docs/todo/t143-l1-semantic-deterministic-frontier.md`
- [ ] T144 - Layer 2 adaptive summarization accelerator: make MiniMax summarization fire earlier only when ROI is provably positive, add background pre-summary, hierarchical task-shaped summaries, stronger prompt contracts, and model-agnostic provider configuration while preserving redaction, anchors, and no-loss fallback. Detail: `docs/todo/t144-l2-adaptive-summarization-accelerator.md`
- [ ] T145 - Layer 3 provider-cache and state-reuse maximizer: aggressively use OpenAI/Codex/Anthropic prompt-cache, `prompt_cache_key`, retention, `previous_response_id`, stable prefix planning, cache heat maps, and provider-reported cached-token accounting without fake savings claims. Detail: `docs/todo/t145-l3-cache-state-reuse-maximizer.md`
- [ ] T146 - Real live corpus maximal evidence program: T146a/T146b/T146c landed. `scripts/verify -mode live-corpus-plan` prints the operator capture/export/metadata/gate runbook, and `benchmark-corpus` now reports/gates evidence level, output tokens, provider-cache read/create/cached tokens, output-reduce hits, errors, latency p95, planner planned-vs-actual replay signals, and observed layer-combination matrices. Remaining: actual operator Codex/App captures and true alternate-run layer-combination replay/A-B harness. Detail: `docs/todo/t146-real-live-corpus-maximal-evidence.md`
- [ ] T147 - Layer 0 real-traffic parser frontier: frontend, Python, SQL/DB, package-manager resolver-error, SQL-shell table, JVM/mobile, Docker/Kubernetes/Helm, monorepo diagnostic slices, runtime Layer-0 hit/miss telemetry, local-tokenizer `filter.db` accounting, and `gain --by-parser` family mapping landed. Covered locally: Next/Vite/Vitest/Jest/Playwright/ESLint/Biome/Oxlint/Turbo/Nx/Lerna/Bun, ruff/pylint/flake8/mypy/pyright/pytest/unittest matching, psql/sqlite/mysql/mariadb/Prisma/Drizzle/SQLFluff/Sqruff matching, npm/pnpm/yarn/bun/pip/uv install/update resolver summaries, psql/mysql/mariadb/sqlite table border compaction, Java/Kotlin/Swift/Dart/Flutter/PHP ecosystem diagnostics, and Docker/Podman/Nerdctl/Kubectl/OC/Helm diagnostics. Remaining: live-corpus quality/hit-rate proof; semantic DB explain summaries only if live corpus shows high-volume query-plan traffic. Detail: `docs/todo/t147-l0-parser-frontier-real-traffic.md`
- [ ] T148 - Output-reduce real-session aggressive autotuning: take T141 beyond local heuristics with true A/B baselines, per-model directive variants, repair-turn detection, task-shape quality gates, and profile evolution on real coding sessions. Detail: `docs/todo/t148-output-reduce-real-session-autotune.md`
- [ ] T149 - Cross-layer compression planner and safety governor: T149a/T149b/T149c/T149d/T149e landed. `internal/planner` deterministically chooses L0/L1/L2/L3/output/WebSocket actions from request facts, the proxy attaches content-free dry-run plans to upstream, local-cache, transparent CONNECT, and direct WebSocket flight/debug/TUI records without changing behavior, `slimference plan inspect` dry-runs planner facts or request files locally, corpus replay compares recorded planner actions against actual layer activity, and T141 output-reduce cooldown now feeds planner facts so aggressive profiles soften instead of drifting from the plan. Remaining: planner-controlled layer hints after real-corpus proof. Detail: `docs/todo/t149-cross-layer-compression-planner.md`

### Phase Z - Robustness, parsers, observability (P1/P2)

- [x] T113 (core) - 2026-05-01: Codex capability matrix + version detection + snapshot helper landed in `internal/hooks/codex_caps.go`. Script generator stays on legacy block+rerun path until Codex honours `updatedInput`. **T113b is BLOCKED** by upstream Codex hooks contract (developers.openai.com/codex/hooks: `updatedInput` parsed but not supported, fails open). Re-activation checklist captured in detail file. Detail: `docs/todo/t113-codex-transparent-rewrite.md`
- [x] T113b-notify - 2026-05-01: Drift watchdog tracks the installed Codex version and the capabilities Slimference advertises for it; doctor surfaces the snapshot so a Codex release flipping `updatedInput` to honoured is visible immediately without manual upstream-doc polling. Full T113b script-branch implementation still BLOCKED on upstream.
- [x] T115 - 2026-05-01: Build/test structured parsers (go, cargo, gcc/clang) replacing substring heuristic; per-tool registry with specific-first dispatch. Detail: `docs/todo/t115-build-test-failure-structured-parsers.md`
- [x] T117 - 2026-05-01: Generic log filtering (tail/journalctl/cat-on-log/grep argv detect + ISO/Unix/Syslog/Bracketed/JSONLines shape detect, dispatched before generic build/test fallbacks). Detail: `docs/todo/t117-generic-log-filtering.md`
- [x] T120 - 2026-05-01: Filter dispatch panic recovery via `runFilter` wrapper + per-filter observability (atomic counters, slow-filter detector, /admin status surface). Detail: `docs/todo/t120-filter-panic-recovery-observability.md`
- [x] T114 - 2026-05-01: Per-model tokenizer calibration with persistent EMA (`~/.slimference/calibration/anthropic.jsonl`). Detail: `docs/todo/t114-tokenizer-cold-start-corpus.md`
- [x] T116 - 2026-05-01: Loop nudge measurement-driven additive/subtractive switch (`[compression.tuning] loop_strategy`); honest LoopNudgeMeasurement replaces fixed 5000-saving estimate. Detail: `docs/todo/t116-loop-nudge-subtractive-migration.md`

### Sequencing notes (Audit Mitigation)

- T109 -> T121 -> T110 -> T111 is the correct L2 order: redaction first
  (so default-on flip is safe), default-off + opt-in second (so the
  flip is operator-explicit), cache fix third (so multi-session
  correctness holds), anchor fix fourth (so summaries are honest).
- T118 unlocks every other regression claim: without it, every "saves
  X%" number is a guess.
- T119 is the largest single-task lever in this program. Sub-tasks
  T119a-h are designed to ship independently so the empty-only-stub
  ratio drops monotonically.
- T120 unblocks T119 measurement: per-filter observability is the
  only way to verify the new parsers actually fire and save bytes
  proportional to expectation.
- T112 only ships behind a flag until T118 corpus exists to measure
  it; flag-on becomes the default after a soak window.
- T115 + T117 are independent of the L2 program and can run in parallel
  with W/X.
- T116 is intentionally last - it requires real-traffic data the
  measurement infrastructure (T118) provides.

### Out of scope (Audit Mitigation Program)

- Removing MiniMax as a provider entirely (operator may have a relationship; T121 makes it explicit-opt-in instead).
- Cross-machine corpus distribution beyond the maintainer's own scrubbed sessions (T118 stays repo-local).
- Auto-detect-and-rewrite of the operator's existing config to flip defaults (T121 preserves explicit prior opt-in; only fresh installs see the new default).
