package analytics

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestPersister_snapshotRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}

	snap := AnalyticsSnapshot{
		SessionStart:     time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
		TotalRequests:    3,
		TotalInputTokens: 900,
		CacheHits:        1,
		PerProvider: map[types.Provider]ProviderStats{
			types.Anthropic: {Messages: 2, InputTokensOrig: 100},
		},
	}
	if err := p.WriteSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	p.Close()

	got, err := ReadDailyStats(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("snapshots: want 1, got %d", len(got))
	}
	if got[0].TotalRequests != 3 || got[0].TotalInputTokens != 900 || got[0].CacheHits != 1 {
		t.Fatalf("unexpected snapshot: %+v", got[0])
	}
	ps := got[0].PerProvider[types.Anthropic]
	if ps.Messages != 2 || ps.InputTokensOrig != 100 {
		t.Fatalf("per-provider: %+v", ps)
	}
}

func TestReadWeeklyStats_emptyDir(t *testing.T) {
	t.Parallel()
	snaps, err := ReadWeeklyStats(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("want 0 snapshots, got %d", len(snaps))
	}
}

func TestPersister_rotateIfNeededNoPanic(t *testing.T) {
	t.Parallel()
	p, err := NewPersister(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.RotateIfNeeded()
}

func TestPersister_writeEvent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	ev := types.AnalyticsEvent{
		Type:     types.EventRequestProcessed,
		Provider: types.Anthropic,
		Model:    "claude",
	}
	if err := p.WriteEvent(ev); err != nil {
		t.Fatal(err)
	}
	p.Close()

	path := filepath.Join(dir, time.Now().Format(dateFormat)+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"type":"analytics_event"`)) {
		t.Fatalf("jsonl: %s", data)
	}
}

func TestNewPersister_mkdirError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	// Create a file where the subdirectory should be (blocks MkdirAll).
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewPersister(filepath.Join(blocker, "analytics"))
	if err == nil {
		t.Fatal("expected error: path component is a file, not a dir")
	}
}

func TestNewPersister_openFileError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	// chmod 0555 prevents file creation inside dir.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()
	_, err := NewPersister(dir)
	if err == nil {
		t.Fatal("expected error opening log file in read-only dir")
	}
}

func TestPersister_rotateActual(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Force date mismatch to trigger actual file rotation.
	p.mu.Lock()
	p.currentDate = "2020-01-01"
	p.mu.Unlock()
	// WriteSnapshot triggers rotateIfNeeded which sees date mismatch and rotates.
	snap := AnalyticsSnapshot{SessionStart: time.Now(), TotalRequests: 1}
	if err := p.WriteSnapshot(snap); err != nil {
		t.Fatal(err)
	}
}

func TestPersister_rotateIfNeeded_logError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Delete today's file so the read-only dir cannot re-create it during rotation.
	todayFile := filepath.Join(dir, time.Now().Format(dateFormat)+".jsonl")
	if err := os.Remove(todayFile); err != nil {
		t.Fatal(err)
	}
	// Force old date so rotation is triggered, then make dir read-only so openFile fails.
	p.mu.Lock()
	p.currentDate = "2020-01-01"
	p.mu.Unlock()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()
	// RotateIfNeeded logs the error instead of returning it - must not panic.
	p.RotateIfNeeded()
}

func TestReadWeeklyStats_scanError(t *testing.T) {
	dir := t.TempDir()
	// Write a line above the 8 MiB scanner limit to trigger scan error.
	bigLine := make([]byte, 9*1024*1024)
	for i := range bigLine {
		bigLine[i] = 'x'
	}
	bigLine[len(bigLine)-1] = '\n'
	todayPath := filepath.Join(dir, time.Now().Format(dateFormat)+".jsonl")
	if err := os.WriteFile(todayPath, bigLine, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadWeeklyStats(dir)
	if err == nil {
		t.Fatal("expected error for line exceeding scanner buffer")
	}
}

func TestPersister_writeEvent_closedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Close the underlying fd while leaving currentDate as today so rotateIfNeeded skips rotation.
	p.mu.Lock()
	_ = p.currentFile.Close()
	p.mu.Unlock()
	ev := types.AnalyticsEvent{Type: types.EventRequestProcessed, Provider: types.Anthropic}
	if err := p.WriteEvent(ev); err == nil {
		t.Fatal("expected error writing to closed file")
	}
}

func TestPersister_writeEvent_rotateError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Delete today's file, force date mismatch, make dir read-only so rotateIfNeeded fails.
	todayFile := filepath.Join(dir, time.Now().Format(dateFormat)+".jsonl")
	if err := os.Remove(todayFile); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.currentDate = "2020-01-01"
	p.mu.Unlock()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()
	ev := types.AnalyticsEvent{Type: types.EventRequestProcessed, Provider: types.Anthropic}
	if err := p.WriteEvent(ev); err == nil {
		t.Fatal("expected error when rotation fails")
	}
}

func TestPersister_writeSnapshot_closedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.mu.Lock()
	_ = p.currentFile.Close()
	p.mu.Unlock()
	snap := AnalyticsSnapshot{SessionStart: time.Now(), TotalRequests: 1}
	if err := p.WriteSnapshot(snap); err == nil {
		t.Fatal("expected error writing to closed file")
	}
}

func TestPersister_writeSnapshot_rotateError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Delete today's file, force date mismatch, make dir read-only so rotateIfNeeded fails.
	todayFile := filepath.Join(dir, time.Now().Format(dateFormat)+".jsonl")
	if err := os.Remove(todayFile); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.currentDate = "2020-01-01"
	p.mu.Unlock()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()
	snap := AnalyticsSnapshot{SessionStart: time.Now(), TotalRequests: 1}
	if err := p.WriteSnapshot(snap); err == nil {
		t.Fatal("expected error when rotation fails")
	}
}

func TestPersister_close_withClosedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-close the fd so both Sync and Close in Close() hit errors (logged, not returned).
	p.mu.Lock()
	_ = p.currentFile.Close()
	p.mu.Unlock()
	// Must not panic.
	p.Close()
}

func TestReadSnapshots_openError(t *testing.T) {
	t.Parallel()
	_, err := readSnapshots("/nonexistent/path/file.jsonl")
	if err == nil {
		t.Fatal("expected error opening nonexistent file")
	}
}

func TestReadSnapshots_malformedLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	// Line 1: empty line -> skipped (len(line)==0 branch)
	// Line 2: invalid JSON -> json.Unmarshal error (logged, skipped)
	// Line 3: valid envelope with non-snapshot type -> skipped (type != "session_snapshot")
	// Line 4: valid session_snapshot envelope but payload is not an object -> payload unmarshal error (logged, skipped)
	// Line 5: valid session_snapshot with valid payload -> included in result
	content := "\n" +
		"{{{invalid json\n" +
		`{"type":"analytics_event","timestamp":"2026-04-10T12:00:00Z","payload":{}}` + "\n" +
		`{"type":"session_snapshot","timestamp":"2026-04-10T12:00:00Z","payload":"not-an-object"}` + "\n" +
		`{"type":"session_snapshot","timestamp":"2026-04-10T12:00:00Z","payload":{"total_requests":7}}` + "\n"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	snaps, err := readSnapshots(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 valid snapshot, got %d", len(snaps))
	}
	if snaps[0].TotalRequests != 7 {
		t.Fatalf("unexpected snapshot: %+v", snaps[0])
	}
}

func TestReadWeeklyStats_withData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := AnalyticsSnapshot{SessionStart: time.Now(), TotalRequests: 5}
	if err := p.WriteSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	p.Close()

	all, err := ReadWeeklyStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("expected at least one snapshot from ReadWeeklyStats")
	}
	if all[0].TotalRequests != 5 {
		t.Fatalf("unexpected snapshot total_requests: %d", all[0].TotalRequests)
	}
}

func TestPersister_rotateIfNeeded_coversError(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	p, err := NewPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Delete today's file so openFile cannot reopen it in the read-only dir.
	todayFile := filepath.Join(dir, time.Now().Format(dateFormat)+".jsonl")
	if err := os.Remove(todayFile); err != nil {
		t.Fatal(err)
	}
	// Force date mismatch so rotateIfNeeded tries to create today's file, which
	// fails because the dir is 0555 and the file no longer exists.
	p.mu.Lock()
	p.currentDate = "2020-01-01"
	p.mu.Unlock()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()
	// RotateIfNeeded logs the error with slog.Warn - must not panic.
	p.RotateIfNeeded()
}
