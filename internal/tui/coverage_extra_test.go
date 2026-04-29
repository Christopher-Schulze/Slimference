package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/slimference/slimference/internal/types"
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
	if len(steps) != 3 {
		t.Fatalf("unexpected step count: %d", len(steps))
	}
	if !steps[2].check() {
		t.Fatal("expected launchd step to report installed service")
	}
}

func TestRenderHeader_IncludesPortAndDuration(t *testing.T) {
	proxy := newMockProxy()
	model := NewModel(proxy)
	got := model.renderHeader(40)
	if !strings.Contains(got, ":8990") || !strings.Contains(got, "SLIMFERENCE v") {
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

func TestUpdate_BypassToggleOnAndOff(t *testing.T) {
	proxy := newMockProxy()
	model := NewModel(proxy)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	model = updated.(Model)
	if !proxy.bypass || !strings.Contains(model.flashMsg, "Bypass: ON") {
		t.Fatalf("after first toggle bypass=%v flash=%q", proxy.bypass, model.flashMsg)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'B'}})
	model = updated.(Model)
	if proxy.bypass || !strings.Contains(model.flashMsg, "Bypass: OFF") {
		t.Fatalf("after second toggle bypass=%v flash=%q", proxy.bypass, model.flashMsg)
	}
}

func TestSetupSteps_ActionClosures(t *testing.T) {
	model := NewModel(newMockProxy())
	svc := &mockServiceControl{}
	model.SetServiceControl(svc)

	steps := model.setupSteps()
	if err := steps[1].action(&model); err != nil {
		t.Fatalf("codex action failed: %v", err)
	}
	if err := steps[2].action(&model); err != nil {
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
	if !strings.Contains(mainView, "QUICK START") {
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

	model.width = 5
	if panel := strings.Join(model.buildRightPanel(5), "\n"); !strings.Contains(panel, "Waiting for requests") && !strings.Contains(panel, "QUICK START") {
		t.Fatalf("unexpected right panel: %q", panel)
	}

	if lines := model.renderRequestLog(); len(lines) != 1 || !strings.Contains(lines[0], "Waiting for requests") {
		t.Fatalf("unexpected empty request log: %v", lines)
	}

	if header := model.renderHeader(1); !strings.Contains(header, "SLIMFERENCE v") {
		t.Fatalf("unexpected narrow header: %q", header)
	}
}

func TestRenderMainView_PadsBothColumns(t *testing.T) {
	model := NewModel(newMockProxy())
	model.width = 100
	model.height = 24
	model.hookStatus = HookStatus{}
	model.proxy.(*mockProxy).recentReqs = nil
	if view := model.renderMainView(); !strings.Contains(view, "QUICK START") {
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
	if view := model.renderMainView(); !strings.Contains(view, "LIVE") {
		t.Fatalf("unexpected live main view: %q", view)
	}

	proxy = newMockProxy()
	model = NewModel(proxy)
	model.width = 100
	model.height = 24
	model.hookStatus = HookStatus{Claude: true, Codex: true}
	if view := model.renderMainView(); !strings.Contains(view, "Waiting for requests") {
		t.Fatalf("unexpected padded live view: %q", view)
	}
}
