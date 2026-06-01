package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

type layer2ErrReader struct{}

func (layer2ErrReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

func TestHandleLayer2Cmd_noArgs(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Cmd([]string{})
	_ = w.Close()
	os.Stderr = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1", exitCode)
	}
	if !strings.Contains(out, "enable|disable|status") {
		t.Fatalf("stderr = %q", out)
	}
}

func TestHandleLayer2Cmd_unknownSub(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Cmd([]string{"boom"})
	_ = w.Close()
	os.Stderr = old

	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1", exitCode)
	}
}

func TestHandleLayer2Enable_withoutAck(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Enable([]string{})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if exitCode != 2 {
		t.Fatalf("exit = %d, want 2", exitCode)
	}
	if !strings.Contains(out, "acknowledge-data-policy") {
		t.Fatalf("expected data policy explanation, got: %q", out)
	}
}

func TestHandleLayer2Enable_withAck(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Enable([]string{"--acknowledge-data-policy"})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "enabled") {
		t.Fatalf("expected enabled message, got: %q", out)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "true") {
		t.Fatalf("config should have layer2_enabled = true, got: %s", data)
	}
}

func TestHandleLayer2Enable_alreadyEnabled(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = true\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Enable([]string{"--acknowledge-data-policy"})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "already enabled") {
		t.Fatalf("expected already-enabled, got: %q", out)
	}
}

func TestHandleLayer2Enable_badConfig(t *testing.T) {
	explicitConfigPath = filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(explicitConfigPath, []byte("not valid [[["), 0644); err != nil {
		t.Fatal(err)
	}
	defer func() { explicitConfigPath = "" }()

	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Enable([]string{"--acknowledge-data-policy"})
	_ = w.Close()
	os.Stderr = old

	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1 for bad config", exitCode)
	}
}

func TestHandleLayer2Disable(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = true\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Disable()
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "disabled") {
		t.Fatalf("expected disabled, got: %q", out)
	}
}

func TestHandleLayer2Disable_alreadyDisabled(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Disable()
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "already disabled") {
		t.Fatalf("expected already disabled, got: %q", out)
	}
}

func TestHandleLayer2Disable_badConfig(t *testing.T) {
	explicitConfigPath = filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(explicitConfigPath, []byte("not valid [[["), 0644); err != nil {
		t.Fatal(err)
	}
	defer func() { explicitConfigPath = "" }()

	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Disable()
	_ = w.Close()
	os.Stderr = old

	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1 for bad config", exitCode)
	}
}

func TestHandleLayer2AcknowledgeAndStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(t.TempDir(), "test.toml")
	content := "[compression]\nlayer2_enabled = true\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\nmodel = \"MiniMax-M2.7\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Cmd([]string{"acknowledge"})
	_ = w.Close()
	os.Stdout = old
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "acknowledgement recorded") {
		t.Fatalf("ack output: %q", string(buf[:n]))
	}
	if !layer2PolicyAcknowledged() {
		t.Fatal("ack marker should be recognised")
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handleLayer2Status()
	_ = w.Close()
	os.Stdout = old
	n, _ = r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "Policy ack:    recorded") {
		t.Fatalf("status should show recorded ack, got: %q", out)
	}
	if !strings.Contains(out, "Model-facing:  summary replacement blocked") {
		t.Fatalf("status should show model-facing summary gate, got: %q", out)
	}
}

func TestEnsureLayer2PolicyAcknowledgedInteractiveAndNonInteractive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	cfg.Compression.Layer2Enabled = true

	var out bytes.Buffer
	if err := ensureLayer2PolicyAcknowledged(cfg, true, strings.NewReader("\n"), &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "acknowledgement recorded") {
		t.Fatalf("interactive output: %q", out.String())
	}
	if !layer2PolicyAcknowledged() {
		t.Fatal("interactive acknowledgement should write marker")
	}

	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	var errOut bytes.Buffer
	if err := ensureLayer2PolicyAcknowledged(cfg, false, nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "[WARN]") || layer2PolicyAcknowledged() {
		t.Fatalf("non-interactive warn=%q acknowledged=%v", errOut.String(), layer2PolicyAcknowledged())
	}
}

