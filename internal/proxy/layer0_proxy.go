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
	proxyLayer0MechanismRepeatedOut   proxyLayer0Mechanism = "repeated_tool_output"
)

type codexLayer0Route string

const (
	codexLayer0RouteUnspecified codexLayer0Route = ""
	codexLayer0RouteHTTP        codexLayer0Route = "http"
	codexLayer0RouteWSSPhaseF   codexLayer0Route = "wss_phasef"
)

type codexLayer0Request struct {
	Route               codexLayer0Route
	Messages            []types.Message
	SessionID           string
	TurnID              string
	RememberedToolUse   map[string]types.ContentBlock
	SuppressedToolKey   map[string]struct{}
	RecentFullPassTurns int
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
	RepeatedOutputBlocks    int
	ReadDeltaKeys           []string
}

func (s proxyLayer0Stats) withoutSavings() proxyLayer0Stats {
	s.TokensSaved = 0
	s.BlocksModified = 0
	s.ReadDeltaBlocks = 0
	s.CapturedOutputBlocks = 0
	s.CodexExecEnvelopeBlocks = 0
	s.RepeatedOutputBlocks = 0
	s.ReadDeltaKeys = nil
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
	// Codex (GPT-4o / GPT-5-codex) bills in o200k_base; count the savings guard
	// with the matching encoding so before/after token math reflects real cost.
	tok := tokens.ForProvider(types.CodexChatGPT)

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
			toolKey := proxyLayer0QualityToolKey(commandLine)
			if _, suppressed := req.SuppressedToolKey[toolKey]; suppressed && toolKey != "" {
				continue
			}
			beforeTokens := tok.CountString(block.Text)
			readCtx := proxyReadFileContext(req.SessionID, commandLine)
			readDeltaAttempted := readDeltaEligible(req.SessionID, commandLine)
			if readDeltaAttempted {
				stats.ReadDeltaAttempts++
			}
			readCommand := readRequestFromCommandLine(commandLine).FilePath != ""
			afterText, changed := compactProxyReadDelta(req.SessionID, req.TurnID, commandLine, block.Text, readCtx, req.RecentFullPassTurns)
			mechanism := proxyLayer0MechanismReadDelta
			if readDeltaAttempted && !changed {
				stats.ReadDeltaMisses++
			}
			if readCommand && !changed {
				continue
			}
			if !changed {
				afterText, changed, mechanism = compactProxyLayer0TextDetailed(commandLine, block.Text, readCtx)
			}
			candidateText := block.Text
			candidateEligible := true
			if changed {
				candidateText = afterText
				candidateEligible = tok.CountString(candidateText) < beforeTokens
			}
			if !readCommand && candidateEligible {
				if repeatedText, repeated := compactProxyRepeatedToolOutput(req.SessionID, commandLine, candidateText); repeated {
					afterText = repeatedText
					changed = true
					mechanism = proxyLayer0MechanismRepeatedOut
				}
			}
			if !changed {
				continue
			}
			afterTokens := tok.CountString(afterText)
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
					if toolKey != "" {
						stats.ReadDeltaKeys = append(stats.ReadDeltaKeys, toolKey)
					}
				case proxyLayer0MechanismCodexEnvelope:
					stats.CodexExecEnvelopeBlocks++
				case proxyLayer0MechanismRepeatedOut:
					stats.RepeatedOutputBlocks++
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

func proxyEditedPathsFromMessages(messages []types.Message, rememberedToolUses map[string]types.ContentBlock) []string {
	toolUses := proxyToolUseIndex(messages)
	for id, use := range rememberedToolUses {
		if _, ok := toolUses[id]; !ok {
			toolUses[id] = use
		}
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				for _, path := range proxyLayer0EditPaths(block) {
					add(path)
				}
				continue
			}
			if block.Type != "tool_result" {
				continue
			}
			use, _ := proxyResolveToolUseDetailed(block, toolUses)
			for _, path := range proxyLayer0EditPaths(use) {
				add(path)
			}
		}
	}
	return out
}

