# T09: Coverage-Luecken schliessen

**Date:** 2026-04-17
**Severity:** MEDIUM
**Status:** DONE
**Parent:** `docs/todo.md` -> Audit-Fixes

## Current Coverage Gaps

### RunCompressionJob (layer2.go) - 79.7%

**Uncovered paths:**
- Input-Token-Cap Truncation (Zeilen 148-159): Wenn `origTokens > 120000` wird der Input trunkiert. Test braucht einen Mock-Input mit ~500k geschätzten Tokens.
- Retry-Edge-Cases: Zweiter Retry nach Validation-Failure mit unterschiedlichen Fail-Reasons.

**Test strategy:**
- Test mit sehr grossem Input: `strings.Repeat("word ", 200000)` erzeugt ~500k Token-Schaetzung
- Verifizieren dass truncation stattfindet und origTokens re-estimated wird
- Verifizieren dass die erste Zeile nach dem Truncation-Cut entfernt wird (Clean-Line-Boundary)

### handleSubcommand (main.go) - 82.1%

**Uncovered paths:**
- Daemon-Subcommands: start, stop, restart, service (Zeilen 1656-1702)
- Diese starten/stopen OS-Prozesse, schreiben PID-Files, senden Signale

**Test strategy:**
- Daemon-Funktionen ueber Interface mocken (aehnlich wie proxyAdapter)
- Oder: Test-Helpers die `os.Process` und `exec.Command` mocken
- Schwer testbar - evtl. als Integration-Test mit `//go:build integration`

### internal/hooks/ - 93.4%

**Uncovered paths:**
- Error-Pfade beim Datei-Schreiben: `os.MkdirAll`, `os.WriteFile` Fehler
- Berechtigungs-Fehler, volle Festplatte, etc.

**Test strategy:**
- Temp-Dir mit ReadOnly-Permissions fuer MkdirAll-Fehler
- Temp-Dir mit vollem Disk-Quota (schwer auf macOS)
- Ggf. Interface fuer File-Operations einfuehren

### NopRecorder.Record (debug/decisions.go:180) - 0%

**Trivial:** Nop-Implementierung die nur `nil` zurueckgibt. Ein Test der die Methode aufruft reicht.

## Coverage Targets

| Package | Current | Target |
|---------|---------|--------|
| summarization | 96.3% | >98% |
| cmd/slimference | 88.3% | >92% |
| hooks | 93.4% | >96% |
| debug | 100%* | 100% |

*NopRecorder.Record ist 0% aber debug-Overall ist 100% weil Nop eine nicht-getestete Methode auf einer inneren struct ist.

## Acceptance Criteria

- [ ] `RunCompressionJob` Input-Token-Cap Pfad getestet
- [ ] `NopRecorder.Record` trivial getestet
- [ ] `go test ./...` gruen
- [ ] `go test -race ./...` clean
