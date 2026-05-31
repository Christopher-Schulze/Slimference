package outputreduce

import (
	"fmt"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

const DefaultMarker = "#slimference-output-rules"

type Profile string

const (
	ProfileAuto            Profile = "auto"
	ProfileOff             Profile = "off"
	ProfileMild            Profile = "mild"
	ProfileStandard        Profile = "standard"
	ProfileAggressive      Profile = "aggressive"
	ProfileCodexAggressive Profile = "codex_aggressive"
	ProfileCustom          Profile = "custom"
	ProfileAnthropic       Profile = "anthropic"
	ProfileOpenAI          Profile = "openai"
	ProfileCodex           Profile = "codex"
	ProfileNoop            Profile = "noop"
)

func ParseProfile(s string) (Profile, error) {
	switch Profile(strings.ToLower(strings.TrimSpace(s))) {
	case "", ProfileAuto:
		return ProfileAuto, nil
	case ProfileOff:
		return ProfileOff, nil
	case ProfileMild:
		return ProfileMild, nil
	case ProfileStandard:
		return ProfileStandard, nil
	case ProfileAggressive:
		return ProfileAggressive, nil
	case ProfileCodexAggressive:
		return ProfileCodexAggressive, nil
	case ProfileCustom:
		return ProfileCustom, nil
	case ProfileAnthropic:
		return ProfileAnthropic, nil
	case ProfileOpenAI:
		return ProfileOpenAI, nil
	case ProfileCodex:
		return ProfileCodex, nil
	case ProfileNoop:
		return ProfileNoop, nil
	default:
		return "", fmt.Errorf("unknown output-reduce profile %q", s)
	}
}

func ResolveProfile(provider types.Provider, configured Profile) Profile {
	if configured != "" && configured != ProfileAuto {
		if configured == ProfileNoop {
			return ProfileOff
		}
		return configured
	}
	switch provider {
	case types.Anthropic:
		return ProfileStandard
	case types.OpenAI:
		return ProfileStandard
	case types.CodexChatGPT:
		return ProfileCodexAggressive
	default:
		return ProfileOff
	}
}

func SafeProfileForShape(profile Profile, shape TaskShape) Profile {
	switch shape {
	case ShapeCodeEdit, ShapeDebugging, ShapeReview, ShapeToolReasoning, ShapeNewFile, ShapeFinalSummary, ShapeReadOnly, ShapePlanning:
		if profile == ProfileAggressive || profile == ProfileCodexAggressive || profile == ProfileCodex {
			return ProfileStandard
		}
	}
	return profile
}

func Directive(profile Profile, marker string) string {
	return DirectiveForShape(profile, ShapeUnknown, marker)
}

func DirectiveForShape(profile Profile, shape TaskShape, marker string) string {
	if strings.TrimSpace(marker) == "" {
		marker = DefaultMarker
	}
	switch profile {
	case ProfileMild:
		return marker + "\nAnswer directly. Avoid preambles, sign-offs, and repeating user/tool content unless needed for correctness."
	case ProfileStandard, ProfileAnthropic, ProfileOpenAI:
		return marker + "\nOutput rules: start with the answer; no preamble; no sign-off; do not repeat received content or tool output; keep status lists one line per item; code edits as diffs/patches unless full/new-file output is required; comments only for non-obvious logic; binary questions: yes/no first, short reason second." + shapeDirective(shape)
	case ProfileAggressive:
		return marker + "\nAggressive output rules: answer in the fewest complete words; no ceremony; no recap; no quoted tool output; no \"let me know\" ending; code edits as patch/diff unless a full new file is required; after successful tool action, report result plus verification only; comments only for non-obvious invariants." + shapeDirective(shape)
	case ProfileCodex, ProfileCodexAggressive:
		if directive := compactCodexDirectiveForShape(shape, marker); directive != "" {
			return directive
		}
		return marker + "\nCodex output rules: be terse; skip preambles and recap filler; never re-emit tool output except the shortest needed error/path; after apply_patch/tool success, stop at result plus verification; prefer diffs/patches for edits unless creating a new file or explicitly asked for full content; preserve exact commands/paths/errors when debugging." + shapeDirective(shape)
	case ProfileCustom:
		return marker
	default:
		return ""
	}
}

func compactCodexDirectiveForShape(shape TaskShape, marker string) string {
	switch shape {
	case ShapeReadOnly:
		return marker + "\nRead-only: concise verdict plus evidence only; do not mention hooks, filters, or Slimference unless explicitly asked."
	case ShapePlanning:
		return marker + "\nPlanning: compact ordered steps, no filler, no hook/filter/Slimference meta."
	case ShapeDirectAnswer:
		return marker + "\nAnswer directly and briefly; no preamble, recap, sign-off, or hook/filter/Slimference meta."
	default:
		return ""
	}
}

func NextSofter(profile Profile) Profile {
	switch profile {
	case ProfileCodexAggressive, ProfileCodex, ProfileAggressive:
		return ProfileStandard
	case ProfileStandard, ProfileAnthropic, ProfileOpenAI:
		return ProfileMild
	case ProfileMild:
		return ProfileOff
	default:
		return ProfileOff
	}
}

func shapeDirective(shape TaskShape) string {
	switch shape {
	case ShapeCodeEdit:
		return " For code-edit tasks, avoid prose recaps after the patch."
	case ShapeNewFile:
		return " For new-file tasks, full file content is allowed when required."
	case ShapeReadOnly:
		return " For read-only analysis, do not suggest or emit edits unless explicitly asked; report only evidence, verdict, and next risk."
	case ShapeReview:
		return " For review tasks, keep all actionable findings; do not compress away severity, file, or line."
	case ShapeDebugging:
		return " For debugging, preserve exact error text, command, path, and line numbers."
	case ShapeToolReasoning:
		return " For tool-result reasoning, summarize only the decision-relevant lines."
	case ShapePlanning:
		return " For planning, use compact ordered steps with no filler."
	case ShapeFinalSummary:
		return " For final summaries, preserve requested files, commands, verification status, and unresolved risks."
	case ShapeRepairFollowup:
		return ""
	default:
		return ""
	}
}

func LowROISkipReason(shape TaskShape, inputTokens int) string {
	switch shape {
	case ShapeRepairFollowup:
		return "repair_followup_low_roi"
	case ShapeReadOnly:
		if inputTokens > 0 && inputTokens < 60000 {
			return "read_only_low_roi"
		}
	case ShapePlanning:
		if inputTokens > 0 && inputTokens < 30000 {
			return "planning_low_roi"
		}
	case ShapeDirectAnswer:
		if inputTokens > 0 && inputTokens < 12000 {
			return "direct_answer_low_roi"
		}
	}
	return ""
}
