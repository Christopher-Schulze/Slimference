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

func TestFindStringForKey_deterministicSiblingTraversal(t *testing.T) {
	t.Parallel()
	for range 50 {
		v := map[string]interface{}{
			"z": map[string]interface{}{"command": "z-last"},
			"a": map[string]interface{}{"command": "a-first"},
		}
		s, ok := findStringForKey(v, "command")
		if !ok || s != "a-first" {
			t.Fatalf("deterministic traversal failed: got %q ok=%v", s, ok)
		}
	}
}

// --- RewriteCommand tests (docs/spec.md §4.2) ---

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

func TestRewriteCommand_SimpleWc(t *testing.T) {
	t.Parallel()
	got, ok := RewriteCommand("wc -l internal/filter/builtin_fs.go", nil)
	if !ok {
		t.Fatal("expected wc filter match")
	}
	if got != "slimference filter wc -l internal/filter/builtin_fs.go" {
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

func TestRewriteCommand_RTKBreadthCommands(t *testing.T) {
	t.Parallel()

	tests := []string{
		"ansible-playbook site.yml",
		"ansible-lint playbook.yml",
		"autopep8 -i app.py",
		"basedpyright .",
		"bazel build //...",
		"brew install go",
		"buf lint",
		"cmake --build build",
		"curl https://api.example.com/data",
		"diff -u old.txt new.txt",
		"df -h /",
		"dprint fmt",
		"du -sh internal",
		"eslint .",
		"fail2ban-client status",
		"g++ -Wall main.cpp",
		"gcloud container clusters list",
		"gocritic check ./...",
		"gofumpt -w .",
		"gosec ./...",
		"gradlew build",
		"gt status",
		"hadolint Dockerfile",
		"iptables -L",
		"jira issue list",
		"jj status",
		"jq . package.json",
		"just test",
		"liquibase update",
		"markdownlint README.md",
		"mise install",
		"mix compile",
		"next build",
		"ninja -C build",
		"npx tsc --noEmit",
		"nx build app",
		"ollama list",
		"oxlint .",
		"pipenv install",
		"ping -c 1 example.com",
		"pio run",
		"prisma migrate dev",
		"poetry install",
		"pre-commit run --all-files",
		"pyright .",
		"ps aux",
		"quarto render report.qmd",
		"rsync -av src dst",
		"semgrep --config=auto .",
		"shellcheck scripts/run.sh",
		"shopify theme pull",
		"shfmt -w script.sh",
		"skopeo inspect docker://example/image",
		"sops -d secrets.yaml",
		"sqlfluff lint models",
		"staticcheck ./...",
		"stat README.md",
		"stylelint src/**/*.css",
		"swift build",
		"task test",
		"taplo check Cargo.toml",
		"terraform plan",
		"tofu validate",
		"trunk build",
		"turbo run build",
		"ty check",
		"uv sync",
		"vite build",
		"wget -qO- https://api.example.com/data",
		"webpack --mode production",
		"xcodebuild test",
		"yadm status",
		"yamllint .",
	}

	for _, cmd := range tests {
		cmd := cmd
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			got, ok := RewriteCommand(cmd, nil)
			if !ok {
				t.Fatalf("expected broad command to be rewritten: %q", cmd)
			}
			if want := "slimference filter " + cmd; got != want {
				t.Fatalf("rewrite mismatch: got %q want %q", got, want)
			}
		})
	}
}

func TestRewriteCommand_RiskyArbitraryOutputCommandsStayUnrewritten(t *testing.T) {
	t.Parallel()

	for _, cmd := range []string{"dart run bin/app.dart", "deno run app.ts", "flutter run", "java -jar app.jar", "ssh host uptime"} {
		cmd := cmd
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			if got, ok := RewriteCommand(cmd, nil); ok {
				t.Fatalf("arbitrary-output command must not be rewritten by default: got %q", got)
			}
		})
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
	// Exact example from docs/spec.md §4.2
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

// Additional tokenizer tests to cover uncovered branches.

func TestTokenize_AppendRedirect(t *testing.T) {
	t.Parallel()
	toks := tokenize("echo hi >> out.txt")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenRedirect && t.Value == ">>" {
			found = true
		}
	}
	if !found {
		t.Fatal(">> not parsed as redirect")
	}
}

