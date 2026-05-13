// Package tlsproof stores local and reflected TLS fingerprint evidence.
package tlsproof

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const DirName = "tls-proofs"

var encodeJSONLine = func(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// Record is one TLS fingerprint proof observation. A failed record is useful:
// it prevents operators from mistaking "not measured" for "proved".
type Record struct {
	Profile        string    `json:"profile"`
	Host           string    `json:"host"`
	Transport      string    `json:"transport"`
	JA3            string    `json:"ja3,omitempty"`
	JA3Hash        string    `json:"ja3_hash,omitempty"`
	JA4            string    `json:"ja4,omitempty"`
	ALPN           string    `json:"alpn,omitempty"`
	H2SettingsHash string    `json:"h2_settings_hash,omitempty"`
	HTTPVersion    string    `json:"http_version,omitempty"`
	Reflector      string    `json:"reflector,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
	Success        bool      `json:"success"`
	Notes          string    `json:"notes,omitempty"`
}

// Status is the latest known evidence for one concrete profile.
type Status struct {
	Profile   string    `json:"profile"`
	Success   bool      `json:"success"`
	AgeDays   int       `json:"age_days"`
	Timestamp time.Time `json:"timestamp"`
	Reflector string    `json:"reflector,omitempty"`
	JA3Hash   string    `json:"ja3_hash,omitempty"`
	JA4       string    `json:"ja4,omitempty"`
	Notes     string    `json:"notes,omitempty"`
}

func DefaultDir(home string) string {
	return filepath.Join(home, ".slimference", DirName)
}

func Append(dir string, record Record) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("tls proof dir is empty")
	}
	if strings.TrimSpace(record.Profile) == "" {
		return "", fmt.Errorf("tls proof profile is empty")
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create tls proof dir: %w", err)
	}
	path := filepath.Join(dir, sanitize(record.Profile)+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("open tls proof log: %w", err)
	}
	defer f.Close() //nolint:errcheck
	if err := encodeJSONLine(f, record); err != nil {
		return "", fmt.Errorf("write tls proof: %w", err)
	}
	return path, nil
}

func LatestByProfile(dir string, now time.Time) (map[string]Status, error) {
	out := make(map[string]Status)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read tls proof dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		records, err := readRecords(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			current, ok := out[record.Profile]
			if !ok || record.Timestamp.After(current.Timestamp) {
				out[record.Profile] = statusFromRecord(record, now)
			}
		}
	}
	return out, nil
}

func ProfilesWithProof(statuses map[string]Status) []string {
	names := make([]string, 0, len(statuses))
	for name := range statuses {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func readRecords(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open tls proof log: %w", err)
	}
	defer f.Close() //nolint:errcheck
	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse tls proof %s: %w", path, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan tls proof %s: %w", path, err)
	}
	return records, nil
}

func statusFromRecord(record Record, now time.Time) Status {
	ageDays := 0
	if !record.Timestamp.IsZero() && now.After(record.Timestamp) {
		ageDays = int(now.Sub(record.Timestamp).Hours() / 24)
	}
	return Status{
		Profile:   record.Profile,
		Success:   record.Success,
		AgeDays:   ageDays,
		Timestamp: record.Timestamp,
		Reflector: record.Reflector,
		JA3Hash:   record.JA3Hash,
		JA4:       record.JA4,
		Notes:     record.Notes,
	}
}

func sanitize(profile string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(profile) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
