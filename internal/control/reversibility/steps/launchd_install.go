package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/slimference/slimference/internal/control/reversibility"
)

// LaunchdInstall is the install step that registers the Slimference
// proxy daemon with launchd. On macOS the user-scope LaunchAgent
// lives under `~/Library/LaunchAgents/<Label>.plist` and is loaded
// with `launchctl load`. We avoid the system-wide `/Library/
// LaunchDaemons/` location so installation does not require sudo.
//
// Apply writes the plist + runs `launchctl load`. Reverse runs
// `launchctl unload` + removes the plist file. Inspect reports
// present if the plist file exists.
type LaunchdInstall struct {
	// Label is the launchd job label, conventionally reverse-DNS.
	// Defaults to "com.slimference.proxy".
	Label string
	// PlistDir overrides the install location; defaults to
	// `~/Library/LaunchAgents/` (per-user).
	PlistDir string
	// BinaryPath is the absolute path to the slimference binary.
	BinaryPath string
	// Args is the argument list passed to the binary in the plist.
	// Defaults to {"--no-tui"}.
	Args []string
	// LaunchctlPath overrides the launchctl binary path. Tests pass a
	// stub script here so apply/reverse don't actually load anything
	// into the host's launchd.
	LaunchctlPath string
	// SkipLoad disables the `launchctl load|unload` invocations.
	// Useful for tests that just want to verify file lifecycle.
	SkipLoad bool
	// Now overrides time-of-day for log labels in the plist.
	// Currently unused but retained for forward compatibility.
}

const launchdStepName = "launchd.install"

var launchdUserHomeDirFn = os.UserHomeDir

// Name implements reversibility.Step.
func (s *LaunchdInstall) Name() string { return launchdStepName }

// Apply writes the plist and (unless SkipLoad) loads it.
func (s *LaunchdInstall) Apply(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("steps: mkdir plist dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(s.renderPlist()), 0o644); err != nil {
		return fmt.Errorf("steps: write plist: %w", err)
	}
	if s.SkipLoad {
		return nil
	}
	cmd := exec.CommandContext(ctx, s.launchctl(), "load", "-w", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Clean up the plist if load fails so re-runs aren't blocked.
		_ = os.Remove(path)
		return fmt.Errorf("steps: launchctl load: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Reverse unloads + removes the plist. Idempotent: missing plist is
// a no-op success.
func (s *LaunchdInstall) Reverse(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	path, err := s.plistPath()
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("steps: stat plist: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("steps: %s is not a regular file", path)
	}
	if !s.SkipLoad {
		cmd := exec.CommandContext(ctx, s.launchctl(), "unload", "-w", path)
		// Ignore unload errors - the plist may already be unloaded
		// and we still want to remove the file.
		_, _ = cmd.CombinedOutput()
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("steps: remove plist: %w", err)
	}
	return nil
}

// Inspect reports whether the plist file exists.
func (s *LaunchdInstall) Inspect(ctx context.Context) reversibility.StepState {
	path, err := s.plistPath()
	if err != nil {
		return reversibility.StateUnknown
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return reversibility.StateAbsent
		}
		return reversibility.StateUnknown
	}
	if !info.Mode().IsRegular() {
		return reversibility.StatePartial
	}
	return reversibility.StatePresent
}

func (s *LaunchdInstall) validate() error {
	if s.BinaryPath == "" {
		return errors.New("steps: LaunchdInstall BinaryPath empty")
	}
	return nil
}

func (s *LaunchdInstall) plistPath() (string, error) {
	dir := s.PlistDir
	if dir == "" {
		home, err := launchdUserHomeDirFn()
		if err != nil {
			return "", fmt.Errorf("steps: home dir: %w", err)
		}
		dir = filepath.Join(home, "Library", "LaunchAgents")
	}
	return filepath.Join(dir, s.label()+".plist"), nil
}

func (s *LaunchdInstall) label() string {
	if s.Label != "" {
		return s.Label
	}
	return "com.slimference.proxy"
}

func (s *LaunchdInstall) launchctl() string {
	if s.LaunchctlPath != "" {
		return s.LaunchctlPath
	}
	return "/bin/launchctl"
}

func (s *LaunchdInstall) renderPlist() string {
	args := s.Args
	if len(args) == 0 {
		args = []string{"--no-tui"}
	}
	var argLines strings.Builder
	argLines.WriteString(fmt.Sprintf("        <string>%s</string>\n", xmlEscape(s.BinaryPath)))
	for _, a := range args {
		argLines.WriteString(fmt.Sprintf("        <string>%s</string>\n", xmlEscape(a)))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
%s    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ProcessType</key>
    <string>Background</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>StandardOutPath</key>
    <string>%s</string>
</dict>
</plist>
`, xmlEscape(s.label()), argLines.String(),
		xmlEscape(s.stdErrPath()), xmlEscape(s.stdOutPath()))
}

func (s *LaunchdInstall) stdErrPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".slimference", "log", "daemon.err.log")
}

func (s *LaunchdInstall) stdOutPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".slimference", "log", "daemon.out.log")
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

var _ reversibility.Step = (*LaunchdInstall)(nil)
