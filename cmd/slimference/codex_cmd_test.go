package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/codexroute"
	"github.com/Christopher-Schulze/Slimference/internal/control"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
)

func withCodexCmdStubs(t *testing.T) {
	t.Helper()
	oldHome := codexRouteHomeFn
	oldEnable := codexRouteEnableFn
	oldDisable := codexRouteDisableFn
	oldInspect := codexRouteInspectFn
	oldHealth := codexRouteHealthFn
	oldProxyRun := codexProxyRunFn
	oldVersion := codexVersionFn
	oldAuto := codexAutoFn
	oldCertSave := codexCertSaveFn
	oldBridgeSave := codexBridgeSaveFn
	oldRecertSave := codexRecertSaveFn
	oldAutoRecert := codexAutoRecertFn
	oldRecertTrigger := codexRecertTriggerFn
	oldRecertLog := codexRecertLogFn
	oldRecertRunCommand := recertRunCommandFn
	oldDaemonAutoRecert := daemonCodexAutoRecertFn
	oldDaemonAutoRecertAllowed := daemonAutoRecertAllowedFn
	oldSetupState := codexSetupStateFn
	oldVersionOut := codexVersionOutFn
	oldNow := codexNowFn
	oldDesktopCA := codexDesktopCATrustFn
	oldDesktopAppPath := codexDesktopAppPathFn
	oldDesktopStat := codexDesktopStatFn
	oldDesktopStart := codexDesktopStartFn
	oldDesktopCleanup := codexDesktopCleanupFn
	oldDesktopRunning := codexDesktopRunningFn
	oldDesktopAppServerActive := codexDesktopAppServerActiveFn
	oldDesktopAppServerCount := codexDesktopAppServerCountFn
	oldScopedCLIActiveCount := scopedCodexCLIActiveCountFn
	oldExecutable := osExecutable
	oldDesktopUpstream := codexDesktopUpstreamCodexFn
	oldDesktopSession := codexDesktopSessionFn
	oldDesktopResult := codexDesktopResultFn
	oldDesktopSinceFile := codexDesktopProofSinceFilePathFn
	oldDesktopCapturePath := codexDesktopProofCapturePathFn
	oldDesktopWSSCapture := codexDesktopWSSCaptureFn
	oldDesktopDirect := tuiCodexDesktopDirectFn
	oldTerminalTitle := terminalTitleWriteFn
	codexVersionFn = func() string { return "codex-test" }
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{
			Transport:      codexroute.TransportHTTP,
			FallbackReason: "wss certification missing",
		}
	}
	codexCertSaveFn = func(string, codexroute.CertificationState) error { return nil }
	codexBridgeSaveFn = func(string, codexroute.BridgeProofState) error { return nil }
	codexRecertSaveFn = func(string, codexroute.RecertState) error { return nil }
	codexAutoRecertFn = func(string, string, string, codexroute.AutoDecision) {}
	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, nil
	}
	codexRecertLogFn = func(string, string) {}
	daemonCodexAutoRecertFn = func(int) {}
	daemonAutoRecertAllowedFn = func() bool { return false }
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return passingCodexCertificationState(), nil
	}
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.130.0\n"), nil }
	codexNowFn = func() time.Time { return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC) }
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true}
	}
	appPath := filepath.Join(t.TempDir(), "Codex.app")
	binaryPath := filepath.Join(appPath, defaultCodexDesktopExecRelPath)
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("mkdir fake Codex.app: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake Codex.app binary: %v", err)
	}
	codexDesktopAppPathFn = func() string { return appPath }
	codexDesktopStatFn = os.Stat
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		fmt.Fprintln(p.Out, "Codex.app launched (PID 0) with scoped Slimference env.")
		return 0
	}
	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	codexDesktopUpstreamCodexFn = func(string) (string, error) { return "/tmp/codex", nil }
	codexDesktopCleanupFn = func(int) error { return nil }
	codexDesktopRunningFn = func(string) ([]int, error) { return nil, nil }
	codexDesktopAppServerActiveFn = func() bool { return false }
	codexDesktopAppServerCountFn = func() int { return 0 }
	scopedCodexCLIActiveCountFn = func() int { return 0 }
	sessionPath := filepath.Join(t.TempDir(), "desktop-proof.json")
	codexDesktopSessionFn = func() string { return sessionPath }
	resultPath := filepath.Join(t.TempDir(), "desktop-proof-result.json")
	codexDesktopResultFn = func() string { return resultPath }
	sincePath := filepath.Join(t.TempDir(), "desktop-proof-since.txt")
	codexDesktopProofSinceFilePathFn = func() string { return sincePath }
	capturePath := filepath.Join(t.TempDir(), "desktop-proof.frames.jsonl")
	codexDesktopProofCapturePathFn = func(startedAt time.Time) string { return capturePath }
	codexDesktopWSSCaptureFn = func(string, string, string, bool, time.Duration) error { return nil }
	tuiCodexDesktopDirectFn = func(string) error { return nil }
	t.Cleanup(func() {
		codexRouteHomeFn = oldHome
		codexRouteEnableFn = oldEnable
		codexRouteDisableFn = oldDisable
		codexRouteInspectFn = oldInspect
		codexRouteHealthFn = oldHealth
		codexProxyRunFn = oldProxyRun
		codexVersionFn = oldVersion
		codexAutoFn = oldAuto
		codexCertSaveFn = oldCertSave
		codexBridgeSaveFn = oldBridgeSave
		codexRecertSaveFn = oldRecertSave
		codexAutoRecertFn = oldAutoRecert
		codexRecertTriggerFn = oldRecertTrigger
		codexRecertLogFn = oldRecertLog
		recertRunCommandFn = oldRecertRunCommand
		daemonCodexAutoRecertFn = oldDaemonAutoRecert
		daemonAutoRecertAllowedFn = oldDaemonAutoRecertAllowed
		codexSetupStateFn = oldSetupState
		codexVersionOutFn = oldVersionOut
		codexNowFn = oldNow
		codexDesktopCATrustFn = oldDesktopCA
		codexDesktopAppPathFn = oldDesktopAppPath
		codexDesktopStatFn = oldDesktopStat
		codexDesktopStartFn = oldDesktopStart
		codexDesktopCleanupFn = oldDesktopCleanup
		codexDesktopRunningFn = oldDesktopRunning
		codexDesktopAppServerActiveFn = oldDesktopAppServerActive
		codexDesktopAppServerCountFn = oldDesktopAppServerCount
		scopedCodexCLIActiveCountFn = oldScopedCLIActiveCount
		osExecutable = oldExecutable
		codexDesktopUpstreamCodexFn = oldDesktopUpstream
		codexDesktopSessionFn = oldDesktopSession
		codexDesktopResultFn = oldDesktopResult
		codexDesktopProofSinceFilePathFn = oldDesktopSinceFile
		codexDesktopProofCapturePathFn = oldDesktopCapturePath
		codexDesktopWSSCaptureFn = oldDesktopWSSCapture
		tuiCodexDesktopDirectFn = oldDesktopDirect
		terminalTitleWriteFn = oldTerminalTitle
	})
}

func TestCodexCmdRunUsesProxiedWhenDaemonHealthy(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHealthFn = func(host, port string) error { return nil }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "--", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	joined := strings.Join(got, "\x00")
	for _, want := range []string{"run", "codex", "--proxied", "--host=127.0.0.1", "--port=8990", "exec", "hi"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("proxy args missing %q in %#v", want, got)
		}
	}
}

func TestCodexCmdRunSetsScopedTerminalTitleOnlyWhenProxied(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHealthFn = func(host, port string) error { return nil }
	var titles []string
	terminalTitleWriteFn = func(title string) {
		titles = append(titles, title)
	}
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		if !strings.Contains(strings.Join(args, "\x00"), "--proxied") {
			t.Fatalf("expected proxied run args, got %#v", args)
		}
		return 0
	}
	t.Setenv("CODEX_MODEL", "gpt-5.5")
	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"run", "--transport=http", "--", "exec", "hi"}, p); rc != 0 {
		t.Fatalf("proxied rc=%d stderr=%s", rc, errBuf.String())
	}
	wantActive := scopedCodexTerminalTitle([]string{"exec", "hi"})
	if got := strings.Join(titles, "|"); got != wantActive+"|"+codexTerminalTitleReset {
		t.Fatalf("terminal titles=%q", got)
	}

	titles = nil
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		if !strings.Contains(strings.Join(args, "\x00"), "--direct") {
			t.Fatalf("expected direct run args, got %#v", args)
		}
		return 0
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"run", "--direct", "--", "exec", "hi"}, p); rc != 0 {
		t.Fatalf("direct rc=%d stderr=%s", rc, errBuf.String())
	}
	if len(titles) != 0 {
		t.Fatalf("direct mode must not touch terminal title: %v", titles)
	}
}

func TestScopedCodexTerminalTitleKeepaliveRefreshesUntilRestore(t *testing.T) {
	oldTerminalTitle := terminalTitleWriteFn
	oldInterval := terminalTitleKeepaliveInterval
	t.Cleanup(func() {
		terminalTitleWriteFn = oldTerminalTitle
		terminalTitleKeepaliveInterval = oldInterval
	})

	terminalTitleKeepaliveInterval = time.Millisecond
	t.Setenv("CODEX_MODEL", "gpt-5.5")
	activeTitle := scopedCodexTerminalTitle(nil)
	var mu sync.Mutex
	var titles []string
	activeWrites := 0
	activeTwice := make(chan struct{}, 1)
	terminalTitleWriteFn = func(title string) {
		mu.Lock()
		defer mu.Unlock()
		titles = append(titles, title)
		if title == activeTitle {
			activeWrites++
			if activeWrites >= 2 {
				select {
				case activeTwice <- struct{}{}:
				default:
				}
			}
		}
	}

	restore := setScopedCodexTerminalTitle(nil)
	select {
	case <-activeTwice:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("scoped terminal title was not refreshed")
	}
	restore()
	restore()
	time.Sleep(5 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), titles...)
	mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("titles=%v", got)
	}
	if got[0] != activeTitle || got[len(got)-1] != codexTerminalTitleReset {
		t.Fatalf("titles=%v", got)
	}
	resetCount := 0
	for i, title := range got {
		if title == codexTerminalTitleReset {
			resetCount++
			if i != len(got)-1 {
				t.Fatalf("reset must be final write, titles=%v", got)
			}
		}
	}
	if resetCount != 1 {
		t.Fatalf("reset count=%d titles=%v", resetCount, got)
	}
}

