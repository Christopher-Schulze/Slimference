package summarization

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

// buildCompressionConfigFixture returns a CompressionConfig wired only
// with the redaction-relevant fields plus an inert MiniMax block, so
// buildRedactor never short-circuits during tests.
func buildCompressionConfigFixture(mode string, drop bool) *config.CompressionConfig {
	return &config.CompressionConfig{
		Summary: config.SummaryConfig{
			OutboundRedaction:      mode,
			OutboundDropToolInputs: drop,
		},
	}
}

func textBlock(s string) types.ContentBlock {
	return types.ContentBlock{Type: "text", Text: s}
}

func toolResult(s string) types.ContentBlock {
	return types.ContentBlock{Type: "tool_result", Text: s}
}

func toolUse(name, input string) types.ContentBlock {
	return types.ContentBlock{Type: "tool_use", ToolName: name, ToolInput: input}
}

func redactTestMsg(role string, blocks ...types.ContentBlock) types.Message {
	return types.Message{Role: role, Content: blocks}
}

// TestRedactor_OffMode_DeepCopyOnly asserts that mode=off returns a
// slice that contains the original byte content unchanged.
func TestRedactor_OffMode_DeepCopyOnly(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeOff})
	src := []types.Message{redactTestMsg("user", textBlock("ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))}
	out, stats := r.Redact(src)
	if out[0].Content[0].Text != src[0].Content[0].Text {
		t.Fatalf("off mode mutated content: got %q, want %q", out[0].Content[0].Text, src[0].Content[0].Text)
	}
	if stats.SecretsRedacted != 0 || stats.PathsNormalised != 0 || stats.HeadersStripped != 0 {
		t.Fatalf("off mode produced non-zero stats: %+v", stats)
	}
}

// TestRedactor_DefaultMode_RedactsKnownSecretPatterns covers each
// default security pattern. Failure would mean the inbound and outbound
// detector inventories have drifted.
func TestRedactor_DefaultMode_RedactsKnownSecretPatterns(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"github_token", "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"openai_key", "sk-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"aws_access", "AKIAIOSFODNN7EXAMPLE"},
		{"anthropic_key", "sk-ant-" + strings.Repeat("abcdef", 20)},
		{"rsa_private", "-----BEGIN RSA PRIVATE KEY-----"},
		{"ssh_private", "-----BEGIN OPENSSH PRIVATE KEY-----"},
		{"bearer_token", "Authorization: bearer abcdefghijklmnopqrstuvwxyz1234567890"},
	}
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := []types.Message{redactTestMsg("user", textBlock(c.text))}
			out, stats := r.Redact(src)
			got := out[0].Content[0].Text
			if strings.Contains(got, c.text) {
				t.Fatalf("secret leaked through: %q still in %q", c.text, got)
			}
			if stats.SecretsRedacted == 0 && stats.HeadersStripped == 0 {
				t.Fatalf("expected at least one redaction, got %+v (output %q)", stats, got)
			}
		})
	}
}

// TestRedactor_PathNormalisation covers macOS / Linux / Windows /
// macOS-tmp shapes plus a non-path control to make sure unrelated text
// stays untouched.
func TestRedactor_PathNormalisation(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantHas    string
		wantMisses string
	}{
		{"macos_users", "Looking at /Users/foo/proj/handler.go for", "<HOME>", "/Users/foo"},
		{"linux_home", "Compiling /home/bar/src/main.rs now", "<HOME>", "/home/bar"},
		{"windows_users", `Path: C:\Users\baz\repo\file.cs`, "<HOME>", `C:\Users\baz`},
		{"macos_tmp", "wrote /var/folders/aa/abcd1234/T/tmpfile.txt out", "<TMP>/", "/var/folders/aa"},
		{"non_path", "no paths here, just text", "no paths here", ""},
	}
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := []types.Message{redactTestMsg("user", textBlock(c.input))}
			out, _ := r.Redact(src)
			got := out[0].Content[0].Text
			if c.wantHas != "" && !strings.Contains(got, c.wantHas) {
				t.Fatalf("expected %q in %q", c.wantHas, got)
			}
			if c.wantMisses != "" && strings.Contains(got, c.wantMisses) {
				t.Fatalf("expected %q to be normalised in %q", c.wantMisses, got)
			}
		})
	}
}

