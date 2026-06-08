package analytics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/types"
)

func TestPromptCachePathsAndEdgeCases(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"2026-04-19.jsonl", "2026-04-18.jsonl", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if paths, err := promptCachePaths(logDir, "today", now); err != nil || len(paths) != 1 {
		t.Fatalf("today paths=%v err=%v", paths, err)
	}
	if paths, err := promptCachePaths(logDir, "week", now); err != nil || len(paths) != 7 {
		t.Fatalf("week paths len=%d err=%v", len(paths), err)
	}
	if paths, err := promptCachePaths(logDir, "month", now); err != nil || len(paths) != 30 {
		t.Fatalf("month paths len=%d err=%v", len(paths), err)
	}
	if paths, err := promptCachePaths(logDir, "all", now); err != nil || len(paths) != 2 {
		t.Fatalf("all paths=%v err=%v", paths, err)
	}
	if _, err := promptCachePaths(logDir, "bad", now); err == nil {
		t.Fatal("expected invalid period error")
	}
}

func TestPromptCacheReportAndCSVErrorPaths(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(logDir, now.Format(dateFormat)+".jsonl")
	line, err := json.Marshal(persistedPromptEnvelope{
		Type: "analytics_event",
		Payload: mustJSON(t, types.AnalyticsEvent{
			Type:              types.EventRequestProcessed,
			CacheReadTokens:   10,
			CacheCreateTokens: 5,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	content := "\n{\n" + string(line) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := ReadPromptCacheReport(logDir, "today", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalRequests != 1 || report.CacheReadRequests != 1 || report.EstimatedSavedRead != 4 {
		t.Fatalf("report=%+v", report)
	}

	report, err = ReadPromptCacheReport(t.TempDir(), "today", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalRequests != 0 {
		t.Fatalf("missing dir report=%+v", report)
	}

	var buf bytes.Buffer
	if err := WritePromptCacheCSV(&buf, PromptCacheReport{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "period,total_requests") {
		t.Fatalf("csv=%q", buf.String())
	}
}

func TestPromptCacheHitRateZeroAndNonZero(t *testing.T) {
	t.Parallel()

	if got := (AnalyticsSnapshot{}).PromptCacheHitRate(); got != 0 {
		t.Fatalf("zero hit rate=%v", got)
	}
	if got := (AnalyticsSnapshot{TotalRequests: 4, PromptCacheReadRequests: 1}).PromptCacheHitRate(); got != 0.25 {
		t.Fatalf("nonzero hit rate=%v", got)
	}
}

type promptCacheErrWriter struct{}

func (promptCacheErrWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPromptCacheReportAndCSVAdditionalErrors(t *testing.T) {
	t.Parallel()

	logDirFile := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(logDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPromptCacheReport(logDirFile, "all", time.Now()); err == nil {
		t.Fatal("expected all-period read dir error")
	}
	if err := WritePromptCacheCSV(promptCacheErrWriter{}, PromptCacheReport{Period: "today"}); err == nil {
		t.Fatal("expected csv writer error")
	}

	notDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPromptCacheReport(notDir, "today", time.Now()); err == nil {
		t.Fatal("expected prompt-cache accumulate error")
	}
}

func TestPromptCacheCSVInjectedWriterErrors(t *testing.T) {
	orig := newPromptCacheCSVWriter
	defer func() { newPromptCacheCSVWriter = orig }()

	newPromptCacheCSVWriter = func(io.Writer) promptCacheCSVWriter {
		return &fakePromptCacheCSVWriter{writeErrs: []error{errors.New("header write")}}
	}
	if err := WritePromptCacheCSV(bytes.NewBuffer(nil), PromptCacheReport{Period: "today"}); err == nil {
		t.Fatal("expected header write error")
	}

	newPromptCacheCSVWriter = func(io.Writer) promptCacheCSVWriter {
		return &fakePromptCacheCSVWriter{writeErrs: []error{nil, errors.New("row write")}}
	}
	if err := WritePromptCacheCSV(bytes.NewBuffer(nil), PromptCacheReport{Period: "today"}); err == nil {
		t.Fatal("expected row write error")
	}
}

func TestAccumulatePromptCacheFileAdditionalBranches(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "events.jsonl")
	lines := []string{
		`{"type":"analytics_event","payload":"bad"}`,
		`{"type":"analytics_event","payload":{"Type":1}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	var report PromptCacheReport
	if err := accumulatePromptCacheFile(path, &report); err != nil {
		t.Fatal(err)
	}
	if report.TotalRequests != 0 {
		t.Fatalf("report=%+v", report)
	}
}

type fakePromptCacheCSVWriter struct {
	writeErrs []error
	flushErr  error
	writes    int
}

func (f *fakePromptCacheCSVWriter) Write([]string) error {
	if f.writes < len(f.writeErrs) {
		err := f.writeErrs[f.writes]
		f.writes++
		return err
	}
	return nil
}

func (f *fakePromptCacheCSVWriter) Flush() {}

func (f *fakePromptCacheCSVWriter) Error() error {
	return f.flushErr
}
