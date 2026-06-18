package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func parseWSSSinceFile(path string) (time.Time, error) {
	if strings.TrimSpace(path) == "" {
		return time.Time{}, fmt.Errorf("--since-file path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("read --since-file %s: %w", path, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return time.Time{}, fmt.Errorf("--since-file must contain RFC3339")
	}
	since, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since-file must contain RFC3339: %w", err)
	}
	return since, nil
}
