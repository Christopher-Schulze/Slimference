package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/slimference/slimference/internal/beterse"
	"github.com/slimference/slimference/internal/outstop"
	"github.com/slimference/slimference/internal/outstop/repdet"
	"github.com/slimference/slimference/internal/proxy/wsmitm"
	"github.com/slimference/slimference/internal/qualityab"
	"github.com/slimference/slimference/internal/staleread"
	"github.com/slimference/slimference/internal/types"
)

type wsPhaseFAdapter struct {
	p *Proxy

	mu          sync.Mutex
	messages    []types.Message
	repdetIndex *repdet.Index
	toolUses    map[string]types.ContentBlock
	counters    wsPhaseFCounters
}

type wsPhaseFCounters struct {
	requestsSeen           atomic.Int64
	requestBodiesSeen      atomic.Int64
	requestMessagesIndexed atomic.Int64
	responseTextDeltasSeen atomic.Int64
	terminalResponsesSeen  atomic.Int64
	mutations              atomic.Int64
}

type wsPhaseFTelemetry struct {
	RequestsSeen           int64
	RequestBodiesSeen      int64
	RequestMessagesIndexed int64
	ResponseTextDeltasSeen int64
	TerminalResponsesSeen  int64
	Mutations              int64
}

func (a *wsPhaseFAdapter) snapshot() wsPhaseFTelemetry {
	if a == nil {
		return wsPhaseFTelemetry{}
	}
	return wsPhaseFTelemetry{
		RequestsSeen:           a.counters.requestsSeen.Load(),
		RequestBodiesSeen:      a.counters.requestBodiesSeen.Load(),
		RequestMessagesIndexed: a.counters.requestMessagesIndexed.Load(),
		ResponseTextDeltasSeen: a.counters.responseTextDeltasSeen.Load(),
		TerminalResponsesSeen:  a.counters.terminalResponsesSeen.Load(),
		Mutations:              a.counters.mutations.Load(),
	}
}

func (d *PhaseFDispatcher) newWSPhaseFAdapter() *wsPhaseFAdapter {
	return &wsPhaseFAdapter{p: d.Proxy}
}

func (a *wsPhaseFAdapter) handle(_ context.Context, dir wsmitm.Direction, env *wsmitm.Envelope) (bool, error) {
	if a == nil || a.p == nil || a.p.config == nil || env == nil || env.Kind.IsControl() {
		return false, nil
	}
	switch dir {
	case wsmitm.DirClientToServer:
		if env.Kind != wsmitm.FrameKindRequest && !wsRequestBodyCandidate(env) {
			return false, nil
		}
		return a.handleRequest(env), nil
	case wsmitm.DirServerToClient:
		if env.Kind == wsmitm.FrameKindUnknown {
			return false, nil
		}
		return a.handleResponse(env), nil
	default:
		return false, nil
	}
}

func wsRequestBodyCandidate(env *wsmitm.Envelope) bool {
	return jsonObject(env.Body) || jsonObject(env.Request) || wsEnvelopeLooksLikeRequestBody(env)
}

func (a *wsPhaseFAdapter) handleRequest(env *wsmitm.Envelope) bool {
	a.counters.requestsSeen.Add(1)
	body, replace, ok := wsRequestBody(env)
	if !ok {
		return false
	}
	a.counters.requestBodiesSeen.Add(1)
	mutated, messages, changed := a.applyInputPipeline(body)
	if len(messages) > 0 {
		a.counters.requestMessagesIndexed.Add(1)
	}
	a.mu.Lock()
	a.messages = messages
	a.repdetIndex = buildRepdetIndex(messages)
	if a.toolUses == nil {
		a.toolUses = make(map[string]types.ContentBlock)
	}
	for id, use := range proxyToolUseIndex(messages) {
		a.toolUses[id] = use
	}
	a.mu.Unlock()
	if !changed {
		return false
	}
	if replace(mutated) != nil {
		return false
	}
	a.counters.mutations.Add(1)
	return true
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
		rememberedToolUses := a.loadToolUses()
		if l0Messages, saved := applyProxyLayer0WithSessionAndToolUses(messages, wsCodexSessionID(out), rememberedToolUses); saved > 0 {
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
	a.rememberToolUsesFromResponse(env)
	if env.Kind.IsTextDelta() {
		a.counters.responseTextDeltasSeen.Add(1)
	}
	if env.Kind.IsTerminal() {
		a.counters.terminalResponsesSeen.Add(1)
	}
	mutated := false
	if a.applyRepdetDelta(env) {
		mutated = true
	}
	if a.applyRepdetResponse(env) {
		mutated = true
	}
	if mutated {
		a.counters.mutations.Add(1)
	}
	return mutated
}

func (a *wsPhaseFAdapter) rememberToolUsesFromResponse(env *wsmitm.Envelope) {
	if a == nil || env == nil {
		return
	}
	a.rememberToolUseItem(env.Item)
	if len(env.Response) == 0 {
		return
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(env.Response, &response); err != nil {
		return
	}
	var output []json.RawMessage
	if err := json.Unmarshal(response["output"], &output); err != nil {
		return
	}
	for _, item := range output {
		a.rememberToolUseItem(item)
	}
}

func (a *wsPhaseFAdapter) rememberToolUseItem(raw json.RawMessage) {
	if a == nil || len(raw) == 0 {
		return
	}
	msg, ok, err := codexInputItemToMessage(0, raw)
	if err != nil || !ok {
		return
	}
	toolUses := proxyToolUseIndex([]types.Message{msg})
	if len(toolUses) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.toolUses == nil {
		a.toolUses = make(map[string]types.ContentBlock, len(toolUses))
	}
	for id, use := range toolUses {
		a.toolUses[id] = use
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

func (a *wsPhaseFAdapter) loadToolUses() map[string]types.ContentBlock {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.toolUses) == 0 {
		return nil
	}
	out := make(map[string]types.ContentBlock, len(a.toolUses))
	for id, use := range a.toolUses {
		out[id] = use
	}
	return out
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
