package filter

import "testing"

// TestBuildToolLabel exercises buildToolLabel for the main tool families.
func TestBuildToolLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		// Go
		{[]string{"go", "build", "./..."}, "go build"},
		// Cargo
		{[]string{"cargo", "build"}, "cargo build"},
		{[]string{"cargo", "check"}, "cargo check"},
		{[]string{"cargo", "doc"}, "cargo doc"},
		// Make
		{[]string{"make", "all"}, "make"},
		{[]string{"gmake"}, "make"},
		// CMake
		{[]string{"cmake", "--build", "."}, "cmake --build"},
		// Maven
		{[]string{"mvn", "package"}, "mvn"},
		// Gradle
		{[]string{"gradle", "build"}, "gradle build"},
		{[]string{"./gradlew", "build"}, "gradle build"},
		// Ninja
		{[]string{"ninja"}, "ninja"},
		{[]string{"ninja", "-C", "build"}, "ninja"},
		// Zig
		{[]string{"zig", "build"}, "zig build"},
		// Bazel
		{[]string{"bazel", "build", "//..."}, "bazel build"},
		{[]string{"bazelisk", "build", "//..."}, "bazel build"},
		// Swift
		{[]string{"swift", "build"}, "swift build"},
		// KO
		{[]string{"ko", "build", "./..."}, "ko build"},
		// Meson
		{[]string{"meson", "compile"}, "meson compile"},
		// Just
		{[]string{"just"}, "just"},
		{[]string{"just", "build"}, "just"},
		// Pack
		{[]string{"pack", "build", "myimage"}, "pack build"},
		// npm/pnpm/yarn run build
		{[]string{"npm", "run", "build"}, "npm run build"},
		{[]string{"pnpm", "run", "build"}, "pnpm run build"},
		{[]string{"yarn", "run", "build"}, "yarn run build"},
		// JS bundlers
		{[]string{"tsc", "--noEmit"}, "tsc"},
		{[]string{"next", "build"}, "next build"},
		{[]string{"vite", "build"}, "vite build"},
		{[]string{"webpack"}, "webpack"},
		{[]string{"webpack-cli"}, "webpack"},
		{[]string{"rspack", "build"}, "rspack build"},
		{[]string{"parcel", "build", "src/"}, "parcel build"},
		{[]string{"rollup", "-c"}, "rollup"},
		{[]string{"esbuild", "--bundle", "src/index.ts"}, "esbuild"},
		{[]string{"nx", "build", "myapp"}, "nx build"},
		{[]string{"turbo", "build"}, "turbo build"},
		{[]string{"buf", "build"}, "buf build"},
		{[]string{"wasm-pack", "build"}, "wasm-pack build"},
		{[]string{"moon", "build"}, "moon build"},
		// npx wrappers
		{[]string{"npx", "tsc", "--noEmit"}, "tsc"},
		{[]string{"npx", "next", "build"}, "next build"},
		{[]string{"npx", "ninja"}, "ninja"},
		// pnpm exec wrappers
		{[]string{"pnpm", "exec", "tsc"}, "tsc"},
		{[]string{"pnpm", "exec", "webpack"}, "webpack"},
		// yarn wrappers
		{[]string{"yarn", "tsc"}, "tsc"},
		{[]string{"yarn", "vite", "build"}, "vite build"},
		// unknown → empty
		{[]string{"unknown-tool", "build"}, ""},
		{[]string{}, ""},
	}
	for _, c := range cases {
		got := buildToolLabel(c.argv)
		if got != c.label {
			t.Errorf("buildToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

// TestLintToolLabel exercises lintToolLabel for all registered lint tools.
func TestLintToolLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		// Switch-based (specific detection functions)
		{[]string{"cargo", "clippy"}, "cargo clippy"},
		{[]string{"cargo", "audit"}, "cargo audit"},
		{[]string{"go", "vet", "./..."}, "go vet"},
		{[]string{"pylint", "src/"}, "pylint"},
		{[]string{"flake8", "."}, "flake8"},
		{[]string{"bandit", "-r", "src/"}, "bandit"},
		{[]string{"mypy", "src/"}, "mypy"},
		{[]string{"dart", "analyze"}, "dart analyze"},
		{[]string{"flutter", "analyze"}, "flutter analyze"},
		// binSub slice
		{[]string{"golangci-lint", "run"}, "golangci-lint"},
		{[]string{"staticcheck", "./..."}, "staticcheck"},
		{[]string{"gocritic", "check", "./..."}, "gocritic"},
		{[]string{"gosec", "./..."}, "gosec"},
		{[]string{"buf", "lint"}, "buf lint"},
		{[]string{"protolint", "lint", "."}, "protolint"},
		{[]string{"semgrep", "--config=auto"}, "semgrep"},
		{[]string{"jscpd", "src/"}, "jscpd"},
		{[]string{"djlint", "templates/"}, "djlint"},
		{[]string{"ty", "check"}, "ty check"},
		{[]string{"gofumpt", "-l", "."}, "gofumpt"},
		{[]string{"revive", "./..."}, "revive"},
		{[]string{"errcheck", "./..."}, "errcheck"},
		{[]string{"ineffassign", "./..."}, "ineffassign"},
		{[]string{"nilaway", "./..."}, "nilaway"},
		{[]string{"unparam", "./..."}, "unparam"},
		{[]string{"misspell", "."}, "misspell"},
		{[]string{"gocyclo", "."}, "gocyclo"},
		{[]string{"forbidigo", "./..."}, "forbidigo"},
		{[]string{"prealloc", "./..."}, "prealloc"},
		{[]string{"ruff", "check", "."}, "ruff check"},
		{[]string{"biome", "check", "."}, "biome check"},
		{[]string{"sqlfluff", "lint", "."}, "sqlfluff lint"},
		{[]string{"taplo", "check", "*.toml"}, "taplo check"},
		{[]string{"cue", "vet"}, "cue vet"},
		{[]string{"spectral", "lint", "openapi.yaml"}, "spectral lint"},
		{[]string{"oxlint", "src/"}, "oxlint"},
		{[]string{"deno", "lint"}, "deno lint"},
		{[]string{"swiftlint"}, "swiftlint"},
		{[]string{"ktlint"}, "ktlint"},
		{[]string{"detekt"}, "detekt"},
		{[]string{"shellcheck", "script.sh"}, "shellcheck"},
		{[]string{"ansible-lint"}, "ansible-lint"},
		{[]string{"hadolint", "Dockerfile"}, "hadolint"},
		{[]string{"markdownlint", "."}, "markdownlint"},
		{[]string{"yamllint", "."}, "yamllint"},
		{[]string{"dotenv-linter"}, "dotenv-linter"},
		{[]string{"kube-linter", "lint", "."}, "kube-linter"},
		{[]string{"tflint"}, "tflint"},
		{[]string{"cfn-lint", "template.yaml"}, "cfn-lint"},
		{[]string{"actionlint"}, "actionlint"},
		{[]string{"zizmor", "."}, "zizmor"},
		{[]string{"vale", "."}, "vale"},
		{[]string{"rubocop"}, "rubocop"},
		{[]string{"pint"}, "pint"},
		{[]string{"phpcs", "src/"}, "phpcs"},
		{[]string{"phpstan", "analyse"}, "phpstan"},
		{[]string{"psalm"}, "psalm"},
		{[]string{"phan"}, "phan"},
		{[]string{"pyright", "."}, "pyright"},
		{[]string{"basedpyright", "."}, "pyright"},
		{[]string{"eslint", "src/"}, "eslint"},
		{[]string{"stylelint", "src/"}, "stylelint"},
		// npx wrappers for some
		{[]string{"npx", "golangci-lint", "run"}, "golangci-lint"},
		{[]string{"npx", "eslint", "src/"}, "eslint"},
		{[]string{"pnpm", "exec", "stylelint", "src/"}, "stylelint"},
		{[]string{"yarn", "rubocop"}, "rubocop"},
		// Unknown
		{[]string{"unknown-linter"}, ""},
	}
	for _, c := range cases {
		got := lintToolLabel(c.argv)
		if got != c.label {
			t.Errorf("lintToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

// TestPkgToolLabel exercises pkgToolLabel for all package managers.
func TestPkgToolLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		{[]string{"npm", "install"}, "npm install"},
		{[]string{"npm", "ci"}, "npm ci"},
		{[]string{"pnpm", "install"}, "pnpm install"},
		{[]string{"pnpm", "ci"}, "pnpm ci"},
		{[]string{"yarn", "install"}, "yarn install"},
		{[]string{"pip", "install", "-r", "requirements.txt"}, "pip install"},
		{[]string{"pip3", "install", "flask"}, "pip install"},
		{[]string{"bun", "install"}, "bun install"},
		// Len < 2 → empty
		{[]string{"npm"}, ""},
		// Unknown subcommand
		{[]string{"npm", "publish"}, ""},
		{[]string{"pip", "list"}, ""},
		// Unknown tool
		{[]string{"cargo", "install"}, ""},
	}
	for _, c := range cases {
		got := pkgToolLabel(c.argv)
		if got != c.label {
			t.Errorf("pkgToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

// TestFormatToolLabel exercises formatToolLabel paths not covered by chain tests.
func TestFormatToolLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv  []string
		label string
	}{
		// Already covered in TryCompactFormatOutput tests but exercise directly:
		{[]string{"prettier", "--write", "src/"}, "prettier"},
		{[]string{"dprint", "fmt"}, "dprint"},
		{[]string{"biome", "format", "."}, "biome"},
		{[]string{"buf", "format", "-w"}, "buf"},
		{[]string{"terraform", "fmt"}, "terraform"},
		{[]string{"tofu", "fmt"}, "terraform"},
		{[]string{"black", "src/"}, "black"},
		{[]string{"taplo", "format", "x.toml"}, "taplo"},
		{[]string{"shfmt", "-w", "script.sh"}, "shfmt"},
		{[]string{"sqlfmt", "q.sql"}, "sqlfmt"},
		{[]string{"isort", "."}, "isort"},
		{[]string{"autopep8", "a.py"}, "autopep8"},
		{[]string{"gofmt", "-l", "."}, "gofmt"},
		{[]string{"go", "fmt", "./..."}, "go fmt"},
		{[]string{"rustfmt", "src/lib.rs"}, "rustfmt"},
		{[]string{"clang-format", "-i", "a.cc"}, "clang-format"},
		{[]string{"zig", "fmt", "src/"}, "zig fmt"},
		// npx wrappers
		{[]string{"npx", "prettier", "."}, "prettier"},
		{[]string{"npx", "gofmt", "."}, "gofmt"},
		{[]string{"npx", "rustfmt", "a.rs"}, "rustfmt"},
		{[]string{"npx", "zig", "fmt", "."}, "zig fmt"},
		// pnpm exec wrappers
		{[]string{"pnpm", "exec", "prettier", "."}, "prettier"},
		{[]string{"pnpm", "exec", "gofmt", "."}, "gofmt"},
		{[]string{"pnpm", "exec", "rustfmt", "src/"}, "rustfmt"},
		{[]string{"pnpm", "exec", "clang-format-18", "a.cc"}, "clang-format"},
		{[]string{"pnpm", "exec", "zig", "fmt", "."}, "zig fmt"},
		// yarn wrappers
		{[]string{"yarn", "prettier", "."}, "prettier"},
		{[]string{"yarn", "gofmt", "."}, "gofmt"},
		{[]string{"yarn", "rustfmt", "a.rs"}, "rustfmt"},
		{[]string{"yarn", "clang-format", "a.cc"}, "clang-format"},
		{[]string{"yarn", "zig", "fmt", "."}, "zig fmt"},
		// shfmt in list mode → not a formatter
		{[]string{"shfmt", "-l", "."}, ""},
		{[]string{"shfmt", "--diff", "."}, ""},
		// unknown
		{[]string{"unknown-formatter"}, ""},
		{[]string{}, ""},
	}
	for _, c := range cases {
		got := formatToolLabel(c.argv)
		if got != c.label {
			t.Errorf("formatToolLabel(%v) = %q, want %q", c.argv, got, c.label)
		}
	}
}

// TestCompactContainerTable_fewRows verifies the rows<=5 passthrough branch.
func TestCompactContainerTable_fewRows(t *testing.T) {
	t.Parallel()
	result := compactContainerTable("label", "docker ps", 3)
	if result != "" {
		t.Errorf("expected empty for 3 rows, got %q", result)
	}
}

// TestCompactContainerTable_manyRows verifies compaction for >5 rows.
func TestCompactContainerTable_manyRows(t *testing.T) {
	t.Parallel()
	result := compactContainerTable("label", "docker ps", 8)
	if result != "[docker ps] 8 item(s)\n" {
		t.Errorf("want item count, got %q", result)
	}
}

// TestIsSingleBinarySubcmdArgv_direct exercises the direct (non-wrapper) path.
func TestIsSingleBinarySubcmdArgv_direct(t *testing.T) {
	t.Parallel()
	if !isSingleBinarySubcmdArgv([]string{"golangci-lint", "run"}, "golangci-lint", "") {
		t.Error("direct golangci-lint should match")
	}
	if !isSingleBinarySubcmdArgv([]string{"buf", "lint"}, "buf", "lint") {
		t.Error("direct buf lint should match")
	}
	if isSingleBinarySubcmdArgv([]string{}, "buf", "lint") {
		t.Error("empty argv should not match")
	}
}