func TestScopedCodexTerminalTitleIncludesCWDAndConfigModel(t *testing.T) {
	oldWD := terminalTitleWorkingDirFn
	oldHome := terminalTitleHomeDirFn
	oldReadFile := terminalTitleReadFileFn
	t.Cleanup(func() {
		terminalTitleWorkingDirFn = oldWD
		terminalTitleHomeDirFn = oldHome
		terminalTitleReadFileFn = oldReadFile
	})

	terminalTitleWorkingDirFn = func() (string, error) { return "/Users/me/CODE/Slimference", nil }
	terminalTitleHomeDirFn = func() (string, error) { return "/Users/me", nil }
	terminalTitleReadFileFn = func(path string) ([]byte, error) {
		if !strings.HasSuffix(path, filepath.Join(".codex", "config.toml")) {
			t.Fatalf("unexpected config path %q", path)
		}
		return []byte("model = \"gpt-5.5\"\nmodel_reasoning_effort = \"medium\"\n[profiles.default]\nmodel = \"ignored\"\n"), nil
	}

	if got := scopedCodexTerminalTitle(nil); got != "[SF] ~/CODE/Slimference | gpt-5.5 medium" {
		t.Fatalf("title=%q", got)
	}
	if got := scopedCodexTerminalTitle([]string{"--model", "gpt-5.5-high"}); got != "[SF] ~/CODE/Slimference | gpt-5.5-high" {
		t.Fatalf("arg title=%q", got)
	}
}

func TestCodexCmdRunUsesScopedWSSWhenRequested(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHealthFn = func(host, port string) error { return nil }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "--transport=wss", "--", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(strings.Join(got, "\x00"), "--proxied-wss") {
		t.Fatalf("expected WSS proxy args, got %#v", got)
	}
}

func TestCodexCmdRunAutoPromotesWSSWhenCertified(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{Mode: codexroute.AutoModeWSSPhaseF, Transport: codexroute.TransportWSS, WSSCertified: true}
	}
	codexRouteHealthFn = func(host, port string) error { return nil }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "--transport=auto", "--", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(strings.Join(got, "\x00"), "--proxied-wss") {
		t.Fatalf("expected WSS proxy args, got %#v", got)
	}
}

func TestCodexCmdRunAutoUsesHTTPWhenOnlyBridgeIsAvailable(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	startedRecert := false
	codexAutoRecertFn = func(string, string, string, codexroute.AutoDecision) {
		startedRecert = true
	}
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{
			Mode:               codexroute.AutoModeHTTP,
			Transport:          codexroute.TransportHTTP,
			WSSBridgeAvailable: true,
			NeedsRecert:        true,
			FallbackReason:     "codex version changed since wss certification; clean WSS bridge proof available; using HTTP savings path until Phase-F recertifies",
			RecertCommand:      "slimference codex recertify wss",
		}
	}
	codexRouteHealthFn = func(host, port string) error { return nil }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "--transport=auto", "--", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	args := strings.Join(got, "\x00")
	if !strings.Contains(args, "--proxied") || strings.Contains(args, "--proxied-wss") {
		t.Fatalf("expected HTTP proxy args, got %#v", got)
	}
	if !startedRecert {
		t.Fatal("auto WSS drift should start background recert")
	}
}

func TestCodexCmdRunAutoHonorsBridgeDecision(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{
			Mode:               codexroute.AutoModeWSSBridge,
			Transport:          codexroute.TransportWSS,
			WSSBridgeAvailable: true,
		}
	}
	codexRouteHealthFn = func(host, port string) error { return nil }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "--transport=auto", "--", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(strings.Join(got, "\x00"), "--proxied-wss-bridge") {
		t.Fatalf("expected WSS bridge proxy args, got %#v", got)
	}
}

func TestCodexCmdRunAutoDoesNotRecertWhenDaemonDown(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	startedRecert := false
	codexAutoRecertFn = func(string, string, string, codexroute.AutoDecision) {
		startedRecert = true
	}
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{
			Mode:               codexroute.AutoModeWSSBridge,
			Transport:          codexroute.TransportWSS,
			WSSBridgeAvailable: true,
			NeedsRecert:        true,
			FallbackReason:     "codex version changed since wss certification",
		}
	}
	codexRouteHealthFn = func(host, port string) error { return errors.New("dial refused") }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "--transport=auto", "--", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if startedRecert {
		t.Fatal("auto recert must not start when the daemon health check already failed")
	}
	if !strings.Contains(strings.Join(got, "\x00"), "--direct") {
		t.Fatalf("expected direct fallback args, got %#v", got)
	}
}

func TestCodexCmdRunFallsBackDirectWhenDaemonDown(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHealthFn = func(host, port string) error { return errors.New("dial refused") }
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"run", "exec", "hi"}, p)
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(strings.Join(got, "\x00"), "--direct") {
		t.Fatalf("expected direct fallback args, got %#v", got)
	}
	if !strings.Contains(errBuf.String(), "falling back to direct Codex") {
		t.Fatalf("missing fallback warning: %q", errBuf.String())
	}
}

func TestCodexCmdRunAutoHomeUnresolvedAndDirectFlag(t *testing.T) {
	withCodexCmdStubs(t)
	var got []string
	codexProxyRunFn = func(args []string, env proxyEnv) int {
		got = append([]string(nil), args...)
		return 0
	}
	codexRouteHomeFn = func() (string, error) { return "", errors.New("no home") }
	codexRouteHealthFn = func(host, port string) error { return nil }
	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"run", "--transport=auto", "--", "exec", "hi"}, p); rc != 0 {
		t.Fatalf("auto rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "HOME unresolved") ||
		!strings.Contains(strings.Join(got, "\x00"), "--proxied") {
		t.Fatalf("auto fallback args=%#v stderr=%q", got, errBuf.String())
	}

	got = nil
	errBuf.Reset()
	codexRouteHealthFn = func(host, port string) error {
		t.Fatalf("health check must not run for --direct")
		return nil
	}
	if rc := runCodexCmd([]string{"run", "--direct", "exec", "hi"}, p); rc != 0 {
		t.Fatalf("direct rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(strings.Join(got, "\x00"), "--direct") {
		t.Fatalf("direct args=%#v", got)
	}
}

func TestCodexCmdEnableWSSDryRun(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"enable", "--transport=wss", "--dry-run"}, p); rc != 0 {
		t.Fatalf("dry-run rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "supports_websockets = true") {
		t.Fatalf("dry-run missing WSS block: %q", out.String())
	}
}

func TestCodexCmdEnableAutoUsesCertificationDecision(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{Transport: codexroute.TransportWSS, WSSCertified: true}
	}
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"enable", "--transport=auto", "--dry-run"}, p); rc != 0 {
		t.Fatalf("dry-run rc=%d stderr=%s", rc, errBuf.String())
	}
	text := out.String()
	if !strings.Contains(text, "Auto transport -> wss (certified)") ||
		!strings.Contains(text, "supports_websockets = true") {
		t.Fatalf("auto WSS dry-run missing detail: %q", text)
	}
}

