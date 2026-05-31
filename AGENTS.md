# Slimference — Agenten- und Entwicklerregeln

Dieses Dokument ist **verbindlich** für alle automatisierten Agenten (Codex, Claude Code, Cursor, …) und für Menschen, die am Repo arbeiten. Abweichungen nur nach ausdrücklicher Projektfreigabe.

---

## 1. Normative Dokumente

| Quelle | Rolle |
|--------|--------|
| `spec+.md` | Technische **Soll-Spezifikation** v2 (implementierungsrelevant) |
| `handover.md` (Repo-Root) §4 | **Implementierungsreihenfolge** + vollständiges Agent-Onboarding (über `spec+.md` §23); Alias: `docs/HANDOVER.md` → Link dorthin |
| `docs/install.md` | **Install/Uninstall SSOT** (Scoped Codex, 2026-05-17): humans + agents read this for `install`, `status --preflight`, scoped `codex run|enable|disable|status`, and global-lab `root-arm --global-chatgpt-hosts`. Meta-Test `docs/install_spec_test.go` hält Spec ↔ Code synchron. |
| `docs/todo.md` | Arbeitsliste |
| `spec.md` | Historisch v1 — **nicht** für neue Implementierung |

---

## 1a. Produkt-Drawdown-Definition (verbindlich)

Ein **Drawdown** ist ausschließlich ein Nachteil im produktiven Laufzeitbetrieb
für den Nutzer oder das Modell. Entwicklungsaufwand ist **kein** Drawdown.
Captures, Benchmarks, Proofs, Tests, Engineering-Aufwand, längere
Implementierungszeit oder aufwendigere Verifikation zählen nicht als Drawdown.

Nicht akzeptable Produkt-Drawdowns sind insbesondere:

- Das Modell wird weniger intelligent, weniger zuverlässig oder arbeitet
  fachlich schlechter.
- Das Modell verliert Kontext, Gedächtnis, Recency, Salienz oder relevante
  Datei-/Tool-Information.
- Das Modell halluziniert, driftet von der echten Repo-/Datei-/Tool-Realität
  weg oder rekonstruiert Inhalte falsch.
- Codex/Agent-Workflow, UX, Tool-Nutzung, Recovery, Compaction oder Routing
  wird im normalen Betrieb schlechter, verwirrender, fragiler oder langsamer in
  einer nutzerrelevanten Weise.
- Funktionen, Memory, Kontextfenster-Nutzbarkeit oder Modellfähigkeiten werden
  durch eine Optimierung eingeschränkt.

Savings-Mechanismen dürfen default-on nur sein, wenn diese Produkt-Drawdowns
eliminiert oder durch deterministische Guards, Recovery, Fail-open-Verhalten und
Live-Proof praktisch ausgeschlossen sind. Eine Optimierung, die nur mit
manuellem Experiment-Schalter sinnvoll ist oder im Normalbetrieb Modellqualität
riskiert, ist kein Produktfeature.

---

## 2. Fremd- und Referenzcode: `research/rtk-ai/rtk/`

- Das Verzeichnis **`research/rtk-ai/rtk/`** ist ein **eingebettetes Fremdprojekt** (RTK, Rust/inspiration only).
- **Nicht bearbeiten, nicht verschieben, nicht in unsere Ordnerstruktur integrieren** — kein Refactoring, keine „Aufräum“-Commits, keine Tests von dort ins Slimference-Layout übernehmen.
- **Nur Inspiration** beim Portieren von Ideen nach Go; die **Spezifikation** für Slimference ist **`spec+.md`**, nicht RTK.

---

## 3. Sprachen: Produktionscode, Tooling, Tests

### 3.1 Produktionscode

- **`cmd/`** und **`internal/`**: ausschließlich **Go** (`go 1.24+` laut `go.mod`).
- **JSON:** `encoding/json` (stdlib) — siehe `spec+.md` „Document authority“.

### 3.2 Tooling unter `scripts/` (Pflicht-Ort für Repo-Werkzeuge)

- **Alle** neuen Hilfsprogramme, Checks, kleinen CLIs, die **kein** Bestandteil der Laufzeit-Binary sind, liegen unter **`scripts/`** in **thematischen Unterordnern** — nicht lose im Repo-Root.
- **Implementierung:** **Go** (`.go`), als Pakete unter `scripts/<thema>/` ausführbar mit `go run ./scripts/<thema>/...` vom Modulroot (oder `package main` + `go install` nach Konvention in `scripts/README.md`).
- **Keine** neuen Shell-Skripte, **kein** neues Python/Node für Slimference-Repo-Tooling — **Ausnahmen** nur mit expliziter Projektfreigabe.

**Standard-Unterordner (erweitern bei Bedarf, immer thematisch benennen):**

