package filter

import (
	"os"
	"path/filepath"
)

// userHomeDirFunc is set to os.UserHomeDir; replaced in tests to inject errors.
var userHomeDirFunc = os.UserHomeDir

// DefaultDataDir returns ~/.tokenproxy (created by callers if needed).
func DefaultDataDir() (string, error) {
	h, err := userHomeDirFunc()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".tokenproxy"), nil
}

// DefaultFilterDBPath returns ~/.tokenproxy/filter.db.
func DefaultFilterDBPath() (string, error) {
	dir, err := DefaultDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "filter.db"), nil
}

// DefaultTeeDir returns ~/.tokenproxy/tee (raw output recovery).
func DefaultTeeDir() (string, error) {
	dir, err := DefaultDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tee"), nil
}
