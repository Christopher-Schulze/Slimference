package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

func TestCommandOutputFirstShimGitStatusCleanFullPassesEmptyOutput(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
if [ "$1" = "status" ]; then
  exit 0
fi
echo "unexpected $*" >&2
exit 2
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "status", "--short"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout=%q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestCommandOutputFirstShimGitStatusDirtyFullPasses(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
if [ "$1" = "status" ]; then
  printf ' M file.go\n?? new.txt\n'
  exit 0
fi
exit 2
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "status", "--short"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != " M file.go\n?? new.txt\n" {
		t.Fatalf("stdout=%q", got)
	}
}

func TestCommandOutputFirstShimGitDiffStatCompacts(t *testing.T) {
	withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(" internal/proxy/file")
		b.WriteString(string(rune('a' + (i % 20))))
		b.WriteString(".go | 10 +++++-----\n")
	}
	b.WriteString(" 40 files changed, 200 insertions(+), 200 deletions(-)\n")
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "diff", "--stat"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[git diff --stat] 40 file(s)") || !strings.Contains(got, "[prefix=internal/proxy/]") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
}

func TestCommandOutputFirstShimGitLsFilesCompacts(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 12; i++ {
		b.WriteString("internal/proxy/path/file")
		b.WriteString(string(rune('a' + i)))
		b.WriteString(".go\n")
	}
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "ls-files", "--cached"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[git ls-files paths]") || !strings.Contains(got, "internal/proxy/path/") {
		t.Fatalf("unexpected compacted stdout=%q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:git] git ls-files --cached") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.ProjectPath != "/repo" {
		t.Fatalf("project path=%q", run.ProjectPath)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimStderrFullPasses(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
printf ' file.go | 1 +\n'
printf 'warning: side channel\n' >&2
exit 0
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "diff", "--stat"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if got := stdout.String(); got != " file.go | 1 +\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := stderr.String(); got != "warning: side channel\n" {
		t.Fatalf("stderr=%q", got)
	}
}

func TestCommandOutputFirstAllowCaptureKeepsPlainDiffOut(t *testing.T) {
	if commandOutputFirstAllowCapture("git", []string{"diff"}) {
		t.Fatal("plain git diff must not be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("git", []string{"diff", "--stat"}) {
		t.Fatal("git diff --stat should be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"-C", "/repo", "diff", "--name-only"}) {
		t.Fatal("git -C repo diff --name-only should be captured")
	}
	if commandOutputFirstAllowCapture("git", []string{"show", "HEAD"}) {
		t.Fatal("git show must not be captured")
	}
	if !commandOutputFirstAllowCapture("git", []string{"grep", "-n", "TODO", "--", "internal"}) {
		t.Fatal("git grep should be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("rg", []string{"TODO"}) {
		t.Fatal("rg should be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("rg", []string{"--files", "internal"}) {
		t.Fatal("rg --files should be captured by the command-output-first shim")
	}
	if commandOutputFirstAllowCapture("grep", []string{"TODO"}) {
		t.Fatal("grep is not part of the first command-output-first command set")
	}
	if !commandOutputFirstAllowCapture("go", []string{"test", "-v", "./..."}) {
		t.Fatal("go test should be captured by the command-output-first shim")
	}
	if !commandOutputFirstAllowCapture("go", []string{"-C", "/repo", "build", "./cmd/slimference"}) {
		t.Fatal("go -C repo build should be captured by the command-output-first shim")
	}
	if commandOutputFirstAllowCapture("go", []string{"env"}) {
		t.Fatal("go env must not be captured by the command-output-first shim")
	}
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{command: "npm", args: []string{"test", "--", "--runInBand"}},
		{command: "npm", args: []string{"run", "test"}},
		{command: "pnpm", args: []string{"run", "test"}},
		{command: "yarn", args: []string{"run", "test"}},
		{command: "bun", args: []string{"test"}},
		{command: "npm", args: []string{"run", "build"}},
		{command: "pnpm", args: []string{"run", "build"}},
		{command: "yarn", args: []string{"run", "build"}},
		{command: "cargo", args: []string{"test", "--", "--nocapture"}},
		{command: "cargo", args: []string{"+nightly", "nextest", "run"}},
		{command: "cargo", args: []string{"check", "--workspace"}},
		{command: "cargo", args: []string{"clippy", "--all-targets"}},
		{command: "pytest", args: []string{"-vv"}},
		{command: "python3", args: []string{"-m", "pytest", "-vv"}},
		{command: "python", args: []string{"-u", "-m", "unittest"}},
		{command: "uv", args: []string{"run", "pytest", "-vv"}},
		{command: "poetry", args: []string{"run", "python", "-m", "pytest"}},
		{command: "fd", args: []string{"--extension", "go", "internal"}},
		{command: "fdfind", args: []string{"-e", "go", "internal"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-type", "f"}},
		{command: "wc", args: []string{"-l", "cmd/slimference/command_output_first.go"}},
		{command: "npx", args: []string{"-y", "next", "build"}},
		{command: "make", args: []string{"-j8"}},
		{command: "cmake", args: []string{"--build", "build", "--parallel"}},
		{command: "tsc", args: []string{"--noEmit"}},
		{command: "next", args: []string{"build"}},
		{command: "vite", args: []string{"build"}},
		{command: "webpack", args: []string{"--mode", "production"}},
		{command: "webpack-cli", args: []string{"--mode", "production"}},
		{command: "pre-commit", args: []string{"run", "--all-files"}},
		{command: "staticcheck", args: []string{"./..."}},
		{command: "errcheck", args: []string{"./..."}},
		{command: "gocyclo", args: []string{"-over", "12", "."}},
		{command: "ruff", args: []string{"check", "."}},
		{command: "pyright", args: []string{"--outputjson", "src"}},
		{command: "stylelint", args: []string{"--formatter", "json", "**/*.css"}},
		{command: "eslint", args: []string{"src", "--format", "stylish"}},
		{command: "prettier", args: []string{"--check", "."}},
		{command: "npm", args: []string{"run", "lint"}},
		{command: "pnpm", args: []string{"run", "typecheck"}},
		{command: "yarn", args: []string{"run", "format:check"}},
		{command: "bun", args: []string{"run", "build"}},
		{command: "tsup", args: []string{"src/index.ts"}},
		{command: "rspack", args: []string{"build"}},
		{command: "parcel", args: []string{"build", "src/index.html"}},
		{command: "rollup", args: []string{"-c"}},
		{command: "esbuild", args: []string{"src/index.ts", "--bundle"}},
		{command: "nx", args: []string{"build", "web"}},
		{command: "turbo", args: []string{"run", "build"}},
		{command: "mvn", args: []string{"test"}},
		{command: "mvnw", args: []string{"-q", "verify"}},
		{command: "gradle", args: []string{"build"}},
		{command: "gradlew", args: []string{"build", "--parallel"}},
		{command: "meson", args: []string{"compile", "-C", "build"}},
		{command: "zig", args: []string{"build"}},
		{command: "wasm-pack", args: []string{"build"}},
		{command: "bazel", args: []string{"build", "//..."}},
		{command: "swift", args: []string{"build"}},
		{command: "buf", args: []string{"build"}},
		{command: "ko", args: []string{"build", "./cmd/app"}},
		{command: "moon", args: []string{"run", "web:build"}},
		{command: "pack", args: []string{"build", "app"}},
		{command: "vitest", args: []string{"run"}},
		{command: "jest", args: []string{"--runInBand"}},
		{command: "mocha", args: []string{"test/**/*.spec.js"}},
		{command: "playwright", args: []string{"test"}},
		{command: "cypress", args: []string{"run", "--headless"}},
		{command: "wdio", args: []string{"run", "wdio.conf.ts"}},
		{command: "nx", args: []string{"test", "web"}},
		{command: "turbo", args: []string{"run", "test"}},
		{command: "deno", args: []string{"test", "--allow-all"}},
		{command: "npx", args: []string{"-y", "vitest", "run"}},
		{command: "nox", args: []string{"-s", "test"}},
		{command: "tox", args: []string{"-e", "test"}},
		{command: "rake", args: []string{"spec"}},
		{command: "rails", args: []string{"test"}},
		{command: "gradle", args: []string{"test"}},
		{command: "sbt", args: []string{"test"}},
		{command: "mill", args: []string{"foo.test"}},
	} {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v should be captured", tc.command, tc.args)
		}
	}
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{command: "npm", args: []string{"install"}},
		{command: "npm", args: []string{"run", "dev"}},
		{command: "pnpm", args: []string{"exec", "vitest"}},
		{command: "yarn", args: []string{"start"}},
		{command: "bun", args: []string{"install"}},
		{command: "cargo", args: []string{"install", "ripgrep"}},
		{command: "cargo", args: []string{"nextest", "list"}},
		{command: "python3", args: []string{"script.py"}},
		{command: "python3", args: []string{"-m", "http.server"}},
		{command: "uv", args: []string{"run", "python", "script.py"}},
		{command: "poetry", args: []string{"install"}},
		{command: "find", args: []string{"internal", "-type", "f"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-delete"}},
		{command: "fd", args: []string{"--exec", "rm", "{}"}},
		{command: "wc", args: []string{"-l"}},
		{command: "wc", args: []string{"--files0-from=list"}},
		{command: "npx", args: []string{"-c", "next build"}},
		{command: "npx", args: []string{"cowsay", "hello"}},
		{command: "make", args: []string{"-n"}},
		{command: "cmake", args: []string{"-S", ".", "-B", "build"}},
		{command: "tsc", args: []string{"--watch"}},
		{command: "next", args: []string{"dev"}},
		{command: "vite", args: []string{"--host", "127.0.0.1"}},
		{command: "webpack", args: []string{"serve"}},
		{command: "prettier", args: []string{"--write", "."}},
		{command: "ruff", args: []string{"format", "."}},
		{command: "npm", args: []string{"run", "dev"}},
		{command: "yarn", args: []string{"run", "format"}},
		{command: "playwright", args: []string{"codegen"}},
		{command: "cypress", args: []string{"open"}},
		{command: "deno", args: []string{"run", "script.ts"}},
		{command: "nox", args: []string{"-s", "lint"}},
		{command: "tox", args: []string{"-e", "lint"}},
		{command: "rake", args: []string{"db:migrate"}},
		{command: "rails", args: []string{"server"}},
		{command: "gradle", args: []string{"assemble"}},
		{command: "sbt", args: []string{"compile"}},
		{command: "mill", args: []string{"foo.compile"}},
		{command: "tsup", args: []string{"--watch"}},
		{command: "rspack", args: []string{"serve"}},
		{command: "parcel", args: []string{"watch", "src/index.html"}},
		{command: "rollup", args: []string{"--watch", "-c"}},
		{command: "esbuild", args: []string{"src/index.ts"}},
		{command: "mvn", args: []string{"deploy"}},
		{command: "mvn", args: []string{"site"}},
		{command: "gradle", args: []string{"assemble"}},
		{command: "meson", args: []string{"setup", "build"}},
		{command: "moon", args: []string{"run", "web:test"}},
	} {
		if commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v must not be captured", tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstShimRgCompactsWithFullRetentionAndAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 36; i++ {
		b.WriteString("internal/proxy/long/path/handler.go:")
		b.WriteString(strings.Repeat("1", len("36")-len("1")))
		b.WriteString("1")
		b.WriteString(":func handleRequest() { return nil } // repeated match payload\n")
	}
	realRg := writeFakeCommand(t, "rg", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRg, "--", "handleRequest", "internal/proxy"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[rg] 36 match(es) in 1 file(s)") || !strings.Contains(got, "internal/proxy/long/path/handler.go") {
		t.Fatalf("unexpected compacted rg stdout=%q", got)
	}
	if strings.Contains(got, "[+") {
		t.Fatalf("rg command-output-first must retain every match in first product slice: %q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:rg] rg handleRequest internal/proxy") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimRgEmptyFullPasses(t *testing.T) {
	realRg := writeFakeCommand(t, "rg", "#!/bin/sh\nexit 0\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRg, "--", "missing"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimRgFilesCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 0; i < 48; i++ {
		b.WriteString("internal/filter/generated/deep/file_")
		if i < 10 {
			b.WriteByte('0')
		}
		b.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		b.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		b.WriteString(".go\n")
	}
	realRg := writeFakeCommand(t, "rg", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=rg", "--real-bin=" + realRg, "--", "--files", "-g", "*.go", "internal/filter"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[rg --files paths]") || !strings.Contains(got, "internal/filter/generated/deep/") || !strings.Contains(got, "file_47.go") {
		t.Fatalf("unexpected compacted rg --files stdout=%q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:rg] rg --files -g *.go internal/filter") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFindAndWcCompact(t *testing.T) {
	var paths strings.Builder
	for i := 0; i < 44; i++ {
		paths.WriteString("docs/todo/generated/task_")
		if i < 10 {
			paths.WriteByte('0')
		}
		paths.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		paths.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		paths.WriteString(".md\n")
	}
	realFind := writeFakeCommand(t, "find", "#!/bin/sh\ncat <<'EOF'\n"+paths.String()+"EOF\n")
	var findStdout, findStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=find", "--real-bin=" + realFind, "--", "docs/todo", "-maxdepth", "4", "-type", "f"}, &bytes.Buffer{}, &findStdout, &findStderr)
	if rc != 0 {
		t.Fatalf("find rc=%d stderr=%q", rc, findStderr.String())
	}
	findOut := findStdout.String()
	if !strings.Contains(findOut, "[find paths]") || !strings.Contains(findOut, "docs/todo/generated/") || !strings.Contains(findOut, "task_43.md") {
		t.Fatalf("unexpected compacted find stdout=%q", findOut)
	}

	var wcRows strings.Builder
	totalLines := 0
	totalWords := 0
	for i := 0; i < 40; i++ {
		lines := 30 + i
		words := 90 + i
		totalLines += lines
		totalWords += words
		wcRows.WriteString("      ")
		wcRows.WriteString(strconv.Itoa(lines))
		wcRows.WriteString("      ")
		wcRows.WriteString(strconv.Itoa(words))
		wcRows.WriteString(" src/file")
		wcRows.WriteString(strconv.Itoa(i))
		wcRows.WriteString(".go\n")
	}
	wcRows.WriteString("      ")
	wcRows.WriteString(strconv.Itoa(totalLines))
	wcRows.WriteString("      ")
	wcRows.WriteString(strconv.Itoa(totalWords))
	wcRows.WriteString(" total\n")
	realWc := writeFakeCommand(t, "wc", "#!/bin/sh\ncat <<'EOF'\n"+wcRows.String()+"EOF\n")
	var wcStdout, wcStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=wc", "--real-bin=" + realWc, "--", "-lw", "src/main.go", "src/lib.go"}, &bytes.Buffer{}, &wcStdout, &wcStderr)
	if rc != 0 {
		t.Fatalf("wc rc=%d stderr=%q", rc, wcStderr.String())
	}
	gotWc := commandOutputFirstVisibleOutput(wcStdout.String())
	if !strings.Contains(gotWc, "[wc prefix=src/]") || !strings.Contains(gotWc, "file39.go: 69L 129W") || !strings.Contains(gotWc, "total: ") {
		t.Fatalf("unexpected compacted wc stdout=%q", gotWc)
	}
}

func TestCommandOutputFirstShimGitGrepCompactsWithFullRetentionAndAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 34; i++ {
		b.WriteString("internal/proxy/long/path/handler.go:")
		b.WriteString("42")
		b.WriteString(":func handleGitGrepResult() { return nil } // repeated search payload\n")
	}
	realGit := writeFakeGit(t, "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "grep", "-n", "handleGitGrepResult", "--", "internal/proxy"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[git grep] 34 match(es) in 1 file(s)") || !strings.Contains(got, "internal/proxy/long/path/handler.go") {
		t.Fatalf("unexpected compacted git grep stdout=%q", got)
	}
	if strings.Contains(got, "[+") {
		t.Fatalf("git grep command-output-first must retain every match in first product slice: %q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:git] git grep -n handleGitGrepResult -- internal/proxy") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGitGrepNoMatchFullPasses(t *testing.T) {
	realGit := writeFakeGit(t, "#!/bin/sh\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "grep", "-n", "missing"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimGoTestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		b.WriteString("=== RUN   TestFeature")
		b.WriteString(strings.Repeat("0", len("60")-len("1")))
		b.WriteString("1\n")
		b.WriteString("--- PASS: TestFeature")
		b.WriteString(strings.Repeat("0", len("60")-len("1")))
		b.WriteString("1 (0.00s)\n")
	}
	b.WriteString("PASS\nok  \tgithub.com/example/project/internal/feature\t0.123s\n")
	realGo := writeFakeCommand(t, "go", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=go", "--real-bin=" + realGo, "--", "test", "-v", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[go test] ok - 60 passed") || !strings.Contains(got, "github.com/example/project/internal/feature") {
		t.Fatalf("unexpected compacted go test stdout=%q", got)
	}
	if strings.Contains(got, "=== RUN") || strings.Contains(got, "--- PASS:") {
		t.Fatalf("go test command-output-first should elide redundant pass roll-call: %q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:go] go test -v ./...") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimGoBuildEmptyFullPasses(t *testing.T) {
	realGo := writeFakeCommand(t, "go", "#!/bin/sh\nexit 0\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=go", "--real-bin=" + realGo, "--", "build", "./cmd/slimference"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimNpmRunTestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	b.WriteString("bun test v1.3.14 (0d9b296a)\n\nsession.test.ts:\n")
	for i := 1; i <= 42; i++ {
		b.WriteString("(pass) sample_session.jsonl > case ")
		if i < 10 {
			b.WriteString("00")
		} else if i < 100 {
			b.WriteString("0")
		}
		b.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		b.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		b.WriteString(" [0.01ms]\n")
	}
	b.WriteString("\n 42 pass\n 0 fail\n 50 expect() calls\nRan 42 tests across 2 files. [3.01s]\n")
	realNpm := writeFakeCommand(t, "npm", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npm", "--real-bin=" + realNpm, "--", "--silent", "run", "test"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[bun test] ok - 42 passed") || !strings.Contains(got, "Ran 42 tests across 2 files.") {
		t.Fatalf("unexpected compacted npm test stdout=%q", got)
	}
	if strings.Contains(got, "case 001") {
		t.Fatalf("npm test command-output-first should elide redundant pass roll-call: %q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:npm] npm --silent run test") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimPackageBuildEmptyFullPasses(t *testing.T) {
	realPnpm := writeFakeCommand(t, "pnpm", "#!/bin/sh\nexit 0\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=pnpm", "--real-bin=" + realPnpm, "--", "run", "build"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimNpxNextBuildCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realNpx := writeFakeCommand(t, "npx", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstNextBuildFixture()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npx", "--real-bin=" + realNpx, "--", "-y", "next", "build"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[next build] ok\n" {
		t.Fatalf("unexpected compacted npx next stdout=%q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:npx] npx -y next build") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimDirectVitestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realVitest := writeFakeCommand(t, "vitest", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstJSTestFixture(36)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=vitest", "--real-bin=" + realVitest, "--", "run"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[vitest] ok - 36 passed") || strings.Contains(got, "renders op 035") {
		t.Fatalf("unexpected compacted vitest stdout=%q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:vitest] vitest run") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimMavenBuildCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realMvn := writeFakeCommand(t, "mvn", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstMavenFixture(24)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=mvn", "--real-bin=" + realMvn, "--", "test"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[mvn] ok (Tests run: 42, Failures: 0, Errors: 0, Skipped: 0)\n" {
		t.Fatalf("unexpected compacted maven stdout=%q", got)
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand command-output-first archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("[INFO] BUILD SUCCESS")) || !bytes.Contains(raw, []byte("Tests run: 42")) {
		t.Fatalf("archive did not preserve Maven raw output: %q", raw)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:mvn] mvn test") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimArchiveUnavailableFullPasses(t *testing.T) {
	oldHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	t.Cleanup(func() { osUserHomeDir = oldHome })

	dir := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	realMvn := filepath.Join(dir, "mvn")
	raw := commandOutputFirstMavenFixture(24)
	if err := os.WriteFile(realMvn, []byte("#!/bin/sh\ncat <<'EOF'\n"+raw+"EOF\n"), 0755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=mvn", "--real-bin=" + realMvn, "--", "test"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != raw {
		t.Fatalf("archive-unavailable path must full-pass raw stdout\ngot=%q\nwant=%q", got, raw)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestCommandOutputFirstShimGradleBuildCompacts(t *testing.T) {
	realGradle := writeFakeCommand(t, "gradle", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstGradleBuildFixture(18)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=gradle", "--real-bin=" + realGradle, "--", "build", "--parallel"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[gradle build] ok (18 actionable tasks: 18 executed)\n" {
		t.Fatalf("unexpected compacted gradle stdout=%q", got)
	}
}

func TestCommandOutputFirstShimNpxEsbuildCompacts(t *testing.T) {
	var build strings.Builder
	for i := 0; i < 40; i++ {
		build.WriteString("dist/chunk")
		build.WriteString(strconv.Itoa(i))
		build.WriteString(".js 12.3 kb\n")
	}
	build.WriteString("Done in 10ms\n")
	realNpx := writeFakeCommand(t, "npx", "#!/bin/sh\ncat <<'EOF'\n"+build.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npx", "--real-bin=" + realNpx, "--", "-y", "esbuild", "src/index.ts", "--bundle"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[esbuild] ok\n" {
		t.Fatalf("unexpected compacted esbuild stdout=%q", got)
	}
}

func TestCommandOutputFirstShimNpxVitestCompacts(t *testing.T) {
	realNpx := writeFakeCommand(t, "npx", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstJSTestFixture(24)+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npx", "--real-bin=" + realNpx, "--", "-y", "vitest", "run"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[vitest] ok - 24 passed") || strings.Contains(got, "renders op 023") {
		t.Fatalf("unexpected compacted npx vitest stdout=%q", got)
	}
}

func TestCommandOutputFirstShimDirectTestRunnerNonzeroFullPasses(t *testing.T) {
	realJest := writeFakeCommand(t, "jest", `#!/bin/sh
printf 'FAIL src/app.test.ts\nTests: 1 failed, 1 total\n'
printf 'runner diagnostic\n' >&2
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=jest", "--real-bin=" + realJest, "--", "--runInBand"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if got := stdout.String(); got != "FAIL src/app.test.ts\nTests: 1 failed, 1 total\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := stderr.String(); got != "runner diagnostic\n" {
		t.Fatalf("stderr=%q", got)
	}
}

func TestCommandOutputFirstShimMakeCompacts(t *testing.T) {
	var build strings.Builder
	build.WriteString("make[1]: Entering directory '/repo/build'\n")
	build.WriteString("Consolidate compiler generated dependencies of target app\n")
	for i := 0; i < 24; i++ {
		build.WriteString("[ 50%] Building CXX object src/CMakeFiles/app.dir/generated/object.cpp.o\n")
	}
	build.WriteString("[100%] Linking CXX executable app\n")
	build.WriteString("[100%] Built target app\n")
	build.WriteString("make[1]: Leaving directory '/repo/build'\n")
	realMake := writeFakeCommand(t, "make", "#!/bin/sh\ncat <<'EOF'\n"+build.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=make", "--real-bin=" + realMake, "--", "-j8"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("make rc=%d stderr=%q", rc, stderr.String())
	}
	if got := commandOutputFirstVisibleOutput(stdout.String()); got != "[make] ok\n" {
		t.Fatalf("unexpected make compacted stdout=%q", got)
	}
}

func commandOutputFirstJSTestFixture(count int) string {
	var out strings.Builder
	out.WriteString("PASS src/app.test.ts\n")
	for i := 0; i < count; i++ {
		out.WriteString("  \u2713 renders op ")
		out.WriteString(strings.Repeat("x", 3))
		out.WriteString(" (2 ms)\n")
	}
	out.WriteString("\nTests: ")
	for _, ch := range strconv.Itoa(count) {
		out.WriteRune(ch)
	}
	out.WriteString(" passed, ")
	for _, ch := range strconv.Itoa(count) {
		out.WriteRune(ch)
	}
	out.WriteString(" total\nTime: 1.2 s\n")
	return out.String()
}

func commandOutputFirstMavenFixture(modules int) string {
	var out strings.Builder
	out.WriteString("[INFO] Scanning for projects...\n")
	out.WriteString("[INFO] \n")
	out.WriteString("[INFO] -----------------------< com.example:demo >------------------------\n")
	out.WriteString("[INFO] Building demo 1.0.0\n")
	out.WriteString("[INFO] --------------------------------[ jar ]---------------------------------\n")
	for i := 0; i < modules; i++ {
		out.WriteString("[INFO] --- maven-resources-plugin:3.3.1:resources (default-resources-")
		out.WriteString(strconv.Itoa(i))
		out.WriteString(") @ demo ---\n")
		out.WriteString("[INFO] Copying 1 resources from src/main/resources to target/classes\n")
	}
	out.WriteString("[INFO] --- maven-compiler-plugin:3.13.0:compile (default-compile) @ demo ---\n")
	out.WriteString("[INFO] Changes detected - recompiling the module!\n")
	out.WriteString("[INFO] Compiling 3 source files with javac [debug target 21] to target/classes\n")
	out.WriteString("[INFO] --- maven-surefire-plugin:3.2.5:test (default-test) @ demo ---\n")
	out.WriteString("[INFO] Running com.example.DemoTest\n")
	out.WriteString("[INFO] Tests run: 42, Failures: 0, Errors: 0, Skipped: 0\n")
	out.WriteString("[INFO] --- maven-jar-plugin:3.4.1:jar (default-jar) @ demo ---\n")
	out.WriteString("[INFO] Building jar: /repo/target/demo.jar\n")
	out.WriteString("[INFO] ------------------------------------------------------------------------\n")
	out.WriteString("[INFO] BUILD SUCCESS\n")
	out.WriteString("[INFO] ------------------------------------------------------------------------\n")
	out.WriteString("[INFO] Total time:  4.123 s\n")
	out.WriteString("[INFO] Finished at: 2026-06-20T01:02:03Z\n")
	out.WriteString("[INFO] ------------------------------------------------------------------------\n")
	return out.String()
}

func commandOutputFirstGradleBuildFixture(tasks int) string {
	var out strings.Builder
	out.WriteString("Starting a Gradle Daemon, 1 busy Daemon could not be reused, use --status for details\n")
	for i := 0; i < tasks; i++ {
		out.WriteString("> Task :module")
		out.WriteString(strconv.Itoa(i))
		out.WriteString(":compileJava\n")
	}
	out.WriteString("BUILD SUCCESSFUL in 4s\n")
	out.WriteString(strconv.Itoa(tasks))
	out.WriteString(" actionable tasks: ")
	out.WriteString(strconv.Itoa(tasks))
	out.WriteString(" executed\n")
	return out.String()
}

func TestCommandOutputFirstNpxToolEdges(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantTool string
		wantArgs []string
		wantOK   bool
	}{
		{name: "yes next", args: []string{"--yes", "next", "build"}, wantTool: "next", wantArgs: []string{"build"}, wantOK: true},
		{name: "package value", args: []string{"--package", "next", "next", "build"}, wantTool: "next", wantArgs: []string{"build"}, wantOK: true},
		{name: "package equals", args: []string{"--package=next", "--", "next", "build"}, wantTool: "next", wantArgs: []string{"build"}, wantOK: true},
		{name: "missing package value", args: []string{"--package"}, wantOK: false},
		{name: "unknown option", args: []string{"--call", "next build"}, wantOK: false},
		{name: "empty", args: []string{""}, wantOK: false},
		{name: "separator without tool", args: []string{"--"}, wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool, toolArgs, ok := commandOutputFirstNpxTool(tc.args)
			if ok != tc.wantOK || tool != tc.wantTool || strings.Join(toolArgs, "\x00") != strings.Join(tc.wantArgs, "\x00") {
				t.Fatalf("npx tool args=%v got tool=%q args=%v ok=%v want tool=%q args=%v ok=%v", tc.args, tool, toolArgs, ok, tc.wantTool, tc.wantArgs, tc.wantOK)
			}
		})
	}
}

func TestCommandOutputFirstShimPreCommitAndPrettierCompact(t *testing.T) {
	var hooks strings.Builder
	for i := 0; i < 20; i++ {
		hooks.WriteString("Hook check...............................................................Passed\n")
	}
	realPreCommit := writeFakeCommand(t, "pre-commit", "#!/bin/sh\ncat <<'EOF'\n"+hooks.String()+"EOF\n")
	var lintStdout, lintStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=pre-commit", "--real-bin=" + realPreCommit, "--", "run", "--all-files"}, &bytes.Buffer{}, &lintStdout, &lintStderr)
	if rc != 0 {
		t.Fatalf("pre-commit rc=%d stderr=%q", rc, lintStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(lintStdout.String()); got != "[pre-commit] ok (20 hooks passed)\n" {
		t.Fatalf("unexpected pre-commit compacted stdout=%q", got)
	}

	realPrettier := writeFakeCommand(t, "prettier", "#!/bin/sh\ncat <<'EOF'\nChecking formatting...\nAll matched files use Prettier code style!\nEOF\n")
	var fmtStdout, fmtStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=prettier", "--real-bin=" + realPrettier, "--", "--check", "."}, &bytes.Buffer{}, &fmtStdout, &fmtStderr)
	if rc != 0 {
		t.Fatalf("prettier rc=%d stderr=%q", rc, fmtStderr.String())
	}
	if got := fmtStdout.String(); got != "Checking formatting...\nAll matched files use Prettier code style!\n" {
		t.Fatalf("small prettier output should full-pass after archive overhead, got %q", got)
	}
	if uri := commandOutputFirstArchiveURI(fmtStdout.String()); uri != "" {
		t.Fatalf("small prettier full-pass must not archive: %q", fmtStdout.String())
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroDiagnosticsCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 60; i++ {
		diagnostics.WriteString("internal/app/app.go:22:7: this value of err is never used (SA4006)\n")
	}
	realStaticcheck := writeFakeCommand(t, "staticcheck", "#!/bin/sh\ncat <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=staticcheck", "--real-bin=" + realStaticcheck, "--", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"[staticcheck] FAILED (60 diagnostics)",
		"internal/app/app.go:22:7: this value of err is never used (SA4006) (repeated 60 times)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused lint compact output missing %q in %q", want, got)
		}
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand focused lint archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("SA4006")) || bytes.Count(raw, []byte("this value of err is never used")) != 60 {
		t.Fatalf("archive did not preserve focused lint raw output: %q", raw)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:staticcheck] staticcheck ./...") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroStderrDiagnosticsCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var diagnostics strings.Builder
	for i := 0; i < 70; i++ {
		diagnostics.WriteString("internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive)\n")
	}
	realGolangci := writeFakeCommand(t, "golangci-lint", "#!/bin/sh\ncat >&2 <<'EOF'\n"+diagnostics.String()+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=golangci-lint", "--real-bin=" + realGolangci, "--", "run", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	got := commandOutputFirstVisibleOutput(stderr.String())
	for _, want := range []string{
		"[golangci-lint] FAILED (70 diagnostics)",
		"internal/app/app.go:10:2: unused-parameter: parameter ctx seems to be unused, consider removing or renaming it as _ (revive) (repeated 70 times)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused lint stderr compact output missing %q in %q", want, got)
		}
	}
	if !strings.Contains(stderr.String(), "stream=stderr") {
		t.Fatalf("stderr archive marker must preserve stream distinction: %q", stderr.String())
	}
	uri := commandOutputFirstArchiveURI(stderr.String())
	if uri == "" {
		t.Fatalf("missing command-output-first archive marker in %q", stderr.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand focused lint stderr archive: %v", err)
	}
	if !bytes.Contains(raw, []byte("unused-parameter")) || bytes.Count(raw, []byte("parameter ctx seems to be unused")) != 70 {
		t.Fatalf("archive did not preserve focused lint raw stderr: %q", raw)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:golangci-lint] golangci-lint run ./...") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroMixedStdoutStderrFullPasses(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realStaticcheck := writeFakeCommand(t, "staticcheck", `#!/bin/sh
printf 'internal/app/app.go:22:7: this value of err is never used (SA4006)\n'
printf 'warning: matched no packages\n' >&2
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=staticcheck", "--real-bin=" + realStaticcheck, "--", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if got := stdout.String(); got != "internal/app/app.go:22:7: this value of err is never used (SA4006)\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := stderr.String(); got != "warning: matched no packages\n" {
		t.Fatalf("stderr=%q", got)
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("stderr full-pass must not archive: %q", stdout.String())
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if run, ok, err := filter.LastFilterRun(db); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("mixed stdout/stderr full-pass must not record accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroUnknownLineFullPasses(t *testing.T) {
	realStaticcheck := writeFakeCommand(t, "staticcheck", `#!/bin/sh
cat <<'EOF'
warning: matched no packages
internal/app/app.go:22:7: this value of err is never used (SA4006)
EOF
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=staticcheck", "--real-bin=" + realStaticcheck, "--", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	want := "warning: matched no packages\ninternal/app/app.go:22:7: this value of err is never used (SA4006)\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout=%q want=%q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("unknown-line full-pass must not archive: %q", stdout.String())
	}
}

func TestCommandOutputFirstShimFocusedLintNonzeroStderrUnknownLineFullPasses(t *testing.T) {
	realGolangci := writeFakeCommand(t, "golangci-lint", `#!/bin/sh
cat >&2 <<'EOF'
level=info msg="golangci-lint has version 2.1.0"
internal/app/app.go:10:2: unused-parameter: bad (revive)
EOF
exit 1
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=golangci-lint", "--real-bin=" + realGolangci, "--", "run", "./..."}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	want := "level=info msg=\"golangci-lint has version 2.1.0\"\ninternal/app/app.go:10:2: unused-parameter: bad (revive)\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr=%q want=%q", got, want)
	}
	if uri := commandOutputFirstArchiveURI(stderr.String()); uri != "" {
		t.Fatalf("unknown-line stderr full-pass must not archive: %q", stderr.String())
	}
}

func TestCommandOutputFirstShimEslintStylishNonzeroStdoutCompactWithArchive(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstEslintStylishFixture("src/app.js", 45)+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src", "--format", "stylish"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	got := commandOutputFirstVisibleOutput(stdout.String())
	for _, want := range []string{
		"[eslint] FINDINGS (90 problems: 45 errors, 45 warnings in 1 file)",
		"src/app.js",
		"2:1 warning [no-console] Unexpected console statement",
		"2:20 error [eqeqeq] Expected '===' and instead saw '=='",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("eslint compact output missing %q in %q", want, got)
		}
	}
	uri := commandOutputFirstArchiveURI(stdout.String())
	if uri == "" {
		t.Fatalf("missing eslint archive marker in %q", stdout.String())
	}
	home, err := osUserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), uri)
	if err != nil {
		t.Fatalf("expand eslint archive: %v", err)
	}
	if bytes.Count(raw, []byte("Unexpected console statement")) != 45 ||
		bytes.Count(raw, []byte("Expected '===' and instead saw '=='")) != 45 {
		t.Fatalf("archive did not preserve eslint raw output: %q", raw)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:eslint] eslint src --format stylish") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimEslintStylishNonzeroStderrCompactWithArchive(t *testing.T) {
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\ncat >&2 <<'EOF'\n"+commandOutputFirstEslintStylishFixture("src/app.js", 40)+"EOF\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if !strings.Contains(commandOutputFirstVisibleOutput(stderr.String()), "[eslint] FINDINGS (80 problems: 40 errors, 40 warnings in 1 file)") {
		t.Fatalf("eslint stderr compact output=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "stream=stderr") {
		t.Fatalf("eslint stderr archive marker must preserve stream: %q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stderr.String()); uri == "" {
		t.Fatalf("missing eslint stderr archive marker in %q", stderr.String())
	}
}

func TestCommandOutputFirstShimEslintStylishMixedStdoutStderrFullPasses(t *testing.T) {
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\ncat <<'EOF'\n"+commandOutputFirstEslintStylishFixture("src/app.js", 20)+"EOF\nprintf 'warning: config ignored\\n' >&2\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "Unexpected console statement") {
		t.Fatalf("stdout full-pass lost eslint output: %q", stdout.String())
	}
	if got := stderr.String(); got != "warning: config ignored\n" {
		t.Fatalf("stderr=%q", got)
	}
	if uri := commandOutputFirstArchiveURI(stdout.String() + stderr.String()); uri != "" {
		t.Fatalf("mixed eslint full-pass must not archive: %q %q", stdout.String(), stderr.String())
	}
}

func TestCommandOutputFirstShimEslintStylishAnsiFullPasses(t *testing.T) {
	realEslint := writeFakeCommand(t, "eslint", "#!/bin/sh\nprintf '\\033[31msrc/app.js\\033[0m\\n  1:1  error  bad  no-console\\n\\n\\342\\234\\226 1 problem (1 error, 0 warnings)\\n'\nexit 1\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=eslint", "--real-bin=" + realEslint, "--", "src"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "\x1b[31msrc/app.js\x1b[0m") {
		t.Fatalf("ansi eslint output should full-pass, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if uri := commandOutputFirstArchiveURI(stdout.String()); uri != "" {
		t.Fatalf("ansi eslint full-pass must not archive: %q", stdout.String())
	}
}

func commandOutputFirstEslintStylishFixture(file string, count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteString("\n")
		b.WriteString(file)
		b.WriteString("\n")
		b.WriteString("  2:1   warning  Unexpected console statement         no-console\n")
		b.WriteString("  2:20  error    Expected '===' and instead saw '=='  eqeqeq\n")
	}
	total := count * 2
	b.WriteString("\n\u2716 ")
	b.WriteString(strconv.Itoa(total))
	b.WriteString(" problems (")
	b.WriteString(strconv.Itoa(count))
	b.WriteString(" errors, ")
	b.WriteString(strconv.Itoa(count))
	b.WriteString(" warnings)\n")
	return b.String()
}

func TestCommandOutputFirstShimPackageLintAndFormatCompact(t *testing.T) {
	realNpm := writeFakeCommand(t, "npm", "#!/bin/sh\ncat <<'EOF'\n> app@1.0.0 lint /repo\n> pre-commit run --all-files\nHook one................................................................Passed\nHook two................................................................Passed\nHook three..............................................................Passed\nHook four...............................................................Passed\nHook five...............................................................Passed\nHook six................................................................Passed\nHook seven..............................................................Passed\nHook eight..............................................................Passed\nHook nine...............................................................Passed\nHook ten................................................................Passed\nHook eleven.............................................................Passed\nHook twelve.............................................................Passed\nEOF\n")
	var lintStdout, lintStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=npm", "--real-bin=" + realNpm, "--", "run", "lint"}, &bytes.Buffer{}, &lintStdout, &lintStderr)
	if rc != 0 {
		t.Fatalf("npm lint rc=%d stderr=%q", rc, lintStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(lintStdout.String()); got != "[pre-commit] ok (12 hooks passed)\n" {
		t.Fatalf("unexpected npm lint compacted stdout=%q", got)
	}

	realYarn := writeFakeCommand(t, "yarn", "#!/bin/sh\ncat <<'EOF'\n> app@1.0.0 format:check /repo\n> prettier --check .\nChecking formatting...\nAll matched files use Prettier code style!\nEOF\n")
	var fmtStdout, fmtStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=yarn", "--real-bin=" + realYarn, "--", "run", "format:check"}, &bytes.Buffer{}, &fmtStdout, &fmtStderr)
	if rc != 0 {
		t.Fatalf("yarn format rc=%d stderr=%q", rc, fmtStderr.String())
	}
	if got := fmtStdout.String(); got != "> app@1.0.0 format:check /repo\n> prettier --check .\nChecking formatting...\nAll matched files use Prettier code style!\n" {
		t.Fatalf("small yarn format output should full-pass after archive overhead, got %q", got)
	}
	if uri := commandOutputFirstArchiveURI(fmtStdout.String()); uri != "" {
		t.Fatalf("small yarn format full-pass must not archive: %q", fmtStdout.String())
	}
}

func commandOutputFirstNextBuildFixture() string {
	var b strings.Builder
	b.WriteString("Next.js 15.3.0\n")
	b.WriteString("Creating an optimized production build ...\n")
	b.WriteString("Compiled successfully in 2.8s\n")
	b.WriteString("Linting and checking validity of types ...\n")
	b.WriteString("Collecting page data ...\n")
	b.WriteString("Generating static pages (0/8) ...\n")
	b.WriteString("Generating static pages (4/8) ...\n")
	b.WriteString("Generating static pages (8/8) ...\n")
	b.WriteString("Finalizing page optimization ...\n")
	b.WriteString("Collecting build traces ...\n")
	b.WriteString("Route (app)                              Size     First Load JS\n")
	for i := 0; i < 24; i++ {
		b.WriteString("/dashboard/section                      2.00 kB        110 kB\n")
	}
	return b.String()
}

func TestCommandOutputFirstShimCargoTestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	b.WriteString("running 42 tests\n")
	for i := 1; i <= 42; i++ {
		b.WriteString("test suite::case_")
		if i < 10 {
			b.WriteString("0")
		}
		b.WriteString(strings.TrimSpace(string(rune('0' + i/10))))
		b.WriteString(strings.TrimSpace(string(rune('0' + i%10))))
		b.WriteString(" ... ok\n")
	}
	b.WriteString("\ntest result: ok. 42 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s\n")
	realCargo := writeFakeCommand(t, "cargo", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargo, "--", "test", "--", "--nocapture"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[cargo test] ok - 42 passed") || !strings.Contains(got, "test result: ok. 42 passed") {
		t.Fatalf("unexpected compacted cargo test stdout=%q", got)
	}
	if strings.Contains(got, "suite::case_01") {
		t.Fatalf("cargo test command-output-first should elide redundant pass roll-call: %q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:cargo] cargo test -- --nocapture") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimCargoBuildAndClippyCompact(t *testing.T) {
	var build strings.Builder
	for i := 0; i < 40; i++ {
		build.WriteString("    Compiling slimtest v0.1.0 (/repo/crates/slimtest)\n")
	}
	build.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	realCargoBuild := writeFakeCommand(t, "cargo", "#!/bin/sh\ncat <<'EOF'\n"+build.String()+"EOF\n")
	var buildStdout, buildStderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargoBuild, "--", "build", "--workspace"}, &bytes.Buffer{}, &buildStdout, &buildStderr)
	if rc != 0 {
		t.Fatalf("cargo build rc=%d stderr=%q", rc, buildStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(buildStdout.String()); got != "[cargo build] ok\n" {
		t.Fatalf("cargo build compacted stdout=%q", got)
	}

	var clippy strings.Builder
	for i := 0; i < 40; i++ {
		clippy.WriteString("    Checking slimtest v0.1.0 (/repo/crates/slimtest)\n")
	}
	clippy.WriteString("    Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.23s\n")
	realCargoClippy := writeFakeCommand(t, "cargo", "#!/bin/sh\ncat <<'EOF'\n"+clippy.String()+"EOF\n")
	var clippyStdout, clippyStderr bytes.Buffer
	rc = runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargoClippy, "--", "clippy", "--all-targets"}, &bytes.Buffer{}, &clippyStdout, &clippyStderr)
	if rc != 0 {
		t.Fatalf("cargo clippy rc=%d stderr=%q", rc, clippyStderr.String())
	}
	if got := commandOutputFirstVisibleOutput(clippyStdout.String()); got != "[cargo clippy] ok\n" {
		t.Fatalf("cargo clippy compacted stdout=%q", got)
	}
}

func TestCommandOutputFirstShimCargoStderrFullPasses(t *testing.T) {
	realCargo := writeFakeCommand(t, "cargo", "#!/bin/sh\nprintf '    Checking slimtest v0.1.0\\n'\nprintf 'warning: diagnostic\\n' >&2\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=cargo", "--real-bin=" + realCargo, "--", "check"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if got := stdout.String(); got != "    Checking slimtest v0.1.0\n" {
		t.Fatalf("stdout passthrough=%q", got)
	}
	if got := stderr.String(); got != "warning: diagnostic\n" {
		t.Fatalf("stderr passthrough=%q", got)
	}
}

func TestCommandOutputFirstShimPythonUnittestCompactsWithAccounting(t *testing.T) {
	dbPath := withCommandOutputFirstRecordingDB(t)
	var b strings.Builder
	for i := 1; i <= 400; i++ {
		b.WriteByte('.')
		if i%40 == 0 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n----------------------------------------------------------------------\n")
	b.WriteString("Ran 400 tests in 0.321s\n\nOK\n")
	realPython := writeFakeCommand(t, "python3", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=python3", "--real-bin=" + realPython, "--", "-u", "-m", "unittest"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if got = commandOutputFirstVisibleOutput(got); got != "[python -m unittest] ok (Ran 400 tests in 0.321s; OK)\n" {
		t.Fatalf("unexpected compacted unittest stdout=%q", got)
	}
	db, err := filter.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, ok, err := filter.LastFilterRun(db)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command-output-first accounting row")
	}
	if !strings.Contains(run.Command, "[command-output-first:python3] python3 -u -m unittest") {
		t.Fatalf("command label=%q", run.Command)
	}
	if run.InputTokens <= run.OutputTokens || run.SavingsPct <= 0 {
		t.Fatalf("non-positive accounting row: %+v", run)
	}
}

func TestCommandOutputFirstShimPytestCompacts(t *testing.T) {
	var b strings.Builder
	b.WriteString("============================= test session starts ==============================\n")
	for i := 0; i < 80; i++ {
		b.WriteString("tests/test_alpha.py::test_op PASSED                                  [ 10%]\n")
	}
	b.WriteString("============================== 80 passed in 0.42s ===============================\n")
	realPytest := writeFakeCommand(t, "pytest", "#!/bin/sh\ncat <<'EOF'\n"+b.String()+"EOF\n")
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=pytest", "--real-bin=" + realPytest, "--", "-v"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "[pytest] ok - 80 passed") || strings.Contains(got, "test_alpha.py::test_op") {
		t.Fatalf("unexpected compacted pytest stdout=%q", got)
	}
}

func TestCommandOutputFirstEnvInjectedOnlyForScopedProxiedRun(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	binDir := t.TempDir()
	self := filepath.Join(binDir, "slimference")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitBin := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", binDir)

	command := codexEnvCommand("proxied-wss", "127.0.0.1", "8990", []string{"exec", "hi"})
	got, cleanup := maybeApplyCommandOutputFirstEnv("proxied-wss", command)
	defer cleanup()
	joined := strings.Join(got, "\x00")
	if !strings.Contains(joined, "\x00"+commandOutputFirstActiveEnv+"=1\x00") {
		t.Fatalf("missing active env in %#v", got)
	}
	if !strings.Contains(joined, "\x00BASH_ENV=") {
		t.Fatalf("missing BASH_ENV in %#v", got)
	}
	pathValue := envValueInCommand(t, got, "PATH")
	if !strings.Contains(pathValue, string(os.PathListSeparator)+binDir) {
		t.Fatalf("PATH did not preserve original path: %q", pathValue)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pathValue, string(os.PathListSeparator))[0], "git")); err != nil {
		t.Fatalf("git shim missing: %v", err)
	}
	bashEnv := envValueInCommand(t, got, "BASH_ENV")
	bashEnvContent, err := os.ReadFile(bashEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bashEnvContent), "export PATH=") || !strings.Contains(string(bashEnvContent), "${PATH:+:$PATH}") {
		t.Fatalf("BASH_ENV does not re-prepend shim PATH: %q", bashEnvContent)
	}
}

func TestProxyRunCodexAppliesCommandOutputFirstEnvForScopedRun(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	binDir := t.TempDir()
	self := filepath.Join(binDir, "slimference")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitBin := filepath.Join(binDir, "git")
	if err := os.WriteFile(gitBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", binDir)

	env, _, stderr, _, _, _ := newProxyEnv(t)
	var gotName string
	var gotArgs []string
	env.RunCommand = func(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}
	if rc := proxyRun([]string{"run", "codex", "--proxied-wss", "--", "exec", "hi"}, env); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if gotName != "env" {
		t.Fatalf("runner name=%q", gotName)
	}
	joined := strings.Join(gotArgs, "\x00")
	if !strings.Contains(joined, commandOutputFirstActiveEnv+"=1") || !strings.Contains(joined, "BASH_ENV=") {
		t.Fatalf("scoped run missing command-output-first env: %#v", gotArgs)
	}
	if !strings.Contains(joined, "\x00codex\x00") {
		t.Fatalf("codex command missing: %#v", gotArgs)
	}
}

func TestCommandOutputFirstEnvNotInjectedForDirectRun(t *testing.T) {
	command := codexEnvCommand("direct", "127.0.0.1", "8990", []string{"exec", "hi"})
	got, cleanup := maybeApplyCommandOutputFirstEnv("direct", command)
	defer cleanup()
	joined := strings.Join(got, "\x00")
	if strings.Contains(joined, commandOutputFirstActiveEnv+"=1") || strings.Contains(joined, "BASH_ENV=") {
		t.Fatalf("direct mode must not get command-output-first env: %#v", got)
	}
}

func TestCommandOutputFirstEnvDisabledAndPrepareFailure(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	command := codexEnvCommand("proxied-wss", "127.0.0.1", "8990", []string{"exec", "hi"})

	t.Setenv(commandOutputFirstDisableEnv, "1")
	got, cleanup := maybeApplyCommandOutputFirstEnv("proxied-wss", command)
	cleanup()
	if strings.Contains(strings.Join(got, "\x00"), commandOutputFirstActiveEnv+"=1") {
		t.Fatalf("disabled env still injected: %#v", got)
	}

	t.Setenv(commandOutputFirstDisableEnv, "0")
	osExecutable = func() (string, error) { return "", errors.New("missing executable") }
	got, cleanup = maybeApplyCommandOutputFirstEnv("proxied-wss", command)
	cleanup()
	if strings.Contains(strings.Join(got, "\x00"), commandOutputFirstActiveEnv+"=1") {
		t.Fatalf("prepare failure still injected: %#v", got)
	}
}

func TestPrepareCommandOutputFirstEnvNoGit(t *testing.T) {
	oldExecutable := osExecutable
	t.Cleanup(func() { osExecutable = oldExecutable })
	self := filepath.Join(t.TempDir(), "slimference")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	osExecutable = func() (string, error) { return self, nil }
	t.Setenv("PATH", t.TempDir())
	env, cleanup, ok := prepareCommandOutputFirstEnv()
	defer cleanup()
	if ok || len(env) != 0 {
		t.Fatalf("expected no env without git, got ok=%v env=%#v", ok, env)
	}
}

func TestWriteCommandOutputFirstShimWriteError(t *testing.T) {
	err := writeCommandOutputFirstShim(filepath.Join(t.TempDir(), "missing", "git"), "/bin/slimference", "/usr/bin/git", "git")
	if err == nil {
		t.Fatal("expected write error for missing parent directory")
	}
}

func TestInsertEnvAssignmentsBeforeUtilityEdges(t *testing.T) {
	empty := insertEnvAssignmentsBeforeUtility(nil, "A=1")
	if len(empty) != 0 {
		t.Fatalf("empty command changed to %#v", empty)
	}
	unchanged := insertEnvAssignmentsBeforeUtility([]string{"env", "codex"})
	if strings.Join(unchanged, " ") != "env codex" {
		t.Fatalf("unexpected unchanged command %#v", unchanged)
	}
	got := insertEnvAssignmentsBeforeUtility([]string{"env", "-u", "HTTP_PROXY", "NO_PROXY=*", "codex"}, "A=1")
	if strings.Join(got, " ") != "env -u HTTP_PROXY NO_PROXY=* A=1 codex" {
		t.Fatalf("unexpected insertion %#v", got)
	}
}

func TestHandleCommandOutputFirstShimUsesExitFn(t *testing.T) {
	oldExit := exitFn
	defer func() { exitFn = oldExit }()
	var gotCode int
	exitFn = func(code int) { gotCode = code }
	handleCommandOutputFirstShim([]string{"--bad"})
	if gotCode != 2 {
		t.Fatalf("exit code=%d", gotCode)
	}
}

func writeFakeGit(t *testing.T, script string) string {
	t.Helper()
	return writeFakeCommand(t, "git", script)
}

func writeFakeCommand(t *testing.T, name string, script string) string {
	t.Helper()
	withCommandOutputFirstArchiveHome(t)
	dir := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func envValueInCommand(t *testing.T, command []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, arg := range command {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	t.Fatalf("%s not found in %#v", key, command)
	return ""
}

func TestCommandOutputFirstShimRejectsBadArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git"}, &bytes.Buffer{}, &stdout, &stderr)
	if rc != 2 {
		t.Fatalf("rc=%d stderr=%q", rc, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestCommandOutputFirstShimRejectsUnknownArgAndMissingRequired(t *testing.T) {
	for _, args := range [][]string{
		{"--wat"},
		{"--command=git", "--"},
		{"--real-bin=/bin/git", "--"},
	} {
		var stdout, stderr bytes.Buffer
		rc := runCommandOutputFirstShim(args, &bytes.Buffer{}, &stdout, &stderr)
		if rc != 2 {
			t.Fatalf("args=%v rc=%d stderr=%q", args, rc, stderr.String())
		}
	}
}

func TestCommandOutputFirstShimNonZeroFullPassesWithoutRecoveryNoise(t *testing.T) {
	realGit := writeFakeGit(t, `#!/bin/sh
printf 'fatal: not a git repository\n' >&2
exit 128
`)
	var stdout, stderr bytes.Buffer
	rc := runCommandOutputFirstShim([]string{"--command=git", "--real-bin=" + realGit, "--", "status", "--short"}, io.Reader(&bytes.Buffer{}), &stdout, &stderr)
	if rc != 128 {
		t.Fatalf("rc=%d", rc)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if got := stderr.String(); got != "fatal: not a git repository\n" {
		t.Fatalf("stderr=%q", got)
	}
}

func TestCommandOutputFirstCompactRejectsWrongCommandAndUnknownSubcommand(t *testing.T) {
	if out, ok := compactCommandOutputFirst("rg", "/usr/bin/rg", []string{"TODO"}, nil, nil, 0); ok || out != nil {
		t.Fatalf("non-git compacted: out=%q ok=%v", out, ok)
	}
	if out, ok := compactCommandOutputFirst("git", "/usr/bin/git", []string{"show", "HEAD"}, []byte("commit x\n"), nil, 0); ok || out != nil {
		t.Fatalf("git show compacted: out=%q ok=%v", out, ok)
	}
	if out, ok := compactCommandOutputFirst("npm", "/usr/bin/npm", []string{"run", "dev"}, []byte("ready\n"), nil, 0); ok || out != nil {
		t.Fatalf("npm run dev compacted: out=%q ok=%v", out, ok)
	}
}

func TestCommandOutputFirstGitSubcommandGlobalOptionEdges(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"-C"}, want: ""},
		{args: []string{"-C", "/repo", "status"}, want: "status"},
		{args: []string{"--git-dir"}, want: ""},
		{args: []string{"--git-dir=/repo/.git", "diff", "--stat"}, want: "diff"},
		{args: []string{"-c", "core.quotePath=false", "ls-files"}, want: "ls-files"},
		{args: []string{"-cfoo=bar", "status"}, want: "status"},
		{args: []string{"--no-pager", "status"}, want: "status"},
		{args: []string{""}, want: ""},
	}
	for _, tc := range cases {
		if got := commandOutputFirstGitSubcommand(tc.args); got != tc.want {
			t.Fatalf("args=%v got=%q want=%q", tc.args, got, tc.want)
		}
	}
}

func TestCommandOutputFirstGoSubcommandGlobalOptionEdges(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"test", "./..."}, want: "test"},
		{args: []string{"-C", "/repo", "build", "./cmd/slimference"}, want: "build"},
		{args: []string{"-C"}, want: ""},
		{args: []string{"-C=/repo", "test"}, want: "test"},
		{args: []string{"-mod=mod", "test"}, want: "test"},
		{args: []string{"env"}, want: "env"},
		{args: []string{""}, want: ""},
	}
	for _, tc := range cases {
		if got := commandOutputFirstGoSubcommand(tc.args); got != tc.want {
			t.Fatalf("args=%v got=%q want=%q", tc.args, got, tc.want)
		}
	}
}

func TestCommandOutputFirstPathListAndWcEdges(t *testing.T) {
	pathListAllowed := []struct {
		command string
		args    []string
	}{
		{command: "rg", args: []string{"--files", "--hidden", "-g", "*.go", "internal"}},
		{command: "fd", args: []string{"--extension", "go", "internal"}},
		{command: "fdfind", args: []string{"-e", "go", "internal"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-type", "f"}},
	}
	for _, tc := range pathListAllowed {
		if !commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v should be captured", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "fd", args: []string{"--exec", "rm", "{}"}},
		{command: "find", args: []string{"internal", "-type", "f"}},
		{command: "find", args: []string{"internal", "-maxdepth", "9", "-type", "f"}},
		{command: "find", args: []string{"internal", "-maxdepth", "4", "-printf", "%p\n"}},
		{command: "wc", args: []string{"-l"}},
		{command: "wc", args: []string{"--files0-from=list"}},
		{command: "wc", args: []string{"-q", "file.go"}},
	}
	for _, tc := range denied {
		if commandOutputFirstAllowCapture(tc.command, tc.args) {
			t.Fatalf("%s %v must not be captured", tc.command, tc.args)
		}
	}
	for _, args := range [][]string{
		{"file.go"},
		{"-l", "file.go"},
		{"--lines", "--words", "file.go"},
		{"-lw", "--", "-leading-name.txt"},
	} {
		if !commandOutputFirstWcAllowed(args) {
			t.Fatalf("wc %v should be allowed", args)
		}
	}
}

func TestCommandOutputFirstCargoSubcommandEdges(t *testing.T) {
	cases := []struct {
		args    []string
		wantSub string
		wantOK  bool
	}{
		{args: []string{"test", "--workspace"}, wantSub: "test", wantOK: true},
		{args: []string{"+nightly", "nextest", "run"}, wantSub: "nextest", wantOK: true},
		{args: []string{"+nightly", "nextest", "list"}, wantSub: "nextest", wantOK: false},
		{args: []string{"--config", "build.jobs=1", "check"}, wantSub: "check", wantOK: true},
		{args: []string{"--config=build.jobs=1", "doc"}, wantSub: "doc", wantOK: true},
		{args: []string{"-Z", "unstable-options", "llvm-cov"}, wantSub: "llvm-cov", wantOK: true},
		{args: []string{"-Zunstable-options", "-v", "audit"}, wantSub: "audit", wantOK: true},
		{args: []string{"--config"}, wantSub: "", wantOK: false},
		{args: []string{"+stable"}, wantSub: "", wantOK: false},
		{args: []string{"install", "ripgrep"}, wantSub: "install", wantOK: false},
		{args: []string{""}, wantSub: "", wantOK: false},
	}
	for _, tc := range cases {
		if got := commandOutputFirstCargoSubcommand(tc.args); got != tc.wantSub {
			t.Fatalf("cargo args=%v sub=%q want %q", tc.args, got, tc.wantSub)
		}
		if got := commandOutputFirstCargoAllowed(tc.args); got != tc.wantOK {
			t.Fatalf("cargo args=%v allowed=%v want %v", tc.args, got, tc.wantOK)
		}
	}
}

func TestCommandOutputFirstPythonTestEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "pytest", args: []string{"-vv"}},
		{command: "py.test", args: []string{"tests/"}},
		{command: "python3", args: []string{"-u", "-m", "pytest"}},
		{command: "python", args: []string{"-W", "ignore", "-m", "unittest"}},
		{command: "python3", args: []string{"-X", "dev", "-m", "unittest"}},
		{command: "uv", args: []string{"--project", "app", "run", "pytest"}},
		{command: "uv", args: []string{"run", "python3", "-m", "pytest"}},
		{command: "poetry", args: []string{"run", "pytest"}},
		{command: "poetry", args: []string{"run", "python", "-m", "pytest"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstPythonTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should be allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "python3", args: []string{"script.py"}},
		{command: "python3", args: []string{"-m", "http.server"}},
		{command: "python3", args: []string{"-W"}},
		{command: "python3", args: []string{"-X", ""}},
		{command: "uv", args: []string{"run", "python", "script.py"}},
		{command: "uv", args: []string{"run"}},
		{command: "uv", args: []string{"pip", "install", "pytest"}},
		{command: "poetry", args: []string{"install"}},
		{command: "poetry", args: []string{"run", "python", "script.py"}},
		{command: "ruby", args: []string{"-e", "puts 1"}},
	}
	for _, tc := range denied {
		if commandOutputFirstPythonTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
		}
	}
	if got := commandOutputFirstPythonModule([]string{"-u", "-m", "pytest"}); got != "pytest" {
		t.Fatalf("python module=%q", got)
	}
}

func TestCommandOutputFirstDirectTestEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "karma", args: []string{"--config", "karma.conf.js", "start"}},
		{command: "playwright", args: []string{"--config", "playwright.config.ts", "test"}},
		{command: "cypress", args: []string{"--config", "video=false", "run"}},
		{command: "wdio", args: []string{"--config", "wdio.conf.ts", "run"}},
		{command: "turbo", args: []string{"test"}},
		{command: "turbo", args: []string{"run", "test", "--filter", "web"}},
		{command: "nox", args: []string{"--session=test"}},
		{command: "tox", args: []string{"--env=test"}},
		{command: "tox", args: []string{"-e=test"}},
		{command: "mill", args: []string{"test"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstDirectTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should be allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "karma", args: []string{"--config"}},
		{command: "playwright", args: []string{"--project", "", "test"}},
		{command: "cypress", args: []string{"--", "open"}},
		{command: "wdio", args: []string{"config"}},
		{command: "turbo", args: []string{"run", "--filter", "web", "build"}},
		{command: "turbo", args: []string{"prune"}},
		{command: "nox", args: []string{"--session"}},
		{command: "nox", args: []string{"--session=lint"}},
		{command: "tox", args: []string{"--env"}},
		{command: "tox", args: []string{"--env=lint"}},
		{command: "mill", args: []string{""}},
		{command: "mill", args: []string{"foo.compile"}},
		{command: "unknown", args: []string{"test"}},
	}
	for _, tc := range denied {
		if commandOutputFirstDirectTestAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
		}
	}

	if got := commandOutputFirstFirstNonOption([]string{"--config", "cfg", "--", "test"}); got != "test" {
		t.Fatalf("first non-option=%q", got)
	}
	if got := commandOutputFirstFirstNonOption(nil); got != "" {
		t.Fatalf("empty args first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{""}); got != "" {
		t.Fatalf("blank arg first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--"}); got != "" {
		t.Fatalf("bare separator first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--", "test"}); got != "test" {
		t.Fatalf("separator first command=%q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--config"}); got != "" {
		t.Fatalf("missing option value should return empty first command, got %q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--config", "", "test"}); got != "" {
		t.Fatalf("blank option value should return empty first command, got %q", got)
	}
	if got := commandOutputFirstFirstNonOption([]string{"--verbose", "test"}); got != "test" {
		t.Fatalf("flag before test first command=%q", got)
	}
}

func TestCommandOutputFirstDirectBuildEdges(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{command: "make", args: []string{"-j8"}},
		{command: "ninja", args: []string{"-C", "build"}},
		{command: "cmake", args: []string{"--build", "build"}},
		{command: "tsc", args: []string{"--noEmit"}},
		{command: "next", args: []string{"--turbo", "build"}},
		{command: "vite", args: []string{"--config", "vite.config.ts", "build"}},
		{command: "webpack", args: []string{"--mode", "production"}},
		{command: "webpack-cli", args: []string{"--mode", "production"}},
		{command: "tsup", args: nil},
		{command: "rollup", args: []string{"--config", "rollup.config.mjs"}},
		{command: "esbuild", args: []string{"src/index.ts", "--bundle=true"}},
		{command: "nx", args: []string{"--project", "web", "build"}},
		{command: "turbo", args: []string{"build"}},
		{command: "turbo", args: []string{"run", "build", "--filter", "web"}},
		{command: "mvn", args: []string{"--batch-mode", "package"}},
		{command: "mvnw", args: []string{"-q", "install"}},
		{command: "gradlew", args: []string{"build", "--parallel"}},
		{command: "moon", args: []string{"run", "build"}},
		{command: "moon", args: []string{"run", "web:build"}},
	}
	for _, tc := range allowed {
		if !commandOutputFirstDirectBuildAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v should be allowed", tc.command, tc.args)
		}
	}

	denied := []struct {
		command string
		args    []string
	}{
		{command: "make", args: []string{"dev"}},
		{command: "ninja", args: []string{"-t", "commands"}},
		{command: "cmake", args: []string{"--build", "build", "--target", "serve"}},
		{command: "tsc", args: []string{"-w"}},
		{command: "next", args: []string{"dev"}},
		{command: "next", args: nil},
		{command: "vite", args: []string{"--host", "127.0.0.1"}},
		{command: "webpack", args: []string{"serve"}},
		{command: "tsup", args: []string{"--watch"}},
		{command: "rspack", args: []string{"serve"}},
		{command: "parcel", args: []string{"watch", "src/index.html"}},
		{command: "rollup", args: []string{"--watch", "--config"}},
		{command: "rollup", args: nil},
		{command: "esbuild", args: []string{"src/index.ts"}},
		{command: "nx", args: []string{"build", "--watch"}},
		{command: "turbo", args: []string{"run", "--filter", "web", "dev"}},
		{command: "turbo", args: []string{"prune"}},
		{command: "mvn", args: nil},
		{command: "mvn", args: []string{"--batch-mode"}},
		{command: "mvn", args: []string{""}},
		{command: "mvn", args: []string{"release"}},
		{command: "gradle", args: []string{"build", "--continuous"}},
		{command: "meson", args: []string{"setup", "build"}},
		{command: "moon", args: []string{"run"}},
		{command: "moon", args: []string{"run", "web:test"}},
		{command: "unknown", args: []string{"build"}},
	}
	for _, tc := range denied {
		if commandOutputFirstDirectBuildAllowed(tc.command, tc.args) {
			t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
		}
	}
}

func TestCommandOutputFirstPackageScriptEdges(t *testing.T) {
	allowed := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "npm global flag test", command: "npm", args: []string{"--silent", "test"}},
		{name: "pnpm cwd run test", command: "pnpm", args: []string{"--dir", "app", "run", "test"}},
		{name: "yarn run option build", command: "yarn", args: []string{"run", "--silent", "build"}},
		{name: "npm inline prefix test", command: "npm", args: []string{"--prefix=app", "test"}},
		{name: "bun direct test", command: "bun", args: []string{"--cwd", "app", "test"}},
		{name: "bun run build", command: "bun", args: []string{"run", "build"}},
		{name: "npm run lint", command: "npm", args: []string{"run", "lint"}},
		{name: "pnpm run typecheck", command: "pnpm", args: []string{"run", "typecheck"}},
		{name: "yarn run format check", command: "yarn", args: []string{"run", "format:check"}},
	}
	for _, tc := range allowed {
		t.Run("allow "+tc.name, func(t *testing.T) {
			if !commandOutputFirstPackageScriptAllowed(tc.command, tc.args) {
				t.Fatalf("%s %v should be allowed", tc.command, tc.args)
			}
		})
	}

	denied := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "unknown command", command: "deno", args: []string{"test"}},
		{name: "npm run without script", command: "npm", args: []string{"run"}},
		{name: "npm empty args", command: "npm", args: []string{""}},
		{name: "pnpm missing option value", command: "pnpm", args: []string{"--dir"}},
		{name: "bun run test", command: "bun", args: []string{"run", "test"}},
		{name: "yarn run write format", command: "yarn", args: []string{"run", "format"}},
	}
	for _, tc := range denied {
		t.Run("deny "+tc.name, func(t *testing.T) {
			if commandOutputFirstPackageScriptAllowed(tc.command, tc.args) {
				t.Fatalf("%s %v must not be allowed", tc.command, tc.args)
			}
		})
	}
	if commandOutputFirstPackageScriptIsBuild("npm", []string{"test"}) {
		t.Fatal("npm test must not be classified as build")
	}
	if commandOutputFirstPackageScriptIsBuild("deno", []string{"run", "build"}) {
		t.Fatal("unknown package manager must not be classified as build")
	}
	if commandOutputFirstPackageScriptIsTest("bun", []string{"run", "test"}) {
		t.Fatal("bun run test must stay out until package-script semantics are explicitly supported")
	}
}

func TestCommandOutputFirstPackageScriptFilterArgs(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    []string
	}{
		{command: "npm", args: []string{"--silent", "run", "test"}, want: []string{"run", "test"}},
		{command: "pnpm", args: []string{"--dir", "app", "run", "test"}, want: []string{"run", "test"}},
		{command: "yarn", args: []string{"run", "--silent", "build"}, want: []string{"run", "build"}},
		{command: "bun", args: []string{"--cwd", "app", "test"}, want: []string{"test"}},
		{command: "deno", args: []string{"--quiet", "test"}, want: []string{"--quiet", "test"}},
		{command: "npm", args: []string{"--dir"}, want: []string{"--dir"}},
	}
	for _, tc := range cases {
		got := commandOutputFirstPackageScriptFilterArgs(tc.command, tc.args)
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("%s %v got %v want %v", tc.command, tc.args, got, tc.want)
		}
	}
}

func TestPackageScriptParserEdges(t *testing.T) {
	firstCommandCases := []struct {
		args      []string
		wantVerb  string
		wantIndex int
	}{
		{args: []string{"--"}, wantVerb: "", wantIndex: -1},
		{args: []string{"--", ""}, wantVerb: "", wantIndex: -1},
		{args: []string{"--", "test"}, wantVerb: "test", wantIndex: 1},
		{args: []string{"--dir"}, wantVerb: "", wantIndex: -1},
		{args: []string{"--dir", ""}, wantVerb: "", wantIndex: -1},
		{args: []string{""}, wantVerb: "", wantIndex: -1},
		{args: []string{"--prefix=app", "test"}, wantVerb: "test", wantIndex: 1},
	}
	for _, tc := range firstCommandCases {
		gotVerb, gotIndex := packageScriptFirstCommand(tc.args)
		if gotVerb != tc.wantVerb || gotIndex != tc.wantIndex {
			t.Fatalf("first command args=%v got=(%q,%d) want=(%q,%d)", tc.args, gotVerb, gotIndex, tc.wantVerb, tc.wantIndex)
		}
	}

	runNameCases := []struct {
		args []string
		want string
	}{
		{args: []string{"test"}, want: ""},
		{args: []string{"run", "", "test"}, want: ""},
		{args: []string{"run", "--silent", "build"}, want: "build"},
		{args: []string{"run", "--if-present", "test"}, want: "test"},
	}
	for _, tc := range runNameCases {
		if got := packageRunScriptName(tc.args); got != tc.want {
			t.Fatalf("run script args=%v got=%q want=%q", tc.args, got, tc.want)
		}
	}
}

func TestCommandExitCodeStartError(t *testing.T) {
	if got := commandExitCode(errors.New("start failed")); got != 127 {
		t.Fatalf("exit code=%d", got)
	}
}

func TestCommandOutputFirstPassthroughMissingBinaryReturns127(t *testing.T) {
	if rc := execCommandOutputFirstPassthrough(filepath.Join(t.TempDir(), "missing-git"), []string{"show"}); rc != 127 {
		t.Fatalf("rc=%d", rc)
	}
}

func withCommandOutputFirstRecordingDB(t *testing.T) string {
	t.Helper()
	withCommandOutputFirstArchiveHome(t)
	oldPath := resolveFilterDBPathFn
	oldGetwd := osGetwd
	dbPath := filepath.Join(t.TempDir(), "filter.db")
	resolveFilterDBPathFn = func() (string, error) { return dbPath, nil }
	osGetwd = func() (string, error) { return "/repo", nil }
	t.Cleanup(func() {
		resolveFilterDBPathFn = oldPath
		osGetwd = oldGetwd
	})
	return dbPath
}

func withCommandOutputFirstArchiveHome(t *testing.T) string {
	t.Helper()
	oldHome := osUserHomeDir
	home := filepath.Join(t.TempDir(), "home")
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = oldHome })
	return home
}

func commandOutputFirstVisibleOutput(output string) string {
	idx := strings.Index(output, "\n[context-archive kind=tool-output uri=local-archive://")
	if idx < 0 {
		return output
	}
	return strings.TrimRight(output[:idx], "\n") + "\n"
}

func commandOutputFirstArchiveURI(output string) string {
	marker := "uri="
	idx := strings.Index(output, marker)
	if idx < 0 {
		return ""
	}
	rest := output[idx+len(marker):]
	end := strings.IndexAny(rest, " ]")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}
