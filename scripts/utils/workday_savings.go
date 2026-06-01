package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/control"
)

type workdaySavingsFlags struct {
	action       string
	baselineFile string
	aggregate    aggregateSavingsFlags
	help         bool
}

type workdaySavingsBaseline struct {
	SchemaVersion int                    `json:"schema_version"`
	StartedAt     time.Time              `json:"started_at"`
	Source        string                 `json:"source"`
	Report        aggregateSavingsReport `json:"report"`
}

type workdaySavingsResult struct {
	SchemaVersion int                    `json:"schema_version"`
	BaselineFile  string                 `json:"baseline_file"`
	StartedAt     time.Time              `json:"started_at,omitempty"`
	FinishedAt    time.Time              `json:"finished_at"`
	Duration      string                 `json:"duration,omitempty"`
	Baseline      aggregateSavingsReport `json:"baseline,omitempty"`
	Current       aggregateSavingsReport `json:"current"`
	Delta         aggregateSavingsReport `json:"delta"`
}

const workdaySavingsHelpText = `workday-savings: start/finish ceremony for real Codex workday savings

Usage:
  go run ./scripts/utils workday-savings start [flags]
  go run ./scripts/utils workday-savings finish [flags]

Flags:
  --baseline-file=<path>     Snapshot file (default ~/.slimference/workday-savings-baseline.json)
  --admin-url=<url>          Admin state endpoint (default http://127.0.0.1:8990/_slimference/admin/state)
  --admin-state-file=<path>  Read admin state from JSON file instead
  --filter-db=<path>         Filter SQLite DB for HTTP Layer-0 period reporting
  --period=<all|today|week|month>  Filter DB period (default today)
  --usd-per-million=<float>  USD cost per million tokens for estimate
  --json                     Output JSON
  --help                     Show this help

Start captures a baseline. Finish captures the current daemon state and prints
the counter delta. Close Codex CLI/Desktop sessions before finish so WSS counters
flush; mid-session WSS counters can under-report by design. The finish report also
keeps the current Codex route / auto-recert snapshot, so a measured window shows
whether it ended in Phase-F, WSS bridge, fallback, or pending repair.`

func runWorkdaySavings(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWorkdaySavingsFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, workdaySavingsHelpText)
		return 0
	}

	switch flags.action {
	case "start":
		return runWorkdaySavingsStart(flags, stdout, stderr)
	case "finish":
		return runWorkdaySavingsFinish(flags, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "workday-savings requires action start or finish")
		return 2
	}
}

func parseWorkdaySavingsFlags(args []string) (workdaySavingsFlags, error) {
	f := workdaySavingsFlags{
		aggregate: aggregateSavingsFlags{
			adminStateURL: "http://127.0.0.1:8990/_slimference/admin/state",
			period:        "today",
			outputFormat:  outputText,
		},
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "start" || a == "finish":
			if f.action != "" {
				return f, fmt.Errorf("multiple actions provided")
			}
			f.action = a
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--json":
			f.aggregate.outputFormat = outputJSON
		case a == "--baseline-file":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return f, err
			}
			f.baselineFile = v
		case strings.HasPrefix(a, "--baseline-file="):
			f.baselineFile = strings.TrimPrefix(a, "--baseline-file=")
		case a == "--admin-url":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return f, err
			}
			f.aggregate.adminStateURL = v
		case strings.HasPrefix(a, "--admin-url="):
			f.aggregate.adminStateURL = strings.TrimPrefix(a, "--admin-url=")
		case a == "--admin-state-file":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return f, err
			}
			f.aggregate.adminStateFile = v
		case strings.HasPrefix(a, "--admin-state-file="):
			f.aggregate.adminStateFile = strings.TrimPrefix(a, "--admin-state-file=")
		case a == "--filter-db":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return f, err
			}
			f.aggregate.filterDB = v
		case strings.HasPrefix(a, "--filter-db="):
			f.aggregate.filterDB = strings.TrimPrefix(a, "--filter-db=")
		case a == "--period":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return f, err
			}
			f.aggregate.period = v
		case strings.HasPrefix(a, "--period="):
			f.aggregate.period = strings.TrimPrefix(a, "--period=")
		case a == "--usd-per-million":
			v, err := aggregateFlagValue(args, &i, a)
			if err != nil {
				return f, err
			}
			if err := parseUSDPerMillion(v, &f.aggregate); err != nil {
				return f, err
			}
		case strings.HasPrefix(a, "--usd-per-million="):
			if err := parseUSDPerMillion(strings.TrimPrefix(a, "--usd-per-million="), &f.aggregate); err != nil {
				return f, err
			}
		default:
			return f, fmt.Errorf("unknown flag or action %q", a)
		}
	}
	if f.baselineFile == "" {
		path, err := defaultWorkdaySavingsBaselinePath()
		if err != nil {
			return f, err
		}
		f.baselineFile = path
	}
	if !validAggregatePeriod(f.aggregate.period) {
		return f, fmt.Errorf("--period must be one of: all, today, week, month")
	}
	return f, nil
}

