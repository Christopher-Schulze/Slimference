package config

// Defaults returns a Config populated with sensible production defaults.
// SummaryConfig is then passed through ApplyL2OperatingMode so its numeric
// fields reflect the default "balanced" profile without forcing every
// caller to run the mode resolver themselves.
func Defaults() *Config {
	cfg := defaultsRaw()
	_ = ApplyL2OperatingMode(&cfg.Compression.Summary, cfg.Compression.Summary.Mode)
	return cfg
}

// defaultsRaw returns the pre-mode-applied defaults. Separated from Defaults
// so Load() can decode TOML over an unresolved shell and resolve the mode
// only after env overrides have been applied.
func defaultsRaw() *Config {
	return &Config{
		Proxy: ProxyConfig{
			ListenAddress:              "127.0.0.1",
			ListenPort:                 8990,
			IPv6:                       false,
			DirectCodexWebSocketPolicy: "tunnel",
			OpenAIPromptCache: OpenAIPromptCacheConfig{
				Enabled:                    false,
				PromptCacheKeyStrategy:     "session",
				Retention:                  "off",
				MinTokens:                  1024,
				MaxRequestsPerKeyPerMinute: 15,
			},
		},
		Transparent: TransparentConfig{
			// Legacy: CONNECT-based MITM. Phase H (2026-05-16) does not
			// auto-arm this. Kept for advanced users who manually
			// configure HTTPS_PROXY. New installs use SNIPeekMode below.
			Enabled: false,
			// Process-local Codex Desktop launcher support. This does not
			// mutate system proxy settings and only serves clients that
			// explicitly inherit HTTPS_PROXY from `codex launch-desktop`.
			ScopedDesktopProxy: true,
			// Phase H primary Traffic-IN switch. Off by default so
			// `slimference install` is silent until the user runs
			// `slimference enable`. See docs/install.md.
			SNIPeekMode:    false,
			SNIPeekPort:    8443,
			InterceptHosts: []string{"api.openai.com", "api.anthropic.com", "chatgpt.com"},
			CertCacheSize:  256,
			AudioBypassPaths: []string{
				"/v1/audio/transcriptions",
				"/v1/audio/translations",
				"/v1/realtime",
				"/backend-api/realtime",
			},
			DefaultTLSProfile: "chromium_stable",
			TLSProfiles: map[string]string{
				"api.openai.com":    "node_stable",
				"api.anthropic.com": "node_stable",
				"chatgpt.com":       "chromium_stable",
			},
		},
		Upstream: UpstreamConfig{
			Anthropic:    ProviderUpstream{BaseURL: "https://api.anthropic.com"},
			OpenAI:       ProviderUpstream{BaseURL: "https://api.openai.com"},
			CodexChatGPT: ProviderUpstream{BaseURL: "https://chatgpt.com"},
		},
		Compression: CompressionConfig{
			Layer1Enabled: true,
			// Layer 2 (MiniMax-backed semantic summarization) defaults
			// to OFF: Slimference ships deterministic-only by default
			// (decision 2026-05-15). Users who explicitly opt into
			// model-based summarization can flip layer2_enabled=true in
			// their config. MiniMax code is preserved for that opt-in
			// path but is not on the default token-saving hot path.
			Layer2Enabled:                     false,
			Layer3Enabled:                     true,
			SlidingWindow:                     5,
			MinMessagesForCompression:         8,
			MinTokensForLayer2:                15000,
			Layer2LatencyBudgetMs:             0, // 0 = guard off (opt-in)
			Layer2LatencyProjectionMultiplier: 1.2,
			Layer2LatencyEMAAlpha:             0.2,
			StructureMinTokens:                500,
			StructureLanguages: []string{
				"go", "typescript", "javascript", "rust", "python",
				"c", "cpp", "java", "ruby", "shell", "zig", "swift",
				"kotlin", "php", "dart", "scala", "elixir", "solidity",
				"svelte",
			},
			DedupSimilarityThreshold: 0.85,
			Summary: SummaryConfig{
				// Mode=balanced is the default operating profile. The
				// numeric fields stay zero so ApplyL2OperatingMode can
				// fill them from the profile without pretending they
				// were operator-set. TOML/env values override after.
				Mode: ModeBalanced,
				// T109: outbound redaction default-on. Operators that
				// truly want raw outbound must set this to "off"
				// explicitly; doctor warns when they do.
				OutboundRedaction: "default",
			},
			OutputReduce: OutputReduceConfig{
				Enabled:              true,
				Profile:              "auto",
				SignatureMarker:      "#slimference-output-rules",
				MaxAddedBytes:        1400,
				MinInputTokens:       400,
				AutoDisableThreshold: 30,
				AutoTuneEnabled:      true,
				AutoTuneMinSamples:   30,
				MinNetSavingsPct:     15,
				MaxFailureRateDelta:  0.05,
				CooldownTurns:        50,
				// T165/T166/T167: deterministic output-token
				// reductions default-on. Operators can disable
				// individually via env or TOML.
				StopSequencesEnabled:               true,
				StreamCutEnabled:                   true,
				RepetitionDetectionEnabled:         true,
				StaleReadAgingEnabled:              true,
				StaleReadAgingMinTurnGap:           3,
				ObsoleteReadPruneEnabled:           true,
				BeTerseHintEnabled:                 false,
				ArchiveRecoveryNoteEnabled:         false,
				CodexSavingsPolicyMode:             "auto",
				CodexChunkDedupEnabled:             false,
				CodexChunkDedupMinBytes:            4096,
				CodexChunkDedupMaxSessions:         256,
				CodexChunkDedupMaxChunksPerSession: 8192,
				CodexChunkDedupTTLSeconds:          4 * 60 * 60,
				CodexChunkDedupMaxReferencePercent: 90,
			},
			Tuning: TuningConfig{
				IncrementalOverlapThreshold: 0.70,
				IncrementalStaircase: []StaircaseStep{
					{MsgCountLE: 60, Threshold: 0.70},
					{MsgCountLE: 120, Threshold: 0.55},
					{MsgCountLE: 1_000_000, Threshold: 0.40},
				},
				OverflowSlidingWindow:       2,
				OverflowTargetRatio:         0.10,
				StructureInWindow:           false,
				StructureInWindowMinTokens:  1500,
				ToolOutputInWindow:          true,
				ToolOutputInWindowMinTokens: 800,
				LoopDetection:               false,
				StructurePreview:            true,
				DedupStaircase: []StaircaseStep{
					{MsgCountLE: 10, Threshold: 0.88},
					{MsgCountLE: 20, Threshold: 0.85},
					{MsgCountLE: 40, Threshold: 0.82},
					{MsgCountLE: 1_000_000, Threshold: 0.78},
				},
				MidExchangeThresholdTokens: 10000,
				ToolCompressor: ToolCompressorTuning{
					AggressiveAfterMultiplier: 2,
					GitModerateDiffLimit:      60,
					TestMaxFailureLines:       40,
				},
				PlannerLiveCorpusConfidence: "unknown",
			},
		},
		Cache: CacheConfig{
			ResponseCacheMaxEntries:       100,
			ResponseCacheTTLSeconds:       300,
			SummaryRefreshIntervalSeconds: 1800,
		},
		Usage: UsageConfig{
			EstimatedPrefillSpeed: 50000,
		},
		Secrets: SecretsConfig{
			Mode:      "redact",
			Allowlist: []string{},
		},
		Analytics: AnalyticsConfig{
			Dashboard:               true,
			LogDir:                  "~/.slimference/analytics",
			DashboardRefreshSeconds: 2,
			GainUSDPerMillionTokens: 0,
		},
		Logging: LoggingConfig{
			Level:  "debug",
			Format: "json",
			File:   "~/.slimference/logs/slimference.jsonl",
		},
		Filter: FilterConfig{
			PassthroughMaxChars: 2000,
		},
		Hooks: HooksConfig{
			CodexPostToolTimeoutSeconds: 4,
			CodexPostToolMinTokens:      800,
		},
		Debug: DebugConfig{},
	}
}

