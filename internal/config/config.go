// Package config handles loading, validating, and accessing Slimference configuration.
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

// Config is the fully resolved configuration for Slimference.
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
	// SlimferenceCommand is the executable name or path embedded in hooks (default "slimference").
	SlimferenceCommand string `toml:"slimference_command"`
	// ExcludeCommands is a list of base command names (argv[0]) that are
	// never rewritten by "slimference rewrite", regardless of filter rules.
	// Corresponds to [hooks] exclude_commands in config.toml (spec+.md §4.9).
	ExcludeCommands []string `toml:"exclude_commands"`
}

// FilterConfig holds Layer-0 CLI paths (optional; env vars still override when set in the CLI).
type FilterConfig struct {
	// FilterDB is the SQLite path for filter_runs. Empty means use SLIMFERENCE_FILTER_DB or ~/.slimference/filter.db.
	FilterDB string `toml:"filter_db"`
	// TeeDir is the raw-output recovery directory. Empty means use SLIMFERENCE_TEE_DIR or ~/.slimference/tee.
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
	Layer1Enabled             bool          `toml:"layer1_enabled"`
	Layer2Enabled             bool          `toml:"layer2_enabled"`
	Layer3Enabled             bool          `toml:"layer3_enabled"`
	SlidingWindow             int           `toml:"sliding_window"`
	MinMessagesForCompression int           `toml:"min_messages_for_compression"`
	MinTokensForLayer2        int           `toml:"min_tokens_for_layer2"`
	StructureMinTokens        int           `toml:"structure_min_tokens"`
	StructureLanguages        []string      `toml:"structure_languages"`
	DedupSimilarityThreshold  float64       `toml:"dedup_similarity_threshold"`
	MiniMax                   MiniMaxConfig `toml:"minimax"`
	Summary                   SummaryConfig `toml:"summary"`
	Tuning                    TuningConfig  `toml:"tuning"`
}

// TuningConfig centralises behaviour-visible numerical knobs that would
// otherwise be scattered as literals across compression and summarization
// hot paths. Every knob has a safe default; overrides live in config.toml
// under [compression.tuning].
//
// Implementation-detail thresholds (e.g. MiniMax bullet-dedup fuzzy Jaccard
// at 0.70) are intentionally not exposed here - they do not change observable
// behaviour in a way operators would tune.
type TuningConfig struct {
	// IncrementalOverlapThreshold is the fallback fraction of the compressible
	// range that must already be covered by an existing summary to qualify
	// for an incremental update instead of a full rebuild. Used whenever the
	// IncrementalStaircase is empty. Default 0.70.
	IncrementalOverlapThreshold float64 `toml:"incremental_overlap_threshold"`
	// IncrementalStaircase is a staircase of thresholds keyed by conversation
	// size. The first step whose `msg_count_le` is >= the current conversation
	// length wins. Long conversations pay a proportionally larger cost for
	// full rebuilds, so a lower threshold is reasonable. If empty, the scalar
	// IncrementalOverlapThreshold is used uniformly. See T27.
	IncrementalStaircase []StaircaseStep `toml:"incremental_staircase"`
	// OverflowSlidingWindow is the aggressive sliding window used when the
	// upstream reports a context overflow (spec+.md §17.4). Default 2.
	OverflowSlidingWindow int `toml:"overflow_sliding_window"`
	// OverflowTargetRatio is the aggressive summary target ratio used during
	// overflow recover. Default 0.10.
	OverflowTargetRatio float64 `toml:"overflow_target_ratio"`
}

