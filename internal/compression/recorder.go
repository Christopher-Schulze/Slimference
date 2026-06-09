package compression

import (
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
)

// MutationRecorder is the contract that lossy Layer 1 sub-layers use to
// archive original block content before mutation. Concrete implementations
// live in internal/contentarchive (production) and tests can supply a stub.
//
// The interface is deliberately tiny: callers either get back an archive id
// they can stamp on the rewritten block, or an empty string when archiving
// was skipped or failed. T76 design contract.
type MutationRecorder interface {
	Record(input contentarchive.Input) (string, error)
}

// noopRecorder is the default MutationRecorder used when archiving is not
// configured. It is functionally a nil-safe no-op: every Record call returns
// "" without touching disk.
type noopRecorder struct{}

func (noopRecorder) Record(contentarchive.Input) (string, error) { return "", nil }

// NoopRecorder is the canonical MutationRecorder for "no archiving".
var NoopRecorder MutationRecorder = noopRecorder{}

// DiskRecorder writes archive entries through the contentarchive package.
// Constructed once per proxy lifetime and reused across all Layer 1 calls.
type DiskRecorder struct {
	dir    string
	limits contentarchive.Limits
}

// NewDiskRecorder builds a recorder rooted at dir. Pass an empty Limits to
// use defaults (5000 entries / 64 MiB).
func NewDiskRecorder(dir string, limits contentarchive.Limits) *DiskRecorder {
	return &DiskRecorder{dir: dir, limits: limits}
}

// Record archives input and returns the archive id. On any error the id
// is empty so callers can keep operating without the archive: archiving is
// best-effort and must never break the hot path.
func (r *DiskRecorder) Record(input contentarchive.Input) (string, error) {
	if r == nil || r.dir == "" {
		return "", nil
	}
	entry, err := contentarchive.Put(r.dir, input, r.limits)
	if err != nil || entry == nil {
		return "", err
	}
	return entry.ID, nil
}

// archiveOriginal is the helper Layer 1 sub-layers call before applying a
// lossy mutation. msgIdx and blockIdx scope the archive entry; subLayer is
// a free-form tag used in metadata (e.g. "preview_pass", "comment_strip").
// A zero-length original or an inactive recorder both result in an empty
// archive id - callers must tolerate that. SessionID is read from the
// compressor's activeSessionID, set per-call by CompressWithSession.
func (c *DeterministicCompressor) archiveOriginal(msgIdx, blockIdx int, subLayer, original string) string {
	if c.recorder == nil || c.recorder == NoopRecorder {
		return ""
	}
	if original == "" {
		return ""
	}
	c.recordMu.Lock()
	id, err := c.recorder.Record(contentarchive.Input{
		SessionID:    c.activeSessionID,
		MessageIndex: msgIdx,
		BlockIndex:   blockIdx,
		SubLayer:     subLayer,
		Original:     original,
	})
	if err == nil && id != "" {
		c.recordArchiveWriteLocked(subLayer)
	}
	c.recordMu.Unlock()
	if err != nil {
		return ""
	}
	return id
}

func (c *DeterministicCompressor) recordArchiveWriteLocked(subLayer string) {
	if c.activeArchiveWrites == nil {
		return
	}
	for _, part := range strings.Split(subLayer, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		c.activeArchiveWrites[id]++
	}
}

func (c *DeterministicCompressor) recordLayer1Attempt(subLayer string) {
	if subLayer == "" {
		return
	}
	c.recordMu.Lock()
	if c.activeAttempts != nil {
		c.activeAttempts[subLayer]++
	}
	c.recordMu.Unlock()
}

func (c *DeterministicCompressor) snapshotArchiveWrites() map[string]int {
	c.recordMu.Lock()
	defer c.recordMu.Unlock()
	if len(c.activeArchiveWrites) == 0 {
		return nil
	}
	out := make(map[string]int, len(c.activeArchiveWrites))
	for id, count := range c.activeArchiveWrites {
		out[id] = count
	}
	return out
}

func (c *DeterministicCompressor) snapshotLayer1Attempts() map[string]int {
	c.recordMu.Lock()
	defer c.recordMu.Unlock()
	if len(c.activeAttempts) == 0 {
		return nil
	}
	out := make(map[string]int, len(c.activeAttempts))
	for id, count := range c.activeAttempts {
		out[id] = count
	}
	return out
}

// WithRecorder returns the receiver after wiring an archive recorder. The
// compressor stays usable without a recorder; this only configures the
// optional T76 archive integration. Session scope is set per-call via
// CompressWithSession.
func (c *DeterministicCompressor) WithRecorder(rec MutationRecorder) *DeterministicCompressor {
	c.recorder = rec
	return c
}
