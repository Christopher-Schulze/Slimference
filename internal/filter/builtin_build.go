package filter

import (
	"path/filepath"
	"strings"
)

// TryCompactGoBuild replaces empty stdout from `go build …` / `npx|pnpm exec|yarn … go build …` with one line (F07 partial).
func TryCompactGoBuild(argv []string, stdout []byte) ([]byte, bool) {
	if !isGoBuildArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[go build] ok\n"), true
}

func isGoBuildArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isGoBinary(argv[0]) {
		for _, a := range argv[1:] {
			if a == "build" {
				return true
			}
		}
		return false
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isGoBuildArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isGoBuildArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isGoBuildArgv(argv[1:])
	}
	return false
}

// TryCompactCargoBuild replaces empty and parser-proven clean stdout from
// `cargo build ...` / `npx|pnpm exec|yarn ... cargo build ...` with one line.
func TryCompactCargoBuild(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoBuildArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[cargo build] ok\n"), true
	}
	if compacted, ok := compactCargoCleanProgressOutput(s, len(stdout), "cargo build"); ok {
		return compacted, true
	}
	return stdout, false
}

func isCargoBuildArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "build" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoBuildArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoBuildArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoBuildArgv(argv[1:])
	}
	return false
}

func isCargoCheckArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "check" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoCheckArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoCheckArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoCheckArgv(argv[1:])
	}
	return false
}

// TryCompactCargoCheck replaces empty and parser-proven clean stdout from
// `cargo check ...` / `npx|pnpm exec|yarn ... cargo check ...`.
func TryCompactCargoCheck(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoCheckArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[cargo check] ok\n"), true
	}
	if compacted, ok := compactCargoCleanProgressOutput(s, len(stdout), "cargo check"); ok {
		return compacted, true
	}
	return stdout, false
}

func isCargoDocArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "doc" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoDocArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoDocArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoDocArgv(argv[1:])
	}
	return false
}

// TryCompactCargoDoc replaces empty and parser-proven clean stdout from
// `cargo doc ...` / `npx|pnpm exec|yarn ... cargo doc ...`.
func TryCompactCargoDoc(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoDocArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[cargo doc] ok\n"), true
	}
	if compacted, ok := compactCargoCleanProgressOutput(s, len(stdout), "cargo doc"); ok {
		return compacted, true
	}
	return stdout, false
}

func compactCargoCleanProgressOutput(stdout string, originalLen int, label string) ([]byte, bool) {
	lines := strings.Split(stdout, "\n")
	sawFinished := false
	sawProgress := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if cargoBuildLineHasUnsafeMarker(line) {
			return nil, false
		}
		switch {
		case cargoBuildProgressLine(line):
			sawProgress = true
		case cargoBuildFinishedLine(line):
			sawFinished = true
		default:
			return nil, false
		}
	}
	if !sawFinished || !sawProgress {
		return nil, false
	}
	out := []byte("[" + label + "] ok\n")
	if len(out) >= originalLen {
		return nil, false
	}
	return out, true
}

func cargoBuildProgressLine(line string) bool {
	for _, prefix := range []string{"Checking ", "Compiling ", "Documenting ", "Fresh "} {
		if strings.HasPrefix(line, prefix) && strings.TrimSpace(strings.TrimPrefix(line, prefix)) != "" {
			return true
		}
	}
	return false
}

func cargoBuildFinishedLine(line string) bool {
	if !strings.HasPrefix(line, "Finished ") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "Finished "))
	return strings.Contains(rest, "profile") && strings.Contains(rest, "target(s) in")
}