// TestRedactor_HeaderStripping covers all canonical auth-bearing header
// names with multiple values.
func TestRedactor_HeaderStripping(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	src := []types.Message{redactTestMsg("user", toolResult(strings.Join([]string{
		"GET /v1/users HTTP/1.1",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz1234567890",
		"Cookie: session=abcdefg-secret-value",
		"Set-Cookie: token=xyz; Path=/",
		"X-Api-Key: my-secret-api-key-value",
		"X-Auth-Token: another-secret-token-value-here",
		"Proxy-Authorization: Basic dXNlcjpwYXNz",
		"Content-Type: application/json",
	}, "\n")))}
	out, stats := r.Redact(src)
	got := out[0].Content[0].Text

	bannedSubstrings := []string{
		"abcdefghijklmnopqrstuvwxyz1234567890",
		"abcdefg-secret-value",
		"my-secret-api-key-value",
		"another-secret-token-value-here",
		"dXNlcjpwYXNz",
	}
	for _, banned := range bannedSubstrings {
		if strings.Contains(got, banned) {
			t.Fatalf("header value %q leaked through: %q", banned, got)
		}
	}
	if !strings.Contains(got, "Content-Type: application/json") {
		t.Fatalf("benign header was stripped: %q", got)
	}
	if stats.HeadersStripped < 6 {
		t.Fatalf("expected >=6 headers stripped, got %d", stats.HeadersStripped)
	}
}

// TestRedactor_JSONCredentialKeys exercises the JSON-key matcher
// against the documented credential vocabulary.
func TestRedactor_JSONCredentialKeys(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		key    string
		secret string
	}{
		{"api_key", `{"api_key": "secretvalue1234"}`, "api_key", "secretvalue1234"},
		{"access_token", `{"access_token": "tok_abcXYZ"}`, "access_token", "tok_abcXYZ"},
		{"client_secret", `{"client_secret":"shh"}`, "client_secret", "shh"},
		{"password", `{"password": "pw1234"}`, "password", "pw1234"},
		{"token_uppercase", `{"Token":"AAA"}`, "Token", "AAA"},
	}
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := []types.Message{redactTestMsg("user", toolResult(c.input))}
			out, stats := r.Redact(src)
			got := out[0].Content[0].Text
			if strings.Contains(got, c.secret) {
				t.Fatalf("credential value %q leaked: %q", c.secret, got)
			}
			if stats.JSONKeyRedacted == 0 {
				t.Fatalf("expected JSONKeyRedacted >= 1, got %+v (output %q)", stats, got)
			}
		})
	}
}

// TestRedactor_StrictMode_DropsToolInputs verifies the strict tier
// removes tool_input bodies entirely.
func TestRedactor_StrictMode_DropsToolInputs(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeStrict})
	src := []types.Message{redactTestMsg("assistant", toolUse("write_file", `{"path":"/Users/x/secret.txt","content":"AKIAIOSFODNN7EXAMPLE"}`))}
	out, stats := r.Redact(src)
	if out[0].Content[0].ToolInput != "" {
		t.Fatalf("strict mode kept tool_input: %q", out[0].Content[0].ToolInput)
	}
	if stats.ToolInputsDropped != 1 {
		t.Fatalf("expected 1 tool_input drop, got %d", stats.ToolInputsDropped)
	}
}

// TestRedactor_DefaultMode_KeepsToolInputs_ButRedactsThem ensures the
// non-strict path preserves the tool_input field but still applies
// secret/path redaction inside it.
func TestRedactor_DefaultMode_KeepsToolInputs_ButRedactsThem(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	src := []types.Message{redactTestMsg("assistant", toolUse("write_file", `{"path":"/Users/x/secret.txt","content":"plain"}`))}
	out, stats := r.Redact(src)
	if out[0].Content[0].ToolInput == "" {
		t.Fatalf("default mode dropped tool_input")
	}
	if strings.Contains(out[0].Content[0].ToolInput, "/Users/x") {
		t.Fatalf("absolute path leaked through tool_input: %q", out[0].Content[0].ToolInput)
	}
	if stats.PathsNormalised == 0 {
		t.Fatalf("expected at least one path normalisation, got %+v", stats)
	}
}