func parseUSDPerMillion(raw string, flags *aggregateSavingsFlags) error {
	parsed, err := parseNonNegativeFloat(raw)
	if err != nil {
		return fmt.Errorf("--usd-per-million must be a non-negative number")
	}
	flags.usdPerMTokens = parsed
	return nil
}

func parseNonNegativeFloat(raw string) (float64, error) {
	out, err := strconv.ParseFloat(raw, 64)
	if err != nil || out < 0 {
		return 0, fmt.Errorf("bad float")
	}
	return out, nil
}

func runWorkdaySavingsStart(flags workdaySavingsFlags, stdout, stderr io.Writer) int {
	report, src, err := loadWorkdayAggregateReport(flags.aggregate)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	baseline := workdaySavingsBaseline{
		SchemaVersion: 1,
		StartedAt:     report.Generated,
		Source:        src,
		Report:        report,
	}
	if err := writeWorkdayBaseline(flags.baselineFile, baseline); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.aggregate.outputFormat == outputJSON {
		data, err := json.MarshalIndent(baseline, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, "=== Slimference Workday Savings Start ===")
	fmt.Fprintf(stdout, "Baseline: %s\n", flags.baselineFile)
	fmt.Fprintf(stdout, "Started:  %s\n", baseline.StartedAt.Format(time.RFC3339))
	fmt.Fprintln(stdout, "Run normal Slimference CLI/Desktop work. Before finish: close sessions so WSS counters flush.")
	fmt.Fprintln(stdout, "Finish:   go run ./scripts/utils workday-savings finish")
	return 0
}

func runWorkdaySavingsFinish(flags workdaySavingsFlags, stdout, stderr io.Writer) int {
	baseline, err := readWorkdayBaseline(flags.baselineFile)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	current, _, err := loadWorkdayAggregateReport(flags.aggregate)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	delta := diffAggregateSavingsReports(baseline.Report, current)
	finished := current.Generated
	result := workdaySavingsResult{
		SchemaVersion: 1,
		BaselineFile:  flags.baselineFile,
		StartedAt:     baseline.StartedAt,
		FinishedAt:    finished,
		Duration:      finished.Sub(baseline.StartedAt).Round(time.Second).String(),
		Baseline:      baseline.Report,
		Current:       current,
		Delta:         delta,
	}
	if flags.aggregate.outputFormat == outputJSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, "=== Slimference Workday Savings Finish ===")
	fmt.Fprintf(stdout, "Baseline: %s\n", flags.baselineFile)
	fmt.Fprintf(stdout, "Window:   %s to %s (%s)\n",
		baseline.StartedAt.Format(time.RFC3339), finished.Format(time.RFC3339), result.Duration)
	fmt.Fprintln(stdout)
	writeAggregateSavingsText(stdout, delta)
	return 0
}

func loadWorkdayAggregateReport(flags aggregateSavingsFlags) (aggregateSavingsReport, string, error) {
	state, src, err := loadAdminState(flags)
	if err != nil {
		return aggregateSavingsReport{}, "", err
	}
	report := buildAggregateSavingsReport(state, src, flags, time.Now().UTC())
	return report, src, nil
}

func writeWorkdayBaseline(path string, baseline workdaySavingsBaseline) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create baseline dir: %w", err)
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func readWorkdayBaseline(path string) (workdaySavingsBaseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return workdaySavingsBaseline{}, fmt.Errorf("read baseline %s: %w", path, err)
	}
	var baseline workdaySavingsBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return workdaySavingsBaseline{}, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return baseline, nil
}

func defaultWorkdaySavingsBaselinePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for baseline file: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("resolve home for baseline file: empty home")
	}
	return filepath.Join(home, ".slimference", "workday-savings-baseline.json"), nil
}

