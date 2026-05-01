package compression

import (
	"fmt"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

type streakRange struct {
	start, end int
}

func CollapseLoopMessages(messages []types.Message) ([]types.Message, int) {
	userTexts := collectUserTexts(messages)
	if len(userTexts) < LoopDetectionMinStreak {
		return messages, 0
	}
	sets := make([]map[string]struct{}, len(userTexts))
	for i, t := range userTexts {
		sets[i] = wordSet(t)
	}

	var streaks []streakRange
	streakStart := 0
	streakLen := 1
	for i := 1; i < len(sets); i++ {
		if jaccard(sets[i], sets[i-1]) >= LoopDetectionThreshold {
			streakLen++
		} else {
			if streakLen >= LoopDetectionMinStreak {
				streaks = append(streaks, streakRange{streakStart, i - 1})
			}
			streakStart = i
			streakLen = 1
		}
	}
	if streakLen >= LoopDetectionMinStreak {
		streaks = append(streaks, streakRange{streakStart, len(sets) - 1})
	}
	if len(streaks) == 0 {
		return messages, 0
	}

	userIdx := collectUserIndices(messages)
	collapseSet := make(map[int]bool)
	for _, s := range streaks {
		for j := s.start + 1; j <= s.end; j++ {
			if j < len(userIdx) {
				collapseSet[userIdx[j]] = true
			}
		}
	}

	out := make([]types.Message, 0, len(messages))
	saved := 0
	for i, m := range messages {
		if collapseSet[i] {
			origLen := msgTextLen(m)
			collapsed := types.Message{
				Role: m.Role,
				Content: []types.ContentBlock{
					{Type: "text", Text: fmt.Sprintf("[Near-duplicate of message %d - collapsed at message %d]", firstStreakUserIdx(streaks, userIdx, i), i)},
				},
			}
			out = append(out, collapsed)
			newLen := msgTextLen(collapsed)
			if origLen > newLen {
				saved += origLen - newLen
			}
		} else {
			out = append(out, m)
		}
	}
	for i := range out {
		out[i].Index = i
	}
	return out, saved
}

func collectUserIndices(messages []types.Message) []int {
	var idx []int
	for i, m := range messages {
		if m.Role == "user" {
			idx = append(idx, i)
		}
	}
	return idx
}

func firstStreakUserIdx(streaks []streakRange, userIdx []int, msgIdx int) int {
	for _, s := range streaks {
		if s.start < len(userIdx) {
			first := userIdx[s.start]
			if first >= 0 {
				return first
			}
		}
	}
	return msgIdx
}

func msgTextLen(m types.Message) int {
	n := 0
	for _, b := range m.Content {
		n += len(b.Text)
	}
	return n
}

func ResolveLoopStrategy(cfg StrategyConfig) LoopStrategyResult {
	switch strings.ToLower(cfg.LoopStrategy) {
	case "subtractive":
		return LoopStrategyResult{Strategy: "subtractive", Apply: applySubtractive}
	case "off":
		return LoopStrategyResult{Strategy: "off", Apply: applyOff}
	default:
		return LoopStrategyResult{Strategy: "additive", Apply: applyAdditive}
	}
}

type StrategyConfig struct {
	LoopDetection bool
	LoopStrategy  string
}

type LoopStrategyResult struct {
	Strategy string
	Apply    func([]types.Message) ([]types.Message, int)
}

func applyAdditive(messages []types.Message) ([]types.Message, int) {
	return ApplyLoopNudge(messages)
}

func applySubtractive(messages []types.Message) ([]types.Message, int) {
	return CollapseLoopMessages(messages)
}

func applyOff(messages []types.Message) ([]types.Message, int) {
	return messages, 0
}
