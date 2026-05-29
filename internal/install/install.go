// Package install owns the Slimference install/uninstall/enable/disable
// lifecycle. It is the single source of truth consumed by both the CLI
// (cmd/slimference) and the TUI (internal/tui). Every operation is
// expressed as a reversibility.Plan so install/uninstall are atomic
// and round-trip-clean.
//
// Scoped Codex architecture:
//
//   - Install plan = CA material + launchd + hooks
//   - Desktop/lab CA trust = explicit opt-in Keychain step
//   - Scoped CLI  = `slimference codex run -- <prompt>`
//   - Shared route = `slimference codex enable|disable|status`
//   - Hosts plan  = global lab-only /etc/hosts marker-fenced patch
//
// What this package does NOT touch: OPENAI_API_BASE env, HTTPS_PROXY
// env, openai_base_url in ~/.codex/config.toml, macOS system network
// proxy settings. Those are legacy/advanced surfaces the user
// configures manually; the install path stays Codex-scoped by default.
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/slimference/slimference/internal/control/reversibility"
	"github.com/slimference/slimference/internal/control/reversibility/steps"
	"github.com/slimference/slimference/internal/install/installsteps"
)

var (
	userHomeDirFn = os.UserHomeDir
	executableFn  = os.Executable
)

// Options controls what the install Plan does. Zero-value options are
// valid and resolve to: HOME from os.UserHomeDir, BinaryPath from
// os.Executable, Codex hooks on, autostart on. Claude Code support
// remains in the repository but is parked: the product install plan
// never writes ~/.claude or Claude hook files.
type Options struct {
	// Home overrides the user home directory. Tests inject a temp dir.
	Home string
	// BinaryPath overrides the path to the slimference binary written
	// into hooks + launchd plist. Empty resolves to os.Executable.
	BinaryPath string
	// Version is rendered into the SLIMFERENCE.md notices. Empty
	// renders as "unknown".
	Version string
	// SkipHooks disables the hooks.codex Step.
	SkipHooks bool
	// WithClaudeHooks is retained only for backwards-compatible flag
	// parsing. It is intentionally ignored while Slimference is
	// Codex-only. Claude Code implementation files stay in tree, but
	// install/uninstall must not touch ~/.claude.
	WithClaudeHooks bool
	// SkipAutoStart disables the launchd.install Step.
	SkipAutoStart bool
	// WithKeychain opts into the ca.keychain Step. Default install keeps
	// Keychain untouched because the CLI WSS path does not need it and the
	// Desktop path first tries process-local CODEX_CA_CERTIFICATE.
	WithKeychain bool
	// SkipKeychain force-disables the ca.keychain Step. It is retained for
	// backwards-compatible flags and tests; it wins over WithKeychain.
	SkipKeychain bool
	// KeychainScope chooses user vs system Keychain. Defaults to user.
	KeychainScope installsteps.KeychainScope
	// KeychainRunner injects a stub `security` runner for tests. Nil
	// uses the real `security` binary.
	KeychainRunner installsteps.KeychainRunner
	// LaunchctlPath injects a stub `launchctl` binary for tests.
	LaunchctlPath string
	// SkipLoad disables `launchctl load|unload` invocations (tests).
	SkipLoad bool
}

// Plan returns the install reversibility.Plan describing how to bring
// a fresh machine to a Slimference-installed state.
//
// Steps (in apply order):
//  1. ca.generate    — CA material under <Home>/.slimference/ca/
//  2. optional ca.keychain — only when Options.WithKeychain is true
//  3. launchd.install     — ~/Library/LaunchAgents/com.slimference.proxy.plist
//  4. hooks.codex         — ~/.codex/hooks.json + ~/.slimference/hooks/codex-*.sh
//  5. no Claude Code steps; that path is parked and not installed
//
// Reverse order (LIFO): hooks.codex → launchd → keychain → ca.
//
// /etc/hosts is NOT part of this plan; see HostsPlan().
func Plan(opts Options) (*reversibility.Plan, error) {
	home, err := resolveHome(opts.Home)
	if err != nil {
		return nil, err
	}

	caDir := filepath.Join(home, ".slimference")
	caCertPath := filepath.Join(caDir, "ca", "root.crt")

	var stepList []reversibility.Step

	stepList = append(stepList, &steps.CAGenerate{Dir: caDir})

	if opts.WithKeychain && !opts.SkipKeychain {
		stepList = append(stepList, &installsteps.KeychainTrust{
			CertPath: caCertPath,
			Scope:    opts.KeychainScope,
			Runner:   opts.KeychainRunner,
		})
	}

	var binary string
	if !opts.SkipAutoStart || !opts.SkipHooks {
		binary, err = resolveBinary(opts.BinaryPath)
		if err != nil {
			return nil, err
		}
	}

	if !opts.SkipAutoStart {
		plistDir := filepath.Join(home, "Library", "LaunchAgents")
		stepList = append(stepList, &steps.LaunchdInstall{
			PlistDir:      plistDir,
			BinaryPath:    binary,
			LaunchctlPath: opts.LaunchctlPath,
			SkipLoad:      opts.SkipLoad,
		})
	}

	if !opts.SkipHooks {
		stepList = append(stepList,
			&installsteps.HooksCodex{Home: home, BinaryPath: binary},
			codexNotice(home, opts.Version),
		)
	}

	return reversibility.NewPlan(stepList...), nil
}

