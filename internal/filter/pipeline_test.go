package filter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunPipeline_StripANSI(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	// echo -e might be bash; use printf for portability
	pr := RunPipeline(context.Background(), t.TempDir(), []string{"sh", "-c", `printf '\x1b[31mplain\x1b[0m\n'`}, 0)
	if pr.Err != nil {
		t.Fatal(pr.Err)
	}
	if pr.Code != 0 {
		t.Fatalf("code %d", pr.Code)
	}
	if strings.Contains(string(pr.Stdout), "\x1b") {
		t.Fatalf("ANSI not stripped: %q", pr.Stdout)
	}
	if !strings.Contains(string(pr.Stdout), "plain") {
		t.Fatalf("unexpected: %q", pr.Stdout)
	}
	if pr.InputTokens < pr.OutputTokens {
		t.Fatalf("in=%d out=%d", pr.InputTokens, pr.OutputTokens)
	}
}

func TestApplyLayer0AfterANSI_builtinSkipsTOML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".slimference"), 0755); err != nil {
		t.Fatal(err)
	}
	toml := `
[filters.ruin]
match_command = "^git\\s+status"
replace = [{ pattern = ".*", replacement = "TOML_RAN" }]
`
	if err := os.WriteFile(filepath.Join(dir, ".slimference", "filters.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}
	argv := []string{"git", "status"}
	porcelain := []byte(" M a.go\n?? b.txt\n")
	out := applyLayer0AfterANSI(dir, argv, porcelain)
	if strings.Contains(string(out), "TOML_RAN") {
		t.Fatalf("TOML must not run when builtin handles: %q", out)
	}
	if !strings.Contains(string(out), "[git status]") {
		t.Fatalf("expected builtin summary: %q", out)
	}
}

func TestApplyLayer0AfterANSI_gitDiffEmptySkipsTOML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".slimference"), 0755); err != nil {
		t.Fatal(err)
	}
	toml := `
[filters.x]
match_command = "^git\\s+diff"
replace = [{ pattern = ".*", replacement = "TOML_RAN" }]
`
	if err := os.WriteFile(filepath.Join(dir, ".slimference", "filters.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}
	out := applyLayer0AfterANSI(dir, []string{"git", "diff"}, []byte(""))
	if strings.Contains(string(out), "TOML_RAN") {
		t.Fatalf("TOML must not run: %q", out)
	}
	if string(out) != "[git diff] empty\n" {
		t.Fatalf("got %q", out)
	}
}

func TestApplyLayer0AfterANSI_tomlWhenBuiltinNoMatch(t *testing.T) {
	t.Setenv("SLIMFERENCE_TRUST_PROJECT_FILTERS", "1")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".slimference"), 0755); err != nil {
		t.Fatal(err)
	}
	toml := `
[filters.x]
match_command = "^git\\s+status"
replace = [{ pattern = "branch", replacement = "BR" }]
`
	if err := os.WriteFile(filepath.Join(dir, ".slimference", "filters.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}
	argv := []string{"git", "status"}
	// Human-readable status: builtin does not compact (non-porcelain path)
	human := []byte("On branch main\nnothing to commit\n")
	out := applyLayer0AfterANSI(dir, argv, human)
	if !strings.Contains(string(out), "On BR main") {
		t.Fatalf("TOML should apply: %q", out)
	}
}

// TestApplyLayer0AfterANSI_allFilters exercises every branch of applyLayer0AfterANSI by
// providing argv+stdout that routes through each TryCompact* function in order.
func TestApplyLayer0AfterANSI_allFilters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cases := []struct {
		name         string
		argv         []string
		stdout       []byte
		wantContains string
		wantAbsent   string // optional: must NOT appear
	}{
		// git filters (first 5 in chain)
		{
			name:         "git log empty",
			argv:         []string{"git", "log", "--oneline"},
			stdout:       []byte(""),
			wantContains: "[git log] empty",
		},
		{
			name:         "git show empty",
			argv:         []string{"git", "show", "HEAD"},
			stdout:       []byte(""),
			wantContains: "[git show] empty",
		},
		{
			name:         "git push ok",
			argv:         []string{"git", "push", "origin", "main"},
			stdout:       []byte(""),
			wantContains: "[git push] ok",
		},
		// build filters (TryCompactBuildOutput)
		{
			name:         "go build ok",
			argv:         []string{"go", "build", "./..."},
			stdout:       []byte(""),
			wantContains: "[go build] ok",
		},
		// test filters (TryCompactTestOutput)
		{
			name:         "go test ok",
			argv:         []string{"go", "test", "./..."},
			stdout:       []byte(""),
			wantContains: "[go test] ok",
		},
		// dotnet
		{
			name:         "dotnet build ok",
			argv:         []string{"dotnet", "build"},
			stdout:       []byte(""),
			wantContains: "[dotnet build] ok",
		},
		// ruby (rake)
		{
			name:         "rake ok",
			argv:         []string{"rake", "test"},
			stdout:       []byte(""),
			wantContains: "[rake] ok",
		},
		// search (rg)
		{
			name:         "rg no matches",
			argv:         []string{"rg", "somepattern", "."},
			stdout:       []byte(""),
			wantContains: "[rg] no matches",
		},
		// filesystem
		{
			name:         "ls empty",
			argv:         []string{"ls", "-la"},
			stdout:       []byte(""),
			wantContains: "[ls] empty",
		},
		{
			name:         "tree empty",
			argv:         []string{"tree"},
			stdout:       []byte(""),
			wantContains: "[tree] empty",
		},
		// file reads full-pass; repeat/range savings live in readcache, not in
		// lossy first-read filtering.
		{
			name:         "cat go file full pass",
			argv:         []string{"cat", "main.go"},
			stdout:       []byte("package main\n\n// This entire comment line gets stripped\nfunc main() {}\n"),
			wantContains: "func main",
			wantAbsent:   "",
		},
		// lint (cargo clippy)
		{
			name:         "cargo clippy ok",
			argv:         []string{"cargo", "clippy", "--all-targets"},
			stdout:       []byte(""),
			wantContains: "[cargo clippy] ok",
		},
		// format filters
		{
			name:         "prettier ok",
			argv:         []string{"prettier", "--write", "file.js"},
			stdout:       []byte(""),
			wantContains: "[prettier] ok",
		},
		{
			name:         "biome format ok",
			argv:         []string{"biome", "format", "--write", "file.ts"},
			stdout:       []byte(""),
			wantContains: "[biome format] ok",
		},
		{
			name:         "buf format ok",
			argv:         []string{"buf", "format", "-w"},
			stdout:       []byte(""),
			wantContains: "[buf format] ok",
		},
		{
			name:         "terraform fmt ok",
			argv:         []string{"terraform", "fmt", "-recursive"},
			stdout:       []byte(""),
			wantContains: "[terraform fmt] ok",
		},
		{
			name:         "black ok",
			argv:         []string{"black", "src/"},
			stdout:       []byte(""),
			wantContains: "[black] ok",
		},
		{
			name:         "ruff format ok",
			argv:         []string{"ruff", "format", "src/"},
			stdout:       []byte(""),
			wantContains: "[ruff format] ok",
		},
		{
			name:         "taplo format ok",
			argv:         []string{"taplo", "format"},
			stdout:       []byte(""),
			wantContains: "[taplo format] ok",
		},
		{
			name:         "shfmt ok",
			argv:         []string{"shfmt", "-w", "script.sh"},
			stdout:       []byte(""),
			wantContains: "[shfmt] ok",
		},
		{
			name:         "sqlfmt ok",
			argv:         []string{"sqlfmt", "query.sql"},
			stdout:       []byte(""),
			wantContains: "[sqlfmt] ok",
		},
		{
			name:         "isort ok",
			argv:         []string{"isort", "src/"},
			stdout:       []byte(""),
			wantContains: "[isort] ok",
		},
		{
			name:         "autopep8 ok",
			argv:         []string{"autopep8", "-i", "file.py"},
			stdout:       []byte(""),
			wantContains: "[autopep8] ok",
		},
		{
			name:         "gofmt ok",
			argv:         []string{"gofmt", "-w", "main.go"},
			stdout:       []byte(""),
			wantContains: "[gofmt] ok",
		},
		{
			name:         "rustfmt ok",
			argv:         []string{"rustfmt", "src/main.rs"},
			stdout:       []byte(""),
			wantContains: "[rustfmt] ok",
		},
		{
			name:         "clang-format ok",
			argv:         []string{"clang-format", "-i", "main.cpp"},
			stdout:       []byte(""),
			wantContains: "[clang-format] ok",
		},
		{
			name:         "zig fmt ok",
			argv:         []string{"zig", "fmt", "src/main.zig"},
			stdout:       []byte(""),
			wantContains: "[zig fmt] ok",
		},
		{
			name:         "dprint fmt ok",
			argv:         []string{"dprint", "fmt"},
			stdout:       []byte(""),
			wantContains: "[dprint fmt] ok",
		},
		// psql
		{
			name:         "psql ok",
			argv:         []string{"psql", "-d", "mydb", "-c", "SELECT 1"},
			stdout:       []byte(""),
			wantContains: "[psql] ok",
		},
		// package management
		{
			name:         "npm install ok",
			argv:         []string{"npm", "install"},
			stdout:       []byte(""),
			wantContains: "[npm install] ok",
		},
		// container (docker ps -q)
		{
			name:         "docker ps quiet empty",
			argv:         []string{"docker", "ps", "-q"},
			stdout:       []byte(""),
			wantContains: "[docker ps] no containers",
		},
		// gh list
		{
			name:         "gh pr list empty",
			argv:         []string{"gh", "pr", "list"},
			stdout:       []byte(""),
			wantContains: "[gh pr list] empty",
		},
		// glab list
		{
			name:         "glab mr list empty",
			argv:         []string{"glab", "mr", "list"},
			stdout:       []byte(""),
			wantContains: "[glab mr list] empty",
		},
		// log dedup (docker logs with repeated lines)
		{
			name:         "docker logs dedup",
			argv:         []string{"docker", "logs", "mycontainer"},
			stdout:       []byte("INFO: heartbeat\nINFO: heartbeat\nINFO: heartbeat\n"),
			wantContains: "[×3]",
		},
		// aws JSON strip
		{
			name:         "aws JSON strip ResponseMetadata",
			argv:         []string{"aws", "s3", "ls"},
			stdout:       []byte(`{"ResponseMetadata":{"HTTPStatusCode":200,"RequestId":"abc-123","RetryAttempts":0},"Contents":[{"Key":"file.txt","Size":1024}]}`),
			wantContains: "Contents",
			wantAbsent:   "ResponseMetadata",
		},
		// JSON minify (argv doesn't match any builtin)
		{
			name:         "JSON minify passthrough",
			argv:         []string{"curl", "https://api.example.com/data"},
			stdout:       []byte("{\n  \"status\": \"ok\",\n  \"count\": 42\n}\n"),
			wantContains: `"status"`,
			wantAbsent:   "  ",
		},
		// raw passthrough (no filter matches)
		{
			name:         "unrecognized tool passthrough",
			argv:         []string{"my-custom-tool", "--flag"},
			stdout:       []byte("custom output line\n"),
			wantContains: "custom output line",
		},

		// -- additional build sub-chain cases --
		{
			name:         "cargo build ok",
			argv:         []string{"cargo", "build", "--release"},
			stdout:       []byte(""),
			wantContains: "[cargo build] ok",
		},
		{
			name:         "cargo check ok",
			argv:         []string{"cargo", "check", "--all-targets"},
			stdout:       []byte(""),
			wantContains: "[cargo check] ok",
		},
		{
			name:         "make ok",
			argv:         []string{"make", "all"},
			stdout:       []byte(""),
			wantContains: "[make] ok",
		},
		{
			name:         "tsc ok",
			argv:         []string{"tsc", "--noEmit"},
			stdout:       []byte(""),
			wantContains: "[tsc] ok",
		},
		{
			name:         "vite build ok",
			argv:         []string{"vite", "build"},
			stdout:       []byte(""),
			wantContains: "[vite build] ok",
		},
		{
			name:         "gradle build ok",
			argv:         []string{"gradle", "build"},
			stdout:       []byte(""),
			wantContains: "[gradle build] ok",
		},
		{
			name:         "mvn package ok",
			argv:         []string{"mvn", "package"},
			stdout:       []byte(""),
			wantContains: "[mvn] ok",
		},
		{
			name:         "swift build ok",
			argv:         []string{"swift", "build"},
			stdout:       []byte(""),
			wantContains: "[swift build] ok",
		},
		{
			name:         "npm run build ok",
			argv:         []string{"npm", "run", "build"},
			stdout:       []byte(""),
			wantContains: "[npm run build] ok",
		},

		// -- additional test sub-chain cases --
		{
			name:         "cargo test ok",
			argv:         []string{"cargo", "test", "--", "--nocapture"},
			stdout:       []byte(""),
			wantContains: "[cargo test] ok",
		},
		{
			name:         "pytest ok",
			argv:         []string{"pytest", "-v", "tests/"},
			stdout:       []byte(""),
			wantContains: "[pytest] ok",
		},
		{
			name:         "jest ok",
			argv:         []string{"jest", "--passWithNoTests"},
			stdout:       []byte(""),
			wantContains: "[jest] ok",
		},
		{
			name:         "vitest ok",
			argv:         []string{"vitest", "run"},
			stdout:       []byte(""),
			wantContains: "[vitest] ok",
		},
		{
			name:         "mocha ok",
			argv:         []string{"mocha", "test/**/*.spec.js"},
			stdout:       []byte(""),
			wantContains: "[mocha] ok",
		},
		{
			name:         "phpunit ok",
			argv:         []string{"phpunit", "--colors=always"},
			stdout:       []byte(""),
			wantContains: "[phpunit] ok",
		},

		// -- additional lint sub-chain cases --
		{
			name:         "cargo audit ok",
			argv:         []string{"cargo", "audit"},
			stdout:       []byte(""),
			wantContains: "[cargo audit] ok",
		},
		{
			name:         "golangci-lint ok",
			argv:         []string{"golangci-lint", "run", "./..."},
			stdout:       []byte(""),
			wantContains: "[golangci-lint] ok",
		},
		{
			name:         "staticcheck ok",
			argv:         []string{"staticcheck", "./..."},
			stdout:       []byte(""),
			wantContains: "[staticcheck] ok",
		},
		{
			name:         "buf lint ok",
			argv:         []string{"buf", "lint"},
			stdout:       []byte(""),
			wantContains: "[buf lint] ok",
		},
		{
			name:         "semgrep ok",
			argv:         []string{"semgrep", "--config=auto", "."},
			stdout:       []byte(""),
			wantContains: "[semgrep] ok",
		},

		// -- additional search sub-chain cases --
		{
			name:         "grep no matches",
			argv:         []string{"grep", "-r", "pattern", "."},
			stdout:       []byte(""),
			wantContains: "[grep] no matches",
		},
		{
			name:         "fd no matches",
			argv:         []string{"fd", "pattern"},
			stdout:       []byte(""),
			wantContains: "[fd] no matches",
		},
		{
			name:         "find no matches",
			argv:         []string{"find", ".", "-name", "*.go"},
			stdout:       []byte(""),
			wantContains: "[find] no matches",
		},

		// -- additional container sub-chain cases --
		{
			name:         "docker images quiet empty",
			argv:         []string{"docker", "images", "-q"},
			stdout:       []byte(""),
			wantContains: "[docker images] empty",
		},
		{
			name:         "helm list quiet empty",
			argv:         []string{"helm", "list", "-q"},
			stdout:       []byte(""),
			wantContains: "[helm list] empty",
		},
		{
			name:         "kubectl get empty",
			argv:         []string{"kubectl", "get", "pods"},
			stdout:       []byte(""),
			wantContains: "[kubectl get] empty",
		},

		// -- additional package sub-chain cases --
		{
			name:         "pip install ok",
			argv:         []string{"pip", "install", "-r", "requirements.txt"},
			stdout:       []byte(""),
			wantContains: "[pip install] ok",
		},
		{
			name:         "poetry install ok",
			argv:         []string{"poetry", "install"},
			stdout:       []byte(""),
			wantContains: "[poetry install] ok",
		},
		{
			name:         "bun install ok",
			argv:         []string{"bun", "install"},
			stdout:       []byte(""),
			wantContains: "[bun install] ok",
		},
		{
			name:         "yarn install ok",
			argv:         []string{"yarn", "install"},
			stdout:       []byte(""),
			wantContains: "[yarn install] ok",
		},

		// -- additional git sub-chain cases --
		{
			name:         "git commit ok",
			argv:         []string{"git", "commit", "-m", "msg"},
			stdout:       []byte(""),
			wantContains: "[git commit] ok",
		},
		{
			name:         "git pull already up to date",
			argv:         []string{"git", "pull"},
			stdout:       []byte("Already up to date.\n"),
			wantContains: "[git pull] up to date",
		},

		// -- kubectl logs dedup --
		{
			name:         "kubectl logs dedup",
			argv:         []string{"kubectl", "logs", "mypod"},
			stdout:       []byte("INFO starting\nINFO starting\n"),
			wantContains: "[×2]",
		},

		// -- remaining build sub-chain cases --
		{
			name:         "cargo doc ok",
			argv:         []string{"cargo", "doc", "--no-deps"},
			stdout:       []byte(""),
			wantContains: "[cargo doc] ok",
		},
		{
			name:         "ninja ok",
			argv:         []string{"ninja", "-C", "builddir"},
			stdout:       []byte(""),
			wantContains: "[ninja] ok",
		},
		{
			name:         "cmake build ok",
			argv:         []string{"cmake", "--build", ".", "--config", "Release"},
			stdout:       []byte(""),
			wantContains: "[cmake --build] ok",
		},
		{
			name:         "webpack ok",
			argv:         []string{"webpack", "--mode=production"},
			stdout:       []byte(""),
			wantContains: "[webpack] ok",
		},
		{
			name:         "rspack build ok",
			argv:         []string{"rspack", "build"},
			stdout:       []byte(""),
			wantContains: "[rspack build] ok",
		},
		{
			name:         "parcel build ok",
			argv:         []string{"parcel", "build", "src/index.html"},
			stdout:       []byte(""),
			wantContains: "[parcel build] ok",
		},
		{
			name:         "rollup config ok",
			argv:         []string{"rollup", "-c", "rollup.config.js"},
			stdout:       []byte(""),
			wantContains: "[rollup] ok",
		},
		{
			name:         "esbuild ok",
			argv:         []string{"esbuild", "--bundle", "src/main.ts", "--outfile=dist/main.js"},
			stdout:       []byte(""),
			wantContains: "[esbuild] ok",
		},
		{
			name:         "nx build ok",
			argv:         []string{"nx", "build", "myapp"},
			stdout:       []byte(""),
			wantContains: "[nx build] ok",
		},
		{
			name:         "turbo build ok",
			argv:         []string{"turbo", "build"},
			stdout:       []byte(""),
			wantContains: "[turbo build] ok",
		},
		{
			name:         "pnpm run build ok",
			argv:         []string{"pnpm", "run", "build"},
			stdout:       []byte(""),
			wantContains: "[pnpm run build] ok",
		},
		{
			name:         "yarn run build ok",
			argv:         []string{"yarn", "run", "build"},
			stdout:       []byte(""),
			wantContains: "[yarn run build] ok",
		},
		{
			name:         "zig build ok",
			argv:         []string{"zig", "build"},
			stdout:       []byte(""),
			wantContains: "[zig build] ok",
		},
		{
			name:         "wasm-pack build ok",
			argv:         []string{"wasm-pack", "build", "--target", "web"},
			stdout:       []byte(""),
			wantContains: "[wasm-pack build] ok",
		},
		{
			name:         "bazel build ok",
			argv:         []string{"bazel", "build", "//..."},
			stdout:       []byte(""),
			wantContains: "[bazel build] ok",
		},
		{
			name:         "buf build ok",
			argv:         []string{"buf", "build"},
			stdout:       []byte(""),
			wantContains: "[buf build] ok",
		},
		{
			name:         "meson compile ok",
			argv:         []string{"meson", "compile", "-C", "builddir"},
			stdout:       []byte(""),
			wantContains: "[meson compile] ok",
		},
		{
			name:         "just ok",
			argv:         []string{"just", "build"},
			stdout:       []byte(""),
			wantContains: "[just] ok",
		},

		// -- remaining test sub-chain cases --
		{
			name:         "cargo nextest run ok",
			argv:         []string{"cargo", "nextest", "run"},
			stdout:       []byte(""),
			wantContains: "[cargo nextest run] ok",
		},
		{
			name:         "cargo llvm-cov ok",
			argv:         []string{"cargo", "llvm-cov"},
			stdout:       []byte(""),
			wantContains: "[cargo llvm-cov] ok",
		},
		{
			name:         "ginkgo ok",
			argv:         []string{"ginkgo", "-v", "./..."},
			stdout:       []byte(""),
			wantContains: "[ginkgo] ok",
		},
		{
			name:         "ctest ok",
			argv:         []string{"ctest", "--test-dir", "build"},
			stdout:       []byte(""),
			wantContains: "[ctest] ok",
		},
		{
			name:         "playwright test ok",
			argv:         []string{"playwright", "test"},
			stdout:       []byte(""),
			wantContains: "[playwright test] ok",
		},
		{
			name:         "cypress run ok",
			argv:         []string{"cypress", "run", "--headless"},
			stdout:       []byte(""),
			wantContains: "[cypress run] ok",
		},
		{
			name:         "npm run test ok",
			argv:         []string{"npm", "run", "test"},
			stdout:       []byte(""),
			wantContains: "[npm run test] ok",
		},
		{
			name:         "pnpm test ok",
			argv:         []string{"pnpm", "test"},
			stdout:       []byte(""),
			wantContains: "[pnpm test] ok",
		},
		{
			name:         "ava ok",
			argv:         []string{"ava", "tests/**/*.js"},
			stdout:       []byte(""),
			wantContains: "[ava] ok",
		},

		// -- remaining lint sub-chain cases --
		{
			name:         "gocritic check ok",
			argv:         []string{"gocritic", "check", "./..."},
			stdout:       []byte(""),
			wantContains: "[gocritic] ok",
		},
		{
			name:         "gosec ok",
			argv:         []string{"gosec", "./..."},
			stdout:       []byte(""),
			wantContains: "[gosec] ok",
		},
		{
			name:         "protolint ok",
			argv:         []string{"protolint", "lint", "."},
			stdout:       []byte(""),
			wantContains: "[protolint] ok",
		},
		{
			name:         "go vet ok",
			argv:         []string{"go", "vet", "./..."},
			stdout:       []byte(""),
			wantContains: "[go vet] ok",
		},
		{
			name:         "ruff check ok",
			argv:         []string{"ruff", "check", "."},
			stdout:       []byte(""),
			wantContains: "[ruff check] ok",
		},
		{
			name:         "pylint ok",
			argv:         []string{"pylint", "src/"},
			stdout:       []byte(""),
			wantContains: "[pylint] ok",
		},
		{
			name:         "flake8 ok",
			argv:         []string{"flake8", "."},
			stdout:       []byte(""),
			wantContains: "[flake8] ok",
		},
		{
			name:         "bandit ok",
			argv:         []string{"bandit", "-r", "."},
			stdout:       []byte(""),
			wantContains: "[bandit] ok",
		},
		{
			name:         "shellcheck ok",
			argv:         []string{"shellcheck", "script.sh"},
			stdout:       []byte(""),
			wantContains: "[shellcheck] ok",
		},
		{
			name:         "hadolint ok",
			argv:         []string{"hadolint", "Dockerfile"},
			stdout:       []byte(""),
			wantContains: "[hadolint] ok",
		},
		{
			name:         "yamllint ok",
			argv:         []string{"yamllint", "."},
			stdout:       []byte(""),
			wantContains: "[yamllint] ok",
		},
		{
			name:         "zizmor ok",
			argv:         []string{"zizmor", ".github/workflows/ci.yml"},
			stdout:       []byte(""),
			wantContains: "[zizmor] ok",
		},
		{
			name:         "rubocop ok",
			argv:         []string{"rubocop", "--autocorrect"},
			stdout:       []byte(""),
			wantContains: "[rubocop] ok",
		},

		// -- remaining package sub-chain cases --
		{
			name:         "pnpm install ok",
			argv:         []string{"pnpm", "install"},
			stdout:       []byte(""),
			wantContains: "[pnpm install] ok",
		},
		{
			name:         "pipenv install ok",
			argv:         []string{"pipenv", "install"},
			stdout:       []byte(""),
			wantContains: "[pipenv install] ok",
		},
		{
			name:         "gem install ok",
			argv:         []string{"gem", "install", "rails"},
			stdout:       []byte(""),
			wantContains: "[gem install] ok",
		},
		{
			name:         "uv pip install ok",
			argv:         []string{"uv", "pip", "install", "-r", "requirements.txt"},
			stdout:       []byte(""),
			wantContains: "[uv pip install] ok",
		},
		{
			name:         "uv sync ok",
			argv:         []string{"uv", "sync"},
			stdout:       []byte(""),
			wantContains: "[uv sync] ok",
		},

		// -- remaining search sub-chain cases --
		{
			name:         "ag no matches",
			argv:         []string{"ag", "pattern", "."},
			stdout:       []byte(""),
			wantContains: "[ag] no matches",
		},
		{
			name:         "git grep no matches",
			argv:         []string{"git", "grep", "pattern"},
			stdout:       []byte(""),
			wantContains: "[git grep] no matches",
		},

		// -- remaining container sub-chain cases --
		{
			name:         "helm search empty",
			argv:         []string{"helm", "search", "repo", "prometheus"},
			stdout:       []byte(""),
			wantContains: "[helm search] empty",
		},

		// -- remaining build tools --
		{
			name:         "next build ok",
			argv:         []string{"next", "build"},
			stdout:       []byte(""),
			wantContains: "[next build] ok",
		},
		{
			name:         "moon run build ok",
			argv:         []string{"moon", "run", ":build"},
			stdout:       []byte(""),
			wantContains: "[moon run build] ok",
		},
		{
			name:         "pack build ok",
			argv:         []string{"pack", "build", "myapp"},
			stdout:       []byte(""),
			wantContains: "[pack build] ok",
		},

		// -- remaining test tools --
		{
			name:         "yarn test ok",
			argv:         []string{"yarn", "test"},
			stdout:       []byte(""),
			wantContains: "[yarn test] ok",
		},
		{
			name:         "bun test ok",
			argv:         []string{"bun", "test"},
			stdout:       []byte(""),
			wantContains: "[bun test] ok",
		},
		{
			name:         "wdio run ok",
			argv:         []string{"wdio", "run", "wdio.config.js"},
			stdout:       []byte(""),
			wantContains: "[wdio run] ok",
		},
		{
			name:         "nx test ok",
			argv:         []string{"nx", "test", "myapp"},
			stdout:       []byte(""),
			wantContains: "[nx test] ok",
		},
		{
			name:         "turbo test ok",
			argv:         []string{"turbo", "test"},
			stdout:       []byte(""),
			wantContains: "[turbo test] ok",
		},
		{
			name:         "tap ok",
			argv:         []string{"tap", "test/**"},
			stdout:       []byte(""),
			wantContains: "[tap] ok",
		},
		{
			name:         "python unittest ok",
			argv:         []string{"python", "-m", "unittest", "discover"},
			stdout:       []byte(""),
			wantContains: "[python -m unittest] ok",
		},
		{
			name:         "gradle test ok",
			argv:         []string{"gradle", "test"},
			stdout:       []byte(""),
			wantContains: "[gradle test] ok",
		},
		{
			name:         "deno test ok",
			argv:         []string{"deno", "test", "--allow-all"},
			stdout:       []byte(""),
			wantContains: "[deno test] ok",
		},
		{
			name:         "karma start ok",
			argv:         []string{"karma", "start", "--single-run"},
			stdout:       []byte(""),
			wantContains: "[karma] ok",
		},

		// -- remaining lint tools --
		{
			name:         "ty check ok",
			argv:         []string{"ty", "check"},
			stdout:       []byte(""),
			wantContains: "[ty check] ok",
		},
		{
			name:         "biome check ok",
			argv:         []string{"biome", "check", "--write"},
			stdout:       []byte(""),
			wantContains: "[biome check] ok",
		},
		{
			name:         "deno lint ok",
			argv:         []string{"deno", "lint"},
			stdout:       []byte(""),
			wantContains: "[deno lint] ok",
		},
		{
			name:         "actionlint ok",
			argv:         []string{"actionlint"},
			stdout:       []byte(""),
			wantContains: "[actionlint] ok",
		},
		{
			name:         "markdownlint ok",
			argv:         []string{"markdownlint", "docs/"},
			stdout:       []byte(""),
			wantContains: "[markdownlint] ok",
		},
		{
			name:         "ansible-lint ok",
			argv:         []string{"ansible-lint", "playbook.yml"},
			stdout:       []byte(""),
			wantContains: "[ansible-lint] ok",
		},
		{
			name:         "tflint ok",
			argv:         []string{"tflint", "--recursive"},
			stdout:       []byte(""),
			wantContains: "[tflint] ok",
		},
		{
			name:         "oxlint ok",
			argv:         []string{"oxlint", "src/"},
			stdout:       []byte(""),
			wantContains: "[oxlint] ok",
		},
		{
			name:         "revive ok",
			argv:         []string{"revive", "-config", "revive.toml", "./..."},
			stdout:       []byte(""),
			wantContains: "[revive] ok",
		},
		{
			name:         "misspell ok",
			argv:         []string{"misspell", "-error", "."},
			stdout:       []byte(""),
			wantContains: "[misspell] ok",
		},
		{
			name:         "errcheck ok",
			argv:         []string{"errcheck", "./..."},
			stdout:       []byte(""),
			wantContains: "[errcheck] ok",
		},
		{
			name:         "ineffassign ok",
			argv:         []string{"ineffassign", "./..."},
			stdout:       []byte(""),
			wantContains: "[ineffassign] ok",
		},
		{
			name:         "unparam ok",
			argv:         []string{"unparam", "./..."},
			stdout:       []byte(""),
			wantContains: "[unparam] ok",
		},
		{
			name:         "swiftlint ok",
			argv:         []string{"swiftlint", "lint", "--strict"},
			stdout:       []byte(""),
			wantContains: "[swiftlint] ok",
		},
		{
			name:         "shellcheck ok via chain",
			argv:         []string{"shellcheck", "-S", "warning", "script.sh"},
			stdout:       []byte(""),
			wantContains: "[shellcheck] ok",
		},
		{
			name:         "vale ok",
			argv:         []string{"vale", "docs/"},
			stdout:       []byte(""),
			wantContains: "[vale] ok",
		},
		{
			name:         "djlint ok",
			argv:         []string{"djlint", "--check", "templates/"},
			stdout:       []byte(""),
			wantContains: "[djlint] ok",
		},

		// -- remaining package tools --
		{
			name:         "composer install ok",
			argv:         []string{"composer", "install", "--no-dev"},
			stdout:       []byte(""),
			wantContains: "[composer install] ok",
		},
		{
			name:         "mix deps.get ok",
			argv:         []string{"mix", "deps.get"},
			stdout:       []byte(""),
			wantContains: "[mix deps.get] ok",
		},
		{
			name:         "bundle install ok",
			argv:         []string{"bundle", "install", "--jobs=4"},
			stdout:       []byte(""),
			wantContains: "[bundle install] ok",
		},
		{
			name:         "cargo fetch ok",
			argv:         []string{"cargo", "fetch"},
			stdout:       []byte(""),
			wantContains: "[cargo fetch] ok",
		},
		{
			name:         "go mod tidy ok",
			argv:         []string{"go", "mod", "tidy"},
			stdout:       []byte(""),
			wantContains: "[go mod tidy] ok",
		},
		{
			name:         "swift package resolve ok",
			argv:         []string{"swift", "package", "resolve"},
			stdout:       []byte(""),
			wantContains: "[swift package resolve] ok",
		},
		// go test -json (TryCompactGoTestJSON — must come BEFORE go test in the chain)
		{
			name:         "go test -json ok",
			argv:         []string{"go", "test", "-json", "./..."},
			stdout:       []byte(`{"Action":"pass","Package":"foo","Elapsed":0.001}` + "\n" + `{"Action":"pass","Package":"bar","Elapsed":0.002}` + "\n"),
			wantContains: "[go test -json] ok",
		},
		// ko build
		{
			name:         "ko build ok",
			argv:         []string{"ko", "build", "."},
			stdout:       []byte(""),
			wantContains: "[ko build] ok",
		},
		// lint: jscpd, gofumpt, nilaway, gocyclo, forbidigo, prealloc
		{
			name:         "jscpd ok",
			argv:         []string{"jscpd", "src/"},
			stdout:       []byte(""),
			wantContains: "[jscpd] ok",
		},
		{
			name:         "gofumpt ok",
			argv:         []string{"gofumpt", "-w", "main.go"},
			stdout:       []byte(""),
			wantContains: "[gofumpt] ok",
		},
		{
			name:         "nilaway ok",
			argv:         []string{"nilaway", "./..."},
			stdout:       []byte(""),
			wantContains: "[nilaway] ok",
		},
		{
			name:         "gocyclo ok",
			argv:         []string{"gocyclo", "-over", "10", "."},
			stdout:       []byte(""),
			wantContains: "[gocyclo] ok",
		},
		{
			name:         "forbidigo ok",
			argv:         []string{"forbidigo", "./..."},
			stdout:       []byte(""),
			wantContains: "[forbidigo] ok",
		},
		{
			name:         "prealloc ok",
			argv:         []string{"prealloc", "./..."},
			stdout:       []byte(""),
			wantContains: "[prealloc] ok",
		},
		// lint: sqlfluff lint, taplo check, spectral lint, cue vet
		{
			name:         "sqlfluff lint ok",
			argv:         []string{"sqlfluff", "lint", "query.sql"},
			stdout:       []byte(""),
			wantContains: "[sqlfluff lint] ok",
		},
		{
			name:         "taplo check ok",
			argv:         []string{"taplo", "check"},
			stdout:       []byte(""),
			wantContains: "[taplo check] ok",
		},
		{
			name:         "spectral lint ok",
			argv:         []string{"spectral", "lint", "openapi.yaml"},
			stdout:       []byte(""),
			wantContains: "[spectral lint] ok",
		},
		{
			name:         "cue vet ok",
			argv:         []string{"cue", "vet", "./..."},
			stdout:       []byte(""),
			wantContains: "[cue vet] ok",
		},
		// lint: dart analyze, flutter analyze, ktlint, detekt
		{
			name:         "dart analyze ok",
			argv:         []string{"dart", "analyze"},
			stdout:       []byte(""),
			wantContains: "[dart analyze] ok",
		},
		{
			name:         "flutter analyze ok",
			argv:         []string{"flutter", "analyze"},
			stdout:       []byte(""),
			wantContains: "[flutter analyze] ok",
		},
		{
			name:         "ktlint ok",
			argv:         []string{"ktlint"},
			stdout:       []byte(""),
			wantContains: "[ktlint] ok",
		},
		{
			name:         "detekt ok",
			argv:         []string{"detekt"},
			stdout:       []byte(""),
			wantContains: "[detekt] ok",
		},
		// lint: cfn-lint, pint, dotenv-linter, kube-linter
		{
			name:         "cfn-lint ok",
			argv:         []string{"cfn-lint", "-t", "template.yaml"},
			stdout:       []byte(""),
			wantContains: "[cfn-lint] ok",
		},
		{
			name:         "pint ok",
			argv:         []string{"pint", "src/"},
			stdout:       []byte(""),
			wantContains: "[pint] ok",
		},
		{
			name:         "dotenv-linter ok",
			argv:         []string{"dotenv-linter", ".env"},
			stdout:       []byte(""),
			wantContains: "[dotenv-linter] ok",
		},
		{
			name:         "kube-linter ok",
			argv:         []string{"kube-linter", "lint", "deploy/"},
			stdout:       []byte(""),
			wantContains: "[kube-linter] ok",
		},
		// test: uv run pytest, poetry run pytest, hatch test, nox test
		{
			name:         "uv run pytest ok",
			argv:         []string{"uv", "run", "pytest"},
			stdout:       []byte(""),
			wantContains: "[uv run pytest] ok",
		},
		{
			name:         "poetry run pytest ok",
			argv:         []string{"poetry", "run", "pytest"},
			stdout:       []byte(""),
			wantContains: "[poetry run pytest] ok",
		},
		{
			name:         "hatch test ok",
			argv:         []string{"hatch", "test"},
			stdout:       []byte(""),
			wantContains: "[hatch test] ok",
		},
		{
			name:         "nox test ok",
			argv:         []string{"nox", "-s", "test"},
			stdout:       []byte(""),
			wantContains: "[nox test] ok",
		},
		// test: rails test, sbt test, mill test, elm-test
		{
			name:         "rails test ok",
			argv:         []string{"rails", "test"},
			stdout:       []byte(""),
			wantContains: "[rails test] ok",
		},
		{
			name:         "sbt test ok",
			argv:         []string{"sbt", "test"},
			stdout:       []byte(""),
			wantContains: "[sbt test] ok",
		},
		{
			name:         "mill test ok",
			argv:         []string{"mill", "test"},
			stdout:       []byte(""),
			wantContains: "[mill test] ok",
		},
		{
			name:         "elm-test ok",
			argv:         []string{"elm-test"},
			stdout:       []byte(""),
			wantContains: "[elm-test] ok",
		},
		// test: dart test, flutter test
		{
			name:         "dart test ok",
			argv:         []string{"dart", "test"},
			stdout:       []byte(""),
			wantContains: "[dart test] ok",
		},
		{
			name:         "flutter test ok",
			argv:         []string{"flutter", "test"},
			stdout:       []byte(""),
			wantContains: "[flutter test] ok",
		},
		// search: ack, ugrep, sift, plocate, locate, sk (not yet covered)
		{
			name:         "ack no matches",
			argv:         []string{"ack", "pattern"},
			stdout:       []byte(""),
			wantContains: "[ack] no matches",
		},
		{
			name:         "ugrep no matches",
			argv:         []string{"ug", "pattern"},
			stdout:       []byte(""),
			wantContains: "[ug] no matches",
		},
		{
			name:         "sift no matches",
			argv:         []string{"sift", "pattern"},
			stdout:       []byte(""),
			wantContains: "[sift] no matches",
		},
		{
			name:         "plocate no matches",
			argv:         []string{"plocate", "filename"},
			stdout:       []byte(""),
			wantContains: "[plocate] no matches",
		},
		{
			name:         "locate no matches",
			argv:         []string{"locate", "filename"},
			stdout:       []byte(""),
			wantContains: "[locate] no matches",
		},
		{
			name:         "sk no matches",
			argv:         []string{"sk"},
			stdout:       []byte(""),
			wantContains: "[sk] no matches",
		},
		// container: docker compose ps -q, docker compose ls -q
		{
			name:         "docker compose ps quiet no containers",
			argv:         []string{"docker", "compose", "ps", "-q"},
			stdout:       []byte(""),
			wantContains: "[docker compose ps] no containers",
		},
		{
			name:         "docker compose ls quiet empty",
			argv:         []string{"docker", "compose", "ls", "-q"},
			stdout:       []byte(""),
			wantContains: "[docker compose ls] empty",
		},
		// lint: phpcs, phpstan, psalm, phan (PHP tools)
		{
			name:         "phpcs ok",
			argv:         []string{"phpcs", "src/"},
			stdout:       []byte(""),
			wantContains: "[phpcs] ok",
		},
		{
			name:         "phpstan ok",
			argv:         []string{"phpstan", "analyse", "src/"},
			stdout:       []byte(""),
			wantContains: "[phpstan] ok",
		},
		{
			name:         "psalm ok",
			argv:         []string{"psalm"},
			stdout:       []byte(""),
			wantContains: "[psalm] ok",
		},
		{
			name:         "phan ok",
			argv:         []string{"phan"},
			stdout:       []byte(""),
			wantContains: "[phan] ok",
		},
		// lint: mypy, pyright (Python type checkers)
		{
			name:         "mypy ok",
			argv:         []string{"mypy", "src/"},
			stdout:       []byte(""),
			wantContains: "[mypy] ok",
		},
		{
			name:         "pyright ok",
			argv:         []string{"pyright"},
			stdout:       []byte(""),
			wantContains: "[pyright] ok",
		},
		// lint: eslint, stylelint (JS/CSS linters)
		{
			name:         "eslint ok",
			argv:         []string{"eslint", "src/"},
			stdout:       []byte(""),
			wantContains: "[eslint] ok",
		},
		{
			name:         "stylelint ok",
			argv:         []string{"stylelint", "src/**/*.css"},
			stdout:       []byte(""),
			wantContains: "[stylelint] ok",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := applyLayer0AfterANSI(dir, tc.argv, tc.stdout)
			s := string(out)
			if !strings.Contains(s, tc.wantContains) {
				t.Errorf("filter %q: want %q in output, got %q", tc.name, tc.wantContains, s)
			}
			if tc.wantAbsent != "" && strings.Contains(s, tc.wantAbsent) {
				t.Errorf("filter %q: want %q absent from output, got %q", tc.name, tc.wantAbsent, s)
			}
		})
	}
}

