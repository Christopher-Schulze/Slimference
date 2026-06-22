package main

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// wssReplayFiles walks path for .jsonl files. When path is a single file,
// it returns [path] and single=true.
func wssReplayFiles(path string) ([]string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, true, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("scan %s: %w", path, err)
	}
	sort.Strings(files)
	return files, false, nil
}

// silenceReplayLogs redirects the default slog logger to io.Discard and
// returns a restore function. Used by replay-based tools to suppress noisy
// replay diagnostics.
func silenceReplayLogs() func() {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return func() {
		slog.SetDefault(previous)
	}
}

// positiveDelta returns max(0, candidate-baseline).
func positiveDelta(candidate, baseline int) int {
	if candidate <= baseline {
		return 0
	}
	return candidate - baseline
}
