package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfigPath_EnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", want)
	if got := configPath(); got != want {
		t.Fatalf("configPath env override: got %q want %q", got, want)
	}
}

func TestModel_CopyDebugLog_ExportDirCreateError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	blocker := filepath.Join(home, ".slimference", "exports")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	proxy := newMockProxy()
	proxy.sessionLogger.Log("INFO", "test", "hello")
	model := NewModel(proxy)

	if path := model.copyDebugLog(); path != "" {
		t.Fatalf("expected copyDebugLog failure, got %q", path)
	}
}

func TestModel_SetupSteps_ServiceInstallCheckTrue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatalf("mkdir plist dir: %v", err)
	}
	if err := os.WriteFile(plist, []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	model := NewModel(newMockProxy())
	model.SetServiceControl(&mockServiceControl{})
	steps := model.setupSteps()
	if len(steps) != 4 {
		t.Fatalf("unexpected step count: %d", len(steps))
	}
	if !steps[3].check() {
		t.Fatal("expected launchd step to report installed service")
	}
}

func TestRenderHeader_IsQuietProductHeader(t *testing.T) {
	proxy := newMockProxy()
	model := NewModel(proxy)
	got := model.renderHeader(40)
	if !strings.Contains(got, "SLIMFERENCE v") || strings.Contains(got, ":8990") || strings.Contains(got, "daemon") {
		t.Fatalf("unexpected header: %q", got)
	}
}

func TestRenderHeader_IncludesBypassBadge(t *testing.T) {
	proxy := newMockProxy()
	proxy.bypass = true
	model := NewModel(proxy)
	got := model.renderHeader(80)
	if !strings.Contains(got, "BYPASS") {
		t.Fatalf("bypass badge missing: %q", got)
	}
}

func TestJoinKeysEmpty(t *testing.T) {
	if got := joinKeys(nil); got != "" {
		t.Fatalf("empty joinKeys = %q", got)
	}
}

func TestModel_CopyDebugLog_HomeAndWriteErrors(t *testing.T) {
	proxy := newMockProxy()
	proxy.sessionLogger.Log("INFO", "test", "hello")
	model := NewModel(proxy)

	origHome := userHomeDirFn
	userHomeDirFn = func() (string, error) { return "", errors.New("no home") }
	if path := model.copyDebugLog(); path != "" {
		t.Fatalf("expected empty path on home-dir error, got %q", path)
	}
	userHomeDirFn = origHome

	origWrite := writeFileFn
	writeFileFn = func(string, []byte, os.FileMode) error { return errors.New("write boom") }
	defer func() {
		writeFileFn = origWrite
		userHomeDirFn = origHome
	}()
	if path := model.copyDebugLog(); path != "" {
		t.Fatalf("expected empty path on write error, got %q", path)
	}
}

func TestUpdate_SetupServiceErrorBranches(t *testing.T) {
	tests := []struct {
		key        rune
		running    bool
		wantSubstr string
	}{
		{key: 'p', running: true, wantSubstr: "Stop failed"},
		{key: 'p', running: false, wantSubstr: "Start failed"},
		{key: 'o', running: false, wantSubstr: "Restart failed"},
		{key: 'e', running: false, wantSubstr: "Install failed"},
		{key: 'w', running: false, wantSubstr: "Uninstall failed"},
	}

	for _, tc := range tests {
		proxy := newMockProxy()
		model := NewModel(proxy)
		model.view = ViewSetup
		model.svc = &mockServiceControl{running: tc.running, err: errors.New("boom")}

		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		got := updated.(Model)
		if !strings.Contains(got.flashMsg, tc.wantSubstr) {
			t.Fatalf("key %q flash = %q, want %q", string(tc.key), got.flashMsg, tc.wantSubstr)
		}
	}
}

