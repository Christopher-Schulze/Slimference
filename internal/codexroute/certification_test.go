package codexroute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if decision.Transport != TransportWSS || !decision.WSSCertified || decision.FallbackReason != "" {
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
			wantRecert := strings.Contains(tc.name, "version changed")
			if decision.NeedsRecert != wantRecert {
				t.Fatalf("needs_recert=%v want %v for %s", decision.NeedsRecert, wantRecert, tc.name)
			}
			if wantRecert && decision.RecertCommand != "slimference codex certify wss" {
				t.Fatalf("recert command=%q", decision.RecertCommand)
			}
			if decision.CurrentCodex != tc.current || decision.CurrentSlimference != tc.slim {
				t.Fatalf("current tuple codex=%q slim=%q", decision.CurrentCodex, decision.CurrentSlimference)
			}
		})
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