func cargoBuildLineHasUnsafeMarker(line string) bool {
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "warning") ||
		strings.HasPrefix(lower, "error") ||
		strings.HasPrefix(lower, "note:") ||
		strings.HasPrefix(lower, "help:") ||
		strings.HasPrefix(lower, "-->") {
		return true
	}
	for _, marker := range []string{
		" failed",
		"failure",
		"panicked",
		"aborting",
		"aborted",
		"could not",
		"cannot ",
		"unresolved",
		" denied",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// TryCompactBufBuild replaces empty stdout from `buf build` / `npx|pnpm exec|yarn … buf build` (F07 partial).
func TryCompactBufBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "buf", "build") {
		return stdout, false
	}
	return []byte("[buf build] ok\n"), true
}

func isKoBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "ko" || b == "ko.exe"
}

// TryCompactKoBuild replaces empty stdout from `ko build` / `npx|pnpm exec|yarn … ko build` (F07 partial).
func TryCompactKoBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isKoBin(argv[0]) && argv[1] == "build" {
		return []byte("[ko build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isKoBin(rest[0]) && rest[1] == "build" {
		return []byte("[ko build] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isKoBin(argv[2]) && argv[3] == "build" {
		return []byte("[ko build] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isKoBin(argv[1]) && argv[2] == "build" {
		return []byte("[ko build] ok\n"), true
	}
	return stdout, false
}

func isMesonBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "meson" || b == "meson.exe"
}

// TryCompactMesonCompile replaces empty stdout from `meson compile` / `npx|pnpm exec|yarn … meson compile` (F07 partial).
func TryCompactMesonCompile(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isMesonBin(argv[0]) && argv[1] == "compile" {
		return []byte("[meson compile] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isMesonBin(rest[0]) && rest[1] == "compile" {
		return []byte("[meson compile] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isMesonBin(argv[2]) && argv[3] == "compile" {
		return []byte("[meson compile] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isMesonBin(argv[1]) && argv[2] == "compile" {
		return []byte("[meson compile] ok\n"), true
	}
	return stdout, false
}

func isMakeBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "make" || b == "gmake" || b == "mingw32-make.exe"
}

func isMakeCompactArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	if !isMakeBin(argv[0]) {
		return false
	}
	for _, a := range argv[1:] {
		if a == "-n" || a == "--just-print" || a == "--dry-run" {
			return false
		}
	}
	return true
}

// TryCompactMake replaces empty stdout from `make` / `gmake` / `npx|pnpm exec|yarn … make …` (F07 partial).
func TryCompactMake(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isMakeCompactArgv(argv) {
		return []byte("[make] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isMakeCompactArgv(rest) {
		return []byte("[make] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isMakeCompactArgv(argv[2:]) {
		return []byte("[make] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isMakeCompactArgv(argv[1:]) {
		return []byte("[make] ok\n"), true
	}
	return stdout, false
}

func isNinjaBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "ninja" || b == "ninja.exe"
}

// TryCompactNinja replaces empty stdout from `ninja` / `npx|pnpm exec|yarn … ninja …` (F07 partial).
func TryCompactNinja(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isNinjaBin(argv[0]) {
		return []byte("[ninja] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isNinjaBin(rest[0]) {
		return []byte("[ninja] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isNinjaBin(argv[2]) {
		return []byte("[ninja] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isNinjaBin(argv[1]) {
		return []byte("[ninja] ok\n"), true
	}
	return stdout, false
}

func isCmakeBuildArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "cmake" && b != "cmake.exe" {
		return false
	}
	return argv[1] == "--build"
}

// TryCompactCmakeBuild replaces empty stdout from `cmake --build …` / `npx|pnpm exec|yarn … cmake --build …` (F07 partial).
func TryCompactCmakeBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isCmakeBuildArgv(argv) {
		return []byte("[cmake --build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isCmakeBuildArgv(rest) {
		return []byte("[cmake --build] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isCmakeBuildArgv(argv[2:]) {
		return []byte("[cmake --build] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isCmakeBuildArgv(argv[1:]) {
		return []byte("[cmake --build] ok\n"), true
	}
	return stdout, false
}

// TryCompactTsc replaces empty stdout from `tsc` / `npx|pnpm exec|yarn … tsc` (F07 partial).
func TryCompactTsc(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "tsc" || b == "tsc.cmd" {
		return []byte("[tsc] ok\n"), true
	}
	if npxMatches(argv, "tsc") {
		return []byte("[tsc] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "tsc" {
		return []byte("[tsc] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "tsc" {
		return []byte("[tsc] ok\n"), true
	}
	return stdout, false
}

// TryCompactNextBuild replaces empty or strictly clean stdout from `next build` / `npx|pnpm exec|yarn ... next build`.
func TryCompactNextBuild(argv []string, stdout []byte) ([]byte, bool) {
	if !isSingleBinarySubcmdArgv(argv, "next", "build") {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[next build] ok\n"), true
	}
	return compactNextBuildCleanOutput(s, len(stdout))
}

func compactNextBuildCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	out := []byte("[next build] ok\n")
	if len(out) >= originalLen || webBuildCleanOutputHasUnsafeSignal(stdout) {
		return nil, false
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "compiled successfully") {
		return nil, false
	}
	for _, marker := range []string{
		"creating an optimized production build",
		"next.js",
		"route (app)",
		"route (pages)",
		"collecting build traces",
		"finalizing page optimization",
	} {
		if strings.Contains(lower, marker) {
			return out, true
		}
	}
	return nil, false
}

// TryCompactViteBuild replaces empty or strictly clean stdout from `vite build` / `npx|pnpm exec|yarn ... vite build`.
func TryCompactViteBuild(argv []string, stdout []byte) ([]byte, bool) {
	if !isSingleBinarySubcmdArgv(argv, "vite", "build") {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[vite build] ok\n"), true
	}
	return compactViteBuildCleanOutput(s, len(stdout))
}

func compactViteBuildCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	out := []byte("[vite build] ok\n")
	if len(out) >= originalLen || webBuildCleanOutputHasUnsafeSignal(stdout) {
		return nil, false
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "vite v") ||
		!strings.Contains(lower, "building for production") ||
		!strings.Contains(lower, "modules transformed") ||
		!strings.Contains(lower, "built in") {
		return nil, false
	}
	return out, true
}

func webBuildCleanOutputHasUnsafeSignal(stdout string) bool {
	if outputHasUnsafeSuccessSignal(stdout) {
		return true
	}
	for _, line := range strings.Split(stdout, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if lower == "" {
			continue
		}
		if strings.HasPrefix(lower, "(!)") ||
			strings.HasPrefix(lower, "warn") ||
			strings.Contains(lower, " warn ") ||
			strings.Contains(lower, "warn:") ||
			strings.Contains(lower, "failed") ||
			strings.Contains(lower, "failure") ||
			strings.Contains(lower, "fatal") ||
			strings.Contains(lower, "panic") ||
			strings.Contains(lower, "exception") ||
			strings.Contains(lower, "traceback") ||
			webBuildLineHasErrorSignal(lower) {
			return true
		}
	}
	return false
}

func webBuildLineHasErrorSignal(lower string) bool {
	if strings.Contains(lower, "0 error") {
		return false
	}
	return strings.HasPrefix(lower, "error") ||
		strings.Contains(lower, " error ") ||
		strings.Contains(lower, ": error") ||
		strings.Contains(lower, "errors:") ||
		strings.Contains(lower, " errors")
}

// TryCompactWebpack replaces empty or strictly clean stdout from `webpack` / `webpack-cli` / `npx|pnpm exec|yarn ... webpack`.
func TryCompactWebpack(argv []string, stdout []byte) ([]byte, bool) {
	if !isWebpackArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[webpack] ok\n"), true
	}
	return compactWebpackCleanOutput(s, len(stdout))
}

func isWebpackArgv(argv []string) bool {
	return isSingleBinarySubcmdArgv(argv, "webpack", "") ||
		isSingleBinarySubcmdArgv(argv, "webpack-cli", "")
}

func compactWebpackCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	out := []byte("[webpack] ok\n")
	if len(out) >= originalLen || webBuildCleanOutputHasUnsafeSignal(stdout) {
		return nil, false
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "webpack") ||
		!strings.Contains(lower, "compiled successfully") ||
		!webBuildHasArtifactSignal(lower) {
		return nil, false
	}
	return out, true
}

// TryCompactRspackBuild replaces empty or strictly clean stdout from `rspack build` / `npx|pnpm exec|yarn ... rspack build`.
func TryCompactRspackBuild(argv []string, stdout []byte) ([]byte, bool) {
	if !isSingleBinarySubcmdArgv(argv, "rspack", "build") {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[rspack build] ok\n"), true
	}
	return compactRspackCleanOutput(s, len(stdout))
}

func compactRspackCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	out := []byte("[rspack build] ok\n")
	if len(out) >= originalLen || webBuildCleanOutputHasUnsafeSignal(stdout) {
		return nil, false
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "rspack") ||
		!strings.Contains(lower, "compiled successfully") ||
		!webBuildHasArtifactSignal(lower) {
		return nil, false
	}
	return out, true
}

// TryCompactParcelBuild replaces empty or strictly clean stdout from `parcel build` / `npx|pnpm exec|yarn ... parcel build`.
func TryCompactParcelBuild(argv []string, stdout []byte) ([]byte, bool) {
	if !isSingleBinarySubcmdArgv(argv, "parcel", "build") {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[parcel build] ok\n"), true
	}
	return compactParcelCleanOutput(s, len(stdout))
}

func compactParcelCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	out := []byte("[parcel build] ok\n")
	if len(out) >= originalLen || webBuildCleanOutputHasUnsafeSignal(stdout) {
		return nil, false
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "built in") || !webBuildHasArtifactSignal(lower) {
		return nil, false
	}
	return out, true
}

// TryCompactRollupConfig replaces empty or strictly clean stdout from `rollup -c` / `rollup --config ...` / `npx|pnpm exec|yarn ... rollup ...`.
func TryCompactRollupConfig(argv []string, stdout []byte) ([]byte, bool) {
	if !isRollupConfigArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[rollup] ok\n"), true
	}
	return compactRollupCleanOutput(s, len(stdout))
}

func isRollupConfigArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "npx" || b == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		return ok && isRollupConfigArgv(rest)
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" {
		return isRollupConfigArgv(argv[2:])
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") {
		return isRollupConfigArgv(argv[1:])
	}
	if b != "rollup" && b != "rollup.cmd" {
		return false
	}
	for _, a := range argv[1:] {
		if a == "-c" || a == "--config" {
			return true
		}
	}
	return false
}

func compactRollupCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	out := []byte("[rollup] ok\n")
	if len(out) >= originalLen || webBuildCleanOutputHasUnsafeSignal(stdout) {
		return nil, false
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "created ") ||
		!strings.Contains(lower, " in ") ||
		!webBuildHasArtifactSignal(lower) {
		return nil, false
	}
	return out, true
}

