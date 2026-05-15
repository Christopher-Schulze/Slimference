package summarization

import "strings"

type SummaryTaskContract string

const (
	ContractGeneric SummaryTaskContract = "generic"
	ContractCoding  SummaryTaskContract = "coding"
	ContractDebug   SummaryTaskContract = "debugging"
	ContractReview  SummaryTaskContract = "review"
	ContractPlan    SummaryTaskContract = "planning"
	ContractDocs    SummaryTaskContract = "documentation"
	ContractLiveE2E SummaryTaskContract = "live_e2e"
)

func pickSummaryTaskContract(input string) SummaryTaskContract {
	low := strings.ToLower(input)
	score := map[SummaryTaskContract]int{
		ContractGeneric: 1,
		ContractCoding:  0,
		ContractDebug:   0,
		ContractReview:  0,
		ContractPlan:    0,
		ContractDocs:    0,
		ContractLiveE2E: 0,
	}
	addSignals(score, low, ContractDebug, 3, "panic:", "traceback", "stack trace", "assertionerror", "test failed", "build failed", "failing test", "root cause", "error:")
	addSignals(score, low, ContractReview, 3, "code review", "review", "findings", "severity", "regression", "security issue", "bug risk")
	addSignals(score, low, ContractLiveE2E, 3, "live e2e", "smoke", "browser-use", "playwright", "websocket", "system proxy", "operator proof", "verification gate")
	addSignals(score, low, ContractDocs, 2, "documentation", "docs/", "readme", "changelog", "spec.md", "todo.md", "write docs")
	addSignals(score, low, ContractPlan, 2, "plan", "architecture", "roadmap", "sequence", "blocker", "milestone", "task list")
	addSignals(score, low, ContractCoding, 2, "apply_patch", "edit_file", "write_file", "modified", "implemented", "refactor", "go test", "cargo test", "npm test")

	best := ContractGeneric
	bestScore := score[best]
	for _, candidate := range []SummaryTaskContract{ContractDebug, ContractReview, ContractLiveE2E, ContractCoding, ContractPlan, ContractDocs} {
		if score[candidate] > bestScore {
			best = candidate
			bestScore = score[candidate]
		}
	}
	return best
}

func addSignals(score map[SummaryTaskContract]int, input string, contract SummaryTaskContract, weight int, signals ...string) {
	for _, signal := range signals {
		score[contract] += strings.Count(input, signal) * weight
	}
}

func taskContractPrompt(contract SummaryTaskContract) string {
	switch contract {
	case ContractCoding:
		return "TASK CONTRACT: coding implementation\n" +
			"- Preserve exact files changed, functions touched, commands run, and test results.\n" +
			"- Summarize code bodies by purpose only; never invent file content.\n" +
			"- Keep unresolved edits, dirty-worktree boundaries, and verification gaps.\n\n"
	case ContractDebug:
		return "TASK CONTRACT: debugging/failure analysis\n" +
			"- Preserve exact error text, failing command, failing test name, path, line, and root-cause evidence.\n" +
			"- Keep attempted fixes and whether each failed or passed.\n" +
			"- Never replace concrete errors with generic phrases.\n\n"
	case ContractReview:
		return "TASK CONTRACT: code review/audit\n" +
			"- Preserve every finding with severity, file path, line/reference, and behavioral risk.\n" +
			"- Preserve explicit no-issue conclusions and remaining test gaps.\n" +
			"- Never collapse multiple findings into one vague risk.\n\n"
	case ContractPlan:
		return "TASK CONTRACT: planning/architecture\n" +
			"- Preserve decisions, alternatives rejected, order constraints, blockers, owners, and next steps.\n" +
			"- Keep must-not-touch boundaries and assumptions verbatim.\n" +
			"- Never turn open questions into decisions.\n\n"
	case ContractDocs:
		return "TASK CONTRACT: documentation\n" +
			"- Preserve source-of-truth files, exact terminology, stale-doc conflicts, and updated sections.\n" +
			"- Keep user-facing wording constraints and unresolved doc drift.\n" +
			"- Never invent product claims or readiness status.\n\n"
	case ContractLiveE2E:
		return "TASK CONTRACT: live E2E/testing\n" +
			"- Preserve exact environment, command, route, provider/model, request shape, token counters, and pass/fail status.\n" +
			"- Mark synthetic, comparable, and true live evidence explicitly.\n" +
			"- Never claim live proof from fixtures or estimates.\n\n"
	default:
		return "TASK CONTRACT: generic\n" +
			"- Preserve paths, commands, decisions, failures, and open blockers.\n" +
			"- Keep uncertainty markers when evidence is incomplete.\n\n"
	}
}
