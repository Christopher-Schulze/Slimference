package proxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

const wssABCaptureEnv = "SLIMFERENCE_WSS_AB_CAPTURE"

type wssABReplayCapture struct {
	mu sync.Mutex
	f  *os.File
}

type wssABReplayCaptureRecord struct {
	Timestamp time.Time        `json:"timestamp"`
	Direction wsmitm.Direction `json:"direction"`
	Payload   json.RawMessage  `json:"payload"`
	Kind      wsmitm.FrameKind `json:"kind,omitempty"`
	Sequence  int64            `json:"sequence,omitempty"`
	// Mutated marks the B-side: the frame as re-serialized AFTER the Phase-F
	// handler replaced it (what actually went upstream). Unmutated frames
	// appear once; mutated frames appear twice (original, then mutated).
	Mutated bool `json:"mutated,omitempty"`
}

func newWSSABReplayCaptureFromEnv() *wssABReplayCapture {
	return newWSSABReplayCapture(os.Getenv(wssABCaptureEnv))
}

func newWSSABReplayCapture(path string) *wssABReplayCapture {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return &wssABReplayCapture{f: f}
}

func (c *wssABReplayCapture) Close() error {
	if c == nil || c.f == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.f.Close()
}

func (c *wssABReplayCapture) Wrap(next wsmitm.FrameHandler) wsmitm.FrameHandler {
	if c == nil {
		return next
	}
	return func(ctx context.Context, dir wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
		c.Record(dir, env)
		if next == nil {
			return false, nil
		}
		replaced, err := next(ctx, dir, env)
		if replaced {
			// B-side: the handler replaced the frame in place; record the
			// authoritative re-serialization that goes upstream so captures
			// carry the exact original/mutated pair per frame (T354 proof).
			c.recordMutated(dir, env)
		}
		return replaced, err
	}
}

func (c *wssABReplayCapture) Record(dir wsmitm.Direction, env *wsmitm.Envelope) {
	c.record(dir, env, false)
}

func (c *wssABReplayCapture) recordMutated(dir wsmitm.Direction, env *wsmitm.Envelope) {
	c.record(dir, env, true)
}

func (c *wssABReplayCapture) record(dir wsmitm.Direction, env *wsmitm.Envelope, mutated bool) {
	if c == nil || c.f == nil || env == nil {
		return
	}
	var payload []byte
	if mutated {
		// env.Raw can still hold the pre-mutation bytes after an in-place
		// Body/Request replacement; Marshal is the authoritative
		// post-mutation serialization that the session forwards upstream.
		marshaled, err := env.Marshal()
		if err != nil {
			return
		}
		payload = marshaled
	} else {
		payload = env.Raw
		if len(payload) == 0 {
			marshaled, err := env.Marshal()
			if err != nil {
				return
			}
			payload = marshaled
		}
	}
	if !json.Valid(payload) {
		return
	}
	rec := wssABReplayCaptureRecord{
		Timestamp: time.Now().UTC(),
		Direction: dir,
		Payload:   append(json.RawMessage(nil), payload...),
		Kind:      env.Kind,
		Sequence:  env.Sequence(),
		Mutated:   mutated,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.f.Write(append(data, '\n'))
}
