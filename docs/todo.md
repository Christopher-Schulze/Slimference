# Slimference - Master TODO

**Normative Spec:** `spec+.md` · **Sequenz + vollständiges Onboarding:** `handover.md` (Repo-Root) §4 (steht über `spec+.md` §23); Kurzlink: `docs/HANDOVER.md`.  
**RTK (`research/rtk-ai/rtk/`):** nur Referenz beim Portieren — keine zweite Spec.

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

- [x] **Hohe Go-Coverage** auf `cmd/`, `internal/` via `*_test.go` — produktive Pfade und Safety-Branches abgedeckt
- [x] Coverage-Gate: Go-Tool unter **`scripts/coverage/`** — `go run ./scripts/coverage -min=95.0` implementiert + getestet
- [x] Benchmarks: `scripts/benchmarks/main.go` — Runner fuer `go test -bench=.` ueber compression + filter; `internal/compression/bench_test.go` (8 Benchmarks: Compress_small/medium/large/code, StripANSI, StripComments, ExtractStructure); `internal/filter/bench_test.go` (7 Benchmarks: GitStatus, BuildOutput, JSONMinify, applyLayer0, Truncate); `go run ./scripts/benchmarks -- -benchtime=3s`
- [x] **Zusätzliche** Testsuites: **`tests/ts/`** (TypeScript) — 6 Tests mit `bun:test`: session fixture schema-Validierung (3 Tests) + CLI integration (3 Tests); alle grün
- [x] `tests/integration/` (Go), `tests/fixtures/`: 3 Integration-Tests (`//go:build integration`) grün: CompressesLargeConversation (ratio=0.80, layers=[1]), PassthroughNonCompressiblePath, HealthEndpoint; Fixtures: `sample_session.jsonl`, `sample_config.toml`
- [x] Tests: Stil/Qualität wie `AGENTS.md` §5
- [x] **`research/rtk-ai/rtk/`**: nicht anfassen, nichts dorthin/davon verschieben (Fremdprojekt); `research/` ist gitignored, damit der lokale RTK-Snapshot nicht versehentlich staged wird

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
- [x] `~/.codex/config.toml` patchen: `openai_base_url` + `[features] hooks = true`
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
- [!] T125 - AST/file-read compaction is code-complete for the safe Go path: large `.go` full-file `cat` reads use stdlib AST skeletoning with selected body inclusion and conservative mode gates; `head`/`tail`, edit/debug, recently-edited, force-full and small-file paths bypass. Archive-backed `expand-body` retrieves omitted Go function/method bodies from the original archived read. Live net-saving proof remains pending. Detail: `docs/todo/t125-ast-code-compaction.md`
- [!] T126 - Cross-tool result deduplication mini-scope is code-complete for the safe Codex PostToolUse path: git status/name-only path detectors, file-backed per-turn hook state, exact-list marker elision for same-session/same-turn/same-CWD `git diff --name-only`, and full tests. Standalone `slimference filter`, non-git output, diff hunks, name-status, and git ls-files stay untouched until live corpus proof. Detail: `docs/todo/t126-cross-tool-dedup.md`
- [!] T127 - REJECTED / no implementation: lossless image re-encoding is not a valid token-saving lever for OpenAI/Anthropic vision because same pixel dimensions bill the same image tokens. Images remain untouched. Detail: `docs/todo/t127-image-compression.md`
- [!] T132 - Layer 2 race-clean blocker is code-complete and full-race green for the known `examplePromptCounters` race: telemetry counters are mutex-protected, focused race tests pass, and `go test -race ./...` passes. Detail: `docs/todo/t132-layer2-race-clean.md`
- [!] T129 - Layer 2 default-ON re-flip is code-complete: fresh configs default to L2 on, explicit `layer2_enabled=false` remains off, doctor emits a WARN-level outbound MiniMax data-flow line, first interactive startup records an explicit acknowledgement, non-interactive startup warns without hanging, `layer2 status` shows ack state, `docs/data-policy.md` reflects T129, and full CI/race gates are green. Detail: `docs/todo/t129-layer2-default-on.md`

### Phase AA - Codex transparent productization + max-efficiency closure (P0/P1)

New target architecture (2026-05-13): Codex CLI/App should not need config-patching or install mutation for the main product path. The primary path is a local always-available Slimference daemon plus macOS trusted local CA plus system HTTPS proxy toggle ("certificate magnet"). The daemon is installed/removed/started/stopped by Slimference, autostart is controlled by Slimference, and the TUI is the operator console for install state, armed/disarmed state, layer toggles, savings, debug logs, and repair actions. Codex hooks remain an optional precision layer because the official Codex hook contract still cannot transparently rewrite tool input (`updatedInput` fails open), while `PostToolUse`, `SessionStart`, `UserPromptSubmit`, `PermissionRequest`, and `Stop` can still add value when explicitly installed.

Required order: T137 -> T133 -> T134 -> T135 -> T136 -> T138 -> T139 -> T141 -> T140. T140 is last because only it proves the whole system against real Codex CLI/App traffic, Browser-Use passthrough, WebSocket continuation, voice bypass, disable/uninstall, and real savings/cached-token telemetry.

- [x] T137 - 2026-05-13: Integration command split cleanup landed. `hook install codex` is hook-only in the sense that it never writes Codex base URLs; it now enables only the required `hooks=true` feature flag for official hooks. `hook verify codex` checks hook artifacts only and points config-patch users to `integrate status --client codex`; `integrate` remains explicit legacy/config-patch mode; docs/help present transparent proxy mode as the default non-mutating Codex path. `go run ./scripts/ci` passes 8/8 with 100% coverage. Detail: `docs/todo/t137-integration-command-split-cleanup.md`
- [x] T133 - 2026-05-13: Transparent daemon/TUI control plane landed. Setup now starts with transparent CA+daemon install and arm steps, Codex/Claude hooks are legacy fallback steps, Dashboard can arm/disarm transparent mode, Setup exposes `[a]` arm/disarm and `[u]` uninstall, status covers CA/trust/autostart/system-proxy/daemon-reachable states from a cached snapshot, and TUI actions reuse `proxy install|enable|disable|uninstall` command plumbing. Live Codex/App UX proof remains T140. Detail: `docs/todo/t133-transparent-tui-daemon-control-plane.md`
- [x] T134 - 2026-05-13: Runtime flight recorder and savings truth landed. Request summaries now hydrate normalized `flight` records; recorder redacts secrets and local paths before memory/disk persistence; proxy, CONNECT/MITM/raw passthrough/WebSocket, hook_pre, hook_post, and readhook paths emit route/layer/token/cache/error metadata; OpenAI/Codex `cached_tokens` and Anthropic cache usage are separated from estimates; `debug flight last|tail|replay|export` supports JSON/CSV; TUI Debug shows the same flight records. Live Codex/App corpus proof remains T140/T118b. Detail: `docs/todo/t134-runtime-flight-recorder-savings-truth.md`
- [x] T135 - 2026-05-13: Codex hook contract max-out landed for local/provable scope. `hook install codex` now installs SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, and Stop scripts; enables only `hooks=true`; verifies each artifact; keeps `updatedInput` disabled as parsed-fail-open; PostToolUse replacement feedback remains available only as explicit `SLIMFERENCE_CODEX_HOOK_MODE=compact`/`aggressive` opt-in; PermissionRequest maps Layer-0 deny/ask policy. Live event-delivery proof remains T140. Detail: `docs/todo/t135-codex-hook-contract-max-out.md`
- [x] T136 - 2026-05-13: OpenAI prompt-cache modernization landed for safe local scope. OpenAI/Codex cached-token usage is parsed into flight/cache telemetry; provider capabilities distinguish cache usage/key/retention and previous-response billing; optional `[proxy.openai_prompt_cache]` injects hashed `prompt_cache_key` and model-gated `prompt_cache_retention` only for generic OpenAI API requests with one retry without hints on provider rejection. CodexChatGPT cache-key injection and WebSocket continuation remain T140 live-proof items. Detail: `docs/todo/t136-openai-codex-prompt-cache-modernization.md`
- [x] T138 - 2026-05-14: Session/turn ownership spine for AST and cross-tool state is complete for the safe local scope: bounded in-memory turn state, file-backed Codex hook state, T125 edit/read gates, archive-backed `expand-body`, T126 same-turn git path-list elision, TUI Debug hook-state card, shared `sessions.SafeSessionID`, shared safe turn ids, read-cache `current_turn_id`/`last_turn_id`, tool-archive turn provenance, repetition `first_turn_id`/`last_turn_id`, and turn-aware quality observer APIs. Unknown turn ids degrade to previous behavior; no compression decision becomes more aggressive from metadata alone. Detail: `docs/todo/t138-session-turn-ownership-spine.md`
- [x] T139 - 2026-05-13: TLS proof hardening landed for local scope. `internal/tlsproof` stores JSONL proof records under `~/.slimference/tls-proofs/`; `tls-probe` now supports reflected HTTPS proof attempts, JSON, save, and compare modes while still using `internal/tlsdial`; alias targets are explicit; `doctor` reports catalogue age plus latest reflected proof status. HTTP/2 reflector negotiation is marked unproven instead of faked. Live provider-edge proof still requires an operator-chosen reflector run. Detail: `docs/todo/t139-tls-provider-edge-proof.md`
- [x] T141 - 2026-05-13/14: Output-reduce auto-tuning landed and was tightened after live Codex CLI evidence. Profiles are tiered (`off`, `mild`, `standard`, `aggressive`, `codex_aggressive`, `custom`), task-shape detection feeds shape-specific guardrails, analytics/flight records carry task-shape metadata, the tracker can auto-downgrade provider/model/task-shape buckets on failure or overhead signals, and explicit read-only/audit prompts now classify as `read_only_analysis` instead of edit/new-file shapes. `gain --output` stays baseline-honest: observed output only, no fake saving claim. Detail: `docs/todo/t141-output-token-auto-tuning.md`
- [~] T140 - Codex CLI/App live E2E certification and real corpus: CLI-only split proof is certified for the current Codex CLI without arming macOS System-HTTPS-Proxy (`proxy env codex --proxied`, per-process `slimference-codex` custom provider, `supports_websockets=false`, App remains direct). Latest 2026-05-14 exact-reply smoke returned `SLIMFERENCE_CLI_MAX_OK` through `/backend-api/codex/responses` with provider-reported input/cache/output accounting and all macOS Network services still `off`; the full local 8-step CI passes including 100% coverage, Codex smoke gate, synthetic live-corpus gate, and leaf-audit gate. Codex CLI tool-output Layer 0 no longer depends on PostToolUse hook delivery: the proxy now resolves Codex `function_call`/`function_call_output`, local-shell call/output, direct `command`/`args` arrays, `cmdline`/`shell_command` aliases, read path aliases, `aggregated_output`, `stdout`, and `stderr` shapes, unwraps Codex exec envelopes, and runs the existing captured-output filter bank directly on the `/backend-api/codex/responses` path, accepting only token-decreasing rewrites. Default WebSocket tunnel works byte-for-byte; the old `direct_codex_websocket_policy = "force_https_fallback"` path remains only as fallback proof mode. Live CLI smoke, CLI tool-loop, and larger read-only CLI repo-audit probe passed through the zstd HTTP pipeline with provider-reported input/cache/output accounting. Output-reduce now correctly bypasses German/English exact-reply prompts even when the input is large, preventing negative directive overhead on CLI smokes. `gain --proxy` and `savings` now report provider-only decision-log flight accounting for real proxied Codex traffic without local hook/readhook inflation. Remaining proof: Codex App/system-proxy E2E, interactive hook delivery, Browser-Use passthrough, voice/WebRTC bypass, disable/uninstall, scrubbed corpus savings, and WebSocket frame-shape/compression feasibility. Detail: `docs/todo/t140-codex-live-e2e-certification.md`

