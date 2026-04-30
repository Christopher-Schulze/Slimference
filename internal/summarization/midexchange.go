package summarization

import (
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

// midExchangeMarker is the leading tag that ApplyMidExchange stamps on
// every synthetic block it produces. DetectMidExchangePoint uses it to
// recognise an already-collapsed range and skip re-collapsing it (T99c
// idempotency). Keep in sync with FormatMidExchangeSummary.
const midExchangeMarker = "[in-progress summary, anchor=msg #"

// IsMidExchangeMarker reports whether a message is a synthetic
// mid-exchange summary produced by ApplyMidExchange. Used to keep
// DetectMidExchangePoint idempotent across consecutive requests.
func IsMidExchangeMarker(msg types.Message) bool {
	for _, b := range msg.Content {
		if b.Type == "text" && strings.HasPrefix(b.Text, midExchangeMarker) {
			return true
		}
	}
	return false
}

// MidExchangePoint describes a summarizable range within the current
// in-flight exchange. T99.
type MidExchangePoint struct {
	Start int
	End   int
}

// DetectMidExchangePoint is a pure function that finds a completed
// tool-use sub-workflow within the current in-flight exchange whose
// token count exceeds threshold. Returns (point, true) when a
// summarizable range exists, or (zero, false) otherwise.
//
// Detection heuristic: find the last user-started exchange boundary,
// then walk the exchange looking for completed tool-use cycles
// (assistant[tool_use] -> user[tool_result] -> assistant). If the
// tokens from the exchange start up to the last completed cycle
// exceed the threshold, that range is the mid-exchange candidate.
func DetectMidExchangePoint(messages []types.Message, threshold int) (MidExchangePoint, bool) {
	if threshold <= 0 || len(messages) < 4 {
		return MidExchangePoint{}, false
	}

	exchangeStart := lastExchangeStart(messages)
	if exchangeStart < 0 {
		return MidExchangePoint{}, false
	}

	// T99c idempotency: if the exchange already contains a synthetic
	// mid-exchange marker, do not collapse again.
	for i := exchangeStart; i < len(messages); i++ {
		if IsMidExchangeMarker(messages[i]) {
			return MidExchangePoint{}, false
		}
	}

	lastCycleEnd := -1
	for i := exchangeStart + 1; i < len(messages)-1; i++ {
		if messages[i].Role == "assistant" && hasToolUse(messages[i]) &&
			messages[i+1].Role == "user" && hasToolResult(messages[i+1]) {
			end := i + 1
			if end+1 < len(messages) && messages[end+1].Role == "assistant" {
				end++
			}
			lastCycleEnd = end
		}
	}

	if lastCycleEnd < 0 || lastCycleEnd <= exchangeStart {
		return MidExchangePoint{}, false
	}

	candidateTokens := 0
	for i := exchangeStart; i <= lastCycleEnd; i++ {
		candidateTokens += estimateMsgTokens(messages[i])
	}

	if candidateTokens < threshold {
		return MidExchangePoint{}, false
	}

	return MidExchangePoint{Start: exchangeStart, End: lastCycleEnd}, true
}

// lastExchangeStart returns the index of the last user message that
// starts an exchange. Returns -1 if no user message exists.
func lastExchangeStart(messages []types.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			hasToolResult := false
			for _, b := range messages[i].Content {
				if b.Type == "tool_result" {
					hasToolResult = true
				}
			}
			if !hasToolResult {
				return i
			}
		}
	}
	return -1
}

// hasToolUse reports whether a message contains at least one tool_use block.
func hasToolUse(msg types.Message) bool {
	for _, b := range msg.Content {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// hasToolResult reports whether a message contains at least one tool_result block.
func hasToolResult(msg types.Message) bool {
	for _, b := range msg.Content {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

// estimateMsgTokens returns a rough token estimate for a message.
func estimateMsgTokens(msg types.Message) int {
	total := 0
	for _, b := range msg.Content {
		total += len(b.Text) / 4
	}
	return total
}

// ApplyMidExchange checks whether the current in-flight exchange exceeds the
// token threshold and, if so, replaces the summarizable range with a synthetic
// in-progress summary message. This is a deterministic stub: no LLM call is
// made; the summary text is generated locally. Returns (newMessages, tokensSaved, applied).
func ApplyMidExchange(messages []types.Message, threshold int) ([]types.Message, int, bool) {
	pt, ok := DetectMidExchangePoint(messages, threshold)
	if !ok {
		return messages, 0, false
	}

	origTokens := 0
	for i := pt.Start; i <= pt.End; i++ {
		origTokens += estimateMsgTokens(messages[i])
	}

	summaryText := FormatMidExchangeSummary("completed steps summarized", pt.Start)
	summaryTokens := len(summaryText) / 4

	synthetic := types.Message{
		Index: messages[pt.Start].Index,
		Role:  "assistant",
		Content: []types.ContentBlock{
			{Type: "text", Text: summaryText},
		},
		Metadata: types.MessageMetadata{
			OriginalTokens:   origTokens,
			CompressedTokens: summaryTokens,
			CompressionLevel: types.CompressionLayer2,
		},
	}

	result := make([]types.Message, 0, len(messages)-(pt.End-pt.Start))
	result = append(result, messages[:pt.Start]...)
	result = append(result, synthetic)
	result = append(result, messages[pt.End+1:]...)

	for i := range result {
		result[i].Index = i
	}

	saved := origTokens - summaryTokens
	if saved < 0 {
		saved = 0
	}
	return result, saved, true
}

// FormatMidExchangeSummary produces the tagged summary text for a
// mid-exchange replacement. The anchor tag tells the model the
// content is an in-progress summary, not a final one.
func FormatMidExchangeSummary(summaryText string, anchorMsgIdx int) string {
	return midExchangeMarker + strconv.Itoa(anchorMsgIdx) + "]\n" + summaryText
}