func TestCodexCmdEnableDisableStatus(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	codexRouteHomeFn = func() (string, error) { return home, nil }
	codexRouteHealthFn = func(host, port string) error { return nil }

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"enable"}, p); rc != 0 {
		t.Fatalf("enable rc=%d stdout=%s stderr=%s", rc, out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "Advanced shared Codex route enabled") ||
		!strings.Contains(out.String(), "ChatGPT.app stay direct") {
		t.Fatalf("bad enable output: %q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"status", "--json"}, p); rc != 0 {
		t.Fatalf("status rc=%d stderr=%s", rc, errBuf.String())
	}
	var got struct {
		Route  codexroute.Status `json:"route"`
		Daemon struct {
			Reachable bool `json:"reachable"`
		} `json:"daemon"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v\n%s", err, out.String())
	}
	if !got.Route.Complete || !got.Daemon.Reachable {
		t.Fatalf("bad status: %+v", got)
	}

	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"disable"}, p); rc != 0 {
		t.Fatalf("disable rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "Normal Codex direct") {
		t.Fatalf("bad disable output: %q", out.String())
	}
}

func TestCodexCmdEnableMissingConfigIsNotSuccess(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"enable"}, p); rc != 1 {
		t.Fatalf("enable rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "does not exist") ||
		!strings.Contains(errBuf.String(), "No files changed") {
		t.Fatalf("bad missing-config output: %q", errBuf.String())
	}
}

func TestCodexCmdDryRunAndErrors(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	codexRouteHealthFn = func(host, port string) error { return errors.New("down") }
	codexProxyRunFn = func(args []string, env proxyEnv) int { return 0 }
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd(nil, p); rc != 0 || !strings.Contains(out.String(), "usage: slimference codex") {
		t.Fatalf("help rc=%d out=%q", rc, out.String())
	}
	out.Reset()
	if rc := runCodexCmd([]string{"enable", "--dry-run", "--host=::1"}, p); rc != 0 {
		t.Fatalf("dry-run rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "http://[::1]:8990/backend-api/codex") {
		t.Fatalf("dry-run missing block: %q", out.String())
	}
	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown subcommand") {
		t.Fatalf("unknown rc=%d stderr=%q", rc, errBuf.String())
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"status", "--bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("bad flag rc=%d stderr=%q", rc, errBuf.String())
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"run", "--transport=bogus"}, p); rc != 2 ||
		!strings.Contains(errBuf.String(), "transport must be auto") {
		t.Fatalf("bad transport rc=%d stderr=%q", rc, errBuf.String())
	}
}

func TestCodexCmdHelpAndErrorBranches(t *testing.T) {
	withCodexCmdStubs(t)
	p, out, errBuf := newTestPrinter()
	for _, args := range [][]string{
		{"--help"},
		{"help"},
		{"run", "--help"},
		{"enable", "--help"},
		{"disable", "--help"},
		{"status", "--help"},
		{"certify", "--help"},
	} {
		out.Reset()
		errBuf.Reset()
		if rc := runCodexCmd(args, p); rc != 0 || !strings.Contains(out.String(), "usage: slimference") {
			t.Fatalf("%v rc=%d out=%q err=%q", args, rc, out.String(), errBuf.String())
		}
	}

	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"enable", "--bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("enable bad flag rc=%d err=%q", rc, errBuf.String())
	}

	codexRouteHomeFn = func() (string, error) { return "", errors.New("no home") }
	for _, args := range [][]string{{"enable"}, {"disable"}, {"status"}} {
		errBuf.Reset()
		if rc := runCodexCmd(args, p); rc != 1 || !strings.Contains(errBuf.String(), "HOME unresolved") {
			t.Fatalf("%v rc=%d err=%q", args, rc, errBuf.String())
		}
	}

	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	codexRouteEnableFn = func(string, string, codexroute.Options) (codexroute.Event, error) {
		return codexroute.Event{}, errors.New("enable failed")
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"enable"}, p); rc != 1 || !strings.Contains(errBuf.String(), "enable failed") {
		t.Fatalf("enable error rc=%d err=%q", rc, errBuf.String())
	}

	codexRouteDisableFn = func(string) (codexroute.Event, error) {
		return codexroute.Event{}, errors.New("disable failed")
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"disable"}, p); rc != 1 || !strings.Contains(errBuf.String(), "disable failed") {
		t.Fatalf("disable error rc=%d err=%q", rc, errBuf.String())
	}

	codexRouteInspectFn = func(string, string, codexroute.Options) (codexroute.Status, error) {
		return codexroute.Status{}, errors.New("inspect failed")
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"status"}, p); rc != 1 || !strings.Contains(errBuf.String(), "inspect failed") {
		t.Fatalf("status error rc=%d err=%q", rc, errBuf.String())
	}

	out.Reset()
	if rc := runCodexCmd([]string{"disable", "--dry-run"}, p); rc != 0 ||
		!strings.Contains(out.String(), "would remove advanced shared Codex route") {
		t.Fatalf("disable dry-run rc=%d out=%q", rc, out.String())
	}
}

func TestRunCodexCertifyWSSHappyPath(t *testing.T) {
	withCodexCmdStubs(t)
	home := t.TempDir()
	codexRouteHomeFn = func() (string, error) { return home, nil }
	var savedHome string
	var saved codexroute.CertificationState
	var saveCalled int
	codexCertSaveFn = func(gotHome string, state codexroute.CertificationState) error {
		saveCalled++
		savedHome = gotHome
		saved = state
		return nil
	}
	p, out, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"certify", "wss", "--operator", "opus-verify", "--notes=T226 issue"}, p)
	if rc != 0 {
		t.Fatalf("certify rc=%d stderr=%s", rc, errBuf.String())
	}
	if saveCalled != 1 || savedHome != home {
		t.Fatalf("saveCalled=%d savedHome=%q want %q", saveCalled, savedHome, home)
	}
	if saved.SchemaVersion != codexroute.CertificationSchemaVersion ||
		saved.Transport != string(codexroute.TransportWSS) ||
		saved.RouteProfile != codexroute.RouteProfileScopedRawWSS ||
		saved.CodexVersion != "0.130.0" ||
		saved.SlimferenceVersion != version ||
		!saved.Passed ||
		saved.FramesReencoded != 7 ||
		saved.DegradedSessions != 0 ||
		saved.ParseFailures != 0 ||
		!saved.Timestamp.Equal(codexNowFn().UTC()) ||
		saved.Operator != "opus-verify" ||
		saved.Notes != "T226 issue" {
		t.Fatalf("bad certification state: %+v", saved)
	}
	if !strings.Contains(out.String(), "Codex WSS certification written") ||
		!strings.Contains(out.String(), "Live frames_reencoded at issue: 7") {
		t.Fatalf("bad output: %q", out.String())
	}
}

func TestRunCodexCertifyWSSFailsOnEachCriterion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*control.SetupState)
		criterion string
		value     string
		threshold string
	}{
		{
			name: "parse failures",
			mutate: func(s *control.SetupState) {
				s.WSS.ParseFailures = 1
			},
			criterion: "wss.parse_failures", value: "got=1", threshold: "want=0",
		},
		{
			name: "degraded sessions",
			mutate: func(s *control.SetupState) {
				s.WSS.DegradedSessions = 1
			},
			criterion: "wss.degraded_sessions", value: "got=1", threshold: "want=0",
		},
		{
			name: "compression errors",
			mutate: func(s *control.SetupState) {
				s.WSS.CompressionErrors = 1
			},
			criterion: "wss.compression_errors", value: "got=1", threshold: "want=0",
		},
		{
			name: "frames reencoded",
			mutate: func(s *control.SetupState) {
				s.WSS.FramesReencoded = 0
			},
			criterion: "wss.frames_reencoded", value: "got=0", threshold: "want=>0",
		},
		{
			name: "compressed messages mutated",
			mutate: func(s *control.SetupState) {
				s.WSS.CompressedMessagesMutated = 0
			},
			criterion: "wss.compressed_messages_mutated", value: "got=0", threshold: "want=>0",
		},
		{
			name: "mutation active",
			mutate: func(s *control.SetupState) {
				s.WSS.MutationActive = false
			},
			criterion: "wss.mutation_active", value: "got=false", threshold: "want=true",
		},
		{
			name: "byte bridge only",
			mutate: func(s *control.SetupState) {
				s.WSS.ByteBridgeOnly = true
			},
			criterion: "wss.byte_bridge_only", value: "got=true", threshold: "want=false",
		},
		{
			name: "daemon reachable",
			mutate: func(s *control.SetupState) {
				s.CodexRoute.DaemonReachable = false
			},
			criterion: "codex_route.daemon_reachable", value: "got=false", threshold: "want=true",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCodexCmdStubs(t)
			codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
			state := passingCodexCertificationState()
			tc.mutate(&state)
			codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
				return state, nil
			}
			saveCalled := false
			codexCertSaveFn = func(string, codexroute.CertificationState) error {
				saveCalled = true
				return nil
			}
			p, _, errBuf := newTestPrinter()
			rc := runCodexCmd([]string{"certify", "wss"}, p)
			errText := errBuf.String()
			if rc != 1 || saveCalled {
				t.Fatalf("rc=%d saveCalled=%v stderr=%s", rc, saveCalled, errText)
			}
			for _, want := range []string{tc.criterion, tc.value, tc.threshold} {
				if !strings.Contains(errText, want) {
					t.Fatalf("stderr missing %q: %s", want, errText)
				}
			}
		})
	}
}

func TestRunCodexCertifyWSSDryRunDoesNotWrite(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	codexCertSaveFn = func(string, codexroute.CertificationState) error {
		t.Fatalf("dry-run must not write certification")
		return nil
	}
	p, out, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"certify", "wss", "--dry-run", "--operator=dry", "--notes", "no write"}, p)
	if rc != 0 {
		t.Fatalf("dry-run rc=%d stderr=%s", rc, errBuf.String())
	}
	var got codexroute.CertificationState
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode dry-run JSON: %v\n%s", err, out.String())
	}
	if got.Transport != string(codexroute.TransportWSS) || got.FramesReencoded != 7 ||
		got.Operator != "dry" || got.Notes != "no write" {
		t.Fatalf("bad dry-run cert: %+v", got)
	}
}

func TestRunCodexCertifyWSSDryRunWriterError(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	errBuf := &bytes.Buffer{}
	p := installPrinter{Out: codexErrWriter{}, Err: errBuf}
	if rc := runCodexCmd([]string{"certify", "wss", "--dry-run"}, p); rc != 1 ||
		!strings.Contains(errBuf.String(), "encode dry-run JSON") {
		t.Fatalf("rc/stderr=%d %q", rc, errBuf.String())
	}
}

func TestRunCodexCertifyWSSRejectsNonWSSSubject(t *testing.T) {
	withCodexCmdStubs(t)
	p, _, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"certify", "http"}, p)
	if rc != 2 || !strings.Contains(errBuf.String(), "subject must be wss") {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
}

func TestRunCodexCertifyErrorsBeforeWrite(t *testing.T) {
	t.Run("home unresolved", func(t *testing.T) {
		withCodexCmdStubs(t)
		codexRouteHomeFn = func() (string, error) { return "", errors.New("no home") }
		p, _, errBuf := newTestPrinter()
		if rc := runCodexCmd([]string{"certify", "wss"}, p); rc != 1 ||
			!strings.Contains(errBuf.String(), "HOME unresolved") {
			t.Fatalf("rc/stderr=%d %q", rc, errBuf.String())
		}
	})
	t.Run("codex version command fails", func(t *testing.T) {
		withCodexCmdStubs(t)
		codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
		codexVersionOutFn = func() ([]byte, error) { return nil, errors.New("missing codex") }
		p, _, errBuf := newTestPrinter()
		if rc := runCodexCmd([]string{"certify", "wss"}, p); rc != 1 ||
			!strings.Contains(errBuf.String(), "codex --version failed") {
			t.Fatalf("rc/stderr=%d %q", rc, errBuf.String())
		}
	})
	t.Run("codex version parse fails", func(t *testing.T) {
		withCodexCmdStubs(t)
		codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
		codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli\n"), nil }
		p, _, errBuf := newTestPrinter()
		if rc := runCodexCmd([]string{"certify", "wss"}, p); rc != 1 ||
			!strings.Contains(errBuf.String(), "unexpected codex --version output") {
			t.Fatalf("rc/stderr=%d %q", rc, errBuf.String())
		}
	})
	t.Run("admin state fails", func(t *testing.T) {
		withCodexCmdStubs(t)
		codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
		codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
			return control.SetupState{}, errors.New("daemon down")
		}
		p, _, errBuf := newTestPrinter()
		if rc := runCodexCmd([]string{"certify", "wss"}, p); rc != 1 ||
			!strings.Contains(errBuf.String(), "admin state unavailable") {
			t.Fatalf("rc/stderr=%d %q", rc, errBuf.String())
		}
	})
	t.Run("save fails", func(t *testing.T) {
		withCodexCmdStubs(t)
		codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
		codexCertSaveFn = func(string, codexroute.CertificationState) error {
			return errors.New("disk full")
		}
		p, _, errBuf := newTestPrinter()
		if rc := runCodexCmd([]string{"certify", "wss"}, p); rc != 1 ||
			!strings.Contains(errBuf.String(), "disk full") {
			t.Fatalf("rc/stderr=%d %q", rc, errBuf.String())
		}
	})
}

func TestRunCodexCertifyParsesHostAndPort(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return t.TempDir(), nil }
	var gotHost, gotPort string
	codexSetupStateFn = func(host, port string, timeout time.Duration) (control.SetupState, error) {
		gotHost, gotPort = host, port
		return passingCodexCertificationState(), nil
	}
	p, _, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"certify", "wss", "--host=::1", "--port=19090", "--dry-run"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errBuf.String())
	}
	if gotHost != "::1" || gotPort != "19090" {
		t.Fatalf("host/port=%q/%q", gotHost, gotPort)
	}
}

func TestParseCodexCLIVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		out     string
		want    string
		wantErr bool
	}{
		{name: "normal", out: "codex-cli 0.130.0\n", want: "0.130.0"},
		{name: "build metadata", out: "codex-cli 0.130.0+abcd extra\n", want: "0.130.0+abcd"},
		{name: "leading blank", out: "\n codex 0.131.0 \n", want: "0.131.0"},
		{name: "empty", out: "", wantErr: true},
		{name: "garbage", out: "codex-cli\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCodexCLIVersion([]byte(tc.out))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("parse=%q err=%v want=%q", got, err, tc.want)
			}
		})
	}
}

func TestCurrentCodexVersionUsesParsedCLIOutput(t *testing.T) {
	withCodexCmdStubs(t)
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.130.0\n"), nil }
	if got := currentCodexVersion(); got != "0.130.0" {
		t.Fatalf("currentCodexVersion=%q", got)
	}
	codexVersionOutFn = func() ([]byte, error) { return nil, errors.New("missing") }
	if got := currentCodexVersion(); got != "unknown" {
		t.Fatalf("currentCodexVersion on command error=%q", got)
	}
	codexVersionOutFn = func() ([]byte, error) { return []byte("garbage\n"), nil }
	if got := currentCodexVersion(); got != "unknown" {
		t.Fatalf("currentCodexVersion on parse error=%q", got)
	}
}

func TestCodexCmdAdditionalDispatchAndParseErrors(t *testing.T) {
	withCodexCmdStubs(t)
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"launch-desktop", "--help"}, p); rc != 0 {
		t.Fatalf("launch-desktop help rc=%d stderr=%s", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "launch-desktop") {
		t.Fatalf("missing launcher help: %q", out.String())
	}
	out.Reset()
	errBuf.Reset()
	if rc := runCodexCmd([]string{"certify", "wss", "--unknown"}, p); rc != 2 {
		t.Fatalf("certify bad flag rc=%d", rc)
	}
	if !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("missing bad flag error: %q", errBuf.String())
	}
	if _, err := parseCodexRouteFlags([]string{"--transport=wss-bridge"}); err == nil {
		t.Fatal("codex route flags must reject wss-bridge outside run-internal transport")
	}
}

func TestParseCodexCertifyFlagsRejectsBadShapes(t *testing.T) {
	for _, args := range [][]string{
		{"wss", "extra"},
		{"wss", "--unknown"},
		{"wss", "--operator"},
		{"wss", "--notes"},
	} {
		if _, err := parseCodexCertifyFlags(args); err == nil {
			t.Fatalf("parseCodexCertifyFlags(%v) expected error", args)
		}
	}
}

func TestFetchCodexSetupState(t *testing.T) {
	state := passingCodexCertificationState()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != proxy.AdminStatePath {
			t.Fatalf("path=%q want %q", r.URL.Path, proxy.AdminStatePath)
		}
		if err := json.NewEncoder(w).Encode(state); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer server.Close()
	host, port := splitHTTPTestServer(t, server)
	got, err := fetchCodexSetupState(host, port, time.Second)
	if err != nil {
		t.Fatalf("fetchCodexSetupState: %v", err)
	}
	if !got.CodexRoute.DaemonReachable || got.WSS.FramesReencoded != state.WSS.FramesReencoded {
		t.Fatalf("bad state: %+v", got)
	}
}

func TestFetchCodexSetupStateErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "non ok",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusTeapot)
			},
			want: "admin returned 418",
		},
		{
			name: "bad json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("{bad"))
			},
			want: "invalid character",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			host, port := splitHTTPTestServer(t, server)
			_, err := fetchCodexSetupState(host, port, time.Second)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want contains %q", err, tc.want)
			}
		})
	}
}

func TestFetchCodexSetupStateRequestAndDialErrors(t *testing.T) {
	if _, err := fetchCodexSetupState("bad host\n", "8990", time.Second); err == nil {
		t.Fatalf("expected bad host request error")
	}
	if _, err := fetchCodexSetupState("127.0.0.1", "1", 50*time.Millisecond); err == nil {
		t.Fatalf("expected dial error")
	}
}

func TestCodexStatusHumanBranches(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	p, out, _ := newTestPrinter()

	codexRouteInspectFn = func(string, string, codexroute.Options) (codexroute.Status, error) {
		return codexroute.Status{Exists: true, Enabled: true, Complete: true, Transport: "wss", BaseURL: "http://127.0.0.1:8990/backend-api/codex"}, nil
	}
	codexRouteHealthFn = func(string, string) error { return nil }
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "route is ready") ||
		!strings.Contains(out.String(), "Transport wss") {
		t.Fatalf("ready status rc=%d out=%q", rc, out.String())
	}

	out.Reset()
	codexRouteHealthFn = func(string, string) error { return errors.New("down") }
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "daemon is unreachable") {
		t.Fatalf("down status rc=%d out=%q", rc, out.String())
	}

	out.Reset()
	codexRouteInspectFn = func(string, string, codexroute.Options) (codexroute.Status, error) {
		return codexroute.Status{
			Exists:     true,
			Enabled:    false,
			Complete:   false,
			Conflict:   "top-level model_provider already set",
			LegacyKeys: true,
			BaseURL:    "http://127.0.0.1:8990/backend-api/codex",
		}, nil
	}
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "Normal Codex direct") ||
		!strings.Contains(out.String(), "Conflict top-level model_provider") ||
		!strings.Contains(out.String(), "Legacy") {
		t.Fatalf("disabled status rc=%d out=%q", rc, out.String())
	}

	out.Reset()
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{
			Transport:            codexroute.TransportHTTP,
			NeedsRecert:          true,
			CurrentCodex:         "0.131.0",
			CurrentSlimference:   "0.6.0",
			CertifiedCodex:       "0.130.0",
			CertifiedSlimference: "0.6.0",
			FallbackReason:       "codex version changed since wss certification",
			RecertCommand:        "slimference codex recertify wss",
		}
	}
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "current codex=0.131.0 slimference=0.6.0") ||
		!strings.Contains(out.String(), "certified codex=0.130.0 slimference=0.6.0") ||
		!strings.Contains(out.String(), "WSS savings repair needed") ||
		!strings.Contains(out.String(), "slimference codex recertify wss") {
		t.Fatalf("recert status rc=%d out=%q", rc, out.String())
	}
}

func TestCodexStatusJSONIncludesRecertState(t *testing.T) {
	withCodexCmdStubs(t)
	codexRouteHomeFn = func() (string, error) { return "/tmp/home", nil }
	codexRouteInspectFn = func(string, string, codexroute.Options) (codexroute.Status, error) {
		return codexroute.Status{Exists: true, BaseURL: "http://127.0.0.1:8990/backend-api/codex"}, nil
	}
	codexRouteHealthFn = func(string, string) error { return nil }
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{
			Transport:            codexroute.TransportHTTP,
			NeedsRecert:          true,
			CurrentCodex:         "0.131.0",
			CurrentSlimference:   "0.6.0",
			CertifiedCodex:       "0.130.0",
			CertifiedSlimference: "0.6.0",
			CertificationPath:    "/tmp/home/.slimference/codex-wss-cert.json",
			FallbackReason:       "codex version changed since wss certification",
			RecertCommand:        "slimference codex recertify wss",
		}
	}

	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"status", "--json"}, p); rc != 0 {
		t.Fatalf("status rc=%d stderr=%s", rc, errBuf.String())
	}
	var got struct {
		Auto codexroute.AutoDecision `json:"auto"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v\n%s", err, out.String())
	}
	if !got.Auto.NeedsRecert ||
		got.Auto.CurrentCodex != "0.131.0" ||
		got.Auto.CertifiedCodex != "0.130.0" ||
		got.Auto.RecertCommand != "slimference codex recertify wss" {
		t.Fatalf("bad auto recert state: %+v", got.Auto)
	}
}

func TestCodexDesktopStatusJSONReadyForLiveProbe(t *testing.T) {
	withCodexCmdStubs(t)
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		state := passingCodexCertificationState()
		state.WSS.MITMBridged = 0
		state.WSS.CompressedMessagesInspected = 0
		return state, nil
	}
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "ready_for_live_desktop_probe" || got.FailureClass != "" || !got.LiveProofRequired {
		t.Fatalf("status=%+v", got)
	}
	if got.LaunchCommand != "slimference codex launch-desktop --transport=app-server" {
		t.Fatalf("launch command=%q", got.LaunchCommand)
	}
	if !got.CATrust.Trusted || !got.DaemonReachable {
		t.Fatalf("readiness not propagated: %+v", got)
	}
}

