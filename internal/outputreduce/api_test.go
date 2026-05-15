package outputreduce

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestInjectBody_AnthropicStringSystem(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"claude","system":"base","messages":[{"role":"user","content":"hi"}]}`)
	out, stats, err := InjectBody(types.Anthropic, body, Options{Enabled: true, Profile: "anthropic", SignatureMarker: DefaultMarker})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Applied || stats.Profile != "anthropic" || stats.AddedTokens == 0 {
		t.Fatalf("stats: %+v", stats)
	}
	var root struct {
		System string `json:"system"`
	}
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(root.System, "base") || !strings.Contains(root.System, DefaultMarker) {
		t.Fatalf("system not injected: %q", root.System)
	}
}

func TestInjectBody_AnthropicArraySystem(t *testing.T) {
	t.Parallel()
	body := []byte(`{"system":[{"type":"text","text":"base"}],"messages":[{"role":"user","content":"hi"}]}`)
	out, stats, err := InjectBody(types.Anthropic, body, Options{Enabled: true, Profile: "auto", SignatureMarker: DefaultMarker})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Applied {
		t.Fatalf("stats: %+v", stats)
	}
	var root struct {
		System []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
	}
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.System) != 2 || !strings.Contains(root.System[1].Text, DefaultMarker) {
		t.Fatalf("system blocks: %#v", root.System)
	}
}

func TestInjectBody_OpenAIPrependsSystem(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"gpt","messages":[{"role":"user","content":"hi"}]}`)
	out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai", SignatureMarker: DefaultMarker})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Applied {
		t.Fatalf("stats: %+v", stats)
	}
	var root struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Messages) != 2 || root.Messages[0].Role != "system" || !strings.Contains(root.Messages[0].Content, DefaultMarker) {
		t.Fatalf("messages: %#v", root.Messages)
	}
}

func TestInjectBody_OpenAIAppendsExistingSystem(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"system","content":"base"},{"role":"user","content":"hi"}]}`)
	out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai", SignatureMarker: DefaultMarker})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Applied {
		t.Fatalf("stats: %+v", stats)
	}
	var root struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Messages) != 2 || !strings.Contains(root.Messages[0].Content, "base") || !strings.Contains(root.Messages[0].Content, DefaultMarker) {
		t.Fatalf("messages: %#v", root.Messages)
	}
}

func TestInjectBody_CodexInputString(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"codex","input":"inspect repo","stream":false}`)
	out, stats, err := InjectBody(types.CodexChatGPT, body, Options{Enabled: true, Profile: "codex", SignatureMarker: DefaultMarker})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Applied {
		t.Fatalf("stats: %+v", stats)
	}
	var root struct {
		Input []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Input) != 2 ||
		root.Input[0].Type != "message" ||
		root.Input[0].Role != "system" ||
		root.Input[0].Content[0].Type != "input_text" ||
		root.Input[1].Content[0].Text != "inspect repo" {
		t.Fatalf("input: %#v", root.Input)
	}
}

