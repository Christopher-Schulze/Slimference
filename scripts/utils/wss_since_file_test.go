package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseWSSSinceFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "since.txt")
	if err := os.WriteFile(path, []byte("2026-06-18T10:11:12Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseWSSSinceFile(path)
	if err != nil {
		t.Fatalf("parseWSSSinceFile() error = %v", err)
	}
	want := time.Date(2026, 6, 18, 10, 11, 12, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseWSSSinceFile() = %s, want %s", got, want)
	}
}

func TestParseWSSSinceFileRejectsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(invalid, []byte("not-rfc3339\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseWSSSinceFile(invalid); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("invalid since-file error = %v, want RFC3339 error", err)
	}
	missing := filepath.Join(dir, "missing.txt")
	if _, err := parseWSSSinceFile(missing); err == nil || !strings.Contains(err.Error(), "read --since-file") {
		t.Fatalf("missing since-file error = %v, want read error", err)
	}
}

func TestWSSSinceFileFlagParsers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "since.txt")
	if err := os.WriteFile(path, []byte("2026-06-18T10:11:12Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 6, 18, 10, 11, 12, 0, time.UTC)

	distributionFlags, err := parseWSSClassDistributionFlags([]string{"captures", "--since-file", path})
	if err != nil {
		t.Fatalf("parseWSSClassDistributionFlags() error = %v", err)
	}
	if distributionFlags.path != "captures" || !distributionFlags.since.Equal(want) {
		t.Fatalf("bad distribution flags: %+v", distributionFlags)
	}

	proofPackFlags, err := parseWSSProofPackFlags([]string{"captures", "--since-file=" + path})
	if err != nil {
		t.Fatalf("parseWSSProofPackFlags() error = %v", err)
	}
	if proofPackFlags.path != "captures" || !proofPackFlags.since.Equal(want) {
		t.Fatalf("bad proof-pack flags: %+v", proofPackFlags)
	}
}
