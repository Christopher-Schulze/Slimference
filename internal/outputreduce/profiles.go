package outputreduce

import (
	"fmt"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

const DefaultMarker = "#slimference-output-rules"

type Profile string

const (
	ProfileAuto      Profile = "auto"
	ProfileAnthropic Profile = "anthropic"
	ProfileOpenAI    Profile = "openai"
	ProfileCodex     Profile = "codex"
	ProfileNoop      Profile = "noop"
)

func ParseProfile(s string) (Profile, error) {
	switch Profile(strings.ToLower(strings.TrimSpace(s))) {
	case "", ProfileAuto:
		return ProfileAuto, nil
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
		return configured
	}
	switch provider {
	case types.Anthropic:
		return ProfileAnthropic
	case types.OpenAI:
		return ProfileOpenAI
	case types.CodexChatGPT:
		return ProfileCodex
	default:
		return ProfileNoop
	}
}

func Directive(profile Profile, marker string) string {
	if strings.TrimSpace(marker) == "" {
		marker = DefaultMarker
	}
	switch profile {
	case ProfileAnthropic:
		return marker + "\nOutput discipline: answer directly; no preamble or sign-off; do not quote back user/tool content; cite only shortest relevant path/error/line; for code edits prefer unified diff/apply_patch unless a full new file or full output is explicitly requested; add comments only for non-obvious invariants; after successful edits, report only result and verification."
	case ProfileOpenAI:
		return marker + "\nOutput rules: start with the answer; no preamble; no sign-off; do not repeat received content or tool output; keep status lists one line per item; code edits as diffs/patches unless full/new-file output is required; comments only for non-obvious logic; binary questions: yes/no first, short reason second."
	case ProfileCodex:
		return marker + "\nCodex output rules: be terse; skip preambles and recap filler; never re-emit tool output except the shortest needed error/path; after apply_patch/tool success, stop at result plus verification; prefer diffs/patches for edits unless creating a new file or explicitly asked for full content."
	default:
		return ""
	}
}
