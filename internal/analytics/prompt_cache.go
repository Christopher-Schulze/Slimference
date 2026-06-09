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

type promptCacheCSVWriter interface {
	Write([]string) error
	Flush()
	Error() error
}

var newPromptCacheCSVWriter = func(w io.Writer) promptCacheCSVWriter {
	return csv.NewWriter(w)
}

// PromptCacheReport summarizes provider-reported prompt-cache activity from
// persisted analytics_event JSONL records.
type PromptCacheReport struct {
	Period             string  `json:"period"`
	TotalRequests      int     `json:"total_requests"`
	CacheReadRequests  int     `json:"cache_read_requests"`
	CacheReadTokens    int     `json:"cache_read_tokens"`
	CacheCreateTokens  int     `json:"cache_create_tokens"`
	EstimatedSavedRead int     `json:"estimated_saved_read_tokens"`
	HitRate            float64 `json:"hit_rate"`
}

type persistedPromptEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ReadPromptCacheReport aggregates persisted analytics events over the
// requested period. Supported periods: today, week, month, all.
func ReadPromptCacheReport(logDir, period string, now time.Time) (PromptCacheReport, error) {
	report := PromptCacheReport{Period: period}
	paths, err := promptCachePaths(logDir, period, now)
	if err != nil {
		return report, err
	}
	for _, path := range paths {
		if err := accumulatePromptCacheFile(path, &report); err != nil {
			return report, err
		}
	}
	if report.TotalRequests > 0 {
		report.HitRate = float64(report.CacheReadRequests) / float64(report.TotalRequests)
	}
	// Prompt-cache read tokens are treated as savings only after create tokens
	// have been paid back. This keeps reports conservative during warmup.
	report.EstimatedSavedRead = cacheReadDiscountEquivalent(report.CacheReadTokens, report.CacheCreateTokens)
	return report, nil
}

func promptCachePaths(logDir, period string, now time.Time) ([]string, error) {
	switch period {
	case "today":
		return []string{filepath.Join(logDir, now.Format(dateFormat)+".jsonl")}, nil
	case "week":
		paths := make([]string, 0, 7)
		for i := 0; i < 7; i++ {
			day := now.AddDate(0, 0, -i)
			paths = append(paths, filepath.Join(logDir, day.Format(dateFormat)+".jsonl"))
		}
		return paths, nil
	case "month":
		paths := make([]string, 0, 30)
		for i := 0; i < 30; i++ {
			day := now.AddDate(0, 0, -i)
			paths = append(paths, filepath.Join(logDir, day.Format(dateFormat)+".jsonl"))
		}
		return paths, nil
	case "all":
		entries, err := os.ReadDir(logDir)
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			paths = append(paths, filepath.Join(logDir, entry.Name()))
		}
		sort.Strings(paths)
		return paths, nil
	default:
		return nil, fmt.Errorf("unknown prompt-cache period: %s", period)
	}
}

func accumulatePromptCacheFile(path string, report *PromptCacheReport) error {
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
		if event.CacheReadTokens > 0 {
			report.CacheReadRequests++
			report.CacheReadTokens += event.CacheReadTokens
		}
		if event.CacheCreateTokens > 0 {
			report.CacheCreateTokens += event.CacheCreateTokens
		}
	}
	return scanner.Err()
}

// WritePromptCacheCSV renders a one-row CSV report for CLI export.
func WritePromptCacheCSV(w io.Writer, report PromptCacheReport) error {
	cw := newPromptCacheCSVWriter(w)
	if err := cw.Write([]string{
		"period",
		"total_requests",
		"cache_read_requests",
		"hit_rate_pct",
		"cache_read_tokens",
		"cache_create_tokens",
		"estimated_saved_read_tokens",
	}); err != nil {
		return err
	}
	if err := cw.Write([]string{
		report.Period,
		fmt.Sprintf("%d", report.TotalRequests),
		fmt.Sprintf("%d", report.CacheReadRequests),
		fmt.Sprintf("%.2f", report.HitRate*100),
		fmt.Sprintf("%d", report.CacheReadTokens),
		fmt.Sprintf("%d", report.CacheCreateTokens),
		fmt.Sprintf("%d", report.EstimatedSavedRead),
	}); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}
