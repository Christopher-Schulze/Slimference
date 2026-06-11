package evidence

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type ContentClass string

const (
	ContentUnknown    ContentClass = "unknown"
	ContentTest       ContentClass = "test"
	ContentLog        ContentClass = "log"
	ContentSearch     ContentClass = "search"
	ContentDiff       ContentClass = "diff"
	ContentStacktrace ContentClass = "stacktrace"
	ContentJSON       ContentClass = "json"
	ContentCode       ContentClass = "code"
	ContentPlain      ContentClass = "plain"
)

type Signal string

const (
	SignalErrorKeyword Signal = "error_keyword"
	SignalStacktrace   Signal = "stacktrace"
	SignalOutlier      Signal = "outlier"
	SignalDedupe       Signal = "dedupe"
	SignalChangedHunk  Signal = "changed_hunk"
	SignalRecency      Signal = "recency"
	SignalCacheHotZone Signal = "cache_hot_zone"
	SignalFirstLast    Signal = "first_last"
	SignalExitStatus   Signal = "exit_status"
	SignalPath         Signal = "path"
	SignalCount        Signal = "count"
	SignalWarning      Signal = "warning"
	SignalImportant    Signal = "important"
	SignalSecurity     Signal = "security"
)

type SafetyClass string

const (
	SafetyExact              SafetyClass = "exact"
	SafetyStructuredEvidence SafetyClass = "structured_evidence"
	SafetyDiagnosticPriority SafetyClass = "diagnostic_priority"
	SafetyRecoverable        SafetyClass = "recoverable"
	SafetyFullPass           SafetyClass = "full_pass"
	SafetyUnknown            SafetyClass = "unknown"
)

type Action string

const (
	ActionApplied    Action = "applied"
	ActionSkipped    Action = "skipped"
	ActionFullPass   Action = "full_pass"
	ActionShadow     Action = "shadow"
	ActionFailedOpen Action = "failed_open"
)

type BlockDecision struct {
	Layer             int          `json:"layer,omitempty"`
	Mechanism         string       `json:"mechanism"`
	ContentClass      ContentClass `json:"content_class"`
	SafetyClass       SafetyClass  `json:"safety_class"`
	Action            Action       `json:"action"`
	Reason            string       `json:"reason"`
	Signals           []Signal     `json:"signals,omitempty"`
	PreservedEvidence []string     `json:"preserved_evidence,omitempty"`
	Recovery          string       `json:"recovery,omitempty"`
	OriginalTokens    int          `json:"original_tokens,omitempty"`
	FinalTokens       int          `json:"final_tokens,omitempty"`
	SavedTokens       int          `json:"saved_tokens,omitempty"`
	AddedTokens       int          `json:"added_tokens,omitempty"`
	NetTokens         int          `json:"net_tokens"`
	CacheImpact       string       `json:"cache_impact,omitempty"`
}

type Analysis struct {
	ContentClass ContentClass `json:"content_class"`
	Signals      []Signal     `json:"signals,omitempty"`
}

// analyzeMaxBytes bounds the content scanned for telemetry classification.
// Classes and signals are heuristics that trigger on patterns present in a
// bounded prefix; scanning the full text makes evidence emission O(output)
// per block, which keeps demoted full-pass passes expensive enough to hold
// the Layer-0 latency budget latched.
const analyzeMaxBytes = 64 * 1024

func Analyze(argv []string, content []byte) Analysis {
	truncated := len(content) > analyzeMaxBytes
	if truncated {
		content = content[:analyzeMaxBytes]
	}
	text := strings.TrimSpace(string(content))
	class := classify(argv, text, truncated)
	signals := detectSignals(text)
	if class == ContentDiff {
		signals = appendSignal(signals, SignalChangedHunk)
	}
	if class == ContentStacktrace {
		signals = appendSignal(signals, SignalStacktrace)
	}
	if hasPathSignal(text) {
		signals = appendSignal(signals, SignalPath)
	}
	sortSignals(signals)
	return Analysis{ContentClass: class, Signals: signals}
}