### Phase AB - Frontier compression engineering (P0/P1)

Phase AB is the maximum-upside track after the current Codex CLI/App path is certified. The goal is not "more knobs"; it is a controlled optimizer that turns every provider-supported reuse mechanism, every deterministic compaction lever, and every safe LLM summarization opportunity into measured savings without quality loss. All tasks below are gated by T146 live-corpus evidence and by T149 planner/safety decisions before default-on.

Required order: T146 baseline corpus expansion -> T142 inspect-only WebSocket frame corpus -> T149 planner spine -> T155 total-cost control plane -> T143/T144/T145/T148/T147 in parallel-safe slices -> T142 mutation mode only after frame-shape proof. WebSocket mutation is never enabled by default from static assumptions.

- [~] T155 - Total cost and usage maxxing control plane: 2026-05-15 user-directed P0 umbrella for end-to-end cost/usage optimization beyond input-only savings. Owns mechanism attribution, logical layer naming, deterministic context compiler, output-token governor, cache/state reuse, reversible tool-prune, live evidence gates, and default-on safety across T143/T145/T148/T149/T151/T153/T154. Detail: `docs/todo/t155-total-cost-and-usage-maxxing-control-plane.md`

- [ ] T142 - Codex WebSocket message-boundary compression: T142a inspect-only foundation and T142b shadow estimator landed, and T149g added the non-mutating shape registry. `internal/wscompact` parses RFC 6455 frames without mutation, reassembles fragmented text for redacted shape summaries, marks RSV/compressed frames as blockers, reports JSON `json_compact` would-save bytes/tokens, and `WebSocketTunnel` can attach the inspector while preserving byte-for-byte default tunnel behavior. Remaining: live Codex frame corpus and mutation mode. Detail: `docs/todo/t142-websocket-message-boundary-compression.md`
- [ ] T143 - Layer 1 semantic deterministic compaction frontier: T143a reversible path dictionary, T143b/T143e multi-language structure extraction, T143c local-tokenizer structure budget gate, and T143d semantic stacktrace/test-failure compaction landed. Remaining: provider-context-specific budgets, config/schema deltas, and quality/live-corpus gates. Detail: `docs/todo/t143-l1-semantic-deterministic-frontier.md`
- [ ] T144 - Layer 2 adaptive summarization accelerator: adaptive ROI gating, T144a task-shaped prompt contracts, path-hallucination validation, deterministic outbound pre-processing, T152 async background summaries, T153 hierarchical capsules, MiniMax failure fallback, and doctor/TUI provider policy status are landed. Remaining: live-corpus quality/default-on proof. Detail: `docs/todo/t144-l2-adaptive-summarization-accelerator.md`
- [ ] T145 - Layer 3 provider-cache and state-reuse maximizer: OpenAI prompt-cache stable-prefix planning, `prompt_cache_key` rotation, retention, HTTP `previous_response_id`, Anthropic cache-control breakpoints, provider-reported cached-token accounting, and `gain --proxy` stable-prefix heat maps landed without fake savings claims. Remaining: WebSocket response-ID proof after T142 and 30+ turn live-corpus hit-rate proof. Detail: `docs/todo/t145-l3-cache-state-reuse-maximizer.md`
- [ ] T146 - Real live corpus maximal evidence program: T146a/T146b/T146c/T146d landed. `scripts/verify -mode live-corpus-plan` prints the operator capture/export/metadata/gate runbook, and `benchmark-corpus` now reports/gates evidence level, output tokens, provider-cache read/create/cached tokens, output-reduce hits, errors, latency p95, planner planned-vs-actual replay signals, observed layer-combination matrices, and failable scenario validators. Remaining: actual operator Codex/App captures and true alternate-run layer-combination replay/A-B harness. Detail: `docs/todo/t146-real-live-corpus-maximal-evidence.md`
- [ ] T147 - Layer 0 real-traffic parser frontier: frontend, Python, SQL/DB, package-manager resolver-error, SQL-shell table, JVM/mobile, Docker/Kubernetes/Helm, monorepo diagnostic slices, runtime Layer-0 hit/miss telemetry, local-tokenizer `filter.db` accounting, and `gain --by-parser` family mapping landed. Covered locally: Next/Vite/Vitest/Jest/Playwright/ESLint/Biome/Oxlint/Turbo/Nx/Lerna/Bun, ruff/pylint/flake8/mypy/pyright/pytest/unittest matching, psql/sqlite/mysql/mariadb/Prisma/Drizzle/SQLFluff/Sqruff matching, npm/pnpm/yarn/bun/pip/uv install/update resolver summaries, psql/mysql/mariadb/sqlite table border compaction, Java/Kotlin/Swift/Dart/Flutter/PHP ecosystem diagnostics, and Docker/Podman/Nerdctl/Kubectl/OC/Helm diagnostics. Remaining: live-corpus quality/hit-rate proof; semantic DB explain summaries only if live corpus shows high-volume query-plan traffic. Detail: `docs/todo/t147-l0-parser-frontier-real-traffic.md`
- [ ] T148 - Output-reduce real-session aggressive autotuning: T148a repair-turn detection and T148b profile-row reporting landed. Output-reduce now detects "you skipped" / "explain more" / malformed-patch follow-ups, skips repair-followup turns as negative ROI, remembers the previous applied provider/model/profile/task bucket per session, feeds repair/user-reask signals into auto-downgrade, and `gain --output` reports provider/model/profile/task-shape rows without fake savings claims. Remaining: true A/B baselines, provider/model directive variants, task-shape quality validators, and live-corpus promotion proof. Detail: `docs/todo/t148-output-reduce-real-session-autotune.md`
- [x] T149 - 2026-05-15: Cross-layer compression planner and safety governor complete for local/provable scope. `internal/planner` deterministically chooses L0/L1/L2/L3/output/WebSocket actions from request facts, the proxy attaches content-free dry-run plans to upstream, local-cache, transparent CONNECT, and direct WebSocket flight/debug/TUI records, `slimference plan inspect` dry-runs planner facts or request files locally, corpus replay compares recorded planner actions against actual layer activity, T141 output-reduce cooldown feeds planner facts, and the HTTP hot path now uses planner actions to gate L0/L1/L2 behavior. T149g closed the remaining placeholder facts: session-owned edit state from hook turn files, live-corpus confidence from config/metadata, and WebSocket shape knowledge from an inspect-only registry. Detail: `docs/todo/t149-cross-layer-compression-planner.md`

### Phase AC - Ultimate low-drawdown savings stack (P0)

User-directed track from 2026-05-15. Scope includes every high-upside lever except repo-onboarding capsules, which were explicitly excluded. These tasks are allowed to build local/provable slices immediately, but default-on aggressive behavior still requires T146/T149 evidence and safety gates.

- [x] T150 - 2026-05-15: L3 stable-prefix cache planner shipped. OpenAI prompt-cache hints now gate on stable-prefix tokens, keys rotate on stable prefix/tool-schema changes, latest user text does not rotate keys, and flight/debug telemetry records content-free prompt-cache plan facts. Child of T145. Detail: `docs/todo/t150-l3-stable-prefix-cache-planner.md`
- [x] T151 - 2026-05-15: L4 tool-schema pruning maximizer shipped. Core tool classes stay attached, project-specific always-keep names are configurable, pruned schemas reattach by mention, missing-tool 4xx responses retry once with the full schema, miss/retry/cooldown telemetry lands in admin/debug/gain, and failed buckets disable future pruning. Child of T103. Detail: `docs/todo/t151-l4-tool-schema-pruning-maximizer.md`
- [x] T152 - 2026-05-15: Async L2 background summary pipeline hardened. Background jobs now use ROI scoring, session candidate hashes, stale-job skip telemetry, and hash-validated apply, so MiniMax work stays off the hot path and stale summaries fail open. Child of T144. Detail: `docs/todo/t152-async-l2-background-summary-pipeline.md`
- [x] T153 - 2026-05-15: Hierarchical context capsules shipped. Added explicit micro/phase/session capsule schema, deterministic archive-backed builders, anchor-safe skipping, tier selectors, and expansion through existing `slimference expand` content-archive URIs. Child of T144/T76. Detail: `docs/todo/t153-hierarchical-context-capsules.md`
- [x] T154 - 2026-05-15: Read/File delta maximizer shipped. Proxy-visible file reads now archive observed content, collapse unchanged rereads to expandable references, render shorter changed-file deltas, and bypass the aggressive path for recent same-session edits or missing safety state. Child of T37/T125/T143. Detail: `docs/todo/t154-read-file-delta-maximizer.md`

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

---

## Phase F — Output-Token Reduction & Deterministic Maxx (planned 2026-05-16)

Token-Spar-Hebel jenseits L0-L4-Input-Kompression. Output-Tokens sind 3-5× teurer als Input — der bisher größte ungenutzte Hebel. Plus eine Reihe Input-side-Wins, die L0/L1/L2/L3 strukturell verstärken. Alle Tasks sind deterministisch, kein neuer LLM-Pfad, kein MiniMax. Reihenfolge unten = Implementierungspriorität (Hebel × Aufwand × Quality-Risiko).

### Top-3 Output-Reduction (P0, ship first; 20-40% Output-Token-Reduktion kumulativ, 0 Quality-Risk)

- [x] T165 - Stop-Sequence-Engineering: shipped 2026-05-16. `internal/outstop/` + injection in `handler.go` step 8.7; 100% coverage; env override `SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS=0`. Detail: `docs/todo/t165-output-stop-sequence-engineering.md`
- [x] T166 - Streaming Trailing-Commentary Cutter: shipped 2026-05-16. `internal/outstop/streamcut/` + `streamingRelayWithCutter`; closes upstream body on fire. 100% coverage; env `SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT=0`. Deviations: first ~15 bytes of opener reach client (server-side generation stop, not client byte-rewrite). Detail: `docs/todo/t166-streaming-trailing-commentary-cutter.md`
- [x] T167 - Streaming Repetition Detector: shipped 2026-05-16. Index + Rabin-Karp matcher + Rewrite in `internal/outstop/repdet/`; non-streaming Anthropic responses are now actually rewritten into `[unchanged: …]` markers via `passthroughAnthropicWithRepdet`. Streaming + OpenAI / Codex rewrite paths deferred (Deviations documented). 100% engine coverage; env `SLIMFERENCE_OUTPUT_REDUCE_REPDET=0`. Detail: `docs/todo/t167-streaming-repetition-detector.md`

