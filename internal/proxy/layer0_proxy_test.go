package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/chunkdedup"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/evidence"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/savingspolicy"
	"github.com/Christopher-Schulze/Slimference/internal/sessions"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestProxyFootprintScoreBucket(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		original  int
		saved     int
		turnSeq   int
		wantScore int
		want      string
	}{
		{name: "none", original: 0, saved: 0, turnSeq: 1, wantScore: 0, want: ""},
		{name: "early high", original: 9000, saved: 5000, turnSeq: 2, wantScore: 40000, want: "high"},
		{name: "mid session mid", original: 3000, saved: 3000, turnSeq: 6, wantScore: 12000, want: "mid"},
		{name: "late low", original: 3000, saved: 3000, turnSeq: 12, wantScore: 3000, want: "low"},
		{name: "full pass uses original", original: 1200, saved: 0, turnSeq: 1, wantScore: 9600, want: "mid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proxyFootprintScore(tt.original, tt.saved, tt.turnSeq); got != tt.wantScore {
				t.Fatalf("score=%d want %d", got, tt.wantScore)
			}
			if got := proxyFootprintScoreBucket(tt.original, tt.saved, tt.turnSeq); got != tt.want {
				t.Fatalf("bucket=%q want %q", got, tt.want)
			}
		})
	}
}

func TestProxyFootprintScoreUsesCachedPriceRatio(t *testing.T) {
	t.Parallel()
	if got := proxyFootprintRemainingTurnsEstimate(2); got != 70 {
		t.Fatalf("early remaining turns = %d want 70", got)
	}
	if got := proxyFootprintRemainingTurnsEstimate(6); got != 30 {
		t.Fatalf("mid remaining turns = %d want 30", got)
	}
	if got := proxyFootprintRemainingTurnsEstimate(12); got != 0 {
		t.Fatalf("late remaining turns = %d want 0", got)
	}
	if got := proxyFootprintScoreWithCachedPriceRatio(1000, 0, 2, 0.10); got != 8000 {
		t.Fatalf("default cached-price score = %d want 8000", got)
	}
	if got := proxyFootprintScoreWithCachedPriceRatio(1000, 0, 2, 0.20); got != 15000 {
		t.Fatalf("higher cached-price score = %d want 15000", got)
	}
	if got := proxyFootprintScoreWithCachedPriceRatio(1000, 0, 12, 0.20); got != 1000 {
		t.Fatalf("late score = %d want 1000", got)
	}
	if got := proxyFootprintScoreWithEstimate(1000, 0, 12, 25, 0.20); got != 6000 {
		t.Fatalf("explicit remaining-turn estimate score = %d want 6000", got)
	}
}

func TestProxyLayer0FullPassEvidenceUsesCheapTokenEstimate(t *testing.T) {
	body := strings.Repeat("deterministic guarded output row\n", 32)
	decision := proxyLayer0EvidenceDecision(
		"python generate_report.py",
		body,
		"",
		proxyLayer0MechanismCapturedOut,
		evidence.ActionFullPass,
		"latency_budget_full_context",
		0,
		0,
		savingspolicy.CodexWorkloadCommand,
		2,
		12,
		0.1,
	)
	wantTokens := len(body) / 4
	if decision.OriginalTokens != wantTokens ||
		decision.FinalTokens != wantTokens ||
		decision.SavedTokens != 0 ||
		decision.NetTokens != 0 ||
		decision.FootprintScore <= 0 ||
		decision.FootprintScoreBucket == "" {
		t.Fatalf("full-pass evidence must own the guarded block without claiming savings: %+v wantTokens=%d", decision, wantTokens)
	}
}

func TestProxyLayer0CacheBustClassKeyHelpers(t *testing.T) {
	t.Parallel()

	if got := proxyLayer0CacheBustClassKeyForString("", evidence.ContentSearch); got != "" {
		t.Fatalf("empty mechanism key=%q, want empty", got)
	}
	if got := proxyLayer0CacheBustClassKeyForString(string(proxyLayer0MechanismCapturedOut), ""); got != "captured_output:unknown" {
		t.Fatalf("empty class key=%q, want unknown fallback", got)
	}
	searchKeyA := proxyLayer0CacheBustClassKey(proxyLayer0MechanismCapturedOut, `rg -n "needle" internal`, "internal/a.go:1:needle\ninternal/b.go:2:needle\n")
	searchKeyB := proxyLayer0CacheBustClassKey(proxyLayer0MechanismCapturedOut, `rg -n "other" internal`, "internal/a.go:1:other\ninternal/b.go:2:other\n")
	if !strings.HasPrefix(searchKeyA, "captured_output:search:key=") || strings.Contains(searchKeyA, "needle") {
		t.Fatalf("search cache-bust key must be hashed and content-free, got %q", searchKeyA)
	}
	if searchKeyA == searchKeyB {
		t.Fatalf("different search identities must not share cache-bust keys: %q", searchKeyA)
	}
	commandKeyA := proxyLayer0CacheBustClassKey(proxyLayer0MechanismRepeatedOut, "wc -l internal/a.go", "10 internal/a.go\n")
	commandKeyB := proxyLayer0CacheBustClassKey(proxyLayer0MechanismRepeatedOut, "wc -l internal/b.go", "10 internal/b.go\n")
	if !strings.HasPrefix(commandKeyA, "repeated_tool_output:plain:cmd=") ||
		strings.Contains(commandKeyA, "internal/a.go") ||
		strings.Contains(commandKeyA, "wc") {
		t.Fatalf("command cache-bust key must be hashed and content-free, got %q", commandKeyA)
	}
	if commandKeyA == commandKeyB {
		t.Fatalf("different command identities must not share cache-bust keys: %q", commandKeyA)
	}
	obsoleteKeyA := proxyLayer0CacheBustClassKey(proxyLayer0MechanismObsoletePrune, "cat internal/a.go", "old a contents\n")
	obsoleteKeyB := proxyLayer0CacheBustClassKey(proxyLayer0MechanismObsoletePrune, "cat internal/b.go", "old b contents\n")
	if !strings.HasPrefix(obsoleteKeyA, "obsolete_prune:plain:cmd=") || obsoleteKeyA == obsoleteKeyB {
		t.Fatalf("obsolete-prune keys must be command-scoped, got %q and %q", obsoleteKeyA, obsoleteKeyB)
	}
	if got := proxyHistoryMutationEvidenceClassFromKeys(map[string]struct{}{obsoleteKeyA: {}}); got != evidence.ContentPlain {
		t.Fatalf("history evidence class from key=%q, want plain", got)
	}
	if got := proxyHistoryMutationEvidenceClassFromKeys(nil); got != evidence.ContentUnknown {
		t.Fatalf("empty history evidence class=%q, want unknown", got)
	}
	if got := proxyHistoryMutationEvidenceClassFromKeys(map[string]struct{}{"malformed": {}}); got != evidence.ContentUnknown {
		t.Fatalf("malformed history evidence class=%q, want unknown", got)
	}
	if got := proxyHistoryMutationEvidenceClassFromKeys(map[string]struct{}{
		obsoleteKeyA: {},
		proxyLayer0CacheBustClassKeyForMechanism(proxyLayer0MechanismObsoletePrune, evidence.ContentCode): {},
	}); got != evidence.ContentUnknown {
		t.Fatalf("mixed history evidence classes must fall back to unknown, got %q", got)
	}
	historyDecision := proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismObsoletePrune, evidence.ActionApplied, "positive_net_savings", evidence.ContentPlain, 40, 10, 2, 0.1)
	if historyDecision.ContentClass != evidence.ContentPlain || historyDecision.FootprintScore <= 0 || historyDecision.FootprintScoreBucket == "" {
		t.Fatalf("history mutation evidence must preserve classified content and footprint: %+v", historyDecision)
	}
	unknownHistoryDecision := proxyHistoryMutationEvidenceDecision(proxyLayer0MechanismObsoletePrune, evidence.ActionFullPass, "guard", "", 40, 40, 2, 0.1)
	if unknownHistoryDecision.ContentClass != evidence.ContentUnknown {
		t.Fatalf("empty history mutation content class must fall back to unknown: %+v", unknownHistoryDecision)
	}
	if got := proxyLayer0CacheBustGeneralClassKey(commandKeyA); got != "repeated_tool_output:plain" {
		t.Fatalf("general key from command key=%q, want repeated_tool_output:plain", got)
	}
	if got := proxyLayer0CacheBustGeneralClassKey("captured_output:search"); got != "captured_output:search" {
		t.Fatalf("general key without suffix=%q, want unchanged", got)
	}
	if got := proxyLayer0CacheBustCommandIdentityKey(""); got != "" {
		t.Fatalf("empty command identity key=%q, want empty", got)
	}
	cdCommandKey := proxyLayer0CacheBustCommandIdentityKey("cd /repo/a && wc -l internal/a.go")
	if cdCommandKey == "" || strings.Contains(cdCommandKey, "/repo/a") || strings.Contains(cdCommandKey, "internal/a.go") {
		t.Fatalf("cd command identity key must be hashed and content-free, got %q", cdCommandKey)
	}
	keys := cloneProxyLayer0CacheBustClassKeys(map[string]struct{}{
		"repeated_tool_output:plain": {},
		"":                           {},
		searchKeyA:                   {},
	})
	if got := proxyLayer0CacheBustClassKeysString(keys); got != searchKeyA+",repeated_tool_output:plain" {
		t.Fatalf("sorted class keys=%q", got)
	}
	mergedKeys := mergeProxyLayer0CacheBustClassKeys(map[string]struct{}{"existing:plain": {}}, map[string]struct{}{"": {}, obsoleteKeyA: {}})
	if _, ok := mergedKeys["existing:plain"]; !ok {
		t.Fatalf("merge lost existing key: %+v", mergedKeys)
	}
	if _, ok := mergedKeys[obsoleteKeyA]; !ok || len(mergedKeys) != 2 {
		t.Fatalf("merge did not add exactly the non-empty source key: %+v", mergedKeys)
	}
	stats := proxyLayer0Stats{EvidenceDecisions: []evidence.BlockDecision{
		{Mechanism: string(proxyLayer0MechanismCapturedOut), ContentClass: evidence.ContentSearch, Action: evidence.ActionApplied},
		{Mechanism: string(proxyLayer0MechanismRepeatedOut), ContentClass: evidence.ContentPlain, Action: evidence.ActionFullPass},
	}}
	fromStats := proxyLayer0CacheBustClassKeysFromStats(stats)
	if _, ok := fromStats["captured_output:search"]; !ok || len(fromStats) != 1 {
		t.Fatalf("applied-only class keys mismatch: %+v", fromStats)
	}
	specificStats := proxyLayer0Stats{
		CacheBustClassKeys: map[string]struct{}{searchKeyA: {}},
		EvidenceDecisions: []evidence.BlockDecision{
			{Mechanism: string(proxyLayer0MechanismCapturedOut), ContentClass: evidence.ContentSearch, Action: evidence.ActionApplied},
		},
	}
	specificFromStats := proxyLayer0CacheBustClassKeysFromStats(specificStats)
	if _, ok := specificFromStats[searchKeyA]; !ok || len(specificFromStats) != 1 {
		t.Fatalf("specific stats should not add broad search fallback: %+v", specificFromStats)
	}
}

func TestProxyLayer0CacheBustCommandIdentityDemotion(t *testing.T) {
	t.Parallel()

	commandA := "wc -l internal/a.go"
	commandB := "wc -l internal/b.go"
	outputA := "10 internal/a.go\n"
	outputB := "10 internal/b.go\n"
	commandKeyA := proxyLayer0CacheBustClassKey(proxyLayer0MechanismRepeatedOut, commandA, outputA)
	generalKey := proxyLayer0CacheBustClassKeyForMechanism(proxyLayer0MechanismRepeatedOut, evidence.ContentPlain)
	req := codexLayer0Request{
		CacheBustDemotedMechanisms: proxyLayer0MechanismMaskFor(proxyLayer0MechanismRepeatedOut),
		CacheBustDemotedClassKeys:  map[string]struct{}{commandKeyA: {}},
	}
	if !proxyLayer0CacheBustCandidateDemoted(req, commandA, outputA, proxyLayer0MechanismRepeatedOut) {
		t.Fatal("matching command identity should be cache-bust demoted")
	}
	if proxyLayer0CacheBustCandidateDemoted(req, commandB, outputB, proxyLayer0MechanismRepeatedOut) {
		t.Fatal("different command identity must not inherit a narrow cache-bust demotion")
	}

	req.CacheBustDemotedClassKeys = map[string]struct{}{generalKey: {}}
	if !proxyLayer0CacheBustCandidateDemoted(req, commandA, outputA, proxyLayer0MechanismRepeatedOut) ||
		!proxyLayer0CacheBustCandidateDemoted(req, commandB, outputB, proxyLayer0MechanismRepeatedOut) {
		t.Fatal("legacy broad class key should still demote every matching content class")
	}
}

func TestApplyProxyLayer0Branches(t *testing.T) {
	t.Parallel()

	unchanged := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "not a tool result"}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", Text: "plain output"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-echo", ToolName: "shell", ToolInput: `{"command":"echo ok"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-echo", Text: "ok\n"}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolUseID: "missing", Text: "ok\n"}}},
	}
	out, saved := applyProxyLayer0(unchanged)
	if saved != 0 || &out[0] != &unchanged[0] {
		t.Fatalf("unchanged messages should be returned as-is, saved=%d", saved)
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(unchanged, "", nil)
	if stats.TokensSaved != 0 || stats.BlocksModified != 0 || stats.ToolResultBlocks != 3 ||
		stats.CommandResolvedBlocks != 1 || stats.CommandUnresolvedBlocks != 2 ||
		stats.ToolUseUnresolvedBlocks != 2 {
		t.Fatalf("unchanged stats mismatch: %+v", stats)
	}

	var status strings.Builder
	for i := 0; i < 80; i++ {
		status.WriteString(" M file")
		status.WriteString(string(rune('a' + i%26)))
		status.WriteString(".go\n")
	}
	changed := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	out, saved = applyProxyLayer0(changed)
	if saved <= 0 {
		t.Fatalf("expected savings, got %d", saved)
	}
	if out[1].Content[0].Text == changed[1].Content[0].Text || !strings.Contains(out[1].Content[0].Text, "[git status]") {
		t.Fatalf("tool result not compacted: %q", out[1].Content[0].Text)
	}
	if changed[1].Content[0].Text == out[1].Content[0].Text {
		t.Fatal("original message slice should not be mutated")
	}
	_, stats = applyProxyLayer0WithSessionAndToolUsesDetailed(changed, "", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.TokensSaved <= 0 ||
		stats.BlocksModified != 1 || stats.CapturedOutputBlocks != 1 ||
		stats.ReadDeltaBlocks != 0 || stats.CodexExecEnvelopeBlocks != 0 {
		t.Fatalf("captured-output stats mismatch: %+v", stats)
	}
}

func TestReduceCodexLayer0CopyOnWriteClonesOnlyMutatedMessage(t *testing.T) {
	t.Parallel()

	var status strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&status, " M cow_file_%02d.go\n", i)
	}
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-echo", ToolName: "shell", ToolInput: `{"command":"echo ok"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-echo", Text: "ok\n"}}},
	}

	result := reduceCodexLayer0(codexLayer0Request{Messages: messages})
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 {
		t.Fatalf("expected one compacted block, stats=%+v", result.Stats)
	}
	if result.Messages[1].Content[0].Text == messages[1].Content[0].Text ||
		messages[1].Content[0].Text != status.String() {
		t.Fatal("mutated output must not mutate the original message")
	}
	if &result.Messages[1].Content[0] == &messages[1].Content[0] {
		t.Fatal("mutated message content must be cloned")
	}
	for _, idx := range []int{0, 2, 3} {
		if &result.Messages[idx].Content[0] != &messages[idx].Content[0] {
			t.Fatalf("unmutated message %d should keep its content backing array", idx)
		}
	}
}

func TestApplyProxyLayer0WithRememberedToolUse(t *testing.T) {
	t.Parallel()

	var status strings.Builder
	for i := 0; i < 80; i++ {
		status.WriteString("?? synthetic_")
		status.WriteString(string(rune('a' + i%26)))
		status.WriteString(".go\n")
	}
	messages := []types.Message{
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	remembered := map[string]types.ContentBlock{
		"call-status": {Type: "tool_use", ToolUseID: "call-status", ToolName: "exec_command", ToolInput: `{"cmd":"git -C /tmp/slimf status --short"}`},
	}
	out, saved := applyProxyLayer0WithSessionAndToolUses(messages, "sess", remembered)
	if saved <= 0 {
		t.Fatalf("expected remembered tool use to produce savings, got %d", saved)
	}
	if !strings.Contains(out[0].Content[0].Text, "[git status]") || strings.Contains(out[0].Content[0].Text, "synthetic_z.go") {
		t.Fatalf("remembered tool use did not compact status output: %q", out[0].Content[0].Text)
	}
}

func TestReduceCodexLayer0WSSCapturedOutputCarriesArchiveReference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var status strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&status, "?? synthetic_%02d.go\n", i)
	}
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-captured",
	})
	text := result.Messages[1].Content[0].Text
	if result.Stats.CapturedOutputBlocks != 1 || result.Stats.TokensSaved <= 0 {
		t.Fatalf("expected WSS captured-output savings, stats=%+v text=%q", result.Stats, text)
	}
	if !strings.Contains(text, "[git status]") || !strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("WSS captured output must be compact and recoverable: %q", text)
	}
}

