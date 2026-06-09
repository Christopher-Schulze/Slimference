package codexroute

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	CertificationSchemaVersion  = 1
	RouteProfileScopedRawWSS    = "scoped_raw_wss_phasef"
	RouteProfileScopedWSSBridge = "scoped_raw_wss_bridge"
	RecertSchemaVersion         = 1
)

type AutoMode string

const (
	AutoModeWSSPhaseF AutoMode = "wss_phasef"
	AutoModeWSSBridge AutoMode = "wss_bridge"
	AutoModeHTTP      AutoMode = "http"
	AutoModeDirect    AutoMode = "direct"
)

// CertificationState records the operator-approved WSS proof for the
// current scoped Codex route. It is deliberately local machine state:
// WSS auto-promotion is only safe after live capture on this machine.
type CertificationState struct {
	SchemaVersion      int       `json:"schema_version"`
	Transport          string    `json:"transport"`
	RouteProfile       string    `json:"route_profile"`
	CodexVersion       string    `json:"codex_version"`
	SlimferenceVersion string    `json:"slimference_version"`
	Passed             bool      `json:"passed"`
	NativeCaptureHash  string    `json:"native_capture_hash,omitempty"`
	ScopedCaptureHash  string    `json:"scoped_capture_hash,omitempty"`
	FramesReencoded    int64     `json:"frames_reencoded"`
	DegradedSessions   int64     `json:"degraded_sessions"`
	ParseFailures      int64     `json:"parse_failures"`
	LastError          string    `json:"last_error,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
	Operator           string    `json:"operator,omitempty"`
	Notes              string    `json:"notes,omitempty"`
}

// BridgeProofState records the lower-risk proof that scoped Codex WSS can
// bridge byte-equal for the current local version tuple.
type BridgeProofState struct {
	SchemaVersion      int       `json:"schema_version"`
	Transport          string    `json:"transport"`
	RouteProfile       string    `json:"route_profile"`
	CodexVersion       string    `json:"codex_version"`
	SlimferenceVersion string    `json:"slimference_version"`
	Passed             bool      `json:"passed"`
	BytesC2S           int64     `json:"bytes_c2s"`
	BytesS2C           int64     `json:"bytes_s2c"`
	C2SFrames          int64     `json:"c2s_frames"`
	S2CFrames          int64     `json:"s2c_frames"`
	FramesForwarded    int64     `json:"frames_forwarded"`
	FramesReencoded    int64     `json:"frames_reencoded"`
	DegradedSessions   int64     `json:"degraded_sessions"`
	ParseFailures      int64     `json:"parse_failures"`
	CompressionErrors  int64     `json:"compression_errors"`
	LastError          string    `json:"last_error,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
	Operator           string    `json:"operator,omitempty"`
	Notes              string    `json:"notes,omitempty"`
}

