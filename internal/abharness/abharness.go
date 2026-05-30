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
)

// Elision is one block whose compressed text is shorter than its original.
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
		if e.Severity == SeverityLost {
			n++
		}
	}
	return n
}

// Saved returns the net bytes the compression removed from the model-facing text.
func (r Report) Saved() int { return r.BytesBefore - r.BytesAfter }

// Compare walks the session and classifies every elision (a block whose After
// text is a strict reduction of its Before text). seenFull accumulates the
// hashes of content the model received verbatim, so a later collapse of the same
// content is recognised as recoverable rather than lost.
func Compare(turns []Turn) Report {
	rep := Report{Turns: len(turns)}
	seenFull := map[string]struct{}{}
	for ti := range turns {
		before := blockTexts(turns[ti].Before)
		after := blockTexts(turns[ti].After)
		// First record everything sent verbatim this turn, then classify
		// elisions, so within-turn ordering does not affect the verdict.
		for i, bt := range before {
			rep.BytesBefore += len(bt)
			at := indexOr(after, i)
			rep.BytesAfter += len(at)
			if bt != "" && at == bt {
				seenFull[hashText(bt)] = struct{}{}
			}
		}
		for i, bt := range before {
			at := indexOr(after, i)
			if bt == "" || at == bt || len(at) >= len(bt) {
				continue
			}
			sev := SeverityLost
			if _, ok := seenFull[hashText(bt)]; ok {
				sev = SeverityRecoverable
			} else if strings.Contains(at, "local-archive://") {
				sev = SeverityReferenced
			}
			rep.Elisions = append(rep.Elisions, Elision{
				Turn:     ti,
				Block:    i,
				Severity: sev,
				Bytes:    len(bt) - len(at),
				Preview:  preview(bt),
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

func indexOr(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
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
