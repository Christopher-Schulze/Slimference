package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/contentarchive"
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

func TestHandleExpandCmd_FallsBackToContentArchive(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	// Toolarchive holds nothing; contentarchive holds the requested entry.
	original := strings.Repeat("// archived comment line that is long enough\n", 8)
	entry, err := contentarchive.Put(contentarchive.DefaultDir(home), contentarchive.Input{
		SessionID:    "sess-content",
		MessageIndex: 1,
		BlockIndex:   0,
		SubLayer:     "comment_strip",
		Original:     original,
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		t.Fatalf("contentarchive put: entry=%#v err=%v", entry, err)
	}

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleExpandCmd([]string{entry.ID})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "archived comment line") {
		t.Fatalf("content-archive expand output=%q", buf.String())
	}
}

func TestHandleExpandBodyCmd_PrintsArchivedGoBody(t *testing.T) {
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
		ToolUseID: "body-1",
		SessionID: "sess-body",
		Command:   "cat service.go",
		Output: `package demo

type Service struct{}

func (s *Service) Run() int {
	return 42
}
` + strings.Repeat("// pad\n", 500),
	})
	if err != nil || entry == nil {
		t.Fatalf("archive entry=%+v err=%v", entry, err)
	}

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleExpandBodyCmd([]string{entry.ID, "Service.Run"})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "func (s *Service) Run() int") || !strings.Contains(buf.String(), "return 42") {
		t.Fatalf("expand-body output=%q", buf.String())
	}
}

func TestHandleExpandBodyCmd_FallsBackToContentArchive(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	entry, err := contentarchive.Put(contentarchive.DefaultDir(home), contentarchive.Input{
		SessionID:    "sess-content-body",
		MessageIndex: 1,
		BlockIndex:   0,
		SubLayer:     "go_ast",
		Original: `package demo

func Helper() int {
	return 7
}
` + strings.Repeat("// pad\n", 80),
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		t.Fatalf("contentarchive put: entry=%#v err=%v", entry, err)
	}

	r, w, _ := os.Pipe()
	os.Stdout = w
	handleExpandBodyCmd([]string{entry.ID, "Helper"})
	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "func Helper() int") || !strings.Contains(buf.String(), "return 7") {
		t.Fatalf("content expand-body output=%q", buf.String())
	}
}