// DefaultTOML returns the default config as a TOML string for `config init`.
func DefaultTOML() string {
	return `# Slimference Configuration
# Generated by: slimference config init

[proxy]
listen_address = "127.0.0.1"
listen_port = 8990
ipv6 = false
# Local Codex CLI direct mode only. "tunnel" preserves WebSockets; set
# "force_https_fallback" to reject direct Codex WebSocket upgrades so Codex
# uses its native HTTPS fallback and Slimference can apply the JSON pipeline.
# Does not affect transparent CONNECT/MITM or Codex App traffic.
direct_codex_websocket_policy = "tunnel"
# T78: when true, the proxy uses provider server-side state
# (previous_response_id for OpenAI Responses / CodexChatGPT) on
# follow-up turns instead of resending the full history. Default off
# so traffic shape stays unchanged until you flip the switch.
server_state_enabled = false

[proxy.openai_prompt_cache]
# T136: optional OpenAI API prompt-cache routing fields. Disabled by default
# because generic OpenAI supports these fields, but CodexChatGPT backend routes
# must stay untouched until their live contract is captured.
enabled = false
prompt_cache_key_strategy = "session" # off | session | model_session | static
static_prompt_cache_key = ""
retention = "off" # off | in_memory | 24h | auto
min_tokens = 1024
max_requests_per_key_per_minute = 15

[transparent]
# T131/T123: transparent system-proxy ingress. Disabled by default so
# config-patch mode remains byte-for-byte unchanged until explicitly enabled
# by slimference proxy enable / operator config.
enabled = false
# T238: process-local Codex Desktop HTTPS_PROXY ingress on the daemon port.
# This does not change system proxy, /etc/hosts, or Codex config; only a
# launcher-spawned process that inherits HTTPS_PROXY uses it.
scoped_desktop_proxy = true
intercept_hosts = ["api.openai.com", "api.anthropic.com", "chatgpt.com"]
cert_cache_size = 256
# Empty means ~/.slimference; the CA files live below ca/.
ca_dir = ""
# WebRTC audio is UDP and bypasses HTTPS proxy settings. These paths are a
# conservative guard for future TCP/TLS realtime fallbacks on intercepted hosts.
audio_bypass_paths = ["/v1/audio/transcriptions", "/v1/audio/translations", "/v1/realtime", "/backend-api/realtime"]
# T123 TLS fingerprint profile. node_stable/python_requests are intent aliases
# mapped to the closest maintained uTLS profile rather than stale JA3 literals.
default_tls_profile = "chromium_stable"

[transparent.tls_profiles]
"api.openai.com" = "node_stable"
"api.anthropic.com" = "node_stable"
"chatgpt.com" = "chromium_stable"

[upstream.anthropic]
base_url = "https://api.anthropic.com"

[upstream.openai]
base_url = "https://api.openai.com"

[compression]
layer1_enabled = true
layer2_enabled = false
layer3_enabled = true
sliding_window = 5
min_messages_for_compression = 8
min_tokens_for_layer2 = 15000
# T54: optional latency guard. Non-zero to activate. Skips L2 when
# EMA-based projection would exceed the budget (ms).
layer2_latency_budget_ms = 0
layer2_latency_projection_multiplier = 1.2
layer2_latency_ema_alpha = 0.2
structure_min_tokens = 500
structure_languages = ["go", "typescript", "javascript", "rust", "python", "c", "cpp", "java", "ruby", "shell", "zig", "swift", "kotlin", "php", "dart", "scala", "elixir", "solidity", "svelte"]
dedup_similarity_threshold = 0.85

[compression.output_reduce]
# T130: output-token discipline injection. This is input-side only: Slimference
# appends concise provider-specific rules to the system prompt and never edits
# provider responses after generation.
enabled = true
profile = "auto"
custom_directive_path = ""
signature_marker = "#slimference-output-rules"
max_added_bytes = 1400
# Below this request size, the directive overhead dominates likely savings.
min_input_tokens = 400
auto_disable_threshold = 30
auto_tune_enabled = true
auto_tune_min_samples = 30
min_net_savings_pct = 15
max_failure_rate_delta = 0.05
cooldown_turns = 50
# T249 recovery contract. Default off until comprehension A/B certifies it:
# when enabled, Codex WSS gets one neutral session note explaining how to
# request full archived content by local-archive id.
archive_recovery_note_enabled = false
archive_recovery_note_text = ""
# Default 0 keeps maximum-savings behavior. Raise only after A/B proof if
# immediate cross-turn re-read recency matters more than the repeated-read saving.
read_delta_recent_full_pass_turns = 0
# Central Codex savings autopilot: off, conservative, auto, or max. "auto"
# enables aggressive recoverable reducers only when their safety prerequisites
# are present and loosens automatically on recency/context-risk signals.
codex_savings_policy_mode = "auto"
# T255 content-defined chunk dedup. This explicit toggle remains for operators
# who want to force the mechanism under conservative policy; auto policy can
# enable it without this field.
codex_chunk_dedup_enabled = false
	codex_chunk_dedup_min_bytes = 4096
	codex_chunk_dedup_max_sessions = 256
	codex_chunk_dedup_max_chunks_per_session = 8192
	codex_chunk_dedup_ttl_seconds = 14400
	codex_chunk_dedup_max_reference_percent = 90
	
	[compression.minimax]
# Historical section name, but the client is OpenAI-compatible:
# set base_url/model/api_key_env to swap MiniMax M2.7 for another
# /v1/chat/completions provider such as NVIDIA NIM.
base_url = "https://api.minimax.io/v1"
api_key_env = "MINIMAX_API_KEY"
model = "MiniMax-M2.7"
temperature = 0
top_p = 1
max_retries = 3
connect_timeout_seconds = 5
response_timeout_seconds = 30
rate_limit_rpm = 10
# T91: emit seed for deterministic summaries; off until verified live.
enable_seed = false
# T91: emit min_tokens to lift the lower bound of the completion length.
# Off by default because the field is not publicly documented for MiniMax.
enable_min_tokens = false
# MiniMax M2.x OpenAI-compatible responses may include <think> content in
# message.content. reasoning_split moves that into reasoning_details. Disable
# for non-MiniMax OpenAI-compatible providers that reject extra fields.
enable_reasoning_split = true

[compression.summary]
# Operating mode: strict | balanced | fast. Selecting a mode configures
# target_ratio / max_ratio / min_ratio / strict as a coherent bundle. Any
# numeric field explicitly set below still overrides the profile.
# Env override: SLIMFERENCE_L2_MODE.
mode = "balanced"
# Explicit overrides (optional; leave unset to inherit from mode).
# target_ratio = 0.20
# max_ratio = 0.40
# min_ratio = 0.05
# strict = true
# T88: require_deterministic skips chain providers that do not advertise
# both temperature=0 + seed support. Default off; turn on (alongside
# [compression.minimax] enable_seed = true) before adding a second
# OpenAI-style fallback whose determinism is unverified.
require_deterministic = false

[compression.tuning]
# Incremental-summary overlap threshold: if an existing summary covers at
# least this fraction of the compressible range, do an incremental update
# instead of a full rebuild. Used only when incremental_staircase is empty.
incremental_overlap_threshold = 0.70
# Aggressive sliding window used only when upstream reports a context
# overflow (spec+.md §17.4).
overflow_sliding_window = 2
# Aggressive summary target ratio for the overflow recover path.
overflow_target_ratio = 0.10

# Conversation-size-keyed staircase of incremental-overlap thresholds. The
# first step whose msg_count_le is >= the current conversation length wins.
# Long conversations benefit from a lower threshold: a full rebuild costs
# proportionally more work while incremental updates remain cheap.
[[compression.tuning.incremental_staircase]]
msg_count_le = 60
threshold = 0.70
[[compression.tuning.incremental_staircase]]
msg_count_le = 120
threshold = 0.55
[[compression.tuning.incremental_staircase]]
msg_count_le = 1000000
threshold = 0.40

# T24: allow Layer 1 structure extraction on large tool_result blocks even
# when they fall inside the sliding window. Conservative default: off.
# When enabled, only tool_result blocks above structure_in_window_min_tokens
# and not on the last message are eligible.
structure_in_window = false
structure_in_window_min_tokens = 1500

# Allow deterministic compaction of large current-turn tool outputs. This is
# what makes short Codex CLI turns with one huge rg/go-test/build output save
# tokens without waiting for the output to age out of the sliding window.
tool_output_in_window = true
tool_output_in_window_min_tokens = 800

# T37: detect retry loops (>=4 consecutive user turns with >=0.75 Jaccard
# word similarity) and prepend a short nudge to the final user message so
# the model can break out. Default off.
loop_detection = false

# T38: replace large tool_result blocks (>=4 KB) with a shape-aware
# preview (JSON / paths / ASCII table) when strictly shorter. Default on
# (T76): every preview mutation is archived via the content-archive
# recorder so the original is recoverable through "slimference expand".
structure_preview = true

# T100: cross-direction coordinator. When on and Layer 2 is enabled,
# Layer 1 skips heavy sub-layers on the prefix that L2 will summarise.
# Cheap passes (ANSI strip, JSON compact) always run. Default off.
coordinator_enabled = false

# T104: goroutine fan-out across independent L1 sub-layers. When on,
# messages in the compressible prefix are processed concurrently
# (bounded by GOMAXPROCS). Default off until benchmark evidence
# justifies the overhead on real bodies.
coordinator_parallel = false

# T103: Layer 4 tool-definition pruning. When on, tool definitions
# idle for more than the threshold are removed from the request
# body and archived for reattachment. Default off.
tool_prune_enabled = false
# Extra exact tool names that must never be pruned. Shell/edit/read/safety,
# browser, and MCP tool classes are always kept even when this list is empty.
tool_prune_always_keep = []

# T99: Layer 2 mid-exchange summarization. When on, long in-flight
# exchanges exceeding the token threshold produce an in-progress
# summary. Default off until a corpus validates the trade-off.
mid_exchange_enabled = false
# Token threshold for mid-exchange summarization (default 10000).
mid_exchange_threshold_tokens = 10000

# T149: planner live-corpus confidence. Leave unknown unless a real
# operator corpus proves that higher-risk layers are safe for this ingress.
planner_live_corpus_confidence = "unknown"
# Optional metadata file or directory containing metadata.json with
# synthetic/evidence_level/expected_request_count fields.
planner_live_corpus_metadata_path = ""

# T108: chunked Layer 1 pipeline (ANSI strip + line dedup +
# repeated-line collapse) with bounded memory. The standalone API
# lives in internal/compression/streaming.go; live wire-in into
# the request path is a follow-up. Default off.
streaming_compression_enabled = false
# Rolling de-dup window the chunked pipeline uses (default 500).
streaming_window_lines = 500

[cache]
response_cache_max_entries = 100
response_cache_ttl_seconds = 300
summary_refresh_interval_seconds = 1800

[usage]
estimated_prefill_speed = 50000

[secrets]
mode = "redact"
# custom_patterns = [
#   { name = "My API Key", regex = "myapp-[a-z0-9]{32}" }
# ]
# allowlist = ["test/fixtures/**"]

[analytics]
dashboard = true
log_dir = "~/.slimference/analytics"
dashboard_refresh_seconds = 2
# Optional rough USD for slimference gain: savings_est_tokens / 1e6 * rate. Env: SLIMFERENCE_GAIN_USD_PER_MILLION
# gain_usd_per_million_tokens = 3.0

[logging]
level = "debug"
format = "json"
file = "~/.slimference/logs/slimference.jsonl"

# Layer 0 (slimference filter) — optional; SLIMFERENCE_FILTER_DB / SLIMFERENCE_TEE_DIR override when set.
[filter]
# Max characters (Unicode code points) of filtered stdout; 0 = unlimited. Env: SLIMFERENCE_FILTER_PASSTHROUGH_MAX_CHARS
passthrough_max_chars = 2000
# filter_db = "~/.slimference/filter.db"
# tee_dir = "~/.slimference/tee"
# Extra deny rules (RE2), matched against the full command line (after built-in destructive checks).
# deny_patterns = ['\\bnc\\s+-l\\s+']
#
# Per-project overrides (same deny_patterns key): .slimference/filters.toml in the working directory.

# Hook install (slimference hook install …) — optional path if "slimference" is not on PATH in the agent.
[hooks]
# slimference_command = "/usr/local/bin/slimference"
codex_posttool_timeout_seconds = 4
codex_posttool_min_tokens = 800

# Future: filter decision JSONL path (SLIMFERENCE_DEBUG_DECISIONS_LOG overrides).
[debug]
# decisions_log = "~/.slimference/decisions.jsonl"
`
}
