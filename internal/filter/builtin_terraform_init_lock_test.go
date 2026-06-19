package filter

import (
	"strings"
	"testing"
)

func TestTryCompactTerraformInitKeepsLockFileFact(t *testing.T) {
	t.Parallel()
	in := []byte(`Initializing the backend...

Initializing provider plugins...
- Finding hashicorp/aws versions matching "~> 5.0"...
- Installing hashicorp/aws v5.31.0...
- Installed hashicorp/aws v5.31.0 (signed by HashiCorp)

Terraform has created a lock file .terraform.lock.hcl to record the provider
selections it made above. Include this file in your version control repository.

Terraform has been successfully initialized!
`)
	out, ok := TryCompactTerraformInit([]string{"terraform", "init"}, in)
	if !ok {
		t.Fatal("expected init compaction")
	}
	text := string(out)
	if !strings.Contains(text, "- lock file created: .terraform.lock.hcl") {
		t.Fatalf("lock-file-created fact was not preserved: %q", text)
	}
	if strings.Contains(text, "hashicorp/aws v5.31.0") {
		t.Fatalf("provider chatter leaked: %q", text)
	}
	if len(out) >= len(in) {
		t.Fatalf("output must be strictly shorter: in=%d out=%d", len(in), len(out))
	}
}