### Input-side aggressive reclamation (P0-P1; 15-30% per long iterative session)

- [x] T170 - Stale-File-Read Aging: shipped 2026-05-16. `internal/staleread/AgeMessages` + wire in `handler.go` step 2.5. Default-on toggle `[compression.output_reduce] stale_read_aging_enabled`; env `SLIMFERENCE_INPUT_REDUCE_STALE_AGING=0` disables. Detail: `docs/todo/t170-stale-file-read-aging.md`
- [x] T174 - Multi-Turn Obsolete-Message Pruning: shipped 2026-05-16. `internal/staleread/PruneObsoleteReads` + wire in `handler.go` step 2.6. Default-on toggle `obsolete_read_prune_enabled`; env `SLIMFERENCE_INPUT_REDUCE_OBSOLETE_PRUNE=0` disables. Detail: `docs/todo/t174-multi-turn-anchor-obsolete-pruning.md`
- [ ] T171 - Tool-Argument Hashing: 15s TTL cache for deterministic repeat calls (git status, ls, pwd, …); short-circuit via PreToolUse. Detail: `docs/todo/t171-tool-argument-hashing-repeat-calls.md`
- [ ] T172 - Cross-Tool Dedup: detect overlapping content between adjacent tool_results; mark second occurrence. Detail: `docs/todo/t172-cross-tool-dedup-conversation-window.md`

### Codex-hook-leveraged (P1; modern hook surface)

- [ ] T175 - PreCompact Output-Aggression Coupling: extend t164 marker beyond sliding-window to also escalate output-side aggression (stop-seqs, t169 hint). Detail: `docs/todo/t175-precompact-coupling-output-aggression.md`
- [ ] T177 - PostToolUse Just-in-Time Awareness: 10-token reminder appended after significant compactions to prevent redundant re-runs. Detail: `docs/todo/t177-posttooluse-just-in-time-awareness.md`
- [ ] T178 - SessionStart Resume Aggressive Pruning: when source=resume, compress pre-resume history harder for the next 3 turns. Detail: `docs/todo/t178-sessionstart-resume-aggressive-pruning.md`
- [ ] T176 - **Speculative** Codex Custom Tools (slimference_dedup_read): register tools the agent prefers over Read; biggest potential, highest uncertainty. Detail: `docs/todo/t176-codex-custom-tools-dedup-read.md`

### Output-side polish (P2-P3; small but free)

- [ ] T168 - Streaming Markdown-Overhead Normalizer: collapse \n{3,} → \n\n, drop standalone --- rules. Detail: `docs/todo/t168-streaming-markdown-overhead-normalizer.md`
- [x] T169 - **Quality-gated** Be-Terse System-Prompt Hint: shipped 2026-05-16 default-off. `internal/beterse` + cohort-routed via T186 harness. Auto-rollback on 5pp failure-rate delta over 50+ samples. Detail: `docs/todo/t169-be-terse-system-prompt-hint-gated.md`
- [ ] T173 - System-Prompt Extractive Compression: apply internal/extract to system prompt itself; benefit stacks with prompt-cache. Detail: `docs/todo/t173-system-prompt-extractive-compression.md`
- [ ] T179 - In-flight JSON Canonicalize: drop whitespace in ```json fenced output blocks. Detail: `docs/todo/t179-inflight-json-canonicalize.md`
- [ ] T181 - Per-Tool Output Budget: inject per-tool max_tokens (ls=100, cat=500, apply_patch=50, …) on the assistant reply after each tool. Detail: `docs/todo/t181-per-tool-output-budget.md`

### Infrastructure & polish (P3; latency/distribution wins, no token-impact)

- [ ] T180 - SSE Chunk Coalescing: aggregate 1-3-token deltas into 5-20ms windows; pure latency win. Detail: `docs/todo/t180-sse-chunk-coalescing.md`
- [ ] T182 - Binary Split (slimference-hook): tiny hook-only binary (≤4 MB) for ≤10 ms cold-start hook latency. Depends on T156. Detail: `docs/todo/t182-binary-split-hook-client.md`

### Sequencing notes (Phase F)

- **T165 + T166 + T167** ship together as the "output-reduction quartet" — they share the streaming pipeline and stop-phrase registry.
- **T170 → T174** is the canonical aging order: aging first (lossless), then obsolete-pruning (drops content the model genuinely needs replaced).
- **T169 last** of the top-tier despite high leverage: requires Quality A/B harness which depends on T118-style live-corpus telemetry.
- **T176 speculative**: schedule only after T165-T174 land so we have a stable baseline to measure custom-tool adoption against.
- **T182** depends on T156 (Unix-socket daemon protocol) — sequence accordingly.

### Out of scope (Phase F)

- Embedding-based RAG retrieval (would require a model and breaks deterministic-only).
- Cross-process / cross-machine cache sharing (single-machine only for now).
- Anything that requires Codex contract changes beyond what 0.130 already exposes (until Codex 0.131+ ships).

## Phase G — Live ChatGPT-Sub WebSocket conversation interception (planned 2026-05-16)

The piece that connects everything else: Codex 0.130+ ChatGPT subscription auth ships its model conversations over `wss://chatgpt.com/backend-api/codex/responses`. The chatgpt-base URL is hardcoded in older Codex provider defaults, but current Codex also exposes process-local provider override hooks. Current product work supersedes the old global-Keychain-first idea: CLI uses the scoped WSS provider path; Desktop proof now prefers the process-local `CODEX_CLI_PATH` app-server shim from T246, with proxy/CA/Keychain/global transparent mode kept as explicit diagnostics or lab only.

User-visible goal (verbatim from the user):

> "Es muss sowieso eine Art Proxy haben, dass die TUI installieren / Status checken / entfernen kann, kann sagen für welche Anwendungen er laufen soll (Codex CLI / Codex Desktop / Claude Code), Statistiken zeigen. Browser-Use, Computer-Use, Mikrofon-Transcription bleibt untouched. Traffic an OpenAI muss von normalem Codex-Traffic ununterscheidbar sein."

Phase G builds on the existing T122/T123/T131/T133/T139 transparent-mode foundations (CODE-COMPLETE, LIVE-CERTIFICATION pending) and closes the gap to a live working install on Codex 0.130 + ChatGPT subscription on macOS.

### Tasks

- [ ] **T187** - Phase G epic: live ChatGPT-sub WebSocket conversation interception. Pulls T188-T196 into one shippable target. Detail: `docs/todo/t187-phase-g-live-chatgpt-sub-interception-epic.md`
- [ ] **T188** - Responses-API WebSocket conversation MITM wire: terminate `wss://chatgpt.com/backend-api/codex/responses`, decode frames via wscompact, route through Phase F handlers (T165/T166/T167/T170/T174/T169/T183/T184/T185/T186), re-encode to real upstream. Bypass-on-schema-drift fail-open. Detail: `docs/todo/t188-responses-websocket-mitm-wire.md`
- [ ] **T189** - Smart SNI + path router: per-domain, per-path, per-app routing decisions. Codex conversation → MITM; voice / computer-use / image-gen / plugin / memories / browser → transparent TCP-bridge passthrough. DoH-backed upstream resolver. Per-app toggle. Detail: `docs/todo/t189-smart-sni-path-router.md`
- [ ] **T190** - Indistinguishability live audit: capture Codex 0.130 baseline traffic, diff against ours. Refresh uTLS profile catalog with `codex_cli_rs_0_130` and `codex_desktop_app_<ver>` profiles. HTTP/2 SETTINGS frame parity. WebSocket extension list parity. Header order parity. Timing budget hold. Golden file under `research/indist/`. Detail: `docs/todo/t190-indistinguishability-live-audit.md`
- [!] **T191** - SUPERSEDED by T239/T245. The old setup wizard with per-app
  install/toggle UI is not the product target. Current UX is Launch Center plus
  one unified Codex install/repair flow and capability-gated Desktop status.
  Detail: `docs/todo/t191-tui-setup-wizard-v2.md`
- [ ] **T192** - Stats Dashboard v2: overview tile + detail screen + period filter (today/week/month/all). Per-app, per-mechanism, per-cohort breakdown. Cost estimation via per-provider pricing table. Detail: `docs/todo/t192-stats-dashboard-v2.md`
- [!] **T193** - SUPERSEDED by T239/T245. Independent per-app toggles are not
  the normal product UX; launch path and capability state replace them. Detail:
  `docs/todo/t193-per-app-activation-state-machine.md`
- [ ] **T194** - Codex Desktop sideband bypass certification: explicit inventory of every Codex Desktop App endpoint family, captured corpus, replay-test, runtime safety guard. Voice / computer-use / image-gen / plugin / memories / browser must never reach our MITM path. Detail: `docs/todo/t194-codex-desktop-sideband-bypass-certification.md`
- [ ] **T195** - Resource footprint budget: RSS ≤ 200 MB hard ceiling, p95 added latency ≤ 25 ms, ≤ 0.5 % idle CPU. Benchmark suite + telemetry + auto-degradation policy. Detail: `docs/todo/t195-resource-footprint-budget.md`
- [ ] **T196** - Full reversibility audit: every install step has clean uninstall. Snapshot-diff E2E test. Atomic rollback on partial-install failure. Detail: `docs/todo/t196-full-reversibility-audit-mitm.md`

### Sequencing

1. **T190 first (preconditions)** — capture Codex 0.130 baseline traffic so all later work knows the target wire shape. Without this, T188 / T189 are speculative.
2. **T188 + T189 in parallel** — the wire and the router. Both implement against the captured corpus from T190.
3. **T194** — runs continuously with T188/T189 to catch routing regressions before they ship.
4. **T191 + T193** — TUI surface; can be developed against the existing daemon while wire matures.
5. **T192** — stats; depends on T191 admin endpoints.
6. **T195 + T196** — last-mile guards; run before any release.
7. **T187** — the epic; closed when all of the above pass live verification on a fresh Mac.

### Existing related (CODE-COMPLETE / LIVE-CERTIFICATION pending)

- T122 transparent-mode foundations
- T123 TLS fingerprint mimicry
- T131 transparent runtime closure
- T133 TUI daemon control plane v1
- T139 TLS provider-edge proof
- T140 (open: live Codex/App proof)

Phase G does NOT redo those; it builds on them.

### Constraints (load-bearing)

- **Indistinguishability**: outbound traffic to OpenAI must look identical to normal Codex traffic at TLS / HTTP/2 / WebSocket / header / timing / body layers. Verified via captured-baseline diff harness.
- **Sideband bypass**: voice / realtime / computer-use / image-gen / plugin / memories / browser traffic must never touch the compression pipeline. Verified via per-endpoint replay tests + runtime guard.
- **Reversibility**: every install step undoable to byte-equal pre-state. Verified via snapshot-diff E2E.
- **Footprint**: ≤ 200 MB RSS, ≤ 25 ms p95 added latency, ≤ 0.5 % idle CPU. Verified via benchmark suite.
- **Fail-open**: any wire-shape parse failure downgrades the affected session to pure tunnel. Never block traffic.
- **Per-app toggle**: independent Codex CLI / Codex Desktop App / Claude Code state. Default Codex CLI + Desktop App ON, Claude Code OFF.

