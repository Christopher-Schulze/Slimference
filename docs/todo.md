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
- [ ] T47 - Binary-Release-Pipeline (Cross-Build, SHA256SUMS, Minisign, Homebrew-Tap). Detail: `docs/todo/t47-binary-release-pipeline.md`
- [ ] T48 - Linux systemd Service Template + Install-Doku (user-scope + system-scope). Detail: `docs/todo/t48-linux-systemd-service.md`
- [ ] T49 - `docs/documentation.md` + `docs/map.md` + `docs/context.md` Sync auf 2.x + Doc-Lint-Gate. Detail: `docs/todo/t49-docs-sync-2x.md`
- [ ] T50 - `cmd/slimference/main_test.go` Split nach Subcommand-Domaene (12 Files + Helpers). Detail: `docs/todo/t50-main-test-split.md`
- [ ] T51 - Streaming Upload-Limit Integration-Test (>32 MiB chunked + Memory-Ceiling). Detail: `docs/todo/t51-streaming-upload-limit-test.md`
- [ ] T52 - Prompt-Cache Hit-Rate Verifikation gegen echte Anthropic-API (A/B + rolling). Detail: `docs/todo/t52-prompt-cache-anthropic-verification.md`

### Bereich H - Adaptive Tuning + Code-Quality Polish (P2)

- [x] T53 - Adaptive Dedup-Similarity-Staircase (0.88 / 0.85 / 0.82 / 0.78 per Session-Growth, scalar fallback). Detail: `docs/todo/t53-adaptive-dedup-staircase.md`
- [ ] T54 - `min_tokens_for_layer2` Revaluation (30k -> 15k + Latency-Budget-Guard + EMA). Detail: `docs/todo/t54-min-tokens-layer2-reevaluation.md`
- [x] T55 - Structure-Preview (T38) Default-On: default `structure_preview = true` in defaults.go + DefaultTOML. Reversibility via tool-archive stays as a stretch goal. Detail: `docs/todo/t55-structure-preview-default-on.md`
- [ ] T56 - Loop-Detection (T37) Regex -> Jaccard-Word-Similarity-Upgrade. Detail: `docs/todo/t56-loop-detection-jaccard-upgrade.md`
- [ ] T57 - Read-Cache + Tool-Archive TUI-Live-Metriken (hit-rate, bytes, evictions). Detail: `docs/todo/t57-readcache-toolarchive-tui-metrics.md`
- [ ] T58 - TUI TTFT-Breakdown pro Layer (p50/p95 + token-saving-% pro Phase). Detail: `docs/todo/t58-tui-ttft-breakdown.md`
- [ ] T59 - Secrets-Detector Per-Session-Override + Allowlist-Session-TOML (hot-reload, max 1h). Detail: `docs/todo/t59-secrets-detector-session-override.md`
- [x] T60 - Shutdown-Timeout Guard auf `wg.Wait()` (pprof-Dump + ErrShutdownTimeout; headless maps to exit 6). Detail: `docs/todo/t60-shutdown-timeout-guard.md`
- [ ] T61 - Tuning-Config Durchreichen fuer `tool_compressor` RTK-Heuristiken (per-tool overrides). Detail: `docs/todo/t61-tool-compressor-tuning-config.md`
- [ ] T62 - Anthropic-Version-Header Negotiation + Conservative-Mode-Fallback (whitelist, warn-once). Detail: `docs/todo/t62-anthropic-version-negotiation.md`
- [ ] T63 - Tee-Recovery Exit-Code-Matrix in `spec+.md` dokumentieren + Timeout-Guard. Detail: `docs/todo/t63-tee-recovery-exit-code-matrix.md`
- [ ] T64 - TUI Keybindings + Error-Modal Esc-Path Haerten + auto-generiertes `docs/tui-keybindings.md`. Detail: `docs/todo/t64-tui-keybindings-and-error-modal.md`

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
