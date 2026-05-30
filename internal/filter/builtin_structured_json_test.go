package filter

import (
	"fmt"
	"strings"
	"testing"
)

func TestTryCompactTscDiagnostics(t *testing.T) {
	t.Parallel()
	input := strings.Repeat("progress\n", 40) + "src/app.ts(7,3): error TS2322: Type 'string' is not assignable to type 'number'.\nFound 1 error.\n"
	out, ok := TryCompactTscDiagnostics([]string{"pnpm", "exec", "tsc", "--noEmit"}, []byte(input))
	if !ok {
		t.Fatal("expected tsc diagnostic compaction")
	}
	got := string(out)
	if !strings.Contains(got, "[typescript] FAILED") || !strings.Contains(got, "TS2322") {
		t.Fatalf("missing tsc diagnostic: %q", got)
	}
}

func TestTryCompactKubectlJSONKeepsAttentionRows(t *testing.T) {
	t.Parallel()
	var items []string
	for i := 0; i < 35; i++ {
		items = append(items, fmt.Sprintf(`{"kind":"Pod","metadata":{"namespace":"default","name":"ok-%02d"},"status":{"phase":"Running","containerStatuses":[{"name":"app","ready":true,"restartCount":0}]}}`, i))
	}
	items = append(items, `{"kind":"Pod","metadata":{"namespace":"prod","name":"bad"},"status":{"phase":"Pending","containerStatuses":[{"name":"app","ready":false,"restartCount":7,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}`)
	input := `{"kind":"List","items":[` + strings.Join(items, ",") + `]}`
	out, ok := TryCompactKubectlJSON([]string{"kubectl", "get", "pods", "-o", "json"}, []byte(input))
	if !ok {
		t.Fatal("expected kubectl json compaction")
	}
	got := string(out)
	if !strings.Contains(got, "[kubectl -o json] 36 item(s)") ||
		!strings.Contains(got, "prod/bad") ||
		!strings.Contains(got, "CrashLoopBackOff") {
		t.Fatalf("attention row missing: %q", got)
	}
}

func TestTryCompactCargoMetadataJSON(t *testing.T) {
	t.Parallel()
	input := `{"packages":[` +
		`{"name":"app","version":"0.1.0","id":"path+file:///app#0.1.0"},` +
		`{"name":"lib","version":"0.2.0","id":"path+file:///lib#0.2.0"},` +
		`{"name":"serde","version":"1.0.0","id":"registry+serde#1.0.0"}` +
		`],"workspace_members":["path+file:///app#0.1.0","path+file:///lib#0.2.0"],` +
		`"resolve":{"nodes":[{"id":"path+file:///app#0.1.0","dependencies":["registry+serde#1.0.0"]}]}}`
	out, ok := TryCompactCargoMetadataJSON([]string{"cargo", "metadata", "--format-version", "1"}, []byte(input))
	if !ok {
		t.Fatal("expected cargo metadata compaction")
	}
	got := string(out)
	if !strings.Contains(got, "[cargo metadata] 3 package(s), 2 workspace member(s), 1 dependency edge(s)") ||
		!strings.Contains(got, "app 0.1.0") ||
		!strings.Contains(got, "lib 0.2.0") {
		t.Fatalf("bad cargo metadata summary: %q", got)
	}
}

func TestTryCompactTerraformShowJSONPlanAndState(t *testing.T) {
	t.Parallel()
	plan := `{"format_version":"1.2","resource_changes":[` +
		`{"address":"aws_s3_bucket.app","type":"aws_s3_bucket","name":"app","change":{"actions":["create"]}},` +
		`{"address":"aws_iam_role.old","type":"aws_iam_role","name":"old","change":{"actions":["delete"]}}` +
		`]}`
	out, ok := TryCompactTerraformShowJSON([]string{"terraform", "show", "-json", "plan.out"}, []byte(plan))
	if !ok {
		t.Fatal("expected terraform plan json compaction")
	}
	got := string(out)
	if !strings.Contains(got, "aws_s3_bucket.app actions=create") ||
		!strings.Contains(got, "aws_iam_role.old actions=delete") {
		t.Fatalf("bad terraform plan summary: %q", got)
	}

	state := `{"values":{"root_module":{"resources":[{"address":"aws_s3_bucket.app"}],"child_modules":[{"resources":[{"address":"module.db.aws_db_instance.main"}]}]}}}`
	out, ok = TryCompactTerraformShowJSON([]string{"tofu", "show", "--json"}, []byte(state))
	if !ok {
		t.Fatal("expected terraform state json compaction")
	}
	got = string(out)
	if !strings.Contains(got, "aws_s3_bucket.app") || !strings.Contains(got, "module.db.aws_db_instance.main") {
		t.Fatalf("bad terraform state summary: %q", got)
	}
}

func TestStructuredJSONParsersPassThrough(t *testing.T) {
	t.Parallel()
	if _, ok := TryCompactKubectlJSON([]string{"kubectl", "get", "pods"}, []byte(`{"items":[]}`)); ok {
		t.Fatal("kubectl without -o json should pass through")
	}
	if _, ok := TryCompactCargoMetadataJSON([]string{"cargo", "test"}, []byte(`{"packages":[]}`)); ok {
		t.Fatal("non-metadata cargo should pass through")
	}
	if _, ok := TryCompactTerraformShowJSON([]string{"terraform", "show"}, []byte(`{"values":{}}`)); ok {
		t.Fatal("terraform show without json should pass through")
	}
}
