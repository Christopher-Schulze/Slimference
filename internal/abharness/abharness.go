// Package abharness compares the model-facing context of a Codex session WITH
// compression against WITHOUT, to detect comprehension impact that token counters
// cannot: content the compressed pipeline elided but the model never received in
// full and cannot recover. This is the offline "no drawback" check (T249, item
// 11). It is pure: callers supply each turn's before/after messages (e.g. by
// running the reducer); the harness never runs the model.
package abharness

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/slimference/slimference/internal/types"
)

var archiveURIPattern = regexp.MustCompile(`(?:local-archive://|slim://archive/)([A-Za-z0-9_\-]+)`)
var contextChunkPattern = regexp.MustCompile(`\[context-chunk status=unchanged uri=((?:local-archive://|slim://archive/)[A-Za-z0-9_\-]+) bytes=[0-9]+\]`)

// Turn holds one request's content messages before and after compression. The
// reducer preserves block order and count, so blocks are paired by index.
type Turn struct {
	Before []types.Message
	After  []types.Message
}

// Severity classifies an elision's comprehension risk.
type Severity string

const (
	// SeverityRecoverable: the elided content was sent verbatim earlier this
	// session, so the model already has it; collapsing a later copy loses nothing.
	SeverityRecoverable Severity = "recoverable_prior_full"
	// SeverityReferenced: content was elided but the replacement carries a
	// recovery reference (local-archive://); recoverable in principle.
	SeverityReferenced Severity = "elided_with_reference"
	// SeverityReferenceMissing: content was elided with an archive reference,
	// but the replay resolver could not expand any referenced archive.
	SeverityReferenceMissing Severity = "reference_missing"
	// SeverityReferenceMismatch: content was elided with an archive reference,
	// but replay expansion did not match the elided source bytes.
	SeverityReferenceMismatch Severity = "reference_mismatch"
	// SeverityLost: content was elided with neither a prior full copy nor a
	// recovery reference. This is a real comprehension drawdown.
	SeverityLost Severity = "lost"
	// SeverityChanged: content changed without a prior full copy or a recovery
	// reference. This is a real comprehension drawdown even when the replacement
	// is not shorter than the original.
	SeverityChanged Severity = "changed_without_reference"
	// SeverityExtra: the compressed path injected an extra model-facing block.
	// Extra text can be intentional, but it must be audited because it changes
	// the model-facing context rather than only removing redundant text.
	SeverityExtra Severity = "extra_after_block"
)

// Elision is one block where the compressed path changed the model-facing text.
type Elision struct {
	Turn     int
	Block    int
	Severity Severity
	Bytes    int
	Preview  string
}

// Report summarises a session comparison.
type Report struct {
	Turns       int
	BytesBefore int
	BytesAfter  int
	Elisions    []Elision
}

// Lost returns the number of high-severity (lost) elisions: the count that must
// be zero for a "no comprehension drawback" claim.
func (r Report) Lost() int {
	n := 0
	for _, e := range r.Elisions {
		switch e.Severity {
		case SeverityLost, SeverityChanged, SeverityExtra, SeverityReferenceMissing, SeverityReferenceMismatch:
			n++
		}
	}
	return n
}

// Saved returns the net bytes the compression removed from the model-facing text.
func (r Report) Saved() int { return r.BytesBefore - r.BytesAfter }

// Compare walks the session and classifies every model-facing text change.
// seenFull accumulates the hashes of content the model received verbatim, so a
// later collapse of the same content is recognised as recoverable rather than
// lost.
func Compare(turns []Turn) Report {
	return compare(turns, nil)
}

type ArchiveResolver func(id string) ([]byte, error)

func CompareWithArchiveExpansion(turns []Turn, resolve ArchiveResolver) Report {
	return compare(turns, resolve)
}

func compare(turns []Turn, resolve ArchiveResolver) Report {
	rep := Report{Turns: len(turns)}
	seenFull := map[string]struct{}{}
	for ti := range turns {
		before := blockTexts(turns[ti].Before)
		after := blockTexts(turns[ti].After)
		for _, at := range after {
			rep.BytesAfter += len(at)
		}
		for _, bt := range before {
			rep.BytesBefore += len(bt)
		}
		pairs := lcsEqualPairs(before, after)
		for _, pair := range pairs {
			bt := before[pair.before]
			for _, stable := range stableSeenFullTexts(bt) {
				if stable != "" {
					seenFull[hashText(stable)] = struct{}{}
				}
			}
		}
		rep.Elisions = append(rep.Elisions, compareTurnSegments(ti, before, after, pairs, seenFull, resolve)...)
	}
	return rep
}

type equalPair struct {
	before int
	after  int
}

