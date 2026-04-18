// Session-report is a supporting file for the benchmarks command. It adds
// a second entry point that aggregates per-layer savings from a JSONL log
// of RequestSummary records and renders a human-readable report plus an
// optional Markdown snippet suitable for pasting into docs/benchmarks.md.
//
// This is the T34 scaffolding: the harness is real, the corpus is whatever
// the user points it at (their own debug JSONL, or tests/fixtures/sessions/).
// See scripts/benchmarks/README.md for the supported invocations.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	dbg "github.com/slimference/slimference/internal/debug"
)

// sessionReportAggregate holds the running totals we care about.
type sessionReportAggregate struct {
	requests       int
	origTokens     int64
	finalTokens    int64
	savedTokens    int64
	layer1Saved    int64
	layer2Saved    int64
	cacheHits      int
	perSubLayer    map[string]int64 // tokens saved by sub-layer name
	perProvider    map[string]int
	cacheReadSum   int64
	cacheCreateSum int64
}

func newSessionReportAggregate() *sessionReportAggregate {
	return &sessionReportAggregate{
		perSubLayer: map[string]int64{},
		perProvider: map[string]int{},
	}
}

// AggregateSessions reads rd line-by-line and accumulates
// RequestSummary statistics. A malformed line is skipped with a warning
// written to errOut.
func AggregateSessions(rd io.Reader, errOut io.Writer) (*sessionReportAggregate, error) {
	agg := newSessionReportAggregate()
	scanner := bufio.NewScanner(rd)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec dbg.RequestSummary
		if err := json.Unmarshal(line, &rec); err != nil {
			if errOut != nil {
				fmt.Fprintf(errOut, "warn: skipping malformed JSON: %v\n", err)
			}
			continue
		}
		agg.requests++
		agg.origTokens += int64(rec.Tokens.Original)
		agg.finalTokens += int64(rec.Tokens.Final)
		agg.savedTokens += int64(rec.Tokens.Saved)
		agg.layer1Saved += int64(rec.Tokens.Original - rec.Tokens.AfterLayer1)
		agg.layer2Saved += int64(rec.Tokens.AfterLayer1 - rec.Tokens.AfterLayer2)
		if rec.CacheHit {
			agg.cacheHits++
		}
		for name, bd := range rec.Layer1Breakdown {
			agg.perSubLayer[name] += int64(bd.Saved)
		}
		if rec.Provider != "" {
			agg.perProvider[rec.Provider]++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return agg, nil
}

// FormatSessionReport renders aggregate into a human-readable, monospaced
// block suitable for console output.
func FormatSessionReport(agg *sessionReportAggregate) string {
	if agg.requests == 0 {
		return "No session records found.\n"
	}
	var sb strings.Builder
	ratio := 0.0
	if agg.origTokens > 0 {
		ratio = float64(agg.savedTokens) / float64(agg.origTokens)
	}
	cacheHitRate := 0.0
	if agg.requests > 0 {
		cacheHitRate = float64(agg.cacheHits) / float64(agg.requests)
	}
	avgOrig := agg.origTokens / int64(agg.requests)
	avgSaved := agg.savedTokens / int64(agg.requests)

	sb.WriteString("Slimference session report\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Requests:           %d\n", agg.requests))
	sb.WriteString(fmt.Sprintf("Original tokens:    %d (avg %d / request)\n", agg.origTokens, avgOrig))
	sb.WriteString(fmt.Sprintf("Final tokens:       %d\n", agg.finalTokens))
	sb.WriteString(fmt.Sprintf("Saved tokens:       %d (avg %d / request)\n", agg.savedTokens, avgSaved))
	sb.WriteString(fmt.Sprintf("Savings ratio:      %.2f%%\n", ratio*100))
	sb.WriteString(fmt.Sprintf("Layer 1 saved:      %d\n", agg.layer1Saved))
	sb.WriteString(fmt.Sprintf("Layer 2 saved:      %d\n", agg.layer2Saved))
	sb.WriteString(fmt.Sprintf("Cache hit rate:     %.2f%% (%d / %d)\n", cacheHitRate*100, agg.cacheHits, agg.requests))

	if len(agg.perSubLayer) > 0 {
		sb.WriteString("\nLayer 1 sub-layer breakdown:\n")
		keys := make([]string, 0, len(agg.perSubLayer))
		for k := range agg.perSubLayer {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return agg.perSubLayer[keys[i]] > agg.perSubLayer[keys[j]]
		})
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("  %-28s %d\n", k, agg.perSubLayer[k]))
		}
	}

	if len(agg.perProvider) > 0 {
		sb.WriteString("\nPer-provider request count:\n")
		provKeys := make([]string, 0, len(agg.perProvider))
		for k := range agg.perProvider {
			provKeys = append(provKeys, k)
		}
		sort.Strings(provKeys)
		for _, k := range provKeys {
			sb.WriteString(fmt.Sprintf("  %-12s %d\n", k, agg.perProvider[k]))
		}
	}
	return sb.String()
}

// FormatSessionMarkdown produces a Markdown table suitable for
// `docs/benchmarks.md` so the numbers are reproducible and reviewable.
func FormatSessionMarkdown(agg *sessionReportAggregate) string {
	if agg.requests == 0 {
		return "_no session records_\n"
	}
	ratio := float64(agg.savedTokens) / math.Max(1, float64(agg.origTokens))
	var sb strings.Builder
	sb.WriteString("| Metric | Value |\n| --- | --- |\n")
	sb.WriteString(fmt.Sprintf("| Requests | %d |\n", agg.requests))
	sb.WriteString(fmt.Sprintf("| Original tokens | %d |\n", agg.origTokens))
	sb.WriteString(fmt.Sprintf("| Final tokens | %d |\n", agg.finalTokens))
	sb.WriteString(fmt.Sprintf("| Saved tokens | %d |\n", agg.savedTokens))
	sb.WriteString(fmt.Sprintf("| Savings ratio | %.2f%% |\n", ratio*100))
	sb.WriteString(fmt.Sprintf("| Layer 1 saved | %d |\n", agg.layer1Saved))
	sb.WriteString(fmt.Sprintf("| Layer 2 saved | %d |\n", agg.layer2Saved))
	sb.WriteString(fmt.Sprintf("| Cache hits | %d |\n", agg.cacheHits))
	return sb.String()
}

// sessionReportFromPath is the CLI helper used when the user runs
// `go run ./scripts/benchmarks session-report <file>`. It exits 1 on
// unreadable input.
func sessionReportFromPath(path, format string) int {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session-report: %v\n", err)
		return 1
	}
	defer f.Close()
	agg, err := AggregateSessions(f, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session-report: %v\n", err)
		return 1
	}
	switch format {
	case "markdown":
		fmt.Print(FormatSessionMarkdown(agg))
	default:
		fmt.Print(FormatSessionReport(agg))
	}
	return 0
}
