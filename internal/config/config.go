// Package config handles loading, validating, and accessing Slimference configuration.
// Priority order: CLI flags > environment variables > config file > defaults.
package config

import (
	"encoding/json"
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
	Savings     SavingsConfig     `toml:"savings"`
	Logging     LoggingConfig     `toml:"logging"`
	Filter      FilterConfig      `toml:"filter"`
	Hooks       HooksConfig       `toml:"hooks"`
	Debug       DebugConfig       `toml:"debug"`
}

// SavingsConfig controls reporting assumptions for savings scorecards.
type SavingsConfig struct {
	// CachedPriceRatio is the provider-cache billing ratio used by savings
	// reports. OpenAI prompt-cache reads are commonly ~10% of uncached input.
	CachedPriceRatio float64 `toml:"cached_price_ratio"`
}

// DebugConfig holds debug and observability settings (docs/spec.md §13).
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

// HooksConfig affects generated hook scripts and agent-rule snippets.
type HooksConfig struct {
	// SlimferenceCommand is the executable name or path embedded in hooks (default "slimference").
	SlimferenceCommand string `toml:"slimference_command"`
	// ExcludeCommands is a list of base command names (argv[0]) that are
	// never rewritten by "slimference rewrite", regardless of filter rules.
	// Corresponds to [hooks] exclude_commands in config.toml (docs/spec.md §4.9).
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
	// PassthroughMaxChars caps filtered stdout length in Unicode code points after built-in/TOML (docs/spec.md §4.6).
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
	// which the Layer 1 pipeline is trusted. Requests
	// carrying an unknown version downgrade to conservative mode (T62).
	// An empty list means "trust everything" for backwards compatibility.
	AnthropicVersions []string `toml:"anthropic_versions"`
	// AnthropicUnknownBehavior decides how unknown-version requests are
	// handled: "conservative" skips non-essential reducers, "passthrough" runs no
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
	// OpenAIPromptCache controls OpenAI prompt-cache routing fields for
	// stable prefixes. It never applies to CodexChatGPT backend routes
	// until their live request contract is proven.
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

// CompressionConfig controls the deterministic compression pipeline.
type CompressionConfig struct {
	Layer1Enabled             bool               `toml:"layer1_enabled"`
	Layer2Enabled             bool               `toml:"layer2_enabled"`
	Layer2EnabledLegacy       bool               `toml:"layer3_enabled"`
	SlidingWindow             int                `toml:"sliding_window"`
	MinMessagesForCompression int                `toml:"min_messages_for_compression"`
	StructureMinTokens        int                `toml:"structure_min_tokens"`
	StructureLanguages        []string           `toml:"structure_languages"`
	DedupSimilarityThreshold  float64            `toml:"dedup_similarity_threshold"`
	OutputReduce              OutputReduceConfig `toml:"output_reduce"`
	Tuning                    TuningConfig       `toml:"tuning"`
}

// OutputReduceConfig controls Layer 3 output-token reduction through
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
	// ConciseChatEnabled injects a conservative user-facing chat style
	// hint only for direct-answer / explanation turns. It is intentionally
	// separate from the older A/B be-terse hint and full output-reduce
	// directive path: code, docs, JSON, command-output relay, repair,
	// planning, review, and tool-output turns full-pass.
	ConciseChatEnabled bool `toml:"concise_chat_enabled"`
	// ConciseChatMinInputTokens prevents the concise-chat hint from expanding
	// tiny prompts where the instruction overhead would dominate likely
	// output savings. Set 0 only for explicit operator experiments.
	ConciseChatMinInputTokens int `toml:"concise_chat_min_input_tokens"`
	// ConciseChatText overrides the default chat style hint. Empty falls
	// back to the built-in conservative wording.
	ConciseChatText string `toml:"concise_chat_text"`
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
	// tool_result content; the proxy then rewrites verbatim tool-output
	// echoes in the response into compact "[unchanged: <name>:L<from>-<to>]"
	// markers. Default true. Env override: SLIMFERENCE_OUTPUT_REDUCE_REPDET=0
	// disables.
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
	// CodexWSSToolOutputMutationEnabled is an experimental non-product
	// lab/proof switch for broad Codex WSS request-body mutation on tool
	// outputs outside the previous_response_id delta flow. It is not part of
	// the default product path. Delta mutation needs the env-only
	// CodexWSSDeltaToolOutputMutationLabEnabled override because live proof
	// showed that flow can poison server state and trigger follow-up 400s.
	CodexWSSToolOutputMutationEnabled bool `toml:"codex_wss_tool_output_mutation_enabled"`
	// CodexWSSDeltaToolOutputMutationLabEnabled is intentionally env-only:
	// SLIMFERENCE_CODEX_WSS_DELTA_TOOL_OUTPUT_MUTATION_LAB=1. It exists only
	// for reproducing T354 delta failures and must never be persisted.
	CodexWSSDeltaToolOutputMutationLabEnabled bool `toml:"-"`
	// CodexWSSHistoryMutationLabEnabled is intentionally env-only:
	// SLIMFERENCE_CODEX_WSS_HISTORY_MUTATION_LAB=1. It exists only for
	// proving whether reconnect/full-history history reducers can safely
	// survive the following downstream delta turn. It must never be persisted.
	CodexWSSHistoryMutationLabEnabled bool `toml:"-"`
	// CodexWSSStatefulToolPrefixElisionEnabled is a lab/proof switch only.
	// Tool schemas are model-facing capability context in Codex WSS; product
	// default keeps them on every request after live proof showed that eliding
	// them can suppress real command_execution tool calls.
	CodexWSSStatefulToolPrefixElisionEnabled bool `toml:"codex_wss_stateful_tool_prefix_elision_enabled"`
	// CodexWSSStatefulPrefixElisionProofEnabled is the legacy env-only proof
	// override: SLIMFERENCE_CODEX_WSS_STATEFUL_PREFIX_ELISION_PROOF=1. It may
	// force the guarded path during scoped proof runs but is never persisted.
	CodexWSSStatefulPrefixElisionProofEnabled bool `toml:"-"`
	// CodexSearchCapProofPath points at a versioned final release-proof-report
	// --json artifact. When the report passes the release minima and Codex route
	// hygiene proof, the selected search cap is promoted into the runtime search
	// compactor. Empty or stale unversioned final reports keep the product
	// default search compactor byte-identical.
	CodexSearchCapProofPath string `toml:"codex_search_cap_proof_path"`
	// CodexSearchCapMaxFiles and CodexSearchCapMaxMatchesPerFile are resolved
	// from the proof report above, or set directly by offline replay tools.
	// They are intentionally not persisted as raw config knobs.
	CodexSearchCapMaxFiles          int `toml:"-"`
	CodexSearchCapMaxMatchesPerFile int `toml:"-"`
	// CodexSearchCapDeltaMutationEnabled is resolved only from the final
	// proof latch. It enables the narrow named-search WSS delta mutation path
	// while the broad lab switch remains default-off.
	CodexSearchCapDeltaMutationEnabled bool `toml:"-"`
	// CodexChunkDedupEnabled gates T255 content-defined chunk dedup for
	// Codex tool outputs/file reads. This is the legacy explicit override;
	// the auto policy can enable chunk dedup without setting this field.
	CodexChunkDedupEnabled bool `toml:"codex_chunk_dedup_enabled"`
	// CodexChunkDedupProofLevel is the highest content-free proof level for
	// chunk dedup on the current installation: none, unit, replay, or live.
	// Auto policy requires live before it emits archive-backed chunk refs.
	CodexChunkDedupProofLevel string `toml:"codex_chunk_dedup_proof_level"`
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
	// CodexChunkDedupMaxReferencePercent caps how much of one model-facing
	// output may be replaced by chunk references. This preserves fresh
	// recency when an output is almost entirely old context.
	CodexChunkDedupMaxReferencePercent int `toml:"codex_chunk_dedup_max_reference_percent"`
	// CodexChunkDedupMaxSessionReferencePercent caps cumulative accepted
	// chunk references within one session.
	CodexChunkDedupMaxSessionReferencePercent int `toml:"codex_chunk_dedup_max_session_reference_percent"`
}

