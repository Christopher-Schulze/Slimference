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
