package tlsproof

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndLatestByProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if got := DefaultDir("/home/test"); got != "/home/test/.slimference/tls-proofs" {
		t.Fatalf("DefaultDir=%q", got)
	}
	oldTS := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	newTS := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	if _, err := Append(dir, Record{Profile: "chromium_stable", Timestamp: oldTS, Success: false, Notes: "h2 unproven"}); err != nil {
		t.Fatal(err)
	}
	path, err := Append(dir, Record{Profile: "chromium_stable", Host: "tls.peet.ws", Timestamp: newTS, Success: true, JA3Hash: "abc", JA4: "t13d", Reflector: "https://tls.peet.ws/api/all"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "chromium_stable.jsonl") {
		t.Fatalf("bad path: %s", path)
	}
	statuses, err := LatestByProfile(dir, newTS.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got := statuses["chromium_stable"]
	if !got.Success || got.JA3Hash != "abc" || got.AgeDays != 2 || got.Reflector == "" {
		t.Fatalf("bad status: %+v", got)
	}
	if names := ProfilesWithProof(statuses); len(names) != 1 || names[0] != "chromium_stable" {
		t.Fatalf("names=%v", names)
	}
}

func TestLatestByProfileMissingDir(t *testing.T) {
	t.Parallel()
	statuses, err := LatestByProfile(filepath.Join(t.TempDir(), "missing"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Fatalf("statuses=%v", statuses)
	}
}

func TestAppendValidationAndParseError(t *testing.T) {
	if _, err := Append("", Record{Profile: "x"}); err == nil {
		t.Fatal("expected empty dir error")
	}
	if _, err := Append(t.TempDir(), Record{}); err == nil {
		t.Fatal("expected empty profile error")
	}
	oldEncode := encodeJSONLine
	defer func() { encodeJSONLine = oldEncode }()
	encodeJSONLine = func(_ io.Writer, _ any) error { return errors.New("encode boom") }
	if _, err := Append(t.TempDir(), Record{Profile: "x"}); err == nil {
		t.Fatal("expected encode error")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.jsonl"), []byte("\n{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestByProfile(dir, time.Now()); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestAppendDefaultTimestampAndErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Append(dir, Record{Profile: "Bad Profile !"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad_profile__.jsonl")); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Append(filepath.Join(filePath, "child"), Record{Profile: "x"}); err == nil {
		t.Fatal("expected mkdir error")
	}
	blockedDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(blockedDir, "blocked.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Append(blockedDir, Record{Profile: "blocked"}); err == nil {
		t.Fatal("expected open-file error")
	}
	if _, err := LatestByProfile(filePath, time.Now()); err == nil {
		t.Fatal("expected read-dir error for file path")
	}
	mixedDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(mixedDir, "sub.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mixedDir, "skip.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	statuses, err := LatestByProfile(mixedDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Fatalf("statuses=%v", statuses)
	}
	if got := sanitize("!!!"); got != "___" {
		t.Fatalf("sanitize punctuation=%q", got)
	}
	if got := sanitize(""); got != "unknown" {
		t.Fatalf("sanitize empty=%q", got)
	}
}

func TestReadRecordsScannerError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.jsonl")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 2*1024*1024)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestByProfile(dir, time.Now()); err == nil {
		t.Fatal("expected scanner error")
	}
	if _, err := readRecords(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("expected open error")
	}
}
