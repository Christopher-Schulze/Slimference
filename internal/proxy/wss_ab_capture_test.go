package proxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if strings.Contains(string(data), "mutated") {
		t.Fatalf("capture must record pre-mutation payload, got %s", data)
	}
	if !strings.Contains(string(data), `"input":[]`) {
		t.Fatalf("capture did not preserve original payload, got %s", data)
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
