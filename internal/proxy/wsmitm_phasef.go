package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"

	"github.com/slimference/slimference/internal/beterse"
	"github.com/slimference/slimference/internal/outstop"
	"github.com/slimference/slimference/internal/outstop/repdet"
	"github.com/slimference/slimference/internal/outstop/streamcut"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
	"github.com/slimference/slimference/internal/qualityab"
	"github.com/slimference/slimference/internal/staleread"
	"github.com/slimference/slimference/internal/types"
)

type wsPhaseFAdapter struct {
	p *Proxy

	mu             sync.Mutex
	messages       []types.Message
	repdetIndex    *repdet.Index
	streamCutter   *streamcut.Cutter
	streamcutFired bool
}

func (d *PhaseFDispatcher) newWSPhaseFAdapter() *wsPhaseFAdapter {
	return &wsPhaseFAdapter{p: d.Proxy}
}

func (a *wsPhaseFAdapter) handle(_ context.Context, dir wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
	if a == nil || a.p == nil || a.p.config == nil || env == nil || env.Kind == wsmitm.FrameKindUnknown || env.Kind.IsControl() {
		return false, nil
	}
	switch dir {
	case wsmitm.DirClientToServer:
		if env.Kind != wsmitm.FrameKindRequest {
			return false, nil
		}
		return a.handleRequest(env), nil
	case wsmitm.DirServerToClient:
		return a.handleResponse(env), nil
	default:
		return false, nil
	}
}

func (a *wsPhaseFAdapter) handleRequest(env *wsmitm.Envelope) bool {
	body, replace, ok := wsRequestBody(env)
	if !ok {
		return false
	}
	mutated, messages, changed := a.applyInputPipeline(body)
	a.mu.Lock()
	a.messages = messages
	a.repdetIndex = buildRepdetIndex(messages)
	a.mu.Unlock()
	if !changed {
		return false
	}
	return replace(mutated) == nil
}

func (a *wsPhaseFAdapter) applyInputPipeline(body []byte) ([]byte, []types.Message, bool) {
	original := append([]byte(nil), body...)
	out := append([]byte(nil), body...)
	messages, _, err := extractMessages(types.CodexChatGPT, out)
	if err == nil && len(messages) > 0 {
		if a.p.config.Compression.OutputReduce.StaleReadAgingEnabled {
			aged, stats := staleread.AgeMessages(messages, staleread.Options{
				MinTurnGap: a.p.config.Compression.OutputReduce.StaleReadAgingMinTurnGap,
			})
			if stats.BlocksReplaced > 0 {
				if rebuilt, rebuildErr := reconstructBody(types.CodexChatGPT, out, aged); rebuildErr == nil {
					out = rebuilt
					messages = aged
					a.p.outputReduceCounters.RecordStaleReadAging(stats.BlocksReplaced, stats.BytesReplaced)
				}
			}
		}
		if a.p.config.Compression.OutputReduce.ObsoleteReadPruneEnabled {
			pruned, stats := staleread.PruneObsoleteReads(messages, staleread.ObsoleteOptions{})
			if stats.BlocksReplaced > 0 {
				if rebuilt, rebuildErr := reconstructBody(types.CodexChatGPT, out, pruned); rebuildErr == nil {
					out = rebuilt
					messages = pruned
					a.p.outputReduceCounters.RecordObsoleteReadPrune(stats.BlocksReplaced, stats.BytesReplaced)
				}
			}
		}
		if l0Messages, saved := applyProxyLayer0WithSession(messages, wsCodexSessionID(out)); saved > 0 {
			if rebuilt, rebuildErr := reconstructBody(types.CodexChatGPT, out, l0Messages); rebuildErr == nil {
				out = rebuilt
				messages = l0Messages
				a.p.outputReduceCounters.RecordProxyLayer0(saved)
			}
		}
	}
	if a.p.config.Compression.OutputReduce.StopSequencesEnabled {
		if injected, res := outstop.MergeIntoBody(types.CodexChatGPT, out); res.OK && res.AddedCount > 0 {
			out = injected
			a.p.outputReduceCounters.RecordStopSeqInjection(res.AddedCount)
		}
	}
	if a.p.config.Compression.OutputReduce.BeTerseHintEnabled && a.p.qualityAB != nil {
		sessionID := wsCodexSessionID(out)
		if a.p.qualityAB.Cohort(sessionID) == qualityab.CohortTreatment {
			if injected, res := beterse.Inject(types.CodexChatGPT, out, a.p.config.Compression.OutputReduce.BeTerseHintText); res.Applied {
				out = injected
				a.p.outputReduceCounters.RecordBeTerseInjection(res.Bytes)
			}
		}
	}
	return out, messages, !bytes.Equal(original, out)
}

func (a *wsPhaseFAdapter) handleResponse(env *wsmitm.Envelope) bool {
	mutated := false
	if a.applyStreamcut(env) {
		mutated = true
	}
	if a.applyRepdetDelta(env) {
		mutated = true
	}
	if a.applyRepdetResponse(env) {
		mutated = true
	}
	return mutated
}

