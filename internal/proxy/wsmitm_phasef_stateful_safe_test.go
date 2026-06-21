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
	for i := range 40 {
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
	for i := range 20 {
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
	cargoNextestAllPass := wssCargoNextestAllPassFixture(80)
	cargoNextestFailure := strings.Replace(cargoNextestAllPass, "0 skipped", "1 failed, 0 skipped", 1)
	cargoClippyClean := wssCargoClippyCleanFixture(80)
	cargoClippyWarning := cargoClippyClean + "warning: generated binding is deprecated\n"
	cargoBuildClean := wssCargoBuildCleanProgressFixture("Compiling", 80)
	cargoCheckClean := wssCargoBuildCleanProgressFixture("Checking", 80)
	cargoDocClean := wssCargoBuildCleanProgressFixture("Documenting", 80)
	cargoBuildWarning := cargoBuildClean + "warning: generated binding is deprecated\n"
	ctestAllPass := wssCtestAllPassFixture(80)
	ctestFailure := strings.Replace(ctestAllPass, "0 tests failed", "1 tests failed", 1) + "The following tests FAILED:\n"
	ginkgoAllPass := wssGinkgoAllPassFixture(80)
	pytestAllPass := wssPytestVerboseAllPassFixture(80)
	pytestWrapperFailure := "tests/test_a.py::test_x FAILED\n=== 1 failed in 0.1s ===\n"
	pytestJSONAllPass := wssPytestJSONAllPassFixture(80)
	pythonUnittestAllPass := wssPythonUnittestAllPassFixture(200)
	pythonUnittestFailure := strings.Replace(pythonUnittestAllPass, "OK\n", "FAILED (failures=1)\n", 1)
	jestAllPass := wssJestVerboseAllPassFixture(70)
	jestFailure := "FAIL src/a.test.ts\n  x broken (3 ms)\nTests: 1 failed, 1 total\n"
	vitestJSONAllPass := wssVitestJSONAllPassFixture(70)
	eslintJSONClean := wssEslintJSONCleanFixture(70)
	mochaAllPass := wssMochaAllPassFixture(70)
	avaAllPass := wssAvaAllPassFixture(70)
	tapAllPass := wssTapAllPassFixture(70)
	playwrightAllPass := wssPlaywrightAllPassFixture(70)
	wdioAllPass := wssWdioRunAllPassFixture(70, 2)
	cypressAllPass := wssCypressRunAllPassFixture(70)
	nxTestAllPass := wssNxTestAllPassFixture(70)
	turboTestAllPass := wssTurboTestAllPassFixture(70, 2)
	bunAllPass := wssBunTestAllPassFixture(70)
	cargoJSONAllPass := wssCargoTestJSONAllPassFixture(70)
	sarifZeroResults := wssSARIFZeroResultsFixture(70)
	railsAllPass := wssRailsTestAllPassFixture(70)
	rspecAllPass := wssRspecAllPassFixture(70)
	rspecFailure := "....F\n\nFailures:\n\n  1) Widget renders failure details\n     Failure/Error: expect(result).to eq(:ok)\n\n     # ./spec/widget_spec.rb:42:in `block (2 levels) in <top (required)>'\n\nFinished in 0.05432 seconds\n5 examples, 1 failure\n"
	ginkgoFailure := "Will run 2 of 2 specs\n•F\nRan 2 of 2 Specs in 0.123 seconds\nFAIL! -- 1 Passed | 1 Failed | 0 Pending | 0 Skipped\n"
	vitestJSONFailure := `{"numTotalTestSuites":1,"numPassedTestSuites":0,"numFailedTestSuites":1,"numTotalTests":1,"numPassedTests":0,"numFailedTests":1,"testResults":[{"name":"src/widget.test.ts","status":"failed","assertionResults":[{"status":"failed","fullName":"widget fails","failureMessages":["expected true to be false"]}]}]}`
	eslintJSONWarning := `[{"filePath":"src/widget.ts","messages":[{"ruleId":"no-console","severity":1,"message":"Unexpected console statement.","line":7,"column":3}],"errorCount":0,"warningCount":1}]`
	sarifWarning := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"clippy"}},"results":[{"ruleId":"W1","level":"warning","message":{"text":"lint warning"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"src/lib.rs"},"region":{"startLine":7}}}]}]}]}`
	mochaFailure := "  widget suite\n    1) renders failure details\n\n  0 passing (10ms)\n  1 failing\n"
	avaFailure := "  ✖ renders failure details\n\n  1 test failed\n"
	tapFailure := "TAP version 13\nnot ok 1 - renders failure details\n1..1\n# tests 1\n# pass 0\n# fail 1\n"
	playwrightFailure := "Running 1 test\n  ✘  1 [chromium] › spec.ts:1:1 › renders failure details\n\n  1 failed\n"
	wdioFailure := strings.Replace(wssWdioRunAllPassFixture(4, 2), "Spec Files:      2 passed, 2 total", "Spec Files:      1 passed, 1 failed, 2 total", 1)
	cypressFailure := strings.Replace(wssCypressRunAllPassFixture(3), "All specs passed!", "1 spec failed!", 1)
	nxTestFailure := strings.Replace(wssNxTestAllPassFixture(3), "Tests: 3 passed, 3 total", "Tests: 2 passed, 1 failed, 3 total", 1)
	turboTestFailure := strings.Replace(wssTurboTestAllPassFixture(4, 2), "Tasks:    2 successful, 2 total", "Tasks:    1 successful, 2 total", 1)
	bunFailure := "bun test v1.3.14\n\nwidget.test.ts:\n(fail) renders failure details [1.00ms]\n\n 0 pass\n 1 fail\nRan 1 tests across 1 files. [1.00ms]\n"
	dotnetAllPass := wssDotnetTestAllPassFixture(60)
	dotnetBuildSuccess := wssDotnetBuildSuccessFixture(24, 0)
	dotnetBuildWarning := wssDotnetBuildSuccessFixture(24, 1)
	dotnetWarning := "Passed!  - Failed: 0, Passed: 60, Skipped: 0, Total: 60, Duration: 1 s - Tests.dll (net8.0)\nWarning: diagnostics were emitted\n"
	mypySuccess := wssMypySuccessFixture(12)
	mypyFailureWithNotice := "Skipping analyzing 'requests': module is installed, but missing library stubs\nsrc/app.py:11: error: Incompatible return value type\nsrc/app.py:11: note: expected str\nFound 1 error in 1 file (checked 48 source files)\n"
	pyrightJSONSuccess := wssPyrightJSONSuccessFixture(24)
	pyrightJSONWarning := strings.Replace(pyrightJSONSuccess, `"warningCount": 0`, `"warningCount": 1`, 1)
	logDuplicateRuns := wssLogDuplicateRunsFixture(24)
	logUnique := "2026-06-18T10:00:00Z INFO service started\n2026-06-18T10:00:01Z WARN connection refused\n"
	logSourceLike := "app.go:10: failed to bind\napp.go:10: failed to bind\n"
	terraformValidateSuccess := strings.Join([]string{
		"Terraform used the selected providers to generate the following execution plan.",
		"Acquiring state lock. This may take a few moments...",
		"Success! The configuration is valid.",
		"The configuration is valid.",
	}, "\n") + "\n"
	terraformValidateFailure := "╷\n│ Error: Missing required argument\n│\n│   on main.tf line 12, in resource \"aws_s3_bucket\" \"bad\":\n│   12: resource \"aws_s3_bucket\" \"bad\" {}\n╵\n"
	emptyBuildEnvelope := "Chunk ID: build-empty\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10\nOutput:\n"
	goBuildWarning := "# github.com/slim/example\n# Compiled successfully\nwarning: generated binding is deprecated\nBuild succeeded with 0 errors and 1 warning.\n"
	npmInstallClean := wssNpmInstallCleanFixture(70)
	npmInstallWarning := "npm warn deprecated left-pad@1.3.0: use String.prototype.padStart()\n" + npmInstallClean
	npmInstallVulnerability := strings.Replace(npmInstallClean, "found 0 vulnerabilities", "3 vulnerabilities (1 moderate, 2 high)", 1)
	pnpmInstallClean := wssPnpmInstallCleanFixture(70)
	pnpmInstallWarning := " WARN  deprecated left-pad@1.3.0\n" + pnpmInstallClean
	yarnInstallClean := wssYarnClassicInstallCleanFixture()
	yarnInstallWarning := strings.Replace(yarnInstallClean, "success Saved lockfile.", "warning Ignored scripts due to flag.\nsuccess Saved lockfile.", 1)
	pipInstallClean := wssPipInstallCleanFixture(70)
	poetryInstallClean := wssPoetryInstallCleanFixture(70)
	poetryInstallWarning := "Warning: lock file is not consistent\n" + poetryInstallClean
	uvSyncClean := wssUvSyncCleanFixture(70)
	uvSyncError := "Resolved 70 packages in 10ms\nerror: No solution found when resolving dependencies\n"
	uvPipInstallClean := wssUvPipInstallCleanFixture(70)

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
		{name: "cargo nextest all-pass", command: "cargo nextest run", output: cargoNextestAllPass, wantSafe: true},
		{name: "cargo clippy clean progress", command: "cargo clippy --all-targets --all-features", output: cargoClippyClean, wantSafe: true},
		{name: "cargo build clean progress", command: "cargo build --workspace", output: cargoBuildClean, wantSafe: true},
		{name: "cargo check clean progress", command: "cargo check --all-targets", output: cargoCheckClean, wantSafe: true},
		{name: "cargo doc clean progress", command: "cargo doc --no-deps", output: cargoDocClean, wantSafe: true},
		{name: "ctest all-pass", command: "ctest --output-on-failure", output: ctestAllPass, wantSafe: true},
		{name: "ginkgo all-pass", command: "ginkgo", output: ginkgoAllPass, wantSafe: true},
		{name: "pytest verbose all-pass", command: "pytest -v", output: pytestAllPass, wantSafe: true},
		{name: "uv run pytest verbose all-pass", command: "uv run pytest -v", output: pytestAllPass, wantSafe: true},
		{name: "poetry run pytest verbose all-pass", command: "poetry run pytest -v", output: pytestAllPass, wantSafe: true},
		{name: "hatch test pytest all-pass", command: "hatch test", output: pytestAllPass, wantSafe: true},
		{name: "nox test pytest all-pass", command: "nox -s test", output: pytestAllPass, wantSafe: true},
		{name: "pytest json all-pass", command: "pytest --json-report --json-report-file=-", output: pytestJSONAllPass, wantSafe: true},
		{name: "python unittest all-pass", command: "python3 -m unittest discover -v", output: pythonUnittestAllPass, wantSafe: true},
		{name: "jest verbose all-pass", command: "jest", output: jestAllPass, wantSafe: true},
		{name: "vitest json all-pass", command: "vitest run --reporter=json", output: vitestJSONAllPass, wantSafe: true},
		{name: "eslint json clean", command: "eslint --format json src", output: eslintJSONClean, wantSafe: true},
		{name: "sarif zero results", command: "clippy --format sarif", output: sarifZeroResults, wantSafe: true},
		{name: "sarif zero results explicit flag", command: "semgrep --sarif", output: sarifZeroResults, wantSafe: true},
		{name: "sarif zero results reporter equals", command: "some-checker --reporter=sarif", output: sarifZeroResults, wantSafe: true},
		{name: "sarif zero results short equals", command: "some-checker -f=sarif", output: sarifZeroResults, wantSafe: true},
		{name: "sarif zero results short joined", command: "some-checker -fsarif", output: sarifZeroResults, wantSafe: true},
		{name: "mocha verbose all-pass", command: "mocha", output: mochaAllPass, wantSafe: true},
		{name: "ava verbose all-pass", command: "ava", output: avaAllPass, wantSafe: true},
		{name: "tap verbose all-pass", command: "tap", output: tapAllPass, wantSafe: true},
		{name: "playwright verbose all-pass", command: "playwright test", output: playwrightAllPass, wantSafe: true},
		{name: "npm test jest all-pass", command: "npm test", output: jestAllPass, wantSafe: true},
		{name: "pnpm run test playwright all-pass", command: "pnpm run test", output: playwrightAllPass, wantSafe: true},
		{name: "yarn test mocha all-pass", command: "yarn test", output: mochaAllPass, wantSafe: true},
		{name: "wdio run all-pass", command: "wdio run wdio.conf.ts", output: wdioAllPass, wantSafe: true},
		{name: "cypress run all-pass", command: "cypress run --headless", output: cypressAllPass, wantSafe: true},
		{name: "nx test all-pass", command: "nx test web", output: nxTestAllPass, wantSafe: true},
		{name: "turbo test all-pass", command: "turbo run test", output: turboTestAllPass, wantSafe: true},
		{name: "bun verbose all-pass", command: "bun test", output: bunAllPass, wantSafe: true},
		{name: "cargo test json all-pass", command: "cargo test -- --format json", output: cargoJSONAllPass, wantSafe: true},
		{name: "rails all-pass", command: "bundle exec rails test", output: railsAllPass, wantSafe: true},
		{name: "rspec all-pass", command: "bundle exec rspec", output: rspecAllPass, wantSafe: true},
		{name: "dotnet test all-pass", command: "dotnet test", output: dotnetAllPass, wantSafe: true},
		{name: "dotnet build success no warnings", command: "dotnet build", output: dotnetBuildSuccess, wantSafe: true},
		{name: "mypy success summary", command: "mypy src", output: mypySuccess, wantSafe: true},
		{name: "pyright JSON clean", command: "pyright --outputjson src", output: pyrightJSONSuccess, wantSafe: true},
		{name: "docker logs duplicate runs", command: "docker logs app", output: logDuplicateRuns, wantSafe: true},
		{name: "terraform validate success summary", command: "terraform validate", output: terraformValidateSuccess, wantSafe: true},
		{name: "npm install clean success", command: "npm install", output: npmInstallClean, wantSafe: true},
		{name: "pnpm install clean success", command: "pnpm install", output: pnpmInstallClean, wantSafe: true},
		{name: "yarn install clean success", command: "yarn install", output: yarnInstallClean, wantSafe: true},
		{name: "pip install clean success", command: "pip install -r requirements.txt", output: pipInstallClean, wantSafe: true},
		{name: "poetry install clean success", command: "poetry install", output: poetryInstallClean, wantSafe: true},
		{name: "uv sync clean success", command: "uv sync", output: uvSyncClean, wantSafe: true},
		{name: "uv pip install clean success", command: "uv pip install requests", output: uvPipInstallClean, wantSafe: true},
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
		{name: "cargo nextest failure", command: "cargo nextest run", output: cargoNextestFailure, wantGuard: "cargo nextest failures stay guarded"},
		{name: "cargo clippy warning", command: "cargo clippy --all-targets", output: cargoClippyWarning, wantGuard: "cargo clippy warnings stay guarded"},
		{name: "cargo build warning", command: "cargo build --workspace", output: cargoBuildWarning, wantGuard: "cargo build warnings stay guarded"},
		{name: "ctest failure", command: "ctest --output-on-failure", output: ctestFailure, wantGuard: "ctest failures stay guarded"},
		{name: "ginkgo failure", command: "ginkgo", output: ginkgoFailure, wantGuard: "ginkgo failures stay guarded"},
		{name: "vitest json failure", command: "vitest run --reporter=json", output: vitestJSONFailure, wantGuard: "vitest JSON failures stay guarded"},
		{name: "eslint json warning", command: "eslint --format json src", output: eslintJSONWarning, wantGuard: "eslint JSON findings stay guarded"},
		{name: "uv run pytest failure", command: "uv run pytest -v", output: pytestWrapperFailure, wantGuard: "uv run pytest failures stay guarded"},
		{name: "poetry run pytest failure", command: "poetry run pytest -v", output: pytestWrapperFailure, wantGuard: "poetry run pytest failures stay guarded"},
		{name: "hatch test failure", command: "hatch test", output: pytestWrapperFailure, wantGuard: "hatch test failures stay guarded"},
		{name: "nox test failure", command: "nox -s test", output: pytestWrapperFailure, wantGuard: "nox test failures stay guarded"},
		{name: "python unittest failure", command: "python3 -m unittest discover -v", output: pythonUnittestFailure, wantGuard: "python unittest failures stay guarded"},
		{name: "sarif warning", command: "clippy --format sarif", output: sarifWarning, wantGuard: "SARIF findings stay guarded"},
		{name: "sarif cat zero results", command: "cat report.sarif", output: sarifZeroResults, wantGuard: "SARIF zero-results cat output stays guarded"},
		{name: "sarif pattern file flag", command: "grep -f sarif.patterns report.sarif", output: sarifZeroResults, wantGuard: "SARIF-looking pattern-file args stay guarded"},
		{name: "pytest failure", command: "pytest -v", output: "tests/test_a.py::test_x FAILED\n=== 1 failed in 0.1s ===\n", wantGuard: "pytest failures stay guarded"},
		{name: "jest failure", command: "jest", output: jestFailure, wantGuard: "jest failures stay guarded"},
		{name: "mocha failure", command: "mocha", output: mochaFailure, wantGuard: "mocha failures stay guarded"},
		{name: "ava failure", command: "ava", output: avaFailure, wantGuard: "ava failures stay guarded"},
		{name: "tap failure", command: "tap", output: tapFailure, wantGuard: "tap failures stay guarded"},
		{name: "playwright failure", command: "playwright test", output: playwrightFailure, wantGuard: "playwright failures stay guarded"},
		{name: "npm test failure", command: "npm test", output: jestFailure, wantGuard: "package-manager test failures stay guarded"},
		{name: "wdio failure", command: "wdio run wdio.conf.ts", output: wdioFailure, wantGuard: "wdio failures stay guarded"},
		{name: "cypress failure", command: "cypress run", output: cypressFailure, wantGuard: "cypress failures stay guarded"},
		{name: "nx test failure", command: "nx test web", output: nxTestFailure, wantGuard: "nx test failures stay guarded"},
		{name: "turbo test failure", command: "turbo run test", output: turboTestFailure, wantGuard: "turbo test failures stay guarded"},
		{name: "bun failure", command: "bun test", output: bunFailure, wantGuard: "bun failures stay guarded"},
		{name: "rspec failure", command: "bundle exec rspec", output: rspecFailure, wantGuard: "rspec failures stay guarded"},
		{name: "dotnet test warning", command: "dotnet test", output: dotnetWarning, wantGuard: "dotnet warnings stay guarded"},
		{name: "dotnet build warning", command: "dotnet build", output: dotnetBuildWarning, wantGuard: "dotnet build warnings stay guarded"},
		{name: "mypy failure with stub notice", command: "mypy src", output: mypyFailureWithNotice, wantGuard: "mypy diagnostics with extra notices stay guarded"},
		{name: "pyright JSON warning", command: "pyright --outputjson src", output: pyrightJSONWarning, wantGuard: "pyright JSON findings stay guarded"},
		{name: "docker logs unique", command: "docker logs app", output: logUnique, wantGuard: "unique logs stay guarded"},
		{name: "docker logs source-looking duplicate", command: "docker logs app", output: logSourceLike, wantGuard: "source-looking logs stay guarded"},
		{name: "terraform validate failure", command: "terraform validate", output: terraformValidateFailure, wantGuard: "terraform validate diagnostics stay guarded"},
		{name: "npm install warning", command: "npm install", output: npmInstallWarning, wantGuard: "package install warnings stay guarded"},
		{name: "npm install vulnerability", command: "npm install", output: npmInstallVulnerability, wantGuard: "package install vulnerability findings stay guarded"},
		{name: "pnpm install warning", command: "pnpm install", output: pnpmInstallWarning, wantGuard: "pnpm install warnings stay guarded"},
		{name: "yarn install warning", command: "yarn install --ignore-scripts", output: yarnInstallWarning, wantGuard: "yarn install warnings stay guarded"},
		{name: "poetry install warning", command: "poetry install", output: poetryInstallWarning, wantGuard: "poetry install warnings stay guarded"},
		{name: "uv sync error", command: "uv sync", output: uvSyncError, wantGuard: "uv sync errors stay guarded"},
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

func TestWSSStatefulSafeCypressRunAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: cypress-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssCypressRunAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-cypress-all-pass", "call_cypress_all_pass", "cypress run --headless", envelope, "stateful-cypress-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle cypress all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history cypress all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[cypress run] ok - 120 tests passed across 120 specs") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "generated_119.cy.ts") {
		t.Fatalf("cypress all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe cypress all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeWdioRunAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: wdio-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssWdioRunAllPassFixture(120, 3)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-wdio-all-pass", "call_wdio_all_pass", "wdio run wdio.conf.ts", envelope, "stateful-wdio-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle wdio all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history wdio all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[wdio run] ok - 120 test(s) passed across 3 spec file(s)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "be able to render op 119") {
		t.Fatalf("wdio all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe wdio all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeNxTestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: nx-test-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssNxTestAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-nx-test-all-pass", "call_nx_test_all_pass", "nx test web", envelope, "stateful-nx-test-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle nx test all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history nx test all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[nx test] ok - 120 passed") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "renders op 119") {
		t.Fatalf("nx test all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe nx test all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeTurboTestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: turbo-test-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssTurboTestAllPassFixture(120, 3)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-turbo-test-all-pass", "call_turbo_test_all_pass", "turbo run test", envelope, "stateful-turbo-test-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle turbo test all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history turbo test all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[turbo test] ok - 120 passed across 3 successful task(s)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "renders op 119") {
		t.Fatalf("turbo test all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe turbo test all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafePackageManagerTestScriptAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: package-manager-test-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssJestVerboseAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-manager-test-all-pass", "call_package_manager_test_all_pass", "npm test", envelope, "stateful-package-manager-test-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle package-manager test all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history package-manager test all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[jest] ok - 120 passed") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "renders op 119") {
		t.Fatalf("package-manager test all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe package-manager test should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafePackageManagerTestScriptTranscriptAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: package-manager-test-transcript-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		"> web@1.0.0 test /repo\n> jest --runInBand\n" + wssJestVerboseAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-manager-test-transcript-all-pass", "call_package_manager_test_transcript_all_pass", "pnpm test", envelope, "stateful-package-manager-test-transcript-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle package-manager test transcript request: %v", err)
	}
	if !replace {
		t.Fatal("full-history package-manager test transcript all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[jest] ok - 120 passed") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "renders op 119") ||
		strings.Contains(body, "web@1.0.0 test") {
		t.Fatalf("package-manager test transcript all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe package-manager test transcript should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafePackageInstallCleanSuccessCompactsFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		output    string
		want      string
		forbidden string
	}{
		{name: "npm", command: "npm install", output: wssNpmInstallCleanFixture(160), want: "[npm install] added 160 packages", forbidden: "package_159"},
		{name: "pnpm", command: "pnpm install", output: wssPnpmInstallCleanFixture(160), want: "[pnpm install] ok (added 160 packages)", forbidden: "slimference-pnpm-package-159"},
		{name: "yarn", command: "yarn install", output: wssYarnClassicInstallCleanFixture(), want: "[yarn install] ok (lockfile saved)", forbidden: "Fetching packages"},
		{name: "poetry", command: "poetry install", output: wssPoetryInstallCleanFixture(160), want: "[poetry install] ok (160 installs, 0 updates, 0 removals", forbidden: "package-159"},
		{name: "uv sync", command: "uv sync", output: wssUvSyncCleanFixture(160), want: "[uv sync] ok (resolved 160 packages", forbidden: "uv-package-159"},
		{name: "uv pip install", command: "uv pip install requests", output: wssUvPipInstallCleanFixture(160), want: "[uv pip install] ok (resolved 160 packages", forbidden: "uv-package-159"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
			envelope := "Chunk ID: package-install-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
				tt.output

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-package-install-clean", "call_package_install_clean", tt.command, envelope, "stateful-package-install-safe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle package install clean success request: %v", err)
			}
			if !replace {
				t.Fatal("full-history package install clean success output should compact")
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, tt.forbidden) {
				t.Fatalf("package install clean success output was not archive-backed compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe package install clean success should save without structured guard: %+v", summary)
			}
		})
	}
}

func TestWSSCompactedPackageSuccessSummaryContract(t *testing.T) {
	t.Parallel()
	cleanOriginal := []byte("fetch package metadata\nadded 3 packages, and audited 4 packages in 1s\nfound 0 vulnerabilities\n")
	cleanSummary := []byte("[npm install] added 3 packages, and audited 4 packages in 1s\n")
	if !wssCompactedPackageSuccessSummary(cleanOriginal, cleanSummary) {
		t.Fatal("clean npm install summary should be accepted")
	}
	if !wssCompactedPackageSuccessSummary([]byte("Collecting a\nSuccessfully installed a-1.0.0\n"), []byte("[pip install] Successfully installed a-1.0.0\n")) {
		t.Fatal("clean pip install summary should be accepted")
	}
	if !wssCompactedPackageSuccessSummary([]byte("resolving packages\nDone in 3.5s.\n"), []byte("[yarn install] Done in 3.5s.\n")) {
		t.Fatal("clean yarn install summary should be accepted")
	}

	rejects := []struct {
		name      string
		original  []byte
		compacted []byte
	}{
		{name: "not bracketed", original: cleanOriginal, compacted: []byte("npm install ok\n")},
		{name: "empty label", original: cleanOriginal, compacted: []byte("[] added 3 packages\n")},
		{name: "unsupported label", original: cleanOriginal, compacted: []byte("[cargo build] added 3 packages\n")},
		{name: "empty status", original: cleanOriginal, compacted: []byte("[npm install]\n")},
		{name: "error status", original: cleanOriginal, compacted: []byte("[npm install] error: failed\n")},
		{name: "warning original", original: []byte("npm warn deprecated x\nadded 3 packages\n"), compacted: cleanSummary},
		{name: "vulnerability original", original: []byte("added 3 packages\n3 vulnerabilities\n"), compacted: cleanSummary},
		{name: "resolver original", original: []byte("ERR_PNPM_NO_MATCHING_VERSION missing\n"), compacted: []byte("[pnpm install] added 3 packages\n")},
	}
	for _, tt := range rejects {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if wssCompactedPackageSuccessSummary(tt.original, tt.compacted) {
				t.Fatalf("unsafe package summary accepted: original=%q compacted=%q", tt.original, tt.compacted)
			}
		})
	}
}

func TestWSSStatefulSafeTier1JSONAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: vitest-json-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssVitestJSONAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-vitest-json-all-pass", "call_vitest_json_all_pass", "vitest run --reporter=json", envelope, "stateful-vitest-json-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle vitest JSON all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history vitest JSON all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[vitest --reporter=json] 120 tests passed") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "renders op 119") {
		t.Fatalf("vitest JSON all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe vitest JSON all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeEslintJSONCleanCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: eslint-json-clean\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssEslintJSONCleanFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-eslint-json-clean", "call_eslint_json_clean", "eslint --format json src", envelope, "stateful-eslint-json-clean-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle eslint JSON clean request: %v", err)
	}
	if !replace {
		t.Fatal("full-history eslint JSON clean output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[eslint] clean (120 file(s))") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "src/generated/widget_119.ts") {
		t.Fatalf("eslint JSON clean output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe eslint JSON clean should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeSARIFZeroResultsCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: sarif-zero-results\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssSARIFZeroResultsFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-sarif-zero-results", "call_sarif_zero_results", "clippy --format sarif", envelope, "stateful-sarif-zero-results-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle SARIF zero-results request: %v", err)
	}
	if !replace {
		t.Fatal("full-history SARIF zero-results output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[sarif: clippy] 0 results") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "slimference.generated.Rule119") {
		t.Fatalf("SARIF zero-results output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe SARIF zero-results should save without structured guard: %+v", summary)
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

func TestWSSStatefulSafeRailsTestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: rails-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssRailsTestAllPassFixture(5000)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-rails-all-pass", "call_rails_all_pass", "bundle exec rails test", envelope, "stateful-rails-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle rails all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history rails all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[rails test] ok - 5000 runs, 10000 assertions") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, strings.Repeat(".", 40)) {
		t.Fatalf("rails all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe rails all-pass should save without structured guard: %+v", summary)
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

func TestWSSStatefulSafePyrightJSONSuccessCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: pyright-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssPyrightJSONSuccessFixture(80)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-pyright-json-success", "call_pyright_json_success", "pyright --outputjson src", envelope, "stateful-pyright-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle pyright JSON success request: %v", err)
	}
	if !replace {
		t.Fatal("full-history pyright JSON success output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[pyright --outputjson] ok (188 files analyzed)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "generalDiagnostics") {
		t.Fatalf("pyright JSON success output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe pyright JSON success should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeCargoClippyCleanCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: cargo-clippy-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssCargoClippyCleanFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-cargo-clippy-clean", "call_cargo_clippy_clean", "cargo clippy --all-targets --all-features", envelope, "stateful-cargo-clippy-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle cargo clippy clean request: %v", err)
	}
	if !replace {
		t.Fatal("full-history cargo clippy clean output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[cargo clippy] ok") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "slimtest_119") {
		t.Fatalf("cargo clippy clean output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe cargo clippy clean should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeCargoBuildCleanCompactsFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name    string
		command string
		output  string
		want    string
	}{
		{name: "build", command: "cargo build --workspace", output: wssCargoBuildCleanProgressFixture("Compiling", 120), want: "[cargo build] ok"},
		{name: "check", command: "cargo check --all-targets", output: wssCargoBuildCleanProgressFixture("Checking", 120), want: "[cargo check] ok"},
		{name: "doc", command: "cargo doc --no-deps", output: wssCargoBuildCleanProgressFixture("Documenting", 120), want: "[cargo doc] ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
			envelope := "Chunk ID: cargo-build-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
				tt.output

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-cargo-build-clean", "call_cargo_build_clean", tt.command, envelope, "stateful-cargo-build-safe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle cargo build clean request: %v", err)
			}
			if !replace {
				t.Fatal("full-history cargo clean output should compact")
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, "slimtest_119") {
				t.Fatalf("cargo clean output was not archive-backed compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe cargo clean should save without structured guard: %+v", summary)
			}
		})
	}
}

func TestWSSStatefulSafeLogDuplicateRunsCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: logs-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssLogDuplicateRunsFixture(80)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-log-duplicate-runs", "call_log_duplicate_runs", "docker logs app", envelope, "stateful-log-duplicate-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle log duplicate-runs request: %v", err)
	}
	if !replace {
		t.Fatal("full-history log duplicate-runs output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "INFO worker heartbeat 079 [×2]") ||
		!strings.Contains(body, "ERROR upstream timeout 079 [×3]") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "INFO worker heartbeat 079\nINFO worker heartbeat 079") {
		t.Fatalf("log duplicate-runs output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe log duplicate-runs should save without structured guard: %+v", summary)
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
	if !replace {
		t.Fatal("first parser-proven tree should compact immediately")
	}
	firstBody := string(first.Body)
	if !strings.Contains(firstBody, "[tree paths]") ||
		!strings.Contains(firstBody, "local-archive://") ||
		!strings.Contains(firstBody, "tree_file_089.go") ||
		strings.Contains(firstBody, "|-- tree_file_089.go") {
		t.Fatalf("first tree was not parser-preserved compacted: %s", firstBody)
	}
	firstSummary := p.DebugRecorder().Last(1, false)[0]
	if firstSummary.Tokens.Saved <= 0 || firstSummary.DebugFacts["wss.structured_mutation_guard"] != "" {
		t.Fatalf("stateful-safe tree parser should save without structured guard: %+v", firstSummary)
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
	if !strings.Contains(body, "local-archive://") ||
		strings.Contains(body, "|-- tree_file_089.go") {
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
	for i := range 90 {
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

func TestWSSStatefulSafeCargoNextestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: cargo-nextest-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssCargoNextestAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-cargo-nextest-all-pass", "call_cargo_nextest_all_pass", "cargo nextest run", envelope, "stateful-cargo-nextest-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle cargo nextest all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history cargo nextest all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[cargo nextest run] ok - 120 passed") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "slimference::test_119") {
		t.Fatalf("cargo nextest all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe cargo nextest all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeCtestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: ctest-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssCtestAllPassFixture(120)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-ctest-all-pass", "call_ctest_all_pass", "ctest --output-on-failure", envelope, "stateful-ctest-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle ctest all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history ctest all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[ctest] ok (100% tests passed, 0 tests failed out of 120)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "generated_119") {
		t.Fatalf("ctest all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe ctest all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafePythonUnittestAllPassCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: python-unittest-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssPythonUnittestAllPassFixture(5000)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-python-unittest-all-pass", "call_python_unittest_all_pass", "python3 -m unittest discover -v", envelope, "stateful-python-unittest-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle python unittest all-pass request: %v", err)
	}
	if !replace {
		t.Fatal("full-history python unittest all-pass output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[python -m unittest] ok (Ran 5000 tests in 0.321s; OK)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, strings.Repeat(".", 40)) {
		t.Fatalf("python unittest all-pass output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe python unittest all-pass should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafePytestWrappersAllPassCompactFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{name: "uv", command: "uv run pytest -v", want: "[uv run pytest] ok - 120 passed"},
		{name: "poetry", command: "poetry run pytest -v", want: "[poetry run pytest] ok - 120 passed"},
		{name: "hatch", command: "hatch test", want: "[hatch test] ok - 120 passed"},
		{name: "nox", command: "nox -s test", want: "[nox test] ok - 120 passed"},
		{name: "tox", command: "tox -e py311", want: "[tox test] ok - 120 passed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
			envelope := "Chunk ID: pytest-wrapper-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
				wssPytestVerboseAllPassFixture(120)

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-pytest-wrapper-all-pass", "call_pytest_wrapper_all_pass", tt.command, envelope, "stateful-pytest-wrapper-safe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle pytest wrapper all-pass request: %v", err)
			}
			if !replace {
				t.Fatal("full-history pytest wrapper all-pass output should compact")
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, "test_op_119") {
				t.Fatalf("pytest wrapper all-pass output was not archive-backed compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe pytest wrapper all-pass should save without structured guard: %+v", summary)
			}
		})
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

func TestWSSStatefulSafeGuardPredicateBoundaries(t *testing.T) {
	t.Parallel()

	gitStatusCases := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "short untracked pathspec", command: "git status --short --untracked-files all -- internal/proxy", want: true},
		{name: "git dir ignored porcelain", command: "git -C /repo status --ignored=matching --porcelain=v1", want: true},
		{name: "invalid untracked value", command: "git status --untracked-files weird", want: false},
		{name: "rich long flag", command: "git status --long", want: false},
		{name: "not git", command: "status --short", want: false},
	}
	for _, tt := range gitStatusCases {
		t.Run("git_status/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssSafeGitStatusCommand(tt.command); got != tt.want {
				t.Fatalf("wssSafeGitStatusCommand(%q)=%v want %v", tt.command, got, tt.want)
			}
		})
	}

	gitLogCases := []struct {
		name    string
		command string
		payload string
		want    bool
	}{
		{name: "dash n bounded", command: "git log --oneline -n 3", payload: "a1b2c3d Tighten guards\nb2c3d4e Recover savings\nc3d4e5f Add proof\n", want: true},
		{name: "max count equals with pathspec", command: "git -C /repo log --oneline --max-count=2 -- internal/proxy", payload: "a1b2c3d Tighten guards\nb2c3d4e Recover savings\n", want: true},
		{name: "compact numeric flag", command: "git log --oneline -3", payload: "a1b2c3d Tighten guards\n", want: true},
		{name: "unbounded", command: "git log --oneline", payload: "a1b2c3d Tighten guards\n", want: false},
		{name: "too many lines", command: "git log --oneline -n 1", payload: "a1b2c3d Tighten guards\nb2c3d4e Recover savings\n", want: false},
		{name: "rich stat flag", command: "git log --stat --oneline -n 1", payload: "a1b2c3d Tighten guards\n", want: false},
		{name: "missing max count value", command: "git log --oneline -n", payload: "a1b2c3d Tighten guards\n", want: false},
		{name: "bad max count value", command: "git log --oneline --max-count=0", payload: "a1b2c3d Tighten guards\n", want: false},
		{name: "non hex hash", command: "git log --oneline -n 1", payload: "zzzzzzz Tighten guards\n", want: false},
		{name: "missing subject", command: "git log --oneline -n 1", payload: "a1b2c3d\n", want: false},
		{name: "search shaped payload", command: "git log --oneline -n 1", payload: "internal/proxy/a.go:10:needle\n", want: false},
	}
	for _, tt := range gitLogCases {
		t.Run("git_log/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssSafeGitLogOnelineOutput(tt.command, tt.payload); got != tt.want {
				t.Fatalf("wssSafeGitLogOnelineOutput(%q, %q)=%v want %v", tt.command, tt.payload, got, tt.want)
			}
		})
	}

	gitDiffStatCases := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "plain stat", command: "git diff --stat", want: true},
		{name: "staged relative diff filter", command: "git -C /repo diff --staged --stat --relative=internal --diff-filter=AM", want: true},
		{name: "split diff filter", command: "git diff --stat --diff-filter AM -- internal", want: true},
		{name: "missing stat", command: "git diff --cached -- internal", want: false},
		{name: "unknown flag", command: "git diff --stat --word-diff", want: false},
		{name: "missing split diff filter", command: "git diff --stat --diff-filter", want: false},
	}
	for _, tt := range gitDiffStatCases {
		t.Run("git_diff_stat/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssSafeGitDiffStatCommand(tt.command); got != tt.want {
				t.Fatalf("wssSafeGitDiffStatCommand(%q)=%v want %v", tt.command, got, tt.want)
			}
		})
	}

	gitShowStatCases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "stat commit", args: []string{"--stat", "--no-ext-diff", "HEAD"}, want: true},
		{name: "stat pathspec", args: []string{"--stat=80", "--diff-filter", "AM", "--", "internal/proxy"}, want: true},
		{name: "missing stat", args: []string{"HEAD"}, want: false},
		{name: "empty pathspec", args: []string{"--stat", "--", ""}, want: false},
		{name: "missing diff filter value", args: []string{"--stat", "--diff-filter"}, want: false},
		{name: "patch flag", args: []string{"--stat", "--patch"}, want: false},
	}
	for _, tt := range gitShowStatCases {
		t.Run("git_show_stat_args/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssSafeGitShowStatArgs(tt.args); got != tt.want {
				t.Fatalf("wssSafeGitShowStatArgs(%q)=%v want %v", tt.args, got, tt.want)
			}
		})
	}

	lsCases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "short safe flags", args: []string{"-1aF", "internal/proxy"}, want: true},
		{name: "long safe flags", args: []string{"--almost-all", "--directory", "--indicator-style=slash", "--", "internal"}, want: true},
		{name: "empty arg", args: []string{""}, want: false},
		{name: "unknown long flag", args: []string{"--recursive"}, want: false},
		{name: "unknown short flag", args: []string{"-lh"}, want: false},
	}
	for _, tt := range lsCases {
		t.Run("ls/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssSafeLsArgs(tt.args); got != tt.want {
				t.Fatalf("wssSafeLsArgs(%q)=%v want %v", tt.args, got, tt.want)
			}
		})
	}

	findCases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bounded path list", args: []string{"internal/proxy", "-maxdepth", "2", "-type", "f", "-name", "*.go", "-print"}, want: true},
		{name: "bounded with mindepth zero", args: []string{".", "-mindepth", "0", "-maxdepth", "1", "(", "-type", "f", "-o", "-type", "d", ")", "-print"}, want: true},
		{name: "missing maxdepth", args: []string{"internal/proxy", "-type", "f", "-print"}, want: false},
		{name: "exec side effect", args: []string{"internal", "-maxdepth", "2", "-exec", "cat", "{}", ";"}, want: false},
		{name: "missing name value", args: []string{"internal", "-maxdepth", "2", "-name"}, want: false},
		{name: "too deep", args: []string{"internal", "-maxdepth", fmt.Sprintf("%d", wssSafeFindMaxDepth+1), "-print"}, want: false},
		{name: "unknown flag", args: []string{"internal", "-maxdepth", "2", "-perm", "0644"}, want: false},
		{name: "empty arg", args: []string{"internal", "-maxdepth", "2", ""}, want: false},
	}
	for _, tt := range findCases {
		t.Run("find/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssSafeFindArgs(tt.args); got != tt.want {
				t.Fatalf("wssSafeFindArgs(%q)=%v want %v", tt.args, got, tt.want)
			}
		})
	}

	treeCases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bounded with charset and separator", args: []string{"-L", "2", "--dirsfirst", "--charset", "ascii", "--", "internal/proxy"}, want: true},
		{name: "joined depth and charset", args: []string{"-adF", "-L2", "--charset=ascii", "internal/proxy"}, want: true},
		{name: "split depth", args: []string{"-d", "-L", "1", "internal/proxy"}, want: true},
		{name: "missing depth", args: []string{"--dirsfirst", "internal/proxy"}, want: false},
		{name: "empty charset value", args: []string{"-L", "2", "--charset", ""}, want: false},
		{name: "rich disk usage flag", args: []string{"-L", "2", "--du", "internal/proxy"}, want: false},
		{name: "too deep", args: []string{fmt.Sprintf("-L%d", wssSafeTreeMaxDepth+1), "internal/proxy"}, want: false},
		{name: "empty separator rest", args: []string{"-L", "2", "--", ""}, want: false},
	}
	for _, tt := range treeCases {
		t.Run("tree/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wssSafeTreeArgs(tt.args); got != tt.want {
				t.Fatalf("wssSafeTreeArgs(%q)=%v want %v", tt.args, got, tt.want)
			}
		})
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

func TestWSPhaseFReconnectPreviousResponseFullHistoryDiffStatCompactsAndDetaches(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(2)

	env := parseWSJSON(t, map[string]any{
		"type": string(wsmitm.FrameKindRequest),
		"body": map[string]any{
			"model":                "gpt-5-codex",
			"previous_response_id": "resp-diffstat-safe-reconnect",
			"prompt_cache_key":     "stateful-diffstat-safe-reconnect-session",
			"input": []map[string]any{
				{"type": "message", "role": "user", "content": "summarize the reconnect diff stat"},
				{"type": "function_call", "call_id": "call_diffstat_reconnect", "name": "exec_command", "arguments": map[string]any{"cmd": "git diff --stat"}},
				{"type": "function_call_output", "call_id": "call_diffstat_reconnect", "output": wssDiffStatFixture(80)},
			},
			"stream": true,
		},
	})

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle reconnect diffstat request: %v", err)
	}
	if !replace {
		t.Fatalf("reconnect previous_response full-history diffstat should compact")
	}
	body := string(env.Body)
	if strings.Contains(body, "previous_response_id") {
		t.Fatalf("reconnect stateful-safe full-history mutation must detach previous_response_id: %s", body)
	}
	if !strings.Contains(body, "[git diff --stat] 80 file(s)") ||
		!strings.Contains(body, "[prefix=internal/proxy/generated/very/deep/path/]") ||
		strings.Contains(body, "internal/proxy/generated/very/deep/path/file_xxxxxxxxxxxx_79.go") {
		t.Fatalf("reconnect diffstat compaction did not preserve compact evidence: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 ||
		summary.DebugFacts["wss.request_shape"] != "full_history" ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.effective_mutation_guard"] != "" ||
		summary.DebugFacts["wss.full_history_stateless_followup"] != "true" ||
		summary.DebugFacts["wss.full_history_detached_previous_response"] != "true" {
		t.Fatalf("reconnect stateful-safe diffstat should save as detached stateless full-history: %+v", summary)
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
	for i := range files {
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
	for i := range noiseLines {
		fmt.Fprintf(&out, "Using Python executable for module %03d: /tmp/slimference-venv/bin/python\n", i)
	}
	out.WriteString("Success: no issues found in 188 source files\n")
	return out.String()
}

func wssPyrightJSONSuccessFixture(paddingLines int) string {
	var out strings.Builder
	out.WriteString("{\n")
	for range paddingLines {
		out.WriteString("  \n")
	}
	out.WriteString(`  "version": "1.1.400",
  "time": "2026-06-18T12:00:00.000Z",
  "generalDiagnostics": [],
  "summary": {
    "filesAnalyzed": 188,
    "errorCount": 0,
    "warningCount": 0,
    "informationCount": 0,
    "timeInSec": 1.23
  }
}
`)
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
	for i := range count {
		fmt.Fprintf(&out, "=== RUN   TestPassing%03d\n--- PASS: TestPassing%03d (0.00s)\n", i, i)
	}
	out.WriteString("PASS\nok  \tslimtest/lib\t0.006s\n")
	return out.String()
}

func wssGoTestVerboseFailureFixture(count int) string {
	var out strings.Builder
	for i := range count {
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
	for i := range count {
		fmt.Fprintf(&out, "test alpha::op_%03d ... ok\n", i)
	}
	fmt.Fprintf(&out, "\ntest result: ok. %d passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n", count)
	return out.String()
}

func wssCargoNextestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("    Finished `test` profile [unoptimized + debuginfo] target(s) in 0.01s\n")
	out.WriteString("------------\n")
	fmt.Fprintf(&out, "    Starting %d tests across 1 binary\n", count)
	for i := range count {
		fmt.Fprintf(&out, "        PASS [   0.%03ds] slimference::test_%03d\n", i%100, i)
	}
	out.WriteString("------------\n")
	fmt.Fprintf(&out, "     Summary [   0.088s] %d tests run: %d passed, 0 skipped\n", count, count)
	return out.String()
}

func wssCtestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("Test project /repo/build\n")
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&out, "      Start %3d: generated_%03d\n", i, i)
		fmt.Fprintf(&out, "%3d/%d Test #%3d: generated_%03d ....................   Passed    0.01 sec\n", i, count, i, i)
	}
	fmt.Fprintf(&out, "100%% tests passed, 0 tests failed out of %d\n", count)
	return out.String()
}

func wssCargoClippyCleanFixture(packages int) string {
	var out strings.Builder
	for i := range packages {
		fmt.Fprintf(&out, "    Checking slimtest_%03d v0.1.0 (/repo/crates/slimtest_%03d)\n", i, i)
	}
	out.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	return out.String()
}

func wssCargoBuildCleanProgressFixture(verb string, packages int) string {
	var out strings.Builder
	for i := range packages {
		fmt.Fprintf(&out, "    %s slimtest_%03d v0.1.0 (/repo/crates/slimtest_%03d)\n", verb, i, i)
	}
	out.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	return out.String()
}

func wssCargoTestJSONAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString(`{"type":"suite","event":"started"}`)
	out.WriteByte('\n')
	for i := range count {
		fmt.Fprintf(&out, `{"type":"test","event":"ok","name":"alpha::op_%03d"}`+"\n", i)
	}
	fmt.Fprintf(&out, `{"type":"suite","event":"ok","passed":%d,"failed":0}`+"\n", count)
	return out.String()
}

func wssGinkgoAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("Running Suite: Slimference Suite - internal/proxy\n")
	out.WriteString("==========================================================\n")
	out.WriteString("Random Seed: 1634748172\n\n")
	fmt.Fprintf(&out, "Will run %d of %d specs\n", count, count)
	out.WriteString(strings.Repeat("•", count))
	out.WriteString("\n\n")
	fmt.Fprintf(&out, "Ran %d of %d Specs in 0.123 seconds\n", count, count)
	fmt.Fprintf(&out, "SUCCESS! -- %d Passed | 0 Failed | 0 Pending | 0 Skipped\n", count)
	out.WriteString("PASS\n")
	return out.String()
}

func wssPytestVerboseAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("============================= test session starts ==============================\n")
	for i := range count {
		fmt.Fprintf(&out, "tests/test_alpha.py::test_op_%03d PASSED                                  [ %2d%%]\n", i, i)
	}
	fmt.Fprintf(&out, "============================== %d passed in 0.42s ===============================\n", count)
	return out.String()
}

func wssPythonUnittestAllPassFixture(count int) string {
	var out strings.Builder
	for i := range count {
		out.WriteByte('.')
		if (i+1)%80 == 0 {
			out.WriteByte('\n')
		}
	}
	out.WriteString("\n----------------------------------------------------------------------\n")
	fmt.Fprintf(&out, "Ran %d tests in 0.321s\n\nOK\n", count)
	return out.String()
}

func wssPytestJSONAllPassFixture(count int) string {
	return fmt.Sprintf(`{"summary":{"passed":%d,"failed":0,"error":0,"skipped":0,"total":%d,"duration":0.42},"tests":[]}`+"\n", count, count)
}

func wssJestVerboseAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("PASS src/alpha.test.ts\n")
	for i := range count {
		fmt.Fprintf(&out, "  \u2713 renders op %03d (2 ms)\n", i)
	}
	fmt.Fprintf(&out, "\nTests: %d passed, %d total\nTime: 1.2 s\n", count, count)
	return out.String()
}

func wssVitestJSONAllPassFixture(count int) string {
	var out strings.Builder
	fmt.Fprintf(&out, `{"numTotalTestSuites":1,"numPassedTestSuites":1,"numFailedTestSuites":0,"numTotalTests":%d,"numPassedTests":%d,"numFailedTests":0,"testResults":[{"name":"src/widget.test.ts","status":"passed","assertionResults":[`, count, count)
	for i := range count {
		if i > 0 {
			out.WriteByte(',')
		}
		fmt.Fprintf(&out, `{"status":"passed","fullName":"widget suite > renders op %03d","title":"renders op %03d"}`, i, i)
	}
	out.WriteString("]}]}\n")
	return out.String()
}

func wssEslintJSONCleanFixture(files int) string {
	var out strings.Builder
	out.WriteByte('[')
	for i := range files {
		if i > 0 {
			out.WriteByte(',')
		}
		fmt.Fprintf(&out, `{"filePath":"src/generated/widget_%03d.ts","messages":[],"errorCount":0,"warningCount":0}`, i)
	}
	out.WriteString("]\n")
	return out.String()
}

func wssSARIFZeroResultsFixture(ruleCount int) string {
	var out strings.Builder
	out.WriteString(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"clippy","rules":[`)
	for i := range ruleCount {
		if i > 0 {
			out.WriteByte(',')
		}
		fmt.Fprintf(&out, `{"id":"slimference.generated.Rule%03d","name":"Generated rule %03d","shortDescription":{"text":"Rule catalog entry %03d"}}`, i, i, i)
	}
	out.WriteString(`]}},"results":[]}]}`)
	out.WriteByte('\n')
	return out.String()
}

func wssMochaAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("  widget suite\n")
	for i := range count {
		fmt.Fprintf(&out, "    ✔ renders op %03d (2ms)\n", i)
	}
	fmt.Fprintf(&out, "\n  %d passing (95ms)\n", count)
	return out.String()
}

func wssAvaAllPassFixture(count int) string {
	var out strings.Builder
	for i := range count {
		fmt.Fprintf(&out, "  ✔ renders op %03d\n", i)
	}
	fmt.Fprintf(&out, "\n  %d tests passed\n", count)
	return out.String()
}

func wssTapAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("TAP version 13\n")
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&out, "ok %d - renders op %03d\n", i, i)
	}
	fmt.Fprintf(&out, "1..%d\n# tests %d\n# pass %d\n# fail 0\n", count, count, count)
	return out.String()
}