func DecisionFromObservation(layer int, mechanism string, safety SafetyClass, action Action, reason string, analysis Analysis, preserved []string, recovery string, beforeTokens int, afterTokens int) BlockDecision {
	saved := beforeTokens - afterTokens
	added := 0
	if saved < 0 {
		added = -saved
		saved = 0
	}
	return BlockDecision{
		Layer:             layer,
		Mechanism:         strings.TrimSpace(mechanism),
		ContentClass:      analysis.ContentClass,
		SafetyClass:       safety,
		Action:            action,
		Reason:            strings.TrimSpace(reason),
		Signals:           cloneSignals(analysis.Signals),
		PreservedEvidence: cloneStrings(preserved),
		Recovery:          strings.TrimSpace(recovery),
		OriginalTokens:    beforeTokens,
		FinalTokens:       afterTokens,
		SavedTokens:       saved,
		AddedTokens:       added,
		NetTokens:         saved - added,
	}
}

func RedactDecision(in BlockDecision) BlockDecision {
	out := in
	out.Signals = cloneSignals(in.Signals)
	out.PreservedEvidence = cloneStrings(in.PreservedEvidence)
	return out
}

func classify(argv []string, text string, truncated bool) ContentClass {
	head := commandHead(argv)
	switch head {
	case "go", "pytest", "cargo", "npm", "pnpm", "yarn", "bun", "vitest", "jest":
		if commandContains(argv, "test") || strings.Contains(text, "FAIL") || strings.Contains(text, "FAILED") {
			return ContentTest
		}
	case "rg", "grep", "ag", "ack":
		return ContentSearch
	case "git", "jj", "hg", "svn":
		if commandContains(argv, "diff") || commandContains(argv, "show") || looksLikeDiff(text) {
			return ContentDiff
		}
	}
	if text == "" {
		return ContentPlain
	}
	if truncated {
		// A truncated prefix of valid JSON cannot pass json.Valid; classify
		// by shape instead of scanning megabytes.
		if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
			return ContentJSON
		}
	} else if json.Valid([]byte(text)) {
		return ContentJSON
	}
	if looksLikeStacktrace(text) {
		return ContentStacktrace
	}
	if looksLikeDiff(text) {
		return ContentDiff
	}
	if looksLikeSearch(text) {
		return ContentSearch
	}
	if looksLikeTest(text) {
		return ContentTest
	}
	if looksLikeLog(text) {
		return ContentLog
	}
	if looksLikeCode(text) {
		return ContentCode
	}
	if head != "" {
		return ContentPlain
	}
	return ContentUnknown
}

func detectSignals(text string) []Signal {
	var out []Signal
	lower := strings.ToLower(text)
	signals := detectKeywordSignals(text)
	for _, signal := range signals {
		out = appendSignal(out, signal)
	}
	if looksLikeStacktrace(text) {
		out = appendSignal(out, SignalStacktrace)
	}
	if hasOutlierLine(text) {
		out = appendSignal(out, SignalOutlier)
	}
	if hasRepeatedLine(text) {
		out = appendSignal(out, SignalDedupe)
	}
	if hasCountSignal(text) {
		out = appendSignal(out, SignalCount)
	}
	if hasExitSignal(lower) {
		out = appendSignal(out, SignalExitStatus)
	}
	if text != "" {
		out = appendSignal(out, SignalFirstLast)
		out = appendSignal(out, SignalRecency)
	}
	return out
}

func commandHead(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return strings.ToLower(filepath.Base(strings.TrimSpace(argv[0])))
}

func commandContains(argv []string, needle string) bool {
	for _, part := range argv {
		if strings.EqualFold(strings.TrimSpace(part), needle) {
			return true
		}
	}
	return false
}

func looksLikeDiff(text string) bool {
	return strings.Contains(text, "\n@@ ") ||
		strings.HasPrefix(text, "@@ ") ||
		strings.Contains(text, "diff --git ") ||
		strings.Contains(text, "\n+++ ") && strings.Contains(text, "\n--- ")
}

