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
	// AnthropicVersions lists the `anthropic-version` header values for
	// which the full Layer 1 / Layer 2 pipeline is trusted. Requests
	// carrying an unknown version downgrade to conservative mode (T62).
	// An empty list means "trust everything" for backwards compatibility.
	AnthropicVersions []string `toml:"anthropic_versions"`
	// AnthropicUnknownBehavior decides how unknown-version requests are
	// handled: "conservative" skips L1+L2, "passthrough" runs no
	// compression at all, "full" trusts the unknown version. Default
	// "conservative". Case-insensitive; empty string means default.
	AnthropicUnknownBehavior string `toml:"anthropic_unknown_behavior"`
	// DrainTimeoutSeconds caps how long Shutdown waits for in-flight
	// requests to finish before forcing exit. Zero means "rely on the
	// caller-provided context only" (legacy behaviour). T85.
	DrainTimeoutSeconds int `toml:"drain_timeout_seconds"`
	// ServerStateEnabled gates the T78 server-side state lever: when on,
	// the proxy rewrites follow-up requests to OpenAI Responses /
	// CodexChatGPT to use `previous_response_id` instead of resending
	// the prefix. Default off; flip per environment after live verify.
	ServerStateEnabled bool `toml:"server_state_enabled"`
}

// UpstreamConfig holds upstream API base URLs.
type UpstreamConfig struct {
	Anthropic    ProviderUpstream `toml:"anthropic"`
	OpenAI       ProviderUpstream `toml:"openai"`
	CodexChatGPT ProviderUpstream `toml:"codex_chatgpt"`
}

// ProviderUpstream holds the base URL for a single upstream provider.
type ProviderUpstream struct {
	BaseURL string `toml:"base_url"`
}

// CompressionConfig controls the multi-layer compression pipeline.
type CompressionConfig struct {
	Layer1Enabled             bool `toml:"layer1_enabled"`
	Layer2Enabled             bool `toml:"layer2_enabled"`
	Layer3Enabled             bool `toml:"layer3_enabled"`
	SlidingWindow             int  `toml:"sliding_window"`
	MinMessagesForCompression int  `toml:"min_messages_for_compression"`
	MinTokensForLayer2        int  `toml:"min_tokens_for_layer2"`
	// Layer2LatencyBudgetMs (T54) caps the per-request time Slimference is
	// willing to spend on Layer 2. If the EMA of past MiniMax latencies
	// multiplied by Layer2LatencyProjectionMultiplier exceeds this budget,
	// L2 is skipped for the current request even if other preconditions
	// are met. 0 disables the guard (legacy behaviour). Default 0.
	Layer2LatencyBudgetMs int `toml:"layer2_latency_budget_ms"`
	// Layer2LatencyProjectionMultiplier is the safety margin applied to the
	// EMA when computing the projection. Default 1.2.
	Layer2LatencyProjectionMultiplier float64 `toml:"layer2_latency_projection_multiplier"`
	// Layer2LatencyEMAAlpha is the exponential-moving-average weight on
	// new observations. Default 0.2.
	Layer2LatencyEMAAlpha    float64       `toml:"layer2_latency_ema_alpha"`
	StructureMinTokens       int           `toml:"structure_min_tokens"`
	StructureLanguages       []string      `toml:"structure_languages"`
	DedupSimilarityThreshold float64       `toml:"dedup_similarity_threshold"`
	MiniMax                  MiniMaxConfig `toml:"minimax"`
	Summary                  SummaryConfig `toml:"summary"`
	Tuning                   TuningConfig  `toml:"tuning"`
	// PromptOverridePath (T86) points at a file whose contents replace
	// the compiled-in MiniMax system prompt header. Empty disables the
	// override. The file's first non-empty line may carry a
	// `# version: <tag>` annotation that is recorded in
	// /admin/status.summarization.active_prompt_version.
	PromptOverridePath string `toml:"prompt_override_path"`
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
	// StructureInWindow enables Layer 1 structure extraction (signature-only
	// compression) for tool_result blocks inside the sliding window. Default
	// false. When enabled, large code blocks older than the most recent
	// message can have their bodies elided once they exceed
	// StructureInWindowMinTokens.
	StructureInWindow bool `toml:"structure_in_window"`
	// StructureInWindowMinTokens is the minimum estimated token count for an
	// in-window tool_result block to be eligible for structure extraction.
	// Default 1500 (conservative: half of a typical file read).
	StructureInWindowMinTokens int `toml:"structure_in_window_min_tokens"`
	// LoopDetection enables T37: when 4+ consecutive user messages share
	// >=0.75 Jaccard word similarity, a synthetic nudge is prepended to
	// the final user message so the model can break out of a retry loop.
	// Default false (opt-in).
	LoopDetection bool `toml:"loop_detection"`
	// StructurePreview enables T38: large tool_result blocks (>=4 KB) with
	// JSON / path-list / ASCII-table shape are replaced with a compact,
	// shape-aware preview when strictly shorter. Default false (T74) until
	// preview recovery is fully reversible via local archive.
	StructurePreview bool `toml:"structure_preview"`
	// CoordinatorEnabled (T100) gates the L1/L2 cross-direction
	// coordinator: when true and Layer 2 is enabled, Layer 1 skips
	// heavy sub-layers on the prefix that L2 will summarise. Cheap
	// passes (ANSI strip, JSON compact) always run. Default off until
	// real-corpus data validates the trade-off.
	CoordinatorEnabled bool `toml:"coordinator_enabled"`
	// CoordinatorParallel (T104) opts in to goroutine fan-out across
	// independent L1 sub-layers. Race-prone when off; off by default
	// until benchmark evidence shows the sequential pipeline is the
	// bottleneck on real bodies.
	CoordinatorParallel bool `toml:"coordinator_parallel"`
	// DedupStaircase lowers the MinHash/LSH Jaccard threshold as the
	// conversation grows. Long sessions accumulate more near-duplicate tool
	// output; a relaxed threshold catches it without false collapses on
	// short sessions. The first step whose msg_count_le is >= len(messages)
	// wins; an empty staircase falls back to the scalar
	// Compression.DedupSimilarityThreshold. See T53.
	DedupStaircase []StaircaseStep `toml:"dedup_staircase"`
	// ToolCompressor holds the RTK-derived heuristic knobs that used to
	// live as local `const` declarations inside
	// internal/compression/tool_compressor.go. Exposing them via config
	// unblocks data-driven tuning without a rebuild. See T61.
	ToolCompressor ToolCompressorTuning `toml:"tool_compressor"`
}

