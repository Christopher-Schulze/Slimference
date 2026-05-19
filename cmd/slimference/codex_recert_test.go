package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/codexroute"
	"github.com/slimference/slimference/internal/control"
)

func TestCodexRecertifyHappyPathWritesCertOnly(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexNowFn = func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) }
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{PromptSequence: []string{"one", "two"}}, nil
	}
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(7, 2), nil
	}
	var cert codexroute.CertificationState
	var bridgeSaved bool
	var recert codexroute.RecertState
	codexCertSaveFn = func(_ string, state codexroute.CertificationState) error {
		cert = state
		return nil
	}
	codexBridgeSaveFn = func(_ string, state codexroute.BridgeProofState) error {
		bridgeSaved = true
		return nil
	}
	codexRecertSaveFn = func(_ string, state codexroute.RecertState) error {
		recert = state
		return nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss"}, p); rc != 0 {
		t.Fatalf("rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	if cert.CodexVersion != "0.131.0" || cert.FramesReencoded != 7 {
		t.Fatalf("bad cert: %+v", cert)
	}
	if bridgeSaved {
		t.Fatal("mutating phase-f run must not be recorded as byte-equal bridge proof")
	}
	if !recert.PhaseFPassed || recert.FramesReencoded != 7 || recert.CompressedMutated != 2 {
		t.Fatalf("bad final recert state: %+v", recert)
	}
	if !strings.Contains(out.String(), "Codex WSS recertified") {
		t.Fatalf("missing success output: %q", out.String())
	}
}

func TestCodexRecertifyBridgeOnlyWritesBridgeAndReturnsFailure(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, nil
	}
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(0, 0), nil
	}
	var certSaved bool
	var bridgeSaved bool
	codexCertSaveFn = func(string, codexroute.CertificationState) error {
		certSaved = true
		return nil
	}
	codexBridgeSaveFn = func(string, codexroute.BridgeProofState) error {
		bridgeSaved = true
		return nil
	}

	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--force"}, p); rc != 1 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if certSaved {
		t.Fatal("phase-f cert must not be saved when mutation proof failed")
	}
	if !bridgeSaved {
		t.Fatal("bridge proof should be saved when byte-equal WSS is clean")
	}
	if !strings.Contains(errBuf.String(), "WSS bridge proof passed") {
		t.Fatalf("missing bridge-only output: %q", errBuf.String())
	}
}

func TestCodexRecertifyJSONKeepsFailureExitCode(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, nil
	}
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(0, 0), nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--json"}, p); rc != 1 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	var got codexRecertifyResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if got.PhaseFPassed || !got.BridgePassed {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestCodexRecertifyJSONSuccessExitCode(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{PromptSequence: []string{"one"}}, nil
	}
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(8, 3), nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	var got codexRecertifyResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if !got.PhaseFPassed || got.DeltaWSS.FramesReencoded != 8 {
		t.Fatalf("unexpected json success: %+v", got)
	}
}

func TestCodexRecertifyNoWriteSkipsCertAndBridgeWrites(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, nil
	}
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(7, 2), nil
	}
	codexCertSaveFn = func(string, codexroute.CertificationState) error {
		t.Fatal("no-write must not save phase-f cert")
		return nil
	}
	codexBridgeSaveFn = func(string, codexroute.BridgeProofState) error {
		t.Fatal("no-write must not save bridge proof")
		return nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--no-write"}, p); rc != 0 {
		t.Fatalf("rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "Codex WSS recertified") {
		t.Fatalf("missing success output: %q", out.String())
	}
}

func TestCodexRecertifyUsesInjectableLogWriter(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, nil
	}
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(7, 2), nil
	}
	var lines []string
	codexRecertLogFn = func(gotHome, line string) {
		if gotHome != home {
			t.Fatalf("log home=%q want %q", gotHome, home)
		}
		lines = append(lines, line)
	}

	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--no-write"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if len(lines) != 2 ||
		!strings.Contains(lines[0], "start attempt=") ||
		!strings.Contains(lines[1], "finish attempt=") {
		t.Fatalf("unexpected recert log lines: %#v", lines)
	}
	if _, err := os.Stat(codexroute.RecertLogPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("test recert must use injected logger instead of writing the real log path, stat err=%v", err)
	}
}

