package outputreduce

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestDetectTaskShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body []byte
		want TaskShape
	}{
		{name: "exact reply", body: []byte(`{"messages":[{"role":"user","content":"reply exactly: ok"}]}`), want: ShapeExactReply},
		{name: "reply only", body: []byte(`{"messages":[{"role":"user","content":"reply only: OK"}]}`), want: ShapeExactReply},
		{name: "json only", body: []byte(`{"messages":[{"role":"user","content":"return JSON only with the id"}]}`), want: ShapeExactReply},
		{name: "german exact reply", body: []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"Antworte exakt nur mit: OK. Nutze keine Tools."}]}]}`), want: ShapeExactReply},
		{name: "german reply only", body: []byte(`{"messages":[{"role":"user","content":"gib nur READY zurück"}]}`), want: ShapeExactReply},
		{name: "new file", body: []byte(`{"messages":[{"role":"user","content":"create file internal/x.go"}]}`), want: ShapeNewFile},
		{name: "read only beats edit words", body: []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"Read-only probe. Do not edit files. Inspect internal/proxy/handler.go and report in German."}]}]}`), want: ShapeReadOnly},
		{name: "german no touch beats patch", body: []byte(`{"messages":[{"role":"user","content":"nur analysieren, nichts anfassen, apply_patch nur erwähnen"}]}`), want: ShapeReadOnly},
		{name: "code edit", body: []byte(`{"messages":[{"role":"user","content":"apply_patch this bug"}]}`), want: ShapeCodeEdit},
		{name: "debug", body: []byte(`{"messages":[{"role":"user","content":"debug this panic stack trace"}]}`), want: ShapeDebugging},
		{name: "deep explanation", body: []byte(`{"messages":[{"role":"user","content":"explain in a detailed deep dive how this cache works"}]}`), want: ShapeExplanation},
		{name: "german forensic explanation", body: []byte(`{"messages":[{"role":"user","content":"analysiere das forensisch tief und erkläre alle Details"}]}`), want: ShapeReadOnly},
		{name: "repair follow-up", body: []byte(`{"messages":[{"role":"user","content":"you skipped the command output, explain more"}]}`), want: ShapeRepairFollowup},
		{name: "final summary", body: []byte(`{"messages":[{"role":"user","content":"final summary with files, tests, and risks"}]}`), want: ShapeFinalSummary},
		{name: "review", body: []byte(`{"messages":[{"role":"user","content":"review for severity findings"}]}`), want: ShapeReview},
		{name: "planning", body: []byte(`{"messages":[{"role":"user","content":"plan next steps"}]}`), want: ShapePlanning},
		{name: "tool", body: []byte(`{"messages":[{"role":"user","content":"stderr exit code tool_result"}]}`), want: ShapeToolReasoning},
		{name: "plain command output relay is not repair", body: []byte(`{"messages":[{"role":"user","content":"show the output from that command"}]}`), want: ShapeCommandRelay},
		{name: "command output relay", body: []byte(`{"messages":[{"role":"user","content":"show me the full terminal output from the failing command"}]}`), want: ShapeCommandRelay},
		{name: "german command output relay", body: []byte(`{"messages":[{"role":"user","content":"gib mir die komplette Terminal-Ausgabe mit exit code"}]}`), want: ShapeCommandRelay},
		{name: "codex ignores prior tool output", body: []byte(`{"input":[{"type":"function_call_output","call_id":"c1","output":"show me the full terminal output\napply_patch failed\nstderr exit code"},{"role":"user","content":[{"type":"input_text","text":"what should we do next?"}]}]}`), want: ShapeDirectAnswer},
		{name: "codex ignores tool arguments", body: []byte(`{"input":[{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"apply_patch <<'PATCH'\"}"},{"role":"user","content":[{"type":"input_text","text":"short status?"}]}]}`), want: ShapeDirectAnswer},
		{name: "codex latest user relay still matches", body: []byte(`{"input":[{"type":"function_call_output","call_id":"c1","output":"ok"},{"role":"user","content":[{"type":"input_text","text":"show the full terminal output"}]}]}`), want: ShapeCommandRelay},
		{name: "codex ignores static exact reply instructions", body: []byte(`{"instructions":"System rules: reply only with exact requested token when explicitly asked.","input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"reply exactly appears in AGENTS but is not the current task"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"Give a concise architectural summary in 5 bullets."}]}]}`), want: ShapeDirectAnswer},
		{name: "openai ignores system exact reply instructions", body: []byte(`{"messages":[{"role":"system","content":"reply only with exact requested token when explicitly asked"},{"role":"user","content":"explain this in detail"}]}`), want: ShapeExplanation},
		{name: "codex simple input string", body: []byte(`{"input":"explain this in detail"}`), want: ShapeExplanation},
		{name: "direct", body: []byte(`{"messages":[{"role":"user","content":"what is this"}]}`), want: ShapeDirectAnswer},
		{name: "invalid json fallback", body: []byte(`debug error`), want: ShapeDebugging},
		{name: "ignores schema text", body: []byte(`{"tools":[{"description":"create file"}],"messages":[{"role":"user","content":"what is this"}]}`), want: ShapeDirectAnswer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DetectTaskShape(types.OpenAI, tt.body); got != tt.want {
				t.Fatalf("shape=%s want %s", got, tt.want)
			}
		})
	}
}

