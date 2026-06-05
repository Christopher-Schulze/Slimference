package proxy

import (
	"errors"
	"strings"

	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/contextledger"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

func (p *Proxy) buildOCRLShadowContextLedgerSummary(stats proxyLayer0Stats, sessionID, activeTurnID string, reReadCount int) dbg.ContextLedgerSummary {
	summary := dbg.ContextLedgerSummary{
		TelemetryOnly:   true,
		CommandCapsules: stats.LedgerCommandCapsules,
		FileCapsules:    stats.LedgerFileCapsules,
		SearchCapsules:  stats.LedgerSearchCapsules,
		FailureCapsules: stats.LedgerFailureCapsules,
		ReReadCount:     reReadCount,
	}
	if p == nil || len(stats.LedgerCapsules) == 0 || strings.TrimSpace(sessionID) == "" {
		return summary
	}
	result := contextledger.BuildOCRLReplacement(stats.LedgerCapsules, contextledger.OCRLPolicy{
		Mode:  contextledger.OCRLModeMax,
		Route: contextledger.OCRLRouteCodexWSS,
		Selection: contextledger.SelectionPolicy{
			SessionID:    sessionID,
			ActiveTurnID: activeTurnID,
			MaxCapsules:  512,
		},
		ArchiveLoader:            p.ocrlShadowArchiveLoader(),
		CountTokens:              tokens.ForProvider(types.CodexChatGPT).CountString,
		UseArchiveOriginalTokens: true,
		RecoveryOverheadTokens:   0,
		MinNetSavedTokens:        1,
		MaxReplacementTokens:     0,
	})
	summary.OCRLMode = string(contextledger.OCRLModeMax)
	summary.OCRLRoute = string(contextledger.OCRLRouteCodexWSS)
	summary.OCRLReason = string(result.Reason)
	summary.OCRLShadowOnly = result.ShadowOnly
	summary.OCRLCandidateCapsules = result.Selection.Capsules
	summary.OCRLVerbatimCapsules = result.Selection.Verbatim
	summary.OCRLRejectedCapsules = result.Selection.Rejected
	summary.OCRLArchiveExpansions = result.ArchiveExpansions
	summary.OCRLOriginalTokens = result.OriginalTokens
	summary.OCRLReplacementTokens = result.ReplacementTokens
	summary.OCRLRecoveryOverhead = result.RecoveryOverheadTokens
	if result.NetSavedTokens > 0 {
		summary.OCRLShadowSavedTokens = result.NetSavedTokens
	}
	return summary
}

func (p *Proxy) ocrlShadowArchiveLoader() contextledger.ArchiveLoader {
	home, err := proxyUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return func(string) ([]byte, error) {
			return nil, errors.New("user home unavailable")
		}
	}
	dir := contentarchive.DefaultDir(home)
	return func(id string) ([]byte, error) {
		_, body, err := contentarchive.Peek(dir, id)
		return body, err
	}
}
