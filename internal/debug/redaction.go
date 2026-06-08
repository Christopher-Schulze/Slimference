package debug

import (
	"os"
	"regexp"
	"strings"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[^\s,;]+`),
	regexp.MustCompile(`(?i)((api[_-]?key|token|password|cookie)=)[^\s,;]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
}

// RedactRequestSummary returns a disk-safe copy of a request summary. The
// recorder stores only this form so debug JSONL can become a shareable corpus
// without leaking auth material or machine-local paths.
func RedactRequestSummary(s RequestSummary) RequestSummary {
	redactions := 0
	redact := func(v string) string {
		out := redactString(v)
		if out != v {
			redactions++
		}
		return out
	}

	s.RequestID = redact(s.RequestID)
	s.SessionID = redact(s.SessionID)
	s.TurnID = redact(s.TurnID)
	s.Source = redact(s.Source)
	s.Provider = redact(s.Provider)
	s.Host = redact(s.Host)
	s.Path = redact(s.Path)
	s.ClientFamily = redact(s.ClientFamily)
	s.RouteMode = redact(s.RouteMode)
	s.BypassReason = redact(s.BypassReason)
	s.Model = redact(s.Model)
	if s.DebugFacts != nil {
		clean := make(map[string]string, len(s.DebugFacts))
		for k, v := range s.DebugFacts {
			clean[redact(k)] = redact(v)
		}
		s.DebugFacts = clean
	}
	for i := range s.Errors {
		s.Errors[i] = redact(s.Errors[i])
	}
	for i := range s.Entries {
		entry := &s.Entries[i]
		entry.RequestID = redact(entry.RequestID)
		entry.ContentType = redact(entry.ContentType)
		entry.ContentClass = redact(entry.ContentClass)
		entry.SubLayer = redact(entry.SubLayer)
		entry.Action = redact(entry.Action)
		entry.Reason = redact(entry.Reason)
		entry.SafetyClass = redact(entry.SafetyClass)
		entry.Recovery = redact(entry.Recovery)
		for j := range entry.Signals {
			entry.Signals[j] = redact(entry.Signals[j])
		}
		if entry.Settings != nil {
			clean := make(map[string]string, len(entry.Settings))
			for k, v := range entry.Settings {
				clean[redact(k)] = redact(v)
			}
			entry.Settings = clean
		}
	}
	for i := range s.EvidenceDecisions {
		decision := &s.EvidenceDecisions[i]
		decision.Mechanism = redact(decision.Mechanism)
		decision.Reason = redact(decision.Reason)
		decision.Recovery = redact(decision.Recovery)
		for j := range decision.PreservedEvidence {
			decision.PreservedEvidence[j] = redact(decision.PreservedEvidence[j])
		}
	}
	if redactions > 0 {
		s.SecretsRedacted += redactions
		s.Flight = nil
	}
	return s
}

func redactString(v string) string {
	out := v
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = strings.ReplaceAll(out, home, "~")
	}
	if tmp := os.TempDir(); tmp != "" {
		out = strings.ReplaceAll(out, tmp, "$TMPDIR")
	}
	for _, pattern := range redactionPatterns {
		switch pattern.String() {
		case `sk-[A-Za-z0-9_-]{8,}`:
			out = pattern.ReplaceAllString(out, "sk-REDACTED")
		default:
			out = pattern.ReplaceAllString(out, "${1}REDACTED")
		}
	}
	return out
}