func TestTokenize_AndRedirect(t *testing.T) {
	t.Parallel()
	toks := tokenize("cmd &> out.txt")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenRedirect && t.Value == "&>" {
			found = true
		}
	}
	if !found {
		t.Fatal("&> not parsed as redirect")
	}
}

func TestTokenize_AndAppendRedirect(t *testing.T) {
	t.Parallel()
	toks := tokenize("cmd &>> out.txt")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenRedirect && t.Value == "&>>" {
			found = true
		}
	}
	if !found {
		t.Fatal("&>> not parsed as redirect")
	}
}

func TestTokenize_HeredocString(t *testing.T) {
	t.Parallel()
	toks := tokenize("cmd <<< word")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenRedirect && t.Value == "<<<" {
			found = true
		}
	}
	if !found {
		t.Fatal("<<< not parsed as redirect")
	}
}

func TestTokenize_DollarBraceExpansion(t *testing.T) {
	t.Parallel()
	toks := tokenize("echo ${HOME}/path")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenShellism && strings.Contains(t.Value, "${HOME}") {
			found = true
		}
	}
	if !found {
		t.Fatal("${VAR} must produce a shellism token")
	}
}

func TestTokenize_BacktickExpansion(t *testing.T) {
	t.Parallel()
	toks := tokenize("echo `date`")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenShellism && strings.Contains(t.Value, "`date`") {
			found = true
		}
	}
	if !found {
		t.Fatal("backtick expansion must produce a shellism token")
	}
}

func TestTokenize_BackslashEscape(t *testing.T) {
	t.Parallel()
	toks := tokenize(`echo hello\ world`)
	if len(toks) < 2 {
		t.Fatalf("want >=2 tokens, got %d: %v", len(toks), toks)
	}
	// "hello world" should be a single token after escape processing
	combined := toks[1].Value
	if !strings.Contains(combined, "hello") {
		t.Fatalf("escaped space not handled: %v", toks)
	}
}

func TestTokenize_ArithmeticExpansion(t *testing.T) {
	t.Parallel()
	toks := tokenize("echo $((1+2))")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenShellism && strings.Contains(t.Value, "$((1+2))") {
			found = true
		}
	}
	if !found {
		t.Fatal("$((expr)) must produce a shellism token")
	}
}

func TestTokenize_SemicolonOperator(t *testing.T) {
	t.Parallel()
	toks := tokenize("a ; b")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenOperator && t.Value == ";" {
			found = true
		}
	}
	if !found {
		t.Fatal("; not parsed as operator")
	}
}

func TestTokenize_DoubleQuoteEscape(t *testing.T) {
	t.Parallel()
	toks := tokenize(`echo "hello \"world\""`)
	// The escaped quote should be handled inside double quotes
	for _, t := range toks {
		if strings.Contains(t.Value, `hello "world"`) {
			return
		}
	}
	t.Fatalf("double-quote escape not handled: %v", toks)
}

func TestTokenize_GlobShellism(t *testing.T) {
	t.Parallel()
	toks := tokenize("ls *.go")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenShellism && t.Value == "*.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("unquoted glob must be shellism")
	}
}

func TestTokenize_SingleRedirect(t *testing.T) {
	t.Parallel()
	toks := tokenize("cmd > out.txt")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenRedirect && t.Value == ">" {
			found = true
		}
	}
	if !found {
		t.Fatal("> not parsed as redirect")
	}
}

func TestTokenize_InputRedirect(t *testing.T) {
	t.Parallel()
	toks := tokenize("cmd < in.txt")
	var found bool
	for _, t := range toks {
		if t.Kind == TokenRedirect && t.Value == "<" {
			found = true
		}
	}
	if !found {
		t.Fatal("< not parsed as redirect")
	}
}

func TestTokenize_LeadingWhitespace(t *testing.T) {
	t.Parallel()
	toks := tokenize("  git status")
	if len(toks) != 2 {
		t.Fatalf("want 2 tokens after leading whitespace, got %d", len(toks))
	}
	if toks[0].Value != "git" {
		t.Fatalf("first token should be 'git', got %q", toks[0].Value)
	}
}

func TestTokenize_EmptyInput(t *testing.T) {
	t.Parallel()
	toks := tokenize("")
	if len(toks) != 0 {
		t.Fatalf("empty input should produce 0 tokens, got %d", len(toks))
	}
}

