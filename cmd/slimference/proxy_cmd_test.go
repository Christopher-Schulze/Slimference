package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/tlsca"
	"github.com/slimference/slimference/internal/transparent"
)

// fakeNetworkManager / fakeKeychain / fakeLaunchAgent are the test
// doubles satisfying the proxy_cmd interfaces.
type fakeNetworkManager struct {
	enableServices  []string
	enableErr       error
	disableServices []string
	disableErr      error
	statusSnap      transparent.Snapshot
}

func (f *fakeNetworkManager) EnableHTTPS(host, port string) ([]string, error) {
	return f.enableServices, f.enableErr
}
func (f *fakeNetworkManager) Disable() ([]string, error) {
	return f.disableServices, f.disableErr
}
func (f *fakeNetworkManager) Status() transparent.Snapshot { return f.statusSnap }

type fakeKeychain struct {
	installPath  string
	installScope transparent.Scope
	installErr   error
	uninstallErr error
	trusted      bool
	verifyErr    error
}

func (f *fakeKeychain) Install(certPath string, scope transparent.Scope) error {
	f.installPath = certPath
	f.installScope = scope
	return f.installErr
}
func (f *fakeKeychain) Uninstall(certSHA1 string, scope transparent.Scope) error {
	return f.uninstallErr
}
func (f *fakeKeychain) IsTrusted(certPath string) (bool, error) {
	return f.trusted, f.verifyErr
}

type fakeLaunchAgent struct {
	installPlist string
	installBin   string
	installLog   string
	installErr   error
	uninstallErr error
	installed    bool
}

func (f *fakeLaunchAgent) Install(plistPath, daemonBinary, logDir string) error {
	f.installPlist = plistPath
	f.installBin = daemonBinary
	f.installLog = logDir
	return f.installErr
}
func (f *fakeLaunchAgent) Uninstall(plistPath string) error  { return f.uninstallErr }
func (f *fakeLaunchAgent) IsInstalled(plistPath string) bool { return f.installed }

func newProxyEnv(t *testing.T) (proxyEnv, *bytes.Buffer, *bytes.Buffer, *fakeNetworkManager, *fakeKeychain, *fakeLaunchAgent) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	home := t.TempDir()
	caDir := filepath.Join(home, ".slimference")
	net := &fakeNetworkManager{}
	kc := &fakeKeychain{}
	la := &fakeLaunchAgent{}
	env := proxyEnv{
		Stdout:   stdout,
		Stderr:   stderr,
		Stdin:    &bytes.Buffer{},
		Home:     home,
		CADirFn:  func() string { return caDir },
		Network:  net,
		Keychain: kc,
		Launch:   la,
		LoadCA:   tlsca.LoadOrGenerateCA,
		HealthCheck: func(host, port string) error {
			return nil
		},
	}
	return env, stdout, stderr, net, kc, la
}

func TestProxyRun_NoArgsUsage(t *testing.T) {
	t.Parallel()
	env, _, stderr, _, _, _ := newProxyEnv(t)
	if rc := proxyRun(nil, env); rc != 2 {
		t.Fatalf("expected 2, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("expected usage hint, got %q", stderr.String())
	}
}

func TestProxyRun_UnknownSubcommand(t *testing.T) {
	t.Parallel()
	env, _, stderr, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"frobnicate"}, env); rc != 2 {
		t.Fatalf("expected 2, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("expected unknown-subcommand hint, got %q", stderr.String())
	}
}

func TestProxyEnvCodex_Direct(t *testing.T) {
	t.Parallel()
	env, stdout, stderr, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"env", "codex", "--direct", "--", "exec", "hello world"}, env); rc != 0 {
		t.Fatalf("expected rc=0, got %d stderr=%q", rc, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Codex CLI direct mode",
		"-u HTTP_PROXY",
		"-u HTTPS_PROXY",
		"-u ALL_PROXY",
		"'NO_PROXY=*'",
		"'no_proxy=*'",
		"codex exec 'hello world'",
		"decisions_log",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("direct env output missing %q in: %q", want, out)
		}
	}
}

