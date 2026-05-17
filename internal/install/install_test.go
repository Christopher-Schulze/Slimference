package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubKeychainRunner records nothing and always succeeds.
func stubKeychainRunner(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, nil
}

func TestPlanFullComposition(t *testing.T) {
	home := t.TempDir()
	plan, err := Plan(Options{
		Home:           home,
		BinaryPath:     "/usr/local/bin/slimference",
		KeychainRunner: stubKeychainRunner,
		SkipLoad:       true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan == nil {
		t.Fatal("nil plan")
	}
	insp := plan.Inspect(context.Background())
	want := []string{
		"ca.generate",
		"ca.keychain",
		"launchd.install",
		"hooks.codex",
		"notice.codex",
	}
	if len(insp.Order) != len(want) {
		t.Fatalf("expected %d steps, got %d (%v)", len(want), len(insp.Order), insp.Order)
	}
	for i, name := range want {
		if insp.Order[i] != name {
			t.Errorf("step %d: got %q want %q", i, insp.Order[i], name)
		}
	}
}

func TestPlanWithClaudeHooksFlagIsParkedNoOp(t *testing.T) {
	home := t.TempDir()
	plan, err := Plan(Options{
		Home:            home,
		BinaryPath:      "/usr/local/bin/slimference",
		KeychainRunner:  stubKeychainRunner,
		SkipLoad:        true,
		WithClaudeHooks: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	insp := plan.Inspect(context.Background())
	for _, name := range insp.Order {
		if strings.Contains(name, "claude") {
			t.Fatalf("Claude Code is parked; plan must not include %q (order=%v)", name, insp.Order)
		}
	}
}

func TestPlanSkipHooks(t *testing.T) {
	plan, err := Plan(Options{
		Home:           t.TempDir(),
		BinaryPath:     "/usr/local/bin/slimference",
		KeychainRunner: stubKeychainRunner,
		SkipLoad:       true,
		SkipHooks:      true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	insp := plan.Inspect(context.Background())
	if len(insp.Order) != 3 {
		t.Fatalf("expected 3 steps with SkipHooks (notices ride hooks), got %d (%v)", len(insp.Order), insp.Order)
	}
	for _, name := range insp.Order {
		if name == "hooks.codex" || name == "hooks.claude" ||
			name == "notice.codex" || name == "notice.claude" {
			t.Errorf("unexpected step %q with SkipHooks", name)
		}
	}
}

func TestPlanSkipAutoStart(t *testing.T) {
	plan, err := Plan(Options{
		Home:           t.TempDir(),
		BinaryPath:     "/usr/local/bin/slimference",
		KeychainRunner: stubKeychainRunner,
		SkipAutoStart:  true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, n := range plan.Inspect(context.Background()).Order {
		if n == "launchd.install" {
			t.Error("launchd.install should be absent with SkipAutoStart")
		}
	}
}

func TestPlanSkipKeychain(t *testing.T) {
	plan, err := Plan(Options{
		Home:         t.TempDir(),
		BinaryPath:   "/usr/local/bin/slimference",
		SkipKeychain: true,
		SkipLoad:     true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, n := range plan.Inspect(context.Background()).Order {
		if n == "ca.keychain" {
			t.Error("ca.keychain should be absent with SkipKeychain")
		}
	}
}

func TestPlanMinimal(t *testing.T) {
	plan, err := Plan(Options{
		Home:          t.TempDir(),
		BinaryPath:    "/usr/local/bin/slimference",
		SkipHooks:     true,
		SkipAutoStart: true,
		SkipKeychain:  true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	insp := plan.Inspect(context.Background())
	if len(insp.Order) != 1 {
		t.Fatalf("expected 1 step (CA only), got %d (%v)", len(insp.Order), insp.Order)
	}
	if insp.Order[0] != "ca.generate" {
		t.Errorf("got %q want ca.generate", insp.Order[0])
	}
}

func TestPlanApplyReverseRoundTrip(t *testing.T) {
	home := t.TempDir()
	plan, err := Plan(Options{
		Home:           home,
		BinaryPath:     "/usr/local/bin/slimference",
		KeychainRunner: stubKeychainRunner,
		SkipLoad:       true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res := plan.Apply(context.Background()); res.Err != nil {
		t.Fatalf("Apply: %v (applied=%v)", res.Err, res.Applied)
	}
	if _, err := os.Stat(filepath.Join(home, ".slimference", "ca")); err != nil {
		t.Errorf("CA dir missing after Apply: %v", err)
	}
	if res := plan.Reverse(context.Background()); res.Err() != nil {
		t.Fatalf("Reverse: %v", res.Err())
	}
}

func TestHostsPlanShape(t *testing.T) {
	hp, err := HostsPlan(HostsOptions{
		Home:      t.TempDir(),
		HostsPath: filepath.Join(t.TempDir(), "hosts"),
	})
	if err != nil {
		t.Fatalf("HostsPlan: %v", err)
	}
	insp := hp.Inspect(context.Background())
	if len(insp.Order) != 1 {
		t.Fatalf("expected 1 step, got %d", len(insp.Order))
	}
	if insp.Order[0] != "hosts.patch" {
		t.Errorf("got %q want hosts.patch", insp.Order[0])
	}
}

func TestHostsPlanCustomTargetsAndAddress(t *testing.T) {
	hp, err := HostsPlan(HostsOptions{
		Home:      t.TempDir(),
		HostsPath: filepath.Join(t.TempDir(), "hosts"),
		Targets:   []string{"example.com"},
		Address:   "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("HostsPlan: %v", err)
	}
	if hp == nil {
		t.Fatal("nil plan")
	}
}

func TestDefaultHostsTargetsList(t *testing.T) {
	got := DefaultHostsTargets()
	want := map[string]bool{
		"chatgpt.com":    true,
		"api.openai.com": true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d targets, want %d", len(got), len(want))
	}
	for _, h := range got {
		if !want[h] {
			t.Errorf("unexpected target %q", h)
		}
	}
}

func TestResolveHomeExplicit(t *testing.T) {
	got, err := resolveHome("/explicit/home")
	if err != nil {
		t.Fatalf("resolveHome: %v", err)
	}
	if got != "/explicit/home" {
		t.Errorf("got %q want /explicit/home", got)
	}
}

func TestResolveHomeDefaultAndMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := resolveHome("")
	if err != nil {
		t.Fatalf("resolveHome default: %v", err)
	}
	if got != home {
		t.Fatalf("resolveHome default=%q want %q", got, home)
	}

	t.Setenv("HOME", "")
	if _, err := resolveHome(""); err == nil {
		t.Fatal("expected unresolved HOME error")
	}
}

func TestResolveBinaryExplicit(t *testing.T) {
	got, err := resolveBinary("/explicit/bin")
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != "/explicit/bin" {
		t.Errorf("got %q want /explicit/bin", got)
	}
}

func TestResolveBinaryDefault(t *testing.T) {
	got, err := resolveBinary("")
	if err != nil {
		t.Fatalf("resolveBinary default: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("resolveBinary default should be absolute, got %q", got)
	}
}

func TestPlanAndHostsPlanDefaultResolvers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plan, err := Plan(Options{
		SkipHooks:     true,
		SkipAutoStart: true,
		SkipKeychain:  true,
	})
	if err != nil {
		t.Fatalf("Plan with default resolvers: %v", err)
	}
	if len(plan.Inspect(context.Background()).Order) != 1 {
		t.Fatalf("unexpected default plan shape: %v", plan.Inspect(context.Background()).Order)
	}
	hp, err := HostsPlan(HostsOptions{HostsPath: filepath.Join(t.TempDir(), "hosts")})
	if err != nil {
		t.Fatalf("HostsPlan with default home: %v", err)
	}
	if hp == nil {
		t.Fatal("nil hosts plan")
	}
}

func TestPlanResolverErrors(t *testing.T) {
	prevHome := userHomeDirFn
	prevExe := executableFn
	t.Cleanup(func() {
		userHomeDirFn = prevHome
		executableFn = prevExe
	})

	userHomeDirFn = func() (string, error) { return "", errors.New("home boom") }
	if _, err := Plan(Options{}); err == nil || !strings.Contains(err.Error(), "home boom") {
		t.Fatalf("Plan home error=%v", err)
	}
	if _, err := HostsPlan(HostsOptions{}); err == nil || !strings.Contains(err.Error(), "home boom") {
		t.Fatalf("HostsPlan home error=%v", err)
	}

	userHomeDirFn = func() (string, error) { return t.TempDir(), nil }
	executableFn = func() (string, error) { return "", errors.New("exe boom") }
	if _, err := Plan(Options{}); err == nil || !strings.Contains(err.Error(), "exe boom") {
		t.Fatalf("Plan binary error=%v", err)
	}
}
