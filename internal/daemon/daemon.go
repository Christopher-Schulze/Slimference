// Package daemon manages the background proxy process lifecycle.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultPIDDir is the directory for the PID file.
const DefaultPIDDir = "~/.slimference"

const launchdLabel = "com.slimference.daemon"

// PIDFile holds metadata about a running daemon instance.
type PIDFile struct {
	PID        int       `json:"pid"`
	Port       int       `json:"port"`
	StartedAt  time.Time `json:"started_at"`
	ConfigPath string    `json:"config_path"`
}

// expandHomeFn is the home-expansion function, overridable in tests.
var expandHomeFn = expandHomeImpl
var (
	isRunningFn    = IsRunning
	sendSignalFn   = sendSignalImpl
	sleepFn        = time.Sleep
	signalNotifyFn = signal.Notify
	signalStopFn   = signal.Stop
	timeNow        = time.Now
	findProcessFn  = os.FindProcess
	resolveUnixFn  = net.ResolveUnixAddr
	listenUnixFn   = net.ListenUnix
	dialUnixFn     = net.DialUnix
	chmodFn        = os.Chmod
	marshalPIDFn   = json.MarshalIndent
)

func expandHomeImpl(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// expandHome is the public wrapper.
func expandHome(path string) string { return expandHomeFn(path) }

// PIDPath returns the path to the PID file.
func PIDPath() string {
	dir := expandHome(DefaultPIDDir)
	return filepath.Join(dir, "slimference.pid")
}

// WritePID creates or overwrites the PID file with the current process info.
func WritePID(port int, configPath string) error {
	pf := PIDFile{
		PID:        os.Getpid(),
		Port:       port,
		StartedAt:  time.Now(),
		ConfigPath: configPath,
	}
	data, err := marshalPIDFn(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pid file: %w", err)
	}
	dir := filepath.Dir(PIDPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pid dir: %w", err)
	}
	return os.WriteFile(PIDPath(), append(data, '\n'), 0644)
}

// RemovePID deletes the PID file.
func RemovePID() error {
	return os.Remove(PIDPath())
}

// ReadPID reads the PID file. Returns nil if it doesn't exist.
func ReadPID() (*PIDFile, error) {
	data, err := os.ReadFile(PIDPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pid file: %w", err)
	}
	var pf PIDFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parse pid file: %w", err)
	}
	return &pf, nil
}

// IsRunning checks whether a daemon process is alive.
func IsRunning() (bool, *PIDFile, error) {
	pf, err := ReadPID()
	if err != nil {
		return false, nil, err
	}
	if pf == nil {
		return false, nil, nil
	}
	// Check if the process exists.
	proc, err := findProcessFn(pf.PID)
	if err != nil {
		return false, pf, nil
	}
	// Send signal 0 to check if process is alive.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process doesn't exist; stale PID file.
		_ = RemovePID()
		return false, pf, nil
	}
	return true, pf, nil
}

