package debug

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/evidence"
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
		EvidenceDecisions: []evidence.BlockDecision{{
			Layer:                0,
			Mechanism:            "stale_read",
			ContentClass:         evidence.ContentUnknown,
			SafetyClass:          evidence.SafetyRecoverable,
			Action:               evidence.ActionApplied,
			Reason:               "positive_net_savings",
			OriginalTokens:       400,
			FinalTokens:          120,
			SavedTokens:          280,
			NetTokens:            280,
			FootprintScoreBucket: "mid",
		}, {
			Layer:       2,
			Mechanism:   "provider_prompt_cache",
			SafetyClass: evidence.SafetyExact,
			Action:      evidence.ActionApplied,
			Reason:      "provider_cache_read",
			SavedTokens: 100,
			NetTokens:   100,
		}},
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
	if byName["json_compact"].SavedTokens != 80 {
		t.Fatalf("layer accounting missing: %+v", byName)
	}
	if byName["stale_read"].NetTokens != 280 || byName["stale_read"].Source != "evidence_decision" {
		t.Fatalf("evidence mechanism accounting missing: %+v", byName["stale_read"])
	}
	if byName["stale_read"].FootprintScoreBucket != "mid" {
		t.Fatalf("evidence footprint bucket missing: %+v", byName["stale_read"])
	}
	if byName["provider_prompt_cache"].NetTokens != 100 || byName["tool_prune"].NetTokens != 100 {
		t.Fatalf("cache/tool accounting missing: %+v", byName)
	}
	if byName["output_reduce_directive"].NetTokens != -12 || byName["request_total"].NetTokens != 288 {
		t.Fatalf("overhead/total accounting missing: %+v", byName)
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
	if byName["provider_prompt_cache"].Count != 0 ||
		byName["tool_prune"].Count != 0 ||
		byName["output_reduce_directive"].Count != 0 {
		t.Fatalf("false bool counts not covered: %+v", byName)
	}
}

func TestEnsureEvidenceDecisionsAddsPromptCacheHotZone(t *testing.T) {
	t.Parallel()
	summary := RequestSummary{
		PromptCache:       PromptCacheSummary{Applied: true, Reason: "stable_prefix", StablePrefixTokens: 120},
		CacheReadTokens:   80,
		CacheCreateTokens: 10,
	}
	summary.EnsureEvidenceDecisions()
	summary.EnsureEvidenceDecisions()
	if len(summary.EvidenceDecisions) != 1 {
		t.Fatalf("expected one cache evidence decision, got %+v", summary.EvidenceDecisions)
	}
	decision := summary.EvidenceDecisions[0]
	if decision.Mechanism != "provider_prompt_cache" ||
		decision.SafetyClass != evidence.SafetyExact ||
		decision.Action != evidence.ActionApplied ||
		decision.NetTokens != 70 ||
		len(decision.Signals) != 1 ||
		decision.Signals[0] != evidence.SignalCacheHotZone {
		t.Fatalf("bad cache evidence decision: %+v", decision)
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
	r.writeLineFn = func(_ io.Writer, _ []byte) error {
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

func TestRecorder_FlushJSONL_KeepsHandleOpenAndReopensAfterRotation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	r := NewRecorder(5, path)
	defer r.Close()

	r.Record(RequestSummary{RequestID: "req-1"})
	data, err := os.ReadFile(path)
	if err != nil || !contains(string(data), "req-1") {
		t.Fatalf("first record must be durable immediately: err=%v data=%q", err, data)
	}
	if r.logFile == nil {
		t.Fatal("append handle should stay open between records")
	}

	// External rotation: remove the file, force the stat window to elapse.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	r.logMu.Lock()
	r.logStatAt = time.Time{}
	r.logMu.Unlock()

	r.Record(RequestSummary{RequestID: "req-2"})
	data, err = os.ReadFile(path)
	if err != nil || !contains(string(data), "req-2") {
		t.Fatalf("recorder must reopen after external rotation: err=%v data=%q", err, data)
	}
	if contains(string(data), "req-1") {
		t.Fatalf("rotated file must only hold post-rotation records: %q", data)
	}

	r.Close()
	if r.logFile != nil {
		t.Fatal("Close must release the append handle")
	}
	r.Close() // idempotent
}

func TestRecorder_AttachProviderUsageSupersedesRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	r := NewRecorder(5, path)
	defer r.Close()

	r.Record(RequestSummary{RequestID: "req-a", Tokens: TokenCounts{Original: 1000, Final: 900, Saved: 100}})
	r.Record(RequestSummary{RequestID: "req-b"})

	if !r.AttachProviderUsage("req-a", 29093, 3456, 0, 240) {
		t.Fatal("attach to known request must succeed")
	}
	if r.AttachProviderUsage("req-missing", 1, 1, 1, 1) {
		t.Fatal("attach to unknown request must report false")
	}

	var ring *RequestSummary
	for _, s := range r.Last(5, false) {
		if s.RequestID == "req-a" {
			clone := s
			ring = &clone
		}
	}
	if ring == nil || ring.ProviderInputTokens != 29093 || ring.ProviderCachedTokens != 3456 || ring.ProviderOutputTokens != 240 {
		t.Fatalf("ring entry not enriched: %+v", ring)
	}
	if ring.Flight == nil || ring.Flight.TokenAccounting.ProviderCachedTokens != 3456 {
		t.Fatalf("flight must carry provider cached tokens: %+v", ring.Flight)
	}

	summaries, err := ReplaySession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("replay must dedupe superseded lines, got %d records", len(summaries))
	}
	if summaries[0].RequestID != "req-a" || summaries[0].ProviderCachedTokens != 3456 ||
		summaries[0].Tokens.Saved != 100 {
		t.Fatalf("replay must keep the newest line at the original position: %+v", summaries[0])
	}
}

func TestRecorder_AttachDebugFactsSupersedesRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "decisions.jsonl")
	r := NewRecorder(5, path)
	defer r.Close()

	r.Record(RequestSummary{
		RequestID:  "req-socket",
		DebugFacts: map[string]string{"wss.socket_seq": "7"},
	})

	if !r.AttachDebugFacts("req-socket", map[string]string{
		"wss.socket_close_initiator": "client_eof",
		"wss.socket_age_ms":          "12",
		"":                           "ignored",
	}) {
		t.Fatal("attach to known request must succeed")
	}
	if r.AttachDebugFacts("req-missing", map[string]string{"x": "y"}) {
		t.Fatal("attach to unknown request must report false")
	}

	var ring *RequestSummary
	for _, s := range r.Last(5, false) {
		if s.RequestID == "req-socket" {
			clone := s
			ring = &clone
		}
	}
	if ring == nil ||
		ring.DebugFacts["wss.socket_seq"] != "7" ||
		ring.DebugFacts["wss.socket_close_initiator"] != "client_eof" ||
		ring.DebugFacts["wss.socket_age_ms"] != "12" {
		t.Fatalf("ring entry not enriched: %+v", ring)
	}
	if _, ok := ring.DebugFacts[""]; ok {
		t.Fatalf("empty fact key must be ignored: %+v", ring.DebugFacts)
	}

	summaries, err := ReplaySession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("replay must dedupe superseded lines, got %d records", len(summaries))
	}
	if got := summaries[0].DebugFacts["wss.socket_close_initiator"]; got != "client_eof" {
		t.Fatalf("replayed close initiator=%q", got)
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
