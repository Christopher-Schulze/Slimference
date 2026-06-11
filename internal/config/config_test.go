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

func TestDefaults_TransparentConfig(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	if cfg.Proxy.DirectCodexWebSocketPolicy != "tunnel" {
		t.Fatalf("direct codex websocket policy = %q", cfg.Proxy.DirectCodexWebSocketPolicy)
	}
	if cfg.Transparent.Enabled {
		t.Fatal("transparent mode must be opt-in by default")
	}
	if !cfg.Transparent.ScopedDesktopProxy {
		t.Fatal("scoped desktop proxy should default on for process-local launcher support")
	}
	if cfg.Transparent.CertCacheSize != 256 {
		t.Fatalf("cert cache size = %d, want 256", cfg.Transparent.CertCacheSize)
	}
	if cfg.Transparent.DefaultTLSProfile != "chromium_stable" {
		t.Fatalf("default tls profile = %q", cfg.Transparent.DefaultTLSProfile)
	}
	if len(cfg.Transparent.InterceptHosts) != 3 {
		t.Fatalf("intercept hosts = %v", cfg.Transparent.InterceptHosts)
	}
}

func TestDefaults_OutputReduceConfig(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	if !cfg.Compression.OutputReduce.Enabled {
		t.Fatal("output-reduce should default on with the min-token gate")
	}
	if cfg.Compression.OutputReduce.Profile != "auto" {
		t.Fatalf("profile = %q", cfg.Compression.OutputReduce.Profile)
	}
	if cfg.Compression.OutputReduce.MinInputTokens != 400 {
		t.Fatalf("min input tokens = %d", cfg.Compression.OutputReduce.MinInputTokens)
	}
	if !cfg.Compression.OutputReduce.AutoTuneEnabled || cfg.Compression.OutputReduce.AutoTuneMinSamples != 30 {
		t.Fatalf("auto tune defaults = %+v", cfg.Compression.OutputReduce)
	}
	if cfg.Compression.OutputReduce.SignatureMarker == "" {
		t.Fatal("signature marker must be non-empty")
	}
	if !cfg.Compression.OutputReduce.ConciseChatEnabled {
		t.Fatal("concise chat hint should default on")
	}
	if cfg.Compression.OutputReduce.ConciseChatMinInputTokens != 400 {
		t.Fatalf("concise chat min input tokens = %d", cfg.Compression.OutputReduce.ConciseChatMinInputTokens)
	}
	if cfg.Compression.OutputReduce.ArchiveRecoveryNoteEnabled {
		t.Fatal("explicit archive recovery note toggle must stay default-off; auto policy injects it only when needed")
	}
	if cfg.Compression.OutputReduce.ReadDeltaRecentFullPassTurns != 0 {
		t.Fatalf("read-delta recency full-pass default = %d, want 0", cfg.Compression.OutputReduce.ReadDeltaRecentFullPassTurns)
	}
	if cfg.Compression.OutputReduce.CodexSavingsPolicyMode != "auto" {
		t.Fatalf("Codex savings policy default = %q, want auto", cfg.Compression.OutputReduce.CodexSavingsPolicyMode)
	}
	if cfg.Compression.OutputReduce.CodexChunkDedupEnabled {
		t.Fatal("explicit Codex chunk dedup override must stay default-off")
	}
	if cfg.Compression.OutputReduce.CodexChunkDedupProofLevel != "live" {
		t.Fatalf("Codex chunk dedup proof level default = %q, want live", cfg.Compression.OutputReduce.CodexChunkDedupProofLevel)
	}
	if cfg.Compression.OutputReduce.CodexChunkDedupMinBytes != 4096 ||
		cfg.Compression.OutputReduce.CodexChunkDedupMaxSessions != 256 ||
		cfg.Compression.OutputReduce.CodexChunkDedupMaxChunksPerSession != 8192 ||
		cfg.Compression.OutputReduce.CodexChunkDedupTTLSeconds != 14400 ||
		cfg.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent != 90 ||
		cfg.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent != 70 {
		t.Fatalf("Codex chunk dedup defaults mismatch: %+v", cfg.Compression.OutputReduce)
	}
}

func TestDefaults_Layer1CoordinatorParallelAuto(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	if !cfg.Compression.Tuning.CoordinatorParallel {
		t.Fatal("Layer-1 coordinator parallel auto-gate should default on")
	}
}

