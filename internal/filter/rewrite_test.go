package filter

import (
	"strings"
	"testing"
)

func TestExtractCommandFromHookJSON(t *testing.T) {
	t.Parallel()
	s, err := ExtractCommandFromHookJSON([]byte(`{"command":"git status"}`))
	if err != nil || s != "git status" {
		t.Fatalf("got %q %v", s, err)
	}
}

func TestExtractCommandFromHookJSON_Nested(t *testing.T) {
	t.Parallel()
	s, err := ExtractCommandFromHookJSON([]byte(`{"tool_input":{"command":"npm test"}}`))
	if err != nil || s != "npm test" {
		t.Fatalf("got %q %v", s, err)
	}
}

func TestExtractCommandFromHookJSON_Error(t *testing.T) {
	t.Parallel()
	_, err := ExtractCommandFromHookJSON([]byte(`{}`))
	if err == nil {
		t.Fatal("want error")
	}
}

// TestExtractCommandFromHookJSON_InvalidJSON covers the json.Unmarshal error path.
func TestExtractCommandFromHookJSON_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := ExtractCommandFromHookJSON([]byte(`not-json`))
	if err == nil {
		t.Fatal("want error for invalid JSON")
	}
}

// TestExtractCommandFromHookJSON_Array covers the []interface{} branch in findStringForKey.
func TestExtractCommandFromHookJSON_Array(t *testing.T) {
	t.Parallel()
	s, err := ExtractCommandFromHookJSON([]byte(`[{"command":"git log"}]`))
	if err != nil || s != "git log" {
		t.Fatalf("array: got %q %v", s, err)
	}
}

// TestFindStringForKey_direct covers the helper directly for edge cases.
func TestFindStringForKey_direct(t *testing.T) {
	t.Parallel()
	// empty string value should not match
	if s, ok := findStringForKey(map[string]interface{}{"command": ""}, "command"); ok || s != "" {
		t.Fatalf("empty string: got %q %v", s, ok)
	}
	// nested array
	v := []interface{}{map[string]interface{}{"command": "npm test"}}
	if s, ok := findStringForKey(v, "command"); !ok || s != "npm test" {
		t.Fatalf("nested array: got %q %v", s, ok)
	}
	// non-matching type (int) should return false
	if _, ok := findStringForKey(42, "command"); ok {
		t.Fatal("int: should return false")
	}
}

// --- RewriteCommand tests (spec+.md §4.2) ---

