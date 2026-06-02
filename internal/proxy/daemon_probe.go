package proxy

import (
	"context"
	"os"

	"github.com/slimference/slimference/internal/control"
)

// DaemonProbe reports the in-process daemon state for /admin/state. It avoids
// a loopback HTTP self-call and gives the host-budget guard a real local RSS
// source.
type DaemonProbe struct {
	Proxy *Proxy
}

func (p DaemonProbe) ProbeDaemon(_ context.Context) control.DaemonState {
	if p.Proxy == nil {
		return control.DaemonState{}
	}
	snap := p.Proxy.daemonResourceSnapshot()
	stateBytes, _ := p.Proxy.daemonStateBytes()
	return control.DaemonState{
		Running:           true,
		HealthOK:          true,
		PID:               os.Getpid(),
		RSSBytes:          snap.RSSBytes,
		UptimeSec:         p.Proxy.uptimeSeconds(),
		CPUUserSeconds:    snap.CPUUserSeconds,
		CPUSystemSeconds:  snap.CPUSystemSeconds,
		CPUPercent:        snap.CPUPercent,
		CPUWindowPercent:  snap.CPUWindowPercent,
		CPUWindowSeconds:  snap.CPUWindowSeconds,
		DiskReadOps:       snap.DiskReadOps,
		DiskWriteOps:      snap.DiskWriteOps,
		DiskReadOpsDelta:  snap.DiskReadOpsDelta,
		DiskWriteOpsDelta: snap.DiskWriteOpsDelta,
		StateBytes:        stateBytes,
		Version:           Version,
	}
}
