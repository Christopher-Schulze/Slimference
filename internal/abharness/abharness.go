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
	"strings"

	"github.com/slimference/slimference/internal/types"
)

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
		case SeverityLost, SeverityChanged, SeverityExtra:
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
	rep := Report{Turns: len(turns)}
	seenFull := map[string]struct{}{}
	for ti := range turns {
		before := blockTexts(turns[ti].Before)
		after := blockTexts(turns[ti].After)
		for _, at := range after {
			rep.BytesAfter += len(at)
		}
		// First record everything sent verbatim this turn, then classify changes,
		// so within-turn ordering does not affect the verdict.
		for i, bt := range before {
			rep.BytesBefore += len(bt)
			at, ok := indexMaybe(after, i)
			if bt != "" && at == bt {
				seenFull[hashText(bt)] = struct{}{}
			}
			if !ok && bt != "" {
				rep.Elisions = append(rep.Elisions, Elision{
					Turn:     ti,
					Block:    i,
					Severity: classifyReplacement(bt, "", seenFull),
					Bytes:    len(bt),
					Preview:  preview(bt),
				})
			}
		}
		for i, bt := range before {
			at, ok := indexMaybe(after, i)
			if bt == "" || !ok || at == bt {
				continue
			}
			rep.Elisions = append(rep.Elisions, Elision{
				Turn:     ti,
				Block:    i,
				Severity: classifyReplacement(bt, at, seenFull),
				Bytes:    len(bt) - len(at),
				Preview:  preview(bt),
			})
		}
		for i := len(before); i < len(after); i++ {
			at := strings.TrimSpace(after[i])
			if at == "" {
				continue
			}
			rep.Elisions = append(rep.Elisions, Elision{
				Turn:     ti,
				Block:    i,
				Severity: SeverityExtra,
				Bytes:    -len(after[i]),
				Preview:  preview(after[i]),
			})
		}
	}
	return rep
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

func indexMaybe(s []string, i int) (string, bool) {
	if i < len(s) {
		return s[i], true
	}
	return "", false
}

func classifyReplacement(before string, after string, seenFull map[string]struct{}) Severity {
	if _, ok := seenFull[hashText(before)]; ok {
		return SeverityRecoverable
	}
	if strings.Contains(after, "local-archive://") {
		return SeverityReferenced
	}
	if len(after) < len(before) {
		return SeverityLost
	}
	return SeverityChanged
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
