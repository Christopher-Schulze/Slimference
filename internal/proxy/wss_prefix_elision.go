package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

type wssStatefulPrefixElisionState struct {
	seenTools map[string]struct{}
}

type wssStatefulPrefixElisionProofResult struct {
	Enabled               bool
	Reason                string
	Requests              int
	ToolRequests          int
	InstructionRequests   int
	PrefixBytesSaved      int
	ToolBytesSaved        int
	InstructionBytesSaved int
}

func (a *wsPhaseFAdapter) applyWSSStatefulPrefixElisionProof(body []byte) ([]byte, wssStatefulPrefixElisionProofResult, bool) {
	if a == nil || a.p == nil || a.p.config == nil ||
		!a.p.config.Compression.OutputReduce.CodexWSSStatefulPrefixElisionProofEnabled {
		return body, wssStatefulPrefixElisionProofResult{}, false
	}
	result := wssStatefulPrefixElisionProofResult{Enabled: true, Reason: "guarded"}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		result.Reason = "malformed_json"
		return body, result, false
	}
	scope := strings.TrimSpace(rawJSONString(raw["prompt_cache_key"]))
	if scope == "" {
		result.Reason = "missing_prompt_cache_key"
		return body, result, false
	}
	previousResponse := strings.TrimSpace(rawJSONString(raw["previous_response_id"])) != ""
	toolKey := ""
	if tools, ok := codexReplayToolSchemaSurface(body); ok {
		toolKey = wssStatefulPrefixElisionKey(scope, "tools", tools)
	}
	if toolKey == "" {
		result.Reason = "no_prefix"
		return body, result, false
	}
	elideTools := a.wssStatefulPrefixElisionDecision(previousResponse, toolKey)
	if !previousResponse {
		result.Reason = "seeded_root"
		return body, result, false
	}
	changed := false
	if elideTools {
		result.ToolRequests = 1
		result.ToolBytesSaved = len(raw["tools"])
		delete(raw, "tools")
		changed = true
	}
	if !changed {
		result.Reason = "unseen_prefix"
		return body, result, false
	}
	out, err := json.Marshal(raw)
	if err != nil {
		result.Reason = "marshal_failed"
		return body, result, false
	}
	result.Reason = "applied"
	result.Requests = 1
	result.PrefixBytesSaved = result.ToolBytesSaved + result.InstructionBytesSaved
	return out, result, true
}

func (a *wsPhaseFAdapter) wssStatefulPrefixElisionDecision(previousResponse bool, toolKey string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	elideTools := false
	if toolKey != "" {
		if a.statefulPrefixElision.seenTools == nil {
			a.statefulPrefixElision.seenTools = make(map[string]struct{})
		}
		_, elideTools = a.statefulPrefixElision.seenTools[toolKey]
		if !previousResponse || !elideTools {
			a.statefulPrefixElision.seenTools[toolKey] = struct{}{}
		}
	}
	if !previousResponse {
		return false
	}
	return elideTools
}

func wssStatefulPrefixElisionKey(scope string, kind string, value string) string {
	scopeSum := sha256.Sum256([]byte(scope))
	valueSum := sha256.Sum256([]byte(value))
	return kind + ":" + hex.EncodeToString(scopeSum[:8]) + ":" + hex.EncodeToString(valueSum[:16])
}

func attachWSSStatefulPrefixElisionDebugFacts(meta *wssRequestMeta, result wssStatefulPrefixElisionProofResult, changed bool) {
	if meta == nil || !result.Enabled {
		return
	}
	if meta.DebugFacts == nil {
		meta.DebugFacts = make(map[string]string)
	}
	meta.DebugFacts["wss.stateful_prefix_elision_proof"] = "true"
	meta.DebugFacts["wss.stateful_prefix_elision_reason"] = result.Reason
	meta.DebugFacts["wss.stateful_prefix_elision_changed"] = strconv.FormatBool(changed)
	meta.DebugFacts["wss.stateful_prefix_elision_requests"] = strconv.Itoa(result.Requests)
	meta.DebugFacts["wss.stateful_prefix_elision_tool_requests"] = strconv.Itoa(result.ToolRequests)
	meta.DebugFacts["wss.stateful_prefix_elision_instruction_requests"] = strconv.Itoa(result.InstructionRequests)
	meta.DebugFacts["wss.stateful_prefix_elision_bytes_saved"] = strconv.Itoa(result.PrefixBytesSaved)
	meta.DebugFacts["wss.stateful_prefix_elision_tool_bytes_saved"] = strconv.Itoa(result.ToolBytesSaved)
	meta.DebugFacts["wss.stateful_prefix_elision_instruction_bytes_saved"] = strconv.Itoa(result.InstructionBytesSaved)
}