// TuningConfig centralises behaviour-visible numerical knobs that would
// otherwise be scattered as literals across deterministic compression hot
// paths. Every knob has a safe default; overrides live in config.toml under
// [compression.tuning].
type TuningConfig struct {
	// OverflowSlidingWindow is the aggressive sliding window used when the
	// upstream reports a context overflow (docs/spec.md §17.4). Default 2.
	OverflowSlidingWindow int `toml:"overflow_sliding_window"`
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
	// shape-aware preview when strictly shorter. Default true since T76 made
	// preview recovery archive-backed.
	StructurePreview bool `toml:"structure_preview"`
	// CoordinatorParallel (T104) enables automatic goroutine fan-out across
	// independent Layer-1 messages once the request has enough prefix work.
	// Small requests stay sequential to avoid goroutine overhead.
	CoordinatorParallel bool `toml:"coordinator_parallel"`
	// ToolPruneEnabled (T103) gates Layer 3 tool-definition pruning:
	// when on, tool definitions idle for more than
	// ToolPruneIdleThresholdTurns are removed from the request body
	// and archived for transparent reattachment. Default off.
	ToolPruneEnabled bool `toml:"tool_prune_enabled"`
	// WSSFullHistoryToolPruneEnabled enables the default-safe Codex WSS slice:
	// tool-prune may run on previous_response_id full-history resends only.
	// Root and steady delta WSS tool prefixes stay byte-equal unless the
	// broader ToolPruneEnabled operator flag is explicitly on.
	WSSFullHistoryToolPruneEnabled bool `toml:"wss_full_history_tool_prune_enabled"`
	// ToolPruneIdleThresholdTurns is the number of observed session turns a
	// tool can remain unused before it becomes prune-eligible. Default 20.
	ToolPruneIdleThresholdTurns int `toml:"tool_prune_idle_threshold_turns"`
	// ToolPruneAlwaysKeep extends the built-in always-keep class for
	// project-specific safety tools. Entries are exact tool names.
	ToolPruneAlwaysKeep []string `toml:"tool_prune_always_keep"`
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
	// ToolCompressor holds heuristic knobs that used to live as local
	// `const` declarations inside internal/compression/tool_compressor.go.
	// Exposing them via config unblocks data-driven tuning without a rebuild.
	// See T61.
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

// ToolCompressorTuning bundles heuristic thresholds for the type-aware
// tool-output compressor. Zero values fall back to the compile-time defaults
// so legacy configs keep byte-equal behaviour.
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

// CacheConfig controls response caching behaviour.
type CacheConfig struct {
	ResponseCacheMaxEntries int `toml:"response_cache_max_entries"`
	ResponseCacheTTLSeconds int `toml:"response_cache_ttl_seconds"`
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
// chain, runs env overrides, and validates the resulting Config. The returned
// LoadInfo identifies which source was used so callers can surface that to
// users.
func LoadWithOptions(opts LoadOptions) (*Config, LoadInfo, error) {
	cfg := defaultsRaw()
	info := ResolveConfigPath(opts)

	if info.Source == "flag_missing" {
		return nil, info, fmt.Errorf("config file not found: %s", info.ResolvedPath)
	}

	if info.ResolvedPath != "" {
		md, err := toml.DecodeFile(info.ResolvedPath, cfg)
		if err != nil {
			return nil, info, fmt.Errorf("parse config %s: %w", info.ResolvedPath, err)
		}
		applyConfigAliases(cfg, md)
	}

	applyEnvOverrides(cfg)

	if err := applyCodexSearchCapProof(cfg); err != nil {
		return nil, info, fmt.Errorf("invalid config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, info, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, info, nil
}

func applyConfigAliases(cfg *Config, md toml.MetaData) {
	if !md.IsDefined("compression", "layer2_enabled") && md.IsDefined("compression", "layer3_enabled") {
		cfg.Compression.Layer2Enabled = cfg.Compression.Layer2EnabledLegacy
	}
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
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CACHED_PRICE_RATIO")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Savings.CachedPriceRatio = f
		}
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
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_PROFILE")); v != "" {
		cfg.Compression.OutputReduce.Profile = v
	}
	if n, ok := envIntOK("SLIMFERENCE_OUTPUT_REDUCE_MIN_INPUT_TOKENS"); ok && n >= 0 {
		cfg.Compression.OutputReduce.MinInputTokens = n
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
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_CONCISE_CHAT")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.ConciseChatEnabled = b
		}
	}
	if n, ok := envIntOK("SLIMFERENCE_OUTPUT_REDUCE_CONCISE_CHAT_MIN_INPUT_TOKENS"); ok && n >= 0 {
		cfg.Compression.OutputReduce.ConciseChatMinInputTokens = n
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_OUTPUT_REDUCE_CONCISE_CHAT_TEXT")); v != "" {
		cfg.Compression.OutputReduce.ConciseChatText = v
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_WSS_TOOL_OUTPUT_MUTATION")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.CodexWSSToolOutputMutationEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_WSS_DELTA_TOOL_OUTPUT_MUTATION_LAB")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.CodexWSSDeltaToolOutputMutationLabEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_WSS_HISTORY_MUTATION_LAB")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.CodexWSSHistoryMutationLabEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_WSS_STATEFUL_TOOL_PREFIX_ELISION")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.CodexWSSStatefulToolPrefixElisionEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_WSS_STATEFUL_PREFIX_ELISION_PROOF")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.CodexWSSStatefulPrefixElisionProofEnabled = b
		}
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
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_SEARCH_CAP_PROOF_PATH")); v != "" {
		cfg.Compression.OutputReduce.CodexSearchCapProofPath = v
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_CHUNK_DEDUP")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.OutputReduce.CodexChunkDedupEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_PROOF_LEVEL")); v != "" {
		cfg.Compression.OutputReduce.CodexChunkDedupProofLevel = v
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
	if n, ok := envIntOK("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_REFERENCE_PERCENT"); ok && n >= 0 {
		cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = n
	}
	if n, ok := envIntOK("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_SESSION_REFERENCE_PERCENT"); ok && n >= 0 {
		cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = n
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_TOOL_PRUNE_ENABLED")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.Tuning.ToolPruneEnabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_WSS_FULL_HISTORY_TOOL_PRUNE")); v != "" {
		if b, ok := parseEnvBool(v); ok {
			cfg.Compression.Tuning.WSSFullHistoryToolPruneEnabled = b
		}
	}
	if n, ok := envIntOK("SLIMFERENCE_TOOL_PRUNE_IDLE_THRESHOLD_TURNS"); ok && n > 0 {
		cfg.Compression.Tuning.ToolPruneIdleThresholdTurns = n
	}
	if v := strings.TrimSpace(os.Getenv("SLIMFERENCE_TOOL_PRUNE_ALWAYS_KEEP")); v != "" {
		cfg.Compression.Tuning.ToolPruneAlwaysKeep = splitCommaEnv(v)
	}
}

