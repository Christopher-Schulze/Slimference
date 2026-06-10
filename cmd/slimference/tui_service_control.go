package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/codexroute"
	"github.com/Christopher-Schulze/Slimference/internal/tlsca"
	"github.com/Christopher-Schulze/Slimference/internal/transparent"
	"github.com/Christopher-Schulze/Slimference/internal/tui"
)

// serviceControlAdapter implements tui.ServiceControlInterface by calling daemon package functions
// and spawning subprocesses for hook install/remove.
type serviceControlAdapter struct{}

var (
	tuiInstallCmdFn            = runInstallCmd
	tuiEnableCmdFn             = runLabEnableCmd
	tuiDisableCmdFn            = runLabDisableCmd
	tuiUninstallCmdFn          = runUninstallCmd
	tuiCodexRouteEnableCmdFn   = runCodexEnableCmd
	tuiCodexRouteDisableCmdFn  = runCodexDisableCmd
	tuiCodexRouteHealthCheckFn = codexRouteHealthFn
	tuiCodexDesktopDirectFn    = func(dir string) error {
		args := []string{"-a", "Codex"}
		if dir != "" {
			args = append(args, dir)
		}
		cmd := exec.Command("open", args...)
		cmd.Env = codexDesktopDirectOpenEnv(os.Environ())
		return cmd.Run()
	}
	tuiLaunchCommandFn = func(name string, args ...string) error {
		return exec.Command(name, args...).Run()
	}
)

func (sca *serviceControlAdapter) StartDaemon() error {
	running, existing, err := daemonIsRunningFn()
	if err != nil {
		return fmt.Errorf("check daemon: %w", err)
	}
	if running {
		if existing == nil || existing.PID <= 0 {
			return fmt.Errorf("daemon state says running but PID metadata is invalid; run `slimference service status` and remove stale PID state if needed")
		}
		return fmt.Errorf("already running (PID %d, port %d)", existing.PID, existing.Port)
	}
	binary, err := resolveDaemonLifecycleBinary("start")
	if err != nil {
		return err
	}
	if err := startDetachedDaemonFn(binary); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	if _, err := waitForDaemonStarted(daemonStartTimeout, daemonStartPollInterval); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return nil
}

func (sca *serviceControlAdapter) StopDaemon() error {
	return daemonStopFn()
}

func (sca *serviceControlAdapter) RestartDaemon() error {
	running, _, err := daemonIsRunningFn()
	if err != nil {
		return fmt.Errorf("check daemon: %w", err)
	}
	if running {
		if err := daemonStopFn(); err != nil {
			return err
		}
	}
	return sca.StartDaemon()
}

func (sca *serviceControlAdapter) InstallService() error {
	binary, err := resolveDaemonLifecycleBinary("service install")
	if err != nil {
		return err
	}
	return daemonInstallLaunchdFn(binary)
}

func (sca *serviceControlAdapter) UninstallService() error {
	return daemonUninstallFn()
}

func (sca *serviceControlAdapter) DaemonStatus() (bool, int, int) {
	running, pf, _ := daemonIsRunningFn()
	if !running || pf == nil {
		return false, 0, 0
	}
	return true, pf.PID, pf.Port
}

func (sca *serviceControlAdapter) DaemonNotice() string {
	return staleSlimferenceProcessNoticeFn()
}

func (sca *serviceControlAdapter) TransparentStatus() tui.TransparentStatus {
	home := os.Getenv("HOME")
	status := tui.TransparentStatus{}
	if home == "" {
		status.Detail = "HOME unresolved"
		return status
	}

	certPath := filepath.Join(home, ".slimference", "ca", "root.crt")
	if _, err := os.Stat(certPath); err == nil {
		status.CAExists = true
		trusted, trustErr := newTransparentKeychainFn().IsTrusted(certPath)
		status.CATrusted = trusted
		if trustErr != nil && status.Detail == "" {
			status.Detail = trustErr.Error()
		}
	}

	plistPath := transparent.DefaultPlistPath(home)
	status.AutoStartInstalled = newTransparentLaunchFn().IsInstalled(plistPath)

	snap := newTransparentNetworkFn().Status()
	if snap.UnreachableErr != nil {
		status.NetworkUnavailable = true
		if status.Detail == "" {
			status.Detail = snap.UnreachableErr.Error()
		}
		return status
	}
	for _, svc := range snap.Services {
		if !svc.HTTPSEnabled || !isSlimferenceProxyTarget(svc.HTTPSProxy, svc.HTTPSPort) {
			continue
		}
		status.ProxyArmed = true
		status.ActiveServices++
		if status.DaemonReachable {
			continue
		}
		if err := transparentProxyHealthFn(svc.HTTPSProxy, svc.HTTPSPort); err == nil {
			status.DaemonReachable = true
		} else if status.Detail == "" {
			status.Detail = err.Error()
		}
	}
	return status
}

func (sca *serviceControlAdapter) InstallTransparent() error {
	return sca.runInstallLifecycleCommand(tuiInstallCmdFn, nil)
}

