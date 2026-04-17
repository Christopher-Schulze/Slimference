package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestModel_CopyDebugLog_WritesPrivateExport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	proxy.sessionLogger.Log("INFO", "test", "hello")
	model := NewModel(proxy)

	path := model.copyDebugLog()
	if path == "" {
		t.Fatal("expected exported debug log path")
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat export dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("export dir perms = %o, want 700", dirInfo.Mode().Perm())
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat export file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("export file perms = %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestModel_CopyDebugLog_ChmodFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	proxy.sessionLogger.Log("INFO", "test", "hello")
	model := NewModel(proxy)

	origChmod := chmodFn
	t.Cleanup(func() { chmodFn = origChmod })

	chmodFn = func(path string, mode os.FileMode) error {
		if filepath.Base(path) == "exports" {
			return errors.New("chmod dir boom")
		}
		return origChmod(path, mode)
	}
	if path := model.copyDebugLog(); path != "" {
		t.Fatalf("expected empty path on export-dir chmod failure, got %q", path)
	}

	chmodFn = func(path string, mode os.FileMode) error {
		if filepath.Base(path) != "exports" {
			return errors.New("chmod file boom")
		}
		return origChmod(path, mode)
	}
	if path := model.copyDebugLog(); path != "" {
		t.Fatalf("expected empty path on export-file chmod failure, got %q", path)
	}
}
