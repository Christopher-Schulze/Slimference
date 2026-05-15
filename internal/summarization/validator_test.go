package summarization

import (
	"regexp"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
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
	// Format: bullet points starting with "- " as required by the validator.
	summary := "- internal/proxy/handler.go and internal/config/config.go are the main files.\n" +
		"- processRequest handles the pipeline.\n" +
		"- validateInput checks inputs.\n" +
		"- " + strings.Repeat("Summary content fills out the required length here. ", summaryLen/50+1)

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
	// Must have "- " prefix to pass format check and hit the length check.
	tinyText := "- " + strings.Repeat("x", 37)
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
	origTokens := 100
	tooLongText := strings.Repeat("- word"+strings.Repeat(" ", 1), 50)
	msgs := buildValidatorMessages(t, strings.Repeat("word ", origTokens))

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

	// Summary deliberately omits all file paths. Must have "- " prefix to pass format check.
	summary := "- " + strings.Repeat("generic summary text ", origTokens/10)

	result := v.Validate(msgs, summary, origTokens)
	if result.Valid {
		t.Error("expected failure when file paths are missing, got valid")
	}
	if !strings.Contains(result.FailReason, "file path") {
		t.Errorf("FailReason = %q, want file path preservation message", result.FailReason)
	}
}

func TestValidator_InventedFilePath(t *testing.T) {
	t.Parallel()

	v := NewCompressionValidator()
	originalText := strings.Repeat("The implementation used internal/proxy/handler.go only. ", 80)
	msgs := buildValidatorMessages(t, originalText)
	origTokens := len(originalText) / 4
	summary := "- internal/proxy/handler.go was used, but internal/secret/fake.go was invented." +
		strings.Repeat(" enough content", 40)

	result := v.Validate(msgs, summary, origTokens)
	if result.Valid {
		t.Fatal("expected invented path to fail validation")
	}
	if !strings.Contains(result.FailReason, "invented file path") {
		t.Fatalf("wrong reason: %q", result.FailReason)
	}
}

func TestInventedSummaryPaths(t *testing.T) {
	t.Parallel()
	if got := inventedSummaryPaths("a.go", []string{"a.go"}, nil); got != nil {
		t.Fatalf("empty summary paths should return nil, got %#v", got)
	}
	got := inventedSummaryPaths("a.go", []string{"a.go"}, []string{"a.go", "b.go"})
	if len(got) != 1 || got[0] != "b.go" {
		t.Fatalf("invented paths = %#v", got)
	}
	if got := inventedSummaryPaths("src/lib/util.go", nil, []string{"/lib/util.go"}); len(got) != 0 {
		t.Fatalf("suffix-equivalent path should not count as invented: %#v", got)
	}
	if got := inventedSummaryPaths("", []string{"root/pkg/file.go", ""}, []string{"pkg/file.go"}); len(got) != 0 {
		t.Fatalf("source-path suffix should not count as invented: %#v", got)
	}
	if got := pathSeenInSource("", "", nil); !got {
		t.Fatal("empty normalized path should be treated as seen")
	}
	if got := pathSeenInSource("", "x.go", []string{""}); got {
		t.Fatal("empty source path should not match non-empty summary path")
	}
	if got := normalizeSummaryPath("././src/main.go."); got != "src/main.go" {
		t.Fatalf("normalized path = %q", got)
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
	// Must have "- " prefix to pass format check.
	padding := origTokens / 10 * 4
	summary := "- " + strings.Repeat("word ", padding/5-1)

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
	summary := "- " + frag40 + strings.Repeat(" word", 33)
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
	// Must have "- " prefix to pass format check.
	summary := "- " + strings.Repeat("generic content ", 33)

	result := v.Validate(msgs, summary, origTokens)
	if result.Valid {
		t.Error("expected failure when error strings are missing from summary, got valid")
	}
	if !strings.Contains(result.FailReason, "error string preservation") {
		t.Errorf("FailReason = %q, want 'error string preservation'", result.FailReason)
	}
}
