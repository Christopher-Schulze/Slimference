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
	"testing"
	"time"

	"github.com/slimference/slimference/internal/codexroute"
	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/proxy"
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
	oldSetupState := codexSetupStateFn
	oldVersionOut := codexVersionOutFn
	oldNow := codexNowFn
	oldDesktopCA := codexDesktopCATrustFn
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
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		return passingCodexCertificationState(), nil
	}
	codexVersionOutFn = func() ([]byte, error) { return []byte("codex-cli 0.130.0\n"), nil }
	codexNowFn = func() time.Time { return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC) }
	codexDesktopCATrustFn = func() codexDesktopCAState {
		return codexDesktopCAState{Path: "/tmp/root.crt", Exists: true, Trusted: true}
	}
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
		codexSetupStateFn = oldSetupState
		codexVersionOutFn = oldVersionOut
		codexNowFn = oldNow
		codexDesktopCATrustFn = oldDesktopCA
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

func TestCodexCmdRunAutoUsesWSSBridgeBeforeHTTP(t *testing.T) {
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
	if !strings.Contains(strings.Join(got, "\x00"), "--proxied-wss-bridge") {
		t.Fatalf("expected WSS bridge proxy args, got %#v", got)
	}
	if !startedRecert {
		t.Fatal("auto WSS drift should start background recert")
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
	if !strings.Contains(out.String(), "Codex route enabled") ||
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
	if !strings.Contains(out.String(), "Codex route disabled") {
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
		!strings.Contains(out.String(), "would remove scoped Codex route") {
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
		!strings.Contains(out.String(), "Route is disabled") ||
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
			CurrentSlimference:   "2.0.2",
			CertifiedCodex:       "0.130.0",
			CertifiedSlimference: "2.0.2",
			FallbackReason:       "codex version changed since wss certification",
			RecertCommand:        "slimference codex recertify wss",
		}
	}
	if rc := runCodexCmd([]string{"status"}, p); rc != 0 ||
		!strings.Contains(out.String(), "current codex=0.131.0 slimference=2.0.2") ||
		!strings.Contains(out.String(), "certified codex=0.130.0 slimference=2.0.2") ||
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
			CurrentSlimference:   "2.0.2",
			CertifiedCodex:       "0.130.0",
			CertifiedSlimference: "2.0.2",
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
	if got.LaunchCommand != "slimference codex launch-desktop --transport=proxy --with-ca-env" {
		t.Fatalf("launch command=%q", got.LaunchCommand)
	}
	if !got.CATrust.Trusted || !got.DaemonReachable {
		t.Fatalf("readiness not propagated: %+v", got)
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
	if !strings.Contains(out.String(), "ca_missing") {
		t.Fatalf("status missing ca gate: %q", out.String())
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
		untrusted.LaunchCommand != "slimference codex launch-desktop --transport=proxy --with-ca-env" {
		t.Fatalf("untrusted status=%+v", untrusted)
	}
	if !strings.Contains(strings.Join(untrusted.Notes, "\n"), "process-local") {
		t.Fatalf("untrusted notes do not explain CA env: %+v", untrusted.Notes)
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

func TestCodexDesktopStatusReportsTLSRejectedAfterConnect(t *testing.T) {
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
	if got.Mode != "desktop_tls_blocked" || got.FailureClass != "tls_trust_rejected" {
		t.Fatalf("status=%+v", got)
	}
	if got.ConversationObserved {
		t.Fatalf("zero-byte CONNECT attempts must not count as observed conversation: %+v", got)
	}
	if !strings.Contains(strings.Join(got.Notes, "\n"), "CODEX_CA_CERTIFICATE/root-store hook") {
		t.Fatalf("notes do not explain root-store blocker: %+v", got.Notes)
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
	t.Cleanup(func() {
		osExecutable = oldExecutable
		tuiLaunchCommandFn = oldLaunch
	})

	osExecutable = func() (string, error) { return "/tmp/slimference", nil }
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
	if !strings.Contains(msg, "Codex CLI launched") {
		t.Fatalf("msg=%q", msg)
	}
	if gotName != "osascript" || len(gotArgs) != 2 || !strings.Contains(gotArgs[1], "codex run --transport=auto --") {
		t.Fatalf("launch command name=%q args=%v", gotName, gotArgs)
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

func TestServiceControlAdapterDesktopStatusTLSRejectedStillAllowsCAEnvRetry(t *testing.T) {
	withCodexCmdStubs(t)
	oldLaunchDesktop := tuiCodexLaunchDesktopCmdFn
	t.Cleanup(func() { tuiCodexLaunchDesktopCmdFn = oldLaunchDesktop })
	codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
		state := control.SetupState{}
		state.WSS.MITMBridged = 14
		return state, nil
	}
	var gotArgs []string
	tuiCodexLaunchDesktopCmdFn = func(args []string, _ installPrinter) int {
		gotArgs = append([]string(nil), args...)
		return 0
	}

	adapter := &serviceControlAdapter{}
	status := adapter.CodexDesktopStatus()
	if status.Mode != "desktop_tls_blocked" || status.FailureClass != "tls_trust_rejected" {
		t.Fatalf("desktop status=%+v", status)
	}
	if _, err := adapter.LaunchCodexApp(); err != nil {
		t.Fatalf("LaunchCodexApp err=%v", err)
	}
	if strings.Join(gotArgs, " ") != "--transport=proxy --with-ca-env" {
		t.Fatalf("args=%v", gotArgs)
	}
}

func TestServiceControlAdapterLaunchCodexAppSuccessAndErrors(t *testing.T) {
	withCodexCmdStubs(t)
	oldLaunchDesktop := tuiCodexLaunchDesktopCmdFn
	t.Cleanup(func() { tuiCodexLaunchDesktopCmdFn = oldLaunchDesktop })

	var gotArgs []string
	tuiCodexLaunchDesktopCmdFn = func(args []string, _ installPrinter) int {
		gotArgs = append([]string(nil), args...)
		return 0
	}
	msg, err := (&serviceControlAdapter{}).LaunchCodexApp()
	if err != nil {
		t.Fatalf("LaunchCodexApp success: %v", err)
	}
	if !strings.Contains(msg, "diagnostic proxy") {
		t.Fatalf("msg=%q", msg)
	}
	if strings.Join(gotArgs, " ") != "--transport=proxy --with-ca-env" {
		t.Fatalf("args=%v", gotArgs)
	}

	tuiCodexLaunchDesktopCmdFn = func(_ []string, p installPrinter) int {
		fmt.Fprint(p.Err, "spawn denied")
		return 1
	}
	if _, err := (&serviceControlAdapter{}).LaunchCodexApp(); err == nil || !strings.Contains(err.Error(), "spawn denied") {
		t.Fatalf("stderr failure err=%v", err)
	}

	tuiCodexLaunchDesktopCmdFn = func(_ []string, _ installPrinter) int { return 7 }
	if _, err := (&serviceControlAdapter{}).LaunchCodexApp(); err == nil || !strings.Contains(err.Error(), "exit 7") {
		t.Fatalf("fallback failure err=%v", err)
	}
}

func TestServiceControlAdapterLaunchCodexAppPreflightFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		ca   codexDesktopCAState
		err  error
		want string
	}{
		{
			name: "ca missing",
			ca:   codexDesktopCAState{},
			want: "ca_missing",
		},
		{
			name: "daemon unreachable",
			ca:   codexDesktopCAState{Exists: true, Trusted: true},
			err:  errors.New("offline"),
			want: "daemon_unreachable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCodexCmdStubs(t)
			codexDesktopCATrustFn = func() codexDesktopCAState { return tc.ca }
			if tc.err != nil {
				codexSetupStateFn = func(string, string, time.Duration) (control.SetupState, error) {
					return control.SetupState{}, tc.err
				}
			}
			_, err := (&serviceControlAdapter{}).LaunchCodexApp()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
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
