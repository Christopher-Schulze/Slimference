package proxy

import (
	"context"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/control"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestDispatcherRecordsSocketLifecycleRingAndCloseCounters(t *testing.T) {
	d := &PhaseFDispatcher{}
	phaseF := wsPhaseFTelemetry{TerminalResponsesSeen: 2}
	snap := wsmitm.SessionTelemetry{
		C2SFrames:        3,
		S2CFrames:        4,
		C2SBytes:         300,
		S2CBytes:         400,
		OpenedAtUnixNano: 1000,
		ClosedAtUnixNano: 6000,
		AgeMillis:        5,
		CloseInitiator:   "client_eof",
	}
	d.finishActiveWSMITMSession(42, snap, phaseF)

	got := d.Snapshot()
	if got.WSMITMSocketsClosed != 1 || got.WSMITMClientEOF != 1 {
		t.Fatalf("close counters not recorded: %+v", got)
	}
	if len(got.RecentSockets) != 1 {
		t.Fatalf("recent sockets len=%d want 1", len(got.RecentSockets))
	}
	recent := got.RecentSockets[0]
	if recent.SocketSeq != 42 || recent.CloseInitiator != "client_eof" ||
		recent.TurnsCompleted != 2 || recent.C2SFrames != 3 || recent.Active {
		t.Fatalf("bad lifecycle entry: %+v", recent)
	}
}

func TestDispatcherSocketLifecycleRingIsBoundedNewestFirst(t *testing.T) {
	d := &PhaseFDispatcher{}
	for i := uint64(1); i <= wssRecentSocketLifecycleLimit+2; i++ {
		d.finishActiveWSMITMSession(i, wsmitm.SessionTelemetry{
			CloseInitiator: "upstream_eof",
		}, wsPhaseFTelemetry{})
	}
	got := d.Snapshot()
	if len(got.RecentSockets) != wssRecentSocketLifecycleLimit {
		t.Fatalf("recent socket ring len=%d want %d", len(got.RecentSockets), wssRecentSocketLifecycleLimit)
	}
	if got.RecentSockets[0].SocketSeq != wssRecentSocketLifecycleLimit+2 {
		t.Fatalf("newest socket not first: %+v", got.RecentSockets[:2])
	}
	if got.WSMITMSocketsClosed != wssRecentSocketLifecycleLimit+2 ||
		got.WSMITMUpstreamEOF != wssRecentSocketLifecycleLimit+2 {
		t.Fatalf("aggregate close counters wrong: %+v", got)
	}
}

func TestWSSProbeMapsSocketLifecycleTelemetry(t *testing.T) {
	p := &Proxy{}
	dispatcher := &PhaseFDispatcher{}
	dispatcher.finishActiveWSMITMSession(7, wsmitm.SessionTelemetry{
		OpenedAtUnixNano: 10,
		ClosedAtUnixNano: 20,
		AgeMillis:        1,
		CloseInitiator:   "our_error",
		CloseError:       "handler: boom",
		C2SFrames:        1,
		S2CFrames:        2,
	}, wsPhaseFTelemetry{TerminalResponsesSeen: 1})
	p.SetWSSDispatcher(dispatcher)

	state := (WSSProbe{Proxy: p}).ProbeWSS(context.Background())
	if state.SocketsClosed != 1 || state.OurErrors != 1 {
		t.Fatalf("state close counters wrong: %+v", state)
	}
	if len(state.RecentSockets) != 1 {
		t.Fatalf("recent sockets len=%d want 1", len(state.RecentSockets))
	}
	want := control.WSSSocketLifecycle{
		SocketSeq:        7,
		OpenedAtUnixNano: 10,
		ClosedAtUnixNano: 20,
		AgeMillis:        1,
		CloseInitiator:   "our_error",
		CloseError:       "handler: boom",
		C2SFrames:        1,
		S2CFrames:        2,
		TurnsCompleted:   1,
	}
	if state.RecentSockets[0] != want {
		t.Fatalf("recent lifecycle mismatch:\ngot  %+v\nwant %+v", state.RecentSockets[0], want)
	}
}

func TestWSSRequestDebugFactsIncludeSocketSeq(t *testing.T) {
	facts := wssRequestDebugFacts(nil, nil, nil, proxyLayer0Stats{}, false, "", wssRequestMeta{
		SocketSeq: 99,
	}, outputreduce.Stats{})
	if got := facts["wss.socket_seq"]; got != "99" {
		t.Fatalf("wss.socket_seq=%q want 99", got)
	}
}