func TestReduceCodexLayer0CacheBustDemotionNarrowsToClassKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var status strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&status, "?? cache_bust_narrow_%02d.go\n", i)
	}
	commandLine := "git status --short"
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	captured := proxyLayer0MechanismMaskFor(proxyLayer0MechanismCapturedOut)
	searchKey := proxyLayer0CacheBustClassKeyForMechanism(proxyLayer0MechanismCapturedOut, evidence.ContentSearch)
	narrow := reduceCodexLayer0(codexLayer0Request{
		Route:                      codexLayer0RouteWSSPhaseF,
		Messages:                   messages,
		SessionID:                  "sess-cache-bust-narrow",
		CacheBustDemotedMechanisms: proxyLayer0MechanismMaskFor(proxyLayer0MechanismCapturedOut),
		CacheBustDemotedClassKeys:  map[string]struct{}{searchKey: {}},
	})
	if narrow.Stats.TokensSaved <= 0 || narrow.Stats.CapturedOutputBlocks != 1 ||
		!strings.Contains(narrow.Messages[1].Content[0].Text, "[git status]") {
		t.Fatalf("class-narrow cache-bust demotion should not block unrelated captured output: stats=%+v text=%q", narrow.Stats, narrow.Messages[1].Content[0].Text)
	}
	if hasEvidenceDecision(narrow.Stats.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "cache_bust_guard", evidence.ActionFullPass) {
		t.Fatalf("unrelated class must not emit cache-bust full-pass evidence: %+v", narrow.Stats.EvidenceDecisions)
	}

	statusKey := proxyLayer0CacheBustClassKey(proxyLayer0MechanismCapturedOut, commandLine, status.String())
	guarded := reduceCodexLayer0(codexLayer0Request{
		Route:                      codexLayer0RouteWSSPhaseF,
		Messages:                   messages,
		SessionID:                  "sess-cache-bust-narrow",
		CacheBustDemotedMechanisms: captured,
		CacheBustDemotedClassKeys:  map[string]struct{}{statusKey: {}},
	})
	if guarded.Stats.TokensSaved != 0 || guarded.Stats.BlocksModified != 0 ||
		guarded.Messages[1].Content[0].Text != status.String() {
		t.Fatalf("matching class key must preserve original output: stats=%+v text=%q", guarded.Stats, guarded.Messages[1].Content[0].Text)
	}
	if !hasEvidenceDecision(guarded.Stats.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "cache_bust_guard", evidence.ActionFullPass) {
		t.Fatalf("matching class key must emit cache-bust evidence: %+v", guarded.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0CacheBustDemotionNarrowsToSearchIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	messagesFor := func(id, command, output string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: output}}},
		}
	}
	commandA := `cd /repo/a && rg -n needle internal`
	commandB := `cd /repo/a && rg -n other internal`
	outputA := proxyWSSSearchOutputFixture("needle", 80)
	outputB := proxyWSSSearchOutputFixture("other", 80)
	captured := proxyLayer0MechanismMaskFor(proxyLayer0MechanismCapturedOut)
	seedA := reduceCodexLayer0(codexLayer0Request{
		Route:                    codexLayer0RouteWSSPhaseF,
		Messages:                 messagesFor("call-search-a-seed", commandA, outputA),
		SessionID:                "sess-cache-bust-search-identity-seed",
		WSSSearchMutationAllowed: true,
	})
	if seedA.Stats.CapturedOutputBlocks != 1 || seedA.Stats.TokensSaved <= 0 || len(seedA.Stats.CacheBustClassKeys) != 1 {
		t.Fatalf("seed search should compact and record one cache-bust key: stats=%+v", seedA.Stats)
	}
	demotedA := cloneProxyLayer0CacheBustClassKeys(seedA.Stats.CacheBustClassKeys)

	unrelated := reduceCodexLayer0(codexLayer0Request{
		Route:                      codexLayer0RouteWSSPhaseF,
		Messages:                   messagesFor("call-search-b", commandB, outputB),
		SessionID:                  "sess-cache-bust-search-identity",
		WSSSearchMutationAllowed:   true,
		CacheBustDemotedMechanisms: captured,
		CacheBustDemotedClassKeys:  demotedA,
	})
	if unrelated.Stats.CapturedOutputBlocks != 1 || unrelated.Stats.TokensSaved <= 0 {
		t.Fatalf("different search identity should still compact: stats=%+v text=%q", unrelated.Stats, unrelated.Messages[1].Content[0].Text)
	}
	if hasEvidenceDecision(unrelated.Stats.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "cache_bust_guard", evidence.ActionFullPass) {
		t.Fatalf("different search identity must not emit cache-bust evidence: %+v", unrelated.Stats.EvidenceDecisions)
	}

	matching := reduceCodexLayer0(codexLayer0Request{
		Route:                      codexLayer0RouteWSSPhaseF,
		Messages:                   messagesFor("call-search-a", commandA, outputA),
		SessionID:                  "sess-cache-bust-search-identity",
		WSSSearchMutationAllowed:   true,
		CacheBustDemotedMechanisms: captured,
		CacheBustDemotedClassKeys:  demotedA,
	})
	if matching.Stats.TokensSaved != 0 || matching.Stats.BlocksModified != 0 || matching.Messages[1].Content[0].Text != outputA {
		t.Fatalf("matching search identity must full-pass: stats=%+v text=%q", matching.Stats, matching.Messages[1].Content[0].Text)
	}
	if !hasEvidenceDecision(matching.Stats.EvidenceDecisions, proxyLayer0MechanismCapturedOut, "cache_bust_guard", evidence.ActionFullPass) {
		t.Fatalf("matching search identity must emit cache-bust evidence: %+v", matching.Stats.EvidenceDecisions)
	}
}

func TestArchiveProxyCapturedOutputArchivesCodexPayload(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	payload := strings.Repeat("src/example.go:42:stable match\n", 40)
	original := "Chunk ID: volatile\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 500\nOutput:\n" + payload
	compacted := "Chunk ID: volatile\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 500\nOutput:\n[rg] 40 match(es)"
	out, ok := archiveProxyCapturedOutput("sess-archive-payload", "rg -n stable src", compacted, original)
	if !ok {
		t.Fatal("expected captured output archive")
	}
	const marker = "uri=local-archive://"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		t.Fatalf("archive marker missing: %q", out)
	}
	id := strings.TrimSpace(strings.TrimSuffix(out[idx+len("uri="):], "]"))
	_, archived, err := contentarchive.Get(contentarchive.DefaultDir(home), id)
	if err != nil {
		t.Fatal(err)
	}
	if string(archived) != payload {
		t.Fatalf("Codex exec archive should store stable payload only, got %q", string(archived[:min(len(archived), 120)]))
	}
}

func TestReduceCodexLayer0WSSCapturedOutputFailsOpenWithoutSession(t *testing.T) {
	t.Parallel()

	var status strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&status, "?? synthetic_%02d.go\n", i)
	}
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-status", ToolName: "shell", ToolInput: `{"command":"git status --short"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-status", Text: status.String()}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:    codexLayer0RouteWSSPhaseF,
		Messages: messages,
	})
	if result.Stats.BlocksModified != 0 || result.Messages[1].Content[0].Text != status.String() {
		t.Fatalf("missing WSS session must fail open, stats=%+v text=%q", result.Stats, result.Messages[1].Content[0].Text)
	}
}

func TestProxyResolveToolUseBranches(t *testing.T) {
	t.Parallel()

	block := types.ContentBlock{ToolResultID: "r1", ToolUseID: "u1"}
	if got := proxyResolveToolUse(block, nil); got != block {
		t.Fatal("nil index should return original block")
	}
	if got := proxyResolveToolUse(types.ContentBlock{}, map[string]types.ContentBlock{"x": {ToolName: "shell"}}); got.ToolName != "" {
		t.Fatal("missing id should return original empty block")
	}
	if got := proxyResolveToolUse(types.ContentBlock{ToolResultID: "missing"}, map[string]types.ContentBlock{"x": {ToolName: "shell"}}); got.ToolName != "" {
		t.Fatal("unknown id should return original block")
	}
	use := types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"pwd"}`}
	if got := proxyResolveToolUse(types.ContentBlock{ToolUseID: "u1"}, map[string]types.ContentBlock{"u1": use}); got.ToolName != "shell" {
		t.Fatalf("fallback ToolUseID did not resolve: %#v", got)
	}
}

func TestProxyLayer0CommandLineVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   types.ContentBlock
		want string
	}{
		{"empty", types.ContentBlock{ToolName: "shell"}, ""},
		{"json_string_shell", types.ContentBlock{ToolName: "shell", ToolInput: `"git status --short"`}, "git status --short"},
		{"command", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"go test ./..."}`}, "go test ./..."},
		{"cmd", types.ContentBlock{ToolName: "shell", ToolInput: `{"cmd":"cargo test"}`}, "cargo test"},
		{"command_line", types.ContentBlock{ToolName: "shell", ToolInput: `{"command_line":"pnpm test"}`}, "pnpm test"},
		{"cmdline", types.ContentBlock{ToolName: "local_shell_call", ToolInput: `{"cmdline":"go vet ./..."}`}, "go vet ./..."},
		{"shell_command", types.ContentBlock{ToolName: "bash_command", ToolInput: `{"shell_command":"git status --short"}`}, "git status --short"},
		{"shellCommand", types.ContentBlock{ToolName: "bash_command", ToolInput: `{"shellCommand":"git diff --stat"}`}, "git diff --stat"},
		{"commandLine", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"commandLine":"rg TODO docs"}`}, "rg TODO docs"},
		{"git_workdir_string", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"cmd":"git status --short","workdir":"/repo/project"}`}, "git -C /repo/project status --short"},
		{"git_workdir_array", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["git","diff","--stat"],"cwd":"/repo/project"}`}, "git -C /repo/project diff --stat"},
		{"bash_lc_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"/opt/homebrew/bin/bash -lc 'git status --short .'"}`}, "git status --short ."},
		{"slimference_filter_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter -- git status --short ."}`}, "git status --short ."},
		{"slimference_filter_stream_wrapper", types.ContentBlock{ToolName: "shell", ToolInput: `{"command":"slimference filter --stream -- rg TODO docs"}`}, "rg TODO docs"},
		{"command_array", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"command":["/bin/sh","-c","git status --short"]}`}, "git status --short"},
		{"cmd_args", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"cmd_args":["sh","-c","go test ./..."]}`}, "go test ./..."},
		{"command_args", types.ContentBlock{ToolName: "terminal.exec", ToolInput: `{"command_args":["rg","needle","path with space"]}`}, `rg needle "path with space"`},
		{"bash_lc_array_read", types.ContentBlock{ToolName: "container.exec", ToolInput: `{"command":["bash","-lc","cat /tmp/t248-target.md"]}`}, "cat /tmp/t248-target.md"},
		{"bash_lc_array_relative_read_workdir", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workdir":"/repo/project"}`}, "cat /repo/project/docs/todo.md"},
		{"bash_lc_array_relative_read_workingDirectory", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cat docs/todo.md"],"workingDirectory":"/repo/project"}`}, "cat /repo/project/docs/todo.md"},
		{"bash_lc_cd_relative_sed", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cd /repo/project && sed -n '10,20p' docs/todo.md"]}`}, "sed -n 10,20p /repo/project/docs/todo.md"},
		{"bash_lc_cd_relative_nl_sed", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cd /repo/project && nl -ba docs/todo.md | sed -n '10,20p'"]}`}, "nl -ba /repo/project/docs/todo.md | sed -n 10,20p"},
		{"bash_lc_cd_git", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"command":["bash","-lc","cd /repo/project && git status --short"]}`}, "git -C /repo/project status --short"},
		{"head_relative_read_workdir", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"cmd":"head -n 20 internal/proxy/layer0_proxy.go","workdir":"/repo/project"}`}, "head -n 20 /repo/project/internal/proxy/layer0_proxy.go"},
		{"nl_sed_relative_read_workdir", types.ContentBlock{ToolName: "exec_command", ToolInput: `{"cmd":"nl -ba internal/proxy/layer0_proxy.go | sed -n '10,20p'","workdir":"/repo/project"}`}, "nl -ba /repo/project/internal/proxy/layer0_proxy.go | sed -n 10,20p"},
		{"argv", types.ContentBlock{ToolName: "exec", ToolInput: `{"argv":["go","test","./pkg with space"]}`}, `go test "./pkg with space"`},
		{"args", types.ContentBlock{ToolName: "run_command", ToolInput: `{"args":["rg","needle","path with space"]}`}, `rg needle "path with space"`},
		{"read_path", types.ContentBlock{ToolName: "Read", ToolInput: `{"path":"pkg/file with space.go"}`}, `cat "pkg/file with space.go"`},
		{"read_path_workdir", types.ContentBlock{ToolName: "Read", ToolInput: `{"path":"pkg/file with space.go","cwd":"/repo/project"}`}, `cat "/repo/project/pkg/file with space.go"`},
		{"read_uri", types.ContentBlock{ToolName: "file.read", ToolInput: `{"uri":"docs/todo.md","workingDir":"/repo/project"}`}, `cat /repo/project/docs/todo.md`},
		{"read_target", types.ContentBlock{ToolName: "mcp.read_file", ToolInput: `{"target":"docs/todo.md","current_working_directory":"/repo/project"}`}, `cat /repo/project/docs/todo.md`},
		{"read_source_path", types.ContentBlock{ToolName: "local_file_read", ToolInput: `{"source_path":"docs/todo.md"}`}, `cat docs/todo.md`},
		{"read_file_path", types.ContentBlock{ToolName: "read_file", ToolInput: `{"file_path":"internal/proxy/provider.go"}`}, `cat internal/proxy/provider.go`},
		{"view_absolute_path", types.ContentBlock{ToolName: "view_file", ToolInput: `{"absolute_path":"/tmp/file with space.go"}`}, `cat "/tmp/file with space.go"`},
		{"raw_read_path", types.ContentBlock{ToolName: "open", ToolInput: `"docs/todo.md"`}, `cat docs/todo.md`},
		{"non_shell_raw", types.ContentBlock{ToolName: "other", ToolInput: "git status"}, ""},
		{"invalid_json_shell", types.ContentBlock{ToolName: "terminal", ToolInput: "git status --short"}, "git status --short"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyLayer0CommandLine(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestCompactProxyLayer0TextCodexExecEnvelope(t *testing.T) {
	t.Parallel()

	var status strings.Builder
	for i := 0; i < 80; i++ {
		status.WriteString(" M internal/proxy/file_")
		status.WriteString(string(rune('a' + i%26)))
		status.WriteString(".go\n")
	}
	envelope := "Chunk ID: abc123\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 800\nOutput:\n" + status.String()
	out, changed := compactProxyLayer0Text("git status --short .", envelope, filter.FileReadContext{Mode: "scan"})
	if !changed {
		t.Fatal("expected Codex exec envelope to compact")
	}
	if !strings.Contains(out, "Process exited with code 0") || !strings.Contains(out, "Output:\n[git status]") {
		t.Fatalf("envelope header or compacted body missing: %q", out)
	}
	if _, changed, mechanism := compactProxyLayer0TextDetailed("git status --short .", envelope, filter.FileReadContext{Mode: "scan"}); !changed || mechanism != proxyLayer0MechanismCodexEnvelope {
		t.Fatalf("expected codex envelope mechanism, changed=%v mechanism=%q", changed, mechanism)
	}
	if strings.Contains(out, "file_z.go") {
		t.Fatalf("uncompacted payload leaked: %q", out)
	}
	if _, changed := compactProxyLayer0Text("git status --short .", "plain\nOutput:\n M one.go\n", filter.FileReadContext{Mode: "scan"}); changed {
		t.Fatal("non-Codex envelope should not compact via envelope fallback")
	}
	if header, payload, ok := splitCodexExecEnvelope("Process exited with code 0\nOutput:\nbody"); !ok || header != "Process exited with code 0\nOutput:\n" || payload != "body" {
		t.Fatalf("splitCodexExecEnvelope mismatch header=%q payload=%q ok=%v", header, payload, ok)
	}
	if header, payload, ok := splitCodexExecEnvelope("Process exited with code 0\r\nOutput:\r\nbody"); !ok || header != "Process exited with code 0\r\nOutput:\r\n" || payload != "body" {
		t.Fatalf("splitCodexExecEnvelope CRLF mismatch header=%q payload=%q ok=%v", header, payload, ok)
	}
	if _, changed := compactCodexExecEnvelope("echo ok", "Process exited with code 0\nOutput:\nok\n", filter.FileReadContext{Mode: "scan"}); changed {
		t.Fatal("envelope with unchanged payload should not compact")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nNo output marker"); ok {
		t.Fatal("missing output marker should not split")
	}
	if _, _, ok := splitCodexExecEnvelope("Process exited with code 0\nOutput:\n"); ok {
		t.Fatal("empty payload should not split")
	}
}

func TestCompactProxyLayer0TextCDWrappedCargoCheckEnvelope(t *testing.T) {
	t.Parallel()

	payload := strings.Join([]string{
		"    Checking slimference-cargo-proof v0.1.0 (/tmp/slimference-cargo-proof)",
		"     Running CARGO=/opt/homebrew/bin/cargo CARGO_CRATE_NAME=slimference_cargo_proof rustc --crate-name slimference_cargo_proof src/main.rs",
		"error[E0308]: mismatched types",
		" --> src/main.rs:2:22",
		"  |",
		"2 |     let value: i32 = \"not an integer\";",
		"  |                ---   ^^^^^^^^^^^^^^^^ expected `i32`, found `&str`",
		"  |                |",
		"  |                expected due to this",
		"",
		"error: could not compile `slimference-cargo-proof` (bin \"slimference-cargo-proof\") due to 1 previous error",
		"",
	}, "\n")
	envelope := "Chunk ID: cargo\nWall time: 0.0000 seconds\nProcess exited with code 101\nOriginal token count: 1200\nOutput:\n" + payload

	out, changed, mechanism := compactProxyLayer0TextDetailed("cd /tmp/slimference-cargo-proof && cargo check -vv", envelope, filter.FileReadContext{Mode: "scan"})
	if !changed || mechanism != proxyLayer0MechanismCodexEnvelope {
		t.Fatalf("expected cd-wrapped cargo envelope savings, changed=%v mechanism=%q out=%q", changed, mechanism, out)
	}
	for _, want := range []string{
		"Process exited with code 101",
		"[cargo check] FAILED",
		"error[E0308]: mismatched types",
		"let value: i32 = \"not an integer\"",
		"expected due to this",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("compacted cargo output missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "Running CARGO=") {
		t.Fatalf("verbose cargo runner noise leaked: %q", out)
	}
}

func TestReduceCodexLayer0InfersCodexEnvelopeCommandWhenToolUseMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("=== RUN   TestSlimferenceFailure\n")
	payload.WriteString("    live_proof_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	payload.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	payload.WriteString("FAIL\texample.test/liveproof\t0.015s\n")
	envelope := "Chunk ID: inferred\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()

	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  []types.Message{{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "missing-call", Text: envelope}}}},
		SessionID: "sess-inferred-go-test",
	})
	text := result.Messages[0].Content[0].Text
	if result.Stats.CommandResolvedBlocks != 1 || result.Stats.CommandUnresolvedBlocks != 0 ||
		result.Stats.CodexExecEnvelopeBlocks != 1 || result.Stats.TokensSaved <= 0 {
		t.Fatalf("expected inferred go-test envelope savings, stats=%+v text=%q", result.Stats, text)
	}
	if !strings.Contains(text, "SLIMFERENCE_TEST_FAILURE_SENTINEL") ||
		strings.Contains(text, "TestPassing089") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("inferred compaction must preserve failure and archive original payload: %q", text)
	}
}