func (sca *serviceControlAdapter) EnableTransparent() error {
	return sca.runInstallLifecycleCommand(tuiEnableCmdFn, nil)
}

func (sca *serviceControlAdapter) DisableTransparent() error {
	return sca.runInstallLifecycleCommand(tuiDisableCmdFn, nil)
}

func (sca *serviceControlAdapter) UninstallTransparent() error {
	return sca.runInstallLifecycleCommand(tuiUninstallCmdFn, nil)
}

func (sca *serviceControlAdapter) CodexRouteStatus() tui.CodexRouteStatus {
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		return tui.CodexRouteStatus{Detail: "HOME unresolved"}
	}
	proxyURL := codexroute.ProxyURL("127.0.0.1", "8990")
	status, err := codexRouteInspectFn(home, proxyURL, codexroute.Options{})
	out := tui.CodexRouteStatus{
		Exists:             status.Exists,
		Enabled:            status.Enabled,
		Complete:           status.Complete,
		Conflict:           status.Conflict,
		LegacyKeys:         status.LegacyKeys,
		Transport:          status.Transport,
		CertificationPath:  codexroute.CertificationPath(home),
		ActiveCLIProcesses: scopedCodexCLIActiveCountFn(),
	}
	if err != nil {
		out.Detail = err.Error()
		return out
	}
	auto := codexAutoFn(home)
	out.AutoTransport = string(auto.Transport)
	out.AutoMode = string(auto.Mode)
	out.WSSCertified = auto.WSSCertified
	out.WSSBridgeAvailable = auto.WSSBridgeAvailable
	out.NeedsRecert = auto.NeedsRecert
	out.CurrentCodex = auto.CurrentCodex
	out.CurrentSlimference = auto.CurrentSlimference
	out.CertifiedCodex = auto.CertifiedCodex
	out.CertifiedSlimference = auto.CertifiedSlimference
	out.FallbackReason = auto.FallbackReason
	out.BridgeProofPath = auto.BridgeProofPath
	out.RecertStatePath = auto.RecertStatePath
	out.RecertLogPath = auto.RecertLogPath
	out.RecertStatus = auto.RecertStatus
	out.RecertAttemptID = auto.RecertAttemptID
	out.RecertStartedAt = auto.RecertStartedAt
	out.RecertFinishedAt = auto.RecertFinishedAt
	out.RecertLastSuccessAt = auto.RecertLastSuccessAt
	out.RecertRetryAfter = auto.RecertRetryAfter
	out.RecertLastError = auto.RecertLastError
	out.RecertCommand = auto.RecertCommand
	out.Detail = auto.LastWSSError
	if err := tuiCodexRouteHealthCheckFn("127.0.0.1", "8990"); err != nil {
		out.Detail = err.Error()
		return out
	}
	out.DaemonReachable = true
	if auto.NeedsRecert {
		codexAutoRecertFn(home, "127.0.0.1", "8990", auto)
	}
	return out
}

func (sca *serviceControlAdapter) CodexDesktopStatus() tui.CodexDesktopStatus {
	status := buildCodexDesktopStatus(codexDesktopStatusFlags{host: "127.0.0.1", port: "8990"})
	appServerProcesses := codexDesktopAppServerCountFn()
	appProcesses := 0
	if appPath := strings.TrimSpace(codexDesktopAppPathFn()); appPath != "" {
		binary := filepath.Join(appPath, defaultCodexDesktopExecRelPath)
		if pids, err := codexDesktopRunningFn(binary); err == nil {
			appProcesses = len(pids)
		}
	}
	detail := status.DaemonError
	if detail == "" && len(status.Notes) > 0 {
		detail = status.Notes[0]
	}
	return tui.CodexDesktopStatus{
		Mode:                 status.Mode,
		FailureClass:         status.FailureClass,
		DaemonReachable:      status.DaemonReachable,
		AppServerActive:      appServerProcesses > 0 || codexDesktopAppServerActiveFn(),
		AppServerProcesses:   appServerProcesses,
		AppProcesses:         appProcesses,
		CATrusted:            status.CATrust.Trusted,
		CAExists:             status.CATrust.Exists,
		ConversationObserved: status.ConversationObserved,
		Detail:               detail,
	}
}

func (sca *serviceControlAdapter) LaunchCodexCLI() (string, error) {
	binary, err := osExecutable()
	if err != nil {
		return "", fmt.Errorf("executable: %w", err)
	}
	dir, err := tuiLaunchDirectory()
	if err != nil {
		return "", err
	}
	return launchCodexCLIInCurrentTerminal(binary, dir)
}