### Out of scope (Phase G)

- Linux / Windows port (macOS arm64 only for v1).
- iPhone/iPad Codex app (out of scope - mobile traffic is not interceptable from a Mac).
- Anything that requires kernel extensions or system-extension capability (only userspace + standard `security`/`launchctl`/`/etc/hosts` mechanisms).
- Distribution / signing / notarization of the Slimference binary itself (existing release process covers).

## Phase G Wire-Up — connect the new packages to the live daemon + TUI (shipped 2026-05-17)

Phase G's self-contained packages are now wired into the daemon, admin state,
TUI, and Phase H install surface. The code-side proof stack is green:
`go run ./scripts/ci` passes all 8 steps and the current formal aggregate
coverage gate reports 99.7% against a 99.5% threshold. The only remaining
P0 gap is T209 live Codex CLI certification, which must happen from a
non-Codex shell rather than this active Codex session.

### Tasks

- [x] **T197/T207** TUI wiring to Phase G / Phase H packages —
  superseded and completed by the Phase H visible-surface collapse:
  Apps view, per-app toggle, arm/disarm dashboard tile, setup actions
  routed through top-level `install/enable/disable/uninstall`, and
  stats/state surfaced through `/admin/state`. Detail:
  `docs/todo/t197-tui-wire-phase-g.md` and
  `docs/todo/t207-phase-h-tui-legacy-collapse.md`
- [x] **T198** (2026-05-16) — `scripts/utils/indist_probe/` operator
  tool with three subcommands: `capture` (wraps tshark, filters by
  host+port, parses JSON output into `indist.Capture`), `diff` (calls
  `indist.Diff()` → exit 1 on drift, 0 on indistinguishable),
  `lock-golden` (copies to `research/indist/<target>/baseline.json`).
  Parser handles JA3, JA4, SNI, ALPN, cipher/extension/curve lists,
  GREASE detection, HTTP/2 pseudo-header order. Tests cover the
  parser without requiring tshark at test-time.
- [x] **T199 Phase A+B+C1** (2026-05-16) — Proxy accessors
  (`SetAppsManager`, `AppsManager`, `OutputReduceCountersSnapshot`,
  `SetStateProvider`); `/admin/state` + `/admin/apps` endpoints;
  `SavingsProbe` + `NoopIndistProbe`; `cmd/slimference` startup
  builds `apps.Manager` at `~/.config/slimference/apps.toml` and wires the
  full probe set; SIGHUP reloads apps policy; `transparent.Engine`
  with byte-equal `PhaseFDispatcher` bridge, gated behind new
  `cfg.Transparent.SNIPeekMode` + `SNIPeekPort` (default 8443, off).
  All tests + race-clean.
- [x] **T208 WSS Phase-F Mutation** (2026-05-17) — Codex WSS
  MITMConversation frames now run through a real Phase-F adapter:
  request envelopes apply stale-read aging, obsolete-read prune,
  stop-sequence injection, and be-terse where the existing gates allow;
  response deltas/completions apply streamcut + repdet. Unknown frames
  remain byte-equal fail-open. `/_slimference/admin/state` `.wss` exposes engine,
  bridge, degraded, forwarded, and re-encoded counters. Detail:
  `docs/todo/t208-wss-phase-f-mutation-adapter.md`
- [x] **MiniMax cleanup in TUI** (2026-05-16) — user-facing labels
  replaced with neutral "Layer 2 semantic" wording.
  `GetMiniMaxTrustClass` removed from `ProxyConfigInterface`. Backing
  analytics fields stay (`MiniMaxCalls`, `MiniMaxAvgLatencyMs`) until
  a broader Layer 2 refactor.
- [x] **TUI Phase H expansion** (2026-05-16) — `ViewApps` (key `a`)
  shows per-app routing with space-toggle. New ARM/DISARM tile on
  main dashboard left panel. Apps tab visible in `renderViewTabs`.
  Footer key-hint expanded to `[a] apps [s] stats [i] setup [b]
  bypass [q] quit`. Original Phase H Quick-Start promoted
  `install/enable/disable/uninstall/status`; T220 later changed the
  product default to scoped Codex routing through
  `slimference codex run|enable|disable|status`. `ProxyInterface` gained `AppEntries()`
  + `SetAppEnabled()`; remote adapter POSTs `/admin/apps`; in-proc
  proxyAdapter goes directly through `apps.Manager`.
- [x] **`slimference cert-trust` subcommand** (2026-05-16) — guides
  the one interactive macOS-Keychain step `slimference install`
  cannot automate. Auto-opens Keychain Access on the cert file +
  prints the sudo one-liner alternative.
- [x] **Phase H live-smoke-test** (2026-05-16) — verified end-to-end
  against real Codex 0.130 binary: install → 11 hook scripts +
  hooks.json populated with 8 events (PermissionRequest, PostCompact,
  PostToolUse, PreCompact, PreToolUse, SessionStart, Stop,
  UserPromptSubmit) + config.toml [features].hooks=true ; daemon
  starts + `/admin/state` returns full SetupState ; `enable` writes
  config + SIGHUPs daemon ; clean shutdown reverts hosts (fail-open
  works even without root). Uninstall round-trip restores byte-equal
  state modulo CA rotated aside. CLI `--no-autostart` / `--no-hooks`
  bugfix: `runUninstallCmd` now propagates these flags so symmetric
  install/uninstall is honoured.
- [x] **Release hygiene proof** (2026-05-17) — final pre-live stack
  verified locally: `go run ./scripts/ci` passes all 8 steps. After
  T220 the formal gate is a pragmatic 99.5% aggregate statement
  threshold; the current run reports 99.7% total. No live arm or real
  Codex traffic was run.
- [x] **T217 Codex-only product lock / Claude parked** (2026-05-17)
  — product commands now refuse or no-op every Claude activation path:
  `install --with-claude`, `uninstall --with-claude`,
  `hook install/remove claude`, `integrate --client=claude`,
  `readhook claude`, `/admin/apps claude_code=true`, apps policy reload
  with stale `claude_code=true`, and Anthropic SNI routing. Claude code
  remains in-tree but is not installed, removed, toggled on, or routed.
  Detached daemon SIGHUP now reloads without exiting, and explicit
  hosts/SNI armed-state tracking prevents no-op cleanups from blocking
  a later `enable`.
  Detail: `docs/todo/t217-codex-only-product-lock-claude-parked.md`
- [x] **T218 Single-binary stripped local build default** (2026-05-17)
  — Slimference stays one binary, no split. Local builds now use
  `go run ./scripts/build`, which wraps `go build -trimpath -ldflags
  "-s -w"` and optionally syncs to `~/.local/bin/slimference`; docs and
  spec build snippets point at the stripped path. Detail:
  `docs/todo/t218-single-binary-stripped-local-build-default.md`

### Sequencing (revised 2026-05-17 — scoped Codex takes precedence)

Phase H delivered the install/control plane and transparent.Engine.
T220 now supersedes the global-hosts default for product use: Browser
ChatGPT and ChatGPT.app must stay direct. The immediate P0 proof is
T209 scoped Codex CLI traffic through `slimference codex run -- <prompt>`.
Desktop interception is a separate scoped proof before any product claim.

1. **Scoped CLI first** (T209) — no hosts, no pfctl, no Keychain trust.
2. **Scoped Desktop proof** (T220) — prove or reject a non-global
   Codex.app launcher/config path.
3. **Global lab** — only with explicit
   `root-arm --global-chatgpt-hosts`.
4. **T198 tshark probe** — operator audit tool, orthogonal and shipped.

### Out of scope for Phase G Wire-Up

- Live capture of HTTP/2 SETTINGS or WS Upgrade headers - tshark
  passive trace cannot decrypt TLS without keys. Documented in T198
  Deviations; TLS-layer indist subset (JA3/JA4/ALPN/cipher/extension/
  curve/GREASE) is sufficient for v1 audit.
- TUI rewrite. The existing 7 800 LOC stays; we extend.
- New Phase F mechanisms. Phase G is plumbing for what Phase F shipped.

## Phase H — Single Entry Point + 2-Surface Consolidation (shipped 2026-05-17, global path now lab-only)

### Why

The user's mandate (2026-05-16): **one install command, one uninstall
command, one README, fail-open if daemon down, fail-open on Codex
update**. Today there are four surfaces touching Codex
(Hooks + URL-redirect + HTTPS_PROXY + transparent SNI-MITM). Phase H
collapses to **two** (Hooks + transparent SNI-MITM) — zero technical
drawback because transparent SNI-MITM is the only universal path that
also catches Codex 0.130's hardcoded WSS conversation URL.

The other two surfaces (URL-redirect, HTTPS_PROXY) stay in tree as
documented advanced/legacy hooks, but **no default install, no TUI
affordance, no integration test** exercises them anymore. Test matrix
flips to the 2-surface model in one sweep (no transition period).

2026-05-17 correction: universal transparent SNI-MITM is technically
correct but not product-correct when Browser ChatGPT and ChatGPT.app
must stay direct. T220 therefore reclassifies root-arm as global lab
only and promotes the per-process Codex CLI runner for T209.

### Tasks

- [x] **T200** Phase H epic (planning done) — operative surface design,
  fail-open semantics, sequencing of T201-T204. Detail:
  `docs/todo/t200-phase-h-single-entry-point-epic.md`
- [x] **T201** (2026-05-16) — `internal/install` package with `Plan()`
  + `HostsPlan()` backed by reversibility.Plan; CLI subcommands
  `install / uninstall / enable / disable / status` with `--dry-run`
  + `--json`. Step wrappers under `internal/install/installsteps/`
  (KeychainTrust, HooksCodex, HooksClaude). Tests + fmt + vet clean.
- [x] **T202** (2026-05-16) — `applyHostsPatch` + `writePIDFile` +
  `reloadSNIPeekModeFromDisk` in `cmd/slimference/hosts_lifecycle.go`.
  Hosts patch armed at daemon start (if SNIPeekMode=true) and
  reverted on shutdown BEFORE SNI listener cancel. SIGHUP re-reads
  config and flips hosts on/off accordingly. `signalDaemonReload`
  in CLI uses PID file to send SIGHUP.
- [x] **T203** (2026-05-16) — `docs/install.md` SSOT with human TL;DR,
  agent-readable YAML spec, fail-open table, recovery instructions.
  Meta-test `docs/install_spec_test.go` parses the YAML block and
  asserts every named Step exists in `internal/install.Plan()`.
- [x] **T204** (2026-05-16) — `agents.md` §9 Verdrahtungs-Doktrin
  documenting the 2-surface architecture and forbidding legacy
  surfaces in defaults. Test matrix is Codex-first: Codex CLI /
  Codex Desktop are the default targets; Claude Code is retained as
  opt-in code only, not a default install or default live route.
  Legacy fields in `internal/config/defaults.go` annotated with
  `// Legacy:` comments.
