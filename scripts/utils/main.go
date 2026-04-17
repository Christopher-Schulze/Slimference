// Package main implements the scripts/utils tool for offline session analysis.
//
// Usage:
//
//	go run ./scripts/utils session-report <file.jsonl>
//	go run ./scripts/utils decision-report <file.jsonl>
//	go run ./scripts/utils filter-report <filter.db>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/types"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/utils <subcommand> <path>")
		fmt.Fprintln(os.Stderr, "Subcommands: session-report, decision-report, filter-report")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "session-report":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: session-report <analytics.jsonl>")
			os.Exit(1)
		}
		if err := sessionReport(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "decision-report":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: decision-report <decisions.jsonl>")
			os.Exit(1)
		}
		if err := decisionReport(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "filter-report":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: filter-report <filter.db>")
			os.Exit(1)
		}
		if err := filterReport(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// --- session-report: parses analytics JSONL files ---

type sessionStats struct {
	firstTS, lastTS time.Time
	totalRequests   int
	origTokens      int
	compTokens      int
	outputTokens    int
	layer1Savings   int
	layer2Savings   int
	layer3Savings   int
	secretsFound    int
	errors          int
	retries         int
	byProvider      map[string]*providerStats
}

type providerStats struct {
	requests   int
	origTokens int
	compTokens int
	saved      int
}

func sessionReport(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	stats := &sessionStats{byProvider: make(map[string]*providerStats)}
	dec := json.NewDecoder(f)

	for dec.More() {
		var envelope struct {
			Type      string          `json:"type"`
			Timestamp time.Time       `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := dec.Decode(&envelope); err != nil {
			continue
		}

		switch envelope.Type {
		case "request_processed":
			var ev types.AnalyticsEvent
			if err := json.Unmarshal(envelope.Payload, &ev); err != nil {
				continue
			}
			stats.totalRequests++
			stats.origTokens += ev.InputTokensOrig
			stats.compTokens += ev.InputTokensComp
			stats.outputTokens += ev.OutputTokens
			for _, l := range ev.Layers {
				switch l {
				case 1:
					stats.layer1Savings += ev.TokensSaved
				case 2:
					stats.layer2Savings += ev.TokensSaved
				case 3:
					stats.layer3Savings += ev.TokensSaved
				}
			}
			prov := ev.Provider.String()
			ps, ok := stats.byProvider[prov]
			if !ok {
				ps = &providerStats{}
				stats.byProvider[prov] = ps
			}
			ps.requests++
			ps.origTokens += ev.InputTokensOrig
			ps.compTokens += ev.InputTokensComp
			ps.saved += ev.InputTokensOrig - ev.InputTokensComp

			if stats.firstTS.IsZero() || envelope.Timestamp.Before(stats.firstTS) {
				stats.firstTS = envelope.Timestamp
			}
			if envelope.Timestamp.After(stats.lastTS) {
				stats.lastTS = envelope.Timestamp
			}

		case "secret_detected":
			stats.secretsFound++
		case "error_occurred":
			stats.errors++
		case "rate_limit_retry", "overflow_retry":
			stats.retries++
		}
	}

	if stats.totalRequests == 0 {
		fmt.Println("No request_processed events found in file.")
		return nil
	}

	totalSaved := stats.origTokens - stats.compTokens
	var ratio float64
	if stats.origTokens > 0 {
		ratio = float64(totalSaved) / float64(stats.origTokens) * 100
	}

	fmt.Printf("=== Session Report: %s ===\n", filepath.Base(path))
	if !stats.firstTS.IsZero() {
		fmt.Printf("Time range: %s to %s\n",
			stats.firstTS.Format("15:04:05"), stats.lastTS.Format("15:04:05"))
	}
	fmt.Printf("Total Requests:     %d\n", stats.totalRequests)
	fmt.Printf("Input Tokens (orig): %s\n", formatNum(stats.origTokens))
	fmt.Printf("Input Tokens (comp): %s\n", formatNum(stats.compTokens))
	fmt.Printf("Savings:            %s (%.1f%%)\n", formatNum(totalSaved), ratio)
	fmt.Printf("Output Tokens:      %s\n", formatNum(stats.outputTokens))
	fmt.Printf("Secrets Detected:   %d\n", stats.secretsFound)
	fmt.Printf("Errors:             %d\n", stats.errors)
	fmt.Printf("Retries:            %d\n", stats.retries)

	fmt.Println("\nPer Provider:")
	for _, prov := range sortedKeys(stats.byProvider) {
		ps := stats.byProvider[prov]
		var pRatio float64
		if ps.origTokens > 0 {
			pRatio = float64(ps.saved) / float64(ps.origTokens) * 100
		}
		fmt.Printf("  %-12s %d requests, %s saved (%.1f%%)\n",
			prov, ps.requests, formatNum(ps.saved), pRatio)
	}

	return nil
}

// --- decision-report: parses debug decision JSONL ---

func decisionReport(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var summaries []dbg.RequestSummary
	dec := json.NewDecoder(f)
	for dec.More() {
		var s dbg.RequestSummary
		if err := dec.Decode(&s); err != nil {
			continue
		}
		summaries = append(summaries, s)
	}

	if len(summaries) == 0 {
		fmt.Println("No request summaries found in file.")
		return nil
	}

	totalOrig, totalFinal := 0, 0
	subLayerTotals := make(map[string]int) // sub-layer name -> total saved tokens

	for _, s := range summaries {
		totalOrig += s.Tokens.Original
		totalFinal += s.Tokens.Final
		for name, bd := range s.Layer1Breakdown {
			subLayerTotals[name] += bd.Saved
		}
	}

	totalSaved := totalOrig - totalFinal
	var ratio float64
	if totalOrig > 0 {
		ratio = float64(totalSaved) / float64(totalOrig) * 100
	}

	fmt.Printf("=== Decision Report: %s ===\n", filepath.Base(path))
	fmt.Printf("Requests analyzed:  %d\n", len(summaries))
	fmt.Printf("Input Tokens (orig): %s\n", formatNum(totalOrig))
	fmt.Printf("Input Tokens (final): %s\n", formatNum(totalFinal))
	fmt.Printf("Total Savings:      %s (%.1f%%)\n", formatNum(totalSaved), ratio)

	if len(subLayerTotals) > 0 {
		fmt.Println("\nPer Sub-Layer Breakdown:")
		for _, name := range sortedStringKeys(subLayerTotals) {
			saved := subLayerTotals[name]
			var pct float64
			if totalSaved > 0 {
				pct = float64(saved) / float64(totalSaved) * 100
			}
			fmt.Printf("  %-25s %s tokens (%.1f%%)\n", name, formatNum(saved), pct)
		}
	}

	return nil
}

// --- filter-report: uses slimference gain infrastructure (requires filter.db) ---

func filterReport(path string) error {
	// The filter DB is SQLite. We can't import the filter package easily from scripts/.
	// Instead, use a lightweight approach: shell out to slimference if available.
	// For pure offline analysis, just note that `slimference gain all` provides this.
	fmt.Printf("=== Filter Report: %s ===\n", filepath.Base(path))
	fmt.Println("Use `slimference gain all --json` for Layer 0 savings from this database.")
	fmt.Println("Or: `slimference debug tail 50 --json` for recent filter runs.")
	return nil
}

// --- helpers ---

func formatNum(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func sortedKeys(m map[string]*providerStats) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// reduce compiler warnings for unused imports
var _ = strings.TrimSpace
