package proxy

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/sessions"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

type proxyLayer0Mechanism string

const (
	proxyLayer0MechanismReadDelta     proxyLayer0Mechanism = "read_delta"
	proxyLayer0MechanismCapturedOut   proxyLayer0Mechanism = "captured_output"
	proxyLayer0MechanismCodexEnvelope proxyLayer0Mechanism = "codex_exec_envelope"
)

type codexLayer0Route string

const (
	codexLayer0RouteUnspecified codexLayer0Route = ""
	codexLayer0RouteHTTP        codexLayer0Route = "http"
	codexLayer0RouteWSSPhaseF   codexLayer0Route = "wss_phasef"
)

type codexLayer0Request struct {
	Route             codexLayer0Route
	Messages          []types.Message
	SessionID         string
	RememberedToolUse map[string]types.ContentBlock
}

type codexLayer0Result struct {
	Messages []types.Message
	Stats    proxyLayer0Stats
}

type proxyLayer0Stats struct {
	Route                   codexLayer0Route
	ToolResultBlocks        int
	ToolUseUnresolvedBlocks int
	CommandResolvedBlocks   int
	CommandUnresolvedBlocks int
	ReadDeltaAttempts       int
	ReadDeltaMisses         int
	TokensSaved             int
	BlocksModified          int
	ReadDeltaBlocks         int
	CapturedOutputBlocks    int
	CodexExecEnvelopeBlocks int
}

func (s proxyLayer0Stats) withoutSavings() proxyLayer0Stats {
	s.TokensSaved = 0
	s.BlocksModified = 0
	s.ReadDeltaBlocks = 0
	s.CapturedOutputBlocks = 0
	s.CodexExecEnvelopeBlocks = 0
	return s
}

func applyProxyLayer0(messages []types.Message) ([]types.Message, int) {
	return applyProxyLayer0WithSession(messages, "")
}

func applyProxyLayer0WithSession(messages []types.Message, sessionID string) ([]types.Message, int) {
	return applyProxyLayer0WithSessionAndToolUses(messages, sessionID, nil)
}

func applyProxyLayer0WithSessionAndToolUses(messages []types.Message, sessionID string, rememberedToolUses map[string]types.ContentBlock) ([]types.Message, int) {
	out, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, sessionID, rememberedToolUses)
	return out, stats.TokensSaved
}

func applyProxyLayer0WithSessionAndToolUsesDetailed(messages []types.Message, sessionID string, rememberedToolUses map[string]types.ContentBlock) ([]types.Message, proxyLayer0Stats) {
	result := reduceCodexLayer0(codexLayer0Request{
		Route:             codexLayer0RouteUnspecified,
		Messages:          messages,
		SessionID:         sessionID,
		RememberedToolUse: rememberedToolUses,
	})
	return result.Messages, result.Stats
}

func reduceCodexLayer0(req codexLayer0Request) codexLayer0Result {
	toolUses := proxyToolUseIndex(req.Messages)
	for id, use := range req.RememberedToolUse {
		if _, ok := toolUses[id]; !ok {
			toolUses[id] = use
		}
	}
	var out []types.Message
	stats := proxyLayer0Stats{Route: req.Route}

	for msgIdx, msg := range req.Messages {
		for blockIdx, block := range msg.Content {
			if block.Type != "tool_result" {
				continue
			}
			stats.ToolResultBlocks++
			use, toolUseResolved := proxyResolveToolUseDetailed(block, toolUses)
			commandLine := proxyLayer0CommandLine(use)
			if commandLine == "" {
				stats.CommandUnresolvedBlocks++
				if !toolUseResolved {
					stats.ToolUseUnresolvedBlocks++
				}
				continue
			}
			stats.CommandResolvedBlocks++
			beforeTokens := tokens.CountString(block.Text)
			readCtx := proxyReadFileContext(req.SessionID, commandLine)
			readDeltaAttempted := readDeltaEligible(req.SessionID, commandLine)
			if readDeltaAttempted {
				stats.ReadDeltaAttempts++
			}
			afterText, changed := compactProxyReadDelta(req.SessionID, commandLine, block.Text, readCtx)
			mechanism := proxyLayer0MechanismReadDelta
			if readDeltaAttempted && !changed {
				stats.ReadDeltaMisses++
			}
			if !changed {
				afterText, changed, mechanism = compactProxyLayer0TextDetailed(commandLine, block.Text, readCtx)
			}
			if !changed {
				continue
			}
			afterTokens := tokens.CountString(afterText)
			if afterTokens < beforeTokens {
				if out == nil {
					out = cloneMessages(req.Messages)
				}
				out[msgIdx].Content[blockIdx].Text = afterText
				stats.TokensSaved += beforeTokens - afterTokens
				stats.BlocksModified++
				switch mechanism {
				case proxyLayer0MechanismReadDelta:
					stats.ReadDeltaBlocks++
				case proxyLayer0MechanismCodexEnvelope:
					stats.CodexExecEnvelopeBlocks++
				default:
					stats.CapturedOutputBlocks++
				}
			}
		}
	}

	if out == nil {
		return codexLayer0Result{Messages: req.Messages, Stats: stats}
	}
	return codexLayer0Result{Messages: out, Stats: stats}
}

