package proxy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/slimference/slimference/internal/abharness"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
	"github.com/slimference/slimference/internal/types"
)

// WSSABReplayFrame is one decompressed Codex WSS envelope in replay order.
type WSSABReplayFrame struct {
	Direction wsmitm.Direction
	Payload   []byte
}

// WSSABReplayResult is the offline comprehension report plus basic reducer
// activity counters for the replay.
type WSSABReplayResult struct {
	Report          abharness.Report
	RequestTurns    int
	MutatedRequests int
}

// RunWSSPhaseFABReplay runs the real Codex WSS Phase-F reducer against a frame
// sequence and compares the model-facing request context against byte-equal
// direct forwarding. It is the T249 bridge between the reducer and the offline
// comprehension harness.
func RunWSSPhaseFABReplay(cfg *config.Config, frames []WSSABReplayFrame) (WSSABReplayResult, error) {
	home, err := os.MkdirTemp("", "slimference-wss-ab-replay-*")
	if err != nil {
		return WSSABReplayResult{}, fmt.Errorf("create isolated replay home: %w", err)
	}
	defer os.RemoveAll(home)

	wssABReplayHomeMu.Lock()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	defer func() {
		proxyUserHomeDir = oldHome
		wssABReplayHomeMu.Unlock()
	}()
	return runWSSPhaseFABReplay(cfg, frames)
}

var wssABReplayHomeMu sync.Mutex

func runWSSPhaseFABReplay(cfg *config.Config, frames []WSSABReplayFrame) (WSSABReplayResult, error) {
	if cfg == nil {
		cfg = config.Defaults()
	}
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	turns := make([]abharness.Turn, 0, len(frames))
	out := WSSABReplayResult{}

	for i, frame := range frames {
		env, err := wsmitm.Parse(frame.Payload)
		if err != nil {
			return WSSABReplayResult{}, fmt.Errorf("parse frame %d: %w", i, err)
		}
		switch frame.Direction {
		case wsmitm.DirServerToClient:
			if _, err := adapter.handle(context.Background(), frame.Direction, &env); err != nil {
				return WSSABReplayResult{}, fmt.Errorf("handle server frame %d: %w", i, err)
			}
		case wsmitm.DirClientToServer:
			before, _, err := extractMessages(types.CodexChatGPT, frame.Payload)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract direct request %d: %w", i, err)
			}
			mutatedBody, runtimeMessages, changed, _, _ := adapter.applyInputPipeline(frame.Payload)
			after, _, err := extractMessages(types.CodexChatGPT, mutatedBody)
			if err != nil {
				return WSSABReplayResult{}, fmt.Errorf("extract compressed request %d: %w", i, err)
			}
			if len(before) > 0 || len(after) > 0 {
				turns = append(turns, abharness.Turn{Before: before, After: after})
				out.RequestTurns++
			}
			if changed && !bytes.Equal(frame.Payload, mutatedBody) {
				out.MutatedRequests++
			}
			rememberReplayRequestState(adapter, runtimeMessages)
		default:
			return WSSABReplayResult{}, fmt.Errorf("frame %d has unsupported direction %q", i, frame.Direction)
		}
	}
	out.Report = abharness.Compare(turns)
	return out, nil
}

func rememberReplayRequestState(adapter *wsPhaseFAdapter, messages []types.Message) {
	if adapter == nil {
		return
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.messages = messages
	adapter.repdetIndex = buildRepdetIndex(messages)
	if adapter.toolUses == nil {
		adapter.toolUses = make(map[string]types.ContentBlock)
	}
	for id, use := range proxyToolUseIndex(messages) {
		adapter.toolUses[id] = use
	}
}
