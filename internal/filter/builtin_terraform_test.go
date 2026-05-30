package filter

import (
	"strings"
	"testing"
)

// TestTryCompactTerraformPlan_passthroughNonTerraform leaves non-terraform
// argv untouched.
func TestTryCompactTerraformPlan_passthroughNonTerraform(t *testing.T) {
	t.Parallel()
	in := []byte("lots of output\n")
	out, ok := TryCompactTerraformPlan([]string{"ls"}, in)
	if ok || string(out) != string(in) {
		t.Fatalf("non-terraform argv must passthrough, got ok=%v out=%q", ok, out)
	}
}

// TestTryCompactTerraformPlan_passthroughUnknownSub leaves subcommands not
// owned by the plan/apply compactor alone.
func TestTryCompactTerraformPlan_passthroughUnknownSub(t *testing.T) {
	t.Parallel()
	in := []byte("lots of output\n")
	out, ok := TryCompactTerraformPlan([]string{"terraform", "import", "aws_s3.x", "id"}, in)
	if ok {
		t.Fatalf("terraform import must passthrough, got %s", out)
	}
}

// TestTryCompactTerraformPlan_compressesPlanOutput drops per-attribute diff
// bodies and keeps resource change headers and summary line.
func TestTryCompactTerraformPlan_compressesPlanOutput(t *testing.T) {
	t.Parallel()
	in := []byte(`Terraform used the selected providers to generate the following execution plan. Resource actions are indicated with the following symbols:
  + create
  - destroy
  ~ update in-place

Terraform will perform the following actions:

  # aws_s3_bucket.main will be created
  + resource "aws_s3_bucket" "main" {
      + acl    = "private"
      + bucket = "my-bucket"
      + tags   = {
          + "Environment" = "prod"
        }
      + versioning {
          + enabled = true
        }
    }

  # aws_iam_role.app will be updated in-place
  ~ resource "aws_iam_role" "app" {
      ~ policy = jsonencode({ ... })
      ~ tags   = {
          ~ "Environment" = "dev" -> "prod"
        }
    }

Plan: 1 to add, 1 to change, 0 to destroy.
`)
	out, ok := TryCompactTerraformPlan([]string{"terraform", "plan"}, in)
	if !ok {
		t.Fatal("expected compression")
	}
	s := string(out)
	for _, need := range []string{
		"# aws_s3_bucket.main will be created",
		"resource \"aws_s3_bucket\"",
		"# aws_iam_role.app will be updated in-place",
		"resource \"aws_iam_role\"",
		"Plan: 1 to add, 1 to change, 0 to destroy.",
	} {
		if !strings.Contains(s, need) {
			t.Fatalf("missing %q in compressed output:\n%s", need, s)
		}
	}
	if strings.Contains(s, "acl    = \"private\"") {
		t.Fatalf("per-attribute diff leaked: %s", s)
	}
	if len(out) >= len(in) {
		t.Fatalf("output must be strictly shorter: in=%d out=%d", len(in), len(out))
	}
}

// TestTryCompactTerraformPlan_applyOutput keeps the "Apply complete" summary.
func TestTryCompactTerraformPlan_applyOutput(t *testing.T) {
	t.Parallel()
	in := []byte(`aws_s3_bucket.main: Creating...
aws_s3_bucket.main: Still creating... [10s elapsed]
aws_s3_bucket.main: Still creating... [20s elapsed]
aws_s3_bucket.main: Creation complete after 25s [id=my-bucket]

Apply complete! Resources: 1 added, 0 changed, 0 destroyed.
`)
	out, ok := TryCompactTerraformPlan([]string{"terraform", "apply"}, in)
	if !ok {
		t.Fatal("expected compression on apply")
	}
	if !strings.Contains(string(out), "Apply complete!") {
		t.Fatalf("apply summary lost: %s", out)
	}
}