- [x] **T205** (2026-05-17) — Codex-only Phase H default. Default
  `install.Plan()` installs `hooks.codex` + `notice.codex` only;
  `--with-claude` is now parked/no-op in the product command path.
  Default hosts are `chatgpt.com` + `api.openai.com`. Detail:
  `docs/todo/t205-codex-only-phase-h-default.md`
- [x] **T206** (2026-05-17) — Config path single source. Phase H
  commands now honor `SLIMFERENCE_CONFIG` first and otherwise write
  canonical XDG config (`~/.config/slimference/config.toml`) instead
  of drifting to legacy `~/.slimference/config.toml`; admin-port and
  SIGHUP reload use the same resolver. Detail:
  `docs/todo/t206-config-path-single-source.md`
- [x] **T207** (2026-05-17) — TUI visible-surface collapse. TUI setup
  and service adapter call top-level `install/enable/disable/uninstall`
  lifecycle commands; quick-start and setup copy no longer point at
  legacy `proxy` commands; Claude is rendered off/opt-in in the default
  UX. Detail: `docs/todo/t207-phase-h-tui-legacy-collapse.md`
- [x] **T208** (2026-05-17) — WSS Phase F mutation adapter. Codex WSS
  request frames now feed the existing Phase F input reducers, response
  deltas/completions run streamcut + repdet, and schema drift still
  degrades byte-equal. `/_slimference/admin/state` `.wss` distinguishes active engine,
  byte bridge, degraded sessions, forwarded frames, and re-encoded
  mutation frames. Detail:
  `docs/todo/t208-wss-phase-f-mutation-adapter.md`
- [!] **T209** Scoped Codex CLI certification without self-break —
  blocked until the user approves a real Codex CLI smoke outside this
  active Codex session. It must use `slimference codex run -- <prompt>`,
  not global `cert-trust` / `root-arm` / `enable`; Browser ChatGPT and
  ChatGPT.app must remain direct. Start from disarmed preflight state:
  admin `:8990` may be healthy, SNI `:8443` off, hosts inactive, Codex
  policy on, Claude policy off. Detail:
  `docs/todo/t209-live-codex-cli-certification-without-self-break.md`
- [x] **T210** (2026-05-17) — Legacy surface retirement audit. Remaining
  URL-redirect, env/proxy, system-proxy, config-patch, and debug-only
  paths are classified as keep-advanced, hide-from-default, deprecate,
  or remove-after-certification. Default help/docs now promote only
  Phase H commands; no legacy code was deleted. Detail:
  `docs/todo/t210-legacy-surface-retirement-audit.md`
- [x] **T211** RTK current delta audit + port queue — compared the
  current embedded RTK research snapshot against Slimference's Layer-0
  filter/rewrite/proxy surface. Result: parser catalog parity, Codex
  stronger through proxy/WSS, future Claude-only gaps isolated. Detail:
  `docs/todo/t211-rtk-current-delta-audit-port-queue.md`
- [x] **T212** Claude Code max hook mode, default-off — built the
  RTK-style Claude Code maximum path behind explicit opt-in:
  Bash rewrite + Read hook + default-off Claude PostToolUse
  `updatedToolOutput` replacement, metrics, and fail-open guards.
  Grep/Glob/LS stay future-only until real payload capture. Claude
  stays unarmed during Codex-first testing. Detail:
  `docs/todo/t212-claude-code-max-hook-mode-default-off.md`
- [x] **T213** Codex maximum tool-output extraction — squeezed the
  maximum possible savings out of Codex despite unsupported
  `updatedInput`: proxy-side Layer-0 adoption metrics, broader tool
  output shape support, WSS request-frame Layer-0 adoption, savings
  probe accounting, and a final Layer-2 incremental anchor-index
  hardening found during 100 % CI closure. Detail:
  `docs/todo/t213-codex-maximum-tool-output-extraction.md`
- [x] **T214** Explicit wrapper polish, advanced only — made
  `slimference filter` / `rewrite` excellent as the hook-internal and
  human-optional wrapper without promoting it to a third default
  integration surface; help/completion now only advertise implemented
  flags. Detail:
  `docs/todo/t214-explicit-wrapper-polish-advanced-only.md`
- [x] **T215** DoH fallback + live-arm preflight — reduced T209 live
  test risk by adding upstream resolver fallback and `status --preflight`
  DoH checks. Detail:
  `docs/todo/t215-doh-fallback-live-arm-preflight.md`
- [x] **T216** Claude toggle UX truth — removed the misleading
  impression that the Claude app toggle alone can route Anthropic
  traffic while the Codex-only hosts patch deliberately excludes
  `api.anthropic.com`. Detail:
  `docs/todo/t216-claude-toggle-ux-truth.md`
- [!] **T220** Scoped Codex routing without global ChatGPT hosts —
  new product constraint from 2026-05-17: Browser ChatGPT and
  ChatGPT.app must remain direct. `root-arm` is now explicit global
  lab-only (`--global-chatgpt-hosts` required). Scoped CLI uses
  `slimference codex run`; shared CLI/App routing uses
  `slimference codex enable|disable|status` with a marker-owned
  Codex provider block. Desktop App routing still needs live proof
  before any "Codex Desktop app intercepted" claim. Detail:
  `docs/todo/t220-scoped-codex-routing-without-global-hosts.md`
- [x] **T221** Scoped Codex WSS Phase-F mode — implementation path
  landed for explicit WSS certification mode: `codex run
  --transport=wss`, `codex enable --transport=wss`, scoped local WSS
  upgrades through `wsmitm.Session`, Phase-F frame mutation, debug
  route mode, and byte-equal fallback on schema drift. Default remains
  stable HTTP until version-bound T226 certification promotes `auto`.
  T224 remains the capture/diff gate for indistinguishability wording. Detail:
  `docs/todo/t221-scoped-codex-wss-phasef-mode.md`
- [x] **T222** Raw scoped WSS frontdoor — pre-live implementation
  landed on the existing `:8990` listener. Codex WSS upgrades are read
  before `net/http`, raw header order/casing/unknown fields are
  preserved with only Host/request-target normalization, then the
  stream enters the T208 Phase-F WSS adapter. `auto` now waits for the
  version-bound T226 local WSS cert before preferring WSS; T224 remains
  the indistinguishability capture gate. Detail:
  `docs/todo/t222-raw-scoped-wss-frontdoor.md`
- [ ] **T223** Scoped upstream fingerprint parity — first closure landed:
  scoped HTTP and scoped WSS upstream dials now share the profile-aware
  TLS resolver independent of global transparent mode, preventing silent
  Go stdlib TLS on the product path. Remaining: telemetry, route-specific
  ALPN tuning, and live capture baseline. Detail:
  `docs/todo/t223-scoped-upstream-fingerprint-parity.md`
- [ ] **T224** Scoped indistinguishability audit — pre-live runbook,
  ignored capture path, promotion criteria, and synthetic WSS parser
  smoke are prepared. Remaining work is the real native/scoped
  HTTP/scoped raw-WSS tshark capture and diff during T209. Detail:
  `docs/todo/t224-scoped-indistinguishability-audit.md`
- [ ] **T225** Codex Desktop scoped proof and launcher — provider/base-URL
  proof is partial and insufficient for current Desktop conversation routing.
  Remaining Desktop proof moved to T238 process-local proxy mode. Detail:
  `docs/todo/t225-codex-desktop-scoped-proof-and-launcher.md`
- [x] **T226** WSS-first auto promotion — local WSS certification state now
  promotes `auto` to WSS for the current Codex/Slimference version tuple.
  Live scoped Codex CLI WSS proof issued the cert after real Phase-F mutation
  (`frames_reencoded=1`, `compressed_messages_mutated=1`,
  `parse_failures=0`, `degraded_sessions=0`), auto-WSS survived daemon restart,
  and Codex-version drift is safely detected. T243 upgrades the fallback
  ordering from HTTP-first to WSS-bridge-first. Detail:
  `docs/todo/t226-wss-first-auto-promotion.md`
- [~] **T227** Codex UX collapse — top-level `slimference enable|disable`
  now operate on scoped Codex route; former global SNI mode is fenced under
  `slimference lab enable|disable`; TUI Setup now separates Codex Mode from
  global lab controls. Remaining work: Desktop observation warnings after
  T238 proof. Detail: `docs/todo/t227-codex-ux-collapse.md`
- [ ] **T228** Codex Desktop zero-friction launcher — base-URL launcher
  shipped as a diagnostic/future-proof process-local spawn, but current
  Codex.app ignores it for conversation routing. T238 owns proxy launcher
  proof. Detail:
  `docs/todo/t228-codex-desktop-zero-friction-launcher.md`
- [ ] **T229** Codex hook hotpath socket — keep hooks as the Codex signal
  layer, but move hook execution to daemon socket RPC with fail-open
  fallback instead of repeatedly forking the full binary. Schedule after the
  WSS ladder and daemon lifecycle are stable unless hook latency becomes the
  measured UX bottleneck. Detail:
  `docs/todo/t229-codex-hook-hotpath-socket.md`
- [ ] **T230** Output-reduce v2 quality-gated max savings — expand the
  output-side savings layer with semantic repetition kill, tool-echo
  suppression, diff-aware budgeting, JSON/code canonicalization, and
  streaming early-cut, all behind quality gates and route-aware attribution.
  Detail:
  `docs/todo/t230-output-reduce-v2-quality-gated.md`
- [ ] **T231** M-series performance profile — deliberately defer performance
  optimization until the product path is otherwise release-ready; then profile
  real scoped Codex HTTP/WSS sessions on Apple Silicon before any
  SIMD/unsafe/build-flag work. Keep one stripped Go binary unless benchmarks
  prove otherwise.
  Detail: `docs/todo/t231-m-series-performance-profile.md`
- [~] **T232** Non-product surface governance — docs/help/TUI now separate
  product scoped Codex from lab/global MITM; normal enable no longer arms
  SNI-peek. Remaining work: T210 legacy retirement references. Detail:
  `docs/todo/t232-nonproduct-surface-governance.md`
- [x] **T233** Responses-safe stop-sequence injection — live T209 HTTP
  smoke proved Codex 0.130 rejects Chat-Completions `stop` on Responses API
  bodies. Fixed: `stop` injection remains for Chat Completions and skips
  Responses-shaped `input` bodies across HTTP and WSS. Live HTTP smoke now
  exits 0 with `stop_seq_injections=0`. Detail:
  `docs/todo/t233-responses-safe-stop-sequences.md`
- [x] **T234** WSS non-envelope text passthrough — Codex 0.130 scoped WSS now
  completes with legal non-envelope and RSV/compressed text frames forwarded
  byte-equal without `parse_failures` or `degraded_sessions`; malformed
  object-shaped envelopes still degrade fail-open. Detail:
  `docs/todo/t234-wss-non-envelope-text-passthrough.md`
