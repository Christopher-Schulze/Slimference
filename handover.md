# TokenProxy — Handover / Agent-Briefing (vollständig)

**Stand:** 2026-04-10 (Reality-Check gegen Repo, nicht nur ältere Texte).  
**Erste Aktion für den nächsten Agenten:** Diese Datei **vollständig** lesen, dann `AGENTS.md` und `docs/todo.md` skimmen; normative Spec ist **`spec+.md`**, Arbeitsliste **`docs/todo.md`**.

---

## 1. Was ist dieses Projekt?

**TokenProxy** ist eine **Go-Binary** (`tokenproxy`), die zwei zusammengehörige Probleme löst:

1. **Layer 0 — Pre-Entry (CLI):** Shell-Befehle, die Coding-Agenten ausführen, können **vor** der Aufnahme in den Chat gekürzt werden (`tokenproxy filter`, Hooks, Rewrites, SQLite-Tracking, Tee-Recovery). Ziel: weniger Tokens in der Historie, bevor der Proxy überhaupt sieht.

2. **Layer 1–3 — Post-Entry (HTTP-Proxy):** Ein **transparenter Reverse-Proxy** zwischen Tools (Claude Code, OpenAI-Codex-CLI o. Ä.) und den APIs **Anthropic / OpenAI**. Eingehende Requests werden modifiziert (Kompression alter Nachrichten, Caching, Secrets, Analytics); **Responses** werden **unverändert** durchgereicht (Streaming/SSE).

**TUI:** Ohne Argumente startet `main()` die **BubbleTea**-Oberfläche + Proxy im Hintergrund — das ist die „Haupt-App“, kein separater Daemon-Modus.

**Referenz-Code:** `rtk-master/` ist ein **eingebettetes Fremdrepo** (Rust/RTK). **Nicht bearbeiten.** Nur Ideen beim Portieren; die Spezifikation ist **`spec+.md`**, nicht RTK.

---

## 2. Normative Dokumente (Reihenfolge der Wahrheit)

| Dokument | Rolle |
|----------|--------|
| **`spec+.md`** | **Soll-Spezifikation v2** — implementierungsrelevant, inkl. „Document authority“-Block am Anfang (SQLite=`modernc.org/sqlite`, JSON=`encoding/json`, Sliding-Window = **user-started exchanges**, Hooks v1 = **Claude Code + Codex**). |
| **`spec.md`** | **Historisch v1.0-final** — Kontext, ursprüngliche Zielgrößen (z. B. 60–80 % Savings); **darf neue Implementierung nicht überschreiben**. |
| **`handover.md` (diese Datei)** | Onboarding, Stand, Workflow, wo was liegt — **kein Ersatz** für `spec+.md`, aber **Repo-Realität** ergänzen. |
| **`docs/todo.md`** | **Master-Arbeitsliste** mit Checkboxen; enthält auch **Phasen A–E** und Abgleich mit älteren HANDOVER-§6/§7-Listen. |
| **`AGENTS.md`** | **Verbindliche Agentenregeln** (Sprachen, `scripts/`, Tests, `rtk-master/`, Coverage-Ziele). |
| **`docs/documentation.md`** | Nutzer-/Architektur-Doku (teils noch v1-Stil); **nach großen Features aktualisieren** (`docs/todo.md` §„Documentation Updates“). |
| **`docs/context.md`** | Kurz-Übersicht Tabellen/Flows (ergänzend zu Spec). |
| **`docs/map.md`**, **`docs/changelog.md`** | Landkarte, Historie — bei Bedarf konsultieren. |

**Wichtig:** In `spec+.md` steht explizit: **`spec+.md` §23 (Rollout)** ist eine **Feature-Checkliste**, keine starre Kalender-Reihenfolge. **Implementierungsreihenfolge in diesem Repo** folgt **`handover.md` §4** (entspricht dem früheren `docs/HANDOVER.md` §3) und **`docs/todo.md` „Implementierungs-Reihenfolge“** — bei Konflikt mit §23 gewinnt diese Reihenfolge.

---

## 3. Spec.md vs spec+.md (kurz und präzise)