func TestHandleExpandBodyCmd_ErrorPaths(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	if code, exited := captureExit(func() { handleExpandBodyCmd([]string{"missing"}) }); !exited || code != 1 {
		t.Fatalf("usage exit=%v code=%d", exited, code)
	}
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	if code, exited := captureExit(func() { handleExpandBodyCmd([]string{"id", "Symbol"}) }); !exited || code != 1 {
		t.Fatalf("home exit=%v code=%d", exited, code)
	}

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	if code, exited := captureExit(func() { handleExpandBodyCmd([]string{"missing", "Symbol"}) }); !exited || code != 1 {
		t.Fatalf("missing exit=%v code=%d", exited, code)
	}
	entry, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "body-miss",
		SessionID: "sess-body",
		Command:   "cat service.go",
		Output:    "package demo\nfunc Present() {}\n" + strings.Repeat("// pad\n", 500),
	})
	if err != nil || entry == nil {
		t.Fatalf("archive entry=%+v err=%v", entry, err)
	}
	if code, exited := captureExit(func() { handleExpandBodyCmd([]string{entry.ID, "Missing"}) }); !exited || code != 1 {
		t.Fatalf("symbol miss exit=%v code=%d", exited, code)
	}

	broken, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "body-broken",
		SessionID: "sess-body",
		Command:   "cat broken.go",
		Output:    "package demo\nfunc {\n" + strings.Repeat("// pad\n", 500),
	})
	if err != nil || broken == nil {
		t.Fatalf("broken archive entry=%+v err=%v", broken, err)
	}
	if code, exited := captureExit(func() { handleExpandBodyCmd([]string{broken.ID, "Missing"}) }); !exited || code != 1 {
		t.Fatalf("parse error exit=%v code=%d", exited, code)
	}

	good, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "body-write",
		SessionID: "sess-body",
		Command:   "cat service.go",
		Output:    "package demo\nfunc Present() {}\n" + strings.Repeat("// pad\n", 500),
	})
	if err != nil || good == nil {
		t.Fatalf("good archive entry=%+v err=%v", good, err)
	}
	_, w, _ := os.Pipe()
	_ = w.Close()
	os.Stdout = w
	if code, exited := captureExit(func() { handleExpandBodyCmd([]string{good.ID, "Present"}) }); !exited || code != 1 {
		t.Fatalf("write exit=%v code=%d", exited, code)
	}
	os.Stdout = origStdout

	errorHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(errorHome, ".slimference"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(errorHome, ".slimference", "content-archive"), []byte("not-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	osUserHomeDir = func() (string, error) { return errorHome, nil }
	if code, exited := captureExit(func() { handleExpandBodyCmd([]string{"missing", "Symbol"}) }); !exited || code != 1 {
		t.Fatalf("generic archive exit=%v code=%d", exited, code)
	}
}

func TestHandleExpandCmd_NotFoundExitsOne(t *testing.T) {
	origHome := osUserHomeDir
	origExit := exitFn
	defer func() {
		osUserHomeDir = origHome
		exitFn = origExit
	}()
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	handleExpandCmd([]string{"missing-id-xyz"})
	if len(exits) == 0 || exits[0] == 0 {
		t.Fatalf("expected non-zero exit, got %v", exits)
	}
}

func TestHandleExpandCmd_ContentArchiveWriteError(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	original := strings.Repeat("// archived comment line that is long enough\n", 8)
	entry, err := contentarchive.Put(contentarchive.DefaultDir(home), contentarchive.Input{
		SessionID:    "sess-content",
		MessageIndex: 1,
		BlockIndex:   0,
		SubLayer:     "comment_strip",
		Original:     original,
	}, contentarchive.Limits{})
	if err != nil || entry == nil {
		t.Fatalf("contentarchive put: entry=%#v err=%v", entry, err)
	}

	// os.Stdin is opened read-only; writing to it fails immediately.
	os.Stdout = os.Stdin

	origExit := exitFn
	defer func() { exitFn = origExit }()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }

	handleExpandCmd([]string{entry.ID})
	if len(exits) == 0 || exits[0] == 0 {
		t.Fatalf("expected non-zero exit on write error, got %v", exits)
	}
}

func TestHandlePostToolCmd_T93RepetitionMarkerOnThirdHit(t *testing.T) {
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
	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
	osUserHomeDir = func() (string, error) { return home, nil }
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 40
	cfg.Hooks.CodexPostToolMinTokens = 0
	configLoadFn = func() (*config.Config, error) { return cfg, nil }

	for i := 0; i < 3; i++ {
		payload, err := json.Marshal(map[string]string{
			"session_id":    "sess-rep",
			"tool_name":     "Bash",
			"tool_use_id":   "tool-rep-" + itoaT93(i),
			"command":       "git status",
			"tool_response": strings.Repeat("identical-line\n", 200),
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
		if i == 2 {
			// Third hit should emit the T93 repetition marker via the
			// archive's preview / additionalContext.
			if !strings.Contains(out, "identical to msg") {
				t.Fatalf("hit %d output missing repetition marker:\n%s", i, out)
			}
		}
	}
}

func itoaT93(i int) string {
	if i < 0 {
		return "-" + itoaT93(-i)
	}
	if i == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
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
	t.Setenv("SLIMFERENCE_CODEX_HOOK_MODE", "compact")
	osUserHomeDir = func() (string, error) { return home, nil }
	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 40
	cfg.Hooks.CodexPostToolMinTokens = 0
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
	if !strings.Contains(out, "local-archive://tool-1") || !strings.Contains(out, "Archive ID: tool-1") {
		t.Fatalf("posttool output=%q", out)
	}
	if _, err := os.Stat(filepath.Join(toolarchive.DefaultDir(home), "entries", "tool-1.json")); err != nil {
		t.Fatalf("archive metadata missing: %v", err)
	}
}
