// Package debug holds observability helpers (session replay, JSONL decisions tooling).
package debug

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SessionFileStats counts non-empty lines and returns file size for a JSONL session file.
// The path must exist and must not be a directory.
func SessionFileStats(path string) (nonEmptyLines int, size int64, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if st.IsDir() {
		return 0, 0, fmt.Errorf("%s is a directory", path)
	}
	size = st.Size()
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			nonEmptyLines++
		}
	}
	if err := sc.Err(); err != nil {
		return 0, size, err
	}
	return nonEmptyLines, size, nil
}

// ReplaySession reads all RequestSummary records from a JSONL decisions log file.
// Returns records in file order (oldest first). Malformed or non-summary lines are silently skipped.
func ReplaySession(path string) ([]RequestSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []RequestSummary
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s RequestSummary
		if json.Unmarshal([]byte(line), &s) == nil {
			out = append(out, s)
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}
