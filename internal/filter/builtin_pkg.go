package filter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// compactEmptyStdoutWithNpxPnpmYarn applies match to argv, npx argv suffix, pnpm exec tail, and yarn tail.
func compactEmptyStdoutWithNpxPnpmYarn(argv []string, stdout []byte, match func([]string) bool, okLine []byte) ([]byte, bool) {
	if strings.TrimSpace(string(stdout)) != "" {
		return stdout, false
	}
	if !packageArgvMatchesWithNpxPnpmYarn(argv, match) {
		return stdout, false
	}
	return okLine, true
}

func packageArgvMatchesWithNpxPnpmYarn(argv []string, match func([]string) bool) bool {
	if len(argv) < 1 {
		return false
	}
	if match(argv) {
		return true
	}
	if rest, ok := npxArgvSuffix(argv); ok && match(rest) {
		return true
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		if match(argv[2:]) {
			return true
		}
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		if match(argv[1:]) {
			return true
		}
	}
	return false
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

// TryCompactNpmInstall summarizes empty stdout or strict clean success from `npm install` / `npm ci` (F12 partial).
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
	if npmInstallArgvUnsafe(argv[2:]) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return compactNpmInstallCleanSuccess(stdout, "npm "+argv[1])
	}
	return []byte(fmt.Sprintf("[npm %s] ok\n", argv[1])), true
}

// TryCompactPnpmInstall summarizes empty stdout or strict clean success from `pnpm install` / `pnpm ci` / `pnpm update` (F12 partial).
func TryCompactPnpmInstall(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "pnpm" && b != "pnpm.cmd" {
		return stdout, false
	}
	switch argv[1] {
	case "install", "ci", "update":
	default:
		return stdout, false
	}
	if pnpmInstallArgvUnsafe(argv[2:]) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return compactPnpmInstallCleanSuccess(stdout, "pnpm "+argv[1])
	}
	return []byte(fmt.Sprintf("[pnpm %s] ok\n", argv[1])), true
}

// TryCompactYarnInstall summarizes empty stdout or strict Yarn Classic clean success from `yarn install` / `yarn upgrade` (F12 partial).
func TryCompactYarnInstall(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 2 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if (b != "yarn" && b != "yarn.cmd" && b != "yarnpkg") || (argv[1] != "install" && argv[1] != "upgrade") {
		return stdout, false
	}
	if yarnInstallArgvUnsafe(argv[2:]) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) != "" {
		return compactYarnClassicInstallCleanSuccess(stdout, "yarn "+argv[1], argv[1])
	}
	return []byte(fmt.Sprintf("[yarn %s] ok\n", argv[1])), true
}

func packageAuditJSONLabel(argv []string) (string, bool) {
	if len(argv) < 3 {
		return "", false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if argv[1] != "audit" || !packageAuditHasJSONFlag(argv[2:]) {
		return "", false
	}
	switch b0 {
	case "npm", "npm.cmd":
		return "npm audit", true
	case "pnpm", "pnpm.cmd":
		return "pnpm audit", true
	default:
		return "", false
	}
}

func packageAuditHasJSONFlag(args []string) bool {
	hasJSON := false
	for _, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		switch lower {
		case "--json", "--json=true", "--json=1":
			hasJSON = true
		case "--json=false", "--json=0":
			return false
		}
	}
	return hasJSON
}

// TryCompactPackageAuditJSON summarizes npm/pnpm audit JSON only when every
// standard severity count is explicitly present and zero.
func TryCompactPackageAuditJSON(argv []string, stdout []byte) ([]byte, bool) {
	label, ok := packageAuditJSONLabel(argv)
	if !ok {
		return stdout, false
	}
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" || !packageAuditJSONZeroVulnerabilities([]byte(trimmed)) {
		return stdout, false
	}
	out := []byte("[" + label + "] 0 vulnerabilities\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func packageAuditJSONZeroVulnerabilities(raw []byte) bool {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return false
	}
	metadataRaw, ok := root["metadata"]
	if !ok {
		return false
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return false
	}
	countsRaw, ok := metadata["vulnerabilities"]
	if !ok || !packageAuditSeverityCountsZero(countsRaw) {
		return false
	}
	if vulnerabilitiesRaw, ok := root["vulnerabilities"]; ok && !jsonObjectIsEmpty(vulnerabilitiesRaw) {
		return false
	}
	if advisoriesRaw, ok := root["advisories"]; ok && !jsonObjectIsEmpty(advisoriesRaw) {
		return false
	}
	if actionsRaw, ok := root["actions"]; ok && !jsonArrayIsEmpty(actionsRaw) {
		return false
	}
	return true
}

