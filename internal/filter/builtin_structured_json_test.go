package filter

import (
	"encoding/json"
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
	for i := range 35 {
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

func TestTryCompactKubectlJSONHealthyListPassesThrough(t *testing.T) {
	t.Parallel()
	var items []string
	for i := range 35 {
		items = append(items, fmt.Sprintf(`{"kind":"Pod","metadata":{"namespace":"default","name":"ok-%02d"},"status":{"phase":"Running","containerStatuses":[{"name":"app","ready":true,"restartCount":0}]}}`, i))
	}
	input := `{"kind":"List","items":[` + strings.Join(items, ",") + `]}`
	if _, ok := TryCompactKubectlJSON([]string{"kubectl", "get", "pods", "-o", "json"}, []byte(input)); ok {
		t.Fatal("healthy non-empty kubectl json lists must pass through")
	}
}

func TestTryCompactKubectlJSONKeepsLateAttentionRowsWithinCap(t *testing.T) {
	t.Parallel()
	var items []string
	for i := range 30 {
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
	for i := range 24 {
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
	for i := range 35 {
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
	for i := range 40 {
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
	for i := range 40 {
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

func TestTryCompactKnownCLIJSONExact_ExactMinify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		argv []string
		body []byte
		want string
	}{
		{
			name: "kubectl healthy json",
			argv: []string{"kubectl", "get", "pods", "-o", "json"},
			body: []byte("{\n  \"kind\": \"List\",\n  \"items\": []\n}\n"),
			want: `{"kind":"List","items":[]}`,
		},
		{
			name: "oc output json",
			argv: []string{"oc", "get", "routes", "--output=json"},
			body: []byte("{\n  \"kind\": \"RouteList\",\n  \"items\": []\n}\n"),
			want: `{"kind":"RouteList","items":[]}`,
		},
		{
			name: "terraform output json",
			argv: []string{"terraform", "output", "-json"},
			body: []byte("{\n  \"endpoint\": {\"value\": \"https://example.com\"}\n}\n"),
			want: `{"endpoint":{"value":"https://example.com"}}`,
		},
		{
			name: "tofu show json",
			argv: []string{"tofu", "show", "--json"},
			body: []byte("{\n  \"format_version\": \"1.0\",\n  \"values\": {}\n}\n"),
			want: `{"format_version":"1.0","values":{}}`,
		},
		{
			name: "cargo metadata fallback",
			argv: []string{"cargo", "metadata", "--format-version", "1"},
			body: []byte("{\n  \"packages\": []\n}\n"),
			want: `{"packages":[]}`,
		},
		{
			name: "docker inspect json",
			argv: []string{"docker", "container", "inspect", "web"},
			body: []byte("[\n  {\"Id\": \"abc\", \"State\": {\"Status\": \"running\"}}\n]\n"),
			want: `[{"Id":"abc","State":{"Status":"running"}}]`,
		},
		{
			name: "docker compose config json",
			argv: []string{"docker", "compose", "config", "--format", "json"},
			body: []byte("{\n  \"services\": {\"web\": {\"image\": \"nginx\"}}\n}\n"),
			want: `{"services":{"web":{"image":"nginx"}}}`,
		},
		{
			name: "go env json",
			argv: []string{"go", "env", "-json"},
			body: []byte("{\n  \"GOOS\": \"darwin\",\n  \"GOARCH\": \"arm64\"\n}\n"),
			want: `{"GOOS":"darwin","GOARCH":"arm64"}`,
		},
		{
			name: "npm view json",
			argv: []string{"npm", "view", "react", "--json=true"},
			body: []byte("{\n  \"name\": \"react\",\n  \"version\": \"19.0.0\"\n}\n"),
			want: `{"name":"react","version":"19.0.0"}`,
		},
		{
			name: "pnpm list json",
			argv: []string{"pnpm", "list", "--json"},
			body: []byte("[\n  {\"name\": \"app\", \"version\": \"1.0.0\"}\n]\n"),
			want: `[{"name":"app","version":"1.0.0"}]`,
		},
		{
			name: "yarn npm info json",
			argv: []string{"yarn", "npm", "info", "react", "--json"},
			body: []byte("{\n  \"name\": \"react\",\n  \"version\": \"19.0.0\"\n}\n"),
			want: `{"name":"react","version":"19.0.0"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactKnownCLIJSONExact(tc.argv, tc.body)
			if !ok {
				t.Fatal("known CLI JSON should enter exact fallback")
			}
			if got := string(out); got != tc.want {
				t.Fatalf("unexpected compact JSON: %q", got)
			}
		})
	}
}

func TestTryCompactKnownCLIJSONExact_LargeJSONNeverSchemaSummarized(t *testing.T) {
	t.Parallel()

	body := "{\n  \"items\": [\n    " +
		strings.Repeat("{\"id\":1,\"name\":\"same\",\"value\":\"abcdef\"},\n    ", 80) +
		"{\"id\":2,\"name\":\"last\",\"value\":\"uvwxyz\"}\n  ]\n}\n"
	out, ok := TryCompactKnownCLIJSONExact([]string{"kubectl", "get", "pods", "-o", "json"}, []byte(body))
	if !ok {
		t.Fatal("large known CLI JSON should be handled to block generic schema extraction")
	}
	got := string(out)
	if strings.Contains(got, "{object,") || strings.Contains(got, "[array,") {
		t.Fatalf("known CLI JSON must not be schema-summarized: %q", got[:min(len(got), 120)])
	}
	if !strings.Contains(got, `"last"`) || !strings.Contains(got, `"uvwxyz"`) {
		t.Fatalf("known CLI JSON lost scalar evidence: %q", got[:min(len(got), 120)])
	}
}

func TestTryCompactKnownCLIJSONExact_NonJSONFullPassAndUnrelated(t *testing.T) {
	t.Parallel()

	text := []byte("plain output\nplain output\n")
	out, ok := TryCompactKnownCLIJSONExact([]string{"kubectl", "get", "pods", "-o", "json"}, text)
	if !ok {
		t.Fatal("known CLI non-JSON should be handled to block later lossy reducers")
	}
	if string(out) != string(text) {
		t.Fatalf("known CLI non-JSON must full-pass, got %q", out)
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"kubectl", "get", "pods"}, []byte(`{"items":[]}`)); ok {
		t.Fatal("kubectl without JSON output flag must not match exact gate")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"terraform", "output"}, []byte(`{"x":1}`)); ok {
		t.Fatal("terraform output without JSON flag must not match exact gate")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"git", "status", "--json"}, []byte(`{"x":1}`)); ok {
		t.Fatal("unrelated JSON-looking command must not match exact gate")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"go", "test", "-json", "./..."}, []byte(`{"Action":"pass"}`)); ok {
		t.Fatal("go test JSONL stream must stay owned by test parsers, not known CLI exact gate")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"npm", "install", "--json"}, []byte(`{"added":1}`)); ok {
		t.Fatal("package installation JSON must stay owned by package parsers, not known CLI exact gate")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"npm", "audit", "--json"}, []byte(`{"metadata":{"vulnerabilities":{"total":0}}}`)); ok {
		t.Fatal("package audit JSON must stay owned by zero-vulnerability audit parser, not known CLI exact gate")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"npm", "view", "react", "--json=false"}, []byte(`{"name":"react"}`)); ok {
		t.Fatal("explicit false JSON flag must not match exact gate")
	}
}

func TestTryCompactKnownCLIJSONExact_WrapperAndVariantBranches(t *testing.T) {
	t.Parallel()

	body := []byte("{\n  \"ok\": true,\n  \"items\": [1, 2]\n}\n")
	want := `{"ok":true,"items":[1,2]}`
	cases := []struct {
		name string
		argv []string
	}{
		{name: "npx unwrap", argv: []string{"npx", "-y", "go", "env", "-json"}},
		{name: "pnpm exec unwrap", argv: []string{"pnpm", "exec", "docker", "inspect", "web"}},
		{name: "npm x unwrap", argv: []string{"npm", "x", "--", "go", "env", "-json"}},
		{name: "bun x unwrap", argv: []string{"bun", "x", "go", "env", "-json"}},
		{name: "bun run unwrap", argv: []string{"bun", "run", "docker", "compose", "config", "--format", "json"}},
		{name: "docker compose split format", argv: []string{"docker", "compose", "config", "--format", "json"}},
		{name: "docker-compose split format", argv: []string{"docker-compose", "config", "--format", "json"}},
		{name: "podman inspect", argv: []string{"podman", "inspect", "web"}},
		{name: "nerdctl object inspect", argv: []string{"nerdctl", "image", "inspect", "web"}},
		{name: "go list json", argv: []string{"go", "list", "-json", "./..."}},
		{name: "go mod edit json", argv: []string{"go", "mod", "edit", "-json"}},
		{name: "yarn info json", argv: []string{"yarn", "info", "react", "--json"}},
		{name: "bun pm list json", argv: []string{"bun", "pm", "list", "--json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, ok := TryCompactKnownCLIJSONExact(tc.argv, body)
			if !ok {
				t.Fatal("expected known CLI JSON exact match")
			}
			if got := string(out); got != want {
				t.Fatalf("unexpected compact JSON: %q", got)
			}
		})
	}

	if _, ok := TryCompactKnownCLIJSONExact([]string{"npx", "-y"}, body); ok {
		t.Fatal("npx without resolved command must not match")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"docker", "compose", "config"}, body); ok {
		t.Fatal("docker compose config without JSON format must not match")
	}
	if _, ok := TryCompactKnownCLIJSONExact([]string{"bun", "pm", "install", "--json"}, body); ok {
		t.Fatal("bun package install JSON must not match generic exact gate")
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

func TestFirstNonEmptyLocal(t *testing.T) {
	t.Parallel()
	if got := firstNonEmptyLocal(); got != "" {
		t.Fatalf("no args must return empty, got %q", got)
	}
	if got := firstNonEmptyLocal("", "  ", "\t"); got != "" {
		t.Fatalf("all whitespace must return empty, got %q", got)
	}
	if got := firstNonEmptyLocal("", "hello", "world"); got != "hello" {
		t.Fatalf("first non-empty must win, got %q", got)
	}
	if got := firstNonEmptyLocal("  trimmed  ", ""); got != "trimmed" {
		t.Fatalf("must trim whitespace, got %q", got)
	}
	if got := firstNonEmptyLocal("", "", "", "last"); got != "last" {
		t.Fatalf("last non-empty must win if earlier are empty, got %q", got)
	}
}

func TestRawString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"string_value", json.RawMessage(`"hello"`), "hello"},
		{"empty_string", json.RawMessage(`""`), ""},
		{"float_value", json.RawMessage(`42`), "42"},
		{"float_zero", json.RawMessage(`0`), "0"},
		{"float_large", json.RawMessage(`123456`), "123456"},
		{"bool_true_invalid_for_string", json.RawMessage(`true`), ""},
		{"null_invalid", json.RawMessage(`null`), ""},
		{"object_invalid", json.RawMessage(`{"a":1}`), ""},
		{"array_invalid", json.RawMessage(`[1,2,3]`), ""},
		{"empty_invalid", json.RawMessage(``), ""},
		{"garbage_invalid", json.RawMessage(`garbage`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := rawString(tc.raw); got != tc.want {
				t.Fatalf("rawString(%s) = %q, want %q", string(tc.raw), got, tc.want)
			}
		})
	}
}
