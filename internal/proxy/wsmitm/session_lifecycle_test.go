package wsmitm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

func TestSessionLifecycleClassifiesClientEOF(t *testing.T) {
	var raw bytes.Buffer
	if _, err := wscompact.WriteFrame(&raw, true, wscompact.OpcodeText, nil, []byte(`{"type":"request"}`)); err != nil {
		t.Fatal(err)
	}
	blockPipe := newBlockingPipe()
	t.Cleanup(blockPipe.Close)
	session := &Session{
		Client:   &fakeRW{r: bytes.NewReader(raw.Bytes())},
		Upstream: &fakeRW{r: blockPipe, w: io.Discard},
	}
	if err := session.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	snap := session.Snapshot()
	if snap.CloseInitiator != "client_eof" || snap.CloseError != "" {
		t.Fatalf("unexpected lifecycle: %+v", snap)
	}
	if snap.OpenedAtUnixNano == 0 || snap.ClosedAtUnixNano == 0 || snap.ClosedAtUnixNano < snap.OpenedAtUnixNano {
		t.Fatalf("bad lifecycle timestamps: %+v", snap)
	}
	if snap.C2SFrames != 1 {
		t.Fatalf("C2SFrames=%d want 1", snap.C2SFrames)
	}
}

func TestSessionLifecycleClassifiesOurHandlerError(t *testing.T) {
	var raw bytes.Buffer
	if _, err := wscompact.WriteFrame(&raw, true, wscompact.OpcodeText, nil, []byte(`{"type":"request"}`)); err != nil {
		t.Fatal(err)
	}
	blockPipe := newBlockingPipe()
	t.Cleanup(blockPipe.Close)
	session := &Session{
		Client:   &fakeRW{r: bytes.NewReader(raw.Bytes())},
		Upstream: &fakeRW{r: blockPipe, w: io.Discard},
		ClientHandler: func(context.Context, Direction, *Envelope) (bool, error) {
			return false, fmt.Errorf("boom")
		},
	}
	err := session.Serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "handler") {
		t.Fatalf("expected handler error, got %v", err)
	}
	snap := session.Snapshot()
	if snap.CloseInitiator != "our_error" || !strings.Contains(snap.CloseError, "handler") {
		t.Fatalf("unexpected lifecycle: %+v", snap)
	}
}

func TestSessionLifecycleClassifiesContextCancel(t *testing.T) {
	blockUpstream := newBlockingPipe()
	t.Cleanup(blockUpstream.Close)
	session := &Session{
		Client:   &fakeRW{r: errorReader{err: context.Canceled}, w: io.Discard},
		Upstream: &fakeRW{r: blockUpstream, w: io.Discard},
	}
	if err := session.Serve(context.Background()); err != nil {
		t.Fatalf("Serve after cancel: %v", err)
	}
	if got := session.Snapshot().CloseInitiator; got != "context_cancel" {
		t.Fatalf("CloseInitiator=%q want context_cancel", got)
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestClassifySessionClose(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		result       sessionPumpResult
		wantInit     string
		wantErrEmpty bool
	}{
		{"nil_err_server_dir", sessionPumpResult{dir: DirServerToClient, err: nil}, "upstream_eof", true},
		{"nil_err_client_dir", sessionPumpResult{dir: DirClientToServer, err: nil}, "client_eof", true},
		{"eof_server_dir", sessionPumpResult{dir: DirServerToClient, err: io.EOF}, "upstream_eof", true},
		{"eof_client_dir", sessionPumpResult{dir: DirClientToServer, err: io.EOF}, "client_eof", true},
		{"session_closed_server", sessionPumpResult{dir: DirServerToClient, err: ErrSessionClosed}, "upstream_eof", true},
		{"context_canceled", sessionPumpResult{dir: DirClientToServer, err: context.Canceled}, "context_cancel", true},
		{"deadline_exceeded", sessionPumpResult{dir: DirServerToClient, err: context.DeadlineExceeded}, "context_cancel", true},
		{"our_handler_error", sessionPumpResult{dir: DirClientToServer, err: fmt.Errorf("handler: boom")}, "our_error", false},
		{"our_remarshal_error", sessionPumpResult{dir: DirServerToClient, err: fmt.Errorf("re-marshal failed")}, "our_error", false},
		{"our_reencode_error", sessionPumpResult{dir: DirClientToServer, err: fmt.Errorf("forced compressed re-encode: deflate")}, "our_error", false},
		{"client_read_frame_error", sessionPumpResult{dir: DirClientToServer, err: fmt.Errorf("read frame: connection reset")}, "client_error", false},
		{"client_other_error", sessionPumpResult{dir: DirClientToServer, err: fmt.Errorf("write: broken pipe")}, "upstream_error", false},
		{"server_read_frame_error", sessionPumpResult{dir: DirServerToClient, err: fmt.Errorf("read frame: timeout")}, "upstream_error", false},
		{"server_other_error", sessionPumpResult{dir: DirServerToClient, err: fmt.Errorf("write: closed")}, "client_error", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			init, errStr := classifySessionClose(tc.result)
			if init != tc.wantInit {
				t.Fatalf("classifySessionClose(%s) init=%q, want %q", tc.name, init, tc.wantInit)
			}
			if tc.wantErrEmpty && errStr != "" {
				t.Fatalf("classifySessionClose(%s) err=%q, want empty", tc.name, errStr)
			}
			if !tc.wantErrEmpty && errStr == "" {
				t.Fatalf("classifySessionClose(%s) err empty, want non-empty", tc.name)
			}
		})
	}
}

func TestTruncateSessionCloseError(t *testing.T) {
	t.Parallel()
	// Short string — returned as-is (after trim).
	if got := truncateSessionCloseError("short error"); got != "short error" {
		t.Fatalf("truncateSessionCloseError(\"short error\") = %q", got)
	}
	// Newlines replaced with spaces.
	if got := truncateSessionCloseError("line1\nline2"); got != "line1 line2" {
		t.Fatalf("truncateSessionCloseError(newlines) = %q, want \"line1 line2\"", got)
	}
	// Leading/trailing whitespace trimmed.
	if got := truncateSessionCloseError("  trimmed  "); got != "trimmed" {
		t.Fatalf("truncateSessionCloseError(whitespace) = %q, want \"trimmed\"", got)
	}
	// Long string — truncated to 160 chars.
	long := strings.Repeat("x", 200)
	got := truncateSessionCloseError(long)
	if len(got) != 160 {
		t.Fatalf("truncateSessionCloseError(long) len=%d, want 160", len(got))
	}
}

func TestIsOurSessionError(t *testing.T) {
	t.Parallel()
	ourErrors := []string{
		"handler: something went wrong",
		"re-marshal failed for frame",
		"forced compressed re-encode: deflate error",
	}
	for _, s := range ourErrors {
		if !isOurSessionError(s) {
			t.Fatalf("isOurSessionError(%q) = false, want true", s)
		}
	}
	notOurErrors := []string{
		"read frame: connection reset",
		"write: broken pipe",
		"unexpected EOF",
		"",
	}
	for _, s := range notOurErrors {
		if isOurSessionError(s) {
			t.Fatalf("isOurSessionError(%q) = true, want false", s)
		}
	}
}
