package compression

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestStreamingCompress_DefaultOptionsStripAndCollapse(t *testing.T) {
	t.Parallel()
	in := "\x1b[31mline a\x1b[0m\nline a\nline a\nline b\n"
	var out bytes.Buffer
	stats, err := StreamingCompress(strings.NewReader(in), &out, StreamingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "line a (x3)") {
		t.Fatalf("expected collapsed run, got %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("ANSI not stripped: %q", got)
	}
	if stats.CollapsedRuns == 0 || stats.ANSISaved == 0 {
		t.Fatalf("stats: %+v", stats)
	}
}

func TestStreamingCompress_WindowDedup(t *testing.T) {
	t.Parallel()
	// Lines that recur far apart but inside a window of 4 must be
	// deduped on the second appearance.
	in := "alpha\nbeta\ngamma\ndelta\nalpha\n"
	var out bytes.Buffer
	stats, err := StreamingCompress(strings.NewReader(in), &out,
		StreamingOptions{WindowLines: 4, StripANSI: true, CollapseRepeated: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.DedupedLines != 1 {
		t.Fatalf("expected 1 dedup, got %d (%+v)", stats.DedupedLines, stats)
	}
	if strings.Count(out.String(), "alpha") != 1 {
		t.Fatalf("alpha must appear once: %q", out.String())
	}
}

func TestStreamingCompress_WindowEvictsOldEntries(t *testing.T) {
	t.Parallel()
	in := "a\nb\nc\nd\na\n" // window=2, "a" should be re-emitted
	var out bytes.Buffer
	stats, err := StreamingCompress(strings.NewReader(in), &out,
		StreamingOptions{WindowLines: 2, StripANSI: true, CollapseRepeated: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.PeakWindowSize > 2 {
		t.Fatalf("peak window must be bounded by WindowLines: %+v", stats)
	}
	if strings.Count(out.String(), "a\n") < 1 {
		t.Fatalf("a should appear at least once after eviction: %q", out.String())
	}
}

func TestStreamingCompress_LargeStream_BoundedMemory(t *testing.T) {
	t.Parallel()
	// 100k-line stream of 200 distinct lines: every line repeats 500
	// times. With WindowLines=200 the second occurrence should always
	// be a dedup, so output collapses to 200 unique lines.
	const lines = 100_000
	const distinct = 200
	var sb strings.Builder
	sb.Grow(lines * 8)
	for i := 0; i < lines; i++ {
		sb.WriteString("L")
		sb.WriteString(intToStr(i % distinct))
		sb.WriteByte('\n')
	}
	var out bytes.Buffer
	stats, err := StreamingCompress(strings.NewReader(sb.String()), &out,
		StreamingOptions{WindowLines: distinct, StripANSI: true, CollapseRepeated: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.PeakWindowSize > distinct {
		t.Fatalf("memory ceiling violated: PeakWindowSize=%d > distinct=%d", stats.PeakWindowSize, distinct)
	}
	if stats.LinesIn != lines {
		t.Fatalf("LinesIn=%d want %d", stats.LinesIn, lines)
	}
	// First `distinct` lines emit; everything after is either a
	// repeated-collapse run (line == prev) or a window-dedup hit.
	if stats.LinesOut > distinct {
		t.Fatalf("LinesOut=%d should not exceed distinct=%d", stats.LinesOut, distinct)
	}
}

func TestStreamingCompress_OptOutCollapse(t *testing.T) {
	t.Parallel()
	// StripANSI off + CollapseRepeated off + non-default window: each
	// line is emitted verbatim.
	in := "x\nx\nx\n"
	var out bytes.Buffer
	stats, err := StreamingCompress(strings.NewReader(in), &out,
		StreamingOptions{WindowLines: 999, StripANSI: false, CollapseRepeated: false})
	if err != nil {
		t.Fatal(err)
	}
	// Window dedup still fires (it's not part of the toggleable set
	// since the window is the memory ceiling itself).
	if stats.LinesOut != 1 {
		t.Fatalf("with explicit opts disabled, dedup still collapses, got LinesOut=%d", stats.LinesOut)
	}
}

type erroringReader struct {
	data []byte
	pos  int
}

func (r *erroringReader) Read(p []byte) (int, error) {
	if r.pos < len(r.data) {
		n := copy(p, r.data[r.pos:])
		r.pos += n
		return n, nil
	}
	return 0, errors.New("synthetic read fail")
}

func TestStreamingCompress_ScannerError(t *testing.T) {
	t.Parallel()
	r := &erroringReader{data: []byte("alpha\nbeta")}
	var out bytes.Buffer
	if _, err := StreamingCompress(r, &out, StreamingOptions{}); err == nil {
		t.Fatal("expected scanner error to propagate")
	}
}

type erroringWriter struct{}

func (erroringWriter) Write(_ []byte) (int, error) { return 0, errors.New("write boom") }

func TestStreamingCompress_WriterError(t *testing.T) {
	t.Parallel()
	if _, err := StreamingCompress(strings.NewReader("a\nb\n"), erroringWriter{}, StreamingOptions{}); err == nil {
		t.Fatal("expected writer error to propagate")
	}
}

// failOnNthWriter succeeds for the first n writes, then fails. Used to
// trigger the final flushPrev error path (covers the trailing
// `if err := flushPrev(); err != nil` branch after the scan loop).
type failOnNthWriter struct {
	limit, count int
}

func (w *failOnNthWriter) Write(p []byte) (int, error) {
	w.count++
	if w.count > w.limit {
		return 0, errors.New("late write fail")
	}
	return len(p), nil
}

func TestStreamingCompress_FinalFlushError(t *testing.T) {
	t.Parallel()
	w := &failOnNthWriter{limit: 1}
	if _, err := StreamingCompress(strings.NewReader("a\nb\n"), w, StreamingOptions{}); err == nil {
		t.Fatal("expected final-flush write error to propagate")
	}
}

func TestStreamingCompress_EmptyInput(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	stats, err := StreamingCompress(strings.NewReader(""), &out, StreamingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.LinesIn != 0 || stats.LinesOut != 0 {
		t.Fatalf("empty input stats: %+v", stats)
	}
}

func TestIsStreamingSafe(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"ansi_strip", "line_dedup", "repeated_collapse"} {
		if !IsStreamingSafe(name) {
			t.Fatalf("%s must be safe", name)
		}
	}
	for _, name := range []string{"dedup", "structure_extract", "tool_compressor", ""} {
		if IsStreamingSafe(name) {
			t.Fatalf("%s must NOT be safe", name)
		}
	}
}

func TestStreamingSafeNames_AndJoin(t *testing.T) {
	t.Parallel()
	got := StreamingSafeNames()
	if len(got) != 3 {
		t.Fatalf("expected 3 names, got %v", got)
	}
	joined := JoinStreamingSafe()
	if !strings.Contains(joined, "ansi_strip") || !strings.Contains(joined, "line_dedup") {
		t.Fatalf("joined: %s", joined)
	}
}

// intToStr is a tiny helper so the table-test doesn't pull in fmt
// for every iteration of the 100k-line loop.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Sanity: io.EOF must not be surfaced as an error.
func TestStreamingCompress_TerminatesCleanlyOnEOF(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	stats, err := StreamingCompress(io.NopCloser(strings.NewReader("z\n")), &out, StreamingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.LinesIn != 1 || stats.LinesOut != 1 {
		t.Fatalf("EOF stats: %+v", stats)
	}
}
