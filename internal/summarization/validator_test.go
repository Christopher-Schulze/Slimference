package summarization

import (
	"regexp"
	"strings"
	"testing"

	"github.com/tokenproxy/tokenproxy/internal/types"
)

// buildValidatorMessages constructs messages for validator tests.
func buildValidatorMessages(t *testing.T, texts ...string) []types.Message {
	t.Helper()
	msgs := make([]types.Message, 0, len(texts))
	for i, text := range texts {
		msgs = append(msgs, types.Message{
			Index: i,
			Role:  "user",
			Content: []types.ContentBlock{
				{Type: "text", Text: text},
			},
		})
	}
	return msgs
}

// countTokens estimates the token count for a string using the 4-chars-per-token heuristic.
func countTokens(s string) int {
	return len(s) / 4
}

// TestValidator_PassesAllChecks verifies that a good summary passes all five checks.
// Uses content with no error keywords so the error preservation check is skipped.
func TestValidator_PassesAllChecks(t *testing.T) {
	t.Parallel()

	v := NewCompressionValidator()

	// Original has file paths; no error/panic/fatal/fail/exception keywords so error check is skipped.
	originalText := strings.Repeat("The proxy processes requests through the pipeline handling many messages. ", 40) +
		"\nKey files: internal/proxy/handler.go and internal/config/config.go are central to the system.\n" +
		"```go\nfunc processRequest() {\n}\nfunc validateInput() {\n}\n```"

	msgs := buildValidatorMessages(t, originalText)
	origTokens := countTokens(originalText)

	// Build a summary at ~20% of original length that preserves all key artifacts.
	summaryLen := (origTokens * 20 / 100) * 4 // 20% token estimate in chars
	if summaryLen < 20 {
		summaryLen = 20
	}
	// The summary includes all file paths and function names from the original.
	summary := "internal/proxy/handler.go and internal/config/config.go are the main files. " +
		"processRequest handles the pipeline. validateInput checks inputs. " +
		strings.Repeat("Summary content fills out the required length here. ", summaryLen/50+1)

	result := v.Validate(msgs, summary, origTokens)
	if !result.Valid {
		t.Errorf("expected valid summary, got fail reason: %s", result.FailReason)
	}
}

// TestValidator_TooShort verifies that a summary below 5% of original tokens fails.
func TestValidator_TooShort(t *testing.T) {
	t.Parallel()

	v := NewCompressionValidator()
	// 1000 tokens original -> minimum is 50 tokens (50*4=200 chars)
	origTokens := 1000
	// Create a summary that is only 10 tokens (~40 chars) - below the 5% threshold.
	tinyText := strings.Repeat("x", 40)
	msgs := buildValidatorMessages(t, strings.Repeat("word ", origTokens*4/5))

	result := v.Validate(msgs, tinyText, origTokens)
	if result.Valid {
		t.Error("expected failure for too-short summary, got valid")
	}
	if !strings.Contains(result.FailReason, "too short") {
		t.Errorf("FailReason = %q, want substring 'too short'", result.FailReason)
	}
}

// TestValidator_TooLong verifies that a summary above 40% of original tokens fails.
func TestValidator_TooLong(t *testing.T) {
	t.Parallel()

	v := NewCompressionValidator()
	origTokens := 1000
	// 41% of 1000 tokens = 410 tokens = 1640 chars - above the 40% limit.
	tooLongText := strings.Repeat("y", 1640)
	msgs := buildValidatorMessages(t, strings.Repeat("word ", origTokens*4/5))

	result := v.Validate(msgs, tooLongText, origTokens)
	if result.Valid {
		t.Error("expected failure for too-long summary, got valid")
	}
	if !strings.Contains(result.FailReason, "too long") {
		t.Errorf("FailReason = %q, want substring 'too long'", result.FailReason)
	}
}

// TestValidator_MissingFilePaths verifies failure when >10% of file paths are absent.
func TestValidator_MissingFilePaths(t *testing.T) {
	t.Parallel()

	v := NewCompressionValidator()

	// Original has 5 file paths; summary must contain >=90% = at least 5 of them.
	originalText := `Files involved:
internal/proxy/handler.go
internal/config/config.go
internal/compression/layer1.go
internal/caching/response_cache.go
internal/security/secrets.go
These are the main files.`

	msgs := buildValidatorMessages(t, originalText)
	origTokens := len(originalText) / 4

	// Summary deliberately omits all file paths.
	summary := strings.Repeat("generic summary text ", origTokens/10)

	result := v.Validate(msgs, summary, origTokens)
	if result.Valid {
		t.Error("expected failure when file paths are missing, got valid")
	}
	if !strings.Contains(result.FailReason, "file path") {
		t.Errorf("FailReason = %q, want file path preservation message", result.FailReason)
	}
}

