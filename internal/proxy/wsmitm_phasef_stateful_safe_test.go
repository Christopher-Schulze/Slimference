package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestWSSStatefulToolOutputMutationSafeAdditionalEvidenceClasses(t *testing.T) {
	meta := wssRequestMeta{SessionID: "codex-wss:stateful-safe", PreviousResponseID: "resp-stateful-safe"}
	diffStat := wssDiffStatFixture(36)
	gitShowStat := wssGitShowStatFixture(36)
	var nameStatusOutput strings.Builder
	for i := 0; i < 40; i++ {
		status := "M"
		if i%3 == 0 {
			status = "A"
		}
		nameStatusOutput.WriteString(status)
		nameStatusOutput.WriteByte('\t')
		nameStatusOutput.WriteString(fmt.Sprintf("internal/proxy/generated/very/deep/path/file_%02d.go\n", i))
	}
	var wcOutput strings.Builder
	wcArgs := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("internal/proxy/generated/very/deep/path/file_%02d.go", i)
		wcArgs = append(wcArgs, path)
		wcOutput.WriteString(fmt.Sprintf("      %d %s\n", i+300, path))
	}
	wcOutput.WriteString("     6190 total\n")
	listingOutput := wssListingFixture(40)
	rgLargeListingOutput := wssListingFixture(wssSafeListingOutputMaxEntries + 1)
	treeOutput := wssTreeFixture(40)
	goTestAllPass := wssGoTestVerboseAllPassFixture(80)
	goTestFailure := wssGoTestVerboseFailureFixture(40)
	goTestRace := "=== RUN   TestRacy\nWARNING: DATA RACE\n--- PASS: TestRacy (0.00s)\nPASS\nok  \tslimtest/lib\t0.006s\n"
	cargoTestAllPass := wssCargoTestVerboseAllPassFixture(80)
	pytestAllPass := wssPytestVerboseAllPassFixture(80)
	jestAllPass := wssJestVerboseAllPassFixture(70)
	mochaAllPass := wssMochaAllPassFixture(70)
	avaAllPass := wssAvaAllPassFixture(70)
	rspecAllPass := wssRspecAllPassFixture(70)
	rspecFailure := "....F\n\nFailures:\n\n  1) Widget renders failure details\n     Failure/Error: expect(result).to eq(:ok)\n\n     # ./spec/widget_spec.rb:42:in `block (2 levels) in <top (required)>'\n\nFinished in 0.05432 seconds\n5 examples, 1 failure\n"
	mochaFailure := "  widget suite\n    1) renders failure details\n\n  0 passing (10ms)\n  1 failing\n"
	avaFailure := "  ✖ renders failure details\n\n  1 test failed\n"
	dotnetAllPass := wssDotnetTestAllPassFixture(60)
	dotnetBuildSuccess := wssDotnetBuildSuccessFixture(24, 0)
	dotnetBuildWarning := wssDotnetBuildSuccessFixture(24, 1)
	dotnetWarning := "Passed!  - Failed: 0, Passed: 60, Skipped: 0, Total: 60, Duration: 1 s - Tests.dll (net8.0)\nWarning: diagnostics were emitted\n"
	mypySuccess := wssMypySuccessFixture(12)
	mypyFailure := "src/app.py:11: error: Incompatible return value type\nsrc/app.py:11: note: expected str\nFound 1 error in 1 file (checked 48 source files)\n"
	terraformValidateSuccess := strings.Join([]string{
		"Terraform used the selected providers to generate the following execution plan.",
		"Acquiring state lock. This may take a few moments...",
		"Success! The configuration is valid.",
		"The configuration is valid.",
	}, "\n") + "\n"
	terraformValidateFailure := "╷\n│ Error: Missing required argument\n│\n│   on main.tf line 12, in resource \"aws_s3_bucket\" \"bad\":\n│   12: resource \"aws_s3_bucket\" \"bad\" {}\n╵\n"
	emptyBuildEnvelope := "Chunk ID: build-empty\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10\nOutput:\n"
	goBuildWarning := "# github.com/slim/example\n# Compiled successfully\nwarning: generated binding is deprecated\nBuild succeeded with 0 errors and 1 warning.\n"

	tests := []struct {
		name      string
		command   string
		output    string
		wantSafe  bool
		wantGuard string
	}{
		{name: "git diff stat", command: "git diff --stat", output: diffStat, wantSafe: true},
		{name: "git show stat", command: "git show --stat HEAD -- internal/proxy", output: gitShowStat, wantSafe: true},
		{name: "git diff name-only path list", command: "git diff --name-only --cached", output: listingOutput, wantSafe: true},
		{name: "git diff name-status path list", command: "git diff --name-status --cached", output: nameStatusOutput.String(), wantSafe: true},
		{name: "git status short pathspec", command: "git status --short .", output: " M internal/proxy/wsmitm_phasef.go\n", wantSafe: true},
		{name: "git log oneline bounded", command: "git log --oneline -n 3", output: "a1b2c3d Tighten guard\nb2c3d4e Recover savings\nc3d4e5f Add proof\n", wantSafe: true},
		{name: "git ls-files path list", command: "git ls-files --cached", output: listingOutput, wantSafe: true},
		{name: "wc line counts", command: "wc -l " + strings.Join(wcArgs, " "), output: wcOutput.String(), wantSafe: true},
		{name: "go test verbose all-pass", command: "GOCACHE=/tmp/slimference-cache go test ./... -v", output: goTestAllPass, wantSafe: true},
		{name: "cargo test verbose all-pass", command: "cargo test", output: cargoTestAllPass, wantSafe: true},
		{name: "pytest verbose all-pass", command: "pytest -v", output: pytestAllPass, wantSafe: true},
		{name: "jest verbose all-pass", command: "jest", output: jestAllPass, wantSafe: true},
		{name: "mocha verbose all-pass", command: "mocha", output: mochaAllPass, wantSafe: true},
		{name: "ava verbose all-pass", command: "ava", output: avaAllPass, wantSafe: true},
		{name: "rspec all-pass", command: "bundle exec rspec", output: rspecAllPass, wantSafe: true},
		{name: "dotnet test all-pass", command: "dotnet test", output: dotnetAllPass, wantSafe: true},
		{name: "dotnet build success no warnings", command: "dotnet build", output: dotnetBuildSuccess, wantSafe: true},
		{name: "mypy success summary", command: "mypy src", output: mypySuccess, wantSafe: true},
		{name: "terraform validate success summary", command: "terraform validate", output: terraformValidateSuccess, wantSafe: true},
		{name: "ls small listing", command: "ls internal/proxy", output: listingOutput, wantSafe: true},
		{name: "cd wrapped ls small listing", command: "cd /repo/project && ls internal/proxy", output: listingOutput, wantSafe: true},
		{name: "format path list", command: "gofmt -l .", output: listingOutput, wantSafe: true},
		{name: "wrapped prettier path list", command: "pnpm exec prettier --write src", output: listingOutput, wantSafe: true},
		{name: "rg files path list", command: "rg --files -g '*.go' internal/proxy", output: listingOutput, wantSafe: true},
		{name: "rg files large path list", command: "rg --files", output: rgLargeListingOutput, wantSafe: true},
		{name: "fd path list", command: "fd .go internal/proxy", output: listingOutput, wantSafe: true},
		{name: "fdfind path list", command: "fdfind --extension go internal/proxy", output: listingOutput, wantSafe: true},
		{name: "find small listing", command: "find internal/proxy -maxdepth 2 -type f -name '*.go' -print", output: listingOutput, wantSafe: true},
		{name: "cd wrapped find small listing", command: "cd /repo/project && find internal/proxy -maxdepth 2 -type f -name '*.go' -print", output: listingOutput, wantSafe: true},
		{name: "tree bounded listing", command: "tree -L 2 internal/proxy", output: treeOutput, wantSafe: true},
		{name: "cd wrapped tree bounded listing", command: "cd /repo/project && tree -L 2 internal/proxy", output: treeOutput, wantSafe: true},
		{name: "tree bounded option separator", command: "tree -L 2 -- internal/proxy", output: treeOutput, wantSafe: true},
		{name: "cd wrapped git diff stat", command: "cd /repo/project && git diff --stat", output: diffStat, wantSafe: true},
		{name: "cd wrapped git log oneline bounded", command: "cd /repo/project && git log --oneline -n 3", output: "a1b2c3d Tighten guard\nb2c3d4e Recover savings\nc3d4e5f Add proof\n", wantSafe: true},
		{name: "cd wrapped wc line counts", command: "cd /repo/project && wc -l " + strings.Join(wcArgs, " "), output: wcOutput.String(), wantSafe: true},
		{name: "cd wrapped go test verbose all-pass", command: "cd /repo/project && GOCACHE=/tmp/slimference-cache go test ./... -v", output: goTestAllPass, wantSafe: true},
		{name: "git status rich output", command: "git status", output: "On branch main\nChanges not staged for commit:\n\tmodified: internal/proxy/wsmitm_phasef.go\n", wantGuard: "rich git status output stays guarded"},
		{name: "relative cd wrapper", command: "cd repo && git diff --stat", output: diffStat, wantGuard: "relative cd wrappers stay guarded"},
		{name: "git ls-files staged metadata", command: "git ls-files --stage", output: "100644 abcdef1234567890abcdef1234567890abcdef12 0\tinternal/proxy/wsmitm_phasef.go\n", wantGuard: "git ls-files metadata stays guarded"},
		{name: "git log oneline unbounded", command: "git log --oneline", output: "a1b2c3d Tighten guard\n", wantGuard: "unbounded log output stays guarded"},
		{name: "git log rich output", command: "git log --stat -n 3", output: "commit a1b2c3d4\n\n    Tighten guard\n\n file.go | 2 ++\n", wantGuard: "rich log output stays guarded"},
		{name: "git show without stat", command: "git show HEAD", output: gitShowStat, wantGuard: "git show without --stat stays guarded"},
		{name: "git show patch argv", command: "git show --stat --patch HEAD", output: wssGitShowPatchFixture(), wantGuard: "git show patch argv stays guarded"},
		{name: "git show patch payload", command: "git show --stat HEAD", output: wssGitShowPatchFixture(), wantGuard: "git show patch payload stays guarded"},
		{name: "git diff name-status rename metadata", command: "git diff --name-status -M", output: "R100\told/path.go\tinternal/proxy/wsmitm_phasef.go\n", wantGuard: "git diff rename name-status stays guarded"},
		{name: "full git diff source", command: "git diff", output: "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-func old() {}\n+func new() {}\n", wantGuard: "full git diff must stay guarded"},
		{name: "go test failure", command: "go test ./... -v", output: goTestFailure, wantSafe: true},
		{name: "go test data race", command: "go test ./... -v", output: goTestRace, wantGuard: "go test data race stays guarded"},
		{name: "cargo test failure", command: "cargo test", output: "running 2 tests\ntest a ... ok\ntest b ... FAILED\n\ntest result: FAILED. 1 passed; 1 failed\n", wantGuard: "cargo test failures stay guarded"},
		{name: "pytest failure", command: "pytest -v", output: "tests/test_a.py::test_x FAILED\n=== 1 failed in 0.1s ===\n", wantGuard: "pytest failures stay guarded"},
		{name: "jest failure", command: "jest", output: "FAIL src/a.test.ts\n  x broken (3 ms)\nTests: 1 failed, 1 total\n", wantGuard: "jest failures stay guarded"},
		{name: "mocha failure", command: "mocha", output: mochaFailure, wantGuard: "mocha failures stay guarded"},
		{name: "ava failure", command: "ava", output: avaFailure, wantGuard: "ava failures stay guarded"},
		{name: "rspec failure", command: "bundle exec rspec", output: rspecFailure, wantGuard: "rspec failures stay guarded"},
		{name: "dotnet test warning", command: "dotnet test", output: dotnetWarning, wantGuard: "dotnet warnings stay guarded"},
		{name: "dotnet build warning", command: "dotnet build", output: dotnetBuildWarning, wantGuard: "dotnet build warnings stay guarded"},
		{name: "mypy failure", command: "mypy src", output: mypyFailure, wantGuard: "mypy diagnostics stay guarded"},
		{name: "terraform validate failure", command: "terraform validate", output: terraformValidateFailure, wantGuard: "terraform validate diagnostics stay guarded"},
		{name: "empty build envelope", command: "go build ./...", output: emptyBuildEnvelope, wantGuard: "empty success envelopes stay guarded because they do not save bytes"},
		{name: "build success with warning", command: "go build ./...", output: goBuildWarning, wantGuard: "build warnings stay guarded"},
		{name: "ls long format", command: "ls -la internal/proxy", output: "total 16\n-rw-r--r--  1 user group 1200 Jan 01 00:00 wsmitm_phasef.go\n", wantGuard: "rich ls output stays guarded"},
		{name: "find unbounded", command: "find internal/proxy -type f -name '*.go' -print", output: listingOutput, wantGuard: "unbounded find stays guarded"},
		{name: "find exec", command: "find internal -type f -exec cat {} ;", output: listingOutput, wantGuard: "find side-effect/rich predicates stay guarded"},
		{name: "format output with timings", command: "prettier --write src", output: "src/file_001.ts 12ms\nsrc/file_002.ts 10ms\n", wantGuard: "formatter timing output stays guarded"},
		{name: "format output with diagnostic", command: "prettier --write src", output: "src/file_001.ts\nerror: failed to parse src/file_002.ts\n", wantGuard: "formatter diagnostics stay guarded"},
		{name: "rg files unsupported output mode", command: "rg --files --json", output: listingOutput, wantGuard: "rg --files rich output modes stay guarded"},
		{name: "rg files search list", command: "rg -l needle internal/proxy", output: listingOutput, wantGuard: "rg match file list stays search-guarded"},
		{name: "rg files too large", command: "rg --files", output: wssListingFixture(wssSafeRgFilesOutputMaxEntries + 1), wantGuard: "oversized rg --files output stays guarded"},
		{name: "fd exec output mode", command: "fd .go internal/proxy --exec cat {}", output: listingOutput, wantGuard: "fd exec output stays guarded"},
		{name: "fd details output mode", command: "fd .go internal/proxy --list-details", output: listingOutput, wantGuard: "fd rich listing output stays guarded"},
		{name: "fd diagnostic output", command: "fd .go internal/proxy", output: "error: invalid regex\n" + listingOutput, wantGuard: "fd diagnostics stay guarded"},
		{name: "fd too large", command: "fd .go internal/proxy", output: wssListingFixture(wssSafeRgFilesOutputMaxEntries + 1), wantGuard: "oversized fd output stays guarded"},
		{name: "tree unbounded", command: "tree internal/proxy", output: treeOutput, wantGuard: "unbounded tree output stays guarded"},
		{name: "tree separator without depth", command: "tree -- internal/proxy", output: treeOutput, wantGuard: "unbounded tree with separator stays guarded"},
		{name: "tree deep", command: "tree -L 99 internal/proxy", output: treeOutput, wantGuard: "deep tree output stays guarded"},
		{name: "tree unknown flag", command: "tree --du internal/proxy", output: treeOutput, wantGuard: "rich tree flags stay guarded"},
		{name: "oversized listing", command: "ls internal/proxy", output: wssListingFixture(wssSafeListingOutputMaxEntries + 1), wantGuard: "oversized listings stay guarded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := []types.Message{{
				Role: "tool",
				Content: []types.ContentBlock{{
					Type:         "tool_result",
					ToolResultID: "call-safe",
					Text:         tt.output,
				}},
			}}
			remembered := map[string]types.ContentBlock{
				"call-safe": {
					Type:      "tool_use",
					ToolUseID: "call-safe",
					ToolName:  "exec_command",
					ToolInput: fmt.Sprintf(`{"cmd":%q}`, tt.command),
				},
			}
			got := wssStatefulToolOutputMutationSafe(meta, true, messages, remembered)
			if got != tt.wantSafe {
				t.Fatalf("stateful safety=%v want %v (%s)", got, tt.wantSafe, tt.wantGuard)
			}
		})
	}
}

func TestWSSStatefulSafeGenericTestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: pytest-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssPytestVerboseAllPassFixture(120)

	env := parseWSJSON(t, wssGoTestRequestBody("resp-pytest-all-pass", "call_pytest_all_pass", "pytest -v", envelope))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle pytest all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history pytest all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[pytest] ok - 120 passed") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "test_op_119") {
		t.Fatalf("pytest all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe pytest all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeRspecAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: rspec-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssRspecAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-rspec-all-pass", "call_rspec_all_pass", "bundle exec rspec", envelope, "stateful-rspec-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle rspec all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history rspec all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[rspec] ok (120 examples, 0 failures)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "example_119") {
		t.Fatalf("rspec all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe rspec all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeDotnetBuildSuccessCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: dotnet-build-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssDotnetBuildSuccessFixture(120, 0)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-dotnet-build-success", "call_dotnet_build_success", "dotnet build", envelope, "stateful-dotnet-build-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle dotnet build success request: %v", err)
	}
	if !replace {
		t.Fatal("full-history dotnet build success output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[dotnet build] ok") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "Project119.dll") {
		t.Fatalf("dotnet build success output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe dotnet build should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeMypySuccessCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: mypy-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssMypySuccessFixture(80)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-mypy-success", "call_mypy_success", "mypy src", envelope, "stateful-mypy-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle mypy success request: %v", err)
	}
	if !replace {
		t.Fatal("full-history mypy success output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[mypy] ok (Success: no issues found in 188 source files)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "Using Python executable for module 079") {
		t.Fatalf("mypy success output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe mypy success should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeListingRepeatCompactsOnSecondFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	listing := wssListingFixture(70)

	first := parseWSJSON(t, wssListingRequestBody("resp-listing-1", "call_listing_1", listing))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &first)
	if err != nil {
		t.Fatalf("handle first listing request: %v", err)
	}
	if replace {
		t.Fatalf("first listing observation should seed only, got mutation: %s", first.Body)
	}

	second := parseWSJSON(t, wssListingRequestBody("resp-listing-2", "call_listing_2", listing))
	replace, err = adapter.handle(context.Background(), wsmitm.DirClientToServer, &second)
	if err != nil {
		t.Fatalf("handle second listing request: %v", err)
	}
	if !replace {
		t.Fatal("second identical listing should compact through repeated-output archive reference")
	}
	body := string(second.Body)
	if !strings.Contains(body, "context-elided kind=tool-output status=unchanged") ||
		!strings.Contains(body, "archive=local-archive://") ||
		strings.Contains(body, "generated_listing_069.go") {
		t.Fatalf("listing repeat was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe listing should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeTreeRepeatCompactsOnSecondFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	tree := wssTreeFixture(90)

	first := parseWSJSON(t, wssTreeRequestBody("resp-tree-1", "call_tree_1", tree))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &first)
	if err != nil {
		t.Fatalf("handle first tree request: %v", err)
	}
	if replace {
		t.Fatalf("first tree observation should seed only, got mutation: %s", first.Body)
	}

	second := parseWSJSON(t, wssTreeRequestBody("resp-tree-2", "call_tree_2", tree))
	replace, err = adapter.handle(context.Background(), wsmitm.DirClientToServer, &second)
	if err != nil {
		t.Fatalf("handle second tree request: %v", err)
	}
	if !replace {
		t.Fatal("second identical tree should compact through repeated-output archive reference")
	}
	body := string(second.Body)
	if !strings.Contains(body, "context-elided kind=tool-output status=unchanged") ||
		!strings.Contains(body, "archive=local-archive://") ||
		strings.Contains(body, "tree_file_089.go") {
		t.Fatalf("tree repeat was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe tree should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeFormatPathListCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	listing := wssListingFixture(90)

	env := parseWSJSON(t, wssFormatPathListRequestBody("resp-format-path-list", "call_format_paths", listing))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle format path-list request: %v", err)
	}
	if !replace {
		t.Fatal("full-history format path-list output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[gofmt] 90 file(s) formatted") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "generated_listing_050.go") {
		t.Fatalf("format path-list output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe format path-list should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGitLsFilesCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	listing := wssListingFixture(90)

	env := parseWSJSON(t, wssGitLsFilesRequestBody("resp-git-ls-files", "call_git_ls_files", listing))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle git ls-files request: %v", err)
	}
	if !replace {
		t.Fatal("full-history git ls-files output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git ls-files paths]") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "internal/proxy/") ||
		!strings.Contains(body, "generated_listing_050.go") ||
		strings.Contains(body, "internal/proxy/generated_listing_050.go") {
		t.Fatalf("git ls-files output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe git ls-files should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeRgFilesRootPathListCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	listing := wssRgFilesRootListingFixture(90)

	env := parseWSJSON(t, wssRgFilesRequestBody("resp-rg-files-root", "call_rg_files_root", "rg --files", listing))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle rg --files root path-list request: %v", err)
	}
	if !replace {
		t.Fatal("full-history rg --files root path-list output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[rg --files paths]") ||
		!strings.Contains(body, "./") ||
		!strings.Contains(body, "AGENTS.md") ||
		!strings.Contains(body, "internal/proxy/generated/deep/path/") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "internal/proxy/generated/deep/path/file_050.go") {
		t.Fatalf("rg --files root path-list output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe rg --files root path-list should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeFdPathListCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	listing := wssListingFixture(90)

	env := parseWSJSON(t, wssFdPathListRequestBody("resp-fd-path-list", "call_fd_path_list", "fd .go internal/proxy", listing))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle fd path-list request: %v", err)
	}
	if !replace {
		t.Fatal("full-history fd path-list output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[fd paths]") ||
		!strings.Contains(body, "internal/proxy/") ||
		!strings.Contains(body, "generated_listing_050.go") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "internal/proxy/generated_listing_050.go") {
		t.Fatalf("fd path-list output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe fd path-list should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGitDiffNameOnlyCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	listing := wssListingFixture(90)

	env := parseWSJSON(t, wssGitDiffNameOnlyRequestBody("resp-git-diff-name-only", "call_git_diff_name_only", listing))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle git diff --name-only request: %v", err)
	}
	if !replace {
		t.Fatal("full-history git diff --name-only output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git diff --name-only paths]") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "internal/proxy/") ||
		!strings.Contains(body, "generated_listing_050.go") ||
		strings.Contains(body, "internal/proxy/generated_listing_050.go") {
		t.Fatalf("git diff --name-only output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe git diff --name-only should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGitDiffNameStatusCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	var listing strings.Builder
	for i := 0; i < 90; i++ {
		status := "M"
		if i%3 == 0 {
			status = "A"
		}
		listing.WriteString(status)
		listing.WriteByte('\t')
		listing.WriteString(fmt.Sprintf("internal/proxy/generated_listing_%03d.go\n", i))
	}

	env := parseWSJSON(t, wssGitDiffNameStatusRequestBody("resp-git-diff-name-status", "call_git_diff_name_status", listing.String()))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle git diff --name-status request: %v", err)
	}
	if !replace {
		t.Fatal("full-history git diff --name-status output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git diff --name-status paths]") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "internal/proxy/") ||
		!strings.Contains(body, "M generated_listing_050.go") ||
		strings.Contains(body, "M\tinternal/proxy/generated_listing_050.go") {
		t.Fatalf("git diff --name-status output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe git diff --name-status should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeFindPathListCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	listing := wssListingFixture(90)

	env := parseWSJSON(t, wssFindPathListRequestBody("resp-find-path-list", "call_find_paths", listing))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle find path-list request: %v", err)
	}
	if !replace {
		t.Fatal("full-history find path-list output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[find paths]") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "internal/proxy/") ||
		!strings.Contains(body, "generated_listing_050.go") ||
		strings.Contains(body, "internal/proxy/generated_listing_050.go") {
		t.Fatalf("find path-list output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe find path-list should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeWcCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	command, output := wssWcFixture(45)

	env := parseWSJSON(t, wssWcRequestBody("resp-wc", "call_wc", command, output))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle wc request: %v", err)
	}
	if !replace {
		t.Fatal("full-history wc output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[wc prefix=internal/proxy/generated/very/deep/path/]") ||
		!strings.Contains(body, "total:") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "144 internal/proxy/generated/very/deep/path/file_44.go") {
		t.Fatalf("wc output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe wc output should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGoTestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: go-test-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssGoTestVerboseAllPassFixture(120)

	env := parseWSJSON(t, wssGoTestRequestBody("resp-go-test-all-pass", "call_go_test_all_pass", "GOCACHE=/tmp/slimference-cache go test ./... -v", envelope))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle go test all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history go test all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[go test] ok - 120 passed") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "TestPassing119") ||
		strings.Contains(body, "--- PASS: TestPassing000") {
		t.Fatalf("go test all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe go test all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGoTestFailureCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: go-test-failure-safe\nWall time: 0.0010 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" +
		wssGoTestVerboseFailureFixture(120)

	env := parseWSJSON(t, wssGoTestRequestBody("resp-go-test-failure", "call_go_test_failure", "GOCACHE=/tmp/slimference-cache go test ./... -v", envelope))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle go test failure request: %v", err)
	}
	if !replace {
		t.Fatal("full-history go test failure output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[go test] FAILED") ||
		!strings.Contains(body, "SLIMFERENCE_TEST_FAILURE_SENTINEL") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "TestPassing119") ||
		strings.Contains(body, "--- PASS: TestPassing000") {
		t.Fatalf("go test failure output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.full_history_stateless_followup"] != "true" {
		t.Fatalf("stateful-safe go test failure should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeGoTestFailureDeltaStillGuarded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: go-test-failure-delta\nWall time: 0.0010 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" +
		wssGoTestVerboseFailureFixture(120)

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-go-test-failure-delta",
			"prompt_cache_key":     "stateful-go-test-failure-delta-session",
			"input": []map[string]any{
				{"type": "function_call_output", "call_id": "call_go_test_failure_delta", "output": envelope},
			},
			"stream": true,
		},
	})
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle go test failure delta request: %v", err)
	}
	if replace ||
		strings.Contains(string(env.Body), "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(string(env.Body), "[go test] FAILED") {
		t.Fatalf("delta go test failure must stay byte-identical under the delta proof gate: %s", env.Body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.effective_mutation_guard"] != "wss_stateful_delta_mutation_proof_gate" ||
		summary.DebugFacts["wss.stateful_delta_mutation_blocked"] != "true" ||
		summary.DebugFacts["wss.request_shape"] != "delta" {
		t.Fatalf("delta go test failure should keep only the delta proof gate: %+v", summary.DebugFacts)
	}
}

func TestWSSStatefulSafeGitLogOnelineRepeatCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	logOutput := wssGitLogOnelineFixture(90)

	first := parseWSJSON(t, wssGitLogOnelineRequestBody("resp-log-1", "call_log_1", logOutput))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &first)
	if err != nil {
		t.Fatalf("handle first git log request: %v", err)
	}
	if replace {
		t.Fatalf("first git log oneline observation should seed only, got mutation: %s", first.Body)
	}

	second := parseWSJSON(t, wssGitLogOnelineRequestBody("resp-log-2", "call_log_2", logOutput))
	replace, err = adapter.handle(context.Background(), wsmitm.DirClientToServer, &second)
	if err != nil {
		t.Fatalf("handle second git log request: %v", err)
	}
	if !replace {
		t.Fatal("second identical git log oneline output should compact through repeated-output archive reference")
	}
	body := string(second.Body)
	if !strings.Contains(body, "context-elided kind=tool-output status=unchanged") ||
		!strings.Contains(body, "archive=local-archive://") ||
		strings.Contains(body, "commit subject 089") {
		t.Fatalf("git log repeat was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe git log oneline repeat should save without structured guard: %+v", summary)
	}
}

func TestWSSGitStatusPathspecBoundary(t *testing.T) {
	command := "git status --short ."
	output := " M internal/proxy/wsmitm_phasef.go\n"
	if !wssSafeGitStatusCommand(command) {
		t.Fatal("git status pathspec command should enter the strict output parser")
	}
	if _, ok := filter.TryCompactGitStatus(filter.ArgvForCapturedOutput(command), []byte(output)); !ok {
		t.Fatal("git status pathspec porcelain output should compact")
	}
	if looksLikeSource(output) || proxyToolResultLooksLikeSearchOutput(output) {
		t.Fatal("git status porcelain path line must not be classified as source or search output")
	}
	toolUse := types.ContentBlock{
		ToolName:  "exec_command",
		ToolInput: `{"cmd":"git status --short ."}`,
	}
	if got := proxyLayer0CommandLine(toolUse); got != command {
		t.Fatalf("tool command line = %q, want %q", got, command)
	}
	if !wssSafeStatefulStatusToolOutput(toolUse, output) {
		t.Fatal("git status pathspec should be stateful-safe after parser validation")
	}
}

func TestWSSStatefulSafeInferredCommandOutputBoundary(t *testing.T) {
	diffEnvelope := "Chunk ID: inferred-diffstat\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssDiffStatFixture(40)
	if command := proxyInferCommandLineFromToolResult(diffEnvelope); command != "git diff --stat" ||
		!wssSafeStatefulStatusCommandOutput(command, diffEnvelope) {
		t.Fatalf("inferred diffstat should be stateful-safe, command=%q", command)
	}

	searchEnvelope := "Chunk ID: inferred-search\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		"internal/proxy/a.go:10:needle\ninternal/proxy/b.go:20:needle\ninternal/proxy/c.go:30:needle\n"
	if command := proxyInferCommandLineFromToolResult(searchEnvelope); command != "rg" ||
		wssSafeStatefulStatusCommandOutput(command, searchEnvelope) {
		t.Fatalf("inferred search must stay guarded, command=%q", command)
	}

	failingTestEnvelope := "Chunk ID: inferred-test-fail\nWall time: 0.0010 seconds\nProcess exited with code 1\nOriginal token count: 10000\nOutput:\n" +
		wssGoTestVerboseFailureFixture(40)
	if command := proxyInferCommandLineFromToolResult(failingTestEnvelope); command != "go test" ||
		!wssSafeStatefulStatusCommandOutput(command, failingTestEnvelope) {
		t.Fatalf("inferred failing go test should be structured-safe, command=%q", command)
	}

	sourceEnvelope := "Chunk ID: inferred-source\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		"package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if command := proxyInferCommandLineFromToolResult(sourceEnvelope); command != "" ||
		wssSafeStatefulStatusCommandOutput(command, sourceEnvelope) {
		t.Fatalf("source-like inferred output must stay guarded, command=%q", command)
	}
}

func TestWSSToolCommandClassFactsAreContentFree(t *testing.T) {
	messages := []types.Message{{
		Role: "tool",
		Content: []types.ContentBlock{
			{Type: "tool_result", ToolResultID: "call_search", Text: "internal/proxy/wsmitm_phasef.go:1:needle\n"},
			{Type: "tool_result", ToolResultID: "call_show", Text: wssGitShowStatFixture(12)},
			{Type: "tool_result", ToolResultID: "call_unknown", Text: "opaque output\n"},
		},
	}}
	toolUses := map[string]types.ContentBlock{
		"call_search": {
			Type:      "tool_use",
			ToolUseID: "call_search",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"rg -n needle internal/proxy"}`,
		},
		"call_show": {
			Type:      "tool_use",
			ToolUseID: "call_show",
			ToolName:  "exec_command",
			ToolInput: `{"cmd":"git show --stat HEAD -- internal/proxy"}`,
		},
	}
	classes, classed, unclassed := wssToolCommandClassFacts(messages, toolUses)
	if classes != "git_show_stat=1,rg_search=1" || classed != 2 || unclassed != 1 {
		t.Fatalf("class facts = %q classed=%d unclassed=%d", classes, classed, unclassed)
	}
	for _, forbidden := range []string{"needle", "internal/proxy", "HEAD", "wsmitm_phasef.go"} {
		if strings.Contains(classes, forbidden) {
			t.Fatalf("class facts leaked command detail %q in %q", forbidden, classes)
		}
	}
}

func TestWSSToolCommandClassStableClasses(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "git status --short .", want: "git_status"},
		{command: "git diff --stat", want: "git_diff_stat"},
		{command: "git diff --name-only --cached", want: "git_diff_name_only"},
		{command: "git diff --name-status --cached", want: "git_diff_name_status"},
		{command: "git diff", want: "git_diff"},
		{command: "git show --stat HEAD -- internal/proxy", want: "git_show_stat"},
		{command: "git show HEAD", want: "git_show"},
		{command: "git log --oneline -n 20", want: "git_log_oneline"},
		{command: "git log --stat -n 3", want: "git_log"},
		{command: "git ls-files --cached", want: "git_ls_files"},
		{command: "git rev-parse HEAD", want: "git"},
		{command: "rg --files internal/proxy", want: "rg_files"},
		{command: "rg -n needle internal/proxy", want: "rg_search"},
		{command: "fd .go internal/proxy", want: "fd"},
		{command: "grep -R needle internal/proxy", want: "search"},
		{command: "go test ./internal/proxy", want: "go_test"},
		{command: "go env GOPATH", want: "go"},
		{command: "cargo test", want: "cargo_test"},
		{command: "cargo clippy", want: "cargo_build"},
		{command: "cargo metadata", want: "cargo"},
		{command: "pnpm test", want: "js_tool"},
		{command: "pytest tests", want: "pytest"},
		{command: "mypy src", want: "mypy"},
		{command: "/tmp/slimference-ok-summary-proof/mypy src", want: "mypy"},
		{command: "python3 -m mypy src", want: "mypy"},
		{command: "/opt/venv/bin/python -m mypy src", want: "mypy"},
		{command: "pnpm exec python3 -m mypy src", want: "mypy"},
		{command: "npx -y mypy src", want: "mypy"},
		{command: "python3 -m unittest", want: "python"},
		{command: "wc -l internal/proxy/wsmitm_phasef.go", want: "wc"},
		{command: "ls internal/proxy", want: "ls"},
		{command: "find internal/proxy -maxdepth 2 -type f", want: "find"},
		{command: "tree -L 2 internal/proxy", want: "tree"},
		{command: "cat internal/proxy/wsmitm_phasef.go", want: "read_like"},
		{command: "gofmt -l .", want: "format"},
		{command: "custom-tool --flag", want: "other"},
		{command: "cd /repo/project && git diff --stat", want: "git_diff_stat"},
		{command: "rg needle | head", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := wssToolCommandClass(tt.command); got != tt.want {
				t.Fatalf("wssToolCommandClass(%q)=%q want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestWSPhaseFPreviousResponseFullHistoryDiffStatCompacts(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-diffstat-safe",
			"prompt_cache_key":     "stateful-diffstat-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the diff stat"},
				{"type": "function_call", "call_id": "call_diffstat", "name": "exec_command", "arguments": map[string]any{"cmd": "git diff --stat"}},
				{"type": "function_call_output", "call_id": "call_diffstat", "output": wssDiffStatFixture(80)},
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle diffstat request: %v", err)
	}
	if !replace {
		t.Fatalf("previous_response full-history diffstat should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git diff --stat] 80 file(s)") ||
		!strings.Contains(body, "[prefix=internal/proxy/generated/very/deep/path/]") ||
		strings.Contains(body, "internal/proxy/generated/very/deep/path/file_xxxxxxxxxxxx_79.go") {
		t.Fatalf("diffstat compaction did not preserve compact evidence: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe diffstat should save without structured guard: %+v", summary)
	}
}

func TestWSPhaseFCDWrappedPreviousResponseFullHistoryDiffStatCompacts(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-cd-diffstat-safe",
			"prompt_cache_key":     "stateful-cd-diffstat-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the diff stat"},
				{"type": "function_call", "call_id": "call_cd_diffstat", "name": "exec_command", "arguments": map[string]any{"cmd": "cd /repo/project && git diff --stat"}},
				{"type": "function_call_output", "call_id": "call_cd_diffstat", "output": wssDiffStatFixture(80)},
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle cd-wrapped diffstat request: %v", err)
	}
	if !replace {
		t.Fatalf("cd-wrapped previous_response full-history diffstat should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git diff --stat] 80 file(s)") ||
		!strings.Contains(body, "[prefix=internal/proxy/generated/very/deep/path/]") ||
		strings.Contains(body, "internal/proxy/generated/very/deep/path/file_xxxxxxxxxxxx_79.go") {
		t.Fatalf("cd-wrapped diffstat compaction did not preserve compact evidence: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("cd-wrapped stateful-safe diffstat should save without structured guard: %+v", summary)
	}
}

func TestWSPhaseFInferredStatefulSafeFullHistoryDiffStatCompacts(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: inferred-diffstat-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssDiffStatFixture(80)

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-inferred-diffstat-safe",
			"prompt_cache_key":     "stateful-inferred-diffstat-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the diff stat"},
				{"type": "message", "role": "assistant", "content": "checking the diff stat"},
				{"type": "function_call_output", "call_id": "call_evicted_diffstat", "output": envelope},
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle inferred diffstat request: %v", err)
	}
	if !replace {
		t.Fatal("full-history inferred diffstat should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git diff --stat] 80 file(s)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "internal/proxy/generated/very/deep/path/file_xxxxxxxxxxxx_79.go") {
		t.Fatalf("inferred diffstat compaction did not preserve compact evidence: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.tool_results_inferred"] != "1" ||
		summary.DebugFacts["wss.tool_command_classes"] != "git_diff_stat=1" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("inferred stateful-safe diffstat should save without structured guard: %+v", summary)
	}
}

func TestWSPhaseFPreviousResponseFullHistoryGitShowStatCompacts(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	env := parseWSJSON(t, wssGitShowStatRequestBody("resp-git-show-stat", "call_git_show_stat", wssGitShowStatFixture(80)))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle git show --stat request: %v", err)
	}
	if !replace {
		t.Fatalf("previous_response full-history git show --stat should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[git show] a1b2c3d Recover safe stat compaction") ||
		!strings.Contains(body, "[git show --stat] 80 file(s)") ||
		!strings.Contains(body, "[prefix=internal/proxy/generated/very/deep/path/]") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "internal/proxy/generated/very/deep/path/file_xxxxxxxxxxxx_79.go") {
		t.Fatalf("git show --stat compaction did not preserve compact evidence: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe git show --stat should save without structured guard: %+v", summary)
	}
}

func TestReduceCodexLayer0StructuredMixedToolOutputsAllowsOnlySafeBlocks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	safeOutput := wssDiffStatFixture(80)
	unsafeOutput := "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-func old() {}\n+func new() {}\n"
	messages := []types.Message{
		{Role: "assistant", Content: []types.ContentBlock{
			{Type: "tool_use", ToolUseID: "call_diffstat", ToolName: "exec_command", ToolInput: `{"cmd":"git diff --stat"}`},
			{Type: "tool_use", ToolUseID: "call_diff", ToolName: "exec_command", ToolInput: `{"cmd":"git diff"}`},
		}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call_diffstat", Text: safeOutput}}},
		{Role: "tool", Content: []types.ContentBlock{{Type: "tool_result", ToolResultID: "call_diff", Text: unsafeOutput}}},
	}

	result := reduceCodexLayer0(codexLayer0Request{
		Route:                        codexLayer0RouteWSSPhaseF,
		Messages:                     messages,
		SessionID:                    "sess-mixed-stateful-safe",
		StructuredMutationBlocked:    true,
		StatefulDeltaMutationBlocked: false,
	})
	safeText := result.Messages[1].Content[0].Text
	unsafeText := result.Messages[2].Content[0].Text
	if result.Stats.BlocksModified != 1 || result.Stats.TokensSaved <= 0 || result.Stats.CapturedOutputBlocks != 1 {
		t.Fatalf("mixed request should mutate exactly the stateful-safe block, stats=%+v", result.Stats)
	}
	if safeText == safeOutput {
		t.Fatal("safe diffstat block should be compacted")
	}
	if !strings.Contains(safeText, "[git diff --stat] 80 file(s)") ||
		!strings.Contains(safeText, "[context-archive kind=tool-output uri=local-archive://") {
		t.Fatalf("safe diffstat block did not compact through archive-backed output: %s", safeText)
	}
	if unsafeText != unsafeOutput {
		t.Fatalf("unsafe full diff block changed: %q", unsafeText)
	}
}

func wssDiffStatFixture(files int) string {
	var out strings.Builder
	for i := 0; i < files; i++ {
		out.WriteString(" internal/proxy/generated/very/deep/path/file_")
		out.WriteString(strings.Repeat("x", 12))
		out.WriteString(fmt.Sprintf("_%02d.go | %d +++++-----\n", i, i+1))
	}
	out.WriteString(fmt.Sprintf(" %d files changed, %d insertions(+), %d deletions(-)\n", files, files*12, files*6))
	return out.String()
}

func wssGitShowStatFixture(files int) string {
	var out strings.Builder
	out.WriteString("commit a1b2c3d4e5f6a7b8\n")
	out.WriteString("Author: Alice <alice@example.com>\n")
	out.WriteString("Date:   Mon Apr 7 10:30:00 2025 +0000\n\n")
	out.WriteString("    Recover safe stat compaction\n\n")
	out.WriteString(wssDiffStatFixture(files))
	return out.String()
}

func wssGitShowPatchFixture() string {
	return wssGitShowStatFixture(3) + `
diff --git a/internal/proxy/wsmitm_phasef.go b/internal/proxy/wsmitm_phasef.go
index 111..222 100644
--- a/internal/proxy/wsmitm_phasef.go
+++ b/internal/proxy/wsmitm_phasef.go
@@ -1,2 +1,2 @@
-old line
+new line
`
}

func wssMypySuccessFixture(noiseLines int) string {
	var out strings.Builder
	for i := 0; i < noiseLines; i++ {
		fmt.Fprintf(&out, "Using Python executable for module %03d: /tmp/slimference-venv/bin/python\n", i)
	}
	out.WriteString("Success: no issues found in 188 source files\n")
	return out.String()
}

func wssCommandOutputRequestBody(previousResponseID, callID, command, output, cacheKey string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     cacheKey,
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the command output"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": command}},
				{"type": "function_call_output", "call_id": callID, "output": output},
			},
			"stream": true,
		},
	}
}

func wssGitShowStatRequestBody(previousResponseID, callID, output string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-git-show-stat-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the commit stat"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "git show --stat HEAD -- internal/proxy"}},
				{"type": "function_call_output", "call_id": callID, "output": output},
			},
			"stream": true,
		},
	}
}

func wssListingRequestBody(previousResponseID, callID, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-listing-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the proxy file listing"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "ls internal/proxy"}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssTreeRequestBody(previousResponseID, callID, tree string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-tree-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the proxy tree"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "tree -L 2 internal/proxy"}},
				{"type": "function_call_output", "call_id": callID, "output": tree},
			},
			"stream": true,
		},
	}
}

func wssFormatPathListRequestBody(previousResponseID, callID, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-format-path-list-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the formatter path list"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "gofmt -l ."}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssGitLsFilesRequestBody(previousResponseID, callID, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-git-ls-files-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the tracked file list"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "git ls-files --cached"}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssGitDiffNameOnlyRequestBody(previousResponseID, callID, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-git-diff-name-only-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the changed file list"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "git diff --name-only --cached"}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssGitDiffNameStatusRequestBody(previousResponseID, callID, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-git-diff-name-status-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the changed file status list"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "git diff --name-status --cached"}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssFindPathListRequestBody(previousResponseID, callID, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-find-path-list-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the bounded find path list"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "find internal/proxy -maxdepth 2 -type f -name '*.go' -print"}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssRgFilesRequestBody(previousResponseID, callID, command, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-rg-files-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the ripgrep file list"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": command}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssFdPathListRequestBody(previousResponseID, callID, command, listing string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-fd-path-list-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the fd file list"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": command}},
				{"type": "function_call_output", "call_id": callID, "output": listing},
			},
			"stream": true,
		},
	}
}

func wssWcRequestBody(previousResponseID, callID, command, output string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-wc-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the line counts"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": command}},
				{"type": "function_call_output", "call_id": callID, "output": output},
			},
			"stream": true,
		},
	}
}

func wssGoTestRequestBody(previousResponseID, callID, command, output string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-go-test-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the all-pass test run"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": command}},
				{"type": "function_call_output", "call_id": callID, "output": output},
			},
			"stream": true,
		},
	}
}

func wssGitLogOnelineRequestBody(previousResponseID, callID, output string) map[string]any {
	return map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": previousResponseID,
			"prompt_cache_key":     "stateful-git-log-oneline-safe-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "show the recent commits"},
				{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": map[string]any{"cmd": "git log --oneline -n 90"}},
				{"type": "function_call_output", "call_id": callID, "output": output},
			},
			"stream": true,
		},
	}
}

func wssGoTestVerboseAllPassFixture(count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	out.WriteString("PASS\nok  \tslimtest/lib\t0.006s\n")
	return out.String()
}

func wssGoTestVerboseFailureFixture(count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	out.WriteString("=== RUN   TestSlimferenceFailure\n")
	out.WriteString("    fail_test.go:42: SLIMFERENCE_TEST_FAILURE_SENTINEL expected alpha got beta\n")
	out.WriteString("--- FAIL: TestSlimferenceFailure (0.00s)\n")
	out.WriteString("FAIL\tslimtest/lib\t0.006s\n")
	return out.String()
}

func wssCargoTestVerboseAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("   Compiling slimtest v0.1.0\n    Finished test profile\n     Running unittests src/lib.rs\n\nrunning ")
	fmt.Fprintf(&out, "%d tests\n", count)
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "test alpha::op_%03d ... ok\n", i)
	}
	fmt.Fprintf(&out, "\ntest result: ok. %d passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n", count)
	return out.String()
}

func wssPytestVerboseAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("============================= test session starts ==============================\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "tests/test_alpha.py::test_op_%03d PASSED                                  [ %2d%%]\n", i, i)
	}
	fmt.Fprintf(&out, "============================== %d passed in 0.42s ===============================\n", count)
	return out.String()
}

func wssJestVerboseAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("PASS src/alpha.test.ts\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "  \u2713 renders op %03d (2 ms)\n", i)
	}
	fmt.Fprintf(&out, "\nTests: %d passed, %d total\nTime: 1.2 s\n", count, count)
	return out.String()
}

func wssMochaAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("  widget suite\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "    ✔ renders op %03d (2ms)\n", i)
	}
	fmt.Fprintf(&out, "\n  %d passing (95ms)\n", count)
	return out.String()
}

func wssAvaAllPassFixture(count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "  ✔ renders op %03d\n", i)
	}
	fmt.Fprintf(&out, "\n  %d tests passed\n", count)
	return out.String()
}

func wssRspecAllPassFixture(count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "spec/models/widget_spec.rb:%03d: example_%03d passed\n", i+1, i)
	}
	out.WriteString("\nFinished in 0.12345 seconds (files took 1.234 seconds to load)\n")
	fmt.Fprintf(&out, "%d examples, 0 failures\n", count)
	return out.String()
}

func wssDotnetTestAllPassFixture(count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, "  Passed Test%03d [1 ms]\n", i)
	}
	fmt.Fprintf(&out, "Passed!  - Failed: 0, Passed: %d, Skipped: 0, Total: %d, Duration: 1 s - Tests.dll (net8.0)\n", count, count)
	return out.String()
}

func wssDotnetBuildSuccessFixture(projects, warnings int) string {
	var out strings.Builder
	out.WriteString("Microsoft (R) Build Engine version 17.8.0\n")
	out.WriteString("  Determining projects to restore...\n")
	for i := 0; i < projects; i++ {
		fmt.Fprintf(&out, "  Project%03d -> /repo/bin/Debug/net8.0/Project%03d.dll\n", i, i)
	}
	out.WriteString("\nBuild succeeded.\n")
	if warnings > 0 {
		fmt.Fprintf(&out, "    %d Warning(s)\n", warnings)
	} else {
		out.WriteString("    0 Warning(s)\n")
	}
	out.WriteString("    0 Error(s)\n\n")
	out.WriteString("Time Elapsed 00:00:03.21\n")
	return out.String()
}

func wssListingFixture(files int) string {
	var out strings.Builder
	for i := 0; i < files; i++ {
		out.WriteString(fmt.Sprintf("internal/proxy/generated_listing_%03d.go\n", i))
	}
	return out.String()
}

func wssRgFilesRootListingFixture(files int) string {
	var out strings.Builder
	for _, path := range []string{"README.md", "AGENTS.md", "go.mod", "SECURITY.md"} {
		out.WriteString(path)
		out.WriteByte('\n')
	}
	for i := 0; i < files; i++ {
		out.WriteString(fmt.Sprintf("internal/proxy/generated/deep/path/file_%03d.go\n", i))
	}
	return out.String()
}

func wssTreeFixture(files int) string {
	var out strings.Builder
	out.WriteString("internal/proxy\n")
	for i := 0; i < files; i++ {
		out.WriteString(fmt.Sprintf("|-- tree_file_%03d.go\n", i))
	}
	out.WriteString(fmt.Sprintf("\n1 directory, %d files\n", files))
	return out.String()
}

func wssWcFixture(files int) (string, string) {
	var command strings.Builder
	var output strings.Builder
	command.WriteString("wc -l")
	total := 0
	for i := 0; i < files; i++ {
		path := fmt.Sprintf(" internal/proxy/generated/very/deep/path/file_%02d.go", i)
		count := i + 100
		total += count
		command.WriteString(path)
		output.WriteString(fmt.Sprintf("%8d%s\n", count, path))
	}
	output.WriteString(fmt.Sprintf("%8d total\n", total))
	return command.String(), output.String()
}

func wssGitLogOnelineFixture(commits int) string {
	var out strings.Builder
	for i := 0; i < commits; i++ {
		fmt.Fprintf(&out, "%07x commit subject %03d\n", i+1, i)
	}
	return out.String()
}
