package debug

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorder_RecordAndLast(t *testing.T) {
	t.Parallel()
	r := NewRecorder(10, "")
	s1 := RequestSummary{RequestID: "req-1", Timestamp: time.Now(), Provider: "anthropic"}
	s2 := RequestSummary{RequestID: "req-2", Timestamp: time.Now(), Provider: "openai"}
	r.Record(s1)
	r.Record(s2)

	got := r.Last(10, false)
	if len(got) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(got))
	}
	// Newest first
	if got[0].RequestID != "req-2" {
		t.Errorf("newest should be req-2, got %s", got[0].RequestID)
	}
	if got[1].RequestID != "req-1" {
		t.Errorf("second should be req-1, got %s", got[1].RequestID)
	}
}

func TestRecorder_RingBufferOverflow(t *testing.T) {
	t.Parallel()
	r := NewRecorder(3, "")
	for i := 0; i < 5; i++ {
		r.Record(RequestSummary{RequestID: "req-" + string(rune('0'+i))})
	}
	got := r.Last(10, false)
	// Only 3 kept (ring capacity)
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (ring cap), got %d", len(got))
	}
}

func TestRecorder_Last_WithEntries(t *testing.T) {
	t.Parallel()
	r := NewRecorder(5, "")
	s := RequestSummary{
		RequestID: "req-x",
		Entries:   []DecisionEntry{{SubLayer: "ansi_strip", Action: "compressed"}},
	}
	r.Record(s)

	gotWith := r.Last(1, true)
	if len(gotWith[0].Entries) != 1 {
		t.Error("withEntries=true should include entries")
	}
	gotWithout := r.Last(1, false)
	if len(gotWithout[0].Entries) != 0 {
		t.Error("withEntries=false should strip entries")
	}
}

func TestRecorder_Last_Empty(t *testing.T) {
	t.Parallel()
	r := NewRecorder(5, "")
	got := r.Last(5, false)
	if got != nil {
		t.Errorf("empty recorder should return nil, got %v", got)
	}
}

func TestRecorder_Aggregate(t *testing.T) {
	t.Parallel()
	r := NewRecorder(10, "")
	r.Record(RequestSummary{
		Layer1Breakdown: map[string]SubLayerBreakdown{
			"ansi_strip": {Blocks: 5, Saved: 200},
			"dedup":      {Blocks: 2, Saved: 1000},
		},
	})
	r.Record(RequestSummary{
		Layer1Breakdown: map[string]SubLayerBreakdown{
			"ansi_strip":      {Blocks: 3, Saved: 150},
			"tool_compressor": {Blocks: 1, Saved: 500},
		},
	})

	agg := r.Aggregate()
	if agg["ansi_strip"].Blocks != 8 {
		t.Errorf("ansi_strip blocks: want 8, got %d", agg["ansi_strip"].Blocks)
	}
	if agg["ansi_strip"].Saved != 350 {
		t.Errorf("ansi_strip saved: want 350, got %d", agg["ansi_strip"].Saved)
	}
	if agg["dedup"].Saved != 1000 {
		t.Errorf("dedup saved: want 1000, got %d", agg["dedup"].Saved)
	}
	if agg["tool_compressor"].Saved != 500 {
		t.Errorf("tool_compressor saved: want 500, got %d", agg["tool_compressor"].Saved)
	}
}