func TestRunPipeline_passthroughMax(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	pr := RunPipeline(context.Background(), t.TempDir(), []string{"sh", "-c", `printf '%4096s' x`}, 32)
	if pr.Err != nil {
		t.Fatal(pr.Err)
	}
	if len(pr.Stdout) > 200 {
		t.Fatalf("expected truncation, got len %d", len(pr.Stdout))
	}
	if !strings.Contains(string(pr.Stdout), "truncated") {
		t.Fatalf("%q", pr.Stdout)
	}
}

func TestRunPipeline_CompactsStderr(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "git")
	script := "#!/bin/sh\nfor i in $(seq 1 40); do printf '?? generated_%03d.txt\\n' \"$i\"; done >&2\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	pr := RunPipeline(context.Background(), dir, []string{bin, "status", "--short"}, 0)
	if pr.Err != nil {
		t.Fatal(pr.Err)
	}
	if pr.Code != 0 {
		t.Fatalf("code %d stderr=%q", pr.Code, pr.Stderr)
	}
	if !strings.Contains(string(pr.Stderr), "[git status]") {
		t.Fatalf("stderr was not compacted: %q", pr.Stderr)
	}
	if strings.Contains(string(pr.Stderr), "generated_040.txt") {
		t.Fatalf("stderr still contains raw status output: %q", pr.Stderr)
	}
	if !strings.Contains(string(pr.RawStderr), "generated_040.txt") {
		t.Fatalf("raw stderr should remain available for recovery: %q", pr.RawStderr)
	}
	if pr.OutputTokens >= pr.InputTokens {
		t.Fatalf("expected stderr compaction savings, in=%d out=%d stderr=%q", pr.InputTokens, pr.OutputTokens, pr.Stderr)
	}
}

