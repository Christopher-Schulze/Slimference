package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// mitmResponseWriter is a minimal http.ResponseWriter for the
// CONNECT-MITM dispatch path. It buffers the response in memory and
// flushes it to the underlying TLS connection in `finish()` so the
// loop in servePlaintextOnTLS controls connection lifetime end-to-end.
//
// The full http.ResponseWriter contract is intentionally NOT
// implemented: streaming via http.Flusher is not supported, hijacking
// is not supported. Inner handlers that rely on those (none in the
// Slimference dispatch chain today) need a different ingress.
type mitmResponseWriter struct {
	conn        io.Writer
	header      http.Header
	body        bytes.Buffer
	statusCode  int
	wroteHeader bool
}

func newMITMResponseWriter(conn io.Writer) *mitmResponseWriter {
	return &mitmResponseWriter{conn: conn, header: http.Header{}}
}

// Header satisfies http.ResponseWriter.
func (rw *mitmResponseWriter) Header() http.Header {
	return rw.header
}

// WriteHeader latches the status code on first call. Subsequent calls
// are ignored, matching net/http.ResponseWriter semantics.
func (rw *mitmResponseWriter) WriteHeader(status int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = status
	rw.wroteHeader = true
}

// Write buffers the response body. WriteHeader is implicitly called
// with 200 if the handler did not set a status before the first Write.
func (rw *mitmResponseWriter) Write(p []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.body.Write(p)
}

// finish writes the buffered status line, headers and body to the
// underlying connection. Returns the first write error, if any.
func (rw *mitmResponseWriter) finish() error {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	statusText := http.StatusText(rw.statusCode)
	if statusText == "" {
		statusText = "Status"
	}
	if rw.header.Get("Content-Length") == "" {
		rw.header.Set("Content-Length", strconv.Itoa(rw.body.Len()))
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "HTTP/1.1 %d %s\r\n", rw.statusCode, statusText)
	writeHeadersSorted(&buf, rw.header)
	buf.WriteString("\r\n")
	if _, err := rw.conn.Write(buf.Bytes()); err != nil {
		return err
	}
	if rw.body.Len() == 0 {
		return nil
	}
	_, err := rw.conn.Write(rw.body.Bytes())
	return err
}

// writeHeadersSorted writes header lines in sorted key order so the
// output is deterministic across runs (useful for tests). The writer
// in the only call site is a bytes.Buffer which never errors; we
// therefore drop the err return entirely.
func writeHeadersSorted(w io.Writer, h http.Header) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			fmt.Fprintf(w, "%s: %s\r\n", k, v)
		}
	}
}

// sortStrings is a local sort helper so this file does not pull in
// the entire sort package; insertion sort is fast enough for header
// counts in the tens at most.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && strings.Compare(s[j-1], s[j]) > 0; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