func TestReduceCodexLayer0InfersCodexEnvelopeCommandForResolvedWrapper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("=== RUN   TestSlimferenceWrapperFailure\n")
	payload.WriteString("    wrapper_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	payload.WriteString("--- FAIL: TestSlimferenceWrapperFailure (0.00s)\n")
	payload.WriteString("FAIL\texample.test/wrapper\t0.015s\n")
	envelope := "Chunk ID: wrapper\nWall time: 0.0000 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" + payload.String()

	result := reduceCodexLayer0(codexLayer0Request{
		Route: codexLayer0RouteWSSPhaseF,
		Messages: []types.Message{{Role: "tool", Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "call-wrapper",
			Text:         envelope,
		}}}},
		SessionID: "sess-wrapper-go-test",
		RememberedToolUse: map[string]types.ContentBlock{
			"call-wrapper": {
				Type:      "tool_use",
				ToolUseID: "call-wrapper",
				ToolName:  "exec_command",
				ToolInput: `{"cmd":"/tmp/slimference-wrapper/run.sh"}`,
			},
		},
	})
	text := result.Messages[0].Content[0].Text
	if result.Stats.CommandResolvedBlocks != 1 || result.Stats.CommandUnresolvedBlocks != 0 ||
		result.Stats.CodexExecEnvelopeBlocks != 1 || result.Stats.TokensSaved <= 0 {
		t.Fatalf("expected wrapper go-test envelope savings, stats=%+v text=%q", result.Stats, text)
	}
	if !strings.Contains(text, "SLIMFERENCE_TEST_FAILURE_SENTINEL") ||
		strings.Contains(text, "TestPassing089") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("wrapper compaction must preserve failure and archive original payload: %q", text)
	}
}

func TestProxyInferCommandLineFromToolResult(t *testing.T) {
	t.Parallel()

	var diffStat strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&diffStat, " internal/proxy/generated/very/deep/path/file_%02d.go | %d +++++-----\n", i, i+1)
	}
	diffStat.WriteString(" 40 files changed, 820 insertions(+), 410 deletions(-)\n")

	var showStat strings.Builder
	showStat.WriteString("commit a1b2c3d4\nAuthor: A <a@example.com>\nDate:   Mon Apr 7 10:30:00 2025 +0000\n\n    change summary\n\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&showStat, " internal/proxy/generated/very/deep/path/file_%02d.go | %d +++++-----\n", i, i+1)
	}
	showStat.WriteString(" 40 files changed, 820 insertions(+), 410 deletions(-)\n")

	var nameStatus strings.Builder
	for i := 0; i < 40; i++ {
		status := "M"
		if i%3 == 0 {
			status = "A"
		}
		fmt.Fprintf(&nameStatus, "%s\tinternal/proxy/generated/very/deep/path/file_%02d.go\n", status, i)
	}

	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "go_test",
			text: "Process exited with code 1\nOutput:\n=== RUN   TestThing\n--- FAIL: TestThing (0.00s)\nFAIL\texample.test/pkg\t0.01s\n",
			want: "go test",
		},
		{
			name: "search",
			text: "Process exited with code 0\nOutput:\ninternal/a.go:10:needle\ninternal/b.go:20:needle\npkg/c.go:30:needle\n",
			want: "rg",
		},
		{
			name: "git_status",
			text: "Process exited with code 0\nOutput:\n M a.go\n?? b.go\nA  c.go\n",
			want: "git status --short",
		},
		{
			name: "git_diff_stat",
			text: "Process exited with code 0\nOutput:\n" + diffStat.String(),
			want: "git diff --stat",
		},
		{
			name: "git_show_stat",
			text: "Process exited with code 0\nOutput:\n" + showStat.String(),
			want: "git show --stat",
		},
		{
			name: "git_diff_name_status",
			text: "Process exited with code 0\nOutput:\n" + nameStatus.String(),
			want: "git diff --name-status",
		},
		{
			name: "git_log_oneline",
			text: "Process exited with code 0\nOutput:\na1b2c3d first change\nb2c3d4e second change\nc3d4e5f third change\n",
			want: "git log --oneline -n 200",
		},
		{
			name: "wc",
			text: "Process exited with code 0\nOutput:\n  123 internal/proxy/layer0_proxy.go\n   45 internal/proxy/wsmitm_phasef.go\n  168 total\n",
			want: "wc -l",
		},
		{
			name: "plain_path_list",
			text: "Process exited with code 0\nOutput:\n" + wssListingFixture(40),
			want: proxyInferredPlainPathListCommandLine,
		},
		{
			name: "git_diff_stat_without_summary",
			text: "Process exited with code 0\nOutput:\n internal/proxy/a.go | 10 +++++-----\n",
			want: "",
		},
		{
			name: "ambiguous",
			text: "Process exited with code 0\nOutput:\nthis is just prose with a:colon\nand another line\n",
			want: "",
		},
		{
			name: "search_style_path_matches",
			text: "Process exited with code 0\nOutput:\n" + proxyWSSSearchOutputFixture("needle", 20),
			want: "rg",
		},
		{
			name: "not_envelope",
			text: "=== RUN   TestThing\n--- FAIL: TestThing\n",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := proxyInferCommandLineFromToolResult(tc.text); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestApplyProxyLayer0WithSessionReadDelta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	first := proxyReadMessages(strings.Repeat("line one\n", 80))
	out, saved := applyProxyLayer0WithSession(first, "sess-read")
	if strings.Contains(out[1].Content[0].Text, "status=unchanged") {
		t.Fatalf("first read must not become a read-cache reference, saved=%d", saved)
	}

	second := proxyReadMessages(strings.Repeat("line one\n", 80))
	out, saved = applyProxyLayer0WithSession(second, "sess-read")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "status=unchanged") {
		t.Fatalf("unchanged reread should become reference, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(second, "sess-read", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.ReadDeltaAttempts != 1 ||
		stats.ReadDeltaMisses != 0 || stats.TokensSaved <= 0 || stats.BlocksModified != 1 || stats.ReadDeltaBlocks != 1 {
		t.Fatalf("read-delta stats mismatch: %+v", stats)
	}
	if len(stats.CacheEvents) != 1 || stats.CacheEvents[0].Action != proxyLayer0CacheHit || stats.CacheEvents[0].Reason != "unchanged" {
		t.Fatalf("read-delta cache hit event mismatch: %+v", stats.CacheEvents)
	}

	changed := proxyReadMessages(strings.Repeat("line one\n", 80) + "line two\n")
	out, saved = applyProxyLayer0WithSession(changed, "sess-read")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "+line two") || !strings.Contains(out[1].Content[0].Text, "uri=local-archive://") {
		t.Fatalf("changed reread should become delta, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0SuppressesCollapsedReadKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var payload strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&payload, "suppression unique payload line %03d with nonrepeating value %08x\n", i, i*7919+17)
	}
	messages := proxyReadMessages(payload.String())
	if result := reduceCodexLayer0(codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-suppress",
	}); result.Stats.ReadDeltaMisses != 1 || result.Stats.TokensSaved != 0 {
		t.Fatalf("first read should seed readcache without savings: %+v", result.Stats)
	}

	suppressed := reduceCodexLayer0(codexLayer0Request{
		Messages:          messages,
		SessionID:         "sess-suppress",
		SuppressedToolKey: map[string]struct{}{"read:main.go": {}},
	})
	if suppressed.Stats.TokensSaved != 0 || suppressed.Stats.ReadDeltaAttempts != 0 ||
		suppressed.Stats.ReadDeltaBlocks != 0 || suppressed.Stats.BlocksModified != 0 {
		t.Fatalf("suppressed read key should full-pass without read-delta: %+v", suppressed.Stats)
	}
	if suppressed.Messages[1].Content[0].Text != messages[1].Content[0].Text {
		t.Fatalf("suppressed read changed model-facing text: %q", suppressed.Messages[1].Content[0].Text)
	}

	unsuppressed := reduceCodexLayer0(codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-suppress",
	})
	if unsuppressed.Stats.ReadDeltaBlocks != 1 || unsuppressed.Stats.TokensSaved <= 0 ||
		!strings.Contains(unsuppressed.Messages[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("unsuppressed reread should still collapse: %+v text=%q",
			unsuppressed.Stats, unsuppressed.Messages[1].Content[0].Text)
	}
}

func TestApplyProxyLayer0WithSessionRepeatedNonFileOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	body := strings.Repeat("deterministic report row with unchanged non-file data\n", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-report", ToolName: "exec_command", ToolInput: `{"cmd":"python generate_report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-report", Text: body}}},
	}
	out, saved := applyProxyLayer0WithSession(messages, "sess-output")
	if saved != 0 || strings.Contains(out[1].Content[0].Text, "previous emitted output") {
		t.Fatalf("first non-file output must not collapse, saved=%d text=%q", saved, out[1].Content[0].Text)
	}

	out, saved = applyProxyLayer0WithSession(messages, "sess-output")
	if saved <= 0 || !strings.Contains(out[1].Content[0].Text, "kind=tool-output") ||
		!strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("repeated non-file output should collapse, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-output", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.TokensSaved <= 0 ||
		stats.BlocksModified != 1 || stats.RepeatedOutputBlocks != 1 ||
		stats.ReadDeltaBlocks != 0 || stats.CapturedOutputBlocks != 0 || stats.CodexExecEnvelopeBlocks != 0 {
		t.Fatalf("repeated-output stats mismatch: %+v", stats)
	}
	if len(stats.CacheEvents) != 1 || stats.CacheEvents[0].Mechanism != "repeated_output" ||
		stats.CacheEvents[0].Action != proxyLayer0CacheHit || stats.CacheEvents[0].Reason != "unchanged" {
		t.Fatalf("repeated-output cache hit event mismatch: %+v", stats.CacheEvents)
	}
}

func TestReduceCodexLayer0WSSSearchSameMatchSetPassesThrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	command := `cd /repo/search && rg -n needle src`
	output := func(reverse bool) string {
		var lines []string
		for i := 1; i <= 30; i++ {
			lines = append(lines, fmt.Sprintf("src/b.go:%d:needle beta context %s", i+100, strings.Repeat("detail ", 30)))
			lines = append(lines, fmt.Sprintf("src/a.go:%d:needle alpha context %s", i, strings.Repeat("detail ", 30)))
		}
		if reverse {
			for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
				lines[i], lines[j] = lines[j], lines[i]
			}
			lines = append([]string{"Chunk ID: second", "Wall time: 0.0001 seconds"}, lines...)
		} else {
			lines = append([]string{"Chunk ID: first", "Wall time: 0.0003 seconds"}, lines...)
		}
		return strings.Join(lines, "\n") + "\n"
	}
	messagesFor := func(callID, text string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: callID, ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: callID, Text: text}}},
		}
	}
	seed := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messagesFor("search-a", output(false)),
		SessionID: "sess-search-match-set",
	})
	if seed.Stats.RepeatedOutputBlocks != 0 || seed.Stats.CapturedOutputBlocks != 0 || seed.Stats.TokensSaved != 0 || seed.Stats.BlocksModified != 0 {
		t.Fatalf("WSS search seed must pass through until live-safe: %+v", seed.Stats)
	}
	out := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messagesFor("search-b", output(true)),
		SessionID: "sess-search-match-set",
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.RepeatedOutputBlocks != 0 || out.Stats.CapturedOutputBlocks != 0 || out.Stats.TokensSaved != 0 || out.Stats.BlocksModified != 0 ||
		strings.Contains(text, "kind=search-output") ||
		strings.Contains(text, "[rg]") ||
		!strings.Contains(text, "src/a.go:1:needle alpha context") {
		t.Fatalf("WSS same search match-set must remain original text: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0WSSSearchChangedMatchSetPassesThrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	command := `cd /repo/search && rg -n needle src`
	output := func(start, end int, extra string) string {
		var lines []string
		for i := start; i <= end; i++ {
			lines = append(lines, fmt.Sprintf("src/a.go:%d:needle alpha context %s", i, strings.Repeat("detail ", 30)))
		}
		if extra != "" {
			lines = append(lines, extra+" "+strings.Repeat("detail ", 30))
		}
		lines = append([]string{"Chunk ID: changed", "Wall time: 0.0001 seconds"}, lines...)
		return strings.Join(lines, "\n") + "\n"
	}
	messagesFor := func(callID, text string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: callID, ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: callID, Text: text}}},
		}
	}
	seed := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messagesFor("search-delta-a", output(1, 80, "")),
		SessionID: "sess-search-match-delta",
	})
	if seed.Stats.CapturedOutputBlocks != 0 || seed.Stats.RepeatedOutputBlocks != 0 || seed.Stats.TokensSaved != 0 || seed.Stats.BlocksModified != 0 {
		t.Fatalf("WSS changed-set seed must pass through until live-safe: %+v", seed.Stats)
	}
	out := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messagesFor("search-delta-b", output(6, 80, "src/c.go:90:needle gamma context")),
		SessionID: "sess-search-match-delta",
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.RepeatedOutputBlocks != 0 || out.Stats.CapturedOutputBlocks != 0 || out.Stats.TokensSaved != 0 || out.Stats.BlocksModified != 0 ||
		strings.Contains(text, "kind=search-output") ||
		strings.Contains(text, "[context-archive kind=full-output uri=local-archive://") ||
		!strings.Contains(text, "src/c.go:90:needle gamma context") {
		t.Fatalf("WSS changed search match-set must remain original text: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0WSSSearchOutputInferencePassesThrough(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var output strings.Builder
	output.WriteString("Chunk ID: live-search\nWall time: 0.0001 seconds\nProcess exited with code 0\nOutput:\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&output, "docs/tasks/TASK-%04d.md:%d:needle with enough detail to group\n", i, i+1)
	}
	original := output.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-shell-search", ToolName: "exec_command", ToolInput: `{"cmd":"/bin/bash -lc 'rg -n needle docs/tasks'"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-shell-search", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-search-inferred",
	})
	if result.Stats.BlocksModified != 0 || result.Stats.TokensSaved != 0 || result.Messages[1].Content[0].Text != original {
		t.Fatalf("WSS inferred search output must pass through, stats=%+v text=%q", result.Stats, result.Messages[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0WSSSearchLatencyBudgetKeepsRiskGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	command := `cd /repo/search && rg -n needle src`
	original := proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg-latency", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg-latency", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                 codexLayer0RouteWSSPhaseF,
		Messages:              messages,
		SessionID:             "sess-wss-search-latency",
		LatencyBudgetExceeded: true,
	})
	if result.Stats.BlocksModified != 0 || result.Stats.TokensSaved != 0 ||
		result.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("WSS search latency gate must keep search risk guard: stats=%+v text=%q evidence=%+v",
			result.Stats, result.Messages[1].Content[0].Text, result.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0WSSSearchLatencyBudgetKeepsTextRiskGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	original := proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-wrapper-latency", ToolName: "exec_command", ToolInput: `{"cmd":"python grep_wrapper.py --pattern needle"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-wrapper-latency", Text: original}}},
	}
	req := codexLayer0Request{
		Route:                 codexLayer0RouteWSSPhaseF,
		Messages:              messages,
		SessionID:             "sess-wss-wrapper-search-latency",
		LatencyBudgetExceeded: true,
	}
	seed := reduceCodexLayer0(req)
	if seed.Stats.BlocksModified != 0 || seed.Stats.TokensSaved != 0 ||
		seed.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(seed.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("WSS search text-risk gate must stay active under latency: stats=%+v text=%q evidence=%+v",
			seed.Stats, seed.Messages[1].Content[0].Text, seed.Stats.EvidenceDecisions)
	}

	repeated := reduceCodexLayer0(req)
	if repeated.Stats.BlocksModified != 0 || repeated.Stats.TokensSaved != 0 ||
		repeated.Stats.RepeatedOutputBlocks != 0 ||
		repeated.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(repeated.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("WSS search text-risk gate must also block latency repeated-output collapse: stats=%+v text=%q evidence=%+v",
			repeated.Stats, repeated.Messages[1].Content[0].Text, repeated.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0WSSFindPathListCompactsWithoutSearchProof(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	var output strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&output, ".reconc/audit/%04d.jsonl\n", i)
	}
	original := output.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-find-reconc", ToolName: "exec_command", ToolInput: `{"cmd":"find .reconc -maxdepth 4 -type f"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-find-reconc", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-find-reconc",
	})
	text := result.Messages[1].Content[0].Text
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 || result.Stats.CapturedOutputBlocks != 1 {
		t.Fatalf("WSS find path-list output should compact, stats=%+v text=%q", result.Stats, text)
	}
	if result.Stats.WSSSearchRiskBlocks != 0 ||
		result.Stats.WSSSearchProofAllowed != 0 ||
		result.Stats.WSSSearchProofBlocked != 0 {
		t.Fatalf("find path-list must not be accounted as WSS search risk: %+v", result.Stats)
	}
	if !strings.Contains(text, "[find paths]") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(text, ".reconc/audit/") ||
		!strings.Contains(text, "0059.jsonl") ||
		strings.Contains(text, ".reconc/audit/0059.jsonl") {
		t.Fatalf("find output was not archive-backed path-list compaction: %s", text)
	}
	if proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("find path-list must not trip search risk gate: %+v", result.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0WSSRgFilesPathListCompactsWithoutSearchProof(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	var output strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&output, "internal/proxy/generated/deep/path/file_%03d.go\n", i)
	}
	original := output.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-rg-files", ToolName: "exec_command", ToolInput: `{"cmd":"rg --files -g '*.go' internal/proxy"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-rg-files", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-rg-files",
	})
	text := result.Messages[1].Content[0].Text
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 || result.Stats.CapturedOutputBlocks != 1 {
		t.Fatalf("WSS rg --files path-list output should compact, stats=%+v text=%q", result.Stats, text)
	}
	if !strings.Contains(text, "[rg --files paths]") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(text, "internal/proxy/generated/deep/path/") ||
		!strings.Contains(text, "file_089.go") ||
		strings.Contains(text, "internal/proxy/generated/deep/path/file_089.go") {
		t.Fatalf("rg --files output was not archive-backed path-list compaction: %s", text)
	}
	if proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("rg --files path-list must not trip search risk gate: %+v", result.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0WSSFdPathListCompactsWithoutSearchProof(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	var output strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&output, "internal/proxy/generated/deep/path/file_%03d.go\n", i)
	}
	original := output.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-fd-files", ToolName: "exec_command", ToolInput: `{"cmd":"fd .go internal/proxy"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-fd-files", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-fd-files",
	})
	text := result.Messages[1].Content[0].Text
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 || result.Stats.CapturedOutputBlocks != 1 {
		t.Fatalf("WSS fd path-list output should compact, stats=%+v text=%q", result.Stats, text)
	}
	if result.Stats.WSSSearchRiskBlocks != 0 ||
		result.Stats.WSSSearchProofAllowed != 0 ||
		result.Stats.WSSSearchProofBlocked != 0 {
		t.Fatalf("fd path-list must not be accounted as WSS search risk: %+v", result.Stats)
	}
	if !strings.Contains(text, "[fd paths]") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(text, "internal/proxy/generated/deep/path/") ||
		!strings.Contains(text, "file_089.go") ||
		strings.Contains(text, "internal/proxy/generated/deep/path/file_089.go") {
		t.Fatalf("fd output was not archive-backed path-list compaction: %s", text)
	}
	if proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("fd path-list must not trip search risk gate: %+v", result.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0WSSSearchProofAllowsNamedDirectSearch(t *testing.T) {
	home := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	command := `cd /repo/search && rg -n needle src`
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg", Text: proxyWSSSearchOutputFixture("needle", 90)}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                    codexLayer0RouteWSSPhaseF,
		Messages:                 messages,
		SessionID:                "sess-wss-search-proof",
		WSSSearchMutationAllowed: true,
	})
	text := result.Messages[1].Content[0].Text
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 || result.Stats.CapturedOutputBlocks != 1 ||
		!strings.Contains(text, "[rg]") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(text, "src/file_089.go:90:needle") {
		t.Fatalf("proof-allowed named WSS search should compact with archive recovery, stats=%+v text=%q", result.Stats, text)
	}
}

