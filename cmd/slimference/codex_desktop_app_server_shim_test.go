package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCodexDesktopAppServerShimExec(t *testing.T) {
	codexBin := writeFakeExecutable(t, "codex")
	env := []string{
		"PATH=/usr/bin",
		"CODEX_CLI_PATH=/tmp/slimference",
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
		"SLIMFERENCE_CODEX_DESKTOP_BASE_URL=http://127.0.0.1:8990/backend-api/codex/",
		"FOO=bar",
	}
	argv0, argv, childEnv, err := buildCodexDesktopAppServerShimExec([]string{"--analytics-default-enabled"}, env)
	if err != nil {
		t.Fatalf("build exec: %v", err)
	}
	if argv0 != codexBin {
		t.Fatalf("argv0=%q", argv0)
	}
	joinedArgs := strings.Join(argv, "\n")
	for _, want := range []string{
		codexBin,
		"app-server",
		"model_provider=\"slimference-codex\"",
		"model_providers.slimference-codex.base_url=\"http://127.0.0.1:8990/backend-api/codex\"",
		"model_providers.slimference-codex.requires_openai_auth=true",
		"model_providers.slimference-codex.supports_websockets=true",
		"model_providers.slimference-codex.wire_api=\"responses\"",
		"--analytics-default-enabled",
	} {
		if !strings.Contains(joinedArgs, want) {
			t.Fatalf("argv missing %q in %v", want, argv)
		}
	}
	// The dead top-level base-url overrides must not return: they never routed
	// the conversation and only created sideband loopback noise.
	for _, forbidden := range []string{"openai_base_url=", "chatgpt_base_url="} {
		if strings.Contains(joinedArgs, forbidden) {
			t.Fatalf("argv must not set %q anymore: %v", forbidden, argv)
		}
	}
	joinedEnv := strings.Join(childEnv, "\n")
	for _, forbidden := range []string{"CODEX_CLI_PATH=", "SLIMFERENCE_CODEX_DESKTOP_ACTIVE=", "SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=", "SLIMFERENCE_CODEX_DESKTOP_BASE_URL="} {
		if strings.Contains(joinedEnv, forbidden) {
			t.Fatalf("child env leaked %s in %v", forbidden, childEnv)
		}
	}
	if !strings.Contains(joinedEnv, "PATH=/usr/bin") || !strings.Contains(joinedEnv, "FOO=bar") {
		t.Fatalf("child env lost ordinary entries: %v", childEnv)
	}
}

func TestBuildCodexDesktopAppServerShimExecRejectsMissingScope(t *testing.T) {
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=/tmp/codex"}); err == nil {
		t.Fatal("expected inactive scope rejection")
	}
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1"}); err == nil {
		t.Fatal("expected missing upstream rejection")
	}
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=/tmp/codex",
	}); err == nil {
		t.Fatal("expected inaccessible upstream rejection before base-url")
	}
	codexBin := writeFakeExecutable(t, "codex")
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
	}); err == nil {
		t.Fatal("expected missing base-url rejection")
	}
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
		"SLIMFERENCE_CODEX_DESKTOP_BASE_URL=https://chatgpt.com/backend-api/codex",
	}); err == nil {
		t.Fatal("expected non-local base-url rejection")
	}
	if _, _, _, err := buildCodexDesktopAppServerShimExec(nil, []string{
		"SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1",
		"SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=" + codexBin,
		"SLIMFERENCE_CODEX_DESKTOP_BASE_URL=http://127.0.0.1:8990/backend-api/codex?x=1",
	}); err == nil {
		t.Fatal("expected query rejection")
	}
}

