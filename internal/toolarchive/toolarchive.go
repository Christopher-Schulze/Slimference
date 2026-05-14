package toolarchive

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/sessions"
)

const (
	statsFilename = "stats.json"
	maxKeep       = 100
)

var (
	toolArchiveMkdirAll      = os.MkdirAll
	toolArchiveWriteFile     = os.WriteFile
	toolArchiveReadFile      = os.ReadFile
	toolArchiveReadDir       = os.ReadDir
	toolArchiveRemove        = os.Remove
	toolArchiveOpen          = os.Open
	toolArchiveMarshalIndent = json.MarshalIndent
	compressArchivePayload   = defaultCompressArchivePayload
	newArchiveGzipWriter     = func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) }
)

type Input struct {
	ToolName  string
	ToolUseID string
	SessionID string
	TurnID    string
	Command   string
	Output    string
	Preview   string
}

type Entry struct {
	ID         string    `json:"id"`
	URI        string    `json:"uri"`
	CreatedAt  time.Time `json:"created_at"`
	ToolName   string    `json:"tool_name,omitempty"`
	ToolUseID  string    `json:"tool_use_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	TurnID     string    `json:"turn_id,omitempty"`
	Command    string    `json:"command,omitempty"`
	Preview    string    `json:"preview"`
	OutputSize int       `json:"output_size"`
	StoredSize int64     `json:"stored_size"`
}

type Stats struct {
	Count        int       `json:"count"`
	Archived     int       `json:"archived"`
	Expanded     int       `json:"expanded"`
	BytesRaw     int64     `json:"bytes_raw"`
	BytesStored  int64     `json:"bytes_stored"`
	LastArchived time.Time `json:"last_archived"`
	LastExpanded time.Time `json:"last_expanded"`
}

func DefaultDir(home string) string {
	return filepath.Join(home, ".slimference", "tool-archive")
}

func Eligible(input Input) bool {
	if strings.TrimSpace(input.Output) == "" {
		return false
	}
	if strings.TrimSpace(input.ToolName) == "" && strings.TrimSpace(input.ToolUseID) == "" && strings.TrimSpace(input.SessionID) == "" {
		return false
	}
	if len(input.Output) >= 3000 {
		return true
	}
	return strings.Count(input.Output, "\n") >= 60
}

func Archive(dir string, input Input) (*Entry, error) {
	if !Eligible(input) {
		return nil, nil
	}
	if err := toolArchiveMkdirAll(entriesDir(dir), 0o755); err != nil {
		return nil, err
	}
	id := buildID(input)
	payloadPath := filepath.Join(entriesDir(dir), id+".txt.gz")
	metaPath := filepath.Join(entriesDir(dir), id+".json")

	payload, err := compressArchivePayload(input.Output)
	if err != nil {
		return nil, err
	}
	if err := toolArchiveWriteFile(payloadPath, payload, 0o644); err != nil {
		return nil, err
	}

	entry := &Entry{
		ID:         id,
		URI:        "local-archive://" + id,
		CreatedAt:  time.Now().UTC(),
		ToolName:   strings.TrimSpace(input.ToolName),
		ToolUseID:  strings.TrimSpace(input.ToolUseID),
		SessionID:  sessions.SafeOptionalSessionID(input.SessionID),
		TurnID:     sessions.SafeOptionalTurnID(input.TurnID),
		Command:    strings.TrimSpace(input.Command),
		Preview:    previewText(input),
		OutputSize: len(input.Output),
		StoredSize: int64(len(payload)),
	}
	data, err := toolArchiveMarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := toolArchiveWriteFile(metaPath, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	stats, err := LoadStats(dir)
	if err != nil {
		return nil, err
	}
	stats.Archived++
	stats.Count++
	stats.BytesRaw += int64(entry.OutputSize)
	stats.BytesStored += entry.StoredSize
	stats.LastArchived = entry.CreatedAt
	if err := SaveStats(dir, stats); err != nil {
		return nil, err
	}
	if err := trim(dir, maxKeep); err != nil {
		return nil, err
	}
	stats, err = Snapshot(dir)
	if err == nil {
		_ = SaveStats(dir, stats)
	}
	return entry, nil
}

func Expand(dir string, rawID string) (*Entry, []byte, error) {
	id := normalizeID(rawID)
	meta, err := loadEntry(dir, id)
	if err != nil {
		return nil, nil, err
	}
	f, err := toolArchiveOpen(filepath.Join(entriesDir(dir), id+".txt.gz"))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, err
	}
	defer gz.Close()
	body, err := io.ReadAll(gz)
	if err != nil {
		return nil, nil, err
	}
	stats, err := LoadStats(dir)
	if err == nil {
		stats.Expanded++
		stats.LastExpanded = time.Now().UTC()
		_ = SaveStats(dir, stats)
	}
	return meta, body, nil
}

func List(dir string) ([]Entry, error) {
	entries, err := toolArchiveReadDir(entriesDir(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := toolArchiveReadFile(filepath.Join(entriesDir(dir), entry.Name()))
		if err != nil {
			return nil, err
		}
		var meta Entry
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, err
		}
		out = append(out, meta)
	}
	slices.SortFunc(out, compareArchiveEntries)
	return out, nil
}

func LoadStats(dir string) (Stats, error) {
	data, err := toolArchiveReadFile(filepath.Join(dir, statsFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return Stats{}, nil
		}
		return Stats{}, err
	}
	var stats Stats
	if err := json.Unmarshal(data, &stats); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

func SaveStats(dir string, stats Stats) error {
	if err := toolArchiveMkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toolArchiveMarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	return toolArchiveWriteFile(filepath.Join(dir, statsFilename), append(data, '\n'), 0o644)
}

func Snapshot(dir string) (Stats, error) {
	stats, err := LoadStats(dir)
	if err != nil {
		return Stats{}, err
	}
	items, err := List(dir)
	if err != nil {
		return Stats{}, err
	}
	stats.Count = len(items)
	stats.BytesRaw = 0
	stats.BytesStored = 0
	for _, item := range items {
		stats.BytesRaw += int64(item.OutputSize)
		stats.BytesStored += item.StoredSize
	}
	if len(items) > 0 {
		stats.LastArchived = items[0].CreatedAt
	}
	return stats, nil
}

func RenderContext(entry Entry) string {
	base := "Large tool output archived locally."
	if entry.Command != "" {
		base = fmt.Sprintf("Large tool output archived locally for %q.", entry.Command)
	}
	bodyHint := ""
	if strings.Contains(entry.Preview, "AST-compacted by Slimference") {
		bodyHint = fmt.Sprintf("Body expand: slimference expand-body %s <symbol>\n", entry.ID)
	}
	return base + "\n" +
		fmt.Sprintf("Reference: %s\n", entry.URI) +
		fmt.Sprintf("Archive ID: %s\n", entry.ID) +
		bodyHint +
		"Preview:\n" + entry.Preview
}

func DefaultPreview(output string, limit int) string {
	if limit < 1 {
		limit = 600
	}
	text := strings.TrimSpace(output)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + fmt.Sprintf("\n[archived preview, full output available via local expand]")
}

func entriesDir(dir string) string {
	return filepath.Join(dir, "entries")
}

func buildID(input Input) string {
	if cleaned := sanitizeID(input.ToolUseID); cleaned != "" {
		return cleaned
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		input.ToolName,
		sessions.SafeOptionalSessionID(input.SessionID),
		sessions.SafeOptionalTurnID(input.TurnID),
		input.Command,
		trimForHash(input.Output),
	}, "\x00")))
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(sum[:6])
}

func previewText(input Input) string {
	if strings.TrimSpace(input.Preview) != "" {
		return strings.TrimSpace(input.Preview)
	}
	return DefaultPreview(input.Output, 600)
}

func compareArchiveEntries(a, b Entry) int {
	if a.CreatedAt.Before(b.CreatedAt) {
		return 1
	}
	if a.CreatedAt.After(b.CreatedAt) {
		return -1
	}
	return strings.Compare(b.ID, a.ID)
}

func trim(dir string, keep int) error {
	items, err := List(dir)
	if err != nil {
		return err
	}
	if keep < 0 || len(items) <= keep {
		return nil
	}
	for _, item := range items[keep:] {
		if err := toolArchiveRemove(filepath.Join(entriesDir(dir), item.ID+".json")); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := toolArchiveRemove(filepath.Join(entriesDir(dir), item.ID+".txt.gz")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func loadEntry(dir string, id string) (*Entry, error) {
	data, err := toolArchiveReadFile(filepath.Join(entriesDir(dir), id+".json"))
	if err != nil {
		return nil, err
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func normalizeID(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "slim://archive/")
	raw = strings.TrimPrefix(raw, "local-archive://")
	return sanitizeID(raw)
}

func sanitizeID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimForHash(s string) string {
	if len(s) <= 4096 {
		return s
	}
	return s[:4096]
}

func defaultCompressArchivePayload(output string) ([]byte, error) {
	var payload bytes.Buffer
	gz := newArchiveGzipWriter(&payload)
	if _, err := gz.Write([]byte(output)); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return payload.Bytes(), nil
}
