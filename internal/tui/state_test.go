package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlSKeyMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlS} }

// tuiStatePathOverride redirects the state file to a tempdir path for the
// duration of a single test and restores the original on cleanup.
func tuiStatePathOverride(t *testing.T, path string) {
	t.Helper()
	orig := tuiStatePathFn
	tuiStatePathFn = func() string { return path }
	t.Cleanup(func() { tuiStatePathFn = orig })
}

// TestPersistedState_Roundtrip saves a state, reloads it, and verifies every
// field made it through intact.
func TestPersistedState_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	tuiStatePathOverride(t, filepath.Join(dir, "nested", "tui_state.json"))

	want := PersistedState{
		ClaudeEnabled: true,
		CodexEnabled:  false,
		Layer1Enabled: true,
		Layer3Enabled: true,
		View:          "stats",
	}
	if err := SavePersistedState(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPersistedState()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != want {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}

// TestPersistedState_MissingFileReturnsNilNil is a boot-friendly fallback.
func TestPersistedState_MissingFileReturnsNilNil(t *testing.T) {
	tuiStatePathOverride(t, filepath.Join(t.TempDir(), "never.json"))
	s, err := LoadPersistedState()
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if s != nil {
		t.Fatalf("missing file must return nil state, got %+v", s)
	}
}

// TestPersistedState_EmptyFileReturnsNilNil handles zero-byte state files.
func TestPersistedState_EmptyFileReturnsNilNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	tuiStatePathOverride(t, path)
	s, err := LoadPersistedState()
	if err != nil || s != nil {
		t.Fatalf("empty file must return nil, got err=%v s=%+v", err, s)
	}
}

// TestPersistedState_CorruptFileReturnsErr surfaces parse errors so the
// caller can decide to boot to defaults.
func TestPersistedState_CorruptFileReturnsErr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	tuiStatePathOverride(t, path)
	if _, err := LoadPersistedState(); err == nil {
		t.Fatal("corrupt file must error")
	}
}

// TestPersistedState_ReadErrorSurfaced on non-IsNotExist errors.
func TestPersistedState_ReadErrorSurfaced(t *testing.T) {
	// Point the state path at a directory; os.ReadFile returns a generic
	// error rather than fs.ErrNotExist so the non-NotExist branch fires.
	tuiStatePathOverride(t, t.TempDir())
	if _, err := LoadPersistedState(); err == nil {
		t.Fatal("reading a directory must error")
	}
}

// TestDefaultTUIStatePathFallback covers the "home unavailable" branch.
func TestDefaultTUIStatePathFallback(t *testing.T) {
	orig := userHomeDirFn
	userHomeDirFn = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userHomeDirFn = orig })
	if got := defaultTUIStatePath(); got != ".slimference/tui_state.json" {
		t.Fatalf("fallback path: %q", got)
	}
	userHomeDirFn = func() (string, error) { return "/users/alice", nil }
	if got := defaultTUIStatePath(); got != "/users/alice/.slimference/tui_state.json" {
		t.Fatalf("home path: %q", got)
	}
}

// TestSavePersistedState_emptyPathIsNoop keeps the state file optional.
func TestSavePersistedState_emptyPathIsNoop(t *testing.T) {
	orig := tuiStatePathFn
	tuiStatePathFn = func() string { return "" }
	t.Cleanup(func() { tuiStatePathFn = orig })
	if err := SavePersistedState(PersistedState{ClaudeEnabled: true}); err != nil {
		t.Fatalf("empty path must be a no-op, got %v", err)
	}
}

// TestSavePersistedState_mkdirFailureSurfaced when parent cannot be created.
func TestSavePersistedState_mkdirFailureSurfaced(t *testing.T) {
	// Put the file under a path component that is itself a regular file,
	// which forces MkdirAll to fail with a syscall error.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tuiStatePathOverride(t, filepath.Join(blocker, "nested", "tui_state.json"))
	if err := SavePersistedState(PersistedState{}); err == nil {
		t.Fatal("expected MkdirAll to fail under a file blocker")
	}
}

// TestViewModeRoundtrip covers every view label both ways.
func TestViewModeRoundtrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v   ViewMode
		tag string
	}{
		{ViewMain, "main"},
		{ViewStats, "stats"},
		{ViewDebug, "debug"},
		{ViewSetup, "setup"},
	}
	for _, tc := range cases {
		if got := viewModeToString(tc.v); got != tc.tag {
			t.Errorf("viewModeToString(%d) = %q, want %q", tc.v, got, tc.tag)
		}
		if got, ok := viewModeFromString(tc.tag); !ok || got != tc.v {
			t.Errorf("viewModeFromString(%q) = %v,%v", tc.tag, got, ok)
		}
	}
	// Unknown string must signal ok=false.
	if _, ok := viewModeFromString("bogus"); ok {
		t.Fatal("unknown tag must fail roundtrip")
	}
	// Invalid ViewMode must serialise to empty string.
	if got := viewModeToString(ViewMode(99)); got != "" {
		t.Fatalf("invalid mode: %q", got)
	}
}

