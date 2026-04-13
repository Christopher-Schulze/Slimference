package security

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

// buildMessages constructs a single-message slice with the given text.
func buildMessages(t *testing.T, role, text string) []types.Message {
	t.Helper()
	return []types.Message{
		{
			Index: 0,
			Role:  role,
			Content: []types.ContentBlock{
				{Type: "text", Text: text},
			},
		},
	}
}

// TestDetector_RedactAWSKey verifies that an AWS access key is redacted in "redact" mode.
func TestDetector_RedactAWSKey(t *testing.T) {
	t.Parallel()

	awsKey := "AKIAIOSFODNN7EXAMPLE"
	d := NewDetector("redact", nil, nil)
	msgs := buildMessages(t, "user", "my key is "+awsKey+" please keep it safe")

	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("ScanMessages returned error: %v", err)
	}
	if len(dets) == 0 {
		t.Fatal("expected at least one detection for AWS key, got none")
	}
	resultText := out[0].Content[0].Text
	if strings.Contains(resultText, awsKey) {
		t.Errorf("AWS key not redacted; result text: %q", resultText)
	}
	if !strings.Contains(resultText, "[REDACTED:") {
		t.Errorf("expected REDACTED marker in output; got: %q", resultText)
	}
}

// TestDetector_RedactGitHubToken verifies that a GitHub personal access token is redacted.
func TestDetector_RedactGitHubToken(t *testing.T) {
	t.Parallel()

	// 36 alphanumeric characters after ghp_
	ghToken := "ghp_" + strings.Repeat("a1B2c3", 6) // 36 chars
	d := NewDetector("redact", nil, nil)
	msgs := buildMessages(t, "user", "token="+ghToken)

	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("ScanMessages returned error: %v", err)
	}
	if len(dets) == 0 {
		t.Fatal("expected detection for GitHub token, got none")
	}
	if strings.Contains(out[0].Content[0].Text, ghToken) {
		t.Errorf("GitHub token not redacted; text: %q", out[0].Content[0].Text)
	}
}

// TestDetector_WarnMode verifies that warn mode returns detections without modifying messages.
func TestDetector_WarnMode(t *testing.T) {
	t.Parallel()

	awsKey := "AKIAIOSFODNN7EXAMPLE"
	d := NewDetector("warn", nil, nil)
	msgs := buildMessages(t, "user", awsKey)

	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error in warn mode: %v", err)
	}
	if len(dets) == 0 {
		t.Fatal("expected detections in warn mode, got none")
	}
	// Original text must be preserved.
	if out[0].Content[0].Text != awsKey {
		t.Errorf("warn mode modified text: got %q, want %q", out[0].Content[0].Text, awsKey)
	}
}

// TestDetector_BlockMode verifies that block mode returns an error when a secret is found.
func TestDetector_BlockMode(t *testing.T) {
	t.Parallel()

	awsKey := "AKIAIOSFODNN7EXAMPLE"
	d := NewDetector("block", nil, nil)
	msgs := buildMessages(t, "user", awsKey)

	out, dets, err := d.ScanMessages(msgs)
	if err == nil {
		t.Fatal("block mode expected error for secret, got nil")
	}
	if out != nil {
		t.Error("block mode should return nil messages on detection")
	}
	if len(dets) == 0 {
		t.Error("block mode should still populate detections")
	}
}

// TestDetector_OffMode verifies that off mode returns original messages unchanged.
func TestDetector_OffMode(t *testing.T) {
	t.Parallel()

	awsKey := "AKIAIOSFODNN7EXAMPLE"
	d := NewDetector("off", nil, nil)
	msgs := buildMessages(t, "user", awsKey)

	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("off mode returned error: %v", err)
	}
	if len(dets) != 0 {
		t.Errorf("off mode should not report detections, got %d", len(dets))
	}
	if out[0].Content[0].Text != awsKey {
		t.Errorf("off mode modified text: got %q, want %q", out[0].Content[0].Text, awsKey)
	}
}

// TestDetector_NoFalsePositives verifies that plain messages without secrets pass unchanged.
func TestDetector_NoFalsePositives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
	}{
		{"normal prose", "Hello, how are you today?"},
		{"code snippet without secrets", "func main() { fmt.Println(\"hello\") }"},
		{"URL without credentials", "https://example.com/api/v1/resource"},
		{"short alphanumeric", "ref: abc123"},
	}

	d := NewDetector("redact", nil, nil)

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msgs := buildMessages(t, "user", tc.text)
			out, dets, err := d.ScanMessages(msgs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(dets) > 0 {
				t.Errorf("false positive: %d detections for %q", len(dets), tc.text)
			}
			if out[0].Content[0].Text != tc.text {
				t.Errorf("text modified without detection: got %q, want %q", out[0].Content[0].Text, tc.text)
			}
		})
	}
}

