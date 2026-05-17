// Package repdet detects when a model's output echoes a contiguous span
// of prompt content (file dumps, tool_result text, fenced code blocks)
// already known to the client. A confirmed echo of >=minMatch bytes is
// replaced with a compact "[unchanged: <name>:L<from>-<to>]" marker so
// downstream consumers understand the model intended that content but
// did not need to re-emit it.
//
// Detection is exact-byte. False positives are not possible: the
// matcher verifies every candidate fingerprint with a byte-by-byte
// extension over the actual content, then enforces a minimum span.
package repdet

import (
	"fmt"
	"sort"
)

const (
	// WindowSize is the rolling-hash window. 100 bytes is large enough
	// that random text never collides into an indexed block, and small
	// enough that we still match every block of MinMatch length.
	WindowSize = 100
	// MinMatch is the minimum confirmed-extension length required to
	// fire a rewrite. Below this we treat the candidate as coincidence.
	MinMatch = 200
)

const (
	rkBase = uint64(131)
	rkMod  = uint64(1000000007) // 30-bit prime; multiplications stay within uint64
)

var rkBasePow [WindowSize + 1]uint64

func init() {
	rkBasePow[0] = 1
	for i := 1; i < len(rkBasePow); i++ {
		rkBasePow[i] = (rkBasePow[i-1] * rkBase) % rkMod
	}
}

// Block describes one indexed prompt span. Name and line range are
// surfaced verbatim in the rewrite marker so the consumer can locate
// the source.
type Block struct {
	Name     string
	LineFrom int
	LineTo   int
	Text     string
}

// Match describes one confirmed echo span in the rewritten text.
type Match struct {
	Start  int // byte offset in original text
	End    int // byte offset exclusive
	Block  int // index into Index.blocks
	Length int
}

type position struct {
	block  int
	offset int
}

// Index holds rolling-hash fingerprints for every WindowSize-byte
// window of every registered block. AddBlock copies the block text.
type Index struct {
	blocks       []Block
	fingerprints map[uint64][]position
}

// NewIndex returns an empty Index. Build the index by calling AddBlock
// for each prompt block worth tracking (typically tool_result text,
// large code fences, pasted file contents).
func NewIndex() *Index {
	return &Index{fingerprints: map[uint64][]position{}}
}

// AddBlock registers one prompt span. Blocks shorter than WindowSize
// are silently dropped (cannot produce a confirmable match).
func (idx *Index) AddBlock(name string, lineFrom, lineTo int, text string) {
	if len(text) < WindowSize {
		return
	}
	id := len(idx.blocks)
	idx.blocks = append(idx.blocks, Block{Name: name, LineFrom: lineFrom, LineTo: lineTo, Text: text})
	h := hashWindow(text[:WindowSize])
	idx.fingerprints[h] = append(idx.fingerprints[h], position{block: id, offset: 0})
	for i := 1; i+WindowSize <= len(text); i++ {
		h = rollAdvance(h, text[i-1], text[i+WindowSize-1])
		idx.fingerprints[h] = append(idx.fingerprints[h], position{block: id, offset: i})
	}
}

// Blocks returns the registered blocks for inspection (read-only).
func (idx *Index) Blocks() []Block { return idx.blocks }

// FindMatches scans text for echoes >=MinMatch of any indexed block.
// Returned matches are non-overlapping and sorted by Start.
func (idx *Index) FindMatches(text string) []Match {
	if len(text) < WindowSize || len(idx.blocks) == 0 {
		return nil
	}
	var matches []Match
	h := hashWindow(text[:WindowSize])
	pos := 0
	for {
		if positions, ok := idx.fingerprints[h]; ok {
			best, found := idx.extendBest(text, pos, positions)
			if found && best.Length >= MinMatch {
				matches = append(matches, best)
				// Skip past this match before continuing the scan.
				nextPos := best.End
				if nextPos+WindowSize > len(text) {
					break
				}
				h = hashWindow(text[nextPos : nextPos+WindowSize])
				pos = nextPos
				continue
			}
		}
		next := pos + 1
		if next+WindowSize > len(text) {
			break
		}
		h = rollAdvance(h, text[pos], text[next+WindowSize-1])
		pos = next
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Start < matches[j].Start })
	return matches
}

// extendBest verifies each candidate at the current text position and
// returns the longest confirmed extension that meets MinMatch.
func (idx *Index) extendBest(text string, textPos int, candidates []position) (Match, bool) {
	var best Match
	found := false
	for _, cand := range candidates {
		blk := idx.blocks[cand.block]
		// Verify the WindowSize-byte window matches exactly.
		if text[textPos:textPos+WindowSize] != blk.Text[cand.offset:cand.offset+WindowSize] {
			continue
		}
		// Extend left.
		ls := 0
		for textPos-ls > 0 && cand.offset-ls > 0 &&
			text[textPos-ls-1] == blk.Text[cand.offset-ls-1] {
			ls++
		}
		// Extend right.
		rs := WindowSize
		for textPos+rs < len(text) && cand.offset+rs < len(blk.Text) &&
			text[textPos+rs] == blk.Text[cand.offset+rs] {
			rs++
		}
		span := ls + rs
		if span > best.Length {
			best = Match{
				Start:  textPos - ls,
				End:    textPos + rs,
				Block:  cand.block,
				Length: span,
			}
			found = true
		}
	}
	return best, found
}

// Rewrite scans text and replaces every confirmed echo span with a
// "[unchanged: <name>:L<from>-<to>]" marker. Returns the rewritten
// text and the match list.
func (idx *Index) Rewrite(text string) (string, []Match) {
	matches := idx.FindMatches(text)
	if len(matches) == 0 {
		return text, nil
	}
	var out []byte
	cursor := 0
	for _, m := range matches {
		out = append(out, text[cursor:m.Start]...)
		out = append(out, []byte(idx.markerFor(m))...)
		cursor = m.End
	}
	out = append(out, text[cursor:]...)
	return string(out), matches
}

func (idx *Index) markerFor(m Match) string {
	b := idx.blocks[m.Block]
	if b.LineFrom > 0 && b.LineTo >= b.LineFrom {
		return fmt.Sprintf("[unchanged: %s:L%d-%d]", b.Name, b.LineFrom, b.LineTo)
	}
	return fmt.Sprintf("[unchanged: %s]", b.Name)
}

func hashWindow(s string) uint64 {
	var h uint64
	for i := 0; i < len(s); i++ {
		h = (h*rkBase + uint64(s[i])) % rkMod
	}
	return h
}

func rollAdvance(prev uint64, out, in byte) uint64 {
	sub := (uint64(out) * rkBasePow[WindowSize-1]) % rkMod
	h := (prev + rkMod - sub) % rkMod
	h = (h*rkBase + uint64(in)) % rkMod
	return h
}
