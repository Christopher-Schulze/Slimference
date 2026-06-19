package proxy

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestWSSRequestDebugFactsAddsContentFreeReadDependencyTrace(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_read",
			Text:         "package main\nfunc main() {}\n",
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_read": {
			Type:      "tool_use",
			ToolUseID: "call_read",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"cat src/app.go"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.dependency_trace"] != "true" ||
		facts["wss.read_trace_requests"] != "1" ||
		facts["wss.read_full_count"] != "1" ||
		facts["wss.read_file_path_hash"] == "" ||
		facts["wss.read_range"] != "full" ||
		facts["wss.read_range_hash"] == "" ||
		facts["wss.tool_command_classes"] != "read_like=1" {
		t.Fatalf("missing read trace facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/app.go")
}

func TestWSSRequestDebugFactsAddsRangeReadDependencyTrace(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_head",
			Text:         "line 1\nline 2\n",
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_head": {
			Type:      "tool_use",
			ToolUseID: "call_head",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"head -n 20 src/app.go"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.dependency_trace"] != "true" ||
		facts["wss.read_partial_count"] != "1" ||
		facts["wss.read_full_count"] != "" ||
		facts["wss.read_range"] != "lines:1:20" ||
		facts["wss.read_file_path_hash"] == "" {
		t.Fatalf("missing range read trace facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/app.go")
}

func TestWSSRequestDebugFactsMarksRecentlyEditedReadWithoutPathLeak(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := sessions.ObserveHookFile(sessions.DefaultHookStateDir(home), "session-read-trace", "src/edit.go", "edit"); err != nil {
		t.Fatalf("ObserveHookFile() error = %v", err)
	}
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_read_edit",
			Text:         "package main\n",
		}},
	}}
	meta := wssRequestMeta{
		SessionID: "session-read-trace",
		ToolUseIndex: map[string]types.ContentBlock{
			"call_read_edit": {
				Type:      "tool_use",
				ToolUseID: "call_read_edit",
				ToolName:  "exec_command",
				ToolInput: `{"cmd":"cat src/edit.go"}`,
			},
		},
	}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.read_after_edit"] != "true" || facts["wss.read_after_edit_count"] != "1" {
		t.Fatalf("recent edit trace missing: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/edit.go")
}

func TestWSSRequestDebugFactsAddsMultipleReadTraceLists(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{
			{
				Type:         "tool_result",
				ToolResultID: "call_full",
				Text:         "package alpha\n",
			},
			{
				Type:         "tool_result",
				ToolResultID: "call_range",
				Text:         "line 1\nline 2\n",
			},
		},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_full": {
			Type:      "tool_use",
			ToolUseID: "call_full",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"cat src/a.go"}`,
		},
		"call_range": {
			Type:      "tool_use",
			ToolUseID: "call_range",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"sed -n '10,20p' src/b.go"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.read_trace_requests"] != "2" ||
		facts["wss.read_full_count"] != "1" ||
		facts["wss.read_partial_count"] != "1" ||
		facts["wss.read_file_path_hash_count"] != "2" ||
		facts["wss.read_file_path_hashes"] == "" ||
		facts["wss.read_range_hash_count"] != "2" ||
		facts["wss.read_range_hashes"] == "" ||
		facts["wss.read_range"] != "" {
		t.Fatalf("missing multi-read trace facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/a.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/b.go")
}

func TestWSSReadDependencyDebugFactsNoopWithoutReadableCommand(t *testing.T) {
	attachWSSReadDependencyDebugFacts(nil, nil, wssRequestMeta{})
	facts := map[string]string{"existing": "1"}
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type: "tool_result",
			Text: "ordinary output without command envelope",
		}},
	}}

	attachWSSReadDependencyDebugFacts(facts, messages, wssRequestMeta{})
	if len(facts) != 1 || facts["existing"] != "1" {
		t.Fatalf("unreadable command should not add read facts: %+v", facts)
	}
}

func TestWSSReadTraceCommandLineUsesBlockCommandFallback(t *testing.T) {
	block := types.ContentBlock{
		Type:      "tool_result",
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"cat src/fallback.go"}`,
		Text:      "package fallback\n",
	}

	commandLine := wssReadTraceCommandLine(block, nil)
	if commandLine != "cat src/fallback.go" {
		t.Fatalf("fallback command line = %q", commandLine)
	}
}

