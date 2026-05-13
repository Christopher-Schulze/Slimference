package main

import (
	"fmt"
	"os"

	"github.com/slimference/slimference/internal/config"
)

func handleOutputReduceCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference output-reduce <enable|disable|status>")
		exitFn(1)
		return
	}
	switch args[0] {
	case "enable":
		handleOutputReduceSet(true)
	case "disable":
		handleOutputReduceSet(false)
	case "status":
		handleOutputReduceStatus()
	default:
		fmt.Fprintf(os.Stderr, "output-reduce: unknown subcommand %q (enable|disable|status)\n", args[0])
		exitFn(1)
	}
}

func handleOutputReduceSet(enabled bool) {
	cfg, info, err := config.LoadWithOptions(config.LoadOptions{ExplicitPath: explicitConfigPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	if cfg.Compression.OutputReduce.Enabled == enabled {
		fmt.Printf("output-reduce: already %s\n", boolStr(enabled, "enabled", "disabled"))
		return
	}
	cfg.Compression.OutputReduce.Enabled = enabled
	path := resolvedOrFallback(info)
	if err := writeConfigUpdate(path, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "output-reduce %s: %v\n", boolStr(enabled, "enable", "disable"), err)
		exitFn(1)
		return
	}
	fmt.Printf("output-reduce: %s (config written to %s)\n", boolStr(enabled, "enabled", "disabled"), path)
}

func handleOutputReduceStatus() {
	cfg, _, err := config.LoadWithOptions(config.LoadOptions{ExplicitPath: explicitConfigPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	or := cfg.Compression.OutputReduce
	fmt.Println("Output-reduce:")
	fmt.Printf("  Enabled:          %s\n", boolStr(or.Enabled, "yes", "no"))
	fmt.Printf("  Profile:          %s\n", or.Profile)
	fmt.Printf("  Marker:           %s\n", or.SignatureMarker)
	fmt.Printf("  Max added bytes:  %d\n", or.MaxAddedBytes)
	fmt.Printf("  Min input tokens: %d\n", or.MinInputTokens)
	fmt.Printf("  Auto tune:        %s\n", boolStr(or.AutoTuneEnabled, "yes", "no"))
	fmt.Printf("  Min samples:      %d\n", or.AutoTuneMinSamples)
	fmt.Printf("  Min net saving:   %.1f%%\n", or.MinNetSavingsPct)
	fmt.Printf("  Max failure delta: %.3f\n", or.MaxFailureRateDelta)
	fmt.Printf("  Cooldown turns:   %d\n", or.CooldownTurns)
	if or.CustomDirectivePath != "" {
		fmt.Printf("  Custom directive: %s\n", or.CustomDirectivePath)
	}
}