// TestRedactor_EmptyInput is a trivial guard: empty slice returns empty
// stats and slice without panic.
func TestRedactor_EmptyInput(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	out, stats := r.Redact(nil)
	if len(out) != 0 {
		t.Fatalf("expected empty output, got len=%d", len(out))
	}
	if stats != (RedactStats{}) {
		t.Fatalf("expected zero stats, got %+v", stats)
	}
}

// TestRedactor_DoesNotMutateInput verifies the deep-copy invariant: a
// caller's slice is never modified.
func TestRedactor_DoesNotMutateInput(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	original := "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA at /Users/foo/file.go"
	src := []types.Message{redactTestMsg("user", textBlock(original))}
	_, _ = r.Redact(src)
	if src[0].Content[0].Text != original {
		t.Fatalf("input was mutated by Redact: %q", src[0].Content[0].Text)
	}
}

// TestRedactor_ChainedRedactions confirms multiple stages each fire on
// one block when the input has secrets + paths + headers + json keys.
func TestRedactor_ChainedRedactions(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	combined := strings.Join([]string{
		"GitHub token: ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"file: /Users/alice/work/repo/main.go",
		"Authorization: Bearer secret-token-1234567890",
		`config: {"api_key":"a-real-api-key-here"}`,
	}, "\n")
	src := []types.Message{redactTestMsg("user", toolResult(combined))}
	out, stats := r.Redact(src)
	got := out[0].Content[0].Text

	bad := []string{
		"ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"/Users/alice",
		"secret-token-1234567890",
		"a-real-api-key-here",
	}
	for _, b := range bad {
		if strings.Contains(got, b) {
			t.Fatalf("leak detected for %q in %q", b, got)
		}
	}
	if stats.SecretsRedacted == 0 {
		t.Fatalf("expected secrets > 0, got %+v", stats)
	}
	if stats.PathsNormalised == 0 {
		t.Fatalf("expected paths > 0, got %+v", stats)
	}
	if stats.HeadersStripped == 0 {
		t.Fatalf("expected headers > 0, got %+v", stats)
	}
	if stats.JSONKeyRedacted == 0 {
		t.Fatalf("expected json keys > 0, got %+v", stats)
	}
}

// TestValidateJSONOutbound covers the structural JSON sweep used by
// strict mode to catch deeply-nested credential keys the line scanner
// would miss.
func TestValidateJSONOutbound(t *testing.T) {
	in := `{"layers":[{"name":"app","auth":{"api_key":"secret","extra":"keep"}}]}`
	out, count, ok := validateJSONOutbound(in)
	if !ok {
		t.Fatalf("expected ok=true on valid JSON")
	}
	if count != 1 {
		t.Fatalf("expected 1 redaction, got %d", count)
	}
	if strings.Contains(out, "secret") {
		t.Fatalf("secret value leaked: %q", out)
	}
	if !strings.Contains(out, "keep") {
		t.Fatalf("benign value lost: %q", out)
	}
}

// TestValidateJSONOutbound_NonJSONReturnsFalse proves the helper is a
// safe no-op for non-JSON inputs.
func TestValidateJSONOutbound_NonJSONReturnsFalse(t *testing.T) {
	in := "this is plain text, not json"
	out, count, ok := validateJSONOutbound(in)
	if ok || count != 0 || out != in {
		t.Fatalf("non-JSON should return passthrough; got out=%q count=%d ok=%v", out, count, ok)
	}
}

// TestNewRedactor_EmptyMode_DefaultsToDefault verifies the
// empty-string fallback behaviour.
func TestNewRedactor_EmptyMode_DefaultsToDefault(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: ""})
	if r.opts.Mode != RedactionModeDefault {
		t.Fatalf("expected default mode, got %q", r.opts.Mode)
	}
}