func (sca *serviceControlAdapter) LaunchCodexApp() (string, error) {
	status := buildCodexDesktopStatus(codexDesktopStatusFlags{host: "127.0.0.1", port: "8990"})
	launchable := status.Mode == "desktop_app_server_phasef_proven" ||
		status.Mode == "desktop_app_server_proven" ||
		status.Mode == "desktop_app_server_route_ready"
	if !launchable || !status.ConversationObserved || status.FailureClass != "" {
		reason := status.FailureClass
		if reason == "" {
			reason = status.Mode
		}
		return "", fmt.Errorf("Desktop Slimference proof is not green (%s). Start Codex.app normally outside Slimference for direct mode, or run `slimference codex desktop prove --manual --json` to prove the app-server shim route", reason)
	}
	var out, errBuf strings.Builder
	rc := runCodexLaunchDesktopCmd(
		[]string{"--transport=app-server", "--replace-existing"},
		installPrinter{Out: &out, Err: &errBuf},
	)
	if rc != 0 {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = fmt.Sprintf("exit %d", rc)
		}
		return "", fmt.Errorf("launch Codex.app via Slimference: %s", msg)
	}
	_ = strings.TrimSpace(out.String())
	return "Codex App started with Slimference", nil
}

func tuiLaunchDirectory() (string, error) {
	dir, err := osGetwd()
	if err != nil {
		return "", fmt.Errorf("resolve launch directory: %w", err)
	}
	if dir == "" {
		return "", fmt.Errorf("resolve launch directory: empty working directory")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("launch directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("launch directory %q is not a directory", dir)
	}
	return dir, nil
}

func (sca *serviceControlAdapter) RepairCodexWSS() (string, error) {
	var stdout strings.Builder
	var stderr strings.Builder
	rc := runCodexRecertifyCmd([]string{"wss", "--force", "--operator=tui", "--notes=TUI Repair CLI WSS"}, installPrinter{Out: &stdout, Err: &stderr})
	if rc != 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = fmt.Sprintf("codex recertify failed with exit %d", rc)
		}
		return "", fmt.Errorf("%s", msg)
	}
	msg := strings.TrimSpace(stdout.String())
	if msg == "" {
		msg = "Codex CLI WSS repaired"
	}
	return msg, nil
}

func (sca *serviceControlAdapter) EnableCodexRoute() error {
	return sca.runInstallLifecycleCommand(tuiCodexRouteEnableCmdFn, nil)
}

func (sca *serviceControlAdapter) DisableCodexRoute() error {
	return sca.runInstallLifecycleCommand(tuiCodexRouteDisableCmdFn, nil)
}

func (sca *serviceControlAdapter) runInstallLifecycleCommand(fn func([]string, installPrinter) int, args []string) error {
	var stdout strings.Builder
	var stderr strings.Builder
	rc := fn(args, installPrinter{Out: &stdout, Err: &stderr})
	if rc == 0 {
		return nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = strings.TrimSpace(stdout.String())
	}
	if msg == "" {
		msg = fmt.Sprintf("install lifecycle command failed with exit %d", rc)
	}
	return fmt.Errorf("%s", msg)
}

func (sca *serviceControlAdapter) runTransparentProxyCommand(args ...string) error {
	var stdout strings.Builder
	var stderr strings.Builder
	rc := proxyRunFn(args, proxyCommandEnv(&stdout, &stderr, os.Stdin))
	if rc == 0 {
		return nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = strings.TrimSpace(stdout.String())
	}
	if msg == "" {
		msg = fmt.Sprintf("proxy %s failed with exit %d", strings.Join(args, " "), rc)
	}
	return fmt.Errorf("%s", msg)
}

func proxyCommandEnv(stdout io.Writer, stderr io.Writer, stdin io.Reader) proxyEnv {
	home := os.Getenv("HOME")
	return proxyEnv{
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,
		Home:   home,
		CADirFn: func() string {
			return filepath.Join(home, ".slimference")
		},
		Network:     newTransparentNetworkFn(),
		Keychain:    newTransparentKeychainFn(),
		Launch:      newTransparentLaunchFn(),
		LoadCA:      tlsca.LoadOrGenerateCA,
		HealthCheck: transparentProxyHealthFn,
	}
}

func (sca *serviceControlAdapter) InstallHook(target string) error {
	if target == "claude" {
		return fmt.Errorf("Claude Code hooks are parked; Slimference installs Codex hooks only")
	}
	home, err := osUserHomeDir()
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	var tpCmd string
	if cfg, err := configLoadFn(); err == nil {
		tpCmd = strings.TrimSpace(cfg.Hooks.SlimferenceCommand)
	}
	switch target {
	case "codex":
		return installCodexHookFn(home, tpCmd)
	default:
		return fmt.Errorf("unknown hook target: %s", target)
	}
}

func (sca *serviceControlAdapter) RemoveHook(target string) error {
	if target == "claude" {
		return fmt.Errorf("Claude Code hooks are parked; Slimference will not modify ~/.claude")
	}
	home, err := osUserHomeDir()
	if err != nil {
		return fmt.Errorf("home: %w", err)
	}
	switch target {
	case "codex":
		return removeCodexHookFn(home)
	default:
		return fmt.Errorf("unknown hook target: %s", target)
	}
}
