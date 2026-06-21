package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubCLIVersion installs a test stub that returns canned output for named
// CLIs and restores the real implementation on cleanup.
func stubCLIVersion(t *testing.T, outputs map[string]string, errs map[string]error) {
	t.Helper()
	orig := cliVersionCmdFn
	cliVersionCmdFn = func(_ context.Context, name string) ([]byte, error) {
		if err, ok := errs[name]; ok {
			return nil, err
		}
		return []byte(outputs[name]), nil
	}
	t.Cleanup(func() { cliVersionCmdFn = orig })
}

// TestParseCLIVersion covers typical and edge-case --version strings.
func TestParseCLIVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"claude 1.5.3 (darwin-arm64)", "1.5.3"},
		{"v0.200.1", "0.200.1"},
		{"Version: 2.0.0", "2.0.0"},
		{"no version here", ""},
		{"", ""},
		{"1.2 incomplete", ""}, // two-part is not a semver triple
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := parseCLIVersion(tc.in); got != tc.want {
				t.Errorf("parseCLIVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSplitSemver rejects malformed triples cleanly.
func TestSplitSemver(t *testing.T) {
	t.Parallel()
	if _, ok := splitSemver("1.2"); ok {
		t.Fatal("two-part must fail")
	}
	if _, ok := splitSemver("1.2.x"); ok {
		t.Fatal("non-numeric must fail")
	}
	got, ok := splitSemver("1.2.3")
	if !ok || got != [3]int{1, 2, 3} {
		t.Fatalf("good triple: %v ok=%v", got, ok)
	}
}

// TestCompareSemver covers all ordering cases.
func TestCompareSemver(t *testing.T) {
	t.Parallel()
	if compareSemver([3]int{1, 0, 0}, [3]int{1, 0, 0}) != 0 {
		t.Fatal("equal must be 0")
	}
	if compareSemver([3]int{0, 9, 0}, [3]int{1, 0, 0}) != -1 {
		t.Fatal("below must be -1")
	}
	if compareSemver([3]int{1, 0, 1}, [3]int{1, 0, 0}) != 1 {
		t.Fatal("above must be 1")
	}
	if compareSemver([3]int{1, 1, 0}, [3]int{1, 0, 5}) != 1 {
		t.Fatal("minor wins")
	}
}

// TestClassifyDrift returns all five possible statuses.
func TestClassifyDrift(t *testing.T) {
	t.Parallel()
	if got := classifyDrift("1.5.0", "1.0.0", "2.0.0"); got != DriftOK {
		t.Fatalf("ok: %v", got)
	}
	if got := classifyDrift("0.9.0", "1.0.0", "2.0.0"); got != DriftBelow {
		t.Fatalf("below: %v", got)
	}
	if got := classifyDrift("2.1.0", "1.0.0", "2.0.0"); got != DriftAbove {
		t.Fatalf("above: %v", got)
	}
	if got := classifyDrift("oops", "1.0.0", "2.0.0"); got != DriftUnknown {
		t.Fatalf("bad detected: %v", got)
	}
	if got := classifyDrift("1.5.0", "bad", "2.0.0"); got != DriftUnknown {
		t.Fatalf("bad min: %v", got)
	}
	if got := classifyDrift("1.5.0", "1.0.0", "bad"); got != DriftUnknown {
		t.Fatalf("bad max: %v", got)
	}
}

// TestDetectDrift_bothInstalled uses stubs to return canned versions and
// verifies the reports carry the expected status.
func TestDetectDrift_bothInstalled(t *testing.T) {
	stubCLIVersion(t,
		map[string]string{
			"claude": "claude 1.5.3",
			"codex":  "codex version 0.120.0",
		},
		nil,
	)
	reports := DetectDrift(context.Background())
	if len(reports) != 2 {
		t.Fatalf("want 2 reports, got %d", len(reports))
	}
	for _, r := range reports {
		if !r.BinaryFound {
			t.Fatalf("%s must report installed", r.CLI)
		}
		if r.Status != DriftOK {
			t.Fatalf("%s status=%s, expected ok", r.CLI, r.Status)
		}
	}
}

// TestDetectDrift_notInstalled surfaces absence cleanly.
func TestDetectDrift_notInstalled(t *testing.T) {
	stubCLIVersion(t,
		nil,
		map[string]error{
			"claude": errors.New("not found"),
			"codex":  errors.New("not found"),
		},
	)
	reports := DetectDrift(context.Background())
	for _, r := range reports {
		if r.BinaryFound || r.Status != DriftUnknown {
			t.Fatalf("%s: expected not installed + unknown, got %+v", r.CLI, r)
		}
	}
}

// TestDetectDrift_unparseable marks the output as unknown.
func TestDetectDrift_unparseable(t *testing.T) {
	stubCLIVersion(t,
		map[string]string{
			"claude": "Claude Code - no version here",
			"codex":  "codex without a triple",
		},
		nil,
	)
	reports := DetectDrift(context.Background())
	for _, r := range reports {
		if r.Status != DriftUnknown {
			t.Fatalf("%s: expected unknown, got %+v", r.CLI, r)
		}
	}
}

// TestDetectDrift_belowMinimum flags old versions.
func TestDetectDrift_belowMinimum(t *testing.T) {
	stubCLIVersion(t,
		map[string]string{
			"claude": "claude 0.9.0",
			"codex":  "codex 0.116.0",
		},
		nil,
	)
	reports := DetectDrift(context.Background())
	for _, r := range reports {
		if r.Status != DriftBelow {
			t.Fatalf("%s expected below-minimum, got %s", r.CLI, r.Status)
		}
	}
}

// TestDetectDrift_aboveTested flags too-new versions.
func TestDetectDrift_aboveTested(t *testing.T) {
	stubCLIVersion(t,
		map[string]string{
			"claude": "claude 99.0.0",
			"codex":  "codex 99.0.0",
		},
		nil,
	)
	reports := DetectDrift(context.Background())
	for _, r := range reports {
		if r.Status != DriftAbove {
			t.Fatalf("%s expected above-tested, got %s", r.CLI, r.Status)
		}
	}
}

// TestCLIVersionCmdFn_defaultRunsRealBinary exercises the default lambda
// with a guaranteed-missing binary so the fallback error path is hit.
func TestCLIVersionCmdFn_defaultRunsRealBinary(t *testing.T) {
	t.Parallel()
	_, err := cliVersionCmdFn(context.Background(), "slimference-nonexistent-cli-xyz")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

// TestFormatDriftReports renders every status variant.
func TestFormatDriftReports(t *testing.T) {
	t.Parallel()
	text := FormatDriftReports([]DriftReport{
		{CLI: "claude", BinaryFound: false, Status: DriftUnknown},
		{CLI: "codex", BinaryFound: true, VersionParsed: "0.120.0", MinSupported: "0.117.0", MaxTested: "0.300.0", Status: DriftOK, Notes: "supported range"},
	})
	if !strings.Contains(text, "Hook drift report:") {
		t.Fatal("missing header")
	}
	if !strings.Contains(text, "claude: not installed") {
		t.Fatal("missing claude line")
	}
	if !strings.Contains(text, "codex: version=0.120.0") || !strings.Contains(text, "status=ok") {
		t.Fatalf("codex line: %s", text)
	}
}

// TestDetectDrift_codexCarriesCapabilities verifies the codex probe
// populates the capability list from the matrix in codex_caps.go so an
// operator running `doctor` sees what hook features Slimference relies
// on for the installed version.
func TestDetectDrift_codexCarriesCapabilities(t *testing.T) {
	stubCLIVersion(t,
		map[string]string{
			"claude": "claude 1.5.3",
			"codex":  "codex 0.125.0",
		},
		nil,
	)
	reports := DetectDrift(context.Background())
	for _, r := range reports {
		if r.CLI != "codex" {
			continue
		}
		want := []string{
			string(CodexCapDecisionBlock),
			string(CodexCapPermissionRequestDecision),
			string(CodexCapPostToolReplaceResult),
			string(CodexCapLifecycleContext),
		}
		if strings.Join(r.Capabilities, ",") != strings.Join(want, ",") {
			t.Fatalf("codex capabilities: got %v, want %v", r.Capabilities, want)
		}
		if r.CapabilityNotice != "" {
			t.Fatalf("decision_block-only steady state must not raise a notice; got %q", r.CapabilityNotice)
		}
	}
}

// TestDetectDrift_claudeOmitsCapabilities confirms only the codex probe
// populates capability data; Claude Code has no Slimference-tracked
// capability matrix yet.
func TestDetectDrift_claudeOmitsCapabilities(t *testing.T) {
	stubCLIVersion(t,
		map[string]string{
			"claude": "claude 1.5.3",
			"codex":  "codex 0.125.0",
		},
		nil,
	)
	reports := DetectDrift(context.Background())
	for _, r := range reports {
		if r.CLI != "claude" {
			continue
		}
		if len(r.Capabilities) != 0 {
			t.Fatalf("claude must have no capability list yet, got %v", r.Capabilities)
		}
		if r.CapabilityNotice != "" {
			t.Fatalf("claude must not raise a capability notice")
		}
	}
}

// TestCodexCapabilityNotice_TransparentRewriteFlipped fires the
// notification path when the capability matrix evolves to advertise
// transparent_rewrite for a future Codex range.
func TestCodexCapabilityNotice_TransparentRewriteFlipped(t *testing.T) {
	t.Parallel()
	caps := []CodexCapability{CodexCapDecisionBlock, CodexCapTransparentRewrite}
	got := codexCapabilityNotice("1.0.0", caps)
	if got == "" {
		t.Fatal("expected a capability notice when transparent_rewrite becomes available")
	}
	if !strings.Contains(got, "updatedInput") {
		t.Fatalf("notice should reference the upstream field, got %q", got)
	}
	if !strings.Contains(got, "hook install codex") {
		t.Fatalf("notice should tell the operator how to act, got %q", got)
	}
}

// TestCodexCapabilityNotice_EmptyVersion stays quiet when the CLI
// version could not be parsed.
func TestCodexCapabilityNotice_EmptyVersion(t *testing.T) {
	t.Parallel()
	if got := codexCapabilityNotice("", nil); got != "" {
		t.Fatalf("empty version must yield empty notice, got %q", got)
	}
}

// TestCodexCapabilityNotice_DecisionBlockOnlyQuiet keeps doctor output
// noise-free on the steady state where only legacy decision_block is
// advertised.
func TestCodexCapabilityNotice_DecisionBlockOnlyQuiet(t *testing.T) {
	t.Parallel()
	got := codexCapabilityNotice("0.125.0", []CodexCapability{CodexCapDecisionBlock})
	if got != "" {
		t.Fatalf("decision_block-only state must be quiet, got %q", got)
	}
}

// TestFormatDriftReports_RendersCapabilitiesAndNotice formats the new
// capability/notice fields so an operator can read them off the doctor
// output.
func TestFormatDriftReports_RendersCapabilitiesAndNotice(t *testing.T) {
	t.Parallel()
	text := FormatDriftReports([]DriftReport{
		{
			CLI:              "codex",
			BinaryFound:      true,
			VersionParsed:    "1.0.0",
			MinSupported:     "0.117.0",
			MaxTested:        "1.0.0",
			Status:           DriftOK,
			Notes:            "supported range",
			Capabilities:     []string{"decision_block", "transparent_rewrite"},
			CapabilityNotice: "modern hook payload available; rerun installer",
		},
	})
	if !strings.Contains(text, "capabilities: decision_block, transparent_rewrite") {
		t.Fatalf("missing capability list line, got %q", text)
	}
	if !strings.Contains(text, "NOTE: modern hook payload available") {
		t.Fatalf("missing notice line, got %q", text)
	}
}