func TestCodexRecertifyDryRunDoesNotTrigger(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	triggered := false
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		triggered = true
		return codexRecertTriggerResult{}, nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--dry-run", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if triggered {
		t.Fatal("dry-run must not trigger live Codex")
	}
	var got codexRecertifyResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if got.CertificationPath == "" || got.BridgeProofPath == "" {
		t.Fatalf("missing paths: %+v", got)
	}

	out.Reset()
	if rc := runCodexCmd([]string{"recertify", "wss", "--dry-run"}, p); rc != 0 {
		t.Fatalf("text dry-run rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "Codex WSS recert plan") {
		t.Fatalf("missing text plan: %q", out.String())
	}
}

func TestCodexRecertifyHelpDoesNotResolveHome(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) {
		t.Fatal("help must not resolve HOME")
		return "", nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "--help"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "slimference codex recertify wss") {
		t.Fatalf("missing help text: %q", out.String())
	}
}

func TestCodexRecertifyTriggerErrorPersistsFailureState(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
	}
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, errors.New("boom")
	}
	var recert codexroute.RecertState
	codexRecertSaveFn = func(_ string, state codexroute.RecertState) error {
		recert = state
		return nil
	}

	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss"}, p); rc != 1 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if recert.Status != "failed" || !strings.Contains(recert.LastError, "trigger failed") {
		t.Fatalf("bad recert state: %+v", recert)
	}
	if !strings.Contains(errBuf.String(), "trigger failed") {
		t.Fatalf("missing trigger error: %q", errBuf.String())
	}
}

func TestCodexRecertifyJSONErrorPersistsFailureState(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
	}
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, errors.New("json boom")
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--json"}, p); rc != 1 {
		t.Fatalf("rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if got["attempt_id"] == "" || !strings.Contains(got["error"], "json boom") {
		t.Fatalf("bad json error: %+v", got)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("json error mode must not write stderr, got %q", errBuf.String())
	}
}

func TestCodexRecertifyErrorBranches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		setup func(t *testing.T)
		want  string
	}{
		{
			name: "parse error",
			args: []string{"recertify", "wss", "--bad"},
			want: "unknown flag",
		},
		{
			name: "bad subject",
			args: []string{"recertify", "http"},
			want: "subject must be wss",
		},
		{
			name: "home unresolved",
			args: []string{"recertify", "wss"},
			setup: func(t *testing.T) {
				codexRouteHomeFn = func() (string, error) { return "", errors.New("home gone") }
			},
			want: "HOME unresolved",
		},
		{
			name: "codex version command fails",
			args: []string{"recertify", "wss"},
			setup: func(t *testing.T) {
				codexVersionOutFn = func() ([]byte, error) { return nil, errors.New("no codex") }
			},
			want: "codex --version failed",
		},
		{
			name: "codex version parse fails",
			args: []string{"recertify", "wss"},
			setup: func(t *testing.T) {
				codexVersionOutFn = func() ([]byte, error) { return []byte("garbage\n"), nil }
			},
			want: "unexpected codex --version output",
		},
		{
			name: "preflight state fails",
			args: []string{"recertify", "wss"},
			setup: func(t *testing.T) {
				codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
					return control.SetupState{}, errors.New("admin down")
				}
			},
			want: "preflight admin state unavailable",
		},
		{
			name: "postflight state fails",
			args: []string{"recertify", "wss"},
			setup: func(t *testing.T) {
				calls := 0
				codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
					calls++
					if calls == 1 {
						return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
					}
					return control.SetupState{}, errors.New("admin gone")
				}
			},
			want: "postflight admin state unavailable",
		},
		{
			name: "bridge write fails",
			args: []string{"recertify", "wss"},
			setup: func(t *testing.T) {
				calls := 0
				codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
					calls++
					if calls == 1 {
						return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
					}
					return recertPostState(0, 0), nil
				}
				codexBridgeSaveFn = func(string, codexroute.BridgeProofState) error {
					return errors.New("bridge disk full")
				}
			},
			want: "write bridge proof",
		},
		{
			name: "cert write fails",
			args: []string{"recertify", "wss"},
			setup: func(t *testing.T) {
				calls := 0
				codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
					calls++
					if calls == 1 {
						return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
					}
					return recertPostState(7, 2), nil
				}
				codexCertSaveFn = func(string, codexroute.CertificationState) error {
					return errors.New("cert disk full")
				}
			},
			want: "write certification",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCodexCmdStubs(t)
			codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
			codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
			codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
				return codexRecertTriggerResult{}, nil
			}
			if tc.setup != nil {
				tc.setup(t)
			}
			p, _, errBuf := newTestPrinter()
			if rc := runCodexCmd(tc.args, p); rc == 0 {
				t.Fatalf("expected failure, stderr=%s", errBuf.String())
			}
			if !strings.Contains(errBuf.String(), tc.want) {
				t.Fatalf("stderr=%q want %q", errBuf.String(), tc.want)
			}
		})
	}
}

