// Package outstop curates the deterministic phrase library used by the
// proxy to suppress trailing-commentary output ("Hope this helps!",
// "Let me know if…"). The same phrase set is consumed at the API
// boundary (stop_sequences / stop) and in streaming-side cutters so
// both layers stay in agreement.
//
// Phrases are conservative on purpose: every entry begins with a "\n"
// so it only fires at a fresh paragraph or list break, never inside
// quoted prose. False-positive cuts cost goodwill - the registry
// errs on the side of preservation.
package outstop

// PhraseSet is a versioned, curated list of trailing-commentary
// openers. The order is significant: the first PhraseSet.TopN(4)
// entries are the ones we ship to providers that cap stop strings
// at four (OpenAI Chat Completions, Anthropic Messages).
type PhraseSet struct {
	Version string
	Items   []string
}

// curated is the registry's single source of truth. Reordering or
// reshuffling entries here changes which subset reaches API-level
// stop_sequences when the provider caps the array - tread carefully.
var curated = PhraseSet{
	Version: "v1-2026-05-16",
	Items: []string{
		"\nLet me know",
		"\nHope this",
		"\nHope that",
		"\nIs there anything",
		"\nWould you like",
		"\nFeel free",
		"\nIf you have",
		"\nDo you want",
		"\nDon't hesitate",
		"\nPlease let me know",
	},
}

// Phrases returns a copy of the curated stop-phrase library. Mutating
// the result does not affect future calls.
func Phrases() []string {
	out := make([]string, len(curated.Items))
	copy(out, curated.Items)
	return out
}

// PhrasesTopN returns at most n phrases from the curated list, in
// declared order. Used for providers that hard-cap the stop array
// (OpenAI / Anthropic at 4). n<=0 returns nil.
func PhrasesTopN(n int) []string {
	if n <= 0 {
		return nil
	}
	if n >= len(curated.Items) {
		return Phrases()
	}
	out := make([]string, n)
	copy(out, curated.Items[:n])
	return out
}

// Version returns the registry version string. Telemetry surfaces this
// so operators can correlate observed savings with phrase-list revisions.
func Version() string { return curated.Version }
