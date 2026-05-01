package outputreduce

import "sync"

type Snapshot struct {
	Enabled              bool    `json:"enabled"`
	Profile              string  `json:"profile"`
	InjectedTurns        int64   `json:"injected_turns"`
	SkippedTurns         int64   `json:"skipped_turns"`
	InputOverheadTokens  int64   `json:"input_overhead_tokens"`
	OutputTokensObserved int64   `json:"output_tokens_observed"`
	AvgOutputTokens      float64 `json:"avg_output_tokens"`
	LastReason           string  `json:"last_reason"`
	LastAddedTokens      int     `json:"last_added_tokens"`
}

type Tracker struct {
	mu                   sync.Mutex
	enabled              bool
	profile              string
	injectedTurns        int64
	skippedTurns         int64
	inputOverheadTokens  int64
	outputTokensObserved int64
	lastReason           string
	lastAddedTokens      int
}

func NewTracker(enabled bool, profile string) *Tracker {
	return &Tracker{enabled: enabled, profile: profile}
}

func (t *Tracker) ObserveInjection(stats Stats) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if stats.Applied {
		t.injectedTurns++
		t.inputOverheadTokens += int64(stats.AddedTokens)
		t.lastAddedTokens = stats.AddedTokens
	} else {
		t.skippedTurns++
		t.lastAddedTokens = 0
	}
	if stats.Profile != "" {
		t.profile = stats.Profile
	}
	t.lastReason = stats.Reason
}

func (t *Tracker) ObserveOutput(outputTokens int) {
	if t == nil || outputTokens <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outputTokensObserved += int64(outputTokens)
}

func (t *Tracker) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	totalTurns := t.injectedTurns + t.skippedTurns
	avg := 0.0
	if totalTurns > 0 {
		avg = float64(t.outputTokensObserved) / float64(totalTurns)
	}
	return Snapshot{
		Enabled:              t.enabled,
		Profile:              t.profile,
		InjectedTurns:        t.injectedTurns,
		SkippedTurns:         t.skippedTurns,
		InputOverheadTokens:  t.inputOverheadTokens,
		OutputTokensObserved: t.outputTokensObserved,
		AvgOutputTokens:      avg,
		LastReason:           t.lastReason,
		LastAddedTokens:      t.lastAddedTokens,
	}
}
