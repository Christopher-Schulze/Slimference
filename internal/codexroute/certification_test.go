package codexroute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecideAutoTransportNoCertificationFallsBackHTTP(t *testing.T) {
	home := t.TempDir()
	decision, err := DecideAutoTransport(home, "codex-1", "slim-1")
	if err != nil {
		t.Fatalf("DecideAutoTransport: %v", err)
	}
	if decision.Transport != TransportHTTP || decision.WSSCertified {
		t.Fatalf("decision=%+v", decision)
	}
	if decision.Mode != AutoModeHTTP || !decision.NeedsRecert {
		t.Fatalf("mode/recert decision=%+v", decision)
	}
	if !strings.Contains(decision.FallbackReason, "missing") {
		t.Fatalf("fallback reason=%q", decision.FallbackReason)
	}
}

func TestDecideAutoTransportGreenCertificationPromotesWSS(t *testing.T) {
	home := t.TempDir()
	if err := SaveCertification(home, CertificationState{
		CodexVersion:       "codex-1",
		SlimferenceVersion: "slim-1",
		Passed:             true,
		FramesReencoded:    9,
	}); err != nil {
		t.Fatalf("SaveCertification: %v", err)
	}
	decision, err := DecideAutoTransport(home, "codex-1", "slim-1")
	if err != nil {
		t.Fatalf("DecideAutoTransport: %v", err)
	}
	if decision.Mode != AutoModeWSSPhaseF || decision.Transport != TransportWSS || !decision.WSSCertified || decision.FallbackReason != "" {
		t.Fatalf("decision=%+v", decision)
	}
	if decision.CertifiedCodex != "codex-1" || decision.CertifiedSlimference != "slim-1" {
		t.Fatalf("version fields missing: %+v", decision)
	}
}

func TestDecideAutoTransportRejectsStaleOrUnhealthyCertification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   CertificationState
		current string
		slim    string
		want    string
	}{
		{
			name: "schema mismatch",
			state: CertificationState{
				SchemaVersion:      99,
				Transport:          string(TransportWSS),
				RouteProfile:       RouteProfileScopedRawWSS,
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
			},
			current: "codex-1",
			slim:    "slim-1",
			want:    "schema mismatch",
		},
		{
			name: "transport mismatch",
			state: CertificationState{
				Transport:          string(TransportHTTP),
				RouteProfile:       RouteProfileScopedRawWSS,
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
			},
			current: "codex-1",
			slim:    "slim-1",
			want:    "transport mismatch",
		},
		{
			name: "route profile mismatch",
			state: CertificationState{
				Transport:          string(TransportWSS),
				RouteProfile:       "other",
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
			},
			current: "codex-1",
			slim:    "slim-1",
			want:    "route profile mismatch",
		},
		{
			name: "not passed",
			state: CertificationState{
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             false,
			},
			current: "codex-1",
			slim:    "slim-1",
			want:    "did not pass",
		},
		{
			name: "codex version changed",
			state: CertificationState{
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
			},
			current: "codex-2",
			slim:    "slim-1",
			want:    "version changed",
		},
		{
			name: "slimference version changed",
			state: CertificationState{
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
			},
			current: "codex-1",
			slim:    "slim-2",
			want:    "slimference version changed",
		},
		{
			name: "parse failures",
			state: CertificationState{
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
				ParseFailures:      1,
			},
			current: "codex-1",
			slim:    "slim-1",
			want:    "parse failures",
		},
		{
			name: "degraded sessions",
			state: CertificationState{
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
				DegradedSessions:   1,
			},
			current: "codex-1",
			slim:    "slim-1",
			want:    "degraded sessions",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := SaveCertification(home, tc.state); err != nil {
				t.Fatalf("SaveCertification: %v", err)
			}
			decision, err := DecideAutoTransport(home, tc.current, tc.slim)
			if err != nil {
				t.Fatalf("DecideAutoTransport: %v", err)
			}
			if decision.Transport != TransportHTTP || decision.WSSCertified {
				t.Fatalf("decision=%+v", decision)
			}
			if !strings.Contains(decision.FallbackReason, tc.want) {
				t.Fatalf("reason=%q want contains %q", decision.FallbackReason, tc.want)
			}
			if !decision.NeedsRecert {
				t.Fatalf("needs_recert=false for %s", tc.name)
			}
			if decision.RecertCommand != "slimference codex recertify wss" {
				t.Fatalf("recert command=%q", decision.RecertCommand)
			}
			if decision.CurrentCodex != tc.current || decision.CurrentSlimference != tc.slim {
				t.Fatalf("current tuple codex=%q slim=%q", decision.CurrentCodex, decision.CurrentSlimference)
			}
		})
	}
}