func TestApplyEnvHooksDebug(t *testing.T) {
	t.Setenv("SLIMFERENCE_HOOK_SLIMFERENCE_COMMAND", "/opt/bin/slimference")
	t.Setenv("SLIMFERENCE_CODEX_POSTTOOL_TIMEOUT_SECONDS", "3")
	t.Setenv("SLIMFERENCE_CODEX_POSTTOOL_MIN_TOKENS", "600")
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", "~/d/decisions.jsonl")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Hooks.SlimferenceCommand != "/opt/bin/slimference" {
		t.Fatalf("hooks command: %q", cfg.Hooks.SlimferenceCommand)
	}
	if cfg.Hooks.CodexPostToolTimeoutSeconds != 3 {
		t.Fatalf("posttool timeout: %d", cfg.Hooks.CodexPostToolTimeoutSeconds)
	}
	if cfg.Hooks.CodexPostToolMinTokens != 600 {
		t.Fatalf("posttool min tokens: %d", cfg.Hooks.CodexPostToolMinTokens)
	}
	if cfg.Debug.DecisionsLog != "~/d/decisions.jsonl" {
		t.Fatalf("decisions log: %q", cfg.Debug.DecisionsLog)
	}
}

func TestApplyEnvDebugAndOutputReduceKnobs(t *testing.T) {
	t.Setenv("SLIMFERENCE_DEBUG_LEVEL", "trace")
	t.Setenv("SLIMFERENCE_DEBUG_FORMAT", "json")
	t.Setenv("SLIMFERENCE_DEBUG_MAX_ENTRIES", "42")
	t.Setenv("SLIMFERENCE_INPUT_REDUCE_STALE_AGING", "on")
	t.Setenv("SLIMFERENCE_INPUT_REDUCE_STALE_AGING_MIN_TURN_GAP", "9")
	t.Setenv("SLIMFERENCE_INPUT_REDUCE_OBSOLETE_PRUNE", "yes")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_PROFILE", "codex_aggressive")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_MIN_INPUT_TOKENS", "123")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT", "true")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT_TEXT", "be terse")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_CONCISE_CHAT", "false")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_CONCISE_CHAT_MIN_INPUT_TOKENS", "77")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_CONCISE_CHAT_TEXT", "answer tight")
	t.Setenv("SLIMFERENCE_ARCHIVE_RECOVERY_NOTE", "true")
	t.Setenv("SLIMFERENCE_ARCHIVE_RECOVERY_NOTE_TEXT", "request archive ids")
	t.Setenv("SLIMFERENCE_READ_DELTA_RECENT_FULL_PASS_TURNS", "2")
	t.Setenv("SLIMFERENCE_CODEX_SAVINGS_POLICY", "max")
	t.Setenv("SLIMFERENCE_CODEX_WSS_TOOL_OUTPUT_MUTATION", "true")
	t.Setenv("SLIMFERENCE_CODEX_WSS_DELTA_TOOL_OUTPUT_MUTATION_LAB", "true")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP", "true")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_PROOF_LEVEL", "replay")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_MIN_BYTES", "4096")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_SESSIONS", "12")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_CHUNKS_PER_SESSION", "34")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_TTL_SECONDS", "56")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_REFERENCE_PERCENT", "78")
	t.Setenv("SLIMFERENCE_CODEX_CHUNK_DEDUP_MAX_SESSION_REFERENCE_PERCENT", "67")
	t.Setenv("SLIMFERENCE_TOOL_PRUNE_ENABLED", "true")
	t.Setenv("SLIMFERENCE_TOOL_PRUNE_IDLE_THRESHOLD_TURNS", "3")
	t.Setenv("SLIMFERENCE_TOOL_PRUNE_ALWAYS_KEEP", "shell, read_file,  write_file ")

	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Debug.Level != "trace" || cfg.Debug.Format != "json" || cfg.Debug.MaxEntries != 42 {
		t.Fatalf("debug env not applied: %+v", cfg.Debug)
	}
	or := cfg.Compression.OutputReduce
	if or.Profile != "codex_aggressive" || or.MinInputTokens != 123 ||
		!or.StaleReadAgingEnabled || or.StaleReadAgingMinTurnGap != 9 ||
		!or.ObsoleteReadPruneEnabled || !or.BeTerseHintEnabled || or.BeTerseHintText != "be terse" ||
		or.ConciseChatEnabled || or.ConciseChatMinInputTokens != 77 || or.ConciseChatText != "answer tight" ||
		!or.ArchiveRecoveryNoteEnabled || or.ArchiveRecoveryNoteText != "request archive ids" ||
		or.ReadDeltaRecentFullPassTurns != 2 || or.CodexSavingsPolicyMode != "max" || !or.CodexChunkDedupEnabled ||
		or.CodexChunkDedupProofLevel != "replay" ||
		or.CodexChunkDedupMinBytes != 4096 || or.CodexChunkDedupMaxSessions != 12 ||
		or.CodexChunkDedupMaxChunksPerSession != 34 || or.CodexChunkDedupTTLSeconds != 56 ||
		or.CodexChunkDedupMaxReferencePercent != 78 ||
		or.CodexChunkDedupMaxSessionReferencePercent != 67 ||
		!or.CodexWSSToolOutputMutationEnabled || !or.CodexWSSDeltaToolOutputMutationLabEnabled {
		t.Fatalf("output-reduce env not applied: %+v", or)
	}
	if !cfg.Compression.Tuning.ToolPruneEnabled ||
		cfg.Compression.Tuning.ToolPruneIdleThresholdTurns != 3 ||
		len(cfg.Compression.Tuning.ToolPruneAlwaysKeep) != 3 ||
		cfg.Compression.Tuning.ToolPruneAlwaysKeep[1] != "read_file" {
		t.Fatalf("tool-prune env not applied: %+v", cfg.Compression.Tuning)
	}
}