// ToolCompressorTuning bundles RTK-style heuristic thresholds for the
// type-aware tool-output compressor. Zero values fall back to the
// compile-time defaults so legacy configs keep byte-equal behaviour.
type ToolCompressorTuning struct {
	// AggressiveAfterMultiplier controls when a message is considered
	// "old enough" for aggressive (more lossy) compression. Age is the
	// distance to the compressible boundary; the message switches to
	// aggressive when age > multiplier * slidingWindow. Default 2.
	AggressiveAfterMultiplier int `toml:"aggressive_after_multiplier"`
	// GitModerateDiffLimit caps the number of diff lines retained when
	// compressing git output in non-aggressive mode. Default 60.
	GitModerateDiffLimit int `toml:"git_moderate_diff_limit"`
	// TestMaxFailureLines caps how many lines around a test failure are
	// preserved in moderate compression. Default 40.
	TestMaxFailureLines int `toml:"test_max_failure_lines"`
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
	// EnableMinTokens emits the `min_tokens` request field (T91). Off by
	// default because MiniMax's contract for this field is not publicly
	// documented; flip after live verification.
	EnableMinTokens bool `toml:"enable_min_tokens"`
	// EnableSeed emits the `seed` request field for stable summaries (T91).
	// Off by default to keep the wire shape unchanged until opt-in.
	EnableSeed bool `toml:"enable_seed"`
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

// DefaultConfigPath returns the legacy default path to the config file.
// Kept for backwards compatibility; new code should use ResolveConfigPath to
// honour the full flag/env/XDG precedence chain.
func DefaultConfigPath() string {
	return filepath.Join(expandHome("~"), ".slimference", "config.toml")
}

// XDGConfigPath returns the XDG-Base-Dir-compliant config path:
// $XDG_CONFIG_HOME/slimference/config.toml, or
// $HOME/.config/slimference/config.toml if XDG_CONFIG_HOME is unset.
func XDGConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "slimference", "config.toml")
	}
	return filepath.Join(expandHome("~"), ".config", "slimference", "config.toml")
}

// LoadOptions carries caller-supplied overrides for config resolution.
// Zero value is valid and triggers the default precedence:
//
//	flag  (LoadOptions.ExplicitPath)
//	env   (SLIMFERENCE_CONFIG)
//	xdg   ($XDG_CONFIG_HOME/slimference/config.toml)
//	legacy (~/.slimference/config.toml)
//	defaults (no file)
type LoadOptions struct {
	// ExplicitPath, if non-empty, is the absolute/relative path to the
	// config file. Typically sourced from a --config CLI flag. Overrides
	// every other source; a non-existent explicit path is a hard error.
	ExplicitPath string
	// AllowLegacyWarn suppresses the deprecation warning on the ~/.slimference
	// path when true. Tests use this to keep log output deterministic.
	AllowLegacyWarn bool
}