func TestDecideAutoTransportPrefersHTTPSavingsWhenOnlyBridgeIsAvailable(t *testing.T) {
	home := t.TempDir()
	if err := SaveCertification(home, CertificationState{
		CodexVersion:       "codex-1",
		SlimferenceVersion: "slim-1",
		Passed:             true,
	}); err != nil {
		t.Fatalf("SaveCertification: %v", err)
	}
	if err := SaveBridgeProof(home, BridgeProofState{
		CodexVersion:       "codex-2",
		SlimferenceVersion: "slim-1",
		Passed:             true,
		BytesC2S:           10,
		BytesS2C:           20,
		C2SFrames:          1,
		S2CFrames:          1,
		FramesForwarded:    2,
	}); err != nil {
		t.Fatalf("SaveBridgeProof: %v", err)
	}
	decision, err := DecideAutoTransport(home, "codex-2", "slim-1")
	if err != nil {
		t.Fatalf("DecideAutoTransport: %v", err)
	}
	if decision.Mode != AutoModeHTTP || decision.Transport != TransportHTTP || !decision.WSSBridgeAvailable {
		t.Fatalf("decision=%+v", decision)
	}
	if !strings.Contains(decision.FallbackReason, "using HTTP savings path") {
		t.Fatalf("fallback reason=%q", decision.FallbackReason)
	}
	if !decision.NeedsRecert || decision.RecertCommand != "slimference codex recertify wss" {
		t.Fatalf("recert state=%+v", decision)
	}
}

func TestDecideAutoTransportReportsBridgeButPrefersHTTPForRunningOrFailedRecert(t *testing.T) {
	for _, tc := range []struct {
		name   string
		recert RecertState
	}{
		{
			name: "running recert",
			recert: RecertState{
				Status:        "running",
				AttemptID:     "attempt-running",
				StartedAt:     time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC),
				LastSuccessAt: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "failed recert",
			recert: RecertState{
				Status:     "failed",
				AttemptID:  "attempt-failed",
				FinishedAt: time.Date(2026, 5, 19, 12, 5, 0, 0, time.UTC),
				RetryAfter: time.Date(2026, 5, 19, 13, 0, 0, 0, time.UTC),
				LastError:  "synthetic failure",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := SaveCertification(home, CertificationState{
				CodexVersion:       "codex-1",
				SlimferenceVersion: "slim-1",
				Passed:             true,
			}); err != nil {
				t.Fatalf("SaveCertification: %v", err)
			}
			if err := SaveBridgeProof(home, BridgeProofState{
				CodexVersion:       "codex-2",
				SlimferenceVersion: "slim-1",
				Passed:             true,
				BytesC2S:           10,
				BytesS2C:           20,
				C2SFrames:          1,
				S2CFrames:          1,
				FramesForwarded:    2,
			}); err != nil {
				t.Fatalf("SaveBridgeProof: %v", err)
			}
			if err := SaveRecertState(home, tc.recert); err != nil {
				t.Fatalf("SaveRecertState: %v", err)
			}
			decision, err := DecideAutoTransport(home, "codex-2", "slim-1")
			if err != nil {
				t.Fatalf("DecideAutoTransport: %v", err)
			}
			if decision.Mode != AutoModeHTTP || decision.Transport != TransportHTTP || !decision.WSSBridgeAvailable {
				t.Fatalf("decision=%+v", decision)
			}
			if !strings.Contains(decision.FallbackReason, "using HTTP savings path") {
				t.Fatalf("fallback reason=%q", decision.FallbackReason)
			}
			if decision.RecertStatus != tc.recert.Status || decision.RecertAttemptID != tc.recert.AttemptID {
				t.Fatalf("recert status missing: %+v", decision)
			}
			if !decision.RecertStartedAt.Equal(tc.recert.StartedAt) ||
				!decision.RecertFinishedAt.Equal(tc.recert.FinishedAt) ||
				!decision.RecertLastSuccessAt.Equal(tc.recert.LastSuccessAt) ||
				!decision.RecertRetryAfter.Equal(tc.recert.RetryAfter) ||
				decision.RecertLastError != tc.recert.LastError ||
				decision.RecertLogPath != RecertLogPath(home) {
				t.Fatalf("recert metadata missing: got=%+v want=%+v", decision, tc.recert)
			}
		})
	}
}