func TestApplyEnvOutputReduceToggles(t *testing.T) {
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS", "0")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT", "false")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_REPDET", "no")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	or := cfg.Compression.OutputReduce
	if or.StopSequencesEnabled {
		t.Errorf("stop sequences not disabled by env")
	}
	if or.StreamCutEnabled {
		t.Errorf("streamcut not disabled by env")
	}
	if or.RepetitionDetectionEnabled {
		t.Errorf("repdet not disabled by env")
	}
}

func TestApplyEnvOutputReduceTogglesEnable(t *testing.T) {
	// Defaults are true; env value "1" should keep them true.
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS", "1")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT", "true")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_REPDET", "yes")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	or := cfg.Compression.OutputReduce
	if !or.StopSequencesEnabled || !or.StreamCutEnabled || !or.RepetitionDetectionEnabled {
		t.Errorf("env enable should leave defaults on: %+v", or)
	}
}

func TestApplyEnvOutputReduceTogglesGarbage(t *testing.T) {
	// Unparseable env values must leave defaults intact.
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS", "xyzzy")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT", "garbage")
	t.Setenv("SLIMFERENCE_OUTPUT_REDUCE_REPDET", "??")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	or := cfg.Compression.OutputReduce
	if !or.StopSequencesEnabled || !or.StreamCutEnabled || !or.RepetitionDetectionEnabled {
		t.Errorf("unparseable env should leave defaults on: %+v", or)
	}
}

