package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

const wssReadTraceListLimit = 16

var wssReadTraceUnifiedNewRangeRe = regexp.MustCompile(`@@\s+-\d+(?:,\d+)?\s+\+(\d+)(?:,(\d+))?\s+@@`)

type wssReadDependencyTrace struct {
	requests        int
	full            int
	partial         int
	recentlyEdited  int
	pathHashes      map[string]struct{}
	rangeHashes     map[string]struct{}
	singleRangeText string
	multipleRanges  bool
	fileHashAfter   string
	editTurnSeq     string
	changedRange    string
	exactAmbiguous  bool
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
	if trace.fileHashAfter != "" && trace.editTurnSeq != "" && trace.changedRange != "" && !trace.exactAmbiguous {
		facts["wss.file_hash_after"] = trace.fileHashAfter
		facts["wss.edit_turn_seq"] = trace.editTurnSeq
		facts["wss.changed_range"] = trace.changedRange
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
				if wssReadTraceAddSearchResultDependencies(&trace, commandLine, block.Text) {
					continue
				}
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
			recentEdit := wssReadTraceRecentEdit(meta.SessionID, req.Path, 2)
			if recentEdit.hit {
				trace.recentlyEdited++
				if req.IsFull() {
					payload, payloadOK := wssReadTraceSuccessfulReadPayload(block.Text)
					changedRange := wssReadTraceChangedRangeForPath(messages, meta.ToolUseIndex, req.Path)
					if payloadOK && changedRange != "" && recentEdit.turnSeq != "" {
						fileHash := wssReadTraceHash("file-after:" + payload)
						trace.addExactPostEditRead(fileHash, recentEdit.turnSeq, changedRange)
					}
				}
			}
		}
	}
	return trace
}

func wssReadTraceAddSearchResultDependencies(trace *wssReadDependencyTrace, commandLine, text string) bool {
	if trace == nil || !wssReadTraceSearchCommand(commandLine) {
		return false
	}
	payload, ok := wssReadTraceSuccessfulSearchPayload(text)
	if !ok {
		return false
	}
	matches, nonEmpty := wssReadTraceSearchMatches(payload)
	if len(matches) == 0 || len(matches)*2 < nonEmpty {
		return false
	}
	trace.requests++
	trace.partial++
	for _, match := range matches {
		pathHash := wssReadTraceHash("path:" + wssReadTraceNormalizePath(match.path))
		rangeText := "lines:" + strconv.Itoa(match.line) + ":" + strconv.Itoa(match.line)
		rangeHash := wssReadTraceHash("range:" + rangeText)
		trace.pathHashes[pathHash] = struct{}{}
		trace.rangeHashes[rangeHash] = struct{}{}
		if trace.singleRangeText == "" {
			trace.singleRangeText = rangeText
		} else if trace.singleRangeText != rangeText {
			trace.multipleRanges = true
		}
	}
	return true
}

func wssReadTraceSearchCommand(commandLine string) bool {
	_, filterCommandLine := proxyLayer0FilterCommandForCompaction(commandLine)
	argv := filter.ArgvForCapturedOutput(filterCommandLine)
	if len(argv) == 0 {
		return false
	}
	base := wssCommandBase(argv[0])
	switch base {
	case "rg", "ripgrep", "grep", "ggrep", "ag", "ack", "ug", "ugrep", "sift":
		return !wssArgvContains(argv[1:], "--files")
	case "git":
		return len(argv) > 1 && argv[1] == "grep"
	default:
		return false
	}
}

func wssReadTraceSuccessfulSearchPayload(text string) (string, bool) {
	header, payload, ok := splitCodexExecEnvelope(text)
	if !ok {
		return text, true
	}
	if !strings.Contains(header, "Process exited with code 0") {
		return "", false
	}
	return payload, strings.TrimSpace(payload) != ""
}

type wssReadTraceSearchMatch struct {
	path string
	line int
}

