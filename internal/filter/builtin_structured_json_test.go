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

func TestTryCompactKubectlJSONKeepsLateAttentionRowsWithinCap(t *testing.T) {
	t.Parallel()
	var items []string
	for i := 0; i < 30; i++ {
		items = append(items, fmt.Sprintf(`{"kind":"Pod","metadata":{"namespace":"prod","name":"bad-%02d"},"status":{"phase":"Pending","containerStatuses":[{"name":"app","ready":false,"restartCount":%d,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}`, i, i+1))
	}
	input := `{"kind":"List","items":[` + strings.Join(items, ",") + `]}`
	out, ok := TryCompactKubectlJSON([]string{"kubectl", "get", "pods", "-o=json"}, []byte(input))
	if !ok {
		t.Fatal("expected kubectl json compaction")
	}
	got := string(out)
	if !strings.Contains(got, "prod/bad-29") {
		t.Fatalf("late attention row dropped: %q", got)
	}
	if strings.Contains(got, "prod/bad-20") {
		t.Fatalf("middle attention row should be capped before tail evidence: %q", got)
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

func TestTryCompactCargoMetadataJSONKeepsLateWorkspaceMembers(t *testing.T) {
	t.Parallel()
	var packages []string
	var members []string
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("path+file:///crate%02d#0.1.0", i)
		packages = append(packages, fmt.Sprintf(`{"name":"crate%02d","version":"0.1.0","id":"%s"}`, i, id))
		members = append(members, `"`+id+`"`)
	}
	input := `{"packages":[` + strings.Join(packages, ",") + `],"workspace_members":[` + strings.Join(members, ",") + `]}`
	out, ok := TryCompactCargoMetadataJSON([]string{"cargo", "metadata"}, []byte(input))
	if !ok {
		t.Fatal("expected cargo metadata compaction")
	}
	got := string(out)
	if !strings.Contains(got, "crate23 0.1.0") {
		t.Fatalf("late workspace member dropped: %q", got)
	}
	if strings.Contains(got, "crate18 0.1.0") {
		t.Fatalf("middle workspace member should be capped before tail evidence: %q", got)
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

func TestTryCompactTerraformShowJSONKeepsLateDestructiveChange(t *testing.T) {
	t.Parallel()
	var changes []string
	for i := 0; i < 35; i++ {
		changes = append(changes, fmt.Sprintf(`{"address":"data.null_data_source.ok_%02d","change":{"actions":["no-op"]}}`, i))
	}
	changes = append(changes, `{"address":"aws_db_instance.prod","change":{"actions":["delete","create"]}}`)
	plan := `{"format_version":"1.2","resource_changes":[` + strings.Join(changes, ",") + `]}`
	out, ok := TryCompactTerraformShowJSON([]string{"terraform", "show", "-json", "plan.out"}, []byte(plan))
	if !ok {
		t.Fatal("expected terraform plan json compaction")
	}
	got := string(out)
	if !strings.Contains(got, "aws_db_instance.prod actions=delete,create") {
		t.Fatalf("late destructive change dropped: %q", got)
	}
	if strings.Contains(got, "data.null_data_source.ok_34") {
		t.Fatalf("benign tail should not crowd out destructive change: %q", got)
	}
}

func TestTryCompactTerraformShowJSONKeepsLateSamePriorityChange(t *testing.T) {
	t.Parallel()
	var changes []string
	for i := 0; i < 40; i++ {
		changes = append(changes, fmt.Sprintf(`{"address":"aws_instance.replace_%02d","change":{"actions":["delete","create"]}}`, i))
	}
	plan := `{"format_version":"1.2","resource_changes":[` + strings.Join(changes, ",") + `]}`
	out, ok := TryCompactTerraformShowJSON([]string{"terraform", "show", "-json", "plan.out"}, []byte(plan))
	if !ok {
		t.Fatal("expected terraform plan json compaction")
	}
	got := string(out)
	if !strings.Contains(got, "aws_instance.replace_39 actions=delete,create") {
		t.Fatalf("late same-priority change dropped: %q", got)
	}
	if strings.Contains(got, "aws_instance.replace_33") {
		t.Fatalf("middle same-priority change should be capped before tail evidence: %q", got)
	}
}

func TestTryCompactTerraformShowJSONKeepsLateStateResource(t *testing.T) {
	t.Parallel()
	var resources []string
	for i := 0; i < 40; i++ {
		resources = append(resources, fmt.Sprintf(`{"address":"aws_instance.node_%02d"}`, i))
	}
	state := `{"values":{"root_module":{"resources":[` + strings.Join(resources, ",") + `]}}}`
	out, ok := TryCompactTerraformShowJSON([]string{"terraform", "show", "-json"}, []byte(state))
	if !ok {
		t.Fatal("expected terraform state json compaction")
	}
	got := string(out)
	if !strings.Contains(got, "aws_instance.node_39") {
		t.Fatalf("late state resource dropped: %q", got)
	}
	if strings.Contains(got, "aws_instance.node_33") {
		t.Fatalf("middle state resource should be capped before tail evidence: %q", got)
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