func readDeltaEligible(sessionID, commandLine string) bool {
	return strings.TrimSpace(sessionID) != "" && filter.ReadPathFromCommandLine(commandLine) != ""
}

func compactProxyLayer0Text(commandLine, text string, ctx filter.FileReadContext) (string, bool) {
	out, changed, _ := compactProxyLayer0TextDetailed(commandLine, text, ctx)
	return out, changed
}

func compactProxyLayer0TextDetailed(commandLine, text string, ctx filter.FileReadContext) (string, bool, proxyLayer0Mechanism) {
	compacted, changed := filter.CompactCapturedOutputWithContext("", commandLine, text, 0, ctx)
	if changed {
		return string(compacted), true, proxyLayer0MechanismCapturedOut
	}
	out, changed := compactCodexExecEnvelope(commandLine, text, ctx)
	if changed {
		return out, true, proxyLayer0MechanismCodexEnvelope
	}
	return "", false, ""
}

func compactCodexExecEnvelope(commandLine, text string, ctx filter.FileReadContext) (string, bool) {
	header, payload, ok := splitCodexExecEnvelope(text)
	if !ok {
		return "", false
	}
	compacted, changed := filter.CompactCapturedOutputWithContext("", commandLine, payload, 0, ctx)
	if !changed {
		return "", false
	}
	return header + string(compacted), true
}

func compactProxyReadDelta(sessionID, commandLine, text string, ctx filter.FileReadContext) (string, bool) {
	path := filter.ReadPathFromCommandLine(commandLine)
	if path == "" || strings.TrimSpace(sessionID) == "" {
		return "", false
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return "", false
	}
	decision, err := readcache.EvaluateObserved(readcache.DefaultDir(home), readcache.Request{
		SessionID: sessionID,
		FilePath:  path,
	}, text, contentarchive.DefaultDir(home), ctx.RecentlyEdited)
	if err != nil || decision.Type != readcache.DecisionBlock || decision.Reason == "" {
		return "", false
	}
	return decision.Reason, true
}

func proxyReadFileContext(sessionID string, commandLine string) filter.FileReadContext {
	ctx := filter.FileReadContext{Mode: "scan"}
	path := filter.ReadPathFromCommandLine(commandLine)
	if path == "" || strings.TrimSpace(sessionID) == "" {
		return ctx
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return ctx
	}
	recent, err := sessions.RecentlyEditedHookFile(sessions.DefaultHookStateDir(home), sessionID, path, 2)
	if err == nil && recent {
		ctx.RecentlyEdited = true
	}
	return ctx
}

func splitCodexExecEnvelope(text string) (header, payload string, ok bool) {
	if !strings.Contains(text, "Process exited with code ") {
		return "", "", false
	}
	for _, marker := range []string{"\nOutput:\n", "\r\nOutput:\r\n"} {
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		header = text[:idx+len(marker)]
		payload = text[idx+len(marker):]
		return header, payload, payload != ""
	}
	return "", "", false
}

func proxyToolUseIndex(messages []types.Message) map[string]types.ContentBlock {
	index := make(map[string]types.ContentBlock)
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ToolUseID != "" {
				index[block.ToolUseID] = block
			}
		}
	}
	return index
}

