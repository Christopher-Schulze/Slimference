package tui

import "fmt"

type ProductPanel struct {
	RouteLine           string
	RouteState          string
	InputSavedLine      string
	RequestReducedLine  string
	OutputWireLine      string
	ProviderCacheLine   string
	ProviderCreateLine  string
	CacheLine           string
	ToolPruneLine       string
	OutputReduceLine    string
	SafetyLine          string
	SafetyNeedsWarning  bool
	HostBudgetAttention bool
}

func PresentProductStatus(product ProductStatus) ProductPanel {
	return ProductPanel{
		RouteLine:           productRouteDetail(product),
		RouteState:          productRouteState(product),
		InputSavedLine:      formatTokens(int(product.BillableInputTokensSaved)) + " input saved",
		RequestReducedLine:  formatBytesCompact(product.RequestSideBytesReduced) + " request",
		OutputWireLine:      formatBytesCompact(product.OutputWireBytesSaved) + " output-wire saved",
		ProviderCacheLine:   formatTokens(int(product.ProviderCacheReadTokens)) + " provider-cache read",
		ProviderCreateLine:  formatTokens(int(product.ProviderCacheCreateTokens)) + " create",
		CacheLine:           productCacheLine(product),
		ToolPruneLine:       productToolPruneLine(product),
		OutputReduceLine:    productOutputReduceLine(product),
		SafetyLine:          productSafetyLine(product),
		SafetyNeedsWarning:  product.SafetyIssues > 0 || product.HostBudgetExceeded || product.HostBudgetStatus == "unknown",
		HostBudgetAttention: product.HostBudgetExceeded || product.HostBudgetStatus == "unknown",
	}
}

func productRouteState(product ProductStatus) string {
	switch {
	case product.SafetyIssues > 0 || product.HostBudgetExceeded:
		return "attention"
	case product.SavingsStatus == "saving":
		return "saving"
	case product.SavingsStatus == "active_no_savings":
		return "active"
	default:
		return "idle"
	}
}

func productCacheLine(product ProductStatus) string {
	return formatProductCacheLine(
		product.CacheHits,
		product.CacheHits+product.CacheMisses,
		product.ReadDeltaHits,
		product.RepeatedOutputHits,
		product.ChunkDedupHits,
	)
}

func formatProductCacheLine(cacheHits, cacheTotal, readHits, repeatedHits, chunkHits int64) string {
	return fmt.Sprintf("cache %d/%d · read %d · repeated %d · chunk %d",
		cacheHits, cacheTotal, readHits, repeatedHits, chunkHits)
}