func (a *wsPhaseFAdapter) applyStreamcut(env *wsmitm.Envelope) bool {
	if !a.p.config.Compression.OutputReduce.StreamCutEnabled || !env.Kind.IsTextDelta() {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streamcutFired {
		if env.Delta == "" {
			return false
		}
		a.blankDelta(env)
		return true
	}
	if env.Delta == "" {
		return false
	}
	if a.streamCutter == nil {
		a.streamCutter = streamcut.NewCutterWithHoldback(types.CodexChatGPT.String(), 0)
	}
	if !a.streamCutter.Observe(wsStreamcutLine(env)) {
		return false
	}
	a.streamcutFired = true
	a.p.outputReduceCounters.RecordStreamcutFire(int64(len(env.Delta)))
	a.blankDelta(env)
	return true
}

func (a *wsPhaseFAdapter) blankDelta(env *wsmitm.Envelope) {
	env.Delta = ""
	if env.Fields != nil {
		env.Fields["delta"] = json.RawMessage(`""`)
	}
}

func (a *wsPhaseFAdapter) applyRepdetDelta(env *wsmitm.Envelope) bool {
	if !a.p.config.Compression.OutputReduce.RepetitionDetectionEnabled || !env.Kind.IsTextDelta() || env.Delta == "" {
		return false
	}
	idx := a.loadRepdetIndex()
	if idx == nil || len(idx.Blocks()) == 0 {
		return false
	}
	rewritten, matches := idx.Rewrite(env.Delta)
	if len(matches) == 0 {
		return false
	}
	saved := 0
	for _, m := range matches {
		saved += m.Length
	}
	env.Delta = rewritten
	a.p.outputReduceCounters.RecordRepdetRewrite(len(matches), saved)
	return true
}

func (a *wsPhaseFAdapter) applyRepdetResponse(env *wsmitm.Envelope) bool {
	if !a.p.config.Compression.OutputReduce.RepetitionDetectionEnabled || !env.Kind.IsTerminal() || len(env.Response) == 0 {
		return false
	}
	idx := a.loadRepdetIndex()
	if idx == nil || len(idx.Blocks()) == 0 {
		return false
	}
	rewritten, saved := rewriteOpenAIResponseBody(env.Response, idx)
	if saved <= 0 {
		return false
	}
	env.Response = rewritten
	a.p.outputReduceCounters.RecordRepdetRewrite(1, saved)
	return true
}

func (a *wsPhaseFAdapter) loadRepdetIndex() *repdet.Index {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.repdetIndex
}

type wsRequestReplacer func([]byte) error

func wsRequestBody(env *wsmitm.Envelope) ([]byte, wsRequestReplacer, bool) {
	if jsonObject(env.Body) {
		return append([]byte(nil), env.Body...), func(next []byte) error {
			env.Body = append(json.RawMessage(nil), next...)
			if env.Fields != nil {
				env.Fields["body"] = append(json.RawMessage(nil), next...)
			}
			return nil
		}, true
	}
	if jsonObject(env.Request) {
		return append([]byte(nil), env.Request...), func(next []byte) error {
			env.Request = append(json.RawMessage(nil), next...)
			if env.Fields != nil {
				env.Fields["request"] = append(json.RawMessage(nil), next...)
			}
			return nil
		}, true
	}
	if wsEnvelopeLooksLikeRequestBody(env) {
		return append([]byte(nil), env.Raw...), func(next []byte) error {
			parsed, err := wsmitm.Parse(next)
			if err != nil {
				return err
			}
			*env = parsed
			return nil
		}, true
	}
	return nil, nil, false
}

func wsEnvelopeLooksLikeRequestBody(env *wsmitm.Envelope) bool {
	if !jsonObject(env.Raw) {
		return false
	}
	_, hasInput := env.Fields["input"]
	_, hasMessages := env.Fields["messages"]
	if hasInput || hasMessages {
		return true
	}
	_, hasModel := env.Fields["model"]
	_, hasStream := env.Fields["stream"]
	return hasModel && hasStream
}

func jsonObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func wsStreamcutLine(env *wsmitm.Envelope) []byte {
	body, _ := json.Marshal(map[string]string{
		"type":  string(env.Kind),
		"delta": env.Delta,
	})
	line := make([]byte, 0, len(body)+8)
	line = append(line, "data: "...)
	line = append(line, body...)
	line = append(line, '\n', '\n')
	return line
}

func wsCodexSessionID(body []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	for _, key := range []string{"conversation_id", "session_id", "user_id"} {
		if s := rawJSONString(raw[key]); s != "" {
			return "codex-wss:" + s
		}
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw["metadata"], &metadata); err == nil {
		for _, key := range []string{"conversation_id", "session_id", "user_id"} {
			if s := rawJSONString(metadata[key]); s != "" {
				return "codex-wss:" + s
			}
		}
	}
	return ""
}