func packageAuditSeverityCountsZero(raw json.RawMessage) bool {
	var counts map[string]int
	if err := json.Unmarshal(raw, &counts); err != nil {
		return false
	}
	required := map[string]struct{}{
		"info":     {},
		"low":      {},
		"moderate": {},
		"high":     {},
		"critical": {},
		"total":    {},
	}
	if len(counts) != len(required) {
		return false
	}
	for severity := range required {
		value, ok := counts[severity]
		if !ok || value != 0 {
			return false
		}
	}
	return true
}

func jsonObjectIsEmpty(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil && len(obj) == 0
}

func jsonArrayIsEmpty(raw json.RawMessage) bool {
	var arr []json.RawMessage
	return json.Unmarshal(raw, &arr) == nil && len(arr) == 0
}

// TryCompactPoetryInstall summarizes empty stdout or strict clean success from `poetry install` / `npx|pnpm exec|yarn … poetry install` (F12 partial).
func TryCompactPoetryInstall(argv []string, stdout []byte) ([]byte, bool) {
	if !packageArgvMatchesWithNpxPnpmYarn(argv, isPoetryInstallArgv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[poetry install] ok\n"), true
	}
	return compactPoetryInstallSuccess(stdout)
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

// TryCompactBunInstall summarizes empty stdout or strict clean success from `bun install` / `npx|pnpm exec|yarn … bun install` (F12 partial).
func TryCompactBunInstall(argv []string, stdout []byte) ([]byte, bool) {
	matched, ok := bunInstallArgvSuffix(argv)
	if !ok {
		return stdout, false
	}
	if bunInstallArgvUnsafe(matched[2:]) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[bun install] ok\n"), true
	}
	return compactBunInstallCleanSuccess(stdout)
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

// TryCompactUvPipInstall summarizes empty stdout or strict clean success from `uv pip install …` / `npx|pnpm exec|yarn … uv pip install …` (F12 partial).
func TryCompactUvPipInstall(argv []string, stdout []byte) ([]byte, bool) {
	if !packageArgvMatchesWithNpxPnpmYarn(argv, isUvPipInstallArgv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[uv pip install] ok\n"), true
	}
	return compactUvPackageSuccess(stdout, "uv pip install")
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

// TryCompactUvSync summarizes empty stdout or strict clean success from `uv sync` / `npx|pnpm exec|yarn … uv sync` (F12 partial).
func TryCompactUvSync(argv []string, stdout []byte) ([]byte, bool) {
	if !packageArgvMatchesWithNpxPnpmYarn(argv, isUvSyncArgv) {
		return stdout, false
	}
	if strings.TrimSpace(string(stdout)) == "" {
		return []byte("[uv sync] ok\n"), true
	}
	return compactUvPackageSuccess(stdout, "uv sync")
}

func compactPoetryInstallSuccess(stdout []byte) ([]byte, bool) {
	text := string(stdout)
	var sawDependencyHeader bool
	var noDependencies bool
	var lockWritten bool
	var currentProject bool
	var installs, updates, removals int
	var expectedOperations *int
	var bulletOperations int
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case packageOutputLineUnsafe(trimmed, lower):
			return stdout, false
		case trimmed == "Installing dependencies from lock file":
			sawDependencyHeader = true
		case trimmed == "No dependencies to install or update" || trimmed == "No changes.":
			noDependencies = true
		case trimmed == "Writing lock file":
			lockWritten = true
		case strings.HasPrefix(trimmed, "Installing the current project: "):
			if strings.TrimSpace(strings.TrimPrefix(trimmed, "Installing the current project: ")) == "" {
				return stdout, false
			}
			currentProject = true
		case strings.HasPrefix(trimmed, "Package operations: "):
			parsedInstalls, parsedUpdates, parsedRemovals, ok := parsePoetryPackageOperations(trimmed)
			if !ok {
				return stdout, false
			}
			installs, updates, removals = parsedInstalls, parsedUpdates, parsedRemovals
			total := installs + updates + removals
			expectedOperations = &total
		case strings.HasPrefix(trimmed, "- Installing ") ||
			strings.HasPrefix(trimmed, "- Updating ") ||
			strings.HasPrefix(trimmed, "- Removing "):
			if !poetryPackageBulletLineOK(trimmed) {
				return stdout, false
			}
			bulletOperations++
		default:
			return stdout, false
		}
	}
	if expectedOperations != nil && *expectedOperations != bulletOperations {
		return stdout, false
	}
	if expectedOperations == nil && !noDependencies && !currentProject {
		return stdout, false
	}
	parts := make([]string, 0, 4)
	if expectedOperations != nil {
		parts = append(parts, poetryOperationsSummary(installs, updates, removals))
	}
	if noDependencies {
		parts = append(parts, "up to date")
	}
	if currentProject {
		parts = append(parts, "current project installed")
	}
	if lockWritten {
		parts = append(parts, "lock file written")
	}
	if len(parts) == 0 || (!sawDependencyHeader && expectedOperations == nil) {
		return stdout, false
	}
	out := []byte("[poetry install] ok (" + strings.Join(parts, "; ") + ")\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func parsePoetryPackageOperations(line string) (int, int, int, bool) {
	rest := strings.TrimPrefix(line, "Package operations: ")
	parts := strings.Split(rest, ",")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	installs, ok := parsePoetryOperationPart(parts[0], "install")
	if !ok {
		return 0, 0, 0, false
	}
	updates, ok := parsePoetryOperationPart(parts[1], "update")
	if !ok {
		return 0, 0, 0, false
	}
	removals, ok := parsePoetryOperationPart(parts[2], "removal")
	if !ok {
		return 0, 0, 0, false
	}
	return installs, updates, removals, true
}

func parsePoetryOperationPart(part, singular string) (int, bool) {
	fields := strings.Fields(strings.TrimSpace(part))
	if len(fields) != 2 {
		return 0, false
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count < 0 {
		return 0, false
	}
	want := singular
	if count != 1 {
		switch singular {
		case "removal":
			want = "removals"
		default:
			want = singular + "s"
		}
	}
	return count, fields[1] == want
}

func poetryPackageBulletLineOK(line string) bool {
	for _, prefix := range []string{"- Installing ", "- Updating ", "- Removing "} {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		detail := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		return detail != "" && strings.Contains(detail, "(") && strings.Contains(detail, ")")
	}
	return false
}

func poetryOperationsSummary(installs, updates, removals int) string {
	return fmt.Sprintf("%d %s, %d %s, %d %s",
		installs, pluralWord(installs, "install", "installs"),
		updates, pluralWord(updates, "update", "updates"),
		removals, pluralWord(removals, "removal", "removals"))
}

func compactUvPackageSuccess(stdout []byte, label string) ([]byte, bool) {
	text := string(stdout)
	counts := map[string]int{}
	rowOperations := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if packageOutputLineUnsafe(trimmed, lower) {
			return stdout, false
		}
		if strings.HasPrefix(trimmed, "+ ") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "~ ") {
			if strings.TrimSpace(trimmed[2:]) == "" {
				return stdout, false
			}
			rowOperations++
			continue
		}
		matched := false
		for _, verb := range []string{"Resolved", "Prepared", "Installed", "Uninstalled", "Updated", "Audited"} {
			if count, ok := parseUvPackageCountLine(trimmed, verb); ok {
				counts[strings.ToLower(verb)] = count
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if strings.HasPrefix(trimmed, "Using CPython ") ||
			strings.HasPrefix(trimmed, "Using Python ") ||
			strings.HasPrefix(trimmed, "Creating virtual environment at: ") ||
			strings.HasPrefix(trimmed, "Creating virtual environment with ") {
			continue
		}
		return stdout, false
	}
	if len(counts) == 0 {
		return stdout, false
	}
	changed := counts["installed"] + counts["uninstalled"] + counts["updated"]
	if rowOperations > 0 {
		if changed == 0 || rowOperations != changed {
			return stdout, false
		}
	}
	if counts["installed"] == 0 && counts["uninstalled"] == 0 && counts["updated"] == 0 && counts["audited"] == 0 {
		return stdout, false
	}
	parts := make([]string, 0, 4)
	for _, key := range []string{"resolved", "prepared", "installed", "uninstalled", "updated", "audited"} {
		count, ok := counts[key]
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d %s", key, count, pluralWord(count, "package", "packages")))
	}
	out := []byte("[" + label + "] ok (" + strings.Join(parts, "; ") + ")\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func npmInstallArgvUnsafe(args []string) bool {
	for i := 0; i < len(args); i++ {
		lower := strings.ToLower(strings.TrimSpace(args[i]))
		switch lower {
		case "--dry-run", "--package-lock-only", "--json", "--parseable", "--porcelain", "--verbose", "-d", "-dd", "-ddd":
			return true
		case "--loglevel":
			if i+1 >= len(args) || npmInstallLogLevelUnsafe(args[i+1]) {
				return true
			}
			i++
		default:
			if strings.HasPrefix(lower, "--loglevel=") && npmInstallLogLevelUnsafe(strings.TrimPrefix(lower, "--loglevel=")) {
				return true
			}
		}
	}
	return false
}

func pnpmInstallArgvUnsafe(args []string) bool {
	for i := 0; i < len(args); i++ {
		lower := strings.ToLower(strings.TrimSpace(args[i]))
		switch lower {
		case "--ignore-scripts", "--frozen-lockfile", "--prefer-frozen-lockfile",
			"--prod", "--production", "-p", "--dev", "-d", "--no-optional",
			"--offline", "--prefer-offline", "--ignore-workspace", "--no-color",
			"--color=false", "--reporter=append-only", "--reporter=default":
			continue
		case "--reporter":
			if i+1 >= len(args) {
				return true
			}
			next := strings.ToLower(strings.TrimSpace(args[i+1]))
			if next != "append-only" && next != "default" {
				return true
			}
			i++
		default:
			return true
		}
	}
	return false
}

func yarnInstallArgvUnsafe(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		switch lower {
		case "--non-interactive", "--no-progress", "--frozen-lockfile",
			"--pure-lockfile", "--prefer-offline", "--offline", "--production",
			"--prod", "--ignore-optional", "--ignore-engines", "--no-bin-links",
			"--check-files", "--no-default-rc", "--no-node-version-check":
			continue
		default:
			return true
		}
	}
	return false
}

func bunInstallArgvSuffix(argv []string) ([]string, bool) {
	if isBunInstallArgv(argv) {
		return argv, true
	}
	if rest, ok := npxArgvSuffix(argv); ok && isBunInstallArgv(rest) {
		return rest, true
	}
	if len(argv) < 1 {
		return nil, false
	}
	b0 := strings.ToLower(filepath.Base(argv[0]))
	if len(argv) >= 4 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" && isBunInstallArgv(argv[2:]) {
		return argv[2:], true
	}
	if len(argv) >= 3 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") && isBunInstallArgv(argv[1:]) {
		return argv[1:], true
	}
	return nil, false
}

