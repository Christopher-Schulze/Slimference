package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/sessions"
)

func TestLoadLatestHookTurnDebugStatusFromDir(t *testing.T) {
	dir := t.TempDir()
	if err := sessions.StartHookSession(dir, "sess-debug"); err != nil {
		t.Fatalf("StartHookSession: %v", err)
	}
	if err := sessions.ObserveHookTool(dir, "sess-debug", "Bash", "git status --short"); err != nil {
		t.Fatalf("ObserveHookTool: %v", err)
	}
	if err := sessions.ObserveHookFile(dir, "sess-debug", "/repo/main.go", "read"); err != nil {
		t.Fatalf("ObserveHookFile read: %v", err)
	}
	if err := sessions.ObserveHookFile(dir, "sess-debug", "/repo/main.go", "edit"); err != nil {
		t.Fatalf("ObserveHookFile edit: %v", err)
	}
	if _, _, err := sessions.ObserveHookGitPathList(dir, "sess-debug", "/repo", "git status", []string{"main.go", "go.mod"}); err != nil {
		t.Fatalf("ObserveHookGitPathList: %v", err)
	}

	status := loadLatestHookTurnDebugStatusFromDir(dir)
	if !status.Present || status.Error != "" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.SessionID != "sess-debug" || status.TurnID != "turn-1" {
		t.Fatalf("wrong identity: %+v", status)
	}
	if len(status.Tools) != 1 || len(status.FilesRead) != 1 || len(status.FilesEdited) != 1 || len(status.GitPathLists) != 1 {
		t.Fatalf("wrong observation counts: %+v", status)
	}
}

func TestLoadLatestHookTurnDebugStatusFromDirEmptyAndBroken(t *testing.T) {
	missing := t.TempDir() + "/missing"
	if status := loadLatestHookTurnDebugStatusFromDir(missing); status.Present || status.Error != "" {
		t.Fatalf("missing dir should be quiet, got %+v", status)
	}
	if status := loadLatestHookTurnDebugStatusFromDir(t.TempDir()); status.Present || status.Error != "" {
		t.Fatalf("empty dir should be quiet, got %+v", status)
	}
	skipOnly := t.TempDir()
	if err := os.WriteFile(skipOnly+"/skip.txt", []byte("ignored"), 0o600); err != nil {
		t.Fatalf("write ignored-only state: %v", err)
	}
	if status := loadLatestHookTurnDebugStatusFromDir(skipOnly); status.Present || status.Error != "" {
		t.Fatalf("ignored files should be quiet, got %+v", status)
	}

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/skip.txt", []byte("ignored"), 0o600); err != nil {
		t.Fatalf("write ignored state: %v", err)
	}
	if err := os.Symlink("missing-target", dir+"/dangling.json"); err != nil {
		t.Fatalf("symlink dangling state: %v", err)
	}
	if err := os.WriteFile(dir+"/broken.json", []byte("{"), 0o600); err != nil {
		t.Fatalf("write broken state: %v", err)
	}
	status := loadLatestHookTurnDebugStatusFromDir(dir)
	if !status.Present || status.Error == "" {
		t.Fatalf("broken state should be surfaced, got %+v", status)
	}

	file := t.TempDir() + "/not-dir"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write not-dir marker: %v", err)
	}
	if status := loadLatestHookTurnDebugStatusFromDir(file); status.Error == "" {
		t.Fatalf("non-directory read should surface error")
	}
	_ = loadLatestHookTurnDebugStatus()
}

func TestRenderHookTurnDiagnostics(t *testing.T) {
	m := NewModel(newMockProxy())
	status := hookTurnDebugStatus{
		Present:     true,
		SessionID:   "session-with-a-very-long-id",
		TurnID:      "turn-2",
		Closed:      true,
		UpdatedAt:   time.Now().Add(-2 * time.Minute),
		Tools:       []string{"Read: cat internal/a.go", "Bash: git status --short", "Bash: git diff --name-only", "Bash: go test ./..."},
		FilesRead:   []string{"/repo/internal/a.go"},
		FilesEdited: []string{"/repo/internal/a.go"},
		GitPathLists: []sessions.HookGitPathListState{{
			Source: "git status --short",
			CWD:    "/repo",
			Count:  2,
		}},
	}
	out := renderHookTurnDiagnostics(&m, status)
	for _, want := range []string{"HOOK TURN STATE", "turn-2", "closed", "tools 4", "read 1", "edited 1", "git-lists 1", "gate:", "git last"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q: %s", want, out)
		}
	}
	if hints := hookTurnDecisionHints(status); len(hints) != 2 {
		t.Fatalf("expected edit + git decision hints, got %v", hints)
	}
	if hints := hookTurnDecisionHints(hookTurnDebugStatus{FilesRead: []string{"a.go"}}); len(hints) != 1 || !strings.Contains(hints[0], "AST") {
		t.Fatalf("expected read-only AST hint, got %v", hints)
	}
	if !strings.Contains(compactDebugList("tools", status.Tools, 3), "+1") {
		t.Fatalf("compactDebugList should report omitted entries")
	}
	if got := compactDebugLabel("abcdef", 1); got != "a" {
		t.Fatalf("compactDebugLabel tiny limit = %q", got)
	}
}

func TestRenderHookTurnDiagnosticsEmptyAndError(t *testing.T) {
	m := NewModel(newMockProxy())
	if out := renderHookTurnDiagnostics(&m, hookTurnDebugStatus{}); !strings.Contains(out, "No hook turn-state") {
		t.Fatalf("empty render: %s", out)
	}
	if out := renderHookTurnDiagnostics(&m, hookTurnDebugStatus{Error: "boom"}); !strings.Contains(out, "boom") {
		t.Fatalf("error render: %s", out)
	}
}