// TryCompactEsbuildBundle replaces empty or strictly clean stdout from `esbuild ... --bundle ...` / `npx|pnpm exec|yarn ... esbuild ... --bundle ...`.
func TryCompactEsbuildBundle(argv []string, stdout []byte) ([]byte, bool) {
	if !isEsbuildBundleArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[esbuild] ok\n"), true
	}
	return compactEsbuildCleanOutput(s, len(stdout))
}

func isEsbuildBundleArgv(argv []string) bool {
	return isSingleBinarySubcmdArgv(argv, "esbuild", "") && argvContainsToken(argv, "--bundle")
}

func compactEsbuildCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	out := []byte("[esbuild] ok\n")
	if len(out) >= originalLen || webBuildCleanOutputHasUnsafeSignal(stdout) {
		return nil, false
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "done in") || !webBuildHasArtifactSignal(lower) {
		return nil, false
	}
	return out, true
}

func webBuildHasArtifactSignal(lower string) bool {
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hasPath := strings.Contains(line, "dist/") ||
			strings.Contains(line, "asset ") ||
			strings.Contains(line, ".js") ||
			strings.Contains(line, ".mjs") ||
			strings.Contains(line, ".cjs") ||
			strings.Contains(line, ".css") ||
			strings.Contains(line, ".html")
		hasSize := strings.Contains(line, " kb") ||
			strings.Contains(line, " kib") ||
			strings.Contains(line, " mb") ||
			strings.Contains(line, " mib") ||
			strings.Contains(line, " bytes")
		if hasPath && (hasSize || strings.Contains(line, "created ") || strings.Contains(line, "asset ")) {
			return true
		}
	}
	return false
}

