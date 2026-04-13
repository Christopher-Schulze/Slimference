# TokenProxy — Agenten- und Entwicklerregeln

Dieses Dokument ist **verbindlich** für alle automatisierten Agenten (Codex, Claude Code, Cursor, …) und für Menschen, die am Repo arbeiten. Abweichungen nur nach ausdrücklicher Projektfreigabe.

---

## 1. Normative Dokumente

| Quelle | Rolle |
|--------|--------|
| `spec+.md` | Technische **Soll-Spezifikation** v2 (implementierungsrelevant) |
| `handover.md` (Repo-Root) §4 | **Implementierungsreihenfolge** + vollständiges Agent-Onboarding (über `spec+.md` §23); Alias: `docs/HANDOVER.md` → Link dorthin |
| `docs/todo.md` | Arbeitsliste |
| `spec.md` | Historisch v1 — **nicht** für neue Implementierung |

---

## 2. Fremd- und Referenzcode: `rtk-master/`

- Das Verzeichnis **`rtk-master/`** ist ein **eingebettetes Fremdprojekt** (RTK, Rust/inspiration only).
- **Nicht bearbeiten, nicht verschieben, nicht in unsere Ordnerstruktur integrieren** — kein Refactoring, keine „Aufräum“-Commits, keine Tests von dort ins TokenProxy-Layout übernehmen.
- **Nur Inspiration** beim Portieren von Ideen nach Go; die **Spezifikation** für TokenProxy ist **`spec+.md`**, nicht RTK.

---

## 3. Sprachen: Produktionscode, Tooling, Tests

### 3.1 Produktionscode

- **`cmd/`** und **`internal/`**: ausschließlich **Go** (`go 1.24+` laut `go.mod`).
- **JSON:** `encoding/json` (stdlib) — siehe `spec+.md` „Document authority“.

### 3.2 Tooling unter `scripts/` (Pflicht-Ort für Repo-Werkzeuge)

- **Alle** neuen Hilfsprogramme, Checks, kleinen CLIs, die **kein** Bestandteil der Laufzeit-Binary sind, liegen unter **`scripts/`** in **thematischen Unterordnern** — nicht lose im Repo-Root.
- **Implementierung:** **Go** (`.go`), als Pakete unter `scripts/<thema>/` ausführbar mit `go run ./scripts/<thema>/...` vom Modulroot (oder `package main` + `go install` nach Konvention in `scripts/README.md`).
- **Keine** neuen Shell-Skripte, **kein** neues Python/Node für TokenProxy-Repo-Tooling — **Ausnahmen** nur mit expliziter Projektfreigabe.

**Standard-Unterordner (erweitern bei Bedarf, immer thematisch benennen):**

| Ordner | Inhalt |
|--------|--------|
| `scripts/coverage/` | Coverage-Auswertung, Gates, Vergleich mit Schwellenwert (z. B. 100 %-Check für CI/lokal) |
| `scripts/benchmarks/` | Benchmark-Runner, Auswertung von `go test -bench`, Vergleichsläufe |
| `scripts/utils/` | Kleine Hilfs-CLIs (Codegen, einmalige Migrationen, Diagnose) |

Weitere Unterordner nach gleichem Muster (z. B. `scripts/lint/`, `scripts/release/`) — **nie** alles in einen Sammelordner ohne Thema.

- **Zweck** jedes Unterordners kurz in **`scripts/README.md`** und bei neuen Tools in einer **README** im Unterordner oder im Go-Paket als Kommentar/Kurzdoc.

### 3.3 `scripts/` ist optional in dem Sinne …

… dass die **Binary** ohne `scripts/` läuft. **`scripts/`** ist **nicht** optional für die **Disziplin**: wer Coverage messen, Gates fahren oder Benchmarks bündeln will, legt das **hier** ab — nicht verstreut im Root.

---

## 4. Tests: Go (Pflicht für Coverage) + TypeScript unter `tests/`

Die Anforderungen **100 %-Coverage** und **Tests unter `tests/` in TypeScript** sind wie folgt **ohne Lücke** zusammengeführt:

### 4.1 Go — Unit- und Paket-Tests (unverzichtbar)

- **`internal/**` und `cmd/**`**: Unit- und Whitebox-Tests in **`*_test.go`** **neben dem Quellcode** (Go-Standard).
- **Grund:** Unexportierte Symbole, `go test ./...`, **Coverage-Zählung** für genau diese Pakete — das geht nicht durch reine TS-Tests ersetzen.
- **Ziel:** **100 %** Statement-/Branch-Coverage auf dem **gesamten produktionsrelevanten Go-Code** (`cmd/`, `internal/`), messbar mit `go test -cover`, ohne dauerhaftes Ausnehmen von Dateien (Ausnahmen nur wie unten §5).

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

## 5. Testabdeckung (Go) — **100 %, ohne Schummeln**

- **Ziel:** 100 % auf `cmd/` + `internal/` wie oben.
- **Nicht schummeln:** kein dauerhaftes Ausschließen ganzer Pakete aus Coverage ohne Ticket; generierter Code nur mit klarer Kennzeichnung und ggf. Freigabe.
- **Qualität:** table-driven wo sinnvoll, `t.Parallel()` wo sicher, **harte** Rand- und Fehlerfälle, deterministisch (keine flaky Tests), aussagekräftige Fehlertexte.
- **Lokale/CI-Prüfung:** z. B. `go test ./... -covermode=atomic -coverprofile=coverage.out`; ein Gate kann unter **`scripts/coverage/`** implementiert werden.

---

## 6. Bestehende TypeScript-Dateien

- TS/JS **außerhalb** unseres `tests/ts/`-Layouts (z. B. nur in **`rtk-master/`**): **nicht anfassen** (siehe §2).
- Migration älterer TS-Tests → Go oder konsolidiert nach `tests/ts/`: in **`docs/todo.md`** nachziehen, wenn konkret.

---

## 7. Repository-Scan (Tooling-Verschiebung)

- **TokenProxy-Root** (ohne `rtk-master/`): Es gibt **keine** bestehenden Shell-Skripte oder Tooling-Artefakte im Root, die nach `scripts/` **verschoben** werden müssten — das Layout ist vorbereitet (`scripts/coverage/`, `scripts/benchmarks/`, `scripts/utils/`).
- **`rtk-master/scripts/`** etc.: **nicht** nach `scripts/` kopieren oder verschieben (Fremdprojekt, §2).

---

## 8. Kurz-Checkliste vor Merge

- [ ] Spec-konform zu `spec+.md` / relevante Punkte aus `handover.md`
- [ ] `go test ./...` grün; **Coverage (Go)** den Projektzielen entsprechend (100 % Soll)
- [ ] Neue **Go**-Logik mit harten `*_test.go`-Tests
- [ ] Neues **Tooling** nur unter **`scripts/<thema>/`**, vorzugsweise **Go**
- [ ] Optional: **`tests/ts/`**-Tests ergänzend, ohne Go-Coverage zu ersetzen
- [ ] **`rtk-master/`** unverändert gelassen

---

*Änderungen an diesen Regeln: Git-Historie von `AGENTS.md`.*
