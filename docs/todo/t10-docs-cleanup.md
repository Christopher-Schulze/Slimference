# T10: Offene Doku + Cleanup

**Date:** 2026-04-17
**Severity:** LOW
**Status:** DONE
**Parent:** `docs/todo.md` -> Audit-Fixes

## Items

### T04遗留: "Wie man echte Zahlen bekommt"

`docs/todo.md` T04 hat einen offenen Punkt:
> Dokumentation: Wie man echte Zahlen bekommt (30 Min Claude Code Session + stats)

**Was fehlt:** Kurze Anleitung in docs/documentation.md wie ein User:
1. Slimference startet
2. 30 Min Claude Code/Codex Session arbeitet
3. `slimference stats today` aufruft um echte Token-Savings zu sehen
4. `slimference gain today` fuer Layer 0 Filter-Savings

### Dedup-Schwellwert 0.70 Jaccard: False-Positive-Risiko

Die neue Fuzzy-Dedup in `minimax.go:deduplicateBullets` nutzt Jaccard-Aehnlichkeit mit Schwellwert 0.70 und Woerter >4 chars. Theoretisch koennen Bullets die zufaellig viele gemeinsame Woerter haben aber unterschiedliche Facts enthalten falsch zusammengefasst werden.

**Beispiel:** "- Fixed authentication bug in handler.go causing 500 errors" vs "- Fixed authentication bug in handler.go causing timeout errors" - diese sind aehnlich aber beide Facts sind relevant.

**Dokumentation:** In docs/documentation.md einen Abschnitt zur Dedup-Strategie und den Schwellwert einfuegen mit Hinweis dass der Schwellwert bei Bedarf angepasst werden kann.

### estimateTokens vs estimateTokensFromText

Zwei unabhaengige Token-Schaetzungsfunktionen im Codebase:

| Funktion | Paket | Methode |
|----------|-------|---------|
| `estimateTokens` | summarization | Wort-basiert, CJK-Support |
| `estimateTokensFromText` | proxy/streaming | `len(text)/4` |

Beide sind intentional verschieden (verschiedene Use-Cases) aber das sollte dokumentiert sein.

**Wo:** `docs/map.md` Eintrag fuer beide Funktionen mit Erklaerung warum sie unterschiedlich sind.

## Acceptance Criteria

- [x] T04 Anleitung in docs/documentation.md ergaenzt
- [x] Dedup-Strategie und Schwellwert dokumentiert
- [x] Token-Schaetzungs-Funktionen in map.md dokumentiert