func TestEnsureLayer2PolicyAcknowledgedSkipsDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.Layer2Enabled = false
	var out bytes.Buffer
	if err := ensureLayer2PolicyAcknowledged(cfg, true, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("disabled layer2 should not prompt, got %q", out.String())
	}
}

func TestLayer2PolicyAckErrorBranches(t *testing.T) {
	origHome := osUserHomeDir
	defer func() { osUserHomeDir = origHome }()
	osUserHomeDir = func() (string, error) { return "", errors.New("home fail") }
	if layer2PolicyAckPath() != "" || layer2PolicyAcknowledged() {
		t.Fatal("home failure should produce no path and no ack")
	}
	if _, err := writeLayer2PolicyAck(time.Now()); err == nil {
		t.Fatal("expected home failure writing ack")
	}

	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()
	var errOut bytes.Buffer
	oldErr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Acknowledge()
	_ = w.Close()
	os.Stderr = oldErr
	_, _ = errOut.ReadFrom(r)
	if exitCode != 1 || !strings.Contains(errOut.String(), "home directory unavailable") {
		t.Fatalf("exit=%d stderr=%q", exitCode, errOut.String())
	}
}

func TestLayer2PolicyAcknowledgedInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := layer2PolicyAckPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if layer2PolicyAcknowledged() {
		t.Fatal("invalid ack JSON should not acknowledge")
	}
}

func TestWriteLayer2PolicyAckWriteError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldWrite := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error { return errors.New("write fail") }
	defer func() { osWriteFile = oldWrite }()
	if _, err := writeLayer2PolicyAck(time.Now()); err == nil {
		t.Fatal("expected write error")
	}
}

func TestWriteLayer2PolicyAckMkdirError(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(homeFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeFile)
	if _, err := writeLayer2PolicyAck(time.Now()); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestEnsureLayer2PolicyAcknowledgedReadAndWriteErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Defaults()
	cfg.Compression.Layer2Enabled = true
	var out bytes.Buffer
	if err := ensureLayer2PolicyAcknowledged(cfg, true, layer2ErrReader{}, &out, io.Discard); err == nil {
		t.Fatal("expected read error")
	}

	oldWrite := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error { return errors.New("write fail") }
	defer func() { osWriteFile = oldWrite }()
	if err := ensureLayer2PolicyAcknowledged(cfg, true, strings.NewReader("\n"), &out, io.Discard); err == nil {
		t.Fatal("expected write error")
	}
}

func TestHandleLayer2Status_badConfig(t *testing.T) {
	explicitConfigPath = filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(explicitConfigPath, []byte("not valid [[["), 0644); err != nil {
		t.Fatal(err)
	}
	defer func() { explicitConfigPath = "" }()

	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Status()
	_ = w.Close()
	os.Stderr = old

	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1 for bad config", exitCode)
	}
}

func TestHandleLayer2Cmd_enableViaDispatch(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Cmd([]string{"enable", "--acknowledge-data-policy"})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "enabled") {
		t.Fatalf("expected enabled, got: %q", out)
	}
}

func TestHandleLayer2Cmd_disableViaDispatch(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = true\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Cmd([]string{"disable"})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "disabled") {
		t.Fatalf("expected disabled, got: %q", out)
	}
}

func TestHandleLayer2Cmd_statusViaDispatch(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\nmodel = \"MiniMax-M2.7\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Cmd([]string{"status"})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "disabled") {
		t.Fatalf("expected status, got: %q", out)
	}
}

