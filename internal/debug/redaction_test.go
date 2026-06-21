package debug

import (
	"os"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/evidence"
)

func TestRedactRequestSummary(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	summary := RequestSummary{
		RequestID:    "req",
		SessionID:    home + "/session",
		Path:         "/v1/responses?api_key=secret",
		BypassReason: "Authorization: Bearer tok_123456",
		Model:        "gpt",
		Errors:       []string{"cookie=abc123", "sk-testsecret123"},
		Entries: []DecisionEntry{{
			RequestID: "req",
			Reason:    home + "/file token=secret",
			Settings:  map[string]string{"password": "password=hunter2"},
		}},
	}
	summary.EnsureFlight()
	redacted := RedactRequestSummary(summary)
	if redacted.Flight != nil {
		t.Fatal("flight must be regenerated after redaction")
	}
	joined := strings.Join([]string{
		redacted.SessionID,
		redacted.Path,
		redacted.BypassReason,
		strings.Join(redacted.Errors, " "),
		redacted.Entries[0].Reason,
		redacted.Entries[0].Settings["password"],
	}, " ")
	for _, forbidden := range []string{home, "tok_123456", "secret", "hunter2", "sk-testsecret123"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("redaction leaked %q in %q", forbidden, joined)
		}
	}
	if redacted.SecretsRedacted == 0 {
		t.Fatal("expected redaction count")
	}
}

func TestRecorderStoresRedactedFlight(t *testing.T) {
	t.Parallel()
	rec := NewRecorder(1, "")
	rec.Record(RequestSummary{
		RequestID:    "req",
		BypassReason: "token=secret",
		Tokens:       TokenCounts{Original: 10, Final: 5, Saved: 5},
	})
	last := rec.Last(1, false)
	if len(last) != 1 || last[0].Flight == nil {
		t.Fatalf("recorded summary missing flight: %+v", last)
	}
	if strings.Contains(last[0].BypassReason, "secret") || strings.Contains(last[0].Flight.BypassReason, "secret") {
		t.Fatalf("stored unredacted summary: %+v", last[0])
	}
}

func TestRedactRequestSummary_EvidenceAndSignals(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	summary := RequestSummary{
		RequestID: "req",
		DebugFacts: map[string]string{
			"key_" + home: "val_token=secret",
		},
		Entries: []DecisionEntry{{
			RequestID: "req",
			Signals:   []string{"signal_" + home, "token=secret"},
			Settings:  map[string]string{"k": "v"},
		}},
		EvidenceDecisions: []evidence.BlockDecision{{
			Mechanism:            "mech_" + home,
			Reason:               "reason_token=secret",
			Recovery:             "recovery_" + home,
			FootprintScoreBucket: "bucket_" + home,
			PreservedEvidence:    []string{"ev_" + home, "token=secret"},
		}},
	}
	redacted := RedactRequestSummary(summary)

	// Check that Signals were redacted.
	for _, sig := range redacted.Entries[0].Signals {
		if strings.Contains(sig, home) || strings.Contains(sig, "secret") {
			t.Fatalf("signal redaction leaked: %q", sig)
		}
	}

	// Check that EvidenceDecisions were redacted.
	for _, ev := range redacted.EvidenceDecisions {
		for _, field := range []string{ev.Mechanism, ev.Reason, ev.Recovery, ev.FootprintScoreBucket} {
			if strings.Contains(field, home) || strings.Contains(field, "secret") {
				t.Fatalf("evidence decision redaction leaked: %q", field)
			}
		}
		for _, pe := range ev.PreservedEvidence {
			if strings.Contains(pe, home) || strings.Contains(pe, "secret") {
				t.Fatalf("preserved evidence redaction leaked: %q", pe)
			}
		}
	}

	// Check that DebugFacts were redacted.
	for k, v := range redacted.DebugFacts {
		if strings.Contains(k, home) || strings.Contains(v, "secret") {
			t.Fatalf("debug facts redaction leaked: %q=%q", k, v)
		}
	}

	if redacted.SecretsRedacted == 0 {
		t.Fatal("expected redaction count")
	}
}

func TestRedactRequestSummary_NoRedactionsKeepsFlight(t *testing.T) {
	t.Parallel()
	summary := RequestSummary{
		RequestID: "req",
		Model:     "gpt",
	}
	summary.EnsureFlight()
	flight := summary.Flight
	redacted := RedactRequestSummary(summary)
	// When no redactions happen, Flight should be preserved.
	if redacted.Flight == nil || redacted.Flight != flight {
		t.Fatalf("flight should be preserved when no redactions occur: %+v", redacted.Flight)
	}
	if redacted.SecretsRedacted != 0 {
		t.Fatalf("expected zero redactions, got %d", redacted.SecretsRedacted)
	}
}
