package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/toolarchive"
)

func TestHandleCheckpointCmd_CaptureAndRestore(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleCheckpointCmd([]string{"capture"})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	id := strings.TrimSpace(buf.String())
	if id == "" {
		t.Fatal("expected checkpoint id")
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleCheckpointCmd([]string{"restore", id})
	_ = w.Close()
	os.Stdout = origStdout
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Slimference checkpoint") {
		t.Fatalf("restore output=%q", buf.String())
	}
}

func TestHandleExpandCmd_PrintsArchivedBody(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	entry, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "archive-1",
		SessionID: "sess-1",
		Command:   "npm test",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil {
		t.Fatal("expected archive entry")
	}

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleExpandCmd([]string{entry.ID})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "line") {
		t.Fatalf("expand output=%q", buf.String())
	}
}

func TestHandlePostToolCmd_ArchivesLargeOutputWhenMetadataPresent(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origHome := osUserHomeDir
	origConfigLoad := configLoadFn
	origStdout := os.Stdout
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osUserHomeDir = origHome
		configLoadFn = origConfigLoad
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	termIsTerminalFn = func(int) bool { return false }
	osUserHomeDir = func() (string, error) { return home, nil }
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 40
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	payload, err := json.Marshal(map[string]string{
		"session_id":    "sess-1",
		"tool_name":     "Bash",
		"tool_use_id":   "tool-1",
		"command":       "npm test",
		"tool_response": strings.Repeat("line\n", 800),
	})
	if err != nil {
		t.Fatal(err)
	}
	readStdinAll = func() ([]byte, error) { return payload, nil }

	r, w, _ := os.Pipe()
	os.Stdout = w
	handlePostToolCmd(nil)
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "slim://archive/tool-1") || !strings.Contains(out, "slimference expand tool-1") {
		t.Fatalf("posttool output=%q", out)
	}
	if _, err := os.Stat(filepath.Join(toolarchive.DefaultDir(home), "entries", "tool-1.json")); err != nil {
		t.Fatalf("archive metadata missing: %v", err)
	}
}