func TestInjectBody_IdempotentMarker(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"system","content":"` + DefaultMarker + `"},{"role":"user","content":"hi"}]}`)
	out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai", SignatureMarker: DefaultMarker})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Applied || stats.Reason != "already_present" || string(out) != string(body) {
		t.Fatalf("out=%s stats=%+v", out, stats)
	}
}

func TestInjectBody_CustomDirectivePathAddsMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "directive.txt")
	if err := os.WriteFile(path, []byte("custom terse rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai", CustomDirectivePath: path, SignatureMarker: DefaultMarker})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Applied || !strings.Contains(string(out), "custom terse rule") || !strings.Contains(string(out), DefaultMarker) {
		t.Fatalf("out=%s stats=%+v", out, stats)
	}
}

func TestInjectBody_SkipsOverCapAndNoop(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	if out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai", SignatureMarker: DefaultMarker, MaxAddedBytes: 10}); err != nil || stats.Applied || stats.Reason != "directive_over_cap" || string(out) != string(body) {
		t.Fatalf("cap out=%s stats=%+v err=%v", out, stats, err)
	}
	if out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "noop"}); err != nil || stats.Applied || stats.Reason != "noop_profile" || string(out) != string(body) {
		t.Fatalf("noop out=%s stats=%+v err=%v", out, stats, err)
	}
	if out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "off"}); err != nil || stats.Applied || stats.Reason != "noop_profile" || string(out) != string(body) {
		t.Fatalf("off out=%s stats=%+v err=%v", out, stats, err)
	}
	exact := []byte(`{"messages":[{"role":"user","content":"reply exactly: ok"}]}`)
	if out, stats, err := InjectBody(types.OpenAI, exact, Options{Enabled: true, Profile: "openai"}); err != nil || stats.Applied || stats.Reason != "exact_reply" || string(out) != string(exact) {
		t.Fatalf("exact out=%s stats=%+v err=%v", out, stats, err)
	}
}

func TestInjectBody_ErrorBranches(t *testing.T) {
	t.Parallel()
	if _, _, err := InjectBody(types.OpenAI, []byte(`{`), Options{Enabled: true, Profile: "openai"}); err == nil {
		t.Fatal("expected parse error")
	}
	if _, _, err := InjectBody(types.OpenAI, []byte(`{}`), Options{Enabled: true, Profile: "wat"}); err == nil {
		t.Fatal("expected profile error")
	}
	if _, _, err := InjectBody(types.OpenAI, []byte(`{"messages":{}}`), Options{Enabled: true, Profile: "openai"}); err == nil {
		t.Fatal("expected messages shape error")
	}
}

func TestInjectBody_SkipBranches(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	if out, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: false}); err != nil || stats.Applied || stats.Reason != "disabled" || string(out) != string(body) {
		t.Fatalf("disabled out=%s stats=%+v err=%v", out, stats, err)
	}
	if out, stats, err := InjectBody(types.MiniMax, []byte(`{}`), Options{Enabled: true, Profile: "anthropic"}); err != nil || stats.Applied || stats.Reason != "unsupported_provider" || string(out) != `{}` {
		t.Fatalf("unsupported provider out=%s stats=%+v err=%v", out, stats, err)
	}
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(emptyPath, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai", CustomDirectivePath: emptyPath}); err != nil || stats.Applied || stats.Reason != "empty_directive" {
		t.Fatalf("empty custom stats=%+v err=%v", stats, err)
	}
	if _, _, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai", CustomDirectivePath: filepath.Join(dir, "missing.txt")}); err == nil {
		t.Fatal("expected missing custom directive error")
	}
}

func TestInjectBody_LowROIGates(t *testing.T) {
	t.Parallel()
	readOnly := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"Read-only inspect and report. Do not edit."}]}]}`)
	if out, stats, err := InjectBody(types.CodexChatGPT, readOnly, Options{Enabled: true, Profile: "codex", InputTokens: 20000}); err != nil || stats.Applied || stats.Reason != "read_only_low_roi" || string(out) != string(readOnly) {
		t.Fatalf("read-only out=%s stats=%+v err=%v", out, stats, err)
	}
	if out, stats, err := InjectBody(types.CodexChatGPT, readOnly, Options{Enabled: true, Profile: "codex", InputTokens: 70000}); err != nil || !stats.Applied || !strings.Contains(string(out), "Read-only: concise verdict plus evidence only") {
		t.Fatalf("large read-only out=%s stats=%+v err=%v", out, stats, err)
	}
	planning := []byte(`{"messages":[{"role":"user","content":"plan next steps"}]}`)
	if _, stats, err := InjectBody(types.OpenAI, planning, Options{Enabled: true, Profile: "openai", InputTokens: 20000}); err != nil || stats.Applied || stats.Reason != "planning_low_roi" {
		t.Fatalf("planning stats=%+v err=%v", stats, err)
	}
	direct := []byte(`{"messages":[{"role":"user","content":"what is this"}]}`)
	if _, stats, err := InjectBody(types.OpenAI, direct, Options{Enabled: true, Profile: "openai", InputTokens: 8000}); err != nil || stats.Applied || stats.Reason != "direct_answer_low_roi" {
		t.Fatalf("direct stats=%+v err=%v", stats, err)
	}
	repair := []byte(`{"messages":[{"role":"user","content":"you skipped the failing test output, explain more"}]}`)
	if out, stats, err := InjectBody(types.OpenAI, repair, Options{Enabled: true, Profile: "openai", InputTokens: 90000}); err != nil || stats.Applied || stats.Reason != "repair_followup_low_roi" || string(out) != string(repair) {
		t.Fatalf("repair out=%s stats=%+v err=%v", out, stats, err)
	}
}

func TestInjectBody_AnthropicUnsupportedSystemShape(t *testing.T) {
	t.Parallel()
	body := []byte(`{"system":123,"messages":[{"role":"user","content":"hi"}]}`)
	if _, _, err := InjectBody(types.Anthropic, body, Options{Enabled: true, Profile: "anthropic"}); err == nil {
		t.Fatal("expected unsupported system shape error")
	}
}

