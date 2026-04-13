// Package config handles loading, validating, and accessing TokenProxy configuration.
// Priority order: CLI flags > environment variables > config file > defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the fully resolved configuration for TokenProxy.
type Config struct {
	Proxy       ProxyConfig       `toml:"proxy"`
	Upstream    UpstreamConfig    `toml:"upstream"`
	Compression CompressionConfig `toml:"compression"`
	Cache       CacheConfig       `toml:"cache"`
	Usage       UsageConfig       `toml:"usage"`
	Secrets     SecretsConfig     `toml:"secrets"`
	Analytics   AnalyticsConfig   `toml:"analytics"`
	Logging     LoggingConfig     `toml:"logging"`
	Filter      FilterConfig      `toml:"filter"`
	Hooks       HooksConfig       `toml:"hooks"`
	Debug       DebugConfig       `toml:"debug"`
}

// DebugConfig holds debug and observability settings (spec+.md §13).
type DebugConfig struct {
	// DecisionsLog is the optional path for the proxy decision JSONL log (one line per request).
	DecisionsLog string `toml:"decisions_log"`
	// Level controls debug verbosity: "trace", "debug", "info", "warn", "error".
	Level string `toml:"level"`
	// Format controls debug output format: "jsonl" (AI agent) or "text" (human).
	Format string `toml:"format"`
	// MaxEntries is the in-memory ring buffer capacity for decision summaries (default 100).
	MaxEntries int `toml:"max_entries"`
}

// HooksConfig affects generated hook scripts and AGENTS.md snippets.
type HooksConfig struct {
	// TokenproxyCommand is the executable name or path embedded in hooks (default "tokenproxy").
	TokenproxyCommand string `toml:"tokenproxy_command"`
}

// FilterConfig holds Layer-0 CLI paths (optional; env vars still override when set in the CLI).
type FilterConfig struct {
	// FilterDB is the SQLite path for filter_runs. Empty means use TOKENPROXY_FILTER_DB or ~/.tokenproxy/filter.db.
	FilterDB string `toml:"filter_db"`
	// TeeDir is the raw-output recovery directory. Empty means use TOKENPROXY_TEE_DIR or ~/.tokenproxy/tee.
	TeeDir string `toml:"tee_dir"`
	// DenyPatterns are extra regexes (RE2) matched against the full command line; invalid entries are ignored at runtime.
	DenyPatterns []string `toml:"deny_patterns"`
	// PassthroughMaxChars caps filtered stdout length in Unicode code points after built-in/TOML (spec+.md §4.6).
	// 0 means no limit. Default from Defaults() is 2000.
	PassthroughMaxChars int `toml:"passthrough_max_chars"`
}

// ProxyConfig holds network listener settings.
type ProxyConfig struct {
	ListenAddress string `toml:"listen_address"`
	ListenPort    int    `toml:"listen_port"`
	IPv6          bool   `toml:"ipv6"`
}

// UpstreamConfig holds upstream API base URLs.
type UpstreamConfig struct {
	Anthropic ProviderUpstream `toml:"anthropic"`
	OpenAI    ProviderUpstream `toml:"openai"`
}

// ProviderUpstream holds the base URL for a single upstream provider.
type ProviderUpstream struct {
	BaseURL string `toml:"base_url"`
}

// CompressionConfig controls the multi-layer compression pipeline.
type CompressionConfig struct {
	Layer1Enabled              bool           `toml:"layer1_enabled"`
	Layer2Enabled              bool           `toml:"layer2_enabled"`
	Layer3Enabled              bool           `toml:"layer3_enabled"`
	SlidingWindow              int            `toml:"sliding_window"`
	MinMessagesForCompression  int            `toml:"min_messages_for_compression"`
	MinTokensForLayer2         int            `toml:"min_tokens_for_layer2"`
	StructureMinTokens         int            `toml:"structure_min_tokens"`
	StructureLanguages         []string       `toml:"structure_languages"`
	DedupSimilarityThreshold   float64        `toml:"dedup_similarity_threshold"`
	MiniMax                    MiniMaxConfig  `toml:"minimax"`
	Summary                    SummaryConfig  `toml:"summary"`
}