func proxyResolveToolUse(block types.ContentBlock, toolUses map[string]types.ContentBlock) types.ContentBlock {
	use, _ := proxyResolveToolUseDetailed(block, toolUses)
	return use
}

func proxyResolveToolUseDetailed(block types.ContentBlock, toolUses map[string]types.ContentBlock) (types.ContentBlock, bool) {
	if len(toolUses) == 0 {
		return block, block.ToolInput != "" || block.ToolName != ""
	}
	id := block.ToolResultID
	if id == "" {
		id = block.ToolUseID
	}
	if id == "" {
		return block, block.ToolInput != "" || block.ToolName != ""
	}
	use, ok := toolUses[id]
	if !ok {
		return block, block.ToolInput != "" || block.ToolName != ""
	}
	return use, true
}

func proxyLayer0CommandLine(block types.ContentBlock) string {
	input := strings.TrimSpace(block.ToolInput)
	if input == "" {
		return ""
	}
	if raw := rawJSONString(json.RawMessage(input)); raw != "" {
		input = raw
		if looksLikeReadTool(block.ToolName) {
			return "cat " + quoteShellArg(input)
		}
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &obj); err == nil {
		for _, key := range []string{"command", "cmd", "command_line", "cmdline", "shell_command"} {
			if s := strings.TrimSpace(rawJSONString(obj[key])); s != "" {
				return normalizeLayer0CommandLine(s)
			}
		}
		for _, key := range []string{"command", "argv", "args"} {
			if argv := proxyStringArray(obj[key]); len(argv) > 0 {
				return normalizeLayer0CommandLine(joinShellArgs(argv))
			}
		}
		if looksLikeReadTool(block.ToolName) {
			for _, key := range []string{"path", "file_path", "filepath", "absolute_path"} {
				if path := strings.TrimSpace(rawJSONString(obj[key])); path != "" {
					return "cat " + quoteShellArg(path)
				}
			}
		}
	}

	if looksLikeShellTool(block.ToolName) {
		return normalizeLayer0CommandLine(input)
	}
	return ""
}

func normalizeLayer0CommandLine(commandLine string) string {
	argv := filter.ArgvForCapturedOutput(commandLine)
	if stripped := stripSlimferenceFilterWrapper(argv); stripped != "" {
		return stripped
	}
	if len(argv) >= 3 && looksLikeShellExecutable(argv[0]) && strings.Contains(argv[1], "c") && strings.HasPrefix(argv[1], "-") {
		return argv[2]
	}
	return commandLine
}

func stripSlimferenceFilterWrapper(argv []string) string {
	if len(argv) < 3 {
		return ""
	}
	if filepath.Base(strings.TrimSpace(argv[0])) != "slimference" || argv[1] != "filter" {
		return ""
	}
	start := 2
	for start < len(argv) {
		switch argv[start] {
		case "--", "--stream":
			start++
		default:
			if strings.HasPrefix(argv[start], "-") {
				return ""
			}
			return joinShellArgs(argv[start:])
		}
	}
	return ""
}

func proxyStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func looksLikeShellTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell", "sh", "exec", "exec_command", "command", "terminal",
		"local_shell", "local_shell_call", "bash_command", "run_command", "terminal.exec",
		"container.exec", "shell_command", "execute", "run", "terminal_command":
		return true
	default:
		return false
	}
}

func looksLikeShellExecutable(name string) bool {
	switch strings.ToLower(filepath.Base(strings.TrimSpace(name))) {
	case "bash", "sh", "zsh":
		return true
	default:
		return false
	}
}

func looksLikeReadTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "cat", "view", "open", "open_file", "read_file", "readfile", "view_file",
		"file_read", "read_path", "view_path", "open_path":
		return true
	default:
		return false
	}
}

func joinShellArgs(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, quoteShellArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteShellArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if strings.IndexFunc(arg, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '\\' ||
			r == '|' || r == '&' || r == ';' || r == '<' || r == '>' || r == '$' ||
			r == '*' || r == '?' || r == '(' || r == ')' || r == '`'
	}) < 0 {
		return arg
	}
	return strconv.Quote(arg)
}

func cloneMessages(messages []types.Message) []types.Message {
	out := make([]types.Message, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].Content = append([]types.ContentBlock(nil), messages[i].Content...)
	}
	return out
}
