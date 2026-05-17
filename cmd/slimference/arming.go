package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	runWithAdminPrivilegesFn = runWithAdminPrivileges
	createRootScriptTempFn   = func(dir, pattern string) (atomicTempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	removeRootScriptFn = os.Remove
	execOsascriptFn    = func(script string) ([]byte, error) {
		return exec.Command("osascript", "-e", script).CombinedOutput()
	}
)

// handleRootArmCmd is `slimference root-arm`. It performs the two
// macOS operations that require root and would otherwise need an
// interactive sudo prompt:
//
//  1. Patch /etc/hosts with marker-fenced Codex-only entries
//     redirecting chatgpt.com / api.openai.com to loopback.
//  2. Load a pfctl rdr anchor that redirects TCP 443 → 127.0.0.1:8443
//     so the unprivileged daemon's SNI-peek listener catches the
//     traffic.
//
// Both run via `osascript ... with administrator privileges` so the
// user sees ONE GUI password prompt instead of repeated sudo prompts.
//
// Idempotent: re-running root-arm is a no-op if both operations are
// in place. Reversed by `slimference root-disarm`.
func handleRootArmCmd(args []string) {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(rootArmHelp)
			return
		}
	}
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		fmt.Fprintln(os.Stderr, "root-arm: HOME unresolved")
		exitFn(1)
		return
	}
	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	if _, err := os.Stat(cert); err != nil {
		fmt.Fprintf(os.Stderr, "root-arm: CA cert missing at %s\n", cert)
		fmt.Fprintln(os.Stderr, "  run `slimference install` first.")
		exitFn(1)
		return
	}

	fmt.Println("slimference root-arm")
	fmt.Println("--------------------")
	fmt.Println()
	fmt.Println("This will request administrator privileges to:")
	fmt.Println("  1. Add marker-fenced entries to /etc/hosts")
	fmt.Println("  2. Load a pfctl rdr anchor (port 443 → 127.0.0.1:8443)")
	fmt.Println()
	fmt.Println("(CA trust is handled separately via Keychain Access — macOS")
	fmt.Println(" forbids non-interactive trust changes even as root.)")
	fmt.Println()
	fmt.Println("A macOS password dialog will appear shortly.")
	fmt.Println()

	script := buildRootArmScript(cert)
	if err := runWithAdminPrivilegesFn(script, "Slimference: arm transparent MITM"); err != nil {
		fmt.Fprintf(os.Stderr, "root-arm: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Println("✓ Hosts + pfctl armed.")
	fmt.Println()
	fmt.Println("CA trust — one click in Keychain Access:")
	fmt.Println("  1. Run `slimference cert-trust` (opens Keychain Access).")
	fmt.Println("  2. Find 'Slimference Local CA …', double-click, expand Trust,")
	fmt.Println("     set 'When using this certificate' to 'Always Trust'.")
	fmt.Println("  3. Close window, enter password.")
	fmt.Println()
	fmt.Println("Then `slimference status` shows hosts_active=true + CA trusted.")
}

// handleRootDisarmCmd reverses root-arm.
func handleRootDisarmCmd(args []string) {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(rootDisarmHelp)
			return
		}
	}
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		fmt.Fprintln(os.Stderr, "root-disarm: HOME unresolved")
		exitFn(1)
		return
	}

	fmt.Println("slimference root-disarm")
	fmt.Println("-----------------------")
	fmt.Println()
	fmt.Println("Requesting admin privileges to revert /etc/hosts and pfctl rdr.")
	fmt.Println()

	script := buildRootDisarmScript()
	if err := runWithAdminPrivilegesFn(script, "Slimference: disarm transparent MITM"); err != nil {
		fmt.Fprintf(os.Stderr, "root-disarm: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Println("✓ Transparent MITM disarmed. Codex talks direct again.")
}

// buildRootArmScript assembles the two sudo-required ops that DO
// work non-interactively as root: /etc/hosts patch + pfctl rdr anchor.
//
// CA trust is NOT in this script because macOS requires explicit
// user interaction (TouchID / password dialog) to mutate trust
// settings — even root cannot bypass that. The CLI prompts the user
// to handle CA trust via Keychain Access GUI separately.
func buildRootArmScript(cert string) string {
	_ = cert // retained for future use if Apple ever exposes a non-interactive path
	return `
echo '[1/2] patching /etc/hosts...'
TMP=$(mktemp /tmp/slim-hosts.XXXXXX)
awk '
  /# slimference:start/ { skip=1; next }
  /# slimference:end/   { skip=0; next }
  !skip { print }
' /etc/hosts > "$TMP"
printf '\n# slimference:start\n127.0.0.1 chatgpt.com api.openai.com\n# slimference:end\n' >> "$TMP"
mv "$TMP" /etc/hosts
chmod 644 /etc/hosts
# Flush DNS cache so existing resolver state doesn't shadow the new map.
dscacheutil -flushcache 2>/dev/null || true
killall -HUP mDNSResponder 2>/dev/null || true

echo '[2/2] loading pfctl rdr anchor...'
echo 'rdr pass on lo0 inet proto tcp from any to any port 443 -> 127.0.0.1 port 8443' > /tmp/slim-pfctl-rule.conf
pfctl -a slimference -f /tmp/slim-pfctl-rule.conf 2>/dev/null || echo '  (pfctl anchor load: non-fatal)'
pfctl -E 2>/dev/null || echo '  (pfctl enable: non-fatal — may already be on)'
rm -f /tmp/slim-pfctl-rule.conf

echo 'done.'
`
}

// buildRootDisarmScript reverses everything root-arm did.
func buildRootDisarmScript() string {
	return `
echo '[1/2] stripping marker-fenced block from /etc/hosts...'
TMP=$(mktemp /tmp/slim-hosts.XXXXXX)
awk '
  /# slimference:start/ { skip=1; next }
  /# slimference:end/   { skip=0; next }
  !skip { print }
' /etc/hosts > "$TMP"
mv "$TMP" /etc/hosts
chmod 644 /etc/hosts

echo '[2/2] flushing pfctl anchor...'
pfctl -a slimference -F all 2>/dev/null || echo '  (no anchor to flush)'

echo 'done.'
echo ''
echo 'CA trust in Keychain Access is NOT removed automatically.'
echo 'If desired: open Keychain Access, search "Slimference", delete the entry.'
`
}

// runWithAdminPrivileges writes the shell script to a temp file and
// asks osascript to execute it via `do shell script "/bin/sh /tmp/...
// " with administrator privileges`. Routing through a file avoids the
// triple-quoting nightmare of embedding a shell script inside an
// AppleScript string inside a Go string.
func runWithAdminPrivileges(script, prompt string) error {
	tmp, err := createRootScriptTempFn("", "slim-root-*.sh")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer removeRootScriptFn(tmp.Name())
	// `set -e` so a failure mid-script aborts the rest.
	body := "#!/bin/sh\nset -e\n" + script + "\n"
	if _, err := tmp.Write([]byte(body)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write script: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	apple := fmt.Sprintf(
		`do shell script "/bin/sh %s 2>&1" with prompt "%s" with administrator privileges`,
		tmp.Name(), prompt,
	)
	out, err := execOsascriptFn(apple)
	if err != nil {
		text := string(out)
		if strings.Contains(text, "User cancelled") || strings.Contains(text, "-128") {
			return fmt.Errorf("user cancelled the admin prompt")
		}
		return fmt.Errorf("osascript: %w (output: %s)", err, strings.TrimSpace(text))
	}
	if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
		fmt.Fprintln(os.Stderr, "  script output:", trimmed)
	}
	return nil
}

const rootArmHelp = `usage: slimference root-arm

Performs the macOS root-required steps to arm transparent MITM:
  1. Patch /etc/hosts (marker-fenced) for the Codex hosts
  2. Load pfctl rdr anchor: port 443 → 127.0.0.1:8443

Triggers ONE macOS admin password dialog via osascript. Idempotent —
re-running is harmless.

CA trust is handled separately via Keychain Access; see
` + "`slimference cert-trust`" + `. macOS requires explicit user
interaction for trust changes — even root cannot bypass.

Reversed by: slimference root-disarm

Flags:
  --help, -h    this text
`

const rootDisarmHelp = `usage: slimference root-disarm

Reverses ` + "`slimference root-arm`" + `:
  1. Strip the marker-fenced block from /etc/hosts
  2. Flush the pfctl rdr anchor

Triggers ONE macOS admin password dialog. Idempotent.

CA trust in Keychain Access is NOT removed automatically; do it via
Keychain Access if desired.

Flags:
  --help, -h    this text
`
