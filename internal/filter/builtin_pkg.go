package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

// compactEmptyStdoutWithNpxPnpmYarn applies match to argv, npx argv suffix, pnpm exec tail, and yarn tail.
func compactEmptyStdoutWithNpxPnpmYarn(argv []string, stdout []byte, match func([]string) bool, okLine []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if match(argv) {
		return okLine, true
	}
	if rest, ok := npxArgvSuffix(argv); ok && match(rest) {
		return okLine, true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if match(argv[2:]) {
			return okLine, true
		}
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if match(argv[1:]) {
			return okLine, true
		}
	}
	return stdout, false
}

func isPoetryInstallArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "poetry" && b != "poetry.exe" {
		return false
	}
	return argv[1] == "install"
}

func isPipenvInstallArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "pipenv" && b != "pipenv.exe" {
		return false
	}
	return argv[1] == "install"
}

func isComposerInstallArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "composer" && b != "composer.exe" && b != "composer.phar" {
		return false
	}
	return argv[1] == "install"
}

func isMixDepsGetArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "mix" && b != "mix.bat" {
		return false
	}
	return argv[1] == "deps.get"
}

func isBundleInstallArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "bundle" && b != "bundle.bat" {
		return false
	}
	return argv[1] == "install"
}

func isGemInstallArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "gem" && b != "gem.cmd" {
		return false
	}
	return argv[1] == "install"
}

func isPipInstallArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "pip" && b != "pip3" {
		return false
	}
	return argv[1] == "install"
}

func isBunInstallArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "bun" && b != "bun.exe" {
		return false
	}
	return argv[1] == "install"
}

// TryCompactNpmInstall summarizes empty stdout from `npm install` / `npm ci` (F12 partial).
func TryCompactNpmInstall(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if strings.ToLower(filepath.Base(argv[0])) != "npm" {
		return stdout, false
	}
	switch argv[1] {
	case "install", "ci", "update":
	default:
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte(fmt.Sprintf("[npm %s] ok\n", argv[1])), true
}

// TryCompactPnpmInstall summarizes empty stdout from `pnpm install` / `pnpm ci` (F12 partial).
func TryCompactPnpmInstall(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "pnpm" {
		return stdout, false
	}
	switch argv[1] {
	case "install", "ci", "update":
	default:
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte(fmt.Sprintf("[pnpm %s] ok\n", argv[1])), true
}

// TryCompactYarnInstall summarizes empty stdout from `yarn install` (F12 partial).
func TryCompactYarnInstall(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	if filepath.Base(argv[0]) != "yarn" || (argv[1] != "install" && argv[1] != "upgrade") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte(fmt.Sprintf("[yarn %s] ok\n", argv[1])), true
}

// TryCompactPoetryInstall summarizes empty stdout from `poetry install` / `npx|pnpm exec|yarn … poetry install` (F12 partial).
func TryCompactPoetryInstall(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isPoetryInstallArgv, []byte("[poetry install] ok\n"))
}

// TryCompactPipenvInstall summarizes empty stdout from `pipenv install` / `npx|pnpm exec|yarn … pipenv install` (F12 partial).
func TryCompactPipenvInstall(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isPipenvInstallArgv, []byte("[pipenv install] ok\n"))
}

// TryCompactComposerInstall summarizes empty stdout from `composer install` / `npx|pnpm exec|yarn … composer install` (F12 partial).
func TryCompactComposerInstall(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isComposerInstallArgv, []byte("[composer install] ok\n"))
}

// TryCompactMixDepsGet summarizes empty stdout from `mix deps.get` / `npx|pnpm exec|yarn … mix deps.get` (Elixir) (F12 partial).
func TryCompactMixDepsGet(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isMixDepsGetArgv, []byte("[mix deps.get] ok\n"))
}

// TryCompactBundleInstall summarizes empty stdout from `bundle install` / `npx|pnpm exec|yarn … bundle install` (Bundler) (F12 partial).
func TryCompactBundleInstall(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isBundleInstallArgv, []byte("[bundle install] ok\n"))
}

// TryCompactGemInstall summarizes empty stdout from `gem install` / `npx|pnpm exec|yarn … gem install` (F12 partial).
func TryCompactGemInstall(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isGemInstallArgv, []byte("[gem install] ok\n"))
}

// TryCompactPipInstall summarizes empty stdout from `pip install` / `pip3 install` / `npx|pnpm exec|yarn … pip|pip3 install` (F12 partial).
func TryCompactPipInstall(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isPipInstallArgv, []byte("[pip install] ok\n"))
}