// TestTryCompactTerraformPlan_errorBlockPreserved keeps error headers and the
// following body lines until a blank line.
func TestTryCompactTerraformPlan_errorBlockPreserved(t *testing.T) {
	t.Parallel()
	in := []byte(`aws_s3_bucket.main: Creating...
│ Error: error creating S3 bucket: BucketAlreadyExists: The requested bucket name is not available.
│   status code: 409
│
│   with aws_s3_bucket.main,
│   on main.tf line 1, in resource "aws_s3_bucket" "main":
│    1: resource "aws_s3_bucket" "main" {

Plan: 0 to add, 0 to change, 0 to destroy.
`)
	out, ok := TryCompactTerraformPlan([]string{"terraform", "apply"}, in)
	if !ok {
		t.Fatal("expected compression")
	}
	s := string(out)
	if !strings.Contains(s, "BucketAlreadyExists") {
		t.Fatalf("error body lost: %s", s)
	}
	if !strings.Contains(s, "Plan: 0 to add") {
		t.Fatalf("summary lost: %s", s)
	}
}

// TestTryCompactTerraformPlan_tooShortPassthrough leaves already-short
// outputs alone.
func TestTryCompactTerraformPlan_tooShortPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte(`Plan: 0 to add, 0 to change, 0 to destroy.`)
	out, ok := TryCompactTerraformPlan([]string{"terraform", "plan"}, in)
	if ok {
		t.Fatalf("short output must passthrough, got %s", out)
	}
	_ = out
}

// TestTryCompactTerraformPlan_tfAlias accepts `tf` and `tofu` as aliases.
func TestTryCompactTerraformPlan_tfAlias(t *testing.T) {
	t.Parallel()
	in := []byte(`  # aws_s3_bucket.main will be created
  + resource "aws_s3_bucket" "main" {
      + acl = "private"
    }

Plan: 1 to add, 0 to change, 0 to destroy.
`)
	out, ok := TryCompactTerraformPlan([]string{"tofu", "plan"}, in)
	if !ok {
		t.Fatal("tofu plan must be recognised")
	}
	if !strings.Contains(string(out), "aws_s3_bucket") {
		t.Fatalf("missing header: %s", out)
	}
}