func TestCodexDesktopStatusDoesNotApplyOldProofToDifferentRunningApp(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:           "desktop_app_server_phasef_proven",
		Transport:      codexDesktopTransportAppServer,
		LaunchPID:      111,
		DesktopProven:  true,
		DesktopSavings: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{222}, nil
	}
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "codex_desktop_already_running" ||
		got.FailureClass != "codex_desktop_already_running" ||
		got.ConversationObserved ||
		!got.LiveProofRequired ||
		!strings.Contains(strings.Join(got.Notes, "\n"), "PID 222") {
		t.Fatalf("stale proof status must not greenlight a different running app: %+v", got)
	}
}

func TestCodexDesktopStatusAllowsLastProofForSameRunningApp(t *testing.T) {
	withCodexCmdStubs(t)
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:           "desktop_app_server_phasef_proven",
		Transport:      codexDesktopTransportAppServer,
		LaunchPID:      111,
		DesktopProven:  true,
		DesktopSavings: true,
	})
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{111}, nil
	}
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var got codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "desktop_app_server_proven" || !got.ConversationObserved || got.LiveProofRequired {
		t.Fatalf("same running scoped app should keep proof status: %+v", got)
	}
}

func TestCodexDesktopStatusTreatsWSSCountersAsDaemonWideNotDesktopProof(t *testing.T) {
	withCodexCmdStubs(t)
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		state := passingCodexCertificationState()
		state.WSS.MITMBridged = 2
		state.WSS.CompressedMessagesInspected = 9
		return state, nil
	}
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "ready_for_live_desktop_probe") ||
		!strings.Contains(out.String(), "conversation_observed=false") ||
		!strings.Contains(out.String(), "scope=daemon_cumulative_not_desktop_proof") ||
		!strings.Contains(out.String(), "pre/post delta tied to the spawned Codex.app process") {
		t.Fatalf("human status missing daemon-wide counter warning: %q", out.String())
	}
}

