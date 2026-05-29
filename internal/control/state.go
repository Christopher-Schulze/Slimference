// Package control aggregates per-component state into a single
// SetupState snapshot consumed by the TUI (T191) and the admin HTTP
// surface. Sub-systems (CA, daemon, hosts file, per-app policy,
// savings) provide probes; this package composes them.
//
// The package is split across files:
//
//   - state.go: the SetupState struct + the Probe interfaces.
//   - probe_*.go: concrete probe implementations.
//   - state_builder.go: the Build() composer used by callers.
package control

import (
	"context"
	"time"

	"github.com/slimference/slimference/internal/control/apps"
	"github.com/slimference/slimference/internal/control/reversibility"
)

// SetupState is the aggregate snapshot returned to the TUI / admin
// endpoint. Every sub-block is independently fillable; missing data
// fills with the zero value so the renderer can show "unknown".
type SetupState struct {
	CA           CAState         `json:"ca"`
	Daemon       DaemonState     `json:"daemon"`
	Listener     ListenerState   `json:"listener"`
	NetworkRedir NetworkState    `json:"network_redirect"`
	Indist       IndistState     `json:"indistinguishability"`
	Apps         []AppEntry      `json:"apps"`
	CodexRoute   CodexRouteState `json:"codex_route"`
	Savings      SavingsSummary  `json:"savings"`
	WSS          WSSState        `json:"wss"`
	Preflight    PreflightState  `json:"preflight"`
	UpdatedAt    time.Time       `json:"updated_at"`
	LastError    string          `json:"last_error,omitempty"`
}

// CAState reports the local Slimference Root CA.
type CAState struct {
	Installed       bool      `json:"installed"`
	InKeychain      bool      `json:"in_keychain"`
	Fingerprint     string    `json:"fingerprint"`
	NotBefore       time.Time `json:"not_before"`
	NotAfter        time.Time `json:"not_after"`
	DaysUntilExpiry int       `json:"days_until_expiry"`
}