func sendSignalImpl(pid int, sig syscall.Signal) error {
	proc, err := findProcessFn(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

// StopDaemon sends SIGTERM to the running daemon process.
func StopDaemon() error {
	running, pf, err := isRunningFn()
	if err != nil {
		return fmt.Errorf("check daemon: %w", err)
	}
	if !running {
		fmt.Println("Slimference is not running.")
		return nil
	}
	if err := sendSignalFn(pf.PID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}
	// Wait for process to exit (up to 10 seconds).
	deadline := timeNow().Add(10 * time.Second)
	for timeNow().Before(deadline) {
		sleepFn(200 * time.Millisecond)
		if alive, _, _ := isRunningFn(); !alive {
			fmt.Printf("Slimference stopped (PID %d).\n", pf.PID)
			return nil
		}
	}
	// Force kill.
	_ = sendSignalFn(pf.PID, syscall.SIGKILL)
	fmt.Printf("Slimference force-killed (PID %d).\n", pf.PID)
	return nil
}

// Status prints the daemon status.
func Status() error {
	running, pf, err := isRunningFn()
	if err != nil {
		return err
	}
	if !running {
		fmt.Println("Slimference is not running.")
		return nil
	}
	fmt.Printf("Slimference is running (PID %d, port %d, since %s)\n", pf.PID, pf.Port, pf.StartedAt.Format(time.RFC3339))
	return nil
}

// RunDaemon starts the proxy without TUI and blocks until SIGINT/SIGTERM.
// The startFn callback creates and starts the proxy; it returns the port and a shutdown function.
type ProxyLifecycle interface {
	StartProxy() (port int, shutdown func(ctx context.Context) error, err error)
}

// RunDaemon starts the proxy in background mode and waits for signals.
func RunDaemon(startProxy func() (port int, shutdown func(ctx context.Context) error, err error)) error {
	// Check if already running via PID file.
	running, existing, _ := isRunningFn()
	if running {
		return fmt.Errorf("slimference is already running (PID %d, port %d)", existing.PID, existing.Port)
	}

	// Acquire socket lock to prevent race conditions during startup.
	lockCloser, err := TryAcquireLock()
	if err != nil {
		return err
	}
	defer lockCloser()

	port, shutdown, err := startProxy()
	if err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}

	// Write PID file.
	if err := WritePID(port, ""); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write PID file: %v\n", err)
	}

	fmt.Printf("Slimference daemon started on :%d (PID %d)\n", port, os.Getpid())

	// Wait for signal.
	sigCh := make(chan os.Signal, 1)
	signalNotifyFn(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signalStopFn(sigCh)
	<-sigCh

	fmt.Println("\nShutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = shutdown(ctx)
	_ = RemovePID()
	fmt.Println("Slimference stopped.")
	return nil
}

// GenerateLaunchdPlist returns a launchd plist XML string for autostart.
func GenerateLaunchdPlist(binaryPath string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + launchdLabel + `</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/sh</string>
        <string>-lc</string>
        <string>. ` + shellQuoteSingle(LaunchdEnvPath()) + ` &amp;&amp; exec ` + shellQuoteSingle(binaryPath) + ` daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
        <key>Crashed</key>
        <true/>
    </dict>
    <key>ThrottleInterval</key>
    <integer>2</integer>
    <key>StandardOutPath</key>
    <string>` + expandHome("~/.slimference/logs/daemon.stdout.log") + `</string>
    <key>StandardErrorPath</key>
    <string>` + expandHome("~/.slimference/logs/daemon.stderr.log") + `</string>
</dict>
</plist>`
}

// LaunchdPlistPathFn is overridable in tests.
var LaunchdPlistPathFn = launchdPlistPathImpl

func launchdPlistPathImpl() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist")
}

// LaunchdPlistPath returns the path where the launchd plist should be installed.
func LaunchdPlistPath() string { return LaunchdPlistPathFn() }

// LaunchdStdoutLogPath returns the absolute path to the launchd stdout log.
func LaunchdStdoutLogPath() string {
	return expandHome("~/.slimference/logs/daemon.stdout.log")
}

// LaunchdStderrLogPath returns the absolute path to the launchd stderr log.
func LaunchdStderrLogPath() string {
	return expandHome("~/.slimference/logs/daemon.stderr.log")
}

// ReadRecentLogLines reads the last n lines of a log file and returns them
// in chronological order. The `since` filter, when non-zero, drops lines
// whose parsed ISO-8601 timestamp (if any) is older than the cutoff; lines
// without a recognisable timestamp pass through unchanged. Missing or empty
// files return nil, nil.
func ReadRecentLogLines(path string, n int, since time.Time) ([]string, error) {
	data, err := osReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if !since.IsZero() {
		lines = filterSinceLogLines(lines, since)
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// osReadFile is overridable in tests.
var osReadFile = func(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// filterSinceLogLines drops lines whose embedded time field is older than
// the cutoff. Lines without a detectable timestamp are kept (conservative).
// Supports the slog JSON `"time":"..."` field and a leading RFC3339 prefix.
func filterSinceLogLines(lines []string, cutoff time.Time) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		ts, ok := extractLogLineTime(line)
		if ok && ts.Before(cutoff) {
			continue
		}
		out = append(out, line)
	}
	return out
}

// extractLogLineTime returns the timestamp found in a structured (slog-json)
// or prefixed log line. Returns zero,false if no timestamp is recognisable.
func extractLogLineTime(line string) (time.Time, bool) {
	// slog JSON: "time":"2026-04-18T12:34:56.789Z"
	if idx := strings.Index(line, `"time":"`); idx >= 0 {
		start := idx + len(`"time":"`)
		end := strings.IndexByte(line[start:], '"')
		if end > 0 {
			if t, err := time.Parse(time.RFC3339Nano, line[start:start+end]); err == nil {
				return t, true
			}
		}
	}
	// Leading RFC3339 (or Nano) prefix: "2026-04-18T12:34:56Z ...".
	// time.RFC3339Nano accepts both nano-precision and plain RFC3339 inputs,
	// so a single parse attempt covers both shapes.
	if len(line) >= 20 {
		space := strings.IndexByte(line, ' ')
		if space > 0 {
			if t, err := time.Parse(time.RFC3339Nano, line[:space]); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// LaunchdEnvPathFn is overridable in tests.
var LaunchdEnvPathFn = launchdEnvPathImpl

func launchdEnvPathImpl() string {
	return filepath.Join(expandHome(DefaultPIDDir), "launchd.env")
}

// LaunchdEnvPath returns the path where the launchd env file should be installed.
func LaunchdEnvPath() string { return LaunchdEnvPathFn() }

var launchctlExec = launchctlExecImpl

func launchctlExecImpl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

func launchdDomain() string {
	return "gui/" + itoa(os.Getuid())
}

func launchdServiceTarget() string {
	return launchdDomain() + "/" + launchdLabel
}

func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func writeLaunchdEnvFile() error {
	path := LaunchdEnvPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}
	var content strings.Builder
	content.WriteString("# Generated by slimference service install\n")
	content.WriteString("# Secrets are stored here instead of the launchd plist.\n")
	if value := os.Getenv("MINIMAX_API_KEY"); value != "" {
		content.WriteString("export MINIMAX_API_KEY=")
		content.WriteString(shellQuoteSingle(value))
		content.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(content.String()), 0600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}
	return nil
}

// InstallLaunchd writes the plist and loads it.
func InstallLaunchd(binaryPath string) error {
	plistsDir := filepath.Dir(LaunchdPlistPath())
	if err := os.MkdirAll(plistsDir, 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.MkdirAll(expandHome(DefaultPIDDir), 0700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.MkdirAll(expandHome("~/.slimference/logs"), 0700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := writeLaunchdEnvFile(); err != nil {
		return err
	}
	plist := GenerateLaunchdPlist(binaryPath)
	if err := os.WriteFile(LaunchdPlistPath(), []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	_ = launchctlExec("bootout", launchdDomain(), LaunchdPlistPath())
	if err := launchctlExec("bootstrap", launchdDomain(), LaunchdPlistPath()); err != nil {
		return err
	}
	if err := launchctlExec("enable", launchdServiceTarget()); err != nil {
		return err
	}
	if err := launchctlExec("kickstart", "-k", launchdServiceTarget()); err != nil {
		return err
	}
	fmt.Printf("Installed launchd plist to %s\n", LaunchdPlistPath())
	return nil
}

// UninstallLaunchd unloads and removes the plist.
func UninstallLaunchd() error {
	_ = launchctlExec("bootout", launchdDomain(), LaunchdPlistPath())
	_ = launchctlExec("disable", launchdServiceTarget())
	if err := os.Remove(LaunchdPlistPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	if err := os.Remove(LaunchdEnvPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove env file: %w", err)
	}
	fmt.Println("Removed launchd plist.")
	return nil
}

// FormatStatus returns a machine-readable status JSON.
func FormatStatus() ([]byte, error) {
	running, pf, err := isRunningFn()
	if err != nil {
		return nil, err
	}
	status := map[string]interface{}{
		"running": running,
	}
	if pf != nil {
		status["pid"] = pf.PID
		status["port"] = pf.Port
		status["started_at"] = pf.StartedAt.Format(time.RFC3339)
	}
	return json.MarshalIndent(status, "", "  ")
}

// itoa is a minimal int-to-string converter.
func itoa(n int) string {
	return strconv.Itoa(n)
}

// --- Socket Lock ---

// LockPath returns the path to the Unix domain socket used as a process lock.
// The socket is atomically created by the OS - if bind() succeeds, we hold the lock.
// If bind() fails with EADDRINUSE, another process is already running.
func LockPath() string {
	dir := expandHome(DefaultPIDDir)
	return filepath.Join(dir, "slimference.lock")
}

// TryAcquireLock attempts to bind a Unix domain socket at LockPath().
// Returns a closer function on success (call to release), or an error if already locked.
// The socket is automatically cleaned up by the OS when the process exits.
var TryAcquireLock = tryAcquireLockImpl

func tryAcquireLockImpl() (func(), error) {
	path := LockPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	addr, err := resolveUnixFn("unix", path)
	if err != nil {
		return nil, fmt.Errorf("resolve lock address: %w", err)
	}

	conn, err := listenUnixFn("unix", addr)
	if err != nil {
		// Bind failed - check if it's a stale socket or a live one.
		// Try connecting to detect if a live process holds the lock.
		if probeConn, dialErr := dialUnixFn("unix", nil, addr); dialErr == nil {
			probeConn.Close()
			return nil, fmt.Errorf("slimference is already running (socket lock held: %s)", path)
		}
		// Stale socket (bind failed, connect failed) - remove and retry.
		_ = os.Remove(path)
		conn, err = listenUnixFn("unix", addr)
		if err != nil {
			return nil, fmt.Errorf("slimference is already running (socket lock held: %s)", path)
		}
	}

	// Set socket permissions so only the current user can access it.
	_ = chmodFn(path, 0600)

	closer := func() {
		conn.Close()
		_ = os.Remove(path)
	}
	return closer, nil
}

// IsLockHeld checks if the socket lock is currently held by any process.
// Returns true if another Slimference instance is running.
func IsLockHeld() bool {
	path := LockPath()
	addr, err := resolveUnixFn("unix", path)
	if err != nil {
		return false
	}
	conn, err := dialUnixFn("unix", nil, addr)
	if err != nil {
		// Cannot connect = no one is listening = not held.
		return false
	}
	conn.Close()
	return true
}