func TestDecideAutoTransportRejectsMutatingBridgeProof(t *testing.T) {
	home := t.TempDir()
	if err := SaveBridgeProof(home, BridgeProofState{
		CodexVersion:       "codex-1",
		SlimferenceVersion: "slim-1",
		Passed:             true,
		BytesC2S:           10,
		BytesS2C:           20,
		C2SFrames:          1,
		FramesReencoded:    1,
	}); err != nil {
		t.Fatalf("SaveBridgeProof: %v", err)
	}
	decision, err := DecideAutoTransport(home, "codex-1", "slim-1")
	if err != nil {
		t.Fatalf("DecideAutoTransport: %v", err)
	}
	if decision.Mode != AutoModeHTTP || decision.Transport != TransportHTTP || decision.WSSBridgeAvailable {
		t.Fatalf("decision=%+v", decision)
	}
	if len(decision.RejectedModes) == 0 {
		t.Fatalf("missing rejection details: %+v", decision)
	}
}

func TestDecideAutoTransportRejectsEachBadBridgeProofCriterion(t *testing.T) {
	base := BridgeProofState{
		CodexVersion:       "codex-1",
		SlimferenceVersion: "slim-1",
		Passed:             true,
		BytesC2S:           10,
		BytesS2C:           20,
		C2SFrames:          1,
		S2CFrames:          1,
		FramesForwarded:    2,
	}
	for _, tc := range []struct {
		name  string
		edit  func(*BridgeProofState)
		want  string
		codex string
		slim  string
	}{
		{name: "schema mismatch", edit: func(s *BridgeProofState) { s.SchemaVersion = 99 }, want: "schema mismatch", codex: "codex-1", slim: "slim-1"},
		{name: "transport mismatch", edit: func(s *BridgeProofState) { s.Transport = string(TransportHTTP) }, want: "transport mismatch", codex: "codex-1", slim: "slim-1"},
		{name: "profile mismatch", edit: func(s *BridgeProofState) { s.RouteProfile = "other" }, want: "route profile mismatch", codex: "codex-1", slim: "slim-1"},
		{name: "not passed", edit: func(s *BridgeProofState) { s.Passed = false }, want: "did not pass", codex: "codex-1", slim: "slim-1"},
		{name: "codex drift", edit: func(*BridgeProofState) {}, want: "codex version changed", codex: "codex-2", slim: "slim-1"},
		{name: "slim drift", edit: func(*BridgeProofState) {}, want: "slimference version changed", codex: "codex-1", slim: "slim-2"},
		{name: "parse failures", edit: func(s *BridgeProofState) { s.ParseFailures = 1 }, want: "parse failures", codex: "codex-1", slim: "slim-1"},
		{name: "degraded", edit: func(s *BridgeProofState) { s.DegradedSessions = 1 }, want: "degraded sessions", codex: "codex-1", slim: "slim-1"},
		{name: "compression errors", edit: func(s *BridgeProofState) { s.CompressionErrors = 1 }, want: "compression errors", codex: "codex-1", slim: "slim-1"},
		{name: "mutation", edit: func(s *BridgeProofState) { s.FramesReencoded = 1 }, want: "contains mutation", codex: "codex-1", slim: "slim-1"},
		{name: "no bytes", edit: func(s *BridgeProofState) { s.BytesC2S = 0 }, want: "missing bidirectional bytes", codex: "codex-1", slim: "slim-1"},
		{name: "no frames", edit: func(s *BridgeProofState) { s.C2SFrames = 0; s.S2CFrames = 0; s.FramesForwarded = 0 }, want: "missing websocket frames", codex: "codex-1", slim: "slim-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			state := base
			tc.edit(&state)
			if err := SaveBridgeProof(home, state); err != nil {
				t.Fatalf("SaveBridgeProof: %v", err)
			}
			decision, err := DecideAutoTransport(home, tc.codex, tc.slim)
			if err != nil {
				t.Fatalf("DecideAutoTransport: %v", err)
			}
			if decision.Mode != AutoModeHTTP || decision.WSSBridgeAvailable {
				t.Fatalf("decision=%+v", decision)
			}
			found := false
			for _, rejected := range decision.RejectedModes {
				if rejected.Mode == AutoModeWSSBridge && strings.Contains(rejected.Reason, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing rejection %q in %+v", tc.want, decision.RejectedModes)
			}
		})
	}
}

