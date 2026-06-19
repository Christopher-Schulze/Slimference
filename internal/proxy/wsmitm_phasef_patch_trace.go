package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

const wssPatchTraceListLimit = 16

type wssPatchContextTrace struct {
	requests       int
	bytes          int
	hashes         map[string]struct{}
	kinds          map[string]int
	failed         bool
	conflict       bool
	rejected       bool
	binary         bool
	rename         bool
	singleHash     string
	multipleHashes bool
	singleKind     string
	multipleKinds  bool
}

func attachWSSPatchContextDebugFacts(facts map[string]string, messages []types.Message, meta wssRequestMeta) {
	if facts == nil {
		return
	}
	trace := collectWSSPatchContextTrace(messages, meta)
	if trace.requests == 0 {
		return
	}
	facts["wss.patch_context_candidate"] = "true"
	facts["wss.patch_context_requests"] = strconv.Itoa(trace.requests)
	facts["wss.patch_context_bytes"] = strconv.Itoa(trace.bytes)
	if trace.singleKind != "" && !trace.multipleKinds {
		facts["wss.patch_context_kind"] = trace.singleKind
	}
	if trace.singleHash != "" && !trace.multipleHashes {
		facts["wss.patch_context_hash"] = trace.singleHash
	}
	attachWSSPatchTraceHashFacts(facts, "wss.patch_context_hashes", "wss.patch_context_hash_count", trace.hashes)
	if len(trace.kinds) > 0 {
		facts["wss.patch_context_kinds"] = wssCompactCountMap(trace.kinds)
	}
	if trace.failed {
		facts["wss.patch_context_failed"] = "true"
	}
	if trace.conflict {
		facts["wss.patch_context_conflict"] = "true"
	}
	if trace.rejected {
		facts["wss.patch_context_rejected"] = "true"
	}
	if trace.binary {
		facts["wss.patch_context_binary"] = "true"
	}
	if trace.rename {
		facts["wss.patch_context_rename"] = "true"
	}
}

func collectWSSPatchContextTrace(messages []types.Message, meta wssRequestMeta) wssPatchContextTrace {
	trace := wssPatchContextTrace{
		hashes: map[string]struct{}{},
		kinds:  map[string]int{},
	}
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			commandLine := wssReadTraceCommandLine(block, meta.ToolUseIndex)
			if commandLine == "" {
				continue
			}
			kind := wssPatchContextCommandKind(commandLine)
			if kind == "" {
				continue
			}
			text := block.Text
			trace.requests++
			trace.bytes += len(text)
			trace.kinds[kind]++
			if trace.singleKind == "" {
				trace.singleKind = kind
			} else if trace.singleKind != kind {
				trace.multipleKinds = true
			}
			hash := wssPatchTraceHash(kind + ":" + text)
			trace.hashes[hash] = struct{}{}
			if trace.singleHash == "" {
				trace.singleHash = hash
			} else if trace.singleHash != hash {
				trace.multipleHashes = true
			}
			wssPatchTraceRiskSignals(text, &trace)
		}
	}
	return trace
}

func wssPatchContextCommandKind(commandLine string) string {
	class := wssToolCommandClass(commandLine)
	switch class {
	case "git_diff", "git_diff_stat", "git_show", "git_show_stat":
		return class
	default:
		return ""
	}
}

func wssPatchTraceRiskSignals(text string, trace *wssPatchContextTrace) {
	if trace == nil {
		return
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "fatal:") || strings.Contains(lower, "error:") || strings.Contains(lower, "failed") {
		trace.failed = true
	}
	if strings.Contains(text, "<<<<<<<") || strings.Contains(text, "=======") || strings.Contains(text, ">>>>>>>") || strings.Contains(lower, "conflict") {
		trace.conflict = true
	}
	if strings.Contains(lower, "rejected") || strings.Contains(lower, ".rej") {
		trace.rejected = true
	}
	if strings.Contains(lower, "binary files") || strings.Contains(lower, "git binary patch") {
		trace.binary = true
	}
	if strings.Contains(lower, "rename from ") || strings.Contains(lower, "rename to ") {
		trace.rename = true
	}
}

func attachWSSPatchTraceHashFacts(facts map[string]string, listKey, countKey string, hashes map[string]struct{}) {
	values := sortedWSSReadTraceHashes(hashes)
	if len(values) == 0 {
		return
	}
	facts[countKey] = strconv.Itoa(len(values))
	if len(values) > wssPatchTraceListLimit {
		values = values[:wssPatchTraceListLimit]
	}
	facts[listKey] = strings.Join(values, ",")
}

func wssPatchTraceHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
