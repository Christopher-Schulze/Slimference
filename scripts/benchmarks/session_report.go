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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
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
	layer0Saved    int64
	layer1Saved    int64
	layer2Saved    int64
	layer3Saved    int64
	cacheHits      int
	perSubLayer    map[string]int64 // tokens saved by sub-layer name
	perProvider    map[string]int
	perCodexRoute  map[string]int
	cacheReadSum   int64
	cacheCreateSum int64
}

func newSessionReportAggregate() *sessionReportAggregate {
	return &sessionReportAggregate{
		perSubLayer:   map[string]int64{},
		perProvider:   map[string]int{},
		perCodexRoute: map[string]int{},
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
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(line, &raw)
		agg.requests++
		agg.origTokens += int64(rec.Tokens.Original)
		agg.finalTokens += int64(rec.Tokens.Final)
		agg.savedTokens += int64(rec.Tokens.Saved)
		agg.layer0Saved += positiveDelta(rec.Tokens.Original, rec.Tokens.AfterLayer0)
		agg.layer1Saved += positiveDelta(rec.Tokens.AfterLayer0, rec.Tokens.AfterLayer1)
		agg.layer2Saved += positiveDelta(rec.Tokens.AfterLayer1, rec.Tokens.AfterLayer2)
		agg.layer3Saved += positiveDelta(rec.Tokens.AfterLayer2, rec.Tokens.Final)
		if rec.CacheHit {
			agg.cacheHits++
		}
		agg.cacheReadSum += int64(rec.CacheReadTokens)
		agg.cacheCreateSum += int64(rec.CacheCreateTokens)
		for name, bd := range rec.Layer1Breakdown {
			agg.perSubLayer[name] += int64(bd.Saved)
		}
		if rec.Provider != "" {
			agg.perProvider[rec.Provider]++
		}
		if route := rawStringField(raw, "codex_route"); route != "" {
			agg.perCodexRoute[route]++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return agg, nil
}

func positiveDelta(before, after int) int64 {
	if before <= after {
		return 0
	}
	return int64(before - after)
}

func rawStringField(raw map[string]json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw[key], &value); err != nil {
		return ""
	}
	return value
}

func mergeSessionReportAggregate(dst, src *sessionReportAggregate) {
	dst.requests += src.requests
	dst.origTokens += src.origTokens
	dst.finalTokens += src.finalTokens
	dst.savedTokens += src.savedTokens
	dst.layer0Saved += src.layer0Saved
	dst.layer1Saved += src.layer1Saved
	dst.layer2Saved += src.layer2Saved
	dst.layer3Saved += src.layer3Saved
	dst.cacheHits += src.cacheHits
	dst.cacheReadSum += src.cacheReadSum
	dst.cacheCreateSum += src.cacheCreateSum
	for key, value := range src.perSubLayer {
		dst.perSubLayer[key] += value
	}
	for key, value := range src.perProvider {
		dst.perProvider[key] += value
	}
	for key, value := range src.perCodexRoute {
		dst.perCodexRoute[key] += value
	}
}

func AggregateSessionsFromPath(path string, errOut io.Writer) (*sessionReportAggregate, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return AggregateSessions(f, errOut)
	}

	agg := newSessionReportAggregate()
	err = filepath.WalkDir(path, func(p string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		child, err := AggregateSessions(bytes.NewReader(data), errOut)
		if err != nil {
			return err
		}
		mergeSessionReportAggregate(agg, child)
		return nil
	})
	if err != nil {
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
	sb.WriteString(fmt.Sprintf("Layer 0 saved:      %d\n", agg.layer0Saved))
	sb.WriteString(fmt.Sprintf("Layer 1 saved:      %d\n", agg.layer1Saved))
	sb.WriteString(fmt.Sprintf("Layer 2 saved:      %d\n", agg.layer2Saved))
	sb.WriteString(fmt.Sprintf("Layer 3 saved:      %d\n", agg.layer3Saved))
	sb.WriteString(fmt.Sprintf("Cache hit rate:     %.2f%% (%d / %d)\n", cacheHitRate*100, agg.cacheHits, agg.requests))
	if agg.cacheReadSum > 0 || agg.cacheCreateSum > 0 {
		sb.WriteString(fmt.Sprintf("Prompt cache read:  %d\n", agg.cacheReadSum))
		sb.WriteString(fmt.Sprintf("Prompt cache write: %d\n", agg.cacheCreateSum))
	}

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
	if len(agg.perCodexRoute) > 0 {
		sb.WriteString("\nCodex route count:\n")
		routeKeys := make([]string, 0, len(agg.perCodexRoute))
		for k := range agg.perCodexRoute {
			routeKeys = append(routeKeys, k)
		}
		sort.Strings(routeKeys)
		for _, k := range routeKeys {
			sb.WriteString(fmt.Sprintf("  %-28s %d\n", k, agg.perCodexRoute[k]))
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
	sb.WriteString(fmt.Sprintf("| Layer 0 saved | %d |\n", agg.layer0Saved))
	sb.WriteString(fmt.Sprintf("| Layer 1 saved | %d |\n", agg.layer1Saved))
	sb.WriteString(fmt.Sprintf("| Layer 2 saved | %d |\n", agg.layer2Saved))
	sb.WriteString(fmt.Sprintf("| Layer 3 saved | %d |\n", agg.layer3Saved))
	sb.WriteString(fmt.Sprintf("| Cache hits | %d |\n", agg.cacheHits))
	if agg.cacheReadSum > 0 || agg.cacheCreateSum > 0 {
		sb.WriteString(fmt.Sprintf("| Prompt cache read tokens | %d |\n", agg.cacheReadSum))
		sb.WriteString(fmt.Sprintf("| Prompt cache create tokens | %d |\n", agg.cacheCreateSum))
	}
	if len(agg.perProvider) > 0 {
		sb.WriteString("\n| Provider | Requests |\n| --- | ---: |\n")
		keys := make([]string, 0, len(agg.perProvider))
		for key := range agg.perProvider {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", key, agg.perProvider[key]))
		}
	}
	if len(agg.perCodexRoute) > 0 {
		sb.WriteString("\n| Codex route | Requests |\n| --- | ---: |\n")
		keys := make([]string, 0, len(agg.perCodexRoute))
		for key := range agg.perCodexRoute {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", key, agg.perCodexRoute[key]))
		}
	}
	return sb.String()
}

// sessionReportFromPath is the CLI helper used when the user runs
// `go run ./scripts/benchmarks session-report <file>`. It exits 1 on
// unreadable input. When path is a directory containing
// `codex-metadata.json`, the report is prefixed with a corpus-provenance
// block so reviewers see how the corpus was captured alongside the numbers.
func sessionReportFromPath(path, format string) int {
	agg, err := AggregateSessionsFromPath(path, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session-report: %v\n", err)
		return 1
	}
	var meta *CorpusMetadata
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		if loaded, ok, err := LoadCorpusMetadata(path); err != nil {
			fmt.Fprintf(os.Stderr, "session-report: %v\n", err)
			return 1
		} else if ok {
			meta = loaded
		}
	}
	switch format {
	case "markdown":
		if meta != nil {
			fmt.Print(FormatCorpusMetadataMarkdown(meta))
		}
		fmt.Print(FormatSessionMarkdown(agg))
	default:
		if meta != nil {
			fmt.Print(FormatCorpusMetadata(meta))
		}
		fmt.Print(FormatSessionReport(agg))
	}
	return 0
}
