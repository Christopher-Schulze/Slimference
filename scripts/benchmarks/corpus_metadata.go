package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const corpusMetadataFilename = "codex-metadata.json"

// CorpusMetadata describes a Codex evidence corpus directory. The file is
// optional; when present it lets `session-report` render context about the
// corpus (Codex version, scrubbing method, scenarios) and lets
// `codex-smoke-gate` enforce a regression baseline against it.
type CorpusMetadata struct {
	SchemaVersion   int                   `json:"schema_version"`
	CorpusName      string                `json:"corpus_name"`
	Description     string                `json:"description"`
	Scrubbed        bool                  `json:"scrubbed"`
	RedactionMethod string                `json:"redaction_method"`
	CapturedAt      string                `json:"captured_at"`
	RequestFixtures []RequestFixtureEntry `json:"request_fixtures"`
	SessionFixtures []SessionFixtureEntry `json:"session_fixtures"`
	RegressionGate  *RegressionGate       `json:"regression_gate"`
}

type RequestFixtureEntry struct {
	File  string `json:"file"`
	Route string `json:"route"`
	Shape string `json:"shape"`
	Notes string `json:"notes"`
}

type SessionFixtureEntry struct {
	File          string   `json:"file"`
	CodexVersion  string   `json:"codex_version"`
	Client        string   `json:"client"`
	HooksEnabled  []string `json:"hooks_enabled"`
	LayersEnabled []int    `json:"layers_enabled"`
	RequestCount  int      `json:"request_count"`
	Scenarios     []string `json:"scenarios"`
}

type RegressionGate struct {
	MinRequests     int            `json:"min_requests"`
	MinSavingsRatio float64        `json:"min_savings_ratio"`
	MinLayer0Saved  int64          `json:"min_layer0_saved"`
	MinLayer1Saved  int64          `json:"min_layer1_saved"`
	MinLayer3Saved  int64          `json:"min_layer3_saved"`
	Providers       map[string]int `json:"providers"`
	Routes          map[string]int `json:"routes"`
}

