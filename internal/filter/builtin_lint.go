package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

func isCargoClippyArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "clippy" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoClippyArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoClippyArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoClippyArgv(argv[1:])
	}
	return false
}

// TryCompactCargoClippy summarizes empty stdout from `cargo clippy` / `npx|pnpm exec|yarn … cargo clippy` (F09 partial).
func TryCompactCargoClippy(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isCargoClippyArgv(argv) {
		return stdout, false
	}
	return []byte("[cargo clippy] ok\n"), true
}

func isCargoAuditArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isCargoBin(argv[0]) && argv[1] == "audit" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isCargoAuditArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isCargoAuditArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isCargoAuditArgv(argv[1:])
	}
	return false
}

// TryCompactCargoAudit summarizes empty stdout from `cargo audit` / `npx|pnpm exec|yarn … cargo audit` (F09 partial).
func TryCompactCargoAudit(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isCargoAuditArgv(argv) {
		return stdout, false
	}
	return []byte("[cargo audit] ok\n"), true
}

// TryCompactGolangciLint summarizes empty stdout from golangci-lint / `npx|pnpm exec|yarn … golangci-lint` (F09 partial).
func TryCompactGolangciLint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "golangci-lint" || b == "golangci-lint.exe" {
		return []byte("[golangci-lint] ok\n"), true
	}
	if npxMatches(argv, "golangci-lint") {
		return []byte("[golangci-lint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "golangci-lint" {
		return []byte("[golangci-lint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "golangci-lint" {
		return []byte("[golangci-lint] ok\n"), true
	}
	return stdout, false
}

// TryCompactStaticcheck summarizes empty stdout from `staticcheck` / `npx|pnpm exec|yarn … staticcheck` (F09 partial).
func TryCompactStaticcheck(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "staticcheck" || b == "staticcheck.exe" {
		return []byte("[staticcheck] ok\n"), true
	}
	if npxMatches(argv, "staticcheck") {
		return []byte("[staticcheck] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "staticcheck" {
		return []byte("[staticcheck] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "staticcheck" {
		return []byte("[staticcheck] ok\n"), true
	}
	return stdout, false
}

// TryCompactGocritic summarizes empty stdout from `gocritic check` / `npx|pnpm exec|yarn … gocritic check` (F09 partial).
func TryCompactGocritic(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "gocritic" || b == "gocritic.exe") && argv[1] == "check" {
		return []byte("[gocritic] ok\n"), true
	}
	if npxMatches(argv, "gocritic", "check") {
		return []byte("[gocritic] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "gocritic" && argv[3] == "check" {
		return []byte("[gocritic] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "gocritic" && argv[2] == "check" {
		return []byte("[gocritic] ok\n"), true
	}
	return stdout, false
}

// TryCompactGosec summarizes empty stdout from `gosec` / `npx|pnpm exec|yarn … gosec` (F09 partial).
func TryCompactGosec(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "gosec" || b == "gosec.exe" {
		return []byte("[gosec] ok\n"), true
	}
	if npxMatches(argv, "gosec") {
		return []byte("[gosec] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "gosec" {
		return []byte("[gosec] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "gosec" {
		return []byte("[gosec] ok\n"), true
	}
	return stdout, false
}

// TryCompactBufLint summarizes empty stdout from `buf lint` / `npx|pnpm exec|yarn … buf lint` (F09 partial).
func TryCompactBufLint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "buf", "lint") {
		return stdout, false
	}
	return []byte("[buf lint] ok\n"), true
}

// TryCompactProtolint summarizes empty stdout from `protolint` / `npx|pnpm exec|yarn … protolint` (F09 partial).
func TryCompactProtolint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "protolint" || b == "protolint.exe" {
		return []byte("[protolint] ok\n"), true
	}
	if npxMatches(argv, "protolint") {
		return []byte("[protolint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "protolint" {
		return []byte("[protolint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "protolint" {
		return []byte("[protolint] ok\n"), true
	}
	return stdout, false
}

// TryCompactSemgrep summarizes empty stdout from `semgrep` / `python -m semgrep` / `npx|pnpm exec|yarn … semgrep` / `pnpm exec|yarn … python … -m semgrep` (F09 partial).
func TryCompactSemgrep(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolArgv(argv, "semgrep") {
		return stdout, false
	}
	return []byte("[semgrep] ok\n"), true
}

// TryCompactDjlint summarizes empty stdout from `djlint` / `python -m djlint` / `npx|pnpm exec|yarn … djlint` / `pnpm exec|yarn … python … -m djlint` (Django/Jinja templates; F09 partial).
func TryCompactDjlint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolArgv(argv, "djlint") {
		return stdout, false
	}
	return []byte("[djlint] ok\n"), true
}

// TryCompactTyCheck summarizes empty stdout from `ty check` / `npx|pnpm exec|yarn … ty check` (Astral Python checker; F09 / F23-style partial).
func TryCompactTyCheck(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "ty", "check") {
		return stdout, false
	}
	return []byte("[ty check] ok\n"), true
}

// TryCompactZizmor summarizes empty stdout from `zizmor` / `npx|pnpm exec|yarn … zizmor` (GitHub Actions workflow linter; F09 partial).
func TryCompactZizmor(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "zizmor" || b == "zizmor.exe" {
		return []byte("[zizmor] ok\n"), true
	}
	if npxMatches(argv, "zizmor") {
		return []byte("[zizmor] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "zizmor" {
		return []byte("[zizmor] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "zizmor" {
		return []byte("[zizmor] ok\n"), true
	}
	return stdout, false
}

// TryCompactKubeLinter summarizes empty stdout from `kube-linter` / `npx|pnpm exec|yarn … kube-linter` (F09 partial).
func TryCompactKubeLinter(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "kube-linter" || b == "kube-linter.exe" {
		return []byte("[kube-linter] ok\n"), true
	}
	if npxMatches(argv, "kube-linter") {
		return []byte("[kube-linter] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "kube-linter" {
		return []byte("[kube-linter] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "kube-linter" {
		return []byte("[kube-linter] ok\n"), true
	}
	return stdout, false
}

// TryCompactPyright summarizes empty stdout from `pyright` / `basedpyright` / `npx|pnpm exec|yarn … pyright|basedpyright` (F09 partial).
func TryCompactPyright(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "pyright" || b == "pyright.exe" || b == "basedpyright" || b == "basedpyright.exe" {
		return []byte("[pyright] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 {
		switch strings.ToLower(filepath.Base(rest[0])) {
		case "pyright", "basedpyright":
			return []byte("[pyright] ok\n"), true
		}
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" {
		switch strings.ToLower(filepath.Base(argv[2])) {
		case "pyright", "basedpyright":
			return []byte("[pyright] ok\n"), true
		}
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") {
		switch strings.ToLower(filepath.Base(argv[1])) {
		case "pyright", "basedpyright":
			return []byte("[pyright] ok\n"), true
		}
	}
	return stdout, false
}

// TryCompactAnsibleLint summarizes empty stdout from `ansible-lint` / `npx|pnpm exec|yarn … ansible-lint` (F09 partial).
func TryCompactAnsibleLint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "ansible-lint" || b == "ansible-lint.exe" {
		return []byte("[ansible-lint] ok\n"), true
	}
	if npxMatches(argv, "ansible-lint") {
		return []byte("[ansible-lint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "ansible-lint" {
		return []byte("[ansible-lint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "ansible-lint" {
		return []byte("[ansible-lint] ok\n"), true
	}
	return stdout, false
}

// TryCompactCueVet summarizes empty stdout from `cue vet` / `npx|pnpm exec|yarn … cue vet` (F09 partial).
func TryCompactCueVet(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "cue", "vet") {
		return stdout, false
	}
	return []byte("[cue vet] ok\n"), true
}

// TryCompactTflint summarizes empty stdout from `tflint` / `npx|pnpm exec|yarn … tflint` (Terraform linter; F09 partial).
func TryCompactTflint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "tflint" || b == "tflint.exe" {
		return []byte("[tflint] ok\n"), true
	}
	if npxMatches(argv, "tflint") {
		return []byte("[tflint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "tflint" {
		return []byte("[tflint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "tflint" {
		return []byte("[tflint] ok\n"), true
	}
	return stdout, false
}

// TryCompactPint summarizes empty stdout from Laravel `pint` / `npx|pnpm exec|yarn … pint` (PHP formatter/linter; F09 partial).
func TryCompactPint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "pint" || b == "pint.exe" {
		return []byte("[pint] ok\n"), true
	}
	if npxMatches(argv, "pint") {
		return []byte("[pint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "pint" {
		return []byte("[pint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "pint" {
		return []byte("[pint] ok\n"), true
	}
	return stdout, false
}

// TryCompactPhpcs summarizes empty stdout from `phpcs` / `npx|pnpm exec|yarn … phpcs` (PHP_CodeSniffer; F09 partial).
func TryCompactPhpcs(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "phpcs" || b == "phpcs.exe" {
		return []byte("[phpcs] ok\n"), true
	}
	if npxMatches(argv, "phpcs") {
		return []byte("[phpcs] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "phpcs" {
		return []byte("[phpcs] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "phpcs" {
		return []byte("[phpcs] ok\n"), true
	}
	return stdout, false
}

// TryCompactCfnLint summarizes empty stdout from `cfn-lint` / `npx|pnpm exec|yarn … cfn-lint` (CloudFormation; F09 partial).
func TryCompactCfnLint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "cfn-lint" || b == "cfn-lint.exe" {
		return []byte("[cfn-lint] ok\n"), true
	}
	if npxMatches(argv, "cfn-lint") {
		return []byte("[cfn-lint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "cfn-lint" {
		return []byte("[cfn-lint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "cfn-lint" {
		return []byte("[cfn-lint] ok\n"), true
	}
	return stdout, false
}

// TryCompactDotenvLinter summarizes empty stdout from `dotenv-linter` / `npx|pnpm exec|yarn … dotenv-linter` (F09 partial).
func TryCompactDotenvLinter(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "dotenv-linter" || b == "dotenv-linter.exe" {
		return []byte("[dotenv-linter] ok\n"), true
	}
	if npxMatches(argv, "dotenv-linter") {
		return []byte("[dotenv-linter] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "dotenv-linter" {
		return []byte("[dotenv-linter] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "dotenv-linter" {
		return []byte("[dotenv-linter] ok\n"), true
	}
	return stdout, false
}

// TryCompactPhpstan summarizes empty stdout from `phpstan` / `npx|pnpm exec|yarn … phpstan` (F09 partial).
func TryCompactPhpstan(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "phpstan" || b == "phpstan.phar" || b == "phpstan.exe" {
		return []byte("[phpstan] ok\n"), true
	}
	if npxMatches(argv, "phpstan") {
		return []byte("[phpstan] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "phpstan" {
		return []byte("[phpstan] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "phpstan" {
		return []byte("[phpstan] ok\n"), true
	}
	return stdout, false
}

// TryCompactPsalm summarizes empty stdout from `psalm` / `npx|pnpm exec|yarn … psalm` (F09 partial).
func TryCompactPsalm(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "psalm" || b == "psalm.phar" || b == "psalm.exe" {
		return []byte("[psalm] ok\n"), true
	}
	if npxMatches(argv, "psalm") {
		return []byte("[psalm] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "psalm" {
		return []byte("[psalm] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "psalm" {
		return []byte("[psalm] ok\n"), true
	}
	return stdout, false
}

// TryCompactPhan summarizes empty stdout from `phan` / `npx|pnpm exec|yarn … phan` (F09 partial).
func TryCompactPhan(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "phan" || b == "phan.phar" || b == "phan.exe" {
		return []byte("[phan] ok\n"), true
	}
	if npxMatches(argv, "phan") {
		return []byte("[phan] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "phan" {
		return []byte("[phan] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "phan" {
		return []byte("[phan] ok\n"), true
	}
	return stdout, false
}

// TryCompactSpectralLint summarizes empty stdout from `spectral lint` / `npx|pnpm exec|yarn … spectral lint` (OpenAPI; F09 partial).
func TryCompactSpectralLint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "spectral", "lint") {
		return stdout, false
	}
	return []byte("[spectral lint] ok\n"), true
}

// TryCompactJscpd summarizes empty stdout from `jscpd` / `npx|pnpm exec|yarn … jscpd` (copy/paste detector; F09 partial).
func TryCompactJscpd(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "jscpd" || b == "jscpd.cmd" {
		return []byte("[jscpd] ok\n"), true
	}
	if npxMatches(argv, "jscpd") {
		return []byte("[jscpd] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "jscpd" {
		return []byte("[jscpd] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "jscpd" {
		return []byte("[jscpd] ok\n"), true
	}
	return stdout, false
}

// TryCompactGofumpt summarizes empty stdout from `gofumpt` / `npx|pnpm exec|yarn … gofumpt` (F09 partial).
func TryCompactGofumpt(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "gofumpt" || b == "gofumpt.exe" {
		return []byte("[gofumpt] ok\n"), true
	}
	if npxMatches(argv, "gofumpt") {
		return []byte("[gofumpt] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "gofumpt" {
		return []byte("[gofumpt] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "gofumpt" {
		return []byte("[gofumpt] ok\n"), true
	}
	return stdout, false
}

// TryCompactRevive summarizes empty stdout from `revive` / `npx|pnpm exec|yarn … revive` (F09 partial).
func TryCompactRevive(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "revive" || b == "revive.exe" {
		return []byte("[revive] ok\n"), true
	}
	if npxMatches(argv, "revive") {
		return []byte("[revive] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "revive" {
		return []byte("[revive] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "revive" {
		return []byte("[revive] ok\n"), true
	}
	return stdout, false
}

// tryCompactEmptyStdoutSingleBinary matches direct `tool` or `npx|pnpm exec|yarn … tool` (empty stdout only).
func tryCompactEmptyStdoutSingleBinary(argv []string, stdout []byte, tool string) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == tool || b == tool+".exe" {
		return []byte("[" + tool + "] ok\n"), true
	}
	if npxMatches(argv, tool) {
		return []byte("[" + tool + "] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == tool {
		return []byte("[" + tool + "] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == tool {
		return []byte("[" + tool + "] ok\n"), true
	}
	return stdout, false
}

// TryCompactErrcheck summarizes empty stdout from `errcheck` / `npx|pnpm exec|yarn … errcheck` (F09 partial).
func TryCompactErrcheck(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "errcheck")
}

// TryCompactIneffassign summarizes empty stdout from `ineffassign` / `npx|pnpm exec|yarn … ineffassign` (F09 partial).
func TryCompactIneffassign(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "ineffassign")
}

// TryCompactNilaway summarizes empty stdout from `nilaway` / `npx|pnpm exec|yarn … nilaway` (F09 partial).
func TryCompactNilaway(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "nilaway")
}

func isGoVetArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if isGoBinary(argv[0]) && argv[1] == "vet" {
		return true
	}
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 2 {
			return false
		}
		return isGoVetArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isGoVetArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isGoVetArgv(argv[1:])
	}
	return false
}

// TryCompactGoVet summarizes empty stdout from `go vet …` / `npx|pnpm exec|yarn … go vet …` (F09 partial).
func TryCompactGoVet(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isGoVetArgv(argv) {
		return stdout, false
	}
	return []byte("[go vet] ok\n"), true
}

// TryCompactUnparam summarizes empty stdout from `unparam` / `npx|pnpm exec|yarn … unparam` (F09 partial).
func TryCompactUnparam(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "unparam")
}

// TryCompactMisspell summarizes empty stdout from `misspell` / `npx|pnpm exec|yarn … misspell` (F09 partial).
func TryCompactMisspell(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "misspell")
}

// TryCompactGocyclo summarizes empty stdout from `gocyclo` / `npx|pnpm exec|yarn … gocyclo` (F09 partial).
func TryCompactGocyclo(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "gocyclo")
}

// TryCompactForbidigo summarizes empty stdout from `forbidigo` / `npx|pnpm exec|yarn … forbidigo` (F09 partial).
func TryCompactForbidigo(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "forbidigo")
}

// TryCompactPrealloc summarizes empty stdout from `prealloc` / `npx|pnpm exec|yarn … prealloc` (F09 partial).
func TryCompactPrealloc(argv []string, stdout []byte) ([]byte, bool) {
	return tryCompactEmptyStdoutSingleBinary(argv, stdout, "prealloc")
}

// TryCompactRuffCheck summarizes empty stdout when argv includes `ruff` … `check` / `python -m ruff … check` / `npx|pnpm exec|yarn … ruff check` / `pnpm exec|yarn … python … -m ruff … check` (F09 partial).
func TryCompactRuffCheck(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 || !argvContainsToken(argv, "check") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isRuffArgv(argv) {
		return stdout, false
	}
	return []byte("[ruff check] ok\n"), true
}

// TryCompactPylint summarizes empty stdout from `pylint` / `python -m pylint` / `npx|pnpm exec|yarn … pylint` / `pnpm exec|yarn … python … -m pylint` (F09 partial).
func TryCompactPylint(argv []string, stdout []byte) ([]byte, bool) {
	if !isPylintArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[pylint] ok\n"), true
}

func isPylintArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		return isPylintArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isPylintArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isPylintArgv(argv[1:])
	}
	b := b0
	if b == "pylint" || b == "pylint.exe" {
		return true
	}
	if b != "python" && b != "python3" && b != "python.exe" && b != "python3.exe" {
		return false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-m" && argv[i+1] == "pylint" {
			return true
		}
	}
	return false
}

// TryCompactFlake8 summarizes empty stdout from `flake8` / `python -m flake8` / `npx|pnpm exec|yarn … flake8` / `pnpm exec|yarn … python … -m flake8` (F09 partial).
func TryCompactFlake8(argv []string, stdout []byte) ([]byte, bool) {
	if !isFlake8Argv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[flake8] ok\n"), true
}

func isFlake8Argv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		return isFlake8Argv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isFlake8Argv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isFlake8Argv(argv[1:])
	}
	b := b0
	if b == "flake8" || b == "flake8.exe" {
		return true
	}
	if b != "python" && b != "python3" && b != "python.exe" && b != "python3.exe" {
		return false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-m" && argv[i+1] == "flake8" {
			return true
		}
	}
	return false
}

// TryCompactShellcheck summarizes empty stdout from `shellcheck` / `npx|pnpm exec|yarn … shellcheck` (F09 partial).
func TryCompactShellcheck(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "shellcheck" || b == "shellcheck.exe" {
		return []byte("[shellcheck] ok\n"), true
	}
	if npxMatches(argv, "shellcheck") {
		return []byte("[shellcheck] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "shellcheck" {
		return []byte("[shellcheck] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "shellcheck" {
		return []byte("[shellcheck] ok\n"), true
	}
	return stdout, false
}

// TryCompactHadolint summarizes empty stdout from `hadolint` / `npx|pnpm exec|yarn … hadolint` (F09 partial).
func TryCompactHadolint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "hadolint" || b == "hadolint.exe" {
		return []byte("[hadolint] ok\n"), true
	}
	if npxMatches(argv, "hadolint") {
		return []byte("[hadolint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "hadolint" {
		return []byte("[hadolint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "hadolint" {
		return []byte("[hadolint] ok\n"), true
	}
	return stdout, false
}

// TryCompactMarkdownlint summarizes empty stdout from `markdownlint` / `npx|pnpm exec|yarn … markdownlint` (F09 partial).
func TryCompactMarkdownlint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "markdownlint" || b == "markdownlint.exe" {
		return []byte("[markdownlint] ok\n"), true
	}
	if npxMatches(argv, "markdownlint") {
		return []byte("[markdownlint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "markdownlint" {
		return []byte("[markdownlint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "markdownlint" {
		return []byte("[markdownlint] ok\n"), true
	}
	return stdout, false
}

// TryCompactYamllint summarizes empty stdout from `yamllint` / `python -m yamllint` / `npx|pnpm exec|yarn … yamllint` / `pnpm exec|yarn … python … -m yamllint` (F09 partial).
func TryCompactYamllint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolArgv(argv, "yamllint") {
		return stdout, false
	}
	return []byte("[yamllint] ok\n"), true
}

// TryCompactActionlint summarizes empty stdout from `actionlint` / `npx|pnpm exec|yarn … actionlint` (F09 partial).
func TryCompactActionlint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "actionlint" || b == "actionlint.exe" {
		return []byte("[actionlint] ok\n"), true
	}
	if npxMatches(argv, "actionlint") {
		return []byte("[actionlint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "actionlint" {
		return []byte("[actionlint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "actionlint" {
		return []byte("[actionlint] ok\n"), true
	}
	return stdout, false
}

// TryCompactVale summarizes empty stdout from `vale` / `npx|pnpm exec|yarn … vale` (prose linter; F09 partial).
func TryCompactVale(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "vale" || b == "vale.exe" {
		return []byte("[vale] ok\n"), true
	}
	if npxMatches(argv, "vale") {
		return []byte("[vale] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "vale" {
		return []byte("[vale] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "vale" {
		return []byte("[vale] ok\n"), true
	}
	return stdout, false
}

// TryCompactBandit summarizes empty stdout from `bandit` / `python -m bandit` / `npx|pnpm exec|yarn … bandit` / `pnpm exec|yarn … python … -m bandit` (F09 partial).
func TryCompactBandit(argv []string, stdout []byte) ([]byte, bool) {
	if !isBanditArgv(argv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	return []byte("[bandit] ok\n"), true
}

func isBanditArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		return isBanditArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isBanditArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isBanditArgv(argv[1:])
	}
	b := b0
	if b == "bandit" || b == "bandit.exe" {
		return true
	}
	if b != "python" && b != "python3" && b != "python.exe" && b != "python3.exe" {
		return false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-m" && argv[i+1] == "bandit" {
			return true
		}
	}
	return false
}

// TryCompactBiomeCheck summarizes empty stdout from `biome check` / `npx|pnpm exec|yarn … biome check` (F09 partial).
func TryCompactBiomeCheck(argv []string, stdout []byte) ([]byte, bool) {
	if !argvContainsToken(argv, "check") {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if (b0 == "biome" || b0 == "biome.exe" || b0 == "biome.cmd") && argvContainsToken(argv, "check") {
		return []byte("[biome check] ok\n"), true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && strings.EqualFold(filepath.Base(rest[0]), "biome") {
		return []byte("[biome check] ok\n"), true
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "biome") {
		return []byte("[biome check] ok\n"), true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "biome") {
		return []byte("[biome check] ok\n"), true
	}
	return stdout, false
}

// TryCompactSqlfluffLint summarizes empty stdout from `sqlfluff lint` / `python -m sqlfluff lint` / `npx|pnpm exec|yarn … sqlfluff lint` / `pnpm exec|yarn … python … -m sqlfluff lint` (F09 partial).
func TryCompactSqlfluffLint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isPyPkgToolSubcommandArgv(argv, "sqlfluff", "lint") {
		return stdout, false
	}
	return []byte("[sqlfluff lint] ok\n"), true
}

// TryCompactTaploCheck summarizes empty stdout from `taplo check` / `npx|pnpm exec|yarn … taplo check` (F09 partial).
func TryCompactTaploCheck(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !execArgvSubcommand(argv, "taplo", "check") {
		return stdout, false
	}
	return []byte("[taplo check] ok\n"), true
}

// TryCompactRubocop summarizes empty stdout from `rubocop` / `npx|pnpm exec|yarn … rubocop` (F09 / F21 partial).
func TryCompactRubocop(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "rubocop" || b == "rubocop.cmd" {
		return []byte("[rubocop] ok\n"), true
	}
	if npxMatches(argv, "rubocop") {
		return []byte("[rubocop] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "rubocop" {
		return []byte("[rubocop] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "rubocop" {
		return []byte("[rubocop] ok\n"), true
	}
	return stdout, false
}

// TryCompactMypy summarizes empty stdout from `mypy` / `python -m mypy` / `npx|pnpm exec|yarn … mypy` / `pnpm exec|yarn … python … -m mypy` (F23 partial).
// TryCompactMypy summarizes mypy output (F23): empty → ok; non-empty → errors + summary line.
func TryCompactMypy(argv []string, stdout []byte) ([]byte, bool) {
	if !isMypyArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[mypy] ok\n"), true
	}
	compact := extractMypyErrors(s)
	if compact == "" {
		return stdout, false
	}
	// Always return compact form if it has a structured result (even if similar size).
	return []byte(compact), true
}

// extractMypyErrors extracts error lines and the summary line from mypy output.
func extractMypyErrors(s string) string {
	lines := strings.Split(s, "\n")
	var errLines []string
	var summaryLine string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		tl := strings.ToLower(t)
		// Summary: "Found N errors in N files" or "Success: no issues found"
		if strings.HasPrefix(tl, "found ") && strings.Contains(tl, "error") {
			summaryLine = t
			continue
		}
		if strings.HasPrefix(tl, "success:") {
			summaryLine = t
			continue
		}
		// Error/note lines: "file.py:N: error: ..." or "file.py:N: note: ..."
		if strings.Contains(t, ": error:") || strings.Contains(t, ": note:") {
			errLines = append(errLines, t)
		}
	}
	if summaryLine == "" && len(errLines) == 0 {
		return ""
	}
	// Success path
	if len(errLines) == 0 && summaryLine != "" {
		return fmt.Sprintf("[mypy] ok (%s)\n", summaryLine)
	}
	var sb strings.Builder
	for _, l := range errLines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	if summaryLine != "" {
		sb.WriteString(summaryLine)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func isMypyArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok || len(rest) < 1 {
			return false
		}
		return isMypyArgv(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return isMypyArgv(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return isMypyArgv(argv[1:])
	}
	b := b0
	if b == "mypy" || b == "mypy.exe" {
		return true
	}
	if b != "python" && b != "python3" && b != "python.exe" && b != "python3.exe" {
		return false
	}
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "-m" && argv[i+1] == "mypy" {
			return true
		}
	}
	return false
}

// TryCompactEslint summarizes empty stdout from `eslint` / `npx|pnpm exec|yarn … eslint` (F09 partial).
func TryCompactEslint(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "eslint" || b == "eslint.cmd" {
		return []byte("[eslint] ok\n"), true
	}
	if npxMatches(argv, "eslint") {
		return []byte("[eslint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "eslint" {
		return []byte("[eslint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "eslint" {
		return []byte("[eslint] ok\n"), true
	}
	return stdout, false
}

// TryCompactStylelint summarizes empty stdout from `stylelint` / `pnpm exec|yarn … stylelint` (F09 partial).
func TryCompactStylelint(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "stylelint" || b == "stylelint.cmd" {
		return []byte("[stylelint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "stylelint" {
		return []byte("[stylelint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "stylelint" {
		return []byte("[stylelint] ok\n"), true
	}
	if npxMatches(argv, "stylelint") {
		return []byte("[stylelint] ok\n"), true
	}
	return stdout, false
}

// TryCompactOxlint summarizes empty stdout from `oxlint` / `npx|pnpm exec|yarn … oxlint` (F09 partial).
func TryCompactOxlint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "oxlint" || b == "oxlint.exe" {
		return []byte("[oxlint] ok\n"), true
	}
	if npxMatches(argv, "oxlint") {
		return []byte("[oxlint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "oxlint" {
		return []byte("[oxlint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "oxlint" {
		return []byte("[oxlint] ok\n"), true
	}
	return stdout, false
}

// TryCompactDenoLint summarizes empty stdout from `deno lint` / `npx|pnpm exec|yarn … deno lint` (F09 partial).
func TryCompactDenoLint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b == "deno" || b == "deno.exe") && argv[1] == "lint" {
		return []byte("[deno lint] ok\n"), true
	}
	if npxMatches(argv, "deno", "lint") {
		return []byte("[deno lint] ok\n"), true
	}
	if len(argv) >= 4 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "deno" && argv[3] == "lint" {
		return []byte("[deno lint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "deno" && argv[2] == "lint" {
		return []byte("[deno lint] ok\n"), true
	}
	return stdout, false
}

func isDartAnalyzeArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if (b0 == "dart" || b0 == "dart.exe") && argv[1] == "analyze" {
		return true
	}
	if len(argv) >= 3 && (b0 == "fvm" || b0 == "fvm.exe") && argv[1] == "dart" && argv[2] == "analyze" {
		return true
	}
	if npxMatches(argv, "dart", "analyze") {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 3 {
		r0 := strings.ToLower(filepath.Base(rest[0]))
		if (r0 == "fvm" || r0 == "fvm.exe") && rest[1] == "dart" && rest[2] == "analyze" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "dart") && argv[3] == "analyze" {
		return true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "dart") && argv[2] == "analyze" {
		return true
	}
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		r2 := strings.ToLower(filepath.Base(argv[2]))
		if (r2 == "fvm" || r2 == "fvm.exe") && argv[3] == "dart" && argv[4] == "analyze" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		r1 := strings.ToLower(filepath.Base(argv[1]))
		if (r1 == "fvm" || r1 == "fvm.exe") && argv[2] == "dart" && argv[3] == "analyze" {
			return true
		}
	}
	return false
}

// TryCompactDartAnalyze summarizes empty stdout from `dart analyze` / `fvm dart analyze` / `npx|pnpm exec|yarn … dart analyze` (F09 partial).
func TryCompactDartAnalyze(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isDartAnalyzeArgv(argv) {
		return stdout, false
	}
	return []byte("[dart analyze] ok\n"), true
}

func isFlutterAnalyzeArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if (b0 == "flutter" || b0 == "flutter.bat" || b0 == "flutter.cmd") && argv[1] == "analyze" {
		return true
	}
	if len(argv) >= 3 && (b0 == "fvm" || b0 == "fvm.exe") && argv[1] == "flutter" && argv[2] == "analyze" {
		return true
	}
	if npxMatches(argv, "flutter", "analyze") {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 3 {
		r0 := strings.ToLower(filepath.Base(rest[0]))
		if (r0 == "fvm" || r0 == "fvm.exe") && rest[1] == "flutter" && rest[2] == "analyze" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "flutter") && argv[3] == "analyze" {
		return true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "flutter") && argv[2] == "analyze" {
		return true
	}
	if len(argv) >= 5 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		r2 := strings.ToLower(filepath.Base(argv[2]))
		if (r2 == "fvm" || r2 == "fvm.exe") && argv[3] == "flutter" && argv[4] == "analyze" {
			return true
		}
	}
	if len(argv) >= 4 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		r1 := strings.ToLower(filepath.Base(argv[1]))
		if (r1 == "fvm" || r1 == "fvm.exe") && argv[2] == "flutter" && argv[3] == "analyze" {
			return true
		}
	}
	return false
}

// TryCompactFlutterAnalyze summarizes empty stdout from `flutter analyze` / `fvm flutter analyze` / `npx|pnpm exec|yarn … flutter analyze` (F09 partial).
func TryCompactFlutterAnalyze(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !isFlutterAnalyzeArgv(argv) {
		return stdout, false
	}
	return []byte("[flutter analyze] ok\n"), true
}

// TryCompactSwiftlint summarizes empty stdout from `swiftlint` / `npx|pnpm exec|yarn … swiftlint` (F09 partial).
func TryCompactSwiftlint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "swiftlint" || b == "swiftlint.exe" {
		return []byte("[swiftlint] ok\n"), true
	}
	if npxMatches(argv, "swiftlint") {
		return []byte("[swiftlint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "swiftlint" {
		return []byte("[swiftlint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "swiftlint" {
		return []byte("[swiftlint] ok\n"), true
	}
	return stdout, false
}

// TryCompactKtlint summarizes empty stdout from `ktlint` / `npx|pnpm exec|yarn … ktlint` (F09 partial).
func TryCompactKtlint(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "ktlint" || b == "ktlint.exe" {
		return []byte("[ktlint] ok\n"), true
	}
	if npxMatches(argv, "ktlint") {
		return []byte("[ktlint] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "ktlint" {
		return []byte("[ktlint] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "ktlint" {
		return []byte("[ktlint] ok\n"), true
	}
	return stdout, false
}

// TryCompactDetekt summarizes empty stdout from `detekt` / `npx|pnpm exec|yarn … detekt` (Kotlin; F09 partial).
func TryCompactDetekt(argv []string, stdout []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "detekt" || b == "detekt.exe" {
		return []byte("[detekt] ok\n"), true
	}
	if npxMatches(argv, "detekt") {
		return []byte("[detekt] ok\n"), true
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" && argv[2] == "detekt" {
		return []byte("[detekt] ok\n"), true
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") && argv[1] == "detekt" {
		return []byte("[detekt] ok\n"), true
	}
	return stdout, false
}

func argvContainsToken(argv []string, tok string) bool {
	for _, a := range argv[1:] {
		if a == tok {
			return true
		}
	}
	return false
}

// TryCompactLintOutput chains common linters with empty-success stdout.
func TryCompactLintOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactCargoClippy(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCargoAudit(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGolangciLint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactStaticcheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGocritic(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGosec(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBufLint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactProtolint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSemgrep(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactJscpd(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDjlint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTyCheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGofumpt(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRevive(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactErrcheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactIneffassign(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactNilaway(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGoVet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactUnparam(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMisspell(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactGocyclo(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactForbidigo(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPrealloc(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRuffCheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPylint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactFlake8(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBandit(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactBiomeCheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSqlfluffLint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTaploCheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCueVet(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSpectralLint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactOxlint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDenoLint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDartAnalyze(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactFlutterAnalyze(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactSwiftlint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactKtlint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDetekt(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactShellcheck(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactAnsibleLint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactHadolint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMarkdownlint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactYamllint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactDotenvLinter(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactKubeLinter(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactTflint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactCfnLint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactActionlint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactZizmor(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactVale(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactRubocop(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPhpcs(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPhpstan(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPsalm(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPhan(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMypy(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPyright(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactEslint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactStylelint(argv, stdout); ok {
		return out, true
	}
	// Fallback: truncate large lint violation output for recognized lint tools.
	if label := lintToolLabel(argv); label != "" {
		s := strings.TrimSpace(string(stdout))
		if s != "" {
			if out, ok := truncateLintViolations(s, label); ok {
				return []byte(out), true
			}
		}
	}
	return stdout, false
}

// lintToolLabel returns the compact label for argv if it is a recognized lint command, else "".
func lintToolLabel(argv []string) string {
	switch {
	case isCargoClippyArgv(argv):
		return "cargo clippy"
	case isCargoAuditArgv(argv):
		return "cargo audit"
	case isGoVetArgv(argv):
		return "go vet"
	case isPylintArgv(argv):
		return "pylint"
	case isFlake8Argv(argv):
		return "flake8"
	case isBanditArgv(argv):
		return "bandit"
	case isMypyArgv(argv):
		return "mypy"
	case isDartAnalyzeArgv(argv):
		return "dart analyze"
	case isFlutterAnalyzeArgv(argv):
		return "flutter analyze"
	}
	type binSub struct{ bin, sub, label string }
	tools := []binSub{
		{"golangci-lint", "", "golangci-lint"},
		{"staticcheck", "", "staticcheck"},
		{"gocritic", "check", "gocritic"},
		{"gosec", "", "gosec"},
		{"buf", "lint", "buf lint"},
		{"protolint", "", "protolint"},
		{"semgrep", "", "semgrep"},
		{"jscpd", "", "jscpd"},
		{"djlint", "", "djlint"},
		{"ty", "check", "ty check"},
		{"gofumpt", "", "gofumpt"},
		{"revive", "", "revive"},
		{"errcheck", "", "errcheck"},
		{"ineffassign", "", "ineffassign"},
		{"nilaway", "", "nilaway"},
		{"unparam", "", "unparam"},
		{"misspell", "", "misspell"},
		{"gocyclo", "", "gocyclo"},
		{"forbidigo", "", "forbidigo"},
		{"prealloc", "", "prealloc"},
		{"ruff", "check", "ruff check"},
		{"biome", "check", "biome check"},
		{"sqlfluff", "lint", "sqlfluff lint"},
		{"taplo", "check", "taplo check"},
		{"cue", "vet", "cue vet"},
		{"spectral", "lint", "spectral lint"},
		{"oxlint", "", "oxlint"},
		{"deno", "lint", "deno lint"},
		{"swiftlint", "", "swiftlint"},
		{"ktlint", "", "ktlint"},
		{"detekt", "", "detekt"},
		{"shellcheck", "", "shellcheck"},
		{"ansible-lint", "", "ansible-lint"},
		{"hadolint", "", "hadolint"},
		{"markdownlint", "", "markdownlint"},
		{"yamllint", "", "yamllint"},
		{"dotenv-linter", "", "dotenv-linter"},
		{"kube-linter", "", "kube-linter"},
		{"tflint", "", "tflint"},
		{"cfn-lint", "", "cfn-lint"},
		{"actionlint", "", "actionlint"},
		{"zizmor", "", "zizmor"},
		{"vale", "", "vale"},
		{"rubocop", "", "rubocop"},
		{"pint", "", "pint"},
		{"phpcs", "", "phpcs"},
		{"phpstan", "", "phpstan"},
		{"psalm", "", "psalm"},
		{"phan", "", "phan"},
		{"pyright", "", "pyright"},
		{"basedpyright", "", "pyright"},
		{"eslint", "", "eslint"},
		{"stylelint", "", "stylelint"},
	}
	for _, t := range tools {
		if isSingleBinarySubcmdArgv(argv, t.bin, t.sub) {
			return t.label
		}
	}
	return ""
}