func TestReduceCodexLayer0WSSSearchProofAllowsNamedDeltaSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	command := `cd /repo/search && rg -n needle src`
	original := proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg-delta", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg-delta", Text: original}}},
	}
	blocked := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-search-delta-blocked",
		StatefulDeltaMutationBlocked: true,
	})
	if blocked.Stats.BlocksModified != 0 || blocked.Stats.TokensSaved != 0 || blocked.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(blocked.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("unproofed delta search must stay byte-equal, stats=%+v text=%q", blocked.Stats, blocked.Messages[1].Content[0].Text)
	}
	if blocked.Stats.WSSSearchRiskBlocks != 1 || blocked.Stats.WSSSearchProofAllowed != 0 ||
		blocked.Stats.WSSSearchProofBlocked != 1 || blocked.Stats.WSSSearchProofReasons["latch_disabled"] != 1 {
		t.Fatalf("unproofed delta search should record precise latch-disabled proof telemetry: %+v", blocked.Stats)
	}

	proofed := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-search-delta-proof",
		WSSSearchMutationAllowed:     true,
		StatefulDeltaMutationBlocked: true,
	})
	proofedText := proofed.Messages[1].Content[0].Text
	if proofed.Stats.BlocksModified != 1 || proofed.Stats.TokensSaved <= 0 || proofed.Stats.CapturedOutputBlocks != 1 ||
		proofedText == original ||
		!strings.Contains(proofedText, "[rg]") ||
		!strings.Contains(proofedText, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(proofedText, "src/file_079.go:80:needle") ||
		proxyLayer0EvidenceHasReason(proofed.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("proofed named delta search should compact through search-cap latch, stats=%+v text=%q", proofed.Stats, proofedText)
	}
	if proofed.Stats.WSSSearchRiskBlocks != 1 || proofed.Stats.WSSSearchProofAllowed != 1 ||
		proofed.Stats.WSSSearchProofBlocked != 0 || len(proofed.Stats.WSSSearchProofReasons) != 0 {
		t.Fatalf("proofed named delta search should record allowed search proof telemetry: %+v", proofed.Stats)
	}
}

func TestReduceCodexLayer0WSSSearchProofAllowsNamedHeadPipelineDeltaSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	command := `cd /repo/search && rg -n needle src | head -200`
	original := proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg-head-delta", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg-head-delta", Text: original}}},
	}
	proofed := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-search-head-delta-proof",
		WSSSearchMutationAllowed:     true,
		StatefulDeltaMutationBlocked: true,
	})
	proofedText := proofed.Messages[1].Content[0].Text
	if proofed.Stats.BlocksModified != 1 || proofed.Stats.TokensSaved <= 0 || proofed.Stats.CapturedOutputBlocks != 1 ||
		proofedText == original ||
		!strings.Contains(proofedText, "[rg]") ||
		!strings.Contains(proofedText, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(proofedText, "src/file_079.go:80:needle") ||
		proxyLayer0EvidenceHasReason(proofed.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("proofed named delta head-limited search should compact through search-cap latch, stats=%+v text=%q", proofed.Stats, proofedText)
	}
	if proofed.Stats.WSSSearchRiskBlocks != 1 || proofed.Stats.WSSSearchProofAllowed != 1 ||
		proofed.Stats.WSSSearchProofBlocked != 0 || len(proofed.Stats.WSSSearchProofReasons) != 0 {
		t.Fatalf("proofed named delta head-limited search should record allowed search proof telemetry: %+v", proofed.Stats)
	}
	if key := proxyLayer0QualityToolKey(command); key != "search:rg\t-n\tneedle\t/repo/search/src\t|head-lines=200" {
		t.Fatalf("head-limited search key must stay distinct from full search key: %q", key)
	}
}

func TestReduceCodexLayer0WSSSearchProofRejectsUnsafePipelineDeltaSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	command := `cd /repo/search && rg -n needle src | sed -n '1,20p'`
	original := proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg-sed-delta", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg-sed-delta", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-search-sed-delta-proof",
		WSSSearchMutationAllowed:     true,
		StatefulDeltaMutationBlocked: true,
	})
	if result.Stats.BlocksModified != 0 || result.Stats.TokensSaved != 0 || result.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("unsafe search pipeline must stay byte-equal behind risk gate, stats=%+v text=%q", result.Stats, result.Messages[1].Content[0].Text)
	}
	if result.Stats.WSSSearchRiskBlocks != 1 || result.Stats.WSSSearchProofAllowed != 0 ||
		result.Stats.WSSSearchProofBlocked != 1 || result.Stats.WSSSearchProofReasons["workload_not_search"] != 1 {
		t.Fatalf("unsafe search pipeline should record workload-not-search proof telemetry: %+v", result.Stats)
	}
}

func TestReduceCodexLayer0WSSSearchDeltaProofPrefersCapturedOutputSavings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	command := `cd /repo/search && rg -n needle src`
	original := proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg-delta-repeated", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg-delta-repeated", Text: original}}},
	}
	seed := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-search-delta-repeated",
	})
	if seed.Stats.RepeatedOutputBlocks != 0 || seed.Stats.TokensSaved != 0 {
		t.Fatalf("first search output should seed repeated-output only: %+v", seed.Stats)
	}

	proofed := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-search-delta-repeated",
		WSSSearchMutationAllowed:     true,
		StatefulDeltaMutationBlocked: true,
	})
	proofedText := proofed.Messages[1].Content[0].Text
	if proofed.Stats.BlocksModified != 1 || proofed.Stats.TokensSaved <= 0 ||
		proofed.Stats.CapturedOutputBlocks != 1 || proofed.Stats.RepeatedOutputBlocks != 0 ||
		proofedText == original ||
		!strings.Contains(proofedText, "[rg]") ||
		!strings.Contains(proofedText, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(proofedText, "src/file_079.go:80:needle") ||
		proxyLayer0EvidenceHasReason(proofed.Stats.EvidenceDecisions, "wss_stateful_delta_mutation_proof_gate") ||
		proxyLayer0EvidenceHasReason(proofed.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("proofed stateful delta search should compact through captured-output latch, stats=%+v text=%q", proofed.Stats, proofedText)
	}
}

func TestReduceCodexLayer0WSSSearchProofAllowsCodexEnvelopeDeltaCapturedOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	command := `cd /repo/search && rg -n needle src`
	original := "Chunk ID: live-regression\nWall time: 0.0001 seconds\nProcess exited with code 0\nOutput:\n" +
		proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg-envelope-delta", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg-envelope-delta", Text: original}}},
	}
	blocked := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-search-envelope-delta-blocked",
		StructuredMutationBlocked:    true,
		StatefulDeltaMutationBlocked: true,
	})
	if blocked.Stats.BlocksModified != 0 || blocked.Stats.TokensSaved != 0 || blocked.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(blocked.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("unproofed delta search envelope must stay blocked, stats=%+v text=%q evidence=%+v", blocked.Stats, blocked.Messages[1].Content[0].Text, blocked.Stats.EvidenceDecisions)
	}

	proofed := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-search-envelope-delta-proof",
		StructuredMutationBlocked:    true,
		WSSSearchMutationAllowed:     true,
		StatefulDeltaMutationBlocked: true,
	})
	proofedText := proofed.Messages[1].Content[0].Text
	if proofed.Stats.BlocksModified != 1 || proofed.Stats.TokensSaved <= 0 ||
		proofed.Stats.CapturedOutputBlocks != 1 || proofed.Stats.CodexExecEnvelopeBlocks != 0 ||
		proofedText == original ||
		strings.HasPrefix(proofedText, "Chunk ID: live-regression") ||
		!strings.Contains(proofedText, "[rg]") ||
		!strings.Contains(proofedText, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(proofedText, "src/file_079.go:80:needle") ||
		proxyLayer0EvidenceHasReason(proofed.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("proofed stateful delta search envelope should compact through captured-output latch, stats=%+v text=%q", proofed.Stats, proofedText)
	}
}

func TestReduceCodexLayer0WSSSearchProofTreatsNonDeltaCodexEnvelopeAsCaptured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	command := `cd /repo/search && rg -n needle src`
	original := "Chunk ID: safe-search-envelope\nWall time: 0.0001 seconds\nProcess exited with code 0\nOutput:\n" +
		proxyWSSSearchOutputFixture("needle", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-rg-envelope-full", ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-rg-envelope-full", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                     codexLayer0RouteWSSPhaseF,
		Messages:                  messages,
		SessionID:                 "sess-wss-search-envelope-full-proof",
		StructuredMutationBlocked: true,
		WSSSearchMutationAllowed:  true,
	})
	text := result.Messages[1].Content[0].Text
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 ||
		result.Stats.CapturedOutputBlocks != 1 || result.Stats.CodexExecEnvelopeBlocks != 0 ||
		text == original ||
		strings.HasPrefix(text, "Chunk ID: safe-search-envelope") ||
		!strings.Contains(text, "[rg]") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(text, "src/file_079.go:80:needle") {
		t.Fatalf("proofed non-delta search envelope should use captured-output path only, stats=%+v text=%q", result.Stats, text)
	}
}

func TestReduceCodexLayer0WSSSearchProofKeepsNonSearchEnvelopeDeltaBlocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var payload strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&payload, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	payload.WriteString("PASS\nok\texample.test/nonsearch\t0.015s\n")
	original := "Chunk ID: non-search-envelope\nWall time: 0.0001 seconds\nProcess exited with code 0\nOutput:\n" + payload.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-wss-go-test-envelope-delta", ToolName: "exec_command", ToolInput: `{"cmd":"go test ./..."}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-wss-go-test-envelope-delta", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-non-search-envelope-delta-proof",
		StructuredMutationBlocked:    true,
		WSSSearchMutationAllowed:     true,
		StatefulDeltaMutationBlocked: true,
	})
	if result.Stats.BlocksModified != 0 || result.Stats.TokensSaved != 0 ||
		result.Stats.CodexExecEnvelopeBlocks != 0 || result.Stats.CapturedOutputBlocks != 0 ||
		result.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_stateful_delta_mutation_proof_gate") {
		t.Fatalf("proof latch must not open non-search codex envelope delta, stats=%+v text=%q", result.Stats, result.Messages[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0WSSSearchProofRejectsInferredSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	payload := proxyWSSSearchOutputFixture("needle", 60)
	original := "Chunk ID: inferred\nWall time: 0.0001 seconds\nProcess exited with code 0\nOutput:\n" + payload
	messages := []types.Message{
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-inferred-search", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                    codexLayer0RouteWSSPhaseF,
		Messages:                 messages,
		SessionID:                "sess-wss-search-inference-proof",
		WSSSearchMutationAllowed: true,
	})
	if result.Stats.BlocksModified != 0 || result.Stats.TokensSaved != 0 || result.Messages[0].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("inferred WSS search must stay blocked, stats=%+v text=%q", result.Stats, result.Messages[0].Content[0].Text)
	}
	if result.Stats.WSSSearchRiskBlocks != 1 || result.Stats.WSSSearchProofAllowed != 0 ||
		result.Stats.WSSSearchProofBlocked != 1 || result.Stats.WSSSearchProofReasons["tool_use_unbound"] != 1 {
		t.Fatalf("inferred WSS search should record tool-use-unbound proof telemetry: %+v", result.Stats)
	}
}

func TestReduceCodexLayer0WSSInferredPlainPathListCompactsWithoutSearchProof(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var listing strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&listing, "internal/proxy/generated/deep/path/file_%03d.go\n", i)
	}
	original := "Chunk ID: inferred-paths\nWall time: 0.0001 seconds\nProcess exited with code 0\nOutput:\n" + listing.String()
	messages := []types.Message{
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-inferred-paths", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-wss-plain-path-list-inference",
	})
	text := result.Messages[0].Content[0].Text
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 || result.Stats.CodexExecEnvelopeBlocks != 1 ||
		result.Stats.WSSSearchRiskBlocks != 0 || result.Stats.WSSSearchProofAllowed != 0 || result.Stats.WSSSearchProofBlocked != 0 {
		t.Fatalf("metadata-less plain path-list should compact without search proof telemetry: stats=%+v text=%q", result.Stats, text)
	}
	if !strings.Contains(text, "[plain paths]") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(text, "internal/proxy/generated/deep/path/") ||
		!strings.Contains(text, "file_079.go") ||
		strings.Contains(text, "internal/proxy/generated/deep/path/file_079.go") {
		t.Fatalf("metadata-less path-list output was not neutral archive-backed compaction: %q", text)
	}
	if proxyLayer0EvidenceHasReason(result.Stats.EvidenceDecisions, "wss_search_output_risk_gate") {
		t.Fatalf("plain path-list inference must not trip search risk gate: %+v", result.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0WSSSearchProofAllowsNonDeltaFindPathList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var output strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&output, ".reconc/audit/%04d.jsonl\n", i)
	}
	original := output.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-proof-find", ToolName: "exec_command", ToolInput: `{"cmd":"find .reconc -maxdepth 4 -type f"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-proof-find", Text: original}}},
	}
	result := reduceCodexLayer0(codexLayer0Request{
		Route:                    codexLayer0RouteWSSPhaseF,
		Messages:                 messages,
		SessionID:                "sess-wss-find-proof",
		WSSSearchMutationAllowed: true,
	})
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 || result.Stats.CapturedOutputBlocks != 1 {
		t.Fatalf("proofed non-delta find path-list should compact, stats=%+v text=%q", result.Stats, result.Messages[1].Content[0].Text)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "[find paths]") ||
		!strings.Contains(text, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(text, ".reconc/audit/0079.jsonl") {
		t.Fatalf("path-list output was not grouped through archive-backed proof path: %q", text)
	}

	delta := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-wss-find-proof-delta",
		WSSSearchMutationAllowed:     true,
		StatefulDeltaMutationBlocked: true,
	})
	if delta.Stats.BlocksModified != 0 || delta.Stats.TokensSaved != 0 || delta.Messages[1].Content[0].Text != original ||
		!proxyLayer0EvidenceHasReason(delta.Stats.EvidenceDecisions, "wss_stateful_delta_mutation_proof_gate") {
		t.Fatalf("delta path-list output must remain guarded, stats=%+v text=%q", delta.Stats, delta.Messages[1].Content[0].Text)
	}
}

func proxyWSSSearchOutputFixture(needle string, count int) string {
	var output strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&output, "src/file_%03d.go:%d:%s with enough detail to compact %s\n", i, i+1, needle, strings.Repeat("context ", 20))
	}
	return output.String()
}

func proxyLayer0EvidenceHasReason(decisions []evidence.BlockDecision, reason string) bool {
	for _, decision := range decisions {
		if decision.Reason == reason {
			return true
		}
	}
	return false
}

func TestProxyLayer0DownstreamStateMechanismSet(t *testing.T) {
	tests := []struct {
		name      string
		mechanism proxyLayer0Mechanism
		want      bool
	}{
		{name: "read_delta", mechanism: proxyLayer0MechanismReadDelta, want: true},
		{name: "stale_read", mechanism: proxyLayer0MechanismStaleRead, want: true},
		{name: "obsolete_prune", mechanism: proxyLayer0MechanismObsoletePrune, want: true},
		{name: "chunk_dedup", mechanism: proxyLayer0MechanismChunkDedup, want: true},
		{name: "captured_output", mechanism: proxyLayer0MechanismCapturedOut, want: false},
		{name: "codex_envelope", mechanism: proxyLayer0MechanismCodexEnvelope, want: false},
		{name: "repeated_output", mechanism: proxyLayer0MechanismRepeatedOut, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proxyLayer0DownstreamStateMechanism(tt.mechanism); got != tt.want {
				t.Fatalf("proxyLayer0DownstreamStateMechanism(%s)=%v want %v", tt.mechanism, got, tt.want)
			}
		})
	}
}