// TestIsTerraformResourceHeader covers positive and negative cases
// including the attribute-delta false-positive filter.
func TestIsTerraformResourceHeader(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line string
		want bool
	}{
		{`  # aws_s3_bucket.main will be created`, true},
		{`  + resource "aws_s3_bucket" "main" {`, true},
		{`  ~ resource "aws_iam_role" "app" {`, true},
		{`  - resource "aws_s3_bucket" "old" {`, true},
		{`  + data "aws_ami" "ubuntu" {`, true},
		{`  ~ module.vpc will be modified`, false},
		{`  + output "url" {`, true},
		{`      + acl = "private"`, false},
		{`unrelated text`, false},
	}
	for _, tc := range cases {
		got := isTerraformResourceHeader(tc.line, strings.TrimLeft(tc.line, " \t"))
		if got != tc.want {
			t.Errorf("isTerraformResourceHeader(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// TestTryCompactTerraformPlan_refresh handles the refresh subcommand
// (same shape as apply: per-resource lifecycle lines + summary).
func TestTryCompactTerraformPlan_refresh(t *testing.T) {
	t.Parallel()
	in := []byte(`aws_s3_bucket.main: Refreshing state... [id=my-bucket]
aws_s3_bucket.main: Refreshed
aws_s3_bucket.other: Refreshing state... [id=other]
aws_s3_bucket.other: Refreshed

  # aws_s3_bucket.main has changed
  ~ resource "aws_s3_bucket" "main" {
      ~ tags = {
        }
    }

Plan: 0 to add, 1 to change, 0 to destroy.
`)
	out, ok := TryCompactTerraformPlan([]string{"terraform", "refresh"}, in)
	if !ok {
		t.Fatal("terraform refresh must compact")
	}
	if !strings.Contains(string(out), "Plan: 0 to add, 1 to change") {
		t.Fatalf("missing summary: %s", out)
	}
}

// TestTryCompactTerraformInit_compactsProviderInstall collapses per-provider
// install chatter into a count line and keeps the success footer.
func TestTryCompactTerraformInit_compactsProviderInstall(t *testing.T) {
	t.Parallel()
	in := []byte(`Initializing the backend...

Initializing provider plugins...
- Finding hashicorp/aws versions matching "~> 5.0"...
- Finding hashicorp/random versions matching "~> 3.5"...
- Finding hashicorp/null versions matching "~> 3.2"...
- Installing hashicorp/aws v5.31.0...
- Installed hashicorp/aws v5.31.0 (signed by HashiCorp)
- Installing hashicorp/random v3.5.1...
- Installed hashicorp/random v3.5.1 (signed by HashiCorp)
- Installing hashicorp/null v3.2.1...
- Installed hashicorp/null v3.2.1 (signed by HashiCorp)

Terraform has created a lock file .terraform.lock.hcl to record the provider
selections it made above. Include this file in your version control repository
so that Terraform can guarantee to make the same selections by default when
you run "terraform init" in the future.

Terraform has been successfully initialized!
`)
	out, ok := TryCompactTerraformInit([]string{"terraform", "init"}, in)
	if !ok {
		t.Fatal("expected init compaction")
	}
	s := string(out)
	if !strings.Contains(s, "successfully initialized") {
		t.Fatalf("missing success line, got %q", s)
	}
	if !strings.Contains(s, "provider(s) installed") {
		t.Fatalf("missing provider count line, got %q", s)
	}
	if strings.Contains(s, "hashicorp/aws v5.31.0") {
		t.Fatalf("per-provider install chatter leaked: %q", s)
	}
	if len(out) >= len(in) {
		t.Fatalf("output must be strictly shorter: in=%d out=%d", len(in), len(out))
	}
}

// TestTryCompactTerraformInit_modulesCounted aggregates per-module download
// lines into a single count line.
func TestTryCompactTerraformInit_modulesCounted(t *testing.T) {
	t.Parallel()
	in := []byte(`Initializing modules...
Downloading registry.terraform.io/terraform-aws-modules/vpc/aws 5.0.0 for module vpc...
Downloading registry.terraform.io/terraform-aws-modules/eks/aws 19.0.0 for module eks...
Downloading registry.terraform.io/terraform-aws-modules/rds/aws 6.0.0 for module rds...

Initializing the backend...
Initializing provider plugins...
- Installing hashicorp/aws v5.31.0...
- Installed hashicorp/aws v5.31.0 (signed by HashiCorp)

Terraform has been successfully initialized!
`)
	out, ok := TryCompactTerraformInit([]string{"terraform", "init"}, in)
	if !ok {
		t.Fatal("expected compaction")
	}
	if !strings.Contains(string(out), "module(s) downloaded") {
		t.Fatalf("missing module count, got %q", out)
	}
	if !strings.Contains(string(out), "provider(s) installed") {
		t.Fatalf("missing provider count, got %q", out)
	}
}

// TestTryCompactTerraformInit_keepsErrorBlock preserves an init failure body.
func TestTryCompactTerraformInit_keepsErrorBlock(t *testing.T) {
	t.Parallel()
	in := []byte(`Initializing the backend...

│ Error: Failed to install provider
│
│ Error while installing hashicorp/aws v5.31.0: checksum list has no SHA-256 hash for
│ "linux_arm64".
│
Initializing provider plugins...
- Finding hashicorp/aws versions matching "~> 5.0"...
- Installing hashicorp/aws v5.31.0...
`)
	out, ok := TryCompactTerraformInit([]string{"terraform", "init"}, in)
	if !ok {
		t.Fatal("expected compaction even on error path")
	}
	s := string(out)
	if !strings.Contains(s, "Failed to install provider") {
		t.Fatalf("error body lost: %q", s)
	}
	if !strings.Contains(s, "checksum list has no SHA-256 hash") {
		t.Fatalf("error detail lost: %q", s)
	}
}

// TestTryCompactTerraformInit_unrecognisedPassthrough returns input unchanged
// when nothing in the output matches an init shape.
func TestTryCompactTerraformInit_unrecognisedPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte("random unrelated text\nmore lines\n")
	out, ok := TryCompactTerraformInit([]string{"terraform", "init"}, in)
	if ok {
		t.Fatalf("expected passthrough on unrecognised body, got compaction: %s", out)
	}
	if string(out) != string(in) {
		t.Fatalf("expected byte-equal passthrough")
	}
}

// TestTryCompactTerraformInit_nonTerraformPassthrough leaves non-init argv alone.
func TestTryCompactTerraformInit_nonTerraformPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte("Terraform has been successfully initialized!\n")
	if _, ok := TryCompactTerraformInit([]string{"ls"}, in); ok {
		t.Fatal("non-terraform argv must passthrough")
	}
	if _, ok := TryCompactTerraformInit([]string{"terraform", "plan"}, in); ok {
		t.Fatal("non-init subcommand must passthrough")
	}
}

// TestTryCompactTerraformValidate_successOnly drops banners and keeps only
// the Success! verdict.
func TestTryCompactTerraformValidate_successOnly(t *testing.T) {
	t.Parallel()
	in := []byte(`Initializing modules...

Success! The configuration is valid.

`)
	out, ok := TryCompactTerraformValidate([]string{"terraform", "validate"}, in)
	if !ok {
		t.Fatal("expected validate compaction")
	}
	s := string(out)
	if !strings.Contains(s, "Success!") {
		t.Fatalf("missing verdict: %q", s)
	}
	if strings.Contains(s, "Initializing modules") {
		t.Fatalf("banner leaked: %q", s)
	}
}

// TestTryCompactTerraformValidate_failureKeepsErrorBlocks preserves every
// │ Error: block in a failed validate run.
func TestTryCompactTerraformValidate_failureKeepsErrorBlocks(t *testing.T) {
	t.Parallel()
	in := []byte(`
│ Error: Reference to undeclared resource
│
│   on main.tf line 14, in resource "aws_s3_bucket_policy" "x":
│   14:   bucket = aws_s3_bucket.does_not_exist.id
│
│ A resource "aws_s3_bucket" "does_not_exist" has not been declared.

`)
	out, ok := TryCompactTerraformValidate([]string{"terraform", "validate"}, in)
	if !ok {
		t.Fatal("expected validate compaction")
	}
	if !strings.Contains(string(out), "undeclared resource") {
		t.Fatalf("error body lost: %q", out)
	}
}

// TestTryCompactTerraformValidate_emptyPassthrough returns input unchanged
// when nothing is recognised.
func TestTryCompactTerraformValidate_emptyPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte("nothing useful here\n")
	out, ok := TryCompactTerraformValidate([]string{"terraform", "validate"}, in)
	if ok {
		t.Fatalf("expected passthrough, got %q", out)
	}
}