func bunInstallArgvUnsafe(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		switch lower {
		case "--ignore-scripts", "--no-progress", "--production", "-p", "--frozen-lockfile", "--yarn", "-y":
			continue
		case "--dry-run", "--lockfile-only", "--verbose", "--silent", "--quiet", "--no-summary", "--analyze", "-a", "--help", "-h", "--global", "-g", "--trust", "--no-save":
			return true
		default:
			return true
		}
	}
	return false
}

func npmInstallLogLevelUnsafe(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "verbose", "silly":
		return true
	default:
		return false
	}
}

func compactPnpmInstallCleanSuccess(stdout []byte, label string) ([]byte, bool) {
	text := string(stdout)
	var sawDone bool
	var sawUpToDate bool
	var sawLockfileUpToDate bool
	var sawProgress bool
	var sawPackageDelta bool
	var added, removed int
	var dependencyRows int
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case trimmed == "Already up to date":
			sawUpToDate = true
		case trimmed == "Lockfile is up to date, resolution step is skipped":
			sawLockfileUpToDate = true
		case packageOutputLineUnsafe(trimmed, lower):
			return stdout, false
		case strings.HasPrefix(trimmed, "Progress: "):
			if !pnpmProgressLineOK(trimmed) {
				return stdout, false
			}
			sawProgress = true
		case strings.HasPrefix(trimmed, "Packages: "):
			parsedAdded, parsedRemoved, ok := parsePnpmPackagesLine(trimmed)
			if !ok {
				return stdout, false
			}
			added += parsedAdded
			removed += parsedRemoved
			sawPackageDelta = true
		case pnpmProgressGlyphLineOK(trimmed):
			continue
		case pnpmDependencySectionLineOK(trimmed):
			continue
		case strings.HasPrefix(trimmed, "+ ") || strings.HasPrefix(trimmed, "- "):
			if !pnpmDependencyRowOK(trimmed) {
				return stdout, false
			}
			dependencyRows++
		default:
			if pnpmDoneLineOK(trimmed) {
				sawDone = true
				continue
			}
			return stdout, false
		}
	}
	if !sawDone {
		return stdout, false
	}
	parts := make([]string, 0, 3)
	if sawUpToDate {
		parts = append(parts, "up to date")
	}
	if sawLockfileUpToDate {
		parts = append(parts, "lockfile up to date")
	}
	if sawPackageDelta {
		if added == 0 && removed == 0 {
			return stdout, false
		}
		if dependencyRows > added+removed {
			return stdout, false
		}
		if added > 0 {
			parts = append(parts, fmt.Sprintf("added %d %s", added, pluralWord(added, "package", "packages")))
		}
		if removed > 0 {
			parts = append(parts, fmt.Sprintf("removed %d %s", removed, pluralWord(removed, "package", "packages")))
		}
	}
	if len(parts) == 0 || (!sawUpToDate && !sawProgress && !sawPackageDelta) {
		return stdout, false
	}
	out := []byte("[" + label + "] ok (" + strings.Join(parts, "; ") + ")\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func pnpmProgressLineOK(line string) bool {
	rest := strings.TrimPrefix(line, "Progress: ")
	parts := strings.Split(rest, ", ")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if part == "done" {
			continue
		}
		fields := strings.Fields(part)
		if len(fields) != 2 {
			return false
		}
		switch fields[0] {
		case "resolved", "reused", "downloaded", "added":
		default:
			return false
		}
		count, err := strconv.Atoi(fields[1])
		if err != nil || count < 0 {
			return false
		}
	}
	return true
}