func TestRewriteCommand_SimpleGit(t *testing.T) {
	t.Parallel()
	got, ok := RewriteCommand("git status", nil)
	if !ok {
		t.Fatal("expected filter match")
	}
	if got != "slimference filter git status" {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteCommand_CompoundAnd(t *testing.T) {
	t.Parallel()
	got, ok := RewriteCommand("git status && cargo test", nil)
	if !ok {
		t.Fatal("expected filter match")
	}
	if got != "slimference filter git status && slimference filter cargo test" {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteCommand_PipeRightSideNotRewritten(t *testing.T) {
	t.Parallel()
	// "cargo test" is the left side - rewritten; "tail -20" is the right side - not rewritten
	got, ok := RewriteCommand("cargo test 2>&1 | tail -20", nil)
	if !ok {
		t.Fatal("expected filter match")
	}
	// right side "tail" should not be prefixed
	if strings.Contains(got, "slimference filter tail") {
		t.Fatalf("right side of pipe must not be rewritten: %q", got)
	}
	if !strings.HasPrefix(got, "slimference filter cargo test") {
		t.Fatalf("left side must be rewritten: %q", got)
	}
}

func TestRewriteCommand_FindBeforePipe(t *testing.T) {
	t.Parallel()
	// find/fd are always rewritten even when before a pipe
	got, ok := RewriteCommand("find . -name '*.go' | head -20", nil)
	if !ok {
		t.Fatal("expected filter match")
	}
	if !strings.HasPrefix(got, "slimference filter find") {
		t.Fatalf("find before pipe must be rewritten: %q", got)
	}
}

func TestRewriteCommand_AlreadyPrefixed(t *testing.T) {
	t.Parallel()
	cmd := "slimference filter git status"
	got, ok := RewriteCommand(cmd, nil)
	if ok {
		t.Fatal("already-prefixed command must not be rewritten")
	}
	if got != cmd {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteCommand_NoFilterMatch(t *testing.T) {
	t.Parallel()
	// "echo" has no built-in filter
	_, ok := RewriteCommand("echo hello world", nil)
	if ok {
		t.Fatal("echo should not match any filter rule")
	}
}

func TestRewriteCommand_Excluded(t *testing.T) {
	t.Parallel()
	_, ok := RewriteCommand("git status", []string{"git"})
	if ok {
		t.Fatal("excluded command must not be rewritten")
	}
}

func TestRewriteCommand_DisabledEnvVar(t *testing.T) {
	t.Parallel()
	_, ok := RewriteCommand("SLIMFERENCE_DISABLED=1 git status", nil)
	if ok {
		t.Fatal("SLIMFERENCE_DISABLED=1 must suppress rewrite")
	}
}

func TestRewriteCommand_CompoundSpecExample(t *testing.T) {
	t.Parallel()
	// Exact example from spec+.md §4.2
	got, ok := RewriteCommand("cargo fmt --all && cargo test 2>&1 | tail -20", nil)
	if !ok {
		t.Fatal("expected filter match")
	}
	if !strings.HasPrefix(got, "slimference filter cargo fmt --all &&") {
		t.Fatalf("first segment must be rewritten: %q", got)
	}
	if !strings.Contains(got, "slimference filter cargo test") {
		t.Fatalf("second segment must be rewritten: %q", got)
	}
	if strings.Contains(got, "slimference filter tail") {
		t.Fatalf("right side of pipe must not be rewritten: %q", got)
	}
}

func TestRewriteCommand_Semicolon(t *testing.T) {
	t.Parallel()
	got, ok := RewriteCommand("git log; cargo build", nil)
	if !ok {
		t.Fatal("expected filter match")
	}
	if !strings.Contains(got, "slimference filter git log") {
		t.Fatalf("git log must be rewritten: %q", got)
	}
	if !strings.Contains(got, "slimference filter cargo build") {
		t.Fatalf("cargo build must be rewritten: %q", got)
	}
}

func TestRewriteCommand_EmptyCommand(t *testing.T) {
	t.Parallel()
	_, ok := RewriteCommand("", nil)
	if ok {
		t.Fatal("empty command must not be rewritten")
	}
}

// --- tokenize tests ---

func TestTokenize_SimpleArgs(t *testing.T) {
	t.Parallel()
	toks := tokenize("git status")
	if len(toks) != 2 {
		t.Fatalf("want 2 tokens, got %d: %v", len(toks), toks)
	}
	if toks[0].Kind != TokenArg || toks[0].Value != "git" {
		t.Fatalf("tok[0]: %v", toks[0])
	}
	if toks[1].Kind != TokenArg || toks[1].Value != "status" {
		t.Fatalf("tok[1]: %v", toks[1])
	}
}

func TestTokenize_Operators(t *testing.T) {
	t.Parallel()
	toks := tokenize("a && b || c ; d")
	kinds := make([]TokenKind, len(toks))
	for i, t := range toks {
		kinds[i] = t.Kind
	}
	want := []TokenKind{TokenArg, TokenOperator, TokenArg, TokenOperator, TokenArg, TokenOperator, TokenArg}
	for i := range want {
		if i >= len(kinds) || kinds[i] != want[i] {
			t.Fatalf("token kinds mismatch: got %v want %v", kinds, want)
		}
	}
}

func TestTokenize_Pipe(t *testing.T) {
	t.Parallel()
	toks := tokenize("cargo test | tail -20")
	var pipeIdx int
	for i, t := range toks {
		if t.Kind == TokenPipe {
			pipeIdx = i
		}
	}
	if pipeIdx == 0 {
		t.Fatal("no pipe found")
	}
}

func TestTokenize_Redirect2And1(t *testing.T) {
	t.Parallel()
	toks := tokenize("cargo test 2>&1")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenRedirect && t.Value == "2>&1" {
			found = true
		}
	}
	if !found {
		t.Fatal("2>&1 not parsed as redirect")
	}
}

func TestTokenize_SingleQuotes(t *testing.T) {
	t.Parallel()
	toks := tokenize("find . -name '*.go'")
	// last token should contain *.go without being flagged as shellism
	// (single-quoted globs are literal)
	last := toks[len(toks)-1]
	if last.Kind == TokenShellism {
		t.Fatalf("single-quoted glob must not be shellism, got %v", last)
	}
	if last.Value != "*.go" {
		t.Fatalf("single-quoted value wrong: %q", last.Value)
	}
}

func TestTokenize_DoubleQuotes(t *testing.T) {
	t.Parallel()
	toks := tokenize(`git commit -m "fix: update deps"`)
	// message should be a single arg token
	var msgTok *ParsedToken
	for i := range toks {
		if toks[i].Value == "fix: update deps" {
			msgTok = &toks[i]
		}
	}
	if msgTok == nil {
		t.Fatalf("double-quoted message not found in tokens: %v", toks)
	}
}

func TestTokenize_DollarExpansion(t *testing.T) {
	t.Parallel()
	toks := tokenize("echo $(git rev-parse HEAD)")
	var foundShellism bool
	for _, t := range toks {
		if t.Kind == TokenShellism {
			foundShellism = true
		}
	}
	if !foundShellism {
		t.Fatal("$(...) must produce a shellism token")
	}
}