func TestDefaultsEnableOutputReduceToggles(t *testing.T) {
	d := Defaults()
	or := d.Compression.OutputReduce
	if !or.StopSequencesEnabled || !or.StreamCutEnabled || !or.RepetitionDetectionEnabled {
		t.Errorf("defaults should enable all output-reduce toggles, got %+v", or)
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

func TestApplyEnvCachedPriceRatio(t *testing.T) {
	t.Setenv("SLIMFERENCE_CACHED_PRICE_RATIO", "0.25")
	cfg := Defaults()
	applyEnvOverrides(cfg)
	if cfg.Savings.CachedPriceRatio != 0.25 {
		t.Fatalf("cached price ratio: %v", cfg.Savings.CachedPriceRatio)
	}
}

func TestEnvPrimitiveParsers(t *testing.T) {
	t.Setenv("CFG_INT_OK", " 12 ")
	if got, ok := envIntOK("CFG_INT_OK"); !ok || got != 12 {
		t.Fatalf("envIntOK=%d,%v want 12,true", got, ok)
	}
	t.Setenv("CFG_INT_BAD", "nope")
	if _, ok := envIntOK("CFG_INT_BAD"); ok {
		t.Fatal("bad int should not parse")
	}
	t.Setenv("CFG_FLOAT_OK", "2.75")
	if got, ok := envFloatOK("CFG_FLOAT_OK"); !ok || got != 2.75 {
		t.Fatalf("envFloatOK=%v,%v want 2.75,true", got, ok)
	}
	t.Setenv("CFG_FLOAT_BAD", "nope")
	if _, ok := envFloatOK("CFG_FLOAT_BAD"); ok {
		t.Fatal("bad float should not parse")
	}
	if _, ok := envFloatOK("CFG_FLOAT_EMPTY"); ok {
		t.Fatal("empty float should not parse")
	}
	t.Setenv("CFG_BOOL_ON", "yes")
	if got, ok := envBoolOK("CFG_BOOL_ON"); !ok || !got {
		t.Fatalf("envBoolOK yes=%v,%v want true,true", got, ok)
	}
	t.Setenv("CFG_BOOL_BAD", "")
	if _, ok := envBoolOK("CFG_BOOL_BAD"); ok {
		t.Fatal("empty bool should not parse")
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

func TestValidate_InvalidTransparentCertCacheSize(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Transparent.CertCacheSize = -1
	if err := validate(cfg); err == nil {
		t.Fatal("validate() with negative transparent cert cache expected error")
	}
}

func TestValidate_InvalidDirectCodexWebSocketPolicy(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Proxy.DirectCodexWebSocketPolicy = "rewrite_frames"
	if err := validate(cfg); err == nil {
		t.Fatal("validate() with invalid direct codex websocket policy expected error")
	}
}

func TestValidate_InvalidOpenAIPromptCacheConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"strategy", func(c *Config) { c.Proxy.OpenAIPromptCache.PromptCacheKeyStrategy = "workspace" }},
		{"retention", func(c *Config) { c.Proxy.OpenAIPromptCache.Retention = "forever" }},
		{"min_tokens", func(c *Config) { c.Proxy.OpenAIPromptCache.MinTokens = -1 }},
		{"rate_limit", func(c *Config) { c.Proxy.OpenAIPromptCache.MaxRequestsPerKeyPerMinute = -1 }},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := Defaults()
			tc.mutate(cfg)
			if err := validate(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidate_InvalidOutputReduceConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"profile", func(c *Config) { c.Compression.OutputReduce.Profile = "bad" }},
		{"max_added", func(c *Config) { c.Compression.OutputReduce.MaxAddedBytes = -1 }},
		{"min_input", func(c *Config) { c.Compression.OutputReduce.MinInputTokens = -1 }},
		{"threshold", func(c *Config) { c.Compression.OutputReduce.AutoDisableThreshold = -1 }},
		{"auto_samples", func(c *Config) { c.Compression.OutputReduce.AutoTuneMinSamples = -1 }},
		{"min_savings", func(c *Config) { c.Compression.OutputReduce.MinNetSavingsPct = -1 }},
		{"failure_rate", func(c *Config) { c.Compression.OutputReduce.MaxFailureRateDelta = 2 }},
		{"cooldown", func(c *Config) { c.Compression.OutputReduce.CooldownTurns = -1 }},
		{"archive_note_text", func(c *Config) { c.Compression.OutputReduce.ArchiveRecoveryNoteText = strings.Repeat("x", 1001) }},
		{"concise_chat_text", func(c *Config) { c.Compression.OutputReduce.ConciseChatText = strings.Repeat("x", 1001) }},
		{"concise_chat_min_input", func(c *Config) { c.Compression.OutputReduce.ConciseChatMinInputTokens = -1 }},
		{"read_delta_recency", func(c *Config) { c.Compression.OutputReduce.ReadDeltaRecentFullPassTurns = -1 }},
		{"codex_policy", func(c *Config) { c.Compression.OutputReduce.CodexSavingsPolicyMode = "reckless" }},
		{"chunk_proof_level", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupProofLevel = "rumor" }},
		{"chunk_min", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupMinBytes = -1 }},
		{"chunk_sessions", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupMaxSessions = -1 }},
		{"chunk_per_session", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupMaxChunksPerSession = -1 }},
		{"chunk_ttl", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupTTLSeconds = -1 }},
		{"chunk_reference_percent_low", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = -1 }},
		{"chunk_reference_percent_high", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupMaxReferencePercent = 101 }},
		{"chunk_session_reference_percent_low", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = -1 }},
		{"chunk_session_reference_percent_high", func(c *Config) { c.Compression.OutputReduce.CodexChunkDedupMaxSessionReferencePercent = 101 }},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := Defaults()
			tc.mutate(cfg)
			if err := validate(cfg); err == nil {
				t.Fatal("expected validation error")
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

// TestValidate_InvalidTuning verifies the new [compression.tuning] range
// checks (T22) reject out-of-range values.
func TestValidate_InvalidTuning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		apply func(c *Config)
	}{
		{"overflow_sliding_window zero", func(c *Config) {
			c.Compression.Tuning.OverflowSlidingWindow = 0
		}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := Defaults()
			tc.apply(cfg)
			if err := validate(cfg); err == nil {
				t.Errorf("validate() expected error for %s, got nil", tc.name)
			}
		})
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
	for _, obsolete := range []string{
		"min_tokens_for_" + "layer" + "2",
		"[compression." + "summary]",
		"[compression." + "mini" + "max]",
		"[compression." + "oc" + "rl]",
		"oc" + "rl",
		"Mini" + "Max",
	} {
		if strings.Contains(out, obsolete) {
			t.Fatalf("DefaultTOML() exposes retired compression surface %q", obsolete)
		}
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

func TestValidateCodexPostToolHookKnobs(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Hooks.CodexPostToolTimeoutSeconds = 0
	if err := validate(cfg); err == nil {
		t.Fatal("expected error for zero posttool timeout")
	}
	cfg = Defaults()
	cfg.Hooks.CodexPostToolTimeoutSeconds = 31
	if err := validate(cfg); err == nil {
		t.Fatal("expected error for too-large posttool timeout")
	}
	cfg = Defaults()
	cfg.Hooks.CodexPostToolMinTokens = -1
	if err := validate(cfg); err == nil {
		t.Fatal("expected error for negative posttool min tokens")
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

func TestValidateCachedPriceRatioBounds(t *testing.T) {
	t.Parallel()
	for _, ratio := range []float64{-0.01, 1.01} {
		cfg := Defaults()
		cfg.Savings.CachedPriceRatio = ratio
		if err := validate(cfg); err == nil {
			t.Fatalf("expected error for cached price ratio %v", ratio)
		}
	}
}

// TestApplyEnvOverrides_UpstreamAndCompression covers upstream URL and sliding window overrides.
func TestApplyEnvOverrides_UpstreamAndCompression(t *testing.T) {
	t.Setenv("SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL", "https://custom.anthropic.com")
	t.Setenv("SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL", "https://custom.openai.com")
	t.Setenv("SLIMFERENCE_UPSTREAM_CODEX_CHATGPT_BASE_URL", "https://custom.chatgpt.com")
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
	if cfg.Upstream.CodexChatGPT.BaseURL != "https://custom.chatgpt.com" {
		t.Errorf("Codex ChatGPT URL: %q", cfg.Upstream.CodexChatGPT.BaseURL)
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

func TestExpandHome_BareTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := expandHome("~"); got != home {
		t.Fatalf("bare tilde = %q, want %q", got, home)
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

func TestExpandHome_BareTildeHomeDirError(t *testing.T) {
	old := userHomeDirFunc
	userHomeDirFunc = func() (string, error) { return "", errors.New("no home") }
	defer func() { userHomeDirFunc = old }()

	if got := expandHome("~"); got != "~" {
		t.Fatalf("want bare tilde unchanged on error, got %q", got)
	}
}

func TestValidate_PlannerLiveCorpusConfidence(t *testing.T) {
	cfg := Defaults()
	cfg.Compression.Tuning.PlannerLiveCorpusConfidence = "high"
	if err := validate(cfg); err != nil {
		t.Fatalf("valid planner confidence rejected: %v", err)
	}
	cfg.Compression.Tuning.PlannerLiveCorpusConfidence = "fake"
	err := validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid planner confidence")
	}
	if !strings.Contains(err.Error(), "planner_live_corpus_confidence") {
		t.Fatalf("unexpected error: %v", err)
	}
}
