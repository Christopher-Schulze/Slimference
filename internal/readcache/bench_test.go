package readcache

import (
	"strings"
	"testing"
)

func BenchmarkEvaluateObserved_FullReadRepeat64KB(b *testing.B) {
	dir := b.TempDir()
	archiveDir := b.TempDir()
	req := Request{SessionID: "bench-full", TurnID: "turn-1", FilePath: "main.go"}
	body := "package main\n" + strings.Repeat("func benchmarkFullRead() {}\n", 2400)
	if decision, err := EvaluateObserved(dir, req, body, archiveDir, false); err != nil || decision.Type != DecisionAllow {
		b.Fatalf("seed decision=%+v err=%v", decision, err)
	}
	b.Cleanup(func() {
		_ = FlushDir(dir)
		clearMemoryDir(dir)
	})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		decision, err := EvaluateObserved(dir, req, body, archiveDir, false)
		if err != nil {
			b.Fatal(err)
		}
		if decision.Type != DecisionBlock || decision.BlockKind != BlockKindUnchanged {
			b.Fatalf("expected unchanged block, got %+v", decision)
		}
	}
}

func BenchmarkEvaluateObserved_RangedReadRepeat16KB(b *testing.B) {
	dir := b.TempDir()
	archiveDir := b.TempDir()
	req := Request{SessionID: "bench-range", TurnID: "turn-1", FilePath: "main.go", Offset: 1, Limit: 400}
	body := strings.Repeat("range benchmark line\n", 800)
	if decision, err := EvaluateObserved(dir, req, body, archiveDir, false); err != nil || decision.Type != DecisionAllow {
		b.Fatalf("seed decision=%+v err=%v", decision, err)
	}
	b.Cleanup(func() {
		_ = FlushDir(dir)
		clearMemoryDir(dir)
	})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		decision, err := EvaluateObserved(dir, req, body, archiveDir, false)
		if err != nil {
			b.Fatal(err)
		}
		if decision.Type != DecisionBlock || decision.BlockKind != BlockKindUnchanged {
			b.Fatalf("expected unchanged range block, got %+v", decision)
		}
	}
}
