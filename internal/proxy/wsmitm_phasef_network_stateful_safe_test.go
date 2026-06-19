package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSSafeExactNetworkResponseBoundary(t *testing.T) {
	prettyJSON := "{\n  \"status\": \"ok\",\n  \"count\": 42\n}\n"
	if !wssSafeStatefulStatusCommandOutput("curl https://api.example.com/data", prettyJSON) {
		t.Fatal("exact curl JSON whitespace minify should be stateful-safe")
	}
	if !wssSafeStatefulStatusCommandOutput("http GET https://api.example.com/data", prettyJSON) {
		t.Fatal("exact HTTPie JSON whitespace minify should be stateful-safe")
	}
	if !wssSafeStatefulStatusCommandOutput("gh api /repos/acme/project", prettyJSON) {
		t.Fatal("exact gh api JSON whitespace minify should be stateful-safe")
	}
	if !wssSafeStatefulStatusCommandOutput("gh pr list --json number,title", "[\n  {\"number\": 1, \"title\": \"first\"}\n]\n") {
		t.Fatal("exact gh --json whitespace minify should be stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("curl https://api.example.com/logs", "INFO boot\nINFO ready\n") {
		t.Fatal("curl non-JSON logs must not become stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("https api.example.com/logs", "INFO boot\nINFO ready\n") {
		t.Fatal("HTTPie non-JSON logs must not become stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("gh api /repos/acme/project", "plain response\nplain response\n") {
		t.Fatal("gh api non-JSON logs must not become stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("gh pr list --json number,title", "plain response\nplain response\n") {
		t.Fatal("gh --json non-JSON output must not become stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("gh pr list", prettyJSON) {
		t.Fatal("gh JSON without API or JSON flag must not enter the VCS exact-minify gate")
	}
	if !wssSafeStatefulStatusCommandOutput("kubectl get pods -o json", prettyJSON) {
		t.Fatal("explicit kubectl JSON output should be exact-minify stateful-safe")
	}
	if !wssSafeStatefulStatusCommandOutput("terraform output -json", prettyJSON) {
		t.Fatal("explicit terraform output JSON should be exact-minify stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("kubectl get pods -o json", "plain response\nplain response\n") {
		t.Fatal("known CLI JSON non-JSON output must not become stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("kubectl get pods", prettyJSON) {
		t.Fatal("kubectl JSON without explicit JSON flag must not enter the known CLI exact-minify gate")
	}
	kubectlAttentionJSON := "{\n  \"kind\": \"List\",\n  \"items\": [\n    {\"kind\": \"Pod\", \"metadata\": {\"namespace\": \"prod\", \"name\": \"bad\"}, \"status\": {\"phase\": \"Pending\", \"containerStatuses\": [{\"name\": \"app\", \"ready\": false, \"restartCount\": 7, \"state\": {\"waiting\": {\"reason\": \"CrashLoopBackOff\"}}}]}}\n  ]\n}\n"
	if !wssSafeStatefulStatusCommandOutput("kubectl get pods -o json", kubectlAttentionJSON) {
		t.Fatal("structured kubectl JSON should be exact-minify stateful-safe")
	}
	cargoMetadataJSON := "{\n  \"packages\": [\n    {\"name\": \"app\", \"version\": \"0.1.0\", \"id\": \"path+file:///app#0.1.0\"}\n  ],\n  \"workspace_members\": [\n    \"path+file:///app#0.1.0\"\n  ]\n}\n"
	if !wssSafeStatefulStatusCommandOutput("cargo metadata --format-version 1", cargoMetadataJSON) {
		t.Fatal("structured cargo metadata JSON should be exact-minify stateful-safe")
	}
	awsJSON := "{\n  \"UserId\": \"x\",\n  \"ResponseMetadata\": {\n    \"RequestId\": \"rid\",\n    \"HTTPStatusCode\": 200\n  }\n}\n"
	if !wssSafeStatefulStatusCommandOutput("aws sts get-caller-identity", awsJSON) {
		t.Fatal("AWS JSON should be exact-minify stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("aws sts get-caller-identity", "plain response\nplain response\n") {
		t.Fatal("AWS non-JSON output must not become stateful-safe")
	}
}

func TestWSSStatefulSafeNetworkJSONExactMinifyCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: network-json-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssNetworkJSONFixture(90)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-network-json", "call_network_json", "curl https://api.example.com/data", envelope, "stateful-network-json-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle network JSON request: %v", err)
	}
	if !replace {
		t.Fatal("full-history exact network JSON output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, `\"final\":\"kept\"`) ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "{object,") ||
		strings.Contains(body, `\"items\": [`) {
		t.Fatalf("network JSON output was not exact-minified and archive-backed: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe network JSON should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeVCSHostJSONExactMinifyCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: api-json-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssNetworkJSONFixture(90)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-api-json", "call_api_json", "gh api /repos/acme/project/releases", envelope, "stateful-api-json-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle API JSON request: %v", err)
	}
	if !replace {
		t.Fatal("full-history exact API JSON output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, `\"final\":\"kept\"`) ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "{object,") ||
		strings.Contains(body, `\"items\": [`) {
		t.Fatalf("API JSON output was not exact-minified and archive-backed: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe API JSON should save without structured guard: %+v", summary)
	}

	flagEnv := parseWSJSON(t, wssCommandOutputRequestBody("resp-vcs-json", "call_vcs_json", "gh pr list --json number,title", envelope, "stateful-vcs-json-session"))
	replace, err = adapter.handle(context.Background(), wsmitm.DirClientToServer, &flagEnv)
	if err != nil {
		t.Fatalf("handle VCS host JSON request: %v", err)
	}
	if !replace {
		t.Fatal("full-history exact VCS host JSON output should compact")
	}
	flagBody := string(flagEnv.Body)
	if !strings.Contains(flagBody, `\"final\":\"kept\"`) ||
		!strings.Contains(flagBody, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(flagBody, "{object,") ||
		strings.Contains(flagBody, `\"items\": [`) {
		t.Fatalf("VCS host JSON output was not exact-minified and archive-backed: %s", flagBody)
	}
	flagSummary := p.DebugRecorder().Last(1, false)[0]
	if flagSummary.Tokens.Saved <= 0 || flagSummary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		flagSummary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe VCS host JSON should save without structured guard: %+v", flagSummary)
	}
}

func TestWSSStatefulSafeKnownCLIJSONExactMinifyCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: known-cli-json-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssNetworkJSONFixture(90)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-known-cli-json", "call_known_cli_json", "kubectl get pods -o json", envelope, "stateful-known-cli-json-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle known CLI JSON request: %v", err)
	}
	if !replace {
		t.Fatal("full-history exact known CLI JSON output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, `\"final\":\"kept\"`) ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "{object,") ||
		strings.Contains(body, `\"items\": [`) {
		t.Fatalf("known CLI JSON output was not exact-minified and archive-backed: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe known CLI JSON should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeAWSJSONExactMinifyPreservesMetadata(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: aws-json-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssAWSJSONFixture(90)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-aws-json", "call_aws_json", "aws ec2 describe-instances --output json", envelope, "stateful-aws-json-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle AWS JSON request: %v", err)
	}
	if !replace {
		t.Fatal("full-history exact AWS JSON output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, `\"ResponseMetadata\"`) ||
		!strings.Contains(body, `\"RequestId\":\"rid-089\"`) ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "{object,") ||
		strings.Contains(body, `\"ResponseMetadata\": {`) {
		t.Fatalf("AWS JSON output was not exact-minified with metadata preserved and archive-backed: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe AWS JSON should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulSafeStructuredKnownCLIJSONUsesExactMinify(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()

	cases := []struct {
		name       string
		command    string
		body       string
		mustKeep   string
		mustReject string
	}{
		{
			name:       "kubectl attention json",
			command:    "kubectl get pods -o json",
			body:       wssPrettyKubectlAttentionJSONFixture(240),
			mustKeep:   `\"CrashLoopBackOff\"`,
			mustReject: "[kubectl -o json]",
		},
		{
			name:       "cargo metadata",
			command:    "cargo metadata --format-version 1",
			body:       wssPrettyCargoMetadataJSONFixture(240),
			mustKeep:   `\"workspace_members\"`,
			mustReject: "[cargo metadata]",
		},
		{
			name:       "terraform show json",
			command:    "terraform show -json plan.out",
			body:       wssPrettyTerraformShowJSONFixture(240),
			mustKeep:   `\"resource_changes\"`,
			mustReject: "[terraform show -json]",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !wssSafeStatefulStatusCommandOutput(tc.command, tc.body) {
				t.Fatalf("%s should be classified as WSS stateful-safe exact JSON", tc.name)
			}
			envelope := "Chunk ID: structured-known-cli-json-safe\n" +
				"Wall time: 0.0010 seconds\n" +
				"Process exited with code 0\n" +
				"Original token count: 10000\n" +
				"Output:\n" +
				tc.body
			caseID := strings.ReplaceAll(tc.name, " ", "_")
			env := parseWSJSON(t, wssCommandOutputRequestBody("resp_"+caseID, "call_"+caseID, tc.command, envelope, "stateful-structured-json-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle %s request: %v", tc.name, err)
			}
			if !replace {
				t.Fatalf("%s should exact-minify and compact", tc.name)
			}
			raw := string(env.Body)
			summary := p.DebugRecorder().Last(1, false)[0]
			if !strings.Contains(raw, tc.mustKeep) ||
				!strings.Contains(raw, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(raw, tc.mustReject) ||
				strings.Contains(raw, `\"items\": [`) ||
				strings.Contains(raw, "{object,") {
				t.Fatalf("%s was not exact-minified archive-backed JSON: summary=%+v raw=%s", tc.name, summary, raw)
			}
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("%s should save without structured guard: %+v", tc.name, summary)
			}
		})
	}
}

func wssNetworkJSONFixture(count int) string {
	var out strings.Builder
	out.WriteString("{\n  \"items\": [\n")
	for i := 0; i < count; i++ {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "    {\"id\": %d, \"name\": \"item-%03d\", \"value\": \"payload-%03d\"}", i, i, i)
	}
	out.WriteString("\n  ],\n  \"final\": \"kept\"\n}\n")
	return out.String()
}

func wssAWSJSONFixture(count int) string {
	var out strings.Builder
	out.WriteString("{\n  \"Reservations\": [\n")
	for i := 0; i < count; i++ {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "    {\"ReservationId\": \"r-%03d\", \"Instances\": [{\"InstanceId\": \"i-%03d\", \"State\": {\"Name\": \"running\"}}], \"ResponseMetadata\": {\"RequestId\": \"rid-%03d\", \"HTTPStatusCode\": 200}}", i, i, i)
	}
	out.WriteString("\n  ],\n  \"ResponseMetadata\": {\n    \"RequestId\": \"rid-top\",\n    \"HTTPStatusCode\": 200\n  }\n}\n")
	return out.String()
}

func wssPrettyKubectlAttentionJSONFixture(count int) string {
	var out strings.Builder
	out.WriteString("{\n  \"kind\": \"List\",\n  \"items\": [\n")
	for i := 0; i < count; i++ {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "    {\"kind\": \"Pod\", \"metadata\": {\"namespace\": \"prod\", \"name\": \"pod-%03d\"}, \"status\": {\"phase\": \"Pending\", \"containerStatuses\": [{\"name\": \"app\", \"ready\": false, \"restartCount\": %d, \"state\": {\"waiting\": {\"reason\": \"CrashLoopBackOff\"}}}]}}", i, i+1)
	}
	out.WriteString("\n  ]\n}\n")
	return out.String()
}

func wssPrettyCargoMetadataJSONFixture(count int) string {
	var out strings.Builder
	out.WriteString("{\n  \"packages\": [\n")
	for i := 0; i < count; i++ {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "    {\"name\": \"crate%03d\", \"version\": \"0.1.%d\", \"id\": \"path+file:///repo/crate%03d#0.1.%d\"}", i, i, i, i)
	}
	out.WriteString("\n  ],\n  \"workspace_members\": [\n")
	for i := 0; i < count; i++ {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "    \"path+file:///repo/crate%03d#0.1.%d\"", i, i)
	}
	out.WriteString("\n  ]\n}\n")
	return out.String()
}

func wssPrettyTerraformShowJSONFixture(count int) string {
	var out strings.Builder
	out.WriteString("{\n  \"format_version\": \"1.0\",\n  \"resource_changes\": [\n")
	for i := 0; i < count; i++ {
		if i > 0 {
			out.WriteString(",\n")
		}
		fmt.Fprintf(&out, "    {\"address\": \"aws_instance.node_%03d\", \"change\": {\"actions\": [\"update\"], \"before\": {\"id\": \"i-%03d\"}, \"after\": {\"id\": \"i-%03d\"}}}", i, i, i)
	}
	out.WriteString("\n  ]\n}\n")
	return out.String()
}
