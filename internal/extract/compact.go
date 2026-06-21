package extract

import (
	"sort"
	"strings"
)

// Config tunes the deterministic compactor. Zero values are safe
// defaults: TargetRatio=0.4 keeps the top 40% of prose sentences;
// MinSentences=2 floors very short outputs; PreserveCodeBlocks and
// PreserveHeaders are true.
type Config struct {
	// TargetRatio is the fraction of prose sentences to keep (0..1].
	// 1.0 means "no compression" (still a valid mode for benchmarking
	// the parser); 0 falls back to default 0.4.
	TargetRatio float64

	// MinSentences sets a floor on prose sentences kept per section.
	// Prevents a 1-sentence section from disappearing entirely. 0
	// defaults to 1 (always keep at least one sentence).
	MinSentences int

	// PreserveCodeBlocks=false would drop fenced/indented code blocks.
	// Defaults to true (zero-value of bool is false; the constructor
	// flips it). Keeping code verbatim is the load-bearing invariant.
	PreserveCodeBlocks bool

	// PreserveHeaders=true keeps markdown headers in output even when
	// the section that follows is empty. Helps the reader/model
	// understand the structure of the compacted text.
	PreserveHeaders bool

	// PreserveLists=true keeps markdown lists verbatim (lists usually
	// carry load-bearing enumerations).
	PreserveLists bool
}

// DefaultConfig returns the production-recommended tuning.
func DefaultConfig() Config {
	return Config{
		TargetRatio:        0.4,
		MinSentences:       1,
		PreserveCodeBlocks: true,
		PreserveHeaders:    true,
		PreserveLists:      true,
	}
}

// Compactor is the public entrypoint. Stateless; the same compactor
// can be reused across goroutines.
type Compactor struct {
	cfg Config
}

// New builds a Compactor with explicit config; zero-value fields are
// replaced with DefaultConfig() values so partial configs are safe.
func New(cfg Config) *Compactor {
	d := DefaultConfig()
	if cfg.TargetRatio <= 0 || cfg.TargetRatio > 1 {
		cfg.TargetRatio = d.TargetRatio
	}
	if cfg.MinSentences < 1 {
		cfg.MinSentences = 1
	}
	// Bool zero-values: explicitly default each preservation flag to
	// true when the caller did not opt out. Callers who want to drop
	// code/headers/lists construct Config explicitly.
	if !cfg.PreserveCodeBlocks && cfg.MinSentences == d.MinSentences && cfg.TargetRatio == d.TargetRatio {
		cfg.PreserveCodeBlocks = d.PreserveCodeBlocks
		cfg.PreserveHeaders = d.PreserveHeaders
		cfg.PreserveLists = d.PreserveLists
	}
	return &Compactor{cfg: cfg}
}

// Compact compresses text using its own sentences as the TF-IDF
// corpus. Useful when the caller has no broader context to score
// against (e.g., compressing one isolated message).
func (c *Compactor) Compact(text string) string {
	return c.CompactWithCorpus(text, nil)
}

// CompactWithCorpus compresses text, scoring sentences against the
// provided corpus (typically other messages in the conversation).
// Passing nil corpus falls back to using the text's own sentences as
// the corpus (self-ranking).
func (c *Compactor) CompactWithCorpus(text string, corpus []string) string {
	sections := parseSections(text)
	corpusSentences := buildCorpusSentences(sections, corpus)
	tfidf := NewTFIDF(corpusSentences)
	var out strings.Builder
	for _, sec := range sections {
		switch sec.Kind {
		case SectionCode:
			if c.cfg.PreserveCodeBlocks {
				out.WriteString(sec.Content)
			}
		case SectionHeader:
			if c.cfg.PreserveHeaders {
				out.WriteString(sec.Content)
			}
		case SectionList:
			if c.cfg.PreserveLists {
				out.WriteString(sec.Content)
			}
		case SectionBlank:
			out.WriteString(sec.Content)
		case SectionProse:
			out.WriteString(c.compactProse(sec.Content, tfidf))
		}
	}
	return out.String()
}