- [x] **T235** WSS permessage-deflate Phase-F mutation — live scoped WSS now
  decodes negotiated `permessage-deflate`, mutates request-side Phase-F payloads,
  and re-encodes RSV1 frames without stripping extensions, poisoning context
  takeover, or promoting global lab routing. Two live Codex CLI WSS runs proved
  `frames_reencoded=1`, `phasef_mutations=1`, `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`, and request-side input savings
  above 1k tokens each. Detail:
  `docs/todo/t235-wss-permessage-deflate-phasef.md`
- [ ] **T236** WSS terminal-safe streamcut — WSS streamcut is deliberately
  disabled after live proof showed delta blanking can hang Codex CLI. Re-enable
  only after Slimference can emit a valid Codex WSS terminal sequence or prove a
  safer no-hang early-cut mechanism across two live runs. Detail:
  `docs/todo/t236-wss-terminal-safe-streamcut.md`
- [x] **T237** Codex provider display name — shipped as a cosmetic-only
  rename from `slimference-codex` to `Slimference` for user-facing provider
  labels. No routing, proxy, savings, or app-scope behavior changed. Detail:
  `docs/todo/t237-codex-provider-display-name.md`
- [x] **T238** Codex Desktop process-local proxy proof — live proof showed
  process-local proxy env plus Electron proxy args route both Codex.app's Rust
  app-server and Chromium NetworkService to Slimference loopback, and CONNECT
  reaches Slimference, but Codex.app closes before application bytes flow.
  `--with-ca-env` plus `CODEX_CA_CERTIFICATE` is insufficient for current
  Desktop savings on that proxy/TLS-MITM branch. The branch stays diagnostic;
  T246/T247 later proved the no-CA app-server shim as the current scoped Desktop
  path, so the proxy/TLS-MITM branch must stay non-product. Must not touch
  Browser ChatGPT, ChatGPT.app, Claude Code, `/etc/hosts`, pfctl, macOS system
  proxy, or `~/.codex/config.toml`. Detail:
  `docs/todo/t238-codex-desktop-process-local-proxy-proof.md`
- [~] **T239** Slimference launch center TUI — first implementation landed in
  the existing BubbleTea TUI: top-level Launch Center now exposes exactly
  Launch Codex CLI, Launch Codex App, Savings, Status, and Manage Slimference.
  No separate "open direct" action; direct mode is launching Codex normally
  outside Slimference. Default Install/Repair is unified for Codex and prepares
  CLI plus Desktop support together; no default CLI/App checkbox split. The TUI
  now launches Codex CLI in the current folder via `transport=auto` and blocks
  Codex.app launch while Desktop Slimference is not green; normal direct
  Desktop launch stays outside the TUI through Finder/Spotlight. Remaining
  work: embedded prompt input, richer Status / Manage detail,
  and final T240 live certification. Detail:
  `docs/todo/t239-slimference-launch-center-tui.md`
- [ ] **T240** Codex zero-drawdown release certification — final product seal
  after T238/T239/T241/T242/T243: prove CLI, Desktop, WSS-first fallback
  ladder, unified install/repair, Browser ChatGPT, ChatGPT.app, savings truth,
  CA-env/Keychain truth, and version-drift fallback as one reproducible macOS
  arm64 release ceremony.
  Detail:
  `docs/todo/t240-codex-zero-drawdown-release-certification.md`
- [x] **T241** Codex update-resilient certification — keep the strict WSS
  version-tuple guard, but make WSS Phase-F savings practically self-healing:
  shared `recertify wss` core, background auto-recert from Slimference-launched
  paths, TUI Repair CLI WSS, bounded recert logs, lock/backoff, delta-window
  proof evaluation, and bridge-proof fallback are landed. Live `codex-cli
  0.131.0` recert is green with Phase-F mutation and config hash stability.
  Detail: `docs/todo/t241-codex-update-resilient-certification.md`
- [x] **T242** Codex Desktop root-store and proxy compatibility matrix —
  process-local `--with-ca-env` probe and launch are partially live-verified
  and now automated behind `codex desktop prove --manual` and `--finish`: the
  detached Desktop launcher is stable, existing app instances are refused before
  scoped launch, daemon-wide WSS counters are never treated as Desktop proof,
  and the prompt-driven live proof now ends as `desktop_ca_env_rejected` /
  `tls_trust_rejected` with `mitm_bridged=14` but zero application bytes and
  zero mutation. Follow-up Electron proxy args now remove the Chromium direct
  socket bypass, but live prompt proof still ends as one CONNECT/MITM session
  with `bytes_c2s=0`, `bytes_s2c=0`, and zero mutation. Current product
  decision: TUI Launch Codex App blocks instead of opening direct or a broken
  proxy; normal Finder/Spotlight Desktop stays direct. T246 records the
  no-CA/no-proxy app-server shim route found during the endpoint-hook audit and
  its final current-build blocker. Detail:
  `docs/todo/t242-codex-desktop-root-store-probe.md`
- [~] **T243** WSS-first auto transport ladder — `transport=auto` now prefers
  `wss_phasef`, then WSS byte-equal bridge, then HTTP, then direct for scoped
  Codex CLI. WSS remains the standard; HTTP is only fallback after WSS bridge is
  unsafe. Live certified-tuple and recert-restore proofs are green; remaining
  work is fallback-branch live proof and non-CLI passthrough audit. Detail:
  `docs/todo/t243-wss-first-auto-transport-ladder.md`
- [x] **T244** Daemon lifecycle and atomic install hardening — atomic
  `scripts/build --install` replacement landed after a macOS `dyld_start`
  hang from in-place binary overwrite. Install planning now rejects temporary
  `go run` executable paths unless the operator passes an explicit
  `--binary=PATH`, preventing hooks/launchd from pointing at deleted temp
  binaries on fresh machines. Direct daemon starts and service installs reject
  temporary Go build executables, stop reports the reboot-only macOS
  `U`/`dyld_start` class if SIGKILL cannot clear a process, and
  `scripts/build --restart` now runs stop -> build -> atomic install -> start.
  Manage Slimference now labels restart as the daemon repair path, install
  state is product-level for Codex CLI+Desktop, status/TUI report old stuck
  processes as reboot-only stale evidence, and live local restart evidence is
  recorded for T240.
  Detail:
  `docs/todo/t244-daemon-lifecycle-atomic-install.md`
- [ ] **T245** Desktop custom CA and macOS trust UX — keep unified install
  usable for Codex CLI and Desktop, but do not make CA env or Keychain trust a
  prerequisite for scoped CLI WSS. Desktop proxy proof tries process-local
  `CODEX_CA_CERTIFICATE` first; Keychain trust is only a Desktop/Lab fallback
  and must be guided, explicit, reversible, SSL-only, and capability-gated in
  Manage Slimference.
  Detail: `docs/todo/t245-macos-ca-trust-ux.md`
- [x] **T246** Codex Desktop app-server shim — CLOSED end-to-end.
  User-confirmed Desktop launch on 2026-05-23 (evening) via
  `slimference codex desktop prove --manual` + `--finish` returned
  `mode=desktop_app_server_route_proven`, `launch_ready=true`,
  `desktop_proven=true`, `phasef_bridged=2`, `compressed_messages_inspected=584`,
  zero parse/degrade/compression errors. TUI Launch Codex App is now
  launch-eligible (persisted at `~/.slimference/codex-desktop-proof.json`).
  Routing solved (`9dcf8f4`, stdin JSON-RPC mediator rewrites default
  `modelProvider` to `slimference-codex`), gate proven (`af972df`, lag-free
  `phasef_bridged` counter). Normal Finder/Spotlight Codex.app stays direct;
  voice/Browser/ChatGPT.app/computer-use/Claude untouched. A later T247 Desktop
  repeat-read proof on 2026-05-29 returned
  `desktop_app_server_phasef_proven` with `desktop_savings=true`, so the same
  route now has both route proof and mutation proof.
  Detail: `docs/todo/t246-codex-desktop-app-server-shim-proof.md`
- [x] **T247** Codex WSS Phase-F reducer efficacy (Responses-API delta model) —
  REDUCER CHAIN PROVEN END-TO-END on real Codex CLI and Desktop traffic. CLI:
  one Codex 0.133.0 3x35KB repeat-read session produced
  `frames_reencoded=3`, `compressed_messages_mutated=3`, `phasef_mutations=3`,
  zero parse/degrade/compression errors, and `savings.input_tokens_saved=26461`;
  Codex 0.135.0 drift was recertified and a 3x71KB CLI repeat-read saved 22620
  input tokens. Desktop: 2026-05-29 Codex.app app-server proof returned
  `desktop_app_server_phasef_proven`, `desktop_savings=true`,
  `frames_reencoded=3`, `compressed_messages_mutated=3`, `phasef_mutations=3`,
  and zero parse/degrade/compression errors on a 3x76KB repeat-read prompt.
  Verified shape: Codex
  tool name `exec_command`, arguments `{"command":["bash","-lc","cat <path>"],
  ...}`, function_call_output.output = Codex exec envelope; the entire mapping
  function_call -> remembered tool_use -> function_call_output -> tool_result ->
  commandLine -> readcache -> delta-marker mutation works without code change.
  Earlier "compressed_messages_mutated=0 on multi-read" reading was Codex-side
  run-variance on identical code path, not a reducer defect. Fixture-based
  regression test landed
  (`TestWSPhaseFRealCodexMultiReadProducesDeltaMarker`, ~0.10s, isolated
  via t.TempDir, synthetic payload, real exec_command shape with `cmd` as
  string). Follow-on optimization and longer-window real-workday aggregate
  measurement moved to T248.
  Detail: `docs/todo/t247-codex-wss-reducer-efficacy.md`