func TestReduceCodexLayer0GuardedCandidateEvidenceCarriesFootprint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	body := uniqueProxyReadPayload("guarded footprint")
	messages := proxyReadMessages(body)

	guarded := reduceCodexLayer0(codexLayer0Request{
		Messages:                     messages,
		SessionID:                    "sess-guarded-footprint",
		TurnID:                       "guarded-1",
		TurnSeq:                      1,
		StatefulDeltaMutationBlocked: true,
	})
	if guarded.Stats.TokensSaved != 0 || guarded.Stats.ReadDeltaBlocks != 0 ||
		guarded.Messages[1].Content[0].Text != body {
		t.Fatalf("stateful delta guard must full-pass bytes and accounting: stats=%+v text=%q", guarded.Stats, guarded.Messages[1].Content[0].Text)
	}
	var got evidence.BlockDecision
	for _, decision := range guarded.Stats.EvidenceDecisions {
		if decision.Mechanism == string(proxyLayer0MechanismReadDelta) &&
			decision.Action == evidence.ActionFullPass &&
			decision.Reason == "wss_stateful_delta_mutation_proof_gate" {
			got = decision
			break
		}
	}
	if got.Mechanism == "" {
		t.Fatalf("guarded candidate evidence missing: %+v", guarded.Stats.EvidenceDecisions)
	}
	if got.OriginalTokens <= 0 || got.FinalTokens != got.OriginalTokens || got.SavedTokens != 0 ||
		got.FootprintScore <= 0 || got.FootprintScoreBucket == "" ||
		guarded.Stats.ReadDeltaAttempts != 1 || guarded.Stats.ReadDeltaMisses != 1 ||
		len(guarded.Stats.CacheEvents) != 1 ||
		guarded.Stats.CacheEvents[0].Mechanism != savingspolicy.CodexMechanismReadDelta ||
		guarded.Stats.CacheEvents[0].Action != proxyLayer0CacheMiss ||
		guarded.Stats.CacheEvents[0].Reason != "first_observation_seeded" {
		t.Fatalf("guarded candidate must be byte-equal footprint evidence with observe-only seeding: decision=%+v stats=%+v", got, guarded.Stats)
	}
	if !hasTokenNeutralEvidenceDecision(guarded.Stats.EvidenceDecisions, proxyLayer0MechanismReadDelta, "read_delta_first_observation_seeded", evidence.ActionShadow) {
		t.Fatalf("guarded read-delta observe-only seed must emit token-neutral shadow evidence: %+v", guarded.Stats.EvidenceDecisions)
	}

	unguarded := reduceCodexLayer0(codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-guarded-footprint",
		TurnID:    "unguarded-2",
		TurnSeq:   2,
	})
	if unguarded.Stats.TokensSaved <= 0 || unguarded.Stats.ReadDeltaBlocks != 1 ||
		unguarded.Messages[1].Content[0].Text == body ||
		!strings.Contains(unguarded.Messages[1].Content[0].Text, "kind=file-read") {
		t.Fatalf("guarded observe-only seed should enable later unguarded read-delta savings: %+v text=%q", unguarded.Stats, unguarded.Messages[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0StatefulDeltaSeedsRepeatedOutputObserveOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := strings.Repeat("deterministic report row with unchanged non-file data\n", 160)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-report", ToolName: "exec_command", ToolInput: `{"cmd":"python generate_report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-report", Text: body}}},
	}

	guarded := reduceCodexLayer0(codexLayer0Request{
		Messages:                     messages,
		SessionID:                    "sess-guarded-repeated",
		TurnSeq:                      1,
		StatefulDeltaMutationBlocked: true,
	})
	if guarded.Stats.TokensSaved != 0 || guarded.Stats.RepeatedOutputBlocks != 0 ||
		guarded.Messages[1].Content[0].Text != body {
		t.Fatalf("stateful delta guard must full-pass repeated output bytes: %+v", guarded.Stats)
	}
	if !proxyLayer0EvidenceHasReason(guarded.Stats.EvidenceDecisions, "wss_stateful_delta_mutation_proof_gate") {
		t.Fatalf("guarded repeated output evidence missing: %+v", guarded.Stats.EvidenceDecisions)
	}
	if len(guarded.Stats.CacheEvents) != 1 ||
		guarded.Stats.CacheEvents[0].Mechanism != savingspolicy.CodexMechanismRepeatedOutput ||
		guarded.Stats.CacheEvents[0].Action != proxyLayer0CacheMiss ||
		guarded.Stats.CacheEvents[0].Reason != "first_observation_seeded" {
		t.Fatalf("stateful delta guard should observe-only seed repeated output: %+v", guarded.Stats.CacheEvents)
	}
	if !hasTokenNeutralEvidenceDecision(guarded.Stats.EvidenceDecisions, proxyLayer0MechanismRepeatedOut, "repeated_output_first_observation_seeded", evidence.ActionShadow) {
		t.Fatalf("guarded repeated-output observe-only seed must emit token-neutral shadow evidence: %+v", guarded.Stats.EvidenceDecisions)
	}

	unguarded := reduceCodexLayer0(codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-guarded-repeated",
		TurnSeq:   2,
	})
	if unguarded.Stats.TokensSaved <= 0 || unguarded.Stats.RepeatedOutputBlocks != 1 ||
		unguarded.Messages[1].Content[0].Text == body ||
		!strings.Contains(unguarded.Messages[1].Content[0].Text, "kind=tool-output") {
		t.Fatalf("guarded observe-only seed should enable later repeated-output savings: %+v text=%q", unguarded.Stats, unguarded.Messages[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0RepeatedOutputMissRecordsObserveOnlyEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := strings.Repeat("deterministic report row with unchanged non-file data\n", 160)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-report", ToolName: "exec_command", ToolInput: `{"cmd":"python generate_report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-report", Text: body}}},
	}

	seed := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-observe-repeated-miss",
		TurnSeq:   1,
	})
	if seed.Stats.TokensSaved != 0 || seed.Stats.BlocksModified != 0 ||
		seed.Stats.RepeatedOutputBlocks != 0 || seed.Messages[1].Content[0].Text != body {
		t.Fatalf("first repeated-output observation must keep wire bytes and savings accounting untouched: %+v", seed.Stats)
	}
	if len(seed.Stats.CacheEvents) != 1 ||
		seed.Stats.CacheEvents[0].Mechanism != savingspolicy.CodexMechanismRepeatedOutput ||
		seed.Stats.CacheEvents[0].Action != proxyLayer0CacheMiss ||
		seed.Stats.CacheEvents[0].Reason != "first_observation_seeded" {
		t.Fatalf("first repeated-output observation should seed cache: %+v", seed.Stats.CacheEvents)
	}
	var shadow evidence.BlockDecision
	for _, decision := range seed.Stats.EvidenceDecisions {
		if decision.Mechanism == string(proxyLayer0MechanismRepeatedOut) &&
			decision.Action == evidence.ActionShadow &&
			decision.Reason == "repeated_output_first_observation_seeded" {
			shadow = decision
			break
		}
	}
	if shadow.Mechanism == "" {
		t.Fatalf("observe-only repeated-output evidence missing: %+v", seed.Stats.EvidenceDecisions)
	}
	if shadow.CommandClass != "python" || shadow.ContentClass != evidence.ContentPlain ||
		shadow.SafetyClass != evidence.SafetyExact || shadow.NetTokens != 0 ||
		shadow.OriginalTokens != 0 || shadow.FinalTokens != 0 || shadow.SavedTokens != 0 ||
		shadow.FootprintScore != 0 || !strings.Contains(shadow.Recovery, "wire unchanged") {
		t.Fatalf("observe-only evidence must stay content-free and token-neutral: %+v", shadow)
	}

	hit := reduceCodexLayer0(codexLayer0Request{
		Route:     codexLayer0RouteWSSPhaseF,
		Messages:  messages,
		SessionID: "sess-observe-repeated-miss",
		TurnSeq:   2,
	})
	if hit.Stats.TokensSaved <= 0 || hit.Stats.RepeatedOutputBlocks != 1 ||
		hit.Messages[1].Content[0].Text == body ||
		!strings.Contains(hit.Messages[1].Content[0].Text, "kind=tool-output") {
		t.Fatalf("observe-only seed should enable later repeated-output savings: %+v text=%q", hit.Stats, hit.Messages[1].Content[0].Text)
	}
}

func TestProxyLayer0StatsWithoutSavingsClearsAppliedAccounting(t *testing.T) {
	stats := proxyLayer0Stats{
		TokensSaved:              100,
		BlocksModified:           2,
		ReadDeltaBlocks:          1,
		CapturedOutputBlocks:     1,
		CodexExecEnvelopeBlocks:  1,
		RepeatedOutputBlocks:     1,
		ChunkDedupBlocks:         1,
		ChunkDedupReferences:     4,
		ChunkDedupRefBytes:       800,
		ChunkDedupInputBytes:     1200,
		StaleReadBlocks:          1,
		StaleReadBytesSaved:      400,
		StaleReadTokensSaved:     50,
		ObsoletePruneBlocks:      1,
		ObsoletePruneBytesSaved:  300,
		ObsoletePruneTokensSaved: 40,
		WSSSearchRiskBlocks:      2,
		WSSSearchProofAllowed:    1,
		WSSSearchProofBlocked:    1,
		WSSSearchProofReasons:    map[string]int{"latch_disabled": 1},
		ReadDeltaKeys:            []string{"read:a.go"},
		PolicyDecisions:          []savingspolicy.CodexMechanismDecision{{Mechanism: savingspolicy.CodexMechanismReadDelta}},
		CacheEvents:              []proxyLayer0CacheEvent{{Mechanism: savingspolicy.CodexMechanismReadDelta, Action: proxyLayer0CacheHit}},
		EvidenceDecisions:        []evidence.BlockDecision{{Mechanism: string(proxyLayer0MechanismReadDelta), Action: evidence.ActionApplied}},
		TotalLatencyNs:           11,
		ReadDeltaLatencyNs:       12,
		FilterLatencyNs:          13,
		RepeatedOutputLatencyNs:  14,
		ChunkDedupLatencyNs:      15,
	}
	got := stats.withoutSavings()
	if got.TokensSaved != 0 || got.BlocksModified != 0 || got.ReadDeltaBlocks != 0 ||
		got.CapturedOutputBlocks != 0 || got.CodexExecEnvelopeBlocks != 0 ||
		got.RepeatedOutputBlocks != 0 || got.ChunkDedupBlocks != 0 ||
		got.ChunkDedupReferences != 0 || got.ChunkDedupRefBytes != 0 ||
		got.ChunkDedupInputBytes != 0 || got.StaleReadBlocks != 0 ||
		got.StaleReadBytesSaved != 0 || got.StaleReadTokensSaved != 0 ||
		got.ObsoletePruneBlocks != 0 || got.ObsoletePruneBytesSaved != 0 ||
		got.ObsoletePruneTokensSaved != 0 || got.ReadDeltaKeys != nil ||
		got.WSSSearchRiskBlocks != 0 || got.WSSSearchProofAllowed != 0 ||
		got.WSSSearchProofBlocked != 0 || got.WSSSearchProofReasons != nil ||
		got.PolicyDecisions != nil || got.CacheEvents != nil || got.EvidenceDecisions != nil {
		t.Fatalf("withoutSavings left applied accounting: %+v", got)
	}
	if got.TotalLatencyNs != 11 || got.ReadDeltaLatencyNs != 12 || got.FilterLatencyNs != 13 ||
		got.RepeatedOutputLatencyNs != 14 || got.ChunkDedupLatencyNs != 15 {
		t.Fatalf("withoutSavings must preserve latency accounting: %+v", got)
	}
}

func TestProxyLayer0StatsNeedsArchiveRecoveryNote(t *testing.T) {
	cases := []struct {
		name  string
		stats proxyLayer0Stats
		want  bool
	}{
		{name: "none", stats: proxyLayer0Stats{}, want: false},
		{name: "http read delta", stats: proxyLayer0Stats{Route: codexLayer0RouteHTTP, ReadDeltaBlocks: 1}, want: true},
		{name: "http repeated output", stats: proxyLayer0Stats{Route: codexLayer0RouteHTTP, RepeatedOutputBlocks: 1}, want: true},
		{name: "http chunk dedup", stats: proxyLayer0Stats{Route: codexLayer0RouteHTTP, ChunkDedupBlocks: 1}, want: true},
		{name: "http captured output", stats: proxyLayer0Stats{Route: codexLayer0RouteHTTP, CapturedOutputBlocks: 1}, want: false},
		{name: "http codex envelope", stats: proxyLayer0Stats{Route: codexLayer0RouteHTTP, CodexExecEnvelopeBlocks: 1}, want: false},
		{name: "wss captured output", stats: proxyLayer0Stats{Route: codexLayer0RouteWSSPhaseF, CapturedOutputBlocks: 1}, want: true},
		{name: "wss codex envelope", stats: proxyLayer0Stats{Route: codexLayer0RouteWSSPhaseF, CodexExecEnvelopeBlocks: 1}, want: true},
		{name: "stale read", stats: proxyLayer0Stats{Route: codexLayer0RouteWSSPhaseF, StaleReadBlocks: 1}, want: false},
		{name: "obsolete read", stats: proxyLayer0Stats{Route: codexLayer0RouteWSSPhaseF, ObsoletePruneBlocks: 1}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := proxyLayer0StatsNeedsArchiveRecoveryNote(tc.stats); got != tc.want {
				t.Fatalf("proxyLayer0StatsNeedsArchiveRecoveryNote(%+v)=%v want %v", tc.stats, got, tc.want)
			}
		})
	}
}

func TestReduceCodexLayer0ReconcCommandsPassThrough(t *testing.T) {
	commands := []string{
		"reconc check .",
		"cd /repo && tools/reconc/dist/reconc-0.5.0-darwin-arm64 check .",
		"/bin/bash -lc 'cd /repo && reconc check . --write docs/tasks.md'",
		"go run ./cmd/reconc check . --json",
	}
	routes := []codexLayer0Route{codexLayer0RouteUnspecified, codexLayer0RouteHTTP, codexLayer0RouteWSSPhaseF}
	var output strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&output, "Decision:  pass\nRepo:      /repo\nLockfile:  .reconc/policy.lock.json\nDefault:   warn\nSummary:   policy pass row %03d\n\n", i)
	}
	original := output.String()

	for _, route := range routes {
		for i, command := range commands {
			t.Run(fmt.Sprintf("%s/%d", route, i), func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				messages := []types.Message{
					{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-reconc", ToolName: "exec_command", ToolInput: `{"cmd":` + strconv.Quote(command) + `}`}}},
					{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-reconc", Text: original}}},
				}
				req := codexLayer0Request{
					Route:     route,
					Messages:  messages,
					SessionID: "sess-reconc-pass-through",
				}
				seed := reduceCodexLayer0(req)
				result := reduceCodexLayer0(req)
				for _, got := range []codexLayer0Result{seed, result} {
					if got.Stats.ToolResultBlocks != 1 || got.Stats.CommandResolvedBlocks != 1 ||
						got.Stats.BlocksModified != 0 || got.Stats.TokensSaved != 0 || len(got.Stats.PolicyDecisions) != 0 ||
						got.Messages[1].Content[0].Text != original {
						t.Fatalf("Reconc command output must pass through unchanged, command=%q stats=%+v text=%q", command, got.Stats, got.Messages[1].Content[0].Text)
					}
				}
			})
		}
	}
}

func TestReduceCodexLayer0HostBudgetDemotesReducers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	body := strings.Repeat("deterministic report row with unchanged non-file data\n", 80)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-report", ToolName: "exec_command", ToolInput: `{"cmd":"python generate_report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-report", Text: body}}},
	}
	baseReq := codexLayer0Request{
		Messages:  messages,
		SessionID: "sess-host-budget",
	}
	seed := reduceCodexLayer0(baseReq)
	if seed.Stats.TokensSaved != 0 {
		t.Fatalf("first observation must only seed, stats=%+v", seed.Stats)
	}
	budgetReq := baseReq
	budgetReq.HostBudgetExceeded = true
	budgeted := reduceCodexLayer0(budgetReq)
	if budgeted.Stats.TokensSaved <= 0 || budgeted.Stats.RepeatedOutputBlocks != 1 ||
		!strings.Contains(budgeted.Messages[1].Content[0].Text, "[context-elided kind=tool-output status=unchanged") {
		t.Fatalf("host budget should keep cheap lossless cache hits, stats=%+v text=%q", budgeted.Stats, budgeted.Messages[1].Content[0].Text)
	}
	latencyReq := baseReq
	latencyReq.LatencyBudgetExceeded = true
	latencyBudgeted := reduceCodexLayer0(latencyReq)
	if latencyBudgeted.Stats.TokensSaved <= 0 || latencyBudgeted.Stats.RepeatedOutputBlocks != 1 ||
		!strings.Contains(latencyBudgeted.Messages[1].Content[0].Text, "[context-elided kind=tool-output status=unchanged") {
		t.Fatalf("latency budget should keep cheap lossless cache hits, stats=%+v text=%q", latencyBudgeted.Stats, latencyBudgeted.Messages[1].Content[0].Text)
	}
	unblocked := reduceCodexLayer0(baseReq)
	if unblocked.Stats.TokensSaved <= 0 || unblocked.Stats.RepeatedOutputBlocks != 1 {
		t.Fatalf("normal budget should still collapse repeated output, stats=%+v", unblocked.Stats)
	}
}

