package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/config"
)

// soakFlags captures the CLI flags for `slimference soak`. T100b / T103c.
type soakFlags struct {
	json bool
}

// SoakReport summarises the quality / cache signals over the chosen
// window so an operator can decide whether enabling T100 (coordinator)
// or T103 (tool-prune) is safe against the soaked-in traffic.
//
// All fields are tolerant of missing data (zero values are preserved
// so the JSON output is stable even on a brand-new install).
type SoakReport struct {
	Period             string  `json:"period"`
	Days               int     `json:"days"`
	Snapshots          int     `json:"snapshots"`
	TotalRequests      int     `json:"total_requests"`
	AvgCompressionPct  float64 `json:"avg_compression_pct"`
	PromptCacheHitRate float64 `json:"prompt_cache_hit_rate"`
	PromptCacheTrend   string  `json:"prompt_cache_trend"`
	ErrorRatePct       float64 `json:"error_rate_pct"`
	OverflowRetries    int     `json:"overflow_retries"`
	RateLimitRetries   int     `json:"rate_limit_retries"`
	MiniMaxFailureRate float64 `json:"minimax_failure_rate"`
	// SafeForT100 / SafeForT103 are the headline verdicts: either the
	// data point at the trends hard enough to call enabling the flag a
	// regression risk, or it doesn't.
	SafeForT100 bool   `json:"safe_for_t100"`
	SafeForT103 bool   `json:"safe_for_t103"`
	Verdict     string `json:"verdict"`
}

func parseSoakArgs(args []string) (period string, f soakFlags, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json", "-json":
			f.json = true
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "-") {
				return "", f, fmt.Errorf("unknown flag: %s", a)
			}
			if period == "" {
				period = a
			} else {
				return "", f, fmt.Errorf("unexpected extra argument: %s", a)
			}
		}
	}
	if period == "" {
		period = "week"
	}
	return period, f, nil
}

// daysFor maps a period keyword to a day count. "today" is one day,
// "week" seven, "month" thirty, "all" three-hundred-sixty-five.
func daysFor(period string) (int, error) {
	switch period {
	case "today":
		return 1, nil
	case "week":
		return 7, nil
	case "month":
		return 30, nil
	case "all":
		return 365, nil
	}
	return 0, fmt.Errorf("invalid period %q (use today|week|month|all)", period)
}

// computeSoakReport walks the daily snapshot log directory and folds
// the snapshots into a single SoakReport for the supplied window.
func computeSoakReport(logDir, period string, now time.Time) (SoakReport, error) {
	days, err := daysFor(period)
	if err != nil {
		return SoakReport{}, err
	}
	rep := SoakReport{Period: period, Days: days}

	var totalReq int
	var totalOrig, totalSaved int
	var totalErr int
	var promptHits, promptReqs int
	var mmCalls, mmFails int

	// Collect a per-day prompt-cache hit-rate so we can detect drift.
	hitRates := make([]float64, 0, days)

	for i := 0; i < days; i++ {
		day := now.AddDate(0, 0, -i)
		snaps, err := analytics.ReadDailyStats(logDir, day)
		if err != nil {
			continue
		}
		var dayHits, dayReqs int
		for _, s := range snaps {
			rep.Snapshots++
			totalReq += s.TotalRequests
			totalOrig += s.TotalInputTokens
			totalSaved += s.SavedInputTokens
			totalErr += s.Errors
			rep.OverflowRetries += s.OverflowRetries
			rep.RateLimitRetries += s.RateLimitRetries
			promptHits += s.PromptCacheReadRequests
			promptReqs += s.TotalRequests
			dayHits += s.PromptCacheReadRequests
			dayReqs += s.TotalRequests
			mmCalls += s.MiniMaxCalls
			mmFails += s.MiniMaxFailures
		}
		if dayReqs > 0 {
			hitRates = append(hitRates, float64(dayHits)/float64(dayReqs))
		}
	}
	// Walking days newest -> oldest above; reverse to chronological so
	// classifyTrend's "first half" really means the older half.
	for i, j := 0, len(hitRates)-1; i < j; i, j = i+1, j-1 {
		hitRates[i], hitRates[j] = hitRates[j], hitRates[i]
	}

	rep.TotalRequests = totalReq
	if totalOrig > 0 {
		rep.AvgCompressionPct = float64(totalSaved) / float64(totalOrig) * 100
	}
	if promptReqs > 0 {
		rep.PromptCacheHitRate = float64(promptHits) / float64(promptReqs)
	}
	if totalReq > 0 {
		rep.ErrorRatePct = float64(totalErr) / float64(totalReq) * 100
	}
	if mmCalls > 0 {
		rep.MiniMaxFailureRate = float64(mmFails) / float64(mmCalls)
	}

	// Trend detection: compare the first half of the window against
	// the second half. >5 pp drop in prompt-cache hit rate is a hint
	// that compression-config drift hurt the cacheable prefix.
	rep.PromptCacheTrend = classifyTrend(hitRates)

	// Verdicts for T100b / T103c. The bar is intentionally
	// conservative: any of the listed signals in the bad half tips
	// the verdict to false. A zero-snapshot window also blocks both
	// flags so an empty install never produces a false greenlight.
	if rep.Snapshots == 0 {
		rep.SafeForT100 = false
		rep.SafeForT103 = false
	} else {
		rep.SafeForT100 = rep.ErrorRatePct < 1.0 &&
			rep.MiniMaxFailureRate < 0.05 &&
			rep.PromptCacheTrend != "regression" &&
			rep.OverflowRetries == 0
		rep.SafeForT103 = rep.ErrorRatePct < 1.0 &&
			rep.PromptCacheTrend != "regression"
	}

	switch {
	case rep.Snapshots == 0:
		rep.Verdict = "no data"
	case rep.SafeForT100 && rep.SafeForT103:
		rep.Verdict = "ok to enable both T100 and T103"
	case rep.SafeForT103 && !rep.SafeForT100:
		rep.Verdict = "T103 looks safe; T100 needs more soak time"
	case rep.SafeForT100 && !rep.SafeForT103:
		rep.Verdict = "T100 looks safe; T103 needs more soak time"
	default:
		rep.Verdict = "neither flag is safe yet"
	}
	return rep, nil
}

