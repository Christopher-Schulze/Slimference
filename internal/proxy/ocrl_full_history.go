package proxy

import (
	"fmt"
	"strings"

	"github.com/slimference/slimference/internal/contentarchive"
	"github.com/slimference/slimference/internal/contextledger"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/tokens"
	"github.com/slimference/slimference/internal/types"
)

const (
	ocrlFullHistorySubLayer          = "ocrl-full-history"
	ocrlFullHistoryMinCandidateToken = 128
)

type ocrlFullHistoryResult struct {
	Messages   []types.Message
	Summary    dbg.ContextLedgerSummary
	Applied    bool
	HasSummary bool
	Saved      int
}

func (p *Proxy) applyHTTPFullHistoryOCRL(provider types.Provider, sessionID string, messages []types.Message, effectiveWindow int, reReadCount int) ocrlFullHistoryResult {
	result := ocrlFullHistoryResult{Messages: messages}
	if p == nil || len(messages) == 0 || strings.TrimSpace(sessionID) == "" || effectiveWindow <= 0 || reReadCount > 0 {
		return result
	}
	policyCfg := p.ocrlPolicyConfig()
	if !ocrlFullHistoryRuntimeMode(policyCfg.Mode) {
		return result
	}
	oldEnd := len(messages) - effectiveWindow
	if oldEnd <= 0 {
		return result
	}
	home, err := proxyUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return result
	}
	archiveDir := contentarchive.DefaultDir(home)
	counter := tokens.ForProvider(provider)
	capacityHint := oldEnd
	if policyCfg.MaxCapsules > 0 && policyCfg.MaxCapsules < capacityHint {
		capacityHint = policyCfg.MaxCapsules
	}
	capsules := make([]contextledger.Capsule, 0, capacityHint)
candidateLoop:
	for msgIdx := 0; msgIdx < oldEnd; msgIdx++ {
		msg := messages[msgIdx]
		if ocrlFullHistorySkipRole(msg.Role) {
			continue
		}
		for blockIdx, block := range msg.Content {
			if policyCfg.MaxCapsules > 0 && len(capsules) >= policyCfg.MaxCapsules {
				break candidateLoop
			}
			if strings.TrimSpace(block.Text) == "" || counter.CountString(block.Text) < ocrlFullHistoryMinCandidateToken {
				continue
			}
			entry, err := contentarchive.Put(archiveDir, contentarchive.Input{
				SessionID:    sessionID,
				MessageIndex: msg.Index,
				BlockIndex:   blockIdx,
				SubLayer:     ocrlFullHistorySubLayer,
				Original:     block.Text,
			}, contentarchive.Limits{})
			if err != nil || entry == nil {
				continue
			}
			capsule, err := contextledger.BuildCommandCapsule(contextledger.CommandObservation{
				SessionID:   sessionID,
				TurnID:      ocrlFullHistoryTurnID(msg, msgIdx),
				CommandLine: ocrlFullHistoryCommandFact(msg, msgIdx, blockIdx),
				ExitCode:    0,
				Stdout:      []byte(block.Text),
				ArchiveIDs:  []string{entry.URI},
				Mechanisms:  []string{"ocrl_full_history"},
			})
			if err == nil {
				capsules = append(capsules, capsule)
			}
		}
	}
	if len(capsules) == 0 {
		return result
	}
	recentTurns := make([]string, 0, effectiveWindow)
	for msgIdx := max(0, oldEnd); msgIdx < len(messages); msgIdx++ {
		recentTurns = append(recentTurns, ocrlFullHistoryTurnID(messages[msgIdx], msgIdx))
	}
	apply, _ := contextledger.ApplyOCRLToMessagesByArchiveMatch(messages, capsules, contextledger.OCRLPolicy{
		Mode:  contextledger.OCRLMode(policyCfg.Mode),
		Route: contextledger.OCRLRouteFullHistoryHTTP,
		Selection: contextledger.SelectionPolicy{
			SessionID:       sessionID,
			ActiveTurnID:    ocrlFullHistoryTurnID(messages[len(messages)-1], len(messages)-1),
			RecentTurnIDs:   recentTurns,
			QualityPressure: reReadCount > 0,
			MaxCapsules:     policyCfg.MaxCapsules,
		},
		ArchiveLoader:        p.ocrlShadowArchiveLoader(),
		CountTokens:          counter.CountString,
		MinNetSavedTokens:    policyCfg.MinNetSavedTokens,
		MaxReplacementTokens: policyCfg.MaxReplacementTokens,
	})
	summary := dbg.ContextLedgerSummary{
		TelemetryOnly:         false,
		CommandCapsules:       len(capsules),
		ReReadCount:           reReadCount,
		OCRLMode:              policyCfg.Mode,
		OCRLRoute:             string(contextledger.OCRLRouteFullHistoryHTTP),
		OCRLReason:            string(apply.OCRL.Reason),
		OCRLShadowOnly:        apply.OCRL.ShadowOnly,
		OCRLCandidateCapsules: apply.OCRL.Selection.Capsules,
		OCRLVerbatimCapsules:  apply.OCRL.Selection.Verbatim,
		OCRLRejectedCapsules:  apply.OCRL.Selection.Rejected,
		OCRLArchiveExpansions: apply.OCRL.ArchiveExpansions,
		OCRLOriginalTokens:    apply.OCRL.OriginalTokens,
		OCRLReplacementTokens: apply.OCRL.ReplacementTokens,
		OCRLRecoveryOverhead:  apply.OCRL.RecoveryOverheadTokens,
	}
	if apply.OCRL.NetSavedTokens > 0 {
		summary.OCRLShadowSavedTokens = apply.OCRL.NetSavedTokens
	}
	result.Summary = summary
	result.HasSummary = true
	if !apply.OCRL.Applied {
		return result
	}
	result.Messages = apply.Messages
	result.Applied = true
	result.Saved = apply.OCRL.NetSavedTokens
	return result
}

func ocrlFullHistoryRuntimeMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(contextledger.OCRLModeShadow), string(contextledger.OCRLModeAuto), string(contextledger.OCRLModeMax):
		return true
	default:
		return false
	}
}

func ocrlFullHistorySkipRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "system":
		return true
	default:
		return false
	}
}

func ocrlFullHistoryTurnID(msg types.Message, fallback int) string {
	if msg.Index >= 0 {
		return fmt.Sprintf("msg-%d", msg.Index)
	}
	return fmt.Sprintf("msg-offset-%d", fallback)
}

func ocrlFullHistoryCommandFact(msg types.Message, msgIdx int, blockIdx int) string {
	return fmt.Sprintf("full_history role=%s message=%d index=%d block=%d", strings.TrimSpace(msg.Role), msgIdx, msg.Index, blockIdx)
}
