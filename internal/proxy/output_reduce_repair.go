package proxy

import (
	"strings"

	"github.com/slimference/slimference/internal/outputreduce"
)

type pendingOutputReduceSignal struct {
	Provider string
	Model    string
	Profile  outputreduce.Profile
	Shape    outputreduce.TaskShape
}

func (p *Proxy) rememberOutputReduceSignal(sessionID string, outcome outputreduce.Outcome) {
	if p == nil || !outcome.Applied {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || outcome.Profile == "" {
		return
	}
	p.outputReduceRepairMu.Lock()
	defer p.outputReduceRepairMu.Unlock()
	if p.outputReduceRepair == nil {
		p.outputReduceRepair = make(map[string]pendingOutputReduceSignal)
	}
	p.outputReduceRepair[sessionID] = pendingOutputReduceSignal{
		Provider: outcome.Provider,
		Model:    outcome.Model,
		Profile:  outputreduce.Profile(outcome.Profile),
		Shape:    outcome.TaskShape,
	}
}

func (p *Proxy) consumeOutputReduceRepairSignal(sessionID string, signal outputreduce.RepairSignal) bool {
	if p == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	p.outputReduceRepairMu.Lock()
	pending, ok := p.outputReduceRepair[sessionID]
	if ok {
		delete(p.outputReduceRepair, sessionID)
	}
	p.outputReduceRepairMu.Unlock()
	if !signal.Repair || !ok || pending.Profile == "" {
		return false
	}
	if p.outputReduce != nil {
		p.outputReduce.ObserveRepairSignal(pending.Provider, pending.Model, pending.Profile, pending.Shape)
	}
	return true
}
