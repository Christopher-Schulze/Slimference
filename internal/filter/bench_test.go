package filter

import (
	"strings"
	"testing"
)

var (
	benchGitStatusOut = []byte(`M  internal/filter/pipeline.go
 M internal/filter/builtin_git.go
?? docs/context.md
?? docs/todo.md
R  internal/filter/old.go -> internal/filter/new.go
UU internal/filter/conflict.go
`)
	benchGitStatusArgv = []string{"git", "status", "--porcelain=v1"}

	benchBuildSuccessOut = []byte(`go: downloading github.com/foo/bar v1.2.3
ok github.com/tokenproxy/tokenproxy/internal/filter (cached)
ok github.com/tokenproxy/tokenproxy/internal/compression (cached)
`)
	benchBuildArgv = []string{"go", "test", "./..."}

	benchLargeJSON = []byte(`{` + strings.Repeat(`"key": "value", `, 500) + `"last": "val"}`)
)

func BenchmarkTryCompactGitStatus(b *testing.B) {
	for b.Loop() {
		TryCompactGitStatus(benchGitStatusArgv, benchGitStatusOut)
	}
}

func BenchmarkTryCompactBuildOutput(b *testing.B) {
	for b.Loop() {
		TryCompactBuildOutput(benchBuildArgv, benchBuildSuccessOut)
	}
}

func BenchmarkTryCompactJSONMinify_large(b *testing.B) {
	for b.Loop() {
		TryCompactJSONMinify(benchLargeJSON)
	}
}

func BenchmarkRunPipeline_gitStatus(b *testing.B) {
	// Benchmark applyLayer0AfterANSI directly (RunPipeline runs a subprocess).
	argv := benchGitStatusArgv
	stdout := benchGitStatusOut
	b.ResetTimer()
	for b.Loop() {
		applyLayer0AfterANSI("", argv, stdout)
	}
}

func BenchmarkApplyLayer0AfterANSI_noMatch(b *testing.B) {
	// Simulates a command that doesn't match any filter (falls through all checks).
	argv := []string{"curl", "https://example.com"}
	stdout := []byte("HTTP/1.1 200 OK\ncontent-type: text/html\n\n<html></html>")
	b.ResetTimer()
	for b.Loop() {
		applyLayer0AfterANSI("", argv, stdout)
	}
}

func BenchmarkTruncateStdoutWithHint_noTrunc(b *testing.B) {
	data := []byte(strings.Repeat("a", 500))
	for b.Loop() {
		TruncateStdoutWithHint(data, 2000)
	}
}

func BenchmarkTruncateStdoutWithHint_truncates(b *testing.B) {
	data := []byte(strings.Repeat("a", 5000))
	for b.Loop() {
		TruncateStdoutWithHint(data, 2000)
	}
}