func TestWSSReadTraceCommandLineFallsBackWhenResolvedUseHasNoCommand(t *testing.T) {
	block := types.ContentBlock{
		Type:         "tool_result",
		ToolResultID: "call_no_command",
		ToolName:     "exec_command",
		ToolInput:    `{"cmd":"cat src/block.go"}`,
		Text:         "package block\n",
	}
	toolUses := map[string]types.ContentBlock{
		"call_no_command": {
			Type:      "tool_use",
			ToolUseID: "call_no_command",
			ToolName:  "exec_command",
		},
	}

	commandLine := wssReadTraceCommandLine(block, toolUses)
	if commandLine != "cat src/block.go" {
		t.Fatalf("fallback after empty resolved use = %q", commandLine)
	}
}

func TestWSSReadTraceHelpersNormalizeAndBoundHashLists(t *testing.T) {
	if got := wssReadTraceNormalizePath(""); got != "" {
		t.Fatalf("empty path normalized to %q", got)
	}
	if got := wssReadTraceNormalizePath("src/../src/app.go"); got != "src/app.go" {
		t.Fatalf("path normalized to %q", got)
	}

	facts := map[string]string{}
	attachWSSReadTraceHashFacts(facts, "single", "list", "count", nil)
	if len(facts) != 0 {
		t.Fatalf("nil hashes should not add facts: %+v", facts)
	}
	hashes := map[string]struct{}{}
	for i := 0; i < wssReadTraceListLimit+3; i++ {
		hashes[wssReadTraceHash("value:"+string(rune('a'+i)))] = struct{}{}
	}

	attachWSSReadTraceHashFacts(facts, "single", "list", "count", hashes)
	if facts["count"] != "19" || facts["single"] != "" {
		t.Fatalf("bad bounded hash facts: %+v", facts)
	}
	if got := strings.Count(facts["list"], ",") + 1; got != wssReadTraceListLimit {
		t.Fatalf("bounded hash list length=%d want %d in %q", got, wssReadTraceListLimit, facts["list"])
	}
	if sortedWSSReadTraceHashes(nil) != nil {
		t.Fatal("nil hash map should sort to nil")
	}
}