// TestValidator_MissingFunctionNames verifies failure when >20% of function names are absent.
func TestValidator_MissingFunctionNames(t *testing.T) {
	t.Parallel()

	v := NewCompressionValidator()

	// Original has multiple function definitions inside code fences.
	originalText := "```go\nfunc handlerOne() error { return nil }\nfunc handlerTwo() error { return nil }\nfunc handlerThree() error { return nil }\nfunc handlerFour() error { return nil }\nfunc handlerFive() error { return nil }\n```"

	msgs := buildValidatorMessages(t, originalText)
	origTokens := len(originalText) / 4

	// Summary mentions no function names at all but is of the right length.
	padding := origTokens / 10 * 4
	summary := strings.Repeat("word ", padding/5)

	result := v.Validate(msgs, summary, origTokens)
	// Should fail on function name preservation or length.
	if result.Valid {
		t.Error("expected failure when function names are missing from summary, got valid")
	}
}

// TestValidator_ErrorPreservation_longFragment exercises trimToLen(es, 40) in the error check.
func TestValidator_ErrorPreservation_longFragment(t *testing.T) {
	t.Parallel()
	v := NewCompressionValidator()
	re := regexp.MustCompile(`(?i)(error|panic|fatal|exception|fail)[^\n]{0,120}`)
	original := "error: " + strings.Repeat("X", 100)
	matches := re.FindAllString(original, -1)
	if len(matches) == 0 {
		t.Fatal("regex should match")
	}
	frag40 := string([]rune(matches[0])[:40])
	msgs := buildValidatorMessages(t, original)
	origTokens := 500
	summary := frag40 + strings.Repeat(" word", 35)
	result := v.Validate(msgs, summary, origTokens)
	if !result.Valid {
		t.Fatalf("expected valid (error fragment preserved): %s", result.FailReason)
	}
}

// TestExtractIdentifier_singleWord covers the len(parts) < 2 branch (extractIdentifier returns "").
func TestExtractIdentifier_singleWord(t *testing.T) {
	t.Parallel()
	got := extractIdentifier("func")
	if got != "" {
		t.Errorf("extractIdentifier(%q) = %q, want empty string", "func", got)
	}
	got2 := extractIdentifier("")
	if got2 != "" {
		t.Errorf("extractIdentifier(%q) = %q, want empty string", "", got2)
	}
}

// TestExtractIdentifier_twoWords covers the normal path where identifier is returned.
func TestExtractIdentifier_twoWords(t *testing.T) {
	t.Parallel()
	got := extractIdentifier("func myFunction")
	if got != "myFunction" {
		t.Errorf("extractIdentifier(%q) = %q, want %q", "func myFunction", got, "myFunction")
	}
}

// TestTrimToLen_longer covers len(runes) > n branch in trimToLen.
func TestTrimToLen_longer(t *testing.T) {
	t.Parallel()
	got := trimToLen("hello world", 5)
	if got != "hello" {
		t.Errorf("trimToLen(%q, 5) = %q, want %q", "hello world", got, "hello")
	}
}

// TestTrimToLen_shorter covers len(runes) <= n branch (returns s unchanged).
func TestTrimToLen_shorter(t *testing.T) {
	t.Parallel()
	got := trimToLen("hi", 10)
	if got != "hi" {
		t.Errorf("trimToLen(%q, 10) = %q, want %q", "hi", got, "hi")
	}
}

// TestItoa_negative covers the negative number branch in itoa.
func TestItoa_negative(t *testing.T) {
	t.Parallel()
	got := itoa(-42)
	if got != "-42" {
		t.Errorf("itoa(-42) = %q, want %q", got, "-42")
	}
}

// TestItoa_zero covers the n==0 early return in itoa.
func TestItoa_zero(t *testing.T) {
	t.Parallel()
	got := itoa(0)
	if got != "0" {
		t.Errorf("itoa(0) = %q, want %q", got, "0")
	}
}

// TestItoa_positive covers a normal positive number in itoa.
func TestItoa_positive(t *testing.T) {
	t.Parallel()
	got := itoa(123)
	if got != "123" {
		t.Errorf("itoa(123) = %q, want %q", got, "123")
	}
}

// TestValidator_ErrorPreservationBelow50 covers the error preservation < 50% failure branch.
func TestValidator_ErrorPreservationBelow50(t *testing.T) {
	t.Parallel()
	v := NewCompressionValidator()

	// Build a message with multiple distinct error strings.
	original := strings.Join([]string{
		"error: connection refused at line 1",
		"error: timeout waiting for response at line 2",
		"fatal: disk full, cannot write at line 3",
		"panic: nil pointer dereference at line 4",
	}, "\n")
	msgs := buildValidatorMessages(t, original)
	origTokens := 500 // large enough to avoid min/max length failures

	// Summary preserves none of the error strings - well below 50%.
	summary := strings.Repeat("generic content ", 35)

	result := v.Validate(msgs, summary, origTokens)
	if result.Valid {
		t.Error("expected failure when error strings are missing from summary, got valid")
	}
	if !strings.Contains(result.FailReason, "error string preservation") {
		t.Errorf("FailReason = %q, want 'error string preservation'", result.FailReason)
	}
}
