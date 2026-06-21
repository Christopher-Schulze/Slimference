# SLIMFERENCE — CONTEXT.md

Lückenloses, stark komprimiertes, eindeutiges Projektkontext-Dokument.
Stand: 2026-06-21. Bindend für alle Agenten-Sessions.

---

## 0. EIN-ZEILEN-MISSION

Maximale lokale Token-Savings `S_local >= 48%` für Codex CLI/Desktop
ohne Produkt-Drawdown. Provider-Cache zählt NICHT als `S_local`.
Entwicklungsaufwand ist KEIN Drawdown.

---

## 1. NORMATIVE QUELLEN (Bindend, in Priorität)

| Quelle | Rolle |
|--------|-------|
| `AGENTS.md` | Bindend für alle Agenten + Humans. Drawdown-Policy, Savings-Doctrine, Test-Disziplin, Routing-Rules. |
| `docs/spec.md` | Technische Target-Spec v3 (implementation-relevant). |
| `docs/install.md` | Install/Uninstall SSOT. Scoped Codex Phase H/I. Meta-test `docs/install_spec_test.go` hält Spec+Code synchron. |
| `docs/documentation.md` | 3646 Zeilen technische Referenz mit file:line Pointern. |
| `docs/todo.md` | Lokale Planfläche (nicht öffentlich). Task-Queue, Execution-Order, Lane-Tabelle. |
| `docs/todo/t*.md` | Task-Detail-Docs: T418, T419, T420, T417, T408, T406, T407, T410, T411, T413, T414, T403. |

---

## 2. DRAWDOWN-POLICY (AGENTS.md §3, Bindend)

**Drawdown = ausschließlich Nachteil in produktivem Runtime-Verhalten.**
Engineering-Aufwand, Captures, Tests, längere Implementierung = NICHT Drawdown.

### Verbotene Drawdowns
- Modell wird weniger intelligent, zuverlässig oder fähig.
- Modell verliert Context/Memory/Recency/Salience/File-Info/Tool-Info.
- Modell halluziniert, driftet, rekonstruiert Content falsch.
- Codex/Agent-Workflow/UX/Tool-Usage/Recovery/Compaction/Routing wird schlechter.
- Funktionen/Memory/Context-Window/Capabilities werden durch Optimierung beschnitten.

### Savings-Regression-Disziplin (§3.1)
- Clean committed baseline vor Edits. `git status` checken.
- Kleinste mögliche Patches: exact predicate, exact route, exact workload, exact request shape.
- Tradeoff quantifizieren bei Savings-Reduktion.
- Disabling muss Fix bringen, sonst revert. Keine akkumulierten permanenten Savings-Verluste.
- Nach Fix: sofort safe Recovery suchen (tighter guard, retry, fail-open, route-specific proof).

### Local-Savings Non-Regression (§3.2)
- `S_local` (exkl. Provider-Cache) ist first-class Metrik.
- Target: `>=48%` auf längeren eligible Codex-Sessions.
- Provider-Cache-Wins dürfen lokale Regression nicht verstecken.
- Guards müssen exacten Drawdown-Vektor benennen + Beweis haben. Keine Handbrakes.
- Tests müssen beide Seiten beweisen: forbidden mutation byte-equal + safe observation/seeding aktiv.

### Aggressive Savings Mitigation Doctrine (§3.3)
- Idee nicht ablehnen nur weil naive Form Drawdown-Risiko hat.
- Drawdown-Vektor identifizieren → Engineering eliminieren/kontrollieren.
- Valid patterns: byte-equal fail-open, stateless detach, metadata-consistent mutation, archive/replay recovery, shadow A/B proof, route-scoped demotion, cache-bust accounting, bounded proof latches, content-free live capture gates.
- `candidate_potential_if_completed` UND `current_production_ready_savings` immer beide reporten.
- "0 potential" nur wenn mitigated complete design immer noch Drawdown hat oder Route physikalisch unmöglich.
- Status: `estimated_candidate` / `engineered_pending_evidence` / `production_ready`.

### Savings Non-Regression Measurement Loop (§3.4)
- Jede savings-relevante Änderung muss gegen clean baseline gemessen werden.
- Guard-Blocking → prüfen ob byte-equal observation/seeding/telemetry aktiv bleiben kann.
- Guards sind nicht statisch → kontinuierlich zum loosest safe predicate engineerieren.
- Split broad guards by route/request-shape/lineage/command-class/content-class/cache-prefix/socket-state/proof-state/recovery-availability.

