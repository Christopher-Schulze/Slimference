package debug

import (
	"os"
	"strings"
	"testing"
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
