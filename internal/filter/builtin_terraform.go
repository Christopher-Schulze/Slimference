package filter

import (
	"fmt"
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

// TryCompactTerraformInit compresses `terraform init` output. Real-session
// init runs are typically 30-150 lines: a banner, per-module download lines,
// per-provider download lines, the success footer. Compaction keeps the
// final success/failure verdict, the provider count, the module count, and
// any error/warning blocks; the per-line download chatter is dropped.
func TryCompactTerraformInit(argv []string, stdout []byte) ([]byte, bool) {
	if !isTerraformSubcommand(argv, "init") {
		return stdout, false
	}
	compressed := compressTerraformInit(stdout)
	if len(compressed) >= len(stdout) {
		return stdout, false
	}
	return compressed, true
}

// TryCompactTerraformValidate compresses `terraform validate` output. The
// happy path is a single "Success!" line; the failure path is a sequence
// of `│ Error:` blocks. Compaction trims any decorative banner lines and
// keeps the verdict + every error block verbatim.
func TryCompactTerraformValidate(argv []string, stdout []byte) ([]byte, bool) {
	if !isTerraformSubcommand(argv, "validate") {
		return stdout, false
	}
	compressed := compressTerraformValidate(stdout)
	if len(compressed) >= len(stdout) {
		return stdout, false
	}
	return compressed, true
}

// TryCompactTerraformStateList compresses `terraform state list` output.
// The output is one resource address per line; long state files routinely
// emit hundreds of them. Compaction keeps the first 30 and the last 5
// lines plus a `... <N> more ...` marker so the model still sees the
// shape and the most-recently-added resources without paying the full
// token tax.
func TryCompactTerraformStateList(argv []string, stdout []byte) ([]byte, bool) {
	if !isTerraformStateList(argv) {
		return stdout, false
	}
	compressed := compressTerraformStateList(stdout)
	if len(compressed) >= len(stdout) {
		return stdout, false
	}
	return compressed, true
}

// TryCompactTerraformOutput compresses `terraform output` (without `-json`)
// when the result is a long sequence of `name = value` lines. Compaction
// keeps the first 30 lines and emits a `... <N> more outputs ...` marker.
func TryCompactTerraformOutput(argv []string, stdout []byte) ([]byte, bool) {
	if !isTerraformSubcommand(argv, "output") {
		return stdout, false
	}
	if hasTerraformJSONFlag(argv) {
		return stdout, false
	}
	compressed := compressTerraformOutputCmd(stdout)
	if len(compressed) >= len(stdout) {
		return stdout, false
	}
	return compressed, true
}

// TryCompactTerraformShow compresses `terraform show` output (the human-
// readable, non-JSON variant). The output mirrors `terraform plan`'s
// structural shape, so we delegate to the same plan/apply compactor; the
// shape detector lets the dispatch chain keep them as separate entries.
//
// When nothing in the body matches a plan/apply shape we passthrough so
// an unstructured `terraform show -no-color` of e.g. a state snapshot is
// not silently emptied.
func TryCompactTerraformShow(argv []string, stdout []byte) ([]byte, bool) {
	if !isTerraformSubcommand(argv, "show") {
		return stdout, false
	}
	if hasTerraformJSONFlag(argv) {
		return stdout, false
	}
	compressed := compressTerraformOutput(stdout)
	if len(compressed) == 0 || len(compressed) >= len(stdout) {
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
	if !isTerraformBinary(argv[0]) {
		return false
	}
	sub := strings.ToLower(argv[1])
	switch sub {
	case "plan", "apply", "destroy", "refresh":
		return true
	}
	return false
}

func isTerraformBinary(arg0 string) bool {
	base := strings.ToLower(filepath.Base(arg0))
	return base == "terraform" || base == "terraform.exe" || base == "tf" || base == "tofu" || base == "tofu.exe"
}

// isTerraformSubcommand returns true when argv is `<terraform-binary> <sub>`,
// regardless of trailing flags.
func isTerraformSubcommand(argv []string, sub string) bool {
	if len(argv) < 2 {
		return false
	}
	if !isTerraformBinary(argv[0]) {
		return false
	}
	return strings.ToLower(argv[1]) == sub
}

// isTerraformStateList matches `terraform state list [...]`, which is a
// two-word subcommand: argv[1] = "state", argv[2] = "list".
func isTerraformStateList(argv []string) bool {
	if len(argv) < 3 {
		return false
	}
	if !isTerraformBinary(argv[0]) {
		return false
	}
	return strings.ToLower(argv[1]) == "state" && strings.ToLower(argv[2]) == "list"
}

// hasTerraformJSONFlag returns true when any argv element is `-json` or
// `--json`. Used to skip compaction of structured-output variants where the
// caller is parsing JSON downstream and shape changes would break them.
func hasTerraformJSONFlag(argv []string) bool {
	for _, a := range argv {
		if a == "-json" || a == "--json" {
			return true
		}
	}
	return false
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

// reTerraformInitFinalizer matches the finalizer line that terraform init
// emits on success ("Terraform has been successfully initialized!" /
// "OpenTofu has been successfully initialized!").
var reTerraformInitFinalizer = regexp.MustCompile(`^(Terraform|OpenTofu) has been successfully initialized!`)

// reTerraformInitProviderInstall matches the per-provider install lines
// terraform emits during init ("- Installing hashicorp/aws v5.31.0...",
// "- Installed hashicorp/aws v5.31.0 (signed by HashiCorp)").
var reTerraformInitProviderInstall = regexp.MustCompile(`^- (Installing|Installed|Reusing|Finding|Using|Downloading) `)

// compressTerraformInit keeps the verdict line, the count of providers /
// modules touched, and any error / warning blocks; per-provider install
// chatter is collapsed into a single "<N> providers installed" line.
func compressTerraformInit(stdout []byte) []byte {
	lines := strings.Split(string(stdout), "\n")
	kept := make([]string, 0, 16)
	providerCount := 0
	moduleCount := 0
	successSeen := false
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case isTerraformErrorHeader(trimmed):
			kept = append(kept, line)
		case hasTerraformErrorPrefix(trimmed):
			kept = append(kept, line)
		case reTerraformInitFinalizer.MatchString(trimmed):
			kept = append(kept, line)
			successSeen = true
		case strings.HasPrefix(trimmed, "Initializing the backend") ||
			strings.HasPrefix(trimmed, "Initializing modules") ||
			strings.HasPrefix(trimmed, "Initializing provider plugins"):
			kept = append(kept, line)
		case strings.HasPrefix(trimmed, "Downloading ") && strings.Contains(trimmed, "for module"):
			moduleCount++
		case reTerraformInitProviderInstall.MatchString(trimmed):
			providerCount++
		}
	}
	if !successSeen && providerCount == 0 && moduleCount == 0 && len(kept) == 0 {
		// Nothing recognised; return original bytes so the caller's
		// shorter-than-input gate falls through to passthrough.
		return stdout
	}
	if providerCount > 0 {
		kept = append(kept, fmt.Sprintf("- %d provider(s) installed", providerCount))
	}
	if moduleCount > 0 {
		kept = append(kept, fmt.Sprintf("- %d module(s) downloaded", moduleCount))
	}
	return []byte(strings.Join(kept, "\n"))
}

// compressTerraformValidate keeps every error block verbatim plus the
// happy-path "Success!" line; all decorative banners are dropped.
func compressTerraformValidate(stdout []byte) []byte {
	lines := strings.Split(string(stdout), "\n")
	kept := make([]string, 0, 8)
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case isTerraformErrorHeader(trimmed):
			kept = append(kept, line)
		case hasTerraformErrorPrefix(trimmed):
			kept = append(kept, line)
		case strings.HasPrefix(trimmed, "Success!") ||
			strings.HasPrefix(trimmed, "The configuration is valid"):
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return stdout
	}
	return []byte(strings.Join(kept, "\n"))
}