func TestTokenize_DoubleQuoteDollar(t *testing.T) {
	t.Parallel()
	// $ inside double quotes should trigger shellism
	toks := tokenize(`echo "$HOME"`)
	var found bool
	for _, t := range toks {
		if t.Kind == TokenShellism {
			found = true
		}
	}
	if !found {
		t.Fatal("$VAR inside double quotes must produce shellism")
	}
}

// --- stageBaseCommand tests ---

func TestStageBaseCommand_SimpleArg(t *testing.T) {
	t.Parallel()
	toks := tokenize("git status")
	got := stageBaseCommand(toks)
	if got != "git" {
		t.Fatalf("want git, got %q", got)
	}
}

func TestStageBaseCommand_EnvVarPrefix(t *testing.T) {
	t.Parallel()
	// VAR=value prefix should be skipped
	toks := tokenize("FOO=bar git status")
	got := stageBaseCommand(toks)
	if got != "git" {
		t.Fatalf("want git after env-var skip, got %q", got)
	}
}

func TestStageBaseCommand_EnvVarWithSlash(t *testing.T) {
	t.Parallel()
	// PATH=/usr/bin: "PATH" has no slash/dot/minus before =, so it IS treated as env-var skip
	// The command after it is the real command
	toks := tokenize("PATH=/usr/bin echo hi")
	got := stageBaseCommand(toks)
	if got != "echo" {
		t.Fatalf("PATH=/usr/bin is env-var skip, base should be 'echo', got %q", got)
	}
}

func TestStageBaseCommand_PathWithSlashNotEnvVar(t *testing.T) {
	t.Parallel()
	// /usr/bin/git has a slash but no = so it is a regular arg (the command itself)
	toks := tokenize("/usr/bin/git status")
	got := stageBaseCommand(toks)
	if got != "git" {
		t.Fatalf("/usr/bin/git base should be 'git', got %q", got)
	}
}

func TestStageBaseCommand_EmptyTokens(t *testing.T) {
	t.Parallel()
	got := stageBaseCommand(nil)
	if got != "" {
		t.Fatalf("empty tokens should give empty string, got %q", got)
	}
}

func TestStageBaseCommand_NonArgOnly(t *testing.T) {
	t.Parallel()
	// Only operator tokens -> no base command
	toks := tokenize("&& ||")
	got := stageBaseCommand(toks)
	if got != "" {
		t.Fatalf("operator-only tokens should give empty string, got %q", got)
	}
}

// --- RewriteCommand edge cases ---

func TestRewriteCommand_OrOperator(t *testing.T) {
	t.Parallel()
	got, ok := RewriteCommand("git status || echo fail", nil)
	if !ok {
		t.Fatal("expected filter match")
	}
	if !strings.Contains(got, "slimference filter git status") {
		t.Fatalf("left side must be rewritten: %q", got)
	}
}

func TestRewriteCommand_RewritePrefix(t *testing.T) {
	t.Parallel()
	// "slimference rewrite" prefix should also be skipped
	cmd := "slimference rewrite something"
	got, ok := RewriteCommand(cmd, nil)
	if ok {
		t.Fatal("slimference rewrite prefix must not be rewritten")
	}
	if got != cmd {
		t.Fatalf("passthrough must be exact: %q vs %q", got, cmd)
	}
}

func TestRewriteCommand_MultipleEnvVars(t *testing.T) {
	t.Parallel()
	got, ok := RewriteCommand("FOO=1 BAR=2 go test ./...", nil)
	if !ok {
		t.Fatal("expected filter match with env-var prefix")
	}
	if !strings.Contains(got, "slimference filter") {
		t.Fatalf("go test should be rewritten: %q", got)
	}
}

func TestRewriteCommand_OnlyWhitespace(t *testing.T) {
	t.Parallel()
	_, ok := RewriteCommand("   ", nil)
	if ok {
		t.Fatal("whitespace-only must not be rewritten")
	}
}

func TestRenderSegTokens_Empty(t *testing.T) {
	t.Parallel()
	got := renderSegTokens(nil)
	if got != "" {
		t.Fatalf("empty tokens should give empty string, got %q", got)
	}
}
