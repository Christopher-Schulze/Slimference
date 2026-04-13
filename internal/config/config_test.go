package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDefaults_Valid verifies that the default configuration passes validate().
func TestDefaults_Valid(t *testing.T) {
	t.Parallel()

	cfg := Defaults()
	if err := validate(cfg); err != nil {
		t.Fatalf("validate(Defaults()) returned error: %v", err)
	}
}

func TestApplyEnvHooksDebug(t *testing.T) {
	t.Setenv("SLIMFERENCE_HOOK_SLIMFERENCE_COMMAND", "/opt/bin/slimference")
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "~/d/decisions.jsonl")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Hooks.SlimferenceCommand != "/opt/bin/slimference" {
		t.Fatalf("hooks command: %q", cfg.Hooks.SlimferenceCommand)
	}
	if cfg.Debug.DecisionsLog != "~/d/decisions.jsonl" {
		t.Fatalf("decisions log: %q", cfg.Debug.DecisionsLog)
	}
}

func TestApplyEnvPassthroughMaxChars(t *testing.T) {
	t.Setenv("SLIMFERENCE_FILTER_PASSTHROUGH_MAX_CHARS", "4096")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Filter.PassthroughMaxChars != 4096 {
		t.Fatalf("passthrough: %d", cfg.Filter.PassthroughMaxChars)
	}
}

func TestApplyEnvGainUsdPerMillion(t *testing.T) {
	t.Setenv("SLIMFERENCE_GAIN_USD_PER_MILLION", "2.5")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Analytics.GainUSDPerMillionTokens != 2.5 {
		t.Fatalf("gain usd: %v", cfg.Analytics.GainUSDPerMillionTokens)
	}
}

func TestExpandHomePath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := ExpandHomePath("~/foo/bar")
	want := fmt.Sprintf("%s/foo/bar", home)
	if got != want {
		t.Fatalf("ExpandHomePath: %q want %q", got, want)
	}
	if ExpandHomePath("/abs") != "/abs" {
		t.Fatal("absolute path changed")
	}
}

// TestLoadMissingFile verifies that Load() succeeds and returns defaults when no config file exists.
func TestLoadMissingFile(t *testing.T) {
	// Not parallel - uses t.Setenv.
	// Point to a guaranteed non-existent file.
	t.Setenv("SLIMFERENCE_CONFIG", "/tmp/slimference_test_nonexistent_file_xyzzy.toml")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with missing config file returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	defaults := Defaults()
	if cfg.Proxy.ListenPort != defaults.Proxy.ListenPort {
		t.Errorf("ListenPort = %d, want %d", cfg.Proxy.ListenPort, defaults.Proxy.ListenPort)
	}
	if cfg.Proxy.ListenAddress != defaults.Proxy.ListenAddress {
		t.Errorf("ListenAddress = %q, want %q", cfg.Proxy.ListenAddress, defaults.Proxy.ListenAddress)
	}
}