// classifyTrend compares the average of the first half of `rates`
// against the second half. Returns "regression" when the second half
// is more than 5 pp below the first, "improvement" when more than
// 5 pp above, "stable" otherwise. Empty / single-element input maps
// to "insufficient_data".
func classifyTrend(rates []float64) string {
	if len(rates) < 2 {
		return "insufficient_data"
	}
	mid := len(rates) / 2
	if mid == 0 {
		return "insufficient_data"
	}
	first := mean(rates[:mid])
	second := mean(rates[mid:])
	delta := second - first
	switch {
	case delta < -0.05:
		return "regression"
	case delta > 0.05:
		return "improvement"
	}
	return "stable"
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// formatSoakText renders a human-readable soak report. The verdict is
// the loud line; the rest is supporting evidence.
func formatSoakText(r SoakReport) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Slimference soak report (%s, %d days)\n", r.Period, r.Days))
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	if r.Snapshots == 0 {
		sb.WriteString("(no analytics snapshots found in window)\n")
		sb.WriteString("Verdict: " + r.Verdict + "\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("Snapshots:                  %d\n", r.Snapshots))
	sb.WriteString(fmt.Sprintf("Total requests:             %d\n", r.TotalRequests))
	sb.WriteString(fmt.Sprintf("Avg compression:            %.1f %%\n", r.AvgCompressionPct))
	sb.WriteString(fmt.Sprintf("Prompt-cache hit rate:      %.1f %%\n", r.PromptCacheHitRate*100))
	sb.WriteString(fmt.Sprintf("Prompt-cache trend:         %s\n", r.PromptCacheTrend))
	sb.WriteString(fmt.Sprintf("Error rate:                 %.2f %%\n", r.ErrorRatePct))
	sb.WriteString(fmt.Sprintf("Overflow retries:           %d\n", r.OverflowRetries))
	sb.WriteString(fmt.Sprintf("Rate-limit retries:         %d\n", r.RateLimitRetries))
	sb.WriteString(fmt.Sprintf("MiniMax failure rate:       %.2f %%\n", r.MiniMaxFailureRate*100))
	sb.WriteString(strings.Repeat("-", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Safe to enable T100 coordinator: %v\n", r.SafeForT100))
	sb.WriteString(fmt.Sprintf("Safe to enable T103 tool-prune:  %v\n", r.SafeForT103))
	sb.WriteString("Verdict: " + r.Verdict + "\n")
	return sb.String()
}

// handleSoakCmd implements `slimference soak [today|week|month|all]
// [--json]`. T100b / T103c data analysis surface that turns existing
// daily analytics + quality snapshots into a verdict on whether the
// operator can flip on the coordinator / tool-prune flags.
func handleSoakCmd(args []string) {
	period, flags, err := parseSoakArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		fmt.Fprintln(os.Stderr, "usage: slimference soak [today|week|month|all] [--json]")
		exitFn(1)
		return
	}
	if _, err := daysFor(period); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exitFn(1)
		return
	}
	cfg, err := configLoadFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	logDir := cfg.Analytics.ResolvedLogDir()
	report, err := computeSoakReport(logDir, period, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "soak report: %v\n", err)
		exitFn(1)
		return
	}
	if flags.json {
		out, _ := json.MarshalIndent(&report, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Print(formatSoakText(report))
}

// configLoadDefaultForSoak is a small adapter exposed for tests so the
// soak path can be exercised without booting a full config. Kept
// separate from the existing configLoadFn hook so the soak tests do
// not stomp other tests' overrides.
func configLoadDefaultForSoak() (*config.Config, error) {
	return config.Defaults(), nil
}