// MiniMaxConfig holds settings for the MiniMax summarization API.
type MiniMaxConfig struct {
	BaseURL                string  `toml:"base_url"`
	APIKeyEnv              string  `toml:"api_key_env"`
	Model                  string  `toml:"model"`
	Temperature            float64 `toml:"temperature"`
	MaxRetries             int     `toml:"max_retries"`
	ConnectTimeoutSeconds  int     `toml:"connect_timeout_seconds"`
	ResponseTimeoutSeconds int     `toml:"response_timeout_seconds"`
	RateLimitRPM           int     `toml:"rate_limit_rpm"`
}

// APIKey resolves the MiniMax API key from the configured environment variable.
func (m MiniMaxConfig) APIKey() string {
	return os.Getenv(m.APIKeyEnv)
}

// ConnectTimeout returns the connect timeout as a duration.
func (m MiniMaxConfig) ConnectTimeout() time.Duration {
	return time.Duration(m.ConnectTimeoutSeconds) * time.Second
}

// ResponseTimeout returns the response timeout as a duration.
func (m MiniMaxConfig) ResponseTimeout() time.Duration {
	return time.Duration(m.ResponseTimeoutSeconds) * time.Second
}

// SummaryConfig controls quality thresholds for MiniMax summaries.
type SummaryConfig struct {
	TargetRatio float64 `toml:"target_ratio"`
	MaxRatio    float64 `toml:"max_ratio"`
	MinRatio    float64 `toml:"min_ratio"`
}

// CacheConfig controls response caching behaviour.
type CacheConfig struct {
	ResponseCacheMaxEntries        int `toml:"response_cache_max_entries"`
	ResponseCacheTTLSeconds        int `toml:"response_cache_ttl_seconds"`
	SummaryRefreshIntervalSeconds  int `toml:"summary_refresh_interval_seconds"`
}

// ResponseCacheTTL returns the TTL as a duration.
func (c CacheConfig) ResponseCacheTTL() time.Duration {
	return time.Duration(c.ResponseCacheTTLSeconds) * time.Second
}

// UsageConfig controls usage estimation parameters.
type UsageConfig struct {
	EstimatedPrefillSpeed int `toml:"estimated_prefill_speed"` // tokens/second
}

// SecretsConfig controls secret detection behaviour.
type SecretsConfig struct {
	Mode           string         `toml:"mode"` // "redact", "warn", "block", "off"
	CustomPatterns []CustomPattern `toml:"custom_patterns"`
	Allowlist      []string       `toml:"allowlist"`
}

// CustomPattern defines a user-provided secret detection pattern.
type CustomPattern struct {
	Name  string `toml:"name"`
	Regex string `toml:"regex"`
}

// AnalyticsConfig controls observability settings.
type AnalyticsConfig struct {
	Dashboard              bool   `toml:"dashboard"`
	LogDir                 string `toml:"log_dir"`
	DashboardRefreshSeconds int   `toml:"dashboard_refresh_seconds"`
	// GainUSDPerMillionTokens is optional: multiply tokens_saved_est / 1e6 for rough $ (tokenproxy gain).
	GainUSDPerMillionTokens float64 `toml:"gain_usd_per_million_tokens"`
}

// LogDir returns the analytics log directory with ~ expanded.
func (a AnalyticsConfig) ResolvedLogDir() string {
	return expandHome(a.LogDir)
}

// LoggingConfig controls structured logging settings.
type LoggingConfig struct {
	Level  string `toml:"level"`  // "debug", "info", "warn", "error"
	Format string `toml:"format"` // "text", "json"
	File   string `toml:"file"`   // empty = stderr only
}

// DefaultConfigPath returns the default path to the config file.
func DefaultConfigPath() string {
	return filepath.Join(expandHome("~"), ".tokenproxy", "config.toml")
}

