package filter

import (
	"path/filepath"
	"regexp"
	"strings"
)

// TryCompactTerraformPlan compresses `terraform plan` and `terraform apply`
// output to its structurally relevant lines: the summary tail ("Plan: N to
// add, M to change, K to destroy" / "Apply complete! Resources: ..."),
// per-resource change headers, and any Error/Warning blocks. Per-attribute
// diff bodies are dropped.
//
// Zero-downside: output is only emitted when strictly shorter than input.
// Non-terraform argv or unrecognised output is passed through unchanged.
func TryCompactTerraformPlan(argv []string, stdout []byte) ([]byte, bool) {
	if !isTerraformPlanOrApply(argv) {
		return stdout, false
	}
	compressed := compressTerraformOutput(stdout)
	if len(compressed) >= len(stdout) {
		return stdout, false
	}
	return compressed, true
}

var (
	reTerraformResourceChange = regexp.MustCompile(`^\s*([#+~\-]|-/\+|<=|\+\/-|\+\s*resource)\s`)
	reTerraformSummary        = regexp.MustCompile(`^(Plan:|Apply complete!|Destroy complete!|No changes\.|Changes to Outputs:)`)
)

// terraformErrorPrefixes are the box-drawing or ASCII pipe characters
// terraform uses to prefix multi-line error/warning bodies.
var terraformErrorPrefixes = []string{"│", "|"}

func hasTerraformErrorPrefix(trimmed string) bool {
	for _, p := range terraformErrorPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func isTerraformErrorHeader(trimmed string) bool {
	// Strip a leading box-drawing / pipe prefix plus spaces before checking.
	s := trimmed
	for _, p := range terraformErrorPrefixes {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.TrimLeft(s, " \t")
	return strings.HasPrefix(s, "Error:") || strings.HasPrefix(s, "Warning:")
}

func isTerraformPlanOrApply(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	if base != "terraform" && base != "terraform.exe" && base != "tf" && base != "tofu" {
		return false
	}
	sub := strings.ToLower(argv[1])
	return sub == "plan" || sub == "apply" || sub == "destroy"
}

// compressTerraformOutput keeps resource change headers, summary lines, and
// error/warning blocks (every line prefixed with the box-drawing `│` or the
// ASCII `|`). Everything else is dropped, including the large per-attribute
// diff body.
func compressTerraformOutput(stdout []byte) []byte {
	lines := strings.Split(string(stdout), "\n")
	kept := make([]string, 0, len(lines)/4)
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case isTerraformErrorHeader(trimmed):
			kept = append(kept, line)
		case hasTerraformErrorPrefix(trimmed):
			kept = append(kept, line)
		case reTerraformSummary.MatchString(trimmed):
			kept = append(kept, line)
		case isTerraformResourceHeader(line, trimmed):
			kept = append(kept, line)
		}
	}
	return []byte(strings.Join(kept, "\n"))
}

// isTerraformResourceHeader matches the opening line of a resource change
// block: `  + resource "aws_s3_bucket" "x" {`,
// `  - resource ...`, `  ~ resource ...`, `  # module.foo will be ...`.
// Plain `+ 5` or `- 3` diff lines are *not* resource headers.
func isTerraformResourceHeader(line, trimmed string) bool {
	if strings.HasPrefix(trimmed, "# ") {
		return true
	}
	if !reTerraformResourceChange.MatchString(line) {
		return false
	}
	// Resource headers carry a quoted terraform construct (resource /
	// data / output). Module-path changes are only headers when prefixed
	// with `#`, which is handled by the caller above. Filter out simple
	// attribute deltas like `+ acl = "private"`.
	return strings.Contains(trimmed, "resource \"") ||
		strings.Contains(trimmed, "data \"") ||
		strings.Contains(trimmed, "output \"")
}