// TestApplyPersistedState_ProjectsOntoProxy propagates toggles and the view
// onto the proxy via SetProviderEnabled / SetLayerEnabled. Reuses the
// package-local mockProxy test helper.
func TestApplyPersistedState_ProjectsOntoProxy(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := &Model{proxy: p}
	applyPersistedState(m, PersistedState{
		ClaudeEnabled: false,
		CodexEnabled:  true,
		Layer1Enabled: false,
		Layer3Enabled: false,
		View:          "debug",
	})
	if p.claudeEnabled || !p.codexEnabled {
		t.Fatalf("provider projection: claude=%v codex=%v", p.claudeEnabled, p.codexEnabled)
	}
	if p.layer1Enabled || p.layer3Enabled {
		t.Fatalf("layer projection: %+v", p)
	}
	if m.view != ViewDebug {
		t.Fatalf("view projection: %v", m.view)
	}
}

// TestApplyPersistedState_UnknownViewPreserved when the tag is unknown.
func TestApplyPersistedState_UnknownViewPreserved(t *testing.T) {
	t.Parallel()
	p := newMockProxy()
	m := &Model{proxy: p, view: ViewSetup}
	applyPersistedState(m, PersistedState{View: "???"})
	if m.view != ViewSetup {
		t.Fatal("unknown tag must leave view untouched")
	}
}

// TestApplyPersistedState_NilProxy is safe when no proxy is attached.
func TestApplyPersistedState_NilProxy(t *testing.T) {
	t.Parallel()
	m := &Model{}
	applyPersistedState(m, PersistedState{ClaudeEnabled: true, View: "main"})
	if !m.claudeEnabled {
		t.Fatal("model-local state must still be applied even without proxy")
	}
}

// TestStateFromModel_Snapshot captures the model's togglable surface.
func TestStateFromModel_Snapshot(t *testing.T) {
	t.Parallel()
	m := &Model{
		claudeEnabled: true,
		codexEnabled:  false,
		layer1Enabled: true,
		layer3Enabled: true,
		view:          ViewStats,
	}
	s := stateFromModel(m)
	want := PersistedState{
		ClaudeEnabled: true,
		Layer1Enabled: true,
		Layer3Enabled: true,
		View:          "stats",
	}
	if s != want {
		t.Fatalf("snapshot mismatch: %+v want %+v", s, want)
	}
}

// TestNewModel_AppliesPersistedState wires load + apply during construction.
func TestNewModel_AppliesPersistedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_state.json")
	tuiStatePathOverride(t, path)
	if err := SavePersistedState(PersistedState{
		ClaudeEnabled: false,
		CodexEnabled:  false,
		Layer1Enabled: true,
		Layer3Enabled: true,
		View:          "stats",
	}); err != nil {
		t.Fatal(err)
	}
	p := newMockProxy()
	m := NewModel(p)
	if m.view != ViewStats {
		t.Fatalf("expected view restored to stats, got %v", m.view)
	}
	if m.claudeEnabled || m.codexEnabled {
		t.Fatalf("providers must follow persisted state: claude=%v codex=%v", m.claudeEnabled, m.codexEnabled)
	}
	if !p.layer1Enabled || !p.layer3Enabled {
		t.Fatalf("proxy toggles must track persisted state: %+v", p)
	}
}

// TestSavePersistedState_marshalError exercises the (normally unreachable)
// json error path by stubbing stateMarshalFn.
func TestSavePersistedState_marshalError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_state.json")
	tuiStatePathOverride(t, path)
	orig := stateMarshalFn
	stateMarshalFn = func(any) ([]byte, error) { return nil, errors.New("marshal fail") }
	t.Cleanup(func() { stateMarshalFn = orig })
	if err := SavePersistedState(PersistedState{ClaudeEnabled: true}); err == nil {
		t.Fatal("expected marshal error to surface")
	}
}

// TestUpdate_ctrlSSavePreferences exercises the ctrl+s branch on Update:
// success path writes to the tempdir state file.
func TestUpdate_ctrlSSavePreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_state.json")
	tuiStatePathOverride(t, path)

	p := newMockProxy()
	m := NewModel(p)
	m.view = ViewDebug
	updated, _ := m.Update(ctrlSKeyMsg())
	if got, ok := updated.(Model); !ok || got.flashMsg != "preferences saved" {
		t.Fatalf("flash must confirm save, got %+v", updated)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("state file must exist after ctrl+s: %v", err)
	}
	if !contains(string(data), "\"view\": \"debug\"") {
		t.Fatalf("state file missing view=debug: %s", data)
	}
}

// TestUpdate_ctrlSSaveFailureFlashesError covers the failure path of the
// ctrl+s handler via the marshal stub.
func TestUpdate_ctrlSSaveFailureFlashesError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_state.json")
	tuiStatePathOverride(t, path)
	orig := stateMarshalFn
	stateMarshalFn = func(any) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { stateMarshalFn = orig })

	m := NewModel(newMockProxy())
	updated, _ := m.Update(ctrlSKeyMsg())
	got, ok := updated.(Model)
	if !ok || !contains(got.flashMsg, "save failed") {
		t.Fatalf("flash must announce failure, got %+v", updated)
	}
}

// contains is a tiny helper to avoid another strings import in this file.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	n, m := len(s), len(sub)
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return i
		}
	}
	return -1
}

// TestNewModel_CorruptStateIgnored boots to proxy-derived defaults.
func TestNewModel_CorruptStateIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui_state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	tuiStatePathOverride(t, path)
	p := newMockProxy()
	m := NewModel(p)
	if m.view != ViewMain {
		t.Fatalf("corrupt state must fall back to default view, got %v", m.view)
	}
	if !m.claudeEnabled {
		t.Fatal("corrupt state must fall back to proxy-derived toggles")
	}
}