func TestRewriteCodexDesktopThreadStart(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		wantChanged  bool
		wantContains []string
	}{
		{"null provider rewritten", `{"id":"1","method":"thread/start","params":{"model":"gpt-5.5","modelProvider":null,"cwd":"/x"}}`, true, []string{`"modelProvider":"slimference-codex"`, `"model":"gpt-5.5"`, `"cwd":"/x"`}},
		{"absent provider rewritten", `{"id":"1","method":"thread/start","params":{"model":"gpt-5.5"}}`, true, []string{`"modelProvider":"slimference-codex"`, `"model":"gpt-5.5"`}},
		{"real service tier preserved", `{"id":"1","method":"thread/start","params":{"model":"gpt-5.5","modelProvider":null,"serviceTier":"priority"}}`, true, []string{`"modelProvider":"slimference-codex"`, `"serviceTier":"priority"`}},
		{"explicit provider respected", `{"id":"1","method":"thread/start","params":{"modelProvider":"openai"}}`, false, nil},
		{"realtime thread skipped", `{"id":"1","method":"thread/start","params":{"modelProvider":null,"config":{"features.realtime_conversation":true}}}`, false, nil},
		{"non-realtime config rewritten", `{"id":"1","method":"thread/start","params":{"modelProvider":null,"config":{"features.realtime_conversation":false}}}`, true, []string{`"modelProvider":"slimference-codex"`}},
		{"unparseable config skipped", `{"id":"1","method":"thread/start","params":{"modelProvider":null,"config":"oops"}}`, false, nil},
		{"turn/start untouched", `{"id":"1","method":"turn/start","params":{"threadId":"t"}}`, false, nil},
		{"thread/resume untouched in first cut", `{"id":"1","method":"thread/resume","params":{"threadId":"t","modelProvider":null}}`, false, nil},
		{"initialize untouched", `{"id":"1","method":"initialize","params":{}}`, false, nil},
		{"thread/start no params untouched", `{"id":"1","method":"thread/start"}`, false, nil},
		{"notification untouched", `{"method":"thread/started","params":{}}`, false, nil},
		{"invalid json untouched", `{not json`, false, nil},
		{"non-object untouched", `[1,2,3]`, false, nil},
		{"empty untouched", ``, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, changed := rewriteCodexDesktopThreadStart([]byte(tt.in))
			if changed != tt.wantChanged {
				t.Fatalf("changed=%v want %v (out=%s)", changed, tt.wantChanged, out)
			}
			if !tt.wantChanged {
				if string(out) != tt.in {
					t.Fatalf("unchanged output must be byte-identical: got %q want %q", out, tt.in)
				}
				return
			}
			var probe map[string]json.RawMessage
			if err := json.Unmarshal(out, &probe); err != nil {
				t.Fatalf("rewritten output is not valid JSON: %v (%s)", err, out)
			}
			for _, w := range tt.wantContains {
				if !strings.Contains(string(out), w) {
					t.Fatalf("rewritten output missing %q: %s", w, out)
				}
			}
		})
	}
}

func TestMaybeRewriteCodexDesktopThreadStartPreservesFraming(t *testing.T) {
	withNL := []byte(`{"id":"1","method":"thread/start","params":{"modelProvider":null}}` + "\n")
	out := maybeRewriteCodexDesktopThreadStart(withNL)
	if len(out) == 0 || out[len(out)-1] != '\n' {
		t.Fatalf("trailing newline not preserved: %q", out)
	}
	if !strings.Contains(string(out), "slimference-codex") {
		t.Fatalf("thread/start not rewritten: %s", out)
	}
	noNL := []byte(`{"id":"1","method":"thread/start","params":{"modelProvider":null}}`)
	out2 := maybeRewriteCodexDesktopThreadStart(noNL)
	if len(out2) > 0 && out2[len(out2)-1] == '\n' {
		t.Fatalf("must not add a newline when input had none: %q", out2)
	}
	passthrough := []byte(`{"id":"2","method":"turn/start","params":{}}` + "\n")
	if !bytes.Equal(maybeRewriteCodexDesktopThreadStart(passthrough), passthrough) {
		t.Fatal("non-matching line must be byte-identical")
	}
}

