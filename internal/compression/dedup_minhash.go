package compression

import (
	"hash/fnv"
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
	shingles := wordShingles(tokenizeWords(text), 3)
	var sig [minHashDim]uint64
	if len(shingles) == 0 {
		return sig
	}
	for i := 0; i < minHashDim; i++ {
		var minv uint64 = 1<<64 - 1
		seed := uint64(i + 1)
		for _, sh := range shingles {
			h := hashWithSeed(sh, seed)
			if h < minv {
				minv = h
			}
		}
		sig[i] = minv
	}
	return sig
}

func hashWithSeed(s string, seed uint64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	var buf [8]byte
	for i := range buf {
		buf[i] = byte(seed >> (8 * i))
	}
	_, _ = h.Write(buf[:])
	return h.Sum64()
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
