package proxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSABReplayCaptureWritesReplayCompatibleFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frames.jsonl")
	capture := newWSSABReplayCapture(path)
	if capture == nil {
		t.Fatal("capture was not created")
	}
	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":            "gpt-5-codex",
			"prompt_cache_key": "capture-session",
			"input":            []map[string]any{{"type": "message", "role": "user", "content": "hello"}},
			"stream":           true,
		},
	})
	capture.Record(wsmitm.DirClientToServer, &env)
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}

	frames, err := readWSSABReplayFramesForTest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%d want 1", len(frames))
	}
	if frames[0].Direction != wsmitm.DirClientToServer {
		t.Fatalf("direction=%q", frames[0].Direction)
	}
	if !strings.Contains(string(frames[0].Payload), `"prompt_cache_key":"capture-session"`) {
		t.Fatalf("payload not preserved: %s", frames[0].Payload)
	}
	if _, err := RunWSSPhaseFABReplay(nil, frames); err != nil {
		t.Fatalf("captured frame should replay: %v", err)
	}
}

func TestWSSABReplayCaptureWrapperRecordsBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frames.jsonl")
	capture := newWSSABReplayCapture(path)
	if capture == nil {
		t.Fatal("capture was not created")
	}
	env := parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindRequest), "body": map[string]any{"input": []any{}}})
	handler := capture.Wrap(func(_ context.Context, _ wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
		env.Body = json.RawMessage(`{"input":[{"type":"message","role":"user","content":"mutated"}]}`)
		return true, nil
	})
	if replaced, err := handler(context.Background(), wsmitm.DirClientToServer, &env); err != nil || !replaced {
		t.Fatalf("handler replaced=%v err=%v", replaced, err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("mutated frame must produce an original+mutated record pair, got %d lines: %s", len(lines), data)
	}
	if strings.Contains(lines[0], `"mutated":true`) || !strings.Contains(lines[0], `"input":[]`) {
		t.Fatalf("first record must be the unmarked original payload, got %s", lines[0])
	}
	if !strings.Contains(lines[1], `"mutated":true`) || !strings.Contains(lines[1], `"content":"mutated"`) {
		t.Fatalf("second record must be the marked post-mutation payload, got %s", lines[1])
	}
}

func TestWSSABReplayCaptureWrapperUnmutatedFrameRecordsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frames.jsonl")
	capture := newWSSABReplayCapture(path)
	if capture == nil {
		t.Fatal("capture was not created")
	}
	env := parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindRequest), "body": map[string]any{"input": []any{}}})
	handler := capture.Wrap(func(_ context.Context, _ wsmitm.Direction, _ *wsmitm.Envelope) (bool, error) {
		return false, nil
	})
	if replaced, err := handler(context.Background(), wsmitm.DirClientToServer, &env); err != nil || replaced {
		t.Fatalf("handler replaced=%v err=%v", replaced, err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || strings.Contains(lines[0], `"mutated":true`) {
		t.Fatalf("unmutated frame must record exactly one unmarked line, got %s", data)
	}
}

func TestWSSABReplayCaptureFromEnvDisabledAndCreatesParent(t *testing.T) {
	t.Setenv(wssABCaptureEnv, "")
	if got := newWSSABReplayCaptureFromEnv(); got != nil {
		t.Fatal("empty env should disable capture")
	}
	path := filepath.Join(t.TempDir(), "nested", "frames.jsonl")
	t.Setenv(wssABCaptureEnv, path)
	capture := newWSSABReplayCaptureFromEnv()
	if capture == nil {
		t.Fatal("capture was not created from env")
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("capture file missing: %v", err)
	}
}

func TestWSSABReplayRuntimeCaptureArmsDisarmsAndExpires(t *testing.T) {
	runtimeCapture := newWSSABReplayRuntimeCapture()
	path := filepath.Join(t.TempDir(), "runtime", "frames.jsonl")
	status, err := runtimeCapture.Set(path, time.Hour)
	if err != nil {
		t.Fatalf("set runtime capture: %v", err)
	}
	if !status.Enabled || status.Path != path || status.ExpiresAt.IsZero() {
		t.Fatalf("status=%+v", status)
	}
	env := parseWSJSON(t, map[string]any{"type": string(wsmitm.FrameKindRequest), "body": map[string]any{"input": []any{}}})
	handler := runtimeCapture.Wrap(func(_ context.Context, _ wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
		env.Body = json.RawMessage(`{"input":[{"type":"message","role":"user","content":"runtime-mutated"}]}`)
		return true, nil
	})
	if replaced, err := handler(context.Background(), wsmitm.DirClientToServer, &env); err != nil || !replaced {
		t.Fatalf("handler replaced=%v err=%v", replaced, err)
	}
	runtimeCapture.Clear()
	if replaced, err := handler(context.Background(), wsmitm.DirClientToServer, &env); err != nil || !replaced {
		t.Fatalf("handler after clear replaced=%v err=%v", replaced, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "runtime-mutated") {
		t.Fatalf("runtime capture should write one original+mutated pair before clear, got %s", data)
	}

	expiredPath := filepath.Join(t.TempDir(), "expired.jsonl")
	if _, err := runtimeCapture.Set(expiredPath, time.Nanosecond); err != nil {
		t.Fatalf("set expiring runtime capture: %v", err)
	}
	time.Sleep(time.Millisecond)
	if replaced, err := handler(context.Background(), wsmitm.DirClientToServer, &env); err != nil || !replaced {
		t.Fatalf("handler after expire replaced=%v err=%v", replaced, err)
	}
	info, err := os.Stat(expiredPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 || runtimeCapture.Status().Enabled {
		t.Fatalf("expired capture wrote bytes or stayed enabled: size=%d status=%+v", info.Size(), runtimeCapture.Status())
	}
}

func readWSSABReplayFramesForTest(path string) ([]WSSABReplayFrame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	frames := make([]WSSABReplayFrame, 0, len(lines))
	for _, line := range lines {
		var rec struct {
			Direction wsmitm.Direction `json:"direction"`
			Payload   json.RawMessage  `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, err
		}
		frames = append(frames, WSSABReplayFrame{Direction: rec.Direction, Payload: rec.Payload})
	}
	return frames, nil
}