- [~] **T248** Unified Codex savings engine for WSS and HTTP — new P0 owner
  for maxing Codex savings without quality/context drawdowns. WSS remains the
  standard; HTTP remains fallback/legacy/useful hook surface. First slices
  implemented shared reducer-mechanism attribution for the existing Codex
  proxy-Layer-0 core so WSS and HTTP can report opportunity counters
  (tool-result blocks, unresolved tool-use references, command-resolved blocks,
  command-unresolved blocks, read-delta attempts, read-delta misses) separately
  from success counters (read-delta blocks, captured-output filter blocks,
  Codex exec-envelope blocks, modified blocks, and tokens saved). The shared
  `reduceCodexLayer0` entry point now serves HTTP and WSS with route labels, and
  `/admin/state` plus `aggregate-savings` expose route-specific HTTP vs
  WSS-Phase-F attribution. Shell command arrays are normalized before
  classification, and relative single-file reads now resolve against Codex
  `workdir`/`cwd` metadata before readcache evaluation. Single-text-part
  `output` / `content` arrays and nested MCP-style result content now
  reconstruct in place and fail open on ambiguous multi-text arrays. Additional
  WSS Phase-F fixtures now prove repeated-read mutation for `local_shell_call`,
  `shell_call`, direct `read_file`, and MCP-style `result.content` outputs.
  Large observed reads are archive-backed instead of bloating session JSON,
  recert state surfaces attempt/timing/log/error metadata, and
  `aggregate-savings` plus `workday-savings start|finish` now carry the current
  Codex route / auto-recert snapshot so workday windows record fallback/repair
  events as well as savings counters. Report hygiene now omits zero recert
  timestamps, uses the canonical `~/.slimference/filter.db` path, and keeps
  Desktop "WSS savings active" distinct from "WSS route ready" across the TUI /
  Launch Center gate. Planner L2/L3 on Codex WSS is proof-gated as `shadow`
  candidate only until separate fixture plus live proof exists. The planner now
  derives `repeated_tool_output` from repeated resolved tool command/read keys,
  so adaptive cache/L2 candidates are grounded in parsed request structure
  instead of manual planner facts. WSS request bodies now get content-free
  planner summaries in `decisions.jsonl`, including content classes, token
  deltas, previous-response state, output-reduce reason, and proof-gated L2/L3
  decisions. Safety hardening after external review replaced lossy changed-read
  diffs with position-aware hunks, feeds WSS-observed edits into the recent-edit
  guard, leaves terminal `response.completed` payloads byte-equal for repdet,
  fixes high-risk filter ordering/value-loss cases, and keeps Layer 2 disabled
  by default in generated config. Fresh 2026-05-30 CLI audits now prove distinct
  session ids across two new conversations and real WSS savings through
  `codex exec resume --last` (`tokens_saved=2815`,
  `compressed_messages_mutated=1`, zero parse/degrade/compression errors).
  A fresh Desktop launch through `slimference codex launch-desktop
  --replace-existing` also passed the same WSS savings gate on a three-turn
  repeated-read workload (`tokens_saved=3151` in the decisions window;
  admin/state total `input_tokens_saved=5966`, `compressed_messages_mutated=2`,
  zero parse/degrade/compression errors). The latest slice adds WSS re-read
  canary telemetry to request summaries and `wss-audit`, neutralizes
  model-facing readcache markers, prefers Codex turn metadata over
  `prompt_cache_key` for WSS session identity, makes operator cost estimates
  billable-input based, and adds exact archive-backed cross-turn dedup for
  repeated non-file tool outputs with dedicated route attribution. The latest
  T249-T255 slice adds Codex o200k token guards, content-derived archive IDs,
  bounded readcache session files, error-priority log/lint truncation, an
  eslint-json Tier-1 parser, FastCDC chunking primitives, a session chunk-dedup
  store, WSS reconnect tool-use persistence, and the core offline comprehension
  A/B comparison engine. Remaining work: future capture-driven tool variants,
  measured L2/L3 upgrades, replay wiring around the A/B engine, and broader real
  workload measurements before T240.
  Detail: `docs/todo/t248-unified-codex-savings-engine.md`
- [~] **T249** Codex comprehension safety net — foundation gate for all aggressive
  savings work. Offline comprehension A/B harness (compressed vs direct, model-facing
  context diff), neutral once-per-session recoverable-archive note so `local-archive://`
  loss becomes recoverable, re-read-after-collapse auto-restore via the existing canary,
  and a documented socket-lifecycle measurement. Core comparison engine is implemented;
  replay wiring/recovery note/auto-restore remain open. No direct savings; unlocks
  t253/t254/t255 to be enabled with data instead of hope.
  Detail: `docs/todo/t249-codex-comprehension-safety-net.md`
- [ ] **T250** Codex lossless cross-turn savings coverage — extend exact/position-aware
  savings to the still-uncovered ranged-read class (`sed`/`head`/`tail`/offset, keyed on
  `path+offset+limit`) and repeated search-output deltas. Lossless, low risk, lifts the
  savings floor on non-repeat-read sessions.
  Detail: `docs/todo/t250-codex-ranged-read-and-search-savings.md`
- [~] **T251** Codex savings stability + cross-turn resolution robustness — persist the
  socket-local toolUse resolution map (content-free, bounded, TTL) so reconnects do not go
  cold, in-memory readcache with async flush (remove per-read disk I/O latency),
  content-addressed archive IDs (collision fix), recency-adaptive aggressiveness,
  prompt-cache-aware mutation, bounded state. Tool-use persistence, archive-id collision
  hardening, and bounded readcache state are implemented; in-memory async flush and
  recency policy remain open. Protects and multiplies existing savings.
  Detail: `docs/todo/t251-codex-savings-stability-and-resolution.md`
- [~] **T252** Codex savings precision + filter/marker tweaks — quick wins: use
  `o200k_base` for Codex token guards (currently `cl100k_base`), fix the `delta.go`
  doubled-newline, make filter caps token-budget-aware + error-priority, add Tier-1
  parsers (eslint-json/tsc/kubectl-json/cargo-metadata/terraform-show-json), compact
  stderr, structured marker notation. o200k, delta newline, log/lint error-priority,
  and eslint-json are implemented; remaining parsers/search+terraform caps/stderr stay open.
  Detail: `docs/todo/t252-codex-savings-precision-and-filter-tweaks.md`
- [ ] **T253** Codex aggressive read compression (GATED by T249) — first-read AST/structure
  scan-mode compression (extends `codecompact`), predictive post-edit file state from the
  parsed `apply_patch`, reasoning-trace compaction (verify-first), apply_patch context
  dedup. High savings, highest drawdown; default-off until the T249 A/B harness proves no
  comprehension regression and the recovery note is live.
  Detail: `docs/todo/t253-codex-aggressive-read-compression.md`
- [ ] **T254** Codex server-state mirror (radical, TASK-SPLIT candidate, gated by T249) —
  maintain a precise mirror of server-side conversation state from forwarded bytes along
  the `previous_response_id` chain, and reduce every client frame to pure novelty against
  it. Generalizes read-delta/dedup/search-delta into one differential transport; savings
  grow with session length.
  Detail: `docs/todo/t254-codex-server-state-mirror.md`
- [~] **T255** Codex content-defined chunk dedup (radical, TASK-SPLIT candidate, gated by
  T249) — FastCDC rolling-hash chunking + session-scoped content-addressed chunk store to
  deduplicate PARTIAL overlap (file after small edit, similar files, shared-line logs)
  that whole-output dedup misses. rsync-for-LLM-context; references recoverable via the
  T249 contract. Chunker + safety-hardened session store are implemented as primitives;
  WSS wiring, decode/reinject, TTL/LRU, and A/B proof remain open.
  Detail: `docs/todo/t255-codex-content-defined-chunk-dedup.md`

### Codex savings v2 — full 24-item index + what the % mean (T249-T255)

What the % mean: the percentages below are ROUGH order-of-magnitude estimates, not
measurements. They denote additional billable-input-token reduction on a representative
multi-turn Codex coding session, ON TOP of today's state. Honest anchor: today the
system saves meaningfully only on repeat-read-heavy sessions (hit-rate ~4-9% of
requests), ~0 otherwise. So the base is low. Some items are MULTIPLIERS of effectiveness
(not additive); some are ENABLERS (no direct %, but they unlock other items). Each item
is tagged. Every item below is captured in a task detail file with exact acceptance
criteria; this index is the traceability map so nothing is lost.

| # | Item | Rough % | Type | Task | Status |
|---|------|---------|------|------|--------|
| 1 | Server-state-mirror / general differential transport | 15-40% on long sessions | Enabler + biggest lever | T254 | queued (gated by T249) |
| 2 | Content-defined chunk dedup (FastCDC) | 10-30% read/log-heavy | Radical | T255 | PARTIAL (chunker+store core landed; WSS/recovery gated) |
| 3 | Predictive post-edit file state | 5-15% | Innovative | T253 | queued (gated by T249) |
| 4 | Cross-turn non-file dedup | 10-25% | Lossless | **T248** | DONE (landed) |
| 5 | First-read AST/structure scan-mode compaction | 20-50% explore-heavy | High savings, high drawdown | T253 | queued (gated by T249) |
| 6 | Ranged/partial read caching | 5-15% | Lossless | T250 | queued |
| 7 | Search-output delta | 3-8% | Lossless | T250 | queued |
| 8 | Reasoning-trace compaction | 0-15% (verify first) | Uncertain | T253 | queued (verify-gated) |
| 9 | apply_patch context dedup | 3-10% | Lossless-ish | T253 | queued (gated by T249) |
| 10 | Resolvable-archive contract | enabler | Enabler | T249 | queued |
| 11 | Comprehension A/B harness | enabler | Enabler | T249 | PARTIAL (core engine landed; replay/CI pending) |
| 12 | Recency-adaptive aggressiveness | +5-10% and drawdown down | Double positive | T251 | queued |
| 13 | Re-read-after-collapse auto-restore | drawdown down | Drawdown fix | T249 | queued |
| 14 | o200k tokenizer for Codex (not cl100k) | +2-5% precision, all layers | Precision | T252 | DONE |
| 15 | toolUse-map disk persistence | multiplier (2-5x hit-rate) | Multiplier | T251 | DONE (core; live efficacy still to measure) |
| 16 | In-memory readcache + async flush | latency/stability | Stability | T251 | queued |
| 17 | Content-addressed archive IDs (collision fix) | correctness/stability | Stability | T251 | DONE (collision/idempotence fix; global content-only dedup not built) |
| 18 | Prompt-cache-aware mutation policy | avoids net-negative billing | Stability | T251 | verified safe-by-construction |
| 19 | Bounded session state (TTL/LRU) | stability | Stability | T251 | DONE for readcache/tooluse state |
| 20 | doubled-newline fix in delta.go | +1-2% changed-reads | Quick win | T252 | DONE |
| 21 | Filter caps token-aware + error-priority | +1-3% | Quick win + drawdown | T252 | PARTIAL (log+lint done; search/terraform open) |
| 22 | More Tier-1 parsers (eslint-json/tsc/kubectl-json/cargo-metadata/tf-show-json) | +2-5% | Quick win | T252 | PARTIAL (eslint-json done; rest queued) |
| 23 | stderr compaction (CLI path) | +1-3% | Quick win | T252 | queued |
| 24 | Marker structured notation | cleaner/parseable | Quick win + drawdown | T252 | queued |

Combined-leverage order: T249 first (safety net + recovery unlock the gated items),
then T250 + T252 (lossless + quick wins, low risk), then T251 (stability/multiplier),
then the gated big plays T253/T254/T255 once the A/B harness proves no comprehension
regression. Honest aggregate expectation with all lossless + gated-aggressive items and
the comprehension proof: roughly +30-60% more billable reduction on repeat-read-heavy
sessions, and a meaningfully higher FLOOR on sessions that save ~0 today (items 6, 7,
and the landed item 4 fire without repeat reads). Items 1 and 2 are the only ones that
break the savings-scales-with-session-length ceiling.

### Sequencing within Phase H

1. **T201 first** — the CLI subcommand IS the entry point. Without it,
   the rest of Phase H has nothing to expose.
2. **T202 alongside T201** — needed before `enable` is safe to run in
   production. The fail-open guarantee depends on shutdown-revert.
3. **T203 alongside T201+T202** — README written as the commands
   solidify, not afterwards. Final truth-table once T202 is done.
4. **T204 last** — only after T201/T202 are solid, flip the test
   matrix in one sweep.
5. **T215 before T209** — it lowers live-arm risk without touching the
   active Codex session.
6. **T213 before T209 where possible** — Codex maximum extraction must
   be fixture-proven before the live certification window.