- **`spec.md`:** Nur **Layer 1–4**-Proxy-Welt (kein Layer 0), ältere Savings-Zahlen, **kein** SQLite-Filter-Tracking, **kein** Hook-System in der Tiefe von v2.
- **`spec+.md`:** **Layer 0** (Filter-Engine, 24 Filter-Module, TOML-DSL, Tee, Permissions), **erweiterte Layer-1**-Sub-Layer (ANSI, Tool-Classifier, …), **Layer-2**-Erweiterungen (adaptives Fenster, Priorität), **Debug/JSONL**-System, aktualisierte **Projektstruktur**, **Appendices D/E**.

**Praktische Regel:** Feature-Anforderung → in **`spec+.md`** nachschlagen; historische Motivation optional in **`spec.md`**.

---

## 4. Implementierungsreihenfolge (für dieses Repo)

Entspricht **`docs/todo.md`** (Phasen A–E) und dem früheren HANDOVER §3:

1. **Phase A — Layer 1 am bestehenden Proxy härten**  
   Pipeline-Reihenfolge/Exchange-Window, Overflow (`spec+.md` §17.4), MinHash/Dedup, Structure/Comments, Tests.

2. **Phase B — Layer 0**  
   `internal/filter/`, `internal/hooks/`, CLI `filter|hook|rewrite|gain`, SQLite, Tee, TOML-DSL — **großteils vorhanden** (siehe §8).

3. **Phase C — Layer 2 Erweiterungen**  
   Adaptives Fenster, Tool-Priorität für Summarization.

4. **Phase D — Advanced L1 + Debug**  
   Fehlende L1.x-Sub-Layer, `internal/debug` vollständig (Decision-JSONL, …).

5. **Phase E — Docs & Polish**  
   `docs/documentation.md`, `docs/map.md`, Changelog, Risiko-Checks.

**Querschnitt:** Provider-Invisibility `spec+.md` §16.4, Hot-Path-Latenz-Ziele, Layer-0 **Exit-Code-Treue**, nur `encoding/json`.

---

## 5. Repository-Layout (was wo liegt)

```
TokenProxy/
  handover.md              ← Diese Datei (Agent-Onboarding)
  AGENTS.md                  ← Verbindliche Repo-Regeln
  spec+.md, spec.md          ← Spezifikationen
  go.mod, go.sum
  .gitignore                 ← u.a. .env.local, .secrets/ (keine Keys committen)
  .env.local                 ← optional lokal: MINIMAX_API_KEY (nicht versionieren)

  cmd/tokenproxy/            ← main, alle CLI-Subcommands, TUI-Einstieg
  internal/
    proxy/                   ← HTTP-Handler, Streaming, Provider (Anthropic/OpenAI)
    compression/             ← Layer 1 (layer1.go, ansi_strip, dedup, structure.go, …)
    summarization/           ← Layer 2 (MiniMax, Cache, Progressive, …)
    caching/, tokens/, security/, resilience/, sessions/, analytics/, util/, types/, config/, tui/
    filter/                  ← Layer 0 (Pipeline, Built-ins, TOML-DSL, SQLite, Tee, …)
    hooks/                   ← Claude/Codex install/remove/verify
    debug/                   ← Replay-Preview, Pfade; volle Decision-Pipeline teils offen

  scripts/
    coverage/                ← Go: Coverage-Auswertung (siehe scripts/README.md)
    benchmarks/, utils/      ← Platzhalter nach Thema

  docs/
    todo.md                  ← Master-TODO
    HANDOVER.md              ← Kurzverweis auf ../handover.md (Alias)
    documentation.md, context.md, map.md, changelog.md

  tests/                     ← README; TS-Tests unter tests/ts/ laut AGENTS (wenn genutzt)
  rtk-master/                ← NUR LESEN, nicht editieren
```

**Modul:** `github.com/tokenproxy/tokenproxy` — **Go-Version in `go.mod` ist maßgeblich** (aktuell **1.25.0**; Spec-Text erwähnt teils 1.24+ — Code nach `go.mod`).

---

## 6. Laufzeit-Flow (End-to-End)

1. **Ohne Args:** `tokenproxy` → `config.Load()` → `proxy.New` → Proxy startet Listener → BubbleTea TUI. User toggelt Provider/Layer, sieht Analytics, Debug-Infos, etc.

2. **Mit Args:** `handleSubcommand` — z. B. `config`, `test`, `doctor`, `stats`, `gain`, `filter`, `rewrite`, `hook`, `debug`, `version`. Viele Pfade beenden mit `os.Exit`; Tests nutzen Subprozesse + Env-Flags.