func proxyLayer0EditPaths(block types.ContentBlock) []string {
	input := strings.TrimSpace(block.ToolInput)
	if input == "" {
		return nil
	}
	rawUnwrapped := false
	if raw := rawJSONString(json.RawMessage(input)); raw != "" {
		input = raw
		rawUnwrapped = true
	}
	if looksLikeEditTool(block.ToolName) {
		if rawUnwrapped {
			if strings.Contains(input, "*** ") || strings.Contains(input, "+++ ") || strings.Contains(input, "--- ") {
				return proxyPatchPathsFromText(input, "")
			}
			return []string{input}
		}
	}
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path != "" {
			out = append(out, path)
		}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &obj); err != nil {
		if looksLikeEditTool(block.ToolName) {
			add(input)
		}
		if looksLikeShellTool(block.ToolName) && strings.Contains(input, "apply_patch") {
			out = append(out, proxyPatchPathsFromText(input, "")...)
		}
		return compactStringSet(out)
	}
	workdir := proxyToolWorkdir(obj)
	if looksLikeEditTool(block.ToolName) {
		for _, key := range []string{"path", "file_path", "filepath", "absolute_path", "target", "source_path"} {
			if path := strings.TrimSpace(rawJSONString(obj[key])); path != "" {
				add(proxyPathWithWorkdir(path, workdir))
			}
		}
	}
	for _, key := range []string{"patch", "diff", "changes"} {
		if patch := strings.TrimSpace(rawJSONString(obj[key])); patch != "" {
			for _, path := range proxyPatchPathsFromText(patch, workdir) {
				add(path)
			}
		}
	}
	for _, key := range []string{"command", "cmd", "command_line", "cmdline", "commandLine", "shell_command", "shellCommand"} {
		if command := strings.TrimSpace(rawJSONString(obj[key])); strings.Contains(command, "apply_patch") {
			for _, path := range proxyPatchPathsFromText(command, workdir) {
				add(path)
			}
		}
	}
	return compactStringSet(out)
}

func looksLikeEditTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "edit", "write", "apply_patch", "multiedit", "multi_edit", "update_file",
		"write_file", "replace_file", "file.write", "fs.write", "mcp.write_file":
		return true
	default:
		return false
	}
}

func proxyPatchPathsFromText(patch, workdir string) []string {
	var out []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if i := strings.IndexByte(path, '\t'); i >= 0 {
			path = strings.TrimSpace(path[:i])
		}
		path = strings.TrimPrefix(path, "a/")
		path = strings.TrimPrefix(path, "b/")
		if path == "" || path == "/dev/null" {
			return
		}
		out = append(out, proxyPathWithWorkdir(path, workdir))
	}
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: ", "+++ ", "--- "} {
			if strings.HasPrefix(line, prefix) {
				add(strings.TrimPrefix(line, prefix))
			}
		}
	}
	return compactStringSet(out)
}

func compactStringSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func readDeltaEligible(sessionID, commandLine string) bool {
	return strings.TrimSpace(sessionID) != "" && readRequestFromCommandLine(commandLine).FilePath != ""
}

func proxyLayer0QualityToolKey(commandLine string) string {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return ""
	}
	if req := readRequestFromCommandLine(commandLine); req.FilePath != "" {
		key := "read:" + filepath.Clean(req.FilePath)
		if req.Offset != 0 || req.Limit != 0 {
			key += ":range:" + strconv.Itoa(req.Offset) + ":" + strconv.Itoa(req.Limit)
		}
		return key
	}
	if key := filter.SearchOutputKeyFromCommandLine(commandLine); key != "" {
		return "search:" + key
	}
	return "command:" + commandLine
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