// TestNewRedactor_StrictMode_SetsAllFlags ensures strict mode wires
// every guard on without the operator restating each flag.
func TestNewRedactor_StrictMode_SetsAllFlags(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeStrict})
	if !r.opts.DropToolInputs || !r.opts.ReplaceAbsPaths || !r.opts.StripAuthHeaders {
		t.Fatalf("strict did not enable all flags: %+v", r.opts)
	}
}

// TestNewRedactor_OffMode_NoRegexCompilation skips the heavy regex
// build path for off mode.
func TestNewRedactor_OffMode_NoRegexCompilation(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeOff})
	if r.headers != nil {
		t.Fatalf("off mode allocated headers: %d", len(r.headers))
	}
	if r.homeRe != nil || r.tmpRe != nil || r.jsonRe != nil {
		t.Fatalf("off mode allocated regexes")
	}
}

// TestNewRedactor_DefaultMode_RegexAllocated shows default mode does
// build the matchers (the inverse of the off-mode test above).
func TestNewRedactor_DefaultMode_RegexAllocated(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	if len(r.headers) != len(authHeaderNames) {
		t.Fatalf("expected %d header regexes, got %d", len(authHeaderNames), len(r.headers))
	}
	if r.homeRe == nil || r.tmpRe == nil || r.jsonRe == nil {
		t.Fatalf("default mode missed regex allocations")
	}
}

// TestValidateJSONOutbound_EmptyAndArrayShapes covers the helper's
// edge cases: empty input, array root, and a deep nested redaction.
func TestValidateJSONOutbound_EmptyAndArrayShapes(t *testing.T) {
	if out, count, ok := validateJSONOutbound(""); ok || count != 0 || out != "" {
		t.Fatalf("empty should be passthrough: %q %d %v", out, count, ok)
	}
	if out, count, ok := validateJSONOutbound("   "); ok || count != 0 || out != "   " {
		t.Fatalf("whitespace should be passthrough: %q %d %v", out, count, ok)
	}
	in := `[{"api_key":"a"},{"client_secret":"b"},{"benign":"keep"}]`
	out, count, ok := validateJSONOutbound(in)
	if !ok || count != 2 {
		t.Fatalf("expected 2 redactions on array root, got count=%d ok=%v out=%q", count, ok, out)
	}
	if strings.Contains(out, `"a"`) || strings.Contains(out, `"b"`) {
		t.Fatalf("credential leaked: %q", out)
	}
	if !strings.Contains(out, "keep") {
		t.Fatalf("benign value lost: %q", out)
	}
}

// TestRedactionCounters_StartZero asserts a fresh Layer2 has zero
// counters and reports the configured mode.
func TestRedactionCounters_StartZero(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	l := &Layer2{redactor: r}
	c := l.RedactionCounters()
	if c.Secrets != 0 || c.Paths != 0 || c.Headers != 0 || c.JSONKeys != 0 || c.Inputs != 0 {
		t.Fatalf("fresh counters not zero: %+v", c)
	}
	if c.Mode != RedactionModeDefault {
		t.Fatalf("expected mode=%q, got %q", RedactionModeDefault, c.Mode)
	}
}

// TestRedactionCounters_AccumulateAcrossCalls runs Redact through the
// Layer2 entry point twice with overlapping fixtures and confirms the
// counters add up rather than overwriting.
func TestRedactionCounters_AccumulateAcrossCalls(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeDefault})
	l := &Layer2{redactor: r}

	src := []types.Message{
		redactTestMsg("user", textBlock("ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA at /Users/alice/x.go")),
	}
	_ = l.applyOutboundRedaction(src)
	first := l.RedactionCounters()
	if first.Secrets == 0 || first.Paths == 0 {
		t.Fatalf("first call did not register: %+v", first)
	}
	_ = l.applyOutboundRedaction(src)
	second := l.RedactionCounters()
	if second.Secrets <= first.Secrets || second.Paths <= first.Paths {
		t.Fatalf("counters did not accumulate: first=%+v second=%+v", first, second)
	}
}

