// Package docs holds a single meta-test that parses the agent-readable
// YAML spec block out of docs/install.md and asserts every Step named
// there exists in internal/install.Plan(). This is the trust anchor
// for agents reading docs/install.md: if the doc says "this Step
// exists", a passing test guarantees it does.
package docs

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/install"
)

// TestInstallSpecMatchesPlan parses the YAML spec block, extracts
// every `step: <name>` line, and asserts that every name appears in
// the actual install.Plan() output.
func TestInstallSpecMatchesPlan(t *testing.T) {
	doc, err := os.ReadFile("install.md")
	if err != nil {
		t.Fatalf("read install.md: %v", err)
	}

	yamlBlock := extractYAMLSpec(string(doc))
	if yamlBlock == "" {
		t.Fatal("no ```yaml block with schema_version: 1 found in docs/install.md")
	}

	specSteps := extractStepNames(yamlBlock)
	if len(specSteps) == 0 {
		t.Fatal("no step: entries parsed from YAML spec")
	}

	plan, err := install.Plan(install.Options{
		Home:       t.TempDir(),
		BinaryPath: "/usr/local/bin/slimference",
		KeychainRunner: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
		SkipLoad: true,
	})
	if err != nil {
		t.Fatalf("install.Plan: %v", err)
	}
	planNames := plan.Inspect(context.Background()).Order
	planSet := make(map[string]bool, len(planNames))
	for _, n := range planNames {
		planSet[n] = true
	}

	// install_plan section in spec must all exist in Plan.
	installSpec := stepsInSection(yamlBlock, "install_plan")
	for _, name := range installSpec {
		if !planSet[name] {
			t.Errorf("docs/install.md install_plan declares step %q but install.Plan() does not contain it", name)
		}
	}

	// And every Plan step should be documented.
	docSet := make(map[string]bool, len(installSpec))
	for _, n := range installSpec {
		docSet[n] = true
	}
	for _, n := range planNames {
		if !docSet[n] {
			t.Errorf("install.Plan() contains step %q that docs/install.md does not document", n)
		}
	}

	// hosts_plan must have exactly one step that matches HostsPlan().
	hostsSpec := stepsInSection(yamlBlock, "hosts_plan")
	if len(hostsSpec) != 1 {
		t.Errorf("hosts_plan should declare exactly 1 step, got %d", len(hostsSpec))
	}
	hp, err := install.HostsPlan(install.HostsOptions{Home: t.TempDir(), HostsPath: "/tmp/test-hosts"})
	if err != nil {
		t.Fatalf("HostsPlan: %v", err)
	}
	hostsPlanNames := hp.Inspect(context.Background()).Order
	if len(hostsPlanNames) != 1 || hostsPlanNames[0] != hostsSpec[0] {
		t.Errorf("hosts_plan mismatch: doc=%v plan=%v", hostsSpec, hostsPlanNames)
	}

	_ = strings.TrimSpace
}

func TestDocumentationTOCAnchorsResolve(t *testing.T) {
	doc, err := os.ReadFile("documentation.md")
	if err != nil {
		t.Fatalf("read documentation.md: %v", err)
	}
	text := string(doc)
	anchors := markdownHeadingAnchors(text)
	linkRe := regexp.MustCompile(`\[[^\]]+\]\(#([^)]+)\)`)
	matches := linkRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		t.Fatal("no local markdown anchors found in docs/documentation.md")
	}
	for _, match := range matches {
		anchor := match[1]
		if !anchors[anchor] {
			t.Errorf("docs/documentation.md local anchor %q has no matching heading", anchor)
		}
	}
}

func markdownHeadingAnchors(doc string) map[string]bool {
	anchors := make(map[string]bool)
	for line := range strings.SplitSeq(doc, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if title == "" {
			continue
		}
		anchors[githubMarkdownAnchor(title)] = true
	}
	return anchors
}

func githubMarkdownAnchor(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// extractYAMLSpec finds the first ```yaml fenced block that contains
// `schema_version: 1` and returns its body.
func extractYAMLSpec(doc string) string {
	// Match a fenced code block whose first non-whitespace line
	// contains "schema_version:" followed by 1.
	re := regexp.MustCompile("(?s)```yaml\n(.*?)\n```")
	for _, m := range re.FindAllStringSubmatch(doc, -1) {
		if strings.Contains(m[1], "schema_version:") {
			return m[1]
		}
	}
	return ""
}

// extractStepNames pulls every `- step: <name>` value from the YAML
// block.
func extractStepNames(yaml string) []string {
	re := regexp.MustCompile(`(?m)^\s*-\s*step:\s*(\S+)`)
	matches := re.FindAllStringSubmatch(yaml, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// stepsInSection returns step names that fall under a named YAML
// section (e.g. "install_plan", "hosts_plan").
func stepsInSection(yaml, section string) []string {
	lines := strings.Split(yaml, "\n")
	startIdx := -1
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == section+":" {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		return nil
	}
	// Find end: next line at the same or less indent that is NOT
	// blank and NOT part of the section.
	sectionIndent := leadingSpaces(lines[startIdx])
	endIdx := len(lines)
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if leadingSpaces(lines[i]) <= sectionIndent && strings.HasSuffix(trimmed, ":") {
			endIdx = i
			break
		}
	}
	return extractStepNames(strings.Join(lines[startIdx:endIdx], "\n"))
}

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
			continue
		}
		break
	}
	return n
}
