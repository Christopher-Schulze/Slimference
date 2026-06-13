package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
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
		summary.DebugFacts["wss.output_reduce_disabled_predicate"] != "tool_output_context" {
		t.Fatalf("source delta guard facts missing: %+v", summary.DebugFacts)
	}
	if len(summary.EvidenceDecisions) == 0 {
		t.Fatalf("source-like delta full-pass must not be no-evidence: %+v", summary)
	}
}

func TestAppendWSSSourceDeltaToolOutputFullPassEvidenceFillsNoEvidenceGap(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call_source",
			Text:         strings.Repeat("package main\n\nfunc main() {\n\tprintln(\"slimference\")\n}\n", 180),
		}},
	}}
	stats := appendWSSSourceDeltaToolOutputFullPassEvidence(proxyLayer0Stats{}, wssRequestMeta{
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