func TestCodexRecertifyBackoffBlocksAutoRetry(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexNowFn = func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) }
	if err := codexroute.SaveRecertState(home, codexroute.RecertState{
		SchemaVersion: codexroute.RecertSchemaVersion,
		Status:        "failed",
		RetryAfter:    codexNowFn().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save recert state: %v", err)
	}
	triggered := false
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		triggered = true
		return codexRecertTriggerResult{}, nil
	}

	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss"}, p); rc != 1 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if triggered {
		t.Fatal("backoff must block the live trigger")
	}
	if !strings.Contains(errBuf.String(), "recert backoff active") {
		t.Fatalf("missing backoff reason: %q", errBuf.String())
	}
}

func TestCodexRecertifyBackoffExpiredDoesNotBlock(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	codexNowFn = func() time.Time { return now }
	if err := codexroute.SaveRecertState(home, codexroute.RecertState{
		SchemaVersion: codexroute.RecertSchemaVersion,
		Status:        "failed",
		RetryAfter:    now.Add(-time.Second),
	}); err != nil {
		t.Fatalf("save recert state: %v", err)
	}
	blocked, reason := codexRecertBackoffActive(home, false)
	if blocked || reason != "" {
		t.Fatalf("expired backoff blocked=%v reason=%q", blocked, reason)
	}
}

func TestCodexRecertifyForceDoesNotBypassActiveLock(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	path := codexroute.RecertLockPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("pid=999999\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--force"}, p); rc != 1 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "already running") {
		t.Fatalf("missing lock reason: %q", errBuf.String())
	}
}

