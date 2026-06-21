package filter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
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

// TryCompactCargoClippy summarizes empty and parser-proven clean stdout from
// `cargo clippy` / `npx|pnpm exec|yarn ... cargo clippy`.
func TryCompactCargoClippy(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoClippyArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[cargo clippy] ok\n"), true
	}
	if compacted, ok := compactCargoClippyCleanOutput(s, len(stdout)); ok {
		return compacted, true
	}
	return stdout, false
}

func compactCargoClippyCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	lines := strings.Split(stdout, "\n")
	sawFinished := false
	sawProgress := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if cargoClippyLineHasUnsafeMarker(line) {
			return nil, false
		}
		switch {
		case cargoClippyProgressLine(line):
			sawProgress = true
		case cargoClippyFinishedLine(line):
			sawFinished = true
		default:
			return nil, false
		}
	}
	if !sawFinished || !sawProgress {
		return nil, false
	}
	out := []byte("[cargo clippy] ok\n")
	if len(out) >= originalLen {
		return nil, false
	}
	return out, true
}

func cargoClippyProgressLine(line string) bool {
	for _, prefix := range []string{"Checking ", "Compiling "} {
		if strings.HasPrefix(line, prefix) && strings.TrimSpace(strings.TrimPrefix(line, prefix)) != "" {
			return true
		}
	}
	return false
}

func cargoClippyFinishedLine(line string) bool {
	if !strings.HasPrefix(line, "Finished ") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "Finished "))
	return strings.Contains(rest, "profile") && strings.Contains(rest, "target(s) in")
}

func cargoClippyLineHasUnsafeMarker(line string) bool {
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

// TryCompactGolangciLint summarizes empty stdout and parser-proven diagnostics
// from golangci-lint / `npx|pnpm exec|yarn ... golangci-lint`.
func TryCompactGolangciLint(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "golangci-lint")
}

// TryCompactStaticcheck summarizes empty stdout and parser-proven diagnostics
// from `staticcheck` / `npx|pnpm exec|yarn ... staticcheck`.
func TryCompactStaticcheck(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "staticcheck")
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

// TryCompactPyright summarizes empty and parser-proven clean output from
// `pyright` / `basedpyright` / wrapped pyright commands.
func TryCompactPyright(argv []string, stdout []byte) ([]byte, bool) {
	if !isPyrightArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[pyright] ok\n"), true
	}
	if compacted, ok := compactPyrightJSONSuccess([]byte(s)); ok {
		return compacted, true
	}
	if compacted, ok := compactPyrightTextSuccess(s); ok {
		return compacted, true
	}
	return stdout, false
}

func isPyrightArgv(argv []string) bool {
	if len(argv) < 1 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b == "pyright" || b == "pyright.exe" || b == "basedpyright" || b == "basedpyright.exe" {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 {
		switch strings.ToLower(filepath.Base(rest[0])) {
		case "pyright", "basedpyright":
			return true
		}
	}
	if len(argv) >= 3 && (b == "pnpm" || b == "pnpm.cmd") && argv[1] == "exec" {
		switch strings.ToLower(filepath.Base(argv[2])) {
		case "pyright", "basedpyright":
			return true
		}
	}
	if len(argv) >= 2 && (b == "yarn" || b == "yarn.cmd" || b == "yarnpkg") {
		switch strings.ToLower(filepath.Base(argv[1])) {
		case "pyright", "basedpyright":
			return true
		}
	}
	return false
}

type pyrightJSONReport struct {
	Version            string                   `json:"version"`
	Time               string                   `json:"time"`
	GeneralDiagnostics []json.RawMessage        `json:"generalDiagnostics"`
	Summary            pyrightJSONReportSummary `json:"summary"`
}

type pyrightJSONReportSummary struct {
	FilesAnalyzed    int     `json:"filesAnalyzed"`
	ErrorCount       int     `json:"errorCount"`
	WarningCount     int     `json:"warningCount"`
	InformationCount int     `json:"informationCount"`
	TimeInSec        float64 `json:"timeInSec"`
}

func compactPyrightJSONSuccess(stdout []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var report pyrightJSONReport
	if err := dec.Decode(&report); err != nil {
		return nil, false
	}
	if report.Summary.FilesAnalyzed <= 0 ||
		report.Summary.ErrorCount != 0 ||
		report.Summary.WarningCount != 0 ||
		report.Summary.InformationCount != 0 ||
		len(report.GeneralDiagnostics) != 0 {
		return nil, false
	}
	out := fmt.Appendf(nil, "[pyright --outputjson] ok (%d files analyzed)\n", report.Summary.FilesAnalyzed)
	if len(out) >= len(stdout) {
		return nil, false
	}
	return out, true
}

func compactPyrightTextSuccess(stdout string) ([]byte, bool) {
	lines := strings.Split(stdout, "\n")
	filesAnalyzed := -1
	sawSummary := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if n, ok := parsePyrightSourceCountLine(line); ok {
			filesAnalyzed = n
			continue
		}
		if strings.HasPrefix(line, "Found ") {
			return nil, false
		}
		if errors, warnings, infos, ok := parsePyrightTextSummaryLine(line); ok {
			if errors == 0 && warnings == 0 && infos == 0 {
				sawSummary = true
				continue
			}
			return nil, false
		}
		return nil, false
	}
	if !sawSummary {
		return nil, false
	}
	out := []byte("[pyright] ok\n")
	if filesAnalyzed >= 0 {
		out = fmt.Appendf(nil, "[pyright] ok (%d files analyzed)\n", filesAnalyzed)
	}
	if len(out) >= len(stdout) {
		return nil, false
	}
	return out, true
}