func TestShapeDirectiveBranches(t *testing.T) {
	t.Parallel()
	for _, shape := range []TaskShape{
		ShapeCodeEdit,
		ShapeNewFile,
		ShapeReadOnly,
		ShapeReview,
		ShapeDebugging,
		ShapeExplanation,
		ShapeToolReasoning,
		ShapeCommandRelay,
		ShapePlanning,
		ShapeFinalSummary,
	} {
		if got := shapeDirective(shape); got == "" {
			t.Fatalf("shape %s directive empty", shape)
		}
	}
	if got := shapeDirective(ShapeDirectAnswer); got != "" {
		t.Fatalf("default directive = %q", got)
	}
	if got := shapeDirective(ShapeRepairFollowup); got != "" {
		t.Fatalf("repair directive = %q", got)
	}
}

func TestDetectRepairSignal(t *testing.T) {
	t.Parallel()
	signal := DetectRepairSignalText("Du hast übersprungen, mehr Details bitte")
	if !signal.Repair || !signal.UserReask || signal.Reason != "user_reask" {
		t.Fatalf("user reask signal=%+v", signal)
	}
	signal = DetectRepairSignalText("apply_patch failed with invalid patch")
	if !signal.Repair || signal.UserReask || signal.Reason != "repair_turn" {
		t.Fatalf("repair signal=%+v", signal)
	}
	signal = DetectRepairSignalText("Da fehlt der wichtigste Output, nochmal ausführlicher bitte")
	if !signal.Repair || !signal.UserReask || signal.Reason != "user_reask" {
		t.Fatalf("german repair signal=%+v", signal)
	}
	signal = DetectRepairSignalText("normal next task")
	if signal.Repair || signal.UserReask || signal.Reason != "" {
		t.Fatalf("normal signal=%+v", signal)
	}
	signal = DetectRepairSignalText("show the output from that command")
	if signal.Repair || signal.UserReask || signal.Reason != "" {
		t.Fatalf("plain command output relay should not be repair signal=%+v", signal)
	}
	body := []byte(`{"messages":[{"role":"user","content":"what did you do?"}]}`)
	if signal = DetectRepairSignal(types.OpenAI, body); !signal.Repair || !signal.UserReask {
		t.Fatalf("request signal=%+v", signal)
	}
}

// TestDetectTaskShape_CommandRelayWithRepairComplaint covers the fallback
// commandOutputRelayRequested branch (shape.go:72-73): when the text triggers
// both commandOutputRelayRequested AND repairComplaintText but NOT
// DetectRepairSignalText, the first command-relay case (line 41) is skipped
// (because repairComplaintText is true), the repair case (line 43) is
// skipped (because DetectRepairSignalText is false), and the fallback
// command-relay case (line 72) fires.
func TestDetectTaskShape_CommandRelayWithRepairComplaint(t *testing.T) {
	t.Parallel()
	// "nochmal" triggers repairComplaintText but NOT DetectRepairSignalText
	// (which needs "nochmal ausführlicher" or "nochmal genauer").
	// "show the output" triggers commandOutputRelayRequested.
	body := []byte(`{"messages":[{"role":"user","content":"nochmal show the output from that command"}]}`)
	if got := DetectTaskShape(types.OpenAI, body); got != ShapeCommandRelay {
		t.Fatalf("shape=%s, want %s (fallback command-relay branch)", got, ShapeCommandRelay)
	}
}