// compressTerraformStateList keeps the first 30 and last 5 lines of a
// `terraform state list` body and emits a `... <N> more ...` marker
// between them. Empty / short outputs (<= 35 lines) pass through.
func compressTerraformStateList(stdout []byte) []byte {
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	const headN = 30
	const tailN = 5
	if len(lines) <= headN+tailN {
		return stdout
	}
	out := make([]string, 0, headN+tailN+1)
	out = append(out, lines[:headN]...)
	out = append(out, fmt.Sprintf("... %d more resources omitted ...", len(lines)-headN-tailN))
	out = append(out, lines[len(lines)-tailN:]...)
	return []byte(strings.Join(out, "\n") + "\n")
}

// compressTerraformOutputCmd keeps the first 30 `name = value` output
// lines and emits a marker for the rest. Multi-line output values
// (objects, lists) are kept whole when they fall inside the first 30
// logical entries; lines past the 30-entry budget are counted and
// omitted.
func compressTerraformOutputCmd(stdout []byte) []byte {
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	const budget = 30
	kept := make([]string, 0, budget+1)
	entries := 0
	depth := 0
	dropped := 0
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		isTopLevelEntry := depth == 0 && reTerraformOutputEntry.MatchString(trimmed)
		if isTopLevelEntry {
			entries++
		}
		if entries > budget {
			if depth == 0 && isTopLevelEntry {
				dropped++
			}
			depth = adjustTerraformOutputDepth(depth, trimmed)
			continue
		}
		kept = append(kept, line)
		depth = adjustTerraformOutputDepth(depth, trimmed)
	}
	if dropped == 0 {
		return stdout
	}
	kept = append(kept, fmt.Sprintf("... %d more outputs omitted ...", dropped))
	return []byte(strings.Join(kept, "\n") + "\n")
}

// reTerraformOutputEntry matches a top-level output entry (`name = ...`)
// at the start of a logical line.
var reTerraformOutputEntry = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=`)

// adjustTerraformOutputDepth tracks brace nesting so multi-line object /
// list values can be kept or skipped as one unit.
func adjustTerraformOutputDepth(depth int, trimmed string) int {
	for _, r := range trimmed {
		switch r {
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}
