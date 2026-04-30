package compression

import (
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// Loop detection (T37, ported in spirit from token-optimizer's
// detectors/looping.py).
//
// When a user sends 4+ consecutive messages with >=0.75 Jaccard word
// similarity, the model is almost certainly stuck in a retry loop. A single
// caught loop typically burns 30-60k tokens on repeated tool calls. We
// detect the pattern inside Layer 1 and inject a synthetic assistant note
// so the model has a chance to break out on the very next turn.
//
// The nudge is a single short assistant message inserted ahead of the
// latest user turn. Claude sees it naturally on the next pass.

// LoopDetectionThreshold is the Jaccard similarity at or above which two
// user messages are considered near-duplicates. Matches the empirical
// token-optimizer default.
const LoopDetectionThreshold = 0.75

// LoopDetectionMinStreak is the minimum consecutive similar-user-message
// count that triggers a nudge. 4 is the token-optimizer default.
const LoopDetectionMinStreak = 4

// DetectLoop scans messages and returns (nudge, streakLen, true) when a
// retry loop is detected. The nudge is a ready-to-inject assistant note.
// When no loop is found, returns ("", 0, false).
func DetectLoop(messages []types.Message) (string, int, bool) {
	userTexts := collectUserTexts(messages)
	if len(userTexts) < LoopDetectionMinStreak {
		return "", 0, false
	}
	sets := make([]map[string]struct{}, len(userTexts))
	for i, t := range userTexts {
		sets[i] = wordSet(t)
	}
	streak := 1
	maxStreak := 1
	for i := 1; i < len(sets); i++ {
		if jaccard(sets[i], sets[i-1]) >= LoopDetectionThreshold {
			streak++
			if streak > maxStreak {
				maxStreak = streak
			}
		} else {
			streak = 1
		}
	}
	if maxStreak < LoopDetectionMinStreak {
		return "", 0, false
	}
	nudge := formatLoopNudge(maxStreak)
	return nudge, maxStreak, true
}

// ApplyLoopNudge prepends a single-line nudge to the final user message
// when a retry loop is detected. The message count is unchanged so
// downstream cache keys and analytics stay stable; the model sees the
// note on the next turn and has the opportunity to break out.
//
// Returns (newMessages, savedTokens). When no loop is detected, or the
// nudge already exists, the slice is returned unchanged.
func ApplyLoopNudge(messages []types.Message) ([]types.Message, int) {
	nudge, streak, ok := DetectLoop(messages)
	if !ok {
		return messages, 0
	}
	if alreadyContainsLoopNudge(messages) {
		return messages, 0
	}
	// DetectLoop succeeded, so at least LoopDetectionMinStreak user text
	// messages exist and lastUserMsgIdx is guaranteed non-negative.
	lastUser := lastUserMsgIdx(messages)
	// Deep-copy the target message to avoid mutating caller-owned slices.
	out := make([]types.Message, len(messages))
	copy(out, messages)
	src := out[lastUser]
	cloned := make([]types.ContentBlock, len(src.Content))
	copy(cloned, src.Content)
	textIdx := -1
	for i := range cloned {
		if cloned[i].Type == "text" && cloned[i].Text != "" {
			textIdx = i
			break
		}
	}
	if textIdx >= 0 {
		cloned[textIdx].Text = nudge + "\n\n" + cloned[textIdx].Text
	} else {
		cloned = append([]types.ContentBlock{{Type: "text", Text: nudge}}, cloned...)
	}
	src.Content = cloned
	out[lastUser] = src
	savedTokens := (streak - 1) * loopNudgeSavingsPerStreakMsg
	return out, savedTokens
}

// loopNudgeSavingsPerStreakMsg is an estimate - a real measurement needs
// A/B data. 5000 per repeat matches token-optimizer's observation.
const loopNudgeSavingsPerStreakMsg = 5000

// LoopNudgeMarker is a substring every injected nudge carries so subsequent
// runs can detect they already ran.
const LoopNudgeMarker = "[slimference-loop-nudge]"

// formatLoopNudge produces the text injected into the conversation.
func formatLoopNudge(streak int) string {
	var sb strings.Builder
	sb.WriteString(LoopNudgeMarker)
	sb.WriteString(" Detected ")
	sb.WriteString(strconv.Itoa(streak))
	sb.WriteString(" near-identical consecutive user turns. The current approach is probably stuck in a retry loop. ")
	sb.WriteString("Consider: restate the problem with a concrete example, try a different angle, or ask for a smaller sub-step first.")
	return sb.String()
}

// collectUserTexts returns the primary text of each user message in order.
func collectUserTexts(messages []types.Message) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		for _, b := range m.Content {
			if b.Type == "text" && len(b.Text) > 10 {
				text := b.Text
				if len(text) > 500 {
					text = text[:500]
				}
				out = append(out, text)
				break
			}
		}
	}
	return out
}

// wordSet tokenises s on whitespace and returns a unique lowercase set.
func wordSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range strings.Fields(strings.ToLower(s)) {
		out[w] = struct{}{}
	}
	return out
}

// jaccard returns the Jaccard similarity between two sets. Both sets must
// be non-empty; an empty set short-circuits to 0.0 because any set unioned
// with itself keeps len >= 1 so union is always positive in the hot path.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for w := range a {
		if _, ok := b[w]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	return float64(intersection) / float64(union)
}

// lastUserMsgIdx returns the index of the final user message or -1.
func lastUserMsgIdx(messages []types.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return i
		}
	}
	return -1
}

// alreadyContainsLoopNudge checks whether the marker is already present.
func alreadyContainsLoopNudge(messages []types.Message) bool {
	for _, m := range messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, LoopNudgeMarker) {
				return true
			}
		}
	}
	return false
}