func wssPlaywrightAllPassFixture(count int) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Running %d tests using 4 workers\n", count)
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&out, "  ✓  %d [chromium] › tests/e2e/spec_%03d.spec.ts:5:1 › renders op %03d (120ms)\n", i, i, i)
	}
	fmt.Fprintf(&out, "\n  %d passed (12.3s)\n", count)
	return out.String()
}

func wssCypressRunAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("====================================================================================================\n")
	out.WriteString("  (Run Finished)\n\n")
	out.WriteString("       Spec                                              Tests  Passing  Failing  Pending  Skipped\n")
	out.WriteString("  ┌────────────────────────────────────────────────────────────────────────────────────────────────┐\n")
	for i := range count {
		fmt.Fprintf(&out, "  │ ✔  cypress/e2e/generated_%03d.cy.ts              00:01        1        1        -        -        - │\n", i)
	}
	out.WriteString("  └────────────────────────────────────────────────────────────────────────────────────────────────┘\n")
	fmt.Fprintf(&out, "    ✔  All specs passed!                              00:12        %d        %d        -        -        -\n", count, count)
	return out.String()
}

func wssWdioRunAllPassFixture(count int, specs int) string {
	if specs <= 0 {
		specs = 1
	}
	perSpec := count / specs
	var out strings.Builder
	out.WriteString("Execution of 2 workers started at 2026-06-18T12:00:00.000Z\n\n")
	written := 0
	for spec := 0; spec < specs; spec++ {
		specCount := perSpec
		if spec == specs-1 {
			specCount = count - written
		}
		fmt.Fprintf(&out, "[0-%d] RUNNING in chrome - file:///test/specs/generated_%03d.e2e.ts\n", spec, spec)
		fmt.Fprintf(&out, "[0-%d] PASSED in chrome - file:///test/specs/generated_%03d.e2e.ts\n\n", spec, spec)
		out.WriteString(" \"spec\" Reporter:\n")
		out.WriteString("------------------------------------------------------------------\n")
		fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d] Running: chrome on mac\n", spec)
		fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d] Session ID: session-%03d\n", spec, spec)
		fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d]\n", spec)
		fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d] \u00bb /test/specs/generated_%03d.e2e.ts\n", spec, spec)
		fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d] Generated suite %03d\n", spec, spec)
		for i := 0; i < specCount; i++ {
			fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d]    ✓ be able to render op %03d\n", spec, written+i)
		}
		fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d]\n", spec)
		fmt.Fprintf(&out, "[chrome 125.0 mac #0-%d] %d passing (5.9s)\n\n", spec, specCount)
		written += specCount
	}
	fmt.Fprintf(&out, "Spec Files:      %d passed, %d total (100%% completed) in 00:00:08\n", specs, specs)
	return out.String()
}

func wssNxTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("> nx run web:test\n\n")
	out.WriteString("PASS apps/web/src/app/app.spec.ts\n")
	for i := range count {
		fmt.Fprintf(&out, "  ✓ renders op %03d (2 ms)\n", i)
	}
	out.WriteString("\n")
	out.WriteString("Test Suites: 1 passed, 1 total\n")
	fmt.Fprintf(&out, "Tests: %d passed, %d total\n", count, count)
	out.WriteString("Snapshots: 0 total\n")
	out.WriteString("Time: 1.2 s\n")
	out.WriteString("Ran all test suites.\n\n")
	out.WriteString(" >  NX   Successfully ran target test for project web\n")
	return out.String()
}

func wssTurboTestAllPassFixture(count int, tasks int) string {
	if tasks <= 0 {
		tasks = 1
	}
	perTask := count / tasks
	var out strings.Builder
	out.WriteString("turbo 2.5.4\n")
	out.WriteString("• Packages in scope: @repo/web, @repo/ui\n")
	fmt.Fprintf(&out, "• Running test in %d packages\n", tasks)
	out.WriteString("• Remote caching disabled\n\n")
	written := 0
	for task := 0; task < tasks; task++ {
		taskName := fmt.Sprintf("@repo/pkg%d:test", task)
		taskCount := perTask
		if task == tasks-1 {
			taskCount = count - written
		}
		fmt.Fprintf(&out, "%s: cache miss, executing abcdef%d\n", taskName, task)
		fmt.Fprintf(&out, "%s:\n", taskName)
		fmt.Fprintf(&out, "%s: PASS packages/pkg%d/src/app.spec.ts\n", taskName, task)
		for i := 0; i < taskCount; i++ {
			fmt.Fprintf(&out, "%s:   ✓ renders op %03d (2 ms)\n", taskName, written+i)
		}
		fmt.Fprintf(&out, "%s:\n", taskName)
		fmt.Fprintf(&out, "%s: Test Suites: 1 passed, 1 total\n", taskName)
		fmt.Fprintf(&out, "%s: Tests: %d passed, %d total\n", taskName, taskCount, taskCount)
		fmt.Fprintf(&out, "%s: Time: 1.2 s\n\n", taskName)
		written += taskCount
	}
	fmt.Fprintf(&out, "Tasks:    %d successful, %d total\n", tasks, tasks)
	fmt.Fprintf(&out, "Cached:   0 cached, %d total\n", tasks)
	out.WriteString("Time:     1.234s\n")
	return out.String()
}

func wssBunTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("bun test v1.3.14 (0d9b296a)\n\nwidget.test.ts:\n")
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&out, "(pass) widget suite > renders op %03d [0.%02dms]\n", i, i%100)
	}
	fmt.Fprintf(&out, "\n %d pass\n 0 fail\n 140 expect() calls\nRan %d tests across 2 files. [3.01s]\n", count, count)
	return out.String()
}

func wssRailsTestAllPassFixture(count int) string {
	var out strings.Builder
	out.WriteString("Run options: --seed 12345\n\n# Running:\n\n")
	out.WriteString(strings.Repeat(".", count))
	out.WriteString("\n\nFinished in 1.234567s, 97.2000 runs/s, 194.4000 assertions/s.\n")
	fmt.Fprintf(&out, "%d runs, %d assertions, 0 failures, 0 errors, 0 skips\n", count, count*2)
	return out.String()
}

func wssRspecAllPassFixture(count int) string {
	var out strings.Builder
	for i := range count {
		fmt.Fprintf(&out, "spec/models/widget_spec.rb:%03d: example_%03d passed\n", i+1, i)
	}
	out.WriteString("\nFinished in 0.12345 seconds (files took 1.234 seconds to load)\n")
	fmt.Fprintf(&out, "%d examples, 0 failures\n", count)
	return out.String()
}

