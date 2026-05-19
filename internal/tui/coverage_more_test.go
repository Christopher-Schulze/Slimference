package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type richMockProxy struct {
	*mockProxy
	checkpoint CheckpointStatus
	archive    ToolArchiveStatus
}

func (m *richMockProxy) GetCheckpointStatus() CheckpointStatus { return m.checkpoint }
func (m *richMockProxy) GetToolArchiveStatus() ToolArchiveStatus {
	return m.archive
}

func TestSetupSelectionHelpersAndFormatting(t *testing.T) {
	m := NewModel(newMockProxy())
	m.SetServiceControl(&mockServiceControl{})
	m.enterSetupView()
	if m.setupStep != 1 || m.setupCursor != 0 {
		t.Fatalf("enterSetupView step=%d cursor=%d", m.setupStep, m.setupCursor)
	}
	m.selectSetupStep(-1)
	if m.setupStep != 1 || m.setupCursor != 0 {
		t.Fatalf("select negative step=%d cursor=%d", m.setupStep, m.setupCursor)
	}

	m.selectSetupStep(2)
	if m.setupStep != 3 || m.setupCursor != 2 {
		t.Fatalf("select step=%d cursor=%d", m.setupStep, m.setupCursor)
	}
	m.selectSetupStep(9)
	if m.setupStep != 5 || m.setupCursor != 4 {
		t.Fatalf("invalid select changed state: step=%d cursor=%d", m.setupStep, m.setupCursor)
	}
	m.moveSetupCursor(-1)
	if m.setupCursor != 3 || m.setupStep != 4 {
		t.Fatalf("move up step=%d cursor=%d", m.setupStep, m.setupCursor)
	}
	m.setupCursor = 0
	m.setupStep = 1
	m.moveSetupCursor(-1)
	if m.setupCursor != 0 || m.setupStep != 1 {
		t.Fatalf("move clamp low step=%d cursor=%d", m.setupStep, m.setupCursor)
	}
	m.moveSetupCursor(99)
	if m.setupCursor != 4 || m.setupStep != 5 {
		t.Fatalf("move clamp step=%d cursor=%d", m.setupStep, m.setupCursor)
	}
	m.setupCursor = 1
	m.setupStep = 0
	m.syncSetupSelection()
	if m.setupStep != 2 {
		t.Fatalf("sync step=%d", m.setupStep)
	}

	if got := formatAgo(time.Now().Add(-2 * time.Minute)); got == "" {
		t.Fatal("formatAgo should not be empty")
	}
	if got := formatAgo(time.Now().Add(-5 * time.Second)); !strings.Contains(got, "s ago") {
		t.Fatalf("formatAgo seconds=%q", got)
	}
	if got := formatAgo(time.Now().Add(-2 * time.Hour)); !strings.Contains(got, "h ago") {
		t.Fatalf("formatAgo hours=%q", got)
	}
	if got := formatAgo(time.Time{}); got != "never" {
		t.Fatalf("formatAgo zero=%q", got)
	}
	if got := formatStatusTime(time.Time{}); got != "never" {
		t.Fatalf("formatStatusTime=%q", got)
	}
	if got := fallbackLabel("", "none"); got != "none" {
		t.Fatalf("fallbackLabel=%q", got)
	}
	if got := fallbackLabel("set", "none"); got != "set" {
		t.Fatalf("fallbackLabel keep=%q", got)
	}
	if got := formatBytesCompact(1536); got != "1.5K" {
		t.Fatalf("formatBytesCompact=%q", got)
	}
	if got := formatBytesCompact(-5); got != "0" {
		t.Fatalf("formatBytesCompact negative=%q", got)
	}
	if got := formatBytesCompact(999); got != "999" {
		t.Fatalf("formatBytesCompact small=%q", got)
	}
	if got := formatBytesCompact(1_500_000); got != "1.5M" {
		t.Fatalf("formatBytesCompact large=%q", got)
	}

	idle := NewModel(newMockProxy())
	idle.enterSetupView()
	if idle.setupStep != 0 || idle.setupCursor != 0 {
		t.Fatalf("idle enter setup step=%d cursor=%d", idle.setupStep, idle.setupCursor)
	}
	idle.syncSetupSelection()
	if idle.setupStep != 0 {
		t.Fatalf("idle sync step=%d", idle.setupStep)
	}
	idle.selectSetupStep(-1)
	if idle.setupStep != 0 {
		t.Fatalf("idle select step=%d", idle.setupStep)
	}
	idle.moveSetupCursor(1)
	if idle.setupStep != 0 {
		t.Fatalf("idle move step=%d", idle.setupStep)
	}

	done := NewModel(newMockProxy())
	done.SetServiceControl(&mockServiceControl{transparentStatus: TransparentStatus{
		CAExists:           true,
		CATrusted:          true,
		AutoStartInstalled: true,
		ProxyArmed:         true,
	}, codexRouteStatus: CodexRouteStatus{
		Exists:          true,
		Enabled:         true,
		Complete:        true,
		DaemonReachable: true,
		AutoMode:        "wss_phasef",
		WSSCertified:    true,
	}})
	done.hookStatus = HookStatus{Claude: true, Codex: true}
	origHomeFn := userHomeDirFn
	home := t.TempDir()
	userHomeDirFn = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDirFn = origHomeFn })
	if err := os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	done.enterSetupView()
	if done.setupStep != 1 || done.setupCursor != 0 {
		t.Fatalf("done enter setup step=%d cursor=%d", done.setupStep, done.setupCursor)
	}
}

func TestPanelsRenderCheckpointAndArchiveData(t *testing.T) {
	t.Parallel()

	p := &richMockProxy{
		mockProxy: newMockProxy(),
		checkpoint: CheckpointStatus{
			Captures:    2,
			Restores:    1,
			Count:       2,
			Bytes:       2048,
			LastTrigger: "manual",
			LastCapture: time.Now(),
		},
		archive: ToolArchiveStatus{
			Archived:    3,
			Expanded:    1,
			Count:       2,
			BytesRaw:    4096,
			BytesStored: 2048,
		},
	}
	p.readStatus = ReadCacheStatus{Evaluations: 5, Blocks: 3, DeltaBlocks: 1, UnchangedBlocks: 2, Sessions: 1, TrackedFiles: 4}
	m := NewModel(p)
	m.width = 120
	m.height = 40
	m.latestSnap.TotalRequests = 2
	m.latestSnap.PromptCacheReadTokens = 3200
	m.latestSnap.PromptCacheCreateTokens = 800

	left := strings.Join(m.buildLeftPanel(36), "\n")
	right := strings.Join(m.buildRightPanel(70), "\n")
	for _, want := range []string{"CHECKPOINTS", "TOOL ARCHIVE", "READ CACHE", "prompt cache"} {
		if !strings.Contains(left+right, want) {
			t.Fatalf("panels missing %q in:\n%s\n%s", want, left, right)
		}
	}
}
