package summarization

import (
	"strings"
	"testing"
)

func TestPickSummaryTaskContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want SummaryTaskContract
	}{
		{"debug", "go test failed with panic: nil pointer and stack trace", ContractDebug},
		{"review", "please do a code review with severity findings", ContractReview},
		{"live", "run live E2E smoke through websocket system proxy", ContractLiveE2E},
		{"docs", "update docs/todo.md and changelog documentation", ContractDocs},
		{"plan", "make an architecture plan with blockers and sequence", ContractPlan},
		{"coding", "apply_patch implemented refactor then go test ./...", ContractCoding},
		{"generic", "summarize this normal conversation", ContractGeneric},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pickSummaryTaskContract(tc.in); got != tc.want {
				t.Fatalf("contract=%s want %s", got, tc.want)
			}
		})
	}
}

func TestTaskContractPrompt(t *testing.T) {
	t.Parallel()
	for _, contract := range []SummaryTaskContract{ContractGeneric, ContractCoding, ContractDebug, ContractReview, ContractPlan, ContractDocs, ContractLiveE2E} {
		prompt := taskContractPrompt(contract)
		if !strings.Contains(prompt, "TASK CONTRACT:") {
			t.Fatalf("contract %s missing header: %q", contract, prompt)
		}
	}
}

func TestBuildSystemPromptIncludesTaskContract(t *testing.T) {
	t.Parallel()
	SetPromptOverride("", "")
	ResetExamplePromptCounts()
	prompt := buildSystemPrompt("go test failed with panic: exact error")
	if !strings.Contains(prompt, "TASK CONTRACT: debugging/failure analysis") {
		t.Fatalf("debug contract missing:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Preserve exact error text") {
		t.Fatalf("debug preservation rule missing:\n%s", prompt)
	}
}
