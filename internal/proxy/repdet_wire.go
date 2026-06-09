package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/outstop/repdet"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// passthroughAnthropicWithRepdet buffers the upstream non-streaming
// Anthropic response, runs repdet over its text blocks, writes the
// rewritten body to the client with an accurate Content-Length, and
// returns the (possibly rewritten) body for downstream cache /
// telemetry use. Any read / parse failure falls back to plain
// passthrough behaviour so we never break a response to optimise.
func (p *Proxy) passthroughAnthropicWithRepdet(w http.ResponseWriter, upstreamResp *http.Response, messages []types.Message, log *slog.Logger) []byte {
	defer upstreamResp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(upstreamResp.Body, maxUpstreamResponseBodySize+1))
	if err != nil {
		slog.Error("read upstream response", "error", err)
		http.Error(w, "upstream read error", http.StatusBadGateway)
		return nil
	}
	if len(body) > maxUpstreamResponseBodySize {
		slog.Error("read upstream response", "error", errUpstreamResponseBodyTooLarge)
		http.Error(w, errUpstreamResponseBodyTooLarge.Error(), http.StatusBadGateway)
		return nil
	}
	out := body
	saved := 0
	idx := buildRepdetIndex(messages)
	if len(idx.Blocks()) > 0 {
		rewritten, savedBytes := rewriteAnthropicResponseBody(body, idx)
		if savedBytes > 0 {
			out = rewritten
			saved = savedBytes
			p.outputReduceCounters.RecordRepdetRewrite(1, saved)
			if log != nil {
				log.Debug("repdet body rewritten", "saved_bytes", saved, "matches_replaced", true)
			}
		}
	}
	for k, vv := range upstreamResp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(out)
	return out
}

// buildRepdetIndex constructs a fresh per-request Index from the
// prompt's tool_result blocks and substantial text blocks. Empty
// index when there are no candidates - the matcher then returns
// nothing without allocating per request.
//
// Per-request lifetime is deliberate: the dominant repeat case is
// "model echoes content from the same turn's prompt." A per-session
// index that catches cross-turn repeats is a follow-up. Single-turn
// coverage already captures the largest hit.
func buildRepdetIndex(messages []types.Message) *repdet.Index {
	idx := repdet.NewIndex()
	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_result":
				name := blockNameForToolResult(block)
				idx.AddBlock(name, 0, 0, block.Text)
			case "text":
				// Only register prompt text long enough to matter.
				// MinMatch+WindowSize guards against indexing
				// short user prose that would never reach a
				// confirmable echo length.
				if len(block.Text) >= repdet.MinMatch+repdet.WindowSize {
					idx.AddBlock("prompt-text", 0, 0, block.Text)
				}
			}
		}
	}
	return idx
}

// blockNameForToolResult picks a human-readable identifier for the
// marker. Falls back to a generic label when no tool-use linkage is
// available.
func blockNameForToolResult(b types.ContentBlock) string {
	if b.ToolName != "" {
		return b.ToolName
	}
	if b.ToolUseID != "" {
		return "tool:" + b.ToolUseID
	}
	return "tool_result"
}

// rewriteAnthropicResponseBody walks an Anthropic non-streaming response,
// runs repdet over every text block, and returns the re-marshalled body
// with `[unchanged: …]` markers replacing confirmed echoes. The total
// bytes saved (sum of replaced spans minus marker lengths) is reported
// for telemetry. On any parse / marshal failure, returns the input body
// unchanged with 0 savings - the proxy never breaks a response to
// optimise.
func rewriteAnthropicResponseBody(body []byte, idx *repdet.Index) ([]byte, int) {
	if len(idx.Blocks()) == 0 || len(body) < repdet.MinMatch {
		return body, 0
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, 0
	}
	contentRaw, ok := raw["content"]
	if !ok {
		return body, 0
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(contentRaw, &blocks); err != nil {
		return body, 0
	}
	saved := 0
	mutated := false
	for i, blk := range blocks {
		typRaw, ok := blk["type"]
		if !ok {
			continue
		}
		var typ string
		if err := json.Unmarshal(typRaw, &typ); err != nil || typ != "text" {
			continue
		}
		textRaw, ok := blk["text"]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(textRaw, &text); err != nil {
			continue
		}
		rewritten, matches := idx.Rewrite(text)
		if len(matches) == 0 {
			continue
		}
		for _, m := range matches {
			saved += m.Length
		}
		// json.Marshal on a string cannot fail; same for a slice of
		// maps whose values came from json.Unmarshal. Errors elided.
		newText, _ := json.Marshal(rewritten)
		blocks[i]["text"] = newText
		mutated = true
	}
	if !mutated {
		return body, 0
	}
	newContent, _ := json.Marshal(blocks)
	raw["content"] = newContent
	out, _ := json.Marshal(raw)
	return out, saved
}

// passthroughOpenAIWithRepdet mirrors the Anthropic variant for the
// OpenAI / Codex non-streaming wire. It accepts both Chat Completions
// (`choices[].message.content`) and Responses API
// (`output[].content[].text`) shapes. Read / parse failures fall back
// to plain passthrough behaviour.
func (p *Proxy) passthroughOpenAIWithRepdet(w http.ResponseWriter, upstreamResp *http.Response, messages []types.Message, log *slog.Logger) []byte {
	defer upstreamResp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(upstreamResp.Body, maxUpstreamResponseBodySize+1))
	if err != nil {
		slog.Error("read upstream response", "error", err)
		http.Error(w, "upstream read error", http.StatusBadGateway)
		return nil
	}
	if len(body) > maxUpstreamResponseBodySize {
		slog.Error("read upstream response", "error", errUpstreamResponseBodyTooLarge)
		http.Error(w, errUpstreamResponseBodyTooLarge.Error(), http.StatusBadGateway)
		return nil
	}
	out := body
	saved := 0
	idx := buildRepdetIndex(messages)
	if len(idx.Blocks()) > 0 {
		rewritten, savedBytes := rewriteOpenAIResponseBody(body, idx)
		if savedBytes > 0 {
			out = rewritten
			saved = savedBytes
			p.outputReduceCounters.RecordRepdetRewrite(1, saved)
			if log != nil {
				log.Debug("repdet body rewritten", "saved_bytes", saved, "matches_replaced", true, "wire", "openai")
			}
		}
	}
	for k, vv := range upstreamResp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(out)
	return out
}

