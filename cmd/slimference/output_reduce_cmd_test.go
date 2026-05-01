package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleOutputReduceCmd_NoArgs(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	handleOutputReduceCmd(nil)
	_ = w.Close()
	os.Stderr = old
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	if exitCode != 1 || !strings.Contains(string(buf[:n]), "output-reduce") {
		t.Fatalf("exit=%d stderr=%q", exitCode, string(buf[:n]))
	}
}

func TestHandleOutputReduceStatus(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[compression.output_reduce]
enabled = true
profile = "codex"
signature_marker = "#x"
max_added_bytes = 123
min_input_tokens = 456
`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleOutputReduceStatus()
	_ = w.Close()
	os.Stdout = old
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "Enabled:          yes") || !strings.Contains(out, "Profile:          codex") || !strings.Contains(out, "Min input tokens: 456") {
		t.Fatalf("status output: %q", out)
	}
}

func TestHandleOutputReduceStatus_CustomDirectiveAndLoadError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[compression.output_reduce]
enabled = false
profile = "auto"
custom_directive_path = "/tmp/rules.txt"
signature_marker = "#x"
max_added_bytes = 123
min_input_tokens = 456
`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleOutputReduceStatus()
	_ = w.Close()
	os.Stdout = old
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "Custom directive: /tmp/rules.txt") {
		t.Fatalf("status output: %q", string(buf[:n]))
	}

	explicitConfigPath = filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(explicitConfigPath, []byte("not valid [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()
	oldErr := os.Stderr
	_, ew, _ := os.Pipe()
	os.Stderr = ew
	handleOutputReduceStatus()
	_ = ew.Close()
	os.Stderr = oldErr
	if exitCode != 1 {
		t.Fatalf("exit=%d", exitCode)
	}
}

func TestHandleOutputReduceSet(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[compression.output_reduce]
enabled = true
profile = "auto"
signature_marker = "#slimference-output-rules"
max_added_bytes = 1400
min_input_tokens = 400
`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleOutputReduceSet(false)
	_ = w.Close()
	os.Stdout = old
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "disabled") {
		t.Fatalf("output: %q", string(buf[:n]))
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "enabled = false") {
		t.Fatalf("config not updated: %s", data)
	}
}

func TestHandleOutputReduceCmd_DispatchAndErrors(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()
	oldErr := os.Stderr
	_, ew, _ := os.Pipe()
	os.Stderr = ew
	handleOutputReduceCmd([]string{"wat"})
	_ = ew.Close()
	os.Stderr = oldErr
	if exitCode != 1 {
		t.Fatalf("unknown exit=%d", exitCode)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[compression.output_reduce]
enabled = false
profile = "auto"
signature_marker = "#slimference-output-rules"
max_added_bytes = 1400
min_input_tokens = 400
`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleOutputReduceCmd([]string{"enable"})
	_ = w.Close()
	os.Stdout = oldOut
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "enabled") {
		t.Fatalf("enable output: %q", string(buf[:n]))
	}
	oldOut = os.Stdout
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleOutputReduceCmd([]string{"status"})
	_ = w.Close()
	os.Stdout = oldOut
	n, _ = r.Read(buf)
	if !strings.Contains(string(buf[:n]), "Output-reduce:") {
		t.Fatalf("status output: %q", string(buf[:n]))
	}
	oldOut = os.Stdout
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleOutputReduceCmd([]string{"disable"})
	_ = w.Close()
	os.Stdout = oldOut
	n, _ = r.Read(buf)
	if !strings.Contains(string(buf[:n]), "disabled") {
		t.Fatalf("disable output: %q", string(buf[:n]))
	}
}

func TestHandleSubcommand_OutputReduceDispatch(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[compression.output_reduce]
enabled = true
profile = "auto"
signature_marker = "#slimference-output-rules"
max_added_bytes = 1400
min_input_tokens = 400
`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()

	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"output-reduce", "status"})
	_ = w.Close()
	os.Stdout = oldOut
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "Output-reduce:") {
		t.Fatalf("status output: %q", string(buf[:n]))
	}
}

func TestHandleOutputReduceSet_AlreadyAndErrors(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[compression.output_reduce]
enabled = true
profile = "auto"
signature_marker = "#slimference-output-rules"
max_added_bytes = 1400
min_input_tokens = 400
`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitConfigPath = cfgPath
	defer func() { explicitConfigPath = "" }()
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleOutputReduceSet(true)
	_ = w.Close()
	os.Stdout = oldOut
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "already enabled") {
		t.Fatalf("already output: %q", string(buf[:n]))
	}

	explicitConfigPath = filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(explicitConfigPath, []byte("not valid [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()
	oldErr := os.Stderr
	_, ew, _ := os.Pipe()
	os.Stderr = ew
	handleOutputReduceSet(false)
	_ = ew.Close()
	os.Stderr = oldErr
	if exitCode != 1 {
		t.Fatalf("load error exit=%d", exitCode)
	}

	cfgPath = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[compression.output_reduce]
enabled = true
profile = "auto"
signature_marker = "#slimference-output-rules"
max_added_bytes = 1400
min_input_tokens = 400
`), 0o644); err != nil {
		t.Fatal(err)
	}
	explicitConfigPath = cfgPath
	oldWriteFile := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error { return os.ErrPermission }
	defer func() { osWriteFile = oldWriteFile }()
	exitCode = 0
	handleOutputReduceSet(false)
	if exitCode != 1 {
		t.Fatalf("write error exit=%d", exitCode)
	}
}
