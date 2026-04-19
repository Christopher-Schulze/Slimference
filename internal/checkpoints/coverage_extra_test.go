package checkpoints

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/types"
)

func TestCheckpointHelpersAndStats(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if got := DefaultDir(home); got != filepath.Join(home, ".slimference", "checkpoints") {
		t.Fatalf("DefaultDir=%q", got)
	}

	dir := t.TempDir()
	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 0 {
		t.Fatalf("unexpected zero stats: %+v", stats)
	}

	want := Stats{Count: 2, Captures: 3, Restores: 1, Bytes: 99, LastTrigger: TriggerFill}
	if err := SaveStats(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != want.Count || got.Captures != want.Captures || got.LastTrigger != want.LastTrigger {
		t.Fatalf("LoadStats=%+v want=%+v", got, want)
	}

	badDirFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveStats(badDirFile, Stats{}); err == nil {
		t.Fatal("expected SaveStats mkdir error")
	}
	if err := saveState(badDirFile, autoState{}); err == nil {
		t.Fatal("expected saveState mkdir error")
	}
}

func TestCheckpointInjectedErrorBranches(t *testing.T) {
	dir := t.TempDir()

	origMarshal := checkpointsMarshalIndent
	origWrite := checkpointsWriteFile
	origReadDir := checkpointsReadDir
	origReadFile := checkpointsReadFile
	origRemove := checkpointsRemove
	defer func() {
		checkpointsMarshalIndent = origMarshal
		checkpointsWriteFile = origWrite
		checkpointsReadDir = origReadDir
		checkpointsReadFile = origReadFile
		checkpointsRemove = origRemove
	}()

	checkpointsMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal") }
	if err := SaveStats(dir, Stats{}); err == nil {
		t.Fatal("expected SaveStats marshal error")
	}
	if err := saveState(dir, autoState{}); err == nil {
		t.Fatal("expected saveState marshal error")
	}
	if err := saveCheckpoint(dir, &Checkpoint{}); err == nil {
		t.Fatal("expected saveCheckpoint marshal error")
	}

	checkpointsMarshalIndent = origMarshal
	checkpointsWriteFile = func(string, []byte, os.FileMode) error { return errors.New("write") }
	if err := SaveStats(dir, Stats{}); err == nil {
		t.Fatal("expected SaveStats write error")
	}
	if err := saveState(dir, autoState{}); err == nil {
		t.Fatal("expected saveState write error")
	}
	if err := saveCheckpoint(dir, &Checkpoint{}); err == nil {
		t.Fatal("expected saveCheckpoint write error")
	}

	checkpointsWriteFile = origWrite
	checkpointsReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir") }
	if _, err := List(dir); err == nil {
		t.Fatal("expected List read dir error")
	}

	checkpointsReadDir = origReadDir
	if err := saveCheckpoint(dir, &Checkpoint{ID: "cp-a", CreatedAt: time.Unix(10, 0), Score: 1, Body: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := saveCheckpoint(dir, &Checkpoint{ID: "cp-b", CreatedAt: time.Unix(10, 0), Score: 1, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if got, err := RestoreBest(dir); err != nil || got.ID != "cp-b" {
		t.Fatalf("RestoreBest tie got=%+v err=%v", got, err)
	}

	checkpointsReadFile = func(string) ([]byte, error) { return nil, errors.New("read") }
	if _, err := List(dir); err == nil {
		t.Fatal("expected List read file error")
	}
	checkpointsReadFile = origReadFile

	checkpointsRemove = func(string) error { return errors.New("remove") }
	if err := trim(dir, 1); err == nil {
		t.Fatal("expected trim remove error")
	}
}

func TestCheckpointListRestoreTrimAndState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	nowA := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	nowB := nowA.Add(time.Minute)
	a := &Checkpoint{ID: "cp-a", CreatedAt: nowA, Trigger: TriggerFill, Score: 10, Body: "a"}
	b := &Checkpoint{ID: "cp-b", CreatedAt: nowB, Trigger: TriggerManual, Score: 90, Body: "b"}
	if err := saveCheckpoint(dir, a); err != nil {
		t.Fatal(err)
	}
	if err := saveCheckpoint(dir, b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stateFilename), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "cp-b" || items[1].ID != "cp-a" {
		t.Fatalf("List=%+v", items)
	}

	best, err := RestoreBest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if best.ID != "cp-b" {
		t.Fatalf("RestoreBest=%+v", best)
	}
	byID, err := RestoreByID(dir, "slim://checkpoint/cp-a")
	if err != nil {
		t.Fatal(err)
	}
	if byID.ID != "cp-a" {
		t.Fatalf("RestoreByID=%+v", byID)
	}

	stats, err := LoadStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Restores != 2 {
		t.Fatalf("restore stats=%+v", stats)
	}

	if err := trim(dir, 1); err != nil {
		t.Fatal(err)
	}
	items, err = List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "cp-b" {
		t.Fatalf("trimmed items=%+v", items)
	}

	state := autoState{LastTrigger: TriggerPressure, LastAutoCapture: nowB}
	if err := saveState(dir, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastTrigger != TriggerPressure || !loaded.LastAutoCapture.Equal(nowB) {
		t.Fatalf("loadState=%+v", loaded)
	}
}

func TestCheckpointMaybeCaptureBranchesAndHelpers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if cp, ok, err := MaybeCapture(dir, CaptureInput{
		Event: types.AnalyticsEvent{Type: types.EventRequestProcessed, InputTokensOrig: 100, InputTokensComp: 80},
	}); err != nil || ok || cp != nil {
		t.Fatalf("MaybeCapture no trigger cp=%+v ok=%v err=%v", cp, ok, err)
	}

	if err := os.WriteFile(filepath.Join(dir, stateFilename), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := MaybeCapture(dir, CaptureInput{Event: types.AnalyticsEvent{Type: types.EventOverflowRetry}}); err == nil {
		t.Fatal("expected invalid state error")
	}
	if _, err := RestoreBest(t.TempDir()); !os.IsNotExist(err) {
		t.Fatalf("expected RestoreBest empty err=%v", err)
	}
}

func TestCheckpointCaptureAndRestoreAdditionalBranches(t *testing.T) {
	dir := t.TempDir()

	cp, err := Capture(dir, CaptureInput{
		Event: types.AnalyticsEvent{Provider: types.Provider(99)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cp.Trigger != TriggerManual || cp.Provider != "" {
		t.Fatalf("capture=%+v", cp)
	}

	origWrite := checkpointsWriteFile
	origMkdir := checkpointsMkdirAll
	origRead := checkpointsReadFile
	origRemove := checkpointsRemove
	defer func() { checkpointsWriteFile = origWrite }()
	defer func() {
		checkpointsMkdirAll = origMkdir
		checkpointsReadFile = origRead
		checkpointsRemove = origRemove
	}()

	checkpointsWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, statsFilename) {
			return errors.New("stats write")
		}
		return origWrite(name, data, perm)
	}
	if _, err := Capture(t.TempDir(), CaptureInput{Trigger: TriggerManual}); err == nil {
		t.Fatal("expected Capture stats write error")
	}

	checkpointsWriteFile = origWrite
	restoreDir := t.TempDir()
	if err := saveCheckpoint(restoreDir, &Checkpoint{ID: "cp-a", CreatedAt: time.Unix(10, 0), Score: 1, Body: "a"}); err != nil {
		t.Fatal(err)
	}
	checkpointsWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, statsFilename) {
			return errors.New("restore write")
		}
		return origWrite(name, data, perm)
	}
	if _, err := RestoreBest(restoreDir); err == nil {
		t.Fatal("expected RestoreBest record error")
	}
	if _, err := RestoreByID(restoreDir, "cp-a"); err == nil {
		t.Fatal("expected RestoreByID record error")
	}

	checkpointsWriteFile = origWrite
	checkpointsMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir") }
	if _, err := Capture(t.TempDir(), CaptureInput{Trigger: TriggerManual}); err == nil {
		t.Fatal("expected Capture mkdir error")
	}

	checkpointsMkdirAll = origMkdir
	checkpointsWriteFile = func(string, []byte, os.FileMode) error { return errors.New("checkpoint write") }
	if _, err := Capture(t.TempDir(), CaptureInput{Trigger: TriggerManual}); err == nil {
		t.Fatal("expected Capture checkpoint write error")
	}

	checkpointsWriteFile = origWrite
	checkpointsReadFile = func(string) ([]byte, error) { return nil, errors.New("stats read") }
	if _, err := Capture(t.TempDir(), CaptureInput{Trigger: TriggerManual}); err == nil {
		t.Fatal("expected Capture stats read error")
	}

	trimDir := t.TempDir()
	for i := 0; i < maxKeep; i++ {
		id := fmt.Sprintf("cp-%02d", i)
		if err := saveCheckpoint(trimDir, &Checkpoint{ID: id, CreatedAt: time.Unix(int64(i), 0), Score: i, Body: id}); err != nil {
			t.Fatal(err)
		}
	}
	checkpointsReadFile = origRead
	checkpointsRemove = func(string) error { return errors.New("trim remove") }
	if _, err := Capture(trimDir, CaptureInput{Trigger: TriggerManual}); err == nil {
		t.Fatal("expected Capture trim error")
	}

	checkpointsRemove = origRemove
	checkpointsWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, stateFilename) {
			return errors.New("state write")
		}
		return origWrite(name, data, perm)
	}
	if _, ok, err := MaybeCapture(t.TempDir(), CaptureInput{Event: types.AnalyticsEvent{Type: types.EventOverflowRetry}}); err == nil || ok {
		t.Fatalf("expected MaybeCapture saveState error ok=%v err=%v", ok, err)
	}
	checkpointsWriteFile = origWrite
	checkpointsReadFile = func(string) ([]byte, error) { return nil, errors.New("capture read") }
	if _, ok, err := MaybeCapture(t.TempDir(), CaptureInput{Event: types.AnalyticsEvent{Type: types.EventOverflowRetry}}); err == nil || ok {
		t.Fatalf("expected MaybeCapture capture error ok=%v err=%v", ok, err)
	}
	checkpointsReadFile = origRead
	checkpointsWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, stateFilename) && !strings.HasSuffix(name, statsFilename) {
			return errors.New("capture checkpoint write")
		}
		return origWrite(name, data, perm)
	}
	if _, ok, err := MaybeCapture(t.TempDir(), CaptureInput{Event: types.AnalyticsEvent{Type: types.EventOverflowRetry}}); err == nil || ok {
		t.Fatalf("expected MaybeCapture checkpoint write error ok=%v err=%v", ok, err)
	}
}

func TestCheckpointRenderingAndScoringHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 19, 12, 34, 56, 0, time.UTC)
	reqs := make([]types.RequestMetrics, 8)
	logs := make([]sessions.LogEntry, 10)
	for i := range reqs {
		reqs[i] = types.RequestMetrics{Timestamp: now.Add(time.Duration(i) * time.Second)}
	}
	for i := range logs {
		logs[i] = sessions.LogEntry{Timestamp: now.Add(time.Duration(i) * time.Second), Level: "INFO", Message: "msg"}
	}
	body := renderBody(now, TriggerOverflow, CaptureInput{
		Snapshot:       analytics.AnalyticsSnapshot{TotalRequests: 7, SavedInputTokens: 999, Errors: 2, AutoRetries: 1},
		RecentRequests: reqs,
		Logs:           logs,
		Decisions: []dbg.RequestSummary{
			{
				Timestamp: now,
				RequestID: "req-1",
				Provider:  "anthropic",
				Model:     "claude-3-7-sonnet",
				Tokens: dbg.TokenCounts{
					Saved: 10,
					Ratio: 0.5,
				},
				LayersApplied: []int{1, 2},
				Layer1Breakdown: map[string]dbg.SubLayerBreakdown{
					"dedup": {Blocks: 1, Saved: 3},
				},
			},
		},
		Event: types.AnalyticsEvent{
			Provider:         types.Anthropic,
			Model:            "claude-3-7-sonnet",
			InputTokensOrig:  2000,
			InputTokensComp:  1000,
			OutputTokens:     50,
			CompressionRatio: 0.5,
		},
	})
	for _, want := range []string{"Slimference checkpoint", "Trigger: overflow", "Recent requests", "Recent logs", "Restore"} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderBody missing %q in %q", want, body)
		}
	}

	if len(newestRequests(reqs, 3)) != 3 || len(tailLogs(logs, 4)) != 4 {
		t.Fatal("newest/tail helpers did not clamp")
	}
	if got := score(TriggerManual, types.AnalyticsEvent{InputTokensOrig: 100000}, analytics.AnalyticsSnapshot{TotalRequests: 99}, 9, 20); got <= 100 {
		t.Fatalf("score too low: %d", got)
	}
	if got := buildID(now, "manual", types.AnalyticsEvent{Provider: types.Anthropic}); !strings.Contains(got, "manual-anthropic") {
		t.Fatalf("buildID=%q", got)
	}
	if got := buildID(now, "manual", types.AnalyticsEvent{}); !strings.Contains(got, "manual") {
		t.Fatalf("buildID unknown=%q", got)
	}
	if got := sanitizeID(" a/b:c "); strings.Contains(got, "/") || strings.Contains(got, ":") {
		t.Fatalf("sanitizeID=%q", got)
	}
	if got := normalizeID("slim://checkpoint/a/b:c"); got != "a-b-c" {
		t.Fatalf("normalizeID=%q", got)
	}
	if estimateModelWindow("claude-3-haiku") != 200000 || estimateModelWindow("gpt-4o") != 128000 || estimateModelWindow("") != 128000 {
		t.Fatal("estimateModelWindow mismatch")
	}

	tests := []struct {
		name  string
		event types.AnalyticsEvent
		want  string
	}{
		{"overflow", types.AnalyticsEvent{Type: types.EventOverflowRetry}, TriggerOverflow},
		{"pressure_fill_ratio", types.AnalyticsEvent{Type: types.EventRequestProcessed, Model: "gpt-4o", InputTokensOrig: 110000, InputTokensComp: 100000}, TriggerPressure},
		{"fill", types.AnalyticsEvent{Type: types.EventRequestProcessed, Model: "gpt-4o", InputTokensOrig: 80000, InputTokensComp: 79000}, TriggerFill},
		{"low_savings", types.AnalyticsEvent{Type: types.EventRequestProcessed, Model: "gpt-4o", InputTokensOrig: 30000, InputTokensComp: 25000, CompressionRatio: 0.9}, TriggerLowSavings},
		{"none", types.AnalyticsEvent{Type: types.EventRequestProcessed, Model: "gpt-4o", InputTokensOrig: 1000, InputTokensComp: 900, CompressionRatio: 0.5}, ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := autoTrigger(tc.event); got != tc.want {
				t.Fatalf("autoTrigger=%q want=%q", got, tc.want)
			}
		})
	}
	if got := score("other", types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 10 {
		t.Fatalf("default score=%d", got)
	}
	if got := score(TriggerOverflow, types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 90 {
		t.Fatalf("overflow score=%d", got)
	}
	if got := score(TriggerFill, types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 70 {
		t.Fatalf("fill score=%d", got)
	}
	if got := score(TriggerLowSavings, types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 60 {
		t.Fatalf("low savings score=%d", got)
	}
	if estimateModelWindow("claude-opus") != 200000 {
		t.Fatal("expected claude window")
	}
	if got := sanitizeID("ABC"); got != "ABC" {
		t.Fatalf("uppercase sanitize=%q", got)
	}
	origEstimate := estimateModelWindowFn
	estimateModelWindowFn = func(string) int { return 0 }
	if got := autoTrigger(types.AnalyticsEvent{
		Type:            types.EventRequestProcessed,
		InputTokensOrig: 1000,
		InputTokensComp: 900,
	}); got != "" {
		t.Fatalf("zero-window trigger=%q", got)
	}
	estimateModelWindowFn = origEstimate
}

func TestCheckpointListAndStatsErrorPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("expected broken checkpoint JSON error")
	}

	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStats(dir); err == nil {
		t.Fatal("expected broken stats JSON error")
	}
	badDirFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badDirFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(badDirFile); err == nil {
		t.Fatal("expected snapshot list error")
	}

	path := filepath.Join(dir, stateFilename)
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointAdditionalHelpersAndCooldownPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if items, err := List(filepath.Join(t.TempDir(), "missing")); err != nil || len(items) != 0 {
		t.Fatalf("missing list items=%+v err=%v", items, err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, statsFilename), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stateFilename), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveCheckpoint(dir, &Checkpoint{ID: "cp-ok", CreatedAt: time.Unix(20, 0), Trigger: TriggerFill, Score: 5, Body: "ok"}); err != nil {
		t.Fatal(err)
	}
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "cp-ok" {
		t.Fatalf("items=%+v", items)
	}
	if _, err := RestoreByID(dir, "missing"); !os.IsNotExist(err) {
		t.Fatalf("expected missing restore err=%v", err)
	}
	if err := trim(dir, -1); err != nil {
		t.Fatalf("trim keep<0 err=%v", err)
	}
	state, err := loadState(t.TempDir())
	if err != nil || !state.LastAutoCapture.IsZero() || state.LastTrigger != "" {
		t.Fatalf("loadState=%+v err=%v", state, err)
	}

	cooldownDir := t.TempDir()
	now := time.Now().UTC()
	if err := saveState(cooldownDir, autoState{LastTrigger: TriggerPressure, LastAutoCapture: now}); err != nil {
		t.Fatal(err)
	}
	cp, ok, err := MaybeCapture(cooldownDir, CaptureInput{
		Event: types.AnalyticsEvent{
			Type:            types.EventRequestProcessed,
			Model:           "gpt-4o",
			InputTokensOrig: 110000,
			InputTokensComp: 100000,
		},
	})
	if err != nil || ok || cp != nil {
		t.Fatalf("cooldown cp=%+v ok=%v err=%v", cp, ok, err)
	}

	if got := score(TriggerOverflow, types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 90 {
		t.Fatalf("overflow score=%d", got)
	}
	if got := score(TriggerPressure, types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 80 {
		t.Fatalf("pressure score=%d", got)
	}
	if got := score(TriggerFill, types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 70 {
		t.Fatalf("fill score=%d", got)
	}
	if got := score(TriggerLowSavings, types.AnalyticsEvent{}, analytics.AnalyticsSnapshot{}, 0, 0); got != 60 {
		t.Fatalf("low savings score=%d", got)
	}
	if estimateModelWindow("claude-opus") != 200000 {
		t.Fatal("expected generic claude window")
	}
	if got := sanitizeID("   "); got != "" {
		t.Fatalf("sanitizeID blank=%q", got)
	}

	brokenDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(brokenDir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBest(brokenDir); err == nil {
		t.Fatal("expected RestoreBest list error")
	}
	if _, err := RestoreByID(brokenDir, "anything"); err == nil {
		t.Fatal("expected RestoreByID list error")
	}
	if err := trim(brokenDir, 0); err == nil {
		t.Fatal("expected trim list error")
	}

	recordDir := t.TempDir()
	if err := saveCheckpoint(recordDir, &Checkpoint{ID: "cp-record", CreatedAt: time.Unix(1, 0), Score: 1, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordDir, statsFilename), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreByID(recordDir, "cp-record"); err == nil {
		t.Fatal("expected recordRestore load stats error")
	}

	snapshotErrDir := t.TempDir()
	if err := SaveStats(snapshotErrDir, Stats{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotErrDir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(snapshotErrDir); err == nil {
		t.Fatal("expected Snapshot list error")
	}

	if got := compareRestorePriority(
		Checkpoint{ID: "a", Score: 1},
		Checkpoint{ID: "b", Score: 2},
	); got != 1 {
		t.Fatalf("score compare got=%d", got)
	}
	if got := compareRestorePriority(
		Checkpoint{ID: "a", Score: 3},
		Checkpoint{ID: "b", Score: 2},
	); got != -1 {
		t.Fatalf("score reverse compare got=%d", got)
	}
	if got := compareRestorePriority(
		Checkpoint{ID: "a", Score: 2, CreatedAt: time.Unix(1, 0)},
		Checkpoint{ID: "b", Score: 2, CreatedAt: time.Unix(2, 0)},
	); got != 1 {
		t.Fatalf("time before compare got=%d", got)
	}
	if got := compareRestorePriority(
		Checkpoint{ID: "a", Score: 2, CreatedAt: time.Unix(3, 0)},
		Checkpoint{ID: "b", Score: 2, CreatedAt: time.Unix(2, 0)},
	); got != -1 {
		t.Fatalf("time after compare got=%d", got)
	}
}
