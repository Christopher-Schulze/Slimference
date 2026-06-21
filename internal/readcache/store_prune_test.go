package readcache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSessionFileWithAge(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPruneSessions_RemovesByAge(t *testing.T) {
	dir := t.TempDir()
	old := writeSessionFileWithAge(t, dir, "old.json", 30*24*time.Hour)
	fresh := writeSessionFileWithAge(t, dir, "fresh.json", 1*time.Hour)
	n, err := PruneSessions(dir, 1000, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned = %d, want 1", n)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old session should be pruned by age")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh session should survive: %v", err)
	}
}

func TestPruneSessions_RemovesByCount(t *testing.T) {
	dir := t.TempDir()
	// s0 is oldest (5h) ... s4 newest (1h). maxAge huge so only the count cap applies.
	paths := make([]string, 5)
	for i := range 5 {
		paths[i] = writeSessionFileWithAge(t, dir, fmt.Sprintf("s%d.json", i), time.Duration(5-i)*time.Hour)
	}
	n, err := PruneSessions(dir, 2, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("pruned = %d, want 3", n)
	}
	for i := range 3 {
		if _, err := os.Stat(paths[i]); !os.IsNotExist(err) {
			t.Fatalf("oldest session s%d should be pruned", i)
		}
	}
	for i := 3; i < 5; i++ {
		if _, err := os.Stat(paths[i]); err != nil {
			t.Fatalf("newest session s%d should survive: %v", i, err)
		}
	}
}

func TestPruneSessions_DefaultsAndNonJSONIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-.json file and a subdir must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	n, err := PruneSessions(dir, 0, 0) // defaults
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("pruned = %d, want 0 (nothing eligible)", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatalf("non-json file must not be pruned: %v", err)
	}
}

func TestPruneSessions_NoDir(t *testing.T) {
	n, err := PruneSessions(filepath.Join(t.TempDir(), "does-not-exist"), 0, 0)
	if err != nil || n != 0 {
		t.Fatalf("missing dir: n=%d err=%v", n, err)
	}
}

func TestPruneSessions_RemoveError(t *testing.T) {
	dir := t.TempDir()
	writeSessionFileWithAge(t, dir, "old.json", 30*24*time.Hour)
	saved := readCacheRemove
	t.Cleanup(func() { readCacheRemove = saved })
	readCacheRemove = func(string) error { return errors.New("remove fail") }
	if _, err := PruneSessions(dir, 1000, 14*24*time.Hour); err == nil {
		t.Fatal("expected remove error to surface")
	}
}

func TestPruneSessions_ReadDirError(t *testing.T) {
	saved := readCacheReadDir
	t.Cleanup(func() { readCacheReadDir = saved })
	readCacheReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("read dir fail") }
	if _, err := PruneSessions(t.TempDir(), 0, 0); err == nil {
		t.Fatal("expected read dir error to surface")
	}
}