func TestUpdate_SetupAdvancedKeysAreCLIOnly(t *testing.T) {
	proxy := newMockProxy()
	model := NewModel(proxy)
	model.view = ViewSetup
	svc := &mockServiceControl{}
	model.SetServiceControl(svc)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	model = updated.(Model)
	if svc.transparentInstalled || svc.transparentEnabled || !strings.Contains(model.flashMsg, "CLI-only") {
		t.Fatalf("global lab key should be CLI-only: svc=%+v flash=%q", svc, model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	model = updated.(Model)
	if svc.transparentRemoved || !strings.Contains(model.flashMsg, "CLI-only") {
		t.Fatalf("uninstall key should be CLI-only: svc=%+v flash=%q", svc, model.flashMsg)
	}
}

func TestUpdate_BackKeyReturnsFromSubview(t *testing.T) {
	proxy := newMockProxy()
	model := NewModel(proxy)
	model.view = ViewStats

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	model = updated.(Model)
	if model.view != ViewMain || proxy.bypass {
		t.Fatalf("b should go back without toggling bypass: view=%v bypass=%v", model.view, proxy.bypass)
	}

	model.view = ViewDebug
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	model = updated.(Model)
	if model.view != ViewMain || proxy.bypass {
		t.Fatalf("B should go back without toggling bypass: view=%v bypass=%v", model.view, proxy.bypass)
	}
}

func TestRenderTransparentStatusLineStates(t *testing.T) {
	styles := NewStyles()
	cases := []struct {
		status TransparentStatus
		want   string
	}{
		{status: TransparentStatus{ProxyArmed: true, DaemonReachable: true, ActiveServices: 2}, want: "daemon reachable"},
		{status: TransparentStatus{ProxyArmed: true, ActiveServices: 1}, want: "daemon unreachable"},
		{status: TransparentStatus{CAExists: true, CATrusted: true, AutoStartInstalled: true}, want: "installed"},
		{status: TransparentStatus{CAExists: true}, want: "partially installed"},
		{status: TransparentStatus{NetworkUnavailable: true}, want: "networksetup unavailable"},
		{status: TransparentStatus{}, want: "not installed"},
	}
	for _, tc := range cases {
		if got := renderTransparentStatusLine(styles, tc.status); !strings.Contains(got, tc.want) {
			t.Fatalf("status line=%q want %q", got, tc.want)
		}
	}
}

func TestSetupSteps_ServiceActionAndPartialState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model := NewModel(newMockProxy())
	svc := &mockServiceControl{transparentStatus: TransparentStatus{CAExists: true}}
	model.SetServiceControl(svc)

	if actions := model.dashboardActions(); len(actions) != 7 {
		t.Fatalf("home menu must expose seven entries: %+v", actions)
	}

	steps := model.setupSteps()
	if err := steps[3].action(&model); err != nil {
		t.Fatalf("service action failed: %v", err)
	}
}

func TestRenderCodexRouteStatusLineBranches(t *testing.T) {
	styles := NewStyles()
	cases := []struct {
		name   string
		status CodexRouteStatus
		want   string
	}{
		{
			name: "ready certified",
			status: CodexRouteStatus{
				Complete:        true,
				DaemonReachable: true,
				AutoTransport:   "wss",
				AutoMode:        "wss_phasef",
				WSSCertified:    true,
				CurrentCodex:    "0.138.0",
				CertifiedCodex:  "0.138.0",
			},
			want: "no stale proof",
		},
		{
			name:   "daemon unreachable",
			status: CodexRouteStatus{Enabled: true},
			want:   "daemon unreachable",
		},
		{
			name:   "conflict",
			status: CodexRouteStatus{Enabled: true, DaemonReachable: true, Conflict: "foreign"},
			want:   "conflict",
		},
		{
			name:   "incomplete",
			status: CodexRouteStatus{Enabled: true, DaemonReachable: true, AutoTransport: "http"},
			want:   "incomplete",
		},
		{
			name: "disabled with fallback",
			status: CodexRouteStatus{
				Exists:         true,
				AutoTransport:  "http",
				FallbackReason: "codex version changed",
			},
			want: "version changed",
		},
		{
			name:   "missing",
			status: CodexRouteStatus{},
			want:   "not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderCodexRouteStatusLine(styles, tc.status); !strings.Contains(got, tc.want) {
				t.Fatalf("line=%q want %q", got, tc.want)
			}
		})
	}
}

func TestStatusCLIDetailShowsStaleProofTruth(t *testing.T) {
	fresh := statusCLIDetail(CodexRouteStatus{
		WSSCertified:   true,
		CurrentCodex:   "0.138.0",
		CertifiedCodex: "0.138.0",
	})
	if !strings.Contains(fresh, "no stale proof") {
		t.Fatalf("fresh detail=%q", fresh)
	}

	stale := statusCLIDetail(CodexRouteStatus{
		NeedsRecert:    true,
		CurrentCodex:   "0.139.0",
		CertifiedCodex: "0.138.0",
	})
	if !strings.Contains(stale, "stale proof") {
		t.Fatalf("stale detail=%q", stale)
	}
}

func TestStatusDesktopDetailShowsSharedWSSFreshness(t *testing.T) {
	detail := statusDesktopDetail(
		CodexDesktopStatus{Mode: "desktop_app_server_proven"},
		CodexRouteStatus{
			NeedsRecert:    true,
			CurrentCodex:   "0.139.0",
			CertifiedCodex: "0.138.0",
		},
	)
	if !strings.Contains(detail, "savings ready") || !strings.Contains(detail, "stale proof") {
		t.Fatalf("desktop detail=%q", detail)
	}
}