func TestHandleLayer2Enable_writeError(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	readOnlyDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	impossiblePath := filepath.Join(readOnlyDir, "sub", "config.toml")

	oldWriteFile := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("permission denied")
	}
	defer func() { osWriteFile = oldWriteFile }()

	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Enable([]string{"--acknowledge-data-policy"})
	_ = w.Close()
	os.Stderr = old

	_ = impossiblePath
	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1 for write error", exitCode)
	}
}

func TestHandleLayer2Disable_writeError(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = true\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	oldWriteFile := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return fmt.Errorf("permission denied")
	}
	defer func() { osWriteFile = oldWriteFile }()

	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	handleLayer2Disable()
	_ = w.Close()
	os.Stderr = old

	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1 for write error", exitCode)
	}
}

func TestWriteConfigUpdate_mkdirError(t *testing.T) {
	err := writeConfigUpdate("/dev/null/impossible/path/config.toml", nil)
	if err == nil {
		t.Fatal("expected error for impossible path")
	}
}

func TestHandleSubcommand_layer2Status(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\nmodel = \"MiniMax-M2.7\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"layer2", "status"})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "disabled") {
		t.Fatalf("expected status via handleSubcommand, got: %q", out)
	}
}

func TestHandleLayer2Enable_emptyResolvedPath(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, ".config"))
	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("MINIMAX_API_KEY", "test-key")
	explicitConfigPath = ""
	defer func() { explicitConfigPath = "" }()

	outDir := filepath.Join(fakeHome, ".slimference")
	_ = os.MkdirAll(outDir, 0755)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Enable([]string{"--acknowledge-data-policy"})
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "enabled") {
		t.Fatalf("expected enabled message, got: %q", out)
	}
}

func TestHandleLayer2Disable_emptyResolvedPath(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, ".config"))
	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("MINIMAX_API_KEY", "test-key")
	explicitConfigPath = ""
	defer func() { explicitConfigPath = "" }()

	_ = os.MkdirAll(filepath.Join(fakeHome, ".slimference"), 0755)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Disable()
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	// 2026-05-15: Layer 2 defaults to OFF (deterministic-only ships by
	// default). handleLayer2Disable correctly reports "already disabled"
	// when the default config has layer2_enabled=false.
	if !strings.Contains(out, "already disabled") {
		t.Fatalf("expected already-disabled with deterministic-default config, got: %q", out)
	}
}

func TestHandleLayer2Status_emptyRedaction(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "test.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleLayer2Status()
	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "Redaction:     default") {
		t.Fatalf("expected default redaction, got: %q", out)
	}
}

func TestWriteConfigUpdate_encodeError(t *testing.T) {
	prev := tomlNewEncoder
	tomlNewEncoder = func(w *strings.Builder) tomlEncoder {
		return &failingEncoder{}
	}
	t.Cleanup(func() { tomlNewEncoder = prev })

	err := writeConfigUpdate(filepath.Join(t.TempDir(), "config.toml"), nil)
	if err == nil {
		t.Fatal("expected encode error")
	}
	if !strings.Contains(err.Error(), "encode config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type failingEncoder struct{}

func (failingEncoder) Encode(_ interface{}) error {
	return fmt.Errorf("synthetic encode failure")
}

func TestBoolStr(t *testing.T) {
	if boolStr(true, "a", "b") != "a" {
		t.Error("true case")
	}
	if boolStr(false, "a", "b") != "b" {
		t.Error("false case")
	}
}

func TestResolvedOrFallback(t *testing.T) {
	if got := resolvedOrFallback(config.LoadInfo{ResolvedPath: "/foo"}); got != "/foo" {
		t.Fatalf("got %q", got)
	}
	if got := resolvedOrFallback(config.LoadInfo{}); got == "" {
		t.Fatal("expected fallback path")
	}
}

func TestEffectiveRedaction(t *testing.T) {
	if got := effectiveRedaction("strict"); got != "strict" {
		t.Fatalf("got %q", got)
	}
	if got := effectiveRedaction(""); got != "default" {
		t.Fatalf("got %q", got)
	}
}