func parsePyrightSourceCountLine(line string) (int, bool) {
	const prefix = "Found "
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}
	for _, suffix := range []string{" source files", " source file"} {
		if !strings.HasSuffix(line, suffix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, prefix), suffix)))
		if err != nil || n < 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func parsePyrightTextSummaryLine(line string) (int, int, int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 6 {
		return 0, 0, 0, false
	}
	if fields[1] != "errors," || fields[3] != "warnings," {
		return 0, 0, 0, false
	}
	if fields[5] != "informations" && fields[5] != "notes" {
		return 0, 0, 0, false
	}
	errors, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, 0, false
	}
	warnings, err := strconv.Atoi(fields[2])
	if err != nil {
		return 0, 0, 0, false
	}
	infos, err := strconv.Atoi(fields[4])
	if err != nil {
		return 0, 0, 0, false
	}
	return errors, warnings, infos, true
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

// TryCompactRevive summarizes empty stdout and parser-proven diagnostics from
// `revive` / `npx|pnpm exec|yarn ... revive`.
func TryCompactRevive(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "revive")
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

// TryCompactErrcheck summarizes empty stdout and parser-proven diagnostics from `errcheck` / `npx|pnpm exec|yarn … errcheck`.
func TryCompactErrcheck(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "errcheck")
}

// TryCompactIneffassign summarizes empty stdout and parser-proven diagnostics from `ineffassign` / `npx|pnpm exec|yarn … ineffassign`.
func TryCompactIneffassign(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "ineffassign")
}

// TryCompactNilaway summarizes empty stdout and parser-proven diagnostics from `nilaway` / `npx|pnpm exec|yarn … nilaway`.
func TryCompactNilaway(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "nilaway")
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

// TryCompactUnparam summarizes empty stdout and parser-proven diagnostics from `unparam` / `npx|pnpm exec|yarn … unparam`.
func TryCompactUnparam(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "unparam")
}

// TryCompactMisspell summarizes empty stdout and parser-proven diagnostics from `misspell` / `npx|pnpm exec|yarn … misspell`.
func TryCompactMisspell(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "misspell")
}

// TryCompactGocyclo summarizes empty stdout and parser-proven diagnostics from `gocyclo` / `npx|pnpm exec|yarn … gocyclo`.
func TryCompactGocyclo(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "gocyclo")
}

// TryCompactForbidigo summarizes empty stdout and parser-proven diagnostics from `forbidigo` / `npx|pnpm exec|yarn … forbidigo`.
func TryCompactForbidigo(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "forbidigo")
}

// TryCompactPrealloc summarizes empty stdout and parser-proven diagnostics from `prealloc` / `npx|pnpm exec|yarn … prealloc`.
func TryCompactPrealloc(argv []string, stdout []byte) ([]byte, bool) {
	return compactFocusedLintOutput(argv, stdout, "prealloc")
}

// TryCompactPreCommit summarizes parser-proven all-passed output from
// `pre-commit run`. Failure, warning, skipped, malformed, and unknown lines
// fail open so hook detail stays visible.
func TryCompactPreCommit(argv []string, stdout []byte) ([]byte, bool) {
	if !isPreCommitRunArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return stdout, false
	}
	hooks, ok := countPreCommitPassedHooks(s)
	if !ok || hooks <= 0 {
		return stdout, false
	}
	hookWord := "hooks"
	if hooks == 1 {
		hookWord = "hook"
	}
	out := fmt.Appendf(nil, "[pre-commit] ok (%d %s passed)\n", hooks, hookWord)
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func isPreCommitRunArgv(argv []string) bool {
	return isSingleBinarySubcmdArgv(argv, "pre-commit", "run")
}