// LoadInfo describes which config source was ultimately used. Exposed via
// `slimference doctor` and admin surfaces so operators can tell at a glance
// whether a file was read and from where.
type LoadInfo struct {
	// ResolvedPath is the path that was actually read. Empty when no file
	// existed and built-in defaults were used.
	ResolvedPath string
	// Source is one of "flag", "env", "xdg", "legacy", or "defaults".
	Source string
	// Checked lists every candidate path inspected, in precedence order, for
	// diagnostic output.
	Checked []string
}

// ResolveConfigPath walks the precedence chain and returns the first
// existing path plus metadata describing which slot matched. When no file
// exists the returned path is empty and Source == "defaults".
func ResolveConfigPath(opts LoadOptions) LoadInfo {
	info := LoadInfo{}

	add := func(label, p string) (string, string, bool) {
		info.Checked = append(info.Checked, label+"="+p)
		if p == "" {
			return "", "", false
		}
		if _, err := os.Stat(p); err == nil {
			return p, label, true
		}
		return "", "", false
	}

	// flag
	if opts.ExplicitPath != "" {
		if p, lbl, ok := add("flag", opts.ExplicitPath); ok {
			info.ResolvedPath, info.Source = p, lbl
			return info
		}
		// Explicit flag that does not exist is NOT silently ignored; caller
		// handles the empty ResolvedPath + Source=="flag_missing".
		info.Source = "flag_missing"
		info.ResolvedPath = opts.ExplicitPath
		return info
	}
	// env
	if p, lbl, ok := add("env", os.Getenv("SLIMFERENCE_CONFIG")); ok {
		info.ResolvedPath, info.Source = p, lbl
		return info
	}
	// xdg
	if p, lbl, ok := add("xdg", XDGConfigPath()); ok {
		info.ResolvedPath, info.Source = p, lbl
		return info
	}
	// legacy
	if p, lbl, ok := add("legacy", DefaultConfigPath()); ok {
		info.ResolvedPath, info.Source = p, lbl
		return info
	}
	info.Source = "defaults"
	return info
}

// Load reads and validates the configuration using the default precedence
// (env / xdg / legacy / defaults). It is preserved for callers that do not
// need LoadInfo; new code should prefer LoadWithOptions.
func Load() (*Config, error) {
	cfg, _, err := LoadWithOptions(LoadOptions{})
	return cfg, err
}

// LoadWithOptions is the full-fidelity loader. It applies the precedence
// chain, runs env overrides, applies the L2 operating mode, and validates
// the resulting Config. The returned LoadInfo identifies which source was
// used so callers can surface that to users.
func LoadWithOptions(opts LoadOptions) (*Config, LoadInfo, error) {
	cfg := defaultsRaw()
	info := ResolveConfigPath(opts)

	if info.Source == "flag_missing" {
		return nil, info, fmt.Errorf("config file not found: %s", info.ResolvedPath)
	}

	if info.ResolvedPath != "" {
		if _, err := toml.DecodeFile(info.ResolvedPath, cfg); err != nil {
			return nil, info, fmt.Errorf("parse config %s: %w", info.ResolvedPath, err)
		}
	}

	applyEnvOverrides(cfg)

	// T36: apply the selected Layer 2 operating mode. This fills any numeric
	// summary fields that the TOML or env did not explicitly set, giving the
	// mode profile the role of "coherent default bundle". Explicit positive
	// overrides from TOML/env win.
	if err := ApplyL2OperatingMode(&cfg.Compression.Summary, cfg.Compression.Summary.Mode); err != nil {
		return nil, info, fmt.Errorf("invalid config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, info, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, info, nil
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
	if v := os.Getenv("SLIMFERENCE_UPSTREAM_CODEX_CHATGPT_BASE_URL"); v != "" {
		cfg.Upstream.CodexChatGPT.BaseURL = v
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

// expandHome expands a leading ~ to the user home directory. Accepts bare "~"
// as well as "~/...". Returns the input unchanged if home lookup fails.
func expandHome(path string) string {
	if path == "~" {
		home, err := userHomeDirFunc()
		if err != nil {
			return path
		}
		return home
	}
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
