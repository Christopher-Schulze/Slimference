package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/analytics"
)

var promptCacheMarshalIndent = json.MarshalIndent

type promptCacheStatsFlags struct {
	period string
	json   bool
	csv    bool
}

func parsePromptCacheStatsArgs(args []string) (promptCacheStatsFlags, error) {
	flags := promptCacheStatsFlags{period: "today"}
	for _, arg := range args {
		switch arg {
		case "--json", "-json":
			flags.json = true
		case "--csv":
			flags.csv = true
		default:
			if arg == "" {
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return flags, fmt.Errorf("unknown flag: %s", arg)
			}
			if flags.period != "today" {
				return flags, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			flags.period = arg
		}
	}
	if flags.json && flags.csv {
		return flags, fmt.Errorf("choose only one of --json or --csv")
	}
	switch flags.period {
	case "today", "week", "month", "all":
	default:
		return flags, fmt.Errorf("usage: slimference stats prompt-cache [today|week|month|all] [--json|--csv]")
	}
	return flags, nil
}

func handlePromptCacheStatsCmd(logDir string, args []string) {
	flags, err := parsePromptCacheStatsArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitFn(1)
	}
	report, err := analytics.ReadPromptCacheReport(logDir, flags.period, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "prompt-cache: %v\n", err)
		exitFn(1)
	}
	if report.TotalRequests == 0 {
		fmt.Printf("No prompt-cache stats for %s.\n", flags.period)
		return
	}
	if flags.json {
		data, err := promptCacheMarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "prompt-cache: %v\n", err)
			exitFn(1)
		}
		fmt.Println(string(data))
		return
	}
	if flags.csv {
		if err := analytics.WritePromptCacheCSV(os.Stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "prompt-cache: %v\n", err)
			exitFn(1)
		}
		return
	}

	fmt.Printf("Prompt cache stats (%s)\n", flags.period)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Requests:                %d\n", report.TotalRequests)
	fmt.Printf("Cache read requests:     %d (%.1f%% hit rate)\n", report.CacheReadRequests, report.HitRate*100)
	fmt.Printf("Cache read tokens:       %s\n", formatTokensPlain(report.CacheReadTokens))
	fmt.Printf("Cache create tokens:     %s\n", formatTokensPlain(report.CacheCreateTokens))
	fmt.Printf("Estimated read savings:  %s\n", formatTokensPlain(report.EstimatedSavedRead))
	fmt.Println(strings.Repeat("-", 50))
}