func countPreCommitPassedHooks(stdout string) (int, bool) {
	count := 0
	for raw := range strings.SplitSeq(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if preCommitAllowedInfoLine(line) {
			continue
		}
		if !preCommitPassedHookLine(line) {
			return 0, false
		}
		count++
	}
	return count, count > 0
}

func preCommitAllowedInfoLine(line string) bool {
	switch {
	case strings.HasPrefix(line, "[INFO] Installing environment for "):
		return strings.TrimSpace(strings.TrimPrefix(line, "[INFO] Installing environment for ")) != ""
	case strings.HasPrefix(line, "[INFO] Initializing environment for "):
		return strings.TrimSpace(strings.TrimPrefix(line, "[INFO] Initializing environment for ")) != ""
	case line == "[INFO] Once installed this environment will be reused.":
		return true
	case line == "[INFO] This may take a few minutes...":
		return true
	default:
		return false
	}
}

func preCommitPassedHookLine(line string) bool {
	const status = "Passed"
	if !strings.HasSuffix(line, status) {
		return false
	}
	prefix := strings.TrimSpace(strings.TrimSuffix(line, status))
	if prefix == "" {
		return false
	}
	dotCount := 0
	for i := len(prefix) - 1; i >= 0 && prefix[i] == '.'; i-- {
		dotCount++
	}
	if dotCount < 3 {
		return false
	}
	hookName := strings.TrimSpace(prefix[:len(prefix)-dotCount])
	return hookName != ""
}

func compactFocusedLintOutput(argv []string, stdout []byte, tool string) ([]byte, bool) {
	if out, ok := tryCompactEmptyStdoutSingleBinary(argv, stdout, tool); ok {
		return out, true
	}
	compact, hadFailures, ok := parseFocusedLintDiagnostics(tool, string(stdout))
	if !ok || !hadFailures || !focusedLintDiagnosticArgvMatchesTool(argv, tool) {
		return stdout, false
	}
	return []byte(compact), true
}

func focusedLintDiagnosticArgvMatchesTool(argv []string, tool string) bool {
	label, ok := focusedLintDiagnosticLabel(argv)
	return ok && label == tool
}

// TryCompactRuffCheck summarizes empty stdout and exact all-clear output when
// argv includes `ruff` ... `check` / `python -m ruff ... check` /
// `npx|pnpm exec|yarn ... ruff check` /
// `pnpm exec|yarn ... python ... -m ruff ... check`.
func TryCompactRuffCheck(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 || !argvContainsToken(argv, "check") {
		return stdout, false
	}
	if !isRuffArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[ruff check] ok\n"), true
	}
	if compacted, ok := compactRuffCheckCleanOutput(s, len(stdout)); ok {
		return compacted, true
	}
	return stdout, false
}

func compactRuffCheckCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	if stdout != "All checks passed!" {
		return nil, false
	}
	out := []byte("[ruff check] ok\n")
	if len(out) >= originalLen {
		return nil, false
	}
	return out, true
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