func TestRunPipeline_NonZeroStderrDoesNotEmitStdoutOK(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cargo")
	script := `#!/bin/sh
cat >&2 <<'OUT'
    Checking demo v0.1.0 (/tmp/demo)
     Running ` + "`" + `CARGO=/toolchain/bin/cargo /toolchain/bin/rustc --crate-name demo src/main.rs --error-format=json --json=diagnostic-rendered-ansi` + "`" + `
error[E0308]: mismatched types
 --> src/main.rs:2:22
  |
2 |     let value: i32 = "not an integer";
  |                ---   ^^^^^^^^^^^^^^^^ expected ` + "`" + `i32` + "`" + `, found ` + "`" + `&str` + "`" + `
  |                |
  |                expected due to this

For more information about this error, try ` + "`" + `rustc --explain E0308` + "`" + `.
error: could not compile ` + "`" + `demo` + "`" + ` (bin "demo") due to 1 previous error

Caused by:
  process didn't exit successfully: ` + "`" + `/toolchain/bin/rustc --crate-name demo src/main.rs --error-format=json --json=diagnostic-rendered-ansi` + "`" + ` (exit status: 1)
OUT
exit 101
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	pr := RunPipeline(context.Background(), dir, []string{bin, "check", "-vv"}, 0)
	if pr.Err != nil {
		t.Fatal(pr.Err)
	}
	if pr.Code != 101 {
		t.Fatalf("code=%d stdout=%q stderr=%q", pr.Code, pr.Stdout, pr.Stderr)
	}
	if strings.Contains(string(pr.Stdout), "[cargo check] ok") || strings.TrimSpace(string(pr.Stdout)) != "" {
		t.Fatalf("stdout must not fake success on stderr failure: %q", pr.Stdout)
	}
	stderr := string(pr.Stderr)
	for _, want := range []string{"[cargo check] FAILED", "error[E0308]", "let value: i32", "expected due to this", "could not compile"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q: %q", want, stderr)
		}
	}
	if strings.Contains(stderr, "Running `CARGO=") {
		t.Fatalf("stderr kept neutral verbose command noise: %q", stderr)
	}
	if pr.OutputTokens >= pr.InputTokens {
		t.Fatalf("expected savings, in=%d out=%d stderr=%q", pr.InputTokens, pr.OutputTokens, stderr)
	}
}

func TestRunPipeline_EmptySuccessCompactsOnlyStdout(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cargo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	pr := RunPipeline(context.Background(), dir, []string{bin, "check"}, 0)
	if pr.Err != nil {
		t.Fatal(pr.Err)
	}
	if strings.TrimSpace(string(pr.Stdout)) != "[cargo check] ok" {
		t.Fatalf("stdout=%q", pr.Stdout)
	}
	if strings.TrimSpace(string(pr.Stderr)) != "" {
		t.Fatalf("stderr must stay empty on empty success: %q", pr.Stderr)
	}
}

// TestRunPipeline_startError covers the runErr!=nil early return in RunPipeline.
func TestRunPipeline_startError(t *testing.T) {
	t.Parallel()
	// Empty argv causes RunCommand to return an error immediately
	pr := RunPipeline(context.Background(), t.TempDir(), []string{}, 0)
	if pr.Err == nil {
		t.Fatal("expected error for empty argv")
	}
}