7. **T211 before any RTK port claim** — no "RTK parity" statement is
   valid until the current research snapshot is compared command by
   command.
8. **T212/T216 are Claude-prep only** — implement and verify offline,
   but keep Claude off for the Codex-first live test unless the user
   explicitly changes scope.
9. **T214 after T213/T212 surfaces are stable** — wrapper polish should
   document and expose the final internal behavior, not pre-empt it.
10. **T220 before any live arm** — global hosts/pf routing cannot be
   the normal product path while Browser ChatGPT and ChatGPT.app must
   stay direct. T209 becomes scoped CLI first; Desktop app interception
   waits for scoped proof.
11. **T221 before WSS claims** — scoped WSS is allowed only after the
   existing T208 frame adapter runs on local provider WSS, not only
   global transparent WSS. T221 defines `auto|wss|http|direct`; T226 now
   makes `auto` prefer WSS only for certified Codex/Slimference tuples.
12. **T222 before "old invisibility" claims** — raw Upgrade preservation
   is required before scoped WSS can be compared fairly to the old
   transparent dispatcher.
13. **T223 before provider-side stealth claims** — scoped upstream dials
   must use profile-aware TLS/ALPN before any fingerprint discussion is
   serious.
14. **T224 before any "indistinguishable" wording** — capture/diff proof
   is the gate; architecture alone is not evidence. T224 is also the
   gate for making WSS-first the Codex CLI default.
15. **T225 after CLI proof** — Desktop follows only after scoped CLI/WSS
   is stable, because Desktop proof is more stateful and easier to
   confuse with Browser ChatGPT traffic.
16. **T226 after T235** — WSS-first auto promotion is valid only after live
   scoped raw-WSS Phase-F mutation proof for the current Codex/Slimference
   version tuple. T224 capture/diff remains the separate gate for
   indistinguishability wording, not for local auto transport selection.
17. **T228/T238 after T225 branch decision** — T228 already proved that
   base-URL env injection is process-local but insufficient for current
   Codex.app conversation routing. T238 is the next Desktop branch: prove or
   reject process-local proxy routing before any Desktop product claim.
18. **T239 after T238 branch decision** — the launch center can ship once the
   Desktop button truth is known: proven process-local proxy, custom-CA-env
   diagnostic, or honest blocked Desktop Slimference state. Direct Desktop mode
   remains outside the TUI through Finder/Spotlight. Do not design ambiguous
   Desktop states or per-app install checkboxes in the default flow.
19. **T240 after T239** — final release certification comes after the launch
   center exists, because the user-facing path itself must be what gets
   certified.
20. **T227 after T226/T238/T239 semantics are known** — collapse top-level UX
   only after the transport and Desktop truth are clear enough to avoid
   renaming confusion twice.
21. **T229 after scoped route is stable** — hook hotpath socket improves
   latency and signal quality, but it does not replace the provider route.
22. **T230 after transport baseline** — output-reduce v2 needs a stable
   Codex HTTP/WSS corpus so savings and quality can be measured honestly.
23. **T231 after live proof** — performance work must use pprof from real
   traffic; no M-series/SIMD/build-flag changes on vibes.
24. **T232 continuously** — every future surface must be classified as
   product, fallback, lab, or legacy before it enters docs/help/TUI.
25. **T233 before resuming T209** — no further scoped Codex HTTP/WSS live
   traffic until Responses-shaped bodies are proven to pass without `stop`.
26. **T234 before WSS certification** — scoped WSS can answer correctly while
   still losing all Phase-F value if one legal non-envelope text frame trips
   degraded mode. Non-mutatable WSS payloads must pass through without
   poisoning the session before T224 can fairly judge real mutation.
27. **T235 before T226** — T235 is now satisfied: scoped WSS preserved native
   `permessage-deflate`, re-encoded real mutated frames, and kept
   parser/degrade/compression errors at 0. T226 recorded version-matched
   certification through the product path and promotes `transport=auto` to WSS
   for that certified tuple.
28. **T236 before WSS streamcut** — the HTTP streamcut mechanism closes SSE and
   emits an SSE terminator. WSS cannot reuse that by blanking deltas: live Codex
   CLI hung. WSS streamcut stays off until a protocol-correct terminal sequence
   is captured, implemented, and live-certified.
29. **T241 before T243/T240** — Codex CLI updates must not silently lose WSS
   Phase-F value without an automatic repair path. The strict tuple guard stays;
   the UX gets shared CLI/TUI/background recert, bounded logs, locks, cooldowns,
   and real mutation proof.
30. **T246/T247 before Desktop success claims** — the Desktop menu item remains
   part of the TUI, but success is gated on bytes/WSS proof. The old
   process-local proxy plus `CODEX_CA_CERTIFICATE` branch is rejected for
   current Codex.app as a zero-byte TLS/root-store path. The no-CA
   `CODEX_CLI_PATH` app-server shim is the current product branch and has a
   2026-05-29 `desktop_app_server_phasef_proven` repeat-read proof with
   mutation counters. Normal direct Desktop launch stays outside Slimference.
31. **T243 before T240** — WSS remains the standard. `transport=auto` must try
   certified WSS Phase-F first, WSS byte-equal bridge second, HTTP third, and
   direct only as final fail-open. Version drift should trigger T241 auto-recert
   while staying on WSS bridge when bridge proof is clean.
32. **T245 before any Desktop/Lab installer claim** — scoped Codex CLI WSS
   does not need CA env or a macOS trusted CA. The TUI may expose
   process-local custom CA env for Desktop proof and may install/repair/remove
   Keychain trust only for Desktop fallback or global lab, with an explicit
   admin prompt and reversible cleanup. Do not block normal CLI WSS install,
   launch, or recert on CA/Keychain state.

### Acceptance for Phase H

- `slimference install` exits 0 → SetupState: daemon/autostart state known,
  hooks present where selected, hosts CLEAN (not patched), and Codex product
  support prepared for both CLI and Desktop without default per-app checkboxes.
  Scoped Codex CLI WSS is usable without CA env or macOS CA trust. Desktop
  support is prepared through the app-server shim and is savings-capable when
  the current proof remains `desktop_app_server_phasef_proven`. Keychain trust
  is optional and only required for Desktop/Lab TLS-MITM diagnostic branches
  that actually need OS trust.
- Scoped Codex CLI test uses `slimference codex run` and leaves
  hosts/pfctl/Browser ChatGPT/ChatGPT.app untouched.
- Scoped WSS mode uses `wsmitm.Session` + Phase-F frame mutation, handles
  negotiated `permessage-deflate`, and degrades unknown frame shapes to
  byte-equal bridge.
- The intended final Codex CLI default is `transport=auto` with WSS preferred
  after T224 proof; HTTP/direct remain fallback and comparison modes.
- Scoped WSS raw frontdoor preserves original Upgrade headers except
  unavoidable authority/path normalization.
- Scoped Codex upstream dials use configurable TLS/ALPN profiles even
  when global transparent mode is disabled.
- Indistinguishability claims require a T224 capture report; docs use
  "scoped / minimized drift" until then.
- Shared Codex CLI/App test uses the Launch Center paths: Launch Codex CLI via
  `transport=auto`; Launch Codex App via the T246 capability-gated branch; then
  verifies direct normal launches outside Slimference remain native.
- Codex Desktop target is scoped WSS-first when the stored app-server proof is
  current and mutation counters stay clean. Earlier app-server probes did not
  produce application bytes or WSS frames, but T246/T247 later closed that gap;
  normal Finder/Spotlight Codex.app remains direct.
- T226 done: `transport=auto` prefers WSS for certified Codex versions. T243
  supersedes the old fallback ordering so stale Phase-F certs prefer WSS
  byte-equal bridge before HTTP/direct when bridge proof is clean.
- After T227, normal `slimference enable|disable|status` means scoped Codex
  route; global transparent MITM is lab-only and cannot be triggered by
  product default UX.
- After T228, Codex Desktop either reloads the shared scoped route with a
  clean one-command flow or has a process-local launcher that is proof-gated
  before any savings claim.
- After T238/T242, the process-local proxy/CA branch is a rejected diagnostic
  path for current Codex.app. After T246/T247, the app-server shim branch is the
  supported Desktop Slimference path when proof-gated green; it uses the same
  scoped WSS Phase-F route as CLI and does not require CA/MITM.
- After T239, the normal human surface is the launch center: Launch Codex CLI,
  Launch Codex App, Savings, Status, and Manage Slimference. Install/Repair is
  one Codex product flow, not separate CLI/App installation.
- After T240, the release claim is evidence-backed: Slimference can be enabled,
  used, repaired, disabled, and uninstalled without making Codex less capable,
  less stable, more expensive, or more confusing.
- After T243, `transport=auto` resolves through the final ladder:
  `wss_phasef -> wss_bridge -> http -> direct`, and Status/TUI show green,
  yellow, orange, or red with exact reasons and bounded logs.
- After T229, Codex hook events use daemon socket RPC on the hot path and fail
  open when the daemon is unavailable.
- After T230, output-reduce v2 reducers are individually gated, observable,
  token-decreasing, and quality-rollback capable.
- WSS output streamcut is not part of the default WSS savings set until T236
  proves terminal-safe behavior. HTTP streamcut remains enabled and tested.
- After T231, Apple Silicon performance claims are backed by real pprof and
  benchmarks; the single stripped Go binary remains the default.
- After T232, app-server, global MITM, proxy/env, and integrate paths are
  visibly lab/debug/legacy, not normal product surfaces.
- `slimference root-arm --global-chatgpt-hosts` is required before
  any global transparent lab test; bare `root-arm` refuses.
- `slimference enable` writes the scoped Codex CLI/App provider route;
  `slimference disable` removes it.
- `slimference lab enable` exits 0 + SIGHUPs daemon → SNI-peek mode is
  configured; actual global routing still requires explicit lab root-arm.
- `slimference lab disable` turns off SNI-peek mode; `lab root-disarm`
  removes the global hosts/pfctl lab route.
- `slimference uninstall` runs Plan.Reverse and restores all touched
  files byte-equal to pre-install (modulo CA rotated aside).
- **Daemon-down**: kill the daemon → hosts reverted automatically →
  Codex works normally.
- **Codex-update**: simulate unknown WS frame schema → wsmitm.Session
  degrades to byte-equal bridge → conversation succeeds.
- `docs/install.md` exists; the YAML spec block parses; every named
  Step exists in `internal/install.Plan()` (verified by meta-test).
- Golden integration coverage is Codex-first. Claude Code remains
  opt-in and should not be part of the default Phase H certification
  matrix.

### Out of scope for Phase H

- Removing legacy URL-redirect / HTTPS_PROXY code paths from the tree.
  They stay as advanced/manual options. Only defaults + tests +
  visible UI affordances flip.
- Live Phase F certification on real Codex WSS traffic (T209). The
  frame adapter is implemented in T208; arming the local machine is
  deferred until an external recovery shell is ready.
- New Phase F mechanisms (Phase H is delivery wiring, not feature
  work).
- Cross-platform install paths (Linux / Windows). macOS only.