// StaircaseStep is one tier of a conversation-size-keyed threshold staircase.
// Steps are consulted in the configured order; the first step whose
// MsgCountLE is >= the conversation length wins.
type StaircaseStep struct {
	MsgCountLE int     `toml:"msg_count_le"`
	Threshold  float64 `toml:"threshold"`
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
//
// Mode (T36) selects a coherent operating profile on top of which the
// individual knobs (TargetRatio / MaxRatio / MinRatio / Strict) act as
// explicit overrides. Precedence: Mode sets the profile first, then any
// non-zero individual knob overrides its field. This resolves the
// "correctness vs aggressiveness vs latency" tension documented in
// docs/gap-analysis.md.
type SummaryConfig struct {
	// Mode is one of "strict" | "balanced" | "fast" | "" (backwards-compat:
	// empty means "respect the individual knobs only"). Env override:
	// SLIMFERENCE_L2_MODE.
	Mode        string  `toml:"mode"`
	TargetRatio float64 `toml:"target_ratio"`
	MaxRatio    float64 `toml:"max_ratio"`
	MinRatio    float64 `toml:"min_ratio"`
	Strict      bool    `toml:"strict"`
}

// CacheConfig controls response caching behaviour.
type CacheConfig struct {
	ResponseCacheMaxEntries       int `toml:"response_cache_max_entries"`
	ResponseCacheTTLSeconds       int `toml:"response_cache_ttl_seconds"`
	SummaryRefreshIntervalSeconds int `toml:"summary_refresh_interval_seconds"`
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
	Mode           string          `toml:"mode"` // "redact", "warn", "block", "off"
	CustomPatterns []CustomPattern `toml:"custom_patterns"`
	Allowlist      []string        `toml:"allowlist"`
}

// CustomPattern defines a user-provided secret detection pattern.
type CustomPattern struct {
	Name  string `toml:"name"`
	Regex string `toml:"regex"`
}

// AnalyticsConfig controls observability settings.
type AnalyticsConfig struct {
	Dashboard               bool   `toml:"dashboard"`
	LogDir                  string `toml:"log_dir"`
	DashboardRefreshSeconds int    `toml:"dashboard_refresh_seconds"`
	// GainUSDPerMillionTokens is optional: multiply tokens_saved_est / 1e6 for rough $ (slimference gain).
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
	return filepath.Join(expandHome("~"), ".slimference", "config.toml")
}

// Load reads and validates the configuration. It applies file -> env -> flag precedence.
// Missing config file is not an error; defaults are applied.
func Load() (*Config, error) {
	cfg := defaultsRaw()

	path := DefaultConfigPath()
	if p := os.Getenv("SLIMFERENCE_CONFIG"); p != "" {
		path = p
	}

	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnvOverrides(cfg)

	// T36: apply the selected Layer 2 operating mode. This fills any numeric
	// summary fields that the TOML or env did not explicitly set, giving the
	// mode profile the role of "coherent default bundle". Explicit positive
	// overrides from TOML/env win.
	if err := ApplyL2OperatingMode(&cfg.Compression.Summary, cfg.Compression.Summary.Mode); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// applyEnvOverrides reads SLIMFERENCE_* environment variables and overlays them on cfg.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SLIMFERENCE_LISTEN_ADDRESS"); v != "" {
		cfg.Proxy.ListenAddress = v
	}
	if v := envInt("SLIMFERENCE_LISTEN_PORT"); v > 0 {
		cfg.Proxy.ListenPort = v
	}
	if v := os.Getenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL"); v != "" {
		cfg.Upstream.Anthropic.BaseURL = v
	}
	if v := os.Getenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL"); v != "" {
		cfg.Upstream.OpenAI.BaseURL = v
	}
	if v := envInt("SLIMFERENCE_COMPRESSION_SLIDING_WINDOW"); v > 0 {
		cfg.Compression.SlidingWindow = v
	}
	if v := os.Getenv("SLIMFERENCE_SECRETS_MODE"); v != "" {
		cfg.Secrets.Mode = v
	}
	if v := os.Getenv("SLIMFERENCE_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("SLIMFERENCE_HOOK_SLIMFERENCE_COMMAND"); v != "" {
		cfg.Hooks.SlimferenceCommand = v
	}
	if v := os.Getenv("SLIMFERENCE_DEBUG_DECISIONS_LOG"); v != "" {
		cfg.Debug.DecisionsLog = v
	}
	if v := os.Getenv("SLIMFERENCE_DEBUG_LEVEL"); v != "" {
		cfg.Debug.Level = v
	}
	if v := os.Getenv("SLIMFERENCE_DEBUG_FORMAT"); v != "" {
		cfg.Debug.Format = v
	}
	if _, ok := os.LookupEnv("SLIMFERENCE_DEBUG_MAX_ENTRIES"); ok {
		cfg.Debug.MaxEntries = envInt("SLIMFERENCE_DEBUG_MAX_ENTRIES")
	}
	if _, ok := os.LookupEnv("SLIMFERENCE_FILTER_PASSTHROUGH_MAX_CHARS"); ok {
		cfg.Filter.PassthroughMaxChars = envInt("SLIMFERENCE_FILTER_PASSTHROUGH_MAX_CHARS")
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_GAIN_USD_PER_MILLION")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Analytics.GainUSDPerMillionTokens = f
		}
	}
	if v := os.Getenv("SLIMFERENCE_L2_MODE"); v != "" {
		cfg.Compression.Summary.Mode = v
	}
	if v := os.Getenv("SLIMFERENCE_MINIMAX_API_KEY"); v != "" {
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
	t := cfg.Compression.Tuning
	if t.IncrementalOverlapThreshold < 0 || t.IncrementalOverlapThreshold > 1 {
		return fmt.Errorf("compression.tuning.incremental_overlap_threshold must be 0.0-1.0")
	}
	prevLE := -1
	for i, step := range t.IncrementalStaircase {
		if step.Threshold < 0 || step.Threshold > 1 {
			return fmt.Errorf("compression.tuning.incremental_staircase[%d].threshold must be 0.0-1.0", i)
		}
		if step.MsgCountLE <= 0 {
			return fmt.Errorf("compression.tuning.incremental_staircase[%d].msg_count_le must be > 0", i)
		}
		if step.MsgCountLE <= prevLE {
			return fmt.Errorf("compression.tuning.incremental_staircase[%d].msg_count_le must be strictly increasing", i)
		}
		prevLE = step.MsgCountLE
	}
	if t.OverflowSlidingWindow < 1 {
		return fmt.Errorf("compression.tuning.overflow_sliding_window must be >= 1")
	}
	if t.OverflowTargetRatio < 0 || t.OverflowTargetRatio > 1 {
		return fmt.Errorf("compression.tuning.overflow_target_ratio must be 0.0-1.0")
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