func TestWSSRequestDebugFactsAddsContentFreePatchContextTrace(t *testing.T) {
	diffOutput := "diff --git a/src/app.go b/src/app.go\n@@ -1 +1 @@\n-old\n+new\n"
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_diff",
			Text:         diffOutput,
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_diff": {
			Type:      "tool_use",
			ToolUseID: "call_diff",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git diff --stat"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.patch_context_candidate"] != "true" ||
		facts["wss.patch_context_requests"] != "1" ||
		facts["wss.patch_context_kind"] != "git_diff_stat" ||
		facts["wss.patch_context_hash"] == "" ||
		facts["wss.patch_context_hash_count"] != "1" ||
		facts["wss.patch_context_hashes"] == "" ||
		facts["wss.patch_context_bytes"] != strconv.Itoa(len(diffOutput)) ||
		facts["wss.tool_command_classes"] != "git_diff_stat=1" {
		t.Fatalf("missing patch-context facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/app.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, diffOutput)
}

func TestWSSRequestDebugFactsMarksPatchContextRiskWithoutPayloadLeak(t *testing.T) {
	diffOutput := "diff --git a/src/app.go b/src/app.go\n<<<<<<< HEAD\nold\n=======\nnew\n>>>>>>> branch\n"
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_diff",
			Text:         diffOutput,
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_diff": {
			Type:      "tool_use",
			ToolUseID: "call_diff",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git diff"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.patch_context_candidate"] != "true" ||
		facts["wss.patch_context_kind"] != "git_diff" ||
		facts["wss.patch_context_conflict"] != "true" {
		t.Fatalf("missing patch conflict facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/app.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, diffOutput)
}

func TestWSSRequestDebugFactsAddsMultiplePatchContextTraceLists(t *testing.T) {
	diffOutput := "diff --git a/src/app.go b/src/app.go\nrename from src/old.go\nrename to src/app.go\nBinary files differ\nfatal: failed\n"
	showOutput := "commit abc123\nerror: patch failed\nchange.rej\n"
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{
			{
				Type:         "tool_result",
				ToolResultID: "call_diff",
				Text:         diffOutput,
			},
			{
				Type:         "tool_result",
				ToolResultID: "call_show",
				Text:         showOutput,
			},
		},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_diff": {
			Type:      "tool_use",
			ToolUseID: "call_diff",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git diff"}`,
		},
		"call_show": {
			Type:      "tool_use",
			ToolUseID: "call_show",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git show --stat"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.patch_context_candidate"] != "true" ||
		facts["wss.patch_context_requests"] != "2" ||
		facts["wss.patch_context_hash"] != "" ||
		facts["wss.patch_context_hash_count"] != "2" ||
		facts["wss.patch_context_hashes"] == "" ||
		facts["wss.patch_context_kind"] != "" ||
		!strings.Contains(facts["wss.patch_context_kinds"], "git_diff=1") ||
		!strings.Contains(facts["wss.patch_context_kinds"], "git_show_stat=1") ||
		facts["wss.patch_context_failed"] != "true" ||
		facts["wss.patch_context_rejected"] != "true" ||
		facts["wss.patch_context_binary"] != "true" ||
		facts["wss.patch_context_rename"] != "true" {
		t.Fatalf("missing multi patch-context facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/app.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, diffOutput)
	assertWSSReadTraceFactsDoNotLeak(t, facts, showOutput)
}

func TestWSSPatchContextDebugFactsNoopWithoutPatchCommand(t *testing.T) {
	attachWSSPatchContextDebugFacts(nil, nil, wssRequestMeta{})
	facts := map[string]string{"existing": "1"}
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_status",
			Text:         "## main\n",
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_status": {
			Type:      "tool_use",
			ToolUseID: "call_status",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git status --short"}`,
		},
	}}

	attachWSSPatchContextDebugFacts(facts, messages, meta)
	if len(facts) != 1 || facts["existing"] != "1" {
		t.Fatalf("non-patch command should not add patch facts: %+v", facts)
	}
}

func TestWSSPatchTraceHelpersClassifyAndBoundHashLists(t *testing.T) {
	for name, tc := range map[string]struct {
		command string
		want    string
	}{
		"diff":      {command: "git diff", want: "git_diff"},
		"diff_stat": {command: "git diff --stat", want: "git_diff_stat"},
		"show":      {command: "git show HEAD", want: "git_show"},
		"show_stat": {command: "git show --stat HEAD", want: "git_show_stat"},
		"status":    {command: "git status --short", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := wssPatchContextCommandKind(tc.command); got != tc.want {
				t.Fatalf("wssPatchContextCommandKind(%q) = %q want %q", tc.command, got, tc.want)
			}
		})
	}

	wssPatchTraceRiskSignals("fatal: failed\n<<<<<<< HEAD\nrejected\nBinary files differ\nrename from a\n", nil)
	facts := map[string]string{}
	attachWSSPatchTraceHashFacts(facts, "list", "count", nil)
	if len(facts) != 0 {
		t.Fatalf("nil patch hashes should not add facts: %+v", facts)
	}
	hashes := map[string]struct{}{}
	for i := 0; i < wssPatchTraceListLimit+2; i++ {
		hashes[wssPatchTraceHash("patch:"+string(rune('a'+i)))] = struct{}{}
	}
	attachWSSPatchTraceHashFacts(facts, "list", "count", hashes)
	if facts["count"] != "18" {
		t.Fatalf("bad patch hash count: %+v", facts)
	}
	if got := strings.Count(facts["list"], ",") + 1; got != wssPatchTraceListLimit {
		t.Fatalf("bounded patch hash list length=%d want %d in %q", got, wssPatchTraceListLimit, facts["list"])
	}
}

func assertWSSReadTraceFactsDoNotLeak(t *testing.T, facts map[string]string, rawPath string) {
	t.Helper()
	for key, value := range facts {
		if strings.Contains(value, rawPath) {
			t.Fatalf("debug fact %s leaked raw path %q in %q", key, rawPath, value)
		}
	}
}
