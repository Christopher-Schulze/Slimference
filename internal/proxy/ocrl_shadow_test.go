package proxy

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/contextledger"
	dbg "github.com/slimference/slimference/internal/debug"
)

func TestBuildOCRLShadowSummaryUsesArchiveTokensWithoutExpansionStats(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := contentarchive.DefaultDir(home)
	entry, err := contentarchive.Put(dir, contentarchive.Input{
		SessionID: "sess-ocrl-shadow",
		SubLayer:  "ocrl-shadow-test",
		Original:  strings.Repeat("archive backed old context token payload ", 120),
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		t.Fatalf("put archive: entry=%+v err=%v", entry, err)
	}
	capsule, err := contextledger.BuildFileCapsule(contextledger.FileObservation{
		SessionID:    "sess-ocrl-shadow",
		TurnID:       "old-turn",
		Path:         "internal/proxy/old.go",
		RepoRoot:     "/repo",
		Range:        "full",
		ArchiveID:    entry.URI,
		FullPassTurn: "old-turn",
	})
	if err != nil {
		t.Fatalf("file capsule: %v", err)
	}
	before, err := contentarchive.LoadStats(dir)
	if err != nil {
		t.Fatalf("load stats before: %v", err)
	}

	cfg := config.Defaults()
	cfg.Compression.OCRL.Mode = "max"
	p := New(cfg)
	summary := p.buildOCRLShadowContextLedgerSummary(proxyLayer0Stats{
		LedgerFileCapsules: 1,
		LedgerCapsules:     []contextledger.Capsule{capsule},
	}, "sess-ocrl-shadow", "active-turn", 2)

	if summary.OCRLReason != string(contextledger.OCRLReasonRouteNotEligible) || !summary.OCRLShadowOnly {
		t.Fatalf("OCRL summary should stay WSS route shadow-only: %+v", summary)
	}
	if summary.OCRLCandidateCapsules != 1 || summary.OCRLArchiveExpansions != 1 || summary.OCRLOriginalTokens <= summary.OCRLReplacementTokens {
		t.Fatalf("OCRL shadow token accounting missing: %+v", summary)
	}
	if summary.OCRLShadowSavedTokens <= 0 {
		t.Fatalf("expected positive would-save shadow signal: %+v", summary)
	}
	after, err := contentarchive.LoadStats(dir)
	if err != nil {
		t.Fatalf("load stats after: %v", err)
	}
	if after.Expanded != before.Expanded || !after.LastExpanded.Equal(before.LastExpanded) {
		t.Fatalf("OCRL shadow must not mutate expansion stats: before=%+v after=%+v", before, after)
	}
}

func TestWSRecordRequestPlanIncludesOCRLShadowTelemetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := contentarchive.DefaultDir(home)
	entry, err := contentarchive.Put(dir, contentarchive.Input{
		SessionID: "sess-ocrl-plan",
		SubLayer:  "ocrl-plan-test",
		Original:  strings.Repeat("old request context eligible for OCRL ", 120),
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		t.Fatalf("put archive: entry=%+v err=%v", entry, err)
	}
	capsule, err := contextledger.BuildFileCapsule(contextledger.FileObservation{
		SessionID:    "sess-ocrl-plan",
		TurnID:       "old-turn",
		Path:         "internal/proxy/plan.go",
		RepoRoot:     "/repo",
		Range:        "full",
		ArchiveID:    entry.URI,
		FullPassTurn: "old-turn",
	})
	if err != nil {
		t.Fatalf("file capsule: %v", err)
	}
	cfg := config.Defaults()
	cfg.Compression.OCRL.Mode = "max"
	p := New(cfg)
	p.debugRecorder = dbg.NewRecorder(10, "")
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.recordRequestPlan([]byte(`{"model":"gpt-5-codex","input":[]}`), []byte(`{"model":"gpt-5-codex","input":[]}`), nil, proxyLayer0Stats{
		LedgerFileCapsules: 1,
		LedgerCapsules:     []contextledger.Capsule{capsule},
	}, false, "", 0, wssRequestMeta{
		SessionID:          "sess-ocrl-plan",
		PreviousResponseID: "active-turn",
		Model:              "gpt-5-codex",
	})

	last := p.debugRecorder.Last(1, false)
	if len(last) != 1 {
		t.Fatalf("missing request summary")
	}
	if last[0].ContextLedger.OCRLReason != string(contextledger.OCRLReasonRouteNotEligible) ||
		last[0].ContextLedger.OCRLCandidateCapsules != 1 ||
		last[0].ContextLedger.OCRLShadowSavedTokens <= 0 {
		t.Fatalf("summary missing OCRL shadow telemetry: %+v", last[0].ContextLedger)
	}
	if last[0].TurnID != "active-turn" {
		t.Fatalf("request summary should retain WSS active turn id: %+v", last[0])
	}
}

func TestBuildOCRLShadowSummaryDefaultModeStaysTelemetryOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := contentarchive.DefaultDir(home)
	entry, err := contentarchive.Put(dir, contentarchive.Input{
		SessionID: "sess-ocrl-default",
		SubLayer:  "ocrl-default-test",
		Original:  strings.Repeat("default shadow old context ", 120),
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		t.Fatalf("put archive: entry=%+v err=%v", entry, err)
	}
	capsule, err := contextledger.BuildFileCapsule(contextledger.FileObservation{
		SessionID:    "sess-ocrl-default",
		TurnID:       "old-turn",
		Path:         "internal/proxy/default.go",
		RepoRoot:     "/repo",
		Range:        "full",
		ArchiveID:    entry.URI,
		FullPassTurn: "old-turn",
	})
	if err != nil {
		t.Fatalf("file capsule: %v", err)
	}

	summary := New(config.Defaults()).buildOCRLShadowContextLedgerSummary(proxyLayer0Stats{
		LedgerFileCapsules: 1,
		LedgerCapsules:     []contextledger.Capsule{capsule},
	}, "sess-ocrl-default", "active-turn", 0)

	if summary.OCRLMode != string(contextledger.OCRLModeShadow) ||
		summary.OCRLReason != string(contextledger.OCRLReasonShadowOnly) ||
		!summary.OCRLShadowOnly ||
		summary.OCRLShadowSavedTokens <= 0 {
		t.Fatalf("default OCRL should compute shadow-only telemetry: %+v", summary)
	}
}

func TestBuildOCRLShadowSummaryOffModeClaimsNoSavings(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OCRL.Mode = "off"
	p := New(cfg)
	capsule, err := contextledger.BuildFileCapsule(contextledger.FileObservation{
		SessionID:    "sess-ocrl-off",
		TurnID:       "old-turn",
		Path:         "internal/proxy/off.go",
		RepoRoot:     "/repo",
		Range:        "full",
		ArchiveID:    "local-archive://sha256:missing",
		FullPassTurn: "old-turn",
	})
	if err != nil {
		t.Fatalf("file capsule: %v", err)
	}

	summary := p.buildOCRLShadowContextLedgerSummary(proxyLayer0Stats{
		LedgerFileCapsules: 1,
		LedgerCapsules:     []contextledger.Capsule{capsule},
	}, "sess-ocrl-off", "active-turn", 0)

	if summary.OCRLMode != string(contextledger.OCRLModeOff) ||
		summary.OCRLReason != string(contextledger.OCRLReasonOff) ||
		summary.OCRLShadowSavedTokens != 0 ||
		summary.OCRLArchiveExpansions != 0 {
		t.Fatalf("off OCRL must not claim savings or touch archives: %+v", summary)
	}
}