func diffAggregateSavingsReports(base, current aggregateSavingsReport) aggregateSavingsReport {
	out := current
	out.Source = base.Source + " -> " + current.Source
	out.CodexRoute = current.CodexRoute
	out.HostBudget = diffAggregateHostBudget(base.HostBudget, current.HostBudget)
	out.WSS = diffAggregateWSS(base.WSS, current.WSS)
	out.OutputReduce = diffAggregateOutputReduce(base.OutputReduce, current.OutputReduce)
	out.FilterLayer0 = diffFilterGainReports(base.FilterLayer0, current.FilterLayer0)
	out.Aggregate.WSSInputTokensSaved = out.WSS.InputTokensSaved
	if out.FilterLayer0 != nil {
		out.Aggregate.Layer0FilterTokensSaved = out.FilterLayer0.TokensSavedEst
	} else {
		out.Aggregate.Layer0FilterTokensSaved = 0
	}
	out.Aggregate.TotalTokensSaved = out.Aggregate.WSSInputTokensSaved + out.Aggregate.Layer0FilterTokensSaved
	if current.Aggregate.TotalTokensSaved > 0 && current.Aggregate.EstUSDSaved > 0 {
		rate := current.Aggregate.EstUSDSaved / float64(current.Aggregate.TotalTokensSaved)
		out.Aggregate.EstUSDSaved = float64(out.Aggregate.TotalTokensSaved) * rate
	} else {
		out.Aggregate.EstUSDSaved = 0
	}
	out.Notes = []string{
		"Delta between workday start and finish snapshots.",
		"Close Codex CLI/Desktop sessions before finish so WSS counters flush.",
		"Route-ready is not a savings claim; positive tokens saved and mutation counters are the proof.",
	}
	if out.WSS.ParseFailures+out.WSS.DegradedSessions+out.WSS.CompressionErrors == 0 {
		out.Notes = append(out.Notes, "No WSS parse/degrade/compression errors occurred in the measured window.")
	}
	if base.CodexRoute.AutoMode != current.CodexRoute.AutoMode || base.CodexRoute.AutoTransport != current.CodexRoute.AutoTransport {
		out.Notes = append(out.Notes, fmt.Sprintf("Codex auto route changed from %s/%s to %s/%s.",
			valueOrDash(base.CodexRoute.AutoMode), valueOrDash(base.CodexRoute.AutoTransport),
			valueOrDash(current.CodexRoute.AutoMode), valueOrDash(current.CodexRoute.AutoTransport)))
	}
	if base.CodexRoute.FallbackReason != current.CodexRoute.FallbackReason {
		out.Notes = append(out.Notes, fmt.Sprintf("Codex fallback reason changed from %s to %s.",
			valueOrDash(base.CodexRoute.FallbackReason), valueOrDash(current.CodexRoute.FallbackReason)))
	}
	if base.CodexRoute.RecertStatus != current.CodexRoute.RecertStatus {
		out.Notes = append(out.Notes, fmt.Sprintf("Recert status changed from %s to %s.",
			valueOrDash(base.CodexRoute.RecertStatus), valueOrDash(current.CodexRoute.RecertStatus)))
	}
	if base.CodexRoute.NeedsRecert != current.CodexRoute.NeedsRecert {
		out.Notes = append(out.Notes, fmt.Sprintf("needs_recert changed from %v to %v.",
			base.CodexRoute.NeedsRecert, current.CodexRoute.NeedsRecert))
	}
	if current.CodexRoute.NeedsRecert {
		out.Notes = append(out.Notes, "Finish snapshot still needs WSS recert repair.")
	}
	if current.CodexRoute.RecertAttemptID != "" && current.CodexRoute.RecertAttemptID != base.CodexRoute.RecertAttemptID {
		out.Notes = append(out.Notes, "Recert attempt changed during the measured window: "+current.CodexRoute.RecertAttemptID)
	}
	if current.HostBudget.Status != "" {
		out.Notes = append(out.Notes, fmt.Sprintf("Host budget finished as %s with RSS=%d bytes, CPU window=%.2f%%, disk write delta=%d, state=%d bytes.",
			current.HostBudget.Status,
			current.HostBudget.RSSBytes,
			current.HostBudget.CPUWindowPercent,
			current.HostBudget.DiskWriteOpsDelta,
			current.HostBudget.StateBytes))
	}
	if current.HostBudget.Exceeded {
		out.Notes = append(out.Notes, "Host resource budget exceeded during finish snapshot; managed reducers should have demoted/loosened.")
	}
	return out
}