// TestListenAddr verifies that ListenAddr() formats the address correctly.
func TestListenAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		port    int
		want    string
	}{
		{
			name:    "loopback default port",
			address: "127.0.0.1",
			port:    8990,
			want:    "127.0.0.1:8990",
		},
		{
			name:    "all interfaces port 80",
			address: "0.0.0.0",
			port:    80,
			want:    "0.0.0.0:80",
		},
		{
			name:    "localhost high port",
			address: "localhost",
			port:    65000,
			want:    "localhost:65000",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := Defaults()
			cfg.Proxy.ListenAddress = tc.address
			cfg.Proxy.ListenPort = tc.port
			got := cfg.ListenAddr()
			if got != tc.want {
				t.Errorf("ListenAddr() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEnvOverrides verifies that SLIMFERENCE_LISTEN_PORT is applied by applyEnvOverrides.
func TestEnvOverrides(t *testing.T) {
	// Not parallel - modifies environment variables.
	t.Setenv("SLIMFERENCE_LISTEN_PORT", "9999")

	cfg := Defaults()
	applyEnvOverrides(cfg)

	if cfg.Proxy.ListenPort != 9999 {
		t.Errorf("ListenPort = %d after env override, want 9999", cfg.Proxy.ListenPort)
	}
}

// TestEnvOverrides_ListenAddress verifies SLIMFERENCE_LISTEN_ADDRESS override.
func TestEnvOverrides_ListenAddress(t *testing.T) {
	t.Setenv("SLIMFERENCE_LISTEN_ADDRESS", "0.0.0.0")

	cfg := Defaults()
	applyEnvOverrides(cfg)

	if cfg.Proxy.ListenAddress != "0.0.0.0" {
		t.Errorf("ListenAddress = %q after env override, want %q", cfg.Proxy.ListenAddress, "0.0.0.0")
	}
}

// TestEnvOverrides_SecretsMode verifies SLIMFERENCE_SECRETS_MODE override.
func TestEnvOverrides_SecretsMode(t *testing.T) {
	t.Setenv("SLIMFERENCE_SECRETS_MODE", "block")

	cfg := Defaults()
	applyEnvOverrides(cfg)

	if cfg.Secrets.Mode != "block" {
		t.Errorf("Secrets.Mode = %q after env override, want %q", cfg.Secrets.Mode, "block")
	}
}

// TestValidate_InvalidPort verifies that out-of-range port values fail validation.
func TestValidate_InvalidPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too high", 65536},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := Defaults()
			cfg.Proxy.ListenPort = tc.port
			if err := validate(cfg); err == nil {
				t.Errorf("validate() with port %d expected error, got nil", tc.port)
			}
		})
	}
}

// TestValidate_InvalidSecretsMode verifies that unknown mode strings fail validation.
func TestValidate_InvalidSecretsMode(t *testing.T) {
	t.Parallel()

	cfg := Defaults()
	cfg.Secrets.Mode = "invalid-mode"
	if err := validate(cfg); err == nil {
		t.Error("validate() with invalid secrets mode expected error, got nil")
	}
}

// TestListenURL verifies the ListenURL helper formats the full URL.
func TestListenURL(t *testing.T) {
	t.Parallel()

	cfg := Defaults()
	cfg.Proxy.ListenAddress = "127.0.0.1"
	cfg.Proxy.ListenPort = 8990
	want := "http://127.0.0.1:8990"
	if got := cfg.ListenURL(); got != want {
		t.Errorf("ListenURL() = %q, want %q", got, want)
	}
}

// TestDefaultConfigPath verifies that the default config path ends with the expected suffix.
func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()

	path := DefaultConfigPath()
	if path == "" {
		t.Fatal("DefaultConfigPath() returned empty string")
	}
	// Must end with config.toml
	suffix := ".slimference/config.toml"
	if len(path) < len(suffix) || path[len(path)-len(suffix):] != suffix {
		t.Errorf("DefaultConfigPath() = %q, expected suffix %q", path, suffix)
	}
}

// TestApplyEnvOverrides_MinimaxAPIKey covers the SLIMFERENCE_MINIMAX_API_KEY branch.
func TestApplyEnvOverrides_MinimaxAPIKey(t *testing.T) {
	t.Setenv("SLIMFERENCE_MINIMAX_API_KEY", "test-key-xyz")
	cfg := Defaults()
	applyEnvOverrides(cfg) // just must not panic; the value is discarded via _ = v
}

// TestApplyEnvOverrides_DebugFields covers the SLIMFERENCE_DEBUG_LEVEL, DEBUG_FORMAT, and DEBUG_MAX_ENTRIES branches.
func TestApplyEnvOverrides_DebugFields(t *testing.T) {
	t.Setenv("SLIMFERENCE_DEBUG_LEVEL", "verbose")
	t.Setenv("SLIMFERENCE_DEBUG_FORMAT", "json")
	t.Setenv("SLIMFERENCE_DEBUG_MAX_ENTRIES", "500")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Debug.Level != "verbose" {
		t.Errorf("Debug.Level: want verbose, got %q", cfg.Debug.Level)
	}
	if cfg.Debug.Format != "json" {
		t.Errorf("Debug.Format: want json, got %q", cfg.Debug.Format)
	}
	if cfg.Debug.MaxEntries != 500 {
		t.Errorf("Debug.MaxEntries: want 500, got %d", cfg.Debug.MaxEntries)
	}
}