func TestCodexDesktopStatusReportsGates(t *testing.T) {
	withCodexCmdStubs(t)
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: false, Trusted: false}
	}
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if strings.Contains(out.String(), "ca_missing") || !strings.Contains(out.String(), "local CA is absent; not required") {
		t.Fatalf("status should not gate app-server route on CA: %q", out.String())
	}

	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return control.SetupState{}, errors.New("down")
	}
	out.Reset()
	if rc := runCodexCmd([]string{"desktop", "status"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if !strings.Contains(out.String(), "daemon_unreachable") {
		t.Fatalf("status missing daemon gate: %q", out.String())
	}
}

func TestCodexDesktopStatusAllowsUntrustedKeychainWithCAEnvAndReportsWSSErrors(t *testing.T) {
	withCodexCmdStubs(t)
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: false}
	}
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop", "status", "--json", "--host=127.0.0.2", "--port=19090"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var untrusted codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &untrusted); err != nil {
		t.Fatalf("json: %v", err)
	}
	if untrusted.FailureClass != "" ||
		untrusted.Mode != "ready_for_live_desktop_probe" ||
		untrusted.ProxyURL != "http://127.0.0.2:19090" ||
		untrusted.LaunchCommand != "slimference codex launch-desktop --transport=app-server" {
		t.Fatalf("untrusted status=%+v", untrusted)
	}
	if !strings.Contains(strings.Join(untrusted.Notes, "\n"), "Keychain trust is not required") {
		t.Fatalf("untrusted notes do not explain app-server route: %+v", untrusted.Notes)
	}

	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true}
	}
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		state := passingCodexCertificationState()
		state.WSS.MITMBridged = 1
		state.WSS.CompressedMessagesInspected = 1
		state.WSS.ParseFailures = 1
		return state, nil
	}
	out.Reset()
	if rc := runCodexCmd([]string{"desktop", "status", "--json"}, p); rc != 0 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var wssErr codexDesktopStatusOutput
	if err := json.Unmarshal(out.Bytes(), &wssErr); err != nil {
		t.Fatalf("json: %v", err)
	}
	if wssErr.Mode != "ready_for_live_desktop_probe" || wssErr.FailureClass != "" || wssErr.ConversationObserved {
		t.Fatalf("wss error status=%+v", wssErr)
	}
	if wssErr.WSSCountersScope != "daemon_cumulative_not_desktop_proof" ||
		!strings.Contains(strings.Join(wssErr.Notes, "\n"), "daemon-wide") {
		t.Fatalf("wss error notes/scope not explicit: %+v", wssErr)
	}
}

func TestCodexDesktopStatusDoesNotTreatLegacyCONNECTCountersAsDesktopProof(t *testing.T) {
	withCodexCmdStubs(t)
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		state := passingCodexCertificationState()
		state.WSS.MITMBridged = 14
		state.WSS.UpstreamDialFail = 0
		state.WSS.BytesC2S = 0
		state.WSS.BytesS2C = 0
		state.WSS.C2SFrames = 0
		state.WSS.S2CFrames = 0
		state.WSS.CompressedMessagesInspected = 0
		state.WSS.CompressedMessagesMutated = 0
		state.WSS.FramesReencoded = 0
		state.WSS.MutationActive = false
		return state, nil
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
		t.Fatalf("status=%+v", got)
	}
	if got.ConversationObserved {
		t.Fatalf("zero-byte CONNECT attempts must not count as observed conversation: %+v", got)
	}
	if !strings.Contains(strings.Join(got.Notes, "\n"), "daemon-wide") {
		t.Fatalf("notes do not mark counters as daemon-wide: %+v", got.Notes)
	}
}

func TestCodexDesktopProveRejectsConnectOnlyDelta(t *testing.T) {
	withCodexCmdStubs(t)
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		state := control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}
		if calls == 2 {
			state.WSS.MITMBridged = 14
		}
		return state, nil
	}

	p, out, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"desktop", "prove", "--json", "--duration=1ns"}, p)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	var got codexDesktopProofOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.Mode != "desktop_connect_only_no_app_server_bytes" ||
		got.FailureClass != "connect_only_no_app_server_bytes" ||
		got.DeltaWSS.MITMBridged != 14 ||
		got.DesktopProven ||
		got.DesktopSavings {
		t.Fatalf("proof=%+v", got)
	}
}

func TestCodexDesktopProveDoesNotReplaceRunningAppWithoutOptIn(t *testing.T) {
	withCodexCmdStubs(t)
	codexDesktopRunningFn = func(string) ([]int, error) {
		return []int{1234}, nil
	}
	startCalled := false
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		startCalled = true
		return 0
	}

	p, out, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"desktop", "prove", "--json", "--duration=1ns"}, p)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q", rc, errBuf.String())
	}
	if startCalled {
		t.Fatal("desktop proof must not spawn or replace Codex.app without explicit opt-in")
	}
	var got codexDesktopProofOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if got.FailureClass != "codex_desktop_already_running" ||
		!strings.Contains(strings.Join(got.Notes, "\n"), "--replace-existing") {
		t.Fatalf("proof=%+v", got)
	}
}

func TestCodexDesktopProvePhaseFPassesAndBridgeStaysNonSavings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		after    control.WSSState
		wantRC   int
		wantMode string
		savings  bool
	}{
		{
			name: "phasef",
			after: control.WSSState{
				MITMBridged:               1,
				BytesC2S:                  100,
				BytesS2C:                  200,
				C2SFrames:                 2,
				S2CFrames:                 3,
				FramesReencoded:           1,
				CompressedMessagesMutated: 1,
			},
			wantRC:   0,
			wantMode: "desktop_app_server_phasef_proven",
			savings:  true,
		},
		{
			name: "bridge",
			after: control.WSSState{
				MITMBridged:     1,
				BytesC2S:        100,
				BytesS2C:        200,
				C2SFrames:       2,
				S2CFrames:       3,
				FramesForwarded: 5,
			},
			wantRC:   1,
			wantMode: "desktop_app_server_wss_bridge",
			savings:  false,
		},
		{
			// phasefBridged>0 with mutation: full green via the reliable counter.
			name: "phasef_via_phasefbridged",
			after: control.WSSState{
				MITMBridged:               1,
				PhasefBridged:             1,
				BytesC2S:                  100,
				BytesS2C:                  200,
				FramesReencoded:           1,
				CompressedMessagesMutated: 1,
			},
			wantRC:   0,
			wantMode: "desktop_app_server_phasef_proven",
			savings:  true,
		},
		{
			// phasefBridged>0, zero errors, no mutation: the conversation reached
			// the Phase-F savings route (launch-eligible) even though a trivial
			// turn had nothing to mutate. This is the reliable, lag-free verdict.
			name: "route_proven_no_mutation",
			after: control.WSSState{
				MITMBridged:     1,
				PhasefBridged:   1,
				FramesForwarded: 5,
			},
			wantRC:   1,
			wantMode: "desktop_app_server_route_proven",
			savings:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCodexCmdStubs(t)
			calls := 0
			codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
				calls++
				state := control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}
				if calls == 2 {
					state.WSS = tc.after
				}
				return state, nil
			}

			p, out, errBuf := newTestPrinter()
			rc := runCodexCmd([]string{"desktop", "prove", "--json", "--duration=1ns", "--keep-open"}, p)
			if rc != tc.wantRC {
				t.Fatalf("rc=%d want %d stderr=%q", rc, tc.wantRC, errBuf.String())
			}
			var got codexDesktopProofOutput
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("json: %v\nraw=%s", err, out.String())
			}
			if got.Mode != tc.wantMode || got.DesktopSavings != tc.savings || !got.DesktopProven {
				t.Fatalf("proof=%+v", got)
			}
		})
	}
}