func parsePnpmPackagesLine(line string) (int, int, bool) {
	fields := strings.Fields(strings.TrimPrefix(line, "Packages: "))
	if len(fields) == 0 {
		return 0, 0, false
	}
	var added, removed int
	for _, field := range fields {
		if len(field) < 2 {
			return 0, 0, false
		}
		sign := field[0]
		if sign != '+' && sign != '-' {
			return 0, 0, false
		}
		count, err := strconv.Atoi(field[1:])
		if err != nil || count <= 0 {
			return 0, 0, false
		}
		if sign == '+' {
			added += count
		} else {
			removed += count
		}
	}
	return added, removed, true
}

func pnpmProgressGlyphLineOK(line string) bool {
	if line == "" || len(line) > 200 {
		return false
	}
	for _, r := range line {
		if r != '+' && r != '-' {
			return false
		}
	}
	return strings.Contains(line, "+") || strings.Contains(line, "-")
}

func pnpmDependencySectionLineOK(line string) bool {
	switch line {
	case "dependencies:", "devDependencies:", "optionalDependencies:", "peerDependencies:":
		return true
	default:
		return false
	}
}

func pnpmDependencyRowOK(line string) bool {
	detail := strings.TrimSpace(line[2:])
	if detail == "" || strings.ContainsAny(detail, "\t") {
		return false
	}
	fields := strings.Fields(detail)
	return len(fields) == 1 || len(fields) == 2
}