func TestInjectBody_AnthropicAddsMissingSystem(t *testing.T) {
	t.Parallel()
	out, stats, err := InjectBody(types.Anthropic, []byte(`{"messages":[{"role":"user","content":"hi"}]}`), Options{Enabled: true, Profile: "anthropic"})
	if err != nil || !stats.Applied || !strings.Contains(string(out), `"system"`) {
		t.Fatalf("out=%s stats=%+v err=%v", out, stats, err)
	}
}

func TestProfilesAndShapeDirective(t *testing.T) {
	t.Parallel()
	for _, profile := range []string{"mild", "standard", "aggressive", "codex_aggressive", "custom", "anthropic", "openai", "codex", "off"} {
		if _, err := ParseProfile(profile); err != nil {
			t.Fatalf("profile %q: %v", profile, err)
		}
	}
	if got := NextSofter(ProfileAggressive); got != ProfileStandard {
		t.Fatalf("NextSofter aggressive=%s", got)
	}
	if got := NextSofter(ProfileMild); got != ProfileOff {
		t.Fatalf("NextSofter mild=%s", got)
	}
	if got := NextSofter(ProfileStandard); got != ProfileMild {
		t.Fatalf("NextSofter standard=%s", got)
	}
	if got := NextSofter(ProfileOff); got != ProfileOff {
		t.Fatalf("NextSofter off=%s", got)
	}
	for _, shape := range []TaskShape{ShapeCodeEdit, ShapeNewFile, ShapeReadOnly, ShapeReview, ShapeDebugging, ShapeToolReasoning, ShapePlanning, ShapeDirectAnswer, ShapeUnknown} {
		if text := DirectiveForShape(ProfileCodexAggressive, shape, DefaultMarker); text == "" {
			t.Fatalf("empty directive for shape %s", shape)
		}
	}
	if text := DirectiveForShape(ProfileCodexAggressive, ShapeReadOnly, DefaultMarker); strings.Contains(text, "Codex output rules") || !strings.Contains(text, "do not mention hooks") {
		t.Fatalf("read-only codex directive should be compact and meta-suppressing: %q", text)
	}
	for _, profile := range []Profile{ProfileMild, ProfileStandard, ProfileAggressive, ProfileCustom, ProfileOff} {
		_ = DirectiveForShape(profile, ShapeDirectAnswer, "")
	}
}