func TestApplyCodexDesktopLastProofRouteProvenIsLaunchableButDistinct(t *testing.T) {
	out := &codexDesktopStatusOutput{}
	applyCodexDesktopLastProof(out, &codexDesktopProofOutput{
		Transport: codexDesktopTransportAppServer,
		Mode:      "desktop_app_server_route_proven",
	})
	// Launch-eligible (no failure, conversation observed) but NOT the savings-proven
	// mode: route-ready must stay distinct from desktop_app_server_proven so the TUI
	// never sells "route ready" as "savings proven".
	if out.Mode != "desktop_app_server_route_ready" || out.FailureClass != "" || !out.ConversationObserved {
		t.Fatalf("route-proven must map to launchable-but-distinct route_ready: %+v", out)
	}
	if out.Mode == "desktop_app_server_proven" {
		t.Fatal("route-ready must not be conflated with savings-proven")
	}
}

func TestClassifyCodexDesktopProofIgnoresPhasefBridgedWithErrors(t *testing.T) {
	// A phasef session that also recorded parser errors must not be treated as
	// route-proven; it falls through to the lower verdicts.
	out := &codexDesktopProofOutput{DeltaWSS: control.WSSState{
		PhasefBridged: 1, MITMBridged: 1, FramesForwarded: 5, ParseFailures: 2,
	}}
	classifyCodexDesktopProof(out, false)
	if out.Mode == "desktop_app_server_route_proven" || out.Mode == "desktop_app_server_phasef_proven" {
		t.Fatalf("errored phasef session must not be proven: %+v", out)
	}
}

func TestCodexDesktopProveManualSessionAndFinish(t *testing.T) {
	withCodexCmdStubs(t)
	capturePath := filepath.Join(t.TempDir(), "frames.jsonl")
	matrixPath := filepath.Join(t.TempDir(), "matrix.jsonl")
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		state := control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}
		if calls >= 3 {
			state.WSS.MITMBridged = 1
			state.WSS.BytesC2S = 100
			state.WSS.BytesS2C = 200
			state.WSS.FramesReencoded = 1
			state.WSS.CompressedMessagesMutated = 1
			state.WSS.MutationActive = true
		}
		return state, nil
	}
	var launchedEnv []string
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		launchedEnv = append([]string(nil), env...)
		fmt.Fprintln(p.Out, "Codex.app launched (PID 4242) with scoped Slimference env.")
		return 0
	}
	type captureCall struct {
		path    string
		enabled bool
	}
	var captureCalls []captureCall
	codexDesktopWSSCaptureFn = func(_ string, _ string, path string, enabled bool, _ time.Duration) error {
		captureCalls = append(captureCalls, captureCall{path: path, enabled: enabled})
		return nil
	}

	p, out, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"desktop", "prove", "--manual", "--json", "--duration=1ns", "--capture=" + capturePath, "--matrix-row=" + matrixPath}, p)
	if rc != 0 {
		t.Fatalf("manual rc=%d stderr=%q out=%q", rc, errBuf.String(), out.String())
	}
	var started codexDesktopProofOutput
	if err := json.Unmarshal(out.Bytes(), &started); err != nil {
		t.Fatalf("manual json: %v\nraw=%s", err, out.String())
	}
	if started.Mode != "desktop_ready_for_prompt" || !started.LaunchReady || started.DesktopSavings || started.SessionPath == "" {
		t.Fatalf("manual proof=%+v", started)
	}
	if started.CapturePath != capturePath || started.MatrixPath != matrixPath {
		t.Fatalf("manual proof lost capture/matrix paths: %+v", started)
	}
	joinedEnv := strings.Join(launchedEnv, "\n")
	if strings.Contains(joinedEnv, "SLIMFERENCE_WSS_AB_CAPTURE=") {
		t.Fatalf("manual proof must not put daemon capture path in Codex.app env: %v", launchedEnv)
	}
	if len(captureCalls) != 1 || !captureCalls[0].enabled || captureCalls[0].path != capturePath {
		t.Fatalf("manual proof did not arm daemon capture exactly once: %+v", captureCalls)
	}
	for _, want := range []string{
		"search-cap-proof --frames " + capturePath,
		"wss-proof-live-row --matrix-row " + matrixPath + " --frames " + capturePath,
		"wss-proof-matrix " + matrixPath,
	} {
		if !strings.Contains(started.SearchCapProofCommand+"\n"+started.MatrixRowCommand+"\n"+started.FocusedMatrixCommand, want) {
			t.Fatalf("manual proof missing command fragment %q: %+v", want, started)
		}
	}

	out.Reset()
	errBuf.Reset()
	rc = runCodexCmd([]string{"desktop", "prove", "--finish", "--json"}, p)
	if rc != 0 {
		t.Fatalf("finish rc=%d stderr=%q out=%q", rc, errBuf.String(), out.String())
	}
	var finished codexDesktopProofOutput
	if err := json.Unmarshal(out.Bytes(), &finished); err != nil {
		t.Fatalf("finish json: %v\nraw=%s", err, out.String())
	}
	if finished.Mode != "desktop_app_server_phasef_proven" || !finished.DesktopSavings || finished.LaunchPID != 4242 {
		t.Fatalf("finish proof=%+v", finished)
	}
	if finished.CapturePath != capturePath || finished.MatrixPath != matrixPath ||
		!strings.Contains(finished.SearchCapProofCommand, "search-cap-proof --frames "+capturePath) ||
		!strings.Contains(finished.FocusedMatrixCommand, "wss-proof-matrix "+matrixPath) {
		t.Fatalf("finish proof lost capture handoff: %+v", finished)
	}
	if len(captureCalls) != 2 || captureCalls[1].enabled || captureCalls[1].path != "" {
		t.Fatalf("finish proof did not disarm daemon capture: %+v", captureCalls)
	}
}

func TestCodexDesktopProveManualPostProbeFailureCleansLaunchedApp(t *testing.T) {
	withCodexCmdStubs(t)
	calls := 0
	var postProbeTimeout time.Duration
	codexSetupStateFn = func(host string, port string, timeout time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		postProbeTimeout = timeout
		return control.SetupState{}, errors.New("admin state timeout")
	}
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		fmt.Fprintln(p.Out, "Codex.app launched (PID 5151) with scoped Slimference env.")
		return 0
	}
	var captureCalls []bool
	codexDesktopWSSCaptureFn = func(_ string, _ string, _ string, enabled bool, _ time.Duration) error {
		captureCalls = append(captureCalls, enabled)
		return nil
	}
	cleanupPID := 0
	codexDesktopCleanupFn = func(pid int) error {
		cleanupPID = pid
		return nil
	}

	p, out, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"desktop", "prove", "--manual", "--json", "--duration=1ns"}, p)
	if rc != 1 {
		t.Fatalf("manual post-probe rc=%d stderr=%q out=%q", rc, errBuf.String(), out.String())
	}
	var proof codexDesktopProofOutput
	if err := json.Unmarshal(out.Bytes(), &proof); err != nil {
		t.Fatalf("manual post-probe json: %v\nraw=%s", err, out.String())
	}
	if proof.Mode != "post_probe_failed" || proof.FailureClass != "post_probe_failed" {
		t.Fatalf("manual post-probe should report failure: %+v", proof)
	}
	if !proof.CleanupAttempted || cleanupPID != 5151 {
		t.Fatalf("manual post-probe must clean launched app: cleanup=%v pid=%d proof=%+v", proof.CleanupAttempted, cleanupPID, proof)
	}
	if proof.CapturePath == "" || len(captureCalls) != 2 || !captureCalls[0] || captureCalls[1] {
		t.Fatalf("manual default capture was not armed then disarmed: capture=%q calls=%v", proof.CapturePath, captureCalls)
	}
	if postProbeTimeout < 10*time.Second {
		t.Fatalf("post-probe timeout too short: %s", postProbeTimeout)
	}
	if proof.LaunchReady || proof.SessionPath != "" || proof.DesktopProven || proof.DesktopSavings {
		t.Fatalf("failed post-probe must not leave a launch-ready proof session: %+v", proof)
	}
}

func TestCodexDesktopProofCaptureHelpersAndHumanRender(t *testing.T) {
	oldHome := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = oldHome })
	osUserHomeDir = func() (string, error) { return "/Users/proof", nil }

	startedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	wantCapture := filepath.Join("/Users/proof", ".slimference", "captures", "codex-desktop-proof-20260518T120000Z", "frames.jsonl")
	if got := codexDesktopDefaultProofCapturePath(startedAt); got != wantCapture {
		t.Fatalf("default capture path=%q want %q", got, wantCapture)
	}
	expanded, err := expandCodexDesktopProofPath("~/captures/frames.jsonl")
	if err != nil {
		t.Fatalf("expand home path: %v", err)
	}
	if expanded != filepath.Join("/Users/proof", "captures", "frames.jsonl") {
		t.Fatalf("expanded path=%q", expanded)
	}

	proof := &codexDesktopProofOutput{CapturePath: "/tmp/proof/frames.jsonl"}
	applyCodexDesktopProofCaptureCommands(proof, "", "")
	if proof.MatrixPath != "/tmp/proof/matrix.jsonl" ||
		!strings.Contains(proof.SearchCapProofCommand, "search-cap-proof --frames /tmp/proof/frames.jsonl") ||
		!strings.Contains(proof.MatrixRowCommand, "--host 127.0.0.1 --port 8990") ||
		!strings.Contains(proof.FocusedMatrixCommand, "wss-proof-matrix /tmp/proof/matrix.jsonl") {
		t.Fatalf("capture commands=%+v", proof)
	}

	var rendered bytes.Buffer
	renderCodexDesktopProof(&rendered, codexDesktopProofOutput{
		Mode:                     "desktop_ready_for_prompt",
		Duration:                 "1s",
		StartedAt:                "2026-05-18T12:00:00Z",
		LaunchPID:                4242,
		Transport:                codexDesktopTransportAppServer,
		SessionPath:              "/tmp/session.json",
		CapturePath:              "/tmp/proof/frames.jsonl",
		ClassDistributionCommand: "measure",
		MatrixRowCommand:         "row",
		FocusedMatrixCommand:     "matrix",
		SearchCapProofCommand:    "search",
		Notes:                    []string{"capture note"},
	})
	for _, want := range []string{
		"Capture   /tmp/proof/frames.jsonl",
		"Measure   measure",
		"Row       row",
		"Matrix    matrix",
		"SearchCap search",
		"Note      capture note",
	} {
		if !strings.Contains(rendered.String(), want) {
			t.Fatalf("rendered proof missing %q:\n%s", want, rendered.String())
		}
	}

	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	if _, err := expandCodexDesktopProofPath("~/frames.jsonl"); err == nil {
		t.Fatal("expected home expansion error")
	}
}