func TestMediateCodexDesktopAppServerStdin(t *testing.T) {
	in := `{"id":"1","method":"initialize","params":{}}` + "\n" +
		`{"id":"2","method":"thread/start","params":{"model":"gpt-5.5","modelProvider":null}}` + "\n" +
		`{"id":"3","method":"turn/start","params":{"threadId":"t"}}` + "\n"
	var out bytes.Buffer
	mediateCodexDesktopAppServerStdin(strings.NewReader(in), &out)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out.String())
	}
	if strings.Contains(lines[0], "slimference") || strings.Contains(lines[2], "slimference") {
		t.Fatalf("only thread/start may change: %v", lines)
	}
	if !strings.Contains(lines[1], `"modelProvider":"slimference-codex"`) || !strings.Contains(lines[1], `"model":"gpt-5.5"`) {
		t.Fatalf("thread/start not rewritten correctly: %s", lines[1])
	}
}

func TestCodexDesktopAppServerStdoutConfigReadBadgeProvider(t *testing.T) {
	mediator := newCodexDesktopAppServerMediator(codexDesktopProviderConfig{
		baseURL: "http://127.0.0.1:8990/backend-api/codex",
	})

	response := `{"id":"cfg1","result":{"config":{"model":"gpt-5.5","model_provider":"openai","mcp_servers":{"filesystem":{"command":"node","args":["server.js"]}},"mcpServers":{"github":{"command":"uvx"}}},"origins":{}}}` + "\n"
	var stdoutOut bytes.Buffer
	mediator.mediateStdout(strings.NewReader(response), &stdoutOut)
	got := strings.TrimSpace(stdoutOut.String())
	if got == strings.TrimSpace(response) {
		t.Fatalf("config/read response was not rewritten: %s", got)
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got), &msg); err != nil {
		t.Fatalf("rewritten response is invalid JSON: %v (%s)", err, got)
	}
	var result struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(msg["result"], &result); err != nil {
		t.Fatalf("result is invalid JSON: %v", err)
	}
	var provider string
	if err := json.Unmarshal(result.Config["model_provider"], &provider); err != nil {
		t.Fatalf("model_provider missing/invalid: %v", err)
	}
	if provider != codexSlimferenceProviderID {
		t.Fatalf("model_provider=%q", provider)
	}
	if err := json.Unmarshal(result.Config["modelProvider"], &provider); err != nil {
		t.Fatalf("modelProvider missing/invalid: %v", err)
	}
	if provider != codexSlimferenceProviderID {
		t.Fatalf("modelProvider=%q", provider)
	}
	var providers map[string]struct {
		ID                      string `json:"id"`
		Name                    string `json:"name"`
		DisplayName             string `json:"displayName"`
		Label                   string `json:"label"`
		BaseURL                 string `json:"base_url"`
		BaseURLCamel            string `json:"baseUrl"`
		BaseURLInitialism       string `json:"baseURL"`
		RequiresOpenAIAuth      bool   `json:"requires_openai_auth"`
		RequiresOpenAIAuthCamel bool   `json:"requiresOpenAIAuth"`
		RequiresOpenAIAuthAlt   bool   `json:"requiresOpenAiAuth"`
		SupportsWebSockets      bool   `json:"supports_websockets"`
		SupportsWebSocketsCamel bool   `json:"supportsWebSockets"`
		SupportsWebsocketsAlt   bool   `json:"supportsWebsockets"`
		WireAPI                 string `json:"wire_api"`
		WireAPICamel            string `json:"wireApi"`
		WireAPIInitialism       string `json:"wireAPI"`
	}
	if err := json.Unmarshal(result.Config["model_providers"], &providers); err != nil {
		t.Fatalf("model_providers missing/invalid: %v", err)
	}
	entry := providers[codexSlimferenceProviderID]
	if entry.ID != codexSlimferenceProviderID || entry.Name != "Slimference" || entry.DisplayName != "Slimference" ||
		entry.Label != "Slimference" || entry.BaseURL != "http://127.0.0.1:8990/backend-api/codex" ||
		entry.BaseURLCamel != "http://127.0.0.1:8990/backend-api/codex" ||
		entry.BaseURLInitialism != "http://127.0.0.1:8990/backend-api/codex" ||
		!entry.RequiresOpenAIAuth || !entry.RequiresOpenAIAuthCamel || !entry.RequiresOpenAIAuthAlt ||
		!entry.SupportsWebSockets || !entry.SupportsWebSocketsCamel || !entry.SupportsWebsocketsAlt ||
		entry.WireAPI != "responses" || entry.WireAPICamel != "responses" || entry.WireAPIInitialism != "responses" {
		t.Fatalf("bad provider entry: %+v", entry)
	}
	var camelProviders map[string]struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(result.Config["modelProviders"], &camelProviders); err != nil {
		t.Fatalf("modelProviders missing/invalid: %v", err)
	}
	if camelProviders[codexSlimferenceProviderID].Name != "Slimference" {
		t.Fatalf("bad camel provider entry: %+v", camelProviders[codexSlimferenceProviderID])
	}
	var mcpServers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(result.Config["mcp_servers"], &mcpServers); err != nil {
		t.Fatalf("mcp_servers must be preserved: %v", err)
	}
	if mcpServers["filesystem"].Command != "node" || len(mcpServers["filesystem"].Args) != 1 || mcpServers["filesystem"].Args[0] != "server.js" {
		t.Fatalf("bad mcp_servers preservation: %+v", mcpServers)
	}
	var camelMCPServers map[string]struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(result.Config["mcpServers"], &camelMCPServers); err != nil {
		t.Fatalf("mcpServers must be preserved: %v", err)
	}
	if camelMCPServers["github"].Command != "uvx" {
		t.Fatalf("bad mcpServers preservation: %+v", camelMCPServers)
	}
}

