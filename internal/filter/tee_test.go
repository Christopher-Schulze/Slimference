package filter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTeeRecovery_mkdirError(t *testing.T) {
	t.Parallel()
	// Create a regular file at the path — MkdirAll will fail trying to make a dir on top of it
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := WriteTeeRecovery(filepath.Join(blocker, "sub"), []byte("out"), []byte("err"))
	if err == nil {
		t.Fatal("expected error when teeDir cannot be created")
	}
}

func TestWriteTeeRecovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := WriteTeeRecovery(dir, []byte("out"), []byte("err"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(p), "tee-") {
		t.Fatal(p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "out") || !strings.Contains(string(b), "err") {
		t.Fatal(string(b))
	}
}
