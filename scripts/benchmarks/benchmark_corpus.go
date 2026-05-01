// benchmark_corpus.go drives the per-category live-corpus regression
// gate. It walks `<root>/<category>/{*.jsonl, metadata.json}`, aggregates
// each category through the existing session-report aggregator, and
// compares the resulting savings ratio plus per-layer breakdowns against
// declared expectations in `metadata.json`. Used both as the standalone
// `benchmark-corpus` subcommand and inside `scripts/ci`.
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

const corpusCategoryMetadataFilename = "metadata.json"

// CategoryMetadata is the minimal description a maintainer commits next
// to a corpus category so the gate has expectations to measure against.
// Only ExpectedSavingsMin is mandatory for the gate; everything else is
// human context that gets rendered in reports.
type CategoryMetadata struct {
	Category               string  `json:"category"`
	Description            string  `json:"description"`
	Synthetic              bool    `json:"synthetic"`
	Language               string  `json:"language"`
	ToolMix                string  `json:"tool_mix"`
	ExpectedSavingsMin     float64 `json:"expected_savings_min"`
	ExpectedSavingsMax     float64 `json:"expected_savings_max"`
	ExpectedRequestCount   int     `json:"expected_request_count"`
	ExpectedLayer2Optional bool    `json:"expected_layer2_optional"`
	Notes                  string  `json:"notes"`
}

// CategoryResult is the per-category outcome of one gate evaluation.
type CategoryResult struct {
	Category       string            `json:"category"`
	Path           string            `json:"path"`
	Sessions       int               `json:"sessions"`
	Requests       int               `json:"requests"`
	OrigTokens     int64             `json:"orig_tokens"`
	SavedTokens    int64             `json:"saved_tokens"`
	SavingsRatio   float64           `json:"savings_ratio"`
	Layer0Saved    int64             `json:"layer0_saved"`
	Layer1Saved    int64             `json:"layer1_saved"`
	Layer2Saved    int64             `json:"layer2_saved"`
	Layer3Saved    int64             `json:"layer3_saved"`
	Synthetic      bool              `json:"synthetic"`
	Failures       []string          `json:"failures,omitempty"`
	GateConfigured bool              `json:"gate_configured"`
	Metadata       *CategoryMetadata `json:"metadata,omitempty"`
}

// CorpusReport is the aggregate of all categories.
type CorpusReport struct {
	Root          string           `json:"root"`
	Categories    []CategoryResult `json:"categories"`
	TotalRequests int              `json:"total_requests"`
	OverallRatio  float64          `json:"overall_savings_ratio"`
	HasSynthetic  bool             `json:"has_synthetic"`
	HasReal       bool             `json:"has_real"`
}

// LoadCategoryMetadata reads `<dir>/metadata.json` and returns it. The
// presence of the file is mandatory for a directory to count as a corpus
// category; absence causes a friendly error so reviewers can spot the
// gap quickly.
func LoadCategoryMetadata(dir string) (*CategoryMetadata, error) {
	path := filepath.Join(dir, corpusCategoryMetadataFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("category %s: missing %s", filepath.Base(dir), corpusCategoryMetadataFilename)
		}
		return nil, err
	}
	var meta CategoryMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if meta.Category == "" {
		meta.Category = filepath.Base(dir)
	}
	return &meta, nil
}

