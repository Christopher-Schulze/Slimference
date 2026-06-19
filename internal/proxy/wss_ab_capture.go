package proxy

import (
	"context"
	"encoding/json"
	"fmt"
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

type wssABReplayRuntimeCapture struct {
	mu        sync.Mutex
	capture   *wssABReplayCapture
	path      string
	expiresAt time.Time
}

type WSSABCaptureStatus struct {
	Enabled   bool      `json:"enabled"`
	Path      string    `json:"path,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
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
	capture, err := openWSSABReplayCapture(path)
	if err != nil {
		return nil
	}
	return capture
}

func openWSSABReplayCapture(path string) (*wssABReplayCapture, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("capture path is empty")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &wssABReplayCapture{f: f}, nil
}

func newWSSABReplayRuntimeCapture() *wssABReplayRuntimeCapture {
	return &wssABReplayRuntimeCapture{}
}

func (c *wssABReplayRuntimeCapture) Set(path string, duration time.Duration) (WSSABCaptureStatus, error) {
	if c == nil {
		return WSSABCaptureStatus{}, fmt.Errorf("runtime capture controller unavailable")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return WSSABCaptureStatus{}, fmt.Errorf("capture path is empty")
	}
	capture, err := openWSSABReplayCapture(path)
	if err != nil {
		return WSSABCaptureStatus{}, err
	}
	var expiresAt time.Time
	if duration > 0 {
		expiresAt = time.Now().UTC().Add(duration)
	}
	c.mu.Lock()
	old := c.capture
	c.capture = capture
	c.path = path
	c.expiresAt = expiresAt
	status := c.statusLocked(time.Now().UTC())
	c.mu.Unlock()
	_ = old.Close()
	return status, nil
}

func (c *wssABReplayRuntimeCapture) Clear() WSSABCaptureStatus {
	if c == nil {
		return WSSABCaptureStatus{}
	}
	c.mu.Lock()
	old := c.capture
	c.capture = nil
	c.path = ""
	c.expiresAt = time.Time{}
	c.mu.Unlock()
	_ = old.Close()
	return WSSABCaptureStatus{}
}

func (c *wssABReplayRuntimeCapture) Status() WSSABCaptureStatus {
	if c == nil {
		return WSSABCaptureStatus{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked(time.Now().UTC())
}

func (c *wssABReplayRuntimeCapture) statusLocked(now time.Time) WSSABCaptureStatus {
	if c.capture == nil {
		return WSSABCaptureStatus{}
	}
	if !c.expiresAt.IsZero() && !now.Before(c.expiresAt) {
		old := c.capture
		c.capture = nil
		c.path = ""
		c.expiresAt = time.Time{}
		_ = old.Close()
		return WSSABCaptureStatus{}
	}
	return WSSABCaptureStatus{
		Enabled:   true,
		Path:      c.path,
		ExpiresAt: c.expiresAt,
	}
}

func (p *Proxy) SetWSSABCapture(path string, duration time.Duration) (WSSABCaptureStatus, error) {
	if p == nil || p.wssABCapture == nil {
		return WSSABCaptureStatus{}, fmt.Errorf("runtime capture controller unavailable")
	}
	return p.wssABCapture.Set(path, duration)
}

func (p *Proxy) ClearWSSABCapture() WSSABCaptureStatus {
	if p == nil || p.wssABCapture == nil {
		return WSSABCaptureStatus{}
	}
	return p.wssABCapture.Clear()
}

func (p *Proxy) WSSABCaptureStatus() WSSABCaptureStatus {
	if p == nil || p.wssABCapture == nil {
		return WSSABCaptureStatus{}
	}
	return p.wssABCapture.Status()
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

func (c *wssABReplayRuntimeCapture) Wrap(next wsmitm.FrameHandler) wsmitm.FrameHandler {
	if c == nil {
		return next
	}
	return func(ctx context.Context, dir wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
		c.record(dir, env, false)
		if next == nil {
			return false, nil
		}
		replaced, err := next(ctx, dir, env)
		if replaced {
			c.record(dir, env, true)
		}
		return replaced, err
	}
}

func (c *wssABReplayRuntimeCapture) record(dir wsmitm.Direction, env *wsmitm.Envelope, mutated bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	status := c.statusLocked(time.Now().UTC())
	if !status.Enabled || c.capture == nil {
		return
	}
	if mutated {
		c.capture.recordMutated(dir, env)
		return
	}
	c.capture.Record(dir, env)
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
