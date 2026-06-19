package proxy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

type windowDecision struct {
	Size   int
	Score  float64
	Reason string
	Min    int
	Max    int
}

func (d windowDecision) String() string {
	return fmt.Sprintf("window=%d score=%.2f reason=%q bounds=[%d,%d]", d.Size, d.Score, d.Reason, d.Min, d.Max)
}

func resolveWindow(messages []types.Message, baseWindow int, enabled bool, wmin, wmax int) windowDecision {
	if wmin <= 0 {
		wmin = 3
	}
	if wmax <= 0 {
		wmax = 12
	}
	if !enabled {
		return windowDecision{Size: baseWindow, Reason: "adaptive disabled", Min: wmin, Max: wmax}
	}
	if len(messages) < baseWindow+2 {
		return windowDecision{Size: baseWindow, Reason: "too few messages", Min: wmin, Max: wmax}
	}

	recentStart := len(messages) - 10
	if recentStart < 0 {
		recentStart = 0
	}
	score := windowComplexityScore(messages[recentStart:])
	adjusted := baseWindow + int(math.Round(score*4)) - 2
	if adjusted < wmin {
		return windowDecision{Size: wmin, Score: score, Reason: "clamped to min", Min: wmin, Max: wmax}
	}
	if adjusted > wmax {
		return windowDecision{Size: wmax, Score: score, Reason: "clamped to max", Min: wmin, Max: wmax}
	}
	reason := "adaptive"
	if adjusted == baseWindow {
		reason = "adaptive (no change)"
	}
	return windowDecision{Size: adjusted, Score: score, Reason: reason, Min: wmin, Max: wmax}
}

func windowComplexityScore(msgs []types.Message) float64 {
	if len(msgs) == 0 {
		return 0.5
	}
	fileScore := normalizeWindowScore(float64(countWindowFilePaths(msgs)), 1, 15)
	toolScore := normalizeWindowScore(float64(countWindowToolDiversity(msgs)), 1, 8)
	anchorScore := windowAnchorDensity(msgs)
	return 0.3*fileScore + 0.3*toolScore + 0.4*anchorScore
}

func normalizeWindowScore(v, lo, hi float64) float64 {
	if hi <= lo {
		return 0
	}
	n := (v - lo) / (hi - lo)
	if n < 0 {
		return 0
	}
	if n > 1 {
		return 1
	}
	return n
}

func countWindowFilePaths(msgs []types.Message) int {
	seen := make(map[string]struct{})
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if path := windowBlockFilePath(block); path != "" {
				seen[path] = struct{}{}
			}
		}
	}
	return len(seen)
}

func countWindowToolDiversity(msgs []types.Message) int {
	seen := make(map[string]struct{})
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ToolName != "" {
				seen[strings.ToLower(block.ToolName)] = struct{}{}
			}
		}
	}
	return len(seen)
}

func windowAnchorDensity(msgs []types.Message) float64 {
	if len(msgs) == 0 {
		return 0
	}
	anchors := 0
	for _, msg := range msgs {
		if windowAnchorMessage(msg) {
			anchors++
		}
	}
	return float64(anchors) / float64(len(msgs))
}

func windowAnchorMessage(msg types.Message) bool {
	text := strings.ToLower(messageText(msg))
	if strings.Contains(text, "error") || strings.Contains(text, "panic") ||
		strings.Contains(text, "traceback") || strings.Contains(text, "exception") ||
		strings.Contains(text, "fatal") {
		return true
	}
	if msg.Role == "user" && len(strings.Fields(text)) < 50 {
		for _, word := range []string{"yes", "ja", "approved", "no", "nein", "stop", "cancel"} {
			if strings.Contains(text, word) {
				return true
			}
		}
	}
	for _, block := range msg.Content {
		if block.Type == "tool_use" && strings.Contains(strings.ToLower(block.ToolName), "edit") {
			return true
		}
		if path := windowBlockFilePath(block); path != "" && looksConfigPath(path) {
			return true
		}
	}
	return false
}

func messageText(msg types.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Text != "" {
			b.WriteString(block.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func windowBlockFilePath(block types.ContentBlock) string {
	input := block.ToolInput
	if input == "" {
		return ""
	}
	if path, parsed := structuredWindowBlockFilePath(block); parsed {
		return path
	}
	return scanWindowBlockFilePath(input)
}

func structuredWindowBlockFilePath(block types.ContentBlock) (string, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(block.ToolInput), &fields); err != nil {
		return "", false
	}
	workdir := proxyToolWorkdir(fields)
	if path := structuredWindowPathFromFields(fields, workdir, looksLikeReadTool(block.ToolName)); path != "" {
		return path, true
	}
	for _, key := range []string{"arguments", "input", "params", "parameters"} {
		if path := structuredWindowNestedPath(fields[key], workdir, looksLikeReadTool(block.ToolName)); path != "" {
			return path, true
		}
	}
	if req := readRequestFromCommandLine(proxyLayer0CommandLine(block)); req.FilePath != "" {
		return req.FilePath, true
	}
	return "", true
}

func structuredWindowNestedPath(raw json.RawMessage, workdir string, readTool bool) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	return structuredWindowPathFromFields(fields, workdir, readTool)
}

func structuredWindowPathFromFields(fields map[string]json.RawMessage, workdir string, readTool bool) string {
	for _, key := range []string{"path", "file_path", "filename", "filepath", "file", "absolute_path"} {
		if path := strings.TrimSpace(rawJSONString(fields[key])); path != "" {
			return proxyPathWithWorkdir(path, workdir)
		}
	}
	if readTool {
		for _, key := range []string{"uri", "target", "source_path"} {
			if path := strings.TrimSpace(rawJSONString(fields[key])); path != "" {
				return proxyPathWithWorkdir(path, workdir)
			}
		}
	}
	return ""
}

func scanWindowBlockFilePath(input string) string {
	for _, key := range []string{`"path"`, `"file_path"`, `"filename"`, `"filepath"`, `"file"`} {
		idx := strings.Index(input, key)
		if idx < 0 {
			continue
		}
		rest := input[idx+len(key):]
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			continue
		}
		rest = strings.TrimSpace(rest[colonIdx+1:])
		if len(rest) == 0 || rest[0] != '"' {
			continue
		}
		end := strings.Index(rest[1:], `"`)
		if end < 0 {
			continue
		}
		return rest[1 : end+1]
	}
	return ""
}

func looksConfigPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return strings.HasSuffix(path, ".json") ||
		strings.HasSuffix(path, ".toml") ||
		strings.HasSuffix(path, ".yaml") ||
		strings.HasSuffix(path, ".yml") ||
		strings.HasSuffix(path, ".env") ||
		strings.HasSuffix(path, ".conf") ||
		strings.HasSuffix(path, "makefile") ||
		strings.HasSuffix(path, "dockerfile")
}

func extractSessionID(provider types.Provider, body []byte, headers http.Header) string {
	switch provider {
	case types.Anthropic:
		if org := headers.Get("anthropic-organization-id"); org != "" {
			if uid := extractMetadataUserID(body); uid != "" {
				return "anthropic:" + org + ":" + uid
			}
			return "anthropic:" + org
		}
		if trace := headers.Get("anthropic-trace-id"); trace != "" {
			return "anthropic:" + trace
		}
	case types.OpenAI, types.CodexChatGPT:
		if provider == types.CodexChatGPT {
			if sid := extractCodexHTTPThreadSessionID(body); sid != "" {
				return sid
			}
			if sid := codexStrongThreadHeaderSessionID(headers); sid != "" {
				return sid
			}
		}
		if cid := headers.Get("openai-conversation-id"); cid != "" {
			return "openai:" + cid
		}
		if rid := extractPreviousResponseID(body); rid != "" {
			return "openai:" + rid
		}
	}
	return contentHashSessionID(body)
}

func extractCodexHTTPThreadSessionID(body []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	if sid := codexStrongThreadRawID(raw); sid != "" {
		return "codex-http:" + sid
	}
	return ""
}

func codexStrongThreadSessionID(raw map[string]json.RawMessage) string {
	if sid := codexStrongThreadRawID(raw); sid != "" {
		return "codex-wss:" + sid
	}
	return ""
}

func codexStrongThreadRawID(raw map[string]json.RawMessage) string {
	if sid := codexRawSessionID(raw); sid != "" {
		return sid
	}
	if sid := codexNestedSessionID(raw["metadata"]); sid != "" {
		return sid
	}
	if sid := codexNestedSessionID(raw["client_metadata"]); sid != "" {
		return sid
	}
	return ""
}

func codexStrongThreadHeaderSessionID(headers http.Header) string {
	for _, key := range []string{"x-codex-thread-id", "x-codex-conversation-id", "x-codex-session-id"} {
		if sid := strings.TrimSpace(headers.Get(key)); sid != "" {
			return "codex-http:" + sid
		}
	}
	return ""
}

func extractClientFamily(provider types.Provider, body []byte, headers http.Header) string {
	if provider != types.CodexChatGPT {
		return ""
	}
	if family := extractCodexHTTPClientFamily(body); family != "" {
		return family
	}
	if family := normalizeCodexClientFamily(headers.Get("User-Agent")); family != "" {
		return family
	}
	return "codex"
}

func extractCodexHTTPClientFamily(body []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	for _, source := range []json.RawMessage{raw["client_metadata"], raw["metadata"]} {
		if family := codexMetadataClientFamily(source); family != "" {
			return family
		}
	}
	return ""
}

func codexRawSessionID(fields map[string]json.RawMessage) string {
	for _, key := range []string{"thread_id", "conversation_id", "session_id"} {
		if s := rawJSONString(fields[key]); s != "" {
			return s
		}
	}
	return ""
}

func codexNestedSessionID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	if sid := codexRawSessionID(fields); sid != "" {
		return sid
	}
	return codexTurnMetadataSessionID(raw)
}

func contentHashSessionID(body []byte) string {
	text := extractFirstUserText(body)
	if text == "" {
		return "empty"
	}
	if len(text) > 200 {
		text = text[:200]
	}
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("fh:%x", h[:8])
}

func extractFirstUserText(body []byte) string {
	var req struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		} `json:"messages"`
		Input interface{} `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			return contentValueString(msg.Content)
		}
	}
	return inputValueFirstUserText(req.Input)
}

func contentValueString(value interface{}) string {
	switch content := value.(type) {
	case string:
		return content
	case []interface{}:
		parts := make([]string, 0, len(content))
		for _, item := range content {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", value)
	}
}

func inputValueFirstUserText(value interface{}) string {
	switch input := value.(type) {
	case string:
		return input
	case []interface{}:
		for _, item := range input {
			if text := inputItemUserText(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func inputItemUserText(value interface{}) string {
	item, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	role, _ := item["role"].(string)
	if role != "" && role != "user" {
		return ""
	}
	if content, ok := item["content"]; ok {
		return contentValueString(content)
	}
	if text, ok := item["text"].(string); ok {
		return text
	}
	return ""
}

func extractMetadataUserID(body []byte) string {
	var req struct {
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Metadata.UserID
}

func extractPreviousResponseID(body []byte) string {
	var req struct {
		PreviousResponseID string `json:"previous_response_id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.PreviousResponseID
}
