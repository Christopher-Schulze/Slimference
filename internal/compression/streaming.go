package compression

import (
	"bufio"
	"hash/fnv"
	"io"
	"strconv"
	"strings"
)

// StreamingOptions tunes the chunk pipeline. Zero values fall back to
// sensible defaults so callers can pass the empty struct for the
// out-of-the-box pipeline. T108.
type StreamingOptions struct {
	// WindowLines is the rolling de-dup window in lines. The pipeline
	// collapses a line if its hash matches one already seen inside the
	// window. Default 500.
	WindowLines int
	// StripANSI enables ANSI-escape stripping per chunk (idempotent;
	// safe for streaming). Default true.
	StripANSI bool
	// CollapseRepeated enables consecutive-identical-line collapse
	// (`<line> (xN)`). Default true.
	CollapseRepeated bool
}

// StreamingStats records counters for one StreamingCompress call so
// tests and operators can verify the chunked pipeline behaves like the
// whole-body baseline. T108.
type StreamingStats struct {
	LinesIn        int
	LinesOut       int
	BytesIn        int64
	BytesOut       int64
	DedupedLines   int
	CollapsedRuns  int
	ANSISaved      int
	PeakWindowSize int
}

// StreamingCompress copies r to w, applying the streaming-safe Layer 1
// sub-layers in a single pass with a bounded memory ceiling. Reader is
// consumed line by line via bufio.Scanner so the pipeline never
// allocates more than a constant multiple of `WindowLines` lines plus
// one scanner buffer (default 64 KiB; raised to 1 MiB so a single
// pathological log line cannot stall the pipeline). T108.
//
// The returned stats reflect what actually happened so the caller can
// assert the memory-ceiling invariant (PeakWindowSize <= WindowLines)
// or compare token counts against the whole-body baseline.
func StreamingCompress(r io.Reader, w io.Writer, opts StreamingOptions) (StreamingStats, error) {
	if opts.WindowLines <= 0 {
		opts.WindowLines = 500
	}
	// Default StripANSI / CollapseRepeated to true via the zero-value
	// trick: the caller can opt out by setting the field to false on
	// a non-zero struct. Zero values therefore default to the most
	// useful pipeline.
	if !opts.StripANSI && !opts.CollapseRepeated && opts.WindowLines == 500 {
		opts.StripANSI = true
		opts.CollapseRepeated = true
	}

	stats := StreamingStats{}
	scanner := bufio.NewScanner(r)
	// Default buffer is 64 KiB; raise to 1 MiB so a single 200 KB log
	// line does not blow up the scanner.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Bounded ring of recently seen line hashes.
	seen := make(map[uint64]struct{}, opts.WindowLines)
	order := make([]uint64, 0, opts.WindowLines)

	var prevLine string
	var prevHash uint64
	var prevCount int
	flushPrev := func() error {
		if prevCount == 0 {
			return nil
		}
		out := prevLine
		if prevCount > 1 {
			out = prevLine + " (x" + strconv.Itoa(prevCount) + ")"
			stats.CollapsedRuns++
		}
		out += "\n"
		n, err := io.WriteString(w, out)
		stats.BytesOut += int64(n)
		stats.LinesOut++
		prevCount = 0
		return err
	}

	for scanner.Scan() {
		raw := scanner.Text()
		stats.LinesIn++
		stats.BytesIn += int64(len(raw)) + 1
		line := raw
		if opts.StripANSI {
			stripped := StripANSICodes(line)
			if len(stripped) < len(line) {
				stats.ANSISaved += len(line) - len(stripped)
				line = stripped
			}
		}
		hash := hashLine(line)

		// Repeated-run collapse: identical to the previous line. Runs
		// before the rolling-window dedup so "A A A" produces a
		// single `A (x3)` rather than one `A` plus two silent dedups.
		if opts.CollapseRepeated && prevCount > 0 && hash == prevHash {
			prevCount++
			continue
		}

		// Window dedup: skip if seen inside the rolling window.
		if _, dup := seen[hash]; dup {
			stats.DedupedLines++
			continue
		}

		// Flush the previous run before emitting the new line.
		if err := flushPrev(); err != nil {
			return stats, err
		}
		prevLine = line
		prevHash = hash
		prevCount = 1

		// Slot the line into the window; evict the oldest when full.
		// Eviction runs before the peak update so PeakWindowSize stays
		// bounded by WindowLines.
		seen[hash] = struct{}{}
		order = append(order, hash)
		if len(order) > opts.WindowLines {
			drop := order[0]
			order = order[1:]
			delete(seen, drop)
		}
		if len(order) > stats.PeakWindowSize {
			stats.PeakWindowSize = len(order)
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}
	if err := flushPrev(); err != nil {
		return stats, err
	}
	return stats, nil
}

// hashLine returns a stable 64-bit hash of s. FNV-1a is fine for the
// dedup window; collisions are bounded by WindowLines so even a
// catastrophic 1-in-2^32 collision rate is harmless.
func hashLine(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// IsStreamingSafe reports whether a sub-layer name is safe to run on
// the chunked pipeline. T108 WP2.
func IsStreamingSafe(subLayer string) bool {
	switch subLayer {
	case "ansi_strip", "line_dedup", "repeated_collapse":
		return true
	}
	return false
}

// StreamingSafeNames lists the sub-layers the streaming pipeline runs
// today. Returned as a sorted slice so tests and telemetry can assert
// on the exact shape. T108 WP2.
func StreamingSafeNames() []string {
	return []string{"ansi_strip", "line_dedup", "repeated_collapse"}
}

// JoinStreamingSafe formats the safe-list as a comma-separated string
// for telemetry / log lines.
func JoinStreamingSafe() string {
	return strings.Join(StreamingSafeNames(), ",")
}
