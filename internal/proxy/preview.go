package proxy

import (
	"encoding/json"

	"github.com/Christopher-Schulze/Slimference/internal/compression"
	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/tokens"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// PreviewResult captures what the proxy would do to a request body if it
// flowed through Layer 1 compression. T82 lets operators see the
// rewritten body locally without paying for an upstream call.
//
// Layer 2 (response cache) is irrelevant before a request is sent.
type PreviewResult struct {
	Provider         types.Provider  `json:"provider"`
	ProviderString   string          `json:"provider_string"`
	Compressed       bool            `json:"compressed"`
	OrigTokens       int             `json:"original_tokens"`
	CompressedTokens int             `json:"compressed_tokens"`
	SavedTokens      int             `json:"saved_tokens"`
	SavingsRatio     float64         `json:"savings_ratio"`
	Layer1Breakdown  map[string]int  `json:"layer1_breakdown"`
	OriginalBody     json.RawMessage `json:"original_body,omitempty"`
	RewrittenBody    json.RawMessage `json:"rewritten_body,omitempty"`
}

// PreviewCompress runs the deterministic Layer 1 pipeline on the
// supplied request body for the given provider hint and returns a
// PreviewResult. providerHint==types.Provider(-1) means "auto-detect"
// from the body / path. T82.
func PreviewCompress(cfg *config.Config, path string, body []byte, providerHint types.Provider, includeBodies bool) (PreviewResult, error) {
	provider := providerHint
	if int(providerHint) < 0 {
		provider = detectProviderWithUA(path, body, "")
	}

	res := PreviewResult{
		Provider:        provider,
		ProviderString:  provider.String(),
		Layer1Breakdown: map[string]int{},
	}
	if includeBodies {
		res.OriginalBody = json.RawMessage(append([]byte(nil), body...))
	}

	messages, _, err := extractMessages(provider, body)
	if err != nil {
		return res, err
	}
	res.OrigTokens = tokens.CountMessages(messages)

	l1 := compression.NewDeterministicCompressor(&cfg.Compression)
	result := l1.Compress(messages)
	if result.TokensSaved > 0 {
		res.Compressed = true
		res.Layer1Breakdown = map[string]int{
			"json":              result.JSONSaved,
			"dedup":             result.DedupSaved,
			"comment":           result.CommentSaved,
			"structure":         result.StructureSaved,
			"delta":             result.DeltaSaved,
			"ansi":              result.ANSISaved,
			"success_short":     result.SuccessShortSaved,
			"tool_compressor":   result.ToolCompressorSaved,
			"image":             result.ImageSaved,
			"repeated_collapse": result.RepeatedCollapseSaved,
			"graph_pruning":     result.GraphPruningSaved,
			"preview":           result.PreviewSaved,
			"loop_nudge":        result.LoopNudgeSaved,
		}
	}
	res.CompressedTokens = tokens.CountMessages(result.Messages)
	res.SavedTokens = res.OrigTokens - res.CompressedTokens
	if res.OrigTokens > 0 {
		res.SavingsRatio = float64(res.SavedTokens) / float64(res.OrigTokens)
	}

	// reconstructBody is best-effort for the preview path: an unusual
	// upstream shape that fails to rebuild leaves the rewritten body
	// empty but the token-attribution numbers still surface so the
	// operator sees what compression would have done.
	if rebuilt, rerr := reconstructBody(provider, body, result.Messages); rerr == nil && includeBodies {
		res.RewrittenBody = json.RawMessage(rebuilt)
	}
	return res, nil
}
