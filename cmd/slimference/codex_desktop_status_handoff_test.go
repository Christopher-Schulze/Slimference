package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control"
)

func TestCodexDesktopStatusPromptRequiredJSONIncludesProofHandoff(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		StartedAt:         "2026-05-18T12:00:00Z",
		LaunchPID:         4242,
		CapturePath:       "/tmp/desktop-proof.frames.jsonl",
		MatrixPath:        "/tmp/desktop-proof.matrix.jsonl",
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
	if got.ProofStartedAt != "2026-05-18T12:00:00Z" {
		t.Fatalf("proof started at=%q", got.ProofStartedAt)
	}
	if !strings.Contains(got.ClassDistributionCommand, "--since=2026-05-18T12:00:00Z") ||
		strings.Contains(got.ClassDistributionCommand, "--since-file=") {
		t.Fatalf("class distribution command=%q", got.ClassDistributionCommand)
	}
	if got.CapturePath != "/tmp/desktop-proof.frames.jsonl" ||
		got.MatrixPath != "/tmp/desktop-proof.matrix.jsonl" ||
		!strings.Contains(got.SearchCapProofCommand, "search-cap-proof --frames /tmp/desktop-proof.frames.jsonl") ||
		!strings.Contains(got.MatrixRowCommand, "wss-proof-live-row --matrix-row /tmp/desktop-proof.matrix.jsonl --frames /tmp/desktop-proof.frames.jsonl") ||
		!strings.Contains(got.FocusedMatrixCommand, "wss-proof-matrix /tmp/desktop-proof.matrix.jsonl") {
		t.Fatalf("capture proof handoff missing: %+v", got)
	}
	if !strings.Contains(strings.Join(got.NextSteps, "\n"), "headroom_present=true") {
		t.Fatalf("next steps missing headroom gate: %+v", got.NextSteps)
	}
}

func TestCodexDesktopStatusPromptRequiredReusesRunningScopedProofApp(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		StartedAt:         "2026-05-18T12:00:00Z",
		LaunchPID:         4242,
		CapturePath:       "/tmp/desktop-proof.frames.jsonl",
		MatrixPath:        "/tmp/desktop-proof.matrix.jsonl",
		LaunchReady:       true,
		ManualPromptStill: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{4242}, nil
	}
	codexDesktopAppServerActiveFn = func() bool { return true }

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
	if got.ManualProofCommand != codexDesktopReuseProofCommand {
		t.Fatalf("manual proof command=%q want reuse command", got.ManualProofCommand)
	}
	joined := strings.Join(got.NextSteps, "\n")
	if strings.Contains(joined, "Quit the current Codex.app yourself") ||
		!strings.Contains(joined, "existing scoped Codex.app") ||
		!strings.Contains(joined, "--reuse-running") {
		t.Fatalf("reuse handoff should avoid replace/quit guidance: %+v", got.NextSteps)
	}
}

func TestCodexDesktopStatusPromptRequiredJSONRecoversProofSinceFromSession(t *testing.T) {
	withCodexCmdStubs(t)
	proof := &codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		LaunchPID:         4343,
		LaunchReady:       true,
		ManualPromptStill: true,
	}
	writeCodexDesktopProofResult(proof)
	startedAt := time.Date(2026, 5, 18, 11, 59, 30, 0, time.UTC)
	if err := writeCodexDesktopProofSession(
		codexDesktopProveFlags{host: "127.0.0.1", port: "8990"},
		control.WSSState{},
		startedAt,
		proof,
	); err != nil {
		t.Fatalf("write session: %v", err)
	}
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{4343}, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}

	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.ProofStartedAt != "2026-05-18T11:59:30Z" {
		t.Fatalf("proof started at=%q", got.ProofStartedAt)
	}
	if !strings.Contains(got.ClassDistributionCommand, "--since=2026-05-18T11:59:30Z") ||
		strings.Contains(got.ClassDistributionCommand, "--since-file=") {
		t.Fatalf("class distribution command=%q", got.ClassDistributionCommand)
	}
}

func TestCodexDesktopStatusPromptRequiredJSONUsesNearbyLegacySinceFile(t *testing.T) {
	withCodexCmdStubs(t)
	proof := &codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		LaunchPID:         4344,
		LaunchReady:       true,
		ManualPromptStill: true,
	}
	writeCodexDesktopProofResult(proof)
	sessionStartedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	if err := writeCodexDesktopProofSession(
		codexDesktopProveFlags{host: "127.0.0.1", port: "8990"},
		control.WSSState{},
		sessionStartedAt,
		proof,
	); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := os.WriteFile(codexDesktopProofSinceFilePathFn(), []byte("2026-05-18T11:59:30Z\n"), 0o600); err != nil {
		t.Fatalf("write since file: %v", err)
	}
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{4344}, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}

	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.ProofStartedAt != "2026-05-18T11:59:30Z" {
		t.Fatalf("proof started at=%q", got.ProofStartedAt)
	}
	if !strings.Contains(got.ClassDistributionCommand, "--since=2026-05-18T11:59:30Z") {
		t.Fatalf("class distribution command=%q", got.ClassDistributionCommand)
	}
}

