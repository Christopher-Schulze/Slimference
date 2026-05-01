package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/slimference/slimference/internal/tlsca"
	"github.com/slimference/slimference/internal/transparent"
)

// handleProxyCmd implements `slimference proxy <install|enable|disable|status|uninstall>`.
//
// Transparent mode is the system-wide intercept path: install once,
// enable to flip all HTTPS to Slimference, disable to drop back to
// direct, uninstall to remove every trace. Each subcommand is
// non-destructive by default and explains what it is about to do
// before touching the system.
func handleProxyCmd(args []string) {
	rc := proxyRun(args, proxyEnv{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Home:   os.Getenv("HOME"),
		CADirFn: func() string {
			return filepath.Join(os.Getenv("HOME"), ".slimference")
		},
		Network:  transparent.NewManager(),
		Keychain: transparent.NewKeychain(),
		Launch:   transparent.NewLaunchAgent(),
		LoadCA:   tlsca.LoadOrGenerateCA,
	})
	if rc != 0 {
		exitFn(rc)
	}
}

type proxyEnv struct {
	Stdout   io.Writer
	Stderr   io.Writer
	Stdin    io.Reader
	Home     string
	CADirFn  func() string
	Network  proxyNetworkManager
	Keychain proxyKeychain
	Launch   proxyLaunchAgent
	LoadCA   func(dir string) (*tlsca.CA, error)
}

// proxyNetworkManager is the subset of *transparent.Manager that the
// subcommand consumes. Defined here so tests can stub it.
type proxyNetworkManager interface {
	EnableHTTPS(host, port string) ([]string, error)
	Disable() ([]string, error)
	Status() transparent.Snapshot
}

// proxyKeychain is the subset of *transparent.Keychain consumed.
type proxyKeychain interface {
	Install(certPath string, scope transparent.Scope) error
	Uninstall(certSHA1 string, scope transparent.Scope) error
	IsTrusted(certPath string) (bool, error)
}

// proxyLaunchAgent is the subset of *transparent.LaunchAgent consumed.
type proxyLaunchAgent interface {
	Install(plistPath, daemonBinary, logDir string) error
	Uninstall(plistPath string) error
	IsInstalled(plistPath string) bool
}

func proxyRun(args []string, env proxyEnv) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "usage: slimference proxy <install|enable|disable|status|uninstall>")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "install":
		return proxyInstall(rest, env)
	case "enable":
		return proxyEnable(rest, env)
	case "disable":
		return proxyDisable(rest, env)
	case "status":
		return proxyStatus(rest, env)
	case "uninstall":
		return proxyUninstall(rest, env)
	default:
		fmt.Fprintf(env.Stderr, "proxy: unknown subcommand %q\n", sub)
		return 2
	}
}

// flags is a tiny --yes / --system / --no-launchd parser shared by
// the subcommands.
type proxyFlags struct {
	yes       bool
	system    bool
	noLaunchd bool
	host      string
	port      string
}

func parseProxyFlags(args []string) (proxyFlags, error) {
	f := proxyFlags{host: "127.0.0.1", port: "8990"}
	for _, a := range args {
		switch {
		case a == "--yes":
			f.yes = true
		case a == "--system":
			f.system = true
		case a == "--no-launchd":
			f.noLaunchd = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("unknown flag %q", a)
		default:
			return f, fmt.Errorf("unexpected positional %q", a)
		}
	}
	return f, nil
}

