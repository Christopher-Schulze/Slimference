// Package contentarchive stores the original bytes of content blocks before
// Layer 1 sub-layers apply lossy mutations (comment-strip, dedup,
// structure-extract, preview, etc.). The archive complements
// internal/toolarchive (which scopes large *tool result* outputs) by giving
// every lossy in-message mutation a reversible "local-archive://<id>"
// reference. This unblocks aggressive defaults (T76, T74, T103, T100).
package contentarchive

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
)

const (
	statsFilename       = "stats.json"
	defaultMaxEntries   = 5000
	defaultMaxBytes     = int64(64 * 1024 * 1024)
	previewByteLimit    = 600
	uriScheme           = "local-archive://"
	legacyURIScheme     = "slim://archive/"
)

// Indirection points for tests so error paths can be exercised without
// platform-specific tricks. They mirror the toolarchive package layout.
var (
	mkdirAll      = os.MkdirAll
	writeFile     = os.WriteFile
	readFile      = os.ReadFile
	readDir       = os.ReadDir
	removeFile    = os.Remove
	openFile      = os.Open
	marshalIndent = json.MarshalIndent
	compressBytes = defaultCompressBytes
	newGzipWriter = func(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) }
)

// Input describes a lossy content mutation that is about to happen.
type Input struct {
	SessionID    string
	MessageIndex int
	BlockIndex   int
	SubLayer     string
	Original     string
	Preview      string
}

// Entry is the on-disk metadata for a single archived content mutation.
type Entry struct {
	ID           string    `json:"id"`
	URI          string    `json:"uri"`
	CreatedAt    time.Time `json:"created_at"`
	SessionID    string    `json:"session_id,omitempty"`
	MessageIndex int       `json:"message_index"`
	BlockIndex   int       `json:"block_index"`
	SubLayer     string    `json:"sub_layer,omitempty"`
	Preview      string    `json:"preview"`
	OriginalSize int       `json:"original_size"`
	StoredSize   int64     `json:"stored_size"`
}

// Stats captures aggregate counters for the archive directory.
type Stats struct {
	Count          int       `json:"count"`
	Archived       int       `json:"archived"`
	Expanded       int       `json:"expanded"`
	ReInjectCount  int       `json:"re_inject_count"`
	Evictions      int       `json:"evictions"`
	BytesRaw       int64     `json:"bytes_raw"`
	BytesStored    int64     `json:"bytes_stored"`
	LastArchived   time.Time `json:"last_archived"`
	LastExpanded   time.Time `json:"last_expanded"`
	LastReInjected time.Time `json:"last_re_injected"`
}

// Limits configures eviction thresholds. Zero values fall back to defaults.
type Limits struct {
	MaxEntries int
	MaxBytes   int64
}

func (l Limits) maxEntries() int {
	if l.MaxEntries <= 0 {
		return defaultMaxEntries
	}
	return l.MaxEntries
}

func (l Limits) maxBytes() int64 {
	if l.MaxBytes <= 0 {
		return defaultMaxBytes
	}
	return l.MaxBytes
}

// DefaultDir returns the canonical on-disk archive root.
func DefaultDir(home string) string {
	return filepath.Join(home, ".slimference", "content-archive")
}

// Eligible reports whether the input is worth archiving. Trivial inputs
// (empty, very short) are skipped because the recovery cost would exceed
// the saved tokens.
func Eligible(input Input) bool {
	if strings.TrimSpace(input.Original) == "" {
		return false
	}
	return len(input.Original) >= 64
}

// Put archives the original bytes for a lossy mutation and returns the
// metadata entry. Inputs that fail Eligible() return (nil, nil) so callers
// can no-op cheaply.
func Put(dir string, input Input, limits Limits) (*Entry, error) {
	if !Eligible(input) {
		return nil, nil
	}
	if err := mkdirAll(entriesDir(dir), 0o755); err != nil {
		return nil, err
	}
	id := buildID(input)
	payloadPath := filepath.Join(entriesDir(dir), id+".txt.gz")
	metaPath := filepath.Join(entriesDir(dir), id+".json")

	payload, err := compressBytes(input.Original)
	if err != nil {
		return nil, err
	}
	if err := writeFile(payloadPath, payload, 0o644); err != nil {
		return nil, err
	}

	entry := &Entry{
		ID:           id,
		URI:          uriScheme + id,
		CreatedAt:    time.Now().UTC(),
		SessionID:    strings.TrimSpace(input.SessionID),
		MessageIndex: input.MessageIndex,
		BlockIndex:   input.BlockIndex,
		SubLayer:     strings.TrimSpace(input.SubLayer),
		Preview:      previewText(input),
		OriginalSize: len(input.Original),
		StoredSize:   int64(len(payload)),
	}
	data, err := marshalIndent(entry, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFile(metaPath, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}

	stats, err := LoadStats(dir)
	if err != nil {
		return nil, err
	}
	stats.Archived++
	stats.Count++
	stats.BytesRaw += int64(entry.OutputSize())
	stats.BytesStored += entry.StoredSize
	stats.LastArchived = entry.CreatedAt
	if err := SaveStats(dir, stats); err != nil {
		return nil, err
	}
	if evicted, err := enforceLimits(dir, limits); err != nil {
		return nil, err
	} else if evicted > 0 {
		stats.Evictions += evicted
		_ = SaveStats(dir, stats)
	}
	if snap, err := Snapshot(dir); err == nil {
		snap.Archived = stats.Archived
		snap.Expanded = stats.Expanded
		snap.ReInjectCount = stats.ReInjectCount
		snap.Evictions = stats.Evictions
		snap.LastArchived = stats.LastArchived
		snap.LastExpanded = stats.LastExpanded
		snap.LastReInjected = stats.LastReInjected
		_ = SaveStats(dir, snap)
	}
	return entry, nil
}

// OutputSize is a small helper so callers don't import json tag names.
func (e *Entry) OutputSize() int { return e.OriginalSize }

// Get loads the metadata + decompressed payload for an archive id. The id
// may be the bare token, the local-archive:// URI, or the legacy
// slim://archive/ URI.
func Get(dir string, rawID string) (*Entry, []byte, error) {
	id := normalizeID(rawID)
	if id == "" {
		return nil, nil, fmt.Errorf("empty archive id")
	}
	meta, err := loadEntry(dir, id)
	if err != nil {
		return nil, nil, err
	}
	f, err := openFile(filepath.Join(entriesDir(dir), id+".txt.gz"))
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

// RecordReInject increments the re-injection counter for telemetry. Best
// effort: errors are silently swallowed because telemetry must never block
// the hot path.
func RecordReInject(dir string) {
	stats, err := LoadStats(dir)
	if err != nil {
		return
	}
	stats.ReInjectCount++
	stats.LastReInjected = time.Now().UTC()
	_ = SaveStats(dir, stats)
}

// List returns every stored entry, newest first.
func List(dir string) ([]Entry, error) {
	entries, err := readDir(entriesDir(dir))
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
		data, err := readFile(filepath.Join(entriesDir(dir), entry.Name()))
		if err != nil {
			return nil, err
		}
		var meta Entry
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, err
		}
		out = append(out, meta)
	}
	slices.SortFunc(out, compareEntries)
	return out, nil
}

