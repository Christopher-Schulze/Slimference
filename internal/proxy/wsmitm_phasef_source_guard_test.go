package proxy

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestWSPhaseFKnownSourceDeltaToolOutputEmitsFullPassEvidence(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	cfg.Compression.OutputReduce.CodexSavingsPolicyMode = "conservative"
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.rememberToolUseItem(json.RawMessage(`{"type":"function_call","call_id":"call_source","name":"exec_command","arguments":{"cmd":"python report_source.py"}}`))

	sourceOutput := strings.Repeat("package main\n\nfunc main() {\n\tprintln(\"slimference\")\n}\n", 180)
	env := parseWSJSON(t, map[string]any{
		"model":                "gpt-5-codex",
		"previous_response_id": "resp-source-delta",
		"prompt_cache_key":     "source-delta-session",
		"input": []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_source",
			"output":  sourceOutput,
		}},
		"stream": true,
	})
	original := append([]byte(nil), env.Raw...)

	if replace := adapter.handleRequest(&env); replace {
		t.Fatalf("source-like delta tool output must stay byte-identical: %s", env.Raw)
	}
	if !bytes.Equal(env.Raw, original) {
		t.Fatalf("source-like delta tool output changed\nbefore=%s\nafter=%s", original, env.Raw)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("expected one debug summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.Tokens.Saved != 0 || summary.MessagesCompressed != 0 {
		t.Fatalf("source-like delta full-pass must not claim savings: %+v", summary)
	}
	if summary.DebugFacts["wss.request_shape"] != "delta" ||
		summary.DebugFacts["wss.source_tool_results"] != "1" ||
		summary.DebugFacts["wss.tool_result_bytes"] == "" ||
		summary.DebugFacts["wss.output_reduce_disabled_predicate"] != "tool_output_context" {
		t.Fatalf("source delta guard facts missing: %+v", summary.DebugFacts)
	}
	if len(summary.EvidenceDecisions) == 0 {
		t.Fatalf("source-like delta full-pass must not be no-evidence: %+v", summary)
	}
}

func TestAppendWSSSourceToolOutputFullPassEvidenceFillsNoEvidenceGap(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_source",
			Text:         strings.Repeat("package main\n\nfunc main() {\n\tprintln(\"slimference\")\n}\n", 180),
		}},
	}}
	stats := appendWSSSourceToolOutputFullPassEvidence(proxyLayer0Stats{}, wssRequestMeta{
		PreviousResponseID:     "resp-source-delta",
		TurnSeq:                3,
		RemainingTurnsEstimate: 8,
	}, messages, 0.1)

	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "wss_source_tool_output_full_pass", evidence.ActionFullPass) {
		t.Fatalf("source-like no-evidence fallback missing: %+v", stats.EvidenceDecisions)
	}
	if stats.ToolResultBlocks != 1 || stats.ToolResultBytes <= 0 {
		t.Fatalf("source-like fallback should account tool-result bytes: %+v", stats)
	}
	decision := stats.EvidenceDecisions[0]
	if decision.ContentClass != evidence.ContentCode ||
		decision.OriginalTokens <= 0 ||
		decision.SavedTokens != 0 ||
		decision.NetTokens != 0 ||
		decision.Recovery != "fail-open to original source output" {
		t.Fatalf("bad source-like fallback evidence: %+v", decision)
	}
}

func TestAppendWSSSourceToolOutputFullPassEvidenceCoversFullHistorySource(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			Content: []types.ContentBlock{{
				Type:      "tool_use",
				ToolUseID: "call_source",
				ToolName:  "exec_command",
				ToolInput: `{"cmd":"sed -n '1,220p' internal/proxy/wsmitm_phasef.go"}`,
			}},
		},
		{
			Role: "tool",
			Content: []types.ContentBlock{{
				Type:         "tool_result",
				ToolResultID: "call_source",
				Text:         strings.Repeat("package proxy\n\nfunc sourceContext() string {\n\treturn \"slimference\"\n}\n", 160),
			}},
		},
	}
	if wssRequestIsDeltaShape(messages) {
		t.Fatal("test fixture must be full-history shaped")
	}
	stats := appendWSSSourceToolOutputFullPassEvidence(proxyLayer0Stats{}, wssRequestMeta{
		PreviousResponseID:     "resp-source-full-history",
		TurnSeq:                4,
		RemainingTurnsEstimate: 7,
	}, messages, 0.1)

	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "wss_source_tool_output_full_pass", evidence.ActionFullPass) {
		t.Fatalf("full-history source-like no-evidence fallback missing: %+v", stats.EvidenceDecisions)
	}
	if stats.TokensSaved != 0 || stats.BlocksModified != 0 || stats.ToolResultBlocks != 1 || stats.ToolResultBytes <= 0 {
		t.Fatalf("full-history source fallback must be evidence-only: %+v", stats)
	}
}

func TestWSSRequestDebugFactsExposeToolResultBytes(t *testing.T) {
	envelope := "Process exited with code 0\nOutput:\n"
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_empty",
			Text:         envelope,
		}},
	}}
	facts := wssRequestDebugFacts([]byte(`{"input":[]}`), []byte(`{"input":[]}`), messages, proxyLayer0Stats{}, false, "", wssRequestMeta{}, outputreduce.Stats{Reason: "disabled"})
	if facts["wss.tool_results"] != "1" ||
		facts["wss.tool_result_bytes"] != strconv.Itoa(len(envelope)) ||
		facts["wss.tool_result_output_bytes"] != "0" ||
		facts["wss.source_tool_bytes"] != "0" {
		t.Fatalf("bad empty tool-result facts: %+v", facts)
	}
}
