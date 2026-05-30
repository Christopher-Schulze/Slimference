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
	Transparent TransparentConfig `toml:"transparent"`
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
	// CodexPostToolTimeoutSeconds is the fail-open watchdog for Codex PostToolUse.
	// It prevents a hung output compactor from reaching Codex' 600s hook timeout.
	CodexPostToolTimeoutSeconds int `toml:"codex_posttool_timeout_seconds"`
	// CodexPostToolMinTokens skips PostToolUse compaction for tiny tool outputs.
	// 0 disables the skip. Default is tuned for Bash output only.
	CodexPostToolMinTokens int `toml:"codex_posttool_min_tokens"`
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
	// DirectCodexWebSocketPolicy controls only local direct Codex CLI
	// WebSocket upgrades that arrive via openai_base_url/chatgpt_base_url
	// pointing at 127.0.0.1. It does not affect CONNECT/MITM transparent
	// mode or Codex App traffic. "tunnel" preserves WebSocket byte-for-byte;
	// "force_https_fallback" rejects the upgrade so Codex uses its native
	// HTTPS transport, which lets the existing JSON compression pipeline run.
	DirectCodexWebSocketPolicy string `toml:"direct_codex_websocket_policy"`
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
	// OpenAIPromptCache controls optional OpenAI prompt-cache routing
	// fields. It never applies to CodexChatGPT backend routes until their
	// live request contract is proven.
	OpenAIPromptCache OpenAIPromptCacheConfig `toml:"openai_prompt_cache"`
}

type OpenAIPromptCacheConfig struct {
	Enabled                    bool   `toml:"enabled"`
	PromptCacheKeyStrategy     string `toml:"prompt_cache_key_strategy"`
	StaticPromptCacheKey       string `toml:"static_prompt_cache_key"`
	Retention                  string `toml:"retention"`
	MinTokens                  int    `toml:"min_tokens"`
	MaxRequestsPerKeyPerMinute int    `toml:"max_requests_per_key_per_minute"`
}

// TransparentConfig controls the system-proxy CONNECT/MITM ingress.
type TransparentConfig struct {
	// Enabled wires the CONNECT interceptor into the live proxy server.
	// Default false keeps config-patch mode unchanged.
	Enabled bool `toml:"enabled"`
	// ScopedDesktopProxy wires the same loopback CONNECT ingress for
	// process-local Codex.app launchers without touching system proxy,
	// /etc/hosts, or ~/.codex/config.toml. It only activates when the
	// local CA already exists; cert trust is enforced by the launcher.
	ScopedDesktopProxy bool `toml:"scoped_desktop_proxy"`
	// InterceptHosts are MITM/compression targets. Empty means MITM all
	// CONNECT hosts and should only be used for dedicated test setups.
	InterceptHosts []string `toml:"intercept_hosts"`
	// CertCacheSize caps the per-domain leaf certificate cache.
	CertCacheSize int `toml:"cert_cache_size"`
	// CADir is the Slimference state directory that contains the `ca/`
	// subtree. Empty resolves to ~/.slimference.
	CADir string `toml:"ca_dir"`
	// AudioBypassPaths are request paths that should stay byte-relayed when
	// future realtime fallbacks share an intercepted host. WebRTC UDP still
	// bypasses the system HTTPS proxy by design.
	AudioBypassPaths []string `toml:"audio_bypass_paths"`
	// TLSProfiles maps upstream hostnames to outbound TLS fingerprint
	// profiles. Empty/unknown hosts use DefaultTLSProfile.
	TLSProfiles map[string]string `toml:"tls_profiles"`
	// DefaultTLSProfile is used when TLSProfiles has no host match.
	DefaultTLSProfile string `toml:"default_tls_profile"`
	// SNIPeekMode enables the T199 transparent.Engine that listens on
	// a raw TCP port, peeks the TLS ClientHello SNI, terminates TLS
	// with the local CA, and routes via internal/proxy/sniroute. This
	// is the path required for clients that ignore the system HTTPS
	// proxy (e.g. Codex 0.130's WebSocket transport). Off by default
	// so existing CONNECT-MITM deployments are unaffected.
	SNIPeekMode bool `toml:"sni_peek_mode"`
	// SNIPeekPort is the TCP port the SNI-peek listener binds. Use 443
	// for production (requires root or pfctl rdr); 8443 for dev. Zero
	// is treated as "disabled" even when SNIPeekMode is true.
	SNIPeekPort int `toml:"sni_peek_port"`
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
	Layer2LatencyEMAAlpha    float64            `toml:"layer2_latency_ema_alpha"`
	StructureMinTokens       int                `toml:"structure_min_tokens"`
	StructureLanguages       []string           `toml:"structure_languages"`
	DedupSimilarityThreshold float64            `toml:"dedup_similarity_threshold"`
	Summary                  SummaryConfig      `toml:"summary"`
	OutputReduce             OutputReduceConfig `toml:"output_reduce"`
	Tuning                   TuningConfig       `toml:"tuning"`
	// PromptOverridePath (T86) points at a file whose contents replace
	// the compiled-in MiniMax system prompt header. Empty disables the
	// override. The file's first non-empty line may carry a
	// `# version: <tag>` annotation that is recorded in
	// /admin/status.summarization.active_prompt_version.
	PromptOverridePath string `toml:"prompt_override_path"`
}

