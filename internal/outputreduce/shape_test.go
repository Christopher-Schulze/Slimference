package outputreduce

import (
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestDetectTaskShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body []byte
		want TaskShape
	}{
		{name: "exact reply", body: []byte(`{"messages":[{"role":"user","content":"reply exactly: ok"}]}`), want: ShapeExactReply},
		{name: "german exact reply", body: []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"Antworte exakt nur mit: OK. Nutze keine Tools."}]}]}`), want: ShapeExactReply},
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
		{name: "tool", body: []byte(`{"messages":[{"role":"user","content":"stderr exit code command output"}]}`), want: ShapeToolReasoning},
		{name: "direct", body: []byte(`{"messages":[{"role":"user","content":"what is this"}]}`), want: ShapeDirectAnswer},
		{name: "invalid json fallback", body: []byte(`debug error`), want: ShapeDebugging},
		{name: "ignores schema text", body: []byte(`{"tools":[{"description":"create file"}],"messages":[{"role":"user","content":"what is this"}]}`), want: ShapeDirectAnswer},
	}
	for _, tt := range tests {
		tt := tt
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
	signal = DetectRepairSignalText("normal next task")
	if signal.Repair || signal.UserReask || signal.Reason != "" {
		t.Fatalf("normal signal=%+v", signal)
	}
	body := []byte(`{"messages":[{"role":"user","content":"what did you do?"}]}`)
	if signal = DetectRepairSignal(types.OpenAI, body); !signal.Repair || !signal.UserReask {
		t.Fatalf("request signal=%+v", signal)
	}
}