// TryCompactBunInstall summarizes empty stdout from `bun install` / `npx|pnpm exec|yarn … bun install` (F12 partial).
func TryCompactBunInstall(argv []string, stdout []byte) ([]byte, bool) {
	return compactEmptyStdoutWithNpxPnpmYarn(argv, stdout, isBunInstallArgv, []byte("[bun install] ok\n"))
}

func isUvPipInstallArgv(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "uv" && b != "uv.exe" {
		return false
	}
	return argv[1] == "pip" && argv[2] == "install"
}

// TryCompactUvPipInstall summarizes empty stdout from `uv pip install …` / `npx|pnpm exec|yarn … uv pip install …` (F12 partial).
func TryCompactUvPipInstall(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isUvPipInstallArgv(argv) {
		return []byte("[uv pip install] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && isUvPipInstallArgv(rest) {
		return []byte("[uv pip install] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if isUvPipInstallArgv(argv[2:]) {
			return []byte("[uv pip install] ok\n"), true
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if isUvPipInstallArgv(argv[1:]) {
			return []byte("[uv pip install] ok\n"), true
		}
	}
	return stdout, false
}

func isUvSyncArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "uv" && b != "uv.exe" {
		return false
	}
	return argv[1] == "sync"
}

// TryCompactUvSync summarizes empty stdout from `uv sync` / `npx|pnpm exec|yarn … uv sync` (F12 partial).
func TryCompactUvSync(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isUvSyncArgv(argv) {
		return []byte("[uv sync] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && isUvSyncArgv(rest) {
		return []byte("[uv sync] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if isUvSyncArgv(argv[2:]) {
			return []byte("[uv sync] ok\n"), true
		}
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if isUvSyncArgv(argv[1:]) {
			return []byte("[uv sync] ok\n"), true
		}
	}
	return stdout, false
}

func isGoModCompactArgv(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	if !isGoBinary(argv[0]) || argv[1] != "mod" {
		return false
	}
	switch argv[2] {
	case "tidy", "download", "verify":
		return true
	default:
		return false
	}
}

// TryCompactGoMod summarizes empty stdout from `go mod tidy|download|verify` / `npx|pnpm exec|yarn … go mod …` (F12 partial).
func TryCompactGoMod(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isGoModCompactArgv(argv) {
		return []byte(fmt.Sprintf("[go mod %s] ok\n", argv[2])), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 3 && isGoModCompactArgv(rest) {
		return []byte(fmt.Sprintf("[go mod %s] ok\n", rest[2])), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		tail := argv[2:]
		if isGoModCompactArgv(tail) {
			return []byte(fmt.Sprintf("[go mod %s] ok\n", tail[2])), true
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		tail := argv[1:]
		if isGoModCompactArgv(tail) {
			return []byte(fmt.Sprintf("[go mod %s] ok\n", tail[2])), true
		}
	}
	return stdout, false
}

func isCargoFetchArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "fetch" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoFetchArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoFetchArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoFetchArgv(argv[1:])
	}
	return false
}

// TryCompactCargoFetch summarizes empty stdout from `cargo fetch` / `npx|pnpm exec|yarn … cargo fetch` (F12 partial).
func TryCompactCargoFetch(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isCargoFetchArgv(argv) {
		return stdout, false
	}
	return []byte("[cargo fetch] ok\n"), true
}

func isCargoUpdateArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "update" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoUpdateArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoUpdateArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoUpdateArgv(argv[1:])
	}
	return false
}

// TryCompactCargoUpdate summarizes empty stdout from `cargo update` / `npx|pnpm exec|yarn … cargo update` (F12 partial).
func TryCompactCargoUpdate(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isCargoUpdateArgv(argv) {
		return stdout, false
	}
	return []byte("[cargo update] ok\n"), true
}

func isSwiftPackageResolveArgv(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	if !isSwiftBin(argv[0]) {
		return false
	}
	return argv[1] == "package" && argv[2] == "resolve"
}

