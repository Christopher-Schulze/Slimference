package apps

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestKnownAppsCovered(t *testing.T) {
	wantSet := map[AppID]struct{}{
		AppCodexCLI:     {},
		AppCodexDesktop: {},
		AppClaudeCode:   {},
	}
	for _, id := range KnownApps {
		if _, ok := wantSet[id]; !ok {
			t.Errorf("KnownApps contains unexpected id %q", id)
		}
		delete(wantSet, id)
	}
	if len(wantSet) != 0 {
		t.Errorf("KnownApps missing %v", wantSet)
	}
}

func TestAppIDIsKnown(t *testing.T) {
	for _, id := range KnownApps {
		if !id.IsKnown() {
			t.Errorf("%q should be known", id)
		}
	}
	if AppID("nonsense").IsKnown() {
		t.Errorf("nonsense should not be known")
	}
}

func TestDefaultPolicyPostInstall(t *testing.T) {
	p := DefaultPolicy()
	if !p.IsEnabled(AppCodexCLI) {
		t.Errorf("Codex CLI must default on")
	}
	if !p.IsEnabled(AppCodexDesktop) {
		t.Errorf("Codex Desktop App must default on")
	}
	if p.IsEnabled(AppClaudeCode) {
		t.Errorf("Claude Code must default off until explicitly enabled")
	}
}

func TestIsEnabledUnknownID(t *testing.T) {
	p := DefaultPolicy()
	if p.IsEnabled(AppID("foo")) {
		t.Errorf("unknown id must report disabled")
	}
}

func TestNewManagerMissingFileReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.toml")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if !m.Policy().IsEnabled(AppCodexCLI) {
		t.Errorf("missing file should yield default policy")
	}
	if m.Policy().IsEnabled(AppClaudeCode) {
		t.Errorf("default policy should not enable Claude Code")
	}
}

func TestNewManagerEmptyPathReturnsDefault(t *testing.T) {
	m, err := NewManager("")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !m.Policy().IsEnabled(AppCodexCLI) {
		t.Errorf("empty path → default policy expected")
	}
}

func TestNewManagerEmptyFileReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !m.Policy().IsEnabled(AppCodexCLI) {
		t.Errorf("empty file should yield default policy")
	}
}

func TestNewManagerMalformedFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("this is { not toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(path); err == nil {
		t.Errorf("expected error on malformed TOML")
	}
}

