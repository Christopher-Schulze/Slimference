package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// wssInventoryPaths walks root for decisions.jsonl files (or
// *.decisions.jsonl). When root is a single file it returns [root].
func wssInventoryPaths(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return []string{root}, nil
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == "decisions.jsonl" || strings.HasSuffix(base, ".decisions.jsonl") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	sort.Strings(paths)
	return paths, nil
}

// wssInventoryName returns the parent directory name of path, or the
// file's base name if there is no meaningful parent.
func wssInventoryName(path string) string {
	parent := filepath.Base(filepath.Dir(path))
	if parent != "." && parent != string(filepath.Separator) && parent != "" {
		return parent
	}
	return filepath.Base(path)
}