// TestLoad_InvalidTOML verifies that a malformed config file returns an error.
func TestMiniMaxConfig_helpers(t *testing.T) {
	t.Setenv("TP_TEST_MINIMAX_KEY", "k9-secret")
	m := MiniMaxConfig{
		APIKeyEnv:               "TP_TEST_MINIMAX_KEY",
		ConnectTimeoutSeconds:   7,
		ResponseTimeoutSeconds:  42,
	}
	if got := m.APIKey(); got != "k9-secret" {
		t.Fatalf("APIKey: %q", got)
	}
	if m.ConnectTimeout() != 7*time.Second {
		t.Fatalf("ConnectTimeout: %v", m.ConnectTimeout())
	}
	if m.ResponseTimeout() != 42*time.Second {
		t.Fatalf("ResponseTimeout: %v", m.ResponseTimeout())
	}
}

func TestCacheConfig_ResponseCacheTTL(t *testing.T) {
	t.Parallel()
	c := CacheConfig{ResponseCacheTTLSeconds: 600}
	if c.ResponseCacheTTL() != 10*time.Minute {
		t.Fatalf("got %v", c.ResponseCacheTTL())
	}
}

func TestAnalyticsConfig_ResolvedLogDir(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	a := AnalyticsConfig{LogDir: "~/analytics/logs"}
	got := a.ResolvedLogDir()
	wantSuffix := "analytics/logs"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("ResolvedLogDir: %q want suffix %q", got, wantSuffix)
	}
	if !strings.HasPrefix(got, home) {
		t.Fatalf("expected under home %q, got %q", home, got)
	}
}

// TestDefaultTOML verifies that DefaultTOML returns a non-empty TOML string with [proxy] section.
func TestDefaultTOML(t *testing.T) {
	t.Parallel()
	out := DefaultTOML()
	if !strings.Contains(out, "[proxy]") {
		t.Fatalf("DefaultTOML() should contain [proxy] section, got: %q...", out[:min(100, len(out))])
	}
	if !strings.Contains(out, "listen_port") {
		t.Error("DefaultTOML() should contain listen_port")
	}
}

// TestValidate_SlidingWindowLessThanOne covers the sliding_window<1 validation error.
func TestValidate_SlidingWindowLessThanOne(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Compression.SlidingWindow = 0
	if err := validate(cfg); err == nil {
		t.Error("expected error for SlidingWindow=0")
	}
}

// TestValidate_DedupSimilarityThreshold covers the out-of-range threshold validation.
func TestValidate_DedupSimilarityThreshold(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Compression.DedupSimilarityThreshold = -0.1
	if err := validate(cfg); err == nil {
		t.Error("expected error for negative DedupSimilarityThreshold")
	}
	cfg2 := Defaults()
	cfg2.Compression.DedupSimilarityThreshold = 1.1
	if err := validate(cfg2); err == nil {
		t.Error("expected error for DedupSimilarityThreshold > 1")
	}
}

// TestValidate_PassthroughMaxCharsNegative covers the passthrough_max_chars<0 validation.
func TestValidate_PassthroughMaxCharsNegative(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Filter.PassthroughMaxChars = -1
	if err := validate(cfg); err == nil {
		t.Error("expected error for PassthroughMaxChars=-1")
	}
}

// TestValidate_GainUSDNegative covers the gain_usd_per_million_tokens<0 validation.
func TestValidate_GainUSDNegative(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Analytics.GainUSDPerMillionTokens = -1.0
	if err := validate(cfg); err == nil {
		t.Error("expected error for negative GainUSDPerMillionTokens")
	}
}

