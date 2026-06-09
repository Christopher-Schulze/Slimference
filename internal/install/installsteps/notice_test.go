package installsteps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
)

func newNotice(t *testing.T) (*Notice, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "SLIMFERENCE.md")
	return &Notice{
		Path:    path,
		Title:   "Slimference touched this folder",
		Body:    "We installed hooks here.",
		AppName: "Test App",
		Now:     func() time.Time { return time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC) },
		Version: "v2.0.0",
	}, dir
}

func TestNoticeApplyWritesFile(t *testing.T) {
	n, _ := newNotice(t)
	if err := n.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	data, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, noticeMarker) {
		t.Error("marker missing")
	}
	if !strings.Contains(s, "Slimference touched this folder") {
		t.Error("title missing")
	}
	if !strings.Contains(s, "We installed hooks here.") {
		t.Error("body missing")
	}
	if !strings.Contains(s, "slimference uninstall") {
		t.Error("revert instructions missing")
	}
	if !strings.Contains(s, "Test App") {
		t.Error("AppName missing")
	}
	if !strings.Contains(s, "v2.0.0") {
		t.Error("version missing in footer")
	}
	if !strings.Contains(s, "2026-05-16T12:00:00Z") {
		t.Error("timestamp missing in footer")
	}
}

func TestNoticeApplyIdempotent(t *testing.T) {
	n, _ := newNotice(t)
	if err := n.Apply(context.Background()); err != nil {
		t.Fatalf("first: %v", err)
	}
	before, _ := os.ReadFile(n.Path)
	if err := n.Apply(context.Background()); err != nil {
		t.Fatalf("second: %v", err)
	}
	after, _ := os.ReadFile(n.Path)
	if string(before) != string(after) {
		t.Errorf("idempotency broken")
	}
}

func TestNoticeReverseRemovesFile(t *testing.T) {
	n, _ := newNotice(t)
	if err := n.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := n.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if _, err := os.Stat(n.Path); !os.IsNotExist(err) {
		t.Errorf("file not removed: %v", err)
	}
}

func TestNoticeReverseMissingFileNoOp(t *testing.T) {
	n, _ := newNotice(t)
	if err := n.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse on missing: %v", err)
	}
}

func TestNoticeReverseReadError(t *testing.T) {
	dir := t.TempDir()
	n := &Notice{Path: dir}
	if err := n.Reverse(context.Background()); err == nil {
		t.Fatal("expected read error when notice path is a directory")
	}
}

func TestNoticeReverseLeavesHumanReplacedFile(t *testing.T) {
	n, _ := newNotice(t)
	// Pre-write a file WITHOUT our marker - a human's edit.
	if err := os.WriteFile(n.Path, []byte("# my own notes\nnothing slimference here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := n.Reverse(context.Background()); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if _, err := os.Stat(n.Path); err != nil {
		t.Errorf("file was removed despite human edit: %v", err)
	}
}

func TestNoticeReverseEmptyPathErrors(t *testing.T) {
	n := &Notice{}
	if err := n.Reverse(context.Background()); err == nil {
		t.Error("expected error on empty Path")
	}
}

func TestNoticeApplyValidatesFields(t *testing.T) {
	tests := []struct {
		name string
		n    Notice
	}{
		{"empty path", Notice{Title: "x", AppName: "y"}},
		{"empty title", Notice{Path: "/tmp/x.md", AppName: "y"}},
		{"empty appname", Notice{Path: "/tmp/x.md", Title: "y"}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.n.Apply(context.Background()); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestNoticeApplyMissingDirRejected(t *testing.T) {
	n := &Notice{
		Path:    "/nonexistent/dir/SLIMFERENCE.md",
		Title:   "x",
		AppName: "y",
	}
	if err := n.Apply(context.Background()); err == nil {
		t.Error("expected error on missing parent dir")
	}
}

func TestNoticeApplyContextCancelled(t *testing.T) {
	n, _ := newNotice(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := n.Apply(ctx); err == nil {
		t.Error("expected ctx.Err")
	}
}

func TestNoticeReverseContextCancelled(t *testing.T) {
	n, _ := newNotice(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := n.Reverse(ctx); err == nil {
		t.Error("expected ctx.Err")
	}
}

func TestNoticeInspectStates(t *testing.T) {
	n, _ := newNotice(t)
	if got := n.Inspect(context.Background()); got != reversibility.StateAbsent {
		t.Errorf("pre-apply Inspect = %v, want StateAbsent", got)
	}
	if err := n.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := n.Inspect(context.Background()); got != reversibility.StatePresent {
		t.Errorf("post-apply Inspect = %v, want StatePresent", got)
	}
}

func TestNoticeInspectHumanReplaced(t *testing.T) {
	n, _ := newNotice(t)
	if err := os.WriteFile(n.Path, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := n.Inspect(context.Background()); got != reversibility.StateAbsent {
		t.Errorf("human-replaced file should read as Absent, got %v", got)
	}
}

func TestNoticeInspectEmptyPath(t *testing.T) {
	n := &Notice{}
	if got := n.Inspect(context.Background()); got != reversibility.StateUnknown {
		t.Errorf("empty Path Inspect = %v, want StateUnknown", got)
	}
}

func TestNoticeInspectReadError(t *testing.T) {
	n := &Notice{Path: t.TempDir()}
	if got := n.Inspect(context.Background()); got != reversibility.StateUnknown {
		t.Errorf("directory Path Inspect = %v, want StateUnknown", got)
	}
}

func TestNoticeNameDefault(t *testing.T) {
	n := &Notice{Path: "/foo/SLIMFERENCE.md"}
	if n.Name() != "notice.slimference" {
		t.Errorf("Name=%q", n.Name())
	}
}

func TestNoticeNameOverride(t *testing.T) {
	n := &Notice{StepName: "notice.codex"}
	if n.Name() != "notice.codex" {
		t.Errorf("Name=%q", n.Name())
	}
}

func TestNoticeNameFallbackEmpty(t *testing.T) {
	n := &Notice{Path: ".md"} // basename without ext = ""
	if got := n.Name(); got != "notice" {
		t.Errorf("Name=%q want 'notice'", got)
	}
}

func TestNoticeRenderWithoutBody(t *testing.T) {
	n := &Notice{
		Path:    "/tmp/x.md",
		Title:   "T",
		AppName: "A",
	}
	out := n.render(time.Unix(0, 0).UTC())
	if !strings.Contains(out, "(no extra detail)") {
		t.Error("missing default body")
	}
}

func TestNoticeRenderWithoutVersion(t *testing.T) {
	n := &Notice{
		Path:    "/tmp/x.md",
		Title:   "T",
		AppName: "A",
		Body:    "x",
	}
	out := n.render(time.Unix(0, 0).UTC())
	if !strings.Contains(out, "Slimference unknown at") {
		t.Errorf("missing 'unknown' version, got: %s", out)
	}
}

func TestNoticeClockDefaultUTC(t *testing.T) {
	n := &Notice{}
	if got := n.clock().Location(); got != time.UTC {
		t.Fatalf("clock location=%v want UTC", got)
	}
}