func diffAggregateHostBudget(base, current aggregateHostBudgetBlock) aggregateHostBudgetBlock {
	out := current
	out.RSSBytes = current.RSSBytes
	out.CPUPercent = current.CPUPercent
	out.CPUWindowPercent = current.CPUWindowPercent
	out.DiskReadOps = nonNegativeDelta(current.DiskReadOps, base.DiskReadOps)
	out.DiskWriteOps = nonNegativeDelta(current.DiskWriteOps, base.DiskWriteOps)
	out.DiskReadOpsDelta = current.DiskReadOpsDelta
	out.DiskWriteOpsDelta = current.DiskWriteOpsDelta
	out.StateBytes = current.StateBytes
	out.Reasons = append([]string(nil), current.Reasons...)
	return out
}

func diffAggregateWSS(base, current aggregateWSSBlock) aggregateWSSBlock {
	return aggregateWSSBlock{
		PhasefBridged:             nonNegativeDelta(current.PhasefBridged, base.PhasefBridged),
		CompressedMessagesMutated: nonNegativeDelta(current.CompressedMessagesMutated, base.CompressedMessagesMutated),
		FramesReencoded:           nonNegativeDelta(current.FramesReencoded, base.FramesReencoded),
		PhasefMutations:           nonNegativeDelta(current.PhasefMutations, base.PhasefMutations),
		InputTokensSaved:          nonNegativeDelta(current.InputTokensSaved, base.InputTokensSaved),
		ProxyLayer0ToolResults:    nonNegativeDelta(current.ProxyLayer0ToolResults, base.ProxyLayer0ToolResults),
		ProxyLayer0ToolMisses:     nonNegativeDelta(current.ProxyLayer0ToolMisses, base.ProxyLayer0ToolMisses),
		ProxyLayer0Commands:       nonNegativeDelta(current.ProxyLayer0Commands, base.ProxyLayer0Commands),
		ProxyLayer0CommandMisses:  nonNegativeDelta(current.ProxyLayer0CommandMisses, base.ProxyLayer0CommandMisses),
		ProxyLayer0ReadAttempts:   nonNegativeDelta(current.ProxyLayer0ReadAttempts, base.ProxyLayer0ReadAttempts),
		ProxyLayer0ReadMisses:     nonNegativeDelta(current.ProxyLayer0ReadMisses, base.ProxyLayer0ReadMisses),
		ProxyLayer0Blocks:         nonNegativeDelta(current.ProxyLayer0Blocks, base.ProxyLayer0Blocks),
		ProxyLayer0ReadDelta:      nonNegativeDelta(current.ProxyLayer0ReadDelta, base.ProxyLayer0ReadDelta),
		ProxyLayer0Captured:       nonNegativeDelta(current.ProxyLayer0Captured, base.ProxyLayer0Captured),
		ProxyLayer0Envelope:       nonNegativeDelta(current.ProxyLayer0Envelope, base.ProxyLayer0Envelope),
		ProxyLayer0Repeated:       nonNegativeDelta(current.ProxyLayer0Repeated, base.ProxyLayer0Repeated),
		ProxyLayer0ChunkDedup:     nonNegativeDelta(current.ProxyLayer0ChunkDedup, base.ProxyLayer0ChunkDedup),
		ProxyLayer0Routes:         diffProxyLayer0Routes(base.ProxyLayer0Routes, current.ProxyLayer0Routes),
		ProxyLayer0Policy:         diffProxyLayer0Policy(base.ProxyLayer0Policy, current.ProxyLayer0Policy),
		ParseFailures:             nonNegativeDelta(current.ParseFailures, base.ParseFailures),
		DegradedSessions:          nonNegativeDelta(current.DegradedSessions, base.DegradedSessions),
		CompressionErrors:         nonNegativeDelta(current.CompressionErrors, base.CompressionErrors),
		MutationActive:            current.MutationActive,
		ByteBridgeOnly:            current.ByteBridgeOnly,
	}
}

func diffAggregateOutputReduce(base, current aggregateOutputReduceBlock) aggregateOutputReduceBlock {
	return aggregateOutputReduceBlock{
		OutputWireBytesSaved:    nonNegativeDelta(current.OutputWireBytesSaved, base.OutputWireBytesSaved),
		RequestSideBytesReduced: nonNegativeDelta(current.RequestSideBytesReduced, base.RequestSideBytesReduced),
		RepdetRewrites:          nonNegativeDelta(current.RepdetRewrites, base.RepdetRewrites),
		RepdetBytesSaved:        nonNegativeDelta(current.RepdetBytesSaved, base.RepdetBytesSaved),
		StaleReadBlocks:         nonNegativeDelta(current.StaleReadBlocks, base.StaleReadBlocks),
		ObsoletePruneBlocks:     nonNegativeDelta(current.ObsoletePruneBlocks, base.ObsoletePruneBlocks),
		StopSeqInjections:       nonNegativeDelta(current.StopSeqInjections, base.StopSeqInjections),
		BeterseInjections:       nonNegativeDelta(current.BeterseInjections, base.BeterseInjections),
		StreamcutFires:          nonNegativeDelta(current.StreamcutFires, base.StreamcutFires),
	}
}

