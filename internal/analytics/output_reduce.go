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

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// OutputReduceReport summarizes persisted T130 output-discipline telemetry.
// It reports observable overhead and output volume only; it does not claim
// output-token savings without a live baseline.
type OutputReduceReport struct {
	Period                   string                   `json:"period"`
	TotalRequests            int                      `json:"total_requests"`
	AppliedRequests          int                      `json:"applied_requests"`
	SkippedRequests          int                      `json:"skipped_requests"`
	InputOverheadTokens      int                      `json:"input_overhead_tokens"`
	OutputTokensObserved     int                      `json:"output_tokens_observed"`
	AppliedOutputTokens      int                      `json:"applied_output_tokens"`
	AvgOutputTokens          float64                  `json:"avg_output_tokens"`
	AvgAppliedOutputTokens   float64                  `json:"avg_applied_output_tokens"`
	AvgInputOverheadPerApply float64                  `json:"avg_input_overhead_per_apply"`
	Profiles                 map[string]int           `json:"profiles,omitempty"`
	TaskShapes               map[string]int           `json:"task_shapes,omitempty"`
	Reasons                  map[string]int           `json:"reasons,omitempty"`
	ProfileRows              []OutputReduceProfileRow `json:"profile_rows,omitempty"`
	profileRows              map[string]*OutputReduceProfileRow
}

// OutputReduceProfileRow is the provider/model/profile/task-shape slice used
// for manual profile evolution. It reports observed volume and directive
// overhead only; it deliberately does not infer output-token savings.
type OutputReduceProfileRow struct {
	Provider                 string         `json:"provider"`
	Model                    string         `json:"model"`
	Profile                  string         `json:"profile"`
	TaskShape                string         `json:"task_shape"`
	Requests                 int            `json:"requests"`
	AppliedRequests          int            `json:"applied_requests"`
	SkippedRequests          int            `json:"skipped_requests"`
	InputOverheadTokens      int            `json:"input_overhead_tokens"`
	OutputTokensObserved     int            `json:"output_tokens_observed"`
	AppliedOutputTokens      int            `json:"applied_output_tokens"`
	AvgOutputTokens          float64        `json:"avg_output_tokens"`
	AvgInputOverheadPerApply float64        `json:"avg_input_overhead_per_apply"`
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
	report.finalizeOutputReduceProfileRows()
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
		report.observeOutputReduceProfileRow(event)
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
	for _, row := range report.ProfileRows {
		_ = cw.Write([]string{
			"profile_row",
			row.Provider,
			row.Model,
			row.Profile,
			row.TaskShape,
			fmt.Sprintf("%d", row.Requests),
			fmt.Sprintf("%d", row.AppliedRequests),
			fmt.Sprintf("%d", row.SkippedRequests),
			fmt.Sprintf("%d", row.InputOverheadTokens),
			fmt.Sprintf("%d", row.OutputTokensObserved),
			fmt.Sprintf("%d", row.AppliedOutputTokens),
			fmt.Sprintf("%.2f", row.AvgOutputTokens),
			fmt.Sprintf("%.2f", row.AvgInputOverheadPerApply),
		})
	}
	cw.Flush()
	return cw.Error()
}

func (report *OutputReduceReport) observeOutputReduceProfileRow(event types.AnalyticsEvent) {
	if !event.OutputReduceApplied && event.OutputReduceProfile == "" && event.OutputReduceTaskShape == "" && event.OutputReduceReason == "" {
		return
	}
	provider := event.Provider.String()
	model := normalizedOutputReduceDimension(event.Model, "unknown")
	profile := normalizedOutputReduceDimension(event.OutputReduceProfile, "none")
	shape := normalizedOutputReduceDimension(event.OutputReduceTaskShape, "unknown")
	key := provider + "\x00" + model + "\x00" + profile + "\x00" + shape
	if report.profileRows == nil {
		report.profileRows = make(map[string]*OutputReduceProfileRow)
	}
	row := report.profileRows[key]
	if row == nil {
		row = &OutputReduceProfileRow{
			Provider:  provider,
			Model:     model,
			Profile:   profile,
			TaskShape: shape,
		}
		report.profileRows[key] = row
	}
	row.Requests++
	row.OutputTokensObserved += event.OutputTokens
	if event.OutputReduceApplied {
		row.AppliedRequests++
		row.InputOverheadTokens += event.OutputReduceAddedTokens
		row.AppliedOutputTokens += event.OutputTokens
	} else if event.OutputReduceReason != "" {
		row.SkippedRequests++
	}
	if event.OutputReduceReason != "" {
		if row.Reasons == nil {
			row.Reasons = make(map[string]int)
		}
		row.Reasons[event.OutputReduceReason]++
	}
}

func (report *OutputReduceReport) finalizeOutputReduceProfileRows() {
	if len(report.profileRows) == 0 {
		report.ProfileRows = nil
		report.profileRows = nil
		return
	}
	report.ProfileRows = make([]OutputReduceProfileRow, 0, len(report.profileRows))
	for _, row := range report.profileRows {
		if row.Requests > 0 {
			row.AvgOutputTokens = float64(row.OutputTokensObserved) / float64(row.Requests)
		}
		if row.AppliedRequests > 0 {
			row.AvgInputOverheadPerApply = float64(row.InputOverheadTokens) / float64(row.AppliedRequests)
		}
		if len(row.Reasons) == 0 {
			row.Reasons = nil
		}
		report.ProfileRows = append(report.ProfileRows, *row)
	}
	sort.Slice(report.ProfileRows, func(i, j int) bool {
		return lessOutputReduceProfileRow(report.ProfileRows[i], report.ProfileRows[j])
	})
	report.profileRows = nil
}

func lessOutputReduceProfileRow(a, b OutputReduceProfileRow) bool {
	if a.Requests != b.Requests {
		return a.Requests > b.Requests
	}
	if a.AppliedRequests != b.AppliedRequests {
		return a.AppliedRequests > b.AppliedRequests
	}
	if a.OutputTokensObserved != b.OutputTokensObserved {
		return a.OutputTokensObserved > b.OutputTokensObserved
	}
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	if a.Model != b.Model {
		return a.Model < b.Model
	}
	if a.TaskShape != b.TaskShape {
		return a.TaskShape < b.TaskShape
	}
	return a.Profile < b.Profile
}

func normalizedOutputReduceDimension(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
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
