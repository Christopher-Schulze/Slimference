package outputreduce

import "sync"

type AutoTuneConfig struct {
	Enabled             bool
	MinSamples          int
	MinNetSavingsPct    float64
	MaxFailureRateDelta float64
	CooldownTurns       int
}

type Outcome struct {
	Provider            string
	Model               string
	Profile             string
	TaskShape           TaskShape
	Applied             bool
	InputOverheadTokens int
	OutputTokens        int
	Failed              bool
	RepairSignal        bool
	UserReaskSignal     bool
}

type Snapshot struct {
	Enabled              bool        `json:"enabled"`
	Profile              string      `json:"profile"`
	AutoTuneEnabled      bool        `json:"auto_tune_enabled"`
	InjectedTurns        int64       `json:"injected_turns"`
	SkippedTurns         int64       `json:"skipped_turns"`
	InputOverheadTokens  int64       `json:"input_overhead_tokens"`
	OutputTokensObserved int64       `json:"output_tokens_observed"`
	AvgOutputTokens      float64     `json:"avg_output_tokens"`
	LastReason           string      `json:"last_reason"`
	LastAddedTokens      int         `json:"last_added_tokens"`
	Downgrades           []Downgrade `json:"downgrades,omitempty"`
}

type Tracker struct {
	mu                   sync.Mutex
	enabled              bool
	profile              string
	auto                 AutoTuneConfig
	injectedTurns        int64
	skippedTurns         int64
	inputOverheadTokens  int64
	outputTokensObserved int64
	lastReason           string
	lastAddedTokens      int
	buckets              map[string]*bucket
	downgrades           map[string]Profile
}

func NewTracker(enabled bool, profile string) *Tracker {
	return NewTrackerWithAutoTune(enabled, profile, AutoTuneConfig{})
}

func NewTrackerWithAutoTune(enabled bool, profile string, auto AutoTuneConfig) *Tracker {
	if auto.MinSamples <= 0 {
		auto.MinSamples = 30
	}
	if auto.CooldownTurns <= 0 {
		auto.CooldownTurns = 20
	}
	return &Tracker{
		enabled:    enabled,
		profile:    profile,
		auto:       auto,
		buckets:    make(map[string]*bucket),
		downgrades: make(map[string]Profile),
	}
}

func (t *Tracker) SelectProfile(provider, model string, requested Profile, shape TaskShape) Profile {
	if t == nil || !t.auto.Enabled {
		return requested
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if downgraded, ok := t.downgrades[bucketKey(provider, model, requested, shape)]; ok {
		return downgraded
	}
	return requested
}

func (t *Tracker) InCooldown(provider, model string, profile Profile, shape TaskShape) bool {
	if t == nil || !t.auto.Enabled {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buckets[bucketKey(provider, model, profile, shape)]
	return b != nil && b.cooldown > 0
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

func (t *Tracker) ObserveOutcome(outcome Outcome) {
	if t == nil || !t.auto.Enabled || !outcome.Applied {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := bucketKey(outcome.Provider, outcome.Model, Profile(outcome.Profile), outcome.TaskShape)
	b := t.buckets[key]
	if b == nil {
		b = &bucket{profile: Profile(outcome.Profile)}
		t.buckets[key] = b
	}
	b.samples++
	b.inputOverheadTokens += int64(outcome.InputOverheadTokens)
	b.outputTokens += int64(outcome.OutputTokens)
	if outcome.Failed || outcome.RepairSignal || outcome.UserReaskSignal {
		b.failures++
	}
	if b.cooldown > 0 {
		b.cooldown--
		return
	}
	if b.samples < int64(t.auto.MinSamples) {
		return
	}
	if shouldDowngrade(b, t.auto) {
		next := NextSofter(b.profile)
		t.downgrades[key] = next
		b.profile = next
		b.cooldown = int64(t.auto.CooldownTurns)
		b.samples = 0
		b.failures = 0
		b.inputOverheadTokens = 0
		b.outputTokens = 0
	}
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
		AutoTuneEnabled:      t.auto.Enabled,
		InjectedTurns:        t.injectedTurns,
		SkippedTurns:         t.skippedTurns,
		InputOverheadTokens:  t.inputOverheadTokens,
		OutputTokensObserved: t.outputTokensObserved,
		AvgOutputTokens:      avg,
		LastReason:           t.lastReason,
		LastAddedTokens:      t.lastAddedTokens,
		Downgrades:           t.snapshotDowngradesLocked(),
	}
}

type Downgrade struct {
	Key     string `json:"key"`
	Profile string `json:"profile"`
}

type bucket struct {
	profile             Profile
	samples             int64
	failures            int64
	inputOverheadTokens int64
	outputTokens        int64
	cooldown            int64
}

func shouldDowngrade(b *bucket, cfg AutoTuneConfig) bool {
	if b.samples == 0 {
		return false
	}
	failureRate := float64(b.failures) / float64(b.samples)
	if cfg.MaxFailureRateDelta > 0 && failureRate > cfg.MaxFailureRateDelta {
		return true
	}
	if b.outputTokens > 0 {
		overheadPct := float64(b.inputOverheadTokens) / float64(b.outputTokens) * 100
		return overheadPct > cfg.MinNetSavingsPct
	}
	return false
}

func (t *Tracker) snapshotDowngradesLocked() []Downgrade {
	if len(t.downgrades) == 0 {
		return nil
	}
	out := make([]Downgrade, 0, len(t.downgrades))
	for key, profile := range t.downgrades {
		out = append(out, Downgrade{Key: key, Profile: string(profile)})
	}
	return out
}

func bucketKey(provider, model string, profile Profile, shape TaskShape) string {
	if model == "" {
		model = "unknown-model"
	}
	if shape == "" {
		shape = ShapeUnknown
	}
	return provider + "/" + model + "/" + string(shape) + "/" + string(profile)
}