// OutputReduceConfig controls Layer 4 output-token reduction through
// provider-specific system-prompt discipline. It never edits provider
// responses after the fact; it only modifies outbound instructions.
type OutputReduceConfig struct {
	Enabled              bool    `toml:"enabled"`
	Profile              string  `toml:"profile"`
	CustomDirectivePath  string  `toml:"custom_directive_path"`
	SignatureMarker      string  `toml:"signature_marker"`
	MaxAddedBytes        int     `toml:"max_added_bytes"`
	MinInputTokens       int     `toml:"min_input_tokens"`
	AutoDisableThreshold int     `toml:"auto_disable_threshold"`
	AutoTuneEnabled      bool    `toml:"auto_tune_enabled"`
	AutoTuneMinSamples   int     `toml:"auto_tune_min_samples"`
	MinNetSavingsPct     float64 `toml:"min_net_savings_pct"`
	MaxFailureRateDelta  float64 `toml:"max_failure_rate_delta"`
	CooldownTurns        int     `toml:"cooldown_turns"`
	// StopSequencesEnabled (T165) injects a curated trailing-commentary
	// stop-sequence list into every Anthropic/OpenAI request so the
	// model halts at the API boundary before emitting "Hope this
	// helps!", "Let me know if…", etc. Default true; the underlying
	// phrase set lives in internal/outstop. Env override:
	// SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS=0 disables.
	StopSequencesEnabled bool `toml:"stop_sequences_enabled"`
	// StreamCutEnabled (T166) attaches a streaming-side cutter that
	// closes the upstream connection mid-response once the same
	// commentary patterns appear past a minimum-content threshold.
	// Acts as a safety net for cases the API stop_sequences (cap 4)
	// fail to cover. Default true. Env override:
	// SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT=0 disables.
	StreamCutEnabled bool `toml:"streamcut_enabled"`
	// RepetitionDetectionEnabled (T167) builds a per-request index of
	// the prompt's tool_result and code-fence content; the proxy then
	// rewrites verbatim echoes in the response into compact
	// "[unchanged: <name>:L<from>-<to>]" markers. Default true. Env
	// override: SLIMFERENCE_OUTPUT_REDUCE_REPDET=0 disables.
	RepetitionDetectionEnabled bool `toml:"repetition_detection_enabled"`
	// StaleReadAgingEnabled (T170) replaces older Read(...) tool_results
	// in the conversation with `[stale read: <path> superseded by
	// turn N]` markers when a newer read of the same path exists.
	// Lossless aging - the model has the current content from the
	// newer read, the older one is redundant. Default true. Env
	// override: SLIMFERENCE_INPUT_REDUCE_STALE_AGING=0 disables.
	StaleReadAgingEnabled bool `toml:"stale_read_aging_enabled"`
	// StaleReadAgingMinTurnGap is the minimum message-distance
	// between the old and new read before aging fires. Default 3.
	StaleReadAgingMinTurnGap int `toml:"stale_read_aging_min_turn_gap"`
	// ObsoleteReadPruneEnabled (T174) replaces tool_result reads
	// that happened before a subsequent file mutation
	// (apply_patch/Write/Edit) with `[obsolete: <path> edited at
	// turn N]`. Default true. Env override:
	// SLIMFERENCE_INPUT_REDUCE_OBSOLETE_PRUNE=0 disables.
	ObsoleteReadPruneEnabled bool `toml:"obsolete_read_prune_enabled"`
	// BeTerseHintEnabled (T169) injects a curated be-terse hint into
	// the system prompt for sessions routed to the qualityab
	// treatment cohort. **Default off** because this lever can
	// degrade quality. Operators opt in by flipping the toggle; the
	// harness auto-rolls-back if treatment-side failures exceed
	// control's by 5pp on 50+ samples. Env override:
	// SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT=1 enables.
	BeTerseHintEnabled bool `toml:"be_terse_hint_enabled"`
	// BeTerseHintText overrides the default hint. Empty falls back
	// to beterse.DefaultHint.
	BeTerseHintText string `toml:"be_terse_hint_text"`
	// ArchiveRecoveryNoteEnabled injects a neutral once-per-session
	// Codex WSS note explaining that local-archive:// ids can be
	// requested for full elided content. Default off until the A/B
	// harness certifies no comprehension drawdown.
	ArchiveRecoveryNoteEnabled bool `toml:"archive_recovery_note_enabled"`
	// ArchiveRecoveryNoteText overrides the neutral recovery note.
	// Empty falls back to the built-in wording.
	ArchiveRecoveryNoteText string `toml:"archive_recovery_note_text"`
	// ReadDeltaRecentFullPassTurns keeps unchanged re-reads full when the
	// previous read of the same path happened within the last N distinct
	// turn IDs. Default 0 keeps current maximum-savings behavior until
	// live A/B proof says the recency trade-off should be enabled.
	ReadDeltaRecentFullPassTurns int `toml:"read_delta_recent_full_pass_turns"`
	// CodexSavingsPolicyMode centralizes Codex WSS/HTTP reducer policy.
	// auto enables aggressive recoverable reducers only when their safety
	// prerequisites are present; conservative keeps only the low-risk
	// lossless reducers unless a mechanism is explicitly enabled.
	CodexSavingsPolicyMode string `toml:"codex_savings_policy_mode"`
	// CodexChunkDedupEnabled gates T255 content-defined chunk dedup for
	// Codex tool outputs/file reads. This is the legacy explicit override;
	// the auto policy can enable chunk dedup without setting this field.
	CodexChunkDedupEnabled bool `toml:"codex_chunk_dedup_enabled"`
	// CodexChunkDedupMinBytes is the minimum model-facing tool output size
	// eligible for chunk dedup. Smaller blocks are not worth reference
	// overhead. Default 4096 bytes, below Codex's observed ~8 KiB
	// truncated exec-output envelope.
	CodexChunkDedupMinBytes int `toml:"codex_chunk_dedup_min_bytes"`
	// CodexChunkDedupMaxSessions bounds the in-memory chunk identity store.
	CodexChunkDedupMaxSessions int `toml:"codex_chunk_dedup_max_sessions"`
	// CodexChunkDedupMaxChunksPerSession bounds per-session chunk identities.
	CodexChunkDedupMaxChunksPerSession int `toml:"codex_chunk_dedup_max_chunks_per_session"`
	// CodexChunkDedupTTLSeconds expires idle chunk identities. Raw content is
	// not kept in this store; archive payloads are bounded separately.
	CodexChunkDedupTTLSeconds int `toml:"codex_chunk_dedup_ttl_seconds"`
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
	// ToolOutputInWindow enables deterministic type-aware compaction for
	// large shell/tool outputs inside the sliding window. This is safe for
	// high-volume listings/search/test/build outputs because omitted markers
	// preserve the fact that output was truncated. Default true for Codex CLI
	// short-turn efficiency.
	ToolOutputInWindow bool `toml:"tool_output_in_window"`
	// ToolOutputInWindowMinTokens is the minimum estimated token count for
	// in-window tool output compaction. Default 800.
	ToolOutputInWindowMinTokens int `toml:"tool_output_in_window_min_tokens"`
	// LoopDetection enables T37: when 4+ consecutive user messages share
	// >=0.75 Jaccard word similarity, a synthetic nudge is prepended to
	// the final user message so the model can break out of a retry loop.
	// Default false (opt-in).
	LoopDetection bool `toml:"loop_detection"`
	// LoopStrategy (T116) selects the loop-handling approach: "additive"
	// (default, injects nudge text), "subtractive" (collapses streak), or
	// "off" (no loop handling). When LoopDetection is false, this is ignored.
	LoopStrategy string `toml:"loop_strategy"`
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
	// ToolPruneEnabled (T103) gates Layer 4 tool-definition pruning:
	// when on, tool definitions idle for more than
	// ToolPruneIdleThresholdTurns are removed from the request body
	// and archived for transparent reattachment. Default off.
	ToolPruneEnabled bool `toml:"tool_prune_enabled"`
	// ToolPruneAlwaysKeep extends the built-in always-keep class for
	// project-specific safety tools. Entries are exact tool names.
	ToolPruneAlwaysKeep []string `toml:"tool_prune_always_keep"`
	// MidExchangeEnabled (T99) gates Layer 2 mid-exchange summarization:
	// when on, long in-flight exchanges exceeding the token threshold
	// produce an in-progress summary. Default off until a corpus
	// validates the trade-off.
	MidExchangeEnabled bool `toml:"mid_exchange_enabled"`
	// MidExchangeThresholdTokens is the token budget above which an
	// in-flight exchange is considered for mid-exchange summarization.
	// Default 10000.
	MidExchangeThresholdTokens int `toml:"mid_exchange_threshold_tokens"`
	// StreamingCompressionEnabled (T108) gates the chunked Layer 1
	// pipeline (ANSI strip / line dedup / repeated-line collapse) for
	// large bodies. The pipeline lives in
	// `internal/compression/streaming.go` and is exposed today as a
	// standalone API; live wire-in into the request hot-path is a
	// follow-up. Default off.
	StreamingCompressionEnabled bool `toml:"streaming_compression_enabled"`
	// StreamingWindowLines is the rolling de-dup window the chunked
	// pipeline uses (default 500).
	StreamingWindowLines int `toml:"streaming_window_lines"`
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
	// AdaptiveWindowEnabled (T112) gates the adaptive sliding window: when
	// on, the proxy computes a complexity score per request and adjusts the
	// compressible-prefix boundary around the static SlidingWindow default.
	// Off = byte-equal to today's fixed window. Default off until soak.
	AdaptiveWindowEnabled bool `toml:"adaptive_window_enabled"`
	// AdaptiveWindowMin is the lower bound for the adaptive window (default 3).
	AdaptiveWindowMin int `toml:"adaptive_window_min"`
	// AdaptiveWindowMax is the upper bound for the adaptive window (default 12).
	AdaptiveWindowMax int `toml:"adaptive_window_max"`
	// PlannerLiveCorpusConfidence is an operator assertion for the T149
	// planner confidence fact. Empty/unknown keeps planner confidence
	// conservative. Valid values: unknown, low, medium, high.
	PlannerLiveCorpusConfidence string `toml:"planner_live_corpus_confidence"`
	// PlannerLiveCorpusMetadataPath points at a committed live-corpus
	// metadata.json file or a directory containing one. When the explicit
	// confidence above is empty/unknown, the proxy derives a conservative
	// high/medium/low planner confidence from that metadata.
	PlannerLiveCorpusMetadataPath string `toml:"planner_live_corpus_metadata_path"`
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

// SummaryConfig controls quality thresholds for deterministic summarizer outputs.
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
	// RequireDeterministic (T88) gates the FallbackChain on
	// strict-determinism capabilities. When on, providers whose
	// capability map does not advertise SupportsTemperatureZero +
	// SupportsSeed are skipped. Default off so legacy MiniMax-only
	// chains keep working.
	RequireDeterministic bool `toml:"require_deterministic"`
	// OutboundRedaction (T109) controls how aggressively outbound
	// summarisation input is sanitised before leaving the proxy. One of
	// "off", "default", "strict". Empty defaults to "default". Under
	// "strict", tool_input bodies are dropped entirely and an
	// additional structural JSON sweep runs on tool_result text.
	OutboundRedaction string `toml:"outbound_redaction"`
	// OutboundDropToolInputs forces tool_input dropping independently of
	// the OutboundRedaction mode. Useful for operators that want
	// default-mode redaction plus the strictest tool_input handling.
	OutboundDropToolInputs bool `toml:"outbound_drop_tool_inputs"`
	// MaxAnchorsInlined (T111) caps how many anchor messages are re-injected
	// verbatim into the compressed output. Excess anchors become one-line
	// digests. Default 8.
	MaxAnchorsInlined int `toml:"max_anchors_inlined"`
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
	if n, ok := envIntOK("SLIMFERENCE_CODEX_POSTTOOL_TIMEOUT_SECONDS"); ok {
		cfg.Hooks.CodexPostToolTimeoutSeconds = n
	}
	if n, ok := envIntOK("SLIMFERENCE_CODEX_POSTTOOL_MIN_TOKENS"); ok {
		cfg.Hooks.CodexPostToolMinTokens = n
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
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_L2_REQUIRE_DETERMINISTIC")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.Summary.RequireDeterministic = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_L2_OUTBOUND_REDACTION")); v != "" {
		cfg.Compression.Summary.OutboundRedaction = v
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_L2_PROMPT_OVERRIDE_PATH")); v != "" {
		cfg.Compression.PromptOverridePath = v
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.StopSequencesEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.StreamCutEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_REPDET")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.RepetitionDetectionEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_INPUT_REDUCE_STALE_AGING")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = b
		}
	}
	if n, ok := envIntOK("SLIMFERENCE_INPUT_REDUCE_STALE_AGING_MIN_TURN_GAP"); ok && n > 0 {
		cfg.Compression.OutputReduce.StaleReadAgingMinTurnGap = n
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_INPUT_REDUCE_OBSOLETE_PRUNE")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.BeTerseHintEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT_TEXT")); v != "" {
		cfg.Compression.OutputReduce.BeTerseHintText = v
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_ARCHIVE_RECOVERY_NOTE")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_ARCHIVE_RECOVERY_NOTE_TEXT")); v != "" {
		cfg.Compression.OutputReduce.ArchiveRecoveryNoteText = v
	}
	if n, ok := envIntOK("SLIMFERENCE_READ_DELTA_RECENT_FULL_PASS_TURNS"); ok && n >= 0 {
		cfg.Compression.OutputReduce.ReadDeltaRecentFullPassTurns = n
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_SAVINGS_POLICY")); v != "" {
		cfg.Compression.OutputReduce.CodexSavingsPolicyMode = v
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_CHUNK_DEDUP")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.CodexChunkDedupEnabled = b
		}
	}
	if n, ok := envIntOK("SLIMFERENCE_CODEX_CHUNK_DEDUP_MIN_BYTES"); ok && n >= 0 {
		cfg.Compression.OutputReduce.CodexChunkDedupMinBytes = n
	}
	if n, ok := envIntOK("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_SESSIONS"); ok && n >= 0 {
		cfg.Compression.OutputReduce.CodexChunkDedupMaxSessions = n
	}
	if n, ok := envIntOK("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_CHUNKS_PER_SESSION"); ok && n >= 0 {
		cfg.Compression.OutputReduce.CodexChunkDedupMaxChunksPerSession = n
	}
	if n, ok := envIntOK("SLIMFERENCE_CODEX_CHUNK_DEDUP_TTL_SECONDS"); ok && n >= 0 {
		cfg.Compression.OutputReduce.CodexChunkDedupTTLSeconds = n
	}
}

