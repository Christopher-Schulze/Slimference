package filter

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultDataDir(t *testing.T) {
	t.Parallel()
	dir, err := DefaultDataDir()
	if err != nil {
		t.Fatalf("DefaultDataDir() error: %v", err)
	}
	if !strings.HasSuffix(dir, ".slimference") {
		t.Errorf("DefaultDataDir() = %q, want suffix .slimference", dir)
	}
}

func TestDefaultFilterDBPath(t *testing.T) {
	t.Parallel()
	path, err := DefaultFilterDBPath()
	if err != nil {
		t.Fatalf("DefaultFilterDBPath() error: %v", err)
	}
	if !strings.HasSuffix(path, "filter.db") {
		t.Errorf("DefaultFilterDBPath() = %q, want suffix filter.db", path)
	}
}

func TestDefaultTeeDir(t *testing.T) {
	t.Parallel()
	dir, err := DefaultTeeDir()
	if err != nil {
		t.Fatalf("DefaultTeeDir() error: %v", err)
	}
	if !strings.HasSuffix(dir, "tee") {
		t.Errorf("DefaultTeeDir() = %q, want suffix tee", dir)
	}
}

// TestDefaultPaths_homeDirError covers the os.UserHomeDir error paths in paths.go and filters_toml.go.
func TestDefaultPaths_homeDirError(t *testing.T) {
	// Not parallel: mutates package-level var userHomeDirFunc.
	old := userHomeDirFunc
	userHomeDirFunc = func() (string, error) { return "", errors.New("no home") }
	defer func() { userHomeDirFunc = old }()

	if _, err := DefaultDataDir(); err == nil {
		t.Error("DefaultDataDir: want error when UserHomeDir fails")
	}
	if _, err := DefaultFilterDBPath(); err == nil {
		t.Error("DefaultFilterDBPath: want error when UserHomeDir fails")
	}
	if _, err := DefaultTeeDir(); err == nil {
		t.Error("DefaultTeeDir: want error when UserHomeDir fails")
	}
	if got := UserFiltersPath(); got != "" {
		t.Errorf("UserFiltersPath: want empty string when UserHomeDir fails, got %q", got)
	}
}