func TestBuildMechanismAccounting(t *testing.T) {
	t.Parallel()
	s := RequestSummary{
		BypassReason: "compressed",
		Tokens:       TokenCounts{Original: 1000, Final: 700, Saved: 300},
		Entries: []DecisionEntry{{
			Layer:        0,
			SubLayer:     "hook_compaction",
			TokensBefore: 1000,
			TokensAfter:  650,
			SavedTokens:  350,
			Reason:       "bash",
		}, {
			Layer:        0,
			SubLayer:     "hook_context",
			TokensBefore: 650,
			TokensAfter:  700,
			SavedTokens:  -50,
			Reason:       "metadata",
		}},
		Layer1Breakdown: map[string]SubLayerBreakdown{"json_compact": {Blocks: 2, Saved: 80}},
		Layer2:          Layer2Summary{Applied: true, OriginalTokens: 500, CompressedTokens: 200},
		ContextLedger:   ContextLedgerSummary{TelemetryOnly: true, CommandCapsules: 2, FileCapsules: 1, ReReadCount: 1, OCRLReason: "route_not_eligible", OCRLShadowSavedTokens: 77},
		PromptCache:     PromptCacheSummary{Applied: true, Reason: "stable_prefix", StablePrefixTokens: 400},
		CacheReadTokens: 100,
		ToolPrune:       ToolPruneSummary{Applied: true, Reason: "unused_tools", SavedTokens: 120, Reattached: 20},
		OutputReduce:    OutputReduceSummary{Applied: true, Reason: "profile", AddedTokens: 12},
	}
	got := BuildMechanismAccounting(s)
	byName := map[string]MechanismAccounting{}
	for _, item := range got {
		byName[item.Name] = item
	}
	if byName["hook_compaction"].NetTokens != 350 {
		t.Fatalf("hook compaction net=%d", byName["hook_compaction"].NetTokens)
	}
	if byName["hook_context"].AddedTokens != 50 || byName["hook_context"].NetTokens != -50 {
		t.Fatalf("hook context accounting=%+v", byName["hook_context"])
	}
	if byName["json_compact"].SavedTokens != 80 || byName["layer2_summarization"].SavedTokens != 300 {
		t.Fatalf("layer accounting missing: %+v", byName)
	}
	if byName["context_ledger_shadow"].Count != 3 ||
		byName["context_ledger_shadow"].SavedTokens != 77 ||
		byName["context_ledger_shadow"].NetTokens != 0 ||
		byName["context_ledger_shadow"].Reason != "ocrl_shadow_route_not_eligible" {
		t.Fatalf("context ledger accounting missing: %+v", byName["context_ledger_shadow"])
	}
	if byName["provider_prompt_cache"].NetTokens != 100 || byName["tool_prune"].NetTokens != 100 {
		t.Fatalf("cache/tool accounting missing: %+v", byName)
	}
	if byName["output_reduce_directive"].NetTokens != -12 || byName["request_total"].NetTokens != 288 {
		t.Fatalf("overhead/total accounting missing: %+v", byName)
	}
}

func TestBuildMechanismAccountingAppliedOCRL(t *testing.T) {
	t.Parallel()
	got := BuildMechanismAccounting(RequestSummary{
		ContextLedger: ContextLedgerSummary{
			CommandCapsules:       1,
			OCRLReason:            "applied",
			OCRLCandidateCapsules: 1,
			OCRLArchiveExpansions: 1,
			OCRLShadowSavedTokens: 4410,
		},
	})
	if len(got) != 1 {
		t.Fatalf("mechanisms = %+v", got)
	}
	if got[0].Name != "context_ledger_ocrl" ||
		got[0].Reason != "ocrl_applied" ||
		got[0].SavedTokens != 4410 ||
		got[0].NetTokens != 4410 {
		t.Fatalf("applied OCRL mechanism mismatch: %+v", got[0])
	}
}

func TestBuildMechanismAccountingEdges(t *testing.T) {
	t.Parallel()
	var nilSummary *RequestSummary
	nilSummary.EnsureMechanisms()

	existing := RequestSummary{Mechanisms: []MechanismAccounting{{Name: "kept", NetTokens: 7}}}
	existing.EnsureMechanisms()
	if len(existing.Mechanisms) != 1 || existing.Mechanisms[0].Name != "kept" {
		t.Fatalf("existing mechanisms changed: %+v", existing.Mechanisms)
	}

	got := BuildMechanismAccounting(RequestSummary{
		Entries: []DecisionEntry{{
			SubLayer:     " ",
			TokensBefore: 10,
			TokensAfter:  12,
			SavedTokens:  -2,
		}},
		Layer2:       Layer2Summary{CompressedTokens: 20},
		PromptCache:  PromptCacheSummary{Reason: "miss"},
		ToolPrune:    ToolPruneSummary{Reason: "skip"},
		OutputReduce: OutputReduceSummary{Reason: "skip"},
		Tokens:       TokenCounts{Final: 10},
	})
	byName := map[string]MechanismAccounting{}
	for _, item := range got {
		byName[item.Name] = item
	}
	if byName["unnamed"].AddedTokens != 2 || byName["unnamed"].NetTokens != -2 {
		t.Fatalf("unnamed negative entry=%+v", byName["unnamed"])
	}
	if byName["layer2_summarization"].SavedTokens != 0 || byName["layer2_summarization"].Count != 0 {
		t.Fatalf("negative layer2 saving not clamped: %+v", byName["layer2_summarization"])
	}
	if byName["provider_prompt_cache"].Count != 0 ||
		byName["tool_prune"].Count != 0 ||
		byName["output_reduce_directive"].Count != 0 {
		t.Fatalf("false bool counts not covered: %+v", byName)
	}
}