func wssReadTraceSearchMatches(payload string) ([]wssReadTraceSearchMatch, int) {
	var matches []wssReadTraceSearchMatch
	nonEmpty := 0
	for rawLine := range strings.SplitSeq(payload, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "Total output lines:") || line == "--" {
			continue
		}
		nonEmpty++
		match, ok := wssReadTraceSearchMatchLine(line)
		if ok {
			matches = append(matches, match)
		}
	}
	return matches, nonEmpty
}

func wssReadTraceSearchMatchLine(line string) (wssReadTraceSearchMatch, bool) {
	first := strings.IndexByte(line, ':')
	if first <= 0 {
		return wssReadTraceSearchMatch{}, false
	}
	second := strings.IndexByte(line[first+1:], ':')
	if second <= 0 {
		return wssReadTraceSearchMatch{}, false
	}
	lineNo := line[first+1 : first+1+second]
	n, err := strconv.Atoi(lineNo)
	if err != nil || n <= 0 {
		return wssReadTraceSearchMatch{}, false
	}
	path := strings.TrimSpace(line[:first])
	if path == "" || strings.Contains(path, "://") {
		return wssReadTraceSearchMatch{}, false
	}
	return wssReadTraceSearchMatch{path: path, line: n}, true
}

func (t *wssReadDependencyTrace) addExactPostEditRead(fileHashAfter, editTurnSeq, changedRange string) {
	if t == nil || fileHashAfter == "" || editTurnSeq == "" || changedRange == "" {
		return
	}
	if t.fileHashAfter == "" && t.editTurnSeq == "" && t.changedRange == "" {
		t.fileHashAfter = fileHashAfter
		t.editTurnSeq = editTurnSeq
		t.changedRange = changedRange
		return
	}
	if t.fileHashAfter != fileHashAfter || t.editTurnSeq != editTurnSeq || t.changedRange != changedRange {
		t.exactAmbiguous = true
	}
}

type wssReadTraceRecentEditState struct {
	hit     bool
	turnSeq string
}

func wssReadTraceRecentEdit(sessionID, path string, previousTurns int) wssReadTraceRecentEditState {
	path = filepath.Clean(strings.TrimSpace(path))
	if sessionID == "" || path == "" || path == "." {
		return wssReadTraceRecentEditState{}
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return wssReadTraceRecentEditState{}
	}
	state, err := sessions.LoadHookState(sessions.DefaultHookStateDir(home), sessionID)
	if err != nil {
		return wssReadTraceRecentEditState{}
	}
	start := max(len(state.Turns)-1-previousTurns, 0)
	for i := len(state.Turns) - 1; i >= start; i-- {
		turn := state.Turns[i]
		for _, edited := range turn.FilesEdited {
			if filepath.Clean(strings.TrimSpace(edited)) == path {
				return wssReadTraceRecentEditState{hit: true, turnSeq: wssReadTraceTurnSeqFact(turn.ID, i)}
			}
		}
	}
	return wssReadTraceRecentEditState{}
}

func wssReadTraceTurnSeqFact(turnID string, fallbackIndex int) string {
	turnID = sessions.SafeOptionalTurnID(turnID)
	if n, ok := strings.CutPrefix(turnID, "turn-"); ok {
		if seq, err := strconv.Atoi(n); err == nil && seq > 0 {
			return strconv.Itoa(seq)
		}
	}
	if turnID != "" {
		return wssReadTraceHash("turn:" + turnID)
	}
	if fallbackIndex >= 0 {
		return strconv.Itoa(fallbackIndex + 1)
	}
	return ""
}

func wssReadTraceSuccessfulReadPayload(text string) (string, bool) {
	header, payload, ok := splitCodexExecEnvelope(text)
	if !ok {
		return text, true
	}
	if !strings.Contains(header, "Process exited with code 0") {
		return "", false
	}
	return payload, true
}