// TestTryCompactTerraformStateList_long keeps head + tail with a marker.
func TestTryCompactTerraformStateList_long(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("aws_s3_bucket.bucket_")
		b.WriteString(strconvI(i))
		b.WriteString("\n")
	}
	in := []byte(b.String())
	out, ok := TryCompactTerraformStateList([]string{"terraform", "state", "list"}, in)
	if !ok {
		t.Fatal("expected state list compaction")
	}
	s := string(out)
	if !strings.Contains(s, "more resources omitted") {
		t.Fatalf("missing marker: %q", s)
	}
	if !strings.Contains(s, "aws_s3_bucket.bucket_0") {
		t.Fatalf("missing head: %q", s)
	}
	if !strings.Contains(s, "aws_s3_bucket.bucket_199") {
		t.Fatalf("missing tail: %q", s)
	}
	if len(out) >= len(in) {
		t.Fatalf("output must be shorter: in=%d out=%d", len(in), len(out))
	}
}

// TestTryCompactTerraformStateList_shortPassthrough leaves short outputs alone.
func TestTryCompactTerraformStateList_shortPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte("aws_s3_bucket.a\naws_s3_bucket.b\n")
	out, ok := TryCompactTerraformStateList([]string{"terraform", "state", "list"}, in)
	if ok {
		t.Fatalf("expected passthrough, got %q", out)
	}
	_ = out
}

// TestTryCompactTerraformStateList_nonStateListPassthrough leaves other
// state subcommands alone.
func TestTryCompactTerraformStateList_nonStateListPassthrough(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactTerraformStateList([]string{"terraform", "state", "show", "x"}, []byte("data")); ok {
		t.Fatal("state show must passthrough")
	}
	if _, ok := TryCompactTerraformStateList([]string{"terraform", "plan"}, []byte("data")); ok {
		t.Fatal("plan must passthrough")
	}
	if _, ok := TryCompactTerraformStateList([]string{"ls"}, []byte("data")); ok {
		t.Fatal("non-terraform argv must passthrough")
	}
	if _, ok := TryCompactTerraformStateList([]string{"terraform"}, []byte("data")); ok {
		t.Fatal("two-word argv with missing subcommand must passthrough")
	}
}