func TestInjectBody_CodexMessagesAndInputBranches(t *testing.T) {
	t.Parallel()
	messagesBody := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	if out, stats, err := InjectBody(types.CodexChatGPT, messagesBody, Options{Enabled: true, Profile: "codex"}); err != nil || !stats.Applied || !strings.Contains(string(out), DefaultMarker) {
		t.Fatalf("codex messages out=%s stats=%+v err=%v", out, stats, err)
	}
	if out, stats, err := InjectBody(types.CodexChatGPT, []byte(`{"input":[]}`), Options{Enabled: true, Profile: "codex"}); err != nil || !stats.Applied || !strings.Contains(string(out), DefaultMarker) {
		t.Fatalf("codex array out=%s stats=%+v err=%v", out, stats, err)
	}
	systemArrayBody := []byte(`{"input":[{"type":"message","role":"system","content":[{"type":"input_text","text":"base"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	out, stats, err := InjectBody(types.CodexChatGPT, systemArrayBody, Options{Enabled: true, Profile: "codex"})
	if err != nil || !stats.Applied {
		t.Fatalf("codex system array out=%s stats=%+v err=%v", out, stats, err)
	}
	if strings.Contains(string(out), `"type":"text"`) || !strings.Contains(string(out), `"type":"input_text"`) {
		t.Fatalf("codex output-reduce must use Responses input_text blocks: %s", out)
	}
	if _, _, err := InjectBody(types.CodexChatGPT, []byte(`{"input":{}}`), Options{Enabled: true, Profile: "codex"}); err == nil {
		t.Fatal("expected object input error")
	}
	if out, stats, err := InjectBody(types.CodexChatGPT, []byte(`{"input":""}`), Options{Enabled: true, Profile: "codex"}); err != nil || !stats.Applied || !strings.Contains(string(out), DefaultMarker) {
		t.Fatalf("empty string input out=%s stats=%+v err=%v", out, stats, err)
	}
	if out, stats, err := InjectBody(types.CodexChatGPT, []byte(`{"input":   }`), Options{Enabled: true, Profile: "codex"}); err == nil || out == nil || stats.Applied {
		t.Fatalf("expected parse error out=%s stats=%+v err=%v", out, stats, err)
	}
	if out, stats, err := InjectBody(types.CodexChatGPT, []byte(`{}`), Options{Enabled: true, Profile: "codex"}); err != nil || stats.Applied || stats.Reason != "unsupported_shape" || string(out) != `{}` {
		t.Fatalf("missing input out=%s stats=%+v err=%v", out, stats, err)
	}
	changed, err := injectCodex(map[string]json.RawMessage{"input": nil}, "rule")
	if err != nil || changed {
		t.Fatalf("nil raw input changed=%v err=%v", changed, err)
	}
	if out, stats, err := InjectBody(types.OpenAI, []byte(`{}`), Options{Enabled: true, Profile: "openai"}); err != nil || stats.Applied || stats.Reason != "unsupported_shape" || string(out) != `{}` {
		t.Fatalf("openai missing messages out=%s stats=%+v err=%v", out, stats, err)
	}
}

func TestInjectBody_AddedBytesFallback(t *testing.T) {
	t.Parallel()
	body := []byte("{\n  \"messages\": [\n    {\"role\":\"user\",\"content\":\"" + strings.Repeat("x", 900) + "\"}\n  ]\n}")
	_, stats, err := InjectBody(types.OpenAI, body, Options{Enabled: true, Profile: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Applied || stats.AddedBytes <= 0 {
		t.Fatalf("stats: %+v", stats)
	}
}

func TestAppendToMessageContentArrayAndFallback(t *testing.T) {
	t.Parallel()
	msg := map[string]json.RawMessage{
		"role":    mustJSON("system"),
		"content": mustJSON([]map[string]string{{"type": "text", "text": "base"}}),
	}
	out := appendToMessageContent(msg, "rule")
	if !strings.Contains(string(out["content"]), "rule") {
		t.Fatalf("array content not appended: %s", out["content"])
	}
	msg = map[string]json.RawMessage{"role": mustJSON("system"), "content": json.RawMessage(`123`)}
	out = appendToMessageContent(msg, "rule")
	if string(out["content"]) != `"rule"` {
		t.Fatalf("fallback content: %s", out["content"])
	}
	codex := appendToCodexMessageContent(map[string]json.RawMessage{
		"role":    mustJSON("system"),
		"content": mustJSON([]map[string]string{{"type": "input_text", "text": "base"}}),
	}, "rule")
	if strings.Contains(string(codex["content"]), `"type":"text"`) || !strings.Contains(string(codex["content"]), "rule") {
		t.Fatalf("codex content block: %s", codex["content"])
	}
	codex = appendToCodexMessageContent(map[string]json.RawMessage{
		"role":    mustJSON("system"),
		"content": mustJSON("base"),
	}, "rule")
	if !strings.Contains(string(codex["content"]), "base") || !strings.Contains(string(codex["content"]), "rule") {
		t.Fatalf("codex string content: %s", codex["content"])
	}
	codex = appendToCodexMessageContent(map[string]json.RawMessage{
		"role":    mustJSON("system"),
		"content": json.RawMessage(`123`),
	}, "rule")
	if !strings.Contains(string(codex["content"]), `"type":"input_text"`) || !strings.Contains(string(codex["content"]), "rule") {
		t.Fatalf("codex fallback content: %s", codex["content"])
	}
	codex = appendToCodexMessageContent(map[string]json.RawMessage{"role": mustJSON("system")}, "rule")
	if !strings.Contains(string(codex["content"]), `"type":"input_text"`) || !strings.Contains(string(codex["content"]), "rule") {
		t.Fatalf("codex missing content: %s", codex["content"])
	}
}

func TestProfilesAndTokenEstimateBranches(t *testing.T) {
	t.Parallel()
	for _, provider := range []types.Provider{types.Anthropic, types.OpenAI, types.CodexChatGPT, types.MiniMax} {
		if ResolveProfile(provider, ProfileAuto) == "" {
			t.Fatalf("empty resolved profile for %v", provider)
		}
	}
	if ResolveProfile(types.OpenAI, ProfileAnthropic) != ProfileAnthropic {
		t.Fatal("explicit profile should win")
	}
	if Directive(ProfileAnthropic, "") == "" || Directive(ProfileOpenAI, "m") == "" || Directive(ProfileCodex, "m") == "" {
		t.Fatal("expected directives")
	}
	if Directive(ProfileNoop, "m") != "" {
		t.Fatal("noop directive should be empty")
	}
	if estimateTokens(0) != 0 || estimateTokens(1) != 1 || estimateTokens(5) != 2 {
		t.Fatal("token estimate branches wrong")
	}
}
