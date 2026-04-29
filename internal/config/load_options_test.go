package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFixture(t *testing.T, dir, name, port string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "[proxy]\nlisten_port = " + port + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveConfigPath_FlagWins(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFixture(t, dir, "custom.toml", "9001")

	t.Setenv("SLIMFERENCE_CONFIG", "/does/not/exist")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/ignored-xdg")

	info := ResolveConfigPath(LoadOptions{ExplicitPath: p})
	if info.Source != "flag" {
		t.Fatalf("source = %q, want flag", info.Source)
	}
	if info.ResolvedPath != p {
		t.Fatalf("resolved = %q, want %q", info.ResolvedPath, p)
	}
}

func TestResolveConfigPath_FlagMissing(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", "")
	info := ResolveConfigPath(LoadOptions{ExplicitPath: "/nope/foo.toml"})
	if info.Source != "flag_missing" {
		t.Fatalf("source = %q, want flag_missing", info.Source)
	}
}

func TestResolveConfigPath_EnvWins(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFixture(t, dir, "env.toml", "9002")

	t.Setenv("SLIMFERENCE_CONFIG", p)
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-notset")

	info := ResolveConfigPath(LoadOptions{})
	if info.Source != "env" {
		t.Fatalf("source = %q, want env", info.Source)
	}
	if info.ResolvedPath != p {
		t.Fatalf("resolved = %q, want %q", info.ResolvedPath, p)
	}
}

func TestResolveConfigPath_XDGWins(t *testing.T) {
	xdg := t.TempDir()
	p := writeConfigFixture(t, filepath.Join(xdg, "slimference"), "config.toml", "9003")

	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	// Simulate a user with no legacy config file.
	t.Setenv("HOME", t.TempDir())

	info := ResolveConfigPath(LoadOptions{})
	if info.Source != "xdg" {
		t.Fatalf("source = %q, want xdg", info.Source)
	}
	if info.ResolvedPath != p {
		t.Fatalf("resolved = %q, want %q", info.ResolvedPath, p)
	}
}

func TestResolveConfigPath_LegacyWinsAfterMissingXDG(t *testing.T) {
	home := t.TempDir()
	p := writeConfigFixture(t, filepath.Join(home, ".slimference"), "config.toml", "9004")

	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "missing-xdg"))
	t.Setenv("HOME", home)

	info := ResolveConfigPath(LoadOptions{})
	if info.Source != "legacy" {
		t.Fatalf("source = %q, want legacy", info.Source)
	}
	if info.ResolvedPath != p {
		t.Fatalf("resolved = %q, want %q", info.ResolvedPath, p)
	}
}

func TestResolveConfigPath_DefaultsFallback(t *testing.T) {
	t.Setenv("SLIMFERENCE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "no-xdg"))
	t.Setenv("HOME", t.TempDir())

	info := ResolveConfigPath(LoadOptions{})
	if info.Source != "defaults" {
		t.Fatalf("source = %q, want defaults", info.Source)
	}
	if info.ResolvedPath != "" {
		t.Fatalf("resolved = %q, want empty", info.ResolvedPath)
	}
	if len(info.Checked) != 3 {
		t.Fatalf("checked paths = %d, want 3 (env+xdg+legacy)", len(info.Checked))
	}
}

func TestLoadWithOptions_FlagMissingIsError(t *testing.T) {
	_, info, err := LoadWithOptions(LoadOptions{ExplicitPath: "/nope/config.toml"})
	if err == nil {
		t.Fatal("expected error for missing explicit path")
	}
	if info.Source != "flag_missing" {
		t.Fatalf("source = %q", info.Source)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %q, want 'not found'", err.Error())
	}
}

func TestLoadWithOptions_FlagReadsFile(t *testing.T) {
	dir := t.TempDir()
	p := writeConfigFixture(t, dir, "custom.toml", "9009")

	cfg, info, err := LoadWithOptions(LoadOptions{ExplicitPath: p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy.ListenPort != 9009 {
		t.Fatalf("port = %d, want 9009", cfg.Proxy.ListenPort)
	}
	if info.Source != "flag" {
		t.Fatalf("source = %q", info.Source)
	}
}

func TestXDGConfigPath_HonoursEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/explicit/xdg")
	got := XDGConfigPath()
	if got != "/explicit/xdg/slimference/config.toml" {
		t.Fatalf("xdg path = %q", got)
	}
}

func TestXDGConfigPath_DefaultHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	// Ensure expansion does not blow up on this platform.
	got := XDGConfigPath()
	if !strings.HasSuffix(got, filepath.Join(".config", "slimference", "config.toml")) {
		t.Fatalf("xdg path = %q", got)
	}
}

func TestLoad_BackwardsCompatible(t *testing.T) {
	// Legacy Load() still works and honours env var.
	dir := t.TempDir()
	p := writeConfigFixture(t, dir, "legacy.toml", "9010")
	t.Setenv("SLIMFERENCE_CONFIG", p)
	t.Setenv("XDG_CONFIG_HOME", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy.ListenPort != 9010 {
		t.Fatalf("port = %d, want 9010", cfg.Proxy.ListenPort)
	}
}
