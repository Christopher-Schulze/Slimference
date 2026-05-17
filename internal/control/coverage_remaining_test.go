package control

import (
	"context"
	"testing"
	"time"
)

type wssProbeFunc func(context.Context) WSSState

func (f wssProbeFunc) ProbeWSS(ctx context.Context) WSSState { return f(ctx) }

type codexRouteProbeFunc func(context.Context) CodexRouteState

func (f codexRouteProbeFunc) ProbeCodexRoute(ctx context.Context) CodexRouteState {
	return f(ctx)
}

func TestBuildIncludesWSSProbe(t *testing.T) {
	wantTime := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	got := Build(context.Background(), Probes{
		CodexRoute: codexRouteProbeFunc(func(context.Context) CodexRouteState {
			return CodexRouteState{
				Enabled:       true,
				Complete:      true,
				AutoTransport: "wss",
				WSSCertified:  true,
			}
		}),
		WSS: wssProbeFunc(func(context.Context) WSSState {
			return WSSState{EngineActive: true, FramesReencoded: 3, MutationActive: true}
		}),
		Clock: func() time.Time { return wantTime },
	})
	if !got.CodexRoute.Enabled || !got.CodexRoute.WSSCertified || got.CodexRoute.AutoTransport != "wss" {
		t.Fatalf("Codex route state not propagated: %+v", got.CodexRoute)
	}
	if !got.WSS.EngineActive || got.WSS.FramesReencoded != 3 || !got.WSS.MutationActive {
		t.Fatalf("WSS state not propagated: %+v", got.WSS)
	}
	if !got.UpdatedAt.Equal(wantTime) {
		t.Fatalf("UpdatedAt=%v want %v", got.UpdatedAt, wantTime)
	}
}