func compactProxyReadDelta(sessionID, turnID, commandLine, text string, ctx filter.FileReadContext, recentFullPassTurns int) (string, bool) {
	req := readRequestFromCommandLine(commandLine)
	if req.FilePath == "" || strings.TrimSpace(sessionID) == "" {
		return "", false
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return "", false
	}
	decision, err := readcache.EvaluateObserved(readcache.DefaultDir(home), readcache.Request{
		SessionID:               sessionID,
		TurnID:                  turnID,
		FilePath:                req.FilePath,
		Offset:                  req.Offset,
		Limit:                   req.Limit,
		RecentFullPassTurnLimit: recentFullPassTurns,
	}, text, contentarchive.DefaultDir(home), ctx.RecentlyEdited)
	if err != nil || decision.Type != readcache.DecisionBlock || decision.Reason == "" {
		return "", false
	}
	return decision.Reason, true
}

func readRequestFromCommandLine(commandLine string) readcache.Request {
	req, ok := filter.ReadRequestFromCommandLine(commandLine)
	if !ok {
		return readcache.Request{}
	}
	return readcache.Request{FilePath: req.Path, Offset: req.Offset, Limit: req.Limit}
}

func compactProxyRepeatedToolOutput(sessionID, commandLine, text string) (string, bool) {
	key := proxyLayer0QualityToolKey(commandLine)
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(key) == "" {
		return "", false
	}
	home, err := proxyUserHomeDir()
	if err != nil {
		return "", false
	}
	decision, err := readcache.EvaluateObservedOutput(readcache.DefaultDir(home), readcache.OutputRequest{
		SessionID:   sessionID,
		Key:         key,
		CommandLine: commandLine,
	}, text, contentarchive.DefaultDir(home))
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
		workdir := proxyToolWorkdir(obj)
		for _, key := range []string{"command", "cmd", "command_line", "cmdline", "commandLine", "shell_command", "shellCommand"} {
			if s := strings.TrimSpace(rawJSONString(obj[key])); s != "" {
				return applyWorkdirToReadCommand(normalizeLayer0CommandLine(s), workdir)
			}
		}
		for _, key := range []string{"command", "argv", "args", "cmd_args", "command_args"} {
			if argv := proxyStringArray(obj[key]); len(argv) > 0 {
				return applyWorkdirToReadCommand(normalizeLayer0CommandLine(joinShellArgs(argv)), workdir)
			}
		}
		if looksLikeReadTool(block.ToolName) {
			for _, key := range []string{"path", "file_path", "filepath", "absolute_path", "uri", "target", "source_path"} {
				if path := strings.TrimSpace(rawJSONString(obj[key])); path != "" {
					return "cat " + quoteShellArg(proxyPathWithWorkdir(path, workdir))
				}
			}
		}
	}

	if looksLikeShellTool(block.ToolName) {
		return normalizeLayer0CommandLine(input)
	}
	return ""
}

func proxyToolWorkdir(obj map[string]json.RawMessage) string {
	for _, key := range []string{"workdir", "cwd", "working_directory", "workingDirectory", "workingDir", "current_working_directory", "directory"} {
		if dir := strings.TrimSpace(rawJSONString(obj[key])); dir != "" && filepath.IsAbs(dir) {
			return filepath.Clean(dir)
		}
	}
	return ""
}

func applyWorkdirToReadCommand(commandLine, workdir string) string {
	workdir = proxyCleanAbsWorkdir(workdir)
	if workdir == "" {
		return commandLine
	}
	path := filter.ReadPathFromCommandLine(commandLine)
	if path == "" || filepath.IsAbs(path) {
		return commandLine
	}
	argv := filter.ArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		return commandLine
	}
	out := append([]string(nil), argv...)
	for i := len(out) - 1; i >= 1; i-- {
		if out[i] == path {
			out[i] = filepath.Clean(filepath.Join(workdir, path))
			return joinShellArgs(out)
		}
	}
	return commandLine
}

func proxyPathWithWorkdir(path, workdir string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	workdir = proxyCleanAbsWorkdir(workdir)
	if workdir == "" {
		return path
	}
	return filepath.Clean(filepath.Join(workdir, path))
}

func proxyCleanAbsWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" || !filepath.IsAbs(workdir) {
		return ""
	}
	return filepath.Clean(workdir)
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
		"file_read", "read_path", "view_path", "open_path", "file.read", "fs.read",
		"read_file_tool", "local_file_read", "mcp.read_file":
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