// rewriteOpenAIResponseBody walks an OpenAI / Codex non-streaming
// response, runs repdet over every text-bearing field across both
// supported response shapes (Chat Completions and Responses API), and
// returns the re-marshalled body. On any parse failure returns the
// original body unchanged with 0 saved.
func rewriteOpenAIResponseBody(body []byte, idx *repdet.Index) ([]byte, int) {
	if len(idx.Blocks()) == 0 || len(body) < repdet.MinMatch {
		return body, 0
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, 0
	}
	saved := 0
	mutated := false

	// Chat Completions: choices[].message.content (string)
	if choicesRaw, ok := raw["choices"]; ok {
		var choices []map[string]json.RawMessage
		if err := json.Unmarshal(choicesRaw, &choices); err == nil {
			for i, ch := range choices {
				msgRaw, ok := ch["message"]
				if !ok {
					continue
				}
				var msg map[string]json.RawMessage
				if err := json.Unmarshal(msgRaw, &msg); err != nil {
					continue
				}
				contentRaw, ok := msg["content"]
				if !ok {
					continue
				}
				var text string
				if err := json.Unmarshal(contentRaw, &text); err != nil {
					// content can be null or an array for tool-call
					// responses - leave those shapes alone.
					continue
				}
				rewritten, matches := idx.Rewrite(text)
				if len(matches) == 0 {
					continue
				}
				for _, m := range matches {
					saved += m.Length
				}
				newText, _ := json.Marshal(rewritten)
				msg["content"] = newText
				newMsg, _ := json.Marshal(msg)
				choices[i]["message"] = newMsg
				mutated = true
			}
			if mutated {
				newChoices, _ := json.Marshal(choices)
				raw["choices"] = newChoices
			}
		}
	}

	// Responses API: output[].content[].text
	if outputRaw, ok := raw["output"]; ok {
		var output []map[string]json.RawMessage
		if err := json.Unmarshal(outputRaw, &output); err == nil {
			outputMutated := false
			for i, item := range output {
				contentRaw, ok := item["content"]
				if !ok {
					continue
				}
				var parts []map[string]json.RawMessage
				if err := json.Unmarshal(contentRaw, &parts); err != nil {
					continue
				}
				partsMutated := false
				for j, part := range parts {
					typRaw, ok := part["type"]
					if !ok {
						continue
					}
					var typ string
					if err := json.Unmarshal(typRaw, &typ); err != nil || (typ != "output_text" && typ != "text") {
						continue
					}
					textRaw, ok := part["text"]
					if !ok {
						continue
					}
					var text string
					if err := json.Unmarshal(textRaw, &text); err != nil {
						continue
					}
					rewritten, matches := idx.Rewrite(text)
					if len(matches) == 0 {
						continue
					}
					for _, m := range matches {
						saved += m.Length
					}
					newText, _ := json.Marshal(rewritten)
					parts[j]["text"] = newText
					partsMutated = true
				}
				if partsMutated {
					newParts, _ := json.Marshal(parts)
					output[i]["content"] = newParts
					outputMutated = true
				}
			}
			if outputMutated {
				newOutput, _ := json.Marshal(output)
				raw["output"] = newOutput
				mutated = true
			}
		}
	}

	if !mutated {
		return body, 0
	}
	out, _ := json.Marshal(raw)
	return out, saved
}