### Command-Output-First Mandate (§3.5)
- RTK-class command-output-first lane für Codex. Preferred savings point: BEFORE large shell/tool output durable WSS history wird.
- Evaluiere+engineere: hook, launcher shim, app-server control point, PTY boundary, command wrapper, MCP/tool proxy, process-local subprocess boundary.
- Preserve: exact command/cwd/args/env/exit-code/stdout-stderr/ordering/stream-distinction; model access to failure/warning/source/diagnostic/path/line/count/artifact facts; byte-equal fail-open; scoped routing only; local raw-output recovery through archive/tee/rerun.
- Lack of ready Codex hook = kein Stop. Suche anderen seam.

### High-Leverage Priority (§3.6)
- Keine Mikrooptimierung während große Savings-Blöcke offen.
- T418 first (command-output-first), dann T419 (recovery gate), dann T417/T408 (Class-B/server-state).

---

## 3. VERBOTENE PRODUKTPFADE

- Semantic summarization als Substitute für exact parser output.
- Local LLMs.
- OCRL (Optical Character Recognition Layer).
- Context-ledger insertion.
- Summary cache.
- Background summary workers.
- Source-code/file-read first-pass elision.

---

## 4. ARCHITEKTUR

### Core Proxy (`internal/proxy/`)
- `layer0_proxy.go` (2992 Zeilen): Layer-0 Orchestrierung, `reduceCodexLayer0` (line 595).
- `wsmitm_phasef.go` (5622 Zeilen): WSS Phase-F Adapter. `permessage-deflate` payload Mutation, fail-open, history-mutation-guards, stateful-delta-blocking, reconnect-full-history-blocking.
- `layer0_proxy_test.go` (3355 Zeilen): Test-Suite.

### Layer 0 Filter (`internal/filter/`)
- `builtin_search.go` (1482 Zeilen): Search-Output-Reducer. `SearchOutputKeyFromCommandLine`, `RepoScopedSearchOutputKeyFromCommandLine`, `CanonicalSearchMatchSet` (line 496, exportiert, order-insensitive, stabil).
- Built-in reducers für: git, rg, go test/build, npm, pytest, cargo, docker, kubectl, helm, terraform, gh, glab, aws, jq, curl, cat, head, sed, awk, ESLint, SARIF, Package-Install-Audit, + viele mehr.

### Readcache (`internal/readcache/`)
- `evaluate.go` (549 Zeilen): `EvaluateObservedOutput` (line 144), `outputIdentityContent` (line 530), `outputDeltaEligible` (line 543).
- Decision-Types: `DecisionAllow`, `DecisionBlock` mit `BlockKindUnchanged` / `BlockKindDelta`.
- Key-Prefix `search:` → `outputDeltaEligible=true` → `CanonicalSearchMatchSet` wird als Identity verwendet.

### Content Archive (`internal/contentarchive/`)
- Archive-backed recovery. `slimference expand` recovery wird als negative `S_local` gezählt.

### Savings Policy (`internal/savingspolicy/`)
- `DecideCodexToolOutput` entscheidet Mechanismen: ReadDelta, RepeatedOutput, ChunkDedup.
- Workloads: `CodexWorkloadCommand`, `CodexWorkloadSearch`, `CodexWorkloadRead`.

### Chunk Dedup (`internal/chunkdedup/`)
- Reference-based dedup mit integrity budget.

### Codex Route (`cmd/slimference/`, `internal/codexroute/`)
- Scoped Codex: `slimference codex run|enable|disable|status`.
- Phase H/I: PATH/BASH_ENV shim, PostToolUse hook.
- Global Lab: `root-arm --global-chatgpt-hosts` (TLS-MITM, lab-only).

### Evidence (`internal/evidence/`)
- Block-level evidence decisions für telemetry.

### Tokens (`internal/tokens/`)
- o200k_base für Codex billing.

---

## 5. ROUTING MODES

| Mode | Mechanism | Scope |
|------|-----------|-------|
| Codex Hooks | PostToolUse replacement, PATH/BASH_ENV shim | Process-local `slimference codex run` |
| Desktop App-Server Shim | App-server boundary interception | Scoped Desktop |
| Advanced Shared Codex Route | WSS Phase-F adapter | Scoped Codex WSS |
| Global Lab (TLS-MITM) | `root-arm --global-chatgpt-hosts` | Lab-only, NICHT production |

---

## 6. TASK QUEUE (docs/todo.md)

