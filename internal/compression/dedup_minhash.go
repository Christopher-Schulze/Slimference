package compression

import (
	"strings"
	"unicode"
)

const minHashDim = 128

func tokenizeWords(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var words []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		words = append(words, b.String())
		b.Reset()
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return words
}

func wordShingles(words []string, k int) []string {
	if len(words) < k {
		if len(words) == 0 {
			return nil
		}
		return []string{strings.Join(words, " ")}
	}
	out := make([]string, 0, len(words)-k+1)
	for i := 0; i+k <= len(words); i++ {
		out = append(out, strings.Join(words[i:i+k], " "))
	}
	return out
}

func minHashSignatureFromText(text string) [minHashDim]uint64 {
	words := wordSpans(text)
	var sig [minHashDim]uint64
	if len(words) == 0 {
		return sig
	}
	for i := 0; i < minHashDim; i++ {
		var minv uint64 = 1<<64 - 1
		seed := uint64(i + 1)
		if len(words) < 3 {
			minv = hashWordSpanShingle(text, words, 0, len(words), seed)
		} else {
			for start := 0; start+3 <= len(words); start++ {
				h := hashWordSpanShingle(text, words, start, start+3, seed)
				if h < minv {
					minv = h
				}
			}
		}
		sig[i] = minv
	}
	return sig
}

func hashWithSeed(s string, seed uint64) uint64 {
	h := fnv64aOffset
	for i := 0; i < len(s); i++ {
		h = fnv64aByte(h, s[i])
	}
	return fnv64aSeed(h, seed)
}

type wordSpan struct {
	start int
	end   int
}

func wordSpans(s string) []wordSpan {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	words := make([]wordSpan, 0, countWords(s))
	start := -1
	for idx, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				words = append(words, wordSpan{start: start, end: idx})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = idx
		}
	}
	if start >= 0 {
		words = append(words, wordSpan{start: start, end: len(s)})
	}
	return words
}

func countWords(s string) int {
	count := 0
	inWord := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
			continue
		}
		if !inWord {
			count++
			inWord = true
		}
	}
	return count
}

const (
	fnv64aOffset uint64 = 14695981039346656037
	fnv64aPrime  uint64 = 1099511628211
)

func hashWordSpanShingle(text string, words []wordSpan, start, end int, seed uint64) uint64 {
	h := fnv64aOffset
	for i := start; i < end; i++ {
		if i > start {
			h = fnv64aByte(h, ' ')
		}
		word := words[i]
		for j := word.start; j < word.end; j++ {
			h = fnv64aByte(h, text[j])
		}
	}
	return fnv64aSeed(h, seed)
}

func fnv64aSeed(h uint64, seed uint64) uint64 {
	for i := 0; i < 8; i++ {
		h = fnv64aByte(h, byte(seed>>(8*i)))
	}
	return h
}

func fnv64aByte(h uint64, b byte) uint64 {
	h ^= uint64(b)
	h *= fnv64aPrime
	return h
}

func minHashJaccardEstimate(a, b [minHashDim]uint64) float64 {
	matches := 0
	for i := 0; i < minHashDim; i++ {
		if a[i] == b[i] {
			matches++
		}
	}
	return float64(matches) / float64(minHashDim)
}
