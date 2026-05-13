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
		{name: "new file", body: []byte(`{"messages":[{"role":"user","content":"create file internal/x.go"}]}`), want: ShapeNewFile},
		{name: "code edit", body: []byte(`{"messages":[{"role":"user","content":"apply_patch this bug"}]}`), want: ShapeCodeEdit},
		{name: "debug", body: []byte(`{"messages":[{"role":"user","content":"debug this panic stack trace"}]}`), want: ShapeDebugging},
		{name: "review", body: []byte(`{"messages":[{"role":"user","content":"review for severity findings"}]}`), want: ShapeReview},
		{name: "planning", body: []byte(`{"messages":[{"role":"user","content":"plan next steps"}]}`), want: ShapePlanning},
		{name: "tool", body: []byte(`{"messages":[{"role":"user","content":"stderr exit code command output"}]}`), want: ShapeToolReasoning},
		{name: "direct", body: []byte(`{"messages":[{"role":"user","content":"what is this"}]}`), want: ShapeDirectAnswer},
		{name: "invalid json fallback", body: []byte(`debug error`), want: ShapeDebugging},
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
