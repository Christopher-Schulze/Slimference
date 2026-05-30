package readcache

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestSanitizeSessionID(t *testing.T) {
	t.Parallel()

	if got := sanitizeSessionID("abc/../x y"); got != "abc____x_y" {
		t.Fatalf("sanitizeSessionID = %q", got)
	}
}

func TestSaveSessionAsyncWriteBehind(t *testing.T) {
	dir := t.TempDir()
	var writes atomic.Int64
	origWrite := readCacheWriteFile
	t.Cleanup(func() {
		readCacheWriteFile = origWrite
		_ = Clear(dir)
	})
	readCacheWriteFile = func(path string, data []byte, mode os.FileMode) error {
		writes.Add(1)
		return origWrite(path, data, mode)
	}

	state := &SessionState{
		SessionID: "async",
		Files: map[string]*FileEntry{
			"main.go": {Path: "main.go", ContentHash: "abc"},
		},
	}
	if err := SaveSessionAsync(dir, state); err != nil {
		t.Fatalf("SaveSessionAsync: %v", err)
	}
	if got := writes.Load(); got != 0 {
		t.Fatalf("SaveSessionAsync wrote synchronously: writes=%d", got)
	}
	loaded, err := LoadSession(dir, "async")
	if err != nil {
		t.Fatalf("LoadSession cached: %v", err)
	}
	if loaded.Files["main.go"].ContentHash != "abc" {
		t.Fatalf("cached state missing: %+v", loaded.Files["main.go"])
	}
	if _, err := os.Stat(filepath.Join(dir, "async.json")); !os.IsNotExist(err) {
		t.Fatalf("session should not be on disk before flush, err=%v", err)
	}
	if err := FlushSession(dir, "async"); err != nil {
		t.Fatalf("FlushSession: %v", err)
	}
	if got := writes.Load(); got != 1 {
		t.Fatalf("FlushSession writes=%d, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "async.json")); err != nil {
		t.Fatalf("session should be flushed to disk: %v", err)
	}
}