### Active
- **T418** Scoped Codex command-output-first control point. Hauptimplementierungs-Lane. Breite Parser-Abdeckung bereits installiert. Nächster Slice: inferred search content-identity key.

### Queue (conditional, nicht blind serial)
1. **T419** Archive-backed recoverable compaction. Mandatory gate vor bytes-omitting T418/T417/T408 slices. Recovery-contract-matrix: 59 rows, 56 product-ready, 0 default gaps.
2. **T420** Scoped Desktop reconnect full-history elimination. Preempts T417 wenn T403 material reconnect mass zeigt. Aktuell NICHT actionable (live: `full_history_requests=0`).
3. **T403** Owner/Desktop Class-B proof gate. Live-input gate, pausiert nicht T418.
4. **T417** T354/Class-B server-state continuation. Structural WSS lane nach T418/T419. Letzter Commit `fc79287`: exact repeated search-risk output allowed (`BlockKindUnchanged`).
5. **T408** Server-state mirror & sideband references. Enabling lane parallel zu T417/T419. Shadow-mirror-replay: 166 files, 907 turns, 5.1M referenceable bytes, 1.28M token estimate.
6. **T406** Stateful-safe parser frontier max-out.
7. **T407** WSS tool-prefix capability mirror.
8. **T410** WSS tool-prune delta stateless recovery.
9. **T411** WSS output-side reduction exact-proof lane.
10. **T413** First-read scan recovery redesign.
11. **T414** Predictive post-edit exact-state proof.

### Execution Order
1. T418 first bis major command-output mass exhausted oder seam ceiling proven.
2. T419 pull vor broader bytes-omitting slices.
3. T403 wenn clean owner Desktop proof window verfügbar.
4. T420 preempts T417 wenn reconnect mass.
5. T417 nach T418/T419 als structural WSS move.
6. T408 parallel wenn es T417/T419 candidate ranking feedet.
7. T406/T407/T410/T411/T413/T414 in queue order wenn höchster unblocked expected-impact move.

---

## 7. AKTUELLER SLICE (T418: Inferred Search Content-Identity Key)

### Root-Cause (live bewiesen)
- `proxyInferCommandLineFromToolResult(text)` → returns `"rg"` für search-shaped Codex exec payloads.
- `proxyLayer0QualityToolKey("rg")` → `""` (Guard bei line 1803: `SearchOutputKeyFromCommandLine("rg") != ""` → REJECTED).
- `toolKey = ""` → `compactProxyRepeatedToolOutputWithKeyDetailed(sessionID, "", ...)` → `"missing_key"` → zero savings.
- `EvaluateObservedOutput` mit `key=""` → `DecisionAllow` mit `"missing_key"` → zero savings.

### Live bewiesen
- `SearchOutputKeyFromCommandLine("rg")` = `"rg"` (non-empty). Diagnose stimmt exakt.
- `CanonicalSearchMatchSet` existiert, ist exportiert, gibt stabile order-insensitive kanonische Form zurück. Wird bereits von `outputIdentityContent` verwendet. Kein neuer Parser nötig.

### Fix-Design
- Content-identity-basierter Repeat-Key für INFERRED search-shaped Outputs.
- Nur aktiv wenn: `commandFromToolUse == false` (inferred) + `proxyInferCommandLineFromToolResult` returned `"rg"` + Content ist als search match set kanonisierbar.
- Key-Format: `search:inferred:content:<sha256(canonical_match_set)[:16]>`.
- Exact repeat → same canonical → same hash → same key → `BlockKindUnchanged` → savings.
- Changed output → different canonical → different hash → different key → full-pass.
- Implicit-cwd guard für echte command lines bleibt unangetastet.
- WSS search-risk guard (T417, commit fc79287) erlaubt bereits `BlockKindUnchanged` für search-risk.
- Archive recovery bleibt korrekt.

### Akzeptanzkriterien
- focused unit test: inferred repeated rg payload spart beim zweiten Mal.
- focused unit test: changed inferred rg payload full-passt.
- focused unit test: echte unscoped `rg -n TODO src` bleibt rejected.
- WSS search-risk: nur `BlockKindUnchanged` darf mutieren.
- archive marker/recovery/accounting bleibt korrekt.
- `go test ./internal/proxy ./internal/readcache ./internal/filter -count=1`.
- `go test ./...`.
- `go run ./scripts/ci`.
- `go run ./scripts/build -restart`.
- `which slimference` + `slimference status --preflight` + `slimference codex status`.
- local commit, kein Push.