// LoadStats reads stats.json or returns a zero-value Stats when absent.
func LoadStats(dir string) (Stats, error) {
	data, err := readFile(filepath.Join(dir, statsFilename))
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

// SaveStats writes stats.json atomically.
func SaveStats(dir string, stats Stats) error {
	if err := mkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := marshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, statsFilename), append(data, '\n'), 0o644)
}

// Snapshot rebuilds aggregate counts from the on-disk entry list. The
// derived fields (Count, BytesRaw, BytesStored, LastArchived) are refreshed;
// archive/expand/re-inject totals are preserved by callers because they live
// in the running stats.
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
		stats.BytesRaw += int64(item.OriginalSize)
		stats.BytesStored += item.StoredSize
	}
	if len(items) > 0 {
		stats.LastArchived = items[0].CreatedAt
	}
	return stats, nil
}

// Reference renders the canonical context line a sub-layer can splice into
// the mutated content so the model sees the recovery handle.
func Reference(entry *Entry) string {
	if entry == nil {
		return ""
	}
	return fmt.Sprintf("[archived: %s]", entry.URI)
}

func entriesDir(dir string) string {
	return filepath.Join(dir, "entries")
}

func buildID(input Input) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		input.SessionID,
		input.SubLayer,
		fmt.Sprintf("m%d:b%d", input.MessageIndex, input.BlockIndex),
		trimForHash(input.Original),
	}, "\x00")))
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(sum[:6])
}

func previewText(input Input) string {
	if strings.TrimSpace(input.Preview) != "" {
		return strings.TrimSpace(input.Preview)
	}
	return DefaultPreview(input.Original, previewByteLimit)
}

// DefaultPreview renders a short head of the original content for the
// metadata preview field.
func DefaultPreview(s string, limit int) string {
	if limit < 1 {
		limit = previewByteLimit
	}
	text := strings.TrimSpace(s)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n[archived preview, full output via slimference expand]"
}

func compareEntries(a, b Entry) int {
	if a.CreatedAt.Before(b.CreatedAt) {
		return 1
	}
	if a.CreatedAt.After(b.CreatedAt) {
		return -1
	}
	return strings.Compare(b.ID, a.ID)
}

func enforceLimits(dir string, limits Limits) (int, error) {
	items, err := List(dir)
	if err != nil {
		return 0, err
	}
	maxEntries := limits.maxEntries()
	maxBytes := limits.maxBytes()

	evicted := 0
	cumulativeBytes := int64(0)
	for _, item := range items {
		cumulativeBytes += item.StoredSize
	}
	// Walk oldest-to-newest from the tail; List() returns newest first.
	for i := len(items) - 1; i >= 0; i-- {
		if len(items)-evicted <= maxEntries && cumulativeBytes <= maxBytes {
			break
		}
		item := items[i]
		if err := removeFile(filepath.Join(entriesDir(dir), item.ID+".json")); err != nil && !os.IsNotExist(err) {
			return evicted, err
		}
		if err := removeFile(filepath.Join(entriesDir(dir), item.ID+".txt.gz")); err != nil && !os.IsNotExist(err) {
			return evicted, err
		}
		cumulativeBytes -= item.StoredSize
		evicted++
	}
	return evicted, nil
}

func loadEntry(dir, id string) (*Entry, error) {
	data, err := readFile(filepath.Join(entriesDir(dir), id+".json"))
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
	raw = strings.TrimPrefix(raw, uriScheme)
	raw = strings.TrimPrefix(raw, legacyURIScheme)
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

func defaultCompressBytes(s string) ([]byte, error) {
	var payload bytes.Buffer
	gz := newGzipWriter(&payload)
	if _, err := gz.Write([]byte(s)); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return payload.Bytes(), nil
}