func TestProxyEnvCodex_Proxied(t *testing.T) {
	t.Parallel()
	env, stdout, stderr, _, _, _ := newProxyEnv(t)
	args := []string{"env", "codex", "--proxied", "--host=127.0.0.1", "--port=8990", "prompt"}
	if rc := proxyRun(args, env); rc != 0 {
		t.Fatalf("expected rc=0, got %d stderr=%q", rc, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Codex CLI proxied mode",
		"per-process custom provider override",
		"WebSockets are disabled",
		"-u HTTP_PROXY",
		"NO_PROXY=127.0.0.1,localhost,::1",
		"model_provider=\"slimference-codex\"",
		"model_providers.slimference-codex.base_url=\"http://127.0.0.1:8990/backend-api/codex\"",
		"model_providers.slimference-codex.requires_openai_auth=true",
		"model_providers.slimference-codex.supports_websockets=false",
		" prompt",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("proxied env output missing %q in: %q", want, out)
		}
	}
}

func TestProxyEnvCodex_IPv6Proxied(t *testing.T) {
	t.Parallel()
	got := shellJoin(codexEnvCommand("proxied", "::1", "8990", nil))
	if !strings.Contains(got, "model_providers.slimference-codex.base_url=\"http://[::1]:8990/backend-api/codex\"") {
		t.Fatalf("expected bracketed IPv6 Codex base URL, got %q", got)
	}
}

func TestProxyEnvCodex_TransparentProxied(t *testing.T) {
	t.Parallel()
	env, stdout, stderr, _, _, _ := newProxyEnv(t)
	args := []string{"env", "codex", "--transparent-proxied", "--host=127.0.0.1", "--port=8990", "prompt"}
	if rc := proxyRun(args, env); rc != 0 {
		t.Fatalf("expected rc=0, got %d stderr=%q", rc, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"transparent-proxied mode",
		"transparent].enabled=true",
		"-u NO_PROXY",
		"HTTP_PROXY=http://127.0.0.1:8990",
		"HTTPS_PROXY=http://127.0.0.1:8990",
		"ALL_PROXY=http://127.0.0.1:8990",
		"http_proxy=http://127.0.0.1:8990",
		"codex prompt",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("transparent proxied env output missing %q in: %q", want, out)
		}
	}
}

func TestProxyEnvCodex_RejectsBadArgs(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"env"},
		{"env", "claude", "--direct"},
		{"env", "codex"},
		{"env", "codex", "--direct", "--proxied"},
		{"env", "codex", "--proxied", "--direct"},
		{"env", "codex", "--proxied", "--transparent-proxied"},
		{"env", "codex", "--direct", "--bogus"},
	}
	for _, args := range cases {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			env, _, _, _, _, _ := newProxyEnv(t)
			if rc := proxyRun(args, env); rc != 2 {
				t.Fatalf("expected rc=2 for %v, got %d", args, rc)
			}
		})
	}
}

func TestShellQuote_EscapesSingleQuote(t *testing.T) {
	t.Parallel()
	got := shellJoin([]string{"codex", "exec", "it's done", ""})
	want := `codex exec 'it'"'"'s done' ''`
	if got != want {
		t.Fatalf("shellJoin = %q, want %q", got, want)
	}
}

func TestProxyInstall_HappyPath(t *testing.T) {
	t.Parallel()
	env, stdout, _, _, kc, la := newProxyEnv(t)
	la.installed = false
	if rc := proxyRun([]string{"install"}, env); rc != 0 {
		t.Fatalf("install: rc=%d", rc)
	}
	if kc.installPath == "" {
		t.Fatal("keychain.Install was not called")
	}
	if la.installPlist == "" {
		t.Fatal("launchd.Install was not called")
	}
	if !strings.Contains(stdout.String(), "CA fingerprint") {
		t.Fatalf("missing fingerprint in output, got %q", stdout.String())
	}
}

func TestProxyInstall_NoLaunchdSkipsAgent(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, la := newProxyEnv(t)
	if rc := proxyRun([]string{"install", "--no-launchd"}, env); rc != 0 {
		t.Fatalf("install: rc=%d", rc)
	}
	if la.installPlist != "" {
		t.Fatal("launchd.Install must be skipped under --no-launchd")
	}
}