func looksLikeStacktrace(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "Traceback (most recent call last):") ||
		strings.Contains(lower, "stack trace") ||
		strings.Contains(lower, "goroutine ") && strings.Contains(text, ".go:") ||
		strings.Contains(text, "\n    at ") ||
		strings.Contains(text, "\n\tat ")
}

func looksLikeSearch(text string) bool {
	lines := strings.Split(text, "\n")
	matches := 0
	for _, line := range lines {
		if countColonFields(line) >= 2 && hasPathishPrefix(line) {
			matches++
		}
	}
	return matches >= 2
}

func looksLikeTest(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "--- FAIL:") ||
		strings.Contains(text, "FAIL\t") ||
		strings.Contains(lower, "tests failed") ||
		strings.Contains(lower, "failed tests") ||
		strings.Contains(lower, "passed") && strings.Contains(lower, "failed")
}

func looksLikeLog(text string) bool {
	lines := strings.Split(text, "\n")
	seen := 0
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "error ") || strings.HasPrefix(lower, "warn ") ||
			strings.Contains(lower, " level=error") || strings.Contains(lower, " level=warn") ||
			startsWithDateLike(line) {
			seen++
		}
	}
	return seen >= 2
}

func looksLikeCode(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "\nfunc ") ||
		strings.Contains(text, "\npackage ") ||
		strings.Contains(text, "\nimport ") ||
		strings.Contains(lower, "\nclass ") ||
		strings.Contains(lower, "\nconst ") ||
		strings.Contains(lower, "\nexport ")
}

func hasOutlierLine(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if len(line) >= 240 {
			return true
		}
	}
	return false
}

func hasRepeatedLine(text string) bool {
	counts := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 8 {
			continue
		}
		counts[trimmed]++
		if counts[trimmed] >= 3 {
			return true
		}
	}
	return false
}

func hasPathSignal(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if hasPathishPrefix(line) || strings.Contains(line, ".go:") || strings.Contains(line, ".ts:") || strings.Contains(line, ".py:") {
			return true
		}
	}
	return false
}

func hasCountSignal(text string) bool {
	for _, r := range text {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func hasExitSignal(lower string) bool {
	return strings.Contains(lower, "exit status") ||
		strings.Contains(lower, "exit code") ||
		strings.Contains(lower, "returned non-zero") ||
		strings.Contains(lower, "command failed")
}

func hasPathishPrefix(line string) bool {
	head := line
	if idx := firstSearchEvidenceSeparator(line); idx >= 0 {
		head = line[:idx]
	}
	head = strings.TrimSpace(head)
	return strings.Contains(head, "/") || strings.Contains(head, "\\") || strings.Contains(head, ".")
}

func firstSearchEvidenceSeparator(line string) int {
	scanStart := 0
	if len(line) >= 3 &&
		((line[0] >= 'A' && line[0] <= 'Z') || (line[0] >= 'a' && line[0] <= 'z')) &&
		line[1] == ':' &&
		(line[2] == '\\' || line[2] == '/') {
		scanStart = 2
	}
	for i := scanStart; i < len(line); i++ {
		if line[i] != ':' && line[i] != '-' {
			continue
		}
		if i+1 < len(line) && line[i+1] >= '0' && line[i+1] <= '9' {
			return i
		}
	}
	if idx := strings.IndexByte(line[scanStart:], ':'); idx >= 0 {
		return scanStart + idx
	}
	return -1
}

func countColonFields(line string) int {
	count := 0
	for _, part := range strings.Split(line, ":") {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func startsWithDateLike(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) < 10 {
		return false
	}
	for i := 0; i < 4; i++ {
		if !unicode.IsDigit(rune(line[i])) {
			return false
		}
	}
	return line[4] == '-' && line[7] == '-'
}

func appendSignal(signals []Signal, signal Signal) []Signal {
	for _, existing := range signals {
		if existing == signal {
			return signals
		}
	}
	return append(signals, signal)
}

func sortSignals(signals []Signal) {
	sort.Slice(signals, func(i, j int) bool {
		return signals[i] < signals[j]
	})
}

func cloneSignals(in []Signal) []Signal {
	if len(in) == 0 {
		return nil
	}
	out := make([]Signal, len(in))
	copy(out, in)
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