// LoadCorpusMetadata reads `<dir>/codex-metadata.json` if present. The boolean
// return distinguishes "absent" from "found and parsed". Absence is not an
// error; callers decide whether metadata is required for their use case.
func LoadCorpusMetadata(dir string) (*CorpusMetadata, bool, error) {
	path := filepath.Join(dir, corpusMetadataFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var meta CorpusMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return &meta, true, nil
}

// FormatCorpusMetadata renders a short header block to attach in front of a
// session report so reviewers see corpus provenance alongside the numbers.
func FormatCorpusMetadata(meta *CorpusMetadata) string {
	if meta == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Corpus metadata\n")
	sb.WriteString(strings.Repeat("-", 60))
	sb.WriteString("\n")
	if meta.CorpusName != "" {
		sb.WriteString(fmt.Sprintf("Name:               %s\n", meta.CorpusName))
	}
	if meta.SchemaVersion > 0 {
		sb.WriteString(fmt.Sprintf("Schema:             v%d\n", meta.SchemaVersion))
	}
	sb.WriteString(fmt.Sprintf("Scrubbed:           %t\n", meta.Scrubbed))
	if meta.RedactionMethod != "" {
		sb.WriteString(fmt.Sprintf("Redaction:          %s\n", meta.RedactionMethod))
	}
	if meta.CapturedAt != "" {
		sb.WriteString(fmt.Sprintf("Captured:           %s\n", meta.CapturedAt))
	}
	if meta.Description != "" {
		sb.WriteString("Description:        ")
		sb.WriteString(meta.Description)
		sb.WriteString("\n")
	}
	if len(meta.SessionFixtures) > 0 {
		sb.WriteString("\nSession fixtures:\n")
		for _, sf := range meta.SessionFixtures {
			sb.WriteString(fmt.Sprintf("  - %s\n", sf.File))
			if sf.CodexVersion != "" {
				sb.WriteString(fmt.Sprintf("      codex_version: %s\n", sf.CodexVersion))
			}
			if sf.Client != "" {
				sb.WriteString(fmt.Sprintf("      client:        %s\n", sf.Client))
			}
			if len(sf.HooksEnabled) > 0 {
				sb.WriteString(fmt.Sprintf("      hooks:         %s\n", strings.Join(sf.HooksEnabled, ",")))
			}
			if len(sf.LayersEnabled) > 0 {
				layerStrs := make([]string, len(sf.LayersEnabled))
				for i, l := range sf.LayersEnabled {
					layerStrs[i] = fmt.Sprintf("L%d", l)
				}
				sb.WriteString(fmt.Sprintf("      layers:        %s\n", strings.Join(layerStrs, ",")))
			}
			if sf.RequestCount > 0 {
				sb.WriteString(fmt.Sprintf("      requests:      %d\n", sf.RequestCount))
			}
			if len(sf.Scenarios) > 0 {
				sb.WriteString(fmt.Sprintf("      scenarios:     %s\n", strings.Join(sf.Scenarios, ", ")))
			}
		}
	}
	if len(meta.RequestFixtures) > 0 {
		sb.WriteString("\nRequest fixtures:\n")
		for _, rf := range meta.RequestFixtures {
			sb.WriteString(fmt.Sprintf("  - %s (route=%s, shape=%s)\n", rf.File, rf.Route, rf.Shape))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// FormatCorpusMetadataMarkdown renders the same provenance block as a Markdown
// section so it can be pasted into docs/benchmarks.md alongside the numbers.
func FormatCorpusMetadataMarkdown(meta *CorpusMetadata) string {
	if meta == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("### Corpus metadata\n\n")
	if meta.CorpusName != "" {
		sb.WriteString(fmt.Sprintf("- **Name:** %s\n", meta.CorpusName))
	}
	if meta.SchemaVersion > 0 {
		sb.WriteString(fmt.Sprintf("- **Schema:** v%d\n", meta.SchemaVersion))
	}
	sb.WriteString(fmt.Sprintf("- **Scrubbed:** %t\n", meta.Scrubbed))
	if meta.RedactionMethod != "" {
		sb.WriteString(fmt.Sprintf("- **Redaction:** %s\n", meta.RedactionMethod))
	}
	if meta.CapturedAt != "" {
		sb.WriteString(fmt.Sprintf("- **Captured:** %s\n", meta.CapturedAt))
	}
	if meta.Description != "" {
		sb.WriteString(fmt.Sprintf("- **Description:** %s\n", meta.Description))
	}
	if len(meta.SessionFixtures) > 0 {
		sb.WriteString("\n| Session fixture | Codex version | Hooks | Layers | Requests |\n")
		sb.WriteString("| --- | --- | --- | --- | ---: |\n")
		for _, sf := range meta.SessionFixtures {
			layerStrs := make([]string, len(sf.LayersEnabled))
			for i, l := range sf.LayersEnabled {
				layerStrs[i] = fmt.Sprintf("L%d", l)
			}
			sb.WriteString(fmt.Sprintf(
				"| %s | %s | %s | %s | %d |\n",
				sf.File,
				strOrDash(sf.CodexVersion),
				strOrDash(strings.Join(sf.HooksEnabled, ",")),
				strOrDash(strings.Join(layerStrs, ",")),
				sf.RequestCount,
			))
		}
	}
	if len(meta.RequestFixtures) > 0 {
		sb.WriteString("\n| Request fixture | Route | Shape |\n| --- | --- | --- |\n")
		for _, rf := range meta.RequestFixtures {
			sb.WriteString(fmt.Sprintf(
				"| %s | %s | %s |\n",
				rf.File,
				strOrDash(rf.Route),
				strOrDash(rf.Shape),
			))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

func strOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// EvaluateRegressionGate compares an aggregate against a metadata gate and
// returns the list of failure messages. An empty slice means the gate passes.
// A nil gate is treated as "no gate configured" and returns nil.
func EvaluateRegressionGate(agg *sessionReportAggregate, gate *RegressionGate) []string {
	if gate == nil {
		return nil
	}
	var failures []string
	if gate.MinRequests > 0 && agg.requests < gate.MinRequests {
		failures = append(failures, fmt.Sprintf("requests=%d < min=%d", agg.requests, gate.MinRequests))
	}
	if gate.MinSavingsRatio > 0 {
		ratio := 0.0
		if agg.origTokens > 0 {
			ratio = float64(agg.savedTokens) / float64(agg.origTokens)
		}
		if ratio+1e-9 < gate.MinSavingsRatio {
			failures = append(failures, fmt.Sprintf("savings_ratio=%.4f < min=%.4f", ratio, gate.MinSavingsRatio))
		}
	}
	if gate.MinLayer0Saved > 0 && agg.layer0Saved < gate.MinLayer0Saved {
		failures = append(failures, fmt.Sprintf("layer0_saved=%d < min=%d", agg.layer0Saved, gate.MinLayer0Saved))
	}
	if gate.MinLayer1Saved > 0 && agg.layer1Saved < gate.MinLayer1Saved {
		failures = append(failures, fmt.Sprintf("layer1_saved=%d < min=%d", agg.layer1Saved, gate.MinLayer1Saved))
	}
	if gate.MinLayer3Saved > 0 && agg.layer3Saved < gate.MinLayer3Saved {
		failures = append(failures, fmt.Sprintf("layer3_saved=%d < min=%d", agg.layer3Saved, gate.MinLayer3Saved))
	}
	if len(gate.Providers) > 0 {
		keys := make([]string, 0, len(gate.Providers))
		for k := range gate.Providers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			want := gate.Providers[key]
			if got := agg.perProvider[key]; got < want {
				failures = append(failures, fmt.Sprintf("provider[%s]=%d < min=%d", key, got, want))
			}
		}
	}
	if len(gate.Routes) > 0 {
		keys := make([]string, 0, len(gate.Routes))
		for k := range gate.Routes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			want := gate.Routes[key]
			if got := agg.perCodexRoute[key]; got < want {
				failures = append(failures, fmt.Sprintf("route[%s]=%d < min=%d", key, got, want))
			}
		}
	}
	return failures
}

// codexSmokeGate aggregates the corpus directory, evaluates the metadata's
// regression gate, and returns a CLI exit code. It is the single source of
// truth used by both the standalone `codex-smoke-gate` subcommand and the
// `scripts/ci` regression step.
func codexSmokeGate(dir string, stdout, stderr io.Writer) int {
	meta, ok, err := LoadCorpusMetadata(dir)
	if err != nil {
		fmt.Fprintf(stderr, "codex-smoke-gate: load metadata: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(stderr, "codex-smoke-gate: missing %s in %s\n", corpusMetadataFilename, dir)
		return 1
	}
	if meta.RegressionGate == nil {
		fmt.Fprintf(stderr, "codex-smoke-gate: %s has no regression_gate block\n", corpusMetadataFilename)
		return 1
	}
	agg, err := AggregateSessionsFromPath(dir, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "codex-smoke-gate: aggregate: %v\n", err)
		return 1
	}
	failures := EvaluateRegressionGate(agg, meta.RegressionGate)
	if len(failures) > 0 {
		fmt.Fprintf(stdout, "codex-smoke-gate: FAIL on %s\n", dir)
		for _, f := range failures {
			fmt.Fprintf(stdout, "  - %s\n", f)
		}
		return 1
	}
	fmt.Fprintf(stdout, "codex-smoke-gate: PASS on %s (requests=%d, ratio=%.2f%%)\n",
		dir, agg.requests, savingsRatioPct(agg))
	return 0
}

func savingsRatioPct(agg *sessionReportAggregate) float64 {
	if agg.origTokens <= 0 {
		return 0
	}
	return float64(agg.savedTokens) / float64(agg.origTokens) * 100
}