func TestProxyInstall_SystemScope(t *testing.T) {
	t.Parallel()
	env, _, _, _, kc, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"install", "--system", "--no-launchd"}, env); rc != 0 {
		t.Fatalf("install: rc=%d", rc)
	}
	if kc.installScope != transparent.ScopeSystem {
		t.Fatalf("expected system scope, got %s", kc.installScope)
	}
}

func TestProxyInstall_BadFlag(t *testing.T) {
	t.Parallel()
	env, _, stderr, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"install", "--bogus"}, env); rc != 2 {
		t.Fatalf("expected 2 on bad flag, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("expected error msg, got %q", stderr.String())
	}
}

func TestProxyInstall_NoHomeFails(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, _ := newProxyEnv(t)
	env.Home = ""
	if rc := proxyRun([]string{"install"}, env); rc != 1 {
		t.Fatalf("expected 1 when HOME unresolved, got %d", rc)
	}
}

func TestProxyInstall_NilCADirFn(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, _ := newProxyEnv(t)
	env.CADirFn = nil
	if rc := proxyRun([]string{"install"}, env); rc != 1 {
		t.Fatalf("expected 1 when CADirFn nil, got %d", rc)
	}
}

func TestProxyInstall_LoadCAFailureSurfaced(t *testing.T) {
	t.Parallel()
	env, _, stderr, _, _, _ := newProxyEnv(t)
	env.LoadCA = func(dir string) (*tlsca.CA, error) { return nil, errors.New("boom") }
	if rc := proxyRun([]string{"install"}, env); rc != 1 {
		t.Fatalf("expected 1, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "load/generate CA") {
		t.Fatalf("expected CA error, got %q", stderr.String())
	}
}

func TestProxyInstall_KeychainFailureSurfaced(t *testing.T) {
	t.Parallel()
	env, _, stderr, _, kc, _ := newProxyEnv(t)
	kc.installErr = errors.New("denied")
	if rc := proxyRun([]string{"install"}, env); rc != 1 {
		t.Fatalf("expected 1, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "keychain") {
		t.Fatalf("expected keychain error, got %q", stderr.String())
	}
}

func TestProxyInstall_LaunchdFailureSurfaced(t *testing.T) {
	t.Parallel()
	env, _, stderr, _, _, la := newProxyEnv(t)
	la.installErr = errors.New("plist locked")
	if rc := proxyRun([]string{"install"}, env); rc != 1 {
		t.Fatalf("expected 1, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "launchd") {
		t.Fatalf("expected launchd error, got %q", stderr.String())
	}
}

func TestProxyInstall_YesFlowsThroughEnable(t *testing.T) {
	t.Parallel()
	env, stdout, _, net, _, _ := newProxyEnv(t)
	net.enableServices = []string{"Wi-Fi", "Ethernet"}
	if rc := proxyRun([]string{"install", "--yes"}, env); rc != 0 {
		t.Fatalf("install --yes: rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "Routed 2 service") {
		t.Fatalf("expected enable summary, got %q", stdout.String())
	}
}

func TestProxyEnable_HappyPath(t *testing.T) {
	t.Parallel()
	env, stdout, _, net, _, _ := newProxyEnv(t)
	net.enableServices = []string{"Wi-Fi"}
	if rc := proxyRun([]string{"enable"}, env); rc != 0 {
		t.Fatalf("enable: rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "Wi-Fi") {
		t.Fatalf("expected service in output, got %q", stdout.String())
	}
}

func TestProxyEnable_NoServices(t *testing.T) {
	t.Parallel()
	env, stdout, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"enable"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "no active") {
		t.Fatalf("expected no-active message, got %q", stdout.String())
	}
}

func TestProxyEnable_FailureSurfaced(t *testing.T) {
	t.Parallel()
	env, _, stderr, net, _, _ := newProxyEnv(t)
	net.enableErr = errors.New("no permission")
	if rc := proxyRun([]string{"enable"}, env); rc != 1 {
		t.Fatalf("expected 1, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "no permission") {
		t.Fatalf("expected error, got %q", stderr.String())
	}
}

func TestProxyEnable_BadFlag(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"enable", "--bogus"}, env); rc != 2 {
		t.Fatalf("expected 2, got %d", rc)
	}
}

func TestProxyDisable_HappyPath(t *testing.T) {
	t.Parallel()
	env, stdout, _, net, _, _ := newProxyEnv(t)
	net.disableServices = []string{"Wi-Fi"}
	if rc := proxyRun([]string{"disable"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "Cleared HTTPS") {
		t.Fatalf("expected cleared message, got %q", stdout.String())
	}
}

func TestProxyDisable_Empty(t *testing.T) {
	t.Parallel()
	env, stdout, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"disable"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "no services") {
		t.Fatalf("expected empty hint, got %q", stdout.String())
	}
}

func TestProxyDisable_FailureSurfaced(t *testing.T) {
	t.Parallel()
	env, _, _, net, _, _ := newProxyEnv(t)
	net.disableErr = errors.New("locked")
	if rc := proxyRun([]string{"disable"}, env); rc != 1 {
		t.Fatalf("expected 1, got %d", rc)
	}
}

func TestProxyDisable_BadFlag(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"disable", "--bogus"}, env); rc != 2 {
		t.Fatalf("expected 2, got %d", rc)
	}
}

func TestProxyStatus_AllSections(t *testing.T) {
	t.Parallel()
	env, stdout, _, net, _, la := newProxyEnv(t)
	la.installed = true
	net.statusSnap = transparent.Snapshot{
		Services: []transparent.ServiceState{
			{Name: "Wi-Fi", HTTPSProxy: "127.0.0.1", HTTPSPort: "8990", HTTPSEnabled: true},
			{Name: "Ethernet", HTTPSEnabled: false},
		},
	}
	if rc := proxyRun([]string{"status"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	out := stdout.String()
	for _, want := range []string{"CA fingerprint", "Transparent runtime", "TLS profiles", "Daemon", "reachable", "Auto-start", "installed", "Wi-Fi", "Ethernet", "ON 127.0.0.1:8990", "off"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q in: %q", want, out)
		}
	}
}

func TestProxyStatus_DaemonDownRepairHint(t *testing.T) {
	t.Parallel()
	env, stdout, _, net, _, _ := newProxyEnv(t)
	env.HealthCheck = func(host, port string) error { return errors.New("connection refused") }
	net.statusSnap = transparent.Snapshot{
		Services: []transparent.ServiceState{
			{Name: "Wi-Fi", HTTPSProxy: "127.0.0.1", HTTPSPort: "8990", HTTPSEnabled: true},
		},
	}
	if rc := proxyRun([]string{"status"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	out := stdout.String()
	for _, want := range []string{"Daemon:", "unreachable", "slimference proxy disable"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q in: %q", want, out)
		}
	}
}

func TestPrintTransparentRuntimeStatus_ConfigLoadError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(cfgPath, []byte("[transparent]\ncert_cache_size=-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	var buf bytes.Buffer
	printTransparentRuntimeStatus(&buf)
	if !strings.Contains(buf.String(), "Transparent config:") {
		t.Fatalf("expected config error, got %q", buf.String())
	}
}

func TestPrintTransparentRuntimeStatus_ProfileError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "bad-profile.toml")
	if err := os.WriteFile(cfgPath, []byte("[transparent]\ndefault_tls_profile=\"missing\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	var buf bytes.Buffer
	printTransparentRuntimeStatus(&buf)
	if !strings.Contains(buf.String(), "TLS profiles:") || !strings.Contains(buf.String(), "missing") {
		t.Fatalf("expected profile error, got %q", buf.String())
	}
}

func TestPrintTransparentRuntimeStatus_Enabled(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "enabled.toml")
	body := []byte("[transparent]\nenabled=true\nintercept_hosts=[\"chatgpt.com\"]\ndefault_tls_profile=\"chromium_stable\"\n")
	if err := os.WriteFile(cfgPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIMFERENCE_CONFIG", cfgPath)
	var buf bytes.Buffer
	printTransparentRuntimeStatus(&buf)
	out := buf.String()
	if !strings.Contains(out, "Transparent runtime: on") || !strings.Contains(out, "chatgpt.com") {
		t.Fatalf("expected enabled transparent status, got %q", out)
	}
}

func TestPrintProxyDaemonStatus_NilHealthCheck(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printProxyDaemonStatus(&buf, nil, "127.0.0.1", "8990")
	if buf.Len() != 0 {
		t.Fatalf("nil health check should be silent, got %q", buf.String())
	}
}

func TestDefaultProxyHealthCheck(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	host, port := splitTestHostPort(t, server.URL)
	if err := defaultProxyHealthCheck(host, port); err != nil {
		t.Fatalf("health check: %v", err)
	}
}

func TestDefaultProxyHealthCheck_BadStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	host, port := splitTestHostPort(t, server.URL)
	if err := defaultProxyHealthCheck(host, port); err == nil {
		t.Fatal("500 health check must fail")
	}
}

func TestDefaultProxyHealthCheck_DialError(t *testing.T) {
	t.Parallel()
	if err := defaultProxyHealthCheck("127.0.0.1", "1"); err == nil {
		t.Fatal("closed health port must fail")
	}
}

func TestProxyStatus_LoadCAFailureSurfaces(t *testing.T) {
	t.Parallel()
	env, stdout, _, _, _, _ := newProxyEnv(t)
	env.LoadCA = func(dir string) (*tlsca.CA, error) { return nil, errors.New("ca gone") }
	if rc := proxyRun([]string{"status"}, env); rc != 0 {
		t.Fatalf("status should still rc=0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "ca gone") {
		t.Fatalf("expected CA error in render, got %q", stdout.String())
	}
}

func TestProxyStatus_NetworkSnapshotError(t *testing.T) {
	t.Parallel()
	env, stdout, _, net, _, _ := newProxyEnv(t)
	net.statusSnap = transparent.Snapshot{UnreachableErr: errors.New("not reachable")}
	if rc := proxyRun([]string{"status"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "not reachable") {
		t.Fatalf("expected network error in render, got %q", stdout.String())
	}
}

func TestProxyStatus_NoActiveServices(t *testing.T) {
	t.Parallel()
	env, stdout, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"status"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "none active") {
		t.Fatalf("expected none-active line, got %q", stdout.String())
	}
}

func TestProxyStatus_BadFlag(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"status", "--bogus"}, env); rc != 2 {
		t.Fatalf("expected 2, got %d", rc)
	}
}

func TestProxyUninstall_FullSuccess(t *testing.T) {
	t.Parallel()
	env, stdout, _, _, _, la := newProxyEnv(t)
	la.installed = true
	if rc := proxyRun([]string{"uninstall"}, env); rc != 0 {
		t.Fatalf("uninstall: rc=%d", rc)
	}
	if !strings.Contains(stdout.String(), "uninstall complete") {
		t.Fatalf("expected complete, got %q", stdout.String())
	}
}

func TestProxyUninstall_PartialFailuresWarn(t *testing.T) {
	t.Parallel()
	env, _, stderr, net, kc, la := newProxyEnv(t)
	net.disableErr = errors.New("svc fail")
	kc.uninstallErr = errors.New("kc fail")
	la.installed = true
	la.uninstallErr = errors.New("ld fail")
	if rc := proxyRun([]string{"uninstall"}, env); rc != 0 {
		t.Fatalf("uninstall must always succeed (warn-only), got %d", rc)
	}
	for _, msg := range []string{"disable failed", "keychain remove failed", "launchd remove failed"} {
		if !strings.Contains(stderr.String(), msg) {
			t.Errorf("missing warning %q in: %q", msg, stderr.String())
		}
	}
}

func TestProxyUninstall_LoadCAErrorSkipsKeychainStep(t *testing.T) {
	t.Parallel()
	env, _, stderr, _, _, _ := newProxyEnv(t)
	env.LoadCA = func(dir string) (*tlsca.CA, error) { return nil, errors.New("no ca") }
	if rc := proxyRun([]string{"uninstall"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if strings.Contains(stderr.String(), "keychain remove failed") {
		t.Fatalf("keychain step should be skipped when CA load fails, got %q", stderr.String())
	}
}

func TestProxyUninstall_BadFlag(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"uninstall", "--bogus"}, env); rc != 2 {
		t.Fatalf("expected 2, got %d", rc)
	}
}

func TestProxyUninstall_SystemScope(t *testing.T) {
	t.Parallel()
	env, _, _, _, _, _ := newProxyEnv(t)
	if rc := proxyRun([]string{"uninstall", "--system"}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
}

func TestParseProxyFlags_Defaults(t *testing.T) {
	t.Parallel()
	f, err := parseProxyFlags(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f.host != "127.0.0.1" || f.port != "8990" {
		t.Fatalf("defaults wrong: %+v", f)
	}
}

func TestParseProxyFlags_AllFlags(t *testing.T) {
	t.Parallel()
	f, err := parseProxyFlags([]string{"--yes", "--system", "--no-launchd", "--host=10.0.0.1", "--port=9000"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !f.yes || !f.system || !f.noLaunchd || f.host != "10.0.0.1" || f.port != "9000" {
		t.Fatalf("flags not parsed: %+v", f)
	}
}

func TestParseProxyFlags_PositionalRejected(t *testing.T) {
	t.Parallel()
	if _, err := parseProxyFlags([]string{"oops"}); err == nil {
		t.Fatal("expected error on positional")
	}
}

func TestDefaultPlistPath_DelegatesToTransparent(t *testing.T) {
	t.Parallel()
	got := DefaultPlistPath("/Users/test")
	want := transparent.DefaultPlistPath("/Users/test")
	if got != want {
		t.Fatalf("delegation broken: got %q want %q", got, want)
	}
}

func TestHandleProxyCmd_PublicEntrypoint(t *testing.T) {
	originalExit := exitFn
	defer func() { exitFn = originalExit }()
	captured := -1
	exitFn = func(code int) { captured = code }
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", "")
	handleProxyCmd(nil)
	if captured != 2 {
		t.Fatalf("expected exit 2 on no args, got %d", captured)
	}
}

func TestHandleSubcommand_ProxyCase(t *testing.T) {
	originalExit := exitFn
	defer func() { exitFn = originalExit }()
	captured := -1
	exitFn = func(code int) { captured = code }
	handleSubcommand([]string{"proxy"})
	if captured != 2 {
		t.Fatalf("expected exit 2 for proxy without subcommand, got %d", captured)
	}
}

func TestHandleSubcommand_CaptureSessionCase(t *testing.T) {
	originalExit := exitFn
	defer func() { exitFn = originalExit }()
	captured := -1
	exitFn = func(code int) { captured = code }
	handleSubcommand([]string{"capture-session", "--bogus"})
	if captured != 2 {
		t.Fatalf("expected exit 2 for bad capture-session flag, got %d", captured)
	}
}

func TestHandleProxyCmd_RealEnvSucceeds(t *testing.T) {
	originalExit := exitFn
	defer func() { exitFn = originalExit }()
	exitFn = func(code int) {
		if code != 0 {
			t.Fatalf("unexpected exit code %d", code)
		}
	}
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", t.TempDir())
	// status uses real subsystems but our HOME is fresh, so the
	// network manager will (a) probe `networksetup` if installed or
	// (b) error out. Both branches are acceptable - the test just
	// exercises the public handleProxyCmd path with the real env.
	handleProxyCmd([]string{"status"})
}

// rcFmt is a no-op formatter helper that keeps fmt referenced even if
// future test edits remove its only direct use; defensive against
// import-pruning when this file is large.
func rcFmt() string { return fmt.Sprintf("%s", "") }

func splitTestHostPort(t *testing.T, rawURL string) (string, string) {
	t.Helper()
	hostPort := strings.TrimPrefix(rawURL, "http://")
	parts := strings.Split(hostPort, ":")
	if len(parts) != 2 {
		t.Fatalf("unexpected server URL %q", rawURL)
	}
	return parts[0], parts[1]
}
