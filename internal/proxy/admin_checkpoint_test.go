package proxy

import (
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/checkpoints"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/toolarchive"
)

func TestAdminStatusSnapshot_CheckpointsAndArchive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := checkpoints.Capture(checkpoints.DefaultDir(home), checkpoints.CaptureInput{
		Trigger: checkpoints.TriggerManual,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "tool-1",
		SessionID: "sess-1",
		Command:   "npm test",
		Output:    strings.Repeat("line\n", 800),
	}); err != nil {
		t.Fatal(err)
	}

	p := New(config.Defaults())
	status := p.AdminStatusSnapshot()
	if status.Checkpoints.Count != 1 || status.Checkpoints.Captures != 1 {
		t.Fatalf("checkpoint status=%+v", status.Checkpoints)
	}
	if status.ToolArchive.Count != 1 || status.ToolArchive.Archived != 1 {
		t.Fatalf("archive status=%+v", status.ToolArchive)
	}
}
