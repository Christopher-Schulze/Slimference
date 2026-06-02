package main

import (
	"sort"

	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/tui"
)

func layer0StatusFromSnapshots(snaps map[string]filter.FilterSnapshot) tui.Layer0Status {
	status := tui.Layer0Status{Filters: make([]tui.Layer0FilterStatus, 0, len(snaps))}
	for name, snap := range snaps {
		row := tui.Layer0FilterStatus{
			Name:       name,
			Attempts:   snap.Attempts,
			Matches:    snap.Matches,
			Misses:     snap.Misses,
			Panics:     snap.Panics,
			BytesSaved: snap.BytesSaved,
			HitRate:    snap.HitRate,
			AvgMs:      snap.AvgMs,
		}
		status.Filters = append(status.Filters, row)
		status.Attempts += row.Attempts
		status.Matches += row.Matches
		status.Misses += row.Misses
		status.Panics += row.Panics
		status.BytesSaved += row.BytesSaved
	}
	if status.Attempts > 0 {
		status.HitRate = float64(status.Matches) / float64(status.Attempts)
	}
	sort.Slice(status.Filters, func(i, j int) bool {
		if status.Filters[i].BytesSaved != status.Filters[j].BytesSaved {
			return status.Filters[i].BytesSaved > status.Filters[j].BytesSaved
		}
		if status.Filters[i].Matches != status.Filters[j].Matches {
			return status.Filters[i].Matches > status.Filters[j].Matches
		}
		return status.Filters[i].Name < status.Filters[j].Name
	})
	return status
}

func (a *proxyAdapter) GetCheckpointStatus() tui.CheckpointStatus {
	status := a.p.AdminStatusSnapshot().Checkpoints
	return tui.CheckpointStatus{
		Count:       status.Count,
		Captures:    status.Captures,
		Restores:    status.Restores,
		Bytes:       status.Bytes,
		LastCapture: status.LastCapture,
		LastRestore: status.LastRestore,
		LastTrigger: status.LastTrigger,
	}
}

func (a *proxyAdapter) GetLayer0Status() tui.Layer0Status {
	return layer0StatusFromSnapshots(a.p.AdminStatusSnapshot().Layer0)
}

func (a *proxyAdapter) GetToolArchiveStatus() tui.ToolArchiveStatus {
	status := a.p.AdminStatusSnapshot().ToolArchive
	return tui.ToolArchiveStatus{
		Count:        status.Count,
		Archived:     status.Archived,
		Expanded:     status.Expanded,
		BytesRaw:     status.BytesRaw,
		BytesStored:  status.BytesStored,
		LastArchived: status.LastArchived,
		LastExpanded: status.LastExpanded,
	}
}

func productStatusFromSetupState(state control.SetupState) tui.ProductStatus {
	product := state.Savings.Product
	if product.Status == "" {
		product = state.Savings.ProductSignalsWithHostBudget(state.HostBudget)
	}
	safetyIssues := product.SafetyIssues
	if state.HostBudget.Exceeded {
		safetyIssues++
	} else if state.WSS.ParseFailures > 0 || state.WSS.DegradedSessions > 0 || state.WSS.CompressionErrors > 0 {
		safetyIssues++
	}
	savingsStatus := product.Status
	if safetyIssues > 0 {
		savingsStatus = "attention"
	}
	return tui.ProductStatus{
		RouteStatus:               productRouteStatus(state),
		FallbackReason:            state.CodexRoute.FallbackReason,
		RecertStatus:              state.CodexRoute.RecertStatus,
		SavingsStatus:             savingsStatus,
		BillableInputTokensSaved:  product.BillableInputTokensSaved,
		ProviderCacheReadTokens:   product.ProviderCacheReadTokens,
		ProviderCacheCreateTokens: product.ProviderCacheCreateTokens,
		OutputWireBytesSaved:      product.OutputWireBytesSaved,
		RequestSideBytesReduced:   product.RequestSideBytesReduced,
		CostUSD:                   product.CostUSD,
		CacheHits:                 product.CacheHits,
		CacheMisses:               product.CacheMisses,
		ReadDeltaHits:             product.ReadDeltaHits,
		RepeatedOutputHits:        product.RepeatedOutputHits,
		ChunkDedupHits:            product.ChunkDedupHits,
		ToolResolutionMisses:      product.ToolResolutionMisses,
		SafetyIssues:              safetyIssues,
		HostBudgetStatus:          state.HostBudget.Status,
		HostBudgetExceeded:        state.HostBudget.Exceeded,
		HostBudgetReasons:         append([]string(nil), state.HostBudget.Reasons...),
		WSSParseFailures:          state.WSS.ParseFailures,
		WSSDegradedSessions:       state.WSS.DegradedSessions,
		WSSCompressionErrors:      state.WSS.CompressionErrors,
		WSSCompressedMutated:      state.WSS.CompressedMessagesMutated,
		WSSCompressedInspected:    state.WSS.CompressedMessagesInspected,
		WSSByteBridgeOnly:         state.WSS.ByteBridgeOnly,
		WSSMutationActive:         state.WSS.MutationActive,
	}
}

func productRouteStatus(state control.SetupState) string {
	switch {
	case state.WSS.MutationActive || state.WSS.CompressedMessagesMutated > 0:
		return "WSS savings active"
	case state.WSS.ByteBridgeOnly || state.WSS.PhasefBridged > 0:
		return "WSS bridge"
	case state.CodexRoute.Transport == "http":
		return "HTTP fallback"
	case state.CodexRoute.Enabled && state.CodexRoute.Transport != "":
		return state.CodexRoute.Transport
	case state.CodexRoute.DaemonReachable:
		return "route ready"
	default:
		return "direct"
	}
}