func pnpmDoneLineOK(line string) bool {
	fields := strings.Fields(line)
	return len(fields) == 6 &&
		fields[0] == "Done" &&
		fields[1] == "in" &&
		fields[2] != "" &&
		fields[3] == "using" &&
		fields[4] == "pnpm" &&
		strings.HasPrefix(fields[5], "v")
}

func compactYarnClassicInstallCleanSuccess(stdout []byte, label, command string) ([]byte, bool) {
	text := string(stdout)
	var sawHeader bool
	var sawDone bool
	var sawStep bool
	var sawSavedLockfile bool
	var sawUpToDate bool
	var sawDependencySection bool
	var dependencyRows int
	var savedDependencies *int
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case packageOutputLineUnsafe(trimmed, lower):
			return stdout, false
		case yarnClassicHeaderLineOK(trimmed, command):
			sawHeader = true
		case trimmed == "info No lockfile found.":
			continue
		case yarnClassicStepLineOK(trimmed):
			sawStep = true
		case trimmed == "success Saved lockfile.":
			sawSavedLockfile = true
		case trimmed == "success Already up-to-date.":
			sawUpToDate = true
		case strings.HasPrefix(trimmed, "success Saved ") &&
			(strings.HasSuffix(trimmed, " new dependency.") || strings.HasSuffix(trimmed, " new dependencies.")):
			count, ok := parseYarnClassicSavedDependencyLine(trimmed)
			if !ok {
				return stdout, false
			}
			savedDependencies = &count
		case trimmed == "info Direct dependencies" || trimmed == "info All dependencies":
			sawDependencySection = true
		case strings.HasPrefix(trimmed, "└─ ") || strings.HasPrefix(trimmed, "├─ "):
			if !yarnClassicDependencyRowOK(trimmed) {
				return stdout, false
			}
			dependencyRows++
		default:
			if yarnClassicDoneLineOK(trimmed) {
				sawDone = true
				continue
			}
			return stdout, false
		}
	}
	if !sawHeader || !sawDone || !sawStep {
		return stdout, false
	}
	parts := make([]string, 0, 3)
	if sawUpToDate {
		parts = append(parts, "up to date")
	}
	if savedDependencies != nil {
		if dependencyRows > 0 && dependencyRows < *savedDependencies {
			return stdout, false
		}
		parts = append(parts, fmt.Sprintf("saved %d %s", *savedDependencies, pluralWord(*savedDependencies, "dependency", "dependencies")))
	}
	if sawSavedLockfile {
		parts = append(parts, "lockfile saved")
	}
	if len(parts) == 0 || (dependencyRows > 0 && !sawDependencySection) {
		return stdout, false
	}
	out := []byte("[" + label + "] ok (" + strings.Join(parts, "; ") + ")\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func yarnClassicHeaderLineOK(line, command string) bool {
	fields := strings.Fields(line)
	return len(fields) == 3 &&
		fields[0] == "yarn" &&
		fields[1] == command &&
		strings.HasPrefix(fields[2], "v1.")
}

