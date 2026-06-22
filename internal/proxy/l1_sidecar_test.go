package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordL1ServerStateSidecar_WritesRow(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	sessionID := "test-session-123"
	recordL1ServerStateSidecar(sessionID, 5000, 1000, 4000)
	path := filepath.Join(tmp, ".slimference", "analytics", "server_state_continuation_"+sessionID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := splitNonEmptyLines(data)
	if len(lines) != 1 {
		t.Fatalf("expected 1 row, got %d", len(lines))
	}
	var row l1SidecarRow
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatalf("parse row: %v", err)
	}
	if row.InputTokens != 5000 || row.OutputTokens != 1000 || row.SavedTokens != 4000 {
		t.Fatalf("bad row: %+v", row)
	}
	if row.Timestamp == "" {
		t.Fatalf("empty timestamp")
	}
}

func TestRecordL1ServerStateSidecar_SkipsZeroSaved(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	recordL1ServerStateSidecar("test-session-zero", 1000, 1000, 0)
	// File should not be created when savedTokens <= 0.
	entries, err := os.ReadDir(filepath.Join(tmp, ".slimference", "analytics"))
	if err != nil {
		if os.IsNotExist(err) {
			return // no dir created — correct
		}
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if filepath.Base(e.Name()) == "server_state_continuation_test-session-zero.jsonl" {
			t.Fatalf("sidecar file should not exist for zero savings")
		}
	}
}

func TestRecordL1ServerStateSidecar_SkipsEmptySession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	recordL1ServerStateSidecar("", 5000, 1000, 4000)
	// No file should be created.
	_, err := os.Stat(filepath.Join(tmp, ".slimference", "analytics"))
	if err == nil {
		t.Fatalf("analytics dir should not exist for empty sessionID")
	}
}

func TestRecordL1ServerStateSidecar_AppendsMultipleRows(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	sessionID := "test-session-multi"
	recordL1ServerStateSidecar(sessionID, 5000, 1000, 4000)
	recordL1ServerStateSidecar(sessionID, 8000, 2000, 6000)
	path := filepath.Join(tmp, ".slimference", "analytics", "server_state_continuation_"+sessionID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := splitNonEmptyLines(data)
	if len(lines) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(lines))
	}
}

func splitNonEmptyLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}
