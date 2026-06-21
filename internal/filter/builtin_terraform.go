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
// final success/failure verdict, the provider count, the module count, the
// lock-file-created fact, and any error/warning blocks; the per-line download
// chatter is dropped.
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

// TryCompactTerraformStateList deliberately full-passes `terraform state list`.
// Resource addresses are the evidence the model requested; dropping middle
// addresses is a product drawdown unless the route guarantees exact archive
// recovery. Keep this disabled in the default filter package.
func TryCompactTerraformStateList(argv []string, stdout []byte) ([]byte, bool) {
	return stdout, false
}

// TryCompactTerraformOutput deliberately full-passes human-readable
// `terraform output`. Output names and values are user-requested facts and may
// include exactly the value the model needs. Structured `terraform show -json`
// remains covered by the safer JSON parser path.
func TryCompactTerraformOutput(argv []string, stdout []byte) ([]byte, bool) {
	return stdout, false
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
// modules touched, the lock-file-created fact, and any error / warning blocks;
// per-provider install chatter is collapsed into a single count line.
func compressTerraformInit(stdout []byte) []byte {
	lines := strings.Split(string(stdout), "\n")
	kept := make([]string, 0, 16)
	providers := make(map[string]struct{})
	moduleCount := 0
	successSeen := false
	lockFileCreated := false
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
			provider, ok := terraformInitProviderAddress(trimmed)
			if !ok {
				kept = append(kept, line)
				continue
			}
			providers[provider] = struct{}{}
		case strings.Contains(trimmed, "has created a lock file") && strings.Contains(trimmed, ".terraform.lock.hcl"):
			lockFileCreated = true
		}
	}
	if !successSeen && len(providers) == 0 && moduleCount == 0 && len(kept) == 0 {
		// Nothing recognised; return original bytes so the caller's
		// shorter-than-input gate falls through to passthrough.
		return stdout
	}
	if len(providers) > 0 {
		kept = append(kept, fmt.Sprintf("- %d provider(s) installed", len(providers)))
	}
	if moduleCount > 0 {
		kept = append(kept, fmt.Sprintf("- %d module(s) downloaded", moduleCount))
	}
	if lockFileCreated {
		kept = append(kept, "- lock file created: .terraform.lock.hcl")
	}
	return []byte(strings.Join(kept, "\n"))
}

func terraformInitProviderAddress(line string) (string, bool) {
	for raw := range strings.FieldsSeq(line) {
		token := strings.Trim(raw, `"'(),:;[]`)
		token = strings.TrimRight(token, ".")
		token = strings.TrimPrefix(token, "registry.terraform.io/")
		parts := strings.Split(token, "/")
		if len(parts) != 2 || !terraformProviderAddressPart(parts[0]) || !terraformProviderAddressPart(parts[1]) {
			continue
		}
		return token, true
	}
	return "", false
}

func terraformProviderAddressPart(part string) bool {
	if part == "" {
		return false
	}
	for _, r := range part {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
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
