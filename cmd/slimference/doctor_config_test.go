package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/tlsdial"
)

func TestHandleSubcommand_doctor_smoke(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	prevHome := osUserHomeDir
	fakeHome := t.TempDir()
	osUserHomeDir = func() (string, error) { return fakeHome, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })

	// Write a minimal config with L2 disabled (T121 default) so the
	// doctor smoke test does not depend on the operator's XDG config.
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "doctor.toml")
	cfgContent := `[compression]
layer1_enabled = true
layer2_enabled = false
layer3_enabled = true
[compression.minimax]
base_url = "https://api.minimax.io/v1"
api_key_env = "MINIMAX_API_KEY"
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("stdout: %q", out)
	}
	if !strings.Contains(out, "TLS profile catalog") || !strings.Contains(out, "utls-chrome-133") {
		t.Fatalf("expected TLS profile catalog warning: %q", out)
	}
	if !strings.Contains(out, "TLS reflected proof") {
		t.Fatalf("expected TLS proof warning: %q", out)
	}
	if !strings.Contains(out, "All checks passed") {
		t.Fatalf("expected success footer: %q", out)
	}
}

func TestFormatTLSCatalogStatusFreshAndStale(t *testing.T) {
	info := tlsdial.Catalog()
	fresh := formatTLSCatalogStatus(info.Generated.Add(24 * time.Hour))
	if !strings.Contains(fresh, "state=fresh") {
		t.Fatalf("fresh status=%q", fresh)
	}
	stale := formatTLSCatalogStatus(info.Generated.Add(time.Duration(info.MaxAgeDays+1) * 24 * time.Hour))
	if !strings.Contains(stale, "state=stale") {
		t.Fatalf("stale status=%q", stale)
	}
}

func TestFormatTLSProofStatusMissingAndPresent(t *testing.T) {
	prevHome := osUserHomeDir
	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = prevHome })
	if got := formatTLSProofStatus(time.Now()); !strings.Contains(got, "no reflected provider-edge proof yet") {
		t.Fatalf("missing proof status=%q", got)
	}
	dir := filepath.Join(home, ".slimference", "tls-proofs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"profile":"chromium_stable","ja3_hash":"abc","timestamp":"2026-05-01T00:00:00Z","success":true}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "chromium_stable.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := formatTLSProofStatus(time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "chromium_stable=ok") || !strings.Contains(got, "ja3=abc") {
		t.Fatalf("proof status=%q", got)
	}
	noHash := `{"profile":"chrome_131","timestamp":"2026-05-02T00:00:00Z","success":false}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "chrome_131.jsonl"), []byte(noHash), 0o600); err != nil {
		t.Fatal(err)
	}
	got = formatTLSProofStatus(time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "chrome_131=failed") || !strings.Contains(got, "ja3=no-ja3") {
		t.Fatalf("proof status without JA3=%q", got)
	}
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	if got := formatTLSProofStatus(time.Now()); !strings.Contains(got, "HOME lookup failed") {
		t.Fatalf("home error status=%q", got)
	}
	osUserHomeDir = func() (string, error) { return home, nil }
	if err := os.WriteFile(filepath.Join(dir, "bad.jsonl"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := formatTLSProofStatus(time.Now()); !strings.Contains(got, "proof status unreadable") {
		t.Fatalf("unreadable status=%q", got)
	}
}

func TestHandleSubcommand_doctor_invalidConfigExits1(t *testing.T) {
	if os.Getenv("TP_DOCTOR_BAD_CFG") == "1" {
		handleSubcommand([]string{"doctor"})
		return
	}
	cfgPath := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(cfgPath, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_doctor_invalidConfigExits1")
	cmd.Env = append(os.Environ(), "TP_DOCTOR_BAD_CFG=1", "SLIMFERENCE_CONFIG="+cfgPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleSubcommand_doctor_failingChecks covers the check() closure !ok branch
// (main.go:592-595), the MiniMax key-missing branch (615-617), the upstream-unreachable
// branches (624-626, 634-636), and the "Some checks failed" footer (652-654).
func TestHandleSubcommand_doctor_failingChecks(t *testing.T) {

	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")

	t.Setenv("MINIMAX_API_KEY", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header in output: %q", out)
	}

	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected at least one FAIL in output: %q", out)
	}
}

// TestHandleSubcommand_doctor_DeterminismGate_OnEnableSeedOff covers
// the T88 doctor warning when require_deterministic is on but
// enable_seed is off (main.go::handleDoctorCmd Determinism gate
// branch FAIL).
func TestHandleSubcommandDoctorDeterminismGateOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgPath := filepath.Join(t.TempDir(), "cfg.toml")
	body := []byte(`[upstream.anthropic]
base_url = "` + srv.URL + `"

[upstream.openai]
base_url = "` + srv.URL + `"

[compression.summary]
require_deterministic = true
`)
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "on (deterministic compactor)") {
		t.Fatalf("determinism gate output missing: %q", buf.String())
	}
}

// TestHandleSubcommand_doctor_OutboundRedaction_Off covers the T109
// FAIL branch when the operator has disabled outbound redaction.
func TestHandleSubcommand_doctor_OutboundRedaction_Off(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cfg.toml")
	body := []byte(`[compression.summary]
outbound_redaction = "off"
`)
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "L2 outbound redaction") || !strings.Contains(out, "OFF") {
		t.Fatalf("expected outbound redaction OFF FAIL line, got: %s", out)
	}
}

