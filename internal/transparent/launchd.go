package transparent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LaunchAgent wraps the on-login auto-start mechanism. The agent is
// optional: `slimference proxy install --no-launchd` skips it. When
// installed it ensures the daemon comes back automatically after a
// reboot or crash, with the proxy setting flipped on by the daemon
// itself.
type LaunchAgent struct {
	exec     func(ctx context.Context, name string, args ...string) ([]byte, error)
	writeFn  func(path string, data []byte, mode os.FileMode) error
	removeFn func(path string) error
	timeout  time.Duration
}

// NewLaunchAgent returns a manager wired to launchctl + os file ops.
func NewLaunchAgent() *LaunchAgent {
	return &LaunchAgent{
		exec:     runCommand,
		writeFn:  os.WriteFile,
		removeFn: os.Remove,
		timeout:  5 * time.Second,
	}
}

// SetExec overrides launchctl invocations; tests pin this so no real
// launchctl runs.
func (a *LaunchAgent) SetExec(fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	if fn != nil {
		a.exec = fn
	}
}

// SetWriteFn overrides the file writer; tests pin this for failure
// injection.
func (a *LaunchAgent) SetWriteFn(fn func(path string, data []byte, mode os.FileMode) error) {
	if fn != nil {
		a.writeFn = fn
	}
}

// SetRemoveFn overrides the file remover; tests pin this for failure
// injection.
func (a *LaunchAgent) SetRemoveFn(fn func(path string) error) {
	if fn != nil {
		a.removeFn = fn
	}
}

// Install writes the launch-agent plist and runs `launchctl load`
// against it. Idempotent: re-installing rewrites the plist content.
//
// daemonBinary is the absolute path to the slimference binary that
// the agent should exec. logDir is the directory for stdout / stderr
// log files (created by launchd if it does not exist).
func (a *LaunchAgent) Install(plistPath, daemonBinary, logDir string) error {
	if plistPath == "" || daemonBinary == "" {
		return fmt.Errorf("transparent: launchd Install requires plistPath and daemonBinary")
	}
	plist := renderLaunchPlist(daemonBinary, logDir)
	if err := a.writeFn(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("transparent: write plist %s: %w", plistPath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	out, err := a.exec(ctx, "launchctl", "load", "-w", plistPath)
	if err != nil {
		// load errors when the agent is already loaded; that is a
		// success state for our idempotent contract. Surface other
		// errors verbatim.
		if strings.Contains(string(out), "already loaded") {
			return nil
		}
		return fmt.Errorf("transparent: launchctl load: %w (output: %s)", err, string(out))
	}
	return nil
}

// Uninstall unloads the launch agent and removes the plist. The
// unload err is intentionally swallowed: "not currently loaded" is
// the same end-state we want, and any other launchctl error becomes
// observable when the operator runs `slimference proxy status` again.
func (a *LaunchAgent) Uninstall(plistPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	_, _ = a.exec(ctx, "launchctl", "unload", "-w", plistPath)
	if err := a.removeFn(plistPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("transparent: remove plist %s: %w", plistPath, err)
	}
	return nil
}

// IsInstalled reports whether the plist file exists at plistPath.
func (a *LaunchAgent) IsInstalled(plistPath string) bool {
	_, err := os.Stat(plistPath)
	return err == nil
}

// DefaultPlistPath returns the conventional path under the operator's
// home directory.
func DefaultPlistPath(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.slimference.daemon.plist")
}

func renderLaunchPlist(daemonBinary, logDir string) string {
	if logDir == "" {
		logDir = "/tmp"
	}
	stdoutPath := filepath.Join(logDir, "slimference.out.log")
	stderrPath := filepath.Join(logDir, "slimference.err.log")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.slimference.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>--no-tui</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, daemonBinary, stdoutPath, stderrPath)
}

// defaultHome returns the operator's HOME or "" when unset.
func defaultHome() string {
	return os.Getenv("HOME")
}
