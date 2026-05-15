package outputreduce

import (
	"encoding/json"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

type TaskShape string

const (
	ShapeUnknown        TaskShape = "unknown"
	ShapeExactReply     TaskShape = "exact_reply"
	ShapeDirectAnswer   TaskShape = "direct_answer"
	ShapeReadOnly       TaskShape = "read_only_analysis"
	ShapeCodeEdit       TaskShape = "code_edit"
	ShapeDebugging      TaskShape = "debugging"
	ShapePlanning       TaskShape = "planning"
	ShapeRepairFollowup TaskShape = "repair_followup"
	ShapeReview         TaskShape = "review"
	ShapeToolReasoning  TaskShape = "tool_result_reasoning"
	ShapeNewFile        TaskShape = "new_file_generation"
)

func DetectTaskShape(provider types.Provider, body []byte) TaskShape {
	text := requestText(provider, body)
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower,
		"reply exactly", "respond exactly", "answer exactly", "say exactly", "output exactly",
		"antworte exakt", "antwort exakt", "gib exakt", "sage exakt", "nur mit:",
	):
		return ShapeExactReply
	case DetectRepairSignalText(lower).Repair:
		return ShapeRepairFollowup
	case containsAny(lower,
		"read-only", "read only", "do not edit", "don't edit", "do not modify", "do not write", "no edits",
		"inspect", "analyze", "analyse", "audit", "report in", "nur analysieren", "nichts anfassen", "nicht anfassen",
	):
		return ShapeReadOnly
	case containsAny(lower, "create file", "new file", "write a file", "add file", "*** add file"):
		return ShapeNewFile
	case containsAny(lower, "apply_patch", "patch", "diff", "edit", "modify", "fix this file", "implement"):
		return ShapeCodeEdit
	case containsAny(lower, "error", "failed", "panic", "stack trace", "debug", "why does", "root cause"):
		return ShapeDebugging
	case containsAny(lower, "review", "audit", "find issues", "severity"):
		return ShapeReview
	case containsAny(lower, "plan", "roadmap", "steps", "todo"):
		return ShapePlanning
	case containsAny(lower, "tool_result", "stdout", "stderr", "exit code", "command output"):
		return ShapeToolReasoning
	case strings.TrimSpace(lower) != "":
		return ShapeDirectAnswer
	default:
		return ShapeUnknown
	}
}

type RepairSignal struct {
	Repair    bool
	UserReask bool
	Reason    string
}

func DetectRepairSignal(provider types.Provider, body []byte) RepairSignal {
	return DetectRepairSignalText(requestText(provider, body))
}

func DetectRepairSignalText(text string) RepairSignal {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower,
		"you skipped", "you omitted", "missing detail", "too short", "explain more",
		"what did you do", "what changed", "show the output", "give details",
		"du hast übersprungen", "du hast ausgelassen", "zu kurz", "mehr details",
		"erklär mehr", "erklaer mehr", "was hast du gemacht", "was wurde geändert",
	):
		return RepairSignal{Repair: true, UserReask: true, Reason: "user_reask"}
	case containsAny(lower,
		"patch failed", "apply_patch failed", "malformed patch", "invalid patch",
		"did not apply", "could not apply", "build failed after your change",
		"test failed after your change", "kaputter patch", "patch ging nicht",
	):
		return RepairSignal{Repair: true, Reason: "repair_turn"}
	default:
		return RepairSignal{}
	}
}

func requestText(provider types.Provider, body []byte) string {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return string(body)
	}
	var out strings.Builder
	walkText(root, &out)
	return out.String()
}

func walkText(v any, out *strings.Builder) {
	switch x := v.(type) {
	case string:
		out.WriteString(x)
		out.WriteByte('\n')
	case []any:
		for _, item := range x {
			walkText(item, out)
		}
	case map[string]any:
		for key, value := range x {
			if key == "messages" || key == "content" || key == "text" || key == "input" || key == "system" || key == "command" || key == "stderr" || key == "stdout" || key == "output" || key == "arguments" {
				walkText(value, out)
			}
		}
	}
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