// codexNotice returns the Notice Step that drops the SLIMFERENCE.md
// README into ~/.codex/. The file lands right after HooksCodex's
// Apply so the directory exists.
func codexNotice(home, version string) *installsteps.Notice {
	return &installsteps.Notice{
		Path:     filepath.Join(home, ".codex", "SLIMFERENCE.md"),
		StepName: "notice.codex",
		Title:    "Slimference touched this folder (Codex CLI / Desktop App)",
		AppName:  "Codex CLI / Desktop App",
		Version:  version,
		Body: `Slimference installed hooks into this folder so the proxy can detect
conversation-lifecycle events (compaction, tool use, session start /
stop, permission requests) and apply prompt-cache / output-token
optimizations.

Files we own here:

- ` + "`hooks.json`" + ` — entries for PreToolUse, PostToolUse, SessionStart,
  Stop, UserPromptSubmit, PermissionRequest, PreCompact, PostCompact.
  Each calls a script under ~/.slimference/hooks/codex-*.sh.
- ` + "`config.toml`" + ` — the line ` + "`hooks = true`" + ` inside the ` + "`[features]`" + `
  table (Codex needs this to honour hooks.json).

Backups of the pre-install state live in ~/.slimference/backups/.`,
	}
}

// claudeNotice returns the Notice Step that drops the SLIMFERENCE.md
// README into ~/.claude/.
func claudeNotice(home, version string) *installsteps.Notice {
	return &installsteps.Notice{
		Path:     filepath.Join(home, ".claude", "SLIMFERENCE.md"),
		StepName: "notice.claude",
		Title:    "Slimference touched this folder (Claude Code)",
		AppName:  "Claude Code",
		Version:  version,
		Body: `Slimference installed hooks into this folder so the proxy can rewrite
Bash and Read tool invocations through its compression pipeline.

Files we own here:

- ` + "`settings.json`" + ` — PreToolUse hook entries for Bash and Read tools.
  These point at scripts under ~/.claude/hooks/slimference-*.sh.
- ` + "`hooks/slimference-rewrite.sh`" + `, ` + "`hooks/slimference-read-cache.sh`" + ` —
  the actual hook scripts Slimference owns.

Backups of the pre-install state live in ~/.slimference/backups/.`,
	}
}

// HostsOptions controls the hosts patch Plan returned by HostsPlan.
type HostsOptions struct {
	// Home overrides the user home directory; defaults to os.UserHomeDir.
	Home string
	// HostsPath overrides /etc/hosts (tests pass a temp file).
	HostsPath string
	// Targets are the hostnames to redirect. Defaults to Codex-only:
	// chatgpt.com and api.openai.com. Claude/Anthropic is opt-in later.
	Targets []string
	// Address is the loopback target. Defaults to 127.0.0.1.
	Address string
}

// HostsPlan returns the runtime hosts-patch Plan executed by the
// daemon at listener bind / shutdown time. CLI never invokes it
// directly; it writes config + signals the daemon. See
// internal/install/hosts_lifecycle.go.
func HostsPlan(opts HostsOptions) (*reversibility.Plan, error) {
	home, err := resolveHome(opts.Home)
	if err != nil {
		return nil, err
	}
	targets := opts.Targets
	if len(targets) == 0 {
		targets = DefaultHostsTargets()
	}
	address := opts.Address
	if address == "" {
		address = "127.0.0.1"
	}
	backupDir := filepath.Join(home, ".slimference", "backups")
	step := &steps.HostsPatch{
		Path:      opts.HostsPath, // empty = /etc/hosts
		Targets:   targets,
		Address:   address,
		BackupDir: backupDir,
	}
	return reversibility.NewPlan(step), nil
}

// DefaultHostsTargets returns the canonical hostnames Slimference
// intercepts when transparent mode is armed. Kept as a function (not
// a var) so callers can't mutate the underlying slice.
func DefaultHostsTargets() []string {
	return []string{"chatgpt.com", "api.openai.com"}
}

// resolveHome returns the resolved HOME path or an error.
func resolveHome(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, err := userHomeDirFn()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("install: HOME unresolved")
	}
	return home, nil
}

// resolveBinary returns the absolute path to the slimference binary.
// Defaults to os.Executable; tests inject via Options.BinaryPath.
func resolveBinary(override string) (string, error) {
	if override != "" {
		return normalizeBinaryPath(override)
	}
	exe, err := executableFn()
	if err != nil {
		return "", err
	}
	path, err := normalizeBinaryPath(exe)
	if err != nil {
		return "", err
	}
	if isTemporaryGoBuildBinary(path) {
		return "", fmt.Errorf("install: executable path %q looks like a temporary Go build artifact; run `go run ./scripts/build --install && ~/.local/bin/slimference install` from a source checkout, or pass --binary=/path/to/slimference", path)
	}
	return path, nil
}

func normalizeBinaryPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("install: binary path unresolved")
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	return filepath.Clean(path), nil
}

func isTemporaryGoBuildBinary(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.Contains(clean, "/go-build") {
		return false
	}
	tmp := filepath.ToSlash(filepath.Clean(os.TempDir()))
	if tmp != "." && strings.HasPrefix(clean, tmp+"/") {
		return true
	}
	return strings.Contains(clean, "/T/go-build")
}