3. **Proxy-Hot-Path:** Request → Handler → Kompression (Layer 1) / Queue für Layer 2 → Upstream → Response-Stream zurück. Details in `internal/proxy/handler.go`, `provider.go`, `streaming.go`.

4. **Layer 0:** `tokenproxy filter -- <argv>` → `filter.RunPipeline` (ANSI, Built-ins, TOML, Truncate) → Exit-Code vom Kind; bei Fehler/nicht-null ggf. Tee-Recovery unter `TOKENPROXY_TEE_DIR` / Config.

---

## 7. CLI-Überblick (Stand Code)

Aus `cmd/tokenproxy/main.go` Kommentar + Implementierung — **nicht** jede Zeile hier, aber Orientierung:

| Bereich | Beispiele |
|---------|-----------|
| Config | `config init`, `config show` |
| Diagnose | `doctor`, `test minimax|anthropic|openai`, `test intercept <claude|codex>` |
| Analytics | `stats today|week|month`, `gain …` (JSON/CSV/by-command/project, USD aus Config) |
| Layer 0 | `filter -- …`, `rewrite …` (oder Hook-JSON auf stdin) |
| Hooks | `hook install|remove|verify|status` (v1: claude, codex) |
| Debug | `debug paths`, `debug last`, `debug summary`, `debug tail`, `debug replay <file>` |
| Sonst | `version` |

**TUI:** `runTUI()` — `main` und `runTUI` haben in Tests oft **0 % Coverage** (normal).

---

## 8. Reality-Check: Was ist implementiert vs. Spec/TODO?

**Bereits stark vorhanden (Kurz):**

- **Proxy** mit Anthropic/OpenAI-Pfaden, Streaming, Retry/Resilience, Session-Logging, Analytics-Persister.
- **Layer 1:** u. a. `ansi_strip`, `json_compact`, `comment_strip`, `dedup` + **`dedup_minhash`**, **`structure.go`** (Regex, nicht Tree-sitter), `delta`, `prompt_cache`, `success_shortcircuit`, Exchange-Window-Logik — **nicht** alle spec+.md-Sub-Layer vollständig (siehe `docs/todo.md` L1.7–L1.14).
- **Layer 2:** MiniMax-Client, Summary-Cache, Progressive, Anchor, Validator, etc.
- **Layer 0:** `internal/filter` mit Pipeline, vielen `TryCompact*`-Built-ins, TOML-DSL (`filters_toml.go`), SQLite `filter_runs`, Tee, Permissions/Deny-Patterns, `tokenproxy gain`.
- **Hooks:** Install/Remove/Verify für Claude & Codex.
- **Debug:** Pfade, last/summary/tail, Replay-**Preview** — volle „Decision JSONL“-Pipeline laut `docs/todo.md` teils **offen**.

**Tests:** Umfangreiche `*_test.go` unter `cmd/tokenproxy` und `internal/**`. Ziel laut **`AGENTS.md`**: **100 %** Statement-Coverage auf `cmd/` + `internal/`; **aktuell** (letzte Messung) **Gesamtrepo ~81 %** Statements — **Lücke bewusst**, weiter mit `go test ./... -cover` und ggf. `scripts/coverage/`.

**Hinweis:** `docs/todo.md` enthält noch Checkboxen wie „Add internal/filter/“ — **teils veraltet** (Paket existiert). Immer **Code + todo** gegenchecken.

---

## 9. Zuletzt (Sessions) — was passierte

*(Für den nächsten Agenten: konkrete Git-Historie ist maßgeblich; hier inhaltliche Schwerpunkte.)*

- **Test-Coverage erhöht** in mehreren Paketen (`cmd/tokenproxy`, `internal/proxy`, Filter, …): Subcommand-Tests, Provider/Streaming-Kanten, `handleDoctorCmd`, `handleConfigCmd`/`filter` (u. a. Tee bei non-zero exit), `GetLayer2Status` via `proxy.ClearLayer2ForTesting()`.
- **Secrets:** Projekt-**`.gitignore`** ergänzt; **`MINIMAX_API_KEY`** kann lokal in **`.env.local`** liegen — **niemals committen**; Keys aus Chats rotieren, falls exponiert.
- **Dokumentation:** Diese **`handover.md`** ersetzt/überführt den veralteten Inhalt von **`docs/HANDOVER.md`** (ältere Version sprach noch von nicht-existierenden Dateinamen wie `treesitter.go` ohne Rename — **im Repo gibt es `structure.go`**).

