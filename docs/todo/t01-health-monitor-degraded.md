# T01 - Health Monitor: degraded-Erkennung fixen

**Status:** done
**Priority:** high
**Files:** `internal/proxy/health_monitor.go`, `internal/proxy/health_monitor_test.go`

## Problem

`TestHealthMonitor_degraded` schlaegt fehl:

```
health_monitor_test.go:78: 30% error rate: want degraded, got 1
```

Der Test erzeugt 7 Erfolge + 3 Fehler (in der Reihenfolge: 7x true, false, true, false).
Erwartet: `ProviderHealthDegraded` (Status=2) weil Error-Rate = 3/10 = 30% > 20% Schwelle.
Tatsaechlich: `ProviderHealthHealthy` (Status=1).

Die letzten 3 Eintraege sind `[false, true, false]` - nicht alle false, also korrekt kein "down".
Aber die Error-Rate-Berechnung liefert einen falschen Wert oder die "down"-Pruefung greift faelschlicherweise vorher.

## Root-Cause Analyse

### Schritt-fuer-Schritt Trace

Ring-Buffer Kapazitaet: 20. `head` startet bei 0.

Eingabe-Reihenfolge:
```
Index 0: true   (success)  head -> 1
Index 1: true   (success)  head -> 2
Index 2: true   (success)  head -> 3
Index 3: true   (success)  head -> 4
Index 4: true   (success)  head -> 5
Index 5: true   (success)  head -> 6
Index 6: true   (success)  head -> 7
Index 7: false  (error)    head -> 8
Index 8: true   (success)  head -> 9
Index 9: false  (error)    head -> 10
```

Nach 10 Eintraegen: `head=10`, `count=10`.

### getStatus Trace

**Error-Zaehlung:**
```
for i := 0; i < r.count; i++ {  // i = 0..9
    idx := (r.head - r.count + i + 20) % 20
    // idx = (10 - 10 + i + 20) % 20 = (i + 20) % 20 = i
}
```

Also iteriert idx = 0,1,2,...,9 - korrekt.
buf[7]=false, buf[9]=false -> errors = 2 (nicht 3!).

**AHA!** Der Test sagt "7 successes + 3 errors = 10 total, 3 errors = 30%".
Aber die tatsaechliche Eingabe ist: 7x true, dann false, true, false.
Das sind 7+1=8 true und 2 false = 2/10 = 20%. Nicht >20%, also healthy.

**Der Test-Kommentar ist falsch.** "total: 7+3=10, 3 errors = 30%" stimmt nicht mit dem Code ueberein.

Die Eingabe:
```go
for i := 0; i < 7; i++ { h.record(types.OpenAI, true) }  // 7 successes
h.record(types.OpenAI, false)   // 1 error
h.record(types.OpenAI, true)    // 1 success
h.record(types.OpenAI, false)   // 1 error
```
= 8 successes + 2 errors = 20% Error-Rate. Das ist **genau** die Schwelle, nicht drueber.

Der Test will `> 0.20` (strict greater than), aber 2/10 = 0.20 genau.

## Fix-Optionen

### Option A: Test korrigieren (empfohlen)

Der Test-Code entspricht nicht dem Test-Kommentar. Fix: einen weiteren error hinzufuegen, damit die Rate eindeutig >20% ist.

```go
// 7 successes, then 3 errors mixed = 7+3=10 total, 3 errors = 30% > 20%
for i := 0; i < 7; i++ {
    h.record(types.OpenAI, true)
}
h.record(types.OpenAI, false)   // error 1
h.record(types.OpenAI, true)    // success
h.record(types.OpenAI, false)   // error 2
// FEHLT: ein dritter error fuer echte 30%
```

Entweder: einen weiteren `h.record(types.OpenAI, false)` am Ende, oder die 7 successes auf 6 reduzieren (dann 6+2+1=9 Eintraege, 3 errors von 4 die noch fehlen... komplizierter).

Einfachste Loesung: vor den letzten 3 Eintraegen noch einen error einfuegen:

```go
for i := 0; i < 7; i++ {
    h.record(types.OpenAI, true)
}
h.record(types.OpenAI, false)   // error
h.record(types.OpenAI, false)   // error
h.record(types.OpenAI, true)    // success
h.record(types.OpenAI, false)   // error
// 7 true + 3 false = 10 total, 3/10 = 30% -> degraded
// Last 3: false, true, false -> not all false -> not down -> degraded
```

### Option B: Health-Monitor auf >=20% aendern

Den Schwellwert von `> 0.20` auf `>= 0.20` aendern. Aber das aendert die Spec-Semantik.

**Empfehlung: Option A.** Der Test hat einen Bug, nicht der Code.

## Sub-Tasks

- [x] Test-Eingabe in `TestHealthMonitor_degraded` korrigieren (echte 30% erzeugen)
- [x] Alle Health-Monitor-Tests gruen
- [x] `go test -race ./internal/proxy/...` clean
- [x] Kommentar im Test korrigieren (7+3=10, 3 errors = 30% MUSS stimmen)

## Verification

```bash
go test -run TestHealthMonitor -v ./internal/proxy/
```

Alle 7 Health-Monitor-Tests muessen gruen sein.
