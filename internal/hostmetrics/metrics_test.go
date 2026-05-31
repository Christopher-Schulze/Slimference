package hostmetrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentProcessInvalidPID(t *testing.T) {
	t.Parallel()
	got := CurrentProcess(0)
	if got.PID != 0 || got.RSSKnown || got.RSSBytes != 0 {
		t.Fatalf("invalid pid snapshot=%+v", got)
	}
}

func TestCurrentProcessSelf(t *testing.T) {
	t.Parallel()
	got := CurrentProcess(os.Getpid())
	if got.PID != os.Getpid() {
		t.Fatalf("pid=%d want %d", got.PID, os.Getpid())
	}
	if got.RSSKnown && got.RSSBytes <= 0 {
		t.Fatalf("known RSS must be positive: %+v", got)
	}
	if got.CPUKnown && got.CPUUserSeconds+got.CPUSystemSeconds < 0 {
		t.Fatalf("known CPU time must be non-negative: %+v", got)
	}
	if got.DiskIOKnown && (got.DiskReadOps < 0 || got.DiskWriteOps < 0) {
		t.Fatalf("known disk IO must be non-negative: %+v", got)
	}
}

func TestDirectorySizeBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "b.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	size, ok := DirectorySizeBytes(dir, 100)
	if !ok || size != 8 {
		t.Fatalf("size=%d ok=%v, want 8/true", size, ok)
	}
}

func TestDirectorySizeBytesMissing(t *testing.T) {
	t.Parallel()
	size, ok := DirectorySizeBytes(filepath.Join(t.TempDir(), "missing"), 100)
	if !ok || size != 0 {
		t.Fatalf("missing size=%d ok=%v, want 0/true", size, ok)
	}
}

func TestDirectorySizeBytesEmptyRoot(t *testing.T) {
	t.Parallel()
	size, ok := DirectorySizeBytes("", 100)
	if ok || size != 0 {
		t.Fatalf("empty root size=%d ok=%v, want 0/false", size, ok)
	}
}
