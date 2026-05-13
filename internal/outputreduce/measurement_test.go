package outputreduce

import "testing"

func TestTrackerSnapshot(t *testing.T) {
	t.Parallel()
	tr := NewTracker(true, "auto")
	tr.ObserveInjection(Stats{Applied: true, Profile: "openai", AddedTokens: 7, Reason: "applied"})
	tr.ObserveInjection(Stats{Applied: false, Profile: "openai", Reason: "already_present"})
	tr.ObserveOutput(30)
	tr.ObserveOutput(10)
	snap := tr.Snapshot()
	if !snap.Enabled || snap.Profile != "openai" || snap.InjectedTurns != 1 || snap.SkippedTurns != 1 {
		t.Fatalf("snapshot: %+v", snap)
	}
	if snap.InputOverheadTokens != 7 || snap.OutputTokensObserved != 40 || snap.AvgOutputTokens != 20 {
		t.Fatalf("snapshot: %+v", snap)
	}
}

func TestNilTrackerSafe(t *testing.T) {
	var tr *Tracker
	tr.ObserveInjection(Stats{Applied: true})
	tr.ObserveOutput(10)
	if snap := tr.Snapshot(); snap.Enabled {
		t.Fatalf("nil snapshot: %+v", snap)
	}
}

func TestTrackerAutoTuneDowngradesOnFailures(t *testing.T) {
	t.Parallel()
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{
		Enabled:             true,
		MinSamples:          2,
		MinNetSavingsPct:    50,
		MaxFailureRateDelta: 0.1,
		CooldownTurns:       3,
	})
	profile := ProfileAggressive
	if got := tr.SelectProfile("openai", "gpt", profile, ShapeCodeEdit); got != ProfileAggressive {
		t.Fatalf("initial profile=%s", got)
	}
	tr.ObserveOutcome(Outcome{Provider: "openai", Model: "gpt", Profile: string(profile), TaskShape: ShapeCodeEdit, Applied: true, Failed: true})
	tr.ObserveOutcome(Outcome{Provider: "openai", Model: "gpt", Profile: string(profile), TaskShape: ShapeCodeEdit, Applied: true, Failed: true})
	if got := tr.SelectProfile("openai", "gpt", profile, ShapeCodeEdit); got != ProfileStandard {
		t.Fatalf("downgraded profile=%s", got)
	}
	snap := tr.Snapshot()
	if !snap.AutoTuneEnabled || len(snap.Downgrades) != 1 {
		t.Fatalf("snapshot=%+v", snap)
	}
}

func TestTrackerAutoTuneDowngradesOnOverhead(t *testing.T) {
	t.Parallel()
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{
		Enabled:          true,
		MinSamples:       1,
		MinNetSavingsPct: 10,
		CooldownTurns:    1,
	})
	tr.ObserveOutcome(Outcome{Provider: "codex", Model: "x", Profile: string(ProfileCodexAggressive), TaskShape: ShapeDirectAnswer, Applied: true, InputOverheadTokens: 20, OutputTokens: 100})
	if got := tr.SelectProfile("codex", "x", ProfileCodexAggressive, ShapeDirectAnswer); got != ProfileStandard {
		t.Fatalf("downgraded profile=%s", got)
	}
}

func TestTrackerAutoTuneSkipBranches(t *testing.T) {
	t.Parallel()
	var nilTracker *Tracker
	if got := nilTracker.SelectProfile("openai", "gpt", ProfileAggressive, ShapeUnknown); got != ProfileAggressive {
		t.Fatalf("nil select=%s", got)
	}
	nilTracker.ObserveOutcome(Outcome{Applied: true})
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{Enabled: false})
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeUnknown); got != ProfileAggressive {
		t.Fatalf("disabled select=%s", got)
	}
	tr.ObserveOutcome(Outcome{Applied: true, Profile: string(ProfileAggressive)})
	if len(tr.Snapshot().Downgrades) != 0 {
		t.Fatal("disabled tuner must not downgrade")
	}
	tr = NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{Enabled: true, MinSamples: 2, MinNetSavingsPct: 90})
	tr.ObserveOutcome(Outcome{Provider: "p", Model: "", Profile: string(ProfileStandard), TaskShape: "", Applied: false})
	tr.ObserveOutcome(Outcome{Provider: "p", Model: "", Profile: string(ProfileStandard), TaskShape: "", Applied: true, InputOverheadTokens: 1, OutputTokens: 100})
	if got := tr.SelectProfile("p", "", ProfileStandard, ""); got != ProfileStandard {
		t.Fatalf("unexpected downgrade=%s", got)
	}
	tr = NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{Enabled: true, MinSamples: 1, MaxFailureRateDelta: 0.1, CooldownTurns: 2})
	tr.ObserveOutcome(Outcome{Provider: "p", Model: "m", Profile: string(ProfileAggressive), TaskShape: ShapeDebugging, Applied: true, Failed: true})
	tr.ObserveOutcome(Outcome{Provider: "p", Model: "m", Profile: string(ProfileAggressive), TaskShape: ShapeDebugging, Applied: true, Failed: true})
	if got := tr.SelectProfile("p", "m", ProfileAggressive, ShapeDebugging); got != ProfileStandard {
		t.Fatalf("cooldown downgrade=%s", got)
	}
}

func TestShouldDowngradeBranches(t *testing.T) {
	t.Parallel()
	if shouldDowngrade(&bucket{}, AutoTuneConfig{MaxFailureRateDelta: 0.1}) {
		t.Fatal("empty bucket must not downgrade")
	}
	if !shouldDowngrade(&bucket{samples: 1, failures: 1}, AutoTuneConfig{MaxFailureRateDelta: 0.1}) {
		t.Fatal("failure rate should downgrade")
	}
	if shouldDowngrade(&bucket{samples: 1, inputOverheadTokens: 1, outputTokens: 100}, AutoTuneConfig{MinNetSavingsPct: 50}) {
		t.Fatal("low overhead should not downgrade")
	}
	if shouldDowngrade(&bucket{samples: 1, inputOverheadTokens: 10}, AutoTuneConfig{MinNetSavingsPct: 1}) {
		t.Fatal("no output baseline should not downgrade")
	}
	if key := bucketKey("p", "", ProfileMild, ""); key != "p/unknown-model/unknown/mild" {
		t.Fatalf("bucket key=%q", key)
	}
}
