package readcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordDecisionAndSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := RecordDecision(dir, Decision{Type: DecisionAllow}); err != nil {
		t.Fatalf("record allow: %v", err)
	}
	if err := RecordDecision(dir, Decision{Type: DecisionBlock, BlockKind: BlockKindUnchanged}); err != nil {
		t.Fatalf("record unchanged block: %v", err)
	}
	if err := RecordDecision(dir, Decision{Type: DecisionBlock, BlockKind: BlockKindDelta}); err != nil {
		t.Fatalf("record delta block: %v", err)
	}
	state := &SessionState{
		SessionID: "sess1",
		Files: map[string]*FileEntry{
			"/tmp/a.txt": {Path: "/tmp/a.txt"},
			"/tmp/b.txt": {Path: "/tmp/b.txt"},
		},
	}
	if err := SaveSession(dir, state); err != nil {
		t.Fatalf("save session: %v", err)
	}
	snapshot, err := Snapshot(dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Evaluations != 3 || snapshot.Allows != 1 || snapshot.Blocks != 2 {
		t.Fatalf("unexpected stats counts: %+v", snapshot)
	}
	if snapshot.UnchangedBlocks != 1 || snapshot.DeltaBlocks != 1 {
		t.Fatalf("unexpected block breakdown: %+v", snapshot)
	}
	if snapshot.Sessions != 1 || snapshot.TrackedFiles != 2 {
		t.Fatalf("unexpected session counts: %+v", snapshot)
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write stats: %v", err)
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir removed, got err=%v", err)
	}
}