// TestTryCompactTerraformOutput_long collapses the tail past the budget.
func TestTryCompactTerraformOutput_long(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("output_")
		b.WriteString(strconvI(i))
		b.WriteString(" = \"value_")
		b.WriteString(strconvI(i))
		b.WriteString("\"\n")
	}
	in := []byte(b.String())
	out, ok := TryCompactTerraformOutput([]string{"terraform", "output"}, in)
	if !ok {
		t.Fatal("expected output compaction")
	}
	s := string(out)
	if !strings.Contains(s, "more outputs omitted") {
		t.Fatalf("missing marker: %q", s)
	}
	if !strings.Contains(s, "output_0 = ") {
		t.Fatalf("missing first entry: %q", s)
	}
	if strings.Contains(s, "output_49 = ") {
		t.Fatalf("over-budget tail leaked: %q", s)
	}
}

func TestTryCompactTerraformOutputKeepsLateDiagnosticOutputs(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 45; i++ {
		b.WriteString("output_")
		b.WriteString(strconvI(i))
		b.WriteString(" = \"value_")
		b.WriteString(strconvI(i))
		b.WriteString("\"\n")
	}
	b.WriteString("diagnostic_error_message = \"E42 subnet missing route table\"\n")
	for i := 45; i < 65; i++ {
		b.WriteString("output_")
		b.WriteString(strconvI(i))
		b.WriteString(" = \"value_")
		b.WriteString(strconvI(i))
		b.WriteString("\"\n")
	}
	in := []byte(b.String())
	out, ok := TryCompactTerraformOutput([]string{"terraform", "output"}, in)
	if !ok {
		t.Fatal("expected output compaction")
	}
	s := string(out)
	if !strings.Contains(s, "diagnostic_error_message") || !strings.Contains(s, "E42 subnet missing route table") {
		t.Fatalf("late diagnostic output was dropped: %q", s)
	}
	if strings.Contains(s, "output_64 = ") {
		t.Fatalf("ordinary late output leaked: %q", s)
	}
	if len(out) >= len(in) {
		t.Fatalf("output must remain shorter: in=%d out=%d", len(in), len(out))
	}
}

// TestTryCompactTerraformOutput_shortPassthrough keeps short results untouched.
func TestTryCompactTerraformOutput_shortPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte("vpc_id = \"vpc-1234\"\nbucket = \"my-bucket\"\n")
	if _, ok := TryCompactTerraformOutput([]string{"terraform", "output"}, in); ok {
		t.Fatal("short output must passthrough")
	}
}

// TestTryCompactTerraformOutput_objectValue keeps multi-line object values
// inside the budget intact.
func TestTryCompactTerraformOutput_objectValue(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("config = {\n")
	b.WriteString("  key1 = \"a\"\n")
	b.WriteString("  key2 = \"b\"\n")
	b.WriteString("}\n")
	for i := 0; i < 40; i++ {
		b.WriteString("entry_")
		b.WriteString(strconvI(i))
		b.WriteString(" = \"x\"\n")
	}
	in := []byte(b.String())
	out, ok := TryCompactTerraformOutput([]string{"terraform", "output"}, in)
	if !ok {
		t.Fatal("expected compaction")
	}
	if !strings.Contains(string(out), "config = {") {
		t.Fatalf("multi-line object lost: %q", out)
	}
	if !strings.Contains(string(out), "key2 = \"b\"") {
		t.Fatalf("nested key lost: %q", out)
	}
}

// TestTryCompactTerraformOutput_jsonFlagPassthrough leaves -json alone so
// downstream JSON consumers see byte-for-byte output.
func TestTryCompactTerraformOutput_jsonFlagPassthrough(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("\"k_")
		b.WriteString(strconvI(i))
		b.WriteString("\":\"v\",\n")
	}
	in := []byte("{\n" + b.String() + "}\n")
	if _, ok := TryCompactTerraformOutput([]string{"terraform", "output", "-json"}, in); ok {
		t.Fatal("-json must passthrough untouched")
	}
}