func wssDotnetTestAllPassFixture(count int) string {
	var out strings.Builder
	for i := range count {
		fmt.Fprintf(&out, "  Passed Test%03d [1 ms]\n", i)
	}
	fmt.Fprintf(&out, "Passed!  - Failed: 0, Passed: %d, Skipped: 0, Total: %d, Duration: 1 s - Tests.dll (net8.0)\n", count, count)
	return out.String()
}

func wssDotnetBuildSuccessFixture(projects, warnings int) string {
	var out strings.Builder
	out.WriteString("Microsoft (R) Build Engine version 17.8.0\n")
	out.WriteString("  Determining projects to restore...\n")
	for i := range projects {
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

func wssNpmInstallCleanFixture(packages int) string {
	var out strings.Builder
	for i := range packages {
		fmt.Fprintf(&out, "npm http fetch GET 200 https://registry.npmjs.org/package_%03d 12%dms\n", i, i%10)
		fmt.Fprintf(&out, "npm timing idealTree:node_modules/package_%03d Completed in %dms\n", i, i%20+1)
	}
	fmt.Fprintf(&out, "\nadded %d packages, and audited %d packages in 12s\n\n", packages, packages+1)
	out.WriteString("12 packages are looking for funding\n")
	out.WriteString("  run `npm fund` for details\n\n")
	out.WriteString("found 0 vulnerabilities\n")
	return out.String()
}

func wssPnpmInstallCleanFixture(packages int) string {
	var out strings.Builder
	out.WriteString("Progress: resolved 1, reused 0, downloaded 0, added 0\n")
	fmt.Fprintf(&out, "Packages: +%d\n", packages)
	out.WriteString(strings.Repeat("+", packages))
	out.WriteString("\n")
	fmt.Fprintf(&out, "Progress: resolved %d, reused %d, downloaded 0, added %d, done\n\n", packages, packages, packages)
	out.WriteString("dependencies:\n")
	for i := range packages {
		fmt.Fprintf(&out, "+ slimference-pnpm-package-%03d 1.0.%d\n", i, i)
	}
	out.WriteString("\nDone in 256ms using pnpm v10.13.1\n")
	return out.String()
}

func wssYarnClassicInstallCleanFixture() string {
	return strings.Join([]string{
		"yarn install v1.22.22",
		"info No lockfile found.",
		"[1/4] Resolving packages...",
		"[2/4] Fetching packages...",
		"[3/4] Linking dependencies...",
		"[4/4] Building fresh packages...",
		"success Saved lockfile.",
		"Done in 0.04s.",
		"",
	}, "\n")
}

func wssPoetryInstallCleanFixture(packages int) string {
	var out strings.Builder
	out.WriteString("Installing dependencies from lock file\n\n")
	fmt.Fprintf(&out, "Package operations: %d %s, 0 updates, 0 removals\n\n", packages, wssPluralWord(packages, "install", "installs"))
	for i := range packages {
		fmt.Fprintf(&out, "  - Installing package-%03d (1.0.%d)\n", i, i)
	}
	out.WriteString("\nWriting lock file\n")
	return out.String()
}

func wssUvSyncCleanFixture(packages int) string {
	return wssUvPackageCleanFixture(packages, true)
}

func wssUvPipInstallCleanFixture(packages int) string {
	return wssUvPackageCleanFixture(packages, false)
}

func wssUvPackageCleanFixture(packages int, audit bool) string {
	var out strings.Builder
	out.WriteString("Using CPython 3.12.4 interpreter at: /usr/bin/python3\n")
	fmt.Fprintf(&out, "Resolved %d %s in 23ms\n", packages, wssPluralWord(packages, "package", "packages"))
	fmt.Fprintf(&out, "Prepared %d %s in 42ms\n", packages, wssPluralWord(packages, "package", "packages"))
	fmt.Fprintf(&out, "Installed %d %s in 5ms\n", packages, wssPluralWord(packages, "package", "packages"))
	for i := range packages {
		fmt.Fprintf(&out, " + uv-package-%03d==1.0.%d\n", i, i)
	}
	if audit {
		fmt.Fprintf(&out, "Audited %d %s in 1ms\n", packages, wssPluralWord(packages, "package", "packages"))
	}
	return out.String()
}

func wssPluralWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func wssPipInstallCleanFixture(packages int) string {
	var out strings.Builder
	installed := make([]string, 0, packages)
	for i := range packages {
		name := fmt.Sprintf("package-%03d", i)
		installed = append(installed, name+"-1.0.0")
		fmt.Fprintf(&out, "Collecting %s\n", name)
		fmt.Fprintf(&out, "  Downloading %s-1.0.0-py3-none-any.whl (62 kB)\n", name)
		out.WriteString("     ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 62.6/62.6 kB 1.2 MB/s eta 0:00:00\n")
	}
	fmt.Fprintf(&out, "Successfully installed %s\n", strings.Join(installed, " "))
	return out.String()
}

func wssLogDuplicateRunsFixture(entries int) string {
	var out strings.Builder
	for i := range entries {
		fmt.Fprintf(&out, "2026-06-18T10:%02d:00Z INFO worker heartbeat %03d\n", i%60, i)
		fmt.Fprintf(&out, "2026-06-18T10:%02d:00Z INFO worker heartbeat %03d\n", i%60, i)
		fmt.Fprintf(&out, "2026-06-18T10:%02d:01Z ERROR upstream timeout %03d\n", i%60, i)
		fmt.Fprintf(&out, "2026-06-18T10:%02d:01Z ERROR upstream timeout %03d\n", i%60, i)
		fmt.Fprintf(&out, "2026-06-18T10:%02d:01Z ERROR upstream timeout %03d\n", i%60, i)
	}
	return out.String()
}

func wssListingFixture(files int) string {
	var out strings.Builder
	for i := range files {
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
	for i := range files {
		out.WriteString(fmt.Sprintf("internal/proxy/generated/deep/path/file_%03d.go\n", i))
	}
	return out.String()
}

func wssTreeFixture(files int) string {
	var out strings.Builder
	out.WriteString("internal/proxy\n")
	for i := range files {
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
	for i := range files {
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
	for i := range commits {
		fmt.Fprintf(&out, "%07x commit subject %03d\n", i+1, i)
	}
	return out.String()
}