func wssReadTraceChangedRangeForPath(messages []types.Message, toolUses map[string]types.ContentBlock, path string) string {
	path = wssReadTraceNormalizePath(path)
	if path == "" {
		return ""
	}
	var ranges []string
	unknownFull := false
	mergedToolUses := mergedProxyToolUseIndex(proxyToolUseIndex(messages), toolUses)
	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_use":
				if r, ok := wssReadTraceChangedRangeFromEditBlock(block, path); ok {
					ranges = append(ranges, r)
				}
			case "tool_result":
				use, resolved := proxyResolveToolUseDetailed(block, mergedToolUses)
				if !resolved {
					continue
				}
				if r, ok := wssReadTraceChangedRangeFromEditBlock(use, path); ok {
					ranges = append(ranges, r)
				}
			}
		}
	}
	if len(ranges) == 0 {
		return "full"
	}
	if slices.Contains(ranges, "full") {
		unknownFull = true
	}
	if unknownFull {
		return "full"
	}
	return strings.Join(compactStringSet(ranges), ",")
}

func wssReadTraceChangedRangeFromEditBlock(block types.ContentBlock, path string) (string, bool) {
	paths := proxyLayer0EditPaths(block)
	if len(paths) == 0 || !wssReadTracePathListContains(paths, path) {
		return "", false
	}
	if len(paths) != 1 {
		return "full", true
	}
	var ranges []string
	for _, patchText := range wssReadTracePatchTexts(block) {
		if r := wssReadTraceChangedRangeFromPatch(patchText); r != "" {
			ranges = append(ranges, r)
		}
	}
	if len(ranges) == 0 {
		return "full", true
	}
	return strings.Join(compactStringSet(ranges), ","), true
}

func wssReadTracePathListContains(paths []string, target string) bool {
	target = wssReadTraceNormalizePath(target)
	for _, path := range paths {
		if wssReadTraceNormalizePath(path) == target {
			return true
		}
	}
	return false
}

func wssReadTracePatchTexts(block types.ContentBlock) []string {
	input := strings.TrimSpace(block.ToolInput)
	if input == "" {
		return nil
	}
	if raw := rawJSONString(json.RawMessage(input)); raw != "" {
		if wssReadTraceLooksPatchLike(raw) {
			return []string{raw}
		}
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &obj); err != nil {
		if wssReadTraceLooksPatchLike(input) {
			return []string{input}
		}
		return nil
	}
	var out []string
	for _, key := range []string{"patch", "diff", "changes", "command", "cmd", "command_line", "cmdline", "commandLine", "shell_command", "shellCommand"} {
		if value := strings.TrimSpace(rawJSONString(obj[key])); wssReadTraceLooksPatchLike(value) {
			out = append(out, value)
		}
	}
	return out
}

func wssReadTraceLooksPatchLike(text string) bool {
	return strings.Contains(text, "@@") ||
		strings.Contains(text, "*** Update File:") ||
		strings.Contains(text, "*** Add File:") ||
		strings.Contains(text, "*** Delete File:") ||
		strings.Contains(text, "diff --git")
}

func wssReadTraceChangedRangeFromPatch(patchText string) string {
	matches := wssReadTraceUnifiedNewRangeRe.FindAllStringSubmatch(patchText, -1)
	if len(matches) == 0 {
		return ""
	}
	ranges := make([]string, 0, len(matches))
	for _, match := range matches {
		start, err := strconv.Atoi(match[1])
		if err != nil || start <= 0 {
			continue
		}
		count := 1
		if len(match) > 2 && match[2] != "" {
			parsed, err := strconv.Atoi(match[2])
			if err != nil || parsed < 0 {
				continue
			}
			count = parsed
		}
		end := start
		if count > 1 {
			end = start + count - 1
		}
		ranges = append(ranges, "lines:"+strconv.Itoa(start)+":"+strconv.Itoa(end))
	}
	return strings.Join(compactStringSet(ranges), ",")
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