// TryCompactBiomeCheck summarizes empty stdout and exact clean summaries from
// `biome check` / `npx|pnpm exec|yarn ... biome check`.
func TryCompactBiomeCheck(argv []string, stdout []byte) ([]byte, bool) {
	if !argvContainsToken(argv, "check") {
		return stdout, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	matched := false
	if (b0 == "biome" || b0 == "biome.exe" || b0 == "biome.cmd") && argvContainsToken(argv, "check") {
		matched = true
	} else if rest, ok := npxArgvSuffix(argv); ok && len(rest) >= 1 && strings.EqualFold(filepath.Base(rest[0]), "biome") {
		matched = true
	} else if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && strings.EqualFold(filepath.Base(argv[2]), "biome") {
		matched = true
	} else if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && strings.EqualFold(filepath.Base(argv[1]), "biome") {
		matched = true
	}
	if !matched {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[biome check] ok\n"), true
	}
	if compacted, ok := compactBiomeCheckCleanOutput(s, len(stdout)); ok {
		return compacted, true
	}
	return stdout, false
}

func compactBiomeCheckCleanOutput(stdout string, originalLen int) ([]byte, bool) {
	if strings.Contains(stdout, "\n") || strings.Contains(stdout, "\r") {
		return nil, false
	}
	const prefix = "Checked "
	const suffix = ". No fixes applied."
	if !strings.HasPrefix(stdout, prefix) || !strings.HasSuffix(stdout, suffix) {
		return nil, false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(stdout, prefix), suffix)
	countText, rest, ok := strings.Cut(middle, " ")
	if !ok {
		return nil, false
	}
	count, err := strconv.Atoi(countText)
	if err != nil || count <= 0 {
		return nil, false
	}
	if after, ok0 := strings.CutPrefix(rest, "file in "); ok0 {
		if count != 1 || strings.TrimSpace(after) == "" {
			return nil, false
		}
	} else if after, ok0 := strings.CutPrefix(rest, "files in "); ok0 {
		if count == 1 || strings.TrimSpace(after) == "" {
			return nil, false
		}
	} else {
		return nil, false
	}
	out := fmt.Appendf(nil, "[biome check] ok (%d files checked)\n", count)
	if count == 1 {
		out = []byte("[biome check] ok (1 file checked)\n")
	}
	if len(out) >= originalLen {
		return nil, false
	}
	return out, true
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

// TryCompactMypyDiagnostics compacts only fully understood mypy failure output.
// Unlike TryCompactMypy, this strict path fails open on stub notices, progress,
// source context, or any other line that is not an error, note, or final count.
func TryCompactMypyDiagnostics(argv []string, stdout []byte) ([]byte, bool) {
	if !isMypyArgv(argv) {
		return stdout, false
	}
	compact, ok := compactStrictMypyDiagnostics(string(stdout))
	if !ok || len(compact) >= len(stdout) {
		return stdout, false
	}
	return []byte(compact), true
}

func compactStrictMypyDiagnostics(stdout string) (string, bool) {
	if strings.TrimSpace(stdout) == "" {
		return "", false
	}
	lines := strings.Split(stdout, "\n")
	diagnostics := make([]string, 0, len(lines))
	errorCount := 0
	summaryCount := 0
	summary := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if count, ok := strictMypyErrorSummary(line); ok {
			if summary != "" {
				return "", false
			}
			summary = line
			summaryCount = count
			continue
		}
		kind, ok := strictMypyDiagnosticKind(line)
		if !ok {
			return "", false
		}
		diagnostics = append(diagnostics, line)
		if kind == "error" {
			errorCount++
		}
	}
	if errorCount == 0 || summary == "" || summaryCount != errorCount {
		return "", false
	}
	result := "[mypy] FAILED (" + diagnosticCountText(len(diagnostics)) + ")\n" +
		strings.Join(compactAdjacentFocusedLintDiagnostics(diagnostics), "\n") + "\n" +
		summary + "\n"
	return result, true
}

func strictMypyDiagnosticKind(line string) (string, bool) {
	before, after, ok := strings.Cut(line, ": ")
	if !ok || (!strings.Contains(before, ".py:") && !strings.Contains(before, ".pyi:")) {
		return "", false
	}
	if !strings.Contains(before, ":") || strings.ContainsAny(before, "\t") {
		return "", false
	}
	switch {
	case strings.HasPrefix(after, "error:"):
		return "error", true
	case strings.HasPrefix(after, "note:"):
		return "note", true
	default:
		return "", false
	}
}

func strictMypyErrorSummary(line string) (int, bool) {
	fields := strings.Fields(strings.ToLower(line))
	if len(fields) < 6 || fields[0] != "found" || fields[3] != "in" {
		return 0, false
	}
	if fields[2] != "error" && fields[2] != "errors" {
		return 0, false
	}
	if fields[5] != "file" && fields[5] != "files" {
		return 0, false
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count <= 0 {
		return 0, false
	}
	fileCount, err := strconv.Atoi(fields[4])
	if err != nil || fileCount <= 0 {
		return 0, false
	}
	if len(fields) == 6 {
		return count, true
	}
	if len(fields) == 10 && fields[6] == "(checked" && fields[8] == "source" &&
		(strings.HasSuffix(fields[9], "file)") || strings.HasSuffix(fields[9], "files)")) {
		checked, err := strconv.Atoi(fields[7])
		return count, err == nil && checked > 0
	}
	return 0, false
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
	return slices.Contains(argv[1:], tok)
}

// TryCompactLintOutput chains common linters with empty-success stdout.
func TryCompactLintOutput(argv []string, stdout []byte) ([]byte, bool) {
	if out, ok := TryCompactSARIF(argv, stdout); ok {
		return out, true
	}
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
	if out, ok := TryCompactPreCommit(argv, stdout); ok {
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
	if out, ok := TryCompactMypyDiagnostics(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactMypy(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactPyright(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactEslintStylish(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactEslint(argv, stdout); ok {
		return out, true
	}
	if out, ok := TryCompactStylelint(argv, stdout); ok {
		return out, true
	}
	if out, ok := compactPackageManagerLintScriptOutput(argv, stdout); ok {
		return out, true
	}
	if compact, ok := ParseFailures(argv, string(stdout)); ok {
		return []byte(compact), true
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