func TestApplyProxyLayer0WithSessionRepeatedPartialReadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var bodyBuilder strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&bodyBuilder, "visible partial file range line %03d with stable value %d\n", i, i*i)
	}
	body := bodyBuilder.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-head", ToolName: "exec_command", ToolInput: `{"cmd":"head -n 200 /tmp/range.data"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-head", Text: body}}},
	}
	out, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-partial-read", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 || stats.RepeatedOutputBlocks != 0 ||
		strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("first partial read should miss read-delta and avoid repeated-output, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}

	out, stats = applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-partial-read", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaBlocks != 1 ||
		stats.RepeatedOutputBlocks != 0 || stats.TokensSaved <= 0 ||
		!strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("partial read should use ranged read-delta, not repeated-output, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}
}

func TestApplyProxyLayer0WithSessionRepeatedAwkRangeReadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var bodyBuilder strings.Builder
	for i := 10; i <= 40; i++ {
		fmt.Fprintf(&bodyBuilder, "awk range line %03d keeps exact context\n", i)
	}
	body := bodyBuilder.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-awk", ToolName: "exec_command", ToolInput: `{"cmd":"awk 'NR>=10 && NR<=40 {print}' /tmp/range.data"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-awk", Text: body}}},
	}
	out, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-awk-range", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 ||
		strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("first awk range read should full-pass and seed readcache, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}

	out, stats = applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-awk-range", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaBlocks != 1 ||
		stats.RepeatedOutputBlocks != 0 || stats.TokensSaved <= 0 ||
		!strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("second awk range read should use read-delta, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}
}

func TestApplyProxyLayer0WithSessionRepeatedNumberedSedRangeReadOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var bodyBuilder strings.Builder
	for i := 10; i <= 40; i++ {
		fmt.Fprintf(&bodyBuilder, "%6d\tnumbered sed range line %03d keeps exact context\n", i, i)
	}
	body := bodyBuilder.String()
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "call-nl-sed", ToolName: "exec_command", ToolInput: `{"cmd":"nl -ba /tmp/range.data | sed -n '10,40p'"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call-nl-sed", Text: body}}},
	}
	out, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-nl-sed-range", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaMisses != 1 || stats.ReadDeltaBlocks != 0 ||
		strings.Contains(out[1].Content[0].Text, "archive=local-archive://") {
		t.Fatalf("first numbered sed range read should full-pass and seed readcache, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}

	out, stats = applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-nl-sed-range", nil)
	if stats.ReadDeltaAttempts != 1 || stats.ReadDeltaBlocks != 1 ||
		stats.RepeatedOutputBlocks != 0 || stats.TokensSaved <= 0 ||
		!strings.Contains(out[1].Content[0].Text, "archive=local-archive://") ||
		!strings.Contains(out[1].Content[0].Text, "range.data") {
		t.Fatalf("second numbered sed range read should use ranged read-delta, stats=%+v text=%q", stats, out[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0ChunkDedupPartialOverlap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := uniqueProxyReadPayload("shared chunk dedup")
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "Read", ToolInput: `{"path":"a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: shared + uniqueProxyReadPayload("tail a")}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "Read", ToolInput: `{"path":"b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: shared + uniqueProxyReadPayload("tail b")}}},
	}

	seed := reduceCodexLayer0(codexLayer0Request{
		Messages:           first,
		SessionID:          "sess-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupProof:    savingspolicy.CodexProofLive,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	if seed.Stats.TokensSaved != 0 || seed.Stats.ChunkDedupBlocks != 0 {
		t.Fatalf("first partially-overlapped read should seed only: %+v", seed.Stats)
	}
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:           second,
		SessionID:          "sess-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupProof:    savingspolicy.CodexProofLive,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(text, "tail b") {
		t.Fatalf("second similar read should chunk-dedup shared regions: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0ChunkDedupHighFootprintScalesMinBytes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := strings.Repeat("t359 high footprint shared chunk line\n", 700)
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "Read", ToolInput: `{"path":"a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: shared + "tail a\n"}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "Read", ToolInput: `{"path":"b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: shared + "tail b\n"}}},
	}
	req := func(messages []types.Message, turnSeq int) codexLayer0Request {
		return codexLayer0Request{
			Messages:            messages,
			SessionID:           "sess-t359-high-footprint",
			ChunkDedupEnabled:   true,
			ChunkDedupProof:     savingspolicy.CodexProofLive,
			ChunkDedupMinBytes:  32768,
			ChunkDedupMaxRefPct: 100,
			ChunkStore:          store,
			ArchiveRecovery:     true,
			TurnSeq:             turnSeq,
		}
	}

	seed := reduceCodexLayer0(req(first, 1))
	if seed.Stats.TokensSaved != 0 || seed.Stats.ChunkDedupBlocks != 0 {
		t.Fatalf("first high-footprint output should seed only: %+v", seed.Stats)
	}
	out := reduceCodexLayer0(req(second, 2))
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "[context-chunk status=unchanged uri=local-archive://") {
		t.Fatalf("early high-footprint output should scale threshold and chunk-dedup: stats=%+v text=%q", out.Stats, text)
	}
	if !hasEvidenceDecision(out.Stats.EvidenceDecisions, proxyLayer0MechanismChunkDedup, "positive_net_savings", evidence.ActionApplied) {
		t.Fatalf("high-footprint chunk dedup should emit applied evidence: %+v", out.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0ChunkDedupReservesBudgetForHigherFootprint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(
		chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096},
		chunkdedup.StoreLimits{MaxSessionRefPct: 45},
		func(_, id string, chunk []byte) string {
			if len(chunk) == 0 || id == "" {
				return ""
			}
			return "local-archive://" + id
		},
	)
	lowShared := strings.Repeat("t359 low budget contender line\n", 180)
	highShared := strings.Repeat("t359 high budget contender line with much more session footprint\n", 900)
	seed := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "seed-low", ToolName: "Read", ToolInput: `{"path":"low.seed"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "seed-low", Text: lowShared + "seed low tail\n"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "seed-high", ToolName: "Read", ToolInput: `{"path":"high.seed"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "seed-high", Text: highShared + "seed high tail\n"}}},
	}
	competing := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "low", ToolName: "Read", ToolInput: `{"path":"low.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "low", Text: lowShared + "fresh low tail\n"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "high", ToolName: "Read", ToolInput: `{"path":"high.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "high", Text: highShared + "fresh high tail\n"}}},
	}
	req := func(messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Messages:            messages,
			SessionID:           "sess-t359-budget-priority",
			ChunkDedupEnabled:   true,
			ChunkDedupProof:     savingspolicy.CodexProofLive,
			ChunkDedupMinBytes:  4096,
			ChunkDedupMaxRefPct: 100,
			ChunkStore:          store,
			ArchiveRecovery:     true,
			TurnSeq:             1,
		}
	}

	seedResult := reduceCodexLayer0(req(seed))
	if seedResult.Stats.TokensSaved != 0 || seedResult.Stats.ChunkDedupBlocks != 0 {
		t.Fatalf("seed request should only populate chunk state: %+v", seedResult.Stats)
	}
	out := reduceCodexLayer0(req(competing))
	lowText := out.Messages[1].Content[0].Text
	highText := out.Messages[3].Content[0].Text
	if strings.Contains(lowText, "[context-chunk status=unchanged") {
		t.Fatalf("lower-footprint earlier block should preserve budget, text=%q stats=%+v", lowText, out.Stats)
	}
	if out.Stats.ChunkDedupBlocks != 1 || out.Stats.TokensSaved <= 0 ||
		!strings.Contains(highText, "[context-chunk status=unchanged uri=local-archive://") {
		t.Fatalf("higher-footprint later block should consume reserved budget: stats=%+v high=%q", out.Stats, highText)
	}
	if !hasEvidenceDecision(out.Stats.EvidenceDecisions, proxyLayer0MechanismChunkDedup, "session_integrity_budget", evidence.ActionFullPass) {
		t.Fatalf("reserved lower-footprint block should emit full-pass budget evidence: %+v", out.Stats.EvidenceDecisions)
	}
}

func TestReduceCodexLayer0ChunkBudgetFullPassSeedsFutureChunkState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(
		chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096},
		chunkdedup.StoreLimits{},
		func(_, id string, chunk []byte) string {
			if len(chunk) == 0 || id == "" {
				return ""
			}
			return "local-archive://" + id
		},
	)
	shared := strings.Repeat("session integrity budget full-pass shared report row\n", 700)
	messages := func(callID string, command string, tail string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: callID, ToolName: "exec_command", ToolInput: fmt.Sprintf(`{"cmd":%q}`, command)}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: callID, Text: shared + tail}}},
		}
	}
	req := func(messages []types.Message, budgetHit bool) codexLayer0Request {
		return codexLayer0Request{
			Messages:                  messages,
			SessionID:                 "sess-budget-full-pass-seed",
			ChunkDedupEnabled:         true,
			ChunkDedupProof:           savingspolicy.CodexProofLive,
			ChunkDedupMinBytes:        4096,
			ChunkDedupMaxRefPct:       100,
			ChunkStore:                store,
			ArchiveRecovery:           true,
			ChunkIntegrityBudgetHit:   budgetHit,
			RecentFullPassTurns:       0,
			RemainingTurnsEstimate:    0,
			StructuredMutationBlocked: false,
		}
	}

	seedText := shared + "first full-pass tail\n"
	seed := reduceCodexLayer0(req(messages("budget-seed", "python report.py --case seed", "first full-pass tail\n"), true))
	if seed.Stats.TokensSaved != 0 || seed.Stats.BlocksModified != 0 ||
		seed.Messages[1].Content[0].Text != seedText {
		t.Fatalf("budget full-pass seed must preserve original output: stats=%+v text=%q", seed.Stats, seed.Messages[1].Content[0].Text)
	}
	if !hasEvidenceDecision(seed.Stats.EvidenceDecisions, proxyLayer0MechanismChunkDedup, "session_integrity_budget", evidence.ActionFullPass) {
		t.Fatalf("budget full-pass seed must emit chunk evidence: %+v", seed.Stats.EvidenceDecisions)
	}

	out := reduceCodexLayer0(req(messages("budget-hit", "python report.py --case hit", "second chunk-dedup tail\n"), false))
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(text, "second chunk-dedup tail") {
		t.Fatalf("post-budget similar output should use full-pass seed: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0ChunkDedupLowFootprintKeepsConfiguredMinBytes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := strings.Repeat("t359 low footprint shared chunk line\n", 700)
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "Read", ToolInput: `{"path":"a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: shared + "tail a\n"}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "Read", ToolInput: `{"path":"b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: shared + "tail b\n"}}},
	}
	req := func(messages []types.Message, turnSeq int) codexLayer0Request {
		return codexLayer0Request{
			Messages:            messages,
			SessionID:           "sess-t359-low-footprint",
			ChunkDedupEnabled:   true,
			ChunkDedupProof:     savingspolicy.CodexProofLive,
			ChunkDedupMinBytes:  32768,
			ChunkDedupMaxRefPct: 100,
			ChunkStore:          store,
			ArchiveRecovery:     true,
			TurnSeq:             turnSeq,
		}
	}

	reduceCodexLayer0(req(first, 10))
	out := reduceCodexLayer0(req(second, 11))
	if out.Stats.TokensSaved != 0 || out.Stats.ChunkDedupBlocks != 0 ||
		strings.Contains(out.Messages[1].Content[0].Text, "[context-chunk status=unchanged") {
		t.Fatalf("low-footprint output below configured min must stay full-pass: stats=%+v text=%q", out.Stats, out.Messages[1].Content[0].Text)
	}
}

func TestProxyScaledChunkDedupMinBytes(t *testing.T) {
	tests := []struct {
		name        string
		base        int
		outputBytes int
		turnSeq     int
		want        int
	}{
		{name: "disabled", base: 0, outputBytes: 64000, turnSeq: 1, want: 0},
		{name: "missing_turn", base: 4096, outputBytes: 64000, turnSeq: 0, want: 4096},
		{name: "high_early", base: 4096, outputBytes: 64000, turnSeq: 1, want: 2048},
		{name: "mid_early_unchanged", base: 4096, outputBytes: 8192, turnSeq: 1, want: 4096},
		{name: "late_unchanged", base: 4096, outputBytes: 64000, turnSeq: 12, want: 4096},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proxyScaledChunkDedupMinBytes(tt.base, tt.outputBytes, tt.turnSeq, 0, proxyDefaultCachedPriceRatio)
			if got != tt.want {
				t.Fatalf("proxyScaledChunkDedupMinBytes(%d,%d,%d)=%d want %d", tt.base, tt.outputBytes, tt.turnSeq, got, tt.want)
			}
		})
	}
}

func TestProxyScaledChunkDedupMinBytesUsesCachedPriceRatio(t *testing.T) {
	if got := proxyScaledChunkDedupMinBytes(4096, 64000, 1, 0, 0.01); got != 4096 {
		t.Fatalf("low cached-price ratio should keep configured threshold, got %d", got)
	}
	if got := proxyScaledChunkDedupMinBytes(4096, 64000, 1, 0, 0.20); got != 2048 {
		t.Fatalf("high cached-price ratio should still scale high-footprint threshold, got %d", got)
	}
	if got := proxyScaledChunkDedupMinBytes(4096, 64000, 12, 30, 0.10); got != 2048 {
		t.Fatalf("explicit remaining-turn estimate should scale late high-footprint threshold, got %d", got)
	}
}

func TestReduceCodexLayer0ChunkDedupInsideCodexExecEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := uniqueProxyReadPayload("shared codex exec envelope")
	firstText := "Chunk ID: aaa111\nWall time: 0ms\nProcess exited with code 0\nOutput:\n" + shared + uniqueProxyReadPayload("tail a")
	secondText := "Chunk ID: bbb222\nWall time: 0ms\nProcess exited with code 0\nOutput:\n" + shared + uniqueProxyReadPayload("tail b")
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "exec_command", ToolInput: `{"cmd":"cat a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: firstText}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "exec_command", ToolInput: `{"cmd":"cat b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: secondText}}},
	}

	seed := reduceCodexLayer0(codexLayer0Request{
		Messages:           first,
		SessionID:          "sess-envelope-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupProof:    savingspolicy.CodexProofLive,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	if seed.Stats.TokensSaved != 0 || seed.Stats.ChunkDedupBlocks != 0 {
		t.Fatalf("first envelope should seed payload chunks only: %+v", seed.Stats)
	}
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:           second,
		SessionID:          "sess-envelope-chunks",
		ChunkDedupEnabled:  true,
		ChunkDedupProof:    savingspolicy.CodexProofLive,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "Chunk ID: bbb222") ||
		!strings.Contains(text, "Output:\n[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(text, "tail b") {
		t.Fatalf("second envelope should chunk-dedup payload while preserving header: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0ChunkDedupCodexTruncatedEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStore(chunkdedup.Config{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := strings.Repeat("codex truncated output stable shared line\n", 210)
	firstText := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 51204\nOutput:\nTotal output lines: 3201\n\n" + shared + "tail for file A\n"
	secondText := "Chunk ID: bbb222\nWall time: 0.0001 seconds\nProcess exited with code 0\nOriginal token count: 51204\nOutput:\nTotal output lines: 3201\n\n" + shared + "tail for file B\n"
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "exec_command", ToolInput: `{"cmd":"cat /tmp/a.txt"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: firstText}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "exec_command", ToolInput: `{"cmd":"cat /tmp/b.txt"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: secondText}}},
	}

	reduceCodexLayer0(codexLayer0Request{
		Messages:           first,
		SessionID:          "sess-truncated-envelope",
		ChunkDedupEnabled:  true,
		ChunkDedupProof:    savingspolicy.CodexProofLive,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:           second,
		SessionID:          "sess-truncated-envelope",
		ChunkDedupEnabled:  true,
		ChunkDedupProof:    savingspolicy.CodexProofLive,
		ChunkDedupMinBytes: 0,
		ChunkStore:         store,
		ArchiveRecovery:    true,
	})
	text := out.Messages[1].Content[0].Text
	if out.Stats.TokensSaved <= 0 || out.Stats.ChunkDedupBlocks != 1 ||
		!strings.Contains(text, "[context-chunk status=unchanged uri=local-archive://") ||
		!strings.Contains(text, "tail for file B") {
		t.Fatalf("Codex-truncated envelope should chunk-dedup stable payload prefix: stats=%+v text=%q", out.Stats, text)
	}
}

func TestReduceCodexLayer0ChunkDedupReferenceDensityGuard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{MinSize: 1024, AvgSize: 2048, MaxSize: 4096}, chunkdedup.StoreLimits{}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := strings.Repeat("stable repeated context line for chunk density\n", 220)
	first := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-a", ToolName: "Read", ToolInput: `{"path":"a.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-a", Text: shared + "fresh A\n"}}},
	}
	second := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-b", ToolName: "Read", ToolInput: `{"path":"b.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-b", Text: shared + "fresh B\n"}}},
	}

	reduceCodexLayer0(codexLayer0Request{
		Messages:            first,
		SessionID:           "sess-density-guard",
		ChunkDedupEnabled:   true,
		ChunkDedupProof:     savingspolicy.CodexProofLive,
		ChunkDedupMinBytes:  0,
		ChunkDedupMaxRefPct: 10,
		ChunkStore:          store,
		ArchiveRecovery:     true,
	})
	out := reduceCodexLayer0(codexLayer0Request{
		Messages:            second,
		SessionID:           "sess-density-guard",
		ChunkDedupEnabled:   true,
		ChunkDedupProof:     savingspolicy.CodexProofLive,
		ChunkDedupMinBytes:  0,
		ChunkDedupMaxRefPct: 10,
		ChunkStore:          store,
		ArchiveRecovery:     true,
	})
	if out.Stats.ChunkDedupBlocks != 0 || strings.Contains(out.Messages[1].Content[0].Text, "context-chunk") {
		t.Fatalf("high reference density should full-pass, stats=%+v text=%q", out.Stats, out.Messages[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0ChunkDedupSessionBudgetPreDemotesChunk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{}, chunkdedup.StoreLimits{MaxSessionRefPct: 20}, func(_, id string, chunk []byte) string {
		if len(chunk) == 0 || id == "" {
			return ""
		}
		return "local-archive://" + id
	})
	shared := strings.Repeat("chunk session integrity guard line keeps context recoverable\n", 1200)
	messagesFor := func(id, path, tail string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "Read", ToolInput: `{"path":"` + path + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: shared + tail}}},
		}
	}
	req := func(messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Route:               codexLayer0RouteWSSPhaseF,
			Messages:            messages,
			SessionID:           "chunk-session-budget-policy",
			ChunkStore:          store,
			ArchiveRecovery:     true,
			ChunkDedupEnabled:   true,
			ChunkDedupProof:     savingspolicy.CodexProofLive,
			ChunkDedupMinBytes:  1,
			ChunkDedupMaxRefPct: 100,
			PolicyMode:          "auto",
		}
	}

	seed := reduceCodexLayer0(req(messagesFor("read-a", "a.txt", "first tail\n")))
	if seed.Stats.ChunkDedupBlocks != 0 || seed.Stats.TokensSaved != 0 {
		t.Fatalf("first output should seed only: %+v", seed.Stats)
	}
	second := reduceCodexLayer0(req(messagesFor("read-b", "b.txt", "second tail\n")))
	if second.Stats.ChunkDedupBlocks != 1 || second.Stats.TokensSaved <= 0 {
		t.Fatalf("second output should consume bounded chunk budget: %+v", second.Stats)
	}
	thirdReq := req(messagesFor("read-c", "c.txt", "third tail\n"))
	thirdReq.ChunkIntegrityBudgetHit = true
	third := reduceCodexLayer0(thirdReq)
	if third.Stats.ChunkDedupBlocks != 0 || strings.Contains(third.Messages[1].Content[0].Text, "context-chunk") {
		t.Fatalf("exhausted session budget should pre-demote chunk refs: stats=%+v text=%q", third.Stats, third.Messages[1].Content[0].Text)
	}
	if actionForMechanism(third.Stats.PolicyDecisions, savingspolicy.CodexMechanismChunkDedup) != savingspolicy.CodexPolicyFullPass {
		t.Fatalf("policy should explain chunk budget full-pass: %+v", third.Stats.PolicyDecisions)
	}
	if actionForMechanism(third.Stats.PolicyDecisions, savingspolicy.CodexMechanismRepeatedOutput) != savingspolicy.CodexPolicyAllow {
		t.Fatalf("lossless repeated-output should stay allowed under chunk budget pressure: %+v", third.Stats.PolicyDecisions)
	}
}