func TestNewManagerStatPermissionError(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(inner, "apps.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(inner, 0o000); err != nil {
		t.Skipf("chmod 000 unsupported in this environment: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(inner, 0o755) })

	if _, err := NewManager(path); err == nil {
		t.Errorf("expected error when parent directory is unreadable")
	}
}

func TestNewManagerStatErrorPropagation(t *testing.T) {
	// Stat on a path inside a non-existent parent returns
	// ENOTDIR / ENOENT but not "the file doesn't exist" - we want
	// the stat-error branch of NewManager to surface that.
	dir := t.TempDir()
	notADir := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(notADir, "apps.toml") // dir-component is a regular file
	if _, err := NewManager(path); err == nil {
		t.Errorf("expected error when path's parent is not a directory")
	}
}

func TestRoundTripSetEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled(AppCodexCLI, false); err != nil {
		t.Fatal(err)
	}
	if m.Policy().IsEnabled(AppCodexCLI) {
		t.Errorf("Codex CLI not disabled after SetEnabled")
	}
	// Re-open: file persistence.
	m2, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Policy().IsEnabled(AppCodexCLI) {
		t.Errorf("Codex CLI disabled state not persisted across re-open")
	}
	// Toggle back off.
	if err := m2.SetEnabled(AppCodexCLI, true); err != nil {
		t.Fatal(err)
	}
	m3, _ := NewManager(path)
	if !m3.Policy().IsEnabled(AppCodexCLI) {
		t.Errorf("toggle on did not persist")
	}
}

func TestSetEnabledClaudeRejected(t *testing.T) {
	m, _ := NewManager("")
	if err := m.SetEnabled(AppClaudeCode, true); err == nil {
		t.Fatal("Claude Code is parked; enabling must be rejected")
	}
	if err := m.SetEnabled(AppClaudeCode, false); err != nil {
		t.Fatalf("disabling parked Claude Code should be harmless: %v", err)
	}
}

func TestSetEnabledUnknownAppRejected(t *testing.T) {
	m, _ := NewManager("")
	if err := m.SetEnabled(AppID("nonsense"), true); err == nil {
		t.Errorf("expected error for unknown app id")
	}
}

func TestSetEnabledWriteError(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "ro")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(inner, "apps.toml")
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if err := os.Chmod(inner, 0o500); err != nil {
		t.Skipf("chmod unsupported: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(inner, 0o755) })
	if err := m.SetEnabled(AppCodexCLI, false); err == nil {
		t.Errorf("expected write error on read-only directory")
	}
}

func TestOnChangeListener(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, _ := NewManager(path)
	var mu sync.Mutex
	calls := 0
	var lastSeen Policy
	m.OnChange(func(p Policy) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		lastSeen = p
	})
	if err := m.SetEnabled(AppCodexDesktop, false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("OnChange fired %d times, want 1", calls)
	}
	if lastSeen.IsEnabled(AppCodexDesktop) {
		t.Errorf("listener saw stale policy")
	}
}

func TestReloadAppliesExternalEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, _ := NewManager(path)
	// Simulate external edit (operator changes the file).
	content := `schema_version = 1
[apps.codex_cli]
enabled = false
[apps.codex_desktop_app]
enabled = false
[apps.claude_code]
enabled = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.OnChange(func(Policy) { calls++ })
	pol, err := m.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if pol.IsEnabled(AppCodexCLI) {
		t.Errorf("Reload missed external disable of CLI")
	}
	if pol.IsEnabled(AppClaudeCode) {
		t.Errorf("Claude Code is parked; external enable must be ignored")
	}
	if calls != 1 {
		t.Errorf("Reload listener fired %d times, want 1", calls)
	}
}

func TestReloadEmptyPathNoOp(t *testing.T) {
	m, _ := NewManager("")
	pol, err := m.Reload()
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !pol.IsEnabled(AppCodexCLI) {
		t.Errorf("default policy lost after empty-path Reload")
	}
}

func TestReloadMalformedFileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := NewManager(path)
	if err := os.WriteFile(path, []byte("not { toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Reload(); err == nil {
		t.Errorf("expected reload error on malformed file")
	}
}

func TestAppFromUserAgentMatches(t *testing.T) {
	m, _ := NewManager("")
	cases := []struct {
		ua   string
		want AppID
		ok   bool
	}{
		{"codex_cli_rs/0.130.0 (Mac; arm64)", AppCodexCLI, true},
		{"codex-cli/0.130.0", AppCodexCLI, true},
		{"codex_desktop_app/2026.05", AppCodexDesktop, true},
		{"Codex.app/2026.05 (macOS)", AppCodexDesktop, true},
		{"Codex/2026.05", AppCodexDesktop, true},
		{"claude-code/0.18", AppClaudeCode, true},
		{"Mozilla/5.0 (Macintosh; Intel)", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := m.AppFromUserAgent(c.ua)
		if ok != c.ok || got != c.want {
			t.Errorf("AppFromUserAgent(%q) = (%q,%v) want (%q,%v)", c.ua, got, ok, c.want, c.ok)
		}
	}
}

func TestDetectedBinariesNoneInstalled(t *testing.T) {
	// Override detection with paths that definitely don't exist.
	m, _ := NewManager("")
	det := Detection{
		UAPrefixes: map[string]AppID{},
		BinaryPaths: map[AppID][]string{
			AppCodexCLI: {"/definitely/does/not/exist/codex"},
		},
	}
	m.detection.Store(&det)
	got := m.DetectedBinaries()
	if len(got) != 0 {
		t.Errorf("expected no detections, got %v", got)
	}
}

func TestDetectedBinariesFindsRealFile(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, _ := NewManager("")
	det := Detection{
		BinaryPaths: map[AppID][]string{AppCodexCLI: {fakeBin}},
	}
	m.detection.Store(&det)
	got := m.DetectedBinaries()
	if len(got[AppCodexCLI]) != 1 || got[AppCodexCLI][0] != fakeBin {
		t.Errorf("expected detection of %s, got %v", fakeBin, got)
	}
}

func TestDetectedBinariesExpandsTilde(t *testing.T) {
	m, _ := NewManager("")
	det := Detection{
		BinaryPaths: map[AppID][]string{
			AppCodexCLI: {"~/.this-cannot-exist-xyzzy/codex"},
		},
	}
	m.detection.Store(&det)
	got := m.DetectedBinaries()
	if len(got) != 0 {
		t.Errorf("tilde-expanded nonsense path should not detect, got %v", got)
	}
}

func TestPolicyAndDetectionAtomicNilSafe(t *testing.T) {
	m := &Manager{}
	// Reading from a never-stored Manager must not panic.
	if !m.Policy().IsEnabled(AppCodexCLI) {
		t.Errorf("nil-pointer Policy should fall back to default")
	}
	det := m.Detection()
	if len(det.UAPrefixes) == 0 {
		t.Errorf("nil-pointer Detection should fall back to default")
	}
}

func TestWriteToFileSchemaVersionDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	pol := Policy{Enabled: map[AppID]bool{AppCodexCLI: true}} // SchemaVersion=0 triggers default
	if err := writeToFile(path, pol); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "schema_version = 1") {
		t.Errorf("schema_version default not written: %s", data)
	}
}

func TestWriteToFileEmptyPathNoOp(t *testing.T) {
	if err := writeToFile("", DefaultPolicy()); err != nil {
		t.Errorf("empty path should no-op, got %v", err)
	}
}

func TestWriteToFileMkdirError(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "not-dir")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeToFile(filepath.Join(notDir, "apps.toml"), DefaultPolicy()); err == nil {
		t.Fatal("expected mkdir error when parent component is a file")
	}
}

func TestLoadFromFileUnknownAppIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	content := `schema_version = 1
[apps.codex_cli]
enabled = true
[apps.future_app_we_dont_know]
enabled = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pol, err := loadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !pol.IsEnabled(AppCodexCLI) {
		t.Errorf("known app lost")
	}
	if pol.IsEnabled(AppID("future_app_we_dont_know")) {
		t.Errorf("unknown app should not appear as enabled")
	}
}

