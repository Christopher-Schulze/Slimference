package summarization

import (
	"errors"
	"os"
	"strings"
	"sync"
)

// This file preserves the public-surface helpers that the proxy admin
// endpoint and the CLI still call. With the in-process deterministic
// summarizer the telemetry they expose (model-prompt version, CoT-tag
// counters, lineage-marker stats) is structurally meaningless, so the
// stubs return empty / zero values. The signatures stay so external
// callers compile unchanged.

// SetPromptOverride records an operator-supplied prompt body and
// version label. It exists for forward compatibility with future
// optional engines; the current deterministic compactor ignores it.
func SetPromptOverride(body, version string) {
	promptOverrideMu.Lock()
	defer promptOverrideMu.Unlock()
	promptOverrideBody = body
	promptOverrideVersion = strings.TrimSpace(version)
}

// PromptVersion returns the active operator-supplied prompt version,
// or "default" when none is set.
func PromptVersion() string {
	promptOverrideMu.RLock()
	defer promptOverrideMu.RUnlock()
	if promptOverrideVersion == "" {
		return "default"
	}
	return promptOverrideVersion
}

// LoadPromptOverrideFromPath reads a prompt override document from
// disk and applies it. Document syntax:
//
//	---
//	version: vX
//	---
//	(prompt body)
//
// On parse success the body+version are installed via
// SetPromptOverride and the version string is returned. Missing files
// and malformed documents return descriptive errors.
func LoadPromptOverrideFromPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty prompt-override path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	body, version := parsePromptDocument(string(data))
	SetPromptOverride(body, version)
	return PromptVersion(), nil
}

// parsePromptDocument extracts an optional version label from a
// prompt-override document. Supported header styles:
//
//	"# version: vX"            — single hash-prefixed comment line
//	"// version: vX"           — single double-slash comment line
//	"---\nversion: vX\n---\n"  — yaml-style front matter
//
// Body is the remaining content. A document with no recognisable
// header keeps version="custom".
func parsePromptDocument(s string) (body, version string) {
	trim := strings.TrimLeft(s, " \t\n\r")
	// Try yaml-style front matter.
	if strings.HasPrefix(trim, "---") {
		rest := strings.TrimPrefix(trim, "---")
		end := strings.Index(rest, "---")
		if end >= 0 {
			header := strings.TrimSpace(rest[:end])
			bodyPart := strings.TrimLeft(rest[end+3:], "\n")
			for _, line := range strings.Split(header, "\n") {
				line = strings.TrimSpace(line)
				if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "version" {
					version = strings.TrimSpace(v)
				}
			}
			if version == "" {
				version = "custom"
			}
			return bodyPart, version
		}
	}
	// Try hash/double-slash header on the first non-empty line.
	lines := strings.SplitN(trim, "\n", 2)
	first := strings.TrimSpace(lines[0])
	for _, prefix := range []string{"#", "//"} {
		if strings.HasPrefix(first, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(first, prefix))
			if k, v, ok := strings.Cut(rest, ":"); ok && strings.TrimSpace(k) == "version" {
				version = strings.TrimSpace(v)
				if len(lines) > 1 {
					return strings.TrimLeft(lines[1], "\n"), version
				}
				return "", version
			}
		}
	}
	return s, "custom"
}

// ExamplePromptCount / ExamplePromptCounts / ResetExamplePromptCounts
// are historical model-prompt telemetry. With no model-backed
// summarizer wired by default, they return zero.
func ExamplePromptCount(lang string) int64  { _ = lang; return 0 }
func ExamplePromptCounts() map[string]int64 { return map[string]int64{} }
func ResetExamplePromptCounts()             { _ = ExamplePromptCounts() }

// defaultCoTTags lists the historical chain-of-thought wrapper tags
// the StripCoTTags helper recognises. Kept for callers that still
// pipe arbitrary content through the helper (e.g. opt-in legacy
// engines).
var defaultCoTTags = []string{"think", "thinking", "reasoning", "scratchpad"}

// CoTTagCount / CoTTagCounts / ResetCoTTagCounts are historical
// chain-of-thought telemetry from model outputs. Deterministic extract
// has no CoT; stubs return zero.
func CoTTagCount(tag string) int64   { _ = tag; return 0 }
func CoTTagCounts() map[string]int64 { return map[string]int64{} }
func ResetCoTTagCounts()             { _ = CoTTagCounts() }

// StripCoTTags strips a fixed set of XML-style tags from s. Kept as a
// general utility (extract output never contains them but callers may
// still pipe arbitrary content through this helper).
func StripCoTTags(s string, tags []string) string {
	for _, tag := range tags {
		openTag := "<" + tag + ">"
		closeTag := "</" + tag + ">"
		for {
			start := strings.Index(s, openTag)
			if start < 0 {
				break
			}
			end := strings.Index(s[start:], closeTag)
			if end < 0 {
				s = s[:start]
				break
			}
			s = s[:start] + s[start+end+len(closeTag):]
		}
	}
	return s
}

// RecordLineageStats / LineageMarkerRate / LineageMarkerCounts /
// ResetLineageMarkerStats are historical lineage-marker telemetry.
// The deterministic compactor does not emit lineage markers; stubs
// return zero.
func RecordLineageStats(summary string)          { _ = summary }
func LineageMarkerRate() float64                 { return 0 }
func LineageMarkerCounts() (marked, total int64) { return 0, 0 }
func ResetLineageMarkerStats()                   { _, _ = LineageMarkerCounts() }

var (
	promptOverrideMu      sync.RWMutex
	promptOverrideBody    string
	promptOverrideVersion string
)
