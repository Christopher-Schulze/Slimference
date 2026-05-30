package proxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/slimference/slimference/internal/proxy/wsmitm"
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
		return next(ctx, dir, env)
	}
}

func (c *wssABReplayCapture) Record(dir wsmitm.Direction, env *wsmitm.Envelope) {
	if c == nil || c.f == nil || env == nil {
		return
	}
	payload := env.Raw
	if len(payload) == 0 {
		marshaled, err := env.Marshal()
		if err != nil {
			return
		}
		payload = marshaled
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
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.f.Write(append(data, '\n'))
}