func TestLoadFromFileMissingFile(t *testing.T) {
	_, err := loadFromFile(filepath.Join(t.TempDir(), "nope.toml"))
	if err == nil {
		t.Errorf("expected error on missing file")
	}
}

func TestSetEnabledFillsMissingKnownApps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, _ := NewManager(path)
	// Replace the in-memory policy with one that's missing entries
	// (simulates a partial-schema TOML or programmatic construction).
	partial := &Policy{
		SchemaVersion: 1,
		Enabled:       map[AppID]bool{AppCodexCLI: true},
	}
	m.policy.Store(partial)
	if err := m.SetEnabled(AppCodexDesktop, true); err != nil {
		t.Fatal(err)
	}
	got := m.Policy()
	for _, id := range KnownApps {
		if _, ok := got.Enabled[id]; !ok {
			t.Errorf("known app %q missing from policy after SetEnabled", id)
		}
	}
}

func TestSetEnabledFillsMissingSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, _ := NewManager(path)
	// Store a policy whose SchemaVersion is 0 so SetEnabled's
	// default-fill branch fires.
	zeroVer := &Policy{
		SchemaVersion: 0,
		Enabled:       map[AppID]bool{},
	}
	m.policy.Store(zeroVer)
	if err := m.SetEnabled(AppCodexCLI, false); err != nil {
		t.Fatal(err)
	}
	if got := m.Policy().SchemaVersion; got != 1 {
		t.Errorf("schema_version=%d, want default 1", got)
	}
}

func TestSetEnabledTwiceSamePolicyStillFiresListener(t *testing.T) {
	// Sanity: calling SetEnabled(X, false) twice produces two listener
	// fires (we don't dedupe). Documented behaviour.
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.toml")
	m, _ := NewManager(path)
	calls := 0
	m.OnChange(func(Policy) { calls++ })
	_ = m.SetEnabled(AppClaudeCode, false)
	_ = m.SetEnabled(AppClaudeCode, false)
	if calls != 2 {
		t.Errorf("expected 2 listener fires, got %d", calls)
	}
}