// TestApplyEnvOverrides_UpstreamAndCompression covers upstream URL and sliding window overrides.
func TestApplyEnvOverrides_UpstreamAndCompression(t *testing.T) {
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "https://custom.anthropic.com")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "https://custom.openai.com")
	t.Setenv("SLIMFERENCE_COMPRESSION_SLIDING_WINDOW", "10")
	t.Setenv("SLIMFERENCE_LOGGING_LEVEL", "debug")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Upstream.Anthropic.BaseURL != "https://custom.anthropic.com" {
		t.Errorf("Anthropic URL: %q", cfg.Upstream.Anthropic.BaseURL)
	}
	if cfg.Upstream.OpenAI.BaseURL != "https://custom.openai.com" {
		t.Errorf("OpenAI URL: %q", cfg.Upstream.OpenAI.BaseURL)
	}
	if cfg.Compression.SlidingWindow != 10 {
		t.Errorf("SlidingWindow: %d", cfg.Compression.SlidingWindow)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level: %q", cfg.Logging.Level)
	}
}

// TestApplyEnvOverrides_InvalidGainFloat covers the parseFloat error branch in applyEnvOverrides.
func TestApplyEnvOverrides_InvalidGainFloat(t *testing.T) {
	t.Setenv("SLIMFERENCE_GAIN_USD_PER_MILLION", "not-a-float")
	cfg := Defaults()
	original := cfg.Analytics.GainUSDPerMillionTokens
	applyEnvOverrides(cfg)
	if cfg.Analytics.GainUSDPerMillionTokens != original {
		t.Error("gain should be unchanged for invalid float")
	}
}

// TestLoad_ValidateFails covers the validate error path in Load().
func TestLoad_ValidateFails(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "config-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	// Valid TOML syntax but invalid config (port=0 fails validation).
	if _, err := fmt.Fprint(f, "[proxy]\nlisten_port = 0\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Setenv("SLIMFERENCE_CONFIG", f.Name())
	_, err = Load()
	if err == nil {
		t.Fatal("Load() with invalid config expected error, got nil")
	}
}

// min is a local helper for config_test.go.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestLoad_InvalidTOML(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "config-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(f, "[proxy\nbad toml = "); err != nil {
		t.Fatal(err)
	}
	f.Close()

	t.Setenv("SLIMFERENCE_CONFIG", f.Name())

	_, err = Load()
	if err == nil {
		t.Fatal("Load() with invalid TOML expected error, got nil")
	}
}

// TestExpandHome covers both branches of expandHome.
func TestExpandHome(t *testing.T) {
	t.Parallel()
	// Non-tilde path → returned as-is
	got := expandHome("/absolute/path")
	if got != "/absolute/path" {
		t.Errorf("absolute path: want /absolute/path, got %q", got)
	}
	// Tilde path → expanded to home directory
	got2 := expandHome("~/myfile.txt")
	if !strings.HasSuffix(got2, "/myfile.txt") || got2 == "~/myfile.txt" {
		t.Errorf("tilde path: want expanded path, got %q", got2)
	}
	// Relative path without ~ → returned as-is
	got3 := expandHome("relative/path")
	if got3 != "relative/path" {
		t.Errorf("relative path: want relative/path, got %q", got3)
	}
}

// TestExpandHome_homeDirError covers the os.UserHomeDir error path in expandHome.
func TestExpandHome_homeDirError(t *testing.T) {
	// Not parallel: mutates package-level var userHomeDirFunc.
	old := userHomeDirFunc
	userHomeDirFunc = func() (string, error) { return "", errors.New("no home") }
	defer func() { userHomeDirFunc = old }()

	// Tilde path with failing UserHomeDir → returned as-is.
	got := expandHome("~/myfile.txt")
	if got != "~/myfile.txt" {
		t.Errorf("want path unchanged on error, got %q", got)
	}
}
