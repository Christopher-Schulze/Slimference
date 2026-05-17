package extract

import "math"

// TFIDF holds inverse-document-frequency weights for a corpus of
// documents. Build once with NewTFIDF(corpus); query per-sentence with
// Score(sentence).
//
// "Document" here is conceptually a sentence: when the caller wants to
// rank sentences of an input message against the broader conversation,
// they pass the conversation's sentences as the corpus. When ranking
// stand-alone, the input's own sentences form the corpus.
type TFIDF struct {
	idf      map[string]float64
	docCount int
}

// NewTFIDF builds IDF weights from a slice of documents. Each
// document is tokenized via tokenizeWords. Standard log-scaled
// IDF = log(N / df), with df clamped to ≥1 to avoid divide-by-zero
// on missing terms.
func NewTFIDF(docs []string) *TFIDF {
	docCount := len(docs)
	if docCount == 0 {
		return &TFIDF{idf: map[string]float64{}}
	}
	df := make(map[string]int)
	for _, doc := range docs {
		seen := make(map[string]bool)
		for _, tok := range tokenizeWords(doc) {
			if seen[tok] {
				continue
			}
			seen[tok] = true
			df[tok]++
		}
	}
	idf := make(map[string]float64, len(df))
	for term, count := range df {
		// df is always ≥1 by construction (a term only appears in
		// the map when at least one document contains it).
		// Smoothed IDF: log(1 + N/df). Non-zero weight for
		// ubiquitous terms dampens score distortion on short corpora.
		idf[term] = math.Log(1.0 + float64(docCount)/float64(count))
	}
	return &TFIDF{idf: idf, docCount: docCount}
}

// Score returns the TF-IDF score of a sentence: sum over each token
// of tf * idf. tf is the count of the token within this sentence,
// idf comes from the constructor's corpus. Tokens not in idf yield
// 0 (no contribution) so out-of-corpus content is gracefully ignored.
func (t *TFIDF) Score(sentence string) float64 {
	if t == nil || len(t.idf) == 0 {
		return 0
	}
	tf := make(map[string]int)
	for _, tok := range tokenizeWords(sentence) {
		tf[tok]++
	}
	var score float64
	for term, count := range tf {
		score += float64(count) * t.idf[term]
	}
	return score
}

// Vocab returns the count of distinct terms in the IDF table. Used by
// tests and diagnostics.
func (t *TFIDF) Vocab() int {
	if t == nil {
		return 0
	}
	return len(t.idf)
}
