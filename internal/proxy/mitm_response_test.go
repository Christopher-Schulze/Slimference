package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestMITMResponseWriter_DefaultStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.Write([]byte("hello"))
	if err := rw.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(&buf), nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("default status must be 200, got %d", resp.StatusCode)
	}
	body, _ := readAll(resp)
	if string(body) != "hello" {
		t.Fatalf("body mismatch: %q", body)
	}
}

func TestMITMResponseWriter_ExplicitStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.WriteHeader(418)
	rw.Write([]byte("teapot"))
	if err := rw.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	resp, _ := http.ReadResponse(bufio.NewReader(&buf), nil)
	if resp.StatusCode != 418 {
		t.Fatalf("expected 418, got %d", resp.StatusCode)
	}
}

func TestMITMResponseWriter_DoubleWriteHeaderIgnored(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.WriteHeader(201)
	rw.WriteHeader(500) // ignored
	rw.Write([]byte("x"))
	rw.finish()
	resp, _ := http.ReadResponse(bufio.NewReader(&buf), nil)
	if resp.StatusCode != 201 {
		t.Fatalf("first WriteHeader must win: %d", resp.StatusCode)
	}
}

func TestMITMResponseWriter_ContentLengthAuto(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.Write([]byte("12345"))
	rw.finish()
	if !strings.Contains(buf.String(), "Content-Length: 5") {
		t.Fatalf("expected auto Content-Length 5, got %q", buf.String())
	}
}

func TestMITMResponseWriter_ContentLengthHonoured(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.Header().Set("Content-Length", "999")
	rw.Write([]byte("12345"))
	rw.finish()
	if !strings.Contains(buf.String(), "Content-Length: 999") {
		t.Fatalf("operator-set Content-Length must round-trip, got %q", buf.String())
	}
}

func TestMITMResponseWriter_FlushStreamsBufferedBody(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.WriteHeader(http.StatusAccepted)
	if _, err := rw.Write([]byte("data: one\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	rw.Flush()
	if !strings.Contains(buf.String(), "HTTP/1.1 202 Accepted") {
		t.Fatalf("flush must write response head, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "data: one\n") {
		t.Fatalf("flush must write buffered body, got %q", buf.String())
	}
	if strings.Contains(buf.String(), "Content-Length") {
		t.Fatalf("streaming flush must not invent content-length, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "Connection: close") {
		t.Fatalf("streaming flush must mark connection-close framing, got %q", buf.String())
	}
	if !rw.streamed() {
		t.Fatal("Flush must mark writer as streamed")
	}
	if _, err := rw.Write([]byte("data: two\n")); err != nil {
		t.Fatalf("stream write: %v", err)
	}
	if err := rw.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !strings.Contains(buf.String(), "data: two\n") {
		t.Fatalf("streaming writes must pass through after Flush, got %q", buf.String())
	}
}

func TestMITMResponseWriter_FlushImplicitStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.Flush()
	rw.Flush()
	if err := rw.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "HTTP/1.1 200 OK") {
		t.Fatalf("flush must default status, got %q", buf.String())
	}
}

func TestMITMResponseWriter_FlushHeaderErrorSurfacesOnFinish(t *testing.T) {
	t.Parallel()
	rw := newMITMResponseWriter(&errorWriter{after: 0})
	rw.Flush()
	rw.Flush()
	if err := rw.finish(); err == nil {
		t.Fatal("flush header error must surface from finish")
	}
}

func TestMITMResponseWriter_FlushBodyErrorSurfacesOnFinish(t *testing.T) {
	t.Parallel()
	rw := newMITMResponseWriter(&errorWriter{after: 1})
	rw.Write([]byte("body"))
	rw.Flush()
	if err := rw.finish(); err == nil {
		t.Fatal("flush body error must surface from finish")
	}
}

func TestMITMResponseWriter_StreamingWriteError(t *testing.T) {
	t.Parallel()
	rw := newMITMResponseWriter(&errorWriter{after: 1})
	rw.Flush()
	if _, err := rw.Write([]byte("body")); err == nil {
		t.Fatal("streaming write must return writer error")
	}
	if err := rw.finish(); err == nil {
		t.Fatal("streaming write error must surface from finish")
	}
}

func TestMITMResponseWriter_HeadersSorted(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.Header().Set("Z-Header", "zz")
	rw.Header().Set("A-Header", "aa")
	rw.Header().Set("M-Header", "mm")
	rw.Write([]byte(""))
	rw.finish()
	idxA := strings.Index(buf.String(), "A-Header")
	idxM := strings.Index(buf.String(), "M-Header")
	idxZ := strings.Index(buf.String(), "Z-Header")
	if !(idxA < idxM && idxM < idxZ) {
		t.Fatalf("headers not sorted: %q", buf.String())
	}
}

func TestMITMResponseWriter_StatusTextFallback(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	rw.WriteHeader(799) // not a known status code
	rw.finish()
	if !strings.Contains(buf.String(), "799 Status") {
		t.Fatalf("expected fallback status text 'Status', got %q", buf.String())
	}
}

func TestMITMResponseWriter_FinishImplicitStatusOnEmptyBody(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rw := newMITMResponseWriter(&buf)
	if err := rw.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "HTTP/1.1 200 OK") {
		t.Fatalf("empty-body finish must default to 200 OK, got %q", buf.String())
	}
}

func TestMITMResponseWriter_WriteFailsHeaderError(t *testing.T) {
	t.Parallel()
	rw := newMITMResponseWriter(&errorWriter{after: 0})
	rw.Write([]byte("body"))
	if err := rw.finish(); err == nil {
		t.Fatal("expected finish to surface header write error")
	}
}

func TestMITMResponseWriter_WriteFailsBodyError(t *testing.T) {
	t.Parallel()
	rw := newMITMResponseWriter(&errorWriter{after: 1})
	rw.Write([]byte("body"))
	if err := rw.finish(); err == nil {
		t.Fatal("expected finish to surface body write error")
	}
}

func TestSortStrings_Insertion(t *testing.T) {
	t.Parallel()
	in := []string{"c", "a", "b", "d"}
	sortStrings(in)
	if strings.Join(in, "") != "abcd" {
		t.Fatalf("not sorted: %v", in)
	}
}

// errorWriter accepts the first `after` Write calls then errors on
// every subsequent one. Used to drive the err paths in finish().
type errorWriter struct {
	after int
	calls int
}

func (e *errorWriter) Write(p []byte) (int, error) {
	e.calls++
	if e.calls > e.after {
		return 0, errors.New("simulated write failure")
	}
	return len(p), nil
}

func readAll(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("nil body")
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 64)
	tmp := make([]byte, 64)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}
