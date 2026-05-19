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
	CertificationSchemaVersion = 1
	RouteProfileScopedRawWSS   = "scoped_raw_wss_phasef"
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

// AutoDecision explains how --transport=auto resolves right now.
type AutoDecision struct {
	Transport            Transport `json:"transport"`
	WSSCertified         bool      `json:"wss_certified"`
	NeedsRecert          bool      `json:"needs_recert"`
	CurrentCodex         string    `json:"current_codex_version,omitempty"`
	CurrentSlimference   string    `json:"current_slimference_version,omitempty"`
	CertifiedCodex       string    `json:"certified_codex_version,omitempty"`
	CertifiedSlimference string    `json:"certified_slimference_version,omitempty"`
	CertificationPath    string    `json:"certification_path"`
	FallbackReason       string    `json:"fallback_reason,omitempty"`
	LastWSSError         string    `json:"last_wss_error,omitempty"`
	RecertCommand        string    `json:"recert_command,omitempty"`
}

// CertificationPath returns the local WSS-certification file.
func CertificationPath(home string) string {
	return filepath.Join(home, ".slimference", "codex-wss-cert.json")
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

// DecideAutoTransport resolves --transport=auto. It is conservative by
// construction: no green, version-matching local WSS proof means HTTP.
func DecideAutoTransport(home, codexVersion, slimferenceVersion string) (AutoDecision, error) {
	decision := AutoDecision{
		Transport:          TransportHTTP,
		CurrentCodex:       strings.TrimSpace(codexVersion),
		CurrentSlimference: strings.TrimSpace(slimferenceVersion),
		CertificationPath:  CertificationPath(home),
		FallbackReason:     "wss certification missing",
	}
	state, exists, err := LoadCertification(home)
	if err != nil {
		decision.FallbackReason = "wss certification unreadable"
		decision.LastWSSError = err.Error()
		return decision, nil
	}
	if !exists {
		return decision, nil
	}
	decision.CertifiedCodex = state.CodexVersion
	decision.CertifiedSlimference = state.SlimferenceVersion
	decision.LastWSSError = state.LastError
	switch {
	case state.SchemaVersion != CertificationSchemaVersion:
		decision.FallbackReason = "wss certification schema mismatch"
	case state.Transport != string(TransportWSS):
		decision.FallbackReason = "wss certification transport mismatch"
	case state.RouteProfile != RouteProfileScopedRawWSS:
		decision.FallbackReason = "wss certification route profile mismatch"
	case !state.Passed:
		decision.FallbackReason = "wss certification did not pass"
	case state.ParseFailures != 0:
		decision.FallbackReason = "recent wss parse failures recorded"
	case state.DegradedSessions != 0:
		decision.FallbackReason = "recent wss degraded sessions recorded"
	case !sameVersion(state.CodexVersion, codexVersion):
		decision.FallbackReason = "codex version changed since wss certification"
		decision.NeedsRecert = true
		decision.RecertCommand = "slimference codex certify wss"
	case !sameVersion(state.SlimferenceVersion, slimferenceVersion):
		decision.FallbackReason = "slimference version changed since wss certification"
		decision.NeedsRecert = true
		decision.RecertCommand = "slimference codex certify wss"
	default:
		decision.Transport = TransportWSS
		decision.WSSCertified = true
		decision.FallbackReason = ""
	}
	return decision, nil
}

func sameVersion(certified, current string) bool {
	certified = strings.TrimSpace(certified)
	current = strings.TrimSpace(current)
	return certified != "" && current != "" && certified == current
}