func TestCodexDesktopStatusPromptRequiredJSONFallsBackForInvalidProofSince(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		StartedAt:         "not-rfc3339",
		LaunchPID:         4444,
		LaunchReady:       true,
		ManualPromptStill: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{4444}, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}

	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.ProofStartedAt != "" {
		t.Fatalf("invalid proof started at must not propagate: %q", got.ProofStartedAt)
	}
	if got.ClassDistributionCommand != codexDesktopClassDistributionCommand ||
		!strings.Contains(got.ClassDistributionCommand, "--since-file=/tmp/slimference-desktop-proof-since.txt") {
		t.Fatalf("class distribution command=%q", got.ClassDistributionCommand)
	}
}

func TestCodexDesktopStatusPromptRequiredTextIncludesProofHandoff(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:              "desktop_ready_for_prompt",
		Transport:         codexDesktopTransportAppServer,
		StartedAt:         "2026-05-18T12:00:00Z",
		LaunchPID:         5151,
		CapturePath:       "/tmp/desktop-proof-text.frames.jsonl",
		MatrixPath:        "/tmp/desktop-proof-text.matrix.jsonl",
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
		"Since     2026-05-18T12:00:00Z",
		"Finish    slimference codex desktop prove --finish --json",
		"Capture   /tmp/desktop-proof-text.frames.jsonl",
		"Measure   go run ./scripts/utils wss-class-distribution",
		"Row       go run ./scripts/utils wss-proof-live-row --matrix-row /tmp/desktop-proof-text.matrix.jsonl --frames /tmp/desktop-proof-text.frames.jsonl",
		"Matrix    go run ./scripts/utils wss-proof-matrix /tmp/desktop-proof-text.matrix.jsonl",
		"SearchCap go run ./scripts/utils search-cap-proof --frames /tmp/desktop-proof-text.frames.jsonl",
		"--since=2026-05-18T12:00:00Z",
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

func TestCodexDesktopStatusAlreadyRunningIncludesSafeOwnerProofRunbook(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:           "desktop_app_server_phasef_proven",
		Transport:      codexDesktopTransportAppServer,
		LaunchPID:      5151,
		DesktopProven:  true,
		DesktopSavings: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{6262}, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}

	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "codex_desktop_already_running" || got.FailureClass != "codex_desktop_already_running" {
		t.Fatalf("status=%+v", got)
	}
	if got.ConversationObserved || !got.LiveProofRequired {
		t.Fatalf("running owner app must not inherit stale proof: %+v", got)
	}
	if got.ManualProofCommand != codexDesktopManualProofCommand ||
		!strings.Contains(got.OwnerPrompt, "PROOF_DONE") ||
		got.FinishCommand != codexDesktopFinishProofCommand {
		t.Fatalf("owner proof handoff missing: %+v", got)
	}
	if strings.Contains(got.ManualProofCommand, "--replace-existing") {
		t.Fatalf("manual proof command must not normalize replace-existing: %q", got.ManualProofCommand)
	}
	joined := strings.Join(got.NextSteps, "\n")
	for _, want := range []string{
		"Quit the current Codex.app yourself",
		"Run manual_proof_command",
		"newly launched scoped Codex.app",
		"finish_command",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("next steps missing %q: %+v", want, got.NextSteps)
		}
	}
}

func TestCodexDesktopStatusNoWSSDeltaIncludesFreshManualProofRunbook(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:         "desktop_no_wss_delta",
		FailureClass: "no_wss_delta",
		Transport:    codexDesktopTransportAppServer,
		StartedAt:    "2026-05-18T12:00:00Z",
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return nil, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	text := out.String()
	for _, want := range []string{
		"desktop_direct_only",
		"Manual    slimference codex desktop prove --manual --json --duration=30s --keep-open",
		"Finish    slimference codex desktop prove --finish --json",
		"Prompt    In the current Slimference repository",
		"PROOF_DONE",
		"newly launched scoped Codex.app",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status text missing %q:\n%s", want, text)
		}
	}
}

func TestCodexDesktopStatusNoWSSDeltaReusesRunningScopedProofApp(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:         "desktop_no_wss_delta",
		FailureClass: "no_wss_delta",
		Transport:    codexDesktopTransportAppServer,
		StartedAt:    "2026-05-18T12:00:00Z",
		LaunchPID:    7373,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{7373}, nil
	}
	codexDesktopAppServerActiveFn = func() bool { return true }

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}

	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "desktop_direct_only" || got.FailureClass != "no_wss_delta" {
		t.Fatalf("status=%+v", got)
	}
	if got.ManualProofCommand != codexDesktopReuseProofCommand {
		t.Fatalf("manual proof command=%q want reuse command", got.ManualProofCommand)
	}
	joined := strings.Join(got.NextSteps, "\n")
	if strings.Contains(joined, "Quit the current Codex.app yourself") ||
		!strings.Contains(joined, "existing scoped Codex.app") ||
		!strings.Contains(joined, "fresh capture") {
		t.Fatalf("reuse no-wss handoff should avoid fresh launch guidance: %+v", got.NextSteps)
	}
}