// TestHandleSubcommand_doctor_OutboundRedaction_Strict covers the T109
// strict-mode reporting branch.
func TestHandleSubcommand_doctor_OutboundRedaction_Strict(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cfg.toml")
	body := []byte(`[compression.summary]
outbound_redaction = "strict"
`)
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "L2 outbound redaction") || !strings.Contains(out, "strict") {
		t.Fatalf("expected outbound redaction strict line, got: %s", out)
	}
}

// TestHandleSubcommand_doctor_OutboundRedaction_Unknown covers the
// fallback warning when an unrecognised mode is configured.
func TestHandleSubcommand_doctor_OutboundRedaction_Unknown(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cfg.toml")
	body := []byte(`[compression.summary]
outbound_redaction = "novel-mode"
`)
	if err := os.WriteFile(cfgPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "L2 outbound redaction") || !strings.Contains(out, "unknown mode") {
		t.Fatalf("expected outbound redaction unknown-mode line, got: %s", out)
	}
}

// TestHandleSubcommand_doctor_DeterminismGate_OnEnableSeedOn covers
// the success branch of the T88 determinism gate.

// TestHandleSubcommand_doctor_configFileMissingBranch covers the
// "not found at ... (using defaults)" branch in the Config file check (main.go:604-606).
// We override HOME so DefaultConfigPath returns a non-existent file.
func TestHandleSubcommand_doctor_configFileMissingBranch(t *testing.T) {

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(fakeHome, "cfg.toml"))

	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header: %q", out)
	}
}

func TestHandleSubcommand_doctor_defaultsConfigBranch(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(fakeHome, ".config"))
	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "no file found, using defaults") {
		t.Fatalf("expected defaults config branch: %q", out)
	}
}

// TestHandleSubcommand_doctor_configFileExistsBranch covers main.go:607 (return path, true)
// when DefaultConfigPath() resolves to an existing file.
//
// DefaultConfigPath calls expandHome("~") which returns the literal string "~" (because "~"
// has no "~/" prefix), so the effective path is the relative path "~/.slimference/config.toml".
// We build that directory structure inside a temp dir and chdir into it.
func TestHandleSubcommand_doctor_configFileExistsBranch(t *testing.T) {
	tmp := t.TempDir()

	tildeSlimferenceDir := filepath.Join(tmp, "~", ".slimference")
	if err := os.MkdirAll(tildeSlimferenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tildeSlimferenceDir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(tmp, "missing.toml"))
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header: %q", out)
	}

	if strings.Contains(out, "not found") {
		t.Fatalf("expected config-found output (got not-found): %q", out)
	}
}

// TestHandleSubcommand_doctor_analyticsLogDirError covers main.go:643-645
// when os.MkdirAll for the analytics log dir fails.
func TestHandleSubcommand_doctor_analyticsLogDirError(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(fakeHome, "missing.toml"))

	blocker := filepath.Join(fakeHome, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	logDirPath := filepath.Join(blocker, "subdir")

	cfgContent := "[analytics]\nlog_dir = \"" + logDirPath + "\"\n"
	cfgFile := filepath.Join(fakeHome, "test.toml")
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgFile)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("MINIMAX_API_KEY", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Doctor") {
		t.Fatalf("expected doctor header: %q", out)
	}

	if !strings.Contains(out, "cannot create") {
		t.Fatalf("expected MkdirAll error in output: %q", out)
	}
}

// TestHandleSubcommand_doctor_homeDirError covers the "home dir unavailable"
// branch in the Content archive doctor check.
func TestHandleSubcommand_doctor_homeDirError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("MINIMAX_API_KEY", "test-key")

	prev := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = prev })

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "home dir unavailable") {
		t.Fatalf("expected home dir unavailable, got: %q", out)
	}
}

// TestHandleSubcommand_doctor_archiveUnreadable covers the LoadStats error
// branch in the Content archive doctor check by writing malformed stats.json.
func TestHandleSubcommand_doctor_archiveUnreadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fakeHome := t.TempDir()
	archiveDir := filepath.Join(fakeHome, ".slimference", "content-archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "stats.json"), []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", fakeHome)
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(fakeHome, "missing.toml"))
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "unreadable:") {
		t.Fatalf("expected unreadable in output: %q", out)
	}
}

