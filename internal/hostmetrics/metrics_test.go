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

func TestDirectorySizeBytesBoundedReportsIncompleteScan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	size, ok, complete := DirectorySizeBytesBounded(dir, 2)
	if !ok || complete || size <= 0 {
		t.Fatalf("bounded scan size=%d ok=%v complete=%v, want partial known incomplete", size, ok, complete)
	}
}

func TestDirectorySizeBytesBoundedDefaultMaxEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("xx"), 0o600); err != nil {
		t.Fatal(err)
	}
	// maxEntries <= 0 falls back to the 20_000 default; a small tree must complete.
	size, ok, complete := DirectorySizeBytesBounded(dir, 0)
	if !ok || !complete || size != 2 {
		t.Fatalf("default maxEntries size=%d ok=%v complete=%v, want 2/true/true", size, ok, complete)
	}
}

func TestParsePSRSSKilobytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in        string
		wantOK    bool
		wantBytes int64
	}{
		{"", false, 0},
		{"   \n  ", false, 0},
		{"notanumber", false, 0},
		{"0", false, 0},
		{"-5", false, 0},
		{"12345", true, 12345 * 1024},
		{"  9876  extra  fields  ", true, 9876 * 1024},
	}
	for _, tc := range cases {
		got, ok := parsePSRSSKilobytes(tc.in)
		if ok != tc.wantOK || got != tc.wantBytes {
			t.Fatalf("parsePSRSSKilobytes(%q) = (%d, %t), want (%d, %t)", tc.in, got, ok, tc.wantBytes, tc.wantOK)
		}
	}
}