// validate checks that configuration values are within acceptable ranges.
func validate(cfg *Config) error {
	if cfg.Proxy.ListenPort < 1 || cfg.Proxy.ListenPort > 65535 {
		return fmt.Errorf("proxy.listen_port must be 1-65535, got %d", cfg.Proxy.ListenPort)
	}
	switch cfg.Proxy.DirectCodexWebSocketPolicy {
	case "", "tunnel", "force_https_fallback":
	default:
		return fmt.Errorf("proxy.direct_codex_websocket_policy must be tunnel/force_https_fallback, got %q", cfg.Proxy.DirectCodexWebSocketPolicy)
	}
	pc := cfg.Proxy.OpenAIPromptCache
	switch pc.PromptCacheKeyStrategy {
	case "", "off", "session", "model_session", "static":
	default:
		return fmt.Errorf("proxy.openai_prompt_cache.prompt_cache_key_strategy must be off/session/model_session/static, got %q", pc.PromptCacheKeyStrategy)
	}
	switch pc.Retention {
	case "", "off", "in_memory", "24h", "auto":
	default:
		return fmt.Errorf("proxy.openai_prompt_cache.retention must be off/in_memory/24h/auto, got %q", pc.Retention)
	}
	if pc.MinTokens < 0 {
		return fmt.Errorf("proxy.openai_prompt_cache.min_tokens must be >= 0, got %d", pc.MinTokens)
	}
	if pc.MaxRequestsPerKeyPerMinute < 0 {
		return fmt.Errorf("proxy.openai_prompt_cache.max_requests_per_key_per_minute must be >= 0, got %d", pc.MaxRequestsPerKeyPerMinute)
	}
	if cfg.Transparent.CertCacheSize < 0 {
		return fmt.Errorf("transparent.cert_cache_size must be >= 0, got %d", cfg.Transparent.CertCacheSize)
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
	switch strings.TrimSpace(t.PlannerLiveCorpusConfidence) {
	case "", "unknown", "low", "medium", "high":
	default:
		return fmt.Errorf("compression.tuning.planner_live_corpus_confidence must be unknown/low/medium/high, got %q", t.PlannerLiveCorpusConfidence)
	}
	mode := cfg.Secrets.Mode
	if mode != "redact" && mode != "warn" && mode != "block" && mode != "off" {
		return fmt.Errorf("secrets.mode must be redact/warn/block/off, got %q", mode)
	}
	if cfg.Filter.PassthroughMaxChars < 0 {
		return fmt.Errorf("filter.passthrough_max_chars must be >= 0, got %d", cfg.Filter.PassthroughMaxChars)
	}
	if cfg.Hooks.CodexPostToolTimeoutSeconds < 1 || cfg.Hooks.CodexPostToolTimeoutSeconds > 30 {
		return fmt.Errorf("hooks.codex_posttool_timeout_seconds must be 1-30, got %d", cfg.Hooks.CodexPostToolTimeoutSeconds)
	}
	if cfg.Hooks.CodexPostToolMinTokens < 0 {
		return fmt.Errorf("hooks.codex_posttool_min_tokens must be >= 0, got %d", cfg.Hooks.CodexPostToolMinTokens)
	}
	if cfg.Compression.Tuning.MidExchangeThresholdTokens < 0 {
		return fmt.Errorf("compression.tuning.mid_exchange_threshold_tokens must be >= 0, got %d", cfg.Compression.Tuning.MidExchangeThresholdTokens)
	}
	if cfg.Analytics.GainUSDPerMillionTokens < 0 {
		return fmt.Errorf("analytics.gain_usd_per_million_tokens must be >= 0, got %v", cfg.Analytics.GainUSDPerMillionTokens)
	}
	or := cfg.Compression.OutputReduce
	if or.Profile != "" && or.Profile != "auto" && or.Profile != "off" && or.Profile != "mild" && or.Profile != "standard" &&
		or.Profile != "aggressive" && or.Profile != "codex_aggressive" && or.Profile != "custom" &&
		or.Profile != "anthropic" && or.Profile != "openai" && or.Profile != "codex" && or.Profile != "noop" {
		return fmt.Errorf("compression.output_reduce.profile must be auto/off/mild/standard/aggressive/codex_aggressive/custom/legacy-provider, got %q", or.Profile)
	}
	if or.MaxAddedBytes < 0 {
		return fmt.Errorf("compression.output_reduce.max_added_bytes must be >= 0, got %d", or.MaxAddedBytes)
	}
	if or.MinInputTokens < 0 {
		return fmt.Errorf("compression.output_reduce.min_input_tokens must be >= 0, got %d", or.MinInputTokens)
	}
	if or.AutoDisableThreshold < 0 {
		return fmt.Errorf("compression.output_reduce.auto_disable_threshold must be >= 0, got %d", or.AutoDisableThreshold)
	}
	if or.AutoTuneMinSamples < 0 {
		return fmt.Errorf("compression.output_reduce.auto_tune_min_samples must be >= 0, got %d", or.AutoTuneMinSamples)
	}
	if or.MinNetSavingsPct < 0 {
		return fmt.Errorf("compression.output_reduce.min_net_savings_pct must be >= 0, got %v", or.MinNetSavingsPct)
	}
	if or.MaxFailureRateDelta < 0 || or.MaxFailureRateDelta > 1 {
		return fmt.Errorf("compression.output_reduce.max_failure_rate_delta must be 0.0-1.0, got %v", or.MaxFailureRateDelta)
	}
	if or.CooldownTurns < 0 {
		return fmt.Errorf("compression.output_reduce.cooldown_turns must be >= 0, got %d", or.CooldownTurns)
	}
	if len(or.ArchiveRecoveryNoteText) > 1000 {
		return fmt.Errorf("compression.output_reduce.archive_recovery_note_text must be <= 1000 bytes")
	}
	if or.ReadDeltaRecentFullPassTurns < 0 {
		return fmt.Errorf("compression.output_reduce.read_delta_recent_full_pass_turns must be >= 0, got %d", or.ReadDeltaRecentFullPassTurns)
	}
	if mode := strings.TrimSpace(or.CodexSavingsPolicyMode); mode != "" && mode != "off" && mode != "conservative" && mode != "safe" && mode != "auto" && mode != "max" && mode != "aggressive" {
		return fmt.Errorf("compression.output_reduce.codex_savings_policy_mode must be off/conservative/auto/max, got %q", or.CodexSavingsPolicyMode)
	}
	if or.CodexChunkDedupMinBytes < 0 {
		return fmt.Errorf("compression.output_reduce.codex_chunk_dedup_min_bytes must be >= 0, got %d", or.CodexChunkDedupMinBytes)
	}
	if or.CodexChunkDedupMaxSessions < 0 {
		return fmt.Errorf("compression.output_reduce.codex_chunk_dedup_max_sessions must be >= 0, got %d", or.CodexChunkDedupMaxSessions)
	}
	if or.CodexChunkDedupMaxChunksPerSession < 0 {
		return fmt.Errorf("compression.output_reduce.codex_chunk_dedup_max_chunks_per_session must be >= 0, got %d", or.CodexChunkDedupMaxChunksPerSession)
	}
	if or.CodexChunkDedupTTLSeconds < 0 {
		return fmt.Errorf("compression.output_reduce.codex_chunk_dedup_ttl_seconds must be >= 0, got %d", or.CodexChunkDedupTTLSeconds)
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

func envIntOK(key string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func envFloatOK(key string) (float64, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func envBoolOK(key string) (bool, bool) {
	return parseEnvBool(strings.TrimSpace(os.Getenv(key)))
}

func parseEnvBool(v string) (bool, bool) {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}
