package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/proxy"
)

// patchSNIPeekMode surgically edits the slimference config.toml so
// that `[transparent].sni_peek_mode = <enabled>` matches `enabled`.
// The edit is in-place; comments and unrelated keys survive.
//
// If the file does not exist, it is created with a minimal stub.
// If the file exists but the [transparent] table is missing, the
// table is appended. If the key is missing inside the table, it is
// inserted. Otherwise the value is replaced.
//
// agents.md mandates surgical edits — never a full-file rewrite.
func patchSNIPeekMode(path string, enabled bool) error {
	if path == "" {
		return errors.New("install: empty config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	value := "false"
	if enabled {
		value = "true"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read: %w", err)
		}
		stub := fmt.Sprintf("[transparent]\nsni_peek_mode = %s\n", value)
		return writeAtomic(path, []byte(stub), 0o600)
	}

	newData := upsertTransparentKey(data, "sni_peek_mode", value)
	if bytes.Equal(data, newData) {
		return nil
	}
	return writeAtomic(path, newData, 0o600)
}

// upsertTransparentKey performs the surgical TOML edit for the
// [transparent] table. Implementation is conservative: it works on
// the raw bytes line-by-line to preserve comments. Limitation: nested
// `[transparent.tlsprofiles]` sub-tables remain intact because we
// match `[transparent]` only on an exact-line basis.
func upsertTransparentKey(data []byte, key, value string) []byte {
	lines := strings.Split(string(data), "\n")
	tableIdx := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "[transparent]" {
			tableIdx = i
			break
		}
	}
	if tableIdx == -1 {
		// Append table at EOF.
		suffix := ""
		if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
			suffix = "\n"
		}
		appended := fmt.Sprintf("%s\n[transparent]\n%s = %s\n", suffix, key, value)
		return append(data, []byte(appended)...)
	}
	// Find end of table (next [section] or EOF).
	endIdx := len(lines)
	for i := tableIdx + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			endIdx = i
			break
		}
	}
	// Search for the key inside [tableIdx+1, endIdx).
	keyRe := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*).*$`)
	for i := tableIdx + 1; i < endIdx; i++ {
		if keyRe.MatchString(lines[i]) {
			lines[i] = keyRe.ReplaceAllString(lines[i], "${1}"+value)
			return []byte(strings.Join(lines, "\n"))
		}
	}
	// Insert key just after [transparent] header.
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:tableIdx+1]...)
	newLines = append(newLines, key+" = "+value)
	newLines = append(newLines, lines[tableIdx+1:]...)
	return []byte(strings.Join(newLines, "\n"))
}

// writeAtomic writes data via a temp file + rename so partial writes
// are impossible.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := createAtomicTempFileFn(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = removeAtomicFileFn(tmp.Name())
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = removeAtomicFileFn(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = removeAtomicFileFn(tmp.Name())
		return err
	}
	return renameAtomicFileFn(tmp.Name(), path)
}

type atomicTempFile interface {
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Close() error
	Name() string
}

var (
	createAtomicTempFileFn = func(dir, pattern string) (atomicTempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	removeAtomicFileFn = os.Remove
	renameAtomicFileFn = os.Rename
)

// pidFilePath returns the path Slimference writes its PID to. Used
// by `slimference enable/disable` to SIGHUP the daemon.
func pidFilePath() string {
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".slimference", "run", "daemon.pid")
}

var (
	findProcessFn = os.FindProcess
	signalPIDFn   = signalPID
)

func signalPID(pid int, sig os.Signal) error {
	proc, err := findProcessFn(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

// signalDaemonReload reads the PID file and sends SIGHUP. Returns
// (sent=true, nil) on success; (false, nil) if no PID file exists
// (daemon not running); (false, err) on filesystem / signal errors.
func signalDaemonReload() (bool, error) {
	path := pidFilePath()
	if path == "" {
		return false, errors.New("HOME unresolved")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, fmt.Errorf("malformed PID file %s: %w", path, err)
	}
	if err := signalPIDFn(pid, syscall.SIGHUP); err != nil {
		// ESRCH = no such process
		if errors.Is(err, syscall.ESRCH) || strings.Contains(err.Error(), "process already finished") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// fetchSetupState calls the local /admin/state endpoint via HTTP.
// Used by `slimference status` so the CLI sees exactly what the daemon
// sees (single source of truth).
func fetchSetupState(timeout time.Duration) (control.SetupState, error) {
	port := defaultAdminPort()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, proxy.AdminStatePath)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return control.SetupState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return control.SetupState{}, fmt.Errorf("admin returned %d", resp.StatusCode)
	}
	var state control.SetupState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return control.SetupState{}, err
	}
	return state, nil
}

// defaultAdminPort reads the listen port from the config file. Falls
// back to 8990 (Slimference's default) when unresolved.
func defaultAdminPort() int {
	const fallback = 8990
	info := config.ResolveConfigPath(config.LoadOptions{})
	if info.ResolvedPath == "" {
		return fallback
	}
	data, err := os.ReadFile(info.ResolvedPath)
	if err != nil {
		return fallback
	}
	re := regexp.MustCompile(`(?m)^\s*listen_port\s*=\s*(\d+)`)
	if m := re.FindSubmatch(data); m != nil {
		if n, err := strconv.Atoi(string(m[1])); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