// DaemonState reports the proxy daemon liveness.
type DaemonState struct {
	Installed bool   `json:"installed"`
	Autostart bool   `json:"autostart"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	HealthOK  bool   `json:"health_ok"`
	RSSBytes  int64  `json:"rss_bytes"`
	UptimeSec int64  `json:"uptime_sec"`
	Version   string `json:"version"`
}

// ListenerState reports the transparent :443 listener readiness.
type ListenerState struct {
	BoundOn443     bool   `json:"bound_on_443"`
	BoundOn8990    bool   `json:"bound_on_8990"`
	BoundOnSNIPeek bool   `json:"bound_on_sni_peek"`
	Method         string `json:"method"` // privileged-port | pfctl-rdr | alt-port
}

// NetworkState reports the DNS / hosts / pfctl redirect state.
type NetworkState struct {
	HostsActive  bool     `json:"hosts_active"`
	HostsEntries []string `json:"hosts_entries"`
	PFCtlActive  bool     `json:"pfctl_active"`
	PFCtlRules   []string `json:"pfctl_rules"`
}

// PreflightState reports local readiness checks that are cheap and
// safe to run before live arming. These checks do not mutate system
// routes, trust stores, app config, or daemon state.
type PreflightState struct {
	DoH []DoHPreflightEntry `json:"doh"`
}

// DoHPreflightEntry reports whether a Codex host resolves through the
// daemon's DoH bypass path to a usable non-loopback upstream IP.
type DoHPreflightEntry struct {
	Host     string `json:"host"`
	OK       bool   `json:"ok"`
	IP       string `json:"ip,omitempty"`
	Loopback bool   `json:"loopback,omitempty"`
	Error    string `json:"error,omitempty"`
}

// IndistState reports the T190 indistinguishability proof status.
type IndistState struct {
	GoldenLocked bool      `json:"golden_locked"`
	GoldenSHA    string    `json:"golden_sha"`
	LastVerified time.Time `json:"last_verified"`
	Drift        []string  `json:"drift,omitempty"`
}

// AppEntry rolls up per-app integration status for one of the
// known applications (Codex CLI / Codex Desktop App / Claude Code).
type AppEntry struct {
	ID       apps.AppID `json:"id"`
	Enabled  bool       `json:"enabled"`
	Detected bool       `json:"detected"`
	BinPath  string     `json:"bin_path,omitempty"`
	Routed   int64      `json:"routed"`
	Bypassed int64      `json:"bypassed"`
	LastSeen time.Time  `json:"last_seen,omitempty"`
}

// CodexRouteState reports the scoped, marker-owned Codex provider route.
// This is the product traffic path for Codex CLI / Codex Desktop App; it
// is intentionally separate from NetworkState, which describes the
// global lab-only /etc/hosts + pfctl surface.
type CodexRouteState struct {
	Path                        string    `json:"path"`
	Exists                      bool      `json:"exists"`
	Enabled                     bool      `json:"enabled"`
	Complete                    bool      `json:"complete"`
	Conflict                    string    `json:"conflict,omitempty"`
	LegacyKeys                  bool      `json:"legacy_keys"`
	BaseURL                     string    `json:"base_url"`
	Transport                   string    `json:"transport"`
	DaemonReachable             bool      `json:"daemon_reachable"`
	DaemonError                 string    `json:"daemon_error,omitempty"`
	AutoTransport               string    `json:"auto_transport"`
	AutoMode                    string    `json:"auto_mode"`
	WSSCertified                bool      `json:"wss_certified"`
	WSSBridgeAvailable          bool      `json:"wss_bridge_available"`
	NeedsRecert                 bool      `json:"needs_recert"`
	CertifiedCodexVersion       string    `json:"certified_codex_version,omitempty"`
	CertifiedSlimferenceVersion string    `json:"certified_slimference_version,omitempty"`
	BridgeCodexVersion          string    `json:"bridge_codex_version,omitempty"`
	BridgeSlimferenceVersion    string    `json:"bridge_slimference_version,omitempty"`
	CertificationPath           string    `json:"certification_path,omitempty"`
	BridgeProofPath             string    `json:"bridge_proof_path,omitempty"`
	RecertStatePath             string    `json:"recert_state_path,omitempty"`
	RecertLogPath               string    `json:"recert_log_path,omitempty"`
	RecertStatus                string    `json:"recert_status,omitempty"`
	RecertAttemptID             string    `json:"recert_attempt_id,omitempty"`
	RecertStartedAt             time.Time `json:"recert_started_at,omitempty"`
	RecertFinishedAt            time.Time `json:"recert_finished_at,omitempty"`
	RecertLastSuccessAt         time.Time `json:"recert_last_success_at,omitempty"`
	RecertRetryAfter            time.Time `json:"recert_retry_after,omitempty"`
	RecertLastError             string    `json:"recert_last_error,omitempty"`
	RecertCommand               string    `json:"recert_command,omitempty"`
	FallbackReason              string    `json:"fallback_reason,omitempty"`
	LastWSSError                string    `json:"last_wss_error,omitempty"`
}

// SavingsSummary rolls up Phase F counters for the dashboard tile.
type SavingsSummary struct {
	InputTokensSaved         int64                    `json:"input_tokens_saved"`
	OutputTokensSaved        int64                    `json:"output_tokens_saved"`
	CostUSD                  float64                  `json:"cost_usd"`
	ProxyLayer0ToolResults   int64                    `json:"proxy_layer0_tool_result_blocks"`
	ProxyLayer0ToolMisses    int64                    `json:"proxy_layer0_tool_use_unresolved_blocks"`
	ProxyLayer0Commands      int64                    `json:"proxy_layer0_command_resolved_blocks"`
	ProxyLayer0CommandMisses int64                    `json:"proxy_layer0_command_unresolved_blocks"`
	ProxyLayer0ReadAttempts  int64                    `json:"proxy_layer0_read_delta_attempts"`
	ProxyLayer0ReadMisses    int64                    `json:"proxy_layer0_read_delta_misses"`
	ProxyLayer0Blocks        int64                    `json:"proxy_layer0_blocks"`
	ProxyLayer0ReadDelta     int64                    `json:"proxy_layer0_read_delta_blocks"`
	ProxyLayer0Captured      int64                    `json:"proxy_layer0_captured_output_blocks"`
	ProxyLayer0Envelope      int64                    `json:"proxy_layer0_codex_exec_envelope_blocks"`
	ProxyLayer0Routes        ProxyLayer0RoutesSummary `json:"proxy_layer0_routes"`
	StreamcutFires           int64                    `json:"streamcut_fires"`
	RepdetRewrites           int64                    `json:"repdet_rewrites"`
	RepdetBytesSaved         int64                    `json:"repdet_bytes_saved"`
	StaleReadBlocks          int64                    `json:"stale_read_blocks"`
	ObsoletePruneBlocks      int64                    `json:"obsolete_prune_blocks"`
	StopSeqInjections        int64                    `json:"stop_seq_injections"`
	BeterseInjections        int64                    `json:"beterse_injections"`
	QualityABRolledBack      bool                     `json:"quality_ab_rolled_back"`
	QualityABControlFail     float64                  `json:"quality_ab_control_failure_rate"`
	QualityABTreatmentFail   float64                  `json:"quality_ab_treatment_failure_rate"`
}

type ProxyLayer0RouteSummary struct {
	ToolResults      int64 `json:"tool_result_blocks"`
	ToolMisses       int64 `json:"tool_use_unresolved_blocks"`
	Commands         int64 `json:"command_resolved_blocks"`
	CommandMisses    int64 `json:"command_unresolved_blocks"`
	ReadAttempts     int64 `json:"read_delta_attempts"`
	ReadMisses       int64 `json:"read_delta_misses"`
	RequestsModified int64 `json:"requests_modified"`
	TokensSaved      int64 `json:"tokens_saved"`
	BlocksModified   int64 `json:"blocks_modified"`
	ReadDeltaBlocks  int64 `json:"read_delta_blocks"`
	CapturedBlocks   int64 `json:"captured_output_blocks"`
	EnvelopeBlocks   int64 `json:"codex_exec_envelope_blocks"`
}

type ProxyLayer0RoutesSummary struct {
	HTTP      ProxyLayer0RouteSummary `json:"http"`
	WSSPhaseF ProxyLayer0RouteSummary `json:"wss_phasef"`
}

// WSSState reports the transparent Codex WebSocket MITM bridge. It is
// intentionally transport-level: savings live under SavingsSummary,
// while this block tells operators whether WSS frames are bridged
// byte-equal, degraded, or actually re-encoded after mutation.
type WSSState struct {
	EngineActive                 bool  `json:"engine_active"`
	PassthroughBridged           int64 `json:"passthrough_bridged"`
	MITMBridged                  int64 `json:"mitm_bridged"`
	PhasefBridged                int64 `json:"phasef_bridged"`
	Rejected                     int64 `json:"rejected"`
	UpstreamDialFail             int64 `json:"upstream_dial_failures"`
	BytesC2S                     int64 `json:"bytes_c2s"`
	BytesS2C                     int64 `json:"bytes_s2c"`
	C2SFrames                    int64 `json:"c2s_frames"`
	S2CFrames                    int64 `json:"s2c_frames"`
	ParseFailures                int64 `json:"parse_failures"`
	DegradedSessions             int64 `json:"degraded_sessions"`
	FramesReencoded              int64 `json:"frames_reencoded"`
	FramesForwarded              int64 `json:"frames_forwarded"`
	CompressedMessagesInspected  int64 `json:"compressed_messages_inspected"`
	CompressedMessagesMutated    int64 `json:"compressed_messages_mutated"`
	CompressedMessagesBypassed   int64 `json:"compressed_messages_bypassed"`
	CompressionErrors            int64 `json:"compression_errors"`
	PhaseFRequests               int64 `json:"phasef_requests"`
	PhaseFRequestBodies          int64 `json:"phasef_request_bodies"`
	PhaseFRequestMessagesIndexed int64 `json:"phasef_request_messages_indexed"`
	PhaseFTextDeltas             int64 `json:"phasef_text_deltas"`
	PhaseFTerminalResponses      int64 `json:"phasef_terminal_responses"`
	PhaseFMutations              int64 `json:"phasef_mutations"`
	MutationActive               bool  `json:"mutation_active"`
	ByteBridgeOnly               bool  `json:"byte_bridge_only"`
}

// Probes is the dependency surface Build() uses. Every method must be
// safe to call concurrently and must return quickly (≤ 100 ms total
// budget across the whole snapshot). Implementations live in
// probe_*.go.
type Probes struct {
	CA           CAProbe
	Daemon       DaemonProbe
	Listener     ListenerProbe
	NetworkRedir NetworkProbe
	Indist       IndistProbe
	Apps         AppsProbe
	CodexRoute   CodexRouteProbe
	Savings      SavingsProbe
	WSS          WSSProbe
	Clock        func() time.Time
}

// Individual probe interfaces - one per state field so callers /
// tests can swap a single concrete probe without rebuilding the
// whole Probes struct.

type CAProbe interface {
	ProbeCA(ctx context.Context) CAState
}
type DaemonProbe interface {
	ProbeDaemon(ctx context.Context) DaemonState
}
type ListenerProbe interface {
	ProbeListener(ctx context.Context) ListenerState
}
type NetworkProbe interface {
	ProbeNetwork(ctx context.Context) NetworkState
}
type IndistProbe interface {
	ProbeIndist(ctx context.Context) IndistState
}
type AppsProbe interface {
	ProbeApps(ctx context.Context) []AppEntry
}
type CodexRouteProbe interface {
	ProbeCodexRoute(ctx context.Context) CodexRouteState
}
type SavingsProbe interface {
	ProbeSavings(ctx context.Context) SavingsSummary
}
type WSSProbe interface {
	ProbeWSS(ctx context.Context) WSSState
}

// PlanInspectorProbe is an optional shortcut for callers that have a
// reversibility.Plan available; the plan's per-Step Inspect() result
// can be reused as an Inventory snapshot.
type PlanInspectorProbe interface {
	InspectPlan(ctx context.Context) reversibility.InspectResult
}