// buildCorpusSentences flattens prose sections plus any external
// corpus strings into a single slice for TFIDF construction. Each
// prose sentence is its own "document" — that's how IDF gets enough
// signal to distinguish boilerplate from content.
func buildCorpusSentences(sections []Section, external []string) []string {
	var corpus []string
	for _, s := range sections {
		if s.Kind != SectionProse {
			continue
		}
		corpus = append(corpus, splitSentences(s.Content)...)
	}
	for _, doc := range external {
		corpus = append(corpus, splitSentences(doc)...)
	}
	return corpus
}

// rankedSentence holds a sentence with its compound score and original
// position index. Position is preserved so the final output reads in
// the same order as the input — the user/model sees the same narrative
// flow, just with some sentences elided.
type rankedSentence struct {
	idx   int
	text  string
	score float64
}

// compactProse picks the top-K sentences from a prose section, ordered
// by their TF-IDF + position + length compound score, then re-emits
// them in original document order.
func (c *Compactor) compactProse(prose string, tfidf *TFIDF) string {
	sentences := splitSentences(prose)
	if len(sentences) <= c.cfg.MinSentences {
		return prose
	}

	ranked := make([]rankedSentence, len(sentences))
	for i, s := range sentences {
		ranked[i] = rankedSentence{
			idx:   i,
			text:  s,
			score: c.scoreSentence(s, i, len(sentences), tfidf),
		}
	}

	keepN := max(int(float64(len(sentences))*c.cfg.TargetRatio), c.cfg.MinSentences)
	if keepN >= len(sentences) {
		return prose
	}

	// Pick top-K by score.
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})
	keep := ranked[:keepN]

	// Re-sort by original position so the output reads sequentially.
	sort.SliceStable(keep, func(i, j int) bool {
		return keep[i].idx < keep[j].idx
	})

	var out strings.Builder
	for _, r := range keep {
		out.WriteString(r.text)
	}
	return out.String()
}

// scoreSentence combines TF-IDF importance with positional and length
// heuristics. Tunable constants are kept local: tweak in one place.
//
// Components:
//
//	tfidfTerm:    raw sum of tf*idf for the sentence
//	positionTerm: opening + closing sentences get a boost (typical
//	              structure: thesis sentence and summary sentence)
//	lengthTerm:   moderate-length sentences score higher (very short
//	              and very long are penalised)
func (c *Compactor) scoreSentence(sentence string, pos, total int, tfidf *TFIDF) float64 {
	const (
		wTFIDF    = 1.0
		wPosition = 0.3
		wLength   = 0.2
	)
	tfidfTerm := tfidf.Score(sentence)
	positionTerm := positionScore(pos, total)
	lengthTerm := lengthScore(sentence)
	return wTFIDF*tfidfTerm + wPosition*positionTerm + wLength*lengthTerm
}

// positionScore rewards opening and closing sentences of a section.
// Bell-shape with anti-modes: high at the ends, low in the middle.
// Bounded to [0, 1].
func positionScore(pos, total int) float64 {
	if total <= 1 {
		return 1
	}
	frac := float64(pos) / float64(total-1)
	if frac < 0.5 {
		return 1 - 2*frac
	}
	return 2 * (frac - 0.5)
}

// lengthScore prefers sentences around 80-160 characters. Very short
// (<20) and very long (>400) penalised. Bounded to [0, 1].
func lengthScore(s string) float64 {
	n := len(s)
	switch {
	case n < 20:
		return float64(n) / 20.0
	case n > 400:
		return 0.2
	case n >= 80 && n <= 160:
		return 1.0
	case n < 80:
		return 0.6 + 0.4*(float64(n-20)/60.0)
	default: // 160 < n <= 400
		return 1.0 - 0.6*(float64(n-160)/240.0)
	}
}
