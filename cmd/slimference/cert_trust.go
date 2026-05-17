package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var openKeychainAccessFn = openKeychainAccess

// handleCertTrustCmd is `slimference cert-trust`. It opens Keychain
// Access pointed at the user's login keychain so the user can mark
// the Slimference Root CA as Always Trust for SSL — the one step
// macOS requires interactive UI authentication for, which a
// non-interactive shell cannot trigger via `security` alone.
//
// Without this trust step, the SNI-peek transparent MITM still
// terminates TLS (the daemon uses its CA to sign leaf certs) but the
// CLIENT will reject the connection because the CA isn't trusted.
// Hence: this step is the gate between "installed" and "actually
// armed end-to-end".
func handleCertTrustCmd(args []string) {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(certTrustHelp)
			return
		}
	}
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		fmt.Fprintln(os.Stderr, "cert-trust: HOME unresolved")
		exitFn(1)
		return
	}
	cert := filepath.Join(home, ".slimference", "ca", "root.crt")
	if _, err := os.Stat(cert); err != nil {
		fmt.Fprintf(os.Stderr, "cert-trust: CA cert missing at %s — run `slimference install` first.\n", cert)
		exitFn(1)
		return
	}

	fmt.Println("Slimference CA trust setup")
	fmt.Println("---------------------------")
	fmt.Println()
	fmt.Printf("CA cert path: %s\n", cert)
	fmt.Println()
	fmt.Println("macOS requires interactive GUI authentication to mark a CA")
	fmt.Println("as 'Always Trust' for SSL. There are two paths:")
	fmt.Println()
	fmt.Println("  A) GUI:  open Keychain Access for you now (recommended)")
	fmt.Println("  B) CLI:  print the sudo one-liner you can paste yourself")
	fmt.Println()

	// Try opening Keychain Access pointed at the cert. The user
	// double-clicks the entry → "When using this certificate" =
	// "Always Trust" → enter password.
	if err := openKeychainAccessFn(cert); err != nil {
		fmt.Fprintf(os.Stderr, "  (could not auto-open Keychain Access: %v)\n", err)
	} else {
		fmt.Println("  Keychain Access opened. Steps:")
		fmt.Println("    1. Find 'Slimference Local CA …' (already added).")
		fmt.Println("    2. Double-click it → expand 'Trust' → set 'When using")
		fmt.Println("       this certificate' to 'Always Trust'.")
		fmt.Println("    3. Close the window → enter your password when prompted.")
		fmt.Println("    4. Re-run `slimference status` — 'CA in_keychain=true'.")
		fmt.Println()
	}

	fmt.Println("Or paste this command (requires sudo password):")
	fmt.Println()
	fmt.Printf("    sudo security add-trusted-cert -d -r trustRoot -p ssl \\\n        -k /Library/Keychains/System.keychain %s\n", cert)
	fmt.Println()
	fmt.Println("After trust is set, restart any open Codex session so")
	fmt.Println("its TLS client re-reads the system trust store.")
}

// openKeychainAccess invokes the `open` command pointed at the cert
// path. macOS's `open` resolves .crt files to Keychain Access by
// default.
func openKeychainAccess(certPath string) error {
	cmd := exec.Command("open", certPath)
	return cmd.Run()
}

const certTrustHelp = `usage: slimference cert-trust

Guides the one interactive step macOS requires after install: marking
the Slimference Root CA as "Always Trust" for SSL.

Without this step, Codex clients reject the connection when transparent
MITM is armed.

Run after ` + "`slimference install`" + `, before ` + "`slimference lab enable`" + `.

Flags:
  --help, -h    this text
`
