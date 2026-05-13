package analytics

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/types"
)

// OutputReduceReport summarizes persisted T130 output-discipline telemetry.
// It reports observable overhead and output volume only; it does not claim
// output-token savings without a live baseline.
type OutputReduceReport struct {
	Period                   string         `json:"period"`
	TotalRequests            int            `json:"total_requests"`
	AppliedRequests          int            `json:"applied_requests"`
	SkippedRequests          int            `json:"skipped_requests"`
	InputOverheadTokens      int            `json:"input_overhead_tokens"`
	OutputTokensObserved     int            `json:"output_tokens_observed"`
	AppliedOutputTokens      int            `json:"applied_output_tokens"`
	AvgOutputTokens          float64        `json:"avg_output_tokens"`
	AvgAppliedOutputTokens   float64        `json:"avg_applied_output_tokens"`
	AvgInputOverheadPerApply float64        `json:"avg_input_overhead_per_apply"`
	Profiles                 map[string]int `json:"profiles,omitempty"`
	TaskShapes               map[string]int `json:"task_shapes,omitempty"`
	Reasons                  map[string]int `json:"reasons,omitempty"`
}

// ReadOutputReduceReport aggregates persisted analytics_event rows for the
// requested period. Supported periods: today, week, month, all.
func ReadOutputReduceReport(logDir, period string, now time.Time) (OutputReduceReport, error) {
	report := OutputReduceReport{
		Period:     period,
		Profiles:   make(map[string]int),
		TaskShapes: make(map[string]int),
		Reasons:    make(map[string]int),
	}
	paths, err := promptCachePaths(logDir, period, now)
	if err != nil {
		return report, err
	}
	for _, path := range paths {
		if err := accumulateOutputReduceFile(path, &report); err != nil {
			return report, err
		}
	}
	if report.TotalRequests > 0 {
		report.AvgOutputTokens = float64(report.OutputTokensObserved) / float64(report.TotalRequests)
	}
	if report.AppliedRequests > 0 {
		report.AvgAppliedOutputTokens = float64(report.AppliedOutputTokens) / float64(report.AppliedRequests)
		report.AvgInputOverheadPerApply = float64(report.InputOverheadTokens) / float64(report.AppliedRequests)
	}
	if len(report.Profiles) == 0 {
		report.Profiles = nil
	}
	if len(report.TaskShapes) == 0 {
		report.TaskShapes = nil
	}
	if len(report.Reasons) == 0 {
		report.Reasons = nil
	}
	return report, nil
}

func accumulateOutputReduceFile(path string, report *OutputReduceReport) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close() //nolint:errcheck

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var env persistedPromptEnvelope
		if err := json.Unmarshal(line, &env); err != nil || env.Type != "analytics_event" {
			continue
		}
		var event types.AnalyticsEvent
		if err := json.Unmarshal(env.Payload, &event); err != nil || event.Type != types.EventRequestProcessed {
			continue
		}
		report.TotalRequests++
		report.OutputTokensObserved += event.OutputTokens
		if event.OutputReduceApplied {
			report.AppliedRequests++
			report.InputOverheadTokens += event.OutputReduceAddedTokens
			report.AppliedOutputTokens += event.OutputTokens
		} else if event.OutputReduceReason != "" {
			report.SkippedRequests++
		}
		if event.OutputReduceProfile != "" {
			report.Profiles[event.OutputReduceProfile]++
		}
		if event.OutputReduceTaskShape != "" {
			report.TaskShapes[event.OutputReduceTaskShape]++
		}
		if event.OutputReduceReason != "" {
			report.Reasons[event.OutputReduceReason]++
		}
	}
	return scanner.Err()
}

// WriteOutputReduceCSV renders a one-row summary followed by profile and reason
// rows. Savings are intentionally absent until a real baseline exists.
func WriteOutputReduceCSV(w io.Writer, report OutputReduceReport) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"period",
		"total_requests",
		"applied_requests",
		"skipped_requests",
		"input_overhead_tokens",
		"output_tokens_observed",
		"applied_output_tokens",
		"avg_output_tokens",
		"avg_applied_output_tokens",
		"avg_input_overhead_per_apply",
	})
	_ = cw.Write([]string{
		report.Period,
		fmt.Sprintf("%d", report.TotalRequests),
		fmt.Sprintf("%d", report.AppliedRequests),
		fmt.Sprintf("%d", report.SkippedRequests),
		fmt.Sprintf("%d", report.InputOverheadTokens),
		fmt.Sprintf("%d", report.OutputTokensObserved),
		fmt.Sprintf("%d", report.AppliedOutputTokens),
		fmt.Sprintf("%.2f", report.AvgOutputTokens),
		fmt.Sprintf("%.2f", report.AvgAppliedOutputTokens),
		fmt.Sprintf("%.2f", report.AvgInputOverheadPerApply),
	})
	for _, key := range sortedOutputReduceKeys(report.Profiles) {
		_ = cw.Write([]string{"profile", key, fmt.Sprintf("%d", report.Profiles[key])})
	}
	for _, key := range sortedOutputReduceKeys(report.TaskShapes) {
		_ = cw.Write([]string{"task_shape", key, fmt.Sprintf("%d", report.TaskShapes[key])})
	}
	for _, key := range sortedOutputReduceKeys(report.Reasons) {
		_ = cw.Write([]string{"reason", key, fmt.Sprintf("%d", report.Reasons[key])})
	}
	cw.Flush()
	return cw.Error()
}

func sortedOutputReduceKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// OutputReduceLogPath returns the analytics JSONL path used by tests and
// diagnostics for a concrete day.
func OutputReduceLogPath(logDir string, day time.Time) string {
	return filepath.Join(logDir, day.Format(dateFormat)+".jsonl")
}