func TestReduceCodexLayer0ChunkDedupSkipsPatchAndDiffOutputs(t *testing.T) {
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{}, chunkdedup.StoreLimits{MaxSessionRefPct: 100}, func(_, id string, chunk []byte) string {
		return "local-archive://" + id
	})
	largeDiff := strings.Repeat("diff --git a/a.go b/a.go\n+added context line that should stay fresh for patch reasoning\n", 900)
	messagesFor := func(id, command string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"` + command + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: largeDiff}}},
		}
	}
	req := func(messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Messages:            messages,
			SessionID:           "chunk-patch-guard",
			ChunkStore:          store,
			ArchiveRecovery:     true,
			ChunkDedupEnabled:   true,
			ChunkDedupProof:     savingspolicy.CodexProofLive,
			ChunkDedupMinBytes:  0,
			ChunkDedupMaxRefPct: 100,
			PolicyMode:          "max",
		}
	}

	seed := reduceCodexLayer0(req(messagesFor("diff-1", "git diff -- a.go")))
	second := reduceCodexLayer0(req(messagesFor("diff-2", "git diff -- a.go")))
	if seed.Stats.ChunkDedupBlocks != 0 || second.Stats.ChunkDedupBlocks != 0 ||
		strings.Contains(second.Messages[1].Content[0].Text, "context-chunk") {
		t.Fatalf("git diff outputs must not receive chunk refs: seed=%+v second=%+v text=%q", seed.Stats, second.Stats, second.Messages[1].Content[0].Text)
	}

	if !chunkDedupAllowedForCommand("cat a.go", true) ||
		chunkDedupAllowedForCommand("apply_patch <<'PATCH'\n*** Begin Patch\n*** Update File: a.go\nPATCH", false) ||
		chunkDedupAllowedForCommand("git -C /repo show HEAD -- a.go", false) {
		t.Fatal("chunk patch/diff guard classification mismatch")
	}
	for _, tc := range []struct {
		name        string
		commandLine string
		read        bool
		want        bool
	}{
		{name: "plain file read", commandLine: "cat a.go", read: true, want: true},
		{name: "patch file read", commandLine: "cat changes.patch", read: true, want: false},
		{name: "diff file read", commandLine: "sed -n '1,80p' review.diff", read: true, want: false},
		{name: "compound git diff", commandLine: "git -C /repo diff -- a.go | cat", want: false},
		{name: "git log patch", commandLine: "git log -p -- a.go", want: false},
		{name: "git log plain", commandLine: "git log --oneline -5", want: true},
		{name: "gh pr diff", commandLine: "gh pr diff 123", want: false},
		{name: "gh pr view patch", commandLine: "gh pr view 123 --patch", want: false},
		{name: "jj diff", commandLine: "jj diff", want: false},
		{name: "hg diff", commandLine: "hg diff -- a.go", want: false},
		{name: "svn diff", commandLine: "svn diff a.go", want: false},
		{name: "plain diff", commandLine: "diff -u a.go b.go", want: false},
		{name: "search mentioning diff", commandLine: "rg diff docs", want: true},
		{name: "search mentioning git diff", commandLine: `rg "git diff" docs`, want: true},
		{name: "git status remains safe", commandLine: "git -C /repo status --short", want: true},
	} {
		if got := chunkDedupAllowedForCommand(tc.commandLine, tc.read); got != tc.want {
			t.Fatalf("%s: chunkDedupAllowedForCommand(%q, %v)=%v want %v", tc.name, tc.commandLine, tc.read, got, tc.want)
		}
	}
}

func TestReduceCodexLayer0PatchContextExactRepeatUsesRepeatedOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	largeDiff := strings.Repeat("diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old stable patch context\n+new stable patch context\n", 220)
	messagesFor := func(id string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"git diff -- a.go"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: largeDiff}}},
		}
	}
	req := func(messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Route:                     codexLayer0RouteWSSPhaseF,
			Messages:                  messages,
			SessionID:                 "patch-context-exact-repeat",
			PolicyMode:                "max",
			StructuredMutationBlocked: true,
		}
	}

	seed := reduceCodexLayer0(req(messagesFor("diff-seed")))
	if seed.Stats.RepeatedOutputBlocks != 0 || seed.Stats.TokensSaved != 0 ||
		seed.Messages[1].Content[0].Text != largeDiff {
		t.Fatalf("first guarded patch context must full-pass and seed only: stats=%+v text=%q", seed.Stats, seed.Messages[1].Content[0].Text)
	}
	repeated := reduceCodexLayer0(req(messagesFor("diff-repeat")))
	if repeated.Stats.RepeatedOutputBlocks != 1 || repeated.Stats.TokensSaved <= 0 ||
		!strings.Contains(repeated.Messages[1].Content[0].Text, "[context-elided kind=tool-output status=unchanged") ||
		!strings.Contains(repeated.Messages[1].Content[0].Text, "archive=local-archive://") ||
		strings.Contains(repeated.Messages[1].Content[0].Text, "old stable patch context") {
		t.Fatalf("second exact patch context should use repeated-output only: stats=%+v text=%q", repeated.Stats, repeated.Messages[1].Content[0].Text)
	}
	if !hasEvidenceDecision(repeated.Stats.EvidenceDecisions, proxyLayer0MechanismRepeatedOut, "positive_net_savings", evidence.ActionApplied) {
		t.Fatalf("repeated patch context should emit applied repeated evidence: %+v", repeated.Stats.EvidenceDecisions)
	}
	for _, decision := range repeated.Stats.EvidenceDecisions {
		if decision.Mechanism == string(proxyLayer0MechanismRepeatedOut) &&
			decision.Action == evidence.ActionApplied &&
			decision.ContentClass != evidence.ContentDiff {
			t.Fatalf("repeated patch context evidence must stay diff-classified: %+v", decision)
		}
	}
}

func TestReduceCodexLayer0PatchContextRiskFullPassesRepeatPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tests := []struct {
		name   string
		text   string
		reason string
	}{
		{
			name:   "failed apply",
			text:   "diff --git a/a.go b/a.go\nerror: patch failed: a.go:1\nfailed to apply patch\n",
			reason: "wss_patch_context_failed_apply_full_pass",
		},
		{
			name:   "conflict",
			text:   "diff --git a/a.go b/a.go\n<<<<<<< ours\nold\n=======\nnew\n>>>>>>> theirs\n",
			reason: "wss_patch_context_conflict_full_pass",
		},
		{
			name:   "rejected hunk",
			text:   "diff --git a/a.go b/a.go\npatch output saved to a.go.rej\nrejected hunk needs manual review\n",
			reason: "wss_patch_context_rejected_hunk_full_pass",
		},
		{
			name:   "binary diff",
			text:   "diff --git a/a.png b/a.png\nBinary files a/a.png and b/a.png differ\n",
			reason: "wss_patch_context_binary_diff_full_pass",
		},
		{
			name:   "rename",
			text:   "diff --git a/old.go b/new.go\nsimilarity index 98%\nrename from old.go\nrename to new.go\n",
			reason: "wss_patch_context_rename_full_pass",
		},
	}
	messagesFor := func(id string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"git diff -- a.go"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id}}},
		}
	}
	req := func(sessionID string, messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Route:                     codexLayer0RouteWSSPhaseF,
			Messages:                  messages,
			SessionID:                 sessionID,
			PolicyMode:                "max",
			StructuredMutationBlocked: true,
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := strings.Repeat(tt.text, 220)
			sessionID := "patch-context-risk-repeat-" + strings.ReplaceAll(tt.name, " ", "-")
			seedMessages := messagesFor("risk-seed")
			seedMessages[1].Content[0].Text = payload
			repeatMessages := messagesFor("risk-repeat")
			repeatMessages[1].Content[0].Text = payload
			seed := reduceCodexLayer0(req(sessionID, seedMessages))
			repeated := reduceCodexLayer0(req(sessionID, repeatMessages))
			for _, result := range []codexLayer0Result{seed, repeated} {
				if result.Stats.BlocksModified != 0 || result.Stats.TokensSaved != 0 ||
					result.Stats.RepeatedOutputBlocks != 0 ||
					result.Messages[1].Content[0].Text != payload {
					t.Fatalf("%s patch context must full-pass byte-identically: stats=%+v text=%q", tt.name, result.Stats, result.Messages[1].Content[0].Text)
				}
				if !hasEvidenceDecision(result.Stats.EvidenceDecisions, proxyLayer0MechanismRepeatedOut, tt.reason, evidence.ActionFullPass) ||
					!hasEvidenceDecision(result.Stats.EvidenceDecisions, proxyLayer0MechanismCapturedOut, tt.reason, evidence.ActionFullPass) {
					t.Fatalf("%s patch full-pass evidence missing: %+v", tt.name, result.Stats.EvidenceDecisions)
				}
			}
		})
	}
}

func TestReduceCodexLayer0PatchContextNonIdenticalRepeatFullPasses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	diffA := strings.Repeat("diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old alpha\n+new alpha\n", 240)
	diffB := strings.Repeat("diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old beta\n+new beta\n", 240)
	messagesFor := func(id, text string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"git diff -- a.go"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: text}}},
		}
	}
	req := func(messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Route:                     codexLayer0RouteWSSPhaseF,
			Messages:                  messages,
			SessionID:                 "patch-context-non-identical",
			PolicyMode:                "max",
			StructuredMutationBlocked: true,
		}
	}

	_ = reduceCodexLayer0(req(messagesFor("diff-a", diffA)))
	result := reduceCodexLayer0(req(messagesFor("diff-b", diffB)))
	if result.Stats.RepeatedOutputBlocks != 0 || result.Stats.TokensSaved != 0 ||
		result.Messages[1].Content[0].Text != diffB {
		t.Fatalf("non-identical patch context must not reuse prior repeat: stats=%+v text=%q", result.Stats, result.Messages[1].Content[0].Text)
	}
}

func TestReduceCodexLayer0ChunkDedupFullPassesAfterRecentEditUncertainty(t *testing.T) {
	store := chunkdedup.NewStoreWithLimits(chunkdedup.Config{}, chunkdedup.StoreLimits{MaxSessionRefPct: 100}, func(_, id string, chunk []byte) string {
		return "local-archive://" + id
	})
	shared := strings.Repeat("stable command output line with useful context\n", 1200)
	req := func(messages []types.Message) codexLayer0Request {
		return codexLayer0Request{
			Route:               codexLayer0RouteWSSPhaseF,
			Messages:            messages,
			SessionID:           "chunk-edit-uncertainty",
			ChunkStore:          store,
			ArchiveRecovery:     true,
			ChunkDedupEnabled:   true,
			ChunkDedupProof:     savingspolicy.CodexProofLive,
			ChunkDedupMinBytes:  0,
			ChunkDedupMaxRefPct: 100,
			PolicyMode:          "auto",
		}
	}
	seed := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "cmd-1", ToolName: "exec_command", ToolInput: `{"cmd":"python emit_context.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "cmd-1", Text: shared + "first tail\n"}}},
	}
	freshAfterEdit := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "edit-1", ToolName: "apply_patch", ToolInput: `{"path":"src/x.go","patch":"*** Begin Patch\n*** Update File: src/x.go\n@@\n-old\n+new\n*** End Patch"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "edit-1", Text: "patch applied"}}},
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "cmd-2", ToolName: "exec_command", ToolInput: `{"cmd":"python emit_context.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "cmd-2", Text: shared + "fresh tail after edit\n"}}},
	}

	reduceCodexLayer0(req(seed))
	out := reduceCodexLayer0(req(freshAfterEdit))
	if out.Stats.ChunkDedupBlocks != 0 || strings.Contains(out.Messages[3].Content[0].Text, "context-chunk") {
		t.Fatalf("fresh post-edit command output must not receive chunk refs: stats=%+v text=%q", out.Stats, out.Messages[3].Content[0].Text)
	}
	if actionForMechanism(out.Stats.PolicyDecisions, savingspolicy.CodexMechanismChunkDedup) != savingspolicy.CodexPolicyFullPass {
		t.Fatalf("chunk mechanism should full-pass on edit uncertainty: %+v", out.Stats.PolicyDecisions)
	}
}

func TestReduceCodexLayer0ChunkDedupRequiresGateAndRecovery(t *testing.T) {
	t.Parallel()
	store := chunkdedup.NewStore(chunkdedup.Config{}, nil)
	body := uniqueProxyReadPayload("large output")
	msgs := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "cmd", ToolName: "exec_command", ToolInput: `{"cmd":"python report.py"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "cmd", Text: body}}},
	}
	reduceCodexLayer0(codexLayer0Request{Messages: msgs, SessionID: "sess-gate", ChunkDedupEnabled: true, ChunkDedupProof: savingspolicy.CodexProofLive, ChunkDedupMinBytes: 0, ChunkStore: store, ArchiveRecovery: true})
	out := reduceCodexLayer0(codexLayer0Request{Messages: msgs, SessionID: "sess-gate", ChunkDedupEnabled: false, ChunkDedupMinBytes: 0, ChunkStore: store, ArchiveRecovery: false})
	if out.Stats.ChunkDedupBlocks != 0 || strings.Contains(out.Messages[1].Content[0].Text, "context-chunk") {
		t.Fatalf("disabled chunk dedup must stay byte-equal: %+v", out.Stats)
	}
}

func TestCodexChunkDedupSettingsAutoPolicyEnablesRecoverableChunks(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = false
	p := New(cfg)
	settings := p.codexChunkDedupSettings()
	if !settings.Enabled || settings.Store == nil || settings.Explicit || settings.PolicyMode != "auto" ||
		!settings.ArchiveRecovery || settings.Proof != savingspolicy.CodexProofLive ||
		settings.MinBytes != cfg.Compression.OutputReduce.CodexChunkDedupMinBytes ||
		settings.MaxRefPct != cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent {
		t.Fatalf("auto policy should make recoverable chunk dedup available: %+v", settings)
	}
	cfg.Compression.OutputReduce.CodexSavingsPolicyMode = "conservative"
	p = New(cfg)
	settings = p.codexChunkDedupSettings()
	if settings.Enabled || settings.Store != nil || settings.ArchiveRecovery {
		t.Fatalf("conservative policy without explicit recovery should not enable chunk dedup: %+v", settings)
	}
	cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
	p = New(cfg)
	settings = p.codexChunkDedupSettings()
	if !settings.Enabled || settings.Store == nil || !settings.Explicit || settings.PolicyMode != "conservative" ||
		!settings.ArchiveRecovery || settings.Proof != savingspolicy.CodexProofLive ||
		settings.MinBytes != cfg.Compression.OutputReduce.CodexChunkDedupMinBytes ||
		settings.MaxRefPct != cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent {
		t.Fatalf("explicit chunk dedup settings not enabled with recovery note: %+v", settings)
	}
}

func TestCodexHTTPChunkDedupSettingsScopeToCodexChatGPT(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.CodexSavingsPolicyMode = "max"
	cfg.Compression.OutputReduce.CodexChunkDedupEnabled = true
	cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = true
	p := New(cfg)

	codexSettings := p.codexHTTPChunkDedupSettings(types.CodexChatGPT)
	if !codexSettings.Enabled || codexSettings.Store == nil || !codexSettings.Explicit || !codexSettings.ArchiveRecovery ||
		codexSettings.PolicyMode != "max" || codexSettings.Proof != savingspolicy.CodexProofLive ||
		codexSettings.MinBytes != cfg.Compression.OutputReduce.CodexChunkDedupMinBytes ||
		codexSettings.MaxRefPct != cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent {
		t.Fatalf("codex http route should allow recoverable chunk refs: %+v", codexSettings)
	}

	openAISettings := p.codexHTTPChunkDedupSettings(types.OpenAI)
	if openAISettings.Enabled || openAISettings.Store != nil || openAISettings.Explicit || openAISettings.ArchiveRecovery ||
		openAISettings.PolicyMode != "max" || openAISettings.Proof != savingspolicy.CodexProofLive ||
		openAISettings.MinBytes != cfg.Compression.OutputReduce.CodexChunkDedupMinBytes ||
		openAISettings.MaxRefPct != cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent {
		t.Fatalf("non-codex http route must not emit chunk/archive refs: %+v", openAISettings)
	}
}

func TestApplyProxyLayer0ReadDeltaMissTelemetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-1", ToolName: "Read", ToolInput: `{"path":"notes.txt"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: strings.Repeat("plain archived seed line\n", 4)}}},
	}
	_, stats := applyProxyLayer0WithSessionAndToolUsesDetailed(messages, "sess-miss", nil)
	if stats.ToolResultBlocks != 1 || stats.CommandResolvedBlocks != 1 || stats.ReadDeltaAttempts != 1 ||
		stats.ReadDeltaMisses != 1 || stats.TokensSaved != 0 || stats.ReadDeltaBlocks != 0 {
		t.Fatalf("read-delta miss stats mismatch: %+v", stats)
	}
	if len(stats.CacheEvents) != 1 || stats.CacheEvents[0].Action != proxyLayer0CacheMiss ||
		stats.CacheEvents[0].Reason != "first_observation_seeded" {
		t.Fatalf("read-delta cache miss event mismatch: %+v", stats.CacheEvents)
	}
	if !hasEvidenceDecision(stats.EvidenceDecisions, proxyLayer0MechanismReadDelta, "read_delta_first_observation_seeded", evidence.ActionShadow) {
		t.Fatalf("read-delta miss should emit observe-only shadow evidence: %+v", stats.EvidenceDecisions)
	}
}

func TestProxyReadDeltaWorkdirSeparatesRelativePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dirA := t.TempDir()
	dirB := t.TempDir()
	command := func(workdir string) string {
		input := `{"cmd":"cat shared.txt","workdir":` + strconv.Quote(workdir) + `}`
		return proxyLayer0CommandLine(types.ContentBlock{ToolName: "exec_command", ToolInput: input})
	}
	largeA := uniqueProxyReadPayload("alpha")
	if out, changed := compactProxyReadDelta("sess-workdir", "", command(dirA), largeA, filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("first workdir A read must not delta, changed=%v out=%q", changed, out)
	}
	if out, changed := compactProxyReadDelta("sess-workdir", "", command(dirA), largeA, filter.FileReadContext{Mode: "scan"}, 0); !changed || !strings.Contains(out, dirA) {
		t.Fatalf("second workdir A read should delta against A path, changed=%v out=%q", changed, out)
	}
	largeB := uniqueProxyReadPayload("beta")
	if out, changed := compactProxyReadDelta("sess-workdir", "", command(dirB), largeB, filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("first workdir B read must not reuse workdir A cache, changed=%v out=%q", changed, out)
	}
}

func TestProxyReadDeltaIgnoresCodexExecEnvelopeVolatileHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	payload := uniqueProxyReadPayload("envelope")
	first := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	second := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload

	if out, changed := compactProxyReadDelta("sess-envelope", "turn-1", "cat AGENTS.md", first, filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("first envelope read must seed without mutation, changed=%v out=%q", changed, out)
	}
	out, changed := compactProxyReadDelta("sess-envelope", "turn-2", "cat AGENTS.md", second, filter.FileReadContext{Mode: "scan"}, 0)
	if !changed {
		t.Fatalf("second envelope read should delta despite volatile header")
	}
	if strings.Contains(out, payload) {
		t.Fatalf("unchanged envelope read should not repeat payload: %q", out)
	}
	if !strings.Contains(out, "Chunk ID: bbb222") || !strings.Contains(out, "[context-elided kind=file-read status=unchanged") {
		t.Fatalf("envelope header and unchanged marker not preserved: %q", out)
	}
}

