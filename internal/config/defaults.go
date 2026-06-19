package config

// Defaults returns a Config populated with sensible production defaults.
func Defaults() *Config {
	return defaultsRaw()
}

func defaultsRaw() *Config {
	return &Config{
		Proxy: ProxyConfig{
			ListenAddress:              "127.0.0.1",
			ListenPort:                 8990,
			IPv6:                       false,
			DirectCodexWebSocketPolicy: "tunnel",
			OpenAIPromptCache: OpenAIPromptCacheConfig{
				Enabled:                    true,
				PromptCacheKeyStrategy:     "model_stable_prefix",
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
			Layer1Enabled:             true,
			Layer2Enabled:             true,
			SlidingWindow:             5,
			MinMessagesForCompression: 8,
			StructureMinTokens:        500,
			StructureLanguages: []string{
				"go", "typescript", "javascript", "rust", "python",
				"c", "cpp", "java", "ruby", "shell", "zig", "swift",
				"kotlin", "php", "dart", "scala", "elixir", "solidity",
				"svelte",
			},
			DedupSimilarityThreshold: 0.85,
			OutputReduce: OutputReduceConfig{
				Enabled:                   true,
				Profile:                   "auto",
				SignatureMarker:           "#slimference-output-rules",
				MaxAddedBytes:             1400,
				MinInputTokens:            400,
				AutoDisableThreshold:      30,
				AutoTuneEnabled:           true,
				AutoTuneMinSamples:        30,
				MinNetSavingsPct:          15,
				MaxFailureRateDelta:       0.05,
				CooldownTurns:             50,
				ConciseChatEnabled:        true,
				ConciseChatMinInputTokens: 400,
				// T165/T166/T167: deterministic output-token
				// reductions default-on. Operators can disable
				// individually via env or TOML.
				StopSequencesEnabled:                      true,
				StreamCutEnabled:                          true,
				RepetitionDetectionEnabled:                true,
				StaleReadAgingEnabled:                     true,
				StaleReadAgingMinTurnGap:                  3,
				ObsoleteReadPruneEnabled:                  true,
				BeTerseHintEnabled:                        false,
				ArchiveRecoveryNoteEnabled:                false,
				CodexSavingsPolicyMode:                    "auto",
				CodexWSSStatefulToolPrefixElisionEnabled:  false,
				CodexChunkDedupEnabled:                    false,
				CodexChunkDedupProofLevel:                 "live",
				CodexChunkDedupMinBytes:                   4096,
				CodexChunkDedupMaxSessions:                256,
				CodexChunkDedupMaxChunksPerSession:        8192,
				CodexChunkDedupTTLSeconds:                 4 * 60 * 60,
				CodexChunkDedupMaxReferencePercent:        90,
				CodexChunkDedupMaxSessionReferencePercent: 70,
			},
			Tuning: TuningConfig{
				OverflowSlidingWindow:       2,
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
				ToolCompressor: ToolCompressorTuning{
					AggressiveAfterMultiplier: 2,
					GitModerateDiffLimit:      60,
					TestMaxFailureLines:       40,
				},
				CoordinatorParallel:            true,
				WSSFullHistoryToolPruneEnabled: true,
				ToolPruneIdleThresholdTurns:    20,
				PlannerLiveCorpusConfidence:    "unknown",
			},
		},
		Cache: CacheConfig{
			ResponseCacheMaxEntries: 100,
			ResponseCacheTTLSeconds: 300,
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
		Savings: SavingsConfig{
			CachedPriceRatio: 0.10,
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
			CodexPostToolMinTokens:      200,
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
# T136/T285: OpenAI API prompt-cache routing fields for stable prefixes.
# Applies only to generic OpenAI API traffic. CodexChatGPT backend routes stay
# untouched until their live contract is captured. Rejected fields trigger a
# fail-open retry and a short per-model cooldown.
enabled = true
prompt_cache_key_strategy = "model_stable_prefix" # off | stable_prefix | model_stable_prefix | session | model_session | static
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
layer2_enabled = true
sliding_window = 5
min_messages_for_compression = 8
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
# Conservative user-facing chat style hint. Applies only to direct-answer /
# explanation turns and full-passes code, docs, JSON, logs, diffs, repair,
# review, planning, and tool-output turns.
concise_chat_enabled = true
# Below this request size, the concise-chat hint overhead dominates likely
# output savings. Set 0 only for explicit operator experiments.
concise_chat_min_input_tokens = 400
concise_chat_text = ""
# T249 recovery contract. Default off as an operator hint. Codex WSS/HTTP can
# still inject one neutral session note automatically when a recoverable chunk
# reference is emitted, explaining how to request full archived content by
# local-archive id.
archive_recovery_note_enabled = false
archive_recovery_note_text = ""
# Default 0 keeps maximum-savings behavior. Raise only after A/B proof if
# immediate cross-turn re-read recency matters more than the repeated-read saving.
read_delta_recent_full_pass_turns = 0
# Central Codex savings autopilot: off, conservative, auto, or max. "auto"
# enables aggressive recoverable reducers only when their safety prerequisites
# are present and loosens automatically on recency/context-risk signals.
codex_savings_policy_mode = "auto"
# Lab/proof switch only. Product default full-passes Codex WSS request bodies
# that carry tool output because current Codex Desktop Responses chains can
# reject later previous_response_id turns after a prior WSS tool-output rewrite.
codex_wss_tool_output_mutation_enabled = false
# Lab/proof switch only. Tool schemas are model-facing capability context in
# Codex WSS; product default keeps them on every request after live proof showed
# that eliding them can suppress real command_execution tool calls.
codex_wss_stateful_tool_prefix_elision_enabled = false
# T359 search-output cap promotion latch. Leave empty unless the final
# release-proof-report JSON path passed with focused search_loop proof, named
# selected cap, and before/after Codex route hygiene snapshot paths. Raw cap
# counts are not config knobs; config loading validates this proof before
# activating sharper runtime search caps.
codex_search_cap_proof_path = ""
# T255 content-defined chunk dedup. This explicit toggle remains for operators
# who want to force the mechanism under conservative policy; auto policy can
# enable it without this field.
codex_chunk_dedup_enabled = false
codex_chunk_dedup_proof_level = "live"
codex_chunk_dedup_min_bytes = 4096
codex_chunk_dedup_max_sessions = 256
codex_chunk_dedup_max_chunks_per_session = 8192
codex_chunk_dedup_ttl_seconds = 14400
codex_chunk_dedup_max_reference_percent = 90
codex_chunk_dedup_max_session_reference_percent = 70

[compression.tuning]
# Aggressive sliding window used only when upstream reports a context
# overflow (docs/spec.md §17.4).
overflow_sliding_window = 2

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

# T104: automatic goroutine fan-out for large enough Layer-1 prefix work.
# Small requests stay sequential; large prefixes are processed concurrently
# with the original message order preserved.
coordinator_parallel = true

# T103: broad Layer 3 tool-definition pruning. When on, tool definitions
# idle for more than the threshold are removed from eligible request bodies
# and archived for reattachment. Default off.
tool_prune_enabled = false
# Default-safe Codex WSS slice: apply tool-prune only to previous_response_id
# full-history resends. Root and steady delta WSS tool prefixes stay byte-equal.
wss_full_history_tool_prune_enabled = true
tool_prune_idle_threshold_turns = 20
# Extra exact tool names that must never be pruned. Shell/edit/read/safety,
# browser, and MCP tool classes are always kept even when this list is empty.
tool_prune_always_keep = []

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

[savings]
# Provider-cache read billing ratio used by slimference savings scorecards. Env: SLIMFERENCE_CACHED_PRICE_RATIO
cached_price_ratio = 0.10

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
codex_posttool_min_tokens = 200

# Future: filter decision JSONL path (SLIMFERENCE_DEBUG_DECISIONS_LOG overrides).
[debug]
# decisions_log = "~/.slimference/decisions.jsonl"
`
}