// Load reads and validates the configuration. It applies file -> env -> flag precedence.
// Missing config file is not an error; defaults are applied.
func Load() (*Config, error) {
	cfg := Defaults()

	path := DefaultConfigPath()
	if p := os.Getenv("TOKENPROXY_CONFIG"); p != "" {
		path = p
	}

	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnvOverrides(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// applyEnvOverrides reads TOKENPROXY_* environment variables and overlays them on cfg.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("TOKENPROXY_LISTEN_ADDRESS"); v != "" {
		cfg.Proxy.ListenAddress = v
	}
	if v := envInt("TOKENPROXY_LISTEN_PORT"); v > 0 {
		cfg.Proxy.ListenPort = v
	}
	if v := os.Getenv("TOKENPROXY_UPSTREAM_ANTHROPIC_BASE_URL"); v != "" {
		cfg.Upstream.Anthropic.BaseURL = v
	}
	if v := os.Getenv("TOKENPROXY_UPSTREAM_OPENAI_BASE_URL"); v != "" {
		cfg.Upstream.OpenAI.BaseURL = v
	}
	if v := envInt("TOKENPROXY_COMPRESSION_SLIDING_WINDOW"); v > 0 {
		cfg.Compression.SlidingWindow = v
	}
	if v := os.Getenv("TOKENPROXY_SECRETS_MODE"); v != "" {
		cfg.Secrets.Mode = v
	}
	if v := os.Getenv("TOKENPROXY_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("TOKENPROXY_HOOK_TOKENPROXY_COMMAND"); v != "" {
		cfg.Hooks.TokenproxyCommand = v
	}
	if v := os.Getenv("TOKENPROXY_DEBUG_DECISIONS_LOG"); v != "" {
		cfg.Debug.DecisionsLog = v
	}
	if v := os.Getenv("TOKENPROXY_DEBUG_LEVEL"); v != "" {
		cfg.Debug.Level = v
	}
	if v := os.Getenv("TOKENPROXY_DEBUG_FORMAT"); v != "" {
		cfg.Debug.Format = v
	}
	if _, ok := os.LookupEnv("TOKENPROXY_DEBUG_MAX_ENTRIES"); ok {
		cfg.Debug.MaxEntries = envInt("TOKENPROXY_DEBUG_MAX_ENTRIES")
	}
	if _, ok := os.LookupEnv("TOKENPROXY_FILTER_PASSTHROUGH_MAX_CHARS"); ok {
		cfg.Filter.PassthroughMaxChars = envInt("TOKENPROXY_FILTER_PASSTHROUGH_MAX_CHARS")
	}
	if v := strings.TrimSpace(os.Getenv("TOKENPROXY_GAIN_USD_PER_MILLION")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Analytics.GainUSDPerMillionTokens = f
		}
	}
	if v := os.Getenv("TOKENPROXY_MINIMAX_API_KEY"); v != "" {
		// Store directly in env var that MiniMaxConfig.APIKey() reads
		// The env var name comes from config, but allow a direct override too.
		_ = v // already readable via os.Getenv(cfg.Compression.MiniMax.APIKeyEnv)
	}
}

// validate checks that configuration values are within acceptable ranges.
func validate(cfg *Config) error {
	if cfg.Proxy.ListenPort < 1 || cfg.Proxy.ListenPort > 65535 {
		return fmt.Errorf("proxy.listen_port must be 1-65535, got %d", cfg.Proxy.ListenPort)
	}
	if cfg.Compression.SlidingWindow < 1 {
		return fmt.Errorf("compression.sliding_window must be >= 1")
	}
	if cfg.Compression.DedupSimilarityThreshold < 0 || cfg.Compression.DedupSimilarityThreshold > 1 {
		return fmt.Errorf("compression.dedup_similarity_threshold must be 0.0-1.0")
	}
	mode := cfg.Secrets.Mode
	if mode != "redact" && mode != "warn" && mode != "block" && mode != "off" {
		return fmt.Errorf("secrets.mode must be redact/warn/block/off, got %q", mode)
	}
	if cfg.Filter.PassthroughMaxChars < 0 {
		return fmt.Errorf("filter.passthrough_max_chars must be >= 0, got %d", cfg.Filter.PassthroughMaxChars)
	}
	if cfg.Analytics.GainUSDPerMillionTokens < 0 {
		return fmt.Errorf("analytics.gain_usd_per_million_tokens must be >= 0, got %v", cfg.Analytics.GainUSDPerMillionTokens)
	}
	return nil
}

// ListenAddr returns the full listen address string for net.Listen.
func (cfg *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", cfg.Proxy.ListenAddress, cfg.Proxy.ListenPort)
}

// ListenURL returns the full URL the proxy listens on.
func (cfg *Config) ListenURL() string {
	return fmt.Sprintf("http://%s:%d", cfg.Proxy.ListenAddress, cfg.Proxy.ListenPort)
}

// ExpandHomePath expands a leading ~/ to the user's home directory.
func ExpandHomePath(path string) string {
	return expandHome(path)
}

// userHomeDirFunc is set to os.UserHomeDir; replaced in tests to inject errors.
var userHomeDirFunc = os.UserHomeDir

// expandHome expands a leading ~ to the user home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := userHomeDirFunc()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func envInt(key string) int {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	var n int
	fmt.Sscanf(v, "%d", &n)
	return n
}