func TestProxyRepeatedOutputIgnoresCodexExecEnvelopeVolatileHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	payload := strings.Repeat("internal/proxy/example.go:42:stable search result\n", 40)
	first := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 900\nOutput:\n" + payload
	second := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 901\nOutput:\n" + payload
	key := "search:rg\t-n\tstable\t/Users/example/CODE/Slimference/internal/proxy"
	command := "rg -n stable /Users/example/CODE/Slimference/internal/proxy"

	if out, ok := compactProxyRepeatedToolOutputWithKey("sess-repeated-envelope", key, command, first); ok || out != "" {
		t.Fatalf("first envelope output must seed without mutation, ok=%v out=%q", ok, out)
	}
	out, ok := compactProxyRepeatedToolOutputWithKey("sess-repeated-envelope", key, command, second)
	if !ok {
		t.Fatalf("second envelope output should collapse despite volatile header")
	}
	if strings.Contains(out, payload) {
		t.Fatalf("unchanged repeated output should not repeat payload: %q", out)
	}
	if !strings.Contains(out, "Chunk ID: bbb222") ||
		!strings.Contains(out, "[context-elided kind=search-output status=same-match-set") {
		t.Fatalf("envelope header and unchanged output marker not preserved: %q", out)
	}
}

func TestProxyRepeatedOutputDiffStatUnchangedMarkerKeepsSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	payload := wssDiffStatFixture(120)
	first := "Chunk ID: aaa111\nWall time: 0.0000 seconds\nProcess exited with code 0\nOriginal token count: 2400\nOutput:\n" + payload
	second := "Chunk ID: bbb222\nWall time: 0.1234 seconds\nProcess exited with code 0\nOriginal token count: 2401\nOutput:\n" + payload
	command := "git diff --stat"
	key := proxyLayer0QualityToolKey(command)
	if key == "" {
		t.Fatal("git diff --stat must have a repeated-output quality key")
	}

	if out, ok := compactProxyRepeatedToolOutputWithKey("sess-repeated-diffstat-summary", key, command, first); ok || out != "" {
		t.Fatalf("first diffstat envelope output must seed without mutation, ok=%v out=%q", ok, out)
	}
	out, ok := compactProxyRepeatedToolOutputWithKey("sess-repeated-diffstat-summary", key, command, second)
	if !ok {
		t.Fatalf("second diffstat envelope output should collapse despite volatile header")
	}
	for _, want := range []string{
		"Chunk ID: bbb222",
		"[context-elided kind=tool-output status=unchanged command=\"git diff --stat\"",
		"[unchanged-evidence]",
		"[git diff --stat] 120 file(s)",
		"summary: 120 files changed, 1440 insertions(+), 720 deletions(-)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("unchanged diffstat marker missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "file_xxxxxxxxxxxx_119.go") {
		t.Fatalf("unchanged diffstat marker must not leak the file list: %q", out)
	}
}

func uniqueProxyReadPayload(prefix string) string {
	var b strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "%s unique payload line %03d with nonrepeating value %08x\n", prefix, i, i*7919+17)
	}
	return b.String()
}

func TestApplyProxyLayer0WithSessionRecentEditBypassesReadDeltaAndCommentStrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := sessions.ObserveHookFile(sessions.DefaultHookStateDir(home), "sess-edit", "main.go", "edit"); err != nil {
		t.Fatal(err)
	}
	source := strings.Repeat("// keep this recent edit comment\n", 20) + "package main\n"
	msgs := proxyReadMessages(source)
	out, saved := applyProxyLayer0WithSession(msgs, "sess-edit")
	if strings.Contains(out[1].Content[0].Text, "uri=local-archive://") ||
		strings.Contains(out[1].Content[0].Text, "Read delta") ||
		!strings.Contains(out[1].Content[0].Text, "// keep this recent edit comment") {
		t.Fatalf("recent edit should bypass read delta and preserve content signal, saved=%d text=%q", saved, out[1].Content[0].Text)
	}
	if _, err := os.Stat(sessions.DefaultHookStateDir(home)); err != nil {
		t.Fatal(err)
	}
}

func TestProxyEditedPathsFromMessages(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Role: "tool", Content: []types.ContentBlock{{
			Type:         "tool_result",
			ToolResultID: "edit-1",
			Text:         "patch applied",
		}}},
	}
	remembered := map[string]types.ContentBlock{
		"edit-1": {
			Type:      "tool_use",
			ToolUseID: "edit-1",
			ToolName:  "apply_patch",
			ToolInput: `{"workdir":"/repo","patch":"*** Begin Patch\n*** Update File: src/main.go\n*** Add File: src/new.go\n*** End Patch"}`,
		},
	}
	paths := proxyEditedPathsFromMessages(msgs, remembered)
	got := strings.Join(paths, "\n")
	for _, want := range []string{"/repo/src/main.go", "/repo/src/new.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("edited paths missing %s: %#v", want, paths)
		}
	}

	rawPatch := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "apply_patch",
		ToolInput: `"*** Update File: a.go\n--- a/old.go\t2026-05-30\n+++ b/new.go\t2026-05-30\n"`,
	}
	paths = proxyEditedPathsFromMessages([]types.Message{{Content: []types.ContentBlock{rawPatch}}}, nil)
	got = strings.Join(paths, ",")
	if got != "a.go,old.go,new.go" {
		t.Fatalf("raw patch paths = %#v", paths)
	}
}

func proxyReadMessages(text string) []types.Message {
	return []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: "read-1", ToolName: "Read", ToolInput: `{"path":"main.go"}`}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "read-1", Text: text}}},
	}
}

func TestProxyLayer0SmallHelpers(t *testing.T) {
	t.Parallel()

	if got := proxyStringArray(nil); got != nil {
		t.Fatalf("nil raw array=%#v", got)
	}
	if got := proxyStringArray(json.RawMessage(`{"no":"array"}`)); got != nil {
		t.Fatalf("invalid array=%#v", got)
	}
	if got := proxyStringArray(json.RawMessage(`["go","test"]`)); len(got) != 2 || got[1] != "test" {
		t.Fatalf("array=%#v", got)
	}
	if !looksLikeShellTool(" Bash ") || !looksLikeShellTool("terminal.exec") || looksLikeShellTool("read") {
		t.Fatal("shell tool classifier mismatch")
	}
	if !looksLikeShellExecutable("/opt/homebrew/bin/bash") || looksLikeShellExecutable("fish") {
		t.Fatal("shell executable classifier mismatch")
	}
	if normalizeLayer0CommandLine("/bin/sh -c 'git status --short'") != "git status --short" ||
		normalizeLayer0CommandLine("git status --short") != "git status --short" {
		t.Fatal("shell wrapper normalization mismatch")
	}
	if got := normalizeLayer0CommandLine("cd /repo/project && rg needle docs"); got != "rg needle /repo/project/docs" {
		t.Fatalf("cd-wrapped search must normalize to repo-scoped key: %q", got)
	}
	if got := applyWorkdirToLayer0Command("rg needle docs", "/repo/project"); got != "rg needle /repo/project/docs" {
		t.Fatalf("workdir search must normalize to repo-scoped key: %q", got)
	}
	if got := normalizeLayer0CommandLine("cd /repo/project && awk 'NR>=10 && NR<=20 {print}' src/main.go"); !strings.Contains(got, "/repo/project/src/main.go") {
		t.Fatalf("cd-wrapped awk read must normalize to repo path: %q", got)
	}
	if got := applyWorkdirToLayer0Command("awk 'NR>=10 && NR<=20 {print}' src/main.go", "/repo/project"); !strings.Contains(got, "/repo/project/src/main.go") {
		t.Fatalf("workdir awk read must normalize to repo path: %q", got)
	}
	if got := applyWorkdirToLayer0Command("awk 'NR>=10&&NR<=20{print $0}' src/main.go", "/repo/project"); readRequestFromCommandLine(got).FilePath != "/repo/project/src/main.go" {
		t.Fatalf("workdir awk $0 read must remain parseable after normalization: %q", got)
	}
	for _, command := range []string{
		"reconc status .",
		"cd /repo && tools/reconc/dist/reconc-0.5.0-darwin-arm64 check .",
		"/bin/bash -lc 'cd /repo && reconc check .'",
		"go run ./cmd/reconc check .",
	} {
		if !proxyCommandLineInvokesReconc(command) {
			t.Fatalf("Reconc command was not recognized: %q", command)
		}
	}
	if proxyCommandLineInvokesReconc("go test ./...") || proxyCommandLineInvokesReconc("rg reconc docs") {
		t.Fatal("non-Reconc commands must not be treated as Reconc evidence commands")
	}
	if got := applyWorkdirToGitCommand("git -C /other status --short", "/repo/project"); got != "git -C /other status --short" {
		t.Fatalf("git -C should not be rewritten: %q", got)
	}
	if stripSlimferenceFilterWrapper([]string{"slimference", "filter", "--bad", "git"}) != "" ||
		stripSlimferenceFilterWrapper([]string{"other", "filter", "git"}) != "" ||
		stripSlimferenceFilterWrapper([]string{"slimference", "filter"}) != "" ||
		stripSlimferenceFilterWrapper([]string{"slimference", "filter", "--"}) != "" {
		t.Fatal("slimference wrapper rejection mismatch")
	}
	if !looksLikeReadTool("open_file") || !looksLikeReadTool("read_file") || looksLikeReadTool("shell") {
		t.Fatal("read tool classifier mismatch")
	}
	if joinShellArgs([]string{"", "plain", "two words"}) != `"" plain "two words"` {
		t.Fatal("joinShellArgs quoting mismatch")
	}
	if quoteShellArg("plain") != "plain" || quoteShellArg("two words") != `"two words"` ||
		quoteShellArg("NR>=10&&NR<=20{print $0}") != `'NR>=10&&NR<=20{print $0}'` {
		t.Fatal("quoteShellArg mismatch")
	}
}

func TestProxyRepeatedSearchOutputKeepsRepoScopedKeys(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	sessionID := "search-repo-scope"
	outputA := strings.Repeat("src/a.go:10:TODO repo A context\n", 30)
	outputB := strings.Repeat("src/a.go:10:TODO repo B context\n", 30)
	cmdA := applyWorkdirToLayer0Command("rg -n TODO src", "/repo/a")
	cmdB := applyWorkdirToLayer0Command("rg -n TODO src", "/repo/b")
	if cmdA == cmdB || !strings.Contains(cmdA, "/repo/a/src") || !strings.Contains(cmdB, "/repo/b/src") {
		t.Fatalf("repo-scoped search commands not distinct: A=%q B=%q", cmdA, cmdB)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, cmdA, outputA); ok || out != "" {
		t.Fatalf("first repo A search should seed without collapse: ok=%v out=%q", ok, out)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, cmdB, outputB); ok || out != "" {
		t.Fatalf("first repo B search must not reuse repo A key: ok=%v out=%q", ok, out)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, cmdB, outputB); !ok ||
		!strings.Contains(out, "kind=search-output") ||
		!strings.Contains(out, "status=same-match-set") ||
		!strings.Contains(out, "/repo/b/src") {
		t.Fatalf("second repo B search should collapse on its own key: ok=%v out=%q", ok, out)
	}
}

func TestProxyRepeatedSearchOutputRejectsImplicitCwdKey(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	sessionID := "search-implicit-cwd"
	command := "rg -n TODO src"
	output := strings.Repeat("src/a.go:10:TODO repo context\n", 30)
	if key := proxyLayer0QualityToolKey(command); key != "" {
		t.Fatalf("implicit-cwd search must not receive a reusable cache key: %q", key)
	}
	if out, ok := compactProxyRepeatedToolOutput(sessionID, command, output); ok || out != "" {
		t.Fatalf("implicit-cwd search must full-pass instead of seeding/collapsing: ok=%v out=%q", ok, out)
	}
}

func TestProxyRepeatedGenericOutputKeepsWorkdirScopedKeys(t *testing.T) {
	tmp := t.TempDir()
	oldHome := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { proxyUserHomeDir = oldHome })

	sessionID := "generic-repo-scope"
	output := strings.Repeat("ok  pkg/example  cached test output\n", 30)
	messagesFor := func(id, workdir string) []types.Message {
		return []types.Message{
			{Role: "assistant", Content: []types.ContentBlock{{Type: "tool_use", ToolUseID: id, ToolName: "exec_command", ToolInput: `{"cmd":"go test ./...","workdir":"` + workdir + `"}`}}},
			{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: id, Text: output}}},
		}
	}

	firstA := reduceCodexLayer0(codexLayer0Request{Messages: messagesFor("call-a", "/repo/a"), SessionID: sessionID})
	if firstA.Stats.RepeatedOutputBlocks != 0 || firstA.Stats.TokensSaved != 0 {
		t.Fatalf("first repo A generic command should seed only: %+v", firstA.Stats)
	}
	firstB := reduceCodexLayer0(codexLayer0Request{Messages: messagesFor("call-b", "/repo/b"), SessionID: sessionID})
	if firstB.Stats.RepeatedOutputBlocks != 0 || firstB.Stats.TokensSaved != 0 {
		t.Fatalf("first repo B generic command must not reuse repo A key: %+v text=%q", firstB.Stats, firstB.Messages[1].Content[0].Text)
	}
	secondB := reduceCodexLayer0(codexLayer0Request{Messages: messagesFor("call-b2", "/repo/b"), SessionID: sessionID})
	if secondB.Stats.RepeatedOutputBlocks != 1 || secondB.Stats.TokensSaved <= 0 ||
		!strings.Contains(secondB.Messages[1].Content[0].Text, "kind=tool-output") {
		t.Fatalf("second repo B generic command should collapse on workdir key: %+v text=%q", secondB.Stats, secondB.Messages[1].Content[0].Text)
	}

	useA := messagesFor("key-a", "/repo/a")[0].Content[0]
	useB := messagesFor("key-b", "/repo/b")[0].Content[0]
	keyA := proxyLayer0QualityToolKeyForUse(useA, proxyLayer0CommandLine(useA))
	keyB := proxyLayer0QualityToolKeyForUse(useB, proxyLayer0CommandLine(useB))
	if keyA == keyB || !strings.Contains(keyA, "cwd:/repo/a") || !strings.Contains(keyB, "cwd:/repo/b") {
		t.Fatalf("workdir-scoped keys not distinct: A=%q B=%q", keyA, keyB)
	}
}

func TestProxyLayer0GenericOutputKeyIncludesDependencyFingerprint(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test/a\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	use := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"go test ./...","workdir":"` + repo + `"}`,
	}
	commandLine := proxyLayer0CommandLine(use)
	first := proxyLayer0QualityToolKeyForUse(use, commandLine)
	if !strings.Contains(first, "cwd:"+repo) || !strings.Contains(first, ":deps:") {
		t.Fatalf("dependency-sensitive key missing cwd/deps: %q", first)
	}

	if err := os.WriteFile(filepath.Join(repo, "go.sum"), []byte("example.test/dep v1.0.0 h1:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := proxyLayer0QualityToolKeyForUse(use, commandLine)
	if first == second || !strings.Contains(second, ":deps:") {
		t.Fatalf("dependency fingerprint should change after lockfile update: first=%q second=%q", first, second)
	}

	plain := types.ContentBlock{
		Type:      "tool_use",
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"date","workdir":"` + repo + `"}`,
	}
	plainKey := proxyLayer0QualityToolKeyForUse(plain, proxyLayer0CommandLine(plain))
	if strings.Contains(plainKey, ":deps:") || !strings.Contains(plainKey, "cwd:"+repo) {
		t.Fatalf("non-sensitive command should stay cwd-scoped without dependency hash: %q", plainKey)
	}
}

func TestProxyReadDeltaFailOpenBranches(t *testing.T) {
	t.Parallel()
	if out, changed := compactProxyReadDelta("", "", "cat main.go", "content", filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("empty session should fail open, out=%q changed=%v", out, changed)
	}
	if out, changed := compactProxyReadDelta("sess", "", "echo nope", "content", filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("non-read command should fail open, out=%q changed=%v", out, changed)
	}
	if out, changed := compactProxyRepeatedToolOutput("", "python report.py", "content"); changed || out != "" {
		t.Fatalf("empty session repeated-output should fail open, out=%q changed=%v", out, changed)
	}
	ctx := proxyReadFileContext("", "cat main.go")
	if ctx.RecentlyEdited {
		t.Fatal("empty session should not mark recent edit")
	}
	ctx = proxyReadFileContext("sess", "echo nope")
	if ctx.RecentlyEdited {
		t.Fatal("non-read command should not mark recent edit")
	}
}

func TestProxyReadDeltaHomeErrorBranches(t *testing.T) {
	orig := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return "", errors.New("home") }
	defer func() { proxyUserHomeDir = orig }()

	if out, changed := compactProxyReadDelta("sess", "", "cat main.go", strings.Repeat("content\n", 20), filter.FileReadContext{Mode: "scan"}, 0); changed || out != "" {
		t.Fatalf("home error should fail open, out=%q changed=%v", out, changed)
	}
	if out, changed := compactProxyRepeatedToolOutput("sess", "python report.py", strings.Repeat("content\n", 80)); changed || out != "" {
		t.Fatalf("home error should fail open for repeated-output, out=%q changed=%v", out, changed)
	}
	ctx := proxyReadFileContext("sess", "cat main.go")
	if ctx.RecentlyEdited {
		t.Fatal("home error should not mark recent edit")
	}
}

func TestToolPruneRetryHelpers(t *testing.T) {
	if resolveToolPruneSessionKey("session", "req") != "session" || resolveToolPruneSessionKey("", "req") != "req" {
		t.Fatal("tool-prune session key fallback mismatch")
	}
	cfg := config.Defaults()
	cfg.Proxy.ServerStateEnabled = true
	p := New(cfg)
	p.serverState.Set("conv", "resp_prev")
	body := []byte(`{"messages":[{"role":"user","content":"old"},{"role":"assistant","content":"ok"},{"role":"user","content":"latest"}]}`)
	rewritten := p.rewriteToolPruneRetryBody(types.OpenAI, body, true, "conv")
	if string(rewritten) == string(body) || !strings.Contains(string(rewritten), `"previous_response_id":"resp_prev"`) || strings.Contains(string(rewritten), "old") {
		t.Fatalf("retry body was not server-state rewritten: %s", rewritten)
	}
	if got := p.rewriteToolPruneRetryBody(types.OpenAI, body, false, "conv"); string(got) != string(body) {
		t.Fatalf("unused server state should keep body: %s", got)
	}
}

func actionForMechanism(decisions []savingspolicy.CodexMechanismDecision, mechanism savingspolicy.CodexMechanism) savingspolicy.CodexPolicyAction {
	for _, decision := range decisions {
		if decision.Mechanism == mechanism {
			return decision.Action
		}
	}
	return ""
}

func hasTokenNeutralEvidenceDecision(decisions []evidence.BlockDecision, mechanism proxyLayer0Mechanism, reason string, action evidence.Action) bool {
	for _, decision := range decisions {
		if decision.Mechanism != string(mechanism) || decision.Reason != reason || decision.Action != action {
			continue
		}
		return decision.OriginalTokens == 0 &&
			decision.FinalTokens == 0 &&
			decision.SavedTokens == 0 &&
			decision.NetTokens == 0 &&
			decision.FootprintScore == 0
	}
	return false
}
