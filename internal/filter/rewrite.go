package filter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// filterableCommands is the set of base-command names that have a built-in
// or TOML-based filter in the pipeline (spec+.md §4.2, §4.4, §4.6).
// Commands not in this set are passed through unchanged by RewriteCommand.
var filterableCommands = map[string]bool{
	// Git (F01-F05)
	"git": true,
	// Build (F07)
	"cargo": true, "go": true, "tsc": true, "npm": true, "npx": true, "pnpm": true,
	"pnpx": true, "yarn": true,
	"bun": true, "make": true, "dotnet": true, "gradle": true, "gradlew": true,
	"mvn": true, "gcc": true, "g++": true, "just": true, "mix": true, "nx": true,
	"pio": true, "swift": true, "task": true, "trunk": true, "turbo": true,
	"xcodebuild": true,
	// Test (F08)
	"pytest": true, "python": true, "python3": true, "vitest": true, "jest": true,
	"rspec": true, "rake": true, "mocha": true,
	// Lint (F09)
	"eslint": true, "tslint": true, "golangci-lint": true, "rubocop": true,
	"ruff": true, "biome": true, "mypy": true, "flake8": true, "pylint": true,
	"basedpyright": true, "hadolint": true, "markdownlint": true, "oxlint": true,
	"shellcheck": true, "ty": true, "yamllint": true,
	// Search (F10)
	"grep": true, "rg": true, "ag": true, "ack": true,
	// Files (F06, F11)
	"cat": true, "head": true, "tail": true, "ls": true, "tree": true,
	"find": true, "fd": true, "bat": true, "wc": true, "df": true, "du": true,
	"jq": true, "ps": true, "stat": true,
	// Package managers (F12)
	"pip": true, "pip3": true, "composer": true, "gem": true, "bundle": true,
	"brew": true, "mise": true, "poetry": true, "uv": true,
	// Container (F13)
	"docker": true, "kubectl": true, "k9s": true, "helm": true, "skopeo": true,
	// Cloud / CI (F16, F18)
	"aws": true, "gh": true, "glab": true, "ansible-playbook": true, "gcloud": true,
	"jira": true, "jj": true, "pre-commit": true, "quarto": true, "rsync": true,
	"shopify": true, "terraform": true, "tofu": true, "yadm": true,
	// Format (F24)
	"prettier": true, "gofmt": true, "rustfmt": true, "black": true, "isort": true,
	"liquibase": true, "sops": true,
	// DB (F19)
	"psql": true, "mysql": true, "sqlite3": true,
	// Misc logs (F15)
	"fail2ban-client": true, "iptables": true, "journalctl": true, "ollama": true,
	"ping": true, "systemctl": true,
}

// findAlwaysCommands are rewritten even when they appear after a pipe
// (they produce file lists / structured output, not pipeline streams).
var findAlwaysCommands = map[string]bool{
	"find": true, "fd": true,
}

// RewriteCommand rewrites cmd for slimference filtering (spec+.md §4.2).
//
// It splits compound commands (&&, ||, ;) and prefixes each filterable
// segment with "slimference filter". The right-hand side of a pipe is
// never rewritten — with the exception of find/fd which produce file lists.
//
// excluded is a list of command names (argv[0] base names) that are
// never rewritten regardless of filter rules.
//
// Returns (rewritten, true) if at least one segment was prefixed,
// or (original, false) when no rewrite was applied (hook should passthrough).
func RewriteCommand(cmd string, excluded []string) (string, bool) {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return cmd, false
	}
	// Already prefixed: skip.
	if strings.HasPrefix(trimmed, "slimference filter") ||
		strings.HasPrefix(trimmed, "slimference rewrite") {
		return cmd, false
	}
	// Caller explicitly opted out.
	if strings.HasPrefix(trimmed, "SLIMFERENCE_DISABLED=1 ") {
		rest := strings.TrimPrefix(trimmed, "SLIMFERENCE_DISABLED=1 ")
		return strings.TrimSpace(rest), false
	}

	excludedSet := make(map[string]bool, len(excluded))
	for _, e := range excluded {
		excludedSet[strings.ToLower(e)] = true
	}

	toks := tokenize(cmd)
	if len(toks) == 0 {
		return cmd, false
	}

	// Split into compound segments on && || ;
	type compoundSeg struct {
		toks     []ParsedToken
		operator string // trailing operator ("&&", "||", ";") or ""
	}
	var segs []compoundSeg
	var cur []ParsedToken
	for _, t := range toks {
		if t.Kind == TokenOperator {
			segs = append(segs, compoundSeg{toks: cur, operator: t.Value})
			cur = nil
		} else {
			cur = append(cur, t)
		}
	}
	if len(cur) > 0 {
		segs = append(segs, compoundSeg{toks: cur, operator: ""})
	}
	// segs is guaranteed non-empty here: toks was verified non-empty above,
	// so either an operator token pushed an initial segment or the trailing
	// cur was non-empty.

	// Rewrite each compound segment.
	var parts []string
	anyRewrite := false
	for _, seg := range segs {
		rewritten, changed := rewriteCompoundSeg(seg.toks, excludedSet)
		if changed {
			anyRewrite = true
		}
		if seg.operator != "" {
			parts = append(parts, rewritten+" "+seg.operator)
		} else {
			parts = append(parts, rewritten)
		}
	}

	if !anyRewrite {
		return cmd, false
	}
	return strings.Join(parts, " "), true
}