func TestRecorder_FlushJSONL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	r := NewRecorder(10, path)

	r.Record(RequestSummary{RequestID: "req-flush", Provider: "anthropic"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected JSONL file to be written: %v", err)
	}
	if len(data) == 0 {
		t.Error("JSONL file should not be empty")
	}
	// Should contain the request ID
	if !contains(string(data), "req-flush") {
		t.Errorf("JSONL should contain req-flush, got: %s", string(data))
	}
}

func TestRecorder_FlushJSONL_ExpandsHomeAndCreatesDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := "~/.slimference/debug/decisions.jsonl"
	r := NewRecorder(10, path)

	r.Record(RequestSummary{RequestID: "req-home", Provider: "openai"})

	resolved := filepath.Join(home, ".slimference", "debug", "decisions.jsonl")
	data, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("expected expanded JSONL file to be written: %v", err)
	}
	if !contains(string(data), "req-home") {
		t.Fatalf("expanded JSONL should contain req-home, got: %s", string(data))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("literal tilde path should not be used, stat err=%v", err)
	}
}

func TestNormalizeDecisionsLogPath_BareHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := normalizeDecisionsLogPath("~"); got != home {
		t.Fatalf("bare home path = %q, want %q", got, home)
	}
}

func TestNopRecorder(t *testing.T) {
	t.Parallel()
	var nr NopRecorder
	nr.Record(RequestSummary{RequestID: "x"})
	if got := nr.Last(10, false); got != nil {
		t.Error("NopRecorder.Last should return nil")
	}
	if got := nr.Aggregate(); got != nil {
		t.Error("NopRecorder.Aggregate should return nil")
	}
}

// TestNewRecorder_ZeroCapacityDefaults covers the capacity <= 0 branch in NewRecorder.
func TestNewRecorder_ZeroCapacityDefaults(t *testing.T) {
	t.Parallel()
	r := NewRecorder(0, "")
	// capacity defaults to 100; record many entries to prove ring works
	for i := 0; i < 50; i++ {
		r.Record(RequestSummary{RequestID: "req"})
	}
	got := r.Last(50, false)
	if len(got) != 50 {
		t.Errorf("zero-capacity recorder should default to 100, got %d entries", len(got))
	}
}

// TestNewRecorder_NegativeCapacityDefaults covers the capacity < 0 branch.
func TestNewRecorder_NegativeCapacityDefaults(t *testing.T) {
	t.Parallel()
	r := NewRecorder(-5, "")
	r.Record(RequestSummary{RequestID: "r1"})
	got := r.Last(1, false)
	if len(got) != 1 {
		t.Errorf("negative-capacity recorder should default to 100, got %d entries", len(got))
	}
}

// TestFlushJSONL_UnwritablePath covers the os.OpenFile error branch in flushJSONL.
func TestFlushJSONL_UnwritablePath(t *testing.T) {
	t.Parallel()
	// /dev/null/nonexistent cannot be opened for writing - triggers error branch silently
	r := NewRecorder(5, "/dev/null/nonexistent/decisions.jsonl")
	// Should not panic even with an unwritable path
	r.Record(RequestSummary{RequestID: "no-panic"})
}

func TestFlushJSONL_OpenFileError(t *testing.T) {
	t.Parallel()
	r := NewRecorder(5, t.TempDir())
	r.Record(RequestSummary{RequestID: "open-file-error"})
}

func TestRecorder_FlushJSONL_MarshalErrorSkipsWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	r := NewRecorder(5, path)

	r.Record(RequestSummary{
		RequestID:      "req-nan",
		ProxyLatencyMs: math.NaN(),
	})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("marshal failure should not create decisions log, stat err=%v", err)
	}
}

func TestRecorder_FlushJSONL_WriteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	r := NewRecorder(5, path)
	r.writeLineFn = func(_ *os.File, _ []byte) error {
		return errors.New("write failed")
	}

	r.Record(RequestSummary{RequestID: "req-write-error"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected decisions log file to exist: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("write failure should leave empty file, got %q", string(data))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		findStr(s, sub))
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