type RecertState struct {
	SchemaVersion      int       `json:"schema_version"`
	Status             string    `json:"status"`
	AttemptID          string    `json:"attempt_id,omitempty"`
	CodexVersion       string    `json:"codex_version,omitempty"`
	SlimferenceVersion string    `json:"slimference_version,omitempty"`
	StartedAt          time.Time `json:"started_at,omitempty"`
	FinishedAt         time.Time `json:"finished_at,omitempty"`
	LastSuccessAt      time.Time `json:"last_success_at,omitempty"`
	RetryAfter         time.Time `json:"retry_after,omitempty"`
	PhaseFPassed       bool      `json:"phasef_passed"`
	BridgePassed       bool      `json:"bridge_passed"`
	BytesC2S           int64     `json:"bytes_c2s,omitempty"`
	BytesS2C           int64     `json:"bytes_s2c,omitempty"`
	FramesForwarded    int64     `json:"frames_forwarded,omitempty"`
	FramesReencoded    int64     `json:"frames_reencoded,omitempty"`
	CompressedMutated  int64     `json:"compressed_messages_mutated,omitempty"`
	ParseFailures      int64     `json:"parse_failures,omitempty"`
	DegradedSessions   int64     `json:"degraded_sessions,omitempty"`
	CompressionErrors  int64     `json:"compression_errors,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
}

type RejectedAutoMode struct {
	Mode   AutoMode `json:"mode"`
	Reason string   `json:"reason"`
}

// AutoDecision explains how --transport=auto resolves right now.
type AutoDecision struct {
	Mode                 AutoMode           `json:"mode"`
	Transport            Transport          `json:"transport"`
	WSSCertified         bool               `json:"wss_certified"`
	WSSBridgeAvailable   bool               `json:"wss_bridge_available"`
	NeedsRecert          bool               `json:"needs_recert"`
	CurrentCodex         string             `json:"current_codex_version,omitempty"`
	CurrentSlimference   string             `json:"current_slimference_version,omitempty"`
	CertifiedCodex       string             `json:"certified_codex_version,omitempty"`
	CertifiedSlimference string             `json:"certified_slimference_version,omitempty"`
	BridgeCodex          string             `json:"bridge_codex_version,omitempty"`
	BridgeSlimference    string             `json:"bridge_slimference_version,omitempty"`
	CertificationPath    string             `json:"certification_path"`
	BridgeProofPath      string             `json:"bridge_proof_path"`
	RecertStatePath      string             `json:"recert_state_path"`
	RecertLogPath        string             `json:"recert_log_path,omitempty"`
	RecertStatus         string             `json:"recert_status,omitempty"`
	RecertAttemptID      string             `json:"recert_attempt_id,omitempty"`
	RecertStartedAt      time.Time          `json:"recert_started_at,omitempty"`
	RecertFinishedAt     time.Time          `json:"recert_finished_at,omitempty"`
	RecertLastSuccessAt  time.Time          `json:"recert_last_success_at,omitempty"`
	RecertRetryAfter     time.Time          `json:"recert_retry_after,omitempty"`
	RecertLastError      string             `json:"recert_last_error,omitempty"`
	FallbackReason       string             `json:"fallback_reason,omitempty"`
	LastWSSError         string             `json:"last_wss_error,omitempty"`
	RecertCommand        string             `json:"recert_command,omitempty"`
	RejectedModes        []RejectedAutoMode `json:"rejected_modes,omitempty"`
}

// CertificationPath returns the local WSS-certification file.
func CertificationPath(home string) string {
	return filepath.Join(home, ".slimference", "codex-wss-cert.json")
}

func BridgeProofPath(home string) string {
	return filepath.Join(home, ".slimference", "codex-wss-bridge.json")
}

func RecertStatePath(home string) string {
	return filepath.Join(home, ".slimference", "codex-wss-recert.json")
}

func RecertLockPath(home string) string {
	return filepath.Join(home, ".slimference", "codex-wss-recert.lock")
}

func RecertLogPath(home string) string {
	return filepath.Join(home, ".slimference", "logs", "codex-wss-recert.log")
}

// LoadCertification reads the local WSS-certification file.
func LoadCertification(home string) (CertificationState, bool, error) {
	path := CertificationPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CertificationState{}, false, nil
		}
		return CertificationState{}, false, err
	}
	var state CertificationState
	if err := json.Unmarshal(data, &state); err != nil {
		return CertificationState{}, true, err
	}
	return state, true, nil
}

func LoadBridgeProof(home string) (BridgeProofState, bool, error) {
	path := BridgeProofPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return BridgeProofState{}, false, nil
		}
		return BridgeProofState{}, false, err
	}
	var state BridgeProofState
	if err := json.Unmarshal(data, &state); err != nil {
		return BridgeProofState{}, true, err
	}
	return state, true, nil
}

func LoadRecertState(home string) (RecertState, bool, error) {
	path := RecertStatePath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RecertState{}, false, nil
		}
		return RecertState{}, false, err
	}
	var state RecertState
	if err := json.Unmarshal(data, &state); err != nil {
		return RecertState{}, true, err
	}
	return state, true, nil
}

// SaveCertification writes the local WSS-certification file.
func SaveCertification(home string, state CertificationState) error {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = CertificationSchemaVersion
	}
	if state.Transport == "" {
		state.Transport = string(TransportWSS)
	}
	if state.RouteProfile == "" {
		state.RouteProfile = RouteProfileScopedRawWSS
	}
	if state.Timestamp.IsZero() {
		state.Timestamp = time.Now().UTC()
	}
	path := CertificationPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}

func SaveBridgeProof(home string, state BridgeProofState) error {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = CertificationSchemaVersion
	}
	if state.Transport == "" {
		state.Transport = string(TransportWSS)
	}
	if state.RouteProfile == "" {
		state.RouteProfile = RouteProfileScopedWSSBridge
	}
	if state.Timestamp.IsZero() {
		state.Timestamp = time.Now().UTC()
	}
	path := BridgeProofPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}

func SaveRecertState(home string, state RecertState) error {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = RecertSchemaVersion
	}
	path := RecertStatePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}

// DecideAutoTransport resolves --transport=auto through the WSS-first ladder:
// WSS Phase-F savings, WSS byte-equal bridge, then HTTP fallback.
func DecideAutoTransport(home, codexVersion, slimferenceVersion string) (AutoDecision, error) {
	decision := AutoDecision{
		Mode:               AutoModeHTTP,
		Transport:          TransportHTTP,
		CurrentCodex:       strings.TrimSpace(codexVersion),
		CurrentSlimference: strings.TrimSpace(slimferenceVersion),
		CertificationPath:  CertificationPath(home),
		BridgeProofPath:    BridgeProofPath(home),
		RecertStatePath:    RecertStatePath(home),
		RecertLogPath:      RecertLogPath(home),
		FallbackReason:     "wss certification missing",
	}

	if recert, exists, err := LoadRecertState(home); err == nil && exists {
		decision.RecertStatus = recert.Status
		decision.RecertAttemptID = recert.AttemptID
		decision.RecertStartedAt = recert.StartedAt
		decision.RecertFinishedAt = recert.FinishedAt
		decision.RecertLastSuccessAt = recert.LastSuccessAt
		decision.RecertRetryAfter = recert.RetryAfter
		decision.RecertLastError = recert.LastError
	} else if err != nil {
		decision.RejectedModes = append(decision.RejectedModes, RejectedAutoMode{Mode: "wss_phasef", Reason: "recert state unreadable: " + err.Error()})
	}

	state, exists, err := LoadCertification(home)
	if err != nil {
		decision.FallbackReason = "wss certification unreadable"
		decision.LastWSSError = err.Error()
		decision.NeedsRecert = true
		decision.RecertCommand = "slimference codex recertify wss"
		decision.RejectedModes = append(decision.RejectedModes, RejectedAutoMode{Mode: AutoModeWSSPhaseF, Reason: decision.FallbackReason})
		return decideBridgeOrHTTP(home, codexVersion, slimferenceVersion, decision), nil
	}
	if exists {
		decision.CertifiedCodex = state.CodexVersion
		decision.CertifiedSlimference = state.SlimferenceVersion
		decision.LastWSSError = state.LastError
	}
	phaseReason, phaseNeedsRecert := phaseFCertRejection(state, exists, codexVersion, slimferenceVersion)
	if phaseReason == "" {
		decision.Mode = AutoModeWSSPhaseF
		decision.Transport = TransportWSS
		decision.WSSCertified = true
		decision.FallbackReason = ""
		return decision, nil
	}
	decision.FallbackReason = phaseReason
	decision.NeedsRecert = phaseNeedsRecert
	decision.RecertCommand = "slimference codex recertify wss"
	decision.RejectedModes = append(decision.RejectedModes, RejectedAutoMode{Mode: AutoModeWSSPhaseF, Reason: phaseReason})
	return decideBridgeOrHTTP(home, codexVersion, slimferenceVersion, decision), nil
}

func decideBridgeOrHTTP(home, codexVersion, slimferenceVersion string, decision AutoDecision) AutoDecision {
	bridge, exists, err := LoadBridgeProof(home)
	if err != nil {
		decision.RejectedModes = append(decision.RejectedModes, RejectedAutoMode{Mode: AutoModeWSSBridge, Reason: "wss bridge proof unreadable"})
		decision.LastWSSError = err.Error()
		return decision
	}
	if exists {
		decision.BridgeCodex = bridge.CodexVersion
		decision.BridgeSlimference = bridge.SlimferenceVersion
	}
	bridgeReason := bridgeProofRejection(bridge, exists, codexVersion, slimferenceVersion)
	if bridgeReason != "" {
		decision.RejectedModes = append(decision.RejectedModes, RejectedAutoMode{Mode: AutoModeWSSBridge, Reason: bridgeReason})
		return decision
	}
	decision.Mode = AutoModeWSSBridge
	decision.Transport = TransportWSS
	decision.WSSBridgeAvailable = true
	decision.FallbackReason = "phase-f savings proof not current; using current WSS bridge proof"
	return decision
}

func phaseFCertRejection(state CertificationState, exists bool, codexVersion, slimferenceVersion string) (string, bool) {
	if !exists {
		return "wss certification missing", true
	}
	switch {
	case state.SchemaVersion != CertificationSchemaVersion:
		return "wss certification schema mismatch", true
	case state.Transport != string(TransportWSS):
		return "wss certification transport mismatch", true
	case state.RouteProfile != RouteProfileScopedRawWSS:
		return "wss certification route profile mismatch", true
	case !state.Passed:
		return "wss certification did not pass", true
	case state.ParseFailures != 0:
		return "recent wss parse failures recorded", true
	case state.DegradedSessions != 0:
		return "recent wss degraded sessions recorded", true
	case !sameVersion(state.CodexVersion, codexVersion):
		return "codex version changed since wss certification", true
	case !sameVersion(state.SlimferenceVersion, slimferenceVersion):
		return "slimference version changed since wss certification", true
	default:
		return "", false
	}
}

func bridgeProofRejection(state BridgeProofState, exists bool, codexVersion, slimferenceVersion string) string {
	if !exists {
		return "wss bridge proof missing"
	}
	switch {
	case state.SchemaVersion != CertificationSchemaVersion:
		return "wss bridge proof schema mismatch"
	case state.Transport != string(TransportWSS):
		return "wss bridge proof transport mismatch"
	case state.RouteProfile != RouteProfileScopedWSSBridge:
		return "wss bridge proof route profile mismatch"
	case !state.Passed:
		return "wss bridge proof did not pass"
	case !sameVersion(state.CodexVersion, codexVersion):
		return "codex version changed since wss bridge proof"
	case !sameVersion(state.SlimferenceVersion, slimferenceVersion):
		return "slimference version changed since wss bridge proof"
	case state.ParseFailures != 0:
		return "wss bridge parse failures recorded"
	case state.DegradedSessions != 0:
		return "wss bridge degraded sessions recorded"
	case state.CompressionErrors != 0:
		return "wss bridge compression errors recorded"
	case state.FramesReencoded != 0:
		return "wss bridge proof contains mutation"
	case state.BytesC2S <= 0 || state.BytesS2C <= 0:
		return "wss bridge proof missing bidirectional bytes"
	case state.C2SFrames+state.S2CFrames <= 0 && state.FramesForwarded <= 0:
		return "wss bridge proof missing websocket frames"
	default:
		return ""
	}
}

func sameVersion(certified, current string) bool {
	certified = strings.TrimSpace(certified)
	current = strings.TrimSpace(current)
	return certified != "" && current != "" && certified == current
}