// TestTryCompactTerraformShow_delegatesToPlanCompactor proves the show
// compactor reuses the plan/apply pipeline.
func TestTryCompactTerraformShow_delegatesToPlanCompactor(t *testing.T) {
	t.Parallel()
	in := []byte(`  # aws_s3_bucket.main was created
  + resource "aws_s3_bucket" "main" {
      + acl = "private"
    }

Plan: 1 to add, 0 to change, 0 to destroy.
`)
	out, ok := TryCompactTerraformShow([]string{"terraform", "show"}, in)
	if !ok {
		t.Fatal("expected show compaction")
	}
	if !strings.Contains(string(out), "aws_s3_bucket") {
		t.Fatalf("missing resource header: %q", out)
	}
	if strings.Contains(string(out), "acl = \"private\"") {
		t.Fatalf("per-attribute leaked: %q", out)
	}
}

// TestTryCompactTerraformShow_jsonPassthrough leaves -json alone.
func TestTryCompactTerraformShow_jsonPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte(`{"format_version":"1.0","planned_values":{}}`)
	if _, ok := TryCompactTerraformShow([]string{"terraform", "show", "-json"}, in); ok {
		t.Fatal("-json must passthrough")
	}
}

// TestTryCompactTerraformShow_nonShowPassthrough rejects non-show argv.
func TestTryCompactTerraformShow_nonShowPassthrough(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactTerraformShow([]string{"terraform", "plan"}, []byte("body")); ok {
		t.Fatal("plan must passthrough")
	}
}

// TestTryCompactTerraformShow_unrecognisedBodyPassthrough returns the
// original bytes when nothing in the body matches a plan/apply shape, so
// the shorter-than-input gate falls through to passthrough.
func TestTryCompactTerraformShow_unrecognisedBodyPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte("nothing matches a terraform shape here\n")
	out, ok := TryCompactTerraformShow([]string{"terraform", "show"}, in)
	if ok {
		t.Fatalf("expected passthrough for unrecognised body, got compaction: %s", out)
	}
}

// TestCompressTerraformOutputCmd_emptyInputPassthrough exercises the
// trim-trailing-newline-then-empty-input branch.
func TestCompressTerraformOutputCmd_emptyInputPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte("")
	out := compressTerraformOutputCmd(in)
	if string(out) != string(in) {
		t.Fatalf("empty input must round-trip, got %q", out)
	}
}

// TestApplyLayer0Filters_terraformInitDispatch proves the dispatcher picks
// the new init compactor up.
func TestApplyLayer0Filters_terraformInitDispatch(t *testing.T) {
	t.Parallel()
	in := []byte(`Initializing the backend...
- Installing hashicorp/aws v5.31.0...
- Installed hashicorp/aws v5.31.0 (signed by HashiCorp)
- Installing hashicorp/random v3.5.1...
- Installed hashicorp/random v3.5.1 (signed by HashiCorp)

Terraform has been successfully initialized!
`)
	out, name := applyLayer0Filters("", []string{"terraform", "init"}, in)
	if name != "terraform_init" {
		t.Fatalf("expected terraform_init dispatch, got %q", name)
	}
	if len(out) >= len(in) {
		t.Fatalf("dispatcher must shrink the output")
	}
}

// strconvI is an inline helper to keep tests self-contained without an
// import cycle on package strconv (already imported elsewhere in the
// filter package; this avoids an additional import in the test file).
func strconvI(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	negative := i < 0
	if negative {
		i = -i
	}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// TestApplyLayer0Filters_terraformDispatch proves the dispatcher picks this
// filter up.
func TestApplyLayer0Filters_terraformDispatch(t *testing.T) {
	t.Parallel()
	in := []byte(`  # aws_s3_bucket.main will be created
  + resource "aws_s3_bucket" "main" {
      + acl = "private"
      + bucket = "my-bucket"
      + tags = {
          + "Environment" = "prod"
        }
    }

Plan: 1 to add, 0 to change, 0 to destroy.
`)
	out, name := applyLayer0Filters("", []string{"terraform", "plan"}, in)
	if name != "terraform_plan" {
		t.Fatalf("expected terraform_plan dispatch, got %q", name)
	}
	if len(out) >= len(in) {
		t.Fatalf("dispatcher must shrink the output")
	}
}
