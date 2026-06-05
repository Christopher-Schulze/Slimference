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
	policyCfg := p.ocrlPolicyConfig()
	result := contextledger.BuildOCRLReplacement(stats.LedgerCapsules, contextledger.OCRLPolicy{
		Mode:  contextledger.OCRLMode(policyCfg.Mode),
		Route: contextledger.OCRLRouteCodexWSS,
		Selection: contextledger.SelectionPolicy{
			SessionID:    sessionID,
			ActiveTurnID: activeTurnID,
			MaxCapsules:  policyCfg.MaxCapsules,
		},
		ArchiveLoader:            p.ocrlShadowArchiveLoader(),
		CountTokens:              tokens.ForProvider(types.CodexChatGPT).CountString,
		UseArchiveOriginalTokens: true,
		RecoveryOverheadTokens:   0,
		MinNetSavedTokens:        policyCfg.MinNetSavedTokens,
		MaxReplacementTokens:     policyCfg.MaxReplacementTokens,
	})
	summary.OCRLMode = policyCfg.Mode
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

type ocrlShadowPolicyConfig struct {
	Mode                 string
	MaxCapsules          int
	MinNetSavedTokens    int
	MaxReplacementTokens int
}

func (p *Proxy) ocrlPolicyConfig() ocrlShadowPolicyConfig {
	cfg := ocrlShadowPolicyConfig{
		Mode:              string(contextledger.OCRLModeShadow),
		MaxCapsules:       512,
		MinNetSavedTokens: 1,
	}
	if p == nil || p.config == nil {
		return cfg
	}
	ocrl := p.config.Compression.OCRL
	if mode := strings.ToLower(strings.TrimSpace(ocrl.Mode)); mode != "" {
		cfg.Mode = mode
	}
	if ocrl.MaxCapsules >= 0 {
		cfg.MaxCapsules = ocrl.MaxCapsules
	}
	if ocrl.MinNetSavedTokens >= 0 {
		cfg.MinNetSavedTokens = ocrl.MinNetSavedTokens
	}
	if ocrl.MaxReplacementTokens >= 0 {
		cfg.MaxReplacementTokens = ocrl.MaxReplacementTokens
	}
	return cfg
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
