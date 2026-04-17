# O1-O6: MiniMax Kompressionsmodell Optimierung

**Date:** 2026-04-17
**Status:** DONE
**Parent:** `docs/todo.md` -> MiniMax Optimierung

## Context

MiniMax M2.7 ist der einzige aktive Summarization-Provider. Die Kompromissqualitaet
war gut aber nicht maximal stabil - gelegentliche Formatverletzungen, suboptimale
targetTokens fuer verschiedene Input-Groessen, und keine Fuzzy-Dedup.

## Changes

### O1: Few-Shot Prompt (minimax.go)
- Konkretes Input/Output-Beispiel im `systemPrompt` hinzugefuegt
- Beispiel zeigt typische Coding-Session (9 Messages) mit korrektem Bullet-Output
- Erwarteter Effekt: M2.7 haelt sich signifikant besser an Format-Vorgaben

### O2: Adaptive targetTokens (layer2.go)
- `computeAdaptiveTarget()` ersetzt starren `origTokens * 0.20`
- Kurze Inputs (<=5 Nachrichten): ratio * 1.5
- Mittlere Inputs (6-10): ratio * 1.25
- Code-dichte Inputs: ratio + density * 0.15
- Sehr kurze Tokens (<1000): mindestens 40% ratio
- Cap bei 60% um ueberkompression zu verhindern

### O3: Content-Type-Erkennung (layer2.go)
- `contentDensity()`: misst Anteil an Code/Tool/Path-Inhalt (0.0-1.0)
- `looksLikeCode()`: erkennt func/var/import/if/for/switch/==/=>/usw.
- `looksLikePath()`: erkennt Pfade mit >=2 slashes, ./ ../ , .go/.ts/.rs/.py etc.
- Hohe Dichte -> mehr targetTokens (Fakten brauchen Platz)

### O4: Praezise Token-Schaetzung (layer2.go)
- `estimateTokens()` umgeschrieben: Wort-basiert statt `len(text)/4`
- CJK-Zeichen (Han/Hiragana/Katakana) = 1 Token je Zeichen
- Whitespace trennt Woerter
- Genauer fuer gemischten Code/Prosa-Input

### O5: Exponentieller Backoff + Jitter (minimax.go, defaults.go)
- Backoff: `500ms * 2^attempt + random jitter (0..base/2)`, max 10s
- `maxRetries` Default von 2 auf 3 erhoeht
- Stabiler bei transienten API-Fehlern (429, 5xx)

### O6: Fuzzy Dedup (minimax.go)
- `similarEnough()`: Jaccard-Wort-Aehnlichkeit mit Schwellwert 0.70
- `toWordSet()`: Woerter >4 chars als Features
- Erkennt nahezu-identische Bullets die sich nur in einem Wort unterscheiden
- Haengt an bestehende exakte Dedup + Substring-Dedup an

## Additional Fixes (gleiche Session)

- `progressive.go:113`: `l.minimax.IsConfigured()` -> `l.chain.ActiveProviderName() == ""`
- `progressive.go:148`: `l.minimax.Summarize()` -> `l.chain.Summarize()` (3-Return-Werte)
- `deduplicateBullets()`: Algorithmus komplett neu geschrieben (laengere Bullets subsumieren kuerzere unabhaengig der Reihenfolge)
- 4 neue `preprocessInput` Tests
- 11 neue Tests fuer die Optimierungen
- `TestEstimateTokens` auf Wort-basierte Logik angepasst

## Test Results

- 20/20 Pakete gruen
- Race-clean
- Summarization Coverage: 96.3%

## Files Changed

- `internal/summarization/minimax.go` - Prompt, Backoff, Dedup
- `internal/summarization/layer2.go` - Adaptive Target, Content-Type, Token-Schaetzung
- `internal/summarization/progressive.go` - Chain-Integration
- `internal/summarization/minimax_test.go` - Dedup, Similarity, WordSet Tests
- `internal/summarization/layer2_test.go` - Token, Density, Adaptive, Code/Path Tests
- `internal/config/defaults.go` - maxRetries 2->3