// EvaluateCategory aggregates all jsonl files under the directory and
// returns a CategoryResult plus the gate failures.
func EvaluateCategory(dir string, errOut io.Writer) (CategoryResult, error) {
	meta, err := LoadCategoryMetadata(dir)
	if err != nil {
		return CategoryResult{}, err
	}
	agg, err := AggregateSessionsFromPath(dir, errOut)
	if err != nil {
		return CategoryResult{}, fmt.Errorf("aggregate %s: %w", dir, err)
	}
	sessions, err := countSessionFiles(dir)
	if err != nil {
		return CategoryResult{}, err
	}
	ratio := 0.0
	if agg.origTokens > 0 {
		ratio = float64(agg.savedTokens) / float64(agg.origTokens)
	}
	res := CategoryResult{
		Category:       meta.Category,
		Path:           dir,
		Sessions:       sessions,
		Requests:       agg.requests,
		OrigTokens:     agg.origTokens,
		SavedTokens:    agg.savedTokens,
		SavingsRatio:   ratio,
		Layer0Saved:    agg.layer0Saved,
		Layer1Saved:    agg.layer1Saved,
		Layer2Saved:    agg.layer2Saved,
		Layer3Saved:    agg.layer3Saved,
		Synthetic:      meta.Synthetic,
		GateConfigured: meta.ExpectedSavingsMin > 0 || meta.ExpectedRequestCount > 0,
		Metadata:       meta,
	}
	res.Failures = evaluateCategoryGate(res, meta)
	return res, nil
}

func countSessionFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".jsonl" {
			count++
		}
	}
	return count, nil
}

func evaluateCategoryGate(res CategoryResult, meta *CategoryMetadata) []string {
	var failures []string
	if meta.ExpectedRequestCount > 0 && res.Requests < meta.ExpectedRequestCount {
		failures = append(failures, fmt.Sprintf("requests=%d < expected=%d", res.Requests, meta.ExpectedRequestCount))
	}
	if meta.ExpectedSavingsMin > 0 {
		if res.SavingsRatio+1e-9 < meta.ExpectedSavingsMin {
			failures = append(failures, fmt.Sprintf("savings_ratio=%.4f < min=%.4f", res.SavingsRatio, meta.ExpectedSavingsMin))
		}
	}
	if meta.ExpectedSavingsMax > 0 && res.SavingsRatio > meta.ExpectedSavingsMax+1e-9 {
		failures = append(failures, fmt.Sprintf("savings_ratio=%.4f > max=%.4f (suspicious overcount)", res.SavingsRatio, meta.ExpectedSavingsMax))
	}
	return failures
}

// EvaluateCorpus walks the root directory and produces a CorpusReport.
// Categories are detected as immediate subdirectories that contain a
// `metadata.json`. Subdirectories without one are ignored with a warning
// to errOut so a maintainer who forgot the metadata file sees the hint.
func EvaluateCorpus(root string, errOut io.Writer) (CorpusReport, error) {
	report := CorpusReport{Root: root}
	entries, err := os.ReadDir(root)
	if err != nil {
		return report, fmt.Errorf("read corpus root: %w", err)
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, e.Name())
	}
	sort.Strings(dirs)

	var totalOrig, totalSaved int64
	for _, name := range dirs {
		dir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(dir, corpusCategoryMetadataFilename)); err != nil {
			if errOut != nil {
				fmt.Fprintf(errOut, "warn: skipping %s (no %s)\n", dir, corpusCategoryMetadataFilename)
			}
			continue
		}
		res, err := EvaluateCategory(dir, errOut)
		if err != nil {
			if errOut != nil {
				fmt.Fprintf(errOut, "warn: %v\n", err)
			}
			continue
		}
		report.Categories = append(report.Categories, res)
		report.TotalRequests += res.Requests
		totalOrig += res.OrigTokens
		totalSaved += res.SavedTokens
		if res.Synthetic {
			report.HasSynthetic = true
		} else {
			report.HasReal = true
		}
	}
	if totalOrig > 0 {
		report.OverallRatio = float64(totalSaved) / float64(totalOrig)
	}
	return report, nil
}

