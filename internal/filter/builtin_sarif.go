package filter

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SARIF (Static Analysis Results Interchange Format) is the OASIS
// standard that most modern static-analysis tools can emit. Clippy,
// ESLint, Ruff, golangci-lint, hadolint, biome, semgrep, codeql, and
// many others support `--format sarif` / `--output-format sarif`.
//
// TryCompactSARIF detects a SARIF document on stdout (regardless of
// which tool produced it) and replaces it with a compact summary like
// "[sarif: clippy] 12 results (3 errors, 9 warnings)\nfoo.rs:10:5
// E0001 unused variable …". This is a **universal Tier-1 parser**:
// one implementation handles every SARIF-emitting tool, where RTK
// would need 10+ per-tool regex compactors.
//
// Strictness: we require the SARIF top-level "$schema" or "version"
// + "runs" array. Anything else falls through to Tier-2/3.
func TryCompactSARIF(argv []string, stdout []byte) ([]byte, bool) {
	s := strings.TrimSpace(string(stdout))
	if s == "" || s[0] != '{' {
		return stdout, false
	}
	// Cheap pre-check: SARIF docs always include "runs":[ near the top.
	// Avoids parsing every JSON blob.
	if !strings.Contains(s, `"runs"`) {
		return stdout, false
	}

	type message struct {
		Text string `json:"text"`
	}
	type region struct {
		StartLine   int `json:"startLine"`
		StartColumn int `json:"startColumn"`
	}
	type artifactLocation struct {
		URI string `json:"uri"`
	}
	type physicalLocation struct {
		ArtifactLocation artifactLocation `json:"artifactLocation"`
		Region           region           `json:"region"`
	}
	type location struct {
		PhysicalLocation physicalLocation `json:"physicalLocation"`
	}
	type result struct {
		RuleID    string     `json:"ruleId"`
		Level     string     `json:"level"`
		Kind      string     `json:"kind"`
		Message   message    `json:"message"`
		Locations []location `json:"locations"`
	}
	type tool struct {
		Driver struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"driver"`
	}
	type run struct {
		Tool    tool     `json:"tool"`
		Results []result `json:"results"`
	}
	type sarif struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []run  `json:"runs"`
	}
	var doc sarif
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		return stdout, false
	}
	if doc.Schema == "" && doc.Version == "" {
		return stdout, false
	}
	if len(doc.Runs) == 0 {
		return stdout, false
	}

	// Aggregate counters and per-tool labels.
	type counts struct {
		Errors    int
		Warnings  int
		Notes     int
		Total     int
		ToolNames []string
	}
	var c counts
	allResults := make([]struct {
		tool string
		res  result
	}, 0)
	for _, r := range doc.Runs {
		name := r.Tool.Driver.Name
		if name == "" {
			name = "sarif"
		}
		// Track unique tool names per run.
		dup := false
		for _, n := range c.ToolNames {
			if n == name {
				dup = true
				break
			}
		}
		if !dup {
			c.ToolNames = append(c.ToolNames, name)
		}
		for _, res := range r.Results {
			c.Total++
			level := strings.ToLower(res.Level)
			switch level {
			case "error":
				c.Errors++
			case "warning", "":
				c.Warnings++
			case "note", "info":
				c.Notes++
			}
			allResults = append(allResults, struct {
				tool string
				res  result
			}{tool: name, res: res})
		}
	}

	sort.Strings(c.ToolNames)
	toolLabel := strings.Join(c.ToolNames, "+")

	// Detect explicit "--format sarif" / "--output-format=sarif" so
	// we only fire when the user actually asked for SARIF. SARIF
	// reports outside that intent are rare in practice but possible
	// (e.g., piping a saved report through `cat`), so we accept any
	// well-formed SARIF doc even without the flag — the parser is
	// strict enough on schema that false positives are extremely
	// unlikely.
	_ = isSARIFArgv(argv) // documented call site; not strictly required

	if c.Total == 0 {
		return []byte(fmt.Sprintf("[sarif: %s] 0 results\n", toolLabel)), true
	}

	var out strings.Builder
	fmt.Fprintf(&out, "[sarif: %s] %d result(s) — %d error, %d warning, %d note\n",
		toolLabel, c.Total, c.Errors, c.Warnings, c.Notes)
	const maxLines = 10
	emitted := 0
	for _, r := range allResults {
		if emitted >= maxLines {
			fmt.Fprintf(&out, "  ... +%d more\n", c.Total-emitted)
			break
		}
		loc := "<no-location>"
		if len(r.res.Locations) > 0 {
			pl := r.res.Locations[0].PhysicalLocation
			if pl.ArtifactLocation.URI != "" {
				loc = pl.ArtifactLocation.URI
				if pl.Region.StartLine > 0 {
					loc = fmt.Sprintf("%s:%d", loc, pl.Region.StartLine)
					if pl.Region.StartColumn > 0 {
						loc = fmt.Sprintf("%s:%d", loc, pl.Region.StartColumn)
					}
				}
			}
		}
		level := r.res.Level
		if level == "" {
			level = "warning"
		}
		rule := r.res.RuleID
		if rule == "" {
			rule = "?"
		}
		msg := strings.TrimSpace(r.res.Message.Text)
		if msg == "" {
			msg = "(no message)"
		}
		// Single-line per result; truncate excessively long messages.
		if len(msg) > 160 {
			msg = msg[:157] + "..."
		}
		fmt.Fprintf(&out, "  %s %s [%s] %s\n", loc, level, rule, msg)
		emitted++
	}
	return []byte(out.String()), true
}

// isSARIFArgv returns true if argv contains an explicit
// SARIF-output flag. Currently informational only — TryCompactSARIF
// fires on any well-formed SARIF document. Kept as a hook for future
// strict-mode policies.
func isSARIFArgv(argv []string) bool {
	joined := strings.ToLower(strings.Join(argv, " "))
	return strings.Contains(joined, "--format sarif") ||
		strings.Contains(joined, "--format=sarif") ||
		strings.Contains(joined, "--output-format sarif") ||
		strings.Contains(joined, "--output-format=sarif") ||
		strings.Contains(joined, "-f sarif")
}
