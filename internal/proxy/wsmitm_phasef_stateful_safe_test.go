package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestWSSStatefulToolOutputMutationSafeAdditionalEvidenceClasses(t *testing.T) {
	meta := wssRequestMeta{SessionID: "codex-wss:stateful-safe", PreviousResponseID: "resp-stateful-safe"}
	diffStat := wssDiffStatFixture(36)
	var wcOutput strings.Builder
	wcArgs := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("internal/proxy/generated/very/deep/path/file_%02d.go", i)
		wcArgs = append(wcArgs, path)
		wcOutput.WriteString(fmt.Sprintf("      %d %s\n", i+300, path))
	}
	wcOutput.WriteString("     6190 total\n")

	tests := []struct {
		name      string
		command   string
		output    string
		wantSafe  bool
		wantGuard string
	}{
		{name: "git diff stat", command: "git diff --stat", output: diffStat, wantSafe: true},
		{name: "git status short pathspec", command: "git status --short .", output: " M internal/proxy/wsmitm_phasef.go\n", wantSafe: true},
		{name: "git log oneline bounded", command: "git log --oneline -n 3", output: "a1b2c3d Tighten guard\nb2c3d4e Recover savings\nc3d4e5f Add proof\n", wantSafe: true},
		{name: "wc line counts", command: "wc -l " + strings.Join(wcArgs, " "), output: wcOutput.String(), wantSafe: true},
		{name: "git status rich output", command: "git status", output: "On branch main\nChanges not staged for commit:\n\tmodified: internal/proxy/wsmitm_phasef.go\n", wantGuard: "rich git status output stays guarded"},
		{name: "git log oneline unbounded", command: "git log --oneline", output: "a1b2c3d Tighten guard\n", wantGuard: "unbounded log output stays guarded"},
		{name: "git log rich output", command: "git log --stat -n 3", output: "commit a1b2c3d4\n\n    Tighten guard\n\n file.go | 2 ++\n", wantGuard: "rich log output stays guarded"},
		{name: "full git diff source", command: "git diff", output: "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-func old() {}\n+func new() {}\n", wantGuard: "full git diff must stay guarded"},
		{name: "listing evidence", command: "ls internal/proxy", output: "layer0_proxy.go\nwsmitm_phasef.go\n", wantGuard: "non-empty listings remain model evidence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := []types.Message{{
				Role: "tool",
				Content: []types.ContentBlock{{
					Type:         "tool_result",
					ToolResultID: "call-safe",
					Text:         tt.output,
				}},
			}}
			remembered := map[string]types.ContentBlock{
				"call-safe": {
					Type:      "tool_use",
					ToolUseID: "call-safe",
					ToolName:  "exec_command",
					ToolInput: fmt.Sprintf(`{"cmd":%q}`, tt.command),
				},
			}
			got := wssStatefulToolOutputMutationSafe(meta, true, messages, remembered)
			if got != tt.wantSafe {
				t.Fatalf("stateful safety=%v want %v (%s)", got, tt.wantSafe, tt.wantGuard)
			}
		})
	}
}

func TestWSSGitStatusPathspecBoundary(t *testing.T) {
	command := "git status --short ."
	output := " M internal/proxy/wsmitm_phasef.go\n"
	if !wssSafeGitStatusCommand(command) {
		t.Fatal("git status pathspec command should enter the strict output parser")
	}
	if _, ok := filter.TryCompactGitStatus(filter.ArgvForCapturedOutput(command), []byte(output)); !ok {
		t.Fatal("git status pathspec porcelain output should compact")
	}
	if looksLikeSource(output) || proxyToolResultLooksLikeSearchOutput(output) {
		t.Fatal("git status porcelain path line must not be classified as source or search output")
	}
	toolUse := types.ContentBlock{
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"git status --short ."}`,
	}
	if got := proxyLayer0CommandLine(toolUse); got != command {
		t.Fatalf("tool command line = %q, want %q", got, command)
	}
	if !wssSafeStatefulStatusToolOutput(toolUse, output) {
		t.Fatal("git status pathspec should be stateful-safe after parser validation")
	}
}

func TestWSPhaseFPreviousResponseFullHistoryDiffStatCompacts(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-diffstat-safe",
			"prompt_cache_key":     "stateful-diffstat-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the diff stat"},
				{"type": "function_call", "call_id": "call_diffstat", "name": "exec_command", "arguments": map[string]any{"cmd": "git diff --stat"}},
				{"type": "function_call_output", "call_id": "call_diffstat", "output": wssDiffStatFixture(80)},
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle diffstat request: %v", err)
	}
	if !replace {
		t.Fatalf("previous_response full-history diffstat should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git diff --stat] 80 file(s)") ||
		!strings.Contains(body, "[prefix=internal/proxy/generated/very/deep/path/]") ||
		strings.Contains(body, "internal/proxy/generated/very/deep/path/file_xxxxxxxxxxxx_79.go") {
		t.Fatalf("diffstat compaction did not preserve compact evidence: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe diffstat should save without structured guard: %+v", summary)
	}
}

func wssDiffStatFixture(files int) string {
	var out strings.Builder
	for i := 0; i < files; i++ {
		out.WriteString(" internal/proxy/generated/very/deep/path/file_")
		out.WriteString(strings.Repeat("x", 12))
		out.WriteString(fmt.Sprintf("_%02d.go | %d +++++-----\n", i, i+1))
	}
	out.WriteString(fmt.Sprintf(" %d files changed, %d insertions(+), %d deletions(-)\n", files, files*12, files*6))
	return out.String()
}
