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
	if tr.InCooldown("openai", "gpt", profile, ShapeCodeEdit) {
		t.Fatal("initial bucket must not be in cooldown")
	}
	tr.ObserveOutcome(Outcome{Provider: "openai", Model: "gpt", Profile: string(profile), TaskShape: ShapeCodeEdit, Applied: true, Failed: true})
	tr.ObserveOutcome(Outcome{Provider: "openai", Model: "gpt", Profile: string(profile), TaskShape: ShapeCodeEdit, Applied: true, Failed: true})
	if got := tr.SelectProfile("openai", "gpt", profile, ShapeCodeEdit); got != ProfileStandard {
		t.Fatalf("downgraded profile=%s", got)
	}
	if !tr.InCooldown("openai", "gpt", profile, ShapeCodeEdit) {
		t.Fatal("downgraded bucket should be in cooldown")
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

func TestTrackerAutoTuneDowngradesOnRepairSignal(t *testing.T) {
	t.Parallel()
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{
		Enabled:             true,
		MinSamples:          30,
		MaxFailureRateDelta: 0.1,
		CooldownTurns:       2,
	})
	tr.ObserveOutcome(Outcome{Provider: "codex", Model: "gpt", Profile: string(ProfileCodexAggressive), TaskShape: ShapeCodeEdit, Applied: true, OutputTokens: 100})
	tr.ObserveRepairSignal("codex", "gpt", ProfileCodexAggressive, ShapeCodeEdit)
	if got := tr.SelectProfile("codex", "gpt", ProfileCodexAggressive, ShapeCodeEdit); got != ProfileStandard {
		t.Fatalf("repair downgrade=%s", got)
	}
}

func TestTrackerAutoTuneRepairSignalCreatesCooldownBucket(t *testing.T) {
	t.Parallel()
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{
		Enabled:             true,
		MinSamples:          30,
		MaxFailureRateDelta: 0.1,
		CooldownTurns:       2,
	})
	tr.ObserveRepairSignal("codex", "gpt", ProfileCodexAggressive, ShapeDirectAnswer)
	if got := tr.SelectProfile("codex", "gpt", ProfileCodexAggressive, ShapeDirectAnswer); got != ProfileStandard {
		t.Fatalf("repair signal should immediately soften even without prior samples, got %s", got)
	}
	if !tr.InCooldown("codex", "gpt", ProfileCodexAggressive, ShapeDirectAnswer) {
		t.Fatal("repair-created bucket should enter cooldown")
	}
}

func TestTrackerAutoTuneSkipBranches(t *testing.T) {
	t.Parallel()
	var nilTracker *Tracker
	if got := nilTracker.SelectProfile("openai", "gpt", ProfileAggressive, ShapeUnknown); got != ProfileAggressive {
		t.Fatalf("nil select=%s", got)
	}
	if nilTracker.InCooldown("openai", "gpt", ProfileAggressive, ShapeUnknown) {
		t.Fatal("nil tracker must not report cooldown")
	}
	nilTracker.ObserveOutcome(Outcome{Applied: true})
	nilTracker.ObserveRepairSignal("openai", "gpt", ProfileAggressive, ShapeUnknown)
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{Enabled: false})
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeUnknown); got != ProfileAggressive {
		t.Fatalf("disabled select=%s", got)
	}
	if tr.InCooldown("openai", "gpt", ProfileAggressive, ShapeUnknown) {
		t.Fatal("disabled tuner must not report cooldown")
	}
	tr.ObserveRepairSignal("openai", "gpt", ProfileAggressive, ShapeUnknown)
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
	tr.ObserveRepairSignal("missing", "m", ProfileAggressive, ShapeDebugging)

	tr = NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{Enabled: true, MinSamples: 3, MaxFailureRateDelta: 0.1, CooldownTurns: 2})
	tr.ObserveOutcome(Outcome{Provider: "p", Model: "m", Profile: string(ProfileAggressive), TaskShape: ShapeCodeEdit, Applied: true, OutputTokens: 100})
	tr.ObserveRepairSignal("p", "m", ProfileAggressive, ShapeCodeEdit)
	if got := tr.SelectProfile("p", "m", ProfileAggressive, ShapeCodeEdit); got != ProfileStandard {
		t.Fatalf("repair signal must downgrade without waiting for min samples=%s", got)
	}
	tr = NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{Enabled: true, MinSamples: 1, MaxFailureRateDelta: 0.1, CooldownTurns: 2})
	tr.ObserveOutcome(Outcome{Provider: "p", Model: "m", Profile: string(ProfileAggressive), TaskShape: ShapeReview, Applied: true, Failed: true})
	tr.ObserveRepairSignal("p", "m", ProfileAggressive, ShapeReview)
}

func TestTrackerAutoTuneCooldownExpires(t *testing.T) {
	t.Parallel()
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{
		Enabled:             true,
		MinSamples:          1,
		MaxFailureRateDelta: 0.1,
		CooldownTurns:       2,
	})
	outcome := Outcome{
		Provider:  "openai",
		Model:     "gpt",
		Profile:   string(ProfileAggressive),
		TaskShape: ShapeDebugging,
		Applied:   true,
		Failed:    true,
	}
	tr.ObserveOutcome(outcome)
	if !tr.InCooldown("openai", "gpt", ProfileAggressive, ShapeDebugging) {
		t.Fatal("bucket should enter cooldown after downgrade")
	}
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeDebugging); got != ProfileStandard {
		t.Fatalf("first cooldown request should use softened profile, got %s", got)
	}
	if !tr.InCooldown("openai", "gpt", ProfileAggressive, ShapeDebugging) {
		t.Fatal("cooldown should remain after one decrement")
	}
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeDebugging); got != ProfileStandard {
		t.Fatalf("last cooldown request should still use softened profile, got %s", got)
	}
	if tr.InCooldown("openai", "gpt", ProfileAggressive, ShapeDebugging) {
		t.Fatal("cooldown should expire after configured turns")
	}
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeDebugging); got != ProfileAggressive {
		t.Fatalf("expired cooldown must restore requested profile, got %s", got)
	}
}

func TestTrackerAutoTuneExpiredCooldownNextDowngradeSoftensOneStep(t *testing.T) {
	t.Parallel()
	tr := NewTrackerWithAutoTune(true, "auto", AutoTuneConfig{
		Enabled:             true,
		MinSamples:          1,
		MaxFailureRateDelta: 0.1,
		CooldownTurns:       1,
	})
	outcome := Outcome{
		Provider:  "openai",
		Model:     "gpt",
		Profile:   string(ProfileAggressive),
		TaskShape: ShapeDebugging,
		Applied:   true,
		Failed:    true,
	}
	tr.ObserveOutcome(outcome)
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeDebugging); got != ProfileStandard {
		t.Fatalf("first downgrade should soften aggressive to standard, got %s", got)
	}
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeDebugging); got != ProfileAggressive {
		t.Fatalf("expired cooldown should restore aggressive, got %s", got)
	}
	tr.ObserveOutcome(outcome)
	if got := tr.SelectProfile("openai", "gpt", ProfileAggressive, ShapeDebugging); got != ProfileStandard {
		t.Fatalf("second downgrade should still soften one step to standard, got %s", got)
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
