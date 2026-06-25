package filter

// Adaptive per-command-class compaction budget (self-tuning L2, zero-drawdown by
// construction). L2 command-output-first compacts a command's output before it
// enters history, with exact archive recovery. Recovery is free of comprehension
// drawdown (the model gets the exact bytes) but carries a token cost: a compacted
// output the model later needs costs compacted + full-re-fetch instead of full.
// So compaction is net-positive on a class only while its re-fetch rate stays
// below break-even.
//
// AdaptiveCompactionShouldFullPass is the deterministic control: it returns true
// (full-pass = emit the full output, strictly safer) only when a class is PROVEN
// net-negative by its measured re-fetch rate. Until enough samples exist it
// returns false, i.e. exactly today's fixed L2 behavior. The mechanism can
// therefore only HOLD or IMPROVE net savings, never regress below the current
// lane: it removes the residual net-negative slice on re-fetch-heavy classes
// while leaving every other class untouched.
//
// Zero-drawdown by construction: comprehension is never at risk (recovery always
// exists, exactly as today); no server-state mutation; no cache-bust (pre-history,
// process-local). The only thing optimized is token economics, and the worst case
// is "behaves like fixed L2".

// adaptiveCompactionMinSamples is the minimum number of compactions of a class
// before its re-fetch rate is trusted enough to demote it to full-pass. Below
// this the control returns the fixed-behavior floor.
const adaptiveCompactionMinSamples = 5

// AdaptiveCompactionShouldFullPass reports whether a class whose output compacts
// to compactedRatio (= compacted_bytes / raw_bytes, in [0,1]) should be
// full-passed instead of compacted, given how many times the class was compacted
// (applied) and how many of those the model later re-fetched (refetch).
//
// The break-even re-fetch rate is exactly 1 - compactedRatio: per item, the
// compacted path costs compactedRatio + refetchRate*1.0 and the full path costs
// 1.0, so compaction nets non-negative iff refetchRate <= 1 - compactedRatio.
// Using this invocation's own ratio makes the threshold per-class and
// conservative (a barely-compacting class demotes readily; a hard-compacting
// class tolerates a high re-fetch rate before demotion).
func AdaptiveCompactionShouldFullPass(applied, refetch int, compactedRatio float64) bool {
	if applied < adaptiveCompactionMinSamples {
		return false // floor = fixed L2 behavior until the rate is trustworthy
	}
	if applied <= 0 {
		return false
	}
	if compactedRatio < 0 {
		compactedRatio = 0
	}
	if compactedRatio > 1 {
		compactedRatio = 1
	}
	breakEven := 1.0 - compactedRatio
	refetchRate := float64(refetch) / float64(applied)
	return refetchRate >= breakEven
}