| Ordner | Inhalt |
|--------|--------|
| `scripts/coverage/` | Coverage-Auswertung, Gates, Vergleich mit Schwellenwert (aktuell 95.0 % Aggregate-Gate für CI/lokal) |
| `scripts/benchmarks/` | Benchmark-Runner, Auswertung von `go test -bench`, Vergleichsläufe |
| `scripts/utils/` | Kleine Hilfs-CLIs (Codegen, einmalige Migrationen, Diagnose) |

Weitere Unterordner nach gleichem Muster (z. B. `scripts/lint/`, `scripts/release/`) — **nie** alles in einen Sammelordner ohne Thema.

- **Zweck** jedes Unterordners kurz in **`scripts/README.md`** und bei neuen Tools in einer **README** im Unterordner oder im Go-Paket als Kommentar/Kurzdoc.

### 3.3 `scripts/` ist optional in dem Sinne …

… dass die **Binary** ohne `scripts/` läuft. **`scripts/`** ist **nicht** optional für die **Disziplin**: wer Coverage messen, Gates fahren oder Benchmarks bündeln will, legt das **hier** ab — nicht verstreut im Root.

---

## 4. Tests: Go (Pflicht für Coverage) + TypeScript unter `tests/`

Die Anforderungen **hohe sinnvolle Go-Coverage** und **Tests unter `tests/` in TypeScript** sind wie folgt **ohne Lücke** zusammengeführt:

### 4.1 Go — Unit- und Paket-Tests (unverzichtbar)

- **`internal/**` und `cmd/**`**: Unit- und Whitebox-Tests in **`*_test.go`** **neben dem Quellcode** (Go-Standard).
- **Grund:** Unexportierte Symbole, `go test ./...`, **Coverage-Zählung** für genau diese Pakete — das geht nicht durch reine TS-Tests ersetzen.
- **Ziel:** mindestens **95.0 % aggregate Statement-Coverage** auf dem **gesamten produktionsrelevanten Go-Code** (`cmd/`, `internal/`), messbar mit `go test -cover`, ohne dauerhaftes Ausnehmen von Dateien (Ausnahmen nur wie unten §5). Neue und geänderte komplexe Logik braucht echte, verhaltensrelevante Tests auch dann, wenn das Aggregate-Gate bereits grün wäre.

### 4.2 TypeScript — zusätzliche Tests unter `tests/ts/`

- **Ergänzende** Testsuite(n) in **TypeScript** liegen unter **`tests/ts/`** (z. B. Vitest/Jest — Konfiguration beim Einführen festlegen).
- Verwendung z. B. für: End-to-End gegen HTTP-API, Contract-Tests, oder agentenfreundliche Szenarien — **zusätzlich** zu den Go-Tests, **nicht** als Ersatz für Paket-Coverage in `internal/`.
- **`tests/integration/`** (Go) bleibt für **Go-Integrationstests** reserviert, sofern genutzt.
- **`tests/fixtures/`**: gemeinsame Dateien für **Go- und/oder TS-Tests** nach Bedarf.

### 4.3 Paketnahe Fixtures

- Kleine, paketspezifische Eingaben: weiterhin **`testdata/`** neben dem jeweiligen Go-Paket.

### 4.4 Nicht verschieben

- Bestehende **`internal/.../*_test.go`** und **`cmd/.../*_test.go`** **nicht** nach `tests/` verschieben — bricht Idiom und Coverage.

---

## 5. Testabdeckung (Go) — **95.0 %+ aggregate, ohne Schummeln**

- **Ziel:** mindestens 95.0 % aggregate auf `cmd/` + `internal/` wie oben. Wichtige Produktpfade, Safety-Branches, Routing-/Fallback-Entscheidungen und Regressionsrisiken brauchen echte Tests; künstliche Tests nur zur Coverage-Zahl, Tests für nicht sinnvoll auslösbare OS-Fehlerkanten und always-green Assertions sind nicht Ziel.
- **Nicht schummeln:** kein dauerhaftes Ausschließen ganzer Pakete aus Coverage ohne Ticket; generierter Code nur mit klarer Kennzeichnung und ggf. Freigabe.
- **Qualität:** table-driven wo sinnvoll, `t.Parallel()` wo sicher, **harte** Rand- und Fehlerfälle, deterministisch (keine flaky Tests), aussagekräftige Fehlertexte.
- **Lokale/CI-Prüfung:** z. B. `go test ./... -covermode=atomic -coverprofile=coverage.out`; ein Gate kann unter **`scripts/coverage/`** implementiert werden.

---

## 6. Bestehende TypeScript-Dateien

- TS/JS **außerhalb** unseres `tests/ts/`-Layouts (z. B. nur in **`research/rtk-ai/rtk/`**): **nicht anfassen** (siehe §2).
- Migration älterer TS-Tests → Go oder konsolidiert nach `tests/ts/`: in **`docs/todo.md`** nachziehen, wenn konkret.

---

## 7. Repository-Scan (Tooling-Verschiebung)