func TestCodexDesktopAppServerStdoutPassesNonConfigResponsesByteIdentical(t *testing.T) {
	mediator := newCodexDesktopAppServerMediator(codexDesktopProviderConfig{
		baseURL: "http://127.0.0.1:8990/backend-api/codex",
	})
	in := `{"id":"cfg1","result":{"origins":{}}}` + "\n" +
		`{"method":"account/updated","params":{}}` + "\n" +
		`not-json` + "\n"
	var out bytes.Buffer
	mediator.mediateStdout(strings.NewReader(in), &out)
	if out.String() != in {
		t.Fatalf("unknown stdout frames must pass byte-identical:\ngot  %q\nwant %q", out.String(), in)
	}
}

func TestCodexDesktopAppServerStdoutModelListPassesByteIdentical(t *testing.T) {
	mediator := newCodexDesktopAppServerMediator(codexDesktopProviderConfig{})
	request := `{"id":"m1","method":"model/list","params":{"limit":100}}` + "\n"
	response := `{"id":"m1","result":{"data":[{"model":"gpt-5.5","displayName":"5.5","isDefault":true,"serviceTiers":[{"id":"priority","name":"Fast"}],"defaultServiceTier":null},{"model":"gpt-5-mini"}]}}` + "\n"

	var stdinOut bytes.Buffer
	mediator.mediateStdin(strings.NewReader(request), &stdinOut)
	if stdinOut.String() != request {
		t.Fatalf("model/list request must pass through unchanged: %s", stdinOut.String())
	}

	var stdoutOut bytes.Buffer
	mediator.mediateStdout(strings.NewReader(response), &stdoutOut)
	if stdoutOut.String() != response {
		t.Fatalf("model/list response must pass through byte-identical:\ngot  %q\nwant %q", stdoutOut.String(), response)
	}
}