func diffProxyLayer0Routes(base, current control.ProxyLayer0RoutesSummary) control.ProxyLayer0RoutesSummary {
	return control.ProxyLayer0RoutesSummary{
		HTTP:      diffProxyLayer0Route(base.HTTP, current.HTTP),
		WSSPhaseF: diffProxyLayer0Route(base.WSSPhaseF, current.WSSPhaseF),
	}
}

func diffProxyLayer0Route(base, current control.ProxyLayer0RouteSummary) control.ProxyLayer0RouteSummary {
	return control.ProxyLayer0RouteSummary{
		ToolResults:      nonNegativeDelta(current.ToolResults, base.ToolResults),
		ToolMisses:       nonNegativeDelta(current.ToolMisses, base.ToolMisses),
		Commands:         nonNegativeDelta(current.Commands, base.Commands),
		CommandMisses:    nonNegativeDelta(current.CommandMisses, base.CommandMisses),
		ReadAttempts:     nonNegativeDelta(current.ReadAttempts, base.ReadAttempts),
		ReadMisses:       nonNegativeDelta(current.ReadMisses, base.ReadMisses),
		RequestsModified: nonNegativeDelta(current.RequestsModified, base.RequestsModified),
		TokensSaved:      nonNegativeDelta(current.TokensSaved, base.TokensSaved),
		BlocksModified:   nonNegativeDelta(current.BlocksModified, base.BlocksModified),
		ReadDeltaBlocks:  nonNegativeDelta(current.ReadDeltaBlocks, base.ReadDeltaBlocks),
		CapturedBlocks:   nonNegativeDelta(current.CapturedBlocks, base.CapturedBlocks),
		EnvelopeBlocks:   nonNegativeDelta(current.EnvelopeBlocks, base.EnvelopeBlocks),
		RepeatedBlocks:   nonNegativeDelta(current.RepeatedBlocks, base.RepeatedBlocks),
		ChunkDedupBlocks: nonNegativeDelta(current.ChunkDedupBlocks, base.ChunkDedupBlocks),
	}
}

func diffProxyLayer0Policy(base, current []control.ProxyLayer0PolicyEntry) []control.ProxyLayer0PolicyEntry {
	if len(current) == 0 {
		return nil
	}
	baseByKey := make(map[string]int64, len(base))
	for _, entry := range base {
		baseByKey[proxyLayer0PolicyEntryKey(entry)] = entry.Count
	}
	out := make([]control.ProxyLayer0PolicyEntry, 0, len(current))
	for _, entry := range current {
		entry.Count = nonNegativeDelta(entry.Count, baseByKey[proxyLayer0PolicyEntryKey(entry)])
		if entry.Count > 0 {
			out = append(out, entry)
		}
	}
	return out
}

func proxyLayer0PolicyEntryKey(entry control.ProxyLayer0PolicyEntry) string {
	return entry.Route + "\x00" + entry.Mechanism + "\x00" + entry.Action + "\x00" + entry.Reason + "\x00" + entry.BlockReason
}

func diffFilterGainReports(base, current *analytics.FilterGainReport) *analytics.FilterGainReport {
	if base == nil || current == nil {
		return nil
	}
	out := *current
	out.Runs = nonNegativeDelta(current.Runs, base.Runs)
	out.InputTokens = nonNegativeDelta(current.InputTokens, base.InputTokens)
	out.OutputTokens = nonNegativeDelta(current.OutputTokens, base.OutputTokens)
	out.TokensSavedEst = nonNegativeDelta(current.TokensSavedEst, base.TokensSavedEst)
	out.SavingsUsdEst = nonNegativeFloatDelta(current.SavingsUsdEst, base.SavingsUsdEst)
	out.ByCommand = nil
	out.ByParser = nil
	return &out
}

func nonNegativeDelta(current, base int64) int64 {
	if current <= base {
		return 0
	}
	return current - base
}

func nonNegativeFloatDelta(current, base float64) float64 {
	if current <= base {
		return 0
	}
	return current - base
}