func yarnClassicStepLineOK(line string) bool {
	closeBracket := strings.IndexByte(line, ']')
	if closeBracket <= 1 || closeBracket+2 >= len(line) || line[closeBracket+1] != ' ' {
		return false
	}
	bracket := line[1:closeBracket]
	counts := strings.Split(bracket, "/")
	if len(counts) != 2 {
		return false
	}
	current, errCurrent := strconv.Atoi(counts[0])
	total, errTotal := strconv.Atoi(counts[1])
	if errCurrent != nil || errTotal != nil || current <= 0 || total <= 0 || current > total {
		return false
	}
	switch line[closeBracket+2:] {
	case "Resolving packages...", "Fetching packages...", "Linking dependencies...",
		"Building fresh packages...", "Rebuilding all packages...":
		return true
	default:
		return false
	}
}

func parseYarnClassicSavedDependencyLine(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 5 || fields[0] != "success" || fields[1] != "Saved" || fields[3] != "new" {
		return 0, false
	}
	count, err := strconv.Atoi(fields[2])
	if err != nil || count <= 0 {
		return 0, false
	}
	if fields[4] != pluralWord(count, "dependency.", "dependencies.") {
		return 0, false
	}
	return count, true
}

func yarnClassicDependencyRowOK(line string) bool {
	detail := strings.TrimSpace(line[2:])
	return detail != "" && strings.Contains(detail, "@") && !strings.ContainsAny(detail, "\t")
}

func yarnClassicDoneLineOK(line string) bool {
	fields := strings.Fields(line)
	return len(fields) == 3 &&
		fields[0] == "Done" &&
		fields[1] == "in" &&
		strings.HasSuffix(fields[2], ".") &&
		len(fields[2]) > 1
}

