package main

import (
	"context"

	"github.com/Christopher-Schulze/Slimference/internal/codexroute"
	"github.com/Christopher-Schulze/Slimference/internal/control"
)

// codexRouteProbe projects the scoped Codex provider route into
// /admin/state. It deliberately reports only the marker-owned Codex
// route and WSS auto-certification state; global lab routing remains in
// NetworkState and ListenerState.
type codexRouteProbe struct {
	home               string
	proxyURL           string
	codexVersionFn     func() string
	slimferenceVersion string
	healthFn           func(host, port string) error
	port               string
}

func (p *codexRouteProbe) ProbeCodexRoute(ctx context.Context) control.CodexRouteState {
	if p == nil || p.home == "" {
		return control.CodexRouteState{}
	}
	select {
	case <-ctx.Done():
		return control.CodexRouteState{}
	default:
	}

	status, err := codexroute.InspectWithOptions(p.home, p.proxyURL, codexroute.Options{})
	out := control.CodexRouteState{
		Path:       status.Path,
		Exists:     status.Exists,
		Enabled:    status.Enabled,
		Complete:   status.Complete,
		Conflict:   status.Conflict,
		LegacyKeys: status.LegacyKeys,
		BaseURL:    status.BaseURL,
		Transport:  status.Transport,
	}
	if err != nil {
		out.DaemonError = err.Error()
		return out
	}

	host := "127.0.0.1"
	port := p.port
	if port == "" {
		port = "8990"
	}
	if p.healthFn != nil {
		if err := p.healthFn(host, port); err != nil {
			out.DaemonError = err.Error()
		} else {
			out.DaemonReachable = true
		}
	}

	codexVersion := "unknown"
	if p.codexVersionFn != nil {
		codexVersion = p.codexVersionFn()
	}
	auto, _ := codexroute.DecideAutoTransport(p.home, codexVersion, p.slimferenceVersion)
	out.AutoTransport = string(auto.Transport)
	out.AutoMode = string(auto.Mode)
	out.WSSCertified = auto.WSSCertified
	out.WSSBridgeAvailable = auto.WSSBridgeAvailable
	out.NeedsRecert = auto.NeedsRecert
	out.CertifiedCodexVersion = auto.CertifiedCodex
	out.CertifiedSlimferenceVersion = auto.CertifiedSlimference
	out.BridgeCodexVersion = auto.BridgeCodex
	out.BridgeSlimferenceVersion = auto.BridgeSlimference
	out.CertificationPath = auto.CertificationPath
	out.BridgeProofPath = auto.BridgeProofPath
	out.RecertStatePath = auto.RecertStatePath
	out.RecertLogPath = auto.RecertLogPath
	out.RecertStatus = auto.RecertStatus
	out.RecertAttemptID = auto.RecertAttemptID
	out.RecertStartedAt = auto.RecertStartedAt
	out.RecertFinishedAt = auto.RecertFinishedAt
	out.RecertLastSuccessAt = auto.RecertLastSuccessAt
	out.RecertRetryAfter = auto.RecertRetryAfter
	out.RecertLastError = auto.RecertLastError
	out.RecertCommand = auto.RecertCommand
	out.FallbackReason = auto.FallbackReason
	out.LastWSSError = auto.LastWSSError
	return out
}