func splitCommaEnv(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

const (
	codexSearchCapReleaseMinRetainedPct        = 40.0
	codexSearchCapReleaseMinSearchOutputs      = 2
	codexSearchCapReleaseMinExtraReducerTokens = 1
	codexSearchCapRequiredProofSchemaVersion   = 1
)

type codexSearchCapReleaseProofReport struct {
	ProofSchemaVersion          int                         `json:"proof_schema_version"`
	MatrixPath                  string                      `json:"matrix_path"`
	ResourceProfileProofOK      bool                        `json:"resource_profile_proof_ok"`
	ResourceProfileProofClients []string                    `json:"resource_profile_proof_clients"`
	ResourceProfileProofIssues  []string                    `json:"resource_profile_proof_issues"`
	MatrixFiles                 int                         `json:"matrix_files"`
	Rows                        int                         `json:"rows"`
	PositiveEconomicTokenRows   int                         `json:"positive_economic_token_rows"`
	HostBudgetIssueRows         int                         `json:"host_budget_issue_rows"`
	ProofEventLossRows          int                         `json:"proof_event_loss_rows"`
	SafetyIssueRows             int                         `json:"safety_issue_rows"`
	ExpectedZeroLocalViolations int                         `json:"expected_zero_local_violations"`
	MissingReleaseWorkloads     []string                    `json:"missing_release_workloads"`
	MissingMaxxWorkloads        []string                    `json:"missing_maxx_workloads"`
	GatePassed                  bool                        `json:"gate_passed"`
	GateFailures                []string                    `json:"gate_failures"`
	SearchCapProof              *codexSearchCapProofReport  `json:"search_cap_proof"`
	CodexRouteHygiene           *codexSearchCapRouteHygiene `json:"codex_route_hygiene"`
}

type codexSearchCapProofReport struct {
	Path                    string           `json:"path"`
	OK                      bool             `json:"ok"`
	Issues                  []string         `json:"issues"`
	Captures                int              `json:"captures"`
	CLI                     int              `json:"cli"`
	Desktop                 int              `json:"desktop"`
	PositiveSavings         int              `json:"positive_savings_captures"`
	SelectedCandidate       string           `json:"selected_candidate"`
	MaxFilesShown           int              `json:"max_files_shown"`
	MaxMatchesPerFile       int              `json:"max_matches_per_file"`
	TotalExtraReducerTokens int              `json:"total_extra_reducer_tokens"`
	MinMatchRetentionPct    float64          `json:"min_match_retention_pct"`
	DeltaToolOutputProof    bool             `json:"delta_tool_output_mutation_proof"`
	RequiredReducerHits     map[string]int64 `json:"required_reducer_hits"`
}

type codexSearchCapRouteHygiene struct {
	OK     bool     `json:"ok"`
	Before string   `json:"before"`
	After  string   `json:"after"`
	Issues []string `json:"issues"`
}

func applyCodexSearchCapProof(cfg *Config) error {
	or := &cfg.Compression.OutputReduce
	path := strings.TrimSpace(or.CodexSearchCapProofPath)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(expandHome(path))
	if err != nil {
		return fmt.Errorf("compression.output_reduce.codex_search_cap_proof_path read %q: %w", path, err)
	}
	var proof codexSearchCapReleaseProofReport
	if err := json.Unmarshal(data, &proof); err != nil {
		return fmt.Errorf("compression.output_reduce.codex_search_cap_proof_path parse %q: %w", path, err)
	}
	if proof.ProofSchemaVersion == 0 {
		if codexSearchCapLooksLikeFinalReleaseProof(proof) {
			return nil
		}
		return fmt.Errorf("compression.output_reduce.codex_search_cap_proof_path rejected %q: missing proof_schema_version on unsupported proof artifact", path)
	}
	if proof.ProofSchemaVersion < codexSearchCapRequiredProofSchemaVersion {
		return fmt.Errorf("compression.output_reduce.codex_search_cap_proof_path rejected %q: proof_schema_version %d < required %d",
			path, proof.ProofSchemaVersion, codexSearchCapRequiredProofSchemaVersion)
	}
	files, matches, issues := validateCodexSearchCapProof(proof)
	if len(issues) > 0 {
		return fmt.Errorf("compression.output_reduce.codex_search_cap_proof_path rejected %q: %s", path, strings.Join(issues, "; "))
	}
	or.CodexSearchCapMaxFiles = files
	or.CodexSearchCapMaxMatchesPerFile = matches
	or.CodexSearchCapDeltaMutationEnabled = true
	return nil
}

func codexSearchCapLooksLikeFinalReleaseProof(proof codexSearchCapReleaseProofReport) bool {
	return strings.TrimSpace(proof.MatrixPath) != "" ||
		proof.SearchCapProof != nil ||
		proof.CodexRouteHygiene != nil ||
		len(proof.ResourceProfileProofClients) > 0
}

func validateCodexSearchCapProof(proof codexSearchCapReleaseProofReport) (int, int, []string) {
	var issues []string
	if !proof.GatePassed {
		issues = append(issues, "final release-proof-report gate did not pass: "+strings.Join(proof.GateFailures, "; "))
	}
	if proof.GatePassed && len(proof.GateFailures) > 0 {
		issues = append(issues, "final release-proof-report gate passed but still contains gate failures: "+strings.Join(proof.GateFailures, "; "))
	}
	if strings.TrimSpace(proof.MatrixPath) == "" {
		issues = append(issues, "missing final release matrix_path")
	}
	if proof.MatrixFiles < 1 {
		issues = append(issues, fmt.Sprintf("expected at least 1 release matrix file, got %d", proof.MatrixFiles))
	}
	if proof.Rows < 1 {
		issues = append(issues, fmt.Sprintf("expected release proof rows, got %d", proof.Rows))
	}
	if proof.PositiveEconomicTokenRows < 1 {
		issues = append(issues, "no positive economic-token proof rows")
	}
	if !proof.ResourceProfileProofOK {
		issues = append(issues, "final release resource/profile proof did not pass: "+strings.Join(proof.ResourceProfileProofIssues, "; "))
	}
	if proof.ResourceProfileProofOK && len(proof.ResourceProfileProofIssues) > 0 {
		issues = append(issues, "final release resource/profile proof passed but still contains issues: "+strings.Join(proof.ResourceProfileProofIssues, "; "))
	}
	if !codexSearchCapReleaseHasClient(proof.ResourceProfileProofClients, "cli") {
		issues = append(issues, "missing final release CLI resource/profile proof")
	}
	if !codexSearchCapReleaseHasClient(proof.ResourceProfileProofClients, "desktop") {
		issues = append(issues, "missing final release Desktop resource/profile proof")
	}
	if proof.HostBudgetIssueRows > 0 {
		issues = append(issues, fmt.Sprintf("host budget issue rows=%d", proof.HostBudgetIssueRows))
	}
	if proof.ProofEventLossRows > 0 {
		issues = append(issues, fmt.Sprintf("proof event loss rows=%d", proof.ProofEventLossRows))
	}
	if proof.SafetyIssueRows > 0 {
		issues = append(issues, fmt.Sprintf("safety issue rows=%d", proof.SafetyIssueRows))
	}
	if proof.ExpectedZeroLocalViolations > 0 {
		issues = append(issues, fmt.Sprintf("expected-zero rows had local savings=%d", proof.ExpectedZeroLocalViolations))
	}
	if len(proof.MissingReleaseWorkloads) > 0 {
		issues = append(issues, "missing release workloads: "+strings.Join(proof.MissingReleaseWorkloads, ", "))
	}
	if len(proof.MissingMaxxWorkloads) > 0 {
		issues = append(issues, "missing maxx workloads: "+strings.Join(proof.MissingMaxxWorkloads, ", "))
	}
	if proof.SearchCapProof == nil {
		issues = append(issues, "missing final release search_cap_proof summary")
		return 0, 0, issues
	}
	if proof.CodexRouteHygiene == nil {
		issues = append(issues, "missing final release Codex route hygiene proof")
		return 0, 0, issues
	}
	searchProof := proof.SearchCapProof
	if !searchProof.OK {
		issues = append(issues, "final release search-cap proof did not pass: "+strings.Join(searchProof.Issues, "; "))
	}
	if searchProof.OK && len(searchProof.Issues) > 0 {
		issues = append(issues, "final release search-cap proof passed but still contains issues: "+strings.Join(searchProof.Issues, "; "))
	}
	if strings.TrimSpace(searchProof.Path) == "" {
		issues = append(issues, "missing final release focused search-cap proof path")
	}
	if !proof.CodexRouteHygiene.OK {
		issues = append(issues, "final release Codex route hygiene proof did not pass: "+strings.Join(proof.CodexRouteHygiene.Issues, "; "))
	}
	if proof.CodexRouteHygiene.OK && len(proof.CodexRouteHygiene.Issues) > 0 {
		issues = append(issues, "final release Codex route hygiene proof passed but still contains issues: "+strings.Join(proof.CodexRouteHygiene.Issues, "; "))
	}
	if strings.TrimSpace(proof.CodexRouteHygiene.Before) == "" {
		issues = append(issues, "missing final release Codex route hygiene before snapshot path")
	}
	if strings.TrimSpace(proof.CodexRouteHygiene.After) == "" {
		issues = append(issues, "missing final release Codex route hygiene after snapshot path")
	}
	if searchProof.Captures < 2 {
		issues = append(issues, fmt.Sprintf("expected at least 2 search_loop captures, got %d", searchProof.Captures))
	}
	if searchProof.CLI < 1 {
		issues = append(issues, "missing CLI search_loop capture")
	}
	if searchProof.Desktop < 1 {
		issues = append(issues, "missing Desktop search_loop capture")
	}
	if searchProof.PositiveSavings < 2 {
		issues = append(issues, fmt.Sprintf("expected at least 2 positive search-cap proof rows, got %d", searchProof.PositiveSavings))
	}
	if searchProof.MinMatchRetentionPct+1e-9 < codexSearchCapReleaseMinRetainedPct {
		issues = append(issues, fmt.Sprintf("selected search-cap candidate min retention %.2f%% < release min %.2f%%",
			searchProof.MinMatchRetentionPct, codexSearchCapReleaseMinRetainedPct))
	}
	if searchProof.TotalExtraReducerTokens <= 0 {
		issues = append(issues, fmt.Sprintf("total search-cap extra reducer tokens must be positive, got %+d", searchProof.TotalExtraReducerTokens))
	}
	if !searchProof.DeltaToolOutputProof {
		issues = append(issues, "missing final release product search-cap latch proof for selected search cap")
	}
	if searchProof.RequiredReducerHits["captured_output"] <= 0 {
		issues = append(issues, "missing final release captured_output reducer proof for selected search cap")
	}
	if strings.TrimSpace(searchProof.SelectedCandidate) == "" {
		issues = append(issues, "missing selected search-cap candidate name")
	}
	if searchProof.MaxFilesShown <= 0 || searchProof.MaxMatchesPerFile <= 0 {
		issues = append(issues, fmt.Sprintf("selected search-cap candidate has invalid cap %d/%d",
			searchProof.MaxFilesShown, searchProof.MaxMatchesPerFile))
	}
	return searchProof.MaxFilesShown, searchProof.MaxMatchesPerFile, issues
}

func codexSearchCapReleaseHasClient(clients []string, want string) bool {
	for _, client := range clients {
		if strings.TrimSpace(client) == want {
			return true
		}
	}
	return false
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
	case "", "off", "stable_prefix", "model_stable_prefix", "session", "model_session", "static":
	default:
		return fmt.Errorf("proxy.openai_prompt_cache.prompt_cache_key_strategy must be off/stable_prefix/model_stable_prefix/session/model_session/static, got %q", pc.PromptCacheKeyStrategy)
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
	if t.OverflowSlidingWindow < 1 {
		return fmt.Errorf("compression.tuning.overflow_sliding_window must be >= 1")
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
	if cfg.Analytics.GainUSDPerMillionTokens < 0 {
		return fmt.Errorf("analytics.gain_usd_per_million_tokens must be >= 0, got %v", cfg.Analytics.GainUSDPerMillionTokens)
	}
	if cfg.Savings.CachedPriceRatio < 0 || cfg.Savings.CachedPriceRatio > 1 {
		return fmt.Errorf("savings.cached_price_ratio must be between 0 and 1, got %v", cfg.Savings.CachedPriceRatio)
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
	if len(or.ConciseChatText) > 1000 {
		return fmt.Errorf("compression.output_reduce.concise_chat_text must be <= 1000 bytes")
	}
	if or.ConciseChatMinInputTokens < 0 {
		return fmt.Errorf("compression.output_reduce.concise_chat_min_input_tokens must be >= 0, got %d", or.ConciseChatMinInputTokens)
	}
	if or.ReadDeltaRecentFullPassTurns < 0 {
		return fmt.Errorf("compression.output_reduce.read_delta_recent_full_pass_turns must be >= 0, got %d", or.ReadDeltaRecentFullPassTurns)
	}
	if or.CodexSearchCapMaxFiles < 0 {
		return fmt.Errorf("compression.output_reduce.codex_search_cap_max_files must be >= 0, got %d", or.CodexSearchCapMaxFiles)
	}
	if or.CodexSearchCapMaxMatchesPerFile < 0 {
		return fmt.Errorf("compression.output_reduce.codex_search_cap_max_matches_per_file must be >= 0, got %d", or.CodexSearchCapMaxMatchesPerFile)
	}
	if mode := strings.TrimSpace(or.CodexSavingsPolicyMode); mode != "" && mode != "off" && mode != "conservative" && mode != "safe" && mode != "auto" && mode != "max" && mode != "aggressive" {
		return fmt.Errorf("compression.output_reduce.codex_savings_policy_mode must be off/conservative/auto/max, got %q", or.CodexSavingsPolicyMode)
	}
	switch strings.ToLower(strings.TrimSpace(or.CodexChunkDedupProofLevel)) {
	case "", "none", "unit", "replay", "live":
	default:
		return fmt.Errorf("compression.output_reduce.codex_chunk_dedup_proof_level must be none/unit/replay/live, got %q", or.CodexChunkDedupProofLevel)
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
	if or.CodexChunkDedupMaxReferencePercent < 0 || or.CodexChunkDedupMaxReferencePercent > 100 {
		return fmt.Errorf("compression.output_reduce.codex_chunk_dedup_max_reference_percent must be between 0 and 100, got %d", or.CodexChunkDedupMaxReferencePercent)
	}
	if or.CodexChunkDedupMaxSessionReferencePercent < 0 || or.CodexChunkDedupMaxSessionReferencePercent > 100 {
		return fmt.Errorf("compression.output_reduce.codex_chunk_dedup_max_session_reference_percent must be between 0 and 100, got %d", or.CodexChunkDedupMaxSessionReferencePercent)
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
