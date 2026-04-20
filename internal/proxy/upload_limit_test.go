package proxy

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// slowChunkReader simulates a chunked HTTP body delivered in short slices.
// It never declares Content-Length so any enforcement path must rely on
// byte-counting, not headers. Remaining is decremented per actual byte
// delivered to the caller's buffer.
type slowChunkReader struct {
	fill         byte
	remaining    int64
	pausePerRead time.Duration
}

func (s *slowChunkReader) Read(p []byte) (int, error) {
	if s.remaining <= 0 {
		return 0, io.EOF
	}
	if s.pausePerRead > 0 {
		time.Sleep(s.pausePerRead)
	}
	n := len(p)
	if int64(n) > s.remaining {
		n = int(s.remaining)
	}
	for i := 0; i < n; i++ {
		p[i] = s.fill
	}
	s.remaining -= int64(n)
	return n, nil
}

// TestT51_ReadBody_ChunkedOverLimitRejected simulates a long streaming body
// that would grow past maxRequestBodySize. The readBody path must detect
// the excess via LimitReader and return errRequestBodyTooLarge even though
// the client does not declare Content-Length.
func TestT51_ReadBody_ChunkedOverLimitRejected(t *testing.T) {
	// Ask for 33 MiB so the limit reader sees one byte past the threshold.
	reader := &slowChunkReader{
		fill:         'x',
		remaining:    int64(maxRequestBodySize) + 1,
		pausePerRead: 0, // fast path for CI
	}
	req, err := http.NewRequest(http.MethodPost, "/v1/messages",
		io.NopCloser(reader))
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1 // chunked
	req.TransferEncoding = []string{"chunked"}

	body, err := readBody(req)
	if err == nil {
		t.Fatalf("expected errRequestBodyTooLarge, got body=%d bytes", len(body))
	}
	if !errors.Is(err, errRequestBodyTooLarge) {
		t.Fatalf("err = %v, want errRequestBodyTooLarge", err)
	}
}

// TestT51_ReadBody_ExactlyAtLimitAccepted verifies the boundary: a body of
// exactly maxRequestBodySize bytes must succeed, while one byte more is
// rejected (tested above).
func TestT51_ReadBody_ExactlyAtLimitAccepted(t *testing.T) {
	// Use strings.NewReader to avoid a 32 MiB allocation spike in test
	// memory; strings.Repeat is fine for one-shot sizing.
	body := strings.Repeat("y", maxRequestBodySize)
	req, _ := http.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(body))
	got, err := readBody(req)
	if err != nil {
		t.Fatalf("unexpected error at exact limit: %v", err)
	}
	if len(got) != maxRequestBodySize {
		t.Fatalf("len = %d, want %d", len(got), maxRequestBodySize)
	}
}

// TestT51_ReadBody_NilBodyReturnsEmpty verifies the nil-body defensive
// path in readBody, which prevents a panic when upstream forwards a
// malformed request with no body at all.
func TestT51_ReadBody_NilBodyReturnsEmpty(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/v1/messages", http.NoBody)
	req.Body = nil
	got, err := readBody(req)
	if err != nil {
		t.Fatalf("nil body err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("nil body returned %d bytes", len(got))
	}
}

// TestT51_ReadBody_ReadErrorPropagated verifies that a genuine I/O error
// from the upstream reader surfaces as a non-nil error and not a silent
// empty-body pass. Uses a reader that always returns io.ErrUnexpectedEOF.
func TestT51_ReadBody_ReadErrorPropagated(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/v1/messages",
		io.NopCloser(uploadErrReader{}))
	if _, err := readBody(req); err == nil {
		t.Fatal("expected error from faulty reader, got nil")
	}
}

type uploadErrReader struct{}

func (uploadErrReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