func compactBunInstallCleanSuccess(stdout []byte) ([]byte, bool) {
	text := string(stdout)
	var sawHeader bool
	var sawSavedLockfile bool
	var sawNoPackages bool
	var sawDone bool
	var packageRows int
	var installedCount *int
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case packageOutputLineUnsafe(trimmed, lower):
			return stdout, false
		case bunInstallHeaderLineOK(trimmed):
			sawHeader = true
		case trimmed == "Saved lockfile":
			sawSavedLockfile = true
		case trimmed == "No packages! Deleted empty lockfile":
			sawNoPackages = true
		case strings.HasPrefix(trimmed, "+ "):
			if !bunInstallPackageRowOK(trimmed) {
				return stdout, false
			}
			packageRows++
		default:
			if count, ok := parseBunInstallTerminalLine(trimmed); ok {
				installedCount = &count
				continue
			}
			if bunInstallDoneLineOK(trimmed) {
				sawDone = true
				continue
			}
			return stdout, false
		}
	}
	if !sawHeader {
		return stdout, false
	}
	parts := make([]string, 0, 2)
	switch {
	case sawNoPackages:
		if packageRows != 0 || installedCount != nil || !sawDone {
			return stdout, false
		}
		parts = append(parts, "no packages", "empty lockfile deleted")
	case packageRows > 0:
		if installedCount == nil || *installedCount != packageRows || sawDone {
			return stdout, false
		}
		parts = append(parts, fmt.Sprintf("installed %d %s", packageRows, pluralWord(packageRows, "package", "packages")))
	default:
		return stdout, false
	}
	if sawSavedLockfile {
		parts = append(parts, "lockfile saved")
	}
	out := []byte("[bun install] ok (" + strings.Join(parts, "; ") + ")\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func bunInstallHeaderLineOK(line string) bool {
	fields := strings.Fields(line)
	return len(fields) == 4 &&
		fields[0] == "bun" &&
		fields[1] == "install" &&
		strings.HasPrefix(fields[2], "v") &&
		strings.HasPrefix(fields[3], "(") &&
		strings.HasSuffix(fields[3], ")")
}

func bunInstallPackageRowOK(line string) bool {
	detail := strings.TrimSpace(strings.TrimPrefix(line, "+ "))
	return detail != "" && strings.Contains(detail, "@") && !strings.ContainsAny(detail, " \t")
}

func parseBunInstallTerminalLine(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return 0, false
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count <= 0 {
		return 0, false
	}
	if fields[1] != pluralWord(count, "package", "packages") || fields[2] != "installed" {
		return 0, false
	}
	return count, bunBracketedDurationOK(fields[3])
}

func bunInstallDoneLineOK(line string) bool {
	fields := strings.Fields(line)
	return len(fields) == 2 && fields[1] == "done" && bunBracketedDurationOK(fields[0])
}

func bunBracketedDurationOK(field string) bool {
	return strings.HasPrefix(field, "[") && strings.HasSuffix(field, "]") && len(field) > 2
}

func compactNpmInstallCleanSuccess(stdout []byte, label string) ([]byte, bool) {
	text := string(stdout)
	var summaryParts []string
	var funding *int
	var sawAuditSummary bool
	var sawZeroVulnerabilities bool
	var awaitingFundingPrompt bool
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if awaitingFundingPrompt {
			if !npmFundingPromptLineOK(trimmed) {
				return stdout, false
			}
			awaitingFundingPrompt = false
			continue
		}
		switch {
		case lower == "found 0 vulnerabilities":
			sawZeroVulnerabilities = true
		case packageOutputLineUnsafe(trimmed, lower):
			return stdout, false
		case npmInstallNoiseLineOK(lower):
			continue
		case strings.HasSuffix(lower, " for funding") || strings.Contains(lower, " looking for funding"):
			count, ok := parseNpmFundingLine(trimmed)
			if !ok {
				return stdout, false
			}
			funding = &count
			awaitingFundingPrompt = true
		default:
			parts, ok := parseNpmInstallAuditSummaryLine(trimmed)
			if !ok {
				return stdout, false
			}
			summaryParts = parts
			sawAuditSummary = true
		}
	}
	if awaitingFundingPrompt || !sawAuditSummary || !sawZeroVulnerabilities || len(summaryParts) == 0 {
		return stdout, false
	}
	if funding != nil {
		summaryParts = append(summaryParts, fmt.Sprintf("funding %d %s", *funding, pluralWord(*funding, "package", "packages")))
	}
	summaryParts = append(summaryParts, "0 vulnerabilities")
	out := []byte("[" + label + "] " + strings.Join(summaryParts, "; ") + "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func npmInstallNoiseLineOK(lower string) bool {
	if strings.HasPrefix(lower, "npm http fetch ") ||
		strings.HasPrefix(lower, "npm timing ") {
		return true
	}
	return strings.HasPrefix(lower, "npm verb lock using:")
}