// TestHandleSubcommand_doctor_promptOverrideConfigured covers the configured
// branch of the Prompt override doctor check.
func TestHandleSubcommand_doctor_promptOverrideConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fakeHome := t.TempDir()
	overridePath := filepath.Join(fakeHome, "override.txt")
	if err := os.WriteFile(overridePath, []byte("# version: vX-doctor\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(fakeHome, "test.toml")
	cfgContent := "[compression]\nprompt_override_path = \"" + overridePath + "\"\n"
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", fakeHome)
	t.Setenv("SLIMFERENCE_CONFIG", cfgFile)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "active version:") || !strings.Contains(out, "override.txt") {
		t.Fatalf("expected configured override path in output: %q", out)
	}
}

func TestHandleSubcommand_configShow(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"config", "show"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, `"Proxy"`) || !strings.Contains(out, `"ListenPort"`) {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_configShow_loadErrorExits1(t *testing.T) {
	if os.Getenv("TP_CFG_SHOW_BAD") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", os.Getenv("TP_CFG_SHOW_BAD_FILE"))
		handleSubcommand([]string{"config", "show"})
		return
	}
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badPath, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configShow_loadErrorExits1")
	cmd.Env = append(os.Environ(), "TP_CFG_SHOW_BAD=1", "TP_CFG_SHOW_BAD_FILE="+badPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_configInit_writesFile(t *testing.T) {

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	t.Setenv("SLIMFERENCE_CONFIG", "")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"config", "init"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	path := filepath.Join(xdg, "slimference", "config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config at %s: %v", path, err)
	}
	if !strings.Contains(out, "Config written") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_configInit_secondIsNoop(t *testing.T) {
	if os.Getenv("TP_CFG_INIT_TWICE") == "1" {
		_ = os.Chdir(os.Getenv("TP_CFG_INIT_HOME"))
		handleSubcommand([]string{"config", "init"})
		handleSubcommand([]string{"config", "init"})
		return
	}
	tmp := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configInit_secondIsNoop")
	cmd.Env = append(os.Environ(), "TP_CFG_INIT_TWICE=1", "TP_CFG_INIT_HOME="+tmp)
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("want exit 0: %v out=%s", err, out)
	}
	if !strings.Contains(string(out), "already exists") {
		t.Fatalf("output: %s", out)
	}
}

func TestHandleSubcommand_configUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_CFG_BAD") == "1" {
		handleSubcommand([]string{"config", "not-a-subcommand"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_CFG_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_configUnknownSubcommandExits1(t *testing.T) {
	if os.Getenv("TP_CFG_UNKNOWN_SUB") == "1" {
		handleSubcommand([]string{"config", "nope"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configUnknownSubcommandExits1")
	cmd.Env = append(os.Environ(), "TP_CFG_UNKNOWN_SUB=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_configUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_CONFIG_USAGE") == "1" {
		handleSubcommand([]string{"config"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_configUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_CONFIG_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleConfigCmd_initMkdirErrorExits1 covers handleConfigCmd "init" MkdirAll error (main.go:443-446).
// Arrange HOME so DefaultConfigPath resolves into a read-only directory that blocks mkdir.
func TestHandleConfigCmd_initMkdirErrorExits1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not applicable on windows")
	}
	if os.Getenv("TP_CFG_INIT_MKDIR_ERR") == "1" {

		handleSubcommand([]string{"config", "init"})
		return
	}
	tmp := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmp, "slimference"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleConfigCmd_initMkdirErrorExits1")
	cmd.Env = append(os.Environ(),
		"TP_CFG_INIT_MKDIR_ERR=1",
		"XDG_CONFIG_HOME="+tmp,
		"SLIMFERENCE_CONFIG=",
	)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

// TestHandleConfigCmd_writeFileError covers the os.WriteFile error exit in handleConfigCmd (main.go:460-463).
func TestHandleConfigCmd_writeFileError(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)

	orig := osWriteFile
	defer func() { osWriteFile = orig }()
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("write failed")
	}

	rp, cleanup := redirectStderr()
	code, exited := captureExit(func() {
		handleSubcommand([]string{"config", "init"})
	})
	cleanup()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)

	if !exited || code != 1 {
		t.Fatalf("want exit 1, got exited=%v code=%d", exited, code)
	}
	if !strings.Contains(buf.String(), "write config") {
		t.Fatalf("stderr: %q", buf.String())
	}
}

func TestHandleSubcommand_doctor_redactionEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "doctor.toml")
	content := "[compression]\nlayer2_enabled = true\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n[compression.summary]\noutbound_redaction = \"\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "default (secrets + paths + auth headers + JSON keys)") {
		t.Fatalf("expected default redaction, got: %q", out)
	}
}

func TestHandleSubcommand_doctor_redactionUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "doctor.toml")
	content := "[compression]\nlayer2_enabled = false\n[compression.minimax]\nbase_url = \"https://api.minimax.io/v1\"\napi_key_env = \"MINIMAX_API_KEY\"\n[compression.summary]\noutbound_redaction = \"bogus_mode\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", srv.URL)
	t.Setenv("MINIMAX_API_KEY", "test-key")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"doctor"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "unknown mode") {
		t.Fatalf("expected unknown mode warning, got: %q", out)
	}
}