func TestLatestUserRequestText(t *testing.T) {
	t.Parallel()
	t.Run("non_map_root_returns_empty", func(t *testing.T) {
		t.Parallel()
		if got := latestUserRequestText("not a map"); got != "" {
			t.Fatalf("non-map root should return empty, got %q", got)
		}
	})
	t.Run("non_map_item_skipped", func(t *testing.T) {
		t.Parallel()
		// messages array with a non-map item (string) should be skipped.
		root := map[string]any{
			"messages": []any{"not a map", map[string]any{"role": "user", "content": "hello"}},
		}
		// TrimSpace removes the trailing newline added by walkInstructionText.
		if got := latestUserRequestText(root); got != "hello" {
			t.Fatalf("non-map item should be skipped, got %q", got)
		}
	})
	t.Run("non_user_role_skipped", func(t *testing.T) {
		t.Parallel()
		// Only the last user message should be returned.
		root := map[string]any{
			"messages": []any{
				map[string]any{"role": "assistant", "content": "response"},
				map[string]any{"role": "user", "content": "question"},
			},
		}
		if got := latestUserRequestText(root); got != "question" {
			t.Fatalf("non-user role should be skipped, got %q", got)
		}
	})
	t.Run("input_string_returns_directly", func(t *testing.T) {
		t.Parallel()
		root := map[string]any{"input": "direct text"}
		if got := latestUserRequestText(root); got != "direct text" {
			t.Fatalf("input string should return directly, got %q", got)
		}
	})
	t.Run("no_user_messages_returns_empty", func(t *testing.T) {
		t.Parallel()
		root := map[string]any{
			"messages": []any{
				map[string]any{"role": "assistant", "content": "response"},
			},
		}
		if got := latestUserRequestText(root); got != "" {
			t.Fatalf("no user messages should return empty, got %q", got)
		}
	})
}

func TestWalkInstructionText(t *testing.T) {
	t.Parallel()
	t.Run("user_role_walks_content_and_text", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(map[string]any{
			"role":    "user",
			"content": "user content",
			"text":    "user text",
		}, &out, false)
		// role-based walk uses allowContent=true regardless of the
		// outer allowContent flag.
		if out.String() != "user content\nuser text\n" {
			t.Fatalf("user role walk = %q", out.String())
		}
	})
	t.Run("system_role_walks_content_and_text", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(map[string]any{
			"role":    "system",
			"content": "system content",
		}, &out, false)
		if out.String() != "system content\n" {
			t.Fatalf("system role walk = %q", out.String())
		}
	})
	t.Run("developer_role_walks_content_and_text", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(map[string]any{
			"role":    "developer",
			"content": "dev content",
		}, &out, false)
		if out.String() != "dev content\n" {
			t.Fatalf("developer role walk = %q", out.String())
		}
	})
	t.Run("unknown_role_returns_early", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(map[string]any{
			"role":    "assistant",
			"content": "should not appear",
		}, &out, true)
		if out.String() != "" {
			t.Fatalf("unknown role should return early, got %q", out.String())
		}
	})
	t.Run("input_text_type_walks_text", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(map[string]any{
			"type": "input_text",
			"text": "typed text",
		}, &out, false)
		if out.String() != "typed text\n" {
			t.Fatalf("input_text type walk = %q", out.String())
		}
	})
	t.Run("text_type_walks_text", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(map[string]any{
			"type": "text",
			"text": "plain text",
		}, &out, false)
		if out.String() != "plain text\n" {
			t.Fatalf("text type walk = %q", out.String())
		}
	})
	t.Run("allow_content_walks_content_and_text", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		// No role, no type — falls through to messages/input walk,
		// then system/instructions/prompt walk, then allowContent
		// content/text walk.
		walkInstructionText(map[string]any{
			"content": "allowed content",
			"text":    "allowed text",
		}, &out, true)
		if !strings.Contains(out.String(), "allowed content") || !strings.Contains(out.String(), "allowed text") {
			t.Fatalf("allowContent walk should include content and text, got %q", out.String())
		}
	})
	t.Run("no_allow_content_skips_content_and_text", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(map[string]any{
			"content": "should not appear",
			"text":    "should not appear",
		}, &out, false)
		if out.String() != "" {
			t.Fatalf("no allowContent should skip content/text, got %q", out.String())
		}
	})
	t.Run("string_with_allow_content", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText("hello world", &out, true)
		if out.String() != "hello world\n" {
			t.Fatalf("string with allowContent = %q", out.String())
		}
	})
	t.Run("string_without_allow_content", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText("hello world", &out, false)
		if out.String() != "" {
			t.Fatalf("string without allowContent should be empty, got %q", out.String())
		}
	})
	t.Run("array_recurses", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText([]any{"a", "b"}, &out, true)
		if out.String() != "a\nb\n" {
			t.Fatalf("array walk = %q", out.String())
		}
	})
	t.Run("nil_value", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		walkInstructionText(nil, &out, true)
		if out.String() != "" {
			t.Fatalf("nil walk should be empty, got %q", out.String())
		}
	})
}
