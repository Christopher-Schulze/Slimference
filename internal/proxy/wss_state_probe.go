package proxy

import (
	"context"

	"github.com/slimference/slimference/internal/control"
)

// SetWSSDispatcher installs the active WebSocket dispatcher for
// /admin/state telemetry. Passing nil clears it.
func (p *Proxy) SetWSSDispatcher(d *PhaseFDispatcher) {
	p.wssDispatcherPtr.Store(d)
}

// WSSDispatcher returns the currently wired WebSocket dispatcher, or
// nil before proxy construction / after explicit clearing.
func (p *Proxy) WSSDispatcher() *PhaseFDispatcher {
	return p.wssDispatcherPtr.Load()
}

// WSSProbe maps the PhaseFDispatcher transport counters into the
// control.SetupState WSS block.
type WSSProbe struct {
	Proxy *Proxy
}

// ProbeWSS implements control.WSSProbe.
func (p WSSProbe) ProbeWSS(_ context.Context) control.WSSState {
	if p.Proxy == nil {
		return control.WSSState{}
	}
	dispatcher := p.Proxy.WSSDispatcher()
	if dispatcher == nil {
		return control.WSSState{}
	}
	snap := dispatcher.Snapshot()
	state := control.WSSState{
		EngineActive:       true,
		PassthroughBridged: snap.PassthroughBridged,
		MITMBridged:        snap.MITMBridged,
		Rejected:           snap.Rejected,
		UpstreamDialFail:   snap.UpstreamDialFail,
		BytesC2S:           snap.BytesC2S,
		BytesS2C:           snap.BytesS2C,
		C2SFrames:          snap.WSMITMC2SFrames,
		S2CFrames:          snap.WSMITMS2CFrames,
		ParseFailures:      snap.WSMITMParseFailures,
		DegradedSessions:   snap.WSMITMDegraded,
		FramesReencoded:    snap.WSMITMReencoded,
		FramesForwarded:    snap.WSMITMForwarded,
	}
	state.MutationActive = snap.WSMITMReencoded > 0
	state.ByteBridgeOnly = snap.WSMITMReencoded == 0 && snap.WSMITMForwarded > 0
	return state
}
