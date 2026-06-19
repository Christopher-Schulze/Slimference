package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

const wssReadTraceListLimit = 16

type wssReadDependencyTrace struct {
	requests        int
	full            int
	partial         int
	recentlyEdited  int
	pathHashes      map[string]struct{}
	rangeHashes     map[string]struct{}
	singleRangeText string
	multipleRanges  bool
}

func attachWSSReadDependencyDebugFacts(facts map[string]string, messages []types.Message, meta wssRequestMeta) {
	if facts == nil {
		return
	}
	trace := collectWSSReadDependencyTrace(messages, meta)
	if trace.requests == 0 {
		return
	}
	facts["wss.dependency_trace"] = "true"
	facts["wss.read_trace_requests"] = strconv.Itoa(trace.requests)
	if trace.full > 0 {
		facts["wss.read_full_count"] = strconv.Itoa(trace.full)
	}
	if trace.partial > 0 {
		facts["wss.read_partial_count"] = strconv.Itoa(trace.partial)
	}
	if trace.recentlyEdited > 0 {
		facts["wss.read_after_edit"] = "true"
		facts["wss.read_after_edit_count"] = strconv.Itoa(trace.recentlyEdited)
	}
	attachWSSReadTraceHashFacts(facts, "wss.read_file_path_hash", "wss.read_file_path_hashes", "wss.read_file_path_hash_count", trace.pathHashes)
	attachWSSReadTraceHashFacts(facts, "wss.read_range_hash", "wss.read_range_hashes", "wss.read_range_hash_count", trace.rangeHashes)
	if trace.singleRangeText != "" && !trace.multipleRanges {
		facts["wss.read_range"] = trace.singleRangeText
	}
}

func collectWSSReadDependencyTrace(messages []types.Message, meta wssRequestMeta) wssReadDependencyTrace {
	trace := wssReadDependencyTrace{
		pathHashes:  map[string]struct{}{},
		rangeHashes: map[string]struct{}{},
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
			req, ok := filter.ReadRequestFromCommandLine(commandLine)
			if !ok || strings.TrimSpace(req.Path) == "" {
				continue
			}
			trace.requests++
			pathHash := wssReadTraceHash("path:" + wssReadTraceNormalizePath(req.Path))
			trace.pathHashes[pathHash] = struct{}{}
			rangeText := wssReadTraceRangeText(req)
			rangeHash := wssReadTraceHash("range:" + rangeText)
			trace.rangeHashes[rangeHash] = struct{}{}
			if trace.singleRangeText == "" {
				trace.singleRangeText = rangeText
			} else if trace.singleRangeText != rangeText {
				trace.multipleRanges = true
			}
			if req.IsFull() {
				trace.full++
			} else {
				trace.partial++
			}
			if proxyReadFileContext(meta.SessionID, commandLine).RecentlyEdited {
				trace.recentlyEdited++
			}
		}
	}
	return trace
}

func wssReadTraceCommandLine(block types.ContentBlock, toolUses map[string]types.ContentBlock) string {
	use, resolved := proxyResolveToolUseDetailed(block, toolUses)
	if resolved {
		if commandLine := proxyLayer0CommandLine(use); commandLine != "" {
			return commandLine
		}
	}
	if commandLine := proxyLayer0CommandLine(block); commandLine != "" {
		return commandLine
	}
	return proxyInferCommandLineFromToolResult(block.Text)
}

func wssReadTraceNormalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func wssReadTraceRangeText(req filter.ReadRequest) string {
	if req.IsFull() {
		return "full"
	}
	return "lines:" + strconv.Itoa(req.Offset) + ":" + strconv.Itoa(req.Limit)
}

func attachWSSReadTraceHashFacts(facts map[string]string, singleKey, listKey, countKey string, hashes map[string]struct{}) {
	values := sortedWSSReadTraceHashes(hashes)
	if len(values) == 0 {
		return
	}
	facts[countKey] = strconv.Itoa(len(values))
	if len(values) == 1 {
		facts[singleKey] = values[0]
		return
	}
	if len(values) > wssReadTraceListLimit {
		values = values[:wssReadTraceListLimit]
	}
	facts[listKey] = strings.Join(values, ",")
}

func sortedWSSReadTraceHashes(hashes map[string]struct{}) []string {
	if len(hashes) == 0 {
		return nil
	}
	values := make([]string, 0, len(hashes))
	for hash := range hashes {
		values = append(values, hash)
	}
	sort.Strings(values)
	return values
}

func wssReadTraceHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