// FormatCorpusReport renders the corpus report as a monospaced block.
func FormatCorpusReport(report CorpusReport) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Live corpus report: %s\n", report.Root))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n")
	if len(report.Categories) == 0 {
		sb.WriteString("No categories found (each subdir needs metadata.json).\n")
		return sb.String()
	}
	for _, c := range report.Categories {
		tag := ""
		if c.Synthetic {
			tag = " [synthetic]"
		}
		sb.WriteString(fmt.Sprintf("\n--- %s%s ---\n", c.Category, tag))
		sb.WriteString(fmt.Sprintf("  sessions:     %d\n", c.Sessions))
		sb.WriteString(fmt.Sprintf("  requests:     %d\n", c.Requests))
		sb.WriteString(fmt.Sprintf("  orig tokens:  %d\n", c.OrigTokens))
		sb.WriteString(fmt.Sprintf("  saved tokens: %d\n", c.SavedTokens))
		sb.WriteString(fmt.Sprintf("  ratio:        %.2f%%\n", c.SavingsRatio*100))
		sb.WriteString(fmt.Sprintf("  L0/L1/L2/L3:  %d / %d / %d / %d\n", c.Layer0Saved, c.Layer1Saved, c.Layer2Saved, c.Layer3Saved))
		if c.GateConfigured {
			if len(c.Failures) == 0 {
				sb.WriteString("  gate:         PASS\n")
			} else {
				sb.WriteString("  gate:         FAIL\n")
				for _, f := range c.Failures {
					sb.WriteString(fmt.Sprintf("    - %s\n", f))
				}
			}
		} else {
			sb.WriteString("  gate:         (no expectations declared)\n")
		}
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", 60))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Total requests: %d\n", report.TotalRequests))
	sb.WriteString(fmt.Sprintf("Overall ratio:  %.2f%%\n", report.OverallRatio*100))
	if report.HasSynthetic && !report.HasReal {
		sb.WriteString("\nNOTE: corpus is synthetic-only. See docs/live-corpus-policy.md for the\n")
		sb.WriteString("operator-driven path to a real-session corpus (T118b).\n")
	}
	return sb.String()
}

// CorpusGate runs EvaluateCorpus and treats any per-category failure as
// a non-zero exit code; used as the CI hook for the live-corpus
// regression check. When the corpus is empty (no categories found),
// CorpusGate exits non-zero so the gap is visible.
func CorpusGate(root string, stdout, stderr io.Writer) int {
	report, err := EvaluateCorpus(root, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "benchmark-corpus: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, FormatCorpusReport(report))
	if len(report.Categories) == 0 {
		fmt.Fprintf(stderr, "benchmark-corpus: corpus root %s has no categories\n", root)
		return 1
	}
	failed := false
	for _, c := range report.Categories {
		if len(c.Failures) > 0 {
			failed = true
		}
	}
	if failed {
		fmt.Fprintf(stdout, "benchmark-corpus: FAIL on %s\n", root)
		return 1
	}
	fmt.Fprintf(stdout, "benchmark-corpus: PASS on %s\n", root)
	return 0
}

// CorpusReportJSON renders the report as canonical JSON for machine use.
func CorpusReportJSON(report CorpusReport) (string, error) {
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// runBenchmarkCorpus is the CLI entrypoint hooked from main.go.
func runBenchmarkCorpus(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: benchmark-corpus <corpus-root> [--check] [--json]")
		return 2
	}
	check := false
	jsonOut := false
	var root string
	for _, a := range args {
		switch a {
		case "--check":
			check = true
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(a, "--") {
				fmt.Fprintf(os.Stderr, "unknown flag %q\n", a)
				return 2
			}
			if root != "" {
				fmt.Fprintln(os.Stderr, "benchmark-corpus takes a single root argument")
				return 2
			}
			root = a
		}
	}
	if root == "" {
		fmt.Fprintln(os.Stderr, "benchmark-corpus: corpus root required")
		return 2
	}
	if check {
		return CorpusGate(root, os.Stdout, os.Stderr)
	}
	report, err := EvaluateCorpus(root, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark-corpus: %v\n", err)
		return 1
	}
	if jsonOut {
		s, err := CorpusReportJSON(report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchmark-corpus: %v\n", err)
			return 1
		}
		fmt.Print(s)
		return 0
	}
	fmt.Print(FormatCorpusReport(report))
	return 0
}