---

## 8. TEST-DISZIPLIN

| Gate | Befehl | Wann |
|------|--------|------|
| Focused tests | `go test ./internal/proxy ./internal/readcache ./internal/filter -count=1 -run "Pattern"` | Nach jedem Slice |
| Package tests | `go test ./internal/proxy ./internal/readcache ./internal/filter -count=1` | Nach jedem Slice |
| Full tests | `go test ./...` | Nach jedem Slice |
| Race tests | `go test ./internal/proxy ./cmd/slimference ./internal/codexroute -race -count=1 -timeout 240s` | Nur bei concurrency/stateful shared mutation |
| CI gate | `go run ./scripts/ci` | Finaler Truth-Gate. gofmt+vet+build+test+95% Coverage+Codex-Smoke+Live-Corpus+Leaf-Audit. |
| Build/Install | `go run ./scripts/build -restart` | Nach CI-Pass. |
| Preflight | `which slimference` + `slimference status --preflight` + `slimference codex status` | Nach Build. |
| Recovery matrix | `go run ./scripts/utils recovery-contract-matrix --json --fail-on-product-gaps` | 0 default gaps muss bleiben. |
| Shadow replay | `go run ./scripts/utils wss-shadow-mirror-replay ~/.slimference/captures --json` | Headroom-Monitoring. |
| Proxy flight gain | `slimference gain all --proxy --json` | Savings-Monitoring. |
| Filter gain | `slimference gain all` | Filter-Savings. |
| WSS sockets | `slimference debug wss-sockets 200 --json` | WSS-State-Monitoring. |

CI-Gate-Floor: `--real-local-min-ratio=0.0597` (5.97%). Target: 48%.

---

## 9. RTK-PATTERN (Referenz, `rtk/` Directory)

RTK (Rust Token Killer) ist ein CLI-Proxy der 60-90% Token-Savings erreicht durch:
- **PreToolUse Hook**: `hooks/claude/rtk-rewrite.sh` ist thin delegate. Empfängt `tool_input.command`, ruft `rtk rewrite "$CMD"` auf, returns rewritten command (z.B. `git status` → `rtk git status`).
- **Command Proxy**: `rtk <cmd>` führt underlying command aus, filtert output durch spezialisierte Filter in `src/cmds/*/`, gibt komprimiertes Output zurück.
- **Exit-Code-Protocol**: 0=rewrite+auto-allow, 1=no equivalent, 2=deny, 3=ask.
- **Registry**: `src/discover/registry.rs` ist single source of truth für rewrite rules.
- **Hooks für**: Claude Code, Copilot, Cursor, Cline, Windsurf, Codex, OpenCode, Hermes, Pi.

Für Slimference relevant: RTK intercepts BEFORE output durable model context wird. Das ist §3.5 Command-Output-First Mandate. Slimference's äquivalenter seam ist T418 (scoped PATH/BASH_ENV shim, PostToolUse hook, WSS Phase-F adapter).

---

## 10. RUNTIME-STATE (live verifiziert 2026-06-21)

- `git status`: main 426 commits ahead of origin/main. Untracked: `rtk/`.
- `slimference status --preflight`: daemon running, WSS certified, Codex/Apps enabled, NOT globally routed. `normal_direct=true`, `advanced_route=false`, `hosts_active=false`, `global_443=false`, `global_8443=false`.
- Go-Cache: geleert (5.9GB → 12KB). Disk: 11GB frei.
- Proxy flight gain (all): 8547 requests, 275.8M original input, 176.4M final input, 99.5M billable savings, 135.0M cache-read discount, 234.4M net billable-equivalent.
- Filter gain (all): 3799 runs, 1.6M input, 230.5K output, 1.4M saved.
- Shadow-mirror-replay: 166 files, 907 turns, 54602 frames, 286 captured mutated requests. Top: `full_history/codex_exec_payload_command_rg` (1.6M bytes, 403K tokens, 100% referenceable), `full_history/codex_exec_payload_command_sed` (1.2M bytes, 311K tokens, 85% referenceable).
- WSS sockets: 16 WSS requests, 6 sockets, 0 actionable, 0 full_history, 0 reconnect.
- Recovery matrix: 59 rows, 56 product-ready+default, 0 default gaps, 3 non-default blocked.

---

## 11. KEY FILE PATHS