// TryCompactNxBuild replaces empty stdout from `nx build …` / `npx|pnpm exec|yarn … nx build` (F07 partial).
func TryCompactNxBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "nx" || b == "nx.cmd") && argv[1] == "build" {
		return []byte("[nx build] ok\n"), true
	}
	if npxMatches(argv, "nx", "build") {
		return []byte("[nx build] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "nx" && argv[3] == "build" {
		return []byte("[nx build] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "nx" && argv[2] == "build" {
		return []byte("[nx build] ok\n"), true
	}
	return stdout, false
}

// TryCompactTurboBuild replaces empty stdout from `turbo run build` / `turbo build` / `npx|pnpm exec|yarn … turbo … build` (F07 partial).
func TryCompactTurboBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "turbo" || b == "turbo.cmd" {
		okTurbo := false
		switch {
		case argv[1] == "build":
			okTurbo = true
		case argv[1] == "run" && len(argv) >= 3 && argv[2] == "build":
			okTurbo = true
		}
		if okTurbo {
			return []byte("[turbo build] ok\n"), true
		}
		return stdout, false
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && strings.EqualFold(filepath.Base(rest[0]), "turbo") {
		if len(rest) >= 2 && rest[1] == "build" {
			return []byte("[turbo build] ok\n"), true
		}
		if len(rest) >= 3 && rest[1] == "run" && rest[2] == "build" {
			return []byte("[turbo build] ok\n"), true
		}
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "turbo" {
		if argv[3] == "build" {
			return []byte("[turbo build] ok\n"), true
		}
		if argv[3] == "run" && len(argv) >= 5 && argv[4] == "build" {
			return []byte("[turbo build] ok\n"), true
		}
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "turbo" {
		if argv[2] == "build" {
			return []byte("[turbo build] ok\n"), true
		}
		if argv[2] == "run" && len(argv) >= 4 && argv[3] == "build" {
			return []byte("[turbo build] ok\n"), true
		}
	}
	return stdout, false
}

// TryCompactNpmRunBuild replaces empty stdout from `npm run build` (F07 partial).
func TryCompactNpmRunBuild(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 3 {
		return stdout, false
	}
	if strings.ToLower(filepath.Base(argv[0])) != "npm" {
		return stdout, false
	}
	if argv[1] != "run" || argv[2] != "build" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[npm run build] ok\n"), true
}

// TryCompactPnpmRunBuild replaces empty stdout from `pnpm run build` (F07 partial).
func TryCompactPnpmRunBuild(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 3 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "pnpm" {
		return stdout, false
	}
	if argv[1] != "run" || argv[2] != "build" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[pnpm run build] ok\n"), true
}

// TryCompactYarnRunBuild replaces empty stdout from `yarn run build` (F07 partial).
func TryCompactYarnRunBuild(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 3 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "yarn" || argv[1] != "run" || argv[2] != "build" {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[yarn run build] ok\n"), true
}

func isMvnCompactArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "mvn" && b != "mvn.cmd" && b != "mvnw" && b != "mvnw.cmd" {
		return false
	}
	for _, a := range argv[1:] {
		switch a {
		case "-version", "--version", "-v", "-h", "-help", "--help", "-?":
			return false
		}
	}
	hasNonFlag := false
	for _, a := range argv[1:] {
		if !strings.HasPrefix(a, "-") {
			hasNonFlag = true
			break
		}
	}
	return hasNonFlag
}

// TryCompactMvn replaces empty stdout from Maven wrapper / mvn / `npx|pnpm exec|yarn … mvn|mvnw …` (F07 partial).
func TryCompactMvn(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isMvnCompactArgv(argv) {
		return []byte("[mvn] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isMvnCompactArgv(rest) {
		return []byte("[mvn] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isMvnCompactArgv(argv[2:]) {
		return []byte("[mvn] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isMvnCompactArgv(argv[1:]) {
		return []byte("[mvn] ok\n"), true
	}
	return stdout, false
}

func isGradleBuildArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "gradle" && b != "gradle.bat" && b != "gradlew" && b != "gradlew.bat" {
		return false
	}
	for _, a := range argv[1:] {
		if a == "build" {
			return true
		}
	}
	return false
}

// TryCompactGradle replaces empty stdout from `gradle build` / `gradlew build` / `npx|pnpm exec|yarn … gradle|gradlew … build` (F07 partial).
func TryCompactGradle(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isGradleBuildArgv(argv) {
		return []byte("[gradle build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isGradleBuildArgv(rest) {
		return []byte("[gradle build] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isGradleBuildArgv(argv[2:]) {
		return []byte("[gradle build] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isGradleBuildArgv(argv[1:]) {
		return []byte("[gradle build] ok\n"), true
	}
	return stdout, false
}

func isZigBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "zig" || b == "zig.exe"
}

// TryCompactZigBuild replaces empty stdout from `zig build` / `npx|pnpm exec|yarn … zig build` (F07 partial).
func TryCompactZigBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isZigBin(argv[0]) && argv[1] == "build" {
		return []byte("[zig build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isZigBin(rest[0]) && rest[1] == "build" {
		return []byte("[zig build] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isZigBin(argv[2]) && argv[3] == "build" {
		return []byte("[zig build] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isZigBin(argv[1]) && argv[2] == "build" {
		return []byte("[zig build] ok\n"), true
	}
	return stdout, false
}

func isJustBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "just" || b == "just.exe"
}

// TryCompactJust replaces empty stdout from `just` / `npx|pnpm exec|yarn … just …` (command runner) (F07 partial).
func TryCompactJust(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isJustBin(argv[0]) {
		return []byte("[just] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isJustBin(rest[0]) {
		return []byte("[just] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isJustBin(argv[2]) {
		return []byte("[just] ok\n"), true
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isJustBin(argv[1]) {
		return []byte("[just] ok\n"), true
	}
	return stdout, false
}

// TryCompactWasmPackBuild replaces empty stdout from `wasm-pack build` / `npx|pnpm exec|yarn … wasm-pack build` (F07 partial).
func TryCompactWasmPackBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "wasm-pack" || b == "wasm-pack.exe") && argv[1] == "build" {
		return []byte("[wasm-pack build] ok\n"), true
	}
	if npxMatches(argv, "wasm-pack", "build") {
		return []byte("[wasm-pack build] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "wasm-pack") && argv[3] == "build" {
		return []byte("[wasm-pack build] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "wasm-pack") && argv[2] == "build" {
		return []byte("[wasm-pack build] ok\n"), true
	}
	return stdout, false
}

func isBazelBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "bazel" || b == "bazel.exe" || b == "bazelisk" || b == "bazelisk.exe"
}

// TryCompactBazelBuild replaces empty stdout from `bazel build` / `bazelisk build` / `npx|pnpm exec|yarn … bazel|bazelisk build` (F07 partial).
func TryCompactBazelBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isBazelBin(argv[0]) && argv[1] == "build" {
		return []byte("[bazel build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isBazelBin(rest[0]) && rest[1] == "build" {
		return []byte("[bazel build] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isBazelBin(argv[2]) && argv[3] == "build" {
		return []byte("[bazel build] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isBazelBin(argv[1]) && argv[2] == "build" {
		return []byte("[bazel build] ok\n"), true
	}
	return stdout, false
}

func isSwiftBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "swift" || b == "swift.exe"
}

// TryCompactSwiftBuild replaces empty stdout from `swift build` / `npx|pnpm exec|yarn … swift build` (F07 partial).
func TryCompactSwiftBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isSwiftBin(argv[0]) && argv[1] == "build" {
		return []byte("[swift build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isSwiftBin(rest[0]) && rest[1] == "build" {
		return []byte("[swift build] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isSwiftBin(argv[2]) && argv[3] == "build" {
		return []byte("[swift build] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isSwiftBin(argv[1]) && argv[2] == "build" {
		return []byte("[swift build] ok\n"), true
	}
	return stdout, false
}

// TryCompactMoonRunBuild replaces empty stdout from `moon run build` / `moon run …:build` / `npx|pnpm exec|yarn … moon … run … build` (F07 partial).
func TryCompactMoonRunBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 3 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "moon" || b == "moon.exe" {
		if argv[1] != "run" {
			return stdout, false
		}
		task := argv[2]
		if task != "build" && !strings.HasSuffix(task, ":build") {
			return stdout, false
		}
		return []byte("[moon run build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 3 && strings.EqualFold(filepath.Base(rest[0]), "moon") {
		if rest[1] != "run" {
			return stdout, false
		}
		task := rest[2]
		if task != "build" && !strings.HasSuffix(task, ":build") {
			return stdout, false
		}
		return []byte("[moon run build] ok\n"), true
	}
	if len(argv) >= 5 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "moon") {
		if argv[3] != "run" {
			return stdout, false
		}
		task := argv[4]
		if task != "build" && !strings.HasSuffix(task, ":build") {
			return stdout, false
		}
		return []byte("[moon run build] ok\n"), true
	}
	if len(argv) >= 4 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "moon") {
		if argv[2] != "run" {
			return stdout, false
		}
		task := argv[3]
		if task != "build" && !strings.HasSuffix(task, ":build") {
			return stdout, false
		}
		return []byte("[moon run build] ok\n"), true
	}
	return stdout, false
}

func isPackBin(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	return b == "pack" || b == "pack.exe"
}

// TryCompactPackBuild replaces empty stdout from `pack build` / `npx|pnpm exec|yarn … pack build` (Cloud Native Buildpacks CLI; F07 partial).
func TryCompactPackBuild(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isPackBin(argv[0]) && argv[1] == "build" {
		return []byte("[pack build] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isPackBin(rest[0]) && rest[1] == "build" {
		return []byte("[pack build] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isPackBin(argv[2]) && argv[3] == "build" {
		return []byte("[pack build] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isPackBin(argv[1]) && argv[2] == "build" {
		return []byte("[pack build] ok\n"), true
	}
	return stdout, false
}

// TryCompactBuildOutput chains go + cargo + tsc/next/npm + JVM builds empty-success summaries.
func TryCompactBuildOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactGoBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoCheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoDoc(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMake(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNinja(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCmakeBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTsc(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNextBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactViteBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactWebpack(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRspackBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactParcelBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRollupConfig(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactEsbuildBundle(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNxBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMoonRunBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTurboBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNpmRunBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPnpmRunBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactYarnRunBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMvn(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGradle(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactZigBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactWasmPackBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBazelBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSwiftBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBufBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactKoBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMesonCompile(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPackBuild(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactJust(argv, stdout); ok {
		return out, true
	}
	if out, ok := compactPackageManagerBuildScriptOutput(argv, stdout); ok {
		return out, true
	}
	// Structured parsers: tool-specific failure extraction before fallback.
	if compact, ok := ParseFailures(argv, string(stdout)); ok {
		return []byte(compact), true
	}
	// Fallback: for recognized build tools with non-empty output, extract errors or detect success.
	if label := buildToolLabel(argv); label != "" {
		s := strings.TrimSpace(string(stdout))
		if s != "" {
			if label == "tsc" && !detectBuildSuccess(s) {
				return stdout, false
			}
			if out, ok := extractBuildErrors(s, label); ok {
				return []byte(out), true
			}
		}
	}
	return stdout, false
}

// buildToolLabel returns the compact label for argv if it is a recognized build command, else "".
func buildToolLabel(argv []string) string {
	if isGoBuildArgv(argv) {
		return "go build"
	}
	if isCargoBuildArgv(argv) {
		return "cargo build"
	}
	if isCargoCheckArgv(argv) {
		return "cargo check"
	}
	if isCargoDocArgv(argv) {
		return "cargo doc"
	}
	if isMakeCompactArgv(argv) {
		return "make"
	}
	if isCmakeBuildArgv(argv) {
		return "cmake --build"
	}
	if isMvnCompactArgv(argv) {
		return "mvn"
	}
	if isGradleBuildArgv(argv) {
		return "gradle build"
	}
	if len(argv) >= 1 {
		b0 := strings.ToLower(filepath.Base(argv[0]))
		isPnpm := b0 == "pnpm" || b0 == "pnpm.cmd"
		isYarn := b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg"
		// Ninja
		if isNinjaBin(argv[0]) {
			return "ninja"
		}
		if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && isNinjaBin(rest[0]) {
			return "ninja"
		}
		if isPnpm && len(argv) >= 3 && argv[1] == "exec" && isNinjaBin(argv[2]) {
			return "ninja"
		}
		if isYarn && len(argv) >= 2 && isNinjaBin(argv[1]) {
			return "ninja"
		}
		// Zig build
		if isZigBin(argv[0]) && len(argv) >= 2 && argv[1] == "build" {
			return "zig build"
		}
		if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isZigBin(rest[0]) && rest[1] == "build" {
			return "zig build"
		}
		// Bazel build
		if isBazelBin(argv[0]) && len(argv) >= 2 && argv[1] == "build" {
			return "bazel build"
		}
		if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 2 && isBazelBin(rest[0]) && rest[1] == "build" {
			return "bazel build"
		}
		// Swift build
		if isSwiftBin(argv[0]) && len(argv) >= 2 && argv[1] == "build" {
			return "swift build"
		}
		// KO build
		if isKoBin(argv[0]) && len(argv) >= 2 && argv[1] == "build" {
			return "ko build"
		}
		// Meson compile
		if isMesonBin(argv[0]) && len(argv) >= 2 && argv[1] == "compile" {
			return "meson compile"
		}
		// Just
		if isJustBin(argv[0]) {
			return "just"
		}
		// Pack build
		if isPackBin(argv[0]) && len(argv) >= 2 && argv[1] == "build" {
			return "pack build"
		}
		// npm/pnpm/yarn run build
		if len(argv) >= 3 && b0 == "npm" && argv[1] == "run" && argv[2] == "build" {
			return "npm run build"
		}
		if len(argv) >= 3 && isPnpm && argv[1] == "run" && argv[2] == "build" {
			return "pnpm run build"
		}
		if len(argv) >= 3 && isYarn && argv[1] == "run" && argv[2] == "build" {
			return "yarn run build"
		}
	}
	// Simple bin+subcommand tools (via direct/npx/pnpm exec/yarn)
	type binSub struct{ bin, sub, label string }
	tools := []binSub{
		{"tsc", "", "tsc"},
		{"next", "build", "next build"},
		{"vite", "build", "vite build"},
		{"webpack", "", "webpack"},
		{"webpack-cli", "", "webpack"},
		{"rspack", "build", "rspack build"},
		{"parcel", "build", "parcel build"},
		{"rollup", "", "rollup"},
		{"esbuild", "", "esbuild"},
		{"nx", "build", "nx build"},
		{"turbo", "build", "turbo build"},
		{"buf", "build", "buf build"},
		{"wasm-pack", "build", "wasm-pack build"},
		{"moon", "build", "moon build"},
	}
	for _, t := range tools {
		if isSingleBinarySubcmdArgv(argv, t.bin, t.sub) {
			return t.label
		}
	}
	return ""
}