func TestSetupSteps_ActionClosures(t *testing.T) {
	model := NewModel(newMockProxy())
	svc := &mockServiceControl{}
	model.SetServiceControl(svc)

	steps := model.setupSteps()
	if err := steps[1].action(&model); err != nil {
		t.Fatalf("codex hook action failed: %v", err)
	}
	if err := steps[2].action(&model); err != nil {
		t.Fatalf("wss repair action failed: %v", err)
	}
	if err := steps[3].action(&model); err != nil {
		t.Fatalf("service action failed: %v", err)
	}
	if !svc.installed {
		t.Fatal("expected install service action to call service control")
	}
}

func TestRenderViews_CoverageBranches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proxy := newMockProxy()
	model := NewModel(proxy)
	model.width = 40
	model.height = 24
	model.hookStatus = HookStatus{}

	mainView := model.renderMainView()
	if !strings.Contains(mainView, "MENU") || !strings.Contains(mainView, "Savings") {
		t.Fatalf("unexpected main view: %q", mainView)
	}

	pidPath := filepath.Join(home, ".slimference", "slimference.pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatalf("mkdir pid dir: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte("1234"), 0o644); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		t.Fatalf("mkdir plist dir: %v", err)
	}
	if err := os.WriteFile(plist, []byte("plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	setupView := model.renderSetupView()
	if !strings.Contains(setupView, "Daemon running") || !strings.Contains(setupView, "launchd auto-start service") {
		t.Fatalf("unexpected setup view: %q", setupView)
	}

	if lines := model.renderRequestLog(); len(lines) != 1 || !strings.Contains(lines[0], "Waiting for requests") {
		t.Fatalf("unexpected empty request log: %v", lines)
	}

	if header := model.renderHeader(1); !strings.Contains(header, "SLIMFERENCE v") {
		t.Fatalf("unexpected narrow header: %q", header)
	}
}

func TestRenderSetupView_TransparentArmedAndTrustMissing(t *testing.T) {
	armed := NewModel(newMockProxy())
	armed.view = ViewSetup
	armed.width = 100
	armed.SetServiceControl(&mockServiceControl{transparentStatus: TransparentStatus{
		ProxyArmed:      true,
		ActiveServices:  1,
		DaemonReachable: true,
	}})
	if view := armed.renderSetupView(); !strings.Contains(view, "Machine-wide route is active") {
		t.Fatalf("armed setup view missing route warning: %s", view)
	}

	untrusted := NewModel(newMockProxy())
	untrusted.view = ViewSetup
	untrusted.width = 100
	untrusted.SetServiceControl(&mockServiceControl{transparentStatus: TransparentStatus{CAExists: true}})
	if view := untrusted.renderSetupView(); strings.Contains(view, "Keychain trust") || strings.Contains(view, "GLOBAL LAB") {
		t.Fatalf("setup view leaked advanced CA/lab guidance: %s", view)
	}
}

func TestRenderMainView_PadsBothColumns(t *testing.T) {
	model := NewModel(newMockProxy())
	model.width = 100
	model.height = 24
	model.hookStatus = HookStatus{}
	model.proxy.(*mockProxy).recentReqs = nil
	if view := model.renderMainView(); !strings.Contains(view, "MENU") {
		t.Fatalf("unexpected quick-start main view: %q", view)
	}

	proxy := newMockProxy()
	for i := 0; i < 10; i++ {
		proxy.recentReqs = append(proxy.recentReqs, types.RequestMetrics{
			Timestamp:        time.Now(),
			Provider:         types.Anthropic,
			Model:            "claude",
			InputTokensOrig:  100,
			InputTokensComp:  50,
			OutputTokens:     25,
			CompressionRatio: 0.5,
			Layers:           []int{1},
			LatencyMs:        10,
		})
	}
	model = NewModel(proxy)
	model.width = 100
	model.height = 24
	model.hookStatus = HookStatus{Claude: true, Codex: true}
	if view := model.renderMainView(); !strings.Contains(view, "MENU") || strings.Contains(view, "LIVE") || strings.Contains(view, "CURRENT SESSION") {
		t.Fatalf("unexpected live main view: %q", view)
	}

	proxy = newMockProxy()
	model = NewModel(proxy)
	model.width = 100
	model.height = 24
	model.hookStatus = HookStatus{Claude: true, Codex: true}
	if view := model.renderMainView(); !strings.Contains(view, "MENU") {
		t.Fatalf("unexpected padded live view: %q", view)
	}
}