func lcsEqualPairs(before, after []string) []equalPair {
	dp := make([][]int, len(before)+1)
	for i := range dp {
		dp[i] = make([]int, len(after)+1)
	}
	for i := len(before) - 1; i >= 0; i-- {
		for j := len(after) - 1; j >= 0; j-- {
			if before[i] != "" && before[i] == after[j] {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var pairs []equalPair
	for i, j := 0, 0; i < len(before) && j < len(after); {
		if before[i] != "" && before[i] == after[j] {
			pairs = append(pairs, equalPair{before: i, after: j})
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return pairs
}

func compareTurnSegments(turn int, before, after []string, pairs []equalPair, seenFull map[string]struct{}, resolve ArchiveResolver) []Elision {
	var out []Elision
	beforeAt, afterAt := 0, 0
	flush := func(beforeEnd, afterEnd int) {
		for beforeAt < beforeEnd && strings.TrimSpace(before[beforeAt]) == "" {
			beforeAt++
		}
		for afterAt < afterEnd && strings.TrimSpace(after[afterAt]) == "" {
			afterAt++
		}
		for beforeEnd-beforeAt > 0 && afterEnd-afterAt > beforeEnd-beforeAt {
			at := after[afterAt]
			if strings.TrimSpace(at) != "" {
				out = append(out, Elision{
					Turn:     turn,
					Block:    afterAt,
					Severity: SeverityExtra,
					Bytes:    -len(at),
					Preview:  preview(at),
				})
			}
			afterAt++
		}
		for beforeAt < beforeEnd && afterAt < afterEnd {
			bt := before[beforeAt]
			at := after[afterAt]
			if strings.TrimSpace(bt) != "" || strings.TrimSpace(at) != "" {
				out = append(out, Elision{
					Turn:     turn,
					Block:    beforeAt,
					Severity: classifyReplacement(bt, at, seenFull, resolve),
					Bytes:    len(bt) - len(at),
					Preview:  preview(bt),
				})
			}
			beforeAt++
			afterAt++
		}
		for beforeAt < beforeEnd {
			bt := before[beforeAt]
			if strings.TrimSpace(bt) != "" {
				out = append(out, Elision{
					Turn:     turn,
					Block:    beforeAt,
					Severity: classifyReplacement(bt, "", seenFull, resolve),
					Bytes:    len(bt),
					Preview:  preview(bt),
				})
			}
			beforeAt++
		}
		for afterAt < afterEnd {
			at := after[afterAt]
			if strings.TrimSpace(at) != "" {
				out = append(out, Elision{
					Turn:     turn,
					Block:    afterAt,
					Severity: SeverityExtra,
					Bytes:    -len(at),
					Preview:  preview(at),
				})
			}
			afterAt++
		}
	}
	for _, pair := range pairs {
		flush(pair.before, pair.after)
		beforeAt = pair.before + 1
		afterAt = pair.after + 1
	}
	flush(len(before), len(after))
	return out
}

func blockTexts(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, b := range m.Content {
			out = append(out, b.Text)
		}
	}
	return out
}

func classifyReplacement(before string, after string, seenFull map[string]struct{}, resolve ArchiveResolver) Severity {
	for _, stable := range stableSeenFullTexts(before) {
		if _, ok := seenFull[hashText(stable)]; ok {
			return SeverityRecoverable
		}
	}
	ids := archiveIDs(after)
	if len(ids) > 0 {
		if resolve == nil {
			return SeverityReferenced
		}
		beforeStable := stableSeenFullTexts(before)
		resolvedAny := false
		unresolved := false
		for _, id := range ids {
			body, err := resolve(id)
			if err != nil {
				unresolved = true
				continue
			}
			resolvedAny = true
			bodyText := string(body)
			if archiveBodyMatchesStable(bodyText, beforeStable, resolve, map[string]struct{}{id: struct{}{}}, 0) {
				return SeverityReferenced
			}
		}
		if !resolvedAny {
			return SeverityReferenceMissing
		}
		if !unresolved {
			if expanded, ok := expandReferencedText(after, resolve); ok {
				for _, stable := range beforeStable {
					if expanded == stable {
						return SeverityReferenced
					}
				}
			}
		}
		return SeverityReferenceMismatch
	}
	if len(after) < len(before) {
		return SeverityLost
	}
	return SeverityChanged
}

func archiveBodyMatchesStable(body string, beforeStable []string, resolve ArchiveResolver, seen map[string]struct{}, depth int) bool {
	for _, stable := range beforeStable {
		if body == stable {
			return true
		}
	}
	if resolve == nil || depth >= 4 {
		return false
	}
	for _, id := range archiveIDs(body) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		nested, err := resolve(id)
		if err != nil {
			continue
		}
		if archiveBodyMatchesStable(string(nested), beforeStable, resolve, seen, depth+1) {
			return true
		}
	}
	return false
}

func stableSeenFullTexts(text string) []string {
	if text == "" {
		return nil
	}
	out := []string{text}
	if payload, ok := codexExecPayload(text); ok {
		out = append(out, payload)
	}
	return out
}

func codexExecPayload(text string) (string, bool) {
	if !strings.Contains(text, "Process exited with code ") {
		return "", false
	}
	for _, marker := range []string{"\nOutput:\n", "\r\nOutput:\r\n"} {
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		payload := text[idx+len(marker):]
		return payload, payload != ""
	}
	return "", false
}

func expandReferencedText(text string, resolve ArchiveResolver) (string, bool) {
	if text == "" || resolve == nil {
		return text, false
	}
	changed := false
	expanded := contextChunkPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := contextChunkPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		body, err := resolve(archiveIDFromURI(parts[1]))
		if err != nil {
			return match
		}
		changed = true
		return string(body)
	})
	if changed {
		return expanded, true
	}
	return text, false
}

func archiveIDFromURI(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "local-archive://")
	raw = strings.TrimPrefix(raw, "slim://archive/")
	return raw
}

func archiveIDs(text string) []string {
	matches := archiveURIPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		id := strings.TrimSpace(match[1])
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

func preview(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:80]
	}
	return s
}