func parseNpmFundingLine(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 6 {
		return 0, false
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count < 0 {
		return 0, false
	}
	if fields[1] != pluralWord(count, "package", "packages") ||
		fields[2] != "are" ||
		fields[3] != "looking" ||
		fields[4] != "for" ||
		fields[5] != "funding" {
		return 0, false
	}
	return count, true
}

func npmFundingPromptLineOK(line string) bool {
	return line == "run `npm fund` for details" || line == "run 'npm fund' for details"
}

func parseNpmInstallAuditSummaryLine(line string) ([]string, bool) {
	const auditMarker = ", and audited "
	if strings.HasPrefix(line, "up to date, audited ") {
		audited, ok := parseNpmAuditedTail(strings.TrimPrefix(line, "up to date, audited "))
		if !ok {
			return nil, false
		}
		return []string{"up to date", fmt.Sprintf("audited %d %s", audited, pluralWord(audited, "package", "packages"))}, true
	}
	prefix, auditTail, ok := strings.Cut(line, auditMarker)
	if !ok {
		return nil, false
	}
	audited, ok := parseNpmAuditedTail(auditTail)
	if !ok {
		return nil, false
	}
	operationParts := strings.Split(prefix, ", ")
	parts := make([]string, 0, len(operationParts)+1)
	for _, part := range operationParts {
		summary, ok := parseNpmInstallOperationPart(part)
		if !ok {
			return nil, false
		}
		parts = append(parts, summary)
	}
	parts = append(parts, fmt.Sprintf("audited %d %s", audited, pluralWord(audited, "package", "packages")))
	return parts, true
}

func parseNpmAuditedTail(tail string) (int, bool) {
	fields := strings.Fields(tail)
	if len(fields) != 4 {
		return 0, false
	}
	count, err := strconv.Atoi(fields[0])
	if err != nil || count < 0 {
		return 0, false
	}
	if fields[1] != pluralWord(count, "package", "packages") || fields[2] != "in" {
		return 0, false
	}
	return count, true
}

func parseNpmInstallOperationPart(part string) (string, bool) {
	fields := strings.Fields(part)
	if len(fields) != 3 {
		return "", false
	}
	verb := fields[0]
	switch verb {
	case "added", "removed", "changed":
	default:
		return "", false
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count < 0 {
		return "", false
	}
	if fields[2] != pluralWord(count, "package", "packages") {
		return "", false
	}
	return fmt.Sprintf("%s %d %s", verb, count, pluralWord(count, "package", "packages")), true
}

func parseUvPackageCountLine(line, verb string) (int, bool) {
	if !strings.HasPrefix(line, verb+" ") {
		return 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return 0, false
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count < 0 {
		return 0, false
	}
	if fields[2] != pluralWord(count, "package", "packages") || fields[3] != "in" {
		return 0, false
	}
	return count, true
}

func packageOutputLineUnsafe(trimmed, lower string) bool {
	if isPackageErrorSummaryLine(trimmed, lower) {
		return true
	}
	for _, marker := range []string{
		"warning", "warn ", "warn:", "deprecated", "deprecation", "vulnerab",
		"failed", "failure", "fatal", "panic", "exception", "traceback",
		"skipped", "pending", "todo", "incomplete", "yanked", "conflict",
		"could not", "cannot", "not found", "no matching version",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return buildOutputLineHasSourceLocationPrefix(trimmed)
}

func pluralWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
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
	if out, ok := TryCompactPackageAuditJSON(argv, stdout); ok {
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
			if (strings.HasPrefix(label, "npm ") || strings.HasPrefix(label, "pnpm ") || strings.HasPrefix(label, "yarn ")) &&
				!packageOutputHasErrorSummaryLine(s) {
				return stdout, false
			}
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
	var sawErrorSummary bool
	if packageOutputHasUnsafeSuccessMarker(s) && !packageOutputHasErrorSummaryLine(s) {
		return "", false
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		tl := strings.ToLower(t)
		if isPackageErrorSummaryLine(t, tl) {
			sawErrorSummary = true
			summaryLines = append(summaryLines, t)
			if len(summaryLines) >= 12 {
				break
			}
			continue
		}
		if sawErrorSummary {
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

func packageOutputHasErrorSummaryLine(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isPackageErrorSummaryLine(trimmed, strings.ToLower(trimmed)) {
			return true
		}
	}
	return false
}

func packageOutputHasUnsafeSuccessMarker(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if lower == "found 0 vulnerabilities" || isPackageErrorSummaryLine(trimmed, lower) {
			continue
		}
		if packageOutputLineUnsafe(trimmed, lower) {
			return true
		}
	}
	return false
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