func TestCodexDesktopProveCapturePrepareFailureDoesNotLaunch(t *testing.T) {
	withCodexCmdStubs(t)
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	launched := false
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		launched = true
		return 0
	}

	p, out, errBuf := newTestPrinter()
	rc := runCodexCmd([]string{"desktop", "prove", "--manual", "--json", "--duration=1ns", "--capture=" + filepath.Join(blocker, "frames.jsonl")}, p)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%q out=%q", rc, errBuf.String(), out.String())
	}
	if launched {
		t.Fatal("capture prepare failure must stop before launching Codex.app")
	}
	var proof codexDesktopProofOutput
	if err := json.Unmarshal(out.Bytes(), &proof); err != nil {
		t.Fatalf("json: %v\nraw=%s", err, out.String())
	}
	if proof.Mode != "capture_prepare_failed" || proof.FailureClass != "capture_prepare_failed" {
		t.Fatalf("proof=%+v", proof)
	}
}

func TestCodexDesktopProveErrorsAndHelpers(t *testing.T) {
	withCodexCmdStubs(t)
	p, out, errBuf := newTestPrinter()

	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return control.SetupState{}, errors.New("daemon down")
	}
	if rc := runCodexCmd([]string{"desktop", "prove", "--json", "--duration=1ns"}, p); rc != 1 {
		t.Fatalf("daemon rc=%d", rc)
	}
	if !strings.Contains(out.String(), "daemon_unreachable") {
		t.Fatalf("daemon proof output=%q", out.String())
	}

	out.Reset()
	errBuf.Reset()
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
	}
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		fmt.Fprint(p.Err, "spawn denied")
		return 1
	}
	if rc := runCodexCmd([]string{"desktop", "prove", "--json", "--duration=1ns"}, p); rc != 1 {
		t.Fatalf("launch rc=%d", rc)
	}
	if !strings.Contains(out.String(), "launch_failed") ||
		!strings.Contains(out.String(), "spawn denied") {
		t.Fatalf("launch proof output=%q stderr=%q", out.String(), errBuf.String())
	}

	if got := parseCodexDesktopLaunchPID("Codex.app launched (PID 37720) with scoped Slimference env."); got != 37720 {
		t.Fatalf("pid=%d", got)
	}
	if _, err := parseCodexDesktopProveFlags([]string{"--duration=0s"}); err == nil {
		t.Fatal("expected bad duration error")
	}
	if _, err := parseCodexDesktopProveFlags([]string{"--capture="}); err == nil {
		t.Fatal("expected empty capture path error")
	}
	if _, err := parseCodexDesktopProveFlags([]string{"--matrix-row="}); err == nil {
		t.Fatal("expected empty matrix-row path error")
	}
	if _, err := parseCodexDesktopProveFlags([]string{"--bogus"}); err == nil {
		t.Fatal("expected bad flag error")
	}
	if _, err := parseCodexDesktopProveFlags([]string{"--manual", "--finish"}); err == nil {
		t.Fatal("expected manual+finish conflict")
	}
}

func TestRunCodexDesktopCmdHelpAndErrors(t *testing.T) {
	withCodexCmdStubs(t)
	p, out, errBuf := newTestPrinter()
	if rc := runCodexCmd([]string{"desktop"}, p); rc != 0 || !strings.Contains(out.String(), "usage: slimference codex desktop") {
		t.Fatalf("desktop help rc=%d out=%q", rc, out.String())
	}
	out.Reset()
	if rc := runCodexCmd([]string{"desktop", "--help"}, p); rc != 0 || !strings.Contains(out.String(), "Desktop-specific") {
		t.Fatalf("desktop --help rc=%d out=%q", rc, out.String())
	}
	if rc := runCodexCmd([]string{"desktop", "bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown subcommand") {
		t.Fatalf("desktop unknown rc=%d err=%q", rc, errBuf.String())
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"desktop", "status", "--bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("desktop status bad flag rc=%d err=%q", rc, errBuf.String())
	}
	out.Reset()
	if rc := runCodexCmd([]string{"desktop", "status", "--help"}, p); rc != 0 || !strings.Contains(out.String(), "codex desktop status") {
		t.Fatalf("desktop status help rc=%d out=%q", rc, out.String())
	}
	out.Reset()
	if rc := runCodexCmd([]string{"desktop", "prove", "--help"}, p); rc != 0 || !strings.Contains(out.String(), "codex desktop prove") {
		t.Fatalf("desktop prove help rc=%d out=%q", rc, out.String())
	}
	errBuf.Reset()
	if rc := runCodexCmd([]string{"desktop", "prove", "--bogus"}, p); rc != 2 || !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("desktop prove bad flag rc=%d err=%q", rc, errBuf.String())
	}
}

func TestHandleCodexCmdUsesExitFn(t *testing.T) {
	withCodexCmdStubs(t)
	oldExit := exitFn
	t.Cleanup(func() { exitFn = oldExit })
	got := -1
	exitFn = func(code int) { got = code }
	handleCodexCmd([]string{"--help"})
	if got != 0 {
		t.Fatalf("exit code=%d", got)
	}
}