func TestRecertPaths(t *testing.T) {
	home := t.TempDir()
	if !strings.HasSuffix(RecertLockPath(home), ".slimference/codex-wss-recert.lock") {
		t.Fatalf("lock path=%q", RecertLockPath(home))
	}
	if !strings.HasSuffix(RecertLogPath(home), ".slimference/logs/codex-wss-recert.log") {
		t.Fatalf("log path=%q", RecertLogPath(home))
	}
}

func TestDecideAutoTransportUnreadableCertificationFailsClosedToHTTP(t *testing.T) {
	home := t.TempDir()
	path := CertificationPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	decision, err := DecideAutoTransport(home, "codex-1", "slim-1")
	if err != nil {
		t.Fatalf("DecideAutoTransport should not fail hard: %v", err)
	}
	if decision.Transport != TransportHTTP || !strings.Contains(decision.FallbackReason, "unreadable") {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestDecideAutoTransportUnreadableCertificationReportsCleanBridgeButKeepsHTTP(t *testing.T) {
	home := t.TempDir()
	certPath := CertificationPath(home)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(certPath, []byte("{bad-cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := SaveBridgeProof(home, BridgeProofState{
		CodexVersion:       "codex-1",
		SlimferenceVersion: "slim-1",
		Passed:             true,
		BytesC2S:           10,
		BytesS2C:           20,
		C2SFrames:          1,
	}); err != nil {
		t.Fatalf("SaveBridgeProof: %v", err)
	}

	decision, err := DecideAutoTransport(home, "codex-1", "slim-1")
	if err != nil {
		t.Fatalf("DecideAutoTransport: %v", err)
	}
	if decision.Mode != AutoModeHTTP || decision.Transport != TransportHTTP || !decision.WSSBridgeAvailable {
		t.Fatalf("decision=%+v", decision)
	}
	if !strings.Contains(decision.FallbackReason, "unreadable") ||
		!strings.Contains(decision.FallbackReason, "using HTTP savings path") {
		t.Fatalf("fallback reason=%q", decision.FallbackReason)
	}
	if !decision.NeedsRecert || decision.LastWSSError == "" {
		t.Fatalf("unreadable cert should be reported while bridge stays usable: %+v", decision)
	}
}

func TestDecideAutoTransportReportsUnreadableRecertAndBridgeProof(t *testing.T) {
	home := t.TempDir()
	for _, path := range []string{RecertStatePath(home), BridgeProofPath(home)} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	decision, err := DecideAutoTransport(home, "codex-1", "slim-1")
	if err != nil {
		t.Fatalf("DecideAutoTransport: %v", err)
	}
	if decision.Mode != AutoModeHTTP || decision.Transport != TransportHTTP {
		t.Fatalf("decision=%+v", decision)
	}
	var sawRecert, sawBridge bool
	for _, rejected := range decision.RejectedModes {
		if strings.Contains(rejected.Reason, "recert state unreadable") {
			sawRecert = true
		}
		if rejected.Mode == AutoModeWSSBridge && strings.Contains(rejected.Reason, "wss bridge proof unreadable") {
			sawBridge = true
		}
	}
	if !sawRecert || !sawBridge || decision.LastWSSError == "" {
		t.Fatalf("missing unreadable rejection details: %+v", decision)
	}
}

func TestCertificationStateFilesRoundTripDefaultsAndMalformedJSON(t *testing.T) {
	home := t.TempDir()
	if state, exists, err := LoadBridgeProof(home); err != nil || exists || state.Passed {
		t.Fatalf("missing bridge proof load state=%+v exists=%v err=%v", state, exists, err)
	}
	if state, exists, err := LoadRecertState(home); err != nil || exists || state.Status != "" {
		t.Fatalf("missing recert load state=%+v exists=%v err=%v", state, exists, err)
	}
	if err := SaveCertification(home, CertificationState{CodexVersion: "codex-1", SlimferenceVersion: "slim-1", Passed: true}); err != nil {
		t.Fatalf("SaveCertification: %v", err)
	}
	cert, exists, err := LoadCertification(home)
	if err != nil || !exists {
		t.Fatalf("LoadCertification exists=%v err=%v", exists, err)
	}
	if cert.SchemaVersion != CertificationSchemaVersion || cert.Transport != string(TransportWSS) ||
		cert.RouteProfile != RouteProfileScopedRawWSS || cert.Timestamp.IsZero() {
		t.Fatalf("cert defaults not applied: %+v", cert)
	}
	if err := SaveBridgeProof(home, BridgeProofState{CodexVersion: "codex-1", SlimferenceVersion: "slim-1", Passed: true, BytesC2S: 1, BytesS2C: 1, C2SFrames: 1}); err != nil {
		t.Fatalf("SaveBridgeProof: %v", err)
	}
	bridge, exists, err := LoadBridgeProof(home)
	if err != nil || !exists {
		t.Fatalf("LoadBridgeProof exists=%v err=%v", exists, err)
	}
	if bridge.SchemaVersion != CertificationSchemaVersion || bridge.Transport != string(TransportWSS) ||
		bridge.RouteProfile != RouteProfileScopedWSSBridge || bridge.Timestamp.IsZero() {
		t.Fatalf("bridge defaults not applied: %+v", bridge)
	}
	if err := SaveRecertState(home, RecertState{Status: "running", AttemptID: "attempt"}); err != nil {
		t.Fatalf("SaveRecertState: %v", err)
	}
	recert, exists, err := LoadRecertState(home)
	if err != nil || !exists || recert.SchemaVersion != RecertSchemaVersion || recert.AttemptID != "attempt" {
		t.Fatalf("recert load state=%+v exists=%v err=%v", recert, exists, err)
	}

	for _, path := range []string{CertificationPath(home), BridgeProofPath(home), RecertStatePath(home)} {
		if err := os.WriteFile(path, []byte("{bad-json"), 0o600); err != nil {
			t.Fatalf("write malformed %s: %v", path, err)
		}
	}
	if _, exists, err := LoadCertification(home); err == nil || !exists {
		t.Fatalf("malformed cert should return exists=true and error, exists=%v err=%v", exists, err)
	}
	if _, exists, err := LoadBridgeProof(home); err == nil || !exists {
		t.Fatalf("malformed bridge should return exists=true and error, exists=%v err=%v", exists, err)
	}
	if _, exists, err := LoadRecertState(home); err == nil || !exists {
		t.Fatalf("malformed recert should return exists=true and error, exists=%v err=%v", exists, err)
	}
}