| File | Role |
|------|------|
| `internal/proxy/layer0_proxy.go` | Layer-0 Orchestrierung, `reduceCodexLayer0` line 595, `proxyLayer0QualityToolKey` line 1788, `proxyInferCommandLineFromToolResult` line 2056, `compactProxyRepeatedToolOutputWithKeyDetailed` line 2344 |
| `internal/proxy/wsmitm_phasef.go` | WSS Phase-F Adapter, history-mutation-guards line 725-876, `StatefulDeltaMutationBlocked` line 844 |
| `internal/proxy/layer0_proxy_test.go` | Test-Suite, `TestProxyRepeatedSearchOutputRejectsImplicitCwdKey` line 3188, inferred WSS search tests lines 1064/1520/1534 |
| `internal/readcache/evaluate.go` | `EvaluateObservedOutput` line 144, `outputIdentityContent` line 530, `outputDeltaEligible` line 543 |
| `internal/filter/builtin_search.go` | `SearchOutputKeyFromCommandLine` line 584, `RepoScopedSearchOutputKeyFromCommandLine` line 646, `CanonicalSearchMatchSet` line 496 |
| `cmd/slimference/command_output_first.go` | T418 shim |
| `internal/savingspolicy/` | `DecideCodexToolOutput`, Workload-Enums |
| `internal/contentarchive/` | Archive-backed recovery |
| `internal/chunkdedup/` | Reference-based dedup |
| `internal/evidence/` | Block-level evidence telemetry |
| `internal/tokens/` | o200k_base token counting |
| `scripts/ci/` | Finaler CI-Gate (gofmt+vet+build+test+coverage+smoke+corpus+leaf-audit) |
| `scripts/build/` | Build+Install+Restart |
| `scripts/utils/` | `wss-shadow-mirror-replay`, `recovery-contract-matrix`, `wss-local-gap`, `wss-class-distribution` |
| `scripts/benchmarks/` | `benchmark-corpus` mit `--real-local-min-ratio` gate |

---

## 12. DEVIN CLI /loop MECHANIK (live aus binary strings extrahiert)

- `/loop <prompt>`: Run prompt → auto-review diff → loop. Requires clean git state.
- Loop-Handler: `chisel/src/repl/loop_handler.rs`, `chisel-agent/src/loop_handler.rs`.
- Auto-Review: `Loop: running git diff for review...` → Model reviewt diff → `<ACCEPT>` oder `<REJECT>`.
- `<ACCEPT>`: `Loop: PR accepted!` → Loop endet.
- `<REJECT>`: `Thanks for your review! Can you implement the fixes thoroughly so that this is ready to merge?` → Loop iteriert, agent implementiert fixes, nächster review-cycle.
- Stop-Bedingung: Model schreibt `<ACCEPT>` → Loop endet. Kein max-iteration-limit gefunden.
- `Models to cycle through with /loop`: Config-supported model-cycling.
- Loop-Prompt wird als user-message injected, diff-review als system-internal.

---

## 13. MEMORY/DISK-MANAGEMENT

- Go-Cache: `~/Library/Caches/go-build`. Bei low disk space: `go clean -cache`.
- Binary: 28MB. Unkriptisch.
- Captures: `~/.slimference/captures/` (166 files, kann wachsen).
- Decisions: `~/.slimference/debug/decisions.jsonl`.
- Bei Disk < 5GB: Go-Cache leeren.

---

## 14. COMMIT-DISZIPLIN

- Local commits nach sauberem Slice. Kein Push unless explicitly asked.
- Commit-Message: `TASK T4XX: <description>`.
- Pre-commit hooks: stage modified files + retry bei failure.
- Keine secrets/keys in commits.
- `git status` + `git diff` + `git log` vor commit.
- Co-Authored-By: Devin trailer.

---

## 15. NICHT TUN

- README nicht ändern.
- Kein Push (lokaler Commit ja).
- Keine globale Proxy/Hosts/Codex-Route setzen.
- `rtk/` nicht anfassen (externes Vergleichsmaterial, untracked).
- Keine Mikrooptimierung während große Brocken offen.
- Keine breiten Guards ohne exakt benannten Drawdown-Vektor.
- Keine Tests weichklopfen.
- Keine "bewiesen = 0 Potenzial" Logik.
- Keine "safe weil wahrscheinlich" Änderung.
- Keine historischen todo checkboxen blind abarbeiten.
- Keinen zweiten Parser bauen wenn bestehende Kanonisierung existiert.
- Kein Warten auf "Go" wenn autonomes Weiterarbeiten beauftragt wurde.
