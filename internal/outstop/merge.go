package outstop

import (
	"bytes"
	"encoding/json"

	"github.com/slimference/slimference/internal/types"
)

// Result describes what MergeIntoBody did. AddedCount==0 with OK==true
// means the user already had every phrase we would have added; the
// body was returned unchanged. OK==false means the body could not be
// parsed; callers should pass the original body upstream untouched.
type Result struct {
	OK         bool
	AddedCount int
	FieldUsed  string
	Provider   types.Provider
}

// MergeIntoBody injects the curated stop-phrase list into the request
// body using the per-provider field name. User-supplied entries are
// preserved verbatim and merged on top of our list. Idempotent:
// re-running on an already-injected body adds nothing and reports
// AddedCount=0.
//
// Anthropic: stop_sequences, capped at 4.
// OpenAI / CodexChatGPT: stop, capped at 4 (Chat Completions limit).
// Other providers: passthrough (returns body unchanged, OK=true).
// Responses-API shaped requests (`input`, or no `messages` array) are
// passthrough because the Responses API rejects Chat-Completions `stop`.
//
// Errors are deliberately swallowed: if the body is malformed JSON we
// return the original bytes with OK=false so the caller forwards it
// upstream unchanged. The proxy never breaks a request to add a
// stop-phrase optimisation.
func MergeIntoBody(provider types.Provider, body []byte) ([]byte, Result) {
	res := Result{Provider: provider}
	if len(body) == 0 {
		return body, res
	}
	switch provider {
	case types.Anthropic:
		res.FieldUsed = "stop_sequences"
	case types.OpenAI, types.CodexChatGPT:
		res.FieldUsed = "stop"
	default:
		// Unknown providers: passthrough. OK=true so callers treat
		// this as a no-op rather than an error.
		res.OK = true
		return body, res
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, res
	}
	if !supportsStopInjectionShape(raw) {
		res.OK = true
		return body, res
	}

	existing, hadField := decodeExistingStop(raw[res.FieldUsed])
	merged, added := mergePreservingUser(existing, PhrasesTopN(4))
	if added == 0 && hadField {
		res.OK = true
		return body, res
	}

	raw[res.FieldUsed] = encodeStopField(merged)
	// json.Marshal on a map[string]json.RawMessage where every value
	// already came from a successful Unmarshal cannot fail. The error
	// is intentionally elided.
	out, _ := json.Marshal(raw)
	res.OK = true
	res.AddedCount = added
	return out, res
}

func supportsStopInjectionShape(raw map[string]json.RawMessage) bool {
	if _, hasInput := raw["input"]; hasInput {
		return false
	}
	messagesRaw, ok := raw["messages"]
	if !ok {
		return false
	}
	var messages []json.RawMessage
	return json.Unmarshal(messagesRaw, &messages) == nil
}

// decodeExistingStop accepts both shapes the OpenAI/Anthropic APIs
// historically allow for the stop field: a single string or an array
// of strings. Returns the normalised slice and whether the field was
// present at all.
func decodeExistingStop(raw json.RawMessage) ([]string, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, true
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return nil, true
		}
		return []string{single}, true
	}
	return nil, false
}

// mergePreservingUser unions existing user entries (kept first, in
// their original order) with our additions (only those not already
// present). Cap at 4 to satisfy both Anthropic stop_sequences and
// OpenAI Chat Completions stop limits. Returns the merged slice and
// the number of new entries added.
func mergePreservingUser(existing, additions []string) ([]string, int) {
	const cap4 = 4
	seen := make(map[string]struct{}, len(existing)+len(additions))
	merged := make([]string, 0, cap4)
	for _, e := range existing {
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		merged = append(merged, e)
		if len(merged) == cap4 {
			return merged, 0
		}
	}
	added := 0
	for _, a := range additions {
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		merged = append(merged, a)
		added++
		if len(merged) == cap4 {
			break
		}
	}
	return merged, added
}

// encodeStopField produces the JSON representation of the merged
// list. Both Anthropic and OpenAI accept array form for multi-entry
// lists; we always emit an array for consistency. json.Marshal on
// []string cannot fail, so the error is intentionally discarded.
func encodeStopField(items []string) json.RawMessage {
	if len(items) == 0 {
		return json.RawMessage(`[]`)
	}
	out, _ := json.Marshal(items)
	return out
}
