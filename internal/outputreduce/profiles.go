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
	case ShapeCodeEdit, ShapeDebugging, ShapeExplanation, ShapeReview, ShapeToolReasoning, ShapeCommandRelay, ShapeNewFile, ShapeFinalSummary, ShapeReadOnly, ShapePlanning:
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
		if directive := compactStandardDirectiveForShape(shape, marker); directive != "" {
			return directive
		}
		return marker + "\nOutput rules: answer first; no preamble, recap, or sign-off; do not repeat user/tool content unless needed; preserve exact commands, paths, errors, and requested details; code edits as diffs unless full content is required." + shapeDirective(shape)
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

func compactStandardDirectiveForShape(shape TaskShape, marker string) string {
	switch shape {
	case ShapeCodeEdit:
		return marker + "\nFor code-edit tasks, report patch result and verification only; preserve exact paths/errors; no prose recap."
	case ShapeNewFile:
		return marker + "\nFor new-file tasks, full file content is allowed when required; no preamble, recap, sign-off, or Slimference meta."
	case ShapeReadOnly:
		return marker + "\nFor read-only analysis: verdict plus evidence/risks; no edits; keep exact facts/paths/errors; no preamble/sign-off/Slimference meta."
	case ShapeReview:
		return marker + "\nFor review tasks, keep actionable findings with severity, file, and line; no filler, recap, or sign-off."
	case ShapeDebugging:
		return marker + "\nFor debugging, preserve exact error text, command, path, and line numbers; root cause first; no filler."
	case ShapeExplanation:
		return marker + "\nFor explanations: concise but complete; keep requested detail, evidence, caveats, exact facts/paths/errors. No preamble, recap, sign-off, or Slimference meta."
	case ShapeToolReasoning:
		return marker + "\nFor tool-result reasoning, keep only decision-relevant lines; preserve exact paths/errors; no filler."
	case ShapeCommandRelay:
		return marker + "\nFor command-output relay, preserve exact requested output, paths, errors, exit codes, and line order; do not summarize unless asked."
	case ShapePlanning:
		return marker + "\nFor planning, use compact ordered steps; keep constraints, risks, and verification gates; no filler."
	case ShapeFinalSummary:
		return marker + "\nFor final summaries, preserve requested files, commands, verification status, and unresolved risks; no preamble/sign-off/Slimference meta."
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
		return marker + "\nAnswer briefly. No preamble, recap, sign-off, or Slimference meta."
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
	case ShapeExplanation:
		return " For explanations and deep analysis, preserve reasoning steps, caveats, concrete evidence, and requested detail."
	case ShapeToolReasoning:
		return " For tool-result reasoning, summarize only the decision-relevant lines."
	case ShapeCommandRelay:
		return " For command-output relay, preserve exact requested output, paths, errors, exit codes, and line order; do not summarize unless explicitly asked."
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
	case ShapeCommandRelay:
		return "command_output_relay_exact_output"
	case ShapeCodeEdit, ShapeNewFile, ShapeReadOnly, ShapeReview, ShapeDebugging, ShapeExplanation, ShapeToolReasoning, ShapePlanning, ShapeFinalSummary:
		return "unproven_task_shape_ab_required"
	case ShapeDirectAnswer:
		if inputTokens > 0 && inputTokens < 12000 {
			return "direct_answer_low_roi"
		}
	}
	return ""
}
