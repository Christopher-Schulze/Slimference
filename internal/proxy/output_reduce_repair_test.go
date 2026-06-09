package proxy

import (
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/outputreduce"
)

func TestOutputReduceRepairSignalLifecycle(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.AutoTuneEnabled = true
	cfg.Compression.OutputReduce.AutoTuneMinSamples = 1
	cfg.Compression.OutputReduce.MaxFailureRateDelta = 0.1
	p := New(cfg)

	outcome := outputreduce.Outcome{
		Provider:     "codex_chatgpt",
		Model:        "gpt",
		Profile:      string(outputreduce.ProfileCodexAggressive),
		TaskShape:    outputreduce.ShapeCodeEdit,
		Applied:      true,
		OutputTokens: 100,
	}
	p.outputReduce.ObserveOutcome(outcome)
	p.rememberOutputReduceSignal("sess", outcome)

	if !p.consumeOutputReduceRepairSignal("sess", outputreduce.RepairSignal{Repair: true, UserReask: true, Reason: "user_reask"}) {
		t.Fatal("expected repair signal to consume pending output-reduce bucket")
	}
	if got := p.outputReduce.SelectProfile("codex_chatgpt", "gpt", outputreduce.ProfileCodexAggressive, outputreduce.ShapeCodeEdit); got != outputreduce.ProfileStandard {
		t.Fatalf("repair signal should downgrade profile, got %s", got)
	}
	if p.consumeOutputReduceRepairSignal("sess", outputreduce.RepairSignal{Repair: true}) {
		t.Fatal("pending signal must be one-shot")
	}
}

func TestOutputReduceRepairSignalSkipBranches(t *testing.T) {
	t.Parallel()
	var nilProxy *Proxy
	nilProxy.rememberOutputReduceSignal("sess", outputreduce.Outcome{Applied: true, Profile: string(outputreduce.ProfileAggressive)})
	if nilProxy.consumeOutputReduceRepairSignal("sess", outputreduce.RepairSignal{Repair: true}) {
		t.Fatal("nil proxy must not consume repair")
	}

	p := New(config.Defaults())
	p.outputReduceRepair = nil
	p.rememberOutputReduceSignal("new-map", outputreduce.Outcome{Applied: true, Profile: string(outputreduce.ProfileAggressive), Provider: "p"})
	if p.outputReduceRepair == nil {
		t.Fatal("remember must initialize missing map")
	}
	p.rememberOutputReduceSignal("", outputreduce.Outcome{Applied: true, Profile: string(outputreduce.ProfileAggressive)})
	p.rememberOutputReduceSignal("sess", outputreduce.Outcome{Applied: false, Profile: string(outputreduce.ProfileAggressive)})
	p.rememberOutputReduceSignal("sess", outputreduce.Outcome{Applied: true})
	if p.consumeOutputReduceRepairSignal("", outputreduce.RepairSignal{Repair: true}) {
		t.Fatal("empty session must not consume repair")
	}
	p.rememberOutputReduceSignal("sess", outputreduce.Outcome{
		Provider:  "codex_chatgpt",
		Model:     "gpt",
		Profile:   string(outputreduce.ProfileAggressive),
		TaskShape: outputreduce.ShapeReview,
		Applied:   true,
	})
	if p.consumeOutputReduceRepairSignal("sess", outputreduce.RepairSignal{}) {
		t.Fatal("non-repair signal must not consume repair")
	}
	if p.consumeOutputReduceRepairSignal("sess", outputreduce.RepairSignal{Repair: true}) {
		t.Fatal("non-repair next turn must discard stale pending bucket")
	}
	if p.consumeOutputReduceRepairSignal("missing", outputreduce.RepairSignal{Repair: true}) {
		t.Fatal("missing pending bucket must not consume repair")
	}
}
