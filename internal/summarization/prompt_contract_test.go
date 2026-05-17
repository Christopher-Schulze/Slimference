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
	tests := map[SummaryTaskContract]string{
		ContractGeneric: "TASK CONTRACT: generic",
		ContractCoding:  "TASK CONTRACT: coding implementation",
		ContractDebug:   "TASK CONTRACT: debugging/failure analysis",
		ContractReview:  "TASK CONTRACT: code review/audit",
		ContractPlan:    "TASK CONTRACT: planning/architecture",
		ContractDocs:    "TASK CONTRACT: documentation",
		ContractLiveE2E: "TASK CONTRACT: live E2E/testing",
	}
	for contract, want := range tests {
		got := taskContractPrompt(contract)
		if !strings.Contains(got, want) {
			t.Fatalf("contract %s prompt=%q missing %q", contract, got, want)
		}
	}
}
