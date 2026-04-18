package hooks

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DriftStatus describes the compatibility of an installed upstream CLI
// (Claude Code or Codex) against Slimference's known-good version range.
type DriftStatus string

const (
	// DriftUnknown means the CLI binary is not installed, not executable,
	// or its --version output could not be parsed.
	DriftUnknown DriftStatus = "unknown"
	// DriftOK means the detected version falls inside the known-good range.
	DriftOK DriftStatus = "ok"
	// DriftBelow means the detected version is older than the minimum
	// supported version.
	DriftBelow DriftStatus = "below-minimum"
	// DriftAbove means the detected version is newer than the last version
	// we verified against - behaviour is probably fine but should be
	// re-checked when time allows.
	DriftAbove DriftStatus = "above-tested"
)

// DriftReport is the user-facing shape of a single CLI compatibility probe.
type DriftReport struct {
	CLI           string
	BinaryFound   bool
	VersionRaw    string
	VersionParsed string
	MinSupported  string
	MaxTested     string
	Status        DriftStatus
	Notes         string
}

// knownGoodVersions tracks the latest (min, max-tested) ranges we have
// actually verified Slimference against. Bump max_tested after a new CLI
// release is smoke-tested; bump min_supported only when a breaking change
// lands that we have explicitly adopted.
var knownGoodVersions = map[string]struct {
	min string
	max string
}{
	// Claude Code shipped its 1.x line and then 2.x. Bump max_tested only
	// after explicitly smoke-testing against a newer release.
	"claude": {min: "1.0.0", max: "3.0.0"},
	// Codex rewrote its hook contract at 0.117.0 (handled by T05) and is
	// still on the 0.x train. Bump max_tested when a new release is
	// smoke-tested.
	"codex": {min: "0.117.0", max: "1.0.0"},
}

// cliVersionCmdFn is overridable in tests so version detection can be
// stubbed without executing real binaries.
var cliVersionCmdFn = func(ctx context.Context, name string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, "--version")
	return cmd.CombinedOutput()
}

// DetectDrift probes the supported CLIs (claude, codex) and returns one
// DriftReport per CLI. Reports always include the CLI name even when the
// binary is not installed - the absence is itself useful signal.
func DetectDrift(ctx context.Context) []DriftReport {
	reports := make([]DriftReport, 0, len(knownGoodVersions))
	// Deterministic order.
	for _, cli := range []string{"claude", "codex"} {
		reports = append(reports, probeCLI(ctx, cli))
	}
	return reports
}

// probeCLI runs `<cli> --version` with a short timeout, parses the output
// and compares it against the known-good range.
func probeCLI(parent context.Context, cli string) DriftReport {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	gg := knownGoodVersions[cli]
	report := DriftReport{
		CLI:          cli,
		MinSupported: gg.min,
		MaxTested:    gg.max,
		Status:       DriftUnknown,
	}

	out, err := cliVersionCmdFn(ctx, cli)
	if err != nil {
		report.Notes = "binary not installed or not executable"
		return report
	}
	report.BinaryFound = true
	report.VersionRaw = strings.TrimSpace(string(out))

	version := parseCLIVersion(report.VersionRaw)
	if version == "" {
		report.Notes = "version string not recognised"
		return report
	}
	report.VersionParsed = version
	report.Status = classifyDrift(version, gg.min, gg.max)
	switch report.Status {
	case DriftOK:
		report.Notes = "supported range"
	case DriftBelow:
		report.Notes = fmt.Sprintf("below minimum supported %s - upgrade recommended", gg.min)
	case DriftAbove:
		report.Notes = fmt.Sprintf("newer than last tested %s - contract may drift", gg.max)
	}
	return report
}

// versionRegex matches a leading semver-like triple anywhere in the output.
// Accepts optional "v" prefix and optional pre-release / metadata tail.
var versionRegex = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// parseCLIVersion extracts the first semver triple from a CLI's --version
// string. Returns "" if no triple is found.
func parseCLIVersion(raw string) string {
	m := versionRegex.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return fmt.Sprintf("%s.%s.%s", m[1], m[2], m[3])
}

// classifyDrift compares detected against [min, max]. All inputs must be
// semver triples without prefix; invalid triples return DriftUnknown.
func classifyDrift(detected, min, max string) DriftStatus {
	d, ok1 := splitSemver(detected)
	lo, ok2 := splitSemver(min)
	hi, ok3 := splitSemver(max)
	if !ok1 || !ok2 || !ok3 {
		return DriftUnknown
	}
	if compareSemver(d, lo) < 0 {
		return DriftBelow
	}
	if compareSemver(d, hi) > 0 {
		return DriftAbove
	}
	return DriftOK
}

func splitSemver(s string) ([3]int, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func compareSemver(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// FormatDriftReports renders the DetectDrift output as a human-readable
// block suitable for `slimference doctor` or `slimference hook
// check-upstream` output.
func FormatDriftReports(reports []DriftReport) string {
	var sb strings.Builder
	sb.WriteString("Hook drift report:\n")
	for _, r := range reports {
		sb.WriteString(fmt.Sprintf("  %s: ", r.CLI))
		if !r.BinaryFound {
			sb.WriteString("not installed\n")
			continue
		}
		sb.WriteString(fmt.Sprintf("version=%s ", r.VersionParsed))
		sb.WriteString(fmt.Sprintf("status=%s ", r.Status))
		sb.WriteString(fmt.Sprintf("supported=[%s, %s]", r.MinSupported, r.MaxTested))
		if r.Notes != "" {
			sb.WriteString(" - ")
			sb.WriteString(r.Notes)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
