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

// TestTryCompactTerraformPlan_passthroughUnknownSub leaves unsupported
// terraform subcommands alone.
func TestTryCompactTerraformPlan_passthroughUnknownSub(t *testing.T) {
	t.Parallel()
	in := []byte("lots of output\n")
	out, ok := TryCompactTerraformPlan([]string{"terraform", "init"}, in)
	if ok {
		t.Fatalf("terraform init must passthrough, got %s", out)
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
