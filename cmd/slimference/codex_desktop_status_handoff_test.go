package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexDesktopStatusPromptRequiredJSONIncludesProofHandoff(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		LaunchPID:         4242,
		LaunchReady:       true,
		ManualPromptStill: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{4242}, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}

	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "desktop_proof_prompt_required" || got.FailureClass != "prompt_required" {
		t.Fatalf("status=%+v", got)
	}
	if !got.LiveProofRequired || got.ConversationObserved {
		t.Fatalf("proof flags=%+v", got)
	}
	if !strings.Contains(got.OwnerPrompt, "PROOF_DONE") || strings.Contains(got.OwnerPrompt, "/Users/") {
		t.Fatalf("owner prompt must be actionable and repo-neutral: %q", got.OwnerPrompt)
	}
	if got.FinishCommand != codexDesktopFinishProofCommand {
		t.Fatalf("finish command=%q", got.FinishCommand)
	}
	if got.ClassDistributionCommand != codexDesktopClassDistributionCommand ||
		!strings.Contains(got.ClassDistributionCommand, "--since-file=/tmp/slimference-desktop-proof-since.txt") {
		t.Fatalf("class distribution command=%q", got.ClassDistributionCommand)
	}
	if !strings.Contains(strings.Join(got.NextSteps, "\n"), "headroom_present=true") {
		t.Fatalf("next steps missing headroom gate: %+v", got.NextSteps)
	}
}

func TestCodexDesktopStatusPromptRequiredTextIncludesProofHandoff(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		LaunchPID:         5151,
		LaunchReady:       true,
		ManualPromptStill: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{5151}, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	text := out.String()
	for _, want := range []string{
		"desktop_proof_prompt_required",
		"Finish    slimference codex desktop prove --finish --json",
		"Measure   go run ./scripts/utils wss-class-distribution",
		"Prompt    In the current Slimference repository",
		"PROOF_DONE",
		"headroom_present=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status text missing %q:\n%s", want, text)
		}
	}
}

func TestCodexDesktopStatusStalePromptLaunchDoesNotEmitHandoff(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		LaunchPID:         6161,
		LaunchReady:       true,
		ManualPromptStill: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return nil, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}

	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "ready_for_live_desktop_probe" || got.FailureClass != "" {
		t.Fatalf("stale prompt handoff should fall back to a fresh probe: %+v", got)
	}
	if got.OwnerPrompt != "" || got.FinishCommand != "" || got.ClassDistributionCommand != "" || len(got.NextSteps) != 0 {
		t.Fatalf("stale prompt handoff must not emit owner proof commands: %+v", got)
	}
	if !strings.Contains(strings.Join(got.Notes, "\n"), "prompt handoff is stale") {
		t.Fatalf("stale handoff note missing: %+v", got.Notes)
	}
}