// TestRedactionCounters_NilRedactor reports an empty mode when the
// receiver has no redactor wired.
func TestRedactionCounters_NilRedactor(t *testing.T) {
	l := &Layer2{}
	c := l.RedactionCounters()
	if c.Mode != "" {
		t.Fatalf("expected empty mode, got %q", c.Mode)
	}
}

// TestApplyOutboundRedaction_NilRedactor returns the input unchanged
// when no redactor is configured (defensive path).
func TestApplyOutboundRedaction_NilRedactor(t *testing.T) {
	l := &Layer2{}
	src := []types.Message{redactTestMsg("user", textBlock("anything"))}
	out := l.applyOutboundRedaction(src)
	if out[0].Content[0].Text != "anything" {
		t.Fatalf("nil redactor altered content: %q", out[0].Content[0].Text)
	}
}

// TestBuildRedactor_HonoursStrictModeFromConfig drives the
// configuration-to-mode mapping via the live build helper.
func TestBuildRedactor_HonoursStrictModeFromConfig(t *testing.T) {
	cfg := buildCompressionConfigFixture(RedactionModeStrict, true)
	r := buildRedactor(cfg)
	if r.opts.Mode != RedactionModeStrict || !r.opts.DropToolInputs {
		t.Fatalf("strict mode not honoured by buildRedactor: %+v", r.opts)
	}
}

// TestBuildRedactor_EmptyModeDefaultsToDefault verifies that an empty
// config string maps to the default mode without panicking.
func TestBuildRedactor_EmptyModeDefaultsToDefault(t *testing.T) {
	cfg := buildCompressionConfigFixture("", false)
	r := buildRedactor(cfg)
	if r.opts.Mode != RedactionModeDefault {
		t.Fatalf("expected default, got %q", r.opts.Mode)
	}
}

// TestBuildRedactor_OffModePreservedFromConfig covers the explicit-off
// path so doctor warnings can rely on it round-tripping.
func TestBuildRedactor_OffModePreservedFromConfig(t *testing.T) {
	cfg := buildCompressionConfigFixture(RedactionModeOff, false)
	r := buildRedactor(cfg)
	if r.opts.Mode != RedactionModeOff {
		t.Fatalf("off mode not preserved: %q", r.opts.Mode)
	}
}

// TestRedactor_StrictMode_JSONSweepFiresAtDepth proves the strict-mode
// structural JSON pass picks up credential keys nested deeper than the
// line-based scanner sees.
func TestRedactor_StrictMode_JSONSweepFiresAtDepth(t *testing.T) {
	r := NewRedactor(RedactOptions{Mode: RedactionModeStrict})
	nested := `{"data":{"user":{"profile":{"api_key":"deepsecret"}},"meta":"keep"}}`
	src := []types.Message{redactTestMsg("user", toolResult(nested))}
	out, stats := r.Redact(src)
	got := out[0].Content[0].Text
	if strings.Contains(got, "deepsecret") {
		t.Fatalf("nested credential leaked in strict mode: %q", got)
	}
	if !strings.Contains(got, "keep") {
		t.Fatalf("benign nested value lost: %q", got)
	}
	if stats.JSONKeyRedacted == 0 {
		t.Fatalf("expected JSONKeyRedacted >= 1 in strict mode, got %+v", stats)
	}
}

// TestValidateJSONOutbound_InvalidJSON_PrefixMatches exercises the
// json.Unmarshal error path: input starts with a JSON sentinel byte
// but is malformed.
func TestValidateJSONOutbound_InvalidJSON_PrefixMatches(t *testing.T) {
	bad := `{"unterminated": "value`
	out, count, ok := validateJSONOutbound(bad)
	if ok || count != 0 {
		t.Fatalf("malformed JSON should fail open: count=%d ok=%v out=%q", count, ok, out)
	}
	if out != bad {
		t.Fatalf("malformed JSON should pass through unchanged: got %q want %q", out, bad)
	}
}
