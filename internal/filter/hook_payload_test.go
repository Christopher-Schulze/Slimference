package filter

import (
	"strings"
	"testing"
)

func TestExtractPostToolPayloadFromHookJSON(t *testing.T) {
	t.Parallel()

	command, toolResponse, err := ExtractPostToolPayloadFromHookJSON([]byte(`{
		"hook_event_name":"PostToolUse",
		"tool_input":{"command":"git status --short"},
		"tool_response":"\u001b[31mM main.go\u001b[0m"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if command != "git status --short" {
		t.Fatalf("command=%q", command)
	}
	if !strings.Contains(toolResponse, "main.go") {
		t.Fatalf("toolResponse=%q", toolResponse)
	}
}

func TestExtractPostToolPayloadFromHookJSON_missingResponse(t *testing.T) {
	t.Parallel()

	command, toolResponse, err := ExtractPostToolPayloadFromHookJSON([]byte(`{"tool_input":{"command":"git status"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if command != "git status" || toolResponse != "" {
		t.Fatalf("command=%q toolResponse=%q", command, toolResponse)
	}
}

func TestCompactCapturedOutput_appliesLayer0Filters(t *testing.T) {
	t.Parallel()

	output := "M  main.go\n?? new.txt\n"
	compacted, changed := CompactCapturedOutput("", "git status", output, 2000)
	if !changed {
		t.Fatal("git status output should be compacted")
	}
	if !strings.Contains(string(compacted), "[git status]") {
		t.Fatalf("unexpected compacted output: %q", compacted)
	}
}

func TestCompactCapturedOutput_genericFallback(t *testing.T) {
	t.Parallel()

	output := "\u001b[31mhello\u001b[0m\n"
	compacted, changed := CompactCapturedOutput("", "", output, 2000)
	if changed {
		t.Fatal("generic ANSI stripping alone should not count as a material change")
	}
	if strings.Contains(string(compacted), "\u001b") {
		t.Fatalf("ANSI codes should be stripped: %q", compacted)
	}
}

func TestCompactCapturedOutput_truncatesLongOutput(t *testing.T) {
	t.Parallel()

	output := strings.Repeat("x", 4096)
	compacted, changed := CompactCapturedOutput("", "", output, 64)
	if !changed {
		t.Fatal("truncation should count as a material change")
	}
	if !strings.Contains(string(compacted), "truncated") {
		t.Fatalf("expected truncation hint, got %q", compacted)
	}
}

func TestExtractPostToolPayloadFromHookJSON_withoutCommand(t *testing.T) {
	t.Parallel()

	command, toolResponse, err := ExtractPostToolPayloadFromHookJSON([]byte(`{"tool_response":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if command != "" || toolResponse != "ok" {
		t.Fatalf("command=%q toolResponse=%q", command, toolResponse)
	}
}

func TestPrimaryArgvForCapturedOutput(t *testing.T) {
	t.Parallel()

	if argv := primaryArgvForCapturedOutput("FOO=bar git status --short"); len(argv) != 3 || argv[0] != "git" {
		t.Fatalf("env assignment parsing failed: %#v", argv)
	}
	if argv := primaryArgvForCapturedOutput("git status | cat"); len(argv) != 2 || argv[0] != "git" || argv[1] != "status" {
		t.Fatalf("pipeline parsing failed: %#v", argv)
	}
	if argv := primaryArgvForCapturedOutput("> out.txt"); argv != nil {
		t.Fatalf("redirect-only command should return nil: %#v", argv)
	}
}

func TestArgvForCapturedOutput_exportedWrapper(t *testing.T) {
	t.Parallel()

	argv := ArgvForCapturedOutput("cd repo && go test ./...")
	if len(argv) != 2 || argv[0] != "cd" || argv[1] != "repo" {
		t.Fatalf("exported wrapper argv=%#v", argv)
	}
}

func TestIsEnvAssignmentToken(t *testing.T) {
	t.Parallel()

	if !isEnvAssignmentToken("FOO=bar") {
		t.Fatal("expected env assignment token")
	}
	if isEnvAssignmentToken("A/B=bar") {
		t.Fatal("slash in variable name should not count as env assignment")
	}
	if isEnvAssignmentToken("not-an-assignment") {
		t.Fatal("plain token should not count as env assignment")
	}
}
