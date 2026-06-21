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

func TestDirectorySizeBytesBounded_EmptyRoot(t *testing.T) {
	t.Parallel()
	size, ok, complete := DirectorySizeBytesBounded("", 100)
	if size != 0 || ok || complete {
		t.Fatalf("empty root = (%d, %v, %v), want (0, false, false)", size, ok, complete)
	}
}

func TestDirectorySizeBytesBounded_NonExistentRoot(t *testing.T) {
	t.Parallel()
	size, ok, complete := DirectorySizeBytesBounded(filepath.Join(t.TempDir(), "missing"), 100)
	// fs.WalkDir on non-existent root returns os.IsNotExist, which
	// maps to (0, true, true) — known-empty.
	if size != 0 || !ok || !complete {
		t.Fatalf("non-existent root = (%d, %v, %v), want (0, true, true)", size, ok, complete)
	}
}

func TestDirectorySizeBytesBounded_NestedDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("xx"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("yyy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "deep", "c.txt"), []byte("zzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	size, ok, complete := DirectorySizeBytesBounded(dir, 100)
	if !ok || !complete || size != 9 {
		t.Fatalf("nested dirs = (%d, %v, %v), want (9, true, true)", size, ok, complete)
	}
}

func TestDirectorySizeBytesBounded_UnreadableFile(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission test not meaningful")
	}
	dir := t.TempDir()
	// Create a file, then make the parent directory unreadable so
	// WalkDir's d.Info() fails. This covers the "err == nil" false
	// branch in the Info() call.
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "a.txt"), []byte("xx"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Remove read permission from subdir.
	if err := os.Chmod(subdir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(subdir, 0o755) })
	// WalkDir handles errors gracefully (returns nil from the callback),
	// so the scan should still complete but may not count the file.
	size, ok, complete := DirectorySizeBytesBounded(dir, 100)
	// The scan completes (WalkDir swallows errors), but the file in the
	// unreadable subdir may or may not be counted depending on OS.
	_ = size
	if !ok {
		t.Fatalf("unreadable subdir: ok should be true, got false")
	}
	_ = complete
}
