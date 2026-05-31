package toolusecache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoad_Missing(t *testing.T) {
	got, err := Load(t.TempDir(), "s")
	if err != nil || len(got) != 0 {
		t.Fatalf("missing -> %v, %v", got, err)
	}
}

func TestMergeAndLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	add := map[string]Entry{
		"call_1": {ToolUseID: "call_1", ToolName: "exec_command", ToolInput: `{"cmd":"cat a.go"}`, Type: "tool_use"},
	}
	if _, err := Merge(dir, "s1", add); err != nil {
		t.Fatal(err)
	}
	// Simulate a reconnect: a fresh Load rehydrates the resolution map.
	got, err := Load(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got["call_1"]
	if !ok || e.ToolName != "exec_command" || e.ToolInput != `{"cmd":"cat a.go"}` {
		t.Fatalf("rehydrated entry wrong: %+v", got)
	}
}

func TestMerge_EmptyAddIsLoadOnly(t *testing.T) {
	dir := t.TempDir()
	got, err := Merge(dir, "s", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty add -> %v %v", got, err)
	}
	if _, err := os.Stat(sessionPath(dir, "s")); !os.IsNotExist(err) {
		t.Fatal("empty add must not create a file")
	}
}

func TestMerge_CapsPerSession(t *testing.T) {
	dir := t.TempDir()
	add := make(map[string]Entry, MaxEntriesPerSession+50)
	for i := 0; i < MaxEntriesPerSession+50; i++ {
		id := fmt.Sprintf("call_%05d", i)
		add[id] = Entry{ToolUseID: id, ToolName: "x"}
	}
	got, err := Merge(dir, "s", add)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxEntriesPerSession {
		t.Fatalf("per-session cap not enforced: %d", len(got))
	}
}

func TestMergeAsync_WriteBehindAndCachedLoad(t *testing.T) {
	dir := t.TempDir()
	resetMemoryForTest(t)

	var writes atomic.Int64
	savedWrite := writeFile
	t.Cleanup(func() { writeFile = savedWrite })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		writes.Add(1)
		return savedWrite(name, data, perm)
	}

	add := map[string]Entry{
		"call_1": {ToolUseID: "call_1", ToolName: "exec_command", ToolInput: `{"cmd":"sed -n '1,120p' a.go"}`, Type: "function_call"},
	}
	if _, err := MergeAsync(dir, "s1", add); err != nil {
		t.Fatalf("MergeAsync: %v", err)
	}
	if got := writes.Load(); got != 0 {
		t.Fatalf("MergeAsync wrote synchronously: writes=%d", got)
	}
	cached, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load after MergeAsync: %v", err)
	}
	if cached["call_1"].ToolInput != add["call_1"].ToolInput {
		t.Fatalf("cached load missed async merge: %+v", cached)
	}
	if err := FlushSession(dir, "s1"); err != nil {
		t.Fatalf("FlushSession: %v", err)
	}
	if got := writes.Load(); got != 1 {
		t.Fatalf("FlushSession writes=%d, want 1", got)
	}
	resetMemoryForTest(t)
	hydrated, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load after disk flush: %v", err)
	}
	if hydrated["call_1"].ToolName != "exec_command" {
		t.Fatalf("disk hydrate missed entry: %+v", hydrated)
	}
}

func TestClear_RemovesDiskAndMemory(t *testing.T) {
	dir := t.TempDir()
	resetMemoryForTest(t)
	if _, err := MergeAsync(dir, "s1", map[string]Entry{
		"call_1": {ToolUseID: "call_1", ToolName: "exec_command"},
	}); err != nil {
		t.Fatalf("MergeAsync: %v", err)
	}
	if err := FlushSession(dir, "s1"); err != nil {
		t.Fatalf("FlushSession: %v", err)
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(sessionPath(dir, "s1")); !os.IsNotExist(err) {
		t.Fatalf("session file after Clear: %v", err)
	}
	got, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load after Clear = %+v, want empty", got)
	}
}

func TestPrune_AgeAndMissingDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAged := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	writeAged("old.json", 30*24*time.Hour)
	writeAged("fresh.json", time.Hour)
	n, err := Prune(dir, 1000, 14*24*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("age prune n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.json")); !os.IsNotExist(err) {
		t.Fatal("old session should be pruned")
	}
	if n2, err := Prune(filepath.Join(t.TempDir(), "nope"), 0, 0); err != nil || n2 != 0 {
		t.Fatalf("missing dir n=%d err=%v", n2, err)
	}
}

func TestLoad_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath(dir, "s"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "s"); err == nil {
		t.Fatal("corrupt json should error")
	}
}

func TestMerge_WriteError(t *testing.T) {
	saved := writeFile
	t.Cleanup(func() { writeFile = saved })
	writeFile = func(string, []byte, os.FileMode) error { return errors.New("write fail") }
	if _, err := Merge(t.TempDir(), "s", map[string]Entry{"c": {ToolUseID: "c"}}); err == nil {
		t.Fatal("write error should surface")
	}
}

func resetMemoryForTest(t *testing.T) {
	t.Helper()
	memory.mu.Lock()
	memory.sessions = map[string]*memoryEntry{}
	memory.mu.Unlock()
}
