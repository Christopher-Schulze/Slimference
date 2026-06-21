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

func TestWSSRequestDebugFactsAddsSearchDependencyTrace(t *testing.T) {
	searchOutput := "internal/proxy/wsmitm_phasef.go:10:func target() {}\ninternal/filter/builtin_search.go:22:target := true\n"
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_rg",
			Text:         "Process exited with code 0\nOutput:\n" + searchOutput,
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_rg": {
			Type:      "tool_use",
			ToolUseID: "call_rg",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"rg -n target internal"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.dependency_trace"] != "true" ||
		facts["wss.read_trace_requests"] != "1" ||
		facts["wss.read_partial_count"] != "1" ||
		facts["wss.read_full_count"] != "" ||
		facts["wss.read_file_path_hash_count"] != "2" ||
		facts["wss.read_file_path_hashes"] == "" ||
		facts["wss.read_range_hash_count"] != "2" ||
		facts["wss.read_range_hashes"] == "" ||
		facts["wss.read_range"] != "" ||
		facts["wss.tool_command_classes"] != "rg_search=1" {
		t.Fatalf("missing search dependency trace facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "internal/proxy/wsmitm_phasef.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, "internal/filter/builtin_search.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, "func target")
	assertWSSReadTraceFactsDoNotLeak(t, facts, searchOutput)
}

func TestWSSRequestDebugFactsAddsSingleSearchRangeTrace(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_rg_single",
			Text:         "src/app.go:7:needle\n",
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_rg_single": {
			Type:      "tool_use",
			ToolUseID: "call_rg_single",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"rg -n needle src"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.dependency_trace"] != "true" ||
		facts["wss.read_trace_requests"] != "1" ||
		facts["wss.read_partial_count"] != "1" ||
		facts["wss.read_range"] != "lines:7:7" ||
		facts["wss.read_file_path_hash"] == "" ||
		facts["wss.read_range_hash"] == "" {
		t.Fatalf("missing single search dependency trace facts: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/app.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, "needle")
}

func TestWSSRequestDebugFactsSkipsFailedSearchDependencyTrace(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_rg_fail",
			Text:         "Process exited with code 2\nOutput:\nrg: unclosed group\n",
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_rg_fail": {
			Type:      "tool_use",
			ToolUseID: "call_rg_fail",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"rg -n '(' src"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.dependency_trace"] != "" ||
		facts["wss.read_trace_requests"] != "" ||
		facts["wss.read_file_path_hash"] != "" ||
		facts["wss.tool_command_classes"] != "rg_search=1" {
		t.Fatalf("failed search should not add dependency trace: %+v", facts)
	}
}

func TestWSSRequestDebugFactsSkipsAmbiguousSearchDependencyTrace(t *testing.T) {
	output := strings.Join([]string{
		"src/app.go:7:needle",
		"warning: something noisy",
		"another unrelated line",
		"third unrelated line",
	}, "\n")
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_rg_ambiguous",
			Text:         output,
		}},
	}}
	meta := wssRequestMeta{ToolUseIndex: map[string]types.ContentBlock{
		"call_rg_ambiguous": {
			Type:      "tool_use",
			ToolUseID: "call_rg_ambiguous",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"rg -n needle src"}`,
		},
	}}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.dependency_trace"] != "" ||
		facts["wss.read_trace_requests"] != "" ||
		facts["wss.read_file_path_hash"] != "" {
		t.Fatalf("ambiguous search should not add dependency trace: %+v", facts)
	}
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
	if facts["wss.file_hash_after"] == "" ||
		facts["wss.edit_turn_seq"] != "1" ||
		facts["wss.changed_range"] != "full" {
		t.Fatalf("exact post-edit state facts missing: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/edit.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, "package main")
}

func TestWSSRequestDebugFactsAddsPatchDerivedPostEditRange(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := sessions.ObserveHookFile(sessions.DefaultHookStateDir(home), "session-read-trace-range", "src/edit.go", "edit"); err != nil {
		t.Fatalf("ObserveHookFile() error = %v", err)
	}
	patchCommand := "apply_patch <<'PATCH'\n*** Begin Patch\n*** Update File: src/edit.go\n@@ -10,2 +10,3 @@\n-old\n+new\n+extra\n*** End Patch\nPATCH"
	messages := []types.Message{
		{
			Role: "assistant",
			Content: []types.ContentBlock{{
				Type:      "tool_use",
				ToolUseID: "call_patch",
				ToolName:  "exec_command",
				ToolInput: `{"cmd":` + strconv.Quote(patchCommand) + `}`,
			}},
		},
		{
			Role: "tool",
			Content: []types.ContentBlock{{
				Type:         "tool_result",
				ToolResultID: "call_read_edit",
				Text:         "line 10\nline 11\nline 12\n",
			}},
		},
	}
	meta := wssRequestMeta{
		SessionID: "session-read-trace-range",
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
	if facts["wss.read_after_edit"] != "true" ||
		facts["wss.file_hash_after"] == "" ||
		facts["wss.edit_turn_seq"] != "1" ||
		facts["wss.changed_range"] != "lines:10:12" {
		t.Fatalf("patch-derived post-edit facts missing: %+v", facts)
	}
	assertWSSReadTraceFactsDoNotLeak(t, facts, "src/edit.go")
	assertWSSReadTraceFactsDoNotLeak(t, facts, "line 10")
}

func TestWSSRequestDebugFactsDoesNotHashFailedPostEditRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := sessions.ObserveHookFile(sessions.DefaultHookStateDir(home), "session-read-trace-fail", "src/missing.go", "edit"); err != nil {
		t.Fatalf("ObserveHookFile() error = %v", err)
	}
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_read_missing",
			Text:         "Process exited with code 1\nOutput:\ncat: src/missing.go: No such file or directory\n",
		}},
	}}
	meta := wssRequestMeta{
		SessionID: "session-read-trace-fail",
		ToolUseIndex: map[string]types.ContentBlock{
			"call_read_missing": {
				Type:      "tool_use",
				ToolUseID: "call_read_missing",
				ToolName:  "exec_command",
				ToolInput: `{"cmd":"cat src/missing.go"}`,
			},
		},
	}

	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", meta, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.read_after_edit"] != "true" {
		t.Fatalf("post-edit surface should still be visible: %+v", facts)
	}
	if facts["wss.file_hash_after"] != "" ||
		facts["wss.edit_turn_seq"] != "" ||
		facts["wss.changed_range"] != "" {
		t.Fatalf("failed read must not claim exact file state: %+v", facts)
	}
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
	for i := range wssReadTraceListLimit + 3 {
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
	for i := range wssPatchTraceListLimit + 2 {
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

func TestWSSReadTracePostEditHelperEdges(t *testing.T) {
	if got := wssReadTraceRecentEdit("", "src/x.go", 2); got.hit {
		t.Fatalf("empty session must not hit recent edit: %+v", got)
	}
	if got := wssReadTraceRecentEdit("session", "", 2); got.hit {
		t.Fatalf("empty path must not hit recent edit: %+v", got)
	}
	if got := wssReadTraceTurnSeqFact("turn-42", -1); got != "42" {
		t.Fatalf("turn seq fact=%q want 42", got)
	}
	if got := wssReadTraceTurnSeqFact("custom/turn", -1); got == "" || strings.Contains(got, "custom") {
		t.Fatalf("custom turn fact must be hashed and non-empty, got %q", got)
	}
	if got := wssReadTraceTurnSeqFact("", 2); got != "3" {
		t.Fatalf("fallback turn seq=%q want 3", got)
	}
	var exact wssReadDependencyTrace
	exact.addExactPostEditRead("hash-a", "1", "full")
	exact.addExactPostEditRead("hash-b", "1", "full")
	if !exact.exactAmbiguous {
		t.Fatalf("different exact-state hashes must mark ambiguity: %+v", exact)
	}
	if payload, ok := wssReadTraceSuccessfulReadPayload("Process exited with code 0\nOutput:\nok\n"); !ok || payload != "ok\n" {
		t.Fatalf("successful envelope payload=%q ok=%v", payload, ok)
	}
	if payload, ok := wssReadTraceSuccessfulReadPayload("raw file\n"); !ok || payload != "raw file\n" {
		t.Fatalf("raw payload=%q ok=%v", payload, ok)
	}
	if payload, ok := wssReadTraceSuccessfulReadPayload("Process exited with code 1\nOutput:\nboom\n"); ok || payload != "" {
		t.Fatalf("failed envelope payload=%q ok=%v", payload, ok)
	}
	if got := wssReadTraceChangedRangeFromPatch("@@ -1 +2 @@\n@@ -5,2 +6,0 @@\n"); got != "lines:2:2,lines:6:6" {
		t.Fatalf("changed range=%q", got)
	}
	if got := wssReadTraceChangedRangeFromPatch("no hunk here"); got != "" {
		t.Fatalf("no-hunk range=%q", got)
	}

	rawPatch := "*** Begin Patch\n*** Update File: src/x.go\n@@ -3 +3 @@\n-old\n+new\n*** End Patch"
	rawTexts := wssReadTracePatchTexts(types.ContentBlock{ToolInput: strconv.Quote(rawPatch)})
	if len(rawTexts) != 1 || rawTexts[0] != rawPatch {
		t.Fatalf("raw patch texts=%v", rawTexts)
	}
	jsonTexts := wssReadTracePatchTexts(types.ContentBlock{ToolInput: `{"cmd":` + strconv.Quote(rawPatch) + `}`})
	if len(jsonTexts) != 1 || jsonTexts[0] != rawPatch {
		t.Fatalf("json patch texts=%v", jsonTexts)
	}
	if texts := wssReadTracePatchTexts(types.ContentBlock{ToolInput: `{"cmd":"echo ok"}`}); len(texts) != 0 {
		t.Fatalf("non-patch command returned patch texts=%v", texts)
	}
	if texts := wssReadTracePatchTexts(types.ContentBlock{ToolInput: rawPatch}); len(texts) != 1 || texts[0] != rawPatch {
		t.Fatalf("invalid-json raw patch texts=%v", texts)
	}
	if texts := wssReadTracePatchTexts(types.ContentBlock{ToolInput: "not json"}); len(texts) != 0 {
		t.Fatalf("invalid-json non-patch texts=%v", texts)
	}
	if texts := wssReadTracePatchTexts(types.ContentBlock{}); len(texts) != 0 {
		t.Fatalf("empty block returned patch texts=%v", texts)
	}

	writeBlock := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "write",
		ToolInput: `{"path":"src/x.go"}`,
	}
	if got, ok := wssReadTraceChangedRangeFromEditBlock(writeBlock, "src/x.go"); !ok || got != "full" {
		t.Fatalf("write edit range=%q ok=%v", got, ok)
	}
	writeMessages := []types.Message{{Content: []types.ContentBlock{writeBlock}}}
	if got := wssReadTraceChangedRangeForPath(writeMessages, nil, "src/x.go"); got != "full" {
		t.Fatalf("write changed range for path=%q", got)
	}
	if got := wssReadTraceChangedRangeForPath(nil, nil, "src/x.go"); got != "full" {
		t.Fatalf("missing edit block should conservatively return full, got %q", got)
	}
	if got := wssReadTraceChangedRangeForPath(nil, nil, ""); got != "" {
		t.Fatalf("empty path changed range=%q", got)
	}
	multiPatch := "apply_patch <<'PATCH'\n*** Begin Patch\n*** Update File: src/x.go\n@@ -1 +1 @@\n-a\n+b\n*** Update File: src/y.go\n@@ -2 +2 @@\n-c\n+d\n*** End Patch\nPATCH"
	multiBlock := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "exec_command",
		ToolInput: `{"cmd":` + strconv.Quote(multiPatch) + `}`,
	}
	if got, ok := wssReadTraceChangedRangeFromEditBlock(multiBlock, "src/x.go"); !ok || got != "full" {
		t.Fatalf("multi-file edit range=%q ok=%v", got, ok)
	}
	if got, ok := wssReadTraceChangedRangeFromEditBlock(writeBlock, "src/missing.go"); ok || got != "" {
		t.Fatalf("unmatched path range=%q ok=%v", got, ok)
	}
	if wssReadTracePathListContains([]string{"./src/../src/x.go"}, "src/x.go") != true {
		t.Fatal("normalized path list should match")
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