- **Slimference-Root** (ohne `research/rtk-ai/rtk/`): Es gibt **keine** bestehenden Shell-Skripte oder Tooling-Artefakte im Root, die nach `scripts/` **verschoben** werden müssten — das Layout ist vorbereitet (`scripts/coverage/`, `scripts/benchmarks/`, `scripts/utils/`).
- **`research/rtk-ai/rtk/scripts/`** etc.: **nicht** nach `scripts/` kopieren oder verschieben (Fremdprojekt, §2).

---

## 8. Kurz-Checkliste vor Merge

- [ ] Spec-konform zu `spec+.md` / relevante Punkte aus `handover.md`
- [ ] `go test ./...` grün; **Coverage (Go)** den Projektzielen entsprechend (95.0 %+ Aggregate-Gate)
- [ ] Neue **Go**-Logik mit harten `*_test.go`-Tests
- [ ] Neues **Tooling** nur unter **`scripts/<thema>/`**, vorzugsweise **Go**
- [ ] Optional: **`tests/ts/`**-Tests ergänzend, ohne Go-Coverage zu ersetzen
- [ ] **`research/rtk-ai/rtk/`** unverändert gelassen
- [ ] Bei Install-/Uninstall-Änderungen: `docs/install.md` aktuell + Meta-Test `go test ./docs/` grün

---

## 9. Verdrahtungs-Doktrin (Scoped Codex, Phase I, 2026-05-17)

Slimference darf den User-Stack im Default nur so anfassen, dass
ChatGPT.app und Browser-ChatGPT normal bleiben:

1. **Signal IN** — Codex-Hooks in `~/.codex/hooks.json` plus
   `~/.codex/config.toml` `[features].hooks=true`.
   Out-of-band Subprozess-Calls, nie über Netzwerk. Claude-Code-Hooks
   bleiben im Code, sind aber Default-off und nur explizit opt-in.
2. **Traffic IN (scoped CLI)** — `slimference codex run -- <prompt>`
   startet nur diesen Codex-CLI-Prozess mit dem
   lokalen `slimference-codex` Provider. Kein `/etc/hosts`, kein pfctl,
   kein System-Proxy, kein Browser-/ChatGPT.app-Blast-Radius.
3. **Traffic IN (global lab only)** — Transparent SNI-MITM
   (`/etc/hosts` + CA in Keychain + Port 443/8443) bleibt im Code, ist
   aber kein Default-Testpfad mehr. `slimference root-arm` verlangt
   explizit `--global-chatgpt-hosts`, weil `chatgpt.com` machine-wide
   auch Browser-ChatGPT und ChatGPT.app betrifft.

**Verboten als Default-Install / Default-Test:**

- persistente `OPENAI_API_BASE` / `OPENAI_BASE_URL` /
  `CHATGPT_BASE_URL` Env-Vars
- persistente `HTTPS_PROXY` / `HTTP_PROXY` Env-Vars
- persistentes `openai_base_url` Feld in `~/.codex/config.toml`
- macOS System-Network-Proxy-Settings
- unbestätigtes `slimference root-arm` ohne `--global-chatgpt-hosts`

Diese Pfade bleiben im Code als **Legacy/Advanced**: Operatoren die
sie manuell setzen, kriegen weiterhin Service. Aber: kein
`slimference install` armiert sie, keine TUI bietet sie an, kein
Integration-Test treibt sie als Primärpfad. Der per-process Codex-CLI
Runner ist die Ausnahme, weil er nicht persistent ist und genau einen
Codex-Prozess scoped.

**Single Entry Point:** Die Subcommands `slimference install`,
`uninstall`, `status`, plus `slimference codex run|enable|disable|status`
sind der normale scoped Codex-Pfad. `cert-trust`,
`root-arm --global-chatgpt-hosts`, transparent `enable`, transparent `disable`, und
`root-disarm` sind globale Lab-/Zertifizierungsbefehle. `proxy run`,
`integrate`, und persistente Proxy-/URL-Patches außerhalb des
marker-owned Codex-Route-Blocks bleiben Legacy.

**Fail-open Mandat:** `slimference codex run` fällt bei Daemonfehler
direkt auf ungefilterten Codex zurück. `slimference codex enable`
bleibt reversibel über `slimference codex disable`; Browser/ChatGPT.app
bleiben immer direkt. Globaler Lab-Pfad mit Daemon down → Hosts-Patch
wird beim Clean Shutdown revertiert; Codex-Update → Frame-Parser
degradiert zu byte-equal Bridge. Diese Eigenschaften sind in
`docs/install.md` dokumentiert und in Tests verifiziert.

**Drift-Verbot:** Änderungen, die das Default-Install-Set um eine
3. Surface erweitern, sind reviewable **nur** mit explizitem
`Phase-H-Override`-Tag in der Änderungsbeschreibung.

---

*Änderungen an diesen Regeln: Git-Historie von `AGENTS.md`.*