func TestCodexRecertifyRemovesStaleLock(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.131.0\n"), nil }
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	codexNowFn = func() time.Time { return now }
	path := codexroute.RecertLockPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("pid=999999\n"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	old := now.Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, nil
	}
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(7, 2), nil
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"recertify", "wss", "--no-write"}, p); rc != 0 {
		t.Fatalf("rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
}

func TestParseCodexRecertifyFlagsAllBranches(t *testing.T) {
	flags, err := parseCodexRecertifyFlags([]string{
		"wss",
		"--dry-run",
		"--no-write",
		"--force",
		"--json",
		"--host=localhost",
		"--port=9000",
		"--timeout=5s",
		"--operator", "operator",
		"--notes", "notes",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if flags.subject != "wss" || flags.host != "localhost" || flags.port != "9000" ||
		flags.timeout != 5*time.Second || flags.operator != "operator" || flags.notes != "notes" ||
		!flags.dryRun || !flags.noWrite || !flags.force || !flags.json {
		t.Fatalf("flags=%+v", flags)
	}
	for _, args := range [][]string{
		{"--timeout=0s"},
		{"--timeout=bogus"},
		{"--operator"},
		{"--notes"},
		{"wss", "extra"},
		{"--unknown"},
	} {
		if _, err := parseCodexRecertifyFlags(args); err == nil {
			t.Fatalf("expected parse error for %v", args)
		}
	}
}

func TestDefaultCodexRecertTriggerUsesScopedWSSRuns(t *testing.T) {
	withCodexCmdStubs(t)
	var calls [][]string
	recertRunCommandFn = func(timeout time.Duration, args ...string) error {
		copied := append([]string(nil), args...)
		calls = append(calls, copied)
		if timeout != 7*time.Second {
			t.Fatalf("timeout=%s", timeout)
		}
		return nil
	}
	result, err := defaultCodexRecertTrigger(codexRecertTriggerInput{
		Host:    "127.0.0.1",
		Port:    "8990",
		Timeout: 7 * time.Second,
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if len(result.PromptSequence) != 1 || len(calls) != 1 {
		t.Fatalf("result=%+v calls=%v", result, calls)
	}
	joined := strings.Join(calls[0], "\x00")
	if !strings.Contains(joined, "--transport=wss") ||
		!strings.Contains(joined, "--ignore-user-config") ||
		!strings.Contains(joined, "--ephemeral") ||
		!strings.Contains(joined, "git -C ") ||
		!strings.Contains(joined, "status --short") {
		t.Fatalf("bad scoped WSS calls: %v", calls)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	gotCD := ""
	for i := 0; i+1 < len(calls[0]); i++ {
		if calls[0][i] == "--cd" {
			gotCD = calls[0][i+1]
			break
		}
	}
	if gotCD != cwd {
		t.Fatalf("recert trigger must start Codex from stable cwd to avoid temp project trust writes, --cd=%q want %q", gotCD, cwd)
	}
	if strings.Contains(gotCD, "slimference-codex-recert") {
		t.Fatalf("recert trigger must not --cd into the temp repo: %q", gotCD)
	}
}

func TestSeedCodexRecertRepoCreatesLongStatusTrigger(t *testing.T) {
	dir := t.TempDir()
	if err := seedCodexRecertRepo(dir); err != nil {
		t.Fatalf("seed recert repo: %v", err)
	}
	out, err := exec.Command("git", "-C", dir, "status", "--short").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	lines := strings.Count(strings.TrimSpace(string(out)), "\n") + 1
	if lines < 120 || !strings.Contains(string(out), "?? synthetic_159.go") {
		t.Fatalf("status trigger too small: lines=%d out=%s", lines, out)
	}
}

func TestRunRecertCommandHelperProcess(t *testing.T) {
	if os.Getenv("SLIMFERENCE_RECERT_HELPER") == "1" {
		for _, arg := range os.Args {
			if arg == "fail" {
				fmt.Fprintln(os.Stderr, "helper failed")
				os.Exit(3)
			}
			if arg == "fail-stdout" {
				fmt.Fprintln(os.Stdout, "stdout failed")
				os.Exit(3)
			}
			if arg == "fail-silent" {
				os.Exit(3)
			}
		}
		os.Exit(0)
	}
	t.Setenv("SLIMFERENCE_RECERT_HELPER", "1")
	if err := runRecertCommand(time.Second, "-test.run=TestRunRecertCommandHelperProcess", "--", "ok"); err != nil {
		t.Fatalf("success command: %v", err)
	}
	err := runRecertCommand(time.Second, "-test.run=TestRunRecertCommandHelperProcess", "--", "fail")
	if err == nil || !strings.Contains(err.Error(), "helper failed") {
		t.Fatalf("failure error=%v", err)
	}
	err = runRecertCommand(time.Second, "-test.run=TestRunRecertCommandHelperProcess", "--", "fail-stdout")
	if err == nil || !strings.Contains(err.Error(), "stdout failed") {
		t.Fatalf("stdout fallback error=%v", err)
	}
	err = runRecertCommand(time.Second, "-test.run=TestRunRecertCommandHelperProcess", "--", "fail-silent")
	if err == nil || err.Error() == "" {
		t.Fatalf("silent fallback error=%v", err)
	}
}

func TestAppendBoundedCodexLogRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex-wss-recert.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(codexRecertLogMaxBytes)), 0o600); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	if err := appendBoundedCodexLog(path, []byte("rotated\n")); err != nil {
		t.Fatalf("append: %v", err)
	}
	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read active: %v", err)
	}
	if string(active) != "rotated\n" {
		t.Fatalf("active log=%q", string(active))
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated backup missing: %v", err)
	}
}

func TestAppendBoundedCodexLogErrorBranches(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "parent-file")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := appendBoundedCodexLog(filepath.Join(parentFile, "log"), []byte("x")); err == nil {
		t.Fatal("expected MkdirAll error when parent path is a file")
	}

	logDir := filepath.Join(t.TempDir(), "logdir")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := appendBoundedCodexLog(logDir, []byte("x")); err == nil {
		t.Fatal("expected OpenFile error when log path is a directory")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "codex-wss-recert.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(codexRecertLogMaxBytes)), 0o600); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	backupDir := path + ".1"
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "held"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := appendBoundedCodexLog(path, []byte("rotate\n")); err == nil {
		t.Fatal("expected rotate error when backup path is a non-empty directory")
	}
}

func TestStartCodexAutoRecertSkipBranches(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	codexNowFn = func() time.Time { return now }
	decision := codexroute.AutoDecision{NeedsRecert: true, FallbackReason: "drift"}

	t.Setenv("SLIMFERENCE_CODEX_AUTO_RECERT", "0")
	startCodexAutoRecert(home, "127.0.0.1", "8990", decision)
	if _, err := os.Stat(codexroute.RecertLogPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled auto-recert should not write log, stat err=%v", err)
	}

	t.Setenv("SLIMFERENCE_CODEX_AUTO_RECERT", "1")
	startCodexAutoRecert(home, "127.0.0.1", "8990", codexroute.AutoDecision{})
	startCodexAutoRecert(home, "127.0.0.1", "8990", codexroute.AutoDecision{NeedsRecert: true, RecertStatus: "running"})
	startCodexAutoRecert(home, "127.0.0.1", "8990", codexroute.AutoDecision{NeedsRecert: true, RecertRetryAfter: now.Add(time.Hour)})
	if _, err := os.Stat(codexroute.RecertLogPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skip branches should not write log, stat err=%v", err)
	}
}

func TestRecertSmallHelpers(t *testing.T) {
	if got := counterDelta(10, 2); got != 2 {
		t.Fatalf("counter reset delta=%d", got)
	}
	if staleCodexRecertLock(filepath.Join(t.TempDir(), "missing.lock")) {
		t.Fatal("missing lock must not be stale")
	}
	home := t.TempDir()
	if blocked, reason := codexRecertBackoffActive(home, true); blocked || reason != "" {
		t.Fatalf("force backoff blocked=%v reason=%q", blocked, reason)
	}
	blocked, reason := codexRecertBackoffActive(home, false)
	if blocked || reason != "" {
		t.Fatalf("missing state backoff blocked=%v reason=%q", blocked, reason)
	}
	if got := successTime(true, time.Unix(123, 0)); got.IsZero() {
		t.Fatal("success time should be preserved on pass")
	}
	if got := successTime(false, time.Unix(123, 0)); !got.IsZero() {
		t.Fatalf("success time should be zero on failure: %s", got)
	}
	if got := retryAfter(true, time.Unix(123, 0)); !got.IsZero() {
		t.Fatalf("retry-after should be zero on pass: %s", got)
	}
	if got := retryAfter(false, time.Unix(123, 0)); !got.Equal(time.Unix(123, 0).Add(30 * time.Minute)) {
		t.Fatalf("retry-after failure value=%s", got)
	}
	if err := appendBoundedCodexLog(filepath.Join(t.TempDir(), "missing", "log"), []byte("ok\n")); err != nil {
		t.Fatalf("append log with new parent dir: %v", err)
	}
}

func recertPostState(reencoded, mutated int64) control.SetupState {
	return control.SetupState{
		CodexRoute: control.CodexRouteState{DaemonReachable: true},
		WSS: control.WSSState{
			EngineActive:                true,
			MITMBridged:                 1,
			BytesC2S:                    100,
			BytesS2C:                    200,
			C2SFrames:                   2,
			S2CFrames:                   3,
			FramesForwarded:             4,
			FramesReencoded:             reencoded,
			CompressedMessagesInspected: 5,
			CompressedMessagesMutated:   mutated,
			MutationActive:              reencoded > 0,
			ByteBridgeOnly:              reencoded == 0,
		},
	}
}