// rewriteCompoundSeg rewrites one compound segment (no && || ; inside).
// It splits on pipes and only rewrites the first stage (plus always-rewrite commands).
func rewriteCompoundSeg(toks []ParsedToken, excluded map[string]bool) (string, bool) {
	if len(toks) == 0 {
		return "", false
	}

	// Split on pipe tokens into pipe stages.
	type pipeStage struct {
		toks     []ParsedToken
		trailing bool // true if a | follows this stage
	}
	var stages []pipeStage
	var cur []ParsedToken
	for _, t := range toks {
		if t.Kind == TokenPipe {
			stages = append(stages, pipeStage{toks: cur, trailing: true})
			cur = nil
		} else {
			cur = append(cur, t)
		}
	}
	stages = append(stages, pipeStage{toks: cur, trailing: false})

	var parts []string
	anyChanged := false
	for i, st := range stages {
		isFirst := i == 0
		seg := st.toks
		rendered := renderSegTokens(seg)

		// Determine if this stage can be rewritten.
		base := stageBaseCommand(seg)
		canRewrite := (isFirst || findAlwaysCommands[base]) && !excluded[strings.ToLower(base)]

		if canRewrite && filterableCommands[base] {
			rendered = "slimference filter " + rendered
			anyChanged = true
		}

		if st.trailing {
			parts = append(parts, rendered+" |")
		} else {
			parts = append(parts, rendered)
		}
	}

	return strings.Join(parts, " "), anyChanged
}

// stageBaseCommand returns the base name of argv[0] for a pipe stage,
// skipping any leading VAR=value env-var assignments.
func stageBaseCommand(toks []ParsedToken) string {
	for _, t := range toks {
		if t.Kind != TokenArg {
			continue
		}
		v := t.Value
		// Skip VAR=value env-var tokens.
		if idx := strings.Index(v, "="); idx > 0 {
			before := v[:idx]
			if !strings.ContainsAny(before, " /.-") {
				continue
			}
		}
		return strings.ToLower(filepath.Base(v))
	}
	return ""
}

// renderSegTokens reassembles a slice of parsed tokens into a command string.
// Redirects and pipes are joined without extra spaces to match shell conventions.
func renderSegTokens(toks []ParsedToken) string {
	if len(toks) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, t := range toks {
		if i > 0 {
			// Redirects and their targets may need special spacing.
			if toks[i-1].Kind == TokenRedirect || t.Kind == TokenRedirect {
				sb.WriteByte(' ')
			} else {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(t.Value)
	}
	return sb.String()
}

// ExtractCommandFromHookJSON finds the first non-empty string value for key "command"
// in a JSON object (recursive). Used for Claude Code PreToolUse hook stdin.
func ExtractCommandFromHookJSON(b []byte) (string, error) {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", fmt.Errorf("filter: JSON: %w", err)
	}
	if s, ok := findStringForKey(v, "command"); ok {
		return s, nil
	}
	return "", fmt.Errorf("filter: no string field \"command\" in JSON")
}

func findStringForKey(v interface{}, key string) (string, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		if s, ok := t[key].(string); ok && s != "" {
			return s, true
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := findStringForKey(t[k], key); ok {
				return s, true
			}
		}
	case []interface{}:
		for _, e := range t {
			if s, ok := findStringForKey(e, key); ok {
				return s, true
			}
		}
	}
	return "", false
}
