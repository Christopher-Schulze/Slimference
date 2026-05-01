package quality

import "sync"

type LoopNudgeMeasurement struct {
	StreakLen           int
	NudgeEmitted        bool
	NextTurnSimilarity  float64
	LoopBroken          bool
	ObservedSavedTokens int
}

type LoopBreakTracker struct {
	mu         sync.Mutex
	detected   int64
	broken     int64
	totalSaved int64
	byStrategy map[string]*loopStrategyCounters
}

type loopStrategyCounters struct {
	detected int64
	broken   int64
	saved    int64
}

type LoopBreakStats struct {
	DetectedTotal int64                             `json:"detected_total"`
	BrokenTotal   int64                             `json:"broken_total"`
	BrokenRate    float64                           `json:"broken_rate"`
	AvgSaved      float64                           `json:"avg_observed_savings"`
	ByStrategy    map[string]LoopBreakStrategyStats `json:"by_strategy"`
}

type LoopBreakStrategyStats struct {
	Detected int64   `json:"detected"`
	Broken   int64   `json:"broken"`
	AvgSaved float64 `json:"avg_saved"`
}

func NewLoopBreakTracker() *LoopBreakTracker {
	return &LoopBreakTracker{
		byStrategy: make(map[string]*loopStrategyCounters),
	}
}

func (t *LoopBreakTracker) Record(m LoopNudgeMeasurement, strategy string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.detected++
	if m.LoopBroken {
		t.broken++
	}
	t.totalSaved += int64(m.ObservedSavedTokens)
	sc, ok := t.byStrategy[strategy]
	if !ok {
		sc = &loopStrategyCounters{}
		t.byStrategy[strategy] = sc
	}
	sc.detected++
	if m.LoopBroken {
		sc.broken++
	}
	sc.saved += int64(m.ObservedSavedTokens)
}

func (t *LoopBreakTracker) Stats() LoopBreakStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	rate := 0.0
	avg := 0.0
	if t.detected > 0 {
		rate = float64(t.broken) / float64(t.detected)
		avg = float64(t.totalSaved) / float64(t.detected)
	}
	byStrat := make(map[string]LoopBreakStrategyStats, len(t.byStrategy))
	for k, v := range t.byStrategy {
		sAvg := 0.0
		if v.detected > 0 {
			sAvg = float64(v.saved) / float64(v.detected)
		}
		byStrat[k] = LoopBreakStrategyStats{
			Detected: v.detected,
			Broken:   v.broken,
			AvgSaved: sAvg,
		}
	}
	return LoopBreakStats{
		DetectedTotal: t.detected,
		BrokenTotal:   t.broken,
		BrokenRate:    rate,
		AvgSaved:      avg,
		ByStrategy:    byStrat,
	}
}

func (t *LoopBreakTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.detected = 0
	t.broken = 0
	t.totalSaved = 0
	t.byStrategy = make(map[string]*loopStrategyCounters)
}