func proxyInstall(args []string, env proxyEnv) int {
	f, err := parseProxyFlags(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy install: %v\n", err)
		return 2
	}
	if env.Home == "" {
		fmt.Fprintln(env.Stderr, "proxy install: HOME unresolved")
		return 1
	}
	if env.CADirFn == nil {
		fmt.Fprintln(env.Stderr, "proxy install: CA directory function unresolved")
		return 1
	}

	caDir := env.CADirFn()
	ca, err := env.LoadCA(caDir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy install: load/generate CA: %v\n", err)
		return 1
	}
	fingerprint := tlsca.Fingerprint(ca)
	scope := transparent.ScopeUser
	if f.system {
		scope = transparent.ScopeSystem
	}
	fmt.Fprintln(env.Stdout, "Slimference transparent mode installer")
	fmt.Fprintln(env.Stdout, "--------------------------------------")
	fmt.Fprintf(env.Stdout, "CA fingerprint (SHA-256): %s\n", fingerprint)
	fmt.Fprintf(env.Stdout, "Trust scope:              %s\n", scope.String())
	fmt.Fprintf(env.Stdout, "System proxy target:      %s:%s\n", f.host, f.port)
	if !f.noLaunchd {
		fmt.Fprintf(env.Stdout, "Auto-start (launchd):     %s\n", DefaultPlistPath(env.Home))
	} else {
		fmt.Fprintln(env.Stdout, "Auto-start (launchd):     skipped (--no-launchd)")
	}

	certPath := filepath.Join(caDir, "ca", "root.crt")
	if err := env.Keychain.Install(certPath, scope); err != nil {
		fmt.Fprintf(env.Stderr, "proxy install: keychain trust failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, "Trust:                    OK")

	if !f.noLaunchd {
		plistPath := DefaultPlistPath(env.Home)
		// os.Executable returns the test binary path under go test and
		// the slimference binary path in production. Either way it is
		// what the launchd plist should exec. The error return is
		// undocumented to fire on the platforms we target.
		bin, _ := os.Executable()
		logDir := filepath.Join(caDir, "log")
		if err := env.Launch.Install(plistPath, bin, logDir); err != nil {
			fmt.Fprintf(env.Stderr, "proxy install: launchd: %v\n", err)
			return 1
		}
		fmt.Fprintln(env.Stdout, "Auto-start:               installed")
	}

	if !f.yes {
		fmt.Fprintln(env.Stdout, "")
		fmt.Fprintln(env.Stdout, "Install complete. Run `slimference proxy enable` when you")
		fmt.Fprintln(env.Stdout, "want HTTPS traffic to flow through Slimference.")
		return 0
	}
	// --yes implies enable as the final step.
	return enableHelper(env, f.host, f.port)
}

// DefaultPlistPath delegates to the transparent package; re-exported
// so tests can compose the same path for assertion.
func DefaultPlistPath(home string) string {
	return transparent.DefaultPlistPath(home)
}

func proxyEnable(args []string, env proxyEnv) int {
	f, err := parseProxyFlags(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy enable: %v\n", err)
		return 2
	}
	return enableHelper(env, f.host, f.port)
}

func enableHelper(env proxyEnv, host, port string) int {
	services, err := env.Network.EnableHTTPS(host, port)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy enable: %v\n", err)
		return 1
	}
	if len(services) == 0 {
		fmt.Fprintln(env.Stdout, "proxy enable: no active network services to flip")
		return 0
	}
	fmt.Fprintf(env.Stdout, "Routed %d service(s) through %s:%s:\n", len(services), host, port)
	for _, s := range services {
		fmt.Fprintf(env.Stdout, "  - %s\n", s)
	}
	return 0
}

func proxyDisable(args []string, env proxyEnv) int {
	if _, err := parseProxyFlags(args); err != nil {
		fmt.Fprintf(env.Stderr, "proxy disable: %v\n", err)
		return 2
	}
	cleared, err := env.Network.Disable()
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy disable: %v\n", err)
		return 1
	}
	if len(cleared) == 0 {
		fmt.Fprintln(env.Stdout, "proxy disable: no services were flipped")
		return 0
	}
	fmt.Fprintf(env.Stdout, "Cleared HTTPS proxy on %d service(s):\n", len(cleared))
	for _, s := range cleared {
		fmt.Fprintf(env.Stdout, "  - %s\n", s)
	}
	return 0
}