// TryCompactSwiftPackageResolve summarizes empty stdout from `swift package resolve` / `npx|pnpm exec|yarn … swift package resolve` (F12 partial).
func TryCompactSwiftPackageResolve(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if isSwiftPackageResolveArgv(argv) {
		return []byte("[swift package resolve] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 3 && isSwiftPackageResolveArgv(rest) {
		return []byte("[swift package resolve] ok\n"), true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isSwiftPackageResolveArgv(argv[2:]) {
		return []byte("[swift package resolve] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isSwiftPackageResolveArgv(argv[1:]) {
		return []byte("[swift package resolve] ok\n"), true
	}
	return stdout, false
}

// TryCompactPackageOutput chains npm/pnpm/yarn and package helpers (incl. npx/pnpm exec/yarn-wrapped poetry, pipenv, composer, mix, bundle, gem, pip, bun, uv, cargo, swift, go mod).
func TryCompactPackageOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactNpmInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPnpmInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactYarnInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPoetryInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPipenvInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactComposerInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMixDepsGet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBundleInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGemInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPipInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBunInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactUvPipInstall(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactUvSync(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoFetch(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoUpdate(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSwiftPackageResolve(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGoMod(argv, stdout); ok {
		return out, true
	}
	// Fallback: strip progress/warning lines from recognized package manager output.
	if label := pkgToolLabel(argv); label != "" {
		s := strings.TrimSpace(string(stdout))
		if s != "" {
			if out, ok := extractPkgSummary(s, label); ok {
				return []byte(out), true
			}
		}
	}
	return stdout, false
}

// pkgToolLabel returns the compact label if argv is a recognized package manager install command.
func pkgToolLabel(argv []string) string {
	if len(argv) < 2 {
		return ""
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	switch {
	case b0 == "npm" && (argv[1] == "install" || argv[1] == "ci" || argv[1] == "update"):
		return fmt.Sprintf("npm %s", argv[1])
	case (b0 == "pnpm" || b0 == "pnpm.cmd") && (argv[1] == "install" || argv[1] == "ci" || argv[1] == "update"):
		return fmt.Sprintf("pnpm %s", argv[1])
	case (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && (argv[1] == "install" || argv[1] == "upgrade"):
		return fmt.Sprintf("yarn %s", argv[1])
	case b0 == "pip" || b0 == "pip3" || b0 == "pip.exe":
		if argv[1] == "install" {
			return "pip install"
		}
	case b0 == "bun" && argv[1] == "install":
		return "bun install"
	case (b0 == "uv" || b0 == "uv.exe") && argv[1] == "sync":
		return "uv sync"
	case (b0 == "uv" || b0 == "uv.exe") && len(argv) >= 3 && argv[1] == "pip" && argv[2] == "install":
		return "uv pip install"
	}
	return ""
}

// extractPkgSummary extracts the meaningful summary from package manager output.
func extractPkgSummary(s, label string) (string, bool) {
	lines := strings.Split(s, "\n")
	var summaryLines []string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		tl := strings.ToLower(t)
		if isPackageErrorSummaryLine(t, tl) {
			summaryLines = append(summaryLines, t)
			if len(summaryLines) >= 12 {
				break
			}
			continue
		}
		// npm/pnpm: "added N packages", "removed N packages", "changed N packages"
		if strings.Contains(tl, "added ") || strings.Contains(tl, "removed ") ||
			strings.Contains(tl, "changed ") || strings.Contains(tl, "audited ") {
			if strings.Contains(tl, "package") {
				summaryLines = append(summaryLines, t)
				continue
			}
		}
		// yarn: "Done in Xs."
		if strings.HasPrefix(tl, "done in ") {
			summaryLines = append(summaryLines, t)
			continue
		}
		// pip: "Successfully installed ..."
		if strings.HasPrefix(tl, "successfully installed") {
			summaryLines = append(summaryLines, t)
			continue
		}
		// bundler: "Bundle complete!"
		if strings.HasPrefix(tl, "bundle complete") {
			summaryLines = append(summaryLines, t)
			continue
		}
	}
	if len(summaryLines) == 0 {
		return "", false
	}
	out := fmt.Sprintf("[%s] %s\n", label, strings.Join(summaryLines, "; "))
	if len(out) >= len(s) {
		return "", false
	}
	return out, true
}

func isPackageErrorSummaryLine(trimmed, lower string) bool {
	return strings.Contains(lower, " err!") ||
		strings.Contains(lower, "eresolve") ||
		strings.Contains(lower, "err_pnpm_") ||
		strings.Contains(lower, "resolutionimpossible") ||
		strings.Contains(lower, "could not find a version") ||
		strings.Contains(lower, "no matching version") ||
		strings.Contains(lower, "no solution found") ||
		strings.Contains(lower, "failed with errors") ||
		strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "error ") ||
		strings.Contains(trimmed, "YN000") && strings.Contains(lower, "error")
}