---

## 10. Workflow für Agenten (wie exakt arbeiten)

1. **Specs:** Änderungen am Verhalten → zuerst **`spec+.md`** klären oder dort als Draft ergänzen (wenn Projektfreigabe).
2. **Code:** Nur Go in `cmd/`, `internal/`; Tooling nur **`scripts/<thema>/`** in Go (siehe **`AGENTS.md` §3**).
3. **`rtk-master/`:** **Nicht** editieren, nicht verschieben, nicht in CI mit TokenProxy-Tests mischen.
4. **Tests:** Neue Logik → `*_test.go` neben dem Code; table-driven + `t.Parallel()` wo sicher; keine dauerhafte Coverage-Ausnahme ohne Freigabe.
5. **PR/Merge-Checkliste:** Siehe **`AGENTS.md` §8** (`go test ./...`, Coverage-Richtung, kein RTK-Diff).
6. **JSON:** Nur **`encoding/json`** (kein `gjson` o. Ä.).

---

## 11. Konfiguration & Umgebungsvariablen (Auszug)

- **Config-Datei:** Standard `~/.tokenproxy/config.toml`; Override **`TOKENPROXY_CONFIG`** (Pfad zur TOML).
- **Wichtige ENV:** `TOKENPROXY_UPSTREAM_*`, `TOKENPROXY_LISTEN_*`, `MINIMAX_API_KEY` (oder Name aus Config `api_key_env`), `TOKENPROXY_FILTER_DB`, `TOKENPROXY_TEE_DIR`, `TOKENPROXY_DEBUG_DECISIONS_LOG`, `TOKENPROXY_HOOK_TOKENPROXY_COMMAND`, `TOKENPROXY_CONFIRM_SUDO` (Layer-0 sudo-Verhalten), … — Vollliste in **`spec+.md` §13** und **`internal/config/config.go`** (`applyEnvOverrides`).

---

## 12. Bekannte technische Schulden / Fallen (kurz)

- **Dedup:** Spec beschreibt historisch auch MinHash — **`dedup_minhash.go` existiert**; Abgleich mit Spec-Grenzfällen laufend.
- **Sliding window:** Überall **user-started exchanges**, nicht rohe Message-Count (`spec+.md` §13.1).
- **Overflow:** Handler muss **`spec+.md` §17.4** (Re-Compression, dann Fallback) entsprechen — bei Abweichungen Spec oder Code fixen.
- **Hooks:** Nur **bash/settings**-Pflege für v1-Targets; echte Claude/Codex-Tests sind manuell wertvoll.
- **`proxy.ClearLayer2ForTesting()`:** Nur für Tests gedacht (TUI-Adapter `GetLayer2Status`).

---

## 13. Empfehlungen — wie es sinnvoll weitergeht

1. **`docs/todo.md`** oben (Phasen A–E) + offene **F01–F24** / **L1.x** / **Debug**-Zeilen durchgehen und **nächstes Arbeitspaket** wählen (ein logischer Block, nicht alles parallel).
2. **Coverage:** Richtung **AGENTS.md**-Ziel; `go test ./... -coverprofile=...`, Hotspots in `cmd/tokenproxy` und dünnen Paketen.
3. **Spec-Sync:** Nach größeren Features **`docs/documentation.md`** / **`docs/map.md`** aktualisieren (`docs/todo.md` §„Documentation Updates“).
4. **Keine Scope-Creep** in `rtk-master/`.

---

## 14. `docs/HANDOVER.md` (Legacy-Pfad)

Die Datei **`docs/HANDOVER.md`** verweist nur noch auf **`../handover.md`**, damit alte Links (`AGENTS.md` Zeilen mit „HANDOVER“) nicht brechen. **Einzelne Quelle für Onboarding:** **`handover.md` im Repo-Root**.

---

*Ende Handover — bei Widerspruch zwischen diesem Dokument und dem Code gewinnt der **Code**; bei Widerspruch zwischen Code und **`spec+.md`** gewinnt die **Spec** (nach Abstimmung mit dem Maintainer).*