func proxyStatus(args []string, env proxyEnv) int {
	if _, err := parseProxyFlags(args); err != nil {
		fmt.Fprintf(env.Stderr, "proxy status: %v\n", err)
		return 2
	}
	caDir := env.CADirFn()
	ca, err := env.LoadCA(caDir)
	caState := "ok"
	caFP := ""
	if err != nil {
		caState = fmt.Sprintf("error: %v", err)
	} else {
		caFP = tlsca.Fingerprint(ca)
	}
	fmt.Fprintln(env.Stdout, "Slimference transparent mode status")
	fmt.Fprintln(env.Stdout, "-----------------------------------")
	fmt.Fprintf(env.Stdout, "CA:                  %s\n", caState)
	if caFP != "" {
		fmt.Fprintf(env.Stdout, "CA fingerprint:      %s\n", caFP)
	}
	plistPath := DefaultPlistPath(env.Home)
	if env.Launch.IsInstalled(plistPath) {
		fmt.Fprintf(env.Stdout, "Auto-start:          installed (%s)\n", plistPath)
	} else {
		fmt.Fprintln(env.Stdout, "Auto-start:          not installed")
	}
	snap := env.Network.Status()
	if snap.UnreachableErr != nil {
		fmt.Fprintf(env.Stdout, "Network services:    error: %v\n", snap.UnreachableErr)
	} else if len(snap.Services) == 0 {
		fmt.Fprintln(env.Stdout, "Network services:    none active")
	} else {
		fmt.Fprintf(env.Stdout, "Network services:    %d active\n", len(snap.Services))
		for _, s := range snap.Services {
			active := "off"
			if s.HTTPSEnabled {
				active = fmt.Sprintf("ON %s:%s", s.HTTPSProxy, s.HTTPSPort)
			}
			fmt.Fprintf(env.Stdout, "  - %-20s %s\n", s.Name, active)
		}
	}
	return 0
}

func proxyUninstall(args []string, env proxyEnv) int {
	f, err := parseProxyFlags(args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "proxy uninstall: %v\n", err)
		return 2
	}
	// Disable first so the system never points at a nonexistent CA.
	if _, derr := env.Network.Disable(); derr != nil {
		fmt.Fprintf(env.Stderr, "proxy uninstall: warning: disable failed: %v\n", derr)
	}
	scope := transparent.ScopeUser
	if f.system {
		scope = transparent.ScopeSystem
	}
	caDir := env.CADirFn()
	ca, err := env.LoadCA(caDir)
	if err == nil {
		sha1 := tlsca.SHA1Fingerprint(ca.Cert)
		// `security delete-certificate` wants the SHA1 without colons.
		sha1 = strings.ReplaceAll(sha1, ":", "")
		if uerr := env.Keychain.Uninstall(sha1, scope); uerr != nil {
			fmt.Fprintf(env.Stderr, "proxy uninstall: warning: keychain remove failed: %v\n", uerr)
		}
	}
	plistPath := DefaultPlistPath(env.Home)
	if env.Launch.IsInstalled(plistPath) {
		if uerr := env.Launch.Uninstall(plistPath); uerr != nil {
			fmt.Fprintf(env.Stderr, "proxy uninstall: warning: launchd remove failed: %v\n", uerr)
		}
	}
	fmt.Fprintln(env.Stdout, "Slimference transparent mode: uninstall complete.")
	fmt.Fprintln(env.Stdout, "Note: the CA files in ~/.slimference/ca remain on disk so")
	fmt.Fprintln(env.Stdout, "you can re-install without regenerating. Delete the directory")
	fmt.Fprintln(env.Stdout, "manually if you want a fully clean slate.")
	_ = ctxBackground
	return 0
}

// ctxBackground is referenced by uninstall just to keep the context
// import slot active for future expansions.
var ctxBackground = context.Background()