func TestCodexProxyEnvCarriesPrinter(t *testing.T) {
	p := installPrinter{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	env := codexProxyEnv(p)
	if env.Stdout != p.Out || env.Stderr != p.Err || env.Stdin == nil {
		t.Fatalf("bad proxy env")
	}
	if env.LoadCA == nil || env.HealthCheck == nil || env.RunCommand == nil {
		t.Fatalf("missing proxy env dependencies")
	}
	if !strings.HasSuffix(env.CADirFn(), ".slimference") {
		t.Fatalf("bad CA dir: %q", env.CADirFn())
	}
}

func TestSetCodexDesktopWSSCapturePostsAdminPayload(t *testing.T) {
	var got proxy.AdminWSSCaptureRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != proxy.AdminWSSCapturePath {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"enabled":true}`))
	}))
	defer server.Close()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("server addr: %v", err)
	}
	if err := setCodexDesktopWSSCapture(host, port, "/tmp/frames.jsonl", true, time.Second); err != nil {
		t.Fatalf("set capture: %v", err)
	}
	if !got.Enabled || got.Path != "/tmp/frames.jsonl" || got.DurationSeconds != 21600 {
		t.Fatalf("request payload=%+v", got)
	}

	got = proxy.AdminWSSCaptureRequest{}
	if err := setCodexDesktopWSSCapture(host, port, "", false, time.Second); err != nil {
		t.Fatalf("clear capture: %v", err)
	}
	if got.Enabled || got.Path != "" || got.DurationSeconds != 0 {
		t.Fatalf("clear payload=%+v", got)
	}
}

func TestSetCodexDesktopWSSCaptureReportsAdminFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer server.Close()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("server addr: %v", err)
	}
	err = setCodexDesktopWSSCapture(host, port, "/tmp/frames.jsonl", true, time.Second)
	if err == nil || !strings.Contains(err.Error(), "admin returned 418") {
		t.Fatalf("expected admin status error, got %v", err)
	}
}

func passingCodexCertificationState() control.SetupState {
	return control.SetupState{
		CodexRoute: control.CodexRouteState{
			DaemonReachable: true,
		},
		WSS: control.WSSState{
			ParseFailures:             0,
			DegradedSessions:          0,
			CompressionErrors:         0,
			FramesReencoded:           7,
			CompressedMessagesMutated: 2,
			MutationActive:            true,
			ByteBridgeOnly:            false,
		},
	}
}

func splitHTTPTestServer(t *testing.T, server *httptest.Server) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("split test server URL %q: %v", server.URL, err)
	}
	return host, port
}

type codexErrWriter struct{}

func (codexErrWriter) Write([]byte) (int, error) {
	return 0, errors.New("forced write error")
}

func TestServiceControlAdapterCodexRoute(t *testing.T) {
	withCodexCmdStubs(t)
	oldEnable := tuiCodexRouteEnableCmdFn
	oldDisable := tuiCodexRouteDisableCmdFn
	oldHealth := tuiCodexRouteHealthCheckFn
	oldHome := osUserHomeDir
	t.Cleanup(func() {
		tuiCodexRouteEnableCmdFn = oldEnable
		tuiCodexRouteDisableCmdFn = oldDisable
		tuiCodexRouteHealthCheckFn = oldHealth
		osUserHomeDir = oldHome
	})

	enableCalled := false
	disableCalled := false
	tuiCodexRouteEnableCmdFn = func(args []string, p installPrinter) int {
		enableCalled = true
		return 0
	}
	tuiCodexRouteDisableCmdFn = func(args []string, p installPrinter) int {
		disableCalled = true
		return 0
	}
	osUserHomeDir = func() (string, error) { return "/tmp/home", nil }
	codexRouteInspectFn = func(home, proxyURL string, opts codexroute.Options) (codexroute.Status, error) {
		return codexroute.Status{Exists: true, Enabled: true, Complete: true}, nil
	}
	tuiCodexRouteHealthCheckFn = func(host, port string) error { return nil }

	adapter := &serviceControlAdapter{}
	if err := adapter.EnableCodexRoute(); err != nil {
		t.Fatalf("EnableCodexRoute: %v", err)
	}
	if err := adapter.DisableCodexRoute(); err != nil {
		t.Fatalf("DisableCodexRoute: %v", err)
	}
	if !enableCalled || !disableCalled {
		t.Fatalf("route commands not called: enable=%v disable=%v", enableCalled, disableCalled)
	}
	status := adapter.CodexRouteStatus()
	if !status.Exists || !status.Enabled || !status.Complete || !status.DaemonReachable {
		t.Fatalf("bad route status: %+v", status)
	}
}

func TestServiceControlAdapterLaunchCodexCLI(t *testing.T) {
	oldExecutable := osExecutable
	oldLaunch := tuiLaunchCommandFn
	oldEnv := tuiTerminalEnvFn
	t.Cleanup(func() {
		osExecutable = oldExecutable
		tuiLaunchCommandFn = oldLaunch
		tuiTerminalEnvFn = oldEnv
	})

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	tuiTerminalEnvFn = func(key string) string { return "Apple_Terminal" }
	var gotName string
	var gotArgs []string
	tuiLaunchCommandFn = func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}

	msg, err := (&serviceControlAdapter{}).LaunchCodexCLI()
	if err != nil {
		t.Fatalf("LaunchCodexCLI: %v", err)
	}
	if !strings.Contains(msg, "Codex CLI started") {
		t.Fatalf("msg=%q", msg)
	}
	if gotName != "osascript" || len(gotArgs) != 2 || !strings.Contains(gotArgs[1], "codex run --transport=auto --") {
		t.Fatalf("launch command name=%q args=%v", gotName, gotArgs)
	}
	if !strings.Contains(gotArgs[1], "/bin/bash -lc") || !strings.Contains(gotArgs[1], "unset") || !strings.Contains(gotArgs[1], "CODEX_") {
		t.Fatalf("launch command must scrub inherited Codex session env, args=%v", gotArgs)
	}
	if !strings.Contains(gotArgs[1], "tell application \"Terminal\"") || !strings.Contains(gotArgs[1], "in front window") {
		t.Fatalf("Terminal launch must open a tab in Terminal.app, args=%v", gotArgs)
	}
	if !strings.Contains(gotArgs[1], "[2J") || !strings.Contains(gotArgs[1], "[H") || !strings.Contains(gotArgs[1], "[SF] Codex CLI started with Slimference") {
		t.Fatalf("TUI launcher must clean the visible startup command, args=%v", gotArgs)
	}

	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	if _, err := (&serviceControlAdapter{}).LaunchCodexCLI(); err == nil || !strings.Contains(err.Error(), "no executable") {
		t.Fatalf("executable error=%v", err)
	}

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
	tuiLaunchCommandFn = func(string, ...string) error { return errors.New("osascript denied") }
	if _, err := (&serviceControlAdapter{}).LaunchCodexCLI(); err == nil || !strings.Contains(err.Error(), "osascript denied") {
		t.Fatalf("launch error=%v", err)
	}
}

func TestServiceControlAdapterDesktopStatusNeedsGreenProofBlocksSlimferenceLaunch(t *testing.T) {
	withCodexCmdStubs(t)
	oldGetwd := osGetwd
	t.Cleanup(func() {
		osGetwd = oldGetwd
	})
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
	}
	dir := t.TempDir()
	osGetwd = func() (string, error) { return dir, nil }
	tuiCodexDesktopDirectFn = func(string) error { t.Fatal("TUI Slimference launch must not open direct Codex.app"); return nil }

	adapter := &serviceControlAdapter{}
	status := adapter.CodexDesktopStatus()
	if status.Mode != "ready_for_live_desktop_probe" || status.FailureClass != "" {
		t.Fatalf("desktop status=%+v", status)
	}
	if _, err := adapter.LaunchCodexApp(); err == nil || !strings.Contains(err.Error(), "Desktop Slimference proof is not green") {
		t.Fatalf("LaunchCodexApp error=%v", err)
	}
}

func TestServiceControlAdapterLaunchCodexAppSuccessAndErrors(t *testing.T) {
	withCodexCmdStubs(t)
	codexDesktopBaseEnvFn = func() []string {
		return []string{
			"PATH=/usr/bin",
			"HOME=/Users/example",
			"PWD=/Users/example/CODE/OldProject",
			"OLDPWD=/Users/example/CODE/OlderProject",
		}
	}
	writeCodexDesktopProofResult(&codexDesktopProofOutput{
		Mode:           "desktop_app_server_phasef_proven",
		Transport:      codexDesktopTransportAppServer,
		DesktopProven:  true,
		DesktopSavings: true,
	})
	runningProbeCalls := 0
	codexDesktopRunningFn = func(string) ([]int, error) {
		runningProbeCalls++
		if runningProbeCalls == 1 {
			return []int{44}, nil
		}
		return nil, nil
	}
	var cleaned []int
	codexDesktopCleanupFn = func(pid int) error {
		cleaned = append(cleaned, pid)
		return nil
	}
	var launchedEnv []string
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		launchedEnv = append([]string(nil), env...)
		fmt.Fprintln(p.Out, "started")
		return 0
	}
	msg, err := (&serviceControlAdapter{}).LaunchCodexApp()
	if err == nil || !strings.Contains(err.Error(), "codex_desktop_already_running") {
		t.Fatalf("LaunchCodexApp must not replace running app automatically: msg=%q err=%v", msg, err)
	}
	if len(cleaned) != 0 {
		t.Fatalf("LaunchCodexApp must not clean running Codex.app automatically, cleaned=%v", cleaned)
	}

	codexDesktopRunningFn = func(string) ([]int, error) { return nil, nil }
	msg, err = (&serviceControlAdapter{}).LaunchCodexApp()
	if err != nil {
		t.Fatalf("LaunchCodexApp success without running app: %v", err)
	}
	if !strings.Contains(msg, "Codex App started with Slimference") {
		t.Fatalf("msg=%q", msg)
	}
	joinedEnv := strings.Join(launchedEnv, "\n")
	for _, forbidden := range []string{"PWD=", "OLDPWD="} {
		if strings.Contains(joinedEnv, forbidden) {
			t.Fatalf("desktop TUI launch must not seed workspace env %s in %v", forbidden, launchedEnv)
		}
	}
	if !strings.Contains(joinedEnv, "CODEX_CLI_PATH=") || !strings.Contains(joinedEnv, "SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1") {
		t.Fatalf("desktop TUI launch lost scoped app-server env: %v", launchedEnv)
	}

	codexDesktopRunningFn = func(string) ([]int, error) { return nil, nil }
	codexDesktopStartFn = func(p installPrinter, binary string, args []string, env []string) int {
		fmt.Fprint(p.Err, "spawn denied")
		return 1
	}
	if _, err := (&serviceControlAdapter{}).LaunchCodexApp(); err == nil || !strings.Contains(err.Error(), "spawn denied") {
		t.Fatalf("proxy launch error=%v", err)
	}
}

func TestServiceControlAdapterRepairCodexWSS(t *testing.T) {
	withCodexCmdStubs(t)
	calls := 0
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		calls++
		if calls == 1 {
			return control.SetupState{CodexRoute: control.CodexRouteState{DaemonReachable: true}}, nil
		}
		return recertPostState(7, 2), nil
	}

	msg, err := (&serviceControlAdapter{}).RepairCodexWSS()
	if err != nil {
		t.Fatalf("RepairCodexWSS: %v", err)
	}
	if !strings.Contains(msg, "Codex WSS recertified") {
		t.Fatalf("msg=%q", msg)
	}

	codexRecertTriggerFn = func(codexRecertTriggerInput) (codexRecertTriggerResult, error) {
		return codexRecertTriggerResult{}, errors.New("trigger denied")
	}
	if _, err := (&serviceControlAdapter{}).RepairCodexWSS(); err == nil || !strings.Contains(err.Error(), "trigger denied") {
		t.Fatalf("expected repair error, got %v", err)
	}
}

func TestServiceControlAdapterCodexRouteStatusStartsAutoRecertAfterHealth(t *testing.T) {
	withCodexCmdStubs(t)
	oldHome := osUserHomeDir
	oldHealth := tuiCodexRouteHealthCheckFn
	t.Cleanup(func() {
		osUserHomeDir = oldHome
		tuiCodexRouteHealthCheckFn = oldHealth
	})
	osUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	codexRouteInspectFn = func(home, proxyURL string, opts codexroute.Options) (codexroute.Status, error) {
		return codexroute.Status{Exists: true, Enabled: true, Complete: true}, nil
	}
	codexAutoFn = func(home string) codexroute.AutoDecision {
		return codexroute.AutoDecision{
			Mode:           codexroute.AutoModeHTTP,
			Transport:      codexroute.TransportHTTP,
			NeedsRecert:    true,
			RecertCommand:  "slimference codex recertify wss",
			FallbackReason: "codex version changed since wss certification",
		}
	}
	started := false
	codexAutoRecertFn = func(string, string, string, codexroute.AutoDecision) {
		started = true
	}
	tuiCodexRouteHealthCheckFn = func(host, port string) error { return nil }

	status := (&serviceControlAdapter{}).CodexRouteStatus()
	if !status.DaemonReachable || !status.NeedsRecert || !started {
		t.Fatalf("status=%+v started=%v", status, started)
	}

	started = false
	tuiCodexRouteHealthCheckFn = func(host, port string) error { return errors.New("offline") }
	status = (&serviceControlAdapter{}).CodexRouteStatus()
	if status.DaemonReachable || started {
		t.Fatalf("offline status=%+v started=%v", status, started)
	}
}