// TestDetector_MultipleSecretsInOneMessage verifies that all secrets in a message are redacted.
func TestDetector_MultipleSecretsInOneMessage(t *testing.T) {
	t.Parallel()

	awsKey := "AKIAIOSFODNN7EXAMPLE"
	ghToken := "ghp_" + strings.Repeat("z9Y8x7", 6) // 36 chars
	text := "aws=" + awsKey + " github=" + ghToken

	d := NewDetector("redact", nil, nil)
	msgs := buildMessages(t, "user", text)

	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dets) < 2 {
		t.Errorf("expected at least 2 detections, got %d", len(dets))
	}
	resultText := out[0].Content[0].Text
	if strings.Contains(resultText, awsKey) {
		t.Errorf("AWS key not redacted from multi-secret message: %q", resultText)
	}
	if strings.Contains(resultText, ghToken) {
		t.Errorf("GitHub token not redacted from multi-secret message: %q", resultText)
	}
}

// TestDetector_UnknownMode_defaultsToWarn verifies the switch default branch in NewDetector.
func TestDetector_UnknownMode_defaultsToWarn(t *testing.T) {
	t.Parallel()
	awsKey := "AKIAIOSFODNN7EXAMPLE"
	d := NewDetector("invalid-mode", nil, nil)
	msgs := buildMessages(t, "user", awsKey)
	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// warn mode: original text preserved, detections returned
	if len(dets) == 0 {
		t.Fatal("expected detections in warn-defaulted mode")
	}
	if out[0].Content[0].Text != awsKey {
		t.Error("warn mode should not modify text")
	}
}

// TestDetector_ToolInputBlock_redacted verifies the ToolInput branch in ScanMessages.
func TestDetector_ToolInputBlock_redacted(t *testing.T) {
	t.Parallel()
	awsKey := "AKIAIOSFODNN7EXAMPLE"
	d := NewDetector("redact", nil, nil)
	msgs := []types.Message{
		{
			Index: 0,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "tool_use", ToolInput: "key=" + awsKey},
			},
		},
	}
	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dets) == 0 {
		t.Fatal("expected detection in ToolInput")
	}
	if strings.Contains(out[0].Content[0].ToolInput, awsKey) {
		t.Errorf("ToolInput not redacted: %q", out[0].Content[0].ToolInput)
	}
	if !strings.Contains(out[0].Content[0].ToolInput, "[REDACTED:") {
		t.Errorf("expected REDACTED marker in ToolInput: %q", out[0].Content[0].ToolInput)
	}
}

// TestDetector_Allowlist_skipsMatch verifies the isAllowlisted true branch in scanText.
func TestDetector_Allowlist_skipsMatch(t *testing.T) {
	t.Parallel()
	awsKey := "AKIAIOSFODNN7EXAMPLE"
	// allowlist contains a substring of the AWS key - isAllowlisted returns true - skipped
	d := NewDetector("redact", nil, []string{"AKIAIOSFODNN7"})
	msgs := buildMessages(t, "user", "key="+awsKey)
	out, dets, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dets) != 0 {
		t.Errorf("expected no detections (allowlisted), got %d", len(dets))
	}
	if out[0].Content[0].Text != "key="+awsKey {
		t.Error("text should be unmodified when allowlisted")
	}
}

// TestDetector_LowEntropy_skipsMatch verifies the MinEntropy branch in scanText.
// The "High-Entropy String" pattern has MinEntropy: 4.5 and regex [a-zA-Z0-9+/=]{40,}.
// A string of 40 identical chars matches the regex but has near-zero entropy, so it is skipped.
func TestDetector_LowEntropy_skipsMatch(t *testing.T) {
	t.Parallel()
	lowEntropyStr := strings.Repeat("a", 40)
	d := NewDetector("redact", nil, nil)
	msgs := buildMessages(t, "user", lowEntropyStr)
	out, _, err := d.ScanMessages(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Low-entropy match should be skipped, text unchanged
	if out[0].Content[0].Text != lowEntropyStr {
		t.Errorf("text should be unmodified for low-entropy match")
	}
}
