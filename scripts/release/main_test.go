package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	want := []byte("hello release pipeline")
	if err := os.WriteFile(src, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile err: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: %q vs %q", got, want)
	}
}

func TestCopyFile_MissingSource(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected error on missing src")
	}
}

func TestCopyFile_UnwritableDest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Destination path points inside a nonexistent directory to force
	// os.Create failure.
	err := copyFile(src, filepath.Join(dir, "nonexistent-dir", "dst.txt"))
	if err == nil {
		t.Fatal("expected error on unwritable dest")
	}
}

func TestTarGzDir_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"a.txt":       []byte("alpha"),
		"sub/b.txt":   []byte("bravo"),
		"sub/c/c.bin": []byte{0x00, 0x01, 0x02},
	}
	for rel, data := range files {
		abs := filepath.Join(bundle, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	archive := filepath.Join(dir, "bundle.tar.gz")
	if err := tarGzDir(bundle, archive); err != nil {
		t.Fatalf("tarGzDir err: %v", err)
	}

	// Read it back and confirm every file is present with matching bytes.
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	seen := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(tr)
		seen[hdr.Name] = body
	}

	for rel, data := range files {
		key := filepath.Join("bundle", rel)
		got, ok := seen[key]
		if !ok {
			t.Errorf("missing %s in archive (saw %v)", key, keysOf(seen))
			continue
		}
		if string(got) != string(data) {
			t.Errorf("content mismatch for %s", key)
		}
	}
}

func TestWriteSHA256Sums_MatchesManualHash(t *testing.T) {
	dir := t.TempDir()
	archives := []string{
		filepath.Join(dir, "a.tar.gz"),
		filepath.Join(dir, "b.tar.gz"),
	}
	contents := map[string][]byte{
		archives[0]: []byte("payload-a"),
		archives[1]: []byte("payload-b"),
	}
	for p, c := range contents {
		if err := os.WriteFile(p, c, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "SHA256SUMS")
	if err := writeSHA256Sums(archives, out); err != nil {
		t.Fatalf("writeSHA256Sums: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for p, c := range contents {
		sum := sha256.Sum256(c)
		want := hex.EncodeToString(sum[:]) + "  " + filepath.Base(p)
		if !strings.Contains(string(got), want) {
			t.Errorf("SHA256SUMS missing line %q\nfull content:\n%s", want, got)
		}
	}
}

func TestWriteSHA256Sums_MissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	err := writeSHA256Sums(
		[]string{filepath.Join(dir, "nope.tar.gz")},
		filepath.Join(dir, "SHA256SUMS"),
	)
	if err == nil {
		t.Fatal("expected error for missing archive")
	}
}

func TestResolveTargets_Default(t *testing.T) {
	ts, err := resolveTargets("")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].os != "darwin" || ts[0].arch != "arm64" {
		t.Fatalf("default targets = %+v, want [darwin/arm64]", ts)
	}
}

func TestResolveTargets_Primary(t *testing.T) {
	ts, err := resolveTargets("primary")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].os != "darwin" || ts[0].arch != "arm64" {
		t.Fatalf("primary = %+v", ts)
	}
}

func TestResolveTargets_All(t *testing.T) {
	ts, err := resolveTargets("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != len(allTargets) {
		t.Fatalf("all = %d, want %d", len(ts), len(allTargets))
	}
}

func TestResolveTargets_Custom(t *testing.T) {
	ts, err := resolveTargets("darwin/arm64,linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 {
		t.Fatalf("custom = %d, want 2", len(ts))
	}
	if ts[1].os != "linux" || ts[1].arch != "amd64" {
		t.Fatalf("second target = %+v", ts[1])
	}
}

func TestResolveTargets_InvalidSpec(t *testing.T) {
	if _, err := resolveTargets("weird-format"); err == nil {
		t.Fatal("expected error on missing slash")
	}
	if _, err := resolveTargets(","); err == nil {
		t.Fatal("expected error on empty list")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
