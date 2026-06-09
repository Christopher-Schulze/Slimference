package checkpoints

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

const (
	statsFilename = "stats.json"
	stateFilename = "state.json"
	maxKeep       = 12

	TriggerManual     = "manual"
	TriggerFill       = "fill"
	TriggerPressure   = "pressure"
	TriggerLowSavings = "low_savings"
	TriggerOverflow   = "overflow"
)

var (
	checkpointsMkdirAll      = os.MkdirAll
	checkpointsReadFile      = os.ReadFile
	checkpointsWriteFile     = os.WriteFile
	checkpointsReadDir       = os.ReadDir
	checkpointsRemove        = os.Remove
	checkpointsMarshalIndent = json.MarshalIndent
	checkpointsUnmarshal     = json.Unmarshal
	estimateModelWindowFn    = estimateModelWindow
)

type CaptureInput struct {
	Trigger        string
	Snapshot       analytics.AnalyticsSnapshot
	RecentRequests []types.RequestMetrics
	Logs           []sessions.LogEntry
	Decisions      []dbg.RequestSummary
	Event          types.AnalyticsEvent
}

type Checkpoint struct {
	ID               string    `json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	Trigger          string    `json:"trigger"`
	Provider         string    `json:"provider,omitempty"`
	Model            string    `json:"model,omitempty"`
	Score            int       `json:"score"`
	InputTokensOrig  int       `json:"input_tokens_orig"`
	InputTokensComp  int       `json:"input_tokens_comp"`
	OutputTokens     int       `json:"output_tokens"`
	CompressionRatio float64   `json:"compression_ratio"`
	Body             string    `json:"body"`
}

type Stats struct {
	Count          int       `json:"count"`
	Captures       int       `json:"captures"`
	AutoCaptures   int       `json:"auto_captures"`
	ManualCaptures int       `json:"manual_captures"`
	Restores       int       `json:"restores"`
	Bytes          int64     `json:"bytes"`
	LastCapture    time.Time `json:"last_capture"`
	LastRestore    time.Time `json:"last_restore"`
	LastTrigger    string    `json:"last_trigger"`
}

type autoState struct {
	LastAutoCapture time.Time `json:"last_auto_capture"`
	LastTrigger     string    `json:"last_trigger"`
}

func DefaultDir(home string) string {
	return filepath.Join(home, ".slimference", "checkpoints")
}

func Capture(dir string, input CaptureInput) (*Checkpoint, error) {
	trigger := strings.TrimSpace(input.Trigger)
	if trigger == "" {
		trigger = TriggerManual
	}
	now := time.Now().UTC()
	cp := &Checkpoint{
		ID:               buildID(now, trigger, input.Event),
		CreatedAt:        now,
		Trigger:          trigger,
		Provider:         input.Event.Provider.String(),
		Model:            strings.TrimSpace(input.Event.Model),
		Score:            score(trigger, input.Event, input.Snapshot, len(input.Decisions), len(input.Logs)),
		InputTokensOrig:  input.Event.InputTokensOrig,
		InputTokensComp:  input.Event.InputTokensComp,
		OutputTokens:     input.Event.OutputTokens,
		CompressionRatio: input.Event.CompressionRatio,
		Body:             renderBody(now, trigger, input),
	}
	if cp.Provider == "unknown" {
		cp.Provider = ""
	}
	if err := checkpointsMkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := saveCheckpoint(dir, cp); err != nil {
		return nil, err
	}
	stats, err := LoadStats(dir)
	if err != nil {
		return nil, err
	}
	stats.Count++
	stats.Captures++
	if trigger == TriggerManual {
		stats.ManualCaptures++
	} else {
		stats.AutoCaptures++
	}
	stats.LastCapture = cp.CreatedAt
	stats.LastTrigger = cp.Trigger
	stats.Bytes += int64(len(cp.Body))
	if err := SaveStats(dir, stats); err != nil {
		return nil, err
	}
	if err := trim(dir, maxKeep); err != nil {
		return nil, err
	}
	stats, err = Snapshot(dir)
	if err == nil {
		_ = SaveStats(dir, stats)
	}
	return cp, nil
}

func MaybeCapture(dir string, input CaptureInput) (*Checkpoint, bool, error) {
	trigger := autoTrigger(input.Event)
	if trigger == "" {
		return nil, false, nil
	}
	state, err := loadState(dir)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	if trigger == state.LastTrigger && now.Sub(state.LastAutoCapture) < 2*time.Minute {
		return nil, false, nil
	}
	input.Trigger = trigger
	cp, err := Capture(dir, input)
	if err != nil {
		return nil, false, err
	}
	state.LastTrigger = trigger
	state.LastAutoCapture = cp.CreatedAt
	if err := saveState(dir, state); err != nil {
		return nil, false, err
	}
	return cp, true, nil
}

func List(dir string) ([]Checkpoint, error) {
	entries, err := checkpointsReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Checkpoint, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Name() == statsFilename || entry.Name() == stateFilename {
			continue
		}
		data, err := checkpointsReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var cp Checkpoint
		if err := checkpointsUnmarshal(data, &cp); err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	slices.SortFunc(out, func(a, b Checkpoint) int {
		if a.CreatedAt.Equal(b.CreatedAt) {
			return strings.Compare(b.ID, a.ID)
		}
		if a.CreatedAt.Before(b.CreatedAt) {
			return 1
		}
		return -1
	})
	return out, nil
}

func RestoreBest(dir string) (*Checkpoint, error) {
	items, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, os.ErrNotExist
	}
	slices.SortFunc(items, compareRestorePriority)
	if err := recordRestore(dir, items[0]); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func RestoreByID(dir string, id string) (*Checkpoint, error) {
	id = normalizeID(id)
	items, err := List(dir)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ID == id {
			if err := recordRestore(dir, item); err != nil {
				return nil, err
			}
			return &item, nil
		}
	}
	return nil, os.ErrNotExist
}

func LoadStats(dir string) (Stats, error) {
	data, err := checkpointsReadFile(filepath.Join(dir, statsFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return Stats{}, nil
		}
		return Stats{}, err
	}
	var stats Stats
	if err := checkpointsUnmarshal(data, &stats); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func SaveStats(dir string, stats Stats) error {
	if err := checkpointsMkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := checkpointsMarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	return checkpointsWriteFile(filepath.Join(dir, statsFilename), append(data, '\n'), 0o644)
}

func Snapshot(dir string) (Stats, error) {
	stats, err := LoadStats(dir)
	if err != nil {
		return Stats{}, err
	}
	items, err := List(dir)
	if err != nil {
		return Stats{}, err
	}
	stats.Count = len(items)
	stats.Bytes = 0
	for _, item := range items {
		stats.Bytes += int64(len(item.Body))
	}
	if len(items) > 0 {
		stats.LastCapture = items[0].CreatedAt
		stats.LastTrigger = items[0].Trigger
	}
	return stats, nil
}

func autoTrigger(event types.AnalyticsEvent) string {
	switch event.Type {
	case types.EventOverflowRetry:
		return TriggerOverflow
	case types.EventRequestProcessed:
		window := estimateModelWindowFn(event.Model)
		if window <= 0 {
			window = 128000
		}
		fill := 0.0
		if window > 0 {
			fill = float64(max(event.InputTokensOrig, event.InputTokensComp)) / float64(window)
		}
		if fill >= 0.78 || event.InputTokensComp >= int(float64(window)*0.75) {
			return TriggerPressure
		}
		if fill >= 0.62 {
			return TriggerFill
		}
		if event.InputTokensOrig >= 24000 && event.CompressionRatio > 0.86 {
			return TriggerLowSavings
		}
	}
	return ""
}

func renderBody(now time.Time, trigger string, input CaptureInput) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("Slimference checkpoint"))
	lines = append(lines, fmt.Sprintf("Captured: %s", now.Format(time.RFC3339)))
	lines = append(lines, fmt.Sprintf("Trigger: %s", trigger))
	if input.Event.Provider.String() != "unknown" {
		lines = append(lines, fmt.Sprintf("Provider: %s", input.Event.Provider))
	}
	if strings.TrimSpace(input.Event.Model) != "" {
		lines = append(lines, fmt.Sprintf("Model: %s", input.Event.Model))
	}
	if input.Event.InputTokensOrig > 0 || input.Event.InputTokensComp > 0 {
		lines = append(lines, fmt.Sprintf(
			"Request tokens: %d -> %d (ratio %.2f, output %d)",
			input.Event.InputTokensOrig,
			input.Event.InputTokensComp,
			input.Event.CompressionRatio,
			input.Event.OutputTokens,
		))
	}
	lines = append(lines, "")
	lines = append(lines, "Session snapshot")
	lines = append(lines, fmt.Sprintf("  Requests: %d", input.Snapshot.TotalRequests))
	lines = append(lines, fmt.Sprintf("  Input saved: %d", input.Snapshot.SavedInputTokens))
	lines = append(lines, fmt.Sprintf("  Errors: %d", input.Snapshot.Errors))
	lines = append(lines, fmt.Sprintf("  Retries: %d", input.Snapshot.AutoRetries))

	if len(input.RecentRequests) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Recent requests")
		for _, req := range newestRequests(input.RecentRequests, 6) {
			lines = append(lines, fmt.Sprintf(
				"  - %s %s %d -> %d ratio %.2f layers=%v latency=%.1fms",
				req.Timestamp.Format("15:04:05"),
				req.Provider,
				req.InputTokensOrig,
				req.InputTokensComp,
				req.CompressionRatio,
				req.Layers,
				req.LatencyMs,
			))
		}
	}

	if len(input.Decisions) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Recent decision summaries")
		for _, summary := range input.Decisions[:min(3, len(input.Decisions))] {
			lines = append(lines, fmt.Sprintf(
				"  - %s req=%s %s/%s saved=%d ratio=%.2f layers=%v",
				summary.Timestamp.Format("15:04:05"),
				summary.RequestID,
				summary.Provider,
				summary.Model,
				summary.Tokens.Saved,
				summary.Tokens.Ratio,
				summary.LayersApplied,
			))
			if len(summary.Layer1Breakdown) > 0 {
				var parts []string
				for name, breakdown := range summary.Layer1Breakdown {
					parts = append(parts, fmt.Sprintf("%s=%d", name, breakdown.Saved))
				}
				slices.Sort(parts)
				lines = append(lines, "    "+strings.Join(parts, " "))
			}
		}
	}

	if len(input.Logs) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Recent logs")
		for _, entry := range tailLogs(input.Logs, 8) {
			lines = append(lines, fmt.Sprintf("  - %s %s %s", entry.Timestamp.Format("15:04:05"), entry.Level, entry.Message))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Restore")
	lines = append(lines, "  slimference checkpoint restore")
	return strings.Join(lines, "\n")
}

func buildID(now time.Time, trigger string, event types.AnalyticsEvent) string {
	base := now.Format("20060102-150405")
	if event.Provider.String() != "unknown" {
		return sanitizeID(base + "-" + trigger + "-" + event.Provider.String())
	}
	return sanitizeID(base + "-" + trigger)
}

func saveCheckpoint(dir string, cp *Checkpoint) error {
	data, err := checkpointsMarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return checkpointsWriteFile(filepath.Join(dir, cp.ID+".json"), append(data, '\n'), 0o644)
}

func trim(dir string, keep int) error {
	items, err := List(dir)
	if err != nil {
		return err
	}
	if keep < 0 || len(items) <= keep {
		return nil
	}
	for _, item := range items[keep:] {
		if err := checkpointsRemove(filepath.Join(dir, item.ID+".json")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func recordRestore(dir string, cp Checkpoint) error {
	stats, err := LoadStats(dir)
	if err != nil {
		return err
	}
	stats.Restores++
	stats.LastRestore = time.Now().UTC()
	return SaveStats(dir, stats)
}

func compareRestorePriority(a, b Checkpoint) int {
	if a.Score != b.Score {
		if a.Score < b.Score {
			return 1
		}
		return -1
	}
	if a.CreatedAt.Before(b.CreatedAt) {
		return 1
	}
	if a.CreatedAt.After(b.CreatedAt) {
		return -1
	}
	return strings.Compare(b.ID, a.ID)
}

func loadState(dir string) (autoState, error) {
	data, err := checkpointsReadFile(filepath.Join(dir, stateFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return autoState{}, nil
		}
		return autoState{}, err
	}
	var state autoState
	if err := checkpointsUnmarshal(data, &state); err != nil {
		return autoState{}, err
	}
	return state, nil
}

func saveState(dir string, state autoState) error {
	if err := checkpointsMkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := checkpointsMarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return checkpointsWriteFile(filepath.Join(dir, stateFilename), append(data, '\n'), 0o644)
}

func newestRequests(items []types.RequestMetrics, maxItems int) []types.RequestMetrics {
	if len(items) <= maxItems {
		return append([]types.RequestMetrics(nil), items...)
	}
	return append([]types.RequestMetrics(nil), items[len(items)-maxItems:]...)
}

func tailLogs(items []sessions.LogEntry, maxItems int) []sessions.LogEntry {
	if len(items) <= maxItems {
		return append([]sessions.LogEntry(nil), items...)
	}
	return append([]sessions.LogEntry(nil), items[len(items)-maxItems:]...)
}

func score(trigger string, event types.AnalyticsEvent, snap analytics.AnalyticsSnapshot, decisions int, logs int) int {
	base := 10
	switch trigger {
	case TriggerManual:
		base = 100
	case TriggerOverflow:
		base = 90
	case TriggerPressure:
		base = 80
	case TriggerFill:
		base = 70
	case TriggerLowSavings:
		base = 60
	}
	base += min(event.InputTokensOrig/2000, 30)
	base += min(snap.TotalRequests, 20)
	base += min(decisions*5, 15)
	base += min(logs, 10)
	return base
}

func estimateModelWindow(model string) int {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "haiku"):
		return 200000
	case strings.Contains(m, "claude"):
		return 200000
	case strings.Contains(m, "gpt-4.1"), strings.Contains(m, "gpt-4o"), strings.Contains(m, "o3"), strings.Contains(m, "o4"), strings.Contains(m, "gpt-5"):
		return 128000
	default:
		return 128000
	}
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeID(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "slim://checkpoint/")
	return sanitizeID(raw)
}