func TestCodexDesktopModelListShapedResponseWithoutRequestMethodPassesByteIdentical(t *testing.T) {
	mediator := newCodexDesktopAppServerMediator(codexDesktopProviderConfig{})
	response := []byte(`{"id":"m1","result":{"data":[{"model":"gpt-5.5","displayName":"5.5"}]}}` + "\n")
	rewritten, method, kind := mediator.maybeRewriteResponseLine(response)
	if kind != "" || method != "" || !bytes.Equal(rewritten, response) {
		t.Fatalf("model/list-shaped response without request method must pass through: method=%q kind=%q out=%s", method, kind, rewritten)
	}
}

func TestRewriteCodexDesktopConfigReadResponseRequiresConfigShape(t *testing.T) {
	tests := []string{
		`{"id":"cfg1","error":{"message":"nope"}}`,
		`{"id":"cfg1","result":{"origins":{}}}`,
		`{"id":"cfg1","result":{"config":"oops","origins":{}}}`,
		`{"method":"account/updated","params":{}}`,
		`not-json`,
	}
	for _, input := range tests {
		out, changed := rewriteCodexDesktopConfigReadResponse([]byte(input), codexDesktopProviderConfig{baseURL: "http://127.0.0.1:8990/backend-api/codex"})
		if changed || string(out) != input {
			t.Fatalf("input should pass through unchanged: changed=%v out=%q input=%q", changed, out, input)
		}
	}
}

func TestCodexDesktopProviderConfigFromArgv(t *testing.T) {
	argv := []string{
		"codex",
		"app-server",
		"-c",
		`model_providers.slimference-codex.base_url="http://127.0.0.1:8990/backend-api/codex"`,
	}
	got := codexDesktopProviderConfigFromArgv(argv)
	if got.baseURL != "http://127.0.0.1:8990/backend-api/codex" {
		t.Fatalf("baseURL=%q", got.baseURL)
	}
}

func TestRunCodexDesktopAppServerMediatedRewritesChildStdin(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "child-stdin.jsonl")
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat > \""+outFile+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdin := strings.NewReader(
		`{"id":"1","method":"thread/start","params":{"model":"gpt-5.5","modelProvider":null,"cwd":"/x"}}` + "\n" +
			`{"id":"2","method":"turn/start","params":{"threadId":"t"}}` + "\n")
	var out, errBuf bytes.Buffer
	rc := runCodexDesktopAppServerMediated(bin, []string{bin, "app-server"}, os.Environ(), stdin, installPrinter{Out: &out, Err: &errBuf})
	if rc != 0 {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"modelProvider":"slimference-codex"`) || !strings.Contains(string(got), `"model":"gpt-5.5"`) {
		t.Fatalf("child stdin missing rewritten thread/start: %s", got)
	}
	if !strings.Contains(string(got), `"method":"turn/start"`) {
		t.Fatalf("child stdin missing passthrough turn/start: %s", got)
	}
	if strings.Count(string(got), "slimference-codex") != 1 {
		t.Fatalf("only thread/start should carry the provider: %s", got)
	}
}

func TestRunCodexDesktopAppServerMediatedPropagatesExitCode(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	rc := runCodexDesktopAppServerMediated(bin, []string{bin, "app-server"}, os.Environ(), strings.NewReader(""), installPrinter{Out: &out, Err: &errBuf})
	if rc != 7 {
		t.Fatalf("rc=%d want 7 (err=%q)", rc, errBuf.String())
	}
}

func TestRunCodexDesktopAppServerMediatedStartFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	var out, errBuf bytes.Buffer
	rc := runCodexDesktopAppServerMediated(missing, []string{missing, "app-server"}, os.Environ(), strings.NewReader(""), installPrinter{Out: &out, Err: &errBuf})
	if rc != 1 || !strings.Contains(errBuf.String(), "start") {
		t.Fatalf("rc=%d err=%q", rc, errBuf.String())
	}
}

func writeFakeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
